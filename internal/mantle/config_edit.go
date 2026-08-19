package mantle

import (
	"fmt"
	"sort"

	"github.com/mostlygeek/llama-swap/internal/config"
	"gopkg.in/yaml.v3"
)

// NamedModelConfig pairs a model's YAML key (its model ID) with its
// settings, for API responses that list every configured model. The field is
// named ID, not Name, because ModelConfig already has its own Name field
// (the optional /v1/models display name) — reusing "Name" here would
// collide with that embedded field's identical JSON tag and silently shadow
// it out of the response.
type NamedModelConfig struct {
	ID string `json:"id"`
	config.ModelConfig
}

// NamedGroupConfig pairs a group's YAML key with its settings.
type NamedGroupConfig struct {
	Name string `json:"name"`
	config.GroupConfig
}

// ListModels returns every configured model, sorted by name, for the guided
// config editor's card list.
func ListModels(cfg *config.Config) []NamedModelConfig {
	names := make([]string, 0, len(cfg.Models))
	for name := range cfg.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]NamedModelConfig, 0, len(names))
	for _, name := range names {
		out = append(out, NamedModelConfig{ID: name, ModelConfig: cfg.Models[name]})
	}
	return out
}

// ListGroups returns every configured group (top-level `groups:`, the
// permanent backwards-compat key documented in config.go), sorted by name.
func ListGroups(cfg *config.Config) []NamedGroupConfig {
	names := make([]string, 0, len(cfg.Groups))
	for name := range cfg.Groups {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]NamedGroupConfig, 0, len(names))
	for _, name := range names {
		out = append(out, NamedGroupConfig{Name: name, GroupConfig: cfg.Groups[name]})
	}
	return out
}

// decodeYAMLDocument parses raw config bytes into a yaml.Node document,
// giving surgical access to a single subtree (e.g. one model or group)
// without disturbing the rest of the file's structure, anchors, or macros.
func decodeYAMLDocument(raw []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("config is not a YAML document")
	}
	return &doc, nil
}

func rootMapping(doc *yaml.Node) (*yaml.Node, error) {
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root is not a YAML mapping")
	}
	return root, nil
}

// findOrCreateMappingChild returns the value node for key within mapping,
// creating an empty mapping node for it if the key doesn't exist yet.
func findOrCreateMappingChild(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, keyNode, valNode)
	return valNode
}

// setMappingEntry sets (or replaces) a single key/value pair within mapping.
func setMappingEntry(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapping.Content = append(mapping.Content, keyNode, value)
}

// deleteMappingEntry removes a key/value pair from mapping, if present.
func deleteMappingEntry(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

// UpsertModelYAML replaces (or adds) a single model entry in the `models:`
// mapping, re-serializing the whole document. Every other model, macros,
// groups, and comments outside the targeted model's own block are left
// untouched, since only that one value node's content is replaced. Any
// hand-written comment INSIDE the edited model's block is lost, since the
// node is fully replaced rather than merged field-by-field — the raw YAML
// editor remains available for that case.
func UpsertModelYAML(raw []byte, name string, model config.ModelConfig) ([]byte, error) {
	doc, err := decodeYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	modelsNode := findOrCreateMappingChild(root, "models")
	var valueNode yaml.Node
	if err := valueNode.Encode(model); err != nil {
		return nil, fmt.Errorf("failed to encode model %q: %w", name, err)
	}
	setMappingEntry(modelsNode, name, &valueNode)
	return yaml.Marshal(doc)
}

// DeleteModelYAML removes a single model entry from the `models:` mapping.
func DeleteModelYAML(raw []byte, name string) ([]byte, error) {
	doc, err := decodeYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	modelsNode := findOrCreateMappingChild(root, "models")
	deleteMappingEntry(modelsNode, name)
	return yaml.Marshal(doc)
}

// UpsertGroupYAML replaces (or adds) a single group entry in the top-level
// `groups:` mapping — the permanent backwards-compat key described in
// config.go, not the internally-normalized routing.router.settings.groups
// path. Same subtree-only replacement semantics as UpsertModelYAML.
func UpsertGroupYAML(raw []byte, name string, group config.GroupConfig) ([]byte, error) {
	doc, err := decodeYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	groupsNode := findOrCreateMappingChild(root, "groups")
	var valueNode yaml.Node
	if err := valueNode.Encode(group); err != nil {
		return nil, fmt.Errorf("failed to encode group %q: %w", name, err)
	}
	setMappingEntry(groupsNode, name, &valueNode)
	return yaml.Marshal(doc)
}

// DeleteGroupYAML removes a single group entry from the top-level `groups:`
// mapping.
func DeleteGroupYAML(raw []byte, name string) ([]byte, error) {
	doc, err := decodeYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	groupsNode := findOrCreateMappingChild(root, "groups")
	deleteMappingEntry(groupsNode, name)
	return yaml.Marshal(doc)
}
