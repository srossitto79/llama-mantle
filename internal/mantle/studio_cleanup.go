package mantle

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func studioStagingMaxAge() time.Duration {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("LLAMA_STUDIO_STAGING_MAX_AGE_HOURS")), 10, 64)
	if err != nil || value < 1 {
		value = 24
	}
	return time.Duration(value) * time.Hour
}

func studioStagingTaskID(name string) string {
	if !strings.HasPrefix(name, ".") {
		return ""
	}
	start := strings.Index(name, ".task-")
	if start < 0 {
		return ""
	}
	start++
	remainder := name[start:]
	end := strings.Index(remainder, ".partial")
	if end < 0 {
		return ""
	}
	return remainder[:end]
}

func cleanupStudioStaging(modelsDir string, active map[string]struct{}, olderThan time.Time) (int, error) {
	root, err := filepath.Abs(modelsDir)
	if err != nil {
		return 0, fmt.Errorf("resolve models directory: %w", err)
	}
	removed := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		taskID := studioStagingTaskID(entry.Name())
		if taskID == "" {
			return nil
		}
		if _, exists := active[taskID]; exists {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(olderThan) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			removed++
			return filepath.SkipDir
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("clean Studio staging outputs: %w", err)
	}
	return removed, nil
}

// StartStudioStagingCleanup removes stale, task-owned partial outputs once per
// models root. Live task IDs are excluded even when handlers are recreated.
func (tm *TaskManager) StartStudioStagingCleanup(modelsDir string) {
	root := filepath.Clean(modelsDir)
	tm.mu.Lock()
	if _, exists := tm.studioCleanupRoots[root]; exists {
		tm.mu.Unlock()
		return
	}
	tm.studioCleanupRoots[root] = struct{}{}
	tasks := make([]*Task, 0, len(tm.tasks))
	for _, task := range tm.tasks {
		tasks = append(tasks, task)
	}
	tm.mu.Unlock()
	active := make(map[string]struct{})
	for _, task := range tasks {
		if task.Type == "studio" && !task.IsTerminal() {
			active[task.ID] = struct{}{}
		}
	}
	go func() {
		removed, err := cleanupStudioStaging(root, active, time.Now().Add(-studioStagingMaxAge()))
		if tm.log == nil {
			return
		}
		if err != nil {
			tm.log.Errorf("[studio] %v", err)
		} else if removed > 0 {
			tm.log.Infof("[studio] removed %d stale staging outputs", removed)
		}
	}()
}
