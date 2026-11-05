package loopguard

import (
	"strings"

	"github.com/tidwall/sjson"
)

// echoFilter drops a continuation's repeat of its prefilled reasoning. The
// chat template may trim the prefill, so the trimmed forms also count. When
// the upstream does not echo, its reasoning passes through unchanged.
type echoFilter struct {
	candidates []string // longest first
	held       strings.Builder
	done       bool
}

func newEchoFilter(echo string) *echoFilter {
	f := &echoFilter{}
	for _, c := range []string{echo, strings.TrimRight(echo, " \t\r\n"), strings.TrimSpace(echo)} {
		if c != "" && (len(f.candidates) == 0 || f.candidates[len(f.candidates)-1] != c) {
			f.candidates = append(f.candidates, c)
		}
	}
	f.done = len(f.candidates) == 0
	return f
}

// take returns the part of a reasoning delta to show the client.
func (f *echoFilter) take(delta string) string {
	if f.done {
		return delta
	}
	f.held.WriteString(delta)
	h := f.held.String()
	for _, c := range f.candidates {
		if len(h) < len(c) && strings.HasPrefix(c, h) {
			return "" // may still be the echo
		}
	}
	f.done = true
	for _, c := range f.candidates {
		if strings.HasPrefix(h, c) {
			return h[len(c):]
		}
	}
	return h
}

// apply filters a continuation chunk. drop is true when nothing is left to
// send.
func (f *echoFilter) apply(c Chunk) (out Chunk, drop bool) {
	if c.Reasoning == "" {
		// Anything else ends the echo; text still held was a partial echo.
		f.done = true
		return c, false
	}
	shown := f.take(c.Reasoning)
	if shown == c.Reasoning {
		return c, false
	}
	c.Reasoning = shown
	path := "choices.0.delta." + c.ReasoningKey
	if shown == "" {
		c.Raw, _ = sjson.DeleteBytes(c.Raw, path)
	} else {
		c.Raw, _ = sjson.SetBytes(c.Raw, path, shown)
	}
	empty := c.Reasoning == "" && c.Content == "" && c.ToolCalls == nil &&
		c.FinishReason == "" && c.Usage == nil && c.Timings == nil
	return c, empty
}
