package tuner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// flagFeatures maps a flag name in a backend's schema.json to a feature.
var flagFeatures = map[string]Feature{
	"--cpu-moe":             FeatureCPUMoE,
	"--n-cpu-moe":           FeatureNCPUMoE,
	"--moe-cache-mib":       FeatureMoECache,
	"--moe-cache-cpu-share": FeatureMoECacheCPUShare,
	"--fit":                 FeatureFit,
	"--load-mode":           FeatureLoadMode,
	"--spec-type":           FeatureSpecType,
	"--model-draft":         FeatureDraftModel,
	"--spec-draft-model":    FeatureDraftModel,
	"--spec-draft-n-max":    FeatureSpecDraftNMax,
}

// upstreamRepo marks a backend built from upstream llama.cpp, whose MTP
// loader requires token_embd in a standalone MTP file.
const upstreamRepo = "github.com/ggml-org/llama.cpp"

// NewCapabilityResolver reads backends from backendsDir (host path), and
// names their binaries under binDir (the path config.yaml uses, /backends in
// the container). Both are usually the same directory.
func NewCapabilityResolver(backendsDir, binDir string) CapabilityResolver {
	return &schemaResolver{dir: backendsDir, binDir: binDir, archs: map[archKey]bool{}}
}

type schemaResolver struct {
	dir    string
	binDir string

	mu    sync.Mutex
	archs map[archKey]bool // binary build + architecture -> supported
}

// archKey identifies one build of a binary, so a rebuild is re-checked.
type archKey struct {
	path string
	size int64
	mod  time.Time
	arch string
}

type schemaFile struct {
	Flags []struct {
		Names   []string `json:"names"`
		Choices []string `json:"choices"`
	} `json:"flags"`
}

type metaFile struct {
	Repo string `json:"repo"`
}

func (r *schemaResolver) Resolve(model ModelInfo, draft *ModelInfo) ([]BackendFit, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, fmt.Errorf("read backends: %w", err)
	}
	var fits []BackendFit
	for _, e := range entries {
		if !e.IsDir() || strings.Contains(e.Name(), ".building-") {
			continue
		}
		dir := filepath.Join(r.dir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "llama-server")); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "schema.json"))
		if err != nil {
			continue // not a llama.cpp backend the tuner can reason about
		}
		var s schemaFile
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		var meta metaFile
		if mb, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
			_ = json.Unmarshal(mb, &meta)
		}
		_, tuneErr := os.Stat(filepath.Join(dir, "tools", "moe-cache-tune.py"))
		fit := resolveBackend(e.Name(), path.Join(r.binDir, e.Name(), "llama-server"), s, meta, tuneErr == nil)
		fit.Blocked = blockedReason(fit, model)
		if fit.Blocked == "" && model.Arch != "" && !r.supportsArch(filepath.Join(dir, "llama-server"), model.Arch) {
			fit.Blocked = "no " + model.Arch + " support"
		}
		if draft != nil {
			fit.Draft = checkDraft(fit, meta, model, *draft)
		}
		fits = append(fits, fit)
	}
	sort.Slice(fits, func(i, j int) bool { return fits[i].Name < fits[j].Name })
	return fits, nil
}

// supportsArch reports whether a llama-server binary knows an architecture.
// llama.cpp names architectures with C strings (LLM_ARCH_NAMES), so the name
// followed by NUL appears in the binary. It is matched as a suffix, not
// between NULs: the linker merges a name into a longer string ending with it
// (gemma4 lives inside "peg-gemma4"). The check can only err toward allowing
// a backend, never toward blocking one that works. An unreadable binary is
// allowed for the same reason.
func (r *schemaResolver) supportsArch(bin, arch string) bool {
	st, err := os.Stat(bin)
	if err != nil {
		return true
	}
	k := archKey{path: bin, size: st.Size(), mod: st.ModTime(), arch: arch}
	r.mu.Lock()
	ok, hit := r.archs[k]
	r.mu.Unlock()
	if hit {
		return ok
	}
	raw, err := os.ReadFile(bin)
	if err != nil {
		return true
	}
	ok = binaryHasArch(raw, arch)
	r.mu.Lock()
	r.archs[k] = ok
	r.mu.Unlock()
	return ok
}

func binaryHasArch(raw []byte, arch string) bool {
	needle := make([]byte, 0, len(arch)+1)
	needle = append(append(needle, arch...), 0)
	return bytes.Contains(raw, needle)
}

func resolveBackend(name, bin string, s schemaFile, meta metaFile, hasTune bool) BackendFit {
	fit := BackendFit{Name: name, Binary: bin, Features: map[Feature]bool{}}
	for _, f := range s.Flags {
		for _, n := range f.Names {
			if feat, ok := flagFeatures[n]; ok {
				fit.Features[feat] = true
			}
			if n == "--cache-type-k" {
				fit.CacheTypes = f.Choices
			}
		}
	}
	// The script replays this build's cache logic; it is only usable with the
	// CPU-share feature from the same change (llama.cpp #30966).
	fit.Features[FeatureMoECacheTune] = hasTune && fit.Features[FeatureMoECacheCPUShare]
	return fit
}

// blockedReason returns why a backend cannot run the model at all.
func blockedReason(fit BackendFit, model ModelInfo) string {
	if model.IsMoE() && !fit.Features[FeatureCPUMoE] && !fit.Features[FeatureNCPUMoE] && !fit.Features[FeatureFit] {
		return "no expert offload"
	}
	if !contains(fit.CacheTypes, "q8_0") {
		return "no q8_0 KV cache"
	}
	return ""
}

// checkDraft decides whether a draft file loads on a backend. A file with
// NextN layers but no output head is a standalone MTP file.
func checkDraft(fit BackendFit, meta metaFile, model, draft ModelInfo) *DraftCompat {
	dc := &DraftCompat{Path: draft.Path, MTP: draft.NextNLayers > 0}
	switch {
	case !fit.Features[FeatureSpecType] || !fit.Features[FeatureDraftModel]:
		dc.Reason = "no draft model support"
	case draft.NextNLayers > 0 && !draft.HasTokenEmbd && strings.Contains(meta.Repo, upstreamRepo):
		// Upstream reads token_embd from the MTP file itself; files made for
		// the qwen4exp fork share it with the main model and fail after load.
		dc.Reason = "MTP file has no token_embd"
	case draft.Arch != "" && model.Arch != "" && draft.NextNLayers == 0 && !sameFamily(draft.Arch, model.Arch):
		dc.Reason = fmt.Sprintf("draft arch %s, model arch %s", draft.Arch, model.Arch)
	default:
		dc.OK = true
	}
	return dc
}

// sameFamily treats architectures sharing a tokenizer family as compatible
// drafts; the server checks the vocabulary at load.
func sameFamily(a, b string) bool {
	strip := func(s string) string {
		s = strings.ToLower(s)
		for _, suf := range []string{"moe", "next", "_text", "vl"} {
			s = strings.TrimSuffix(s, suf)
		}
		return s
	}
	return strip(a) == strip(b)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
