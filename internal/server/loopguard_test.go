package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/tidwall/gjson"
)

// loopingUpstream streams llama.cpp-style chunks: reasoning that loops until
// the request is cancelled, or, for an assistant prefill, a short answer.
type loopingUpstream struct {
	mu    sync.Mutex
	calls []string
}

func (u *loopingUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.calls = append(u.calls, string(body))
	u.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	chunk := func(delta string) error {
		_, err := fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":7,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", delta)
		w.(http.Flusher).Flush()
		return err
	}
	if gjson.Get(string(body), "messages.@reverse.0.role").String() == "assistant" {
		chunk(`{"content":"ok"}`)
		fmt.Fprint(w, "data: {\"id\":\"c2\",\"object\":\"chat.completion.chunk\",\"created\":8,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3000,\"completion_tokens\":1,\"total_tokens\":3001}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	for i := 0; r.Context().Err() == nil && i < 100000; i++ {
		if chunk(fmt.Sprintf(`{"reasoning_content":"again %d "}`, i%5)) != nil {
			return
		}
	}
}

func loopGuardServer(t *testing.T, up http.Handler) *Server {
	t.Helper()
	local := newStubRouter([]string{"guarded", "plain"}, "")
	local.serveHTTP = up.ServeHTTP
	s := newTestServer(local, newStubRouter(nil, ""))
	s.cfg = config.Config{Models: map[string]config.ModelConfig{
		"guarded": {ReasoningLoopGuard: config.ReasoningLoopGuardConfig{
			Mode: "recover", Recovery: "wrapUp", Coverage: 0.25, CheckEvery: 256,
		}},
		"plain": {},
	}}
	s.routes()
	return s
}

func TestLoopGuard_ServerChainRecovers(t *testing.T) {
	up := &loopingUpstream{}
	s := loopGuardServer(t, up)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"guarded","stream":true,"messages":[{"role":"user","content":"q"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.HasSuffix(body, "data: [DONE]\n\n") || strings.Count(body, "[DONE]") != 1 {
		t.Fatalf("status %d, stream end %q", w.Code, body[max(0, len(body)-200):])
	}
	if !strings.Contains(body, `"content":"ok"`) || strings.Contains(body, `"id":"c2"`) {
		t.Fatal("continuation missing or not rewritten to the first id")
	}
	up.mu.Lock()
	calls := len(up.calls)
	up.mu.Unlock()
	if calls != 2 {
		t.Fatalf("upstream calls %d, want 2", calls)
	}

	entries := metricsEntries(t, s.metrics)
	if len(entries) != 1 {
		t.Fatalf("metrics entries %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Metadata[loopGuardMetadataKey] != "recovered" {
		t.Errorf("metadata %v, want %s=recovered", e.Metadata, loopGuardMetadataKey)
	}
	if e.Tokens.OutputTokens <= 1 || e.Tokens.InputTokens+e.Tokens.OutputTokens != 3001 {
		t.Errorf("tokens in=%d out=%d, want the cut tokens counted as output", e.Tokens.InputTokens, e.Tokens.OutputTokens)
	}
}

func TestLoopGuard_ServerChainOffPassesThrough(t *testing.T) {
	const upstream = "data: {\"id\":\"x\"}\n\nnot even sse\n"
	s := loopGuardServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, upstream)
	}))

	for _, body := range []string{
		`{"model":"plain","stream":true,"messages":[{"role":"user","content":"q"}]}`,
		// Guarded model, but an image input makes it ineligible.
		`{"model":"guarded","stream":true,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Body.String() != upstream {
			t.Errorf("body %q, want the upstream bytes unchanged", w.Body.String())
		}
	}
}
