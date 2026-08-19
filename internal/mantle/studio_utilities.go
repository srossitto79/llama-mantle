package mantle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// StudioUtilityRequest is the typed contract for small artifact and inspection
// tools that do not warrant separate top-level pages. Tool selects one of the
// explicitly supported contracts; arbitrary command-line arguments are never
// accepted.
type StudioUtilityRequest struct {
	StudioRuntimeOptions
	Tool            string   `json:"tool"`
	Model           string   `json:"model,omitempty"`
	Input           string   `json:"input,omitempty"`
	Inputs          []string `json:"inputs,omitempty"`
	Output          string   `json:"output,omitempty"`
	Positive        string   `json:"positive,omitempty"`
	Negative        string   `json:"negative,omitempty"`
	Prompt          string   `json:"prompt,omitempty"`
	Predict         int      `json:"predict,omitempty"`
	Template        string   `json:"template,omitempty"`
	TemplateFile    string   `json:"templateFile,omitempty"`
	Method          string   `json:"method,omitempty"`
	OutputFormat    string   `json:"outputFormat,omitempty"`
	Chunks          int      `json:"chunks,omitempty"`
	FromChunk       int      `json:"fromChunk,omitempty"`
	OutputFrequency int      `json:"outputFrequency,omitempty"`
	SaveFrequency   int      `json:"saveFrequency,omitempty"`
	PCABatch        int      `json:"pcaBatch,omitempty"`
	PCAIterations   int      `json:"pcaIterations,omitempty"`
	Epochs          int      `json:"epochs,omitempty"`
	LearningRate    float64  `json:"learningRate,omitempty"`
	LearningRateMin float64  `json:"learningRateMin,omitempty"`
	DecayEpochs     float64  `json:"decayEpochs,omitempty"`
	WeightDecay     float64  `json:"weightDecay,omitempty"`
	ValidationSplit float64  `json:"validationSplit,omitempty"`
	Optimizer       string   `json:"optimizer,omitempty"`
	IDs             bool     `json:"ids,omitempty"`
	NoBOS           bool     `json:"noBos,omitempty"`
	NoParseSpecial  bool     `json:"noParseSpecial,omitempty"`
	ShowCount       bool     `json:"showCount,omitempty"`
	ProcessOutput   bool     `json:"processOutput,omitempty"`
	NoPPL           bool     `json:"noPpl,omitempty"`
	ParseSpecial    bool     `json:"parseSpecial,omitempty"`
	ShowStatistics  bool     `json:"showStatistics,omitempty"`
	Check           bool     `json:"check,omitempty"`
}

func studioUtilityArgs(req StudioUtilityRequest, modelPath, inputPath string, inputPaths []string, outputPath, positivePath, negativePath, templatePath string) (string, []string, bool, error) {
	if req.ContextSize < 0 || req.BatchSize < 0 || req.UBatchSize < 0 || req.Threads < 0 || req.GPULayers < -1 || req.Predict < 0 || req.Chunks < 0 || req.FromChunk < 0 || req.OutputFrequency < 0 || req.SaveFrequency < 0 || req.PCABatch < 0 || req.PCAIterations < 0 || req.Epochs < 0 || req.LearningRate < 0 || req.WeightDecay < 0 || req.ValidationSplit < 0 || req.ValidationSplit >= 1 {
		return "", nil, false, fmt.Errorf("numeric utility options are outside their supported range")
	}
	common := func(args []string) []string {
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
			args = append(args, "--gpu-layers", strconv.Itoa(req.GPULayers))
		}
		return args
	}
	switch req.Tool {
	case "imatrix":
		if modelPath == "" || inputPath == "" || outputPath == "" {
			return "", nil, false, fmt.Errorf("imatrix requires model, input, and output")
		}
		format := strings.ToLower(strings.TrimSpace(req.OutputFormat))
		if format == "" {
			format = "gguf"
		}
		if format != "gguf" && format != "dat" {
			return "", nil, false, fmt.Errorf("imatrix output format must be gguf or dat")
		}
		args := common([]string{"--model", modelPath, "--file", inputPath, "--output", outputPath, "--output-format", format})
		for _, path := range inputPaths {
			args = append(args, "--in-file", path)
		}
		if req.Chunks > 0 {
			args = append(args, "--chunks", strconv.Itoa(req.Chunks))
		}
		if req.FromChunk > 0 {
			args = append(args, "--chunk", strconv.Itoa(req.FromChunk))
		}
		if req.OutputFrequency > 0 {
			args = append(args, "--output-frequency", strconv.Itoa(req.OutputFrequency))
		}
		if req.SaveFrequency > 0 {
			args = append(args, "--save-frequency", strconv.Itoa(req.SaveFrequency))
		}
		if req.ProcessOutput {
			args = append(args, "--process-output")
		}
		if req.NoPPL {
			args = append(args, "--no-ppl")
		}
		if req.ParseSpecial {
			args = append(args, "--parse-special")
		}
		if req.ShowStatistics {
			args = append(args, "--show-statistics")
		}
		return "llama-imatrix", args, true, nil
	case "tokenize":
		if modelPath == "" || (inputPath == "" && req.Prompt == "") {
			return "", nil, false, fmt.Errorf("tokenize requires a model and input file or prompt")
		}
		args := []string{"--model", modelPath}
		if inputPath != "" {
			args = append(args, "--file", inputPath)
		} else {
			args = append(args, "--prompt", req.Prompt)
		}
		if req.IDs {
			args = append(args, "--ids")
		}
		if req.NoBOS {
			args = append(args, "--no-bos")
		}
		if req.NoParseSpecial {
			args = append(args, "--no-parse-special")
		}
		if req.ShowCount {
			args = append(args, "--show-count")
		}
		return "llama-tokenize", args, false, nil
	case "template-analysis":
		args := []string{}
		if templatePath != "" {
			args = append(args, "--template-file", templatePath)
		} else {
			return "", nil, false, fmt.Errorf("template analysis requires a template file; built-in suites are not shipped in the runtime image")
		}
		return "llama-template-analysis", args, false, nil
	case "control-vector":
		if modelPath == "" || positivePath == "" || negativePath == "" || outputPath == "" {
			return "", nil, false, fmt.Errorf("control vector requires model, positive/negative datasets, and output")
		}
		method := strings.ToLower(strings.TrimSpace(req.Method))
		if method == "" {
			method = "pca"
		}
		if method != "pca" && method != "mean" {
			return "", nil, false, fmt.Errorf("control-vector method must be pca or mean")
		}
		args := common([]string{"--model", modelPath, "--positive-file", positivePath, "--negative-file", negativePath, "--output", outputPath, "--method", method})
		if req.PCABatch > 0 {
			args = append(args, "--pca-batch", strconv.Itoa(req.PCABatch))
		}
		if req.PCAIterations > 0 {
			args = append(args, "--pca-iter", strconv.Itoa(req.PCAIterations))
		}
		return "llama-cvector-generator", args, true, nil
	case "lookup-merge":
		if len(inputPaths) < 2 || outputPath == "" {
			return "", nil, false, fmt.Errorf("lookup merge requires at least two inputs and an output")
		}
		return "llama-lookup-merge", append(append([]string{}, inputPaths...), outputPath), true, nil
	case "lookup-create":
		if modelPath == "" || inputPath == "" || outputPath == "" || req.Predict == 0 {
			return "", nil, false, fmt.Errorf("lookup create requires model, input, output, and a bounded prediction count")
		}
		return "llama-lookup-create", common([]string{"--model", modelPath, "--file", inputPath, "--lookup-cache-dynamic", outputPath, "--predict", strconv.Itoa(req.Predict)}), true, nil
	case "lookup-stats":
		if modelPath == "" || inputPath == "" {
			return "", nil, false, fmt.Errorf("lookup stats requires a model and input cache")
		}
		return "llama-lookup-stats", common([]string{"--model", modelPath, "--lookup-cache-static", inputPath}), false, nil
	case "fit-params":
		if modelPath == "" {
			return "", nil, false, fmt.Errorf("fit parameters requires a model")
		}
		return "llama-fit-params", common([]string{"--model", modelPath, "--fit-print", "on"}), false, nil
	case "results":
		if modelPath == "" || inputPath == "" || outputPath == "" {
			return "", nil, false, fmt.Errorf("results requires a model, input file, and output")
		}
		args := common([]string{"--model", modelPath, "--file", inputPath})
		args = append(args, "--output", outputPath)
		if req.Check {
			args = append(args, "--check")
		}
		return "llama-results", args, true, nil
	case "finetune":
		if modelPath == "" || inputPath == "" || outputPath == "" {
			return "", nil, false, fmt.Errorf("fine-tuning requires model, dataset, and output")
		}
		optimizer := strings.ToLower(strings.TrimSpace(req.Optimizer))
		if optimizer != "" && !stringAllowed(optimizer, "sgd", "adamw", "adamw_f16", "adamw_q8_0", "adamw_q6_k", "adamw_iq4_nl") {
			return "", nil, false, fmt.Errorf("unsupported optimizer %q", optimizer)
		}
		args := common([]string{"--model", modelPath, "--file", inputPath, "--output", outputPath})
		if req.Epochs > 0 {
			args = append(args, "--epochs", strconv.Itoa(req.Epochs))
		}
		if req.LearningRate > 0 {
			args = append(args, "--learning-rate", strconv.FormatFloat(req.LearningRate, 'g', -1, 64))
		}
		if req.LearningRateMin > 0 {
			args = append(args, "--learning-rate-min", strconv.FormatFloat(req.LearningRateMin, 'g', -1, 64))
		}
		if req.DecayEpochs > 0 {
			args = append(args, "--learning-rate-decay-epochs", strconv.FormatFloat(req.DecayEpochs, 'g', -1, 64))
		}
		if req.WeightDecay > 0 {
			args = append(args, "--weight-decay", strconv.FormatFloat(req.WeightDecay, 'g', -1, 64))
		}
		if req.ValidationSplit > 0 {
			args = append(args, "--val-split", strconv.FormatFloat(req.ValidationSplit, 'g', -1, 64))
		}
		if optimizer != "" {
			args = append(args, "--optimizer", optimizer)
		}
		return "llama-finetune", args, true, nil
	default:
		return "", nil, false, fmt.Errorf("unsupported Studio utility %q", req.Tool)
	}
}

func (tm *TaskManager) StartStudioUtility(req StudioUtilityRequest, modelsDir string) (*Task, error) {
	req.Tool = strings.ToLower(strings.TrimSpace(req.Tool))
	resolve := func(value, label string) (string, string, error) {
		if value == "" {
			return "", "", nil
		}
		path, name, err := resolveStudioInput(modelsDir, value, "")
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", label, err)
		}
		return path, name, nil
	}
	modelPath, modelName := "", ""
	var err error
	if req.Model != "" {
		modelPath, modelName, err = resolveStudioInput(modelsDir, req.Model, ".gguf")
		if err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
	}
	inputPath, inputName, err := resolve(req.Input, "input")
	if err != nil {
		return nil, err
	}
	positivePath, _, err := resolve(req.Positive, "positive dataset")
	if err != nil {
		return nil, err
	}
	negativePath, _, err := resolve(req.Negative, "negative dataset")
	if err != nil {
		return nil, err
	}
	templatePath, _, err := resolve(req.TemplateFile, "template file")
	if err != nil {
		return nil, err
	}
	inputPaths := make([]string, 0, len(req.Inputs))
	for _, value := range req.Inputs {
		path, _, inputErr := resolve(value, "utility input")
		if inputErr != nil {
			return nil, inputErr
		}
		inputPaths = append(inputPaths, path)
	}
	outputPath, outputName := "", ""
	if req.Output != "" {
		outputPath, outputName, err = resolveStudioOutput(modelsDir, req.Output, "")
		if err != nil {
			return nil, err
		}
		if err = ensureStudioDiskSpace(outputPath, 64*1024*1024); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return nil, fmt.Errorf("create output directory: %w", err)
		}
	}
	binaryName, args, publishes, err := studioUtilityArgs(req, modelPath, inputPath, inputPaths, outputPath, positivePath, negativePath, templatePath)
	if err != nil {
		return nil, err
	}
	binary, err := exec.LookPath(binaryName)
	if err != nil {
		return nil, fmt.Errorf("%s is not installed", binaryName)
	}
	primary := modelName
	if primary == "" {
		primary = inputName
	}
	task := tm.newStudioTask("utility-"+req.Tool, primary, outputName, map[string]any{"tool": req.Tool})
	run := func() {
		tm.runStudioCommand(task, binary, args, "Running "+req.Tool+"...", req.Tool+" complete", nil, nil)
	}
	if !publishes {
		tm.enqueueStudioTask(task, StudioJobLight, run)
		return task, nil
	}
	if err := tm.enqueueStudioTaskWithOutputs(task, StudioJobHeavy, []string{outputPath}, func() {
		staged := studioStagingPath(outputPath, task.ID)
		defer os.Remove(staged)
		stagedArgs := replaceStudioArg(args, outputPath, staged)
		collect := func() []Artifact {
			info, statErr := os.Stat(outputPath)
			if statErr != nil {
				return nil
			}
			rel, _ := filepath.Rel(modelsDir, outputPath)
			return []Artifact{{Name: filepath.ToSlash(rel), Path: filepath.ToSlash(rel), Size: info.Size(), Kind: req.Tool}}
		}
		tm.runStudioCommand(task, binary, stagedArgs, "Running "+req.Tool+"...", req.Tool+" complete", collect, func() error { return publishStudioFile(staged, outputPath) })
	}); err != nil {
		task.UpdateProgress(TaskFailed, err.Error(), 0)
		return nil, err
	}
	return task, nil
}
