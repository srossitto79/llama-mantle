package mantle

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type HashRequest struct {
	Input     string `json:"input"`
	Algorithm string `json:"algorithm"`
	NoLayer   bool   `json:"noLayer,omitempty"`
	UUID      bool   `json:"uuid,omitempty"`
}

type SplitRequest struct {
	Input              string `json:"input"`
	Output             string `json:"output"`
	MaxTensors         int    `json:"maxTensors,omitempty"`
	MaxSize            string `json:"maxSize,omitempty"`
	NoTensorFirstSplit bool   `json:"noTensorFirstSplit,omitempty"`
	DryRun             bool   `json:"dryRun,omitempty"`
}

type MergeRequest struct {
	Base         string   `json:"base"`
	Models       []string `json:"models"`
	Output       string   `json:"output"`
	Method       string   `json:"method"`
	Density      float64  `json:"density,omitempty"`
	Threads      int      `json:"threads,omitempty"`
	MemoryBudget string   `json:"memoryBudget,omitempty"`
	Calibration  string   `json:"calibration,omitempty"`
	TargetType   string   `json:"targetType,omitempty"`
	Population   int      `json:"population,omitempty"`
	Generations  int      `json:"generations,omitempty"`
	GPULayers    int      `json:"gpuLayers,omitempty"`
	Device       string   `json:"device,omitempty"`
	MergeGPU     bool     `json:"mergeGpu,omitempty"`
}

type PruneRequest struct {
	Phase              string    `json:"phase"`
	Model              string    `json:"model,omitempty"`
	Dataset            string    `json:"dataset,omitempty"`
	Ratios             []float64 `json:"ratios,omitempty"`
	OutputDir          string    `json:"outputDir,omitempty"`
	ImportanceCache    string    `json:"importanceCache,omitempty"`
	Profile            string    `json:"profile,omitempty"`
	Output             string    `json:"output,omitempty"`
	Validate           bool      `json:"validate,omitempty"`
	MaxPPLDeltaPercent float64   `json:"maxPplDeltaPercent,omitempty"`
	Metric             string    `json:"metric,omitempty"`
	PPLMask            string    `json:"pplMask,omitempty"`
	MaxLayerRatio      float64   `json:"maxLayerRatio,omitempty"`
	Evaluate           *bool     `json:"evaluate,omitempty"`
	Seed               int       `json:"seed,omitempty"`
	ContextSize        int       `json:"contextSize,omitempty"`
	BatchSize          int       `json:"batchSize,omitempty"`
	UBatchSize         int       `json:"ubatchSize,omitempty"`
	Threads            int       `json:"threads,omitempty"`
	DatasetThreads     int       `json:"datasetThreads,omitempty"`
	GPULayers          int       `json:"gpuLayers,omitempty"`
}

type TrainQLoRARequest struct {
	Model             string  `json:"model"`
	Dataset           string  `json:"dataset"`
	Output            string  `json:"output"`
	Resume            string  `json:"resume,omitempty"`
	Epochs            int     `json:"epochs,omitempty"`
	LearningRate      float64 `json:"learningRate,omitempty"`
	ValidationSplit   float64 `json:"validationSplit,omitempty"`
	Rank              int     `json:"rank,omitempty"`
	Alpha             float64 `json:"alpha,omitempty"`
	Targets           string  `json:"targets,omitempty"`
	Optimizer         string  `json:"optimizer,omitempty"`
	SaveEvery         int     `json:"saveEvery,omitempty"`
	FreezeLayers      int     `json:"freezeLayers,omitempty"`
	GradCheckpoint    int     `json:"gradCheckpoint,omitempty"`
	LoRAQAT           string  `json:"loraQat,omitempty"`
	Scheduler         string  `json:"scheduler,omitempty"`
	WarmupSteps       int     `json:"warmupSteps,omitempty"`
	VerboseLoss       bool    `json:"verboseLoss,omitempty"`
	TrainOnPrompt     bool    `json:"trainOnPrompt,omitempty"`
	ShuffleDataset    bool    `json:"shuffleDataset,omitempty"`
	CriticalTokenMode string  `json:"criticalTokenMode,omitempty"`
	ContextSize       int     `json:"contextSize,omitempty"`
	BatchSize         int     `json:"batchSize,omitempty"`
	UBatchSize        int     `json:"ubatchSize,omitempty"`
	Threads           int     `json:"threads,omitempty"`
	DatasetThreads    int     `json:"datasetThreads,omitempty"`
	GPULayers         int     `json:"gpuLayers,omitempty"`
}

type ExportLoRARequest struct {
	Base       string   `json:"base"`
	Adapters   []string `json:"adapters"`
	Output     string   `json:"output"`
	TensorType string   `json:"tensorType,omitempty"`
}

type EvaluateRequest struct {
	Mode                 string  `json:"mode"`
	Model                string  `json:"model"`
	Dataset              string  `json:"dataset,omitempty"`
	PromptTokens         int     `json:"promptTokens,omitempty"`
	GenTokens            int     `json:"genTokens,omitempty"`
	Repetitions          int     `json:"repetitions,omitempty"`
	Chunks               int     `json:"chunks,omitempty"`
	ContextSize          int     `json:"contextSize,omitempty"`
	BatchSize            int     `json:"batchSize,omitempty"`
	UBatchSize           int     `json:"ubatchSize,omitempty"`
	Threads              int     `json:"threads,omitempty"`
	GPULayers            int     `json:"gpuLayers,omitempty"`
	BaselineJobID        string  `json:"baselineJobID,omitempty"`
	MaxRegressionPercent float64 `json:"maxRegressionPercent,omitempty"`
}

var splitSizePattern = regexp.MustCompile(`^[1-9][0-9]*(M|G)$`)
var studioDevicePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
var loraTargetsPattern = regexp.MustCompile(`^[A-Za-z0-9_.,-]+$`)
var exportLoRATypes = map[string]struct{}{
	"F32": {}, "F16": {}, "BF16": {}, "Q8_0": {}, "Q8_1": {}, "Q6_K": {}, "Q5_K": {},
	"Q5_1": {}, "Q5_0": {}, "Q4_K": {}, "Q4_1": {}, "Q4_0": {}, "Q3_K": {}, "Q2_K": {},
	"IQ4_XS": {}, "IQ4_NL": {}, "IQ3_S": {}, "IQ3_XXS": {}, "IQ2_S": {}, "TQ1_0": {},
	"TQ2_0": {}, "MXFP4": {}, "NVFP4": {}, "Q1_0": {}, "Q2_0": {},
}

func (tm *TaskManager) StartHash(req HashRequest, modelsDir string) (*Task, error) {
	inputPath, inputName, err := resolveStudioInput(modelsDir, req.Input, ".gguf")
	if err != nil {
		return nil, err
	}
	algorithm := strings.ToLower(strings.TrimSpace(req.Algorithm))
	if algorithm == "" {
		algorithm = "sha256"
	}
	if algorithm != "xxh64" && algorithm != "sha1" && algorithm != "sha256" && algorithm != "all" {
		return nil, fmt.Errorf("unsupported hash algorithm %q", algorithm)
	}
	binary, err := exec.LookPath("llama-gguf-hash")
	if err != nil {
		return nil, fmt.Errorf("llama-gguf-hash is not installed")
	}
	args := []string{"--" + algorithm}
	if req.NoLayer {
		args = append(args, "--no-layer")
	}
	if req.UUID {
		args = append(args, "--uuid")
	}
	args = append(args, inputPath)
	task := tm.newStudioTask("hash", inputName, "", map[string]any{
		"algorithm": algorithm, "noLayer": req.NoLayer, "uuid": req.UUID,
	})
	tm.enqueueStudioTask(task, StudioJobLight, func() {
		tm.runStudioCommand(task, binary, args, "Hashing model...", "Hash complete", nil, nil)
	})
	return task, nil
}

func (tm *TaskManager) StartSplit(req SplitRequest, modelsDir string) (*Task, error) {
	inputPath, inputName, err := resolveStudioInput(modelsDir, req.Input, ".gguf")
	if err != nil {
		return nil, err
	}
	outputPath, outputName, err := resolveStudioOutput(modelsDir, req.Output, ".gguf")
	if err != nil {
		return nil, err
	}
	if samePath(inputPath, outputPath) {
		return nil, fmt.Errorf("output must differ from input")
	}
	if req.MaxTensors < 0 {
		return nil, fmt.Errorf("max tensors must be positive")
	}
	req.MaxSize = strings.ToUpper(strings.TrimSpace(req.MaxSize))
	if req.MaxSize != "" && !splitSizePattern.MatchString(req.MaxSize) {
		return nil, fmt.Errorf("max size must use a whole number followed by M or G")
	}
	if matches, _ := filepath.Glob(splitOutputGlob(outputPath)); len(matches) > 0 {
		return nil, fmt.Errorf("one or more output shards already exist")
	}
	if err := ensureStudioDiskSpace(outputPath, estimateStudioOutput(inputPath, 1.05, 64*1024*1024)); err != nil {
		return nil, err
	}
	binary, err := exec.LookPath("llama-gguf-split")
	if err != nil {
		return nil, fmt.Errorf("llama-gguf-split is not installed")
	}
	args := []string{"--split"}
	if req.MaxTensors > 0 {
		args = append(args, "--split-max-tensors", strconv.Itoa(req.MaxTensors))
	}
	if req.MaxSize != "" {
		args = append(args, "--split-max-size", req.MaxSize)
	}
	if req.NoTensorFirstSplit {
		args = append(args, "--no-tensor-first-split")
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, inputPath, outputPath)
	task := tm.newStudioTask("split", inputName, outputName, map[string]any{
		"maxTensors": req.MaxTensors, "maxSize": req.MaxSize,
		"noTensorFirstSplit": req.NoTensorFirstSplit, "dryRun": req.DryRun,
	})
	collect := func() []Artifact {
		if req.DryRun {
			return nil
		}
		matches, _ := filepath.Glob(splitOutputGlob(outputPath))
		artifacts := make([]Artifact, 0, len(matches))
		for _, path := range matches {
			info, statErr := os.Stat(path)
			if statErr == nil && info.Mode().IsRegular() {
				rel, _ := filepath.Rel(modelsDir, path)
				name := filepath.ToSlash(rel)
				artifacts = append(artifacts, Artifact{Name: name, Path: name, Size: info.Size(), Kind: "gguf-shard"})
			}
		}
		return artifacts
	}
	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobIO, []string{outputPath}, func() {
		if req.DryRun {
			tm.runStudioCommand(task, binary, args, "Planning model split...", "Split plan complete", nil, nil)
			return
		}
		staged := studioStagingPath(outputPath, task.ID)
		defer removeStudioFileSet(staged)
		tm.runStudioCommand(task, binary, replaceStudioArg(args, outputPath, staged), "Splitting model...", "Split complete", collect,
			func() error { return publishStudioFileSet(staged, outputPath) })
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

func (tm *TaskManager) StartMerge(req MergeRequest, modelsDir string) (*Task, error) {
	basePath, baseName, err := resolveStudioInput(modelsDir, req.Base, ".gguf")
	if err != nil {
		return nil, fmt.Errorf("base model: %w", err)
	}
	if len(req.Models) == 0 {
		return nil, fmt.Errorf("at least one model to merge is required")
	}
	modelPaths := make([]string, 0, len(req.Models))
	modelNames := make([]string, 0, len(req.Models))
	for _, name := range req.Models {
		path, clean, inputErr := resolveStudioInput(modelsDir, name, ".gguf")
		if inputErr != nil {
			return nil, fmt.Errorf("merge model: %w", inputErr)
		}
		if samePath(basePath, path) {
			return nil, fmt.Errorf("merge model must differ from base model")
		}
		modelPaths = append(modelPaths, path)
		modelNames = append(modelNames, clean)
	}
	outputPath, outputName, err := resolveStudioOutput(modelsDir, req.Output, ".gguf")
	if err != nil {
		return nil, err
	}
	if err := ensureStudioDiskSpace(outputPath, estimateStudioOutput(basePath, 1.1, 64*1024*1024)); err != nil {
		return nil, err
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	if req.Method == "" {
		req.Method = "ties"
	}
	if req.Method != "ties" && req.Method != "evo" {
		return nil, fmt.Errorf("method must be ties or evo")
	}
	if req.Density == 0 {
		req.Density = 0.5
	}
	if req.Density <= 0 || req.Density > 1 {
		return nil, fmt.Errorf("density must be greater than 0 and at most 1")
	}
	if req.Threads < 0 || req.Population < 0 || req.Generations < 0 || req.GPULayers < -1 {
		return nil, fmt.Errorf("numeric merge options are outside their supported range")
	}
	req.MemoryBudget = strings.ToUpper(strings.TrimSpace(req.MemoryBudget))
	if req.MemoryBudget != "" && !splitSizePattern.MatchString(req.MemoryBudget) {
		return nil, fmt.Errorf("memory budget must use a whole number followed by M or G")
	}
	calibrationPath := ""
	if req.Calibration != "" {
		calibrationPath, _, err = resolveStudioInput(modelsDir, req.Calibration, "")
		if err != nil {
			return nil, fmt.Errorf("calibration: %w", err)
		}
	}
	if req.Method == "evo" && calibrationPath == "" {
		return nil, fmt.Errorf("evo merge requires a calibration dataset")
	}
	if req.Device != "" && !studioDevicePattern.MatchString(req.Device) {
		return nil, fmt.Errorf("invalid device name")
	}
	target := strings.ToLower(strings.TrimSpace(req.TargetType))
	if target != "" && target != "q4_0" && target != "q3_k" && target != "q4_k" && target != "mxfp4" {
		return nil, fmt.Errorf("unsupported evo target type %q", target)
	}
	binary, err := exec.LookPath("llama-model-merge")
	if err != nil {
		return nil, fmt.Errorf("llama-model-merge is not installed")
	}
	args := []string{"--base", basePath, "--method", req.Method, "--density", strconv.FormatFloat(req.Density, 'f', -1, 64)}
	for _, path := range modelPaths {
		args = append(args, "--model", path)
	}
	if req.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(req.Threads))
	}
	if req.MemoryBudget != "" {
		args = append(args, "--memory-budget", req.MemoryBudget)
	}
	if calibrationPath != "" {
		args = append(args, "--calibration", calibrationPath)
	}
	if target != "" {
		args = append(args, "--target-type", target)
	}
	if req.Population > 0 {
		args = append(args, "--population", strconv.Itoa(req.Population))
	}
	if req.Generations > 0 {
		args = append(args, "--generations", strconv.Itoa(req.Generations))
	}
	if req.GPULayers != 0 {
		args = append(args, "--gpu-layers", strconv.Itoa(req.GPULayers))
	}
	if req.Device != "" {
		args = append(args, "--device", req.Device)
	}
	if req.MergeGPU {
		args = append(args, "--merge-gpu")
	}
	args = append(args, "--output", outputPath)
	task := tm.newStudioTask("merge", baseName, outputName, map[string]any{
		"models": modelNames, "method": req.Method, "density": req.Density,
		"threads": req.Threads, "memoryBudget": req.MemoryBudget, "targetType": target,
	})
	collect := func() []Artifact {
		info, statErr := os.Stat(outputPath)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		return []Artifact{{Name: outputName, Path: outputName, Size: info.Size(), Kind: "gguf"}}
	}
	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobHeavy, []string{outputPath}, func() {
		staged := studioStagingPath(outputPath, task.ID)
		defer os.Remove(staged)
		tm.runStudioCommand(task, binary, replaceStudioArg(args, outputPath, staged), "Merging models...", "Merge complete", collect,
			func() error { return publishStudioFile(staged, outputPath) })
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

func (tm *TaskManager) StartPrune(req PruneRequest, modelsDir string) (*Task, error) {
	req.Phase = strings.ToLower(strings.TrimSpace(req.Phase))
	if req.Phase != "analyze" && req.Phase != "profiles" && req.Phase != "inspect" && req.Phase != "hard" {
		return nil, fmt.Errorf("prune phase must be analyze, profiles, inspect, or hard")
	}
	if req.MaxPPLDeltaPercent < 0 || req.MaxLayerRatio < 0 || req.MaxLayerRatio > 1 ||
		req.ContextSize < 0 || req.BatchSize < 0 || req.UBatchSize < 0 || req.Threads < 0 ||
		req.DatasetThreads < 0 || req.GPULayers < -1 {
		return nil, fmt.Errorf("numeric prune options are outside their supported range")
	}
	if req.Metric != "" && req.Metric != "router-output" {
		return nil, fmt.Errorf("unsupported pruning metric %q", req.Metric)
	}
	if req.PPLMask != "" && req.PPLMask != "all" && req.PPLMask != "assistant" && req.PPLMask != "reasoning" && req.PPLMask != "content" {
		return nil, fmt.Errorf("unsupported perplexity mask %q", req.PPLMask)
	}

	resolveInput := func(label, name, extension string) (string, string, error) {
		path, clean, err := resolveStudioInput(modelsDir, name, extension)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", label, err)
		}
		return path, clean, nil
	}
	var modelPath, modelName, datasetPath, cachePath, profilePath string
	var err error
	if req.Phase == "analyze" || req.Phase == "inspect" || req.Phase == "hard" {
		modelPath, modelName, err = resolveInput("model", req.Model, ".gguf")
		if err != nil {
			return nil, err
		}
	}
	if req.Phase == "analyze" || (req.Phase == "hard" && req.Dataset != "") {
		datasetPath, _, err = resolveInput("dataset", req.Dataset, "")
		if err != nil {
			return nil, err
		}
	}
	if req.Phase == "profiles" {
		var cacheName string
		cachePath, cacheName, err = resolveInput("importance cache", req.ImportanceCache, "")
		if err != nil {
			return nil, err
		}
		req.ImportanceCache = cacheName
	}
	if req.Phase == "inspect" || req.Phase == "hard" {
		profilePath, _, err = resolveInput("profile", req.Profile, "")
		if err != nil {
			return nil, err
		}
	}

	args := []string{req.Phase}
	inputName := modelName
	outputName := ""
	var collect func() []Artifact
	switch req.Phase {
	case "analyze", "profiles":
		if len(req.Ratios) == 0 {
			return nil, fmt.Errorf("at least one pruning ratio is required")
		}
		ratioValues := make([]string, len(req.Ratios))
		for i, ratio := range req.Ratios {
			if ratio <= 0 || ratio >= 1 {
				return nil, fmt.Errorf("pruning ratios must be greater than 0 and less than 1")
			}
			ratioValues[i] = strconv.FormatFloat(ratio, 'f', -1, 64)
		}
		outputPath, clean, outputErr := resolveStudioOutputDirectory(modelsDir, req.OutputDir)
		if outputErr != nil {
			return nil, outputErr
		}
		estimate := int64(64 * 1024 * 1024)
		if req.Phase == "analyze" {
			estimate = estimateStudioOutput(modelPath, 0.1, 256*1024*1024)
		}
		if err := ensureStudioDiskSpace(outputPath, estimate); err != nil {
			return nil, err
		}
		outputName = clean
		if req.Phase == "analyze" {
			args = append(args, "--model", modelPath, "--dataset", datasetPath)
		} else {
			args = append(args, "--importance-cache", cachePath)
			inputName = filepath.ToSlash(req.ImportanceCache)
		}
		args = append(args, "--ratios", strings.Join(ratioValues, ","))
		appendPruneOptions(&args, req)
		args = append(args, "--output-dir", outputPath)
		collect = func() []Artifact { return collectStudioDirectory(modelsDir, outputPath, "prune-output") }
	case "inspect":
		args = append(args, "--model", modelPath, "--profile", profilePath)
	case "hard":
		outputPath, clean, outputErr := resolveStudioOutput(modelsDir, req.Output, ".gguf")
		if outputErr != nil {
			return nil, outputErr
		}
		if err := ensureStudioDiskSpace(outputPath, estimateStudioOutput(modelPath, 1.1, 64*1024*1024)); err != nil {
			return nil, err
		}
		outputName = clean
		args = append(args, "--model", modelPath, "--profile", profilePath)
		if datasetPath != "" {
			args = append(args, "--dataset", datasetPath)
		}
		if req.Validate {
			if datasetPath == "" {
				return nil, fmt.Errorf("hard-prune validation requires a dataset")
			}
			args = append(args, "--validate")
			if req.MaxPPLDeltaPercent > 0 {
				args = append(args, "--max-ppl-delta-percent", strconv.FormatFloat(req.MaxPPLDeltaPercent, 'f', -1, 64))
			}
		}
		appendPruneOptions(&args, req)
		args = append(args, "--output", outputPath)
		collect = func() []Artifact {
			info, statErr := os.Stat(outputPath)
			if statErr != nil || !info.Mode().IsRegular() {
				return nil
			}
			return []Artifact{{Name: clean, Path: clean, Size: info.Size(), Kind: "gguf-pruned"}}
		}
	}
	binary, err := exec.LookPath("llama-prune")
	if err != nil {
		return nil, fmt.Errorf("llama-prune is not installed")
	}
	task := tm.newStudioTask("prune-"+req.Phase, inputName, outputName, map[string]any{
		"phase": req.Phase, "ratios": req.Ratios, "validate": req.Validate,
		"maxPplDeltaPercent": req.MaxPPLDeltaPercent, "profile": req.Profile,
	})
	class := StudioJobHeavy
	if req.Phase == "inspect" {
		class = StudioJobLight
	}
	var outputKeys []string
	if outputName != "" {
		outputKeys = []string{filepath.Join(modelsDir, filepath.FromSlash(outputName))}
	}
	if err := tm.enqueueStudioTaskWithOutputs(task, class, outputKeys, func() {
		if req.Phase == "hard" {
			final := filepath.Join(modelsDir, filepath.FromSlash(outputName))
			staged := studioStagingPath(final, task.ID)
			defer os.Remove(staged)
			tm.runStudioCommand(task, binary, replaceStudioArg(args, final, staged), "Running prune hard...", "Prune hard complete", collect,
				func() error { return publishStudioFile(staged, final) })
			return
		}
		if req.Phase == "analyze" || req.Phase == "profiles" {
			final := filepath.Join(modelsDir, filepath.FromSlash(outputName))
			staged := studioStagingDirectory(final, task.ID)
			defer os.RemoveAll(staged)
			tm.runStudioCommand(task, binary, replaceStudioArg(args, final, staged), "Running prune "+req.Phase+"...", "Prune "+req.Phase+" complete", collect,
				func() error { return publishStudioDirectory(staged, final) })
			return
		}
		tm.runStudioCommand(task, binary, args, "Running prune "+req.Phase+"...", "Prune "+req.Phase+" complete", collect, nil)
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

func (tm *TaskManager) StartTrainQLoRA(req TrainQLoRARequest, modelsDir string) (*Task, error) {
	modelPath, modelName, err := resolveStudioInput(modelsDir, req.Model, ".gguf")
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	datasetPath, datasetName, err := resolveStudioInput(modelsDir, req.Dataset, "")
	if err != nil {
		return nil, fmt.Errorf("dataset: %w", err)
	}
	outputPath, outputName, err := resolveStudioOutput(modelsDir, req.Output, ".gguf")
	if err != nil {
		return nil, err
	}
	if matches, _ := filepath.Glob(splitOutputGlob(outputPath)); len(matches) > 0 {
		return nil, fmt.Errorf("one or more adapter or checkpoint outputs already exist")
	}
	if err := ensureStudioDiskSpace(outputPath, estimateStudioOutput(modelPath, 0.15, gib)); err != nil {
		return nil, err
	}
	resumePath := ""
	if req.Resume != "" {
		resumePath, _, err = resolveStudioInput(modelsDir, req.Resume, ".gguf")
		if err != nil {
			return nil, fmt.Errorf("resume checkpoint: %w", err)
		}
	}
	if req.Epochs < 0 || req.Rank < 0 || req.SaveEvery < 0 || req.FreezeLayers < 0 || req.GradCheckpoint < 0 ||
		req.WarmupSteps < 0 || req.ContextSize < 0 || req.BatchSize < 0 || req.UBatchSize < 0 ||
		req.Threads < 0 || req.DatasetThreads < 0 || req.GPULayers < -1 {
		return nil, fmt.Errorf("numeric training options are outside their supported range")
	}
	if req.LearningRate < 0 || req.ValidationSplit < 0 || req.ValidationSplit >= 1 || req.Alpha < 0 {
		return nil, fmt.Errorf("floating-point training options are outside their supported range")
	}
	if req.Targets != "" && !loraTargetsPattern.MatchString(req.Targets) {
		return nil, fmt.Errorf("LoRA targets may only contain tensor-name characters and commas")
	}
	if req.Optimizer != "" && !stringAllowed(req.Optimizer, "sgd", "adamw", "adamw_f16", "adamw_q8_0", "adamw_q6_k", "adamw_iq4_nl") {
		return nil, fmt.Errorf("unsupported optimizer %q", req.Optimizer)
	}
	if req.LoRAQAT != "" && !stringAllowed(req.LoRAQAT, "none", "q3_k", "q4_k", "q4_0", "mxfp4", "q6_k", "q8_0") {
		return nil, fmt.Errorf("unsupported LoRA QAT type %q", req.LoRAQAT)
	}
	if req.Scheduler != "" && !stringAllowed(req.Scheduler, "constant", "cosine") {
		return nil, fmt.Errorf("unsupported learning-rate scheduler %q", req.Scheduler)
	}
	if req.CriticalTokenMode != "" && !stringAllowed(req.CriticalTokenMode, "none", "spans", "confidence", "hybrid") {
		return nil, fmt.Errorf("unsupported critical-token mode %q", req.CriticalTokenMode)
	}
	binary, err := exec.LookPath("llama-finetune-qlora")
	if err != nil {
		return nil, fmt.Errorf("llama-finetune-qlora is not installed")
	}
	args := []string{"--model", modelPath, "--train-file", datasetPath}
	if resumePath != "" {
		args = append(args, "--resume", resumePath)
	}
	appendIntArg := func(flag string, value int) {
		if value > 0 {
			args = append(args, flag, strconv.Itoa(value))
		}
	}
	appendFloatArg := func(flag string, value float64) {
		if value > 0 {
			args = append(args, flag, strconv.FormatFloat(value, 'g', -1, 64))
		}
	}
	appendIntArg("--epochs", req.Epochs)
	appendFloatArg("--learning-rate", req.LearningRate)
	appendFloatArg("--val-split", req.ValidationSplit)
	appendIntArg("--lora-rank", req.Rank)
	appendFloatArg("--lora-alpha", req.Alpha)
	if req.Targets != "" {
		args = append(args, "--lora-targets", req.Targets)
	}
	if req.Optimizer != "" {
		args = append(args, "--optimizer", req.Optimizer)
	}
	appendIntArg("--save-every", req.SaveEvery)
	appendIntArg("--freeze-layers", req.FreezeLayers)
	appendIntArg("--grad-checkpoint", req.GradCheckpoint)
	if req.LoRAQAT != "" {
		args = append(args, "--lora-qat", req.LoRAQAT)
	}
	if req.Scheduler != "" {
		args = append(args, "--lr-scheduler", req.Scheduler)
	}
	appendIntArg("--warmup-steps", req.WarmupSteps)
	if req.VerboseLoss {
		args = append(args, "--verbose-loss")
	}
	if req.TrainOnPrompt {
		args = append(args, "--train-on-prompt")
	}
	if req.ShuffleDataset {
		args = append(args, "--shuffle-dataset")
	}
	if req.CriticalTokenMode != "" {
		args = append(args, "--critical-token-mode", req.CriticalTokenMode)
	}
	appendIntArg("--ctx-size", req.ContextSize)
	appendIntArg("--batch-size", req.BatchSize)
	appendIntArg("--ubatch-size", req.UBatchSize)
	appendIntArg("--threads", req.Threads)
	appendIntArg("--dataset-threads", req.DatasetThreads)
	if req.GPULayers != 0 {
		args = append(args, "--n-gpu-layers", strconv.Itoa(req.GPULayers))
	}
	args = append(args, "--lora-out", outputPath)
	task := tm.newStudioTask("train-qlora", modelName, outputName, map[string]any{
		"dataset": datasetName, "epochs": req.Epochs, "rank": req.Rank, "optimizer": req.Optimizer,
		"loraQat": req.LoRAQAT, "resume": req.Resume,
	})
	collect := func() []Artifact {
		matches, _ := filepath.Glob(splitOutputGlob(outputPath))
		artifacts := make([]Artifact, 0, len(matches))
		for _, path := range matches {
			info, statErr := os.Stat(path)
			if statErr != nil || !info.Mode().IsRegular() {
				continue
			}
			rel, _ := filepath.Rel(modelsDir, path)
			name := filepath.ToSlash(rel)
			kind := "lora-checkpoint"
			if samePath(path, outputPath) {
				kind = "lora-adapter"
			}
			artifacts = append(artifacts, Artifact{Name: name, Path: name, Size: info.Size(), Kind: kind})
		}
		return artifacts
	}
	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobHeavy, []string{outputPath}, func() {
		staged := studioStagingPath(outputPath, task.ID)
		defer removeStudioFileSet(staged)
		tm.runStudioCommand(task, binary, replaceStudioArg(args, outputPath, staged), "Training QLoRA adapter...", "QLoRA training complete", collect,
			func() error { return publishStudioFileSet(staged, outputPath) })
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

func (tm *TaskManager) StartExportLoRA(req ExportLoRARequest, modelsDir string) (*Task, error) {
	basePath, baseName, err := resolveStudioInput(modelsDir, req.Base, ".gguf")
	if err != nil {
		return nil, fmt.Errorf("base model: %w", err)
	}
	if len(req.Adapters) == 0 {
		return nil, fmt.Errorf("at least one LoRA adapter is required")
	}
	adapterPaths := make([]string, 0, len(req.Adapters))
	adapterNames := make([]string, 0, len(req.Adapters))
	for _, adapter := range req.Adapters {
		path, clean, inputErr := resolveStudioInput(modelsDir, adapter, ".gguf")
		if inputErr != nil {
			return nil, fmt.Errorf("LoRA adapter: %w", inputErr)
		}
		adapterPaths = append(adapterPaths, path)
		adapterNames = append(adapterNames, clean)
	}
	outputPath, outputName, err := resolveStudioOutput(modelsDir, req.Output, ".gguf")
	if err != nil {
		return nil, err
	}
	if err := ensureStudioDiskSpace(outputPath, estimateStudioOutput(basePath, 1.1, 64*1024*1024)); err != nil {
		return nil, err
	}
	req.TensorType = strings.ToUpper(strings.TrimSpace(req.TensorType))
	if req.TensorType != "" {
		if _, ok := exportLoRATypes[req.TensorType]; !ok {
			return nil, fmt.Errorf("unsupported output tensor type %q", req.TensorType)
		}
	}
	binary, err := exec.LookPath("llama-export-lora")
	if err != nil {
		return nil, fmt.Errorf("llama-export-lora is not installed")
	}
	args := []string{"--model", basePath, "--lora", strings.Join(adapterPaths, ",")}
	if req.TensorType != "" {
		args = append(args, "--type", strings.ToLower(req.TensorType))
	}
	args = append(args, "--output", outputPath)
	task := tm.newStudioTask("export-lora", baseName, outputName, map[string]any{
		"adapters": adapterNames, "tensorType": req.TensorType,
	})
	collect := func() []Artifact {
		info, statErr := os.Stat(outputPath)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		return []Artifact{{Name: outputName, Path: outputName, Size: info.Size(), Kind: "gguf"}}
	}
	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobHeavy, []string{outputPath}, func() {
		staged := studioStagingPath(outputPath, task.ID)
		defer os.Remove(staged)
		tm.runStudioCommand(task, binary, replaceStudioArg(args, outputPath, staged), "Exporting LoRA into model...", "LoRA export complete", collect,
			func() error { return publishStudioFile(staged, outputPath) })
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

func (tm *TaskManager) StartEvaluate(req EvaluateRequest, modelsDir string) (*Task, error) {
	modelPath, modelName, err := resolveStudioInput(modelsDir, req.Model, ".gguf")
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode != "benchmark" && req.Mode != "perplexity" {
		return nil, fmt.Errorf("evaluation mode must be benchmark or perplexity")
	}
	if req.PromptTokens < 0 || req.GenTokens < 0 || req.Repetitions < 0 || req.Chunks < 0 || req.MaxRegressionPercent < 0 ||
		req.ContextSize < 0 || req.BatchSize < 0 || req.UBatchSize < 0 || req.Threads < 0 || req.GPULayers < -1 {
		return nil, fmt.Errorf("numeric evaluation options are outside their supported range")
	}
	binaryName := "llama-bench"
	args := []string{"--model", modelPath}
	datasetName := ""
	if req.Mode == "benchmark" {
		if req.PromptTokens > 0 {
			args = append(args, "--n-prompt", strconv.Itoa(req.PromptTokens))
		}
		if req.GenTokens > 0 {
			args = append(args, "--n-gen", strconv.Itoa(req.GenTokens))
		}
		if req.Repetitions > 0 {
			args = append(args, "--repetitions", strconv.Itoa(req.Repetitions))
		}
		if req.BatchSize > 0 {
			args = append(args, "--batch-size", strconv.Itoa(req.BatchSize))
		}
		if req.UBatchSize > 0 {
			args = append(args, "--ubatch-size", strconv.Itoa(req.UBatchSize))
		}
		if req.Threads > 0 {
			args = append(args, "--threads", strconv.Itoa(req.Threads))
		}
		if req.GPULayers != 0 {
			args = append(args, "--n-gpu-layers", strconv.Itoa(req.GPULayers))
		}
		args = append(args, "--output", "json", "--progress")
	} else {
		binaryName = "llama-perplexity"
		datasetPath, clean, inputErr := resolveStudioInput(modelsDir, req.Dataset, "")
		if inputErr != nil {
			return nil, fmt.Errorf("evaluation dataset: %w", inputErr)
		}
		datasetName = clean
		args = append(args, "--file", datasetPath)
		if req.Chunks > 0 {
			args = append(args, "--chunks", strconv.Itoa(req.Chunks))
		}
		if req.ContextSize > 0 {
			args = append(args, "--ctx-size", strconv.Itoa(req.ContextSize))
		}
		if req.BatchSize > 0 {
			args = append(args, "--batch-size", strconv.Itoa(req.BatchSize))
		}
		if req.UBatchSize > 0 {
			args = append(args, "--ubatch-size", strconv.Itoa(req.UBatchSize))
		}
		if req.Threads > 0 {
			args = append(args, "--threads", strconv.Itoa(req.Threads))
		}
		if req.GPULayers != 0 {
			args = append(args, "--n-gpu-layers", strconv.Itoa(req.GPULayers))
		}
	}
	binary, err := exec.LookPath(binaryName)
	if err != nil {
		return nil, fmt.Errorf("%s is not installed", binaryName)
	}
	task := tm.newStudioTask("evaluate-"+req.Mode, modelName, "", map[string]any{
		"mode": req.Mode, "dataset": datasetName, "promptTokens": req.PromptTokens,
		"genTokens": req.GenTokens, "repetitions": req.Repetitions, "chunks": req.Chunks,
		"baselineJobID": req.BaselineJobID, "maxRegressionPercent": req.MaxRegressionPercent,
	})
	tm.enqueueStudioTask(task, StudioJobHeavy, func() {
		tm.runStudioCommand(task, binary, args, "Running "+req.Mode+" evaluation...", "Evaluation complete", nil,
			func() error { return tm.recordAndGateStudioEvaluation(task, req) })
	})
	return task, nil
}

func stringAllowed(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func appendPruneOptions(args *[]string, req PruneRequest) {
	if req.Metric != "" {
		*args = append(*args, "--metric", req.Metric)
	}
	if req.PPLMask != "" {
		*args = append(*args, "--ppl-mask", req.PPLMask)
	}
	if req.MaxLayerRatio > 0 {
		*args = append(*args, "--max-layer-ratio", strconv.FormatFloat(req.MaxLayerRatio, 'f', -1, 64))
	}
	if req.Evaluate != nil {
		if *req.Evaluate {
			*args = append(*args, "--evaluate")
		} else {
			*args = append(*args, "--no-evaluate")
		}
	}
	if req.Seed != 0 {
		*args = append(*args, "--seed", strconv.Itoa(req.Seed))
	}
	if req.ContextSize > 0 {
		*args = append(*args, "--ctx-size", strconv.Itoa(req.ContextSize))
	}
	if req.BatchSize > 0 {
		*args = append(*args, "--batch-size", strconv.Itoa(req.BatchSize))
	}
	if req.UBatchSize > 0 {
		*args = append(*args, "--ubatch-size", strconv.Itoa(req.UBatchSize))
	}
	if req.Threads > 0 {
		*args = append(*args, "--threads", strconv.Itoa(req.Threads))
	}
	if req.DatasetThreads > 0 {
		*args = append(*args, "--dataset-threads", strconv.Itoa(req.DatasetThreads))
	}
	if req.GPULayers != 0 {
		*args = append(*args, "--n-gpu-layers", strconv.Itoa(req.GPULayers))
	}
}

func (tm *TaskManager) newStudioTask(operation, input, output string, parameters map[string]any) *Task {
	task := tm.CreateTask("studio", "", "", "")
	task.mu.Lock()
	task.Operation = operation
	task.Input = input
	task.Output = output
	task.Parameters = parameters
	task.mu.Unlock()
	tm.PersistStudioTask(task)
	return task
}

func (tm *TaskManager) runStudioCommand(task *Task, binary string, args []string, running, completed string, collect func() []Artifact, publish func() error) {
	if task.Output != "" {
		// Output paths are already containment checked; create only their parent.
		if err := os.MkdirAll(filepath.Dir(args[len(args)-1]), 0755); err != nil {
			task.UpdateProgress(TaskFailed, fmt.Sprintf("create output directory: %v", err), 0)
			return
		}
	}
	task.UpdateProgress(TaskRunning, running, 0)
	cmd := exec.CommandContext(task.Context(), binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		task.UpdateProgress(TaskFailed, fmt.Sprintf("capture output: %v", err), 0)
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		task.UpdateProgress(TaskFailed, fmt.Sprintf("start %s: %v", task.Operation, err), 0)
		return
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		task.AppendLog(scanner.Text())
	}
	err = cmd.Wait()
	if task.Context().Err() != nil {
		return
	}
	if err != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		task.SetExitCode(code)
		task.UpdateProgress(TaskFailed, fmt.Sprintf("%s failed (exit %d)", task.Operation, code), 0)
		return
	}
	task.SetExitCode(0)
	if publish != nil {
		if err := publish(); err != nil {
			task.UpdateProgress(TaskFailed, fmt.Sprintf("finalize operation: %v", err), 0)
			return
		}
	}
	if collect != nil {
		for _, artifact := range collect() {
			task.AddArtifact(artifact)
		}
	}
	task.UpdateProgress(TaskCompleted, completed, 100)
}

func studioStagingPath(finalPath, taskID string) string {
	ext := filepath.Ext(finalPath)
	base := strings.TrimSuffix(filepath.Base(finalPath), ext)
	return filepath.Join(filepath.Dir(finalPath), "."+base+"."+taskID+".partial"+ext)
}

func studioStagingDirectory(finalPath, taskID string) string {
	return filepath.Join(filepath.Dir(finalPath), "."+filepath.Base(finalPath)+"."+taskID+".partial")
}

func replaceStudioArg(args []string, oldValue, newValue string) []string {
	result := append([]string(nil), args...)
	for i := range result {
		if samePath(result[i], oldValue) {
			result[i] = newValue
		}
	}
	return result
}

func publishStudioFile(stagedPath, finalPath string) error {
	info, err := os.Lstat(stagedPath)
	if err != nil {
		return fmt.Errorf("output was not created: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("staged output is not a regular file")
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check destination: %w", err)
	}
	if err := os.Rename(stagedPath, finalPath); err != nil {
		return fmt.Errorf("rename staged output: %w", err)
	}
	return nil
}

func studioFileSetTargets(stagedBase, finalBase string) ([]string, []string, error) {
	stagedFiles, err := filepath.Glob(splitOutputGlob(stagedBase))
	if err != nil {
		return nil, nil, fmt.Errorf("find staged outputs: %w", err)
	}
	if len(stagedFiles) == 0 {
		return nil, nil, fmt.Errorf("no output files were created")
	}
	stagedStem := strings.TrimSuffix(stagedBase, filepath.Ext(stagedBase))
	finalStem := strings.TrimSuffix(finalBase, filepath.Ext(finalBase))
	finalFiles := make([]string, len(stagedFiles))
	for i, staged := range stagedFiles {
		info, statErr := os.Lstat(staged)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("staged output %q is not a regular file", filepath.Base(staged))
		}
		suffix := strings.TrimPrefix(strings.TrimSuffix(staged, filepath.Ext(staged)), stagedStem)
		if suffix == strings.TrimSuffix(staged, filepath.Ext(staged)) {
			return nil, nil, fmt.Errorf("unexpected staged output name %q", filepath.Base(staged))
		}
		finalFiles[i] = finalStem + suffix + filepath.Ext(staged)
		if _, statErr = os.Lstat(finalFiles[i]); statErr == nil {
			return nil, nil, fmt.Errorf("destination %q already exists", filepath.Base(finalFiles[i]))
		} else if !os.IsNotExist(statErr) {
			return nil, nil, fmt.Errorf("check destination %q: %w", filepath.Base(finalFiles[i]), statErr)
		}
	}
	return stagedFiles, finalFiles, nil
}

func publishStudioFileSet(stagedBase, finalBase string) error {
	stagedFiles, finalFiles, err := studioFileSetTargets(stagedBase, finalBase)
	if err != nil {
		return err
	}
	published := 0
	for i := range stagedFiles {
		if err := os.Rename(stagedFiles[i], finalFiles[i]); err != nil {
			for rollback := published - 1; rollback >= 0; rollback-- {
				_ = os.Rename(finalFiles[rollback], stagedFiles[rollback])
			}
			return fmt.Errorf("publish %q: %w", filepath.Base(finalFiles[i]), err)
		}
		published++
	}
	return nil
}

func removeStudioFileSet(stagedBase string) {
	files, _ := filepath.Glob(splitOutputGlob(stagedBase))
	for _, path := range files {
		_ = os.Remove(path)
	}
}

func publishStudioDirectory(stagedPath, finalPath string) error {
	info, err := os.Lstat(stagedPath)
	if err != nil {
		return fmt.Errorf("output directory was not created: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("staged output is not a regular directory")
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check destination: %w", err)
	}
	if err := os.Rename(stagedPath, finalPath); err != nil {
		return fmt.Errorf("rename staged directory: %w", err)
	}
	return nil
}

func splitOutputGlob(outputPath string) string {
	ext := filepath.Ext(outputPath)
	return strings.TrimSuffix(outputPath, ext) + "*" + ext
}

func resolveStudioOutputDirectory(modelsDir, name string) (string, string, error) {
	path, cleanName, err := resolveStudioPath(modelsDir, name, "")
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", "", fmt.Errorf("output directory already exists")
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("check output directory: %w", err)
	}
	parent := filepath.Dir(path)
	for {
		resolved, evalErr := filepath.EvalSymlinks(parent)
		if evalErr == nil {
			base, baseErr := filepath.EvalSymlinks(modelsDir)
			if baseErr != nil || !withinDir(base, resolved) {
				return "", "", fmt.Errorf("output path escapes models directory")
			}
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", "", fmt.Errorf("invalid output directory")
		}
		parent = next
	}
	return path, cleanName, nil
}

func collectStudioDirectory(modelsDir, root, kind string) []Artifact {
	var artifacts []Artifact
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(modelsDir, path)
		if relErr != nil || !withinDir(modelsDir, path) {
			return nil
		}
		name := filepath.ToSlash(rel)
		artifacts = append(artifacts, Artifact{Name: name, Path: name, Size: info.Size(), Kind: kind})
		return nil
	})
	return artifacts
}
