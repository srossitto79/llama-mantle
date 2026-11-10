package tuner

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// fakeProjector models a backend's memory: weights, KV per token, a compute
// buffer per ubatch, the MoE cache (rounded up by extraCache, as whole
// experts are) and expert layers kept on the GPU under -ncmoe.
type fakeProjector struct {
	free, model, kvPerKTok, computePer1KUb, extraCache, perLayer int64
	layers                                                       int
	err                                                          error
	calls                                                        int
}

func (f *fakeProjector) Project(ctx context.Context, bin string, args []string) (Projection, error) {
	f.calls++
	if f.err != nil {
		return Projection{}, f.err
	}
	val := func(flag string) int64 {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				n, _ := strconv.ParseInt(args[i+1], 10, 64)
				return n
			}
		}
		return -1
	}
	p := Projection{FreeMiB: f.free, ModelMiB: f.model}
	if c := val("--moe-cache-mib"); c >= 0 {
		p.ContextMiB += c + f.extraCache
	}
	if n := val("-ncmoe"); n >= 0 {
		p.ModelMiB += int64(f.layers-int(n)) * f.perLayer
	}
	p.ContextMiB += val("-c") / 1024 * f.kvPerKTok
	p.ComputeMiB = val("-ub") / 1024 * f.computePer1KUb
	p.TotalMiB = p.ModelMiB + p.ContextMiB + p.ComputeMiB
	return p, nil
}

// grootLike is close to Groot 280B on the station: 23300 MiB free, 7920 MiB
// of weights, a compute buffer that grows with the ubatch.
func grootLike() *fakeProjector {
	return &fakeProjector{free: 23300, model: 7920, kvPerKTok: 2, computePer1KUb: 1500}
}

func moeCacheCandidate(tier, ub int, draft bool) Candidate {
	args := []string{"-ngl", "99", "-b", "8192", "-ub", strconv.Itoa(ub), "-cmoe", "--moe-cache-mib", strconv.Itoa(minCacheMiB), "-lm", "none"}
	c := Candidate{ID: "c" + strconv.Itoa(ub), Tier: tier, Backend: "ggml-org", Binary: "/b", Layout: LayoutMoECache, layoutReason: "Experts in host RAM"}
	if draft {
		args = append(args, "--spec-type", "ngram-mod")
		c.ID += "-ngram"
		c.draftReason = "n-gram drafts"
	}
	c.Args = append(args, "--threads", "24")
	return c
}

func idle() SystemInfo {
	return SystemInfo{GPUs: []GPUInfo{{TotalMiB: 24576, UsedMiB: 13}}}
}

func TestTuner_SizeCacheFromProjection(t *testing.T) {
	f := grootLike()
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 2048, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)

	if len(tp.Candidates) != 1 || tp.Skipped != "" {
		t.Fatalf("plan = %+v", tp)
	}
	c := tp.Candidates[0]
	// free - target - (model + kv + compute) = 23300 - 1024 - (7920 + 128 + 3000)
	want := roundDown(23300-1024-(7920+128+3000), cacheStepMiB)
	if got := int64(argInt(c, "--moe-cache-mib")); got != want {
		t.Fatalf("cache = %d, want %d", got, want)
	}
	if c.Memory == nil || c.Memory.TotalMiB > 23300-1024 {
		t.Fatalf("memory = %+v", c.Memory)
	}
	if !strings.Contains(c.Reason, strconv.FormatInt(want, 10)+" MiB cache") {
		t.Errorf("reason = %q", c.Reason)
	}
}

func TestTuner_SizeCacheShrinksWithContext(t *testing.T) {
	f := grootLike()
	f.kvPerKTok = 32 // 2048 MiB more KV at 128K than at 64K
	tp64 := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 2048, false)}}
	tp128 := TierPlan{Tier: 131072, Candidates: []Candidate{moeCacheCandidate(131072, 2048, false)}}
	s := newSizer(f, ModelInfo{})
	s.sizeTier(context.Background(), idle(), &tp64)
	s.sizeTier(context.Background(), idle(), &tp128)
	diff := argInt(tp64.Candidates[0], "--moe-cache-mib") - argInt(tp128.Candidates[0], "--moe-cache-mib")
	if diff != 2048 {
		t.Fatalf("cache 64K - 128K = %d, want 2048", diff)
	}
}

func TestTuner_SizeCacheRefinesOvershoot(t *testing.T) {
	f := grootLike()
	f.extraCache = 300 // llama.cpp rounds the cache up to whole experts
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 2048, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	c := tp.Candidates[0]
	if c.Blocked != "" || c.Memory.TotalMiB > 23300-1024 {
		t.Fatalf("candidate %+v memory %+v", c, c.Memory)
	}
	if argInt(c, "--moe-cache-mib")%cacheStepMiB != 0 {
		t.Fatalf("cache %d not on the step", argInt(c, "--moe-cache-mib"))
	}
}

func TestTuner_SizeDropsWhatDoesNotFit(t *testing.T) {
	f := grootLike()
	f.computePer1KUb = 4000 // ub 4096 leaves no room for a cache
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 4096, false), moeCacheCandidate(65536, 1024, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if len(tp.Candidates) != 1 || ubOf(tp.Candidates[0]) != 1024 {
		t.Fatalf("candidates = %+v", tp.Candidates)
	}

	tp = TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 4096, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if len(tp.Candidates) != 0 || tp.Skipped != "No layout fits in VRAM" {
		t.Fatalf("plan = %+v", tp)
	}
}

func TestTuner_SizeNgramSharesProjection(t *testing.T) {
	f := grootLike()
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 2048, false), moeCacheCandidate(65536, 2048, true)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if len(tp.Candidates) != 2 {
		t.Fatalf("candidates = %d", len(tp.Candidates))
	}
	// Seed and confirm once; the n-gram variant reuses both.
	if f.calls != 2 {
		t.Fatalf("projections = %d, want 2", f.calls)
	}
	if !strings.HasSuffix(tp.Candidates[1].Reason, "; n-gram drafts") && !strings.HasSuffix(tp.Candidates[0].Reason, "; n-gram drafts") {
		t.Errorf("draft reason lost: %q / %q", tp.Candidates[0].Reason, tp.Candidates[1].Reason)
	}
}

func TestTuner_SizePicksLargerCacheFirst(t *testing.T) {
	f := grootLike()
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 4096, false), moeCacheCandidate(65536, 1024, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if len(tp.Candidates) != 2 || ubOf(tp.Candidates[0]) != 1024 {
		t.Fatalf("order = %v, %v", tp.Candidates[0].Args, tp.Candidates[1].Args)
	}
}

func TestTuner_SizeCPULayers(t *testing.T) {
	// 40 layers of 1000 MiB experts; 23300 - 1024 - 4000 (weights) - 64 (KV)
	// - 1000 (compute) leaves room for 17 layers on the GPU.
	f := &fakeProjector{free: 23300, model: 4000, kvPerKTok: 1, computePer1KUb: 1000, perLayer: 1000, layers: 40}
	model := ModelInfo{Layers: 40, ExpertBytes: 40 * 1000 << 20}
	for _, seed := range []int{40, 10, 23} {
		c := Candidate{ID: "n", Tier: 65536, Binary: "/b", Layout: LayoutNCPUMoE, Args: []string{"-ub", "1024", "-ncmoe", strconv.Itoa(seed)}}
		tp := TierPlan{Tier: 65536, Candidates: []Candidate{c}}
		newSizer(f, model).sizeTier(context.Background(), idle(), &tp)
		if len(tp.Candidates) != 1 {
			t.Fatalf("seed %d: plan = %+v", seed, tp)
		}
		if n := argInt(tp.Candidates[0], "-ncmoe"); n != 23 {
			t.Errorf("seed %d: -ncmoe = %d, want 23", seed, n)
		}
	}
}

func TestTuner_SizeFullGPUBlockedWhenOver(t *testing.T) {
	f := &fakeProjector{free: 23300, model: 23000}
	c := Candidate{ID: "g", Tier: 65536, Binary: "/b", Layout: LayoutFullGPU, Args: []string{"-ub", "1024"}}
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{c}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if len(tp.Candidates) != 0 || tp.Skipped != "No layout fits in VRAM" {
		t.Fatalf("plan = %+v", tp)
	}
}

func TestTuner_SizeGPUBusySkips(t *testing.T) {
	f := grootLike()
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 2048, false)}}
	busy := SystemInfo{GPUs: []GPUInfo{{TotalMiB: 24576, UsedMiB: 20000}}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), busy, &tp)
	if f.calls != 0 || len(tp.Candidates) != 0 || !strings.HasPrefix(tp.Skipped, "GPU in use") {
		t.Fatalf("calls %d plan %+v", f.calls, tp)
	}
}

func TestTuner_SizeProjectionFailure(t *testing.T) {
	f := grootLike()
	f.err = errors.New("unknown model architecture")
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 2048, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if tp.Skipped != "Projection failed: unknown model architecture" {
		t.Fatalf("skipped = %q", tp.Skipped)
	}
}

func TestTuner_ParseBreakdown(t *testing.T) {
	line := "0.00.267.058 I common_memory_breakdown_print: |   - CUDA0 (RTX 3090)   | 24575 = 23300 + ( 12897 =   7920 +    2217 +    2759) +      -11621 |"
	p, ok := parseBreakdown(line)
	if !ok {
		t.Fatal("not parsed")
	}
	if p != (Projection{FreeMiB: 23300, TotalMiB: 12897, ModelMiB: 7920, ContextMiB: 2217, ComputeMiB: 2759}) {
		t.Fatalf("projection = %+v", p)
	}
	if _, ok := parseBreakdown("|   - Host               |                  327159 = 325876 +       0 +    1282 |"); ok {
		t.Fatal("host row parsed as GPU")
	}
}

func TestTuner_SizeNearTieGoesToBatchSize(t *testing.T) {
	// ub 512 leaves 512 MiB more cache than ub 1024: within a GiB of the
	// best, so the larger batch ranks first.
	f := grootLike()
	f.computePer1KUb = 512 // ub 512 projects 0, ub 1024 projects 512
	tp := TierPlan{Tier: 65536, Candidates: []Candidate{moeCacheCandidate(65536, 512, false), moeCacheCandidate(65536, 1024, false)}}
	newSizer(f, ModelInfo{}).sizeTier(context.Background(), idle(), &tp)
	if len(tp.Candidates) != 2 || ubOf(tp.Candidates[0]) != 1024 {
		t.Fatalf("order = %v, %v", tp.Candidates[0].Args, tp.Candidates[1].Args)
	}
}
