package tuner

import (
	"strings"
	"testing"
)

func TestTuner_EntryID(t *testing.T) {
	if got := EntryID("qwen", 131072); got != "qwen-tuned-station-128k" {
		t.Fatalf("got %q", got)
	}
	if got := EntryID("qwen", 65536); got != "qwen-tuned-station-64k" {
		t.Fatalf("got %q", got)
	}
}

func TestTuner_EntryPrefix(t *testing.T) {
	cases := []struct {
		req  Request
		want string
	}{
		{Request{Model: "/models/Qwen3-Next-80B-A3B-00001-of-00002.gguf"}, "qwen3-next-80b-a3b"},
		{Request{Model: "/models/Foo_Bar-Q4_K_M.gguf"}, "foo-bar-q4-k-m"},
		{Request{Model: "/models/plain.gguf"}, "plain"},
		{Request{Model: "/models/x-00001-of-00002.gguf", Name: "custom"}, "custom"},
	}
	for _, c := range cases {
		if got := entryPrefix(c.req); got != c.want {
			t.Errorf("entryPrefix(%+v) = %q, want %q", c.req, got, c.want)
		}
	}
}

func TestTuner_ProposeBuildsEntry(t *testing.T) {
	model := ModelInfo{Path: "/models/qwen/Station-00001-of-00002.gguf"}
	c := Candidate{
		Tier:    131072,
		Backend: "ggml-org",
		Binary:  "/backends/ggml-org/llama-server",
		Args:    []string{"-ngl", "99", "--moe-cache-mib", "11264"},
		Reason:  "Experts in host RAM",
	}
	req := Request{Model: model.Path, Name: "station"}

	ch, err := NewConfigProposer().Propose(model, c, req)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != "add" || ch.Section != "models" {
		t.Fatalf("op/section = %q/%q", ch.Op, ch.Section)
	}
	if ch.ID != "station-tuned-station-128k" {
		t.Fatalf("id = %q", ch.ID)
	}
	val, ok := ch.Value.(map[string]any)
	if !ok {
		t.Fatalf("value is %T", ch.Value)
	}
	cmd, _ := val["cmd"].(string)
	for _, want := range []string{
		"/backends/ggml-org/llama-server",
		"--port ${PORT}",
		"-m " + model.Path,
		"-c 131072",
		"--parallel 1",
		"-ngl 99 --moe-cache-mib 11264",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("cmd lacks %q: %s", want, cmd)
		}
	}
	if !strings.HasPrefix(cmd, "/backends/ggml-org/llama-server ") {
		t.Errorf("cmd does not start with the binary: %s", cmd)
	}
	if val["proxy"] != "http://127.0.0.1:${PORT}" {
		t.Errorf("proxy = %v", val["proxy"])
	}
	if name, _ := val["name"].(string); name != "station 128K tuned" {
		t.Errorf("name = %q", name)
	}
}

func TestTuner_ProposeDerivedPrefix(t *testing.T) {
	model := ModelInfo{Path: "/models/Qwen3-Next-80B-A3B-00001-of-00002.gguf"}
	c := Candidate{Tier: 131072, Backend: "b", Binary: "/b/llama-server"}
	ch, err := NewConfigProposer().Propose(model, c, Request{Model: model.Path})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != "qwen3-next-80b-a3b-tuned-station-128k" {
		t.Fatalf("id = %q", ch.ID)
	}
}

func TestTuner_ProposeRejectsBlocked(t *testing.T) {
	c := Candidate{Tier: 65536, Binary: "/b/llama-server", Blocked: "MTP file has no token_embd"}
	_, err := NewConfigProposer().Propose(ModelInfo{Path: "/m/x.gguf"}, c, Request{Model: "/m/x.gguf"})
	if err == nil || !strings.Contains(err.Error(), "MTP file has no token_embd") {
		t.Fatalf("err = %v", err)
	}
}

func TestTuner_ProposeRejectsMissingBinary(t *testing.T) {
	_, err := NewConfigProposer().Propose(ModelInfo{Path: "/m/x.gguf"}, Candidate{Tier: 65536}, Request{Model: "/m/x.gguf"})
	if err == nil {
		t.Fatal("expected an error for a candidate without a binary")
	}
}
