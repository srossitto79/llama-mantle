package tuner

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The script's output without --markdown, from a real run's format.
const tuneOutput = `Model-00001-of-00003.gguf: 61440 MiB of experts, CUDA0 23300 MiB free, cache up to 9472 MiB

  cache MiB  share   t/s  hits
       6144      0  20.1   71%

predicted t/s, warm cache:
  cache MiB     0   0.5
       9472  21.0  23.4*

recommended: --moe-cache-mib 9472 --moe-cache-cpu-share 0.5 (23.4 t/s, 21.0 with share 0)
`

func TestCacheTune_Parse(t *testing.T) {
	r, err := parseCacheTune(tuneOutput)
	if err != nil {
		t.Fatal(err)
	}
	if r.CacheMiB != 9472 || r.CPUShare != 0.5 || r.TokensPerSec != 23.4 || r.ShareZeroTPS != 21.0 || r.AllFit {
		t.Fatalf("parsed %+v", r)
	}
	if !strings.HasPrefix(r.Report, "Model-00001") {
		t.Fatalf("report = %q", r.Report)
	}
}

func TestCacheTune_ParseShareZero(t *testing.T) {
	r, err := parseCacheTune("recommended: --moe-cache-mib 4096 --moe-cache-cpu-share 0 (12.5 t/s)\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.CacheMiB != 4096 || r.CPUShare != 0 || r.TokensPerSec != 12.5 || r.ShareZeroTPS != 12.5 {
		t.Fatalf("parsed %+v", r)
	}
}

func TestCacheTune_ParseAllFit(t *testing.T) {
	r, err := parseCacheTune("all experts fit on CUDA0 (20480 MiB): drop -cmoe, or tune a smaller cache with --max-mib\n")
	if err != nil || !r.AllFit {
		t.Fatalf("parsed %+v, %v", r, err)
	}
}

func TestCacheTune_ParseNoRecommendation(t *testing.T) {
	if _, err := parseCacheTune("predicted t/s, warm cache:\n"); err == nil {
		t.Fatal("no error without a recommendation")
	}
}

func TestCacheTune_JobLeavesOutCacheFlags(t *testing.T) {
	c := Candidate{
		Tier: 131072, Binary: "/backends/ggml-org/llama-server", Layout: LayoutMoECache,
		Args: []string{"-ngl", "99", "-cmoe", "--moe-cache-mib", "9728", "-lm", "none", "--moe-cache-cpu-share", "0.5"},
	}
	job := cacheTuneJob(ModelInfo{Path: "/models/m.gguf"}, c, Request{Model: "/models/m.gguf", Name: "swift"})
	want := []string{"/backends/ggml-org/llama-server", "-m", "/models/m.gguf", "-c", "131072", "-fa", "on",
		"-ctk", "q8_0", "-ctv", "q8_0", "--parallel", "1", "-ngl", "99", "-cmoe", "-lm", "none"}
	if !reflect.DeepEqual(job.Cmd, want) {
		t.Fatalf("cmd = %v", job.Cmd)
	}
	if job.MaxMiB != 9728 {
		t.Fatalf("max = %d", job.MaxMiB)
	}
	if job.Script != filepath.Join("/backends/ggml-org", "tools", "moe-cache-tune.py") {
		t.Fatalf("script = %s", job.Script)
	}
	if dir := filepath.ToSlash(job.OutDir); !strings.HasPrefix(dir, "/backends/ggml-org/tune/swift-128k-") {
		t.Fatalf("out = %s", dir)
	}
}

type fakeCacheTuner struct {
	job    CacheTuneJob
	result CacheTune
	err    error
}

func (f *fakeCacheTuner) Tune(ctx context.Context, job CacheTuneJob, progress func(string)) (CacheTune, error) {
	f.job = job
	if progress != nil {
		progress("1/6 6144 MiB, share 0: 20.1 t/s")
	}
	return f.result, f.err
}

type busyProbe struct{ usedMiB int64 }

func (b busyProbe) Probe(ctx context.Context) (SystemInfo, error) {
	return SystemInfo{GPUs: []GPUInfo{{UsedMiB: b.usedMiB}}}, nil
}

func tunableAnalysis() Analysis {
	args := []string{"-ngl", "99", "-cmoe", "--moe-cache-mib", "9728", "-lm", "none"}
	return Analysis{
		Model: ModelInfo{Path: "/models/m.gguf"},
		Tiers: []TierPlan{{Tier: 65536, Candidates: []Candidate{
			{ID: "c", Tier: 65536, Backend: "ggml-org", Binary: "/backends/ggml-org/llama-server", Layout: LayoutMoECache,
				Args: args, Tunable: true, layoutReason: "Experts in host RAM, 9728 MiB cache", draftReason: "n-gram drafts"},
			{ID: "plain", Tier: 65536, Backend: "ggml-org", Binary: "/backends/ggml-org/llama-server", Layout: LayoutMoECache, Args: args},
		}}},
	}
}

func TestTuner_TuneCacheAppliesPick(t *testing.T) {
	ct := &fakeCacheTuner{result: CacheTune{CacheMiB: 9472, CPUShare: 0.5, TokensPerSec: 23.4}}
	pr := &stubProposer{}
	tn := NewTuner(Deps{Probe: busyProbe{}, Projector: stubProjector{}, Proposer: pr, CacheTuner: ct})
	a := tunableAnalysis()
	var lines []string
	c, err := tn.TuneCache(context.Background(), a, Request{Model: "/models/m.gguf"}, "c", func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if argInt(c, "--moe-cache-mib") != 9472 {
		t.Errorf("cache = %d", argInt(c, "--moe-cache-mib"))
	}
	if got := c.Args[len(c.Args)-2:]; !reflect.DeepEqual(got, []string{"--moe-cache-cpu-share", "0.5"}) {
		t.Errorf("share args = %v", got)
	}
	if c.CacheTune == nil || c.Entry == nil || c.Memory == nil || pr.calls != 1 {
		t.Errorf("candidate %+v, proposer calls %d", c, pr.calls)
	}
	if c.Reason != "Experts in host RAM, 9472 MiB cache, CPU share 0.5; n-gram drafts" {
		t.Errorf("reason = %q", c.Reason)
	}
	if len(lines) != 1 || ct.job.MaxMiB != 9728 {
		t.Errorf("progress %v, job %+v", lines, ct.job)
	}
	// The analysis keeps its own arguments.
	if argInt(a.Tiers[0].Candidates[0], "--moe-cache-mib") != 9728 {
		t.Error("TuneCache changed the analysis")
	}
}

func TestTuner_TuneCacheRefusesBusyGPU(t *testing.T) {
	ct := &fakeCacheTuner{}
	tn := NewTuner(Deps{Probe: busyProbe{usedMiB: 12000}, Proposer: &stubProposer{}, CacheTuner: ct})
	if _, err := tn.TuneCache(context.Background(), tunableAnalysis(), Request{}, "c", nil); err == nil || !strings.Contains(err.Error(), "GPU in use") {
		t.Fatalf("err = %v", err)
	}
	if ct.job.Script != "" {
		t.Fatal("script ran with the GPU in use")
	}
}

func TestTuner_TuneCacheRejectsUntunable(t *testing.T) {
	tn := NewTuner(Deps{Probe: busyProbe{}, Proposer: &stubProposer{}, CacheTuner: &fakeCacheTuner{}})
	for _, id := range []string{"plain", "missing"} {
		if _, err := tn.TuneCache(context.Background(), tunableAnalysis(), Request{}, id, nil); err == nil {
			t.Errorf("%s: no error", id)
		}
	}
}

func TestTuner_TuneCacheScriptError(t *testing.T) {
	tn := NewTuner(Deps{Probe: busyProbe{}, Proposer: &stubProposer{}, CacheTuner: &fakeCacheTuner{err: errors.New("moe-cache-tune: busy")}})
	if _, err := tn.TuneCache(context.Background(), tunableAnalysis(), Request{}, "c", nil); err == nil || err.Error() != "moe-cache-tune: busy" {
		t.Fatalf("err = %v", err)
	}
}

func TestTuner_TuneCacheAllFitKeepsArgs(t *testing.T) {
	tn := NewTuner(Deps{Probe: busyProbe{}, Proposer: &stubProposer{}, CacheTuner: &fakeCacheTuner{result: CacheTune{AllFit: true}}})
	c, err := tn.TuneCache(context.Background(), tunableAnalysis(), Request{}, "c", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.CacheTune == nil || !c.CacheTune.AllFit || argInt(c, "--moe-cache-mib") != 9728 {
		t.Fatalf("candidate %+v", c)
	}
}
