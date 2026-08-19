package mantle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// FlagSpec describes a single command-line flag extracted from a backend's
// `--help` output. Type is a best-effort classification of the flag's value
// placeholder ("boolean", "enum", "number", "path", "string") that a UI can
// use to pick a form control; when the classifier isn't confident it falls
// back to "string" rather than guessing wrong, so unusual/fork-specific
// flags always round-trip as plain text instead of blocking the form.
type FlagSpec struct {
	Names        []string `json:"names"`
	CanonicalEnv string   `json:"canonicalEnv,omitempty"`
	Value        string   `json:"value,omitempty"`
	Type         string   `json:"type"`
	Choices      []string `json:"choices,omitempty"`
	Default      string   `json:"default,omitempty"`
	Help         string   `json:"help"`
	Section      string   `json:"section"`
}

// BackendSchema is the parsed flag schema for one backend binary.
type BackendSchema struct {
	Backend  string     `json:"backend"`
	ParsedAt time.Time  `json:"parsedAt"`
	Flags    []FlagSpec `json:"flags"`
}

var (
	sectionHeaderRe = regexp.MustCompile(`^-{2,}\s+(.+?)\s+-{2,}$`)
	gapRe           = regexp.MustCompile(`[ \t]{2,}`)
	aliasRe         = regexp.MustCompile(`^(-{1,2}[A-Za-z][A-Za-z0-9-]*)(?:\s+(.+))?$`)
	envRe           = regexp.MustCompile(`\(env:\s*([A-Za-z0-9_]+)\)`)
	defaultRe       = regexp.MustCompile(`\(default:\s*([^)]+)\)`)
	allowedValuesRe = regexp.MustCompile(`allowed values:\s*([A-Za-z0-9_,\s]+?)(?:\s*\(|\.|$)`)
	numericRangeRe  = regexp.MustCompile(`^-?\d+(\.\.\.|-)-?\d+$`)
)

// ParseHelpText parses llama.cpp-style `--help` output (shared by upstream
// llama.cpp and every fork observed so far — forks only append new flags to
// the same formatter, they don't reformat it) into a flat list of FlagSpecs.
//
// The parser tracks two things per flag block: the flag-declaration line(s)
// and the wrapped description that follows. The flag/description boundary on
// a block's first physical line is the LAST run of 2+ spaces on that line,
// not the first — llama.cpp's formatter pads between short and long aliases
// (e.g. "-h,    --help, --usage") with runs of spaces that look identical to
// the flag/description boundary, so splitting on the first run misparses the
// aliases. When a block's first line has no such run at all (long alias
// lists that overflow the description column, e.g.
// "--spec-draft-hf, -hfd, -hfrd, --hf-repo-draft <user>/<model>[:quant]"),
// the whole line is flag declaration and the description starts on the next
// (indented) line — which the block/continuation-line logic already handles
// without any special case.
func ParseHelpText(text string) []FlagSpec {
	lines := strings.Split(text, "\n")
	var flags []FlagSpec
	section := ""
	var cur *FlagSpec
	var descLines []string

	flush := func() {
		if cur == nil {
			return
		}
		help := strings.TrimSpace(strings.Join(descLines, " "))
		cur.Help = help
		applyAnnotations(cur, help)
		flags = append(flags, *cur)
		cur = nil
		descLines = nil
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := sectionHeaderRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			section = m[1]
			continue
		}
		if strings.HasPrefix(line, "-") {
			flush()
			flagpart, desc := splitFlagLine(line)
			names, value := parseFlagPart(flagpart)
			if len(names) == 0 {
				// Not actually a flag declaration (e.g. a description line that
				// happens to start with '-'); treat as a continuation instead.
				if cur != nil {
					descLines = append(descLines, trimmed)
				}
				continue
			}
			cur = &FlagSpec{Names: names, Value: value, Section: section}
			if desc != "" {
				descLines = append(descLines, desc)
			}
			continue
		}
		if cur != nil {
			descLines = append(descLines, trimmed)
		}
	}
	flush()
	return flags
}

// splitFlagLine splits a flag block's first line into the flag-declaration
// portion and the description portion (if any), on the last run of 2+
// whitespace characters in the line.
func splitFlagLine(line string) (flagpart, desc string) {
	matches := gapRe.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return strings.TrimRight(line, " \t"), ""
	}
	last := matches[len(matches)-1]
	return strings.TrimRight(line[:last[0]], " \t"), strings.TrimSpace(line[last[1]:])
}

// parseFlagPart splits a flag-declaration string like
// "-dev,  --device <dev1,dev2,..>" into its alias names and trailing value
// placeholder. Splitting is bracket-depth-aware over <>{}[] so commas inside
// a value placeholder (as in the example above) are not mistaken for alias
// separators.
func parseFlagPart(flagpart string) (names []string, value string) {
	for _, seg := range splitTopLevelCommas(flagpart) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		m := aliasRe.FindStringSubmatch(seg)
		if m == nil {
			continue
		}
		names = append(names, m[1])
		if m[2] != "" {
			value = strings.TrimSpace(m[2])
		}
	}
	return
}

func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '{', '[':
			depth++
		case '>', '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func applyAnnotations(f *FlagSpec, help string) {
	if m := envRe.FindStringSubmatch(help); m != nil {
		f.CanonicalEnv = m[1]
	}
	if m := defaultRe.FindStringSubmatch(help); m != nil {
		f.Default = strings.TrimSpace(m[1])
	}

	choices, isEnum := classifyChoices(f.Value)
	if !isEnum {
		if m := allowedValuesRe.FindStringSubmatch(help); m != nil {
			var raw []string
			for _, c := range strings.Split(m[1], ",") {
				c = strings.TrimSpace(c)
				if c != "" {
					raw = append(raw, c)
				}
			}
			if len(raw) > 0 {
				choices, isEnum = raw, true
			}
		}
	}
	f.Choices = choices
	f.Type = classifyType(f.Value, isEnum)
}

// classifyChoices recognizes bracketed choice lists like "{none,linear,yarn}",
// "[on|off|auto]" or "<0|1>" as an enum. Placeholders like "<dev1,dev2,..>"
// (a free-form comma list, signalled by the literal ".." token) or
// "N0,N1,N2,..." (no wrapping brackets at all) are deliberately rejected so
// they fall through to "string" instead of being misread as a fixed choice
// set.
func classifyChoices(value string) ([]string, bool) {
	if len(value) < 2 {
		return nil, false
	}
	var closeCh byte
	switch value[0] {
	case '{':
		closeCh = '}'
	case '[':
		closeCh = ']'
	case '<':
		closeCh = '>'
	default:
		return nil, false
	}
	if value[len(value)-1] != closeCh {
		return nil, false
	}
	inner := value[1 : len(value)-1]
	var sep string
	switch {
	case strings.Contains(inner, "|"):
		sep = "|"
	case strings.Contains(inner, ","):
		sep = ","
	default:
		return nil, false
	}
	var choices []string
	for _, p := range strings.Split(inner, sep) {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "..") {
			return nil, false
		}
		choices = append(choices, p)
	}
	if len(choices) < 2 {
		return nil, false
	}
	return choices, true
}

func classifyType(value string, isEnum bool) string {
	if value == "" {
		return "boolean"
	}
	if isEnum {
		return "enum"
	}
	switch value {
	case "FNAME", "FILE", "PATH":
		return "path"
	case "N", "SEED":
		return "number"
	}
	if len(value) >= 2 && (value[0] == '<' || value[0] == '[') {
		if numericRangeRe.MatchString(value[1 : len(value)-1]) {
			return "number"
		}
	}
	return "string"
}

// RunHelp executes `<binPath> --help` and returns its combined output. Some
// backend builds print help to stdout, others to stderr, and some exit
// non-zero after printing it, so both streams are captured and a non-zero
// exit is only treated as an error when no output was produced at all.
func RunHelp(binPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "--help").CombinedOutput()
	if len(out) == 0 && err != nil {
		return "", fmt.Errorf("failed to run %s --help: %w", binPath, err)
	}
	return string(out), nil
}

// LoadOrBuildBackendSchema returns the cached flag schema for a backend,
// regenerating and re-caching it (backends/<name>/schema.json) if missing or
// older than the backend's llama-server binary. Caching is best-effort: a
// write failure is not returned as an error, since the schema was still
// computed successfully and can simply be regenerated next time.
func LoadOrBuildBackendSchema(backendsDir, name string) (*BackendSchema, error) {
	binPath := filepath.Join(backendsDir, name, "llama-server")
	binInfo, err := os.Stat(binPath)
	if err != nil {
		return nil, err
	}

	schemaPath := filepath.Join(backendsDir, name, "schema.json")
	if schemaInfo, err := os.Stat(schemaPath); err == nil && schemaInfo.ModTime().After(binInfo.ModTime()) {
		if data, err := os.ReadFile(schemaPath); err == nil {
			var schema BackendSchema
			if json.Unmarshal(data, &schema) == nil {
				return &schema, nil
			}
		}
	}

	help, err := RunHelp(binPath)
	if err != nil {
		return nil, err
	}
	schema := &BackendSchema{
		Backend:  name,
		ParsedAt: time.Now(),
		Flags:    ParseHelpText(help),
	}
	if data, err := json.MarshalIndent(schema, "", "  "); err == nil {
		_ = os.WriteFile(schemaPath, data, 0644)
	}
	return schema, nil
}
