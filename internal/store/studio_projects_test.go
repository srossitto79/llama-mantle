package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_StudioProjectPersistsResources(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "studio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().Truncate(time.Millisecond)
	if err := st.SaveStudioProject(context.Background(), StudioProjectRecord{ID: "project-1", Name: "Experiment", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceStudioProjectResources(context.Background(), "project-1", []string{"model.gguf", "datasets/train.jsonl"}); err != nil {
		t.Fatal(err)
	}
	projects, err := st.ListStudioProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || len(projects[0].Resources) != 2 {
		t.Fatalf("unexpected projects: %#v", projects)
	}
}

func TestStore_StudioProjectExists(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SaveStudioProject(context.Background(), StudioProjectRecord{ID: "project-1", Name: "Experiment", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	exists, err := st.StudioProjectExists(context.Background(), "project-1")
	if err != nil || !exists {
		t.Fatalf("exists = %v, err = %v", exists, err)
	}
}
