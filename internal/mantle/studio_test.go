package mantle

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveStudioPath_RejectsTraversalAndAbsolutePaths(t *testing.T) {
	modelsDir := t.TempDir()
	for _, name := range []string{"../model.gguf", "a/../../model.gguf", "/tmp/model.gguf", ""} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := resolveStudioPath(modelsDir, name, ".gguf"); err == nil {
				t.Fatalf("resolveStudioPath(%q) unexpectedly succeeded", name)
			}
		})
	}
}

func TestResolveStudioOutput_RejectsExistingFile(t *testing.T) {
	modelsDir := t.TempDir()
	path := filepath.Join(modelsDir, "existing.gguf")
	if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveStudioOutput(modelsDir, "existing.gguf", ".gguf"); err == nil {
		t.Fatal("resolveStudioOutput unexpectedly allowed an existing file")
	}
}

func TestPublishStudioFile_AtomicallyPublishesRegularFile(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "model.gguf")
	stagedPath := studioStagingPath(finalPath, "task-1")
	if err := os.WriteFile(stagedPath, []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishStudioFile(stagedPath, finalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("staged output still exists: %v", err)
	}
	got, err := os.ReadFile(finalPath)
	if err != nil || string(got) != "model" {
		t.Fatalf("published output = %q, %v", got, err)
	}
}

func TestPublishStudioFile_DoesNotReplaceDestination(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "model.gguf")
	stagedPath := studioStagingPath(finalPath, "task-1")
	if err := os.WriteFile(stagedPath, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(finalPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishStudioFile(stagedPath, finalPath); err == nil {
		t.Fatal("publishStudioFile unexpectedly replaced destination")
	}
	got, err := os.ReadFile(finalPath)
	if err != nil || string(got) != "existing" {
		t.Fatalf("destination = %q, %v", got, err)
	}
}

func TestPublishStudioFileSet_PublishesAllOutputs(t *testing.T) {
	dir := t.TempDir()
	finalBase := filepath.Join(dir, "model.gguf")
	stagedBase := studioStagingPath(finalBase, "task-1")
	stagedStem := strings.TrimSuffix(stagedBase, ".gguf")
	for _, suffix := range []string{"-00001-of-00002.gguf", "-00002-of-00002.gguf"} {
		if err := os.WriteFile(stagedStem+suffix, []byte(suffix), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := publishStudioFileSet(stagedBase, finalBase); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-00001-of-00002.gguf", "-00002-of-00002.gguf"} {
		if _, err := os.Stat(strings.TrimSuffix(finalBase, ".gguf") + suffix); err != nil {
			t.Fatalf("published shard %q: %v", suffix, err)
		}
	}
}

func TestPublishStudioFileSet_ValidatesEveryDestinationFirst(t *testing.T) {
	dir := t.TempDir()
	finalBase := filepath.Join(dir, "adapter.gguf")
	stagedBase := studioStagingPath(finalBase, "task-1")
	stagedStem := strings.TrimSuffix(stagedBase, ".gguf")
	finalStem := strings.TrimSuffix(finalBase, ".gguf")
	for _, suffix := range []string{".gguf", "-checkpoint.gguf"} {
		if err := os.WriteFile(stagedStem+suffix, []byte("new"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(finalStem+"-checkpoint.gguf", []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishStudioFileSet(stagedBase, finalBase); err == nil {
		t.Fatal("publishStudioFileSet unexpectedly replaced an output")
	}
	if _, err := os.Stat(finalBase); !os.IsNotExist(err) {
		t.Fatalf("first output was published before validation completed: %v", err)
	}
}

func TestPublishStudioDirectory_PublishesDirectory(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "profiles")
	stagedPath := studioStagingDirectory(finalPath, "task-1")
	if err := os.Mkdir(stagedPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedPath, "profile.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishStudioDirectory(stagedPath, finalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(finalPath, "profile.json")); err != nil {
		t.Fatal(err)
	}
}

func TestInspectStudioModel_ReadsMetadata(t *testing.T) {
	modelsDir := t.TempDir()
	path := filepath.Join(modelsDir, "model.gguf")
	data := minimalGGUF(t, map[string]string{"general.name": "Studio Test"})
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	inspection, err := InspectStudioModel(modelsDir, "model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Name != "model.gguf" || inspection.Version != 3 {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
	if got := inspection.Metadata["general.name"]; got != "Studio Test" {
		t.Fatalf("general.name = %v", got)
	}
}

func TestGGUFPositiveInt_ConvertsContextLengths(t *testing.T) {
	for _, test := range []struct {
		value any
		want  int
	}{
		{uint32(75008), 75008},
		{uint64(131072), 131072},
		{int32(4096), 4096},
		{int64(8192), 8192},
		{int64(-1), 0},
		{"75008", 0},
	} {
		if got := ggufPositiveInt(test.value); got != test.want {
			t.Errorf("ggufPositiveInt(%T(%v)) = %d, want %d", test.value, test.value, got, test.want)
		}
	}
}

func TestQuantizeArgs_RequantizeDryRun(t *testing.T) {
	req := QuantizeRequest{
		Type:              "Q5_K_M",
		AllowRequantize:   true,
		LeaveOutputTensor: true,
		Pure:              true,
		DryRun:            true,
		Threads:           8,
	}
	got := quantizeArgs(req, "/models/in.gguf", "", "/models/model.imatrix")
	want := []string{
		"--allow-requantize", "--leave-output-tensor", "--pure", "--dry-run",
		"--imatrix", "/models/model.imatrix", "/models/in.gguf", "Q5_K_M", "8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quantizeArgs() = %q, want %q", got, want)
	}
}

func TestQuantizeProgress_ParsesTensorProgress(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"[   1/ 314] tensor", 0},
		{"[ 157/ 314] tensor", 50},
		{"[ 314/ 314] tensor", 99},
		{"llama_model_quantize_impl: model size", -1},
	}
	for _, tt := range tests {
		if got := quantizeProgress(tt.line); got != tt.want {
			t.Errorf("quantizeProgress(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestSplitOutputGlob_UsesGGUFStem(t *testing.T) {
	got := splitOutputGlob(filepath.Join("models", "part.gguf"))
	want := filepath.Join("models", "part*.gguf")
	if got != want {
		t.Fatalf("splitOutputGlob() = %q, want %q", got, want)
	}
}

func TestResolveStudioOutputDirectory_RejectsExistingDirectory(t *testing.T) {
	modelsDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(modelsDir, "profiles"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveStudioOutputDirectory(modelsDir, "profiles"); err == nil {
		t.Fatal("expected existing output directory to be rejected")
	}
}

func TestAppendPruneOptions_BuildsApprovedArguments(t *testing.T) {
	evaluate := false
	args := []string{"analyze"}
	appendPruneOptions(&args, PruneRequest{
		PPLMask: "assistant", MaxLayerRatio: 0.4, Evaluate: &evaluate,
		ContextSize: 2048, Threads: 8, GPULayers: -1,
	})
	want := []string{"analyze", "--ppl-mask", "assistant", "--max-layer-ratio", "0.4", "--no-evaluate", "--ctx-size", "2048", "--threads", "8", "--n-gpu-layers", "-1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("appendPruneOptions() = %#v, want %#v", args, want)
	}
}

func minimalGGUF(t *testing.T, values map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	write := func(value any) {
		if err := binary.Write(&buf, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	write(ggufMagic)
	write(uint32(3))
	write(uint64(0))
	write(uint64(len(values)))
	for key, value := range values {
		write(uint64(len(key)))
		buf.WriteString(key)
		write(ggufTypeString)
		write(uint64(len(value)))
		buf.WriteString(value)
	}
	return buf.Bytes()
}
