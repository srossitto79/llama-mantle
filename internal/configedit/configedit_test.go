package configedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
)

const testSecret = "sk-env-secret-0123456789"

func loadConfig(raw []byte) (config.Config, error) {
	return config.LoadConfigFromReader(bytes.NewReader(raw))
}

func fixture(t *testing.T) []byte {
	t.Helper()
	t.Setenv("TEST_CONFIGEDIT_KEY", testSecret)
	raw, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(raw); err != nil {
		t.Fatalf("fixture must be a valid config: %v", err)
	}
	return raw
}

func newPlanner() Planner { return NewPlanner(NewValidator(loadConfig)) }

func plan(t *testing.T, raw []byte, changes ...Change) Plan {
	t.Helper()
	p, err := newPlanner().Plan(raw, Hash(raw), changes)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(p.Problems) > 0 {
		t.Fatalf("plan result has problems: %+v", p.Problems)
	}
	return p
}

// changedLines returns lines removed from a and added in b, by a simple
// multiset comparison; enough to assert edits stay local.
func changedLines(a, b string) (removed, added []string) {
	count := map[string]int{}
	for _, l := range strings.Split(a, "\n") {
		count[l]++
	}
	for _, l := range strings.Split(b, "\n") {
		if count[l] > 0 {
			count[l]--
		} else {
			added = append(added, l)
		}
	}
	for l, n := range count {
		for ; n > 0; n-- {
			removed = append(removed, l)
		}
	}
	return removed, added
}

func assertNoSecrets(t *testing.T, what string, s string) {
	t.Helper()
	if strings.Contains(s, testSecret) {
		t.Fatalf("%s contains the env secret", what)
	}
}

func TestConfigEdit_SourceKeepsReferences(t *testing.T) {
	raw := fixture(t)
	doc, err := NewSourceReader().ReadSource(raw)
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]SourceEntry{}
	for _, e := range doc.Sections[SectionModels] {
		models[e.ID] = e
	}
	qwen := models["qwen-8b"]
	if cmd, _ := qwen.Fields["cmd"].Value.(string); !strings.Contains(cmd, "${models_dir}") {
		t.Errorf("cmd should keep macro reference, got %q", cmd)
	}
	if got := qwen.Fields["filters"].Alias; got != "qwen38-effort-with-high" {
		t.Errorf("filters alias = %q", got)
	}
	strata := models["strata-ds"]
	if got := strata.Fields["apiKey"]; got.Value != "${env.TEST_CONFIGEDIT_KEY}" || got.Redacted {
		t.Errorf("env reference apiKey should be shown as written, got %+v", got)
	}
	lit := models["literal-key"]
	if got := lit.Fields["apiKey"]; got.Value != Redacted || !got.Redacted {
		t.Errorf("literal apiKey should be redacted, got %+v", got)
	}
	if env := lit.Fields["env"].Value.([]any); env[0] != "HF_TOKEN="+Redacted {
		t.Errorf("literal env secret should be redacted, got %v", env)
	}
	if len(doc.Macros) != 3 || doc.Macros[0].Name != "models_dir" {
		t.Errorf("macros = %+v", doc.Macros)
	}
	b, _ := json.Marshal(doc)
	assertNoSecrets(t, "source view", string(b))
	for _, lit := range []string{"sk-literal-fixture-value", "hf-literal-fixture-value"} {
		if strings.Contains(string(b), lit) {
			t.Errorf("source view contains literal secret %q", lit)
		}
	}
}

func TestConfigEdit_EffectiveHasNoSecrets(t *testing.T) {
	raw := fixture(t)
	models, err := NewEffectiveReader(loadConfig).EffectiveModels(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(models)
	assertNoSecrets(t, "effective view", string(b))
	if strings.Contains(string(b), "hf-literal-fixture-value") {
		t.Error("effective view contains literal env secret")
	}
	if cmd := models["qwen-8b"]["cmd"].(string); !strings.Contains(cmd, "/models/qwen-8b.gguf") {
		t.Errorf("effective cmd should be expanded, got %q", cmd)
	}
	env := models["strata-ds"]["env"].([]any)
	if env[0] != "STRATA_API_KEY=${env.TEST_CONFIGEDIT_KEY}" {
		t.Errorf("env reference should stay a reference, got %v", env[0])
	}
	if _, ok := models["strata-ds"]["apiKey"]; ok {
		t.Error("apiKey must not appear in the effective view")
	}
}

func TestConfigEdit_NoChangeIsByteIdentical(t *testing.T) {
	raw := fixture(t)
	if p := plan(t, raw); p.YAML != string(raw) {
		t.Fatal("empty plan changed the file")
	}
	same := plan(t, raw, Change{Op: "set", Section: SectionModels, ID: "qwen-8b", Path: []string{"ttl"}, Value: 600})
	if same.YAML != string(raw) {
		t.Fatal("setting a field to its current value changed the file")
	}
	// Re-posting the aliased filters with the same content keeps the alias.
	filters := map[string]any{"setParamsByID": map[string]any{
		"${MODEL_ID}:low":  map[string]any{"reasoning_effort": "low"},
		"${MODEL_ID}:high": map[string]any{"reasoning_effort": "high"},
	}}
	same = plan(t, raw, Change{Op: "set", Section: SectionModels, ID: "qwen-8b", Path: []string{"filters"}, Value: filters})
	if same.YAML != string(raw) {
		t.Fatal("re-posting an unchanged aliased field changed the file")
	}
}

func TestConfigEdit_SetOneFieldChangesOnlyThatLine(t *testing.T) {
	raw := fixture(t)
	p := plan(t, raw, Change{Op: "set", Section: SectionModels, ID: "qwen-8b", Path: []string{"ttl"}, Value: 1200})
	removed, added := changedLines(string(raw), p.YAML)
	if len(removed) != 1 || len(added) != 1 {
		t.Fatalf("want one line changed, removed=%q added=%q", removed, added)
	}
	if strings.TrimSpace(added[0]) != "ttl: 1200 # keep warm" {
		t.Errorf("line comment lost: %q", added[0])
	}
}

func TestConfigEdit_EditMultilineCmdKeepsMacrosElsewhere(t *testing.T) {
	raw := fixture(t)
	cmd := "${server} --port ${PORT}\n-m ${models_dir}/qwen-8b.gguf\n--fit-margin ${fit_margin}\n--ctx-size 8192\n"
	p := plan(t, raw, Change{Op: "set", Section: SectionModels, ID: "qwen-8b", Path: []string{"cmd"}, Value: cmd})
	removed, added := changedLines(string(raw), p.YAML)
	if len(removed) != 0 || len(added) != 1 || strings.TrimSpace(added[0]) != "--ctx-size 8192" {
		t.Fatalf("want only the new cmd line added, removed=%q added=%q", removed, added)
	}
	if !strings.Contains(p.YAML, "cmd: |") {
		t.Error("literal block style lost")
	}
}

func TestConfigEdit_AliasedFieldNeedsDetach(t *testing.T) {
	raw := fixture(t)
	change := Change{Op: "set", Section: SectionModels, ID: "qwen-8b", Path: []string{"filters", "setParamsByID", "${MODEL_ID}:low", "reasoning_effort"}, Value: "minimal"}
	_, err := newPlanner().Plan(raw, "", []Change{change})
	var ce *ChangeError
	if !errors.As(err, &ce) || ce.Code != CodeAliased || ce.Anchor != "qwen38-effort-with-high" {
		t.Fatalf("want aliased error naming the anchor, got %v", err)
	}
	change.Detach = true
	p := plan(t, raw, change)
	if strings.Count(p.YAML, "filters: *qwen38-effort-with-high") != 1 {
		t.Error("the other model must keep the alias")
	}
	if !strings.Contains(p.YAML, "&qwen38-effort-with-high") {
		t.Error("anchor definition must stay")
	}
	cfg, err := loadConfig([]byte(p.YAML))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Models["qwen-8b"].Filters.SetParamsByID["qwen-8b:low"]["reasoning_effort"]; got != "minimal" {
		t.Errorf("detached value = %v", got)
	}
	if got := cfg.Models["gemma-4b"].Filters.SetParamsByID["gemma-4b:low"]["reasoning_effort"]; got != "low" {
		t.Errorf("anchor value changed for gemma: %v", got)
	}
}

func TestConfigEdit_RenameUpdatesReferences(t *testing.T) {
	raw := fixture(t)
	p := plan(t, raw, Change{Op: "rename", Section: SectionModels, ID: "qwen-8b", NewID: "qwen-9b"})
	if strings.Contains(p.YAML, "qwen-8b:") {
		t.Error("old key still present")
	}
	cfg, err := loadConfig([]byte(p.YAML))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Models["qwen-9b"]; !ok {
		t.Fatal("renamed model missing")
	}
	if m := cfg.Groups["main"].Members; m[0] != "qwen-9b" {
		t.Errorf("group members not updated: %v", m)
	}
	if pl := cfg.Hooks.OnStartup.Preload; pl[0] != "qwen-9b" {
		t.Errorf("preload not updated: %v", pl)
	}
	if !strings.Contains(p.YAML, "# Qwen 8B, main local model.") || !strings.Contains(p.YAML, "members: *shared-members") {
		t.Error("comment or alias lost on rename")
	}
	if strings.Count(p.YAML, "\n") != strings.Count(string(raw), "\n") {
		t.Error("rename should not change the line count")
	}
}

func TestConfigEdit_DeleteRemovesReferences(t *testing.T) {
	raw := fixture(t)
	p := plan(t, raw, Change{Op: "delete", Section: SectionModels, ID: "gemma-4b"})
	cfg, err := loadConfig([]byte(p.YAML))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Models["gemma-4b"]; ok {
		t.Fatal("model still present")
	}
	if len(cfg.Groups["solo"].Members) != 0 {
		t.Errorf("group members not cleaned: %v", cfg.Groups["solo"].Members)
	}
	if !strings.Contains(p.YAML, "# Remote engine; key comes from the environment.") {
		t.Error("the next model's comment was removed")
	}
}

func TestConfigEdit_DuplicateIDsRefused(t *testing.T) {
	raw := fixture(t)
	cases := []Change{
		{Op: "add", Section: SectionModels, ID: "qwen", Value: map[string]any{"cmd": "x"}},
		{Op: "add", Section: SectionModels, ID: "gemma-4b", Value: map[string]any{"cmd": "x"}},
		{Op: "rename", Section: SectionModels, ID: "qwen-8b", NewID: "gemma-4b"},
		{Op: "rename", Section: SectionGroups, ID: "main", NewID: "solo"},
	}
	for _, c := range cases {
		_, err := newPlanner().Plan(raw, "", []Change{c})
		var ce *ChangeError
		if !errors.As(err, &ce) || ce.Code != CodeExists {
			t.Errorf("%s %s -> %s%s: want exists error, got %v", c.Op, c.ID, c.NewID, c.ID, err)
		}
	}
}

func TestConfigEdit_AddModelAndGroupAtomically(t *testing.T) {
	raw := fixture(t)
	p := plan(t, raw,
		Change{Op: "add", Section: SectionModels, ID: "phi-3", Value: map[string]any{"cmd": "${server} --port ${PORT} -m ${models_dir}/phi.gguf", "ttl": 60}},
		Change{Op: "set", Section: SectionGroups, ID: "solo", Path: []string{"members"}, Value: []any{"gemma-4b", "phi-3"}},
	)
	removed, added := changedLines(string(raw), p.YAML)
	if len(removed) != 0 {
		t.Errorf("adding should not remove lines, removed=%q", removed)
	}
	if len(added) != 4 {
		t.Errorf("want 4 added lines (key, cmd, ttl, member), got %q", added)
	}
	if !strings.Contains(p.YAML, "${models_dir}/phi.gguf") {
		t.Error("macro reference not kept in new model")
	}
}

func TestConfigEdit_RedactedNeverWritten(t *testing.T) {
	raw := fixture(t)
	p := plan(t, raw,
		Change{Op: "set", Section: SectionModels, ID: "literal-key", Path: []string{"apiKey"}, Value: Redacted},
		Change{Op: "set", Section: SectionModels, ID: "literal-key", Path: []string{"env"}, Value: []any{"HF_TOKEN=" + Redacted, "NEW_VAR=1"}},
	)
	if strings.Contains(p.YAML, Redacted) {
		t.Fatal("redaction marker written to the file")
	}
	if !strings.Contains(p.YAML, "sk-literal-fixture-value") || !strings.Contains(p.YAML, "hf-literal-fixture-value") {
		t.Fatal("redacted values must keep their source text")
	}
	if !strings.Contains(p.YAML, "NEW_VAR=1") {
		t.Fatal("new env entry missing")
	}
	_, err := newPlanner().Plan(raw, "", []Change{{Op: "set", Section: SectionModels, ID: "qwen-8b", Path: []string{"apiKey"}, Value: Redacted}})
	var ce *ChangeError
	if !errors.As(err, &ce) || ce.Code != CodeRedacted {
		t.Fatalf("writing the marker to a new field must fail, got %v", err)
	}
}

func TestConfigEdit_UpdateFromEffectiveKeepsSource(t *testing.T) {
	raw := fixture(t)
	models, err := NewEffectiveReader(loadConfig).EffectiveModels(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"strata-ds", "qwen-8b"} {
		current := models[id]
		posted := map[string]any{}
		for k, v := range current {
			posted[k] = v
		}
		posted["ttl"] = 42.0
		changes := ChangesForModelUpdate(id, current, posted)
		if len(changes) != 1 || changes[0].Path[0] != "ttl" {
			t.Fatalf("%s: want one ttl change, got %+v", id, changes)
		}
		p := plan(t, raw, changes...)
		assertNoSecrets(t, "written file", p.YAML)
		if !strings.Contains(p.YAML, "apiKey: ${env.TEST_CONFIGEDIT_KEY}") || !strings.Contains(p.YAML, "${models_dir}/qwen-8b.gguf") {
			t.Fatalf("%s: source references lost", id)
		}
	}
}

func TestConfigEdit_ValidateReportsPositions(t *testing.T) {
	v := NewValidator(loadConfig)
	syntax := v.Validate([]byte("models:\n  a:\n    cmd: x\n   bad: [\n"))
	if len(syntax) != 1 || syntax[0].Line == 0 {
		t.Fatalf("syntax error needs a line, got %+v", syntax)
	}
	unknown := v.Validate([]byte("models:\n  first:\n    cmd: a ${PORT}\n  second-model:\n    cmd: run ${PORT} ${nope}\n"))
	if len(unknown) != 1 || unknown[0].Line != 4 {
		t.Fatalf("unknown macro should point at the model, got %+v", unknown)
	}
	missing := v.Validate([]byte("models:\n  a:\n    cmd: x ${PORT}\n    apiKey: ${env.TEST_CONFIGEDIT_MISSING}\n"))
	if len(missing) != 1 || missing[0].Line != 4 || !strings.Contains(missing[0].Message, "TEST_CONFIGEDIT_MISSING") {
		t.Fatalf("missing env var, got %+v", missing)
	}
	t.Setenv("TEST_CONFIGEDIT_KEY", testSecret)
	typeErr := v.Validate([]byte("models:\n  a:\n    cmd: x ${PORT}\n    ttl: ${env.TEST_CONFIGEDIT_KEY}\n"))
	if len(typeErr) != 1 {
		t.Fatalf("want a type error, got %+v", typeErr)
	}
	assertNoSecrets(t, "validation message", typeErr[0].Message)
}

func TestConfigEdit_KeepsCRLF(t *testing.T) {
	raw := bytes.ReplaceAll(fixture(t), []byte("\n"), []byte("\r\n"))
	p := plan(t, raw, Change{Op: "set", Section: SectionModels, ID: "gemma-4b", Path: []string{"ttl"}, Value: 30})
	if strings.Count(p.YAML, "\r\n") != strings.Count(p.YAML, "\n") {
		t.Fatal("line endings changed")
	}
}

func TestConfigEdit_StaleBaseRefused(t *testing.T) {
	raw := fixture(t)
	_, err := newPlanner().Plan(raw, "not-the-hash", nil)
	var ce *ChangeError
	if !errors.As(err, &ce) || ce.Code != CodeStale {
		t.Fatalf("want stale error, got %v", err)
	}
}

func TestConfigEdit_MacrosAndRootSettings(t *testing.T) {
	raw := fixture(t)
	p := plan(t, raw,
		Change{Op: "set", Section: SectionMacros, ID: "fit_margin", Value: 2048},
		Change{Op: "add", Section: SectionMacros, ID: "ctx", Value: 8192},
		Change{Op: "set", Section: SectionRoot, ID: "globalTTL", Value: 0},
	)
	removed, added := changedLines(string(raw), p.YAML)
	if len(removed) != 2 || len(added) != 3 {
		t.Fatalf("removed=%q added=%q", removed, added)
	}
}
