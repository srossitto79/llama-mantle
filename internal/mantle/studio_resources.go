package mantle

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

type StudioPreflightRequest struct {
	Operation string `json:"operation"`
	Model     string `json:"model,omitempty"`
	Dataset   string `json:"dataset,omitempty"`
}

type StudioHardwareProfile struct {
	CPUThreads     int   `json:"cpuThreads"`
	GPUCount       int   `json:"gpuCount"`
	TotalRAMBytes  int64 `json:"totalRamBytes,omitempty"`
	FreeRAMBytes   int64 `json:"freeRamBytes,omitempty"`
	TotalVRAMBytes int64 `json:"totalVramBytes,omitempty"`
	FreeVRAMBytes  int64 `json:"freeVramBytes,omitempty"`
	DiskFreeBytes  int64 `json:"diskFreeBytes,omitempty"`
	RAMKnown       bool  `json:"ramKnown"`
	VRAMKnown      bool  `json:"vramKnown"`
}

type StudioPreflightReport struct {
	Operation            string                `json:"operation"`
	Model                string                `json:"model,omitempty"`
	ModelBytes           int64                 `json:"modelBytes,omitempty"`
	DatasetBytes         int64                 `json:"datasetBytes,omitempty"`
	EstimatedOutputBytes int64                 `json:"estimatedOutputBytes,omitempty"`
	EstimatedRAMBytes    int64                 `json:"estimatedRamBytes,omitempty"`
	EstimatedVRAMBytes   int64                 `json:"estimatedVramBytes,omitempty"`
	Fits                 bool                  `json:"fits"`
	Hardware             StudioHardwareProfile `json:"hardware"`
	Recommendations      map[string]any        `json:"recommendations"`
	Warnings             []string              `json:"warnings,omitempty"`
}

func (tm *TaskManager) StudioPreflight(modelsDir string, req StudioPreflightRequest) (*StudioPreflightReport, error) {
	req.Operation = strings.TrimSpace(req.Operation)
	allowed := map[string]bool{"quantize": true, "train-qlora": true, "merge": true, "prune": true, "evaluate": true, "serve": true}
	if !allowed[req.Operation] {
		return nil, fmt.Errorf("unsupported preflight operation %q", req.Operation)
	}
	report := &StudioPreflightReport{Operation: req.Operation, Fits: true, Recommendations: map[string]any{}}
	if req.Model != "" {
		path, clean, err := resolveStudioInput(modelsDir, req.Model, ".gguf")
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		report.Model = clean
		report.ModelBytes = info.Size()
	}
	if req.Dataset != "" {
		path, _, err := resolveStudioInput(modelsDir, req.Dataset, "")
		if err != nil {
			return nil, err
		}
		info, _ := os.Stat(path)
		report.DatasetBytes = info.Size()
	}
	tm.mu.Lock()
	provider := tm.studioResources
	tm.mu.Unlock()
	var snapshot StudioResourceSnapshot
	if provider != nil {
		snapshot = provider()
	}
	report.Hardware = StudioHardwareProfile{CPUThreads: runtime.NumCPU(), GPUCount: snapshot.GPUCount, TotalRAMBytes: snapshot.TotalRAMBytes, FreeRAMBytes: snapshot.FreeRAMBytes, TotalVRAMBytes: snapshot.TotalVRAMBytes, FreeVRAMBytes: snapshot.FreeVRAMBytes, RAMKnown: snapshot.RAMKnown, VRAMKnown: snapshot.VRAMKnown}
	if usage, err := studioDiskUsage(modelsDir); err == nil {
		report.Hardware.DiskFreeBytes = int64(usage.Free)
	}
	modelBytes := report.ModelBytes
	switch req.Operation {
	case "quantize":
		typeName, ratio := recommendStudioQuantization(modelBytes, snapshot)
		report.EstimatedOutputBytes = int64(float64(modelBytes) * ratio)
		report.EstimatedRAMBytes = modelBytes + report.EstimatedOutputBytes
		report.Recommendations["quantizationType"] = typeName
		report.Recommendations["threads"] = max(1, runtime.NumCPU()-1)
	case "train-qlora":
		report.EstimatedOutputBytes = max(256*1024*1024, modelBytes/8)
		report.EstimatedRAMBytes = modelBytes * 2
		report.EstimatedVRAMBytes = modelBytes + modelBytes/3
		report.Recommendations["rank"] = 16
		report.Recommendations["batchSize"] = 512
		report.Recommendations["gradientCheckpointing"] = 1
	case "merge", "prune":
		report.EstimatedOutputBytes = modelBytes
		report.EstimatedRAMBytes = modelBytes * 2
	case "evaluate", "serve":
		report.EstimatedRAMBytes = modelBytes + modelBytes/5
		report.EstimatedVRAMBytes = report.EstimatedRAMBytes
		report.Recommendations["gpuLayers"] = -1
		report.Recommendations["contextSize"] = 4096
	}
	if report.EstimatedOutputBytes > 0 && report.Hardware.DiskFreeBytes > 0 && report.Hardware.DiskFreeBytes < report.EstimatedOutputBytes+studioDiskReserveBytes() {
		report.Fits = false
		report.Warnings = append(report.Warnings, "Not enough free disk space including the configured reserve.")
	}
	if report.EstimatedRAMBytes > 0 && snapshot.RAMKnown && snapshot.FreeRAMBytes < report.EstimatedRAMBytes {
		report.Fits = false
		report.Warnings = append(report.Warnings, "Estimated peak memory exceeds currently available RAM.")
	}
	if report.EstimatedVRAMBytes > 0 && snapshot.VRAMKnown && snapshot.FreeVRAMBytes < report.EstimatedVRAMBytes {
		report.Warnings = append(report.Warnings, "The model may not fully fit in current VRAM; partial GPU offload or smaller settings may be required.")
		if !snapshot.RAMKnown || snapshot.FreeRAMBytes < report.EstimatedRAMBytes {
			report.Fits = false
		}
	}
	if !snapshot.RAMKnown {
		report.Warnings = append(report.Warnings, "RAM telemetry is unavailable, so memory fit cannot be guaranteed.")
	}
	return report, nil
}

func recommendStudioQuantization(modelBytes int64, snapshot StudioResourceSnapshot) (string, float64) {
	budget := int64(0)
	if snapshot.VRAMKnown {
		budget = snapshot.FreeVRAMBytes * 80 / 100
	} else if snapshot.RAMKnown {
		budget = snapshot.FreeRAMBytes * 65 / 100
	}
	choices := []struct {
		name  string
		ratio float64
	}{{"Q8_0", .55}, {"Q6_K", .42}, {"Q5_K_M", .36}, {"Q4_K_M", .30}, {"IQ3_M", .24}, {"IQ2_M", .18}}
	if modelBytes <= 0 || budget <= 0 {
		return "Q4_K_M", .30
	}
	for _, choice := range choices {
		if int64(float64(modelBytes)*choice.ratio) <= budget {
			return choice.name, choice.ratio
		}
	}
	return "IQ2_M", .18
}

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
