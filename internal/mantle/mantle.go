package mantle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mostlygeek/llama-swap/internal/event"
	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/shared"
	"github.com/mostlygeek/llama-swap/internal/store"
)

// TaskState is the current state of a long-running task.
type TaskState string

const (
	TaskQueued    TaskState = "queued"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"
)

// Task tracks a long-running operation (download or build).
type Task struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"` // "download" or "build"
	State      TaskState  `json:"state"`
	Message    string     `json:"message"`
	Pct        int        `json:"pct"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	QueuedAt   *time.Time `json:"queuedAt,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`

	// Type-specific metadata
	Repo    string `json:"repo,omitempty"`
	Branch  string `json:"branch,omitempty"`
	ModelID string `json:"modelID,omitempty"`

	// Studio operation metadata. Legacy download/build tasks leave these empty.
	Operation  string         `json:"operation,omitempty"`
	Input      string         `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Logs       []string       `json:"logs,omitempty"`
	ExitCode   *int           `json:"exitCode,omitempty"`
	Artifacts  []Artifact     `json:"artifacts,omitempty"`
	JobClass   string         `json:"jobClass,omitempty"`

	ctx      context.Context
	cancel   context.CancelFunc
	cancelCh chan struct{}
	mu       sync.Mutex
	persist  func(*Task)
	logCount int
}

// Artifact is a file produced by a Studio operation.
type Artifact struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	Kind string `json:"kind"`
}

// Done returns a channel that's closed when the task is cancelled.
func (t *Task) Done() <-chan struct{} {
	return t.cancelCh
}

// Context is cancelled when the task is cancelled.
func (t *Task) Context() context.Context {
	return t.ctx
}

// AppendLog retains a bounded tail of operation output.
func (t *Task) AppendLog(line string) {
	const maxLines = 500
	t.mu.Lock()
	t.Logs = append(t.Logs, line)
	if len(t.Logs) > maxLines {
		t.Logs = append([]string(nil), t.Logs[len(t.Logs)-maxLines:]...)
	}
	t.UpdatedAt = time.Now()
	t.logCount++
	shouldPersist := t.logCount%25 == 0
	t.mu.Unlock()
	if shouldPersist {
		t.persistNow()
	}
}

// SetExitCode records the process exit status.
func (t *Task) SetExitCode(code int) {
	t.mu.Lock()
	t.ExitCode = &code
	t.UpdatedAt = time.Now()
	t.mu.Unlock()
	t.persistNow()
}

// AddArtifact records a generated file.
func (t *Task) AddArtifact(artifact Artifact) {
	t.mu.Lock()
	t.Artifacts = append(t.Artifacts, artifact)
	t.UpdatedAt = time.Now()
	t.mu.Unlock()
	t.persistNow()
}

// IsTerminal reports whether no further task updates are expected.
func (t *Task) IsTerminal() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return isTerminalTaskState(t.State)
}

// Snapshot returns a serialization-safe copy of the task's public state.
func (t *Task) Snapshot() *Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	copy := &Task{
		ID: t.ID, Type: t.Type, State: t.State, Message: t.Message, Pct: t.Pct,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, QueuedAt: t.QueuedAt,
		StartedAt: t.StartedAt, FinishedAt: t.FinishedAt, Repo: t.Repo,
		Branch: t.Branch, ModelID: t.ModelID, Operation: t.Operation,
		Input: t.Input, Output: t.Output, ExitCode: t.ExitCode, JobClass: t.JobClass,
		Parameters: make(map[string]any, len(t.Parameters)),
	}
	for key, value := range t.Parameters {
		copy.Parameters[key] = value
	}
	copy.Logs = append([]string(nil), t.Logs...)
	copy.Artifacts = append([]Artifact(nil), t.Artifacts...)
	return copy
}

func isTerminalTaskState(state TaskState) bool {
	return state == TaskCompleted || state == TaskFailed || state == TaskCancelled
}

// Cancel cancels the task.
func (t *Task) Cancel() {
	t.mu.Lock()
	if t.State != TaskRunning && t.State != TaskQueued {
		t.mu.Unlock()
		return
	}
	if t.cancel != nil {
		t.cancel()
	}
	close(t.cancelCh)
	t.State = TaskCancelled
	now := time.Now()
	t.UpdatedAt = now
	t.FinishedAt = &now
	t.mu.Unlock()
	t.persistNow()
}

// UpdateProgress updates a task's state and emits a progress event.
func (t *Task) UpdateProgress(state TaskState, msg string, pct int) {
	t.mu.Lock()
	t.State = state
	t.Message = msg
	t.Pct = pct
	now := time.Now()
	t.UpdatedAt = now
	if state == TaskRunning && t.StartedAt == nil {
		t.StartedAt = &now
	}
	if isTerminalTaskState(state) {
		t.FinishedAt = &now
	}
	t.mu.Unlock()
	t.persistNow()

	if t.Type == "build" {
		event.Emit(shared.BackendBuildProgressEvent{
			TaskID:  t.ID,
			Repo:    t.Repo,
			Branch:  t.Branch,
			State:   shared.ProgressState(state),
			Message: msg,
			Pct:     pct,
		})
	} else if t.Type == "download" {
		event.Emit(shared.ModelDownloadProgressEvent{
			TaskID:  t.ID,
			ModelID: t.ModelID,
			State:   shared.ProgressState(state),
			Message: msg,
			Pct:     pct,
		})
	}
}

func (t *Task) persistNow() {
	if t.persist != nil && t.Type == "studio" {
		t.persist(t)
	}
}

// TaskManager holds all active and recent tasks.
type TaskManager struct {
	mu                 sync.Mutex
	tasks              map[string]*Task
	next               atomic.Uint64
	log                *logmon.Monitor
	studioStore        *store.Store
	studioLoaded       bool
	studioQueue        []*studioQueueItem
	studioRunning      int
	studioHeavyRunning int
	studioMaxRunning   int
	studioMaxHeavy     int
	studioOutputs      map[string]string
	studioResources    func() StudioResourceSnapshot
	studioRetryPending bool
	studioCleanupRoots map[string]struct{}
	studioRegister     func(RegisterStudioModelRequest, string) error
}

// NewTaskManager creates a new task manager. log receives a line for every
// line of build output, so it shows up in the container logs / /logs
// endpoint (in addition to the task's own last-line progress message).
func NewTaskManager(log *logmon.Monitor) *TaskManager {
	return &TaskManager{
		tasks:              make(map[string]*Task),
		log:                log,
		studioMaxRunning:   studioJobLimit("LLAMA_STUDIO_MAX_JOBS", 2),
		studioMaxHeavy:     studioJobLimit("LLAMA_STUDIO_MAX_HEAVY_JOBS", 1),
		studioOutputs:      make(map[string]string),
		studioCleanupRoots: make(map[string]struct{}),
	}
}

// logBuildLine forwards a build output line to the shared log monitor, if one
// was configured.
func (tm *TaskManager) logBuildLine(taskID, line string) {
	if tm.log != nil {
		tm.log.Infof("[build %s] %s", taskID, line)
	}
}

func (tm *TaskManager) newID() string {
	return fmt.Sprintf("task-%d-%d", time.Now().UnixMilli(), tm.next.Add(1))
}

// CreateTask registers a new task with a cancellable context and returns it.
func (tm *TaskManager) CreateTask(taskType, repo, branch, modelID string) *Task {
	ctx, cancel := context.WithCancel(context.Background())
	t := &Task{
		ID:        tm.newID(),
		Type:      taskType,
		State:     TaskRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Repo:      repo,
		Branch:    branch,
		ModelID:   modelID,
		ctx:       ctx,
		cancel:    cancel,
		cancelCh:  make(chan struct{}),
		persist:   tm.persistStudioTask,
	}
	_ = ctx // context is used via cancel()

	tm.mu.Lock()
	tm.tasks[t.ID] = t
	tm.mu.Unlock()
	return t
}

// SetStudioStore attaches durable Studio storage. The first attachment also
// recovers interrupted work and hydrates recent tasks into memory.
func (tm *TaskManager) SetStudioStore(st *store.Store) error {
	tm.mu.Lock()
	tm.studioStore = st
	load := !tm.studioLoaded
	if load {
		tm.studioLoaded = true
	}
	tm.mu.Unlock()
	if !load {
		return nil
	}
	ctx := context.Background()
	if err := st.RecoverStudioJobs(ctx); err != nil {
		return err
	}
	jobs, err := st.ListStudioJobs(ctx, 100)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		task, err := tm.taskFromStudioRecord(job)
		if err != nil {
			return fmt.Errorf("restore studio job %s: %w", job.ID, err)
		}
		tm.mu.Lock()
		tm.tasks[task.ID] = task
		tm.mu.Unlock()
	}
	return nil
}

func (tm *TaskManager) taskFromStudioRecord(job store.StudioJobRecord) (*Task, error) {
	var parameters map[string]any
	var logs []string
	if err := json.Unmarshal([]byte(job.ParametersJSON), &parameters); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(job.LogsJSON), &logs); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &Task{
		ID: job.ID, Type: "studio", State: TaskState(job.State), Message: job.Message,
		Pct: job.Pct, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		Operation: job.Operation, Input: job.Input, Output: job.Output,
		Parameters: parameters, Logs: logs, ExitCode: job.ExitCode,
		JobClass: job.JobClass, QueuedAt: job.QueuedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
		ctx: ctx, cancel: cancel, cancelCh: make(chan struct{}), persist: tm.persistStudioTask,
	}
	for _, artifact := range job.Artifacts {
		task.Artifacts = append(task.Artifacts, Artifact{
			Name: artifact.Name, Path: artifact.Path, Size: artifact.Size, Kind: artifact.Kind,
		})
	}
	return task, nil
}

func (tm *TaskManager) persistStudioTask(task *Task) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return
	}
	task.mu.Lock()
	parameters, err1 := json.Marshal(task.Parameters)
	logs, err2 := json.Marshal(task.Logs)
	record := store.StudioJobRecord{
		ID: task.ID, Operation: task.Operation, State: string(task.State), Message: task.Message,
		Pct: task.Pct, Input: task.Input, Output: task.Output,
		ParametersJSON: string(parameters), LogsJSON: string(logs), ExitCode: task.ExitCode,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
		JobClass: task.JobClass, QueuedAt: task.QueuedAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt,
	}
	for _, artifact := range task.Artifacts {
		record.Artifacts = append(record.Artifacts, store.StudioArtifactRecord{
			Name: artifact.Name, Path: artifact.Path, Size: artifact.Size, Kind: artifact.Kind,
			MetadataJSON: "{}",
		})
	}
	task.mu.Unlock()
	if err1 != nil || err2 != nil {
		return
	}
	if err := st.SaveStudioJob(context.Background(), record); err != nil && tm.log != nil {
		tm.log.Errorf("[studio %s] persist task: %v", task.ID, err)
	}
}

// PersistStudioTask stores metadata assigned immediately after task creation.
func (tm *TaskManager) PersistStudioTask(task *Task) {
	tm.persistStudioTask(task)
}

// GetTask returns a task by ID.
func (tm *TaskManager) GetTask(id string) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.tasks[id]
}

// ListTasks returns all recent tasks.
func (tm *TaskManager) ListTasks() []*Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	result := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		result = append(result, t.Snapshot())
	}
	return result
}

// CancelTask cancels a running task by ID.
func (tm *TaskManager) CancelTask(id string) bool {
	t := tm.GetTask(id)
	if t == nil {
		return false
	}
	if t.Type == "studio" && tm.cancelQueuedStudioTask(t) {
		return true
	}
	t.Cancel()
	return true
}

// ---------------------------------------------------------------------------
// HF Model search
// ---------------------------------------------------------------------------

// HFModel is a single result from the HuggingFace model API.
type HFModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Downloads   int64    `json:"downloads"`
	Likes       int64    `json:"likes"`
	UpdatedAt   string   `json:"updatedAt"`
	Tags        []string `json:"tags"`
	GGUF        bool     `json:"gguf"`
}

// hfSortParam maps a UI sort key to the HuggingFace API sort field. An empty
// field means "relevance" (let the search ranking decide the order).
func hfSortParam(sort string) string {
	switch sort {
	case "relevance":
		return ""
	case "trending":
		return "trendingScore"
	case "likes":
		return "likes"
	case "created":
		return "createdAt"
	case "modified":
		return "lastModified"
	case "downloads":
		return "downloads"
	default:
		return "downloads"
	}
}

// hfPipelineTag maps a UI model-type "kind" to the HuggingFace pipeline_tag
// filter. An empty result means no pipeline filter (text/LLM models).
func hfPipelineTag(kind string) string {
	switch kind {
	case "image":
		return "text-to-image"
	case "transcription":
		return "automatic-speech-recognition"
	case "tts":
		return "text-to-speech"
	default:
		return ""
	}
}

// SearchHFModels queries the HuggingFace model hub.
// sort is one of: relevance, trending, downloads, likes, created, modified.
// kind filters by model type: "" / "text" (LLMs), "image", "transcription", "tts".
func SearchHFModels(query string, limit int, sort, kind string) ([]HFModel, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	params := url.Values{}
	params.Set("search", query)
	params.Set("limit", fmt.Sprintf("%d", limit))
	if field := hfSortParam(sort); field != "" {
		params.Set("sort", field)
		params.Set("direction", "-1")
	}
	if tag := hfPipelineTag(kind); tag != "" {
		params.Set("pipeline_tag", tag)
	}
	apiURL := "https://huggingface.co/api/models?" + params.Encode()
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("HF API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HF API returned status %d", resp.StatusCode)
	}

	var raw []struct {
		ID          string   `json:"id"`
		Downloads   int64    `json:"downloads"`
		Likes       int64    `json:"likes"`
		LastUpdated string   `json:"lastModified"`
		Tags        []string `json:"tags"`
		Siblings    []struct {
			Rfilename string `json:"rfilename"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("HF API decode failed: %w", err)
	}

	results := make([]HFModel, 0, len(raw))
	for _, m := range raw {
		hasGGUF := false
		for _, s := range m.Siblings {
			if len(s.Rfilename) > 5 && s.Rfilename[len(s.Rfilename)-5:] == ".gguf" {
				hasGGUF = true
				break
			}
		}
		results = append(results, HFModel{
			ID:        m.ID,
			Name:      m.ID,
			Downloads: m.Downloads,
			Likes:     m.Likes,
			UpdatedAt: m.LastUpdated,
			Tags:      m.Tags,
			GGUF:      hasGGUF,
		})
	}
	return results, nil
}

// HFFile is a single downloadable file in a HF model repo.
type HFFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ListHFFiles lists every file in a HF model repo with its size, using the
// tree API (the basic models endpoint does not report file sizes). LFS-backed
// files (model weights) report their real size under lfs.size.
func ListHFFiles(modelID string) ([]HFFile, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/main?recursive=true", modelID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HF API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HF API returned status %d for model %s", resp.StatusCode, modelID)
	}

	var raw []struct {
		Type string `json:"type"`
		Path string `json:"path"`
		Size int64  `json:"size"`
		LFS  *struct {
			Size int64 `json:"size"`
		} `json:"lfs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("HF API decode failed: %w", err)
	}

	files := make([]HFFile, 0, len(raw))
	for _, e := range raw {
		if e.Type == "directory" {
			continue
		}
		size := e.Size
		if e.LFS != nil && e.LFS.Size > 0 {
			size = e.LFS.Size
		}
		files = append(files, HFFile{Path: e.Path, Size: size})
	}
	return files, nil
}
