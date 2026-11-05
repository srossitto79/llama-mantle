package server

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mostlygeek/llama-swap/internal/chain"
	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/loopguard"
	"github.com/mostlygeek/llama-swap/internal/shared"
)

// loopGuardMetadataKey marks guarded requests in the activity log.
const loopGuardMetadataKey = "reasoning_loop"

// CreateLoopGuardMiddleware returns middleware that runs chat completions of
// models with reasoningLoopGuard enabled through the reasoning loop guard. It
// sits after the metrics middleware, so metrics and captures see the reply the
// client receives.
func CreateLoopGuardMiddleware(cfg config.Config, log loopguard.Logger) chain.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			settings, ok := loopGuardSettings(cfg, r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				shared.SendResponse(w, r, http.StatusBadRequest, "could not read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.Header.Del("Transfer-Encoding")
			r.Header.Set("Content-Length", strconv.Itoa(len(body)))
			r.ContentLength = int64(len(body))

			if !loopguard.Eligible(body) {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			settings.OnAction = func(action string) {
				_ = shared.SetReqData(ctx, loopGuardMetadataKey, action)
			}
			guard, err := loopguard.NewHandler(settings, next, log)
			if err != nil {
				shared.SendResponse(w, r, http.StatusInternalServerError, err.Error())
				return
			}
			guard.ServeHTTP(w, r)
		})
	}
}

// loopGuardSettings returns the guard settings for a chat completion request
// to a local model with the guard enabled.
func loopGuardSettings(cfg config.Config, r *http.Request) (loopguard.Settings, bool) {
	if r.Method != http.MethodPost || (r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/v/chat/completions") {
		return loopguard.Settings{}, false
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		return loopguard.Settings{}, false
	}
	data, ok := shared.ReadContext(r.Context())
	if !ok {
		return loopguard.Settings{}, false
	}
	mc, ok := cfg.Models[data.ModelID]
	if !ok || !mc.ReasoningLoopGuard.Enabled() {
		return loopguard.Settings{}, false
	}
	g := mc.ReasoningLoopGuard
	return loopguard.Settings{
		Model:        data.ModelID,
		Mode:         g.Mode,
		Recovery:     g.Recovery,
		Continuation: g.Continuation,
		Coverage:     g.Coverage,
		CheckEvery:   g.CheckEvery,
	}, true
}
