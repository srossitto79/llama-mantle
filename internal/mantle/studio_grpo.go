package mantle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultGRPORewardTimeout = 60 * time.Second
	maxGRPOSamples           = 100000
	maxGRPORewardResponse    = 4 * 1024 * 1024
)

type studioGRPOSample struct {
	Prompt    string
	Reference any
	Record    map[string]any
}

type studioGRPORewardRequest struct {
	Step        int            `json:"step"`
	Prompt      string         `json:"prompt"`
	Generations []string       `json:"generations"`
	Reference   any            `json:"reference,omitempty"`
	Sample      map[string]any `json:"sample,omitempty"`
}

type studioGRPORewardResponse struct {
	Rewards []float64 `json:"rewards"`
	Details any       `json:"details,omitempty"`
	Error   string    `json:"error,omitempty"`
}

type studioGRPORollout struct {
	Step        int       `json:"step"`
	Prompt      string    `json:"prompt"`
	Generations []string  `json:"generations"`
	RawRewards  []float64 `json:"rawRewards"`
	Advantages  []float64 `json:"advantages"`
	Details     any       `json:"details,omitempty"`
}

type studioGRPORewardProvider interface {
	Score(context.Context, studioGRPORewardRequest) (studioGRPORewardResponse, error)
	Close() error
}

func validateStudioGRPORequest(req TrainQLoRARequest, modelsDir string) error {
	req.GRPORewardProvider = strings.ToLower(strings.TrimSpace(req.GRPORewardProvider))
	if req.GRPORewardProvider == "" {
		return fmt.Errorf("GRPO reward provider is required")
	}
	if req.NGen <= 1 {
		return fmt.Errorf("GRPO requires at least two generations per prompt")
	}
	if req.NSteps <= 0 || req.GRPOMaxTokens <= 0 {
		return fmt.Errorf("GRPO steps and maximum generation tokens must be positive")
	}
	if req.GRPORewardTimeout < 0 || req.GRPONumericTolerance < 0 {
		return fmt.Errorf("GRPO reward timeout and numeric tolerance must not be negative")
	}
	if strings.TrimSpace(req.GRPOPromptField) == "" {
		return fmt.Errorf("GRPO prompt field is required")
	}
	switch req.GRPORewardProvider {
	case "builtin":
		if !stringAllowed(req.GRPOBuiltinReward, "exact", "numeric", "regex", "json-valid") {
			return fmt.Errorf("unsupported built-in GRPO reward %q", req.GRPOBuiltinReward)
		}
		if req.GRPOBuiltinReward != "json-valid" && strings.TrimSpace(req.GRPOReferenceField) == "" {
			return fmt.Errorf("built-in %s reward requires a reference field", req.GRPOBuiltinReward)
		}
	case "script":
		path, _, err := resolveStudioInput(modelsDir, req.GRPORewardScript, ".py")
		if err != nil {
			return fmt.Errorf("GRPO reward script: %w", err)
		}
		if _, err := exec.LookPath("python3"); err != nil {
			return fmt.Errorf("python3 is required for GRPO reward scripts")
		}
		_ = path
	case "http":
		parsed, err := url.Parse(req.GRPORewardURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("GRPO reward URL must be an HTTP(S) URL without embedded credentials")
		}
	default:
		return fmt.Errorf("unsupported GRPO reward provider %q", req.GRPORewardProvider)
	}
	return nil
}

func loadStudioGRPOSamples(path, promptField, referenceField string) ([]studioGRPOSample, error) {
	if strings.ToLower(filepath.Ext(path)) != ".jsonl" {
		return nil, fmt.Errorf("GRPO prompt datasets must be JSONL")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var samples []studioGRPOSample
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
	for line := 1; scanner.Scan(); line++ {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("GRPO dataset line %d: %w", line, err)
		}
		promptValue, ok := studioGRPOField(record, promptField)
		prompt, stringOK := promptValue.(string)
		if !ok || !stringOK || strings.TrimSpace(prompt) == "" {
			return nil, fmt.Errorf("GRPO dataset line %d has no non-empty string field %q", line, promptField)
		}
		var reference any
		if referenceField != "" {
			reference, ok = studioGRPOField(record, referenceField)
			if !ok {
				return nil, fmt.Errorf("GRPO dataset line %d has no field %q", line, referenceField)
			}
		}
		samples = append(samples, studioGRPOSample{Prompt: prompt, Reference: reference, Record: record})
		if len(samples) > maxGRPOSamples {
			return nil, fmt.Errorf("GRPO dataset exceeds %d samples", maxGRPOSamples)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("GRPO dataset contains no samples")
	}
	return samples, nil
}

func studioGRPOField(record map[string]any, path string) (any, bool) {
	var current any = record
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

type studioGRPOBuiltinProvider struct {
	mode          string
	caseSensitive bool
	tolerance     float64
}

func (p *studioGRPOBuiltinProvider) Close() error { return nil }

func (p *studioGRPOBuiltinProvider) Score(_ context.Context, request studioGRPORewardRequest) (studioGRPORewardResponse, error) {
	rewards := make([]float64, len(request.Generations))
	reference := fmt.Sprint(request.Reference)
	for i, generation := range request.Generations {
		switch p.mode {
		case "exact":
			got, want := strings.TrimSpace(generation), strings.TrimSpace(reference)
			if !p.caseSensitive {
				got, want = strings.ToLower(got), strings.ToLower(want)
			}
			if got == want {
				rewards[i] = 1
			}
		case "numeric":
			got, gotErr := strconv.ParseFloat(strings.TrimSpace(generation), 64)
			want, wantErr := strconv.ParseFloat(strings.TrimSpace(reference), 64)
			if gotErr == nil && wantErr == nil && math.Abs(got-want) <= p.tolerance {
				rewards[i] = 1
			}
		case "regex":
			pattern := reference
			if !p.caseSensitive {
				pattern = "(?i)" + pattern
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return studioGRPORewardResponse{}, fmt.Errorf("invalid reference regex: %w", err)
			}
			if re.MatchString(generation) {
				rewards[i] = 1
			}
		case "json-valid":
			if json.Valid([]byte(strings.TrimSpace(generation))) {
				rewards[i] = 1
			}
		}
	}
	return studioGRPORewardResponse{Rewards: rewards}, nil
}

type studioGRPOHTTPProvider struct {
	url    string
	client *http.Client
}

func (p *studioGRPOHTTPProvider) Close() error { return nil }

func (p *studioGRPOHTTPProvider) Score(ctx context.Context, request studioGRPORewardRequest) (studioGRPORewardResponse, error) {
	body, _ := json.Marshal(request)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return studioGRPORewardResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(httpReq)
	if err != nil {
		return studioGRPORewardResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return studioGRPORewardResponse{}, fmt.Errorf("reward service returned HTTP %d", response.StatusCode)
	}
	var result studioGRPORewardResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxGRPORewardResponse))
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode reward response: %w", err)
	}
	return result, nil
}

type studioGRPOScriptProvider struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  <-chan string
	errors <-chan error
	mu     sync.Mutex
}

func newStudioGRPOScriptProvider(ctx context.Context, path string, task *Task) (*studioGRPOScriptProvider, error) {
	cmd := exec.CommandContext(ctx, "python3", "-u", path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	lines, scanErrors := studioScanner(stdout)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
		for scanner.Scan() {
			task.AppendLog("[reward] " + scanner.Text())
		}
	}()
	return &studioGRPOScriptProvider{cmd: cmd, stdin: stdin, lines: lines, errors: scanErrors}, nil
}

func (p *studioGRPOScriptProvider) Score(ctx context.Context, request studioGRPORewardRequest) (studioGRPORewardResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	encoded, _ := json.Marshal(request)
	if _, err := fmt.Fprintf(p.stdin, "%s\n", encoded); err != nil {
		return studioGRPORewardResponse{}, err
	}
	select {
	case <-ctx.Done():
		return studioGRPORewardResponse{}, ctx.Err()
	case err := <-p.errors:
		if err == nil {
			err = io.EOF
		}
		return studioGRPORewardResponse{}, err
	case line, ok := <-p.lines:
		if !ok {
			return studioGRPORewardResponse{}, io.EOF
		}
		var response studioGRPORewardResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			return response, fmt.Errorf("reward script returned invalid JSON: %w", err)
		}
		if response.Error != "" {
			return response, fmt.Errorf("reward script: %s", response.Error)
		}
		return response, nil
	}
}

func (p *studioGRPOScriptProvider) Close() error {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.cmd.Wait()
}

func newStudioGRPORewardProvider(ctx context.Context, req TrainQLoRARequest, modelsDir string, task *Task) (studioGRPORewardProvider, error) {
	timeout := time.Duration(req.GRPORewardTimeout) * time.Second
	if timeout == 0 {
		timeout = defaultGRPORewardTimeout
	}
	switch req.GRPORewardProvider {
	case "builtin":
		return &studioGRPOBuiltinProvider{mode: req.GRPOBuiltinReward, caseSensitive: req.GRPOCaseSensitive, tolerance: req.GRPONumericTolerance}, nil
	case "script":
		path, _, err := resolveStudioInput(modelsDir, req.GRPORewardScript, ".py")
		if err != nil {
			return nil, err
		}
		return newStudioGRPOScriptProvider(ctx, path, task)
	case "http":
		return &studioGRPOHTTPProvider{url: req.GRPORewardURL, client: &http.Client{Timeout: timeout}}, nil
	default:
		return nil, fmt.Errorf("unsupported reward provider")
	}
}

func validateStudioGRPORewards(rewards []float64, expected int) error {
	if len(rewards) != expected {
		return fmt.Errorf("reward provider returned %d rewards for %d generations", len(rewards), expected)
	}
	for i, reward := range rewards {
		if math.IsNaN(reward) || math.IsInf(reward, 0) {
			return fmt.Errorf("reward %d is not finite", i+1)
		}
	}
	return nil
}

func normalizeStudioGRPORewards(rewards []float64) []float64 {
	result := make([]float64, len(rewards))
	if len(rewards) == 0 {
		return result
	}
	mean := 0.0
	for _, reward := range rewards {
		mean += reward
	}
	mean /= float64(len(rewards))
	variance := 0.0
	for _, reward := range rewards {
		variance += (reward - mean) * (reward - mean)
	}
	variance /= float64(len(rewards))
	stddev := math.Sqrt(variance)
	if stddev <= 1e-8 {
		for i := range result {
			result[i] = 0.5
		}
		return result
	}
	for i, reward := range rewards {
		result[i] = max(0, min(1, 0.5+(reward-mean)/stddev/6))
	}
	return result
}

var studioGRPOMessagePattern = regexp.MustCompile(`^\[QLORA:([A-Z_]+)(?::([^]]*))?\](?:\s*(.*))?$`)
var studioGRPOProgressPattern = regexp.MustCompile(`step=(\d+)(?:/(\d+))?`)

func studioGRPOUnescape(value string) string {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 >= len(value) {
			result.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		default:
			result.WriteByte(value[i])
		}
	}
	return result.String()
}

func studioGRPOEscape(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r").Replace(value)
}

func studioScanner(reader io.Reader) (<-chan string, <-chan error) {
	lines := make(chan string)
	errors := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		errors <- scanner.Err()
		close(errors)
	}()
	return lines, errors
}

func (tm *TaskManager) runStudioGRPO(task *Task, binary string, args []string, req TrainQLoRARequest, datasetPath, modelsDir, stagedAdapter, finalAdapter, outputName, stagedRollouts, finalRollouts string) {
	samples, err := loadStudioGRPOSamples(datasetPath, req.GRPOPromptField, req.GRPOReferenceField)
	if err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return
	}
	provider, err := newStudioGRPORewardProvider(task.Context(), req, modelsDir, task)
	if err != nil {
		task.UpdateProgress(TaskFailed, "start reward provider: "+err.Error(), 0)
		return
	}
	defer provider.Close()
	rollouts, err := os.OpenFile(stagedRollouts, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		task.UpdateProgress(TaskFailed, "create rollout artifact: "+err.Error(), 0)
		return
	}
	defer rollouts.Close()
	cmd := exec.CommandContext(task.Context(), binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		task.UpdateProgress(TaskFailed, "open trainer stdin: "+err.Error(), 0)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		task.UpdateProgress(TaskFailed, "open trainer stdout: "+err.Error(), 0)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		task.UpdateProgress(TaskFailed, "open trainer stderr: "+err.Error(), 0)
		return
	}
	if err := cmd.Start(); err != nil {
		task.UpdateProgress(TaskFailed, "start GRPO trainer: "+err.Error(), 0)
		return
	}
	task.UpdateProgress(TaskRunning, "Initializing GRPO trainer...", 0)
	lines, scanErrors := studioScanner(stdout)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
		for scanner.Scan() {
			task.AppendLog(scanner.Text())
		}
	}()
	writeCommand := func(value string) error {
		_, writeErr := fmt.Fprintln(stdin, value)
		return writeErr
	}
	step, currentIndex := 0, 0
	var generations []string
	done := false
	for !done {
		select {
		case <-task.Context().Done():
			_ = writeCommand("STOP")
			return
		case scanErr := <-scanErrors:
			if scanErr != nil {
				task.UpdateProgress(TaskFailed, "read GRPO protocol: "+scanErr.Error(), 0)
				return
			}
			scanErrors = nil
		case line, ok := <-lines:
			if !ok {
				lines = nil
				if !done {
					err = errors.New("GRPO trainer closed the protocol stream before DONE")
				}
				continue
			}
			match := studioGRPOMessagePattern.FindStringSubmatch(line)
			if match == nil {
				task.AppendLog("[trainer stdout] " + line)
				continue
			}
			typeName, sequence, payload := match[1], match[2], match[3]
			switch typeName {
			case "READY":
				task.AppendLog("GRPO trainer ready")
			case "PROMPT_REQ":
				step, _ = strconv.Atoi(sequence)
				if step <= 0 {
					step++
				}
				currentIndex = (step - 1) % len(samples)
				generations = nil
				if err = writeCommand("PROMPT " + studioGRPOEscape(samples[currentIndex].Prompt)); err != nil {
					done = true
				}
			case "GEN":
				generations = append(generations, studioGRPOUnescape(payload))
			case "REWARD_REQ":
				expected, _ := strconv.Atoi(sequence)
				if expected == 0 {
					expected = len(generations)
				}
				timeout := time.Duration(req.GRPORewardTimeout) * time.Second
				if timeout == 0 {
					timeout = defaultGRPORewardTimeout
				}
				rewardCtx, cancel := context.WithTimeout(task.Context(), timeout)
				response, scoreErr := provider.Score(rewardCtx, studioGRPORewardRequest{Step: step, Prompt: samples[currentIndex].Prompt, Generations: generations, Reference: samples[currentIndex].Reference, Sample: samples[currentIndex].Record})
				cancel()
				if scoreErr != nil {
					err = fmt.Errorf("score GRPO step %d: %w", step, scoreErr)
					done = true
					continue
				}
				if response.Error != "" {
					err = fmt.Errorf("score GRPO step %d: %s", step, response.Error)
					done = true
					continue
				}
				if err = validateStudioGRPORewards(response.Rewards, expected); err != nil {
					done = true
					continue
				}
				advantages := normalizeStudioGRPORewards(response.Rewards)
				if encodeErr := json.NewEncoder(rollouts).Encode(studioGRPORollout{Step: step, Prompt: samples[currentIndex].Prompt, Generations: generations, RawRewards: response.Rewards, Advantages: advantages, Details: response.Details}); encodeErr != nil {
					err, done = encodeErr, true
					continue
				}
				values := make([]string, len(advantages))
				for i, value := range advantages {
					values[i] = strconv.FormatFloat(value, 'f', 6, 64)
				}
				task.AppendLog(fmt.Sprintf("GRPO step %d rewards=%v advantages=%v", step, response.Rewards, advantages))
				if err = writeCommand("REWARD " + strings.Join(values, " ")); err != nil {
					done = true
				}
			case "PROGRESS":
				pct := 0
				if progress := studioGRPOProgressPattern.FindStringSubmatch(payload); progress != nil {
					current, _ := strconv.Atoi(progress[1])
					total, _ := strconv.Atoi(progress[2])
					if total > 0 {
						pct = current * 100 / total
					}
				}
				task.UpdateProgress(TaskRunning, payload, pct)
			case "CHECKPOINT":
				task.AppendLog("GRPO checkpoint: " + payload)
			case "ERROR":
				err, done = errors.New(payload), true
			case "DONE":
				task.AppendLog("GRPO complete: " + payload)
				done = true
			}
		}
		if lines == nil && err != nil {
			break
		}
	}
	_ = stdin.Close()
	if err != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if task.Context().Err() != nil {
		return
	}
	if err == nil {
		err = waitErr
	}
	if err != nil {
		task.UpdateProgress(TaskFailed, "GRPO training failed: "+err.Error(), 0)
		return
	}
	if err = rollouts.Close(); err != nil {
		task.UpdateProgress(TaskFailed, "finalize rollout artifact: "+err.Error(), 0)
		return
	}
	err = publishStudioGRPOOutputs(stagedAdapter, finalAdapter, stagedRollouts, finalRollouts)
	if err != nil {
		task.UpdateProgress(TaskFailed, "publish GRPO outputs: "+err.Error(), 0)
		return
	}
	for _, artifact := range []struct{ path, name, kind string }{{finalAdapter, outputName, "lora-adapter"}, {finalRollouts, filepath.Base(finalRollouts), "grpo-rollouts"}} {
		if info, statErr := os.Stat(artifact.path); statErr == nil {
			rel, _ := filepath.Rel(modelsDir, artifact.path)
			task.AddArtifact(Artifact{Name: artifact.name, Path: filepath.ToSlash(rel), Size: info.Size(), Kind: artifact.kind})
		}
	}
	task.SetExitCode(0)
	task.UpdateProgress(TaskCompleted, "GRPO training complete", 100)
}

func publishStudioGRPOOutputs(stagedAdapter, finalAdapter, stagedRollouts, finalRollouts string) error {
	for _, path := range []string{finalAdapter, finalRollouts} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("output already exists: %s", filepath.Base(path))
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(stagedRollouts, finalRollouts); err != nil {
		return err
	}
	if err := os.Rename(stagedAdapter, finalAdapter); err != nil {
		if rollbackErr := os.Rename(finalRollouts, stagedRollouts); rollbackErr != nil {
			return fmt.Errorf("publish adapter: %w (rollout rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	return nil
}
