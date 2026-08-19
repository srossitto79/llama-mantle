package mantle

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mostlygeek/llama-swap/internal/store"
)

type StudioCatalogArtifact struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Size        int64          `json:"size"`
	Kind        string         `json:"kind"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	JobID       string         `json:"jobID"`
	ProjectID   string         `json:"projectID,omitempty"`
	Operation   string         `json:"operation"`
	Input       string         `json:"input,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	Exists      bool           `json:"exists"`
	SHA256      string         `json:"sha256,omitempty"`
	GGUFValid   *bool          `json:"ggufValid,omitempty"`
	VerifyError string         `json:"verificationError,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Notes       string         `json:"notes,omitempty"`
	VerifiedAt  *time.Time     `json:"verifiedAt,omitempty"`
	Registered  bool           `json:"registered,omitempty"`
}

type StudioArtifactAnnotationRequest struct {
	Path  string   `json:"path"`
	Tags  []string `json:"tags"`
	Notes string   `json:"notes"`
}

type StudioArtifactCleanupRequest struct {
	Path    string `json:"path"`
	Confirm bool   `json:"confirm"`
}

type StudioRetentionPolicy struct {
	MaxAgeDays    int      `json:"maxAgeDays"`
	Kinds         []string `json:"kinds,omitempty"`
	IncludeTagged bool     `json:"includeTagged,omitempty"`
}

type StudioRetentionPreview struct {
	Token      string                  `json:"token"`
	Candidates []StudioCatalogArtifact `json:"candidates"`
	TotalBytes int64                   `json:"totalBytes"`
}

var studioArtifactTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)

type StudioLineageEdge struct {
	JobID     string    `json:"jobID"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	Relation  string    `json:"relation"`
	CreatedAt time.Time `json:"createdAt"`
}

func (tm *TaskManager) ListStudioCatalogArtifacts(modelsDir, kind string, limit int) ([]StudioCatalogArtifact, error) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	records, err := st.ListStudioCatalogArtifacts(context.Background(), limit, kind)
	if err != nil {
		return nil, err
	}
	artifacts := make([]StudioCatalogArtifact, 0, len(records))
	for _, record := range records {
		var metadata map[string]any
		_ = json.Unmarshal([]byte(record.MetadataJSON), &metadata)
		artifact := StudioCatalogArtifact{
			Name: record.Name, Path: record.Path, Size: record.Size, Kind: record.Kind,
			Metadata: metadata, JobID: record.JobID, ProjectID: record.ProjectID, Operation: record.Operation,
			Input: record.Input, CreatedAt: record.CreatedAt, SHA256: record.SHA256,
			GGUFValid: record.GGUFValid, VerifyError: record.VerifyError, Notes: record.Notes,
			VerifiedAt: record.VerifiedAt, Registered: record.Registered,
		}
		_ = json.Unmarshal([]byte(record.TagsJSON), &artifact.Tags)
		if path, _, pathErr := resolveStudioPath(modelsDir, record.Path, ""); pathErr == nil {
			if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
				artifact.Exists = info.Mode().IsRegular() || info.IsDir()
				artifact.Size = info.Size()
			}
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func (tm *TaskManager) SaveStudioArtifactAnnotation(req StudioArtifactAnnotationRequest) error {
	if len(req.Tags) > 20 || len(req.Notes) > 4000 {
		return fmt.Errorf("artifact annotations exceed their size limit")
	}
	seen := make(map[string]bool)
	tags := make([]string, 0, len(req.Tags))
	for _, tag := range req.Tags {
		tag = strings.TrimSpace(tag)
		if !studioArtifactTagPattern.MatchString(tag) {
			return fmt.Errorf("invalid artifact tag %q", tag)
		}
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	st, err := tm.studioStoreForArtifact(req.Path)
	if err != nil {
		return err
	}
	annotation, err := st.GetStudioArtifactAnnotation(context.Background(), req.Path)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(tags)
	annotation.TagsJSON = string(encoded)
	annotation.Notes = strings.TrimSpace(req.Notes)
	annotation.UpdatedAt = time.Now()
	return st.SaveStudioArtifactAnnotation(context.Background(), annotation)
}

func (tm *TaskManager) StartVerifyStudioArtifact(path, modelsDir string) (*Task, error) {
	filePath, clean, err := resolveStudioInput(modelsDir, path, "")
	if err != nil {
		return nil, err
	}
	st, err := tm.studioStoreForArtifact(clean)
	if err != nil {
		return nil, err
	}
	task := tm.newStudioTask("verify-artifact", clean, "", map[string]any{"path": clean})
	tm.enqueueStudioTask(task, StudioJobIO, func() {
		tm.verifyStudioArtifact(task, st, filePath, clean)
	})
	return task, nil
}

func (tm *TaskManager) StartVerifyStudioArtifacts(paths []string, modelsDir string) (*Task, error) {
	if len(paths) == 0 || len(paths) > 250 {
		return nil, fmt.Errorf("bulk verification requires 1 to 250 artifacts")
	}
	for _, path := range paths {
		if _, err := tm.studioStoreForArtifact(path); err != nil {
			return nil, err
		}
	}
	parent := tm.newStudioTask("verify-artifacts", "", "", map[string]any{"paths": paths})
	parent.mu.Lock()
	parent.JobClass = "workflow"
	parent.mu.Unlock()
	parent.persistNow()
	go func() {
		failures := 0
		for index, path := range paths {
			if parent.Context().Err() != nil {
				return
			}
			parent.UpdateProgress(TaskRunning, fmt.Sprintf("Verifying %d/%d: %s", index+1, len(paths), path), index*100/len(paths))
			child, err := tm.StartVerifyStudioArtifact(path, modelsDir)
			if err != nil {
				failures++
				parent.AppendLog(path + ": " + err.Error())
				continue
			}
			result, cancelled := tm.waitForPipelineChild(parent, child)
			if cancelled {
				return
			}
			if result.State != TaskCompleted {
				failures++
				parent.AppendLog(path + ": " + result.Message)
			}
		}
		if failures > 0 {
			parent.UpdateProgress(TaskFailed, fmt.Sprintf("Verification completed with %d failures", failures), 100)
		} else {
			parent.UpdateProgress(TaskCompleted, fmt.Sprintf("Verified %d artifacts", len(paths)), 100)
		}
	}()
	return parent, nil
}

func (tm *TaskManager) PreviewStudioRetention(modelsDir string, policy StudioRetentionPolicy) (*StudioRetentionPreview, error) {
	if policy.MaxAgeDays < 1 || policy.MaxAgeDays > 36500 {
		return nil, fmt.Errorf("retention age must be between 1 and 36500 days")
	}
	artifacts, err := tm.ListStudioCatalogArtifacts(modelsDir, "", 10000)
	if err != nil {
		return nil, err
	}
	kinds := make(map[string]bool)
	for _, kind := range policy.Kinds {
		kinds[kind] = true
	}
	cutoff := time.Now().Add(-time.Duration(policy.MaxAgeDays) * 24 * time.Hour)
	preview := &StudioRetentionPreview{}
	for _, artifact := range artifacts {
		if !artifact.Exists || !artifact.CreatedAt.Before(cutoff) || (len(kinds) > 0 && !kinds[artifact.Kind]) ||
			(!policy.IncludeTagged && len(artifact.Tags) > 0) || artifact.Registered {
			continue
		}
		preview.Candidates = append(preview.Candidates, artifact)
		preview.TotalBytes += artifact.Size
	}
	sort.Slice(preview.Candidates, func(i, j int) bool { return preview.Candidates[i].Path < preview.Candidates[j].Path })
	digest := sha256.New()
	for _, artifact := range preview.Candidates {
		fmt.Fprintf(digest, "%s\x00%d\x00%d\n", artifact.Path, artifact.Size, artifact.CreatedAt.UnixMilli())
	}
	preview.Token = fmt.Sprintf("%x", digest.Sum(nil))
	return preview, nil
}

func (tm *TaskManager) StartApplyStudioRetention(policy StudioRetentionPolicy, token, modelsDir string) (*Task, error) {
	preview, err := tm.PreviewStudioRetention(modelsDir, policy)
	if err != nil {
		return nil, err
	}
	if token == "" || token != preview.Token {
		return nil, fmt.Errorf("retention preview is stale; preview the policy again")
	}
	paths := make([]string, len(preview.Candidates))
	for i, artifact := range preview.Candidates {
		paths[i] = artifact.Path
	}
	parent := tm.newStudioTask("retention-cleanup", "", "", map[string]any{"policy": policy, "paths": paths, "previewToken": token})
	parent.mu.Lock()
	parent.JobClass = "workflow"
	parent.mu.Unlock()
	parent.persistNow()
	go func() {
		for index, path := range paths {
			if parent.Context().Err() != nil {
				return
			}
			parent.UpdateProgress(TaskRunning, fmt.Sprintf("Removing %d/%d: %s", index+1, len(paths), path), index*100/max(1, len(paths)))
			child, err := tm.StartCleanupStudioArtifact(StudioArtifactCleanupRequest{Path: path, Confirm: true}, modelsDir)
			if err != nil {
				parent.UpdateProgress(TaskFailed, fmt.Sprintf("cleanup %s: %v", path, err), index*100/max(1, len(paths)))
				return
			}
			result, cancelled := tm.waitForPipelineChild(parent, child)
			if cancelled {
				return
			}
			if result.State != TaskCompleted {
				parent.UpdateProgress(TaskFailed, result.Message, index*100/max(1, len(paths)))
				return
			}
		}
		parent.UpdateProgress(TaskCompleted, fmt.Sprintf("Removed %d retained artifacts", len(paths)), 100)
	}()
	return parent, nil
}

func (tm *TaskManager) verifyStudioArtifact(task *Task, st *store.Store, filePath, clean string) {
	file, err := os.Open(filePath)
	if err != nil {
		task.UpdateProgress(TaskFailed, fmt.Sprintf("open artifact: %v", err), 0)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		task.UpdateProgress(TaskFailed, "artifact is not a regular file", 0)
		return
	}
	hash := sha256.New()
	buffer := make([]byte, 4*1024*1024)
	var read int64
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
			read += int64(count)
			pct := 0
			if info.Size() > 0 {
				pct = min(95, int(read*95/info.Size()))
			}
			task.UpdateProgress(TaskRunning, "Calculating SHA-256...", pct)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			task.UpdateProgress(TaskFailed, fmt.Sprintf("read artifact: %v", readErr), 0)
			return
		}
		if task.Context().Err() != nil {
			return
		}
	}
	annotation, err := st.GetStudioArtifactAnnotation(context.Background(), clean)
	if err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return
	}
	annotation.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	annotation.VerifyError = ""
	annotation.GGUFValid = nil
	if strings.EqualFold(filepath.Ext(clean), ".gguf") {
		valid := true
		if _, metadataErr := ReadGGUFMetadata(filePath); metadataErr != nil {
			valid = false
			annotation.VerifyError = metadataErr.Error()
		}
		annotation.GGUFValid = &valid
	}
	now := time.Now()
	annotation.VerifiedAt = &now
	annotation.UpdatedAt = now
	if err := st.SaveStudioArtifactAnnotation(context.Background(), annotation); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return
	}
	if annotation.GGUFValid != nil && !*annotation.GGUFValid {
		task.UpdateProgress(TaskFailed, "GGUF validation failed: "+annotation.VerifyError, 100)
		return
	}
	task.UpdateProgress(TaskCompleted, "Artifact verification complete", 100)
}

func (tm *TaskManager) StartCleanupStudioArtifact(req StudioArtifactCleanupRequest, modelsDir string) (*Task, error) {
	if !req.Confirm {
		return nil, fmt.Errorf("artifact cleanup requires explicit confirmation")
	}
	filePath, clean, err := resolveStudioInput(modelsDir, req.Path, "")
	if err != nil {
		return nil, err
	}
	if _, err := tm.studioStoreForArtifact(clean); err != nil {
		return nil, err
	}
	info, err := os.Lstat(filePath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cleanup currently supports regular files only")
	}
	task := tm.newStudioTask("cleanup-artifact", clean, "", map[string]any{"path": clean})
	tm.enqueueStudioTask(task, StudioJobIO, func() {
		if err := os.Remove(filePath); err != nil {
			task.UpdateProgress(TaskFailed, fmt.Sprintf("remove artifact: %v", err), 0)
			return
		}
		task.UpdateProgress(TaskCompleted, "Artifact removed; lineage retained", 100)
	})
	return task, nil
}

func (tm *TaskManager) studioStoreForArtifact(path string) (*store.Store, error) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	exists, err := st.StudioArtifactExists(context.Background(), path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("artifact %q is not in the Studio catalog", path)
	}
	return st, nil
}

func (tm *TaskManager) StudioLineage(path string) ([]StudioLineageEdge, error) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	records, err := st.ListStudioLineage(context.Background())
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	if path != "" {
		active[path] = true
	}
	selected := make([]bool, len(records))
	if path == "" {
		for i := range selected {
			selected[i] = true
		}
	} else {
		changed := true
		for changed {
			changed = false
			for i, edge := range records {
				if selected[i] || (!active[edge.Input] && !active[edge.Output]) {
					continue
				}
				selected[i] = true
				active[edge.Input] = true
				active[edge.Output] = true
				changed = true
			}
		}
	}
	edges := make([]StudioLineageEdge, 0)
	for i, record := range records {
		if selected[i] {
			edges = append(edges, StudioLineageEdge{
				JobID: record.JobID, Input: record.Input, Output: record.Output,
				Relation: record.Relation, CreatedAt: record.CreatedAt,
			})
		}
	}
	return edges, nil
}
