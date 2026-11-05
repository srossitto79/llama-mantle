package configedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var yamlLineRegex = regexp.MustCompile(`line (\d+)(?::| column (\d+))?`)

// Validate implements Validator. Checks run in order and stop at the first
// failing stage, since later stages assume earlier ones passed:
//  1. YAML syntax (with line and column);
//  2. config semantics, loaded with ${env.*} replaced by placeholders so no
//     environment value can appear in a message;
//  3. referenced environment variables exist;
//  4. a real load with environment values, its message scrubbed of them.
func (e *editor) Validate(raw []byte) []Problem {
	norm, _ := normalizeNewlines(raw)
	doc, err := parseDocument(norm)
	if err != nil {
		return []Problem{yamlProblem(err)}
	}
	if e.load == nil {
		return []Problem{}
	}
	if _, err := e.load(withEnvPlaceholders(norm)); err != nil {
		msg := restoreEnvRefs(err.Error())
		return []Problem{locate(doc, msg)}
	}
	var problems []Problem
	for _, name := range referencedEnvNames(norm) {
		if _, ok := os.LookupEnv(name); !ok {
			line, col := envRefPosition(norm, name)
			problems = append(problems, Problem{Line: line, Column: col, Message: fmt.Sprintf("environment variable %s is not set", name)})
		}
	}
	if len(problems) > 0 {
		return problems
	}
	if _, err := e.load(norm); err != nil {
		return []Problem{locate(doc, e.ScrubSecrets(norm, err.Error()))}
	}
	return []Problem{}
}

// ScrubSecrets implements Validator.
func (e *editor) ScrubSecrets(raw []byte, msg string) string {
	return scrubEnvValues(raw, msg)
}

func yamlProblem(err error) Problem {
	msg := err.Error()
	p := Problem{Message: strings.TrimPrefix(msg, "yaml: ")}
	if m := yamlLineRegex.FindStringSubmatch(msg); m != nil {
		p.Line, _ = strconv.Atoi(m[1])
		if m[2] != "" {
			p.Column, _ = strconv.Atoi(m[2])
		}
	}
	return p
}

func envRefPosition(raw []byte, name string) (int, int) {
	needle := []byte("${env." + name + "}")
	idx := bytes.Index(raw, needle)
	if idx < 0 {
		return 0, 0
	}
	line := bytes.Count(raw[:idx], []byte("\n")) + 1
	col := idx - bytes.LastIndexByte(raw[:idx], '\n')
	return line, col
}

// locate attaches the position of the model or group a semantic error names.
// Loader messages carry no positions, but most name the entry ("model X: ...").
func locate(doc *yaml.Node, msg string) Problem {
	p := Problem{Message: msg}
	if m := yamlLineRegex.FindStringSubmatch(msg); m != nil && strings.Contains(msg, "yaml:") {
		p.Line, _ = strconv.Atoi(m[1])
		return p
	}
	root := doc.Content[0]
	var candidates []*yaml.Node
	for _, section := range entrySections {
		_, m := lookup(root, section)
		m = resolve(m)
		if m == nil || m.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(m.Content); i += 2 {
			candidates = append(candidates, m.Content[i])
		}
	}
	// Longest id first, so "qwen-8b" wins over "qwen".
	sort.SliceStable(candidates, func(i, j int) bool { return len(candidates[i].Value) > len(candidates[j].Value) })
	for _, k := range candidates {
		if containsWord(msg, k.Value) {
			p.Line, p.Column = k.Line, k.Column
			return p
		}
	}
	return p
}

func containsWord(s, word string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		before := start == 0 || !isIDChar(s[start-1])
		after := end == len(s) || !isIDChar(s[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

func isIDChar(c byte) bool {
	return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// EffectiveModels implements EffectiveReader.
func (e *editor) EffectiveModels(raw []byte) (map[string]map[string]any, error) {
	norm, _ := normalizeNewlines(raw)
	cfg, err := e.load(withEnvPlaceholders(norm))
	if err != nil {
		return nil, fmt.Errorf("%s", restoreEnvRefs(err.Error()))
	}
	out := make(map[string]map[string]any, len(cfg.Models))
	for id, mc := range cfg.Models {
		b, err := json.Marshal(mc)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		restored := restoreEnvRefsDeep(m).(map[string]any)
		redacted, _ := redactValue("", restored)
		out[id] = redacted.(map[string]any)
	}
	return out, nil
}
