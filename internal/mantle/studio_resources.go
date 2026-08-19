package mantle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

const gib = int64(1024 * 1024 * 1024)

var studioDiskUsage = disk.Usage

func studioDiskReserveBytes() int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("LLAMA_STUDIO_DISK_RESERVE_GB")), 10, 64)
	if err != nil || value < 0 {
		return gib
	}
	return value * gib
}

func ensureStudioDiskSpace(outputPath string, estimatedOutputBytes int64) error {
	probe := outputPath
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		next := filepath.Dir(probe)
		if next == probe {
			return fmt.Errorf("cannot determine output filesystem")
		}
		probe = next
	}
	usage, err := studioDiskUsage(probe)
	if err != nil {
		return fmt.Errorf("check output disk space: %w", err)
	}
	required := estimatedOutputBytes + studioDiskReserveBytes()
	if int64(usage.Free) < required {
		return fmt.Errorf("insufficient output disk space: need %s including reserve, have %s free",
			formatStudioBytes(required), formatStudioBytes(int64(usage.Free)))
	}
	return nil
}

func estimateStudioOutput(sourcePath string, multiplier float64, minimum int64) int64 {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return minimum
	}
	estimate := int64(float64(info.Size()) * multiplier)
	if estimate < minimum {
		return minimum
	}
	return estimate
}

func formatStudioBytes(value int64) string {
	if value >= gib {
		return fmt.Sprintf("%.1f GiB", float64(value)/float64(gib))
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/(1024*1024))
}
