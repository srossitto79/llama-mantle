package loopguard

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/tidwall/gjson"
)

// newReplyWriter returns the ReplyWriter for the client's request shape.
func newReplyWriter(w http.ResponseWriter, streaming bool) ReplyWriter {
	if streaming {
		return &sseReplyWriter{w: w}
	}
	return &jsonReplyWriter{w: w}
}

// sseReplyWriter forwards the reply as one SSE stream.
type sseReplyWriter struct {
	w http.ResponseWriter
}

func (s *sseReplyWriter) Begin(header http.Header) {
	copyHeaders(s.w.Header(), header)
	if s.w.Header().Get("Content-Type") == "" {
		s.w.Header().Set("Content-Type", "text/event-stream")
	}
	s.w.WriteHeader(http.StatusOK)
	s.flush()
}

func (s *sseReplyWriter) Event(block []byte) error {
	return s.write(block, []byte("\n\n"))
}

func (s *sseReplyWriter) Chunk(c Chunk) error {
	return s.write([]byte("data: "), c.Raw, []byte("\n\n"))
}

func (s *sseReplyWriter) End() error {
	return s.write([]byte("data: [DONE]\n\n"))
}

func (s *sseReplyWriter) write(parts ...[]byte) error {
	for _, p := range parts {
		if _, err := s.w.Write(p); err != nil {
			return err
		}
	}
	s.flush()
	return nil
}

func (s *sseReplyWriter) flush() {
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
}

// jsonReplyWriter assembles the chunks into one chat.completion response.
type jsonReplyWriter struct {
	w      http.ResponseWriter
	header http.Header

	seen         bool
	id           string
	model        string
	created      int64
	fingerprint  string
	reasoning    []byte
	content      []byte
	toolCalls    map[int64]*toolCall
	finishReason string
	usage        json.RawMessage
	timings      json.RawMessage
	errorBody    []byte
}

type toolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (j *jsonReplyWriter) Begin(header http.Header) { j.header = header }

// Event keeps an upstream error event to return when no completion follows.
func (j *jsonReplyWriter) Event(block []byte) error {
	ev, _ := readEventBytes(block)
	if ev.data != nil && gjson.GetBytes(ev.data, "error").Exists() {
		j.errorBody = ev.data
	}
	return nil
}

func (j *jsonReplyWriter) Chunk(c Chunk) error {
	if !j.seen {
		j.seen = true
		j.id = gjson.GetBytes(c.Raw, "id").String()
		j.model = gjson.GetBytes(c.Raw, "model").String()
		j.created = gjson.GetBytes(c.Raw, "created").Int()
		j.fingerprint = gjson.GetBytes(c.Raw, "system_fingerprint").String()
	}
	j.reasoning = append(j.reasoning, c.Reasoning...)
	j.content = append(j.content, c.Content...)
	if c.FinishReason != "" {
		j.finishReason = c.FinishReason
	}
	if c.Usage != nil {
		j.usage = c.Usage
	}
	if c.Timings != nil {
		j.timings = c.Timings
	}
	for _, tc := range gjson.ParseBytes(c.ToolCalls).Array() {
		if j.toolCalls == nil {
			j.toolCalls = map[int64]*toolCall{}
		}
		idx := tc.Get("index").Int()
		call, ok := j.toolCalls[idx]
		if !ok {
			call = &toolCall{Type: "function"}
			j.toolCalls[idx] = call
		}
		if v := tc.Get("id").String(); v != "" {
			call.ID = v
		}
		if v := tc.Get("type").String(); v != "" {
			call.Type = v
		}
		call.Function.Name += tc.Get("function.name").String()
		call.Function.Arguments += tc.Get("function.arguments").String()
	}
	return nil
}

func (j *jsonReplyWriter) End() error {
	h := j.w.Header()
	copyHeaders(h, j.header)
	h.Set("Content-Type", "application/json")

	if !j.seen && j.errorBody != nil {
		j.w.WriteHeader(http.StatusInternalServerError)
		_, err := j.w.Write(j.errorBody)
		return err
	}

	message := map[string]any{"role": "assistant", "content": string(j.content)}
	if len(j.reasoning) > 0 {
		message["reasoning_content"] = string(j.reasoning)
	}
	if len(j.toolCalls) > 0 {
		idx := make([]int64, 0, len(j.toolCalls))
		for i := range j.toolCalls {
			idx = append(idx, i)
		}
		sort.Slice(idx, func(a, b int) bool { return idx[a] < idx[b] })
		calls := make([]*toolCall, 0, len(idx))
		for _, i := range idx {
			calls = append(calls, j.toolCalls[i])
		}
		message["tool_calls"] = calls
		if len(j.content) == 0 {
			message["content"] = nil
		}
	}

	resp := map[string]any{
		"id":      j.id,
		"object":  "chat.completion",
		"created": j.created,
		"model":   j.model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": j.finishReason,
		}},
	}
	if j.fingerprint != "" {
		resp["system_fingerprint"] = j.fingerprint
	}
	if j.usage != nil {
		resp["usage"] = j.usage
	}
	if j.timings != nil {
		resp["timings"] = j.timings
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	j.w.WriteHeader(http.StatusOK)
	_, err = j.w.Write(out)
	return err
}

// copyHeaders copies upstream response headers that still describe the
// rewritten body.
func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Length", "Content-Encoding", "Transfer-Encoding", "Content-Type":
			continue
		}
		dst[k] = append([]string(nil), vs...)
	}
	if ct := src.Get("Content-Type"); ct != "" && dst.Get("Content-Type") == "" {
		dst.Set("Content-Type", ct)
	}
}
