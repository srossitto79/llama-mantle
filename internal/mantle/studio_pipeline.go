package mantle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mostlygeek/llama-swap/internal/store"
)

const maxStudioPipelineSteps = 20

type StudioPipelineRequest struct {
	Name  string               `json:"name,omitempty"`
	Input string               `json:"input,omitempty"`
	Steps []StudioPipelineStep `json:"steps"`
}

type StudioPipelineStep struct {
	Operation   string          `json:"operation"`
	UsePrevious bool            `json:"usePrevious,omitempty"`
	Request     json.RawMessage `json:"request"`
}

type StudioPipelineTemplate struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Pipeline  StudioPipelineRequest `json:"pipeline"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

type studioPipelineDispatch func(StudioPipelineStep, string) (*Task, error)

func (tm *TaskManager) StartStudioPipeline(req StudioPipelineRequest, modelsDir string) (*Task, error) {
	return tm.startStudioPipeline(req, modelsDir, tm.dispatchStudioPipelineStep)
}

func (tm *TaskManager) startStudioPipeline(req StudioPipelineRequest, modelsDir string, dispatch studioPipelineDispatch) (*Task, error) {
	if err := validateStudioPipeline(req); err != nil {
		return nil, err
	}
	task := tm.newStudioTask("pipeline", req.Input, "", map[string]any{
		"name": req.Name, "steps": req.Steps, "childTaskIDs": []string{},
	})
	task.mu.Lock()
	task.JobClass = "workflow"
	task.mu.Unlock()
	tm.PersistStudioTask(task)
	go tm.runStudioPipeline(task, req, modelsDir, dispatch)
	return task, nil
}

func validateStudioPipeline(req StudioPipelineRequest) error {
	if len(req.Steps) == 0 {
		return fmt.Errorf("pipeline requires at least one step")
	}
	if len(req.Steps) > maxStudioPipelineSteps {
		return fmt.Errorf("pipeline may contain at most %d steps", maxStudioPipelineSteps)
	}
	for i, step := range req.Steps {
		if !studioPipelineOperationAllowed(step.Operation) {
			return fmt.Errorf("pipeline step %d has unsupported operation %q", i+1, step.Operation)
		}
		if len(step.Request) == 0 || !json.Valid(step.Request) {
			return fmt.Errorf("pipeline step %d has an invalid request", i+1)
		}
	}
	return nil
}

func (tm *TaskManager) SaveStudioPipelineTemplate(template StudioPipelineTemplate) (*StudioPipelineTemplate, error) {
	template.Name = strings.TrimSpace(template.Name)
	if template.Name == "" || len(template.Name) > 100 {
		return nil, fmt.Errorf("template name must contain 1 to 100 characters")
	}
	if err := validateStudioPipeline(template.Pipeline); err != nil {
		return nil, err
	}
	if template.ID == "" {
		template.ID = strings.Replace(tm.newID(), "task-", "pipeline-", 1)
	} else if !isSafeBackendName(template.ID) {
		return nil, fmt.Errorf("invalid template ID")
	}
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	definition, err := json.Marshal(template.Pipeline)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if template.CreatedAt.IsZero() {
		template.CreatedAt = now
	}
	template.UpdatedAt = now
	if err := st.SaveStudioPipelineTemplate(context.Background(), store.StudioPipelineTemplateRecord{
		ID: template.ID, Name: template.Name, DefinitionJSON: string(definition),
		CreatedAt: template.CreatedAt, UpdatedAt: template.UpdatedAt,
	}); err != nil {
		return nil, err
	}
	return &template, nil
}

func (tm *TaskManager) ListStudioPipelineTemplates() ([]StudioPipelineTemplate, error) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	records, err := st.ListStudioPipelineTemplates(context.Background())
	if err != nil {
		return nil, err
	}
	templates := make([]StudioPipelineTemplate, 0, len(records))
	for _, record := range records {
		var pipeline StudioPipelineRequest
		if err := json.Unmarshal([]byte(record.DefinitionJSON), &pipeline); err != nil {
			return nil, fmt.Errorf("decode pipeline template %s: %w", record.ID, err)
		}
		templates = append(templates, StudioPipelineTemplate{
			ID: record.ID, Name: record.Name, Pipeline: pipeline,
			CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		})
	}
	return templates, nil
}

func (tm *TaskManager) DeleteStudioPipelineTemplate(id string) (bool, error) {
	if !isSafeBackendName(id) {
		return false, fmt.Errorf("invalid template ID")
	}
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return false, fmt.Errorf("Studio storage is not configured")
	}
	return st.DeleteStudioPipelineTemplate(context.Background(), id)
}

func studioPipelineOperationAllowed(operation string) bool {
	switch operation {
	case "quantize", "hash", "split", "merge", "prune", "train-qlora", "export-lora", "evaluate", "register":
		return true
	default:
		return false
	}
}

func (tm *TaskManager) runStudioPipeline(parent *Task, req StudioPipelineRequest, modelsDir string, dispatch studioPipelineDispatch) {
	previous := req.Input
	childIDs := make([]string, 0, len(req.Steps))
	for index, step := range req.Steps {
		if parent.Context().Err() != nil {
			return
		}
		if step.UsePrevious {
			if previous == "" {
				parent.UpdateProgress(TaskFailed, fmt.Sprintf("pipeline step %d has no previous artifact", index+1), index*100/len(req.Steps))
				return
			}
			var err error
			step, err = studioPipelineApplyPrevious(step, previous)
			if err != nil {
				parent.UpdateProgress(TaskFailed, fmt.Sprintf("pipeline step %d: %v", index+1, err), index*100/len(req.Steps))
				return
			}
		}
		parent.UpdateProgress(TaskRunning, fmt.Sprintf("Starting step %d/%d: %s", index+1, len(req.Steps), step.Operation), index*100/len(req.Steps))
		child, err := dispatch(step, modelsDir)
		if err != nil {
			parent.UpdateProgress(TaskFailed, fmt.Sprintf("pipeline step %d (%s): %v", index+1, step.Operation, err), index*100/len(req.Steps))
			return
		}
		childIDs = append(childIDs, child.ID)
		parent.mu.Lock()
		parent.Parameters["childTaskIDs"] = append([]string(nil), childIDs...)
		parent.mu.Unlock()
		parent.persistNow()
		result, cancelled := tm.waitForPipelineChild(parent, child)
		if cancelled {
			return
		}
		if result.State != TaskCompleted {
			parent.UpdateProgress(TaskFailed, fmt.Sprintf("pipeline step %d (%s) %s: %s", index+1, step.Operation, result.State, result.Message), index*100/len(req.Steps))
			return
		}
		for _, artifact := range result.Artifacts {
			parent.AddArtifact(artifact)
		}
		if result.Output != "" {
			previous = result.Output
			parent.mu.Lock()
			parent.Output = previous
			parent.mu.Unlock()
			parent.persistNow()
		}
	}
	parent.UpdateProgress(TaskCompleted, fmt.Sprintf("Pipeline completed (%d steps)", len(req.Steps)), 100)
}

func (tm *TaskManager) waitForPipelineChild(parent, child *Task) (*Task, bool) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result := child.Snapshot()
		if isTerminalTaskState(result.State) {
			return result, false
		}
		select {
		case <-parent.Context().Done():
			tm.CancelTask(child.ID)
			return nil, true
		case <-ticker.C:
		}
	}
}

func studioPipelineApplyPrevious(step StudioPipelineStep, previous string) (StudioPipelineStep, error) {
	var request map[string]any
	if err := json.Unmarshal(step.Request, &request); err != nil {
		return step, err
	}
	var field string
	switch step.Operation {
	case "quantize", "hash", "split":
		field = "input"
	case "merge", "export-lora":
		field = "base"
	case "prune":
		field = "model"
	case "train-qlora", "evaluate":
		field = "model"
	case "register":
		field = "model"
	default:
		return step, fmt.Errorf("operation %q cannot consume a previous artifact", step.Operation)
	}
	request[field] = previous
	encoded, err := json.Marshal(request)
	if err != nil {
		return step, err
	}
	step.Request = encoded
	return step, nil
}

func (tm *TaskManager) dispatchStudioPipelineStep(step StudioPipelineStep, modelsDir string) (*Task, error) {
	decode := func(target any) error {
		if err := json.Unmarshal(step.Request, target); err != nil {
			return fmt.Errorf("decode %s request: %w", step.Operation, err)
		}
		return nil
	}
	switch step.Operation {
	case "quantize":
		var req QuantizeRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartQuantize(req, modelsDir)
	case "hash":
		var req HashRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartHash(req, modelsDir)
	case "split":
		var req SplitRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartSplit(req, modelsDir)
	case "merge":
		var req MergeRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartMerge(req, modelsDir)
	case "prune":
		var req PruneRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartPrune(req, modelsDir)
	case "train-qlora":
		var req TrainQLoRARequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartTrainQLoRA(req, modelsDir)
	case "export-lora":
		var req ExportLoRARequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartExportLoRA(req, modelsDir)
	case "evaluate":
		var req EvaluateRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		return tm.StartEvaluate(req, modelsDir)
	case "register":
		var req RegisterStudioModelRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		register := tm.studioRegisterFunc()
		if register == nil {
			return nil, fmt.Errorf("Studio model registration is not configured")
		}
		return tm.StartRegisterStudioModel(req, modelsDir, register)
	default:
		return nil, fmt.Errorf("unsupported pipeline operation %q", step.Operation)
	}
}
