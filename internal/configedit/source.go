package configedit

import (
	"bytes"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// editor implements every interface of the package. It is stateless apart
// from its dependencies; construct it through the New* factories.
type editor struct {
	load      Loader
	validator Validator
}

var entrySections = []string{SectionModels, SectionGroups, SectionProfiles, SectionPeers}

func isEntrySection(s string) bool {
	for _, e := range entrySections {
		if e == s {
			return true
		}
	}
	return false
}

// normalizeNewlines converts CRLF to LF; reports whether the input used CRLF.
func normalizeNewlines(raw []byte) ([]byte, bool) {
	if bytes.Contains(raw, []byte("\r\n")) {
		return bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), true
	}
	return raw, false
}

// parseDocument parses raw into a document node whose root is a mapping. An
// empty file yields an empty mapping.
func parseDocument(raw []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root is not a YAML mapping")
	}
	return &doc, nil
}

// lookup returns the key and value nodes for key in mapping, or nils.
func lookup(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

// resolve follows aliases to the anchored node.
func resolve(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// decodeNode decodes n into plain Go values (aliases resolved, no
// substitution). Integer types are normalized to int64/uint64/float64.
func decodeNode(n *yaml.Node) any {
	var v any
	if err := n.Decode(&v); err != nil {
		return nil
	}
	return normalize(v)
}

// normalize converts decoded YAML/JSON values to a canonical form so values
// from both sources compare equal: all numbers become float64, maps get
// string keys.
func normalize(v any) any {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint64:
		return float64(val)
	case float32:
		return float64(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalize(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalize(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[fmt.Sprint(k)] = normalize(item)
		}
		return out
	default:
		return val
	}
}

func kindName(n *yaml.Node) string {
	switch resolve(n).Kind {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	default:
		return "scalar"
	}
}

func sourceField(key string, value *yaml.Node) SourceField {
	v, redacted := redactValue(key, decodeNode(value))
	f := SourceField{Value: v, Redacted: redacted, Line: value.Line}
	if value.Kind == yaml.AliasNode {
		f.Alias = value.Value
	}
	return f
}

func readEntry(keyNode, value *yaml.Node) SourceEntry {
	e := SourceEntry{ID: keyNode.Value, Line: keyNode.Line, Fields: map[string]SourceField{}, Order: []string{}}
	if value.Kind == yaml.AliasNode {
		e.Alias = value.Value
	}
	m := resolve(value)
	if m.Kind != yaml.MappingNode {
		return e
	}
	var merges []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Value == "<<" && k.Tag == "!!merge" {
			merges = append(merges, v)
			continue
		}
		e.Fields[k.Value] = sourceField(k.Value, v)
		e.Order = append(e.Order, k.Value)
	}
	// Fields from merge keys that are not overridden locally.
	for _, mv := range merges {
		sources := []*yaml.Node{mv}
		if mv.Kind == yaml.SequenceNode {
			sources = mv.Content
		}
		for _, src := range sources {
			target := resolve(src)
			if target == nil || target.Kind != yaml.MappingNode {
				continue
			}
			for i := 0; i+1 < len(target.Content); i += 2 {
				k, v := target.Content[i], target.Content[i+1]
				if _, exists := e.Fields[k.Value]; exists {
					continue
				}
				f := sourceField(k.Value, v)
				f.InheritedFrom = src.Value
				f.Alias = ""
				e.Fields[k.Value] = f
				e.Order = append(e.Order, k.Value)
			}
		}
	}
	return e
}

func collectAnchors(n *yaml.Node, out *[]Anchor, seen map[*yaml.Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	if n.Anchor != "" {
		v, _ := redactValue(n.Anchor, decodeNode(n))
		*out = append(*out, Anchor{Name: n.Anchor, Kind: kindName(n), Line: n.Line, Value: v})
	}
	for _, c := range n.Content {
		if c.Kind != yaml.AliasNode {
			collectAnchors(c, out, seen)
		}
	}
}

// ReadSource implements SourceReader.
func (e *editor) ReadSource(raw []byte) (SourceDocument, error) {
	norm, _ := normalizeNewlines(raw)
	doc, err := parseDocument(norm)
	if err != nil {
		return SourceDocument{}, err
	}
	root := doc.Content[0]
	out := SourceDocument{
		Hash:     Hash(raw),
		Sections: map[string][]SourceEntry{},
		Macros:   []SourceMacro{},
		Settings: map[string]SourceField{},
		Anchors:  []Anchor{},
	}
	for _, s := range entrySections {
		out.Sections[s] = []SourceEntry{}
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		switch {
		case isEntrySection(k.Value):
			m := resolve(v)
			if m.Kind != yaml.MappingNode {
				continue
			}
			entries := []SourceEntry{}
			for j := 0; j+1 < len(m.Content); j += 2 {
				entries = append(entries, readEntry(m.Content[j], m.Content[j+1]))
			}
			out.Sections[k.Value] = entries
		case k.Value == SectionMacros:
			m := resolve(v)
			if m.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(m.Content); j += 2 {
				mk, mv := m.Content[j], m.Content[j+1]
				val, red := redactValue(mk.Value, decodeNode(mv))
				out.Macros = append(out.Macros, SourceMacro{Name: mk.Value, Value: val, Redacted: red, Line: mk.Line})
			}
		default:
			out.Settings[k.Value] = sourceField(k.Value, v)
		}
	}
	collectAnchors(root, &out.Anchors, map[*yaml.Node]bool{})
	sort.SliceStable(out.Anchors, func(i, j int) bool { return out.Anchors[i].Line < out.Anchors[j].Line })
	return out, nil
}
