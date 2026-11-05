package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/shared"
	"github.com/tidwall/gjson"
)

func TestServer_ApplyFilters(t *testing.T) {
	t.Run("useModelName rewrite", func(t *testing.T) {
		out, err := applyFilters([]byte(`{"model":"alias","temp":1}`), "alias", "real-model", config.Filters{})
		if err != nil {
			t.Fatalf("applyFilters: %v", err)
		}
		if got := gjson.GetBytes(out, "model").String(); got != "real-model" {
			t.Errorf("model = %q, want real-model", got)
		}
	})

	t.Run("strip and set params", func(t *testing.T) {
		f := config.Filters{
			StripParams: "temperature",
			SetParams:   map[string]any{"top_p": 0.9},
		}
		out, err := applyFilters([]byte(`{"model":"m","temperature":0.7}`), "m", "", f)
		if err != nil {
			t.Fatalf("applyFilters: %v", err)
		}
		if gjson.GetBytes(out, "temperature").Exists() {
			t.Error("temperature should be stripped")
		}
		if got := gjson.GetBytes(out, "top_p").Float(); got != 0.9 {
			t.Errorf("top_p = %v, want 0.9", got)
		}
	})

	t.Run("setParamsByID overrides setParams", func(t *testing.T) {
		f := config.Filters{
			SetParams:     map[string]any{"top_p": 0.5},
			SetParamsByID: map[string]map[string]any{"alias": {"top_p": 0.1}},
		}
		out, err := applyFilters([]byte(`{"model":"alias"}`), "alias", "", f)
		if err != nil {
			t.Fatalf("applyFilters: %v", err)
		}
		if got := gjson.GetBytes(out, "top_p").Float(); got != 0.1 {
			t.Errorf("top_p = %v, want 0.1", got)
		}
	})

	t.Run("unset token limits are removed", func(t *testing.T) {
		tests := []struct {
			body     string
			kept     []string
			stripped []string
		}{
			{`{"max_tokens":0,"max_completion_tokens":0}`, nil, []string{"max_tokens", "max_completion_tokens"}},
			{`{"max_tokens":-1,"max_completion_tokens":null}`, nil, []string{"max_tokens", "max_completion_tokens"}},
			{`{"max_tokens":256,"max_completion_tokens":1}`, []string{"max_tokens", "max_completion_tokens"}, nil},
			{`{"max_tokens":"0","n_predict":0}`, []string{"max_tokens", "n_predict"}, nil}, // not a number; not a limit we own
		}
		for _, tt := range tests {
			out, err := applyFilters([]byte(tt.body), "m", "", config.Filters{})
			if err != nil {
				t.Fatalf("applyFilters(%s): %v", tt.body, err)
			}
			for _, k := range tt.kept {
				if !gjson.GetBytes(out, k).Exists() {
					t.Errorf("%s: %s removed, want kept", tt.body, k)
				}
			}
			for _, k := range tt.stripped {
				if gjson.GetBytes(out, k).Exists() {
					t.Errorf("%s: %s kept, want removed", tt.body, k)
				}
			}
		}
	})

	t.Run("setParams still sets a token limit", func(t *testing.T) {
		f := config.Filters{SetParams: map[string]any{"max_tokens": 4096}}
		out, err := applyFilters([]byte(`{"max_tokens":0}`), "m", "", f)
		if err != nil {
			t.Fatalf("applyFilters: %v", err)
		}
		if got := gjson.GetBytes(out, "max_tokens").Int(); got != 4096 {
			t.Errorf("max_tokens = %d, want 4096", got)
		}
	})
}

func TestServer_FormFilterMiddleware(t *testing.T) {
	cfg := config.Config{Models: map[string]config.ModelConfig{
		"whisper": {UseModelName: "whisper-large-v3"},
	}}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("model", "whisper")
	fw, _ := mw.CreateFormFile("file", "a.wav")
	fw.Write([]byte("xx"))
	mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())

	var gotModel, gotFilename, gotFileBody string
	var gotContext shared.ReqContextData
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(shared.MaxMultiPartSize); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		gotModel = r.MultipartForm.Value["model"][0]
		fileHeader := r.MultipartForm.File["file"][0]
		gotFilename = fileHeader.Filename
		file, err := fileHeader.Open()
		if err != nil {
			t.Errorf("open file: %v", err)
			return
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Errorf("read file: %v", err)
			return
		}
		gotFileBody = string(data)
		gotContext, _ = shared.ReadContext(r.Context())
	})
	CreateFormFilterMiddleware(cfg)(final).ServeHTTP(httptest.NewRecorder(), r)

	if gotModel != "whisper-large-v3" {
		t.Errorf("model rewritten to %q, want whisper-large-v3", gotModel)
	}
	if gotFilename != "a.wav" {
		t.Errorf("filename = %q, want a.wav", gotFilename)
	}
	if gotFileBody != "xx" {
		t.Errorf("file body = %q, want xx", gotFileBody)
	}
	if gotContext.Model != "whisper" || gotContext.ModelID != "whisper" {
		t.Errorf("request context = %+v, want original whisper model", gotContext)
	}
}
