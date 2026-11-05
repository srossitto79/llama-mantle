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

// Continuation strategies: how the upstream is asked to resume inside the
// reasoning.
const (
	// ContinuationLlamaCpp sends the reasoning as a final assistant message;
	// llama.cpp continues an assistant message as a prefill.
	ContinuationLlamaCpp = "llamacpp"
	// ContinuationVLLM sends the same message with add_generation_prompt off
	// and chat_template_kwargs.continue_reasoning on. vLLM renders the prompt
	// as is, so the chat template must leave the last assistant message's
	// <think> block open when continue_reasoning is set (see
	// docker/hyperqwen/chat_template.jinja).
	ContinuationVLLM = "vllm"
)

// Continuation sampling floors, from Strata.
const (
	minTemperature     = 1.0
	minPresencePenalty = 1.5
)

// NewContinuer returns a ReplyContinuer that resends the chat request with
// the reasoning so far as a final assistant message (no content), for the
// upstream to continue inside the reasoning. strategy is ContinuationLlamaCpp
// (also when empty) or ContinuationVLLM.
func NewContinuer(policy, strategy string) (ReplyContinuer, error) {
	switch policy {
	case RecoveryWrapUp, RecoveryLowEffort:
	default:
		return nil, fmt.Errorf("loopguard: unknown recovery policy %q", policy)
	}
	switch strategy {
	case "", ContinuationLlamaCpp:
		return &prefillContinuer{policy: policy}, nil
	case ContinuationVLLM:
		return &prefillContinuer{policy: policy, templateFlag: true}, nil
	default:
		return nil, fmt.Errorf("loopguard: unknown continuation %q", strategy)
	}
}

type prefillContinuer struct {
	policy string
	// templateFlag asks the chat template to leave the reasoning open instead
	// of relying on the server to treat the assistant message as a prefill.
	templateFlag bool
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

	echo := reasoning + lead
	if c.templateFlag {
		if body, err = sjson.SetBytes(body, "add_generation_prompt", false); err != nil {
			return Continuation{}, fmt.Errorf("loopguard: setting add_generation_prompt: %w", err)
		}
		if body, err = sjson.SetBytes(body, "chat_template_kwargs.continue_reasoning", true); err != nil {
			return Continuation{}, fmt.Errorf("loopguard: setting continue_reasoning: %w", err)
		}
		echo = "" // vLLM streams only new tokens
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

	return Continuation{Body: body, Lead: lead, Echo: echo}, nil
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
