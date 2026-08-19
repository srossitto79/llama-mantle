package config

import (
	"reflect"
	"testing"
)

func TestBuildCommandString_RoundTripsThroughSanitizeCommand(t *testing.T) {
	cases := [][]string{
		{"llama-server", "--port", "${PORT}", "-m", "/models/model.gguf", "-ngl", "99"},
		{"llama-server", "-m", "/models/some file.gguf", "--alias", "my model"},
		{"llama-server", "--override-kv", `tokenizer.ggml.add_bos_token=bool:false`},
		{"llama-server", "--grammar", `root ::= "yes" | "no"`},
	}

	for _, argv := range cases {
		cmd := BuildCommandString(argv)
		got, err := SanitizeCommand(cmd)
		if err != nil {
			t.Fatalf("SanitizeCommand(%q) failed: %v", cmd, err)
		}
		if !reflect.DeepEqual(got, argv) {
			t.Fatalf("round-trip mismatch:\n  built: %q\n  want:  %#v\n  got:   %#v", cmd, argv, got)
		}
	}
}

func TestBuildCommandString_QuotesWhitespaceAndQuotes(t *testing.T) {
	cmd := BuildCommandString([]string{"-m", "/models/some file.gguf", "--alias", `has "quotes"`})
	if cmd != `-m "/models/some file.gguf" --alias "has \"quotes\""` {
		t.Fatalf("unexpected quoting: %q", cmd)
	}
}
