package tuner

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const smiLine = "NVIDIA GeForce RTX 3090, 24576, 23192, 1131, 4, 16"

func TestTuner_ParseNvidiaSMI(t *testing.T) {
	gpus, err := parseNvidiaSMI(smiLine + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 1 {
		t.Fatalf("gpus = %d", len(gpus))
	}
	g := gpus[0]
	want := GPUInfo{Name: "NVIDIA GeForce RTX 3090", TotalMiB: 24576, UsedMiB: 23192, FreeMiB: 1131, PCIeGen: 4, PCIeWidth: 16}
	if g != want {
		t.Fatalf("got %+v, want %+v", g, want)
	}
}

func TestTuner_ParseNvidiaSMIErrors(t *testing.T) {
	if _, err := parseNvidiaSMI("\n  \n"); err == nil {
		t.Fatal("expected an error for empty output")
	}
	if _, err := parseNvidiaSMI("RTX, 1, 2\n"); err == nil {
		t.Fatal("expected an error for a short line")
	}
}

func TestTuner_ParseMemTotalMiB(t *testing.T) {
	info := "MemTotal:       65536000 kB\nMemFree:        1000 kB\n"
	if got := parseMemTotalMiB(info); got != 64000 {
		t.Fatalf("got %d, want 64000", got)
	}
	if got := parseMemTotalMiB("nothing here"); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func fakeProbe(smi string) SystemProbe {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "nvidia-smi" {
			return nil, errors.New("unexpected command " + name)
		}
		return []byte(smi), nil
	}
	read := func(path string) ([]byte, error) {
		switch path {
		case "/proc/meminfo":
			return []byte("MemTotal:       65536000 kB\n"), nil
		case "/proc/cpuinfo":
			return []byte("flags\t\t: fpu sse2 avx2 avx512f avx512bw\n"), nil
		}
		return nil, errors.New("not found")
	}
	return NewSystemProbe(run, read)
}

func TestTuner_ProbeWarnsWhenGPUBusy(t *testing.T) {
	info, err := fakeProbe(smiLine).Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "23192 MiB in use") {
		t.Fatalf("warnings = %v", info.Warnings)
	}
	if info.RAMMiB != 64000 {
		t.Fatalf("RAMMiB = %d", info.RAMMiB)
	}
	if !info.AVX512 {
		t.Fatal("AVX512 = false")
	}
	if info.Threads < 1 {
		t.Fatalf("Threads = %d", info.Threads)
	}
	if info.Hash == "" {
		t.Fatal("empty hash")
	}
}

func TestTuner_ProbeNoWarningWhenIdle(t *testing.T) {
	info, err := fakeProbe("NVIDIA GeForce RTX 3090, 24576, 1131, 23192, 4, 16").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Warnings) != 0 {
		t.Fatalf("warnings = %v", info.Warnings)
	}
}

func TestTuner_ProbeHashIgnoresUsage(t *testing.T) {
	a, err := fakeProbe("NVIDIA GeForce RTX 3090, 24576, 1131, 23192, 4, 16").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := fakeProbe("NVIDIA GeForce RTX 3090, 24576, 9000, 15000, 4, 16").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("hash changed with usage: %s vs %s", a.Hash, b.Hash)
	}
	c, err := fakeProbe("NVIDIA GeForce RTX 4090, 24576, 1131, 23192, 4, 16").Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash == c.Hash {
		t.Fatal("hash did not change with the GPU model")
	}
}

func TestTuner_ProbeRunnerError(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("not installed")
	}
	if _, err := NewSystemProbe(run, func(string) ([]byte, error) { return nil, errors.New("x") }).Probe(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}
