// Package configedit reads and edits config.yaml as written.
//
// Every edit keeps what the file relies on: macro references (${models_dir}),
// ${env.*} references, YAML anchors and aliases, comments and formatting of
// everything that was not edited. Changes are applied to the YAML node tree and
// spliced back into the original text, so only the edited entries are
// re-emitted.
//
// No secret value leaves the package: the source view redacts literal secrets,
// the effective view never substitutes ${env.*}, and validation messages are
// scrubbed of environment values.
package configedit

import "github.com/mostlygeek/llama-swap/internal/config"

// Redacted replaces a secret value in every view. A change whose value
// contains it is rejected, so the marker can never be written to the file.
const Redacted = "[redacted]"

// Sections whose value is a mapping of id -> mapping.
const (
	SectionModels   = "models"
	SectionGroups   = "groups"
	SectionProfiles = "profiles"
	SectionPeers    = "peers"
	// SectionMacros is a mapping of name -> scalar.
	SectionMacros = "macros"
	// SectionRoot addresses top-level keys directly: ID is the key.
	SectionRoot = ""
)

// Problem is a validation finding. Line and Column are 1-based; zero when the
// finding has no position.
type Problem struct {
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

// SourceField is one field of an entry, as written.
type SourceField struct {
	// Value is the field decoded without macro or ${env.*} substitution.
	// Aliases are resolved to the anchor's value.
	Value any `json:"value"`
	// Alias names the anchor when the field is written as *anchor.
	Alias string `json:"alias,omitempty"`
	// InheritedFrom names the anchor when the field comes from a `<<: *anchor`
	// merge key and is not overridden locally.
	InheritedFrom string `json:"inheritedFrom,omitempty"`
	// Redacted is true when Value had a secret replaced with Redacted.
	Redacted bool `json:"redacted,omitempty"`
	Line     int  `json:"line,omitempty"`
}

// SourceEntry is one entry of a section (a model, a group, ...).
type SourceEntry struct {
	ID   string `json:"id"`
	Line int    `json:"line"`
	// Alias names the anchor when the whole entry is written as *anchor.
	Alias  string                 `json:"alias,omitempty"`
	Fields map[string]SourceField `json:"fields"`
	// Order lists field names in file order.
	Order []string `json:"order"`
}

// Anchor is a YAML anchor defined in the file.
type Anchor struct {
	Name string `json:"name"`
	// Kind is "scalar", "sequence" or "mapping".
	Kind  string `json:"kind"`
	Line  int    `json:"line"`
	Value any    `json:"value"`
}

// SourceDocument is the whole file as written.
type SourceDocument struct {
	// Hash identifies the file contents; pass it back as Plan's base hash.
	Hash     string                   `json:"hash"`
	Sections map[string][]SourceEntry `json:"sections"`
	// Macros are the global macros, in file order.
	Macros []SourceMacro `json:"macros"`
	// Settings are the remaining top-level keys.
	Settings map[string]SourceField `json:"settings"`
	Anchors  []Anchor               `json:"anchors"`
}

// SourceMacro is a global macro as written.
type SourceMacro struct {
	Name     string `json:"name"`
	Value    any    `json:"value"`
	Redacted bool   `json:"redacted,omitempty"`
	Line     int    `json:"line"`
}

// Change is one edit. Ops:
//   - "set":    set Path within entry ID (empty Path sets the whole entry)
//   - "unset":  remove Path within entry ID
//   - "add":    add entry ID with Value
//   - "delete": remove entry ID (models: also from group members and preload)
//   - "rename": rename entry ID to NewID (models: also in group members and preload)
//
// Detach allows replacing an alias on the path with a local copy.
type Change struct {
	Op      string   `json:"op"`
	Section string   `json:"section"`
	ID      string   `json:"id"`
	Path    []string `json:"path,omitempty"`
	Value   any      `json:"value,omitempty"`
	NewID   string   `json:"newId,omitempty"`
	Detach  bool     `json:"detach,omitempty"`
}

// Error codes returned in ChangeError.
const (
	CodeNotFound  = "not_found"
	CodeExists    = "exists"
	CodeAliased   = "aliased"
	CodeInherited = "inherited"
	CodeRedacted  = "redacted"
	CodeInvalid   = "invalid"
	CodeStale     = "stale"
)

// ChangeError reports a change that cannot be applied.
type ChangeError struct {
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Anchor  string `json:"anchor,omitempty"`
	Message string `json:"message"`
}

func (e *ChangeError) Error() string { return e.Message }

// Plan is the result of applying changes, before anything is written.
type Plan struct {
	BaseHash string    `json:"baseHash"`
	Hash     string    `json:"hash"`
	YAML     string    `json:"yaml"`
	Problems []Problem `json:"problems"`
}

// SourceReader reads entries as written in the file.
type SourceReader interface {
	ReadSource(raw []byte) (SourceDocument, error)
}

// EffectiveReader returns models after macro expansion, with ${env.*} kept as
// references and literal secrets redacted. Keyed by model ID; each value has
// the JSON shape of config.ModelConfig.
type EffectiveReader interface {
	EffectiveModels(raw []byte) (map[string]map[string]any, error)
}

// Planner applies changes to raw file bytes without writing anything.
// It returns *ChangeError when a change cannot be applied.
type Planner interface {
	Plan(raw []byte, baseHash string, changes []Change) (Plan, error)
}

// Validator checks raw file bytes and reports problems with positions.
type Validator interface {
	Validate(raw []byte) []Problem
	// ScrubSecrets removes environment values referenced by raw from msg.
	ScrubSecrets(raw []byte, msg string) string
}

// Loader parses config bytes into a config.Config; config.LoadConfigFromReader
// in production.
type Loader func(raw []byte) (config.Config, error)

// NewSourceReader returns the YAML source reader.
func NewSourceReader() SourceReader { return &editor{} }

// NewEffectiveReader returns the effective-view reader.
func NewEffectiveReader(load Loader) EffectiveReader { return &editor{load: load} }

// NewPlanner returns the change planner; v validates each plan's result.
func NewPlanner(v Validator) Planner { return &editor{validator: v} }

// NewValidator returns the validator.
func NewValidator(load Loader) Validator { return &editor{load: load} }
