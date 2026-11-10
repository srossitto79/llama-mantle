package mantle

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/mostlygeek/llama-swap/internal/mantle/tuner"
)

// newTuner builds the tuner stages for this station. Backends are read from
// backendsDir, which is also the path config.yaml names binaries by.
func newTuner(backendsDir string) tuner.Tuner {
	return tuner.NewTuner(tuner.Deps{
		Probe:      tuner.NewSystemProbe(nil, nil),
		Inspector:  tuner.NewModelInspector(),
		Resolver:   tuner.NewCapabilityResolver(backendsDir, backendsDir),
		Planner:    tuner.NewPlanGenerator(),
		Projector:  tuner.NewMemoryProjector(),
		Proposer:   tuner.NewConfigProposer(),
		CacheTuner: tuner.NewCacheTuner(),
	})
}

// tunerSession keeps the last analysis, so a cache tune names a candidate by
// id instead of sending back a command line for the server to run. It lives
// on the TaskManager, so staging entries (a config reload) does not lose it.
type tunerSession struct {
	mu  sync.Mutex
	req tuner.Request
	a   *tuner.Analysis
}

func (s *tunerSession) set(req tuner.Request, a tuner.Analysis) {
	s.mu.Lock()
	s.req, s.a = req, &a
	s.mu.Unlock()
}

func (s *tunerSession) get() (tuner.Request, tuner.Analysis, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.a == nil {
		return tuner.Request{}, tuner.Analysis{}, false
	}
	return s.req, *s.a, true
}

// insideDir reports whether path is dir or below it.
func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// SetLocalUnloader sets how the handler stops every local model before a job
// that needs the whole GPU.
func (h *Handler) SetLocalUnloader(unload func()) { h.unloadLocal = unload }

// POST /api/mantle/tuner/analyse probes the station, inspects the model and
// returns candidate entries per context tier. Nothing is written. Local model
// requests wait meanwhile, since the projections start the backends.
func (h *Handler) handleTunerAnalyse(w http.ResponseWriter, r *http.Request) {
	var req tuner.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Model == "" || !strings.HasSuffix(req.Model, ".gguf") || !insideDir(h.modelsDir, req.Model) {
		jsonError(w, http.StatusBadRequest, "model must be a .gguf file under the models directory")
		return
	}
	if req.Draft != "" && (!strings.HasSuffix(req.Draft, ".gguf") || !insideDir(h.modelsDir, req.Draft)) {
		jsonError(w, http.StatusBadRequest, "draft must be a .gguf file under the models directory")
		return
	}
	for _, t := range req.Tiers {
		if t < 4096 || t > 1<<20 {
			jsonError(w, http.StatusBadRequest, "tiers must be between 4096 and 1048576")
			return
		}
	}
	release, ok := h.tm.GPUHold().Acquire("tuner analysis")
	if !ok {
		jsonError(w, http.StatusConflict, "GPU held by "+h.tm.GPUHold().Reason())
		return
	}
	defer release()
	a, err := h.tuner.Analyse(r.Context(), req)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.tm.tuning.set(req, a)
	jsonResponse(w, http.StatusOK, a)
}

type cacheTuneRequest struct {
	Candidate string `json:"candidate"`
}

// POST /api/mantle/tuner/cache-tune starts moe-cache-tune on a candidate of
// the last analysis and returns the task. Local models are unloaded and local
// requests wait until it ends; the finished task carries the tuned candidate
// in parameters.candidate.
func (h *Handler) handleTunerCacheTune(w http.ResponseWriter, r *http.Request) {
	var body cacheTuneRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Candidate == "" {
		jsonError(w, http.StatusBadRequest, "candidate is required")
		return
	}
	req, a, ok := h.tm.tuning.get()
	if !ok {
		jsonError(w, http.StatusNotFound, "no analysis; analyse first")
		return
	}
	c, ok := a.Candidate(body.Candidate)
	if !ok {
		jsonError(w, http.StatusNotFound, "candidate not in the last analysis")
		return
	}
	if !tuner.CanTuneCache(c) {
		jsonError(w, http.StatusBadRequest, "candidate has no tunable MoE cache")
		return
	}
	label := fmt.Sprintf("MoE cache tune (%s, %dK)", c.Backend, c.Tier/1024)
	release, ok := h.tm.GPUHold().Acquire(label)
	if !ok {
		jsonError(w, http.StatusConflict, "GPU held by "+h.tm.GPUHold().Reason())
		return
	}
	task := h.tm.CreateTask("tuner", "", "", c.ID)
	task.mu.Lock()
	task.Operation = "moe-cache-tune"
	task.Input = req.Model
	task.mu.Unlock()
	task.UpdateProgress(TaskRunning, "Unloading models", 0)
	go h.runCacheTune(task, release, req, a, c.ID)
	jsonResponse(w, http.StatusAccepted, task.Snapshot())
}

// runProgressRe matches the script's line after each server run: "3/6 4096 MiB, share 0.5: 12.3 t/s".
var runProgressRe = regexp.MustCompile(`^(\d+)/(\d+) `)

func (h *Handler) runCacheTune(task *Task, release func(), req tuner.Request, a tuner.Analysis, id string) {
	defer release()
	if h.unloadLocal != nil {
		h.unloadLocal()
	}
	pct := 0
	progress := func(line string) {
		task.AppendLog(line)
		if m := runProgressRe.FindStringSubmatch(line); m != nil {
			k, _ := strconv.Atoi(m[1])
			n, _ := strconv.Atoi(m[2])
			if n > 0 {
				pct = max(pct, min(k*95/n, 95))
			}
		}
		if !task.IsTerminal() {
			task.UpdateProgress(TaskRunning, line, pct)
		}
	}
	task.UpdateProgress(TaskRunning, "Starting moe-cache-tune", 0)
	c, err := h.tuner.TuneCache(task.Context(), a, req, id, progress)
	if task.IsTerminal() {
		return // cancelled
	}
	if err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), pct)
		return
	}
	task.mu.Lock()
	task.Parameters = map[string]any{"candidate": c}
	task.mu.Unlock()
	msg := "All experts fit in VRAM"
	if ct := c.CacheTune; ct != nil && !ct.AllFit {
		msg = fmt.Sprintf("%d MiB cache, CPU share %g: %.1f tok/s predicted", ct.CacheMiB, ct.CPUShare, ct.TokensPerSec)
	}
	task.UpdateProgress(TaskCompleted, msg, 100)
}

// DELETE /api/mantle/tuner/cache-tune/{id} cancels a running cache tune.
func (h *Handler) handleCancelCacheTune(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task := h.tm.GetTask(id)
	if task == nil || task.Type != "tuner" || !h.tm.CancelTask(id) {
		jsonError(w, http.StatusNotFound, "running cache tune not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"msg": "cancelled"})
}
