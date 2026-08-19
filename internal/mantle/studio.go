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

// QuantizeRequest is the typed contract for quantization and requantization.
// Input and Output are model-root-relative paths, never host paths.
type QuantizeRequest struct {
	Input              string   `json:"input"`
	Output             string   `json:"output"`
	Type               string   `json:"type"`
	ImportanceMatrix   string   `json:"importanceMatrix,omitempty"`
	AllowRequantize    bool     `json:"allowRequantize,omitempty"`
	LeaveOutputTensor  bool     `json:"leaveOutputTensor,omitempty"`
	Pure               bool     `json:"pure,omitempty"`
	DryRun             bool     `json:"dryRun,omitempty"`
	Threads            int      `json:"threads,omitempty"`
	IncludeWeights     []string `json:"includeWeights,omitempty"`
	ExcludeWeights     []string `json:"excludeWeights,omitempty"`
	OutputTensorType   string   `json:"outputTensorType,omitempty"`
	TokenEmbeddingType string   `json:"tokenEmbeddingType,omitempty"`
	TensorTypes        []string `json:"tensorTypes,omitempty"`
	TensorTypeFile     string   `json:"tensorTypeFile,omitempty"`
	PruneLayers        []int    `json:"pruneLayers,omitempty"`
	KeepSplit          bool     `json:"keepSplit,omitempty"`
	OverrideKV         []string `json:"overrideKV,omitempty"`
}

var quantizeTypes = map[string]struct{}{
	"Q1_0": {}, "Q2_0": {}, "Q4_0": {}, "Q4_1": {}, "MXFP4_MOE": {},
	"Q5_0": {}, "Q5_1": {}, "IQ2_XXS": {}, "IQ2_XS": {}, "IQ2_S": {},
	"IQ2_M": {}, "IQ1_S": {}, "IQ1_M": {}, "TQ1_0": {}, "TQ2_0": {},
	"Q2_K": {}, "Q2_K_S": {}, "IQ3_XXS": {}, "IQ3_S": {}, "IQ3_M": {},
	"Q3_K": {}, "IQ3_XS": {}, "Q3_K_S": {}, "Q3_K_M": {}, "Q3_K_L": {},
	"IQ4_NL": {}, "IQ4_XS": {}, "Q4_K": {}, "Q4_K_S": {}, "Q4_K_M": {},
	"Q5_K": {}, "Q5_K_S": {}, "Q5_K_M": {}, "Q6_K": {}, "Q8_0": {},
	"F16": {}, "BF16": {}, "F32": {}, "COPY": {},
}

var quantizeOverridePattern = regexp.MustCompile(`^[A-Za-z0-9_.*?+^$()\[\]{}|,:=-]{1,512}$`)

func validateQuantizeOverrides(req QuantizeRequest) error {
	if len(req.IncludeWeights) > 0 && len(req.ExcludeWeights) > 0 {
		return fmt.Errorf("include and exclude tensor patterns cannot be used together")
	}
	if len(req.IncludeWeights) > 128 || len(req.ExcludeWeights) > 128 || len(req.TensorTypes) > 256 || len(req.OverrideKV) > 128 || len(req.PruneLayers) > 4096 {
		return fmt.Errorf("too many quantization overrides")
	}
	values := append(append(append([]string{}, req.IncludeWeights...), req.ExcludeWeights...), req.TensorTypes...)
	values = append(values, req.OverrideKV...)
	for _, value := range values {
		if !quantizeOverridePattern.MatchString(value) {
			return fmt.Errorf("invalid quantization override %q", value)
		}
	}
	for _, layer := range req.PruneLayers {
		if layer < 0 {
			return fmt.Errorf("prune layers must be non-negative")
		}
	}
	return nil
}

// StudioModelInspection is the safe model information returned to the UI.
type StudioModelInspection struct {
	Name       string         `json:"name"`
	Size       int64          `json:"size"`
	ModifiedAt string         `json:"modifiedAt"`
	Version    uint32         `json:"version"`
	Metadata   map[string]any `json:"metadata"`
}

func InspectStudioModel(modelsDir, name string) (*StudioModelInspection, error) {
	path, cleanName, err := resolveStudioInput(modelsDir, name, ".gguf")
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect model: %w", err)
	}
	metadata, err := ReadGGUFMetadata(path)
	if err != nil {
		return nil, fmt.Errorf("inspect model: %w", err)
	}
	return &StudioModelInspection{
		Name:       cleanName,
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		Version:    metadata.Version,
		Metadata:   metadata.KV,
	}, nil
}

// StartQuantize validates and launches llama-quantize without invoking a shell.
func (tm *TaskManager) StartQuantize(req QuantizeRequest, modelsDir string) (*Task, error) {
	inputPath, inputName, err := resolveStudioInput(modelsDir, req.Input, ".gguf")
	if err != nil {
		return nil, err
	}
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	if _, ok := quantizeTypes[req.Type]; !ok {
		return nil, fmt.Errorf("unsupported quantization type %q", req.Type)
	}
	if req.Threads < 0 {
		return nil, fmt.Errorf("threads must be positive")
	}

	outputPath := ""
	outputName := ""
	if !req.DryRun {
		outputPath, outputName, err = resolveStudioOutput(modelsDir, req.Output, ".gguf")
		if err != nil {
			return nil, err
		}
		if samePath(inputPath, outputPath) {
			return nil, fmt.Errorf("output must differ from input")
		}
		if err := ensureStudioDiskSpace(outputPath, estimateStudioOutput(inputPath, 1.1, 64*1024*1024)); err != nil {
			return nil, err
		}
	}

	imatrixPath := ""
	if strings.TrimSpace(req.ImportanceMatrix) != "" {
		imatrixPath, _, err = resolveStudioInput(modelsDir, req.ImportanceMatrix, "")
		if err != nil {
			return nil, fmt.Errorf("importance matrix: %w", err)
		}
	}
	tensorTypeFilePath := ""
	if strings.TrimSpace(req.TensorTypeFile) != "" {
		tensorTypeFilePath, _, err = resolveStudioInput(modelsDir, req.TensorTypeFile, "")
		if err != nil {
			return nil, fmt.Errorf("tensor type file: %w", err)
		}
	}
	if err := validateQuantizeOverrides(req); err != nil {
		return nil, err
	}

	binary, err := exec.LookPath("llama-quantize")
	if err != nil {
		return nil, fmt.Errorf("llama-quantize is not installed")
	}
	args := quantizeArgs(req, inputPath, outputPath, imatrixPath, tensorTypeFilePath)
	task := tm.CreateTask("studio", "", "", "")
	task.mu.Lock()
	task.Operation = "quantize"
	task.Input = inputName
	task.Output = outputName
	task.Parameters = map[string]any{
		"type": req.Type, "allowRequantize": req.AllowRequantize,
		"dryRun": req.DryRun, "threads": req.Threads,
	}
	task.mu.Unlock()
	tm.PersistStudioTask(task)

	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobHeavy, []string{outputPath}, func() {
		if req.DryRun {
			tm.runQuantize(task, binary, args, "", "", outputName, true)
			return
		}
		staged := studioStagingPath(outputPath, task.ID)
		defer os.Remove(staged)
		tm.runQuantize(task, binary, replaceStudioArg(args, outputPath, staged), staged, outputPath, outputName, false)
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}

func quantizeArgs(req QuantizeRequest, inputPath, outputPath, imatrixPath, tensorTypeFilePath string) []string {
	args := make([]string, 0, 12)
	if req.AllowRequantize {
		args = append(args, "--allow-requantize")
	}
	if req.LeaveOutputTensor {
		args = append(args, "--leave-output-tensor")
	}
	if req.Pure {
		args = append(args, "--pure")
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	if imatrixPath != "" {
		args = append(args, "--imatrix", imatrixPath)
	}
	for _, pattern := range req.IncludeWeights {
		args = append(args, "--include-weights", pattern)
	}
	for _, pattern := range req.ExcludeWeights {
		args = append(args, "--exclude-weights", pattern)
	}
	if req.OutputTensorType != "" {
		args = append(args, "--output-tensor-type", strings.ToLower(req.OutputTensorType))
	}
	if req.TokenEmbeddingType != "" {
		args = append(args, "--token-embedding-type", strings.ToLower(req.TokenEmbeddingType))
	}
	for _, value := range req.TensorTypes {
		args = append(args, "--tensor-type", value)
	}
	if tensorTypeFilePath != "" {
		args = append(args, "--tensor-type-file", tensorTypeFilePath)
	}
	if len(req.PruneLayers) > 0 {
		values := make([]string, len(req.PruneLayers))
		for i, layer := range req.PruneLayers {
			values[i] = strconv.Itoa(layer)
		}
		args = append(args, "--prune-layers", strings.Join(values, ","))
	}
	if req.KeepSplit {
		args = append(args, "--keep-split")
	}
	for _, value := range req.OverrideKV {
		args = append(args, "--override-kv", value)
	}
	args = append(args, inputPath)
	if !req.DryRun {
		args = append(args, outputPath)
	}
	args = append(args, req.Type)
	if req.Threads > 0 {
		args = append(args, strconv.Itoa(req.Threads))
	}
	return args
}

func (tm *TaskManager) runQuantize(task *Task, binary string, args []string, stagedPath, outputPath, outputName string, dryRun bool) {
	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			task.UpdateProgress(TaskFailed, fmt.Sprintf("create output directory: %v", err), 0)
			return
		}
	}
	task.UpdateProgress(TaskRunning, "Preparing quantization...", 0)
	cmd := exec.CommandContext(task.Context(), binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		task.UpdateProgress(TaskFailed, fmt.Sprintf("capture output: %v", err), 0)
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		task.UpdateProgress(TaskFailed, fmt.Sprintf("start quantization: %v", err), 0)
		return
	}

	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		task.AppendLog(line)
		pct := quantizeProgress(line)
		if pct >= 0 {
			task.UpdateProgress(TaskRunning, line, pct)
		}
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
		task.UpdateProgress(TaskFailed, fmt.Sprintf("quantization failed (exit %d)", code), 0)
		return
	}
	task.SetExitCode(0)
	if dryRun {
		task.UpdateProgress(TaskCompleted, "Quantization plan complete", 100)
		return
	}
	if err := publishStudioFile(stagedPath, outputPath); err != nil {
		task.UpdateProgress(TaskFailed, fmt.Sprintf("publish output: %v", err), 0)
		return
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		task.UpdateProgress(TaskFailed, "quantization completed without an output file", 0)
		return
	}
	task.AddArtifact(Artifact{Name: outputName, Path: outputName, Size: info.Size(), Kind: "gguf"})
	task.UpdateProgress(TaskCompleted, "Quantization complete", 100)
}

var quantizeProgressPattern = regexp.MustCompile(`^\[\s*(\d+)\s*/\s*(\d+)\]`)

func quantizeProgress(line string) int {
	matches := quantizeProgressPattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return -1
	}
	current, err1 := strconv.Atoi(matches[1])
	total, err2 := strconv.Atoi(matches[2])
	if err1 != nil || err2 != nil || total <= 0 {
		return -1
	}
	return min(99, current*100/total)
}

func resolveStudioInput(modelsDir, name, extension string) (string, string, error) {
	path, cleanName, err := resolveStudioPath(modelsDir, name, extension)
	if err != nil {
		return "", "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("model does not exist")
	}
	base, err := filepath.EvalSymlinks(modelsDir)
	if err != nil {
		return "", "", fmt.Errorf("models directory is unavailable")
	}
	if !withinDir(base, resolved) {
		return "", "", fmt.Errorf("model path escapes models directory")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("model is not a regular file")
	}
	return resolved, cleanName, nil
}

func resolveStudioOutput(modelsDir, name, extension string) (string, string, error) {
	path, cleanName, err := resolveStudioPath(modelsDir, name, extension)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", "", fmt.Errorf("output already exists")
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("check output: %w", err)
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

func resolveStudioPath(modelsDir, name, extension string) (string, string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", "", fmt.Errorf("model name must be relative to the models directory")
	}
	cleanName := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", "", fmt.Errorf("model path escapes models directory")
	}
	if extension != "" && !strings.EqualFold(filepath.Ext(cleanName), extension) {
		return "", "", fmt.Errorf("model must have a %s extension", extension)
	}
	path := filepath.Join(modelsDir, filepath.FromSlash(cleanName))
	if !withinDir(modelsDir, path) {
		return "", "", fmt.Errorf("model path escapes models directory")
	}
	return path, cleanName, nil
}

func samePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
