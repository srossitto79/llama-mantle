package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_StudioPipelineTemplateCRUD(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "templates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().Truncate(time.Millisecond)
	record := StudioPipelineTemplateRecord{
		ID: "pipeline-1", ProjectID: "project-1", Name: "Quantize", DefinitionJSON: `{"steps":[{"operation":"quantize","request":{}}]}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.SaveStudioPipelineTemplate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	templates, err := st.ListStudioPipelineTemplates(context.Background())
	if err != nil || len(templates) != 1 || templates[0].Name != "Quantize" || templates[0].ProjectID != "project-1" {
		t.Fatalf("templates = %#v, err = %v", templates, err)
	}
	record.Name = "Updated"
	record.UpdatedAt = now.Add(time.Second)
	if err := st.SaveStudioPipelineTemplate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	templates, err = st.ListStudioPipelineTemplates(context.Background())
	if err != nil || len(templates) != 1 || templates[0].Name != "Updated" {
		t.Fatalf("updated templates = %#v, err = %v", templates, err)
	}
	deleted, err := st.DeleteStudioPipelineTemplate(context.Background(), record.ID)
	if err != nil || !deleted {
		t.Fatalf("deleted = %v, err = %v", deleted, err)
	}
}
