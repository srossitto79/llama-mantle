package tuner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Projection is llama.cpp's own account of the device memory a launch would
// use on the first GPU, printed before any weights are read.
type Projection struct {
	FreeMiB    int64 `json:"freeMiB"`    // free device memory as CUDA reports it
	ModelMiB   int64 `json:"modelMiB"`   // weights on the device
	ContextMiB int64 `json:"contextMiB"` // KV, recurrent state and the MoE cache
	ComputeMiB int64 `json:"computeMiB"` // compute buffers
	TotalMiB   int64 `json:"totalMiB"`   // model + context + compute
}

// MemoryProjector asks a backend how much device memory a launch needs.
// It is the boundary that starts processes; tests use a fake.
type MemoryProjector interface {
	Project(ctx context.Context, bin string, args []string) (Projection, error)
}

// projectTimeout bounds one projection. The breakdown is printed after the
// GGUF headers are read (about 0.5 s for a 9-shard 280B model).
const projectTimeout = 60 * time.Second

// NewMemoryProjector returns the projector that runs llama-server.
func NewMemoryProjector() MemoryProjector { return execProjector{} }

type execProjector struct{}

// breakdownRe matches llama.cpp's memory breakdown row for the first GPU:
// "| - CUDA0 (RTX 3090) | 24575 = 23300 + ( 12897 = 7920 + 2217 + 2759) + -11621 |"
// The columns are total = free + (self = model + context + compute) + unaccounted.
var breakdownRe = regexp.MustCompile(`- CUDA0 \(.*?\)\s*\|\s*(\d+)\s*=\s*(\d+)\s*\+\s*\(\s*(\d+)\s*=\s*(\d+)\s*\+\s*(\d+)\s*\+\s*(\d+)\s*\)`)

// parseBreakdown reads one log line; ok is false when it is not the row.
func parseBreakdown(line string) (Projection, bool) {
	m := breakdownRe.FindStringSubmatch(line)
	if m == nil {
		return Projection{}, false
	}
	n := func(i int) int64 { v, _ := strconv.ParseInt(m[i], 10, 64); return v }
	return Projection{FreeMiB: n(2), TotalMiB: n(3), ModelMiB: n(4), ContextMiB: n(5), ComputeMiB: n(6)}, true
}

func (execProjector) Project(ctx context.Context, bin string, args []string) (Projection, error) {
	port, err := freePort()
	if err != nil {
		return Projection{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, projectTimeout)
	defer cancel()
	// The breakdown is logged at info level, which needs verbosity 4.
	full := append(append([]string{}, args...), "--port", strconv.Itoa(port), "--host", "127.0.0.1", "-lv", "4")
	cmd := exec.CommandContext(ctx, bin, full...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return Projection{}, fmt.Errorf("start %s: %w", bin, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); pw.Close() }()

	var tail []string
	found, ok := Projection{}, false
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if found, ok = parseBreakdown(line); ok {
			break
		}
		if len(tail) == 8 {
			tail = tail[1:]
		}
		tail = append(tail, line)
	}
	// Stop the server either way; it would go on to load the weights.
	_ = cmd.Process.Kill()
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	<-done
	if ok {
		return found, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Projection{}, fmt.Errorf("no memory projection within %s", projectTimeout)
	}
	return Projection{}, fmt.Errorf("no memory projection: %s", strings.Join(tail, " | "))
}

// freePort returns a loopback port that was free a moment ago.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
