package mantle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadStudioGRPOSamples_Fields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompts.jsonl")
	data := "{\"input\":{\"prompt\":\"2+2?\"},\"answer\":\"4\"}\n{\"input\":{\"prompt\":\"3+3?\"},\"answer\":6}\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	samples, err := loadStudioGRPOSamples(path, "input.prompt", "answer")
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].Prompt != "2+2?" || samples[0].Reference != "4" || samples[1].Reference != float64(6) {
		t.Fatalf("unexpected samples: %#v", samples)
	}
}

func TestStudioGRPOBuiltinProvider_Rewards(t *testing.T) {
	tests := []struct {
		name       string
		provider   studioGRPOBuiltinProvider
		reference  any
		generation []string
		want       []float64
	}{
		{"exact", studioGRPOBuiltinProvider{mode: "exact"}, "Paris", []string{"Paris", " paris "}, []float64{1, 1}},
		{"numeric", studioGRPOBuiltinProvider{mode: "numeric", tolerance: 0.01}, 3.14, []string{"3.141", "3.2"}, []float64{1, 0}},
		{"regex", studioGRPOBuiltinProvider{mode: "regex"}, `^ok\b`, []string{"OK result", "bad"}, []float64{1, 0}},
		{"json", studioGRPOBuiltinProvider{mode: "json-valid"}, nil, []string{`{"ok":true}`, "not json"}, []float64{1, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := test.provider.Score(context.Background(), studioGRPORewardRequest{Reference: test.reference, Generations: test.generation})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(response.Rewards, test.want) {
				t.Fatalf("rewards = %v, want %v", response.Rewards, test.want)
			}
		})
	}
}

func TestNormalizeStudioGRPORewards_GroupRelative(t *testing.T) {
	got := normalizeStudioGRPORewards([]float64{0, 1, 2})
	if !(got[0] < got[1] && got[1] < got[2]) || got[1] != 0.5 {
		t.Fatalf("unexpected normalized rewards: %v", got)
	}
	if equal := normalizeStudioGRPORewards([]float64{2, 2}); !reflect.DeepEqual(equal, []float64{0.5, 0.5}) {
		t.Fatalf("equal rewards = %v", equal)
	}
}

func TestStudioGRPOHTTPProvider_Score(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request studioGRPORewardRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Generations) != 2 || request.Reference != "answer" {
			t.Errorf("unexpected request: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(studioGRPORewardResponse{Rewards: []float64{0.25, 0.75}, Details: map[string]any{"judge": "test"}})
	}))
	defer server.Close()
	provider := studioGRPOHTTPProvider{url: server.URL, client: server.Client()}
	response, err := provider.Score(context.Background(), studioGRPORewardRequest{Generations: []string{"a", "b"}, Reference: "answer"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Rewards, []float64{0.25, 0.75}) {
		t.Fatalf("unexpected rewards: %v", response.Rewards)
	}
}

func TestValidateStudioGRPORewards_RejectsInvalid(t *testing.T) {
	if err := validateStudioGRPORewards([]float64{1}, 2); err == nil {
		t.Fatal("expected reward count error")
	}
}

func TestPublishStudioGRPOOutputs_PublishesPair(t *testing.T) {
	dir := t.TempDir()
	stagedAdapter := filepath.Join(dir, ".adapter.partial.gguf")
	stagedRollouts := filepath.Join(dir, ".rollouts.partial.jsonl")
	finalAdapter := filepath.Join(dir, "adapter.gguf")
	finalRollouts := filepath.Join(dir, "adapter.gguf.rollouts.jsonl")
	if err := os.WriteFile(stagedAdapter, []byte("adapter"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedRollouts, []byte("rollouts"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishStudioGRPOOutputs(stagedAdapter, finalAdapter, stagedRollouts, finalRollouts); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{finalAdapter, finalRollouts} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}
