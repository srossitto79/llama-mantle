package mantle

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mostlygeek/llama-swap/internal/store"
)

type StudioEvaluation struct {
	JobID      string         `json:"jobID"`
	ProjectID  string         `json:"projectID,omitempty"`
	Model      string         `json:"model"`
	Mode       string         `json:"mode"`
	Metrics    map[string]any `json:"metrics"`
	Parameters map[string]any `json:"parameters"`
	CreatedAt  time.Time      `json:"createdAt"`
}

var perplexityResultPattern = regexp.MustCompile(`(?i)\bppl\s*=\s*([0-9]+(?:\.[0-9]+)?)`)
var evaluationScorePattern = regexp.MustCompile(`(?i)\b(acc(?:uracy)?|score|kl(?:[-_ ]divergence)?)\s*[:=]\s*([0-9]+(?:\.[0-9]+)?)`)

func (tm *TaskManager) recordAndGateStudioEvaluation(task *Task, req EvaluateRequest) error {
	snapshot := task.Snapshot()
	metrics := parseStudioEvaluationMetrics(req.Mode, snapshot.Logs)
	metricsJSON, err1 := json.Marshal(metrics)
	parametersJSON, err2 := json.Marshal(snapshot.Parameters)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("encode evaluation metrics")
	}
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return fmt.Errorf("Studio storage is not configured")
	}
	if err := st.SaveStudioEvaluation(context.Background(), store.StudioEvaluationRecord{
		JobID: snapshot.ID, Model: snapshot.Input, Mode: req.Mode, MetricsJSON: string(metricsJSON),
		ParametersJSON: string(parametersJSON), CreatedAt: snapshot.CreatedAt,
	}); err != nil {
		return err
	}
	if req.BaselineJobID == "" || req.MaxRegressionPercent <= 0 {
		return nil
	}
	baseline, err := st.GetStudioEvaluation(context.Background(), req.BaselineJobID)
	if err != nil {
		return err
	}
	if baseline.Mode != req.Mode {
		return fmt.Errorf("baseline mode %q does not match %q", baseline.Mode, req.Mode)
	}
	var baselineMetrics map[string]any
	if err := json.Unmarshal([]byte(baseline.MetricsJSON), &baselineMetrics); err != nil {
		return fmt.Errorf("decode baseline metrics: %w", err)
	}
	regression, metric, err := studioEvaluationRegression(req.Mode, baselineMetrics, metrics)
	if err != nil {
		return err
	}
	if regression > req.MaxRegressionPercent {
		return fmt.Errorf("%s regressed %.2f%% against baseline %s (limit %.2f%%)", metric, regression, req.BaselineJobID, req.MaxRegressionPercent)
	}
	return nil
}

func studioEvaluationRegression(mode string, baseline, current map[string]any) (float64, string, error) {
	key := "perplexity"
	lowerIsBetter := true
	if mode == "benchmark" {
		key = "generationTokensPerSecond"
		lowerIsBetter = false
		if _, ok := baseline[key]; !ok {
			key = "promptTokensPerSecond"
		}
	} else if _, ok := baseline[key]; !ok {
		if _, scoreOK := baseline["accuracy"]; scoreOK {
			key, lowerIsBetter = "accuracy", false
		} else if _, scoreOK := baseline["score"]; scoreOK {
			key, lowerIsBetter = "score", false
		} else if _, klOK := baseline["klDivergence"]; klOK {
			key = "klDivergence"
		}
	}
	base, ok1 := numberValue(baseline[key])
	value, ok2 := numberValue(current[key])
	if !ok1 || !ok2 || base <= 0 {
		return 0, key, fmt.Errorf("cannot compare baseline metric %q", key)
	}
	if lowerIsBetter {
		return (value - base) * 100 / base, key, nil
	}
	return (base - value) * 100 / base, key, nil
}

func parseStudioEvaluationMetrics(mode string, logs []string) map[string]any {
	metrics := make(map[string]any)
	joined := strings.Join(logs, "\n")
	if mode == "perplexity" {
		matches := perplexityResultPattern.FindAllStringSubmatch(joined, -1)
		if len(matches) > 0 {
			value, _ := strconv.ParseFloat(matches[len(matches)-1][1], 64)
			metrics["perplexity"] = value
		}
		for _, match := range evaluationScorePattern.FindAllStringSubmatch(joined, -1) {
			value, _ := strconv.ParseFloat(match[2], 64)
			key := strings.ToLower(match[1])
			switch {
			case strings.HasPrefix(key, "acc"):
				metrics["accuracy"] = value
			case strings.HasPrefix(key, "kl"):
				metrics["klDivergence"] = value
			default:
				metrics["score"] = value
			}
		}
		return metrics
	}
	start, end := strings.Index(joined, "["), strings.LastIndex(joined, "]")
	if start < 0 || end <= start {
		return metrics
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(joined[start:end+1]), &rows); err != nil {
		return metrics
	}
	metrics["results"] = rows
	for _, row := range rows {
		average, ok := numberValue(row["avg_ts"])
		if !ok {
			continue
		}
		prompt, _ := numberValue(row["n_prompt"])
		generated, _ := numberValue(row["n_gen"])
		if prompt > 0 && generated == 0 {
			metrics["promptTokensPerSecond"] = average
		}
		if generated > 0 && prompt == 0 {
			metrics["generationTokensPerSecond"] = average
		}
	}
	return metrics
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil
	default:
		return 0, false
	}
}

func (tm *TaskManager) ListStudioEvaluations(model string) ([]StudioEvaluation, error) {
	tm.mu.Lock()
	st := tm.studioStore
	tm.mu.Unlock()
	if st == nil {
		return nil, fmt.Errorf("Studio storage is not configured")
	}
	records, err := st.ListStudioEvaluations(context.Background(), model, 100)
	if err != nil {
		return nil, err
	}
	evaluations := make([]StudioEvaluation, 0, len(records))
	for _, record := range records {
		var metrics, parameters map[string]any
		if err := json.Unmarshal([]byte(record.MetricsJSON), &metrics); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(record.ParametersJSON), &parameters); err != nil {
			return nil, err
		}
		evaluations = append(evaluations, StudioEvaluation{
			JobID: record.JobID, ProjectID: record.ProjectID, Model: record.Model, Mode: record.Mode,
			Metrics: metrics, Parameters: parameters, CreatedAt: record.CreatedAt,
		})
	}
	return evaluations, nil
}
