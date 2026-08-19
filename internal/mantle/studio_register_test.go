package mantle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
)

func TestAddStudioModelToConfig_PreservesConfigAndAddsModel(t *testing.T) {
	body := []byte("# keep this comment\nmodels:\n  existing:\n    cmd: echo ${PORT}\n")
	updated, err := addStudioModelToConfig(body, RegisterStudioModelRequest{
		ModelID: "studio-model", Name: "Studio Model", ContextSize: 4096, GPULayers: -1, TTL: 600,
	}, "/models/generated.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "# keep this comment") {
		t.Fatal("top-level YAML comment was not preserved")
	}
	cfg, err := config.LoadConfigFromReader(strings.NewReader(string(updated)))
	if err != nil {
		t.Fatal(err)
	}
	model, exists := cfg.Models["studio-model"]
	if !exists {
		t.Fatal("registered model missing from config")
	}
	if !strings.Contains(model.Cmd, "generated.gguf") || !strings.Contains(model.Cmd, "--ctx-size 4096") || !strings.Contains(model.Cmd, "--n-gpu-layers -1") {
		t.Fatalf("unexpected model command: %q", model.Cmd)
	}
	if model.UnloadAfter != 600 || model.Name != "Studio Model" {
		t.Fatalf("unexpected registered model: %#v", model)
	}
}

func TestAddStudioModelToConfig_RequiresOverwrite(t *testing.T) {
	body := []byte("models:\n  existing:\n    cmd: echo ${PORT}\n")
	_, err := addStudioModelToConfig(body, RegisterStudioModelRequest{ModelID: "existing"}, "/models/model.gguf")
	if err == nil {
		t.Fatal("existing model ID was overwritten without permission")
	}
}

func TestTaskManager_RegisterStudioModelCompletes(t *testing.T) {
	modelsDir := t.TempDir()
	modelPath := filepath.Join(modelsDir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("gguf"), 0644); err != nil {
		t.Fatal(err)
	}
	tm := NewTaskManager(nil)
	called := false
	task, err := tm.StartRegisterStudioModel(RegisterStudioModelRequest{Model: "model.gguf", ModelID: "model"}, modelsDir,
		func(_ RegisterStudioModelRequest, path string) error {
			called = path == modelPath
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTaskState(t, task, TaskCompleted)
	if !called || len(result.Artifacts) != 1 || result.Artifacts[0].Kind != "served-model" {
		t.Fatalf("unexpected registration result: %#v", result)
	}
}
