package mantle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mostlygeek/llama-swap/internal/store"
)

type StudioProject struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Resources   []string  `json:"resources"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (tm *TaskManager) SaveStudioProject(project StudioProject) (*StudioProject, error) {
	project.Name = strings.TrimSpace(project.Name)
	project.Description = strings.TrimSpace(project.Description)
	if project.Name == "" || len(project.Name) > 100 {
		return nil, fmt.Errorf("project name must contain 1 to 100 characters")
	}
	if len(project.Description) > 2000 {
		return nil, fmt.Errorf("project description is too long")
	}
	if project.ID == "" {
		project.ID = strings.Replace(tm.newID(), "task-", "project-", 1)
	} else if !isSafeBackendName(project.ID) {
		return nil, fmt.Errorf("invalid project ID")
	}
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	now := time.Now()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now
	if err := st.SaveStudioProject(context.Background(), store.StudioProjectRecord{ID: project.ID, Name: project.Name, Description: project.Description, CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt}); err != nil {
		return nil, err
	}
	return &project, nil
}

func (tm *TaskManager) ListStudioProjects() ([]StudioProject, error) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	records, err := st.ListStudioProjects(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]StudioProject, 0, len(records))
	for _, item := range records {
		result = append(result, StudioProject{ID: item.ID, Name: item.Name, Description: item.Description, Resources: item.Resources, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return result, nil
}

func (tm *TaskManager) SetStudioProjectResources(projectID string, paths []string, modelsDir string) error {
	if !isSafeBackendName(projectID) {
		return fmt.Errorf("invalid project ID")
	}
	if len(paths) > 1000 {
		return fmt.Errorf("a project may contain at most 1000 resources")
	}
	available, err := tm.ListStudioResources(modelsDir)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, item := range available {
		known[item.Path] = true
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if !known[path] {
			return fmt.Errorf("resource %q is not in the Studio catalog", path)
		}
		if !seen[path] {
			seen[path] = true
			clean = append(clean, path)
		}
	}
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return fmt.Errorf("Studio storage is not configured")
	}
	return st.ReplaceStudioProjectResources(context.Background(), projectID, clean)
}

func (tm *TaskManager) DeleteStudioProject(id string) (bool, error) {
	if !isSafeBackendName(id) {
		return false, fmt.Errorf("invalid project ID")
	}
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return false, fmt.Errorf("Studio storage is not configured")
	}
	return st.DeleteStudioProject(context.Background(), id)
}
