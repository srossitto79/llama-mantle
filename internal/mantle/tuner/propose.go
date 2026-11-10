package tuner

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mostlygeek/llama-swap/internal/configedit"
)

// NewConfigProposer returns the proposer that writes llama-server entries.
func NewConfigProposer() ConfigProposer { return proposer{} }

type proposer struct{}

var (
	shardSuffixRe = regexp.MustCompile(`-\d{5}-of-\d{5}$`)
	idUnsafeRe    = regexp.MustCompile(`[^a-z0-9.]+`)
)

// EntryID is the id of the entry proposed for a tier.
func EntryID(prefix string, tier int) string {
	return fmt.Sprintf("%s-tuned-station-%dk", prefix, tier/1024)
}

// entryPrefix derives an id prefix from the model file name.
func entryPrefix(req Request) string {
	if req.Name != "" {
		return req.Name
	}
	base := strings.TrimSuffix(filepath.Base(req.Model), ".gguf")
	base = shardSuffixRe.ReplaceAllString(base, "")
	if p := strings.Trim(idUnsafeRe.ReplaceAllString(strings.ToLower(base), "-"), "-"); p != "" {
		return p
	}
	return "model"
}

// quoteArg quotes an argument with whitespace so llama-swap's shell-style
// command split keeps it whole.
func quoteArg(a string) string {
	if strings.ContainsAny(a, " \t") {
		return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	return a
}

// launchArgs are a candidate's llama-server arguments without the port. The
// projector measures exactly what the proposer writes.
func launchArgs(model ModelInfo, c Candidate) []string {
	a := []string{
		"-m", model.Path,
		"-c", strconv.Itoa(c.Tier),
		"-fa", "on",
		"-ctk", "q8_0", "-ctv", "q8_0",
		"--parallel", "1",
	}
	return append(a, c.Args...)
}

func (proposer) Propose(model ModelInfo, c Candidate, req Request) (configedit.Change, error) {
	if c.Blocked != "" {
		return configedit.Change{}, fmt.Errorf("candidate is blocked: %s", c.Blocked)
	}
	if c.Binary == "" {
		return configedit.Change{}, fmt.Errorf("candidate has no backend binary")
	}
	cmd := append([]string{c.Binary, "--port", "${PORT}"}, launchArgs(model, c)...)
	for i := range cmd {
		cmd[i] = quoteArg(cmd[i])
	}
	prefix := entryPrefix(req)
	return configedit.Change{
		Op:      "add",
		Section: "models",
		ID:      EntryID(prefix, c.Tier),
		Value: map[string]any{
			"name":        fmt.Sprintf("%s %dK tuned", prefix, c.Tier/1024),
			"description": fmt.Sprintf("%s, %s", c.Backend, c.Reason),
			"cmd":         strings.Join(cmd, " "),
			"proxy":       "http://127.0.0.1:${PORT}",
		},
	}, nil
}
