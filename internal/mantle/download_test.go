package mantle

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFile_RestartsWhenServerIgnoresRange(t *testing.T) {
	content := []byte("complete dataset")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			t.Error("expected resume Range header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "dataset.jsonl")
	if err := os.WriteFile(target+".part", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	task := NewTaskManager(nil).CreateTask("download", "", "", "dataset")
	if _, err := downloadFile(task, server.URL, target, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("download was appended instead of restarted: %q", got)
	}
}
