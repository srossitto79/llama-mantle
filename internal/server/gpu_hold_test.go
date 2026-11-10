package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/mantle"
	"github.com/mostlygeek/llama-swap/internal/router"
)

// unwrapLocal returns the router behind the GPU hold.
func unwrapLocal(r router.LocalRouter) router.LocalRouter {
	if h, ok := r.(heldRouter); ok {
		return h.LocalRouter
	}
	return r
}

// servedRouter records that a request reached the local router.
type servedRouter struct {
	router.LocalRouter
	served chan struct{}
}

func (s servedRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.served <- struct{}{}
}

func TestServer_HeldRouterWaitsForRelease(t *testing.T) {
	var hold mantle.GPUHold
	release, ok := hold.Acquire("test job")
	if !ok {
		t.Fatal("Acquire failed")
	}
	inner := servedRouter{served: make(chan struct{}, 1)}
	h := heldRouter{LocalRouter: inner, hold: &hold, log: logmon.NewWriter(io.Discard)}

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		close(done)
	}()
	select {
	case <-inner.served:
		t.Fatal("request reached the router while the GPU was held")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-inner.served:
	case <-time.After(time.Second):
		t.Fatal("request did not reach the router after release")
	}
	<-done
}

func TestServer_HeldRouterDropsCancelledWaiter(t *testing.T) {
	var hold mantle.GPUHold
	release, _ := hold.Acquire("test job")
	defer release()
	inner := servedRouter{served: make(chan struct{}, 1)}
	h := heldRouter{LocalRouter: inner, hold: &hold, log: logmon.NewWriter(io.Discard)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	h.ServeHTTP(httptest.NewRecorder(), req)
	select {
	case <-inner.served:
		t.Fatal("cancelled request reached the router")
	default:
	}
}
