package mantle

import (
	"os"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

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
