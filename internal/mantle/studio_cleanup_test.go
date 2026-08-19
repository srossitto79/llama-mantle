package mantle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStudioStaging_RemovesOnlyStaleInactiveOutputs(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	stale := filepath.Join(root, ".model.task-stale.partial.gguf")
	active := filepath.Join(root, ".model.task-active.partial.gguf")
	recent := filepath.Join(root, ".model.task-recent.partial.gguf")
	unrelated := filepath.Join(root, ".ordinary.partial.gguf")
	staleDir := filepath.Join(root, ".profiles.task-stale-dir.partial")
	for _, path := range []string{stale, active, recent, unrelated} {
		if err := os.WriteFile(path, []byte("partial"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(staleDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{stale, active, unrelated, staleDir} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := cleanupStudioStaging(root, map[string]struct{}{"task-active": {}}, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	for _, path := range []string{active, recent, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preserved path %q: %v", filepath.Base(path), err)
		}
	}
	for _, path := range []string{stale, staleDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale path %q still exists: %v", filepath.Base(path), err)
		}
	}
}

func TestStudioStagingTaskID(t *testing.T) {
	for name, want := range map[string]string{
		".model.task-123-4.partial.gguf":                "task-123-4",
		".model.task-123-4.partial-00001-of-00002.gguf": "task-123-4",
		"model.task-123-4.partial.gguf":                 "",
		".ordinary.partial.gguf":                        "",
	} {
		if got := studioStagingTaskID(name); got != want {
			t.Errorf("studioStagingTaskID(%q) = %q, want %q", name, got, want)
		}
	}
}
