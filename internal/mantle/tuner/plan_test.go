package tuner

import (
	"strconv"
	"testing"
)

const gib = int64(1 << 30)

func stationSystem() SystemInfo {
	return SystemInfo{GPUs: []GPUInfo{{Name: "RTX 3090", TotalMiB: 24576}}, Threads: 24, RAMMiB: 112683}
}

func stationMoE() ModelInfo {
	return ModelInfo{
		Arch:              "qwen3next",
		Layers:            48,
		EmbeddingLength:   2048,
		ExpertCount:       128,
		DenseBytes:        3 * gib,
		ExpertBytes:       40 * gib,
		KVBytesPerTokenQ8: 65536,
		TrainedContext:    262144,
	}
}

func moeCacheFit() BackendFit {
	features := map[Feature]bool{FeatureCPUMoE: true, FeatureNCPUMoE: true, FeatureMoECache: true, FeatureMoECacheCPUShare: true, FeatureMoECacheTune: true, FeatureLoadMode: true}
	return BackendFit{
		Name:       "ggml-org",
		Binary:     "/backends/ggml-org/llama-server",
		Features:   features,
		CacheTypes: []string{"q8_0"},
	}
}

func nCPUMoEFit() BackendFit {
	return BackendFit{
		Name:       "qwen4exp_mtp",
		Binary:     "/backends/qwen4exp_mtp/llama-server",
		Features:   map[Feature]bool{FeatureNCPUMoE: true, FeatureSpecType: true, FeatureDraftModel: true},
		CacheTypes: []string{"q8_0"},
	}
}

func nonBlocked(tp TierPlan) []Candidate {
	var out []Candidate
	for _, c := range tp.Candidates {
		if c.Blocked == "" {
			out = append(out, c)
		}
	}
	return out
}

func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func TestTuner_PlansEnumerate(t *testing.T) {
	fits := []BackendFit{moeCacheFit(), nCPUMoEFit()}
	plans := NewPlanGenerator().Plans(stationSystem(), stationMoE(), fits, Request{Tiers: []int{65536}})
	per := map[Layout]int{}
	for _, c := range nonBlocked(plans[0]) {
		per[c.Layout]++
		if c.Binary == "" || c.ID == "" || c.Reason == "" {
			t.Errorf("incomplete candidate %+v", c)
		}
		if v, ok := argValue(c.Args, "--threads"); !ok || v != "24" {
			t.Errorf("threads arg = %q", v)
		}
		switch c.Layout {
		case LayoutMoECache:
			// The cache is a seed; the sizer sets it from a projection.
			if v, _ := argValue(c.Args, "--moe-cache-mib"); v != strconv.Itoa(minCacheMiB) || !c.Tunable {
				t.Errorf("moe-cache seed %q tunable %v", v, c.Tunable)
			}
		case LayoutNCPUMoE:
			if _, ok := argValue(c.Args, "-ncmoe"); !ok || c.Tunable {
				t.Errorf("n-cpu-moe candidate %v tunable %v", c.Args, c.Tunable)
			}
		}
	}
	// Every batch size; the n-cpu-moe fork has spec-type, so each comes with
	// and without n-gram drafts there.
	if per[LayoutMoECache] != len(ubatchSizes) || per[LayoutNCPUMoE] != 2*len(ubatchSizes) {
		t.Fatalf("candidates per layout = %v", per)
	}
}

func TestTuner_PlansMoECacheArgs(t *testing.T) {
	plans := NewPlanGenerator().Plans(stationSystem(), stationMoE(), []BackendFit{moeCacheFit()}, Request{Tiers: []int{65536}})
	c := plans[0].Candidates[0]
	if _, ok := argValue(c.Args, "-lm"); !ok {
		t.Errorf("no -lm in %v", c.Args)
	}
	found := false
	for _, a := range c.Args {
		if a == "-cmoe" {
			found = true
		}
	}
	if !found {
		t.Errorf("no -cmoe in %v", c.Args)
	}
}

func TestTuner_PlansSkipWhenExpertsExceedRAM(t *testing.T) {
	model := stationMoE()
	model.ExpertBytes = 138 * gib // Groot 280B: 138 GiB of experts, 110 GiB of RAM
	plans := NewPlanGenerator().Plans(stationSystem(), model, []BackendFit{moeCacheFit(), nCPUMoEFit()}, Request{Tiers: []int{65536}})
	if len(plans[0].Candidates) != 0 || plans[0].Skipped != "Experts exceed RAM: 114 GiB, 102 GiB available" {
		t.Fatalf("plan = %+v", plans[0])
	}

	// All experts in RAM exceed it, the ones left after a full GPU do not:
	// only -ncmoe fits.
	model.ExpertBytes = 120 * gib
	plans = NewPlanGenerator().Plans(stationSystem(), model, []BackendFit{moeCacheFit(), nCPUMoEFit()}, Request{Tiers: []int{65536}})
	for _, c := range nonBlocked(plans[0]) {
		if c.Layout == LayoutMoECache {
			t.Errorf("moe-cache candidate %v with experts above RAM", c.Args)
		}
	}
	if len(nonBlocked(plans[0])) == 0 {
		t.Fatalf("no -ncmoe candidate: %+v", plans[0])
	}
}

func TestTuner_PlansSkipAboveTrainedContext(t *testing.T) {
	model := stationMoE()
	model.TrainedContext = 131072
	fits := []BackendFit{moeCacheFit()}

	plans := NewPlanGenerator().Plans(stationSystem(), model, fits, Request{Tiers: []int{131072, 262144}})
	if plans[0].Skipped != "" {
		t.Fatalf("131072 skipped: %q", plans[0].Skipped)
	}
	if plans[1].Skipped == "" || len(plans[1].Candidates) != 0 {
		t.Fatalf("262144 not skipped: %+v", plans[1])
	}

	plans = NewPlanGenerator().Plans(stationSystem(), model, fits, Request{Tiers: []int{262144}, RopeScale: true})
	if plans[0].Skipped != "" {
		t.Fatalf("skipped despite RopeScale: %q", plans[0].Skipped)
	}
}

func TestTuner_PlansDefaultTiers(t *testing.T) {
	plans := NewPlanGenerator().Plans(stationSystem(), stationMoE(), []BackendFit{moeCacheFit()}, Request{})
	if len(plans) != len(DefaultTiers) {
		t.Fatalf("tiers = %d, want %d", len(plans), len(DefaultTiers))
	}
	for i, tp := range plans {
		if tp.Tier != DefaultTiers[i] {
			t.Errorf("tier %d = %d", i, tp.Tier)
		}
	}
}

func TestTuner_PlansSmallDenseFullGPU(t *testing.T) {
	model := ModelInfo{
		Arch:              "llama",
		Layers:            32,
		EmbeddingLength:   4096,
		DenseBytes:        4 * gib,
		KVBytesPerTokenQ8: 32768,
		TrainedContext:    131072,
	}
	fit := BackendFit{
		Name:       "ggml-org",
		Binary:     "/backends/ggml-org/llama-server",
		Features:   map[Feature]bool{FeatureFit: true},
		CacheTypes: []string{"q8_0"},
	}
	plans := NewPlanGenerator().Plans(stationSystem(), model, []BackendFit{fit}, Request{Tiers: []int{65536}})
	good := nonBlocked(plans[0])
	if len(good) == 0 {
		t.Fatal("no candidates")
	}
	for _, c := range good {
		if c.Layout != LayoutFullGPU {
			t.Errorf("layout = %s, want %s", c.Layout, LayoutFullGPU)
		}
		if c.Tunable {
			t.Error("full-GPU candidate is tunable")
		}
	}
}

func TestTuner_PlansBlockedDraft(t *testing.T) {
	const reason = "MTP file has no token_embd"
	fit := moeCacheFit()
	fit.Features[FeatureSpecType] = true
	fit.Features[FeatureDraftModel] = true
	fit.Draft = &DraftCompat{Path: "/models/mtp.gguf", OK: false, MTP: true, Reason: reason}
	req := Request{Tiers: []int{65536}, Draft: "/models/mtp.gguf"}

	plans := NewPlanGenerator().Plans(stationSystem(), stationMoE(), []BackendFit{fit}, req)
	var blocked []Candidate
	for _, c := range plans[0].Candidates {
		if c.Blocked != "" {
			blocked = append(blocked, c)
			continue
		}
		for _, a := range c.Args {
			if a == "/models/mtp.gguf" {
				t.Errorf("usable candidate carries the blocked draft: %v", c.Args)
			}
		}
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked candidates = %d, want 1", len(blocked))
	}
	if blocked[0].Blocked != reason || blocked[0].Backend != "ggml-org" || blocked[0].Entry != nil {
		t.Fatalf("blocked candidate: %+v", blocked[0])
	}
	if len(nonBlocked(plans[0])) == 0 {
		t.Fatal("no usable candidates alongside the blocked draft")
	}
}

func TestTuner_PlansSkipBlockedBackend(t *testing.T) {
	fit := moeCacheFit()
	fit.Blocked = "no q8_0 KV cache"
	plans := NewPlanGenerator().Plans(stationSystem(), stationMoE(), []BackendFit{fit}, Request{Tiers: []int{65536}})
	if len(plans[0].Candidates) != 0 {
		t.Fatalf("candidates from a blocked backend: %+v", plans[0].Candidates)
	}
}

func TestTuner_PlansEmptyTierReason(t *testing.T) {
	blocked := moeCacheFit()
	blocked.Blocked = "no q8_0 KV cache"
	plans := NewPlanGenerator().Plans(stationSystem(), stationMoE(), []BackendFit{blocked}, Request{Tiers: []int{65536}})
	if plans[0].Skipped != "No usable backend" {
		t.Fatalf("Skipped = %q, want No usable backend", plans[0].Skipped)
	}

}
