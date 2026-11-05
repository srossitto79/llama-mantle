package loopguard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

// fakeLlama is an upstream that streams llama.cpp-style chunks. A request
// without an assistant prefill reasons in a loop until it is cancelled (or
// answers at once when loop is false); a prefill request finishes the reply.
type fakeLlama struct {
	loop bool

	mu        sync.Mutex
	bodies    [][]byte
	cancelled []bool
	streamed  strings.Builder // reasoning sent by the first request
}

func (f *fakeLlama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	idx := len(f.bodies)
	f.bodies = append(f.bodies, body)
	f.cancelled = append(f.cancelled, false)
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.cancelled[idx] = r.Context().Err() != nil
		f.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Upstream", "fake")
	w.WriteHeader(http.StatusOK)
	id := fmt.Sprintf("chatcmpl-%d", idx)
	send := func(delta string, extra string) bool {
		_, err := fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"up-%d\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]%s}\n\n", id, 1000+idx, idx, delta, extra)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return err == nil
	}
	reason := func(s string) bool {
		d, _ := json.Marshal(map[string]string{"reasoning_content": s})
		return send(string(d), "")
	}
	finish := func(prompt, completion int) {
		fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"up-%d\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", id, 1000+idx, idx)
		fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"up-%d\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", id, 1000+idx, idx, prompt, completion, prompt+completion)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}

	send(`{"role":"assistant","content":null}`, "")
	messages := gjson.GetBytes(body, "messages").Array()
	if messages[len(messages)-1].Get("role").String() == "assistant" {
		// llama.cpp re-sends the prefill with the first new token.
		reason(messages[len(messages)-1].Get("reasoning_content").String() + " I settle it.")
		send(`{"content":"The answer"}`, "")
		send(`{"content":" is 42."}`, "")
		// The prompt holds the prefilled reasoning, about a token per word.
		finish(100+len(strings.Fields(messages[len(messages)-1].Get("reasoning_content").String())), 7)
		return
	}
	if !f.loop {
		reason("Short thought.")
		send(`{"content":"Done."}`, "")
		finish(100, 3)
		return
	}
	for i := 0; i < 300; i++ {
		word := fmt.Sprintf("step%d ", i)
		f.mu.Lock()
		f.streamed.WriteString(word)
		f.mu.Unlock()
		if !reason(word) {
			return
		}
	}
	passage := strings.Fields("so the value is seven but wait let me recheck the value from the start again")
	for i := 0; ; i++ {
		if r.Context().Err() != nil {
			return
		}
		word := passage[i%len(passage)] + " "
		f.mu.Lock()
		f.streamed.WriteString(word)
		f.mu.Unlock()
		if !reason(word) {
			return
		}
		if i > 200000 {
			return
		}
	}
}

func newTestGuard(t *testing.T, mode string, up http.Handler) http.Handler {
	t.Helper()
	h, err := NewHandler(Settings{Model: "m", Mode: mode, Recovery: RecoveryWrapUp, Coverage: 0.25, CheckEvery: 128}, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func chatBody(stream bool) string {
	return fmt.Sprintf(`{"model":"m","stream":%v,"max_tokens":100000,"messages":[{"role":"user","content":"hi"}]}`, stream)
}

// readSSE returns the data payloads of an SSE body.
func readSSE(t *testing.T, body []byte) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if p, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			out = append(out, p)
		}
	}
	return out
}

func TestLoopGuard_StreamRecovers(t *testing.T) {
	up := &fakeLlama{loop: true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody(true)))
	newTestGuard(t, ModeRecover, up).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("X-Upstream") != "fake" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	events := readSSE(t, rec.Body.Bytes())
	if events[len(events)-1] != "[DONE]" {
		t.Fatalf("last event %q, want [DONE]", events[len(events)-1])
	}
	for _, e := range events[:len(events)-1] {
		if e == "[DONE]" {
			t.Fatal("[DONE] before the end of the stream")
		}
	}

	var reasoning, content strings.Builder
	var usage gjson.Result
	finishes := 0
	for _, e := range events[:len(events)-1] {
		if got := gjson.Get(e, "id").String(); got != "chatcmpl-0" {
			t.Fatalf("chunk id %q, want chatcmpl-0: %s", got, e)
		}
		if gjson.Get(e, "model").String() != "up-0" || gjson.Get(e, "created").Int() != 1000 {
			t.Fatalf("chunk identity not rewritten: %s", e)
		}
		reasoning.WriteString(gjson.Get(e, "choices.0.delta.reasoning_content").String())
		content.WriteString(gjson.Get(e, "choices.0.delta.content").String())
		if gjson.Get(e, "choices.0.finish_reason").String() != "" {
			finishes++
		}
		if u := gjson.Get(e, "usage"); u.Exists() {
			usage = u
		}
	}
	if content.String() != "The answer is 42." || finishes != 1 {
		t.Fatalf("content %q, finishes %d", content.String(), finishes)
	}

	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.bodies) != 2 || !up.cancelled[0] {
		t.Fatalf("upstream calls %d, first cancelled %v", len(up.bodies), up.cancelled)
	}
	// The client saw the looping reasoning, then the continuation's.
	cut := strings.TrimSuffix(reasoning.String(), wrapUpSentence+" I settle it.")
	if !strings.HasPrefix(up.streamed.String(), cut) || len(cut) < 2000 {
		t.Fatalf("reasoning before the cut (%d bytes) is not what upstream streamed", len(cut))
	}

	cont := up.bodies[1]
	msgs := gjson.GetBytes(cont, "messages").Array()
	last := msgs[len(msgs)-1]
	if len(msgs) != 2 || last.Get("role").String() != "assistant" || last.Get("content").String() != "" {
		t.Fatalf("continuation messages: %s", gjson.GetBytes(cont, "messages").Raw)
	}
	if last.Get("reasoning_content").String() != cut+wrapUpSentence {
		t.Fatalf("prefill reasoning does not match the streamed reasoning plus the wrap-up sentence")
	}
	if gjson.GetBytes(cont, "temperature").Float() != 1.0 || gjson.GetBytes(cont, "presence_penalty").Float() != 1.5 {
		t.Fatalf("continuation sampling: %s", cont)
	}
	cutTokens := strings.Count(cut, " ")
	if got := gjson.GetBytes(cont, "max_tokens").Int(); got != int64(100000-cutTokens) {
		t.Fatalf("max_tokens %d, want %d", got, 100000-cutTokens)
	}
	contPrompt := 100 + len(strings.Fields(cut+wrapUpSentence))
	if usage.Get("completion_tokens").Int() != int64(7+cutTokens) ||
		usage.Get("prompt_tokens").Int() != int64(contPrompt-cutTokens) ||
		usage.Get("total_tokens").Int() != int64(contPrompt+7) {
		t.Fatalf("usage %s, cut at %d tokens", usage.Raw, cutTokens)
	}
}

func TestLoopGuard_NonStreamAssemblesJSON(t *testing.T) {
	up := &fakeLlama{loop: true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody(false)))
	newTestGuard(t, ModeRecover, up).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.Bytes()
	if !gjson.ValidBytes(body) {
		t.Fatalf("invalid JSON: %.200s", body)
	}
	if gjson.GetBytes(body, "object").String() != "chat.completion" || gjson.GetBytes(body, "id").String() != "chatcmpl-0" {
		t.Fatalf("response: %.300s", body)
	}
	if got := gjson.GetBytes(body, "choices.0.message.content").String(); got != "The answer is 42." {
		t.Fatalf("content %q", got)
	}
	if gjson.GetBytes(body, "choices.0.finish_reason").String() != "stop" || !gjson.GetBytes(body, "usage.total_tokens").Exists() {
		t.Fatalf("finish/usage: %.300s", body)
	}
	up.mu.Lock()
	first := up.bodies[0]
	up.mu.Unlock()
	if !gjson.GetBytes(first, "stream").Bool() || !gjson.GetBytes(first, "stream_options.include_usage").Bool() {
		t.Fatalf("upstream request not streamed: %s", first)
	}
}

func TestLoopGuard_StopMode(t *testing.T) {
	up := &fakeLlama{loop: true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody(true)))
	newTestGuard(t, ModeStop, up).ServeHTTP(rec, req)

	events := readSSE(t, rec.Body.Bytes())
	if len(events) < 2 || events[len(events)-1] != "[DONE]" {
		t.Fatal("stream not terminated")
	}
	final := events[len(events)-2]
	if gjson.Get(final, "choices.0.finish_reason").String() != "length" || gjson.Get(final, "id").String() != "chatcmpl-0" {
		t.Fatalf("final chunk %s", final)
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.bodies) != 1 || !up.cancelled[0] {
		t.Fatalf("upstream calls %d, cancelled %v", len(up.bodies), up.cancelled)
	}
}

func TestLoopGuard_NoLoopPassesStream(t *testing.T) {
	up := &fakeLlama{loop: false}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody(true)))
	newTestGuard(t, ModeRecover, up).ServeHTTP(rec, req)

	events := readSSE(t, rec.Body.Bytes())
	// role, reasoning, content, finish, usage, [DONE]: all forwarded unchanged.
	if len(events) != 6 || events[5] != "[DONE]" || gjson.Get(events[2], "choices.0.delta.content").String() != "Done." {
		t.Fatalf("events %q", events)
	}
	if len(up.bodies) != 1 {
		t.Fatalf("upstream calls %d", len(up.bodies))
	}
}

func TestLoopGuard_UpstreamErrorPassesThrough(t *testing.T) {
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad"}}`)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody(false)))
	newTestGuard(t, ModeRecover, up).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || rec.Body.String() != `{"error":{"message":"bad"}}` {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestLoopGuard_ClientDisconnectCancelsUpstream(t *testing.T) {
	// The fake loops; the detector is set never to fire, so only the client
	// going away can end the request.
	up := &fakeLlama{loop: true}
	h, err := NewHandler(Settings{Model: "m", Mode: ModeRecover, Recovery: RecoveryWrapUp, Coverage: 2, CheckEvery: 128}, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(chatBody(true)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		up.mu.Lock()
		done := len(up.cancelled) == 1 && up.cancelled[0]
		up.mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("upstream request not cancelled after client disconnect")
}

func TestLoopGuard_Eligible(t *testing.T) {
	tests := []struct {
		body string
		want bool
	}{
		{`{"messages":[{"role":"user","content":"hi"}]}`, true},
		{`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`, true},
		{`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`, false},
		{`{"n":2,"messages":[{"role":"user","content":"hi"}]}`, false},
		{`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"pre"}]}`, false},
		{`{"messages":[]}`, false},
		{`not json`, false},
	}
	for _, tt := range tests {
		if got := Eligible([]byte(tt.body)); got != tt.want {
			t.Errorf("Eligible(%s) = %v, want %v", tt.body, got, tt.want)
		}
	}
}

func TestLoopGuard_LowEffortContinuation(t *testing.T) {
	c, err := NewContinuer(RecoveryLowEffort, "")
	if err != nil {
		t.Fatal(err)
	}
	orig := []byte(`{"messages":[{"role":"user","content":"q"}],"temperature":1.2,"presence_penalty":0.5,"chat_template_kwargs":{"enable_thinking":true,"reasoning_effort":"xhigh"},"max_tokens":10}`)
	got, err := c.Continue(orig, "thinking", 20)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Body
	if gjson.GetBytes(b, "chat_template_kwargs.reasoning_effort").String() != "low" ||
		!gjson.GetBytes(b, "chat_template_kwargs.enable_thinking").Bool() ||
		gjson.GetBytes(b, "temperature").Float() != 1.2 ||
		gjson.GetBytes(b, "presence_penalty").Float() != 1.5 ||
		gjson.GetBytes(b, "max_tokens").Int() != 1 ||
		gjson.GetBytes(b, "messages.1.reasoning_content").String() != "thinking" {
		t.Fatalf("continuation body %s", b)
	}
}

func TestLoopGuard_VLLMContinuation(t *testing.T) {
	c, err := NewContinuer(RecoveryWrapUp, ContinuationVLLM)
	if err != nil {
		t.Fatal(err)
	}
	orig := []byte(`{"messages":[{"role":"user","content":"q"}],"chat_template_kwargs":{"enable_thinking":true}}`)
	got, err := c.Continue(orig, "thinking", 20)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Body
	if gjson.GetBytes(b, "add_generation_prompt").Bool() ||
		!gjson.GetBytes(b, "add_generation_prompt").Exists() ||
		!gjson.GetBytes(b, "chat_template_kwargs.continue_reasoning").Bool() ||
		!gjson.GetBytes(b, "chat_template_kwargs.enable_thinking").Bool() ||
		gjson.GetBytes(b, "messages.1.role").String() != "assistant" ||
		gjson.GetBytes(b, "messages.1.reasoning_content").String() != "thinking"+wrapUpSentence {
		t.Fatalf("continuation body %s", b)
	}
	if got.Echo != "" || got.Lead != wrapUpSentence {
		t.Fatalf("echo %q lead %q", got.Echo, got.Lead)
	}
	if _, err := NewContinuer(RecoveryWrapUp, "sglang"); err == nil {
		t.Fatal("unknown continuation accepted")
	}
}
