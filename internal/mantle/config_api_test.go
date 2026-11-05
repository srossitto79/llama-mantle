package mantle

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/configedit"
)

const httpTestSecret = "sk-http-secret-9876543210"

// newConfigTestServer serves the mantle routes over a copy of the configedit
// fixture, the same config.yaml shape production uses.
func newConfigTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	t.Setenv("TEST_CONFIGEDIT_KEY", httpTestSecret)
	raw, err := os.ReadFile(filepath.Join("..", "configedit", "testdata", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFromReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	cfg.WatchConfig = true
	h := NewHandler(NewTaskManager(nil), &cfg, path, filepath.Join(dir, "models"), filepath.Join(dir, "backends"), "")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, path
}

func doJSON(t *testing.T, method, url string, body any) (int, string) {
	t.Helper()
	var r *bytes.Reader
	switch b := body.(type) {
	case nil:
		r = bytes.NewReader(nil)
	case string:
		r = bytes.NewReader([]byte(b))
	default:
		data, _ := json.Marshal(b)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, url, r)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

func TestConfigEdit_HTTPResponsesNeverContainSecrets(t *testing.T) {
	srv, _ := newConfigTestServer(t)
	for _, path := range []string{"/api/mantle/config/models", "/api/mantle/config/source", "/api/mantle/config/groups"} {
		status, body := doJSON(t, "GET", srv.URL+path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, status, body)
		}
		if strings.Contains(body, httpTestSecret) {
			t.Fatalf("%s leaks the env secret", path)
		}
	}
	status, body := doJSON(t, "POST", srv.URL+"/api/mantle/config/validate", "models:\n  a:\n    cmd: x ${PORT}\n    ttl: ${env.TEST_CONFIGEDIT_KEY}\n")
	if status != http.StatusOK || strings.Contains(body, httpTestSecret) {
		t.Fatalf("validate leaks the env secret or failed: %d", status)
	}
}

func TestConfigEdit_HTTPPutModelKeepsSource(t *testing.T) {
	srv, path := newConfigTestServer(t)
	_, body := doJSON(t, "GET", srv.URL+"/api/mantle/config/models", nil)
	var models []map[string]any
	if err := json.Unmarshal([]byte(body), &models); err != nil {
		t.Fatal(err)
	}
	var strata map[string]any
	for _, m := range models {
		if m["id"] == "strata-ds" {
			strata = m
		}
	}
	strata["ttl"] = 77
	status, resp := doJSON(t, "PUT", srv.URL+"/api/mantle/config/models/strata-ds", strata)
	if status != http.StatusOK {
		t.Fatalf("put: %d %s", status, resp)
	}
	written, _ := os.ReadFile(path)
	if strings.Contains(string(written), httpTestSecret) {
		t.Fatal("secret written to config.yaml")
	}
	for _, want := range []string{"apiKey: ${env.TEST_CONFIGEDIT_KEY}", "STRATA_API_KEY=${env.TEST_CONFIGEDIT_KEY}", "ttl: 77", "filters: *qwen38-effort-with-high", "# Remote engine"} {
		if !strings.Contains(string(written), want) {
			t.Errorf("written file lost %q", want)
		}
	}
}

func TestConfigEdit_HTTPApplyIsAtomicAndChecksHash(t *testing.T) {
	srv, path := newConfigTestServer(t)
	before, _ := os.ReadFile(path)
	hash := configedit.Hash(before)

	// One invalid change fails the whole set; nothing is written.
	status, _ := doJSON(t, "POST", srv.URL+"/api/mantle/config/changes/apply", map[string]any{
		"baseHash": hash,
		"changes": []configedit.Change{
			{Op: "set", Section: "models", ID: "gemma-4b", Path: []string{"ttl"}, Value: 5},
			{Op: "set", Section: "models", ID: "gemma-4b", Path: []string{"ttl"}, Value: -3},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid set should be refused, got %d", status)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
		t.Fatal("refused change set modified the file")
	}

	status, body := doJSON(t, "POST", srv.URL+"/api/mantle/config/changes/preview", map[string]any{
		"baseHash": hash,
		"changes":  []configedit.Change{{Op: "rename", Section: "models", ID: "gemma-4b", NewID: "gemma-4b-it"}},
	})
	if status != http.StatusOK || !strings.Contains(body, `"reload":"all"`) {
		t.Fatalf("preview: %d %s", status, body)
	}

	status, _ = doJSON(t, "POST", srv.URL+"/api/mantle/config/changes/apply", map[string]any{
		"baseHash": hash,
		"changes":  []configedit.Change{{Op: "rename", Section: "models", ID: "gemma-4b", NewID: "gemma-4b-it"}},
	})
	if status != http.StatusOK {
		t.Fatalf("apply: %d", status)
	}
	// Same base hash again: the file changed, so it is stale.
	status, body = doJSON(t, "POST", srv.URL+"/api/mantle/config/changes/apply", map[string]any{
		"baseHash": hash,
		"changes":  []configedit.Change{{Op: "set", Section: "models", ID: "qwen-8b", Path: []string{"ttl"}, Value: 1}},
	})
	if status != http.StatusConflict || !strings.Contains(body, configedit.CodeStale) {
		t.Fatalf("stale apply: %d %s", status, body)
	}
}

func TestConfigEdit_HTTPPutRawReportsProblems(t *testing.T) {
	srv, _ := newConfigTestServer(t)
	status, body := doJSON(t, "PUT", srv.URL+"/api/mantle/config", "models:\n  a:\n    cmd: x\n   bad: [\n")
	if status != http.StatusBadRequest || !strings.Contains(body, `"line"`) {
		t.Fatalf("want problems with a line, got %d %s", status, body)
	}
}
