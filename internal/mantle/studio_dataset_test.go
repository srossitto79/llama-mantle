package mantle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectStudioDataset_RecognizesSupportedFormats(t *testing.T) {
	modelsDir := t.TempDir()
	data := "{\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}\n" +
		"{\"prompt\":\"question\",\"response\":\"answer\"}\n" +
		"{\"text\":\"plain training text\"}\n"
	if err := os.WriteFile(filepath.Join(modelsDir, "train.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectStudioDataset(modelsDir, "train.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.RecordsScanned != 3 || inspection.Formats["messages"] != 1 ||
		inspection.Formats["prompt-response"] != 1 || inspection.Formats["text"] != 1 {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
}

func TestInspectStudioDataset_RejectsUnknownRecord(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(modelsDir, "bad.jsonl"), []byte("{\"input\":\"hello\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectStudioDataset(modelsDir, "bad.jsonl"); err == nil {
		t.Fatal("expected unsupported dataset record to fail")
	}
}
