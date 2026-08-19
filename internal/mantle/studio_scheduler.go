package mantle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	StudioJobLight = "light"
	StudioJobIO    = "io"
	StudioJobHeavy = "heavy"
)

type studioQueueItem struct {
	task       *Task
	class      string
	run        func()
	outputKeys []string
}

type StudioSchedulerStatus struct {
	MaxRunning    int    `json:"maxRunning"`
	MaxHeavy      int    `json:"maxHeavy"`
	Running       int    `json:"running"`
	HeavyRunning  int    `json:"heavyRunning"`
	Queued        int    `json:"queued"`
	Blocked       int    `json:"blocked"`
	BlockedReason string `json:"blockedReason,omitempty"`
}

type StudioResourceSnapshot struct {
	FreeRAMBytes  int64
	FreeVRAMBytes int64
	RAMKnown      bool
	VRAMKnown     bool
}

func studioJobLimit(envName string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(envName)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func studioResourceReserve(envName string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(envName)), 10, 64)
	if err != nil || value < 0 {
		return fallback * gib
	}
	return value * gib
}

func (tm *TaskManager) SetStudioResourceProvider(provider func() StudioResourceSnapshot) {
	tm.mu.Lock()
	tm.studioResources = provider
	tm.mu.Unlock()
	tm.dispatchStudioJobs()
}

func (tm *TaskManager) enqueueStudioTask(task *Task, class string, run func()) {
	if err := tm.enqueueStudioTaskWithOutputs(task, class, nil, run); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
	}
}

func (tm *TaskManager) enqueueStudioTaskWithOutputs(task *Task, class string, outputs []string, run func()) error {
	if class != StudioJobLight && class != StudioJobIO && class != StudioJobHeavy {
		class = StudioJobHeavy
	}
	now := timeNow()
	task.mu.Lock()
	task.State = TaskQueued
	task.Message = "Queued"
	task.JobClass = class
	task.QueuedAt = &now
	task.UpdatedAt = now
	task.mu.Unlock()

	keys := make([]string, 0, len(outputs))
	for _, output := range outputs {
		if output != "" {
			keys = append(keys, strings.ToLower(filepath.Clean(output)))
		}
	}
	tm.mu.Lock()
	for _, key := range keys {
		if owner, exists := tm.studioOutputs[key]; exists {
			tm.mu.Unlock()
			return fmt.Errorf("output is already reserved by job %s", owner)
		}
	}
	for _, key := range keys {
		tm.studioOutputs[key] = task.ID
	}
	tm.studioQueue = append(tm.studioQueue, &studioQueueItem{task: task, class: class, run: run, outputKeys: keys})
	tm.mu.Unlock()
	task.persistNow()
	tm.dispatchStudioJobs()
	return nil
}

// timeNow is a variable so scheduler timing can be deterministic in tests.
var timeNow = time.Now

func (tm *TaskManager) dispatchStudioJobs() {
	var launch []*studioQueueItem
	tm.mu.Lock()
	resourceOK, _ := tm.studioResourcesAvailableLocked()
	for tm.studioRunning < tm.studioMaxRunning {
		index := -1
		for i, item := range tm.studioQueue {
			if (item.class != StudioJobHeavy || tm.studioHeavyRunning < tm.studioMaxHeavy) &&
				(item.class != StudioJobHeavy || resourceOK) {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		item := tm.studioQueue[index]
		tm.studioQueue = append(tm.studioQueue[:index], tm.studioQueue[index+1:]...)
		tm.studioRunning++
		if item.class == StudioJobHeavy {
			tm.studioHeavyRunning++
		}
		launch = append(launch, item)
	}
	if len(tm.studioQueue) > 0 && !resourceOK && !tm.studioRetryPending {
		tm.studioRetryPending = true
		time.AfterFunc(5*time.Second, func() {
			tm.mu.Lock()
			tm.studioRetryPending = false
			tm.mu.Unlock()
			tm.dispatchStudioJobs()
		})
	}
	tm.mu.Unlock()
	for _, item := range launch {
		go tm.runScheduledStudioJob(item)
	}
}

func (tm *TaskManager) studioResourcesAvailableLocked() (bool, string) {
	if tm.studioResources == nil {
		return true, ""
	}
	snapshot := tm.studioResources()
	ramReserve := studioResourceReserve("LLAMA_STUDIO_MIN_FREE_RAM_GB", 2)
	if snapshot.RAMKnown && snapshot.FreeRAMBytes < ramReserve {
		return false, fmt.Sprintf("waiting for %s free RAM", formatStudioBytes(ramReserve))
	}
	vramReserve := studioResourceReserve("LLAMA_STUDIO_MIN_FREE_VRAM_GB", 0)
	if vramReserve > 0 && snapshot.VRAMKnown && snapshot.FreeVRAMBytes < vramReserve {
		return false, fmt.Sprintf("waiting for %s free VRAM", formatStudioBytes(vramReserve))
	}
	return true, ""
}

func (tm *TaskManager) runScheduledStudioJob(item *studioQueueItem) {
	defer func() {
		if recovered := recover(); recovered != nil {
			item.task.UpdateProgress(TaskFailed, fmt.Sprintf("operation panicked: %v", recovered), 0)
		}
		if !item.task.IsTerminal() {
			item.task.UpdateProgress(TaskFailed, "operation ended without a terminal state", 0)
		}
		tm.mu.Lock()
		tm.studioRunning--
		if item.class == StudioJobHeavy {
			tm.studioHeavyRunning--
		}
		for _, key := range item.outputKeys {
			delete(tm.studioOutputs, key)
		}
		tm.mu.Unlock()
		tm.dispatchStudioJobs()
	}()
	item.task.UpdateProgress(TaskRunning, "Starting queued operation...", 0)
	item.run()
}

func (tm *TaskManager) cancelQueuedStudioTask(task *Task) bool {
	tm.mu.Lock()
	for i, item := range tm.studioQueue {
		if item.task == task {
			tm.studioQueue = append(tm.studioQueue[:i], tm.studioQueue[i+1:]...)
			for _, key := range item.outputKeys {
				delete(tm.studioOutputs, key)
			}
			tm.mu.Unlock()
			task.Cancel()
			tm.dispatchStudioJobs()
			return true
		}
	}
	tm.mu.Unlock()
	return false
}

func (tm *TaskManager) StudioSchedulerStatus() StudioSchedulerStatus {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	available, reason := tm.studioResourcesAvailableLocked()
	blocked := 0
	if !available {
		for _, item := range tm.studioQueue {
			if item.class == StudioJobHeavy {
				blocked++
			}
		}
	}
	return StudioSchedulerStatus{
		MaxRunning: tm.studioMaxRunning, MaxHeavy: tm.studioMaxHeavy,
		Running: tm.studioRunning, HeavyRunning: tm.studioHeavyRunning, Queued: len(tm.studioQueue),
		Blocked: blocked, BlockedReason: reason,
	}
}
