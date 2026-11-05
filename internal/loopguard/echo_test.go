package loopguard

import (
	"strings"
	"testing"
)

func TestLoopGuard_EchoFilter(t *testing.T) {
	tests := []struct {
		name   string
		echo   string
		deltas []string
		want   string
	}{
		{"echo in first delta", "abc def", []string{"abc def the", " end"}, " the end"},
		{"echo split across deltas", "abc def", []string{"abc", " de", "f!", " ok"}, "! ok"},
		{"trimmed echo", "abc def \n\n", []string{"abc def", " next"}, " next"},
		{"no echo", "abc def", []string{"xyz", " abc"}, "xyz abc"},
		{"diverges inside the echo", "abc def", []string{"abc", " zz"}, "abc zz"},
		{"empty echo", "", []string{"a", "b"}, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEchoFilter(tt.echo)
			var got strings.Builder
			for _, d := range tt.deltas {
				got.WriteString(f.take(d))
			}
			if got.String() != tt.want {
				t.Errorf("shown %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestLoopGuard_EchoFilterChunks(t *testing.T) {
	f := newEchoFilter("abc")
	c, _ := parseChunk([]byte(`{"id":"x","choices":[{"index":0,"delta":{"reasoning_content":"abc"}}]}`))
	if _, drop := f.apply(c); !drop {
		t.Fatal("chunk holding only the echo should be dropped")
	}
	c, _ = parseChunk([]byte(`{"id":"x","choices":[{"index":0,"delta":{"content":"hi"}}]}`))
	if out, drop := f.apply(c); drop || out.Content != "hi" {
		t.Fatal("content chunk should pass")
	}
	c, _ = parseChunk([]byte(`{"id":"x","choices":[{"index":0,"delta":{"reasoning_content":"abc"}}]}`))
	if out, drop := f.apply(c); drop || out.Reasoning != "abc" {
		t.Fatal("after the echo ends, reasoning passes unchanged")
	}
}
