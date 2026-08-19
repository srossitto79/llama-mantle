package mantle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskManager_ListStudioResourcesCombinesModelsAndDatasets(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(modelsDir, "model.gguf"), []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modelsDir, "datasets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "datasets", "train.jsonl"), []byte("{\"text\":\"hello\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resources, err := NewTaskManager(nil).ListStudioResources(modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected model and dataset, got %#v", resources)
	}
	seen := map[string]string{}
	for _, resource := range resources {
		seen[resource.Path] = resource.Type
	}
	if seen["model.gguf"] != "model" || seen["datasets/train.jsonl"] != "dataset" {
		t.Fatalf("unexpected resources: %#v", resources)
	}
}
