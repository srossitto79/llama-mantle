package config

import (
	"errors"
	"fmt"
	"runtime"

	"gopkg.in/yaml.v3"
)

const (
	MODEL_CONFIG_DEFAULT_TTL = -1
)

var validModalities = map[string]struct{}{
	"text":  {},
	"audio": {},
	"image": {},
}

// ModelCapConfig defines what modalities and features a model supports.
// Used in /v1/models to inform clients. An empty block (all zero values) is
// treated as not configured.
//
// The custom UnmarshalYAML stores an untyped representation so YAML anchors
// can be resolved before macro substitution. After ResolveMacros is called
// the typed fields (In, Out, Tools, Reranker, Context) are populated.
type ModelCapConfig struct {
	In       []string `yaml:"in" json:"in,omitempty"`
	Out      []string `yaml:"out" json:"out,omitempty"`
	Tools    bool     `yaml:"tools" json:"tools,omitempty"`
	Reranker bool     `yaml:"reranker" json:"reranker,omitempty"`
	Context  int      `yaml:"context" json:"context,omitempty"`

	raw map[string]any
}

// UnmarshalYAML decodes capabilities into an untyped map. This materializes
// YAML aliases and merge keys while allowing macro placeholders in fields
// that will ultimately be decoded as ints or bools.
func (c *ModelCapConfig) UnmarshalYAML(value *yaml.Node) error {
	return value.Decode(&c.raw)
}

// ResolveMacros substitutes all macros in the untyped representation (LIFO
// order matching LoadConfigFromReader), then decodes the resolved values into
// the typed fields.
func (c *ModelCapConfig) ResolveMacros(macros MacroList) error {
	if c.raw == nil {
		return c.Validate()
	}

	var resolved any = c.raw
	for i := len(macros) - 1; i >= 0; i-- {
		entry := macros[i]
		var err error
		resolved, err = substituteMacroInValue(resolved, entry.Name, entry.Value)
		if err != nil {
			return fmt.Errorf("capabilities: failed macro substitution: %w", err)
		}
	}
	if err := validateNestedForUnknownMacros(resolved, "capabilities"); err != nil {
		return err
	}

	var node yaml.Node
	if err := node.Encode(resolved); err != nil {
		return fmt.Errorf("capabilities: failed to encode after macro substitution: %w", err)
	}
	*c = ModelCapConfig{}
	type rawCap ModelCapConfig
	if err := node.Decode((*rawCap)(c)); err != nil {
		return fmt.Errorf("capabilities: failed to decode after macro substitution: %w", err)
	}

	return c.Validate()
}

// Empty returns true when all fields are at their zero values.
func (c ModelCapConfig) Empty() bool {
	return len(c.In) == 0 && len(c.Out) == 0 && !c.Tools && !c.Reranker && c.Context == 0
}

// Validate checks that all modality values are recognized and context is
// non-negative. Returns an error if any value is invalid.
func (c ModelCapConfig) Validate() error {
	for _, m := range c.In {
		if _, ok := validModalities[m]; !ok {
			return fmt.Errorf("capabilities.in: invalid modality %q, must be one of: text, audio, image", m)
		}
	}
	for _, m := range c.Out {
		if _, ok := validModalities[m]; !ok {
			return fmt.Errorf("capabilities.out: invalid modality %q, must be one of: text, audio, image", m)
		}
	}
	if c.Context < 0 {
		return errors.New("capabilities.context: must be >= 0")
	}
	return nil
}

// TimeoutsConfig holds timeout settings for proxy connections
// 0 = no timeout
type TimeoutsConfig struct {
	Connect        int `yaml:"connect" json:"connect,omitempty"`
	KeepAlive      int `yaml:"keepalive" json:"keepalive,omitempty"`
	ResponseHeader int `yaml:"responseHeader" json:"responseHeader,omitempty"`
	TLSHandshake   int `yaml:"tlsHandshake" json:"tlsHandshake,omitempty"`
	ExpectContinue int `yaml:"expectContinue" json:"expectContinue,omitempty"`
	IdleConn       int `yaml:"idleConn" json:"idleConn,omitempty"`
}

type ModelConfig struct {
	Cmd           string   `yaml:"cmd" json:"cmd"`
	CmdStop       string   `yaml:"cmdStop" json:"cmdStop,omitempty"`
	Proxy         string   `yaml:"proxy" json:"proxy,omitempty"`
	Aliases       []string `yaml:"aliases" json:"aliases,omitempty"`
	Env           []string `yaml:"env" json:"env,omitempty"`
	CheckEndpoint string   `yaml:"checkEndpoint" json:"checkEndpoint,omitempty"`
	UnloadAfter   int      `yaml:"ttl" json:"ttl"`
	UnloadTimeout int      `yaml:"unloadTimeout" json:"unloadTimeout,omitempty"`
	Unlisted      bool     `yaml:"unlisted" json:"unlisted,omitempty"`
	UseModelName  string   `yaml:"useModelName" json:"useModelName,omitempty"`

	// ApiKey is sent upstream as Authorization: Bearer and x-api-key,
	// replacing any client-supplied key. Never serialized to JSON.
	ApiKey string `yaml:"apiKey" json:"-"`

	// #179 for /v1/models
	Name        string `yaml:"name" json:"name,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`

	// Limit concurrency of HTTP requests to process
	ConcurrencyLimit int `yaml:"concurrencyLimit" json:"concurrencyLimit,omitempty"`

	// Model filters see issue #174
	Filters ModelFilters `yaml:"filters" json:"filters,omitempty"`

	// Macros: see #264
	// Model level macros take precedence over the global macros
	Macros MacroList `yaml:"macros" json:"macros,omitempty"`

	// Metadata: see #264
	// Arbitrary metadata that can be exposed through the API
	Metadata map[string]any `yaml:"metadata" json:"metadata,omitempty"`

	// override global setting
	SendLoadingState *bool `yaml:"sendLoadingState" json:"sendLoadingState,omitempty"`

	// Timeout settings for proxy connections
	Timeouts TimeoutsConfig `yaml:"timeouts" json:"timeouts,omitempty"`

	// Capabilities defines what modalities and features the model supports.
	Capabilities ModelCapConfig `yaml:"capabilities" json:"capabilities,omitempty"`

	// ReasoningLoopGuard stops reasoning that repeats whole passages and
	// continues the reply. Off unless configured.
	ReasoningLoopGuard ReasoningLoopGuardConfig `yaml:"reasoningLoopGuard" json:"reasoningLoopGuard,omitempty"`

	// Copy of HealthCheckTimeout from global config
	HealthCheckTimeout int `yaml:"healthCheckTimeout" json:"healthCheckTimeout,omitempty"`
}

// Reasoning loop guard modes and recovery policies.
const (
	LoopGuardModeOff     = "off"
	LoopGuardModeStop    = "stop"
	LoopGuardModeRecover = "recover"

	LoopGuardRecoveryWrapUp    = "wrapUp"
	LoopGuardRecoveryLowEffort = "lowEffort"

	LoopGuardContinuationLlamaCpp = "llamacpp"
	LoopGuardContinuationVLLM     = "vllm"

	LoopGuardDefaultCoverage   = 0.25
	LoopGuardDefaultCheckEvery = 512
)

// ReasoningLoopGuardConfig configures the per-model reasoning loop guard.
// Zero values mean defaults: mode off, recovery wrapUp, continuation
// llamacpp, coverage 0.25, checkEvery 512.
type ReasoningLoopGuardConfig struct {
	Mode     string `yaml:"mode" json:"mode,omitempty"`
	Recovery string `yaml:"recovery" json:"recovery,omitempty"`
	// Continuation is how the upstream resumes inside the reasoning:
	// llamacpp (assistant prefill) or vllm (chat template flag).
	Continuation string  `yaml:"continuation" json:"continuation,omitempty"`
	Coverage     float64 `yaml:"coverage" json:"coverage,omitempty"`
	CheckEvery   int     `yaml:"checkEvery" json:"checkEvery,omitempty"`
}

// Enabled reports whether the guard acts on requests.
func (c ReasoningLoopGuardConfig) Enabled() bool {
	return c.Mode == LoopGuardModeStop || c.Mode == LoopGuardModeRecover
}

// normalize validates the values and, when the guard is enabled, fills the
// defaults. A disabled guard stays as configured.
func (c *ReasoningLoopGuardConfig) normalize() error {
	switch c.Mode {
	case "", LoopGuardModeOff:
		if c.Recovery == "" && c.Continuation == "" && c.Coverage == 0 && c.CheckEvery == 0 {
			return nil
		}
	case LoopGuardModeStop, LoopGuardModeRecover:
	default:
		return fmt.Errorf("reasoningLoopGuard.mode: invalid value %q, must be one of: off, stop, recover", c.Mode)
	}
	switch c.Recovery {
	case "":
		c.Recovery = LoopGuardRecoveryWrapUp
	case LoopGuardRecoveryWrapUp, LoopGuardRecoveryLowEffort:
	default:
		return fmt.Errorf("reasoningLoopGuard.recovery: invalid value %q, must be one of: wrapUp, lowEffort", c.Recovery)
	}
	switch c.Continuation {
	case "":
		c.Continuation = LoopGuardContinuationLlamaCpp
	case LoopGuardContinuationLlamaCpp, LoopGuardContinuationVLLM:
	default:
		return fmt.Errorf("reasoningLoopGuard.continuation: invalid value %q, must be one of: llamacpp, vllm", c.Continuation)
	}
	if c.Coverage == 0 {
		c.Coverage = LoopGuardDefaultCoverage
	}
	if c.Coverage < 0 || c.Coverage > 1 {
		return fmt.Errorf("reasoningLoopGuard.coverage: must be between 0 and 1, got %v", c.Coverage)
	}
	if c.CheckEvery == 0 {
		c.CheckEvery = LoopGuardDefaultCheckEvery
	}
	if c.CheckEvery < 0 {
		return fmt.Errorf("reasoningLoopGuard.checkEvery: must be > 0, got %d", c.CheckEvery)
	}
	return nil
}

func (m *ModelConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawModelConfig ModelConfig
	defaults := rawModelConfig{
		Cmd:              "",
		CmdStop:          "",
		Proxy:            "http://localhost:${PORT}",
		Aliases:          []string{},
		Env:              []string{},
		CheckEndpoint:    "/health",
		UnloadAfter:      MODEL_CONFIG_DEFAULT_TTL, // use GlobalTTL
		UnloadTimeout:    0,                        // use global UnloadTimeout
		Unlisted:         false,
		UseModelName:     "",
		ConcurrencyLimit: 0,
		Name:             "",
		Description:      "",

		// matches http.DefaultTransport, except IdleConn
		Timeouts: TimeoutsConfig{
			Connect:        30,
			KeepAlive:      30,
			ResponseHeader: 0,
			TLSHandshake:   10,
			ExpectContinue: 1,
			// Below the upstream's keep-alive: llama-server (cpp-httplib) and
			// uvicorn close idle connections after 5s. Reusing one as the
			// upstream closes it fails the request with EOF, and Go does not
			// retry a POST, so the client gets a 502.
			IdleConn: 4,
		},
	}

	// the default cmdStop to taskkill /f /t /pid ${PID}
	if runtime.GOOS == "windows" {
		defaults.CmdStop = "taskkill /f /t /pid ${PID}"
	}

	if err := unmarshal(&defaults); err != nil {
		return err
	}

	*m = ModelConfig(defaults)
	return nil
}

func (m *ModelConfig) SanitizedCommand() ([]string, error) {
	return SanitizeCommand(m.Cmd)
}

// ModelFilters embeds Filters and adds legacy support for strip_params field
// See issue #174
type ModelFilters struct {
	Filters `yaml:",inline"`
}

func (m *ModelFilters) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawModelFilters ModelFilters
	defaults := rawModelFilters{}

	if err := unmarshal(&defaults); err != nil {
		return err
	}

	// Try to unmarshal with the old field name for backwards compatibility
	if defaults.StripParams == "" {
		var legacy struct {
			StripParams string `yaml:"strip_params"`
		}
		if legacyErr := unmarshal(&legacy); legacyErr != nil {
			return errors.New("failed to unmarshal legacy filters.strip_params: " + legacyErr.Error())
		}
		defaults.StripParams = legacy.StripParams
	}

	*m = ModelFilters(defaults)
	return nil
}

// SanitizedStripParams wraps Filters.SanitizedStripParams for backwards compatibility
// Returns ([]string, error) to match existing API
func (f ModelFilters) SanitizedStripParams() ([]string, error) {
	return f.Filters.SanitizedStripParams(), nil
}
