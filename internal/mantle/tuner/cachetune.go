package tuner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CacheTune is moe-cache-tune.py's pick for one MoE-cache candidate.
type CacheTune struct {
	CacheMiB     int     `json:"cacheMiB"`
	CPUShare     float64 `json:"cpuShare"`
	TokensPerSec float64 `json:"tokensPerSec"`           // predicted generation speed, warm cache
	ShareZeroTPS float64 `json:"shareZeroTps,omitempty"` // the same cache with CPU share 0
	AllFit       bool    `json:"allFit,omitempty"`       // every expert fits in VRAM: drop -cmoe
	Report       string  `json:"report"`                 // the script's tables
}

// CacheTuneJob is one run of the script.
type CacheTuneJob struct {
	Script string   // tools/moe-cache-tune.py of the candidate's backend
	OutDir string   // run directory, reused while Cmd is unchanged
	MaxMiB int      // largest cache to consider: the size the sizer fitted
	Cmd    []string // llama-server and its arguments, without port, cache size or CPU share
}

// CacheTuner runs llama.cpp's moe-cache-tune.py. It is the boundary that
// starts processes; tests use a fake.
type CacheTuner interface {
	Tune(ctx context.Context, job CacheTuneJob, progress func(line string)) (CacheTune, error)
}

// cacheTuneStopGrace is how long a cancelled script gets to stop its
// llama-server before both are killed.
const cacheTuneStopGrace = 2 * time.Minute

// NewCacheTuner returns the tuner that runs the script with python3.
func NewCacheTuner() CacheTuner { return execCacheTuner{python: "python3"} }

type execCacheTuner struct{ python string }

func (e execCacheTuner) Tune(ctx context.Context, job CacheTuneJob, progress func(string)) (CacheTune, error) {
	args := []string{job.Script, "--out", job.OutDir}
	if job.MaxMiB > 0 {
		args = append(args, "--max-mib", strconv.Itoa(job.MaxMiB))
	}
	args = append(append(args, "--"), job.Cmd...)
	cmd := exec.CommandContext(ctx, e.python, args...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	// SIGINT lets the script stop its llama-server in its finally block; a
	// kill would leave the server running with the GPU and pinned RAM.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = cacheTuneStopGrace
	var out bytes.Buffer
	cmd.Stdout = &out
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return CacheTune{}, err
	}
	if err := cmd.Start(); err != nil {
		return CacheTune{}, fmt.Errorf("start moe-cache-tune: %w", err)
	}
	// Progress and errors go to stderr: one line per finished run, and
	// sys.exit messages.
	var last string
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		last = line
		if progress != nil {
			progress(line)
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return CacheTune{}, ctx.Err()
	}
	if err != nil {
		if last == "" {
			last = err.Error()
		}
		return CacheTune{}, fmt.Errorf("moe-cache-tune: %s", last)
	}
	return parseCacheTune(out.String())
}

// recommendRe matches the script's last line without --markdown, e.g.
// "recommended: --moe-cache-mib 9472 --moe-cache-cpu-share 0.5 (12.3 t/s, 10.1 with share 0)".
var recommendRe = regexp.MustCompile(`recommended: --moe-cache-mib (\d+) --moe-cache-cpu-share ([0-9.]+) \(([0-9.]+) t/s(?:, ([0-9.]+) with share 0)?\)`)

func parseCacheTune(out string) (CacheTune, error) {
	r := CacheTune{Report: strings.TrimSpace(out)}
	if strings.Contains(out, "all experts fit on ") {
		r.AllFit = true
		return r, nil
	}
	m := recommendRe.FindStringSubmatch(out)
	if m == nil {
		return r, errors.New("moe-cache-tune printed no recommendation")
	}
	r.CacheMiB, _ = strconv.Atoi(m[1])
	r.CPUShare, _ = strconv.ParseFloat(m[2], 64)
	r.TokensPerSec, _ = strconv.ParseFloat(m[3], 64)
	if m[4] != "" {
		r.ShareZeroTPS, _ = strconv.ParseFloat(m[4], 64)
	} else {
		r.ShareZeroTPS = r.TokensPerSec
	}
	return r, nil
}

// cacheTuneJob builds the script run for a candidate. The script sets the
// cache size and share itself, so they are left out of the command; that also
// keeps the run directory reusable when a later analysis sizes the cache
// differently.
func cacheTuneJob(model ModelInfo, c Candidate, req Request) CacheTuneJob {
	cmd := append([]string{c.Binary}, withoutFlags(launchArgs(model, c), "--moe-cache-mib", "--moe-cache-cpu-share")...)
	sum := sha256.Sum256([]byte(strings.Join(cmd, "\x00")))
	dir := filepath.Dir(c.Binary)
	return CacheTuneJob{
		Script: filepath.Join(dir, "tools", "moe-cache-tune.py"),
		OutDir: filepath.Join(dir, "tune", fmt.Sprintf("%s-%dk-%s", entryPrefix(req), c.Tier/1024, hex.EncodeToString(sum[:4]))),
		MaxMiB: argInt(c, "--moe-cache-mib"),
		Cmd:    cmd,
	}
}

// withoutFlags drops each named flag and its value.
func withoutFlags(args []string, flags ...string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		drop := false
		for _, f := range flags {
			if args[i] == f {
				drop = true
				break
			}
		}
		if drop {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// setArgValue replaces the value of flag in c.Args, or appends the flag.
func setArgValue(c *Candidate, flag, v string) {
	for i, a := range c.Args {
		if a == flag && i+1 < len(c.Args) {
			c.Args[i+1] = v
			return
		}
	}
	c.Args = append(c.Args, flag, v)
}

// Candidate returns the candidate with the given id.
func (a Analysis) Candidate(id string) (Candidate, bool) {
	for _, tp := range a.Tiers {
		for _, c := range tp.Candidates {
			if c.ID == id {
				return c, true
			}
		}
	}
	return Candidate{}, false
}

// CanTuneCache reports whether moe-cache-tune can refine c.
func CanTuneCache(c Candidate) bool {
	return c.Tunable && c.Blocked == "" && c.Layout == LayoutMoECache && c.Binary != ""
}

func (t *tuner) TuneCache(ctx context.Context, a Analysis, req Request, id string, progress func(string)) (Candidate, error) {
	c, ok := a.Candidate(id)
	if !ok {
		return Candidate{}, fmt.Errorf("no candidate %s", id)
	}
	if !CanTuneCache(c) {
		return Candidate{}, fmt.Errorf("candidate %s has no tunable MoE cache", id)
	}
	if t.d.CacheTuner == nil {
		return Candidate{}, errors.New("no cache tuner")
	}
	// The script sizes its cache from free VRAM and times every run, so a
	// resident model would skew both.
	sys, err := t.d.Probe.Probe(ctx)
	if err != nil {
		return Candidate{}, err
	}
	if len(sys.GPUs) > 0 && sys.GPUs[0].UsedMiB > busyMiB {
		return Candidate{}, fmt.Errorf("GPU in use (%d MiB): unload models and try again", sys.GPUs[0].UsedMiB)
	}

	r, err := t.d.CacheTuner.Tune(ctx, cacheTuneJob(a.Model, c, req), progress)
	if err != nil {
		return Candidate{}, err
	}
	c.Args = append([]string(nil), c.Args...) // the analysis shares the slice
	c.CacheTune = &r
	if r.AllFit {
		return c, nil
	}
	setArg(&c, "--moe-cache-mib", r.CacheMiB)
	setArgValue(&c, "--moe-cache-cpu-share", strconv.FormatFloat(r.CPUShare, 'g', -1, 64))
	if t.d.Projector != nil {
		p, err := t.d.Projector.Project(ctx, c.Binary, launchArgs(a.Model, c))
		if err != nil {
			return Candidate{}, err
		}
		if avail := p.FreeMiB - fitTargetMiB; p.TotalMiB > avail {
			return Candidate{}, fmt.Errorf("tuned launch needs %d MiB, %d MiB available", p.TotalMiB, avail)
		}
		c.Memory = &p
	}
	c.layoutReason = fmt.Sprintf("Experts in host RAM, %d MiB cache, CPU share %g", r.CacheMiB, r.CPUShare)
	c.Reason = joinReason(c.layoutReason, c.draftReason)
	ch, err := t.d.Proposer.Propose(a.Model, c, req)
	if err != nil {
		return Candidate{}, err
	}
	c.Entry = &ch
	return c, nil
}
