package mantle

import (
	"strings"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
)

const fixtureYAML = `
macros:
  modelsDir: /models

globalTTL: 600

models:
  llama3:
    cmd: llama-server --port ${PORT} -m /models/llama3.gguf
    ttl: 300
  qwen:
    cmd: llama-server --port ${PORT} -m /models/qwen.gguf
    ttl: 600

groups:
  group1:
    swap: true
    exclusive: true
    members:
      - llama3
      - qwen
`

func TestUpsertModelYAML_LeavesOtherSectionsUntouched(t *testing.T) {
	newModel := config.ModelConfig{Cmd: "llama-server --port ${PORT} -m /models/llama3-new.gguf", UnloadAfter: 900}
	out, err := UpsertModelYAML([]byte(fixtureYAML), "llama3", newModel)
	if err != nil {
		t.Fatalf("UpsertModelYAML failed: %v", err)
	}
	result := string(out)

	if !strings.Contains(result, "llama3-new.gguf") {
		t.Fatalf("expected updated model cmd in output, got:\n%s", result)
	}
	if !strings.Contains(result, "qwen.gguf") {
		t.Fatalf("expected untouched sibling model qwen to survive, got:\n%s", result)
	}
	if !strings.Contains(result, "modelsDir") {
		t.Fatalf("expected macros section to survive untouched, got:\n%s", result)
	}
	if !strings.Contains(result, "globalTTL") {
		t.Fatalf("expected unrelated top-level fields to survive untouched, got:\n%s", result)
	}
	if !strings.Contains(result, "group1") {
		t.Fatalf("expected groups section to survive untouched, got:\n%s", result)
	}

	// Round-trip through the real config loader to make sure the produced
	// YAML is not just textually plausible but actually valid.
	reloaded, err := config.LoadConfigFromReader(strings.NewReader(result))
	if err != nil {
		t.Fatalf("produced YAML failed to reload: %v", err)
	}
	if reloaded.Models["llama3"].UnloadAfter != 900 {
		t.Fatalf("expected reloaded llama3 ttl 900, got %d", reloaded.Models["llama3"].UnloadAfter)
	}
	if _, ok := reloaded.Models["qwen"]; !ok {
		t.Fatalf("expected qwen model to still be present after reload")
	}
}

func TestUpsertModelYAML_AddsNewModel(t *testing.T) {
	newModel := config.ModelConfig{Cmd: "llama-server --port ${PORT} -m /models/mixtral.gguf"}
	out, err := UpsertModelYAML([]byte(fixtureYAML), "mixtral", newModel)
	if err != nil {
		t.Fatalf("UpsertModelYAML failed: %v", err)
	}
	reloaded, err := config.LoadConfigFromReader(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("produced YAML failed to reload: %v", err)
	}
	if len(reloaded.Models) != 3 {
		t.Fatalf("expected 3 models after add, got %d", len(reloaded.Models))
	}
	if _, ok := reloaded.Models["mixtral"]; !ok {
		t.Fatalf("expected new mixtral model to be present")
	}
}

func TestDeleteModelYAML_RemovesOnlyTargetModel(t *testing.T) {
	out, err := DeleteModelYAML([]byte(fixtureYAML), "qwen")
	if err != nil {
		t.Fatalf("DeleteModelYAML failed: %v", err)
	}
	reloaded, err := config.LoadConfigFromReader(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("produced YAML failed to reload: %v", err)
	}
	if _, ok := reloaded.Models["qwen"]; ok {
		t.Fatalf("expected qwen to be removed")
	}
	if _, ok := reloaded.Models["llama3"]; !ok {
		t.Fatalf("expected llama3 to survive")
	}
}

func TestUpsertGroupYAML_LeavesModelsUntouched(t *testing.T) {
	newGroup := config.GroupConfig{Swap: true, Exclusive: false, Members: []string{"llama3"}}
	out, err := UpsertGroupYAML([]byte(fixtureYAML), "group1", newGroup)
	if err != nil {
		t.Fatalf("UpsertGroupYAML failed: %v", err)
	}
	reloaded, err := config.LoadConfigFromReader(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("produced YAML failed to reload: %v", err)
	}
	if reloaded.Groups["group1"].Exclusive {
		t.Fatalf("expected group1.exclusive to be false after update")
	}
	if len(reloaded.Models) != 2 {
		t.Fatalf("expected models section untouched, got %d models", len(reloaded.Models))
	}
}

func TestDeleteGroupYAML_RemovesOnlyTargetGroup(t *testing.T) {
	out, err := DeleteGroupYAML([]byte(fixtureYAML), "group1")
	if err != nil {
		t.Fatalf("DeleteGroupYAML failed: %v", err)
	}
	reloaded, err := config.LoadConfigFromReader(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("produced YAML failed to reload: %v", err)
	}
	if _, ok := reloaded.Groups["group1"]; ok {
		t.Fatalf("expected group1 to be removed")
	}
}

func TestListModels_SortedByName(t *testing.T) {
	cfg, err := config.LoadConfigFromReader(strings.NewReader(fixtureYAML))
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}
	models := ListModels(&cfg)
	if len(models) != 2 || models[0].ID != "llama3" || models[1].ID != "qwen" {
		t.Fatalf("expected sorted [llama3 qwen], got %+v", models)
	}
}
