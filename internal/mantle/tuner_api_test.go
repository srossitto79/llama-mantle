package mantle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/mantle/tuner"
)

func TestTunerAPI_InsideDir(t *testing.T) {
	root := filepath.FromSlash("/models")
	cases := []struct {
		name string
		dir  string
		path string
		want bool
	}{
		{"file below", root, filepath.FromSlash("/models/a/b.gguf"), true},
		{"dir itself", root, filepath.FromSlash("/models"), true},
		{"trailing separator", filepath.FromSlash("/models/"), filepath.FromSlash("/models/a.gguf"), true},
		{"dot dot escape", root, filepath.FromSlash("/models/../etc/passwd"), false},
		{"dot dot inside", root, filepath.FromSlash("/models/a/../b.gguf"), true},
		{"sibling prefix", root, filepath.FromSlash("/modelsX/a.gguf"), false},
		{"unrelated", root, filepath.FromSlash("/etc/passwd"), false},
		{"relative path", root, filepath.FromSlash("a/b.gguf"), false},
	}
	for _, c := range cases {
		if got := insideDir(c.dir, c.path); got != c.want {
			t.Errorf("%s: insideDir(%q, %q) = %v, want %v", c.name, c.dir, c.path, got, c.want)
		}
	}
}

// fakeTuner returns a fixed tuned candidate; unblock lets a test hold it mid-run.
type fakeTuner struct {
	unblock chan struct{}
	ran     chan string
}

func (f *fakeTuner) Analyse(ctx context.Context, req tuner.Request) (tuner.Analysis, error) {
	return tuner.Analysis{}, nil
}

func (f *fakeTuner) TuneCache(ctx context.Context, a tuner.Analysis, req tuner.Request, id string, progress func(string)) (tuner.Candidate, error) {
	f.ran <- id
	progress("3/6 6144 MiB, share 0.95: 20.0 t/s")
	<-f.unblock
	c, _ := a.Candidate(id)
	c.CacheTune = &tuner.CacheTune{CacheMiB: 9472, CPUShare: 0.5, TokensPerSec: 23.4}
	return c, nil
}

func tunerTestAnalysis() tuner.Analysis {
	return tuner.Analysis{Tiers: []tuner.TierPlan{{Tier: 65536, Candidates: []tuner.Candidate{
		{ID: "t", Tier: 65536, Backend: "ggml-org", Binary: "/b/llama-server", Layout: tuner.LayoutMoECache, Tunable: true},
		{ID: "f", Tier: 65536, Backend: "ggml-org", Binary: "/b/llama-server", Layout: tuner.LayoutFullGPU},
	}}}}
}

func postCacheTune(h *Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.handleTunerCacheTune(rec, httptest.NewRequest(http.MethodPost, "/api/mantle/tuner/cache-tune", strings.NewReader(`{"candidate":"`+id+`"}`)))
	return rec
}

func TestTunerAPI_CacheTuneGuards(t *testing.T) {
	h := &Handler{tm: NewTaskManager(nil), tuner: &fakeTuner{}}
	if rec := postCacheTune(h, "t"); rec.Code != http.StatusNotFound {
		t.Fatalf("without analysis: %d", rec.Code)
	}
	h.tm.tuning.set(tuner.Request{Model: "/m/x.gguf"}, tunerTestAnalysis())
	if rec := postCacheTune(h, "missing"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown candidate: %d", rec.Code)
	}
	if rec := postCacheTune(h, "f"); rec.Code != http.StatusBadRequest {
		t.Errorf("untunable candidate: %d", rec.Code)
	}
	release, _ := h.tm.GPUHold().Acquire("another job")
	defer release()
	if rec := postCacheTune(h, "t"); rec.Code != http.StatusConflict {
		t.Errorf("GPU held: %d", rec.Code)
	}
}

func TestTunerAPI_CacheTuneHoldsGPUAndStoresResult(t *testing.T) {
	ft := &fakeTuner{unblock: make(chan struct{}), ran: make(chan string, 1)}
	h := &Handler{tm: NewTaskManager(nil), tuner: ft}
	unloaded := make(chan struct{}, 1)
	h.SetLocalUnloader(func() { unloaded <- struct{}{} })
	h.tm.tuning.set(tuner.Request{Model: "/m/x.gguf"}, tunerTestAnalysis())

	rec := postCacheTune(h, "t")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var started Task
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	<-ft.ran
	select {
	case <-unloaded:
	default:
		t.Error("local models were not unloaded before the tune")
	}
	if h.tm.GPUHold().Reason() == "" {
		t.Error("GPU not held during the tune")
	}
	close(ft.unblock)

	task := h.tm.GetTask(started.ID)
	deadline := time.Now().Add(2 * time.Second)
	for !task.IsTerminal() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	snap := task.Snapshot()
	if snap.State != TaskCompleted {
		t.Fatalf("state %s: %s", snap.State, snap.Message)
	}
	c, ok := snap.Parameters["candidate"].(tuner.Candidate)
	if !ok || c.CacheTune == nil || c.CacheTune.CacheMiB != 9472 {
		t.Fatalf("parameters %+v", snap.Parameters)
	}
	if len(snap.Logs) != 1 {
		t.Errorf("logs %v", snap.Logs)
	}
	if h.tm.GPUHold().Reason() != "" {
		t.Error("GPU still held after the tune")
	}
}
