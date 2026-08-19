package mantle

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestTaskManager_StudioPipelineChainsPreviousOutput(t *testing.T) {
	tm := NewTaskManager(nil)
	var secondInput string
	dispatched := 0
	dispatch := func(step StudioPipelineStep, _ string) (*Task, error) {
		dispatched++
		if dispatched == 2 {
			var request map[string]any
			if err := json.Unmarshal(step.Request, &request); err != nil {
				t.Fatal(err)
			}
			secondInput, _ = request["model"].(string)
		}
		child := tm.newStudioTask(step.Operation, "input.gguf", "", nil)
		if dispatched == 1 {
			child.mu.Lock()
			child.Output = "quantized.gguf"
			child.mu.Unlock()
			child.AddArtifact(Artifact{Name: "quantized.gguf", Path: "quantized.gguf", Kind: "gguf"})
		}
		child.UpdateProgress(TaskCompleted, "done", 100)
		return child, nil
	}
	request := StudioPipelineRequest{Input: "source.gguf", Steps: []StudioPipelineStep{
		{Operation: "quantize", Request: json.RawMessage(`{"output":"quantized.gguf","type":"Q4_K_M"}`)},
		{Operation: "evaluate", UsePrevious: true, Request: json.RawMessage(`{"mode":"benchmark"}`)},
	}}
	parent, err := tm.startStudioPipeline(request, "", dispatch)
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTaskState(t, parent, TaskCompleted)
	if secondInput != "quantized.gguf" {
		t.Fatalf("second step model = %q, want quantized.gguf", secondInput)
	}
	if result.Output != "quantized.gguf" || len(result.Artifacts) != 1 {
		t.Fatalf("unexpected pipeline result: %#v", result)
	}
	childIDs, ok := result.Parameters["childTaskIDs"].([]string)
	if !ok || len(childIDs) != 2 {
		t.Fatalf("childTaskIDs = %#v", result.Parameters["childTaskIDs"])
	}
}

func TestTaskManager_StudioPipelineStopsOnDispatchFailure(t *testing.T) {
	tm := NewTaskManager(nil)
	request := StudioPipelineRequest{Steps: []StudioPipelineStep{{
		Operation: "hash", Request: json.RawMessage(`{"input":"model.gguf"}`),
	}}}
	parent, err := tm.startStudioPipeline(request, "", func(StudioPipelineStep, string) (*Task, error) {
		return nil, errors.New("tool unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTaskState(t, parent, TaskFailed)
	if result.Message == "" {
		t.Fatal("pipeline failure did not include a message")
	}
}

func TestTaskManager_StudioPipelineCancellationCancelsChild(t *testing.T) {
	tm := NewTaskManager(nil)
	childReady := make(chan *Task, 1)
	request := StudioPipelineRequest{Steps: []StudioPipelineStep{{
		Operation: "hash", Request: json.RawMessage(`{"input":"model.gguf"}`),
	}}}
	parent, err := tm.startStudioPipeline(request, "", func(step StudioPipelineStep, _ string) (*Task, error) {
		child := tm.newStudioTask(step.Operation, "model.gguf", "", nil)
		childReady <- child
		return child, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	child := <-childReady
	if !tm.CancelTask(parent.ID) {
		t.Fatal("CancelTask(parent) returned false")
	}
	waitForTaskState(t, child, TaskCancelled)
}

func TestTaskManager_StudioPipelineRunsFanOutVariants(t *testing.T) {
	tm := NewTaskManager(nil)
	dispatched := make(chan string, 2)
	request := StudioPipelineRequest{Steps: []StudioPipelineStep{{Operation: "quantize", Variants: []json.RawMessage{
		json.RawMessage(`{"input":"model.gguf","output":"q4.gguf","type":"Q4_K_M"}`),
		json.RawMessage(`{"input":"model.gguf","output":"q6.gguf","type":"Q6_K"}`),
	}}}}
	parent, err := tm.startStudioPipeline(request, "", func(step StudioPipelineStep, _ string) (*Task, error) {
		var body map[string]any
		_ = json.Unmarshal(step.Request, &body)
		output, _ := body["output"].(string)
		dispatched <- output
		child := tm.newStudioTask(step.Operation, "model.gguf", output, nil)
		child.AddArtifact(Artifact{Name: output, Path: output, Kind: "gguf"})
		child.UpdateProgress(TaskCompleted, "done", 100)
		return child, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTaskState(t, parent, TaskCompleted)
	if len(result.Artifacts) != 2 || len(dispatched) != 2 {
		t.Fatalf("fan-out did not retain both variants: %#v", result)
	}
}

func TestValidateStudioPipeline_RejectsGateOnNonEvaluation(t *testing.T) {
	minimum := 1.0
	err := validateStudioPipeline(StudioPipelineRequest{Steps: []StudioPipelineStep{{Operation: "quantize", Request: json.RawMessage(`{"input":"model.gguf"}`), Gate: &StudioPipelineGate{Metric: "speed", Min: &minimum}}}})
	if err == nil {
		t.Fatal("expected non-evaluation gate to fail validation")
	}
}

func TestTaskManager_RetryStudioPipelineUsesPreviousOutput(t *testing.T) {
	tm := NewTaskManager(nil)
	child := tm.newStudioTask("quantize", "source.gguf", "quantized.gguf", nil)
	child.UpdateProgress(TaskCompleted, "done", 100)
	original := tm.newStudioTask("pipeline", "source.gguf", "", map[string]any{
		"name": "workflow", "childTaskIDs": []string{child.ID}, "steps": []StudioPipelineStep{
			{Operation: "quantize", Request: json.RawMessage(`{"output":"quantized.gguf","type":"Q4_K_M"}`)},
			{Operation: "evaluate", UsePrevious: true, Request: json.RawMessage(`{"mode":"benchmark"}`)},
		},
	})
	original.UpdateProgress(TaskFailed, "step 2 failed", 50)
	retry, err := tm.RetryStudioPipeline(original.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if retry.Snapshot().Input != "quantized.gguf" {
		t.Fatalf("retry input = %q", retry.Snapshot().Input)
	}
}

func waitForTaskState(t *testing.T, task *Task, state TaskState) *Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := task.Snapshot()
		if snapshot.State == state {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task state = %q, want %q", task.Snapshot().State, state)
	return nil
}
