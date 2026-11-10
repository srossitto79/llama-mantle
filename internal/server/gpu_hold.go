package server

import (
	"net/http"

	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/mantle"
	"github.com/mostlygeek/llama-swap/internal/router"
)

// heldRouter makes requests to local models wait while a job holds the GPU.
// Wrapping the router covers every way into it: model requests, /upstream
// and preload. Peers have their own GPUs and are not wrapped.
type heldRouter struct {
	router.LocalRouter
	hold *mantle.GPUHold
	log  *logmon.Monitor
}

func (h heldRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h.hold.Wait(r.Context(), func(reason string) {
		h.log.Infof("request %s waiting: GPU held by %s", r.URL.Path, reason)
	})
	if err != nil {
		return // the client went away while waiting
	}
	h.LocalRouter.ServeHTTP(w, r)
}
