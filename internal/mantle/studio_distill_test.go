package mantle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func writeDistillSourceDataset(t *testing.T, modelsDir string, prompts []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(modelsDir, "datasets"), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(modelsDir, "datasets", "prompts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, prompt := range prompts {
		if err := encoder.Encode(map[string]string{"prompt": prompt}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDistill_RejectsMissingModel(t *testing.T) {
	if err := validateStudioDistillRequest(DistillRequest{ServerURL: "http://127.0.0.1:8080/v1/chat/completions"}); err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestDistill_RejectsBadServerURL(t *testing.T) {
	tests := []string{"", "not-a-url", "ftp://host/v1/chat/completions", "http://user:pass@host/v1/chat/completions"}
	for _, url := range tests {
		if err := validateStudioDistillRequest(DistillRequest{Model: "big", ServerURL: url}); err == nil {
			t.Fatalf("expected error for server URL %q", url)
		}
	}
}

func TestDistill_RejectsUnsupportedReasoningEffort(t *testing.T) {
	req := DistillRequest{Model: "big", ServerURL: "http://127.0.0.1:8080/v1/chat/completions", ReasoningEffort: "extreme"}
	if err := validateStudioDistillRequest(req); err == nil {
		t.Fatal("expected error for unsupported reasoning effort")
	}
}

func TestDistill_GeneratesDatasetFromTeacherModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Errorf("Authorization header = %q, want Bearer secret-key", got)
		}
		var body studioDistillChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) == 0 {
			t.Fatal("expected at least one message")
		}
		prompt := body.Messages[len(body.Messages)-1].Content
		_ = json.NewEncoder(w).Encode(studioDistillChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: "echo: " + prompt}}},
		})
	}))
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillSourceDataset(t, modelsDir, []string{"2+2?", "capital of France?"})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/prompts.jsonl",
		Output:        "datasets/distilled.jsonl",
		ServerURL:     server.URL,
		APIKey:        "secret-key",
		Model:         "big-model",
		Concurrency:   2,
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}

	final := waitForTaskState(t, task, TaskCompleted)
	if len(final.Artifacts) != 1 || final.Artifacts[0].Kind != "dataset" {
		t.Fatalf("unexpected artifacts: %#v", final.Artifacts)
	}
	if _, persisted := final.Parameters["apiKey"]; persisted {
		t.Fatal("apiKey must not be persisted in task parameters")
	}

	file, err := os.Open(filepath.Join(modelsDir, "datasets", "distilled.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	records := map[string]string{}
	for scanner.Scan() {
		var record studioDistillRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records[record.Prompt] = record.Response
	}
	if len(records) != 2 || records["2+2?"] != "echo: 2+2?" || records["capital of France?"] != "echo: capital of France?" {
		t.Fatalf("unexpected records: %#v", records)
	}
}

func writeDistillChatSourceDataset(t *testing.T, modelsDir, filename string, messages []map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(modelsDir, "datasets"), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(modelsDir, "datasets", filename))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(map[string]any{"messages": messages}); err != nil {
		t.Fatal(err)
	}
}

func readJSONLLines(t *testing.T, path string) [][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines [][]byte
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	return lines
}

func newDistillEchoServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body studioDistillChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		last := body.Messages[len(body.Messages)-1].Content
		content := fmt.Sprintf("gen%d(ctx=%d,last=%s)", n, len(body.Messages), last)
		_ = json.NewEncoder(w).Encode(studioDistillChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: content}}},
		})
	}))
	return server, &calls
}

func TestDistill_ExpandsMultiTurnConversations(t *testing.T) {
	server, _ := newDistillEchoServer(t)
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillChatSourceDataset(t, modelsDir, "chat.jsonl", []map[string]string{
		{"role": "system", "content": "You are terse."},
		{"role": "user", "content": "2+2?"},
		{"role": "assistant", "content": "ORIGINAL-4"},
		{"role": "user", "content": "3+3?"},
		{"role": "assistant", "content": "ORIGINAL-6"},
	})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/chat.jsonl",
		Output:        "datasets/chat_out.jsonl",
		ServerURL:     server.URL,
		Model:         "big-model",
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskCompleted)

	lines := readJSONLLines(t, filepath.Join(modelsDir, "datasets", "chat_out.jsonl"))
	if len(lines) != 2 {
		t.Fatalf("got %d output lines, want 2", len(lines))
	}
	var rec1, rec2 studioDistillChatRecord
	if err := json.Unmarshal(lines[0], &rec1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &rec2); err != nil {
		t.Fatal(err)
	}
	if len(rec1.Messages) != 3 || len(rec2.Messages) != 5 {
		t.Fatalf("unexpected turn counts: rec1=%d rec2=%d", len(rec1.Messages), len(rec2.Messages))
	}
	if rec1.Messages[0].Role != "system" || rec1.Messages[0].Content != "You are terse." {
		t.Fatalf("system message not preserved: %#v", rec1.Messages[0])
	}
	generatedTurn1 := rec1.Messages[2].Content
	if generatedTurn1 == "ORIGINAL-4" {
		t.Fatal("expected the first assistant turn to be regenerated, not the original source text")
	}
	if rec2.Messages[2].Content != generatedTurn1 {
		t.Fatalf("second turn's context should reuse the regenerated first reply: got %q, want %q", rec2.Messages[2].Content, generatedTurn1)
	}
	if rec2.Messages[4].Content == "ORIGINAL-6" {
		t.Fatal("expected the second assistant turn to be regenerated, not the original source text")
	}
}

func TestDistill_LastTurnOnlyPreservesEarlierTurns(t *testing.T) {
	server, _ := newDistillEchoServer(t)
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillChatSourceDataset(t, modelsDir, "chat.jsonl", []map[string]string{
		{"role": "system", "content": "You are terse."},
		{"role": "user", "content": "2+2?"},
		{"role": "assistant", "content": "ORIGINAL-4"},
		{"role": "user", "content": "3+3?"},
		{"role": "assistant", "content": "ORIGINAL-6"},
	})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/chat.jsonl",
		Output:        "datasets/chat_out.jsonl",
		ServerURL:     server.URL,
		Model:         "big-model",
		LastTurnOnly:  true,
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskCompleted)

	lines := readJSONLLines(t, filepath.Join(modelsDir, "datasets", "chat_out.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("got %d output lines, want 1", len(lines))
	}
	var rec studioDistillChatRecord
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.Messages) != 5 {
		t.Fatalf("got %d messages, want 5", len(rec.Messages))
	}
	if rec.Messages[2].Content != "ORIGINAL-4" {
		t.Fatalf("earlier assistant turn should be preserved verbatim, got %q", rec.Messages[2].Content)
	}
	if rec.Messages[4].Content == "ORIGINAL-6" {
		t.Fatal("expected the final assistant turn to be regenerated")
	}
}

func TestDistill_PreservesExistingSystemMessage(t *testing.T) {
	server, _ := newDistillEchoServer(t)
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillChatSourceDataset(t, modelsDir, "chat.jsonl", []map[string]string{
		{"role": "system", "content": "Original persona."},
		{"role": "user", "content": "Hello"},
	})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/chat.jsonl",
		Output:        "datasets/chat_out.jsonl",
		ServerURL:     server.URL,
		Model:         "big-model",
		SystemPrompt:  "Override persona.",
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskCompleted)

	lines := readJSONLLines(t, filepath.Join(modelsDir, "datasets", "chat_out.jsonl"))
	var rec studioDistillChatRecord
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Messages[0].Content != "Original persona." {
		t.Fatalf("source system message should win over req.SystemPrompt, got %q", rec.Messages[0].Content)
	}
}

func TestDistill_InjectsSystemPromptWhenSourceHasNone(t *testing.T) {
	server, _ := newDistillEchoServer(t)
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillChatSourceDataset(t, modelsDir, "chat.jsonl", []map[string]string{
		{"role": "user", "content": "Hello"},
	})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/chat.jsonl",
		Output:        "datasets/chat_out.jsonl",
		ServerURL:     server.URL,
		Model:         "big-model",
		SystemPrompt:  "Injected persona.",
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskCompleted)

	lines := readJSONLLines(t, filepath.Join(modelsDir, "datasets", "chat_out.jsonl"))
	var rec studioDistillChatRecord
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Messages[0].Role != "system" || rec.Messages[0].Content != "Injected persona." {
		t.Fatalf("expected injected system message, got %#v", rec.Messages[0])
	}
}

func TestDistill_RejectsNonStringMessageContent(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(modelsDir, "datasets"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(modelsDir, "datasets", "bad.jsonl")
	if err := os.WriteFile(path, []byte(`{"messages":[{"role":"user","content":{"nested":true}}]}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadStudioDistillConversations(path, "prompt"); err == nil {
		t.Fatal("expected an error for non-string message content")
	}
}

func TestDistill_SurfacesServerErrorBodyInLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("model not found"))
	}))
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillSourceDataset(t, modelsDir, []string{"only prompt"})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/prompts.jsonl",
		Output:        "datasets/distilled.jsonl",
		ServerURL:     server.URL,
		Model:         "wrong-model-id",
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	final := waitForTaskState(t, task, TaskFailed)

	found := false
	for _, line := range final.Logs {
		if strings.Contains(line, "404") && strings.Contains(line, "model not found") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a log line with the 404 status and response body, got: %v", final.Logs)
	}
}

func TestDistill_AllSamplesFailingFailsTheTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	modelsDir := t.TempDir()
	writeDistillSourceDataset(t, modelsDir, []string{"only prompt"})

	tm := NewTaskManager(nil)
	task, err := tm.StartDistill(DistillRequest{
		SourceDataset: "datasets/prompts.jsonl",
		Output:        "datasets/distilled.jsonl",
		ServerURL:     server.URL,
		Model:         "big-model",
	}, modelsDir)
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, task, TaskFailed)
	if _, statErr := os.Stat(filepath.Join(modelsDir, "datasets", "distilled.jsonl")); !os.IsNotExist(statErr) {
		t.Fatal("expected no output dataset when every sample fails")
	}
}
