package mantle

import (
	"os"
	"testing"
)

func findFlag(t *testing.T, flags []FlagSpec, name string) FlagSpec {
	t.Helper()
	for _, f := range flags {
		for _, n := range f.Names {
			if n == name {
				return f
			}
		}
	}
	t.Fatalf("flag %q not found", name)
	return FlagSpec{}
}

func TestParseHelpText_SplitsOnLastSpaceRun(t *testing.T) {
	// "-h,    --help, --usage" has an internal 2+-space run (alignment padding
	// between the short and long aliases) that must not be mistaken for the
	// flag/description boundary; only the LAST such run, before "print usage
	// and exit", is the real boundary.
	flags := ParseHelpText("-h,    --help, --usage                  print usage and exit\n")
	f := findFlag(t, flags, "-h")
	if len(f.Names) != 3 || f.Names[1] != "--help" || f.Names[2] != "--usage" {
		t.Fatalf("expected aliases [-h --help --usage], got %v", f.Names)
	}
	if f.Help != "print usage and exit" {
		t.Fatalf("expected help %q, got %q", "print usage and exit", f.Help)
	}
	if f.Type != "boolean" {
		t.Fatalf("expected type boolean, got %q", f.Type)
	}
}

func TestParseHelpText_BracketAwareAliasSplit(t *testing.T) {
	// The comma inside "<dev1,dev2,..>" must not be treated as an alias
	// separator between "-dev" and "--device <dev1,dev2,..>".
	flags := ParseHelpText("-dev,  --device <dev1,dev2,..>          comma-separated list of devices\n")
	f := findFlag(t, flags, "-dev")
	if len(f.Names) != 2 || f.Names[0] != "-dev" || f.Names[1] != "--device" {
		t.Fatalf("expected aliases [-dev --device], got %v", f.Names)
	}
	if f.Value != "<dev1,dev2,..>" {
		t.Fatalf("expected value %q, got %q", "<dev1,dev2,..>", f.Value)
	}
	// contains ".." so it must not be misread as a fixed enum choice list
	if f.Type == "enum" {
		t.Fatalf("expected non-enum type for free-form device list, got enum with choices %v", f.Choices)
	}
}

func TestParseHelpText_HandlesOverflowingFlagLine(t *testing.T) {
	// The alias list itself is too long to leave room for a description on
	// the same line, so the description starts on the next physical line.
	text := "--spec-draft-hf, -hfd, -hfrd, --hf-repo-draft <user>/<model>[:quant]\n" +
		"                                        Same as --hf-repo, but for the draft model (default: unused)\n" +
		"                                        (env: LLAMA_ARG_SPEC_DRAFT_HF_REPO)\n"
	flags := ParseHelpText(text)
	f := findFlag(t, flags, "--spec-draft-hf")
	if len(f.Names) != 4 {
		t.Fatalf("expected 4 aliases, got %v", f.Names)
	}
	if f.CanonicalEnv != "LLAMA_ARG_SPEC_DRAFT_HF_REPO" {
		t.Fatalf("expected env LLAMA_ARG_SPEC_DRAFT_HF_REPO, got %q", f.CanonicalEnv)
	}
	if f.Default != "unused" {
		t.Fatalf("expected default %q, got %q", "unused", f.Default)
	}
}

func TestParseHelpText_ClassifiesTypes(t *testing.T) {
	text := "--flash-attn [on|off|auto]       set Flash Attention use\n" +
		"--ctx-size N                     size of the prompt context (default: 0)\n" +
		"--lora FNAME                     path to LoRA adapter\n" +
		"--cont-batching, --no-cont-batching   whether to enable continuous batching\n"
	flags := ParseHelpText(text)

	fa := findFlag(t, flags, "--flash-attn")
	if fa.Type != "enum" {
		t.Fatalf("expected --flash-attn type enum, got %q", fa.Type)
	}
	if len(fa.Choices) != 3 || fa.Choices[0] != "on" || fa.Choices[2] != "auto" {
		t.Fatalf("expected choices [on off auto], got %v", fa.Choices)
	}

	ctx := findFlag(t, flags, "--ctx-size")
	if ctx.Type != "number" {
		t.Fatalf("expected --ctx-size type number, got %q", ctx.Type)
	}

	lora := findFlag(t, flags, "--lora")
	if lora.Type != "path" {
		t.Fatalf("expected --lora type path, got %q", lora.Type)
	}

	cb := findFlag(t, flags, "--cont-batching")
	if cb.Type != "boolean" {
		t.Fatalf("expected --cont-batching type boolean, got %q", cb.Type)
	}
	if len(cb.Names) != 2 || cb.Names[1] != "--no-cont-batching" {
		t.Fatalf("expected toggle pair aliases, got %v", cb.Names)
	}
}

func TestParseHelpText_ExtractsDefaultAndEnv(t *testing.T) {
	text := "-ngl,  --gpu-layers, --n-gpu-layers N   max. number of layers to store in VRAM, either an exact number,\n" +
		"                                        'auto', or 'all' (default: auto)\n" +
		"                                        (env: LLAMA_ARG_N_GPU_LAYERS)\n"
	flags := ParseHelpText(text)
	f := findFlag(t, flags, "-ngl")
	if f.Default != "auto" {
		t.Fatalf("expected default %q, got %q", "auto", f.Default)
	}
	if f.CanonicalEnv != "LLAMA_ARG_N_GPU_LAYERS" {
		t.Fatalf("expected env LLAMA_ARG_N_GPU_LAYERS, got %q", f.CanonicalEnv)
	}
}

func TestParseHelpText_AllowedValuesLine(t *testing.T) {
	text := "-ctk,  --cache-type-k TYPE              KV cache data type for K\n" +
		"                                        allowed values: f32, f16, bf16, q8_0, q4_0, q4_1, iq4_nl, q5_0, q5_1\n" +
		"                                        (default: f16)\n" +
		"                                        (env: LLAMA_ARG_CACHE_TYPE_K)\n"
	flags := ParseHelpText(text)
	f := findFlag(t, flags, "-ctk")
	if f.Type != "enum" {
		t.Fatalf("expected type enum, got %q", f.Type)
	}
	if len(f.Choices) != 9 || f.Choices[0] != "f32" || f.Choices[8] != "q5_1" {
		t.Fatalf("expected 9 choices starting f32 ending q5_1, got %v", f.Choices)
	}
}

func TestParseHelpText_SectionGrouping(t *testing.T) {
	text := "----- common params -----\n" +
		"--foo N                          a common flag\n" +
		"\n" +
		"----- sampling params -----\n" +
		"--bar N                          a sampling flag\n"
	flags := ParseHelpText(text)
	foo := findFlag(t, flags, "--foo")
	bar := findFlag(t, flags, "--bar")
	if foo.Section != "common params" {
		t.Fatalf("expected section %q, got %q", "common params", foo.Section)
	}
	if bar.Section != "sampling params" {
		t.Fatalf("expected section %q, got %q", "sampling params", bar.Section)
	}
}

// TestParseHelpText_RealCapturedOutput runs the parser against the real,
// unmodified `llama-server --help` output captured from the ggml-org backend
// installed in this repo's dev container, to make sure the fixture used in
// the targeted tests above is representative of actual upstream formatting
// (not just what the parser is expected to handle).
func TestParseHelpText_RealCapturedOutput(t *testing.T) {
	data, err := os.ReadFile("testdata/llama-server-help-ggml-org.txt")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}
	flags := ParseHelpText(string(data))
	if len(flags) < 200 {
		t.Fatalf("expected at least 200 parsed flags from real --help output, got %d", len(flags))
	}

	help := findFlag(t, flags, "-h")
	if len(help.Names) != 3 {
		t.Fatalf("expected -h to have 3 aliases, got %v", help.Names)
	}

	ngl := findFlag(t, flags, "-ngl")
	if ngl.CanonicalEnv != "LLAMA_ARG_N_GPU_LAYERS" {
		t.Fatalf("expected -ngl env LLAMA_ARG_N_GPU_LAYERS, got %+v", ngl)
	}
	if ngl.Type != "number" {
		t.Fatalf("expected -ngl type number, got %q (value %q)", ngl.Type, ngl.Value)
	}

	device := findFlag(t, flags, "-dev")
	if device.Type == "enum" {
		t.Fatalf("expected -dev not to be classified as enum, got choices %v", device.Choices)
	}

	specDraft := findFlag(t, flags, "--spec-draft-hf")
	if specDraft.CanonicalEnv != "LLAMA_ARG_SPEC_DRAFT_HF_REPO" {
		t.Fatalf("expected --spec-draft-hf env, got %+v", specDraft)
	}

	// No flag should end up with an empty Names slice or a stray leading
	// dash swallowed into the help text.
	for _, f := range flags {
		if len(f.Names) == 0 {
			t.Fatalf("found flag with no names: %+v", f)
		}
	}
}
