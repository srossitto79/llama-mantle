package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_StudioJobPersistenceAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "studio.db")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Millisecond)
	queuedAt := now.Add(-2 * time.Second)
	startedAt := now.Add(-time.Second)
	code := 0
	err = st.SaveStudioJob(context.Background(), StudioJobRecord{
		ID: "studio-1", Operation: "quantize", State: "running", Message: "working", Pct: 42,
		Input: "source.gguf", Output: "result.gguf", ParametersJSON: `{"type":"Q4_K_M"}`,
		LogsJSON: `["one","two"]`, ExitCode: &code, CreatedAt: now, UpdatedAt: now,
		JobClass: "heavy", QueuedAt: &queuedAt, StartedAt: &startedAt,
		Artifacts: []StudioArtifactRecord{{Name: "result.gguf", Path: "result.gguf", Size: 123, Kind: "gguf", MetadataJSON: `{}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.RecoverStudioJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.ListStudioJobs(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	if jobs[0].State != "failed" || jobs[0].Message != "Interrupted by process restart" {
		t.Fatalf("unexpected recovered job: %#v", jobs[0])
	}
	if jobs[0].JobClass != "heavy" || jobs[0].QueuedAt == nil || jobs[0].StartedAt == nil || jobs[0].FinishedAt == nil {
		t.Fatalf("unexpected scheduling metadata: %#v", jobs[0])
	}
	if len(jobs[0].Artifacts) != 1 || jobs[0].Artifacts[0].Path != "result.gguf" {
		t.Fatalf("unexpected artifacts: %#v", jobs[0].Artifacts)
	}
}

func TestStore_StudioArtifactCatalogPrefersProducingOperation(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().Truncate(time.Millisecond)
	for _, job := range []StudioJobRecord{
		{ID: "producer", Operation: "quantize", State: "completed", Input: "source.gguf", Output: "result.gguf", ParametersJSON: `{}`, LogsJSON: `[]`, CreatedAt: now, UpdatedAt: now, Artifacts: []StudioArtifactRecord{{Name: "result.gguf", Path: "result.gguf", Size: 100, Kind: "gguf", MetadataJSON: `{}`}}},
		{ID: "pipeline", Operation: "pipeline", State: "completed", Input: "source.gguf", Output: "result.gguf", ParametersJSON: `{}`, LogsJSON: `[]`, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second), Artifacts: []StudioArtifactRecord{{Name: "result.gguf", Path: "result.gguf", Size: 100, Kind: "gguf", MetadataJSON: `{}`}}},
	} {
		if err := st.SaveStudioJob(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	artifacts, err := st.ListStudioCatalogArtifacts(context.Background(), 10, "")
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts = %#v, err = %v", artifacts, err)
	}
	if artifacts[0].JobID != "producer" || artifacts[0].Operation != "quantize" {
		t.Fatalf("catalog selected aggregate instead of producer: %#v", artifacts[0])
	}
	lineage, err := st.ListStudioLineage(context.Background())
	if err != nil || len(lineage) != 2 {
		t.Fatalf("lineage = %#v, err = %v", lineage, err)
	}
}
