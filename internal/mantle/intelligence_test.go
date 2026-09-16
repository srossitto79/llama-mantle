package mantle

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIntelligence_Proxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/run?example=1" || r.Method != "POST" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Error("client credentials forwarded")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"models":["test"]}` {
			t.Errorf("unexpected body: %s", body)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_id":"test"}`))
	}))
	defer upstream.Close()
	t.Setenv("LLAMA_INTELLIGENCE_URL", upstream.URL)
	mux := http.NewServeMux()
	(&Handler{}).registerIntelligenceRoutes(mux)
	r := httptest.NewRequest("POST", "/api/mantle/studio/intelligence/run?example=1", strings.NewReader(`{"models":["test"]}`))
	r.Header.Set("Authorization", "Bearer private")
	r.Header.Set("Cookie", "session=private")
	r.Header.Set("X-Api-Key", "private")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || w.Body.String() != `{"run_id":"test"}` {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
	}
}

func TestIntelligence_ProxyDelete(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runs/abc123" || r.Method != http.MethodDelete {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	t.Setenv("LLAMA_INTELLIGENCE_URL", upstream.URL)
	mux := http.NewServeMux()
	(&Handler{}).registerIntelligenceRoutes(mux)
	r := httptest.NewRequest(http.MethodDelete, "/api/mantle/studio/intelligence/runs/abc123", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
	}
}

func TestIntelligence_Unconfigured(t *testing.T) {
	t.Setenv("LLAMA_INTELLIGENCE_URL", "")
	mux := http.NewServeMux()
	(&Handler{}).registerIntelligenceRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/mantle/studio/intelligence/config", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestIntelligence_Stream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"event\":\"run_start\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	t.Setenv("LLAMA_INTELLIGENCE_URL", upstream.URL)
	mux := http.NewServeMux()
	(&Handler{}).registerIntelligenceRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/mantle/studio/intelligence/run/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buf := make([]byte, len("data: {\"event\":\"run_start\"}\n\n"))
	if _, err := io.ReadFull(response.Body, buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), "run_start") {
		t.Fatalf("unexpected event: %s", buf)
	}
}

func TestIntelligence_ProxySuiteAndHumanScores(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Method + " " + r.URL.Path))
	}))
	defer upstream.Close()
	t.Setenv("LLAMA_INTELLIGENCE_URL", upstream.URL)
	mux := http.NewServeMux()
	(&Handler{}).registerIntelligenceRoutes(mux)
	for _, tc := range []struct{ method, path, want string }{
		{"GET", "suite", "GET /api/suite"},
		{"GET", "human-scores", "GET /api/human-scores"},
		{"POST", "human-scores", "POST /api/human-scores"},
		{"GET", "results", "GET /api/results"},
	} {
		r := httptest.NewRequest(tc.method, "/api/mantle/studio/intelligence/"+tc.path, strings.NewReader("{}"))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != tc.want {
			t.Errorf("%s %s: got %d %q, want %q", tc.method, tc.path, w.Code, w.Body, tc.want)
		}
	}
}
