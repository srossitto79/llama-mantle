// Package loopguard stops a chat completion whose reasoning starts repeating
// whole passages and continues the same reply in a way that breaks the loop.
// The client sees one normal response, streamed or not.
//
// The detection follows Strata's reasoning_repeat_coverage (server.py,
// reasoning_loop_recovery); the recovery continues the reply through the
// upstream's own chat endpoint with the reasoning so far as an assistant
// prefill.
package loopguard

import "net/http"

// LoopDetector watches reasoning text for repeated passages. It is fed the
// reasoning incrementally and asked at check points; it does no I/O.
type LoopDetector interface {
	// Feed appends reasoning text.
	Feed(text string)
	// Check reports whether the reasoning fed so far is looping and the
	// coverage it measured (0..1).
	Check() (looping bool, coverage float64)
}

// Continuation is the upstream request that resumes a reply, and how to read
// its response.
type Continuation struct {
	// Body is the JSON chat-completion request body.
	Body []byte
	// Lead is reasoning the client has not seen yet that the continuation
	// adds before the model's new tokens (a wrap-up sentence). The guard sends
	// it to the client itself.
	Lead string
	// Echo is reasoning the upstream may repeat at the start of its reply
	// (llama.cpp re-sends an assistant prefill in its first delta). The guard
	// drops it when it is there.
	Echo string
}

// ReplyContinuer builds the request that continues a reply cut short inside
// its reasoning. One implementation per continuation strategy.
type ReplyContinuer interface {
	// Continue returns the continuation of the reply that original started,
	// given the reasoning generated so far and its approximate token count.
	Continue(original []byte, reasoning string, generatedTokens int) (Continuation, error)
}

// Chunk is one chat.completion.chunk event, already rewritten to the
// client-facing id, model and created values.
type Chunk struct {
	// Raw is the JSON payload of the event.
	Raw []byte
	// Fields parsed from Raw (choice 0 only; the guard skips n > 1).
	Reasoning    string
	Content      string
	ToolCalls    []byte // raw JSON array of tool call deltas, nil when absent
	FinishReason string
	Usage        []byte // raw JSON usage object, nil when absent
	Timings      []byte // raw JSON llama.cpp timings object, nil when absent
}

// ReplyWriter writes the client-facing reply. Streaming clients get SSE,
// others one assembled JSON response.
type ReplyWriter interface {
	// Begin sends the response headers, taken from the first upstream.
	Begin(header http.Header)
	// Event forwards an SSE event that is not a completion chunk (llama-swap's
	// loading state, comments, upstream error events), raw block included.
	Event(block []byte) error
	// Chunk writes one completion chunk.
	Chunk(c Chunk) error
	// End finishes the reply.
	End() error
}

// Logger receives one line per guard action.
type Logger interface {
	Infof(format string, args ...any)
}
