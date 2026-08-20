package mantle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DistillRequest synthesizes an SFT dataset by running a source dataset through
// an OpenAI-compatible chat-completions endpoint. Flat-prompt source records
// ({"prompt": "..."} or a custom PromptField) produce {"prompt","response"}
// output lines. Chat-format source records ({"messages":[...]}) are expanded
// into one {"messages":[...]} output line per assistant turn, each a growing,
// self-consistent conversation prefix ending in a freshly generated reply -
// the schema train-qlora's multi-turn "messages" mode consumes, since the
// underlying trainer only supervises the final assistant turn of each JSONL
// line (see finetune_qlora.cpp's last_assistant_index handling).
type DistillRequest struct {
	SourceDataset   string   `json:"sourceDataset"`
	PromptField     string   `json:"promptField,omitempty"`
	Output          string   `json:"output"`
	Shuffle         bool     `json:"shuffle,omitempty"`
	Seed            int      `json:"seed,omitempty"`
	MaxSamples      int      `json:"maxSamples,omitempty"`
	ServerURL       string   `json:"serverUrl"`
	APIKey          string   `json:"apiKey,omitempty"`
	Model           string   `json:"model"`
	SystemPrompt    string   `json:"systemPrompt,omitempty"`
	Temperature     float64  `json:"temperature,omitempty"`
	TopP            float64  `json:"topP,omitempty"`
	TopK            int      `json:"topK,omitempty"`
	MaxTokens       int      `json:"maxTokens,omitempty"`
	ReasoningEffort string   `json:"reasoningEffort,omitempty"`
	Stop            []string `json:"stop,omitempty"`
	Concurrency     int      `json:"concurrency,omitempty"`
	TimeoutSeconds  int      `json:"timeoutSeconds,omitempty"`
	Retries         int      `json:"retries,omitempty"`
	// LastTurnOnly regenerates only the final assistant turn of a chat source
	// record, copying every earlier turn through verbatim. The default (false)
	// regenerates every assistant turn in sequence, so the whole retrace comes
	// from one consistent teacher rather than mixing original and fresh text.
	LastTurnOnly bool `json:"lastTurnOnly,omitempty"`
}

const (
	defaultDistillConcurrency = 4
	maxDistillConcurrency     = 16
	defaultDistillRetries     = 2
	maxDistillRetries         = 5
	defaultDistillTimeout     = 120 * time.Second
	maxDistillTimeoutSeconds  = 600
	maxDistillResponseBytes   = 8 * 1024 * 1024
	maxDistillRecords         = 100000
)

func validateStudioDistillRequest(req DistillRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("distillation requires a model name")
	}
	parsed, err := url.Parse(req.ServerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("distillation server URL must be an HTTP(S) URL without embedded credentials")
	}
	if !stringAllowed(req.ReasoningEffort, "", "low", "medium", "high") {
		return fmt.Errorf("unsupported reasoning effort %q", req.ReasoningEffort)
	}
	if req.MaxSamples < 0 || req.Seed < 0 {
		return fmt.Errorf("maximum samples and seed must not be negative")
	}
	if req.Temperature < 0 || req.TopP < 0 || req.TopK < 0 || req.MaxTokens < 0 {
		return fmt.Errorf("sampling options must not be negative")
	}
	if req.Concurrency < 0 || req.Concurrency > maxDistillConcurrency {
		return fmt.Errorf("concurrency must be between 1 and %d", maxDistillConcurrency)
	}
	if req.Retries < 0 || req.Retries > maxDistillRetries {
		return fmt.Errorf("retries must be between 0 and %d", maxDistillRetries)
	}
	if req.TimeoutSeconds < 0 || req.TimeoutSeconds > maxDistillTimeoutSeconds {
		return fmt.Errorf("timeout must be between 1 and %d seconds", maxDistillTimeoutSeconds)
	}
	return nil
}

func (tm *TaskManager) StartDistill(req DistillRequest, modelsDir string) (*Task, error) {
	req.PromptField = strings.TrimSpace(req.PromptField)
	if req.PromptField == "" {
		req.PromptField = "prompt"
	}
	req.ReasoningEffort = strings.ToLower(strings.TrimSpace(req.ReasoningEffort))
	if err := validateStudioDistillRequest(req); err != nil {
		return nil, err
	}
	sourcePath, sourceName, err := resolveStudioInput(modelsDir, req.SourceDataset, ".jsonl")
	if err != nil {
		return nil, fmt.Errorf("source dataset: %w", err)
	}
	outputPath, outputName, err := resolveStudioOutput(modelsDir, req.Output, ".jsonl")
	if err != nil {
		return nil, err
	}
	if req.Concurrency == 0 {
		req.Concurrency = defaultDistillConcurrency
	}
	if req.Retries == 0 {
		req.Retries = defaultDistillRetries
	}
	timeout := defaultDistillTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	task := tm.newStudioTask("distill", sourceName, outputName, map[string]any{
		"sourceDataset": sourceName, "promptField": req.PromptField, "shuffle": req.Shuffle,
		"maxSamples": req.MaxSamples, "serverUrl": req.ServerURL, "model": req.Model,
		"temperature": req.Temperature, "topP": req.TopP, "topK": req.TopK, "maxTokens": req.MaxTokens,
		"reasoningEffort": req.ReasoningEffort, "concurrency": req.Concurrency, "retries": req.Retries,
		"lastTurnOnly": req.LastTurnOnly,
	})
	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobIO, []string{outputPath}, func() {
		tm.runStudioDistill(task, req, sourcePath, outputPath, outputName, timeout)
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

// studioDistillMessage is one chat turn, reused both for the OpenAI-compatible
// wire request and for source/output "messages" JSONL records.
type studioDistillMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// studioDistillConversation is one parsed source record: either a flat prompt
// or a chat message history to expand turn by turn.
type studioDistillConversation struct {
	IsChat   bool
	Prompt   string
	Messages []studioDistillMessage
}

type studioDistillRecord struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

type studioDistillChatRecord struct {
	Messages []studioDistillMessage `json:"messages"`
}

// loadStudioDistillConversations parses a JSONL source dataset. Records with a
// non-empty "messages" array (matching the convention in datasetRecordFormat)
// are treated as chat conversations; everything else falls back to a flat
// string field named by promptField (dot-path via studioGRPOField).
func loadStudioDistillConversations(path, promptField string) ([]studioDistillConversation, error) {
	if strings.ToLower(filepath.Ext(path)) != ".jsonl" {
		return nil, fmt.Errorf("distill source datasets must be JSONL")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var conversations []studioDistillConversation
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxDatasetRecordBytes)
	for line := 1; scanner.Scan(); line++ {
		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("distill dataset line %d: %w", line, err)
		}
		if messagesRaw, ok := fields["messages"]; ok {
			conversation, convErr := parseStudioDistillChatRecord(messagesRaw)
			if convErr != nil {
				return nil, fmt.Errorf("distill dataset line %d: %w", line, convErr)
			}
			conversations = append(conversations, conversation)
		} else {
			var record map[string]any
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, fmt.Errorf("distill dataset line %d: %w", line, err)
			}
			promptValue, ok := studioGRPOField(record, promptField)
			prompt, stringOK := promptValue.(string)
			if !ok || !stringOK || strings.TrimSpace(prompt) == "" {
				return nil, fmt.Errorf("distill dataset line %d must contain a messages array or a non-empty string field %q", line, promptField)
			}
			conversations = append(conversations, studioDistillConversation{Prompt: prompt})
		}
		if len(conversations) > maxDistillRecords {
			return nil, fmt.Errorf("distill dataset exceeds %d records", maxDistillRecords)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(conversations) == 0 {
		return nil, fmt.Errorf("distill dataset contains no records")
	}
	return conversations, nil
}

func parseStudioDistillChatRecord(messagesRaw json.RawMessage) (studioDistillConversation, error) {
	var parsed []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messagesRaw, &parsed); err != nil {
		return studioDistillConversation{}, fmt.Errorf("invalid messages array: %w", err)
	}
	if len(parsed) == 0 {
		return studioDistillConversation{}, fmt.Errorf("messages array is empty")
	}
	messages := make([]studioDistillMessage, len(parsed))
	for i, m := range parsed {
		if strings.TrimSpace(m.Role) == "" {
			return studioDistillConversation{}, fmt.Errorf("message %d has no role", i+1)
		}
		var content string
		if err := json.Unmarshal(m.Content, &content); err != nil {
			return studioDistillConversation{}, fmt.Errorf("message %d content must be a plain string", i+1)
		}
		messages[i] = studioDistillMessage{Role: m.Role, Content: content}
	}
	return studioDistillConversation{IsChat: true, Messages: messages}, nil
}

// studioDistillBoundaries returns, in order, the message indices whose
// assistant turn should be (re)generated. -1 marks an implicit trailing
// boundary: the conversation doesn't end on an assistant turn, so a new final
// reply is appended. This mirrors finetune_qlora.cpp's own last-assistant-turn
// search, just applied at every position instead of only the last.
func studioDistillBoundaries(messages []studioDistillMessage) []int {
	var boundaries []int
	for i, m := range messages {
		if m.Role == "assistant" {
			boundaries = append(boundaries, i)
		}
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "assistant" {
		boundaries = append(boundaries, -1)
	}
	return boundaries
}

func studioDistillEffectiveBoundaries(conv studioDistillConversation, lastTurnOnly bool) []int {
	boundaries := studioDistillBoundaries(conv.Messages)
	if lastTurnOnly && len(boundaries) > 1 {
		boundaries = boundaries[len(boundaries)-1:]
	}
	return boundaries
}

func studioDistillCountUnits(conv studioDistillConversation, lastTurnOnly bool) int {
	if !conv.IsChat {
		return 1
	}
	return len(studioDistillEffectiveBoundaries(conv, lastTurnOnly))
}

func studioDistillHasSystemMessage(messages []studioDistillMessage) bool {
	for _, m := range messages {
		if m.Role == "system" {
			return true
		}
	}
	return false
}

// studioDistillProcessConversation walks one conversation's turn boundaries in
// order, calling the teacher for each and emitting one output record per
// generated turn. Turns within a conversation are sequential by necessity:
// each boundary's context includes every earlier turn's freshly regenerated
// content, not the original source text, so the whole retrace stays
// self-consistent with a single teacher. emit is called once per generated
// unit (unitErr set on failure, which stops processing the rest of this
// conversation but leaves already-emitted turns intact).
func studioDistillProcessConversation(ctx context.Context, client *http.Client, req DistillRequest, conv studioDistillConversation, emit func(record any, unitErr error)) {
	if !conv.IsChat {
		messages := make([]studioDistillMessage, 0, 2)
		if strings.TrimSpace(req.SystemPrompt) != "" {
			messages = append(messages, studioDistillMessage{Role: "system", Content: req.SystemPrompt})
		}
		messages = append(messages, studioDistillMessage{Role: "user", Content: conv.Prompt})
		content, err := studioDistillComplete(ctx, client, req, messages)
		if err != nil {
			emit(nil, err)
			return
		}
		emit(studioDistillRecord{Prompt: conv.Prompt, Response: content}, nil)
		return
	}

	boundaries := studioDistillEffectiveBoundaries(conv, req.LastTurnOnly)
	running := make([]studioDistillMessage, 0, len(conv.Messages)+len(boundaries))
	if strings.TrimSpace(req.SystemPrompt) != "" && !studioDistillHasSystemMessage(conv.Messages) {
		running = append(running, studioDistillMessage{Role: "system", Content: req.SystemPrompt})
	}
	cursor := 0
	for _, boundary := range boundaries {
		end := boundary
		if boundary == -1 {
			end = len(conv.Messages)
		}
		running = append(running, conv.Messages[cursor:end]...)
		content, err := studioDistillComplete(ctx, client, req, running)
		if err != nil {
			emit(nil, err)
			return
		}
		running = append(running, studioDistillMessage{Role: "assistant", Content: content})
		emit(studioDistillChatRecord{Messages: append([]studioDistillMessage(nil), running...)}, nil)
		if boundary == -1 {
			cursor = len(conv.Messages)
		} else {
			cursor = boundary + 1
		}
	}
}

func (tm *TaskManager) runStudioDistill(task *Task, req DistillRequest, sourcePath, outputPath, outputName string, timeout time.Duration) {
	conversations, err := loadStudioDistillConversations(sourcePath, req.PromptField)
	if err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return
	}
	if req.Shuffle {
		seed := int64(req.Seed)
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
		rng := rand.New(rand.NewSource(seed))
		rng.Shuffle(len(conversations), func(i, j int) { conversations[i], conversations[j] = conversations[j], conversations[i] })
	}
	if req.MaxSamples > 0 && req.MaxSamples < len(conversations) {
		conversations = conversations[:req.MaxSamples]
	}
	total := 0
	for _, conv := range conversations {
		total += studioDistillCountUnits(conv, req.LastTurnOnly)
	}

	staged := studioStagingPath(outputPath, task.ID)
	file, err := os.Create(staged)
	if err != nil {
		task.UpdateProgress(TaskFailed, "create staged output: "+err.Error(), 0)
		return
	}
	defer os.Remove(staged)

	task.UpdateProgress(TaskRunning, fmt.Sprintf("Distilling 0/%d turns...", total), 0)
	client := &http.Client{Timeout: timeout}

	type result struct {
		record any
		err    error
	}
	jobs := make(chan int)
	results := make(chan result)
	var wg sync.WaitGroup
	for w := 0; w < req.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				studioDistillProcessConversation(task.Context(), client, req, conversations[index], func(record any, unitErr error) {
					results <- result{record: record, err: unitErr}
				})
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range conversations {
			select {
			case <-task.Context().Done():
				return
			case jobs <- i:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var done, failed int64
	encoder := json.NewEncoder(file)
	for res := range results {
		if res.err != nil {
			atomic.AddInt64(&failed, 1)
			task.AppendLog("turn failed: " + res.err.Error())
		} else {
			if encodeErr := encoder.Encode(res.record); encodeErr != nil {
				task.UpdateProgress(TaskFailed, "write dataset record: "+encodeErr.Error(), 0)
				_ = file.Close()
				return
			}
			atomic.AddInt64(&done, 1)
		}
		completed := atomic.LoadInt64(&done) + atomic.LoadInt64(&failed)
		pct := 0
		if total > 0 {
			pct = int(completed) * 100 / total
		}
		task.UpdateProgress(TaskRunning, fmt.Sprintf("Distilled %d/%d turns (%d failed)", atomic.LoadInt64(&done), total, atomic.LoadInt64(&failed)), pct)
	}
	if closeErr := file.Close(); closeErr != nil {
		task.UpdateProgress(TaskFailed, "finalize staged output: "+closeErr.Error(), 0)
		return
	}
	if task.Context().Err() != nil && done == 0 {
		task.UpdateProgress(TaskCancelled, "Distillation cancelled", 0)
		return
	}
	if done == 0 {
		task.UpdateProgress(TaskFailed, "every turn failed to distill", 0)
		return
	}
	if err := publishStudioFile(staged, outputPath); err != nil {
		task.UpdateProgress(TaskFailed, "publish dataset: "+err.Error(), 0)
		return
	}
	info, statErr := os.Stat(outputPath)
	size := int64(0)
	if statErr == nil {
		size = info.Size()
	}
	task.AddArtifact(Artifact{Name: outputName, Path: outputName, Size: size, Kind: "dataset"})
	task.SetExitCode(0)
	message := fmt.Sprintf("Distilled %d/%d turns (%d failed)", done, total, failed)
	if task.Context().Err() != nil {
		task.UpdateProgress(TaskCompleted, message+" (cancelled early)", 100)
		return
	}
	task.UpdateProgress(TaskCompleted, message, 100)
}

type studioDistillChatRequest struct {
	Model           string                 `json:"model"`
	Messages        []studioDistillMessage `json:"messages"`
	Temperature     float64                `json:"temperature,omitempty"`
	TopP            float64                `json:"top_p,omitempty"`
	TopK            int                    `json:"top_k,omitempty"`
	MaxTokens       int                    `json:"max_tokens,omitempty"`
	Stop            []string               `json:"stop,omitempty"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
}

type studioDistillChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func studioDistillComplete(ctx context.Context, client *http.Client, req DistillRequest, messages []studioDistillMessage) (string, error) {
	body := studioDistillChatRequest{
		Model: req.Model, Messages: messages, Temperature: req.Temperature, TopP: req.TopP,
		TopK: req.TopK, MaxTokens: req.MaxTokens, Stop: req.Stop, ReasoningEffort: req.ReasoningEffort,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	attempts := req.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		content, err := studioDistillRequestOnce(ctx, client, req.ServerURL, req.APIKey, encoded)
		if err == nil {
			return content, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func studioDistillRequestOnce(ctx context.Context, client *http.Client, serverURL, apiKey string, body []byte) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if text := strings.TrimSpace(string(detail)); text != "" {
			return "", fmt.Errorf("server returned HTTP %d: %s", resp.StatusCode, text)
		}
		return "", fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	var decoded studioDistillChatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDistillResponseBytes)).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return "", fmt.Errorf("server error: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("server returned no completion")
	}
	return decoded.Choices[0].Message.Content, nil
}
