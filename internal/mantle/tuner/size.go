package tuner

import (
	"context"
	"fmt"
	"strings"
)

const (
	// fitTargetMiB is the device memory llama.cpp keeps free (its default
	// --fit-target). Sizing to free - target leaves the same headroom
	// llama.cpp would leave for itself.
	fitTargetMiB = 1024
	// busyMiB: GPU use above this means a model is resident, and free memory
	// would size entries for what is left beside it.
	busyMiB = 2048
	// maxRefine bounds the projections spent correcting one candidate.
	maxRefine = 3
)

// sizer sets each candidate's memory-dependent flags from llama.cpp's
// projection of the exact launch, instead of estimating buffers.
type sizer struct {
	p     MemoryProjector
	model ModelInfo
	seen  map[string]projected // by launch, so draft variants share a run
}

type projected struct {
	p   Projection
	err error
}

func newSizer(p MemoryProjector, model ModelInfo) *sizer {
	return &sizer{p: p, model: model, seen: map[string]projected{}}
}

// memoryKey drops the flags that do not change device memory: n-gram
// drafting works on host-side token history, and thread count is CPU only.
func memoryKey(c Candidate, args []string) string {
	var b strings.Builder
	b.WriteString(c.Binary)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "--spec-type" && i+1 < len(args) && strings.HasPrefix(args[i+1], "ngram")) || a == "--threads" {
			i++
			continue
		}
		b.WriteByte(' ')
		b.WriteString(a)
	}
	return b.String()
}

func (s *sizer) project(ctx context.Context, c Candidate) (Projection, error) {
	args := launchArgs(s.model, c)
	k := memoryKey(c, args)
	if r, ok := s.seen[k]; ok {
		return r.p, r.err
	}
	p, err := s.p.Project(ctx, c.Binary, args)
	s.seen[k] = projected{p, err}
	return p, err
}

// sizeTier sizes the tier's candidates, drops the ones that do not fit and
// keeps the best few. Draft-blocked candidates are kept for display.
func (s *sizer) sizeTier(ctx context.Context, sys SystemInfo, tp *TierPlan) {
	if tp.Skipped != "" {
		return
	}
	if s.p == nil {
		tp.Skipped = "No memory projector"
		tp.Candidates = []Candidate{}
		return
	}
	if len(sys.GPUs) > 0 && sys.GPUs[0].UsedMiB > busyMiB {
		tp.Skipped = "GPU in use. Unload models and analyse again."
		tp.Candidates = []Candidate{}
		return
	}
	var sized, blocked []Candidate
	var lastErr string
	for _, c := range tp.Candidates {
		if c.Blocked != "" {
			blocked = append(blocked, c)
			continue
		}
		if err := s.size(ctx, &c); err != nil {
			lastErr = err.Error()
			continue
		}
		if c.Blocked == "" {
			sized = append(sized, c)
		}
	}
	tp.Candidates = append(pick(sized), blocked...)
	if len(sized) == 0 {
		switch {
		case lastErr != "":
			tp.Skipped = "Projection failed: " + lastErr
		case len(blocked) == 0:
			tp.Skipped = "No layout fits in VRAM"
		}
	}
}

// size fits one candidate to the projected free memory. It returns an error
// when the backend gave no projection; a candidate that does not fit is
// marked Blocked.
func (s *sizer) size(ctx context.Context, c *Candidate) error {
	switch c.Layout {
	case LayoutMoECache:
		return s.sizeCache(ctx, c)
	case LayoutNCPUMoE:
		return s.sizeCPULayers(ctx, c)
	case LayoutFullGPU:
		p, err := s.project(ctx, *c)
		if err != nil {
			return err
		}
		c.Memory = &p
		if avail := p.FreeMiB - fitTargetMiB; p.TotalMiB > avail {
			c.Blocked = fmt.Sprintf("Needs %d MiB, %d MiB available", p.TotalMiB, avail)
		}
		return nil
	default:
		// --fit sizes the split itself at load.
		return nil
	}
}

// sizeCache projects with the seed cache, then sets the cache to what is
// left. The cache lives in the context buffer, so everything else is the
// projection minus the seed. A re-projection confirms the result; the cache
// llama.cpp allocates is rounded to whole experts, so it can differ a little.
func (s *sizer) sizeCache(ctx context.Context, c *Candidate) error {
	seed := argInt(*c, "--moe-cache-mib")
	p, err := s.project(ctx, *c)
	if err != nil {
		return err
	}
	avail := p.FreeMiB - fitTargetMiB
	cache := roundDown(avail-(p.TotalMiB-int64(seed)), cacheStepMiB)
	for i := 0; ; i++ {
		if cache < minCacheMiB {
			c.Memory = &p
			c.Blocked = "No room for an expert cache"
			return nil
		}
		setArg(c, "--moe-cache-mib", int(cache))
		if p, err = s.project(ctx, *c); err != nil {
			return err
		}
		if p.TotalMiB <= avail || i == maxRefine {
			break
		}
		cache -= roundUp(p.TotalMiB-avail, cacheStepMiB)
	}
	c.Memory = &p
	if p.TotalMiB > avail {
		c.Blocked = fmt.Sprintf("Needs %d MiB, %d MiB available", p.TotalMiB, avail)
		return nil
	}
	c.layoutReason = fmt.Sprintf("%s, %d MiB cache", c.layoutReason, cache)
	c.Reason = joinReason(c.layoutReason, c.draftReason)
	return nil
}

// sizeCPULayers moves expert layers to the CPU until the launch fits, then
// tries to bring back as many as the remaining room holds.
func (s *sizer) sizeCPULayers(ctx context.Context, c *Candidate) error {
	layers := max(s.model.Layers, 1)
	perLayer := max(mib(s.model.ExpertBytes)/int64(layers), 1)
	n := argInt(*c, "-ncmoe")
	var fit *Projection
	fitN := 0
	for i := 0; i <= maxRefine; i++ {
		setArg(c, "-ncmoe", n)
		p, err := s.project(ctx, *c)
		if err != nil {
			return err
		}
		avail := p.FreeMiB - fitTargetMiB
		if p.TotalMiB > avail {
			if n == layers || (fit != nil && n < fitN) {
				break // all experts on the CPU, or the step down overshot
			}
			n = min(n+int(roundUp(p.TotalMiB-avail, perLayer)/perLayer), layers)
			continue
		}
		pc := p
		fit, fitN = &pc, n
		room := int((avail - p.TotalMiB) / perLayer)
		if room == 0 || n == 0 {
			break
		}
		n = max(n-room, 0)
	}
	if fit == nil {
		c.Blocked = "Does not fit with all experts on the CPU"
		return nil
	}
	setArg(c, "-ncmoe", fitN)
	c.Memory = fit
	c.layoutReason = fmt.Sprintf("Experts of %d layers on CPU", fitN)
	c.Reason = joinReason(c.layoutReason, c.draftReason)
	return nil
}

func roundDown(v, step int64) int64 {
	if v < 0 {
		return 0
	}
	return v / step * step
}

func roundUp(v, step int64) int64 { return (v + step - 1) / step * step }
