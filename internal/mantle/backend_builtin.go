package mantle

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// builtinBackend describes a server binary baked into the image (see
// docker/Dockerfile) and installed on PATH rather than in the backends dir.
type builtinBackend struct {
	Name       string // display/API name, reserved in the backends dir
	Binary     string // command name as it appears in config cmd lines
	Kind       string // "llm", "image" or "transcription"
	Repo       string
	VersionKey string // key in versionsFile written at image build time
}

var builtinBackends = []builtinBackend{
	{Name: "llama.cpp", Binary: "llama-server", Kind: "llm", Repo: "https://github.com/srossitto79/llama.cpp", VersionKey: "llama.cpp"},
	{Name: "ik_llama.cpp", Binary: "ik-llama-server", Kind: "llm", Repo: "https://github.com/ikawrakow/ik_llama.cpp", VersionKey: "ik_llama.cpp"},
	{Name: "stable-diffusion.cpp", Binary: "sd-server", Kind: "image", Repo: "https://github.com/leejet/stable-diffusion.cpp", VersionKey: "stable-diffusion.cpp"},
	{Name: "whisper.cpp", Binary: "whisper-server", Kind: "transcription", Repo: "https://github.com/ggml-org/whisper.cpp", VersionKey: "whisper.cpp"},
}

// versionsFile holds the commit hashes the image was built from; overridable
// in tests.
var versionsFile = "/versions.txt"

// lookPath is exec.LookPath, overridable in tests.
var lookPath = exec.LookPath

func findBuiltinBackend(name string) (builtinBackend, bool) {
	for _, b := range builtinBackends {
		if b.Name == name {
			return b, true
		}
	}
	return builtinBackend{}, false
}

// isBuiltinBackendName reports whether name is reserved for a built-in
// backend, so it can't be built, updated or deleted in the backends dir.
func isBuiltinBackendName(name string) bool {
	_, ok := findBuiltinBackend(name)
	return ok
}

// listBuiltinBackends returns the built-in backends whose binary is found on
// PATH. Path is the bare command name, matching how config cmd lines invoke
// them, so the config editor can match argv[0] against it.
func listBuiltinBackends() []BackendEntry {
	versions := readVersionsFile(versionsFile)
	entries := []BackendEntry{}
	for _, b := range builtinBackends {
		resolved, err := lookPath(b.Binary)
		if err != nil {
			continue
		}
		entry := BackendEntry{
			Name:    b.Name,
			Path:    b.Binary,
			Repo:    b.Repo,
			Branch:  versions[b.VersionKey],
			Builtin: true,
			Kind:    b.Kind,
		}
		if fi, err := os.Stat(resolved); err == nil {
			entry.Size = fi.Size()
		}
		entries = append(entries, entry)
	}
	return entries
}

// readVersionsFile parses "key: value" lines; a missing file yields an empty
// map.
func readVersionsFile(path string) map[string]string {
	versions := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return versions
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		versions[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return versions
}

// LoadOrBuildBuiltinSchema is LoadOrBuildBackendSchema for a built-in
// backend. The binary lives in a read-only system dir, so its schema is
// cached under backendsDir/.builtin instead.
func LoadOrBuildBuiltinSchema(backendsDir, name string) (*BackendSchema, error) {
	b, ok := findBuiltinBackend(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	binPath, err := lookPath(b.Binary)
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(backendsDir, ".builtin")
	_ = os.MkdirAll(cacheDir, 0755)
	return loadOrBuildSchema(name, binPath, filepath.Join(cacheDir, name+".schema.json"))
}
