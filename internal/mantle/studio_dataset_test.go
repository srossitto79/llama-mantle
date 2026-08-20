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

func TestStudioDataset_ImportListAndPreview(t *testing.T) {
	modelsDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "source.jsonl")
	data := []byte("{\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}\n{\"text\":\"second\"}\n")
	if err := os.WriteFile(sourcePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	imported, err := ImportStudioDataset(modelsDir, "project/train.jsonl", source)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Path != "datasets/project/train.jsonl" || imported.Size != int64(len(data)) {
		t.Fatalf("unexpected imported dataset: %#v", imported)
	}
	datasets, err := ListStudioDatasets(modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(datasets) != 1 || datasets[0].Path != imported.Path {
		t.Fatalf("unexpected dataset catalog: %#v", datasets)
	}
	preview, err := PreviewStudioDataset(modelsDir, imported.Path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Records) != 1 || !preview.Truncated || preview.Formats["messages"] != 1 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
}

func TestStudioDataset_ImportRejectsOverwrite(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(modelsDir, "datasets"), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(modelsDir, "datasets", "train.jsonl")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := ImportStudioDataset(modelsDir, "datasets/train.jsonl", source); err == nil {
		t.Fatal("expected overwrite to be rejected")
	}
}

func TestStudioDataset_DeleteRemovesFile(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(modelsDir, "datasets"), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(modelsDir, "datasets", "train.jsonl")
	if err := os.WriteFile(target, []byte("{\"text\":\"hello\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteStudioDataset(modelsDir, "datasets/train.jsonl"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected dataset to be removed, stat err = %v", err)
	}
}

func TestStudioDataset_DeleteRejectsPathOutsideDatasetsDirectory(t *testing.T) {
	modelsDir := t.TempDir()
	target := filepath.Join(modelsDir, "model.gguf")
	if err := os.WriteFile(target, []byte("gguf"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteStudioDataset(modelsDir, "model.gguf"); err == nil {
		t.Fatal("expected deletion outside datasets/ to be rejected")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("file outside datasets/ should not have been removed")
	}
}

func TestStudioDataset_DeleteRejectsMissingFile(t *testing.T) {
	modelsDir := t.TempDir()
	if err := DeleteStudioDataset(modelsDir, "datasets/missing.jsonl"); err == nil {
		t.Fatal("expected deletion of a missing dataset to fail")
	}
}

func TestStudioDataset_ImportRejectsDatasetDirectoryEscape(t *testing.T) {
	modelsDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "source.jsonl")
	if err := os.WriteFile(sourcePath, []byte("{\"text\":\"hello\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := ImportStudioDataset(modelsDir, "../outside.jsonl", source); err == nil {
		t.Fatal("expected datasets directory escape to be rejected")
	}
}
