package mantle

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/store"
)

func TestTaskManager_VerifyStudioArtifactStoresHashAndGGUFStatus(t *testing.T) {
	modelsDir := t.TempDir()
	content := minimalGGUF(t, map[string]string{"general.architecture": "llama"})
	if err := os.WriteFile(filepath.Join(modelsDir, "model.gguf"), content, 0644); err != nil {
		t.Fatal(err)
	}
	tm, st := taskManagerWithCatalogArtifact(t, modelsDir, "model.gguf")
	task, err := tm.StartVerifyStudioArtifact("model.gguf", modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskCompleted)
	annotation, err := st.GetStudioArtifactAnnotation(context.Background(), "model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(content))
	if annotation.SHA256 != wantHash || annotation.GGUFValid == nil || !*annotation.GGUFValid || annotation.VerifiedAt == nil {
		t.Fatalf("unexpected verification annotation: %#v", annotation)
	}
}

func TestTaskManager_CleanupStudioArtifactRequiresConfirmation(t *testing.T) {
	modelsDir := t.TempDir()
	path := filepath.Join(modelsDir, "model.gguf")
	if err := os.WriteFile(path, []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}
	tm, _ := taskManagerWithCatalogArtifact(t, modelsDir, "model.gguf")
	if _, err := tm.StartCleanupStudioArtifact(StudioArtifactCleanupRequest{Path: "model.gguf"}, modelsDir); err == nil {
		t.Fatal("cleanup succeeded without confirmation")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("unconfirmed cleanup changed the artifact")
	}
	task, err := tm.StartCleanupStudioArtifact(StudioArtifactCleanupRequest{Path: "model.gguf", Confirm: true}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskCompleted)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("artifact still exists after cleanup: %v", err)
	}
}

func TestTaskManager_StudioRetentionRejectsStalePreview(t *testing.T) {
	modelsDir := t.TempDir()
	path := filepath.Join(modelsDir, "old.gguf")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old := time.Now().Add(-48 * time.Hour)
	if err := st.SaveStudioJob(context.Background(), store.StudioJobRecord{
		ID: "old-producer", Operation: "quantize", State: "completed", Input: "source.gguf", Output: "old.gguf",
		ParametersJSON: `{}`, LogsJSON: `[]`, CreatedAt: old, UpdatedAt: old,
		Artifacts: []store.StudioArtifactRecord{{Name: "old.gguf", Path: "old.gguf", Size: 3, Kind: "gguf", MetadataJSON: `{}`}},
	}); err != nil {
		t.Fatal(err)
	}
	tm := NewTaskManager(nil)
	if err := tm.SetStudioStore(st); err != nil {
		t.Fatal(err)
	}
	policy := StudioRetentionPolicy{MaxAgeDays: 1}
	preview, err := tm.PreviewStudioRetention(modelsDir, policy)
	if err != nil || len(preview.Candidates) != 1 {
		t.Fatalf("preview = %#v, err = %v", preview, err)
	}
	if err := os.WriteFile(path, []byte("changed-size"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.StartApplyStudioRetention(policy, preview.Token, modelsDir); err == nil {
		t.Fatal("stale retention preview was accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("stale retention apply changed the artifact")
	}
}

func taskManagerWithCatalogArtifact(t *testing.T, modelsDir, path string) (*TaskManager, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "studio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now()
	if err := st.SaveStudioJob(context.Background(), store.StudioJobRecord{
		ID: "producer", Operation: "quantize", State: "completed", Input: "source.gguf", Output: path,
		ParametersJSON: `{}`, LogsJSON: `[]`, CreatedAt: now, UpdatedAt: now,
		Artifacts: []store.StudioArtifactRecord{{Name: path, Path: path, Kind: "gguf", MetadataJSON: `{}`}},
	}); err != nil {
		t.Fatal(err)
	}
	tm := NewTaskManager(nil)
	if err := tm.SetStudioStore(st); err != nil {
		t.Fatal(err)
	}
	return tm, st
}
