package mantle

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StudioResource struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Type      string    `json:"type"`
	Kind      string    `json:"kind"`
	Size      int64     `json:"size"`
	Exists    bool      `json:"exists"`
	JobID     string    `json:"jobID,omitempty"`
	Operation string    `json:"operation,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

func (tm *TaskManager) ListStudioResources(modelsDir string) ([]StudioResource, error) {
	resources := make(map[string]StudioResource)
	models, err := ListLocalModels(modelsDir)
	if err != nil {
		return nil, err
	}
	for _, model := range models {
		if model.Kind != "gguf" {
			continue
		}
		path := filepath.ToSlash(model.Name)
		resources[path] = StudioResource{Name: filepath.Base(path), Path: path, Type: "model", Kind: model.Kind, Size: model.Size, Exists: true}
	}
	datasets, err := ListStudioDatasets(modelsDir)
	if err != nil {
		return nil, err
	}
	for _, dataset := range datasets {
		resources[dataset.Path] = StudioResource{Name: dataset.Name, Path: dataset.Path, Type: "dataset", Kind: dataset.Format, Size: dataset.Size, Exists: true}
	}
	tm.mu.Lock()
	hasStore := tm.studioStore != nil
	tm.mu.Unlock()
	artifacts, err := tm.ListStudioCatalogArtifacts(modelsDir, "", 10000)
	if err != nil && hasStore {
		return nil, err
	}
	if err == nil {
		for _, artifact := range artifacts {
			typeName := "artifact"
			if strings.HasPrefix(artifact.Kind, "lora-") {
				typeName = "adapter"
			}
			if artifact.Kind == "lora-checkpoint" {
				typeName = "checkpoint"
			}
			if strings.HasPrefix(artifact.Kind, "gguf") || artifact.Kind == "served-model" {
				typeName = "model"
			}
			if artifact.Kind == "dataset" {
				typeName = "dataset"
			}
			resources[artifact.Path] = StudioResource{Name: artifact.Name, Path: artifact.Path, Type: typeName, Kind: artifact.Kind, Size: artifact.Size, Exists: artifact.Exists, JobID: artifact.JobID, Operation: artifact.Operation, CreatedAt: artifact.CreatedAt}
		}
	}
	result := make([]StudioResource, 0, len(resources))
	for _, resource := range resources {
		result = append(result, resource)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type == result[j].Type {
			return result[i].Path < result[j].Path
		}
		return result[i].Type < result[j].Type
	})
	return result, nil
}
