package mantle

import (
	"context"
	"sync"
)

// GPUHold makes local model requests wait while a job needs the whole GPU,
// such as a MoE cache tune timing its own llama-server runs. A request that
// arrived during the hold would load a model beside the job: it would skew
// the job's measurements or run either one out of memory.
//
// It lives on the TaskManager so it outlives config reloads, which rebuild the
// server and the handler but not the jobs.
type GPUHold struct {
	mu       sync.Mutex
	reason   string
	released chan struct{} // closed on release; nil while not held
}

// Acquire takes the hold. ok is false when it is already held; release is
// idempotent.
func (g *GPUHold) Acquire(reason string) (release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released != nil {
		return func() {}, false
	}
	ch := make(chan struct{})
	g.reason, g.released = reason, ch
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.reason, g.released = "", nil
			g.mu.Unlock()
			close(ch)
		})
	}, true
}

// Reason is what holds the GPU, or "" when nothing does.
func (g *GPUHold) Reason() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reason
}

// Wait returns once the GPU is not held, or with ctx's error. waiting is
// called once, with the reason, when the caller has to wait.
func (g *GPUHold) Wait(ctx context.Context, waiting func(reason string)) error {
	for {
		g.mu.Lock()
		ch, reason := g.released, g.reason
		g.mu.Unlock()
		if ch == nil {
			return nil
		}
		if waiting != nil {
			waiting(reason)
			waiting = nil
		}
		select {
		case <-ch:
			// Loop: another job may have taken the hold right after.
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
