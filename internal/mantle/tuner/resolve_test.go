package tuner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type testFlag struct {
	Names   []string `json:"names"`
	Choices []string `json:"choices,omitempty"`
}

func baseFlags() []testFlag {
	return []testFlag{
		{Names: []string{"--cpu-moe", "-cmoe"}},
		{Names: []string{"--n-cpu-moe", "-ncmoe"}},
		{Names: []string{"--load-mode", "-lm"}},
		{Names: []string{"--spec-type"}},
		{Names: []string{"-md", "--model-draft", "--spec-draft-model"}},
		{Names: []string{"--spec-draft-n-max"}},
		{Names: []string{"-ctk", "--cache-type-k"}, Choices: []string{"f16", "q8_0", "q4_0"}},
	}
}

// fakeServer stands in for a llama-server binary: just an architecture name
// table, as LLM_ARCH_NAMES leaves in the real one.
var fakeServer = []byte("\x00clip\x00llama\x00qwen3next\x00qwen4exp\x00deepseek41\x00peg-gemma4\x00")

// writeTestBackend creates <root>/<name> with an empty llama-server and,
// when flags is non-nil, a schema.json and meta.json.
func writeTestBackend(t *testing.T, root, name, repo string, flags []testFlag, withTune bool) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "llama-server"), fakeServer, 0o755); err != nil {
		t.Fatal(err)
	}
	if flags != nil {
		raw, err := json.Marshal(map[string]any{"backend": name, "flags": flags})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "schema.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		meta, err := json.Marshal(map[string]string{"repo": repo})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withTune {
		if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tools", "moe-cache-tune.py"), []byte("# tune\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func testBackendsDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	upstream := append(baseFlags(),
		testFlag{Names: []string{"--moe-cache-mib"}},
		testFlag{Names: []string{"--moe-cache-cpu-share"}},
	)
	writeTestBackend(t, root, "qwen4exp_mtp", "https://github.com/danielhanchen/llama.cpp.git", baseFlags(), false)
	writeTestBackend(t, root, "ggml-org", "https://github.com/ggml-org/llama.cpp.git", upstream, true)
	// Ignored: no schema.json, and an unfinished build directory.
	writeTestBackend(t, root, "noschema", "", nil, false)
	writeTestBackend(t, root, "x.building-1", "https://github.com/ggml-org/llama.cpp.git", upstream, true)
	// Ignored: a plain file.
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func moeModel() ModelInfo {
	return ModelInfo{Arch: "qwen3next", ExpertCount: 128, ExpertBytes: 1 << 30, DenseBytes: 1 << 30, HasTokenEmbd: true}
}

func fitByName(t *testing.T, fits []BackendFit, name string) BackendFit {
	t.Helper()
	for _, f := range fits {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("backend %q not resolved", name)
	return BackendFit{}
}

func TestTuner_ResolveFeaturesAndSorting(t *testing.T) {
	root := testBackendsDir(t)
	fits, err := NewCapabilityResolver(root, "/backends").Resolve(moeModel(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fits) != 2 {
		t.Fatalf("got %d backends, want 2: %+v", len(fits), fits)
	}
	if fits[0].Name != "ggml-org" || fits[1].Name != "qwen4exp_mtp" {
		t.Fatalf("order: %s, %s", fits[0].Name, fits[1].Name)
	}
	g, q := fits[0], fits[1]
	if g.Binary != "/backends/ggml-org/llama-server" {
		t.Fatalf("binary = %q", g.Binary)
	}
	for _, f := range []Feature{FeatureCPUMoE, FeatureNCPUMoE, FeatureLoadMode, FeatureSpecType, FeatureDraftModel, FeatureSpecDraftNMax} {
		if !g.Features[f] || !q.Features[f] {
			t.Errorf("feature %s missing (ggml-org %v, qwen4exp_mtp %v)", f, g.Features[f], q.Features[f])
		}
	}
	for _, f := range []Feature{FeatureMoECache, FeatureMoECacheCPUShare, FeatureMoECacheTune} {
		if !g.Features[f] {
			t.Errorf("ggml-org lacks %s", f)
		}
		if q.Features[f] {
			t.Errorf("qwen4exp_mtp has %s", f)
		}
	}
	if !contains(g.CacheTypes, "q8_0") || !contains(q.CacheTypes, "q8_0") {
		t.Fatalf("cache types: %v / %v", g.CacheTypes, q.CacheTypes)
	}
	if g.Blocked != "" || q.Blocked != "" {
		t.Fatalf("blocked: %q / %q", g.Blocked, q.Blocked)
	}
	if g.Draft != nil || q.Draft != nil {
		t.Fatal("Draft set without a draft file")
	}
}

func TestTuner_ResolveTuneNeedsCPUShare(t *testing.T) {
	root := t.TempDir()
	// The tune script ships with the build but the CPU-share flag is absent.
	flags := append(baseFlags(), testFlag{Names: []string{"--moe-cache-mib"}})
	writeTestBackend(t, root, "old", "https://github.com/ggml-org/llama.cpp.git", flags, true)
	fits, err := NewCapabilityResolver(root, root).Resolve(moeModel(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := fitByName(t, fits, "old")
	if !f.Features[FeatureMoECache] || f.Features[FeatureMoECacheTune] {
		t.Fatalf("features: %v", f.Features)
	}
}

func TestTuner_ResolveMTPDraftWithoutTokenEmbd(t *testing.T) {
	root := testBackendsDir(t)
	draft := ModelInfo{Path: "/models/mtp.gguf", Arch: "qwen3next", NextNLayers: 1, HasTokenEmbd: false}
	fits, err := NewCapabilityResolver(root, "/backends").Resolve(moeModel(), &draft)
	if err != nil {
		t.Fatal(err)
	}
	g := fitByName(t, fits, "ggml-org")
	if g.Draft == nil || g.Draft.OK || g.Draft.Reason != "MTP file has no token_embd" || !g.Draft.MTP {
		t.Fatalf("ggml-org draft verdict: %+v", g.Draft)
	}
	if g.Draft.Path != "/models/mtp.gguf" {
		t.Fatalf("draft path = %q", g.Draft.Path)
	}
	q := fitByName(t, fits, "qwen4exp_mtp")
	if q.Draft == nil || !q.Draft.OK || !q.Draft.MTP {
		t.Fatalf("qwen4exp_mtp draft verdict: %+v", q.Draft)
	}
}

func TestTuner_ResolveMTPDraftWithTokenEmbd(t *testing.T) {
	root := testBackendsDir(t)
	draft := ModelInfo{Path: "/models/mtp.gguf", Arch: "qwen3next", NextNLayers: 1, HasTokenEmbd: true}
	fits, err := NewCapabilityResolver(root, "/backends").Resolve(moeModel(), &draft)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fits {
		if f.Draft == nil || !f.Draft.OK {
			t.Errorf("%s: draft rejected: %+v", f.Name, f.Draft)
		}
	}
}

func TestTuner_CheckDraftVerdicts(t *testing.T) {
	full := BackendFit{Features: map[Feature]bool{FeatureSpecType: true, FeatureDraftModel: true}}
	model := ModelInfo{Arch: "qwen3"}

	if dc := checkDraft(BackendFit{Features: map[Feature]bool{}}, metaFile{}, model, ModelInfo{Arch: "qwen3"}); dc.OK || dc.Reason != "no draft model support" {
		t.Fatalf("unsupported backend: %+v", dc)
	}
	if dc := checkDraft(full, metaFile{}, model, ModelInfo{Arch: "qwen3moe"}); !dc.OK {
		t.Fatalf("same family: %+v", dc)
	}
	dc := checkDraft(full, metaFile{}, model, ModelInfo{Arch: "llama"})
	if dc.OK || dc.Reason != "draft arch llama, model arch qwen3" {
		t.Fatalf("different family: %+v", dc)
	}
}

func TestTuner_BlockedReason(t *testing.T) {
	moe := moeModel()
	noOffload := BackendFit{Features: map[Feature]bool{}, CacheTypes: []string{"q8_0"}}
	if got := blockedReason(noOffload, moe); got != "no expert offload" {
		t.Fatalf("got %q", got)
	}
	noQ8 := BackendFit{Features: map[Feature]bool{FeatureCPUMoE: true}, CacheTypes: []string{"f16"}}
	if got := blockedReason(noQ8, moe); got != "no q8_0 KV cache" {
		t.Fatalf("got %q", got)
	}
	ok := BackendFit{Features: map[Feature]bool{FeatureCPUMoE: true}, CacheTypes: []string{"q8_0"}}
	if got := blockedReason(ok, moe); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestTuner_ResolveMissingDir(t *testing.T) {
	if _, err := NewCapabilityResolver(filepath.Join(t.TempDir(), "absent"), "/backends").Resolve(moeModel(), nil); err == nil {
		t.Fatal("expected an error for a missing backends directory")
	}
}

// copyRealBackend copies a local backend's schema.json and
// meta.json into root. It skips the test when the files are absent
// (backends/ is gitignored).
func copyRealBackend(t *testing.T, root, name string) {
	t.Helper()
	src := filepath.Join("..", "..", "..", "backends", name)
	schema, err := os.ReadFile(filepath.Join(src, "schema.json"))
	if err != nil {
		t.Skipf("backends/%s/schema.json not available: %v", name, err)
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	if meta, err := os.ReadFile(filepath.Join(src, "meta.json")); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "llama-server"), fakeServer, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestTuner_ResolveRealSchemas(t *testing.T) {
	root := t.TempDir()
	copyRealBackend(t, root, "ggml-org")
	copyRealBackend(t, root, "qwen4exp_mtp")
	fits, err := NewCapabilityResolver(root, "/backends").Resolve(moeModel(), nil)
	if err != nil {
		t.Fatal(err)
	}
	g := fitByName(t, fits, "ggml-org")
	q := fitByName(t, fits, "qwen4exp_mtp")
	if !g.Features[FeatureMoECache] || !g.Features[FeatureMoECacheCPUShare] {
		t.Errorf("ggml-org features: %v", g.Features)
	}
	if q.Features[FeatureMoECache] || q.Features[FeatureMoECacheCPUShare] {
		t.Errorf("qwen4exp_mtp features: %v", q.Features)
	}
}

func TestTuner_ResolveBlocksUnknownArch(t *testing.T) {
	root := t.TempDir()
	writeTestBackend(t, root, "fork", "", []testFlag{{Names: []string{"--n-cpu-moe"}}, {Names: []string{"--cache-type-k"}, Choices: []string{"q8_0"}}}, false)
	r := NewCapabilityResolver(root, "/backends")
	for _, tc := range []struct {
		arch, blocked string
	}{
		{"qwen4exp", ""},
		{"gemma4", ""}, // merged into "peg-gemma4": a suffix match still finds it
		{"gemma", "no gemma support"},
		{"deepseek4", "no deepseek4 support"}, // a prefix of deepseek41 is not a match
	} {
		fits, err := r.Resolve(ModelInfo{Arch: tc.arch, ExpertCount: 8}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if fits[0].Blocked != tc.blocked {
			t.Errorf("%s: Blocked = %q, want %q", tc.arch, fits[0].Blocked, tc.blocked)
		}
	}
}
