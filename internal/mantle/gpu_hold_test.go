package mantle

import (
	"context"
	"testing"
	"time"
)

func TestGPUHold_AcquireIsExclusive(t *testing.T) {
	var g GPUHold
	release, ok := g.Acquire("first")
	if !ok || g.Reason() != "first" {
		t.Fatalf("Acquire = %v, reason %q", ok, g.Reason())
	}
	if _, ok := g.Acquire("second"); ok {
		t.Fatal("second Acquire succeeded while held")
	}
	release()
	release() // idempotent
	if g.Reason() != "" {
		t.Fatalf("reason after release = %q", g.Reason())
	}
	if _, ok := g.Acquire("third"); !ok {
		t.Fatal("Acquire after release failed")
	}
}

func TestGPUHold_WaitReturnsAtOnceWhenFree(t *testing.T) {
	var g GPUHold
	called := false
	if err := g.Wait(context.Background(), func(string) { called = true }); err != nil || called {
		t.Fatalf("Wait = %v, waiting called = %v", err, called)
	}
}

func TestGPUHold_WaitBlocksUntilRelease(t *testing.T) {
	var g GPUHold
	release, _ := g.Acquire("job")
	var reason string
	done := make(chan error, 1)
	go func() { done <- g.Wait(context.Background(), func(r string) { reason = r }) }()
	select {
	case <-done:
		t.Fatal("Wait returned while held")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil || reason != "job" {
			t.Fatalf("Wait = %v, reason %q", err, reason)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after release")
	}
}

func TestGPUHold_WaitHonoursContext(t *testing.T) {
	var g GPUHold
	release, _ := g.Acquire("job")
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := g.Wait(ctx, nil); err == nil {
		t.Fatal("Wait returned nil with the GPU held and the context done")
	}
}
