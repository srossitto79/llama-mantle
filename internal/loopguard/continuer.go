package loopguard

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Recovery policies.
const (
	// RecoveryWrapUp keeps the prompt identical (prefix cache reused) and
	// appends a closing sentence to the reasoning.
	RecoveryWrapUp = "wrapUp"
	// RecoveryLowEffort asks the template for low reasoning effort, as Strata
	// does. The system prompt changes, so the whole prompt is processed again.
	RecoveryLowEffort = "lowEffort"
)

// wrapUpSentence closes looping reasoning under RecoveryWrapUp.
const wrapUpSentence = "\n\nI am repeating myself; time to settle on an answer."

// Continuation sampling floors, from Strata.
const (
	minTemperature     = 1.0
	minPresencePenalty = 1.5
)

// NewPrefillContinuer returns a ReplyContinuer that resends the chat request
// with the reasoning so far as a final assistant message (no content), which
// llama.cpp continues as a prefill inside the reasoning.
func NewPrefillContinuer(policy string) (ReplyContinuer, error) {
	switch policy {
	case RecoveryWrapUp, RecoveryLowEffort:
		return &prefillContinuer{policy: policy}, nil
	default:
		return nil, fmt.Errorf("loopguard: unknown recovery policy %q", policy)
	}
}

type prefillContinuer struct {
	policy string
}

func (c *prefillContinuer) Continue(original []byte, reasoning string, generatedTokens int) (Continuation, error) {
	body := original
	var err error

	lead := ""
	if c.policy == RecoveryWrapUp {
		lead = wrapUpSentence
	}
	prefill := map[string]any{
		"role":              "assistant",
		"content":           "",
		"reasoning_content": reasoning + lead,
	}
	if body, err = sjson.SetBytes(body, "messages.-1", prefill); err != nil {
		return Continuation{}, fmt.Errorf("loopguard: appending prefill: %w", err)
	}

	if c.policy == RecoveryLowEffort {
		if body, err = sjson.SetBytes(body, "chat_template_kwargs.reasoning_effort", "low"); err != nil {
			return Continuation{}, fmt.Errorf("loopguard: setting reasoning effort: %w", err)
		}
	}

	if body, err = raiseFloor(body, "temperature", minTemperature); err != nil {
		return Continuation{}, err
	}
	if body, err = raiseFloor(body, "presence_penalty", minPresencePenalty); err != nil {
		return Continuation{}, err
	}

	// The tokens already generated count against the client's limit.
	for _, key := range []string{"max_tokens", "max_completion_tokens", "n_predict"} {
		v := gjson.GetBytes(body, key)
		if !v.Exists() || v.Int() <= 0 {
			continue
		}
		left := v.Int() - int64(generatedTokens)
		if left < 1 {
			left = 1
		}
		if body, err = sjson.SetBytes(body, key, left); err != nil {
			return Continuation{}, fmt.Errorf("loopguard: setting %s: %w", key, err)
		}
	}

	return Continuation{Body: body, Lead: lead, Echo: reasoning + lead}, nil
}

func raiseFloor(body []byte, key string, floor float64) ([]byte, error) {
	if v := gjson.GetBytes(body, key); v.Exists() && v.Float() >= floor {
		return body, nil
	}
	out, err := sjson.SetBytes(body, key, floor)
	if err != nil {
		return nil, fmt.Errorf("loopguard: setting %s: %w", key, err)
	}
	return out, nil
}
