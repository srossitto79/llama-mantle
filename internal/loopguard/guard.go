package loopguard

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Guard modes.
const (
	ModeStop    = "stop"
	ModeRecover = "recover"
)

// Actions reported through Settings.OnAction.
const (
	ActionRecovered = "recovered"
	ActionStopped   = "stopped"
)

// Settings configure the guard for one request.
type Settings struct {
	Model    string // model id, for the log line
	Mode     string // ModeStop or ModeRecover
	Recovery string // RecoveryWrapUp or RecoveryLowEffort
	// Continuation is ContinuationLlamaCpp (default when empty) or
	// ContinuationVLLM.
	Continuation string
	Coverage     float64 // loop threshold
	CheckEvery   int     // reasoning tokens between checks

	// OnAction, when set, is called once when the guard stops or recovers a
	// reply.
	OnAction func(action string)
}

// Eligible reports whether a chat-completion request body can be guarded:
// one choice, no image or audio input (a continuation would encode it again),
// and no client-supplied assistant prefill.
func Eligible(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() && n.Int() > 1 {
		return false
	}
	messages := gjson.GetBytes(body, "messages").Array()
	if len(messages) == 0 || messages[len(messages)-1].Get("role").String() == "assistant" {
		return false
	}
	for _, m := range messages {
		for _, part := range m.Get("content").Array() {
			switch part.Get("type").String() {
			case "image_url", "input_image", "image", "input_audio", "audio":
				return false
			}
		}
	}
	return true
}

// NewHandler returns a handler that serves a chat completion through next
// (the upstream dispatcher) with the guard watching its reasoning.
func NewHandler(s Settings, next http.Handler, log Logger) (http.Handler, error) {
	if s.Mode != ModeStop && s.Mode != ModeRecover {
		return nil, fmt.Errorf("loopguard: unknown mode %q", s.Mode)
	}
	if s.CheckEvery <= 0 {
		return nil, fmt.Errorf("loopguard: checkEvery must be > 0")
	}
	g := &guard{settings: s, next: next, log: log}
	if s.Mode == ModeRecover {
		c, err := NewContinuer(s.Recovery, s.Continuation)
		if err != nil {
			return nil, err
		}
		g.continuer = c
	}
	return g, nil
}

type guard struct {
	settings  Settings
	next      http.Handler
	continuer ReplyContinuer
	log       Logger
}

// reply is the state of one guarded reply.
type reply struct {
	out ReplyWriter

	id, model string
	created   int64
	hasID     bool

	reasoning bytes.Buffer
	tokens    int // reasoning chunks before the cut, about one token each
	answered  bool
	lastCheck int
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return
	}
	streaming := gjson.GetBytes(body, "stream").Bool()

	// The guard always reads a stream; a non-streaming reply is assembled.
	upBody, err := sjson.SetBytes(body, "stream", true)
	if err == nil && !streaming {
		upBody, err = sjson.SetBytes(upBody, "stream_options.include_usage", true)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	up := startUpstream(r, g.next, upBody)
	if up.status != http.StatusOK || !isEventStream(up.header) {
		passThrough(w, up)
		up.close()
		return
	}

	rp := &reply{out: newReplyWriter(w, streaming)}
	rp.out.Begin(up.header)

	detector := NewDetector(g.settings.Coverage)
	cut, coverage, ok := g.relay(up, rp, detector, nil)
	up.close()
	if !ok {
		return // client gone
	}
	if !cut {
		_ = rp.out.End()
		return
	}

	limitReached := tokenLimitReached(upBody, rp.tokens)
	if g.settings.Mode == ModeStop || limitReached || r.Context().Err() != nil {
		g.logf("<%s> repeated reasoning detected at %d tokens (coverage=%.3f); stopping the reply", rp.tokens, coverage)
		g.report(ActionStopped)
		g.finish(rp, "length")
		return
	}

	g.logf("<%s> repeated reasoning detected at %d tokens (coverage=%.3f); continuing the reply ("+g.settings.Recovery+")", rp.tokens, coverage)
	g.report(ActionRecovered)

	cont, err := g.continuer.Continue(upBody, rp.reasoning.String(), rp.tokens)
	if err != nil {
		g.logf("<%s> continuation not built: %v", err)
		g.finish(rp, "length")
		return
	}
	up2 := startUpstream(r, g.next, cont.Body)
	if up2.status != http.StatusOK || !isEventStream(up2.header) {
		msg, _ := io.ReadAll(io.LimitReader(up2.body, 2048))
		up2.close()
		g.logf("<%s> continuation failed: status %d: %s", up2.status, bytes.TrimSpace(msg))
		g.finish(rp, "length")
		return
	}
	if cont.Lead != "" && rp.out.Chunk(rp.reasoningChunk(cont.Lead)) != nil {
		up2.close()
		return
	}
	_, _, ok = g.relay(up2, rp, nil, newEchoFilter(cont.Echo))
	up2.close()
	if ok {
		_ = rp.out.End()
	}
}

// relay copies the upstream's events to the client. With a detector it checks
// the reasoning every CheckEvery tokens until the answer starts, and returns
// cut=true when it loops. A continuation (echo set) drops the echoed prefill,
// rewrites the chunks to the reply's identity and folds the cut tokens into
// usage. ok is false when the client can no longer be written to.
func (g *guard) relay(up *upstreamResponse, rp *reply, detector LoopDetector, echo *echoFilter) (cut bool, coverage float64, ok bool) {
	continuation := echo != nil
	for {
		ev, err := readEvent(up.body)
		if err != nil {
			return false, 0, true // upstream finished or was cancelled
		}
		if ev.data == nil {
			if err := rp.out.Event(ev.block); err != nil {
				return false, 0, false
			}
			continue
		}
		if string(ev.data) == "[DONE]" {
			continue
		}
		c, isChunk := parseChunk(ev.data)
		if !isChunk {
			// llama-swap's loading state and upstream errors carry no id.
			if err := rp.out.Event(ev.block); err != nil {
				return false, 0, false
			}
			continue
		}

		if continuation {
			if roleOnly(c.Raw) {
				continue
			}
			var drop bool
			if c, drop = echo.apply(c); drop {
				continue
			}
			c = rp.rewrite(c)
		} else if !rp.hasID {
			rp.hasID = true
			rp.id = gjson.GetBytes(c.Raw, "id").String()
			rp.model = gjson.GetBytes(c.Raw, "model").String()
			rp.created = gjson.GetBytes(c.Raw, "created").Int()
		}

		if c.Content != "" || c.ToolCalls != nil {
			rp.answered = true
		}
		if !continuation && !rp.answered && c.Reasoning != "" {
			rp.reasoning.WriteString(c.Reasoning)
			rp.tokens++
			if detector != nil {
				detector.Feed(c.Reasoning)
			}
		}

		if err := rp.out.Chunk(c); err != nil {
			return false, 0, false
		}

		if detector != nil && !rp.answered && rp.tokens-rp.lastCheck >= g.settings.CheckEvery {
			rp.lastCheck = rp.tokens
			if looping, cov := detector.Check(); looping {
				return true, cov, true
			}
		}
	}
}

// finish ends the reply with a final chunk carrying finishReason.
func (g *guard) finish(rp *reply, finishReason string) {
	raw := rp.chunkRaw()
	raw, _ = sjson.SetRawBytes(raw, "choices", []byte(`[{"index":0,"delta":{},"finish_reason":`+strconv.Quote(finishReason)+`}]`))
	if err := rp.out.Chunk(Chunk{Raw: raw, FinishReason: finishReason}); err == nil {
		_ = rp.out.End()
	}
}

// chunkRaw returns an empty chunk with the reply's identity.
func (rp *reply) chunkRaw() []byte {
	raw, _ := sjson.SetBytes(nil, "id", rp.id)
	raw, _ = sjson.SetBytes(raw, "object", "chat.completion.chunk")
	raw, _ = sjson.SetBytes(raw, "created", rp.created)
	raw, _ = sjson.SetBytes(raw, "model", rp.model)
	return raw
}

// reasoningChunk returns a chunk carrying reasoning text.
func (rp *reply) reasoningChunk(text string) Chunk {
	raw := rp.chunkRaw()
	raw, _ = sjson.SetRawBytes(raw, "choices", []byte(`[{"index":0,"delta":{},"finish_reason":null}]`))
	raw, _ = sjson.SetBytes(raw, "choices.0.delta.reasoning_content", text)
	return Chunk{Raw: raw, Reasoning: text}
}

func (g *guard) logf(format string, args ...any) {
	if g.log != nil {
		g.log.Infof(format, append([]any{g.settings.Model}, args...)...)
	}
}

func (g *guard) report(action string) {
	if g.settings.OnAction != nil {
		g.settings.OnAction(action)
	}
}

// rewrite gives a continuation chunk the reply's id, model and created, and
// counts the tokens generated before the cut as completion, not prompt.
func (rp *reply) rewrite(c Chunk) Chunk {
	raw := c.Raw
	raw, _ = sjson.SetBytes(raw, "id", rp.id)
	raw, _ = sjson.SetBytes(raw, "model", rp.model)
	raw, _ = sjson.SetBytes(raw, "created", rp.created)
	if c.Usage != nil {
		prompt := gjson.GetBytes(c.Usage, "prompt_tokens").Int() - int64(rp.tokens)
		if prompt < 0 {
			prompt = 0
		}
		completion := gjson.GetBytes(c.Usage, "completion_tokens").Int() + int64(rp.tokens)
		usage := c.Usage
		usage, _ = sjson.SetBytes(usage, "prompt_tokens", prompt)
		usage, _ = sjson.SetBytes(usage, "completion_tokens", completion)
		usage, _ = sjson.SetBytes(usage, "total_tokens", prompt+completion)
		raw, _ = sjson.SetRawBytes(raw, "usage", usage)
		c.Usage = usage
	}
	c.Raw = raw
	return c
}

// parseChunk reads a chat.completion.chunk payload. Payloads without an id
// (llama-swap's loading state, error objects) are not chunks.
func parseChunk(data []byte) (Chunk, bool) {
	if !gjson.ValidBytes(data) {
		return Chunk{}, false
	}
	if gjson.GetBytes(data, "id").String() == "" {
		return Chunk{}, false
	}
	raw := append([]byte(nil), data...)
	choice := gjson.GetBytes(raw, "choices.0")
	c := Chunk{
		Raw:          raw,
		Reasoning:    choice.Get("delta.reasoning_content").String(),
		Content:      choice.Get("delta.content").String(),
		FinishReason: choice.Get("finish_reason").String(),
	}
	if tc := choice.Get("delta.tool_calls"); tc.IsArray() && len(tc.Array()) > 0 {
		c.ToolCalls = []byte(tc.Raw)
	}
	if u := gjson.GetBytes(raw, "usage"); u.IsObject() {
		c.Usage = []byte(u.Raw)
	}
	if t := gjson.GetBytes(raw, "timings"); t.IsObject() {
		c.Timings = []byte(t.Raw)
	}
	return c, true
}

// roleOnly reports whether a chunk only announces the assistant role.
func roleOnly(raw []byte) bool {
	choice := gjson.GetBytes(raw, "choices.0")
	if !choice.Exists() || choice.Get("finish_reason").String() != "" || gjson.GetBytes(raw, "usage").IsObject() {
		return false
	}
	only := true
	choice.Get("delta").ForEach(func(k, v gjson.Result) bool {
		if k.String() != "role" && v.Type != gjson.Null && v.String() != "" {
			only = false
		}
		return only
	})
	return only
}

// tokenLimitReached reports whether the request's token limit is used up.
func tokenLimitReached(body []byte, generated int) bool {
	for _, key := range []string{"max_tokens", "max_completion_tokens", "n_predict"} {
		if v := gjson.GetBytes(body, key); v.Exists() && v.Int() > 0 && int64(generated) >= v.Int() {
			return true
		}
	}
	return false
}

func isEventStream(h http.Header) bool {
	mt, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	return mt == "text/event-stream"
}

// passThrough relays an upstream response the guard does not handle.
func passThrough(w http.ResponseWriter, up *upstreamResponse) {
	for k, vs := range up.header {
		w.Header()[k] = append([]string(nil), vs...)
	}
	w.WriteHeader(up.status)
	_, _ = io.Copy(w, up.body)
}

// readEventBytes parses one event block.
func readEventBytes(block []byte) (sseEvent, error) {
	return readEvent(bufio.NewReader(bytes.NewReader(block)))
}
