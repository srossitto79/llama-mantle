package mantle

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskManager_StudioSchedulerLimitsHeavyJobs(t *testing.T) {
	tm := NewTaskManager(nil)
	tm.studioMaxRunning = 2
	tm.studioMaxHeavy = 1

	releaseFirst := make(chan struct{})
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	lightStarted := make(chan struct{})
	first := tm.CreateTask("studio", "", "", "")
	second := tm.CreateTask("studio", "", "", "")
	light := tm.CreateTask("studio", "", "", "")

	tm.enqueueStudioTask(first, StudioJobHeavy, func() {
		close(firstStarted)
		<-releaseFirst
		first.UpdateProgress(TaskCompleted, "done", 100)
	})
	<-firstStarted
	tm.enqueueStudioTask(second, StudioJobHeavy, func() {
		close(secondStarted)
		second.UpdateProgress(TaskCompleted, "done", 100)
	})
	tm.enqueueStudioTask(light, StudioJobLight, func() {
		close(lightStarted)
		light.UpdateProgress(TaskCompleted, "done", 100)
	})

	select {
	case <-lightStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("light job did not use available global capacity")
	}
	select {
	case <-secondStarted:
		t.Fatal("second heavy job started before heavy capacity was released")
	default:
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("second heavy job did not start after capacity was released")
	}
}

func TestTaskManager_StudioSchedulerReservesOutputs(t *testing.T) {
	tm := NewTaskManager(nil)
	tm.studioMaxRunning = 1

	release := make(chan struct{})
	started := make(chan struct{})
	output := filepath.Join(t.TempDir(), "result.gguf")
	first := tm.CreateTask("studio", "", "", "")
	second := tm.CreateTask("studio", "", "", "")
	if err := tm.enqueueStudioTaskWithOutputs(first, StudioJobHeavy, []string{output}, func() {
		close(started)
		<-release
		first.UpdateProgress(TaskCompleted, "done", 100)
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := tm.enqueueStudioTaskWithOutputs(second, StudioJobHeavy, []string{output}, func() {}); err == nil {
		t.Fatal("duplicate output reservation succeeded")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := tm.enqueueStudioTaskWithOutputs(second, StudioJobHeavy, []string{output}, func() {
			second.UpdateProgress(TaskCompleted, "done", 100)
		}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("output reservation was not released")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTaskManager_StudioSchedulerWaitsForFreeRAM(t *testing.T) {
	t.Setenv("LLAMA_STUDIO_MIN_FREE_RAM_GB", "2")
	tm := NewTaskManager(nil)
	tm.SetStudioResourceProvider(func() StudioResourceSnapshot {
		return StudioResourceSnapshot{FreeRAMBytes: gib, RAMKnown: true}
	})
	task := tm.CreateTask("studio", "", "", "")
	var ran atomic.Bool
	tm.enqueueStudioTask(task, StudioJobHeavy, func() {
		ran.Store(true)
		task.UpdateProgress(TaskCompleted, "done", 100)
	})
	time.Sleep(25 * time.Millisecond)
	if ran.Load() {
		t.Fatal("heavy job ran without the configured free RAM reserve")
	}
	status := tm.StudioSchedulerStatus()
	if status.Blocked != 1 || status.BlockedReason == "" {
		t.Fatalf("unexpected blocked status: %#v", status)
	}
	if !tm.CancelTask(task.ID) {
		t.Fatal("CancelTask() returned false")
	}
}

func TestTaskManager_CancelQueuedStudioTask(t *testing.T) {
	tm := NewTaskManager(nil)
	tm.studioMaxRunning = 1
	tm.studioMaxHeavy = 1

	releaseFirst := make(chan struct{})
	firstStarted := make(chan struct{})
	first := tm.CreateTask("studio", "", "", "")
	queued := tm.CreateTask("studio", "", "", "")
	var queuedRan atomic.Bool
	tm.enqueueStudioTask(first, StudioJobHeavy, func() {
		close(firstStarted)
		<-releaseFirst
		first.UpdateProgress(TaskCompleted, "done", 100)
	})
	<-firstStarted
	tm.enqueueStudioTask(queued, StudioJobHeavy, func() {
		queuedRan.Store(true)
		queued.UpdateProgress(TaskCompleted, "done", 100)
	})

	if !tm.CancelTask(queued.ID) {
		t.Fatal("CancelTask() returned false")
	}
	close(releaseFirst)
	time.Sleep(50 * time.Millisecond)
	if queuedRan.Load() {
		t.Fatal("cancelled queued job was executed")
	}
	queued.mu.Lock()
	queuedState := queued.State
	queued.mu.Unlock()
	if queuedState != TaskCancelled {
		t.Fatalf("queued state = %q, want cancelled", queuedState)
	}
}
