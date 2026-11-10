package tuner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// CommandRunner runs a command and returns its stdout.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// FileReader reads a file; os.ReadFile in production.
type FileReader func(path string) ([]byte, error)

// gpuBusyFraction is the share of VRAM in use above which the probe warns
// that free VRAM does not reflect an idle GPU (another model is resident).
const gpuBusyFraction = 0.25

// NewSystemProbe returns the probe for this station. Nil arguments use
// exec and os.ReadFile.
func NewSystemProbe(run CommandRunner, read FileReader) SystemProbe {
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		}
	}
	if read == nil {
		read = os.ReadFile
	}
	return &stationProbe{run: run, read: read}
}

type stationProbe struct {
	run  CommandRunner
	read FileReader
}

func (p *stationProbe) Probe(ctx context.Context) (SystemInfo, error) {
	out, err := p.run(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total,memory.used,memory.free,pcie.link.gen.max,pcie.link.width.max",
		"--format=csv,noheader,nounits")
	if err != nil {
		return SystemInfo{}, fmt.Errorf("nvidia-smi: %w", err)
	}
	gpus, err := parseNvidiaSMI(string(out))
	if err != nil {
		return SystemInfo{}, err
	}
	info := SystemInfo{GPUs: gpus, Threads: runtime.NumCPU()}
	if b, err := p.read("/proc/meminfo"); err == nil {
		info.RAMMiB = parseMemTotalMiB(string(b))
	}
	if b, err := p.read("/proc/cpuinfo"); err == nil {
		info.AVX512 = strings.Contains(string(b), " avx512f")
	}
	for _, g := range gpus {
		if g.TotalMiB > 0 && float64(g.UsedMiB) > gpuBusyFraction*float64(g.TotalMiB) {
			info.Warnings = append(info.Warnings, fmt.Sprintf("%s: %d MiB in use", g.Name, g.UsedMiB))
		}
	}
	info.Hash = probeHash(info)
	return info, nil
}

func parseNvidiaSMI(out string) ([]GPUInfo, error) {
	var gpus []GPUInfo
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 6 {
			return nil, fmt.Errorf("nvidia-smi: unexpected line %q", line)
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		n := func(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
		gpus = append(gpus, GPUInfo{
			Name: f[0], TotalMiB: n(f[1]), UsedMiB: n(f[2]), FreeMiB: n(f[3]),
			PCIeGen: int(n(f[4])), PCIeWidth: int(n(f[5])),
		})
	}
	if len(gpus) == 0 {
		return nil, fmt.Errorf("nvidia-smi: no GPU")
	}
	return gpus, nil
}

func parseMemTotalMiB(meminfo string) int64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				return kb / 1024
			}
		}
	}
	return 0
}

// probeHash identifies the hardware, not its momentary state: free/used VRAM
// are left out so the hash is stable across runs.
func probeHash(s SystemInfo) string {
	h := sha256.New()
	for _, g := range s.GPUs {
		fmt.Fprintf(h, "%s|%d|%d|%d;", g.Name, g.TotalMiB, g.PCIeGen, g.PCIeWidth)
	}
	fmt.Fprintf(h, "%d|%d|%t", s.RAMMiB/1024, s.Threads, s.AVX512)
	return hex.EncodeToString(h.Sum(nil))[:16]
}
