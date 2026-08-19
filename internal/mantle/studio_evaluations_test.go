package mantle

import "testing"

func TestParseStudioEvaluationMetrics_Benchmark(t *testing.T) {
	metrics := parseStudioEvaluationMetrics("benchmark", []string{
		"progress", `[{"n_prompt":512,"n_gen":0,"avg_ts":123.4},{"n_prompt":0,"n_gen":128,"avg_ts":45.6}]`,
	})
	if metrics["promptTokensPerSecond"] != 123.4 || metrics["generationTokensPerSecond"] != 45.6 {
		t.Fatalf("unexpected benchmark metrics: %#v", metrics)
	}
}

func TestParseStudioEvaluationMetrics_PerplexityUsesFinalValue(t *testing.T) {
	metrics := parseStudioEvaluationMetrics("perplexity", []string{"ppl = 12.5", "Final estimate: PPL = 9.75"})
	if metrics["perplexity"] != 9.75 {
		t.Fatalf("unexpected perplexity metrics: %#v", metrics)
	}
}

func TestStudioEvaluationRegression_Directions(t *testing.T) {
	regression, _, err := studioEvaluationRegression("perplexity", map[string]any{"perplexity": 10.0}, map[string]any{"perplexity": 11.0})
	if err != nil || regression != 10 {
		t.Fatalf("perplexity regression = %v, %v", regression, err)
	}
	regression, _, err = studioEvaluationRegression("benchmark", map[string]any{"generationTokensPerSecond": 50.0}, map[string]any{"generationTokensPerSecond": 45.0})
	if err != nil || regression != 10 {
		t.Fatalf("throughput regression = %v, %v", regression, err)
	}
}
