package mantle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// stubBuiltins points lookPath at a temp bin dir holding the given binaries
// and versionsFile at a temp file with the given contents.
func stubBuiltins(t *testing.T, binaries []string, versions string) string {
	t.Helper()
	binDir := t.TempDir()
	for _, b := range binaries {
		if err := os.WriteFile(filepath.Join(binDir, b), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	vf := filepath.Join(t.TempDir(), "versions.txt")
	if err := os.WriteFile(vf, []byte(versions), 0644); err != nil {
		t.Fatal(err)
	}
	origLook, origVersions := lookPath, versionsFile
	lookPath = func(name string) (string, error) {
		p := filepath.Join(binDir, name)
		if _, err := os.Stat(p); err != nil {
			return "", exec.ErrNotFound
		}
		return p, nil
	}
	versionsFile = vf
	t.Cleanup(func() { lookPath, versionsFile = origLook, origVersions })
	return binDir
}

func TestListBackends_IncludesBuiltinsOnPath(t *testing.T) {
	stubBuiltins(t, []string{"llama-server", "whisper-server"},
		"llama.cpp: abc123\nwhisper.cpp: def456\nbackend: cuda\n")

	backendsDir := t.TempDir()
	compiled := filepath.Join(backendsDir, "my-fork")
	os.MkdirAll(compiled, 0755)
	os.WriteFile(filepath.Join(compiled, "llama-server"), []byte("x"), 0755)
	// A dir that shadows a built-in name is hidden, not listed twice.
	shadow := filepath.Join(backendsDir, "llama.cpp")
	os.MkdirAll(shadow, 0755)
	os.WriteFile(filepath.Join(shadow, "llama-server"), []byte("x"), 0755)

	got, err := ListBackends(backendsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d backends, want 3: %+v", len(got), got)
	}
	llama, whisper, fork := got[0], got[1], got[2]
	if llama.Name != "llama.cpp" || llama.Path != "llama-server" || !llama.Builtin || llama.Kind != "llm" || llama.Branch != "abc123" {
		t.Errorf("unexpected llama.cpp entry: %+v", llama)
	}
	if whisper.Name != "whisper.cpp" || whisper.Kind != "transcription" || whisper.Branch != "def456" {
		t.Errorf("unexpected whisper.cpp entry: %+v", whisper)
	}
	if fork.Name != "my-fork" || fork.Builtin {
		t.Errorf("unexpected compiled entry: %+v", fork)
	}
}

func TestListBackends_BuiltinsWithoutBackendsDir(t *testing.T) {
	stubBuiltins(t, []string{"ik-llama-server"}, "")
	got, err := ListBackends(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "ik_llama.cpp" || got[0].Branch != "" {
		t.Fatalf("unexpected backends: %+v", got)
	}
}

func TestLoadOrBuildBuiltinSchema_CachesUnderBackendsDir(t *testing.T) {
	binDir := stubBuiltins(t, nil, "")
	script := "#!/bin/sh\necho '-t,    --threads N    number of threads (default: 4)'\n"
	if err := os.WriteFile(filepath.Join(binDir, "llama-server"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	backendsDir := t.TempDir()

	schema, err := LoadOrBuildBuiltinSchema(backendsDir, "llama.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if f := findFlag(t, schema.Flags, "--threads"); f.Default != "4" {
		t.Errorf("default = %q, want 4", f.Default)
	}
	if _, err := os.Stat(filepath.Join(backendsDir, ".builtin", "llama.cpp.schema.json")); err != nil {
		t.Errorf("schema not cached: %v", err)
	}
	if _, err := LoadOrBuildBuiltinSchema(backendsDir, "nope"); err == nil {
		t.Error("expected error for unknown built-in")
	}
}
