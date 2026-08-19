package mantle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestTaskManager_StudioPreflightRecommendsHardwareFit(t *testing.T) {
	modelsDir := t.TempDir()
	model := filepath.Join(modelsDir, "model.gguf")
	if err := os.WriteFile(model, make([]byte, 10*1024*1024), 0600); err != nil {
		t.Fatal(err)
	}
	original := studioDiskUsage
	studioDiskUsage = func(string) (*disk.UsageStat, error) { return &disk.UsageStat{Free: uint64(20 * gib)}, nil }
	t.Cleanup(func() { studioDiskUsage = original })
	tm := NewTaskManager(nil)
	tm.SetStudioResourceProvider(func() StudioResourceSnapshot {
		return StudioResourceSnapshot{TotalRAMBytes: 16 * gib, FreeRAMBytes: 12 * gib, TotalVRAMBytes: 8 * gib, FreeVRAMBytes: 6 * gib, GPUCount: 1, RAMKnown: true, VRAMKnown: true}
	})
	report, err := tm.StudioPreflight(modelsDir, StudioPreflightRequest{Operation: "quantize", Model: "model.gguf"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Fits || report.Hardware.GPUCount != 1 || report.Recommendations["quantizationType"] != "Q8_0" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestTaskManager_StudioPreflightReportsInsufficientMemory(t *testing.T) {
	modelsDir := t.TempDir()
	model := filepath.Join(modelsDir, "model.gguf")
	if err := os.WriteFile(model, make([]byte, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	tm := NewTaskManager(nil)
	tm.SetStudioResourceProvider(func() StudioResourceSnapshot { return StudioResourceSnapshot{FreeRAMBytes: 1000, RAMKnown: true} })
	report, err := tm.StudioPreflight(modelsDir, StudioPreflightRequest{Operation: "train-qlora", Model: "model.gguf"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Fits || len(report.Warnings) == 0 {
		t.Fatalf("expected a failed fit report: %#v", report)
	}
}

func TestEnsureStudioDiskSpace_RejectsInsufficientCapacity(t *testing.T) {
	original := studioDiskUsage
	studioDiskUsage = func(string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Free: 100}, nil
	}
	t.Cleanup(func() { studioDiskUsage = original })
	t.Setenv("LLAMA_STUDIO_DISK_RESERVE_GB", "0")

	if err := ensureStudioDiskSpace(t.TempDir(), 101); err == nil {
		t.Fatal("expected insufficient disk space error")
	}
}

func TestEnsureStudioDiskSpace_AllowsEstimatedOutputAndReserve(t *testing.T) {
	original := studioDiskUsage
	studioDiskUsage = func(string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Free: uint64(3 * gib)}, nil
	}
	t.Cleanup(func() { studioDiskUsage = original })
	t.Setenv("LLAMA_STUDIO_DISK_RESERVE_GB", "1")

	output := t.TempDir() + string(os.PathSeparator) + "nested" + string(os.PathSeparator) + "model.gguf"
	if err := ensureStudioDiskSpace(output, gib); err != nil {
		t.Fatalf("ensureStudioDiskSpace() error = %v", err)
	}
}
