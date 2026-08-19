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
