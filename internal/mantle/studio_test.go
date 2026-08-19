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
	got := quantizeArgs(req, "/models/in.gguf", "", "/models/model.imatrix", "")
	want := []string{
		"--allow-requantize", "--leave-output-tensor", "--pure", "--dry-run",
		"--imatrix", "/models/model.imatrix", "/models/in.gguf", "Q5_K_M", "8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quantizeArgs() = %q, want %q", got, want)
	}
}

func TestQuantizeArgs_AdvancedOverrides(t *testing.T) {
	req := QuantizeRequest{Type: "Q4_K_M", IncludeWeights: []string{"blk.*"}, OutputTensorType: "F32", TokenEmbeddingType: "F16", TensorTypes: []string{"blk.0=q8_0"}, PruneLayers: []int{0, 2}, KeepSplit: true, OverrideKV: []string{"general.name=str:test"}}
	got := quantizeArgs(req, "/models/in.gguf", "/models/out.gguf", "", "/models/types.txt")
	want := []string{"--include-weights", "blk.*", "--output-tensor-type", "f32", "--token-embedding-type", "f16", "--tensor-type", "blk.0=q8_0", "--tensor-type-file", "/models/types.txt", "--prune-layers", "0,2", "--keep-split", "--override-kv", "general.name=str:test", "/models/in.gguf", "/models/out.gguf", "Q4_K_M"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quantizeArgs() = %q, want %q", got, want)
	}
}

func TestValidateQuantizeOverrides_RejectsIncludeAndExclude(t *testing.T) {
	err := validateQuantizeOverrides(QuantizeRequest{IncludeWeights: []string{"blk.*"}, ExcludeWeights: []string{"output.*"}})
	if err == nil {
		t.Fatal("expected mutually exclusive tensor patterns to fail")
	}
}

func TestEvaluationArgs_BenchmarkExpertOptions(t *testing.T) {
	req := EvaluateRequest{StudioRuntimeOptions: StudioRuntimeOptions{BatchSize: 512, UBatchSize: 128, Threads: 8, GPULayers: -1}, Mode: "benchmark", PromptTokens: 256, GenTokens: 64, Repetitions: 3, NoWarmup: true, CacheTypeK: "q8_0", FlashAttention: "on", Device: "CUDA0", LoadMode: "mmap", SplitMode: "layer", TensorSplit: "3,1", NoKVOffload: true, FitTarget: 1024}
	binary, got, err := evaluationArgs(req, "/models/model.gguf", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if binary != "llama-bench" {
		t.Fatalf("binary = %q", binary)
	}
	want := []string{"--model", "/models/model.gguf", "--n-prompt", "256", "--n-gen", "64", "--repetitions", "3", "--batch-size", "512", "--ubatch-size", "128", "--threads", "8", "--fit-target", "1024", "--n-gpu-layers", "-1", "--no-warmup", "--no-kv-offload", "1", "--cache-type-k", "q8_0", "--flash-attn", "on", "--device", "CUDA0", "--load-mode", "mmap", "--split-mode", "layer", "--tensor-split", "3,1", "--output", "json", "--progress"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluationArgs() = %q, want %q", got, want)
	}
}

func TestEvaluationArgs_PerplexityTask(t *testing.T) {
	req := EvaluateRequest{Mode: "perplexity", PPLTask: "hellaswag", TaskCount: 20, Chunks: 2, PPLStride: 64, PPLOutputType: 1, NoWarmup: true}
	binary, got, err := evaluationArgs(req, "/models/model.gguf", "/models/data.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if binary != "llama-perplexity" {
		t.Fatalf("binary = %q", binary)
	}
	want := []string{"--model", "/models/model.gguf", "--file", "/models/data.txt", "--chunks", "2", "--no-warmup", "--hellaswag", "--hellaswag-tasks", "20", "--ppl-stride", "64", "--ppl-output-type", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluationArgs() = %q, want %q", got, want)
	}
}

func TestStudioUtilityArgs_Imatrix(t *testing.T) {
	req := StudioUtilityRequest{StudioRuntimeOptions: StudioRuntimeOptions{Threads: 8, GPULayers: -1}, Tool: "imatrix", OutputFormat: "dat", Chunks: 3, FromChunk: 1, ProcessOutput: true, NoPPL: true}
	binary, args, publishes, err := studioUtilityArgs(req, "/models/model.gguf", "/models/data.txt", []string{"/models/old.gguf"}, "/models/imatrix.dat", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "/models/model.gguf", "--file", "/models/data.txt", "--output", "/models/imatrix.dat", "--output-format", "dat", "--threads", "8", "--gpu-layers", "-1", "--in-file", "/models/old.gguf", "--chunks", "3", "--chunk", "1", "--process-output", "--no-ppl"}
	if binary != "llama-imatrix" || !publishes || !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected utility command: binary=%q publish=%v args=%v", binary, publishes, args)
	}
}

func TestStudioUtilityArgs_ControlVector(t *testing.T) {
	req := StudioUtilityRequest{Tool: "control-vector", Method: "mean", PCABatch: 64, PCAIterations: 500}
	binary, args, publishes, err := studioUtilityArgs(req, "/models/model.gguf", "", nil, "/models/vector.gguf", "/models/positive.txt", "/models/negative.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "/models/model.gguf", "--positive-file", "/models/positive.txt", "--negative-file", "/models/negative.txt", "--output", "/models/vector.gguf", "--method", "mean", "--pca-batch", "64", "--pca-iter", "500"}
	if binary != "llama-cvector-generator" || !publishes || !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected utility command: binary=%q publish=%v args=%v", binary, publishes, args)
	}
}

func TestStudioUtilityArgs_Finetune(t *testing.T) {
	req := StudioUtilityRequest{StudioRuntimeOptions: StudioRuntimeOptions{ContextSize: 4096, BatchSize: 256}, Tool: "finetune", Epochs: 2, LearningRate: 1e-5, ValidationSplit: 0.1, Optimizer: "adamw_q8_0"}
	binary, args, publishes, err := studioUtilityArgs(req, "/models/model.gguf", "/models/train.txt", nil, "/models/tuned.gguf", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "/models/model.gguf", "--file", "/models/train.txt", "--output", "/models/tuned.gguf", "--ctx-size", "4096", "--batch-size", "256", "--epochs", "2", "--learning-rate", "1e-05", "--val-split", "0.1", "--optimizer", "adamw_q8_0"}
	if binary != "llama-finetune" || !publishes || !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected utility command: binary=%q publish=%v args=%v", binary, publishes, args)
	}
}

func TestStudioUtilityArgs_LookupCreate(t *testing.T) {
	req := StudioUtilityRequest{StudioRuntimeOptions: StudioRuntimeOptions{ContextSize: 2048}, Tool: "lookup-create", Predict: 64}
	binary, args, publishes, err := studioUtilityArgs(req, "/models/model.gguf", "/models/corpus.txt", nil, "/models/cache.bin", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "/models/model.gguf", "--file", "/models/corpus.txt", "--lookup-cache-dynamic", "/models/cache.bin", "--predict", "64", "--ctx-size", "2048"}
	if binary != "llama-lookup-create" || !publishes || !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected utility command: binary=%q publish=%v args=%v", binary, publishes, args)
	}
}

func TestStudioUtilityArgs_ResultsRequiresOutput(t *testing.T) {
	_, _, _, err := studioUtilityArgs(StudioUtilityRequest{Tool: "results"}, "/models/model.gguf", "/models/input.json", nil, "", "", "", "")
	if err == nil {
		t.Fatal("expected output validation error")
	}
}

func TestStudioUtilityArgs_RejectsUnsafeTool(t *testing.T) {
	_, _, _, err := studioUtilityArgs(StudioUtilityRequest{Tool: "llama-cli"}, "", "", nil, "", "", "", "")
	if err == nil {
		t.Fatal("expected unsupported utility error")
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
		StudioRuntimeOptions: StudioRuntimeOptions{ContextSize: 2048, Threads: 8, GPULayers: -1},
		PPLMask:              "assistant", MaxLayerRatio: 0.4, Evaluate: &evaluate,
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
