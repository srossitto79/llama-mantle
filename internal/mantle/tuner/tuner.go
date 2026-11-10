// Package tuner proposes llama-server entries tuned for single-user end-to-end
// speed on the local station. See docs/config-tuner-plan.md.
//
// Analysis probes the station, inspects the model, resolves which backends can
// run which features and generates candidates per context tier, each with a
// proposed config entry. TuneCache then runs llama.cpp's moe-cache-tune.py on
// one MoE-cache candidate to pick its cache size and CPU share. Benchmarker and
// Recommender are declared for a later slice and have no implementation yet.
package tuner

import (
	"context"

	"github.com/mostlygeek/llama-swap/internal/configedit"
)

// GPUInfo is one CUDA device as reported by nvidia-smi.
type GPUInfo struct {
	Name      string `json:"name"`
	TotalMiB  int64  `json:"totalMiB"`
	UsedMiB   int64  `json:"usedMiB"`
	FreeMiB   int64  `json:"freeMiB"`
	PCIeGen   int    `json:"pcieGen"`
	PCIeWidth int    `json:"pcieWidth"`
}

// SystemInfo is what the planner knows about the station.
type SystemInfo struct {
	GPUs     []GPUInfo `json:"gpus"`
	RAMMiB   int64     `json:"ramMiB"`
	Threads  int       `json:"threads"`
	AVX512   bool      `json:"avx512"`
	Hash     string    `json:"hash"`     // ties cached results to this hardware
	Warnings []string  `json:"warnings"` // e.g. GPU memory already in use
}

// ModelInfo is what the planner knows about a GGUF model, read from its
// header and tensor table (all shards of a split file).
type ModelInfo struct {
	Path              string `json:"path"`
	Arch              string `json:"arch"`
	Layers            int    `json:"layers"`
	KVLayers          int    `json:"kvLayers"` // layers with attention K/V (hybrid models have fewer)
	EmbeddingLength   int    `json:"embeddingLength"`
	HeadCountKV       int    `json:"headCountKV"`
	KeyLength         int    `json:"keyLength"`
	ValueLength       int    `json:"valueLength"`
	TrainedContext    int    `json:"trainedContext"`
	ExpertCount       int    `json:"expertCount"` // 0 for dense models
	ExpertUsedCount   int    `json:"expertUsedCount"`
	NextNLayers       int    `json:"nextnLayers"` // MTP layers inside this file
	HasTokenEmbd      bool   `json:"hasTokenEmbd"`
	TotalBytes        int64  `json:"totalBytes"`
	ExpertBytes       int64  `json:"expertBytes"` // routed-expert tensors (*_exps)
	DenseBytes        int64  `json:"denseBytes"`  // everything else that goes to the GPU
	HostBytes         int64  `json:"hostBytes"`   // input-layer tensors llama.cpp keeps in host memory
	KVBytesPerTokenQ8 int64  `json:"kvBytesPerTokenQ8"`
	Shards            int    `json:"shards"`
}

// IsMoE reports whether the model has routed experts.
func (m ModelInfo) IsMoE() bool { return m.ExpertCount > 0 && m.ExpertBytes > 0 }

// Feature names a capability a backend may or may not have.
type Feature string

const (
	FeatureCPUMoE           Feature = "cpu-moe"             // -cmoe
	FeatureNCPUMoE          Feature = "n-cpu-moe"           // -ncmoe
	FeatureMoECache         Feature = "moe-cache"           // --moe-cache-mib
	FeatureMoECacheCPUShare Feature = "moe-cache-cpu-share" // --moe-cache-cpu-share (llama.cpp #30966)
	FeatureMoECacheTune     Feature = "moe-cache-tune"      // tools/moe-cache-tune.py shipped with the build
	FeatureFit              Feature = "fit"                 // --fit
	FeatureLoadMode         Feature = "load-mode"           // -lm
	FeatureSpecType         Feature = "spec-type"           // --spec-type
	FeatureDraftModel       Feature = "draft-model"         // -md
	FeatureSpecDraftNMax    Feature = "spec-draft-n-max"    // --spec-draft-n-max
)

// BackendFit says what one llama.cpp backend can do for a model.
type BackendFit struct {
	Name       string           `json:"name"`
	Binary     string           `json:"binary"`
	Features   map[Feature]bool `json:"features"`
	Draft      *DraftCompat     `json:"draft,omitempty"` // set when a draft file was given
	Blocked    string           `json:"blocked,omitempty"`
	CacheTypes []string         `json:"cacheTypes"`
}

// DraftCompat is the resolver's verdict on a draft/MTP file for one backend.
type DraftCompat struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	MTP    bool   `json:"mtp"` // standalone MTP file (NextN layers, no output head)
	Reason string `json:"reason,omitempty"`
}

// Layout is how weights are placed in memory.
type Layout string

const (
	LayoutFullGPU  Layout = "full-gpu"  // everything in VRAM
	LayoutMoECache Layout = "moe-cache" // experts in pinned host RAM, hot ones cached in VRAM
	LayoutNCPUMoE  Layout = "n-cpu-moe" // first N layers' experts on the CPU
	LayoutFit      Layout = "fit"       // dense model, llama.cpp --fit decides
)

// Candidate is one launch configuration for one tier.
type Candidate struct {
	ID      string   `json:"id"`
	Tier    int      `json:"tier"` // context size in tokens
	Backend string   `json:"backend"`
	Binary  string   `json:"binary"`
	Layout  Layout   `json:"layout"`
	Args    []string `json:"args"` // tuning flags only; Entry.cmd has the full command
	Reason  string   `json:"reason"`
	Blocked string   `json:"blocked,omitempty"`
	// Tunable: moe-cache-tune.py can refine the cache size and CPU share.
	Tunable bool `json:"tunable"`
	// CacheTune is the script's result once the candidate has been tuned.
	CacheTune *CacheTune `json:"cacheTune,omitempty"`
	// Memory is llama.cpp's projection for this launch; nil until sized.
	Memory *Projection `json:"memory,omitempty"`
	// Entry is the proposed config entry ("add" to the models section).
	Entry *configedit.Change `json:"entry,omitempty"`

	layoutReason, draftReason string // parts of Reason, rebuilt by the sizer
}

// TierPlan is the candidates for one context tier.
type TierPlan struct {
	Tier       int         `json:"tier"`
	Skipped    string      `json:"skipped,omitempty"`
	Candidates []Candidate `json:"candidates"`
}

// Request asks for an analysis.
type Request struct {
	Model     string `json:"model"`           // GGUF path (first shard of a split file)
	Draft     string `json:"draft,omitempty"` // optional draft/MTP GGUF path
	Tiers     []int  `json:"tiers,omitempty"` // default 65536, 131072, 262144
	Name      string `json:"name,omitempty"`  // entry id prefix; default from the file name
	RopeScale bool   `json:"ropeScale,omitempty"`
}

// Analysis is the result of Analyse.
type Analysis struct {
	System   SystemInfo   `json:"system"`
	Model    ModelInfo    `json:"model"`
	Backends []BackendFit `json:"backends"`
	Tiers    []TierPlan   `json:"tiers"`
}

// Workload is one reference request; see the brief.
type Workload struct {
	Name         string `json:"name"`
	PromptTokens int    `json:"promptTokens"`
	GenTokens    int    `json:"genTokens"`
	FollowUp     bool   `json:"followUp"`
}

// Result is one candidate's benchmark outcome (next slice).
type Result struct {
	Candidate Candidate `json:"candidate"`
	ScoreMS   float64   `json:"scoreMs"`
	Failed    string    `json:"failed,omitempty"`
}

// Recommendation is the chosen candidate per tier.
type Recommendation struct {
	Picks []Candidate `json:"picks"`
}

// SystemProbe reads the station's hardware.
type SystemProbe interface {
	Probe(ctx context.Context) (SystemInfo, error)
}

// ModelInspector reads a GGUF model's facts.
type ModelInspector interface {
	Inspect(path string) (ModelInfo, error)
}

// CapabilityResolver decides which backends can run which features, and
// whether a draft file works with each.
type CapabilityResolver interface {
	Resolve(model ModelInfo, draft *ModelInfo) ([]BackendFit, error)
}

// PlanGenerator narrows the options to a few candidates per tier.
type PlanGenerator interface {
	Plans(sys SystemInfo, model ModelInfo, fits []BackendFit, req Request) []TierPlan
}

// Benchmarker measures candidates (next slice).
type Benchmarker interface {
	Run(ctx context.Context, c Candidate, workloads []Workload) (Result, error)
}

// Recommender picks the winner per tier (next slice).
type Recommender interface {
	Pick(results []Result) Recommendation
}

// ConfigProposer turns a candidate into a config entry. It never writes; the
// caller stages the change and the config API previews and applies it.
type ConfigProposer interface {
	Propose(model ModelInfo, c Candidate, req Request) (configedit.Change, error)
}

// Tuner runs the stages.
type Tuner interface {
	Analyse(ctx context.Context, req Request) (Analysis, error)
	// TuneCache runs moe-cache-tune.py for candidate id of a, which Analyse
	// returned for req, and returns that candidate with the picked cache size
	// and CPU share and a new entry. It takes minutes and starts llama-server
	// several times, so the caller keeps other models off the GPU meanwhile.
	TuneCache(ctx context.Context, a Analysis, req Request, id string, progress func(line string)) (Candidate, error)
}

// Deps are the stages a Tuner is built from.
type Deps struct {
	Probe      SystemProbe
	Inspector  ModelInspector
	Resolver   CapabilityResolver
	Planner    PlanGenerator
	Projector  MemoryProjector
	Proposer   ConfigProposer
	CacheTuner CacheTuner // optional; TuneCache fails without it
}

// DefaultTiers are the context tiers tuned when a request names none.
var DefaultTiers = []int{65536, 131072, 262144}

// NewTuner builds a Tuner from its stages.
func NewTuner(d Deps) Tuner { return &tuner{d: d} }

type tuner struct{ d Deps }

func (t *tuner) Analyse(ctx context.Context, req Request) (Analysis, error) {
	var a Analysis
	sys, err := t.d.Probe.Probe(ctx)
	if err != nil {
		return a, err
	}
	model, err := t.d.Inspector.Inspect(req.Model)
	if err != nil {
		return a, err
	}
	var draft *ModelInfo
	if req.Draft != "" {
		d, err := t.d.Inspector.Inspect(req.Draft)
		if err != nil {
			return a, err
		}
		draft = &d
	}
	fits, err := t.d.Resolver.Resolve(model, draft)
	if err != nil {
		return a, err
	}
	if len(req.Tiers) == 0 {
		req.Tiers = DefaultTiers
	}
	tiers := t.d.Planner.Plans(sys, model, fits, req)
	sz := newSizer(t.d.Projector, model)
	for ti := range tiers {
		sz.sizeTier(ctx, sys, &tiers[ti])
	}
	for ti := range tiers {
		for ci := range tiers[ti].Candidates {
			c := &tiers[ti].Candidates[ci]
			if c.Blocked != "" {
				continue
			}
			ch, err := t.d.Proposer.Propose(model, *c, req)
			if err != nil {
				c.Blocked = err.Error()
				continue
			}
			c.Entry = &ch
		}
	}
	return Analysis{System: sys, Model: model, Backends: fits, Tiers: tiers}, nil
}
