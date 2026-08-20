import type { StudioPipelineStep, StudioResource } from "./types";

export type Operation = StudioPipelineStep["operation"];
export type DraftStep = { operation: Operation; usePrevious: boolean; requestText: string; variantsText: string; continueOnFailure: boolean; gateMetric: string; gateMin: string; gateMax: string };
export type FieldSpec = { key: string; label: string; type?: "text" | "number" | "boolean" | "list" | "numberList" | "resource" | "resourceList" | "scaledResourceList"; options?: string[]; resourceTypes?: StudioResource["type"][]; placeholder?: string };

export const operations: Operation[] = ["quantize", "hash", "split", "merge", "prune", "train-qlora", "export-lora", "evaluate", "utility", "register", "distill"];
export const variantsPlaceholder = '[{"output":"q4.gguf","type":"Q4_K_M"},{"output":"q6.gguf","type":"Q6_K"}]';

export const defaults: Record<Operation, Record<string, unknown>> = {
	quantize: { output: "output-Q4_K_M.gguf", type: "Q4_K_M", importanceMatrix: "", allowRequantize: false, leaveOutputTensor: false, pure: false, dryRun: false, threads: 0, includeWeights: [], excludeWeights: [], outputTensorType: "", tokenEmbeddingType: "", tensorTypes: [], tensorTypeFile: "", pruneLayers: [], keepSplit: false, overrideKV: [] },
	hash: { algorithm: "sha256", noLayer: true, uuid: false, manifest: "" },
	split: { mode: "split", output: "output-split.gguf", maxTensors: 128, maxSize: "", noTensorFirstSplit: false, dryRun: false },
	merge: { models: [], output: "merged.gguf", method: "ties", density: 0.5, threads: 0, memoryBudget: "2G", calibration: "", targetType: "q4_k", population: 0, generations: 0, eliteCount: 0, sigma0: 0, seed: 0, contextSize: 0, gpuLayers: -1, device: "", mergeGpu: false },
	prune: { phase: "hard", dataset: "", ratios: [], outputDir: "pruning", importanceCache: "", profile: "pruning/profile.json", output: "pruned.gguf", validate: true, maxPplDeltaPercent: 5, metric: "ppl", pplMask: "", maxLayerRatio: 0, evaluate: true, seed: 0, contextSize: 0, batchSize: 0, ubatchSize: 0, threads: 0, datasetThreads: 0, gpuLayers: -1 },
	"train-qlora": { dataset: "datasets/train.jsonl", output: "adapter.gguf", resume: "", epochs: 2, learningRate: 0.0002, learningRateMin: 0, decayEpochs: 0, weightDecay: 0, validationSplit: 0.05, rank: 16, alpha: 32, targets: "", optimizer: "adamw", optimizerRestartEvery: 0, saveEvery: 100, freezeLayers: 0, gradCheckpoint: 1, loraQat: "none", scheduler: "cosine", warmupSteps: 0, warmupInitRatio: 0.1, verboseLoss: false, trainOnPrompt: false, shuffleDataset: true, criticalTokenMode: "none", criticalTokenWeight: 3, criticalConfidenceThreshold: 0.25, criticalWeightShape: "constant", criticalWarmupSteps: 0, criticalMaxFraction: 1, criticalStatsEvery: 10, grpoMode: false, nGen: 8, nSteps: 500, grpoTemperature: 0.8, grpoMaxTokens: 512, grpoPromptField: "prompt", grpoReferenceField: "response", grpoRewardProvider: "builtin", grpoBuiltinReward: "exact", grpoRewardScript: "", grpoRewardUrl: "", grpoRewardTimeout: 60, grpoCaseSensitive: false, grpoNumericTolerance: 0.000001, contextSize: 0, batchSize: 256, ubatchSize: 256, threads: 0, datasetThreads: 0, gpuLayers: -1 },
	"export-lora": { adapters: [], scaledAdapters: [], output: "lora-merged.gguf", tensorType: "F16" },
	evaluate: { mode: "benchmark", dataset: "", promptTokens: 512, genTokens: 128, repetitions: 5, chunks: 0, contextSize: 0, batchSize: 0, ubatchSize: 0, threads: 0, gpuLayers: -1, noWarmup: false, priority: 0, delay: 0, depth: 0, embeddings: false, cacheTypeK: "f16", cacheTypeV: "f16", flashAttention: "auto", device: "", loadMode: "auto", splitMode: "layer", tensorSplit: "", mainGpu: 0, noKvOffload: false, noOpOffload: false, noHost: false, fitTarget: 0, fitContext: 4096, numa: "", pplTask: "perplexity", taskCount: 0, pplStride: 0, pplOutputType: 0, klBase: "", saveAllLogits: false, baselineJobID: "", maxRegressionPercent: 0 },
	utility: { tool: "imatrix", model: "", input: "", inputs: [], output: "utility-output.gguf", positive: "", negative: "", prompt: "", predict: 128, template: "", templateFile: "", method: "pca", outputFormat: "gguf", chunks: 0, fromChunk: 0, outputFrequency: 10, saveFrequency: 0, pcaBatch: 100, pcaIterations: 1000, contextSize: 0, batchSize: 0, ubatchSize: 0, threads: 0, gpuLayers: -1, epochs: 2, learningRate: 0.00001, learningRateMin: 0, decayEpochs: 0, weightDecay: 0, validationSplit: 0.05, optimizer: "adamw", ids: false, noBos: false, noParseSpecial: false, showCount: true, processOutput: false, noPpl: false, parseSpecial: false, showStatistics: false, check: false },
	register: { modelID: "studio-model", name: "", description: "", contextSize: 4096, gpuLayers: -1, ttl: 0, overwrite: false },
	distill: { sourceDataset: "", promptField: "prompt", output: "datasets/distilled.jsonl", shuffle: true, seed: 0, maxSamples: 0, serverUrl: "http://127.0.0.1:8080/v1/chat/completions", apiKey: "", model: "", systemPrompt: "", temperature: 0.7, topP: 0.9, topK: 0, maxTokens: 512, reasoningEffort: "", stop: [], concurrency: 4, timeoutSeconds: 120, retries: 2, lastTurnOnly: false },
};

export const fields: Record<Operation, FieldSpec[]> = {
	quantize: [{ key: "output", label: "Output" }, { key: "type", label: "Tensor type", options: ["Q4_K_M", "Q5_K_M", "Q6_K", "Q8_0", "Q4_0", "Q5_0", "IQ4_XS", "IQ4_NL", "IQ3_M", "IQ2_M", "TQ1_0", "TQ2_0", "F16", "BF16"] }, { key: "importanceMatrix", label: "Importance matrix", type: "resource", resourceTypes: ["artifact"] }, { key: "includeWeights", label: "Include tensor patterns", type: "list" }, { key: "excludeWeights", label: "Exclude tensor patterns", type: "list" }, { key: "outputTensorType", label: "Output tensor type" }, { key: "tokenEmbeddingType", label: "Token embedding type" }, { key: "tensorTypes", label: "Per-tensor type rules", type: "list", placeholder: "pattern=q4_k, pattern=q6_k" }, { key: "tensorTypeFile", label: "Tensor type rules file", type: "resource", resourceTypes: ["artifact", "dataset"] }, { key: "pruneLayers", label: "Layers to prune", type: "numberList" }, { key: "overrideKV", label: "Metadata overrides", type: "list" }, { key: "threads", label: "Threads (0 = automatic)", type: "number" }, { key: "allowRequantize", label: "Allow requantization", type: "boolean" }, { key: "leaveOutputTensor", label: "Leave output tensor unquantized", type: "boolean" }, { key: "keepSplit", label: "Preserve input split", type: "boolean" }, { key: "pure", label: "Pure quantization", type: "boolean" }, { key: "dryRun", label: "Dry run", type: "boolean" }],
	hash: [{ key: "algorithm", label: "Algorithm", options: ["sha256", "sha1", "xxh64", "all"] }, { key: "manifest", label: "Verification manifest", type: "resource", resourceTypes: ["artifact", "dataset"] }, { key: "noLayer", label: "Skip layer hashes", type: "boolean" }, { key: "uuid", label: "Include UUID", type: "boolean" }],
	split: [{ key: "mode", label: "Mode", options: ["split", "merge"] }, { key: "output", label: "Output prefix / merged model" }, { key: "maxTensors", label: "Maximum tensors", type: "number" }, { key: "maxSize", label: "Maximum shard size", placeholder: "4G" }, { key: "noTensorFirstSplit", label: "No tensors in first split", type: "boolean" }, { key: "dryRun", label: "Dry run", type: "boolean" }],
	merge: [{ key: "models", label: "Models", type: "resourceList", resourceTypes: ["model", "artifact"] }, { key: "output", label: "Output" }, { key: "method", label: "Method", options: ["ties", "evo"] }, { key: "density", label: "Density", type: "number" }, { key: "threads", label: "Threads", type: "number" }, { key: "memoryBudget", label: "Memory budget", placeholder: "2G" }, { key: "calibration", label: "Calibration dataset", type: "resource", resourceTypes: ["dataset"] }, { key: "targetType", label: "Evolution target type", options: ["q4_0", "q3_k", "q4_k", "mxfp4"] }, { key: "population", label: "Population", type: "number" }, { key: "generations", label: "Generations", type: "number" }, { key: "eliteCount", label: "Elite count", type: "number" }, { key: "sigma0", label: "Initial CMA-ES sigma", type: "number" }, { key: "seed", label: "Random seed", type: "number" }, { key: "contextSize", label: "Fitness context size", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "device", label: "Device" }, { key: "mergeGpu", label: "Merge on GPU", type: "boolean" }],
	prune: [{ key: "phase", label: "Phase", options: ["analyze", "profiles", "inspect", "hard"] }, { key: "dataset", label: "Training / calibration dataset", type: "resource", resourceTypes: ["dataset"] }, { key: "ratios", label: "Pruning ratios", type: "numberList", placeholder: "0.1, 0.2, 0.3" }, { key: "outputDir", label: "Output directory" }, { key: "importanceCache", label: "Importance cache", type: "resource", resourceTypes: ["artifact"] }, { key: "profile", label: "Pruning profile", type: "resource", resourceTypes: ["artifact"] }, { key: "output", label: "Output model" }, { key: "maxPplDeltaPercent", label: "Maximum perplexity delta %", type: "number" }, { key: "metric", label: "Validation metric" }, { key: "pplMask", label: "Perplexity mask" }, { key: "maxLayerRatio", label: "Maximum layer ratio", type: "number" }, { key: "seed", label: "Random seed", type: "number" }, { key: "contextSize", label: "Context size", type: "number" }, { key: "batchSize", label: "Logical batch size", type: "number" }, { key: "ubatchSize", label: "Physical micro-batch", type: "number" }, { key: "threads", label: "Threads", type: "number" }, { key: "datasetThreads", label: "Dataset workers", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "validate", label: "Validate result", type: "boolean" }, { key: "evaluate", label: "Evaluate profiles", type: "boolean" }],
	"train-qlora": [{ key: "dataset", label: "Training dataset", type: "resource", resourceTypes: ["dataset"] }, { key: "output", label: "Adapter output" }, { key: "resume", label: "Resume checkpoint", type: "resource", resourceTypes: ["checkpoint"] }, { key: "epochs", label: "Epochs", type: "number" }, { key: "learningRate", label: "Learning rate", type: "number" }, { key: "learningRateMin", label: "Minimum learning rate", type: "number" }, { key: "decayEpochs", label: "Learning-rate decay epochs", type: "number" }, { key: "weightDecay", label: "Weight decay", type: "number" }, { key: "validationSplit", label: "Validation split", type: "number" }, { key: "rank", label: "LoRA rank", type: "number" }, { key: "alpha", label: "LoRA alpha", type: "number" }, { key: "targets", label: "Target modules" }, { key: "optimizer", label: "Optimizer", options: ["sgd", "adamw", "adamw_f16", "adamw_q8_0", "adamw_q6_k", "adamw_iq4_nl"] }, { key: "optimizerRestartEvery", label: "Optimizer restart every epochs", type: "number" }, { key: "scheduler", label: "Scheduler", options: ["constant", "cosine"] }, { key: "warmupSteps", label: "Warmup steps", type: "number" }, { key: "warmupInitRatio", label: "Warmup initial ratio", type: "number" }, { key: "saveEvery", label: "Checkpoint interval", type: "number" }, { key: "freezeLayers", label: "Freeze layers", type: "number" }, { key: "gradCheckpoint", label: "Gradient checkpointing", type: "number" }, { key: "loraQat", label: "LoRA QAT mode", options: ["none", "q3_k", "q4_k", "q4_0", "mxfp4", "q6_k", "q8_0"] }, { key: "criticalTokenMode", label: "Critical-token mode", options: ["none", "spans", "confidence", "hybrid"] }, { key: "criticalTokenWeight", label: "Critical-token weight", type: "number" }, { key: "criticalConfidenceThreshold", label: "Critical confidence threshold", type: "number" }, { key: "criticalWeightShape", label: "Critical weight shape", options: ["constant", "linear"] }, { key: "criticalWarmupSteps", label: "Critical warmup steps", type: "number" }, { key: "criticalMaxFraction", label: "Critical maximum fraction", type: "number" }, { key: "criticalStatsEvery", label: "Critical stats interval", type: "number" }, { key: "grpoMode", label: "Enable GRPO mode", type: "boolean" }, { key: "nGen", label: "GRPO generations per prompt", type: "number" }, { key: "nSteps", label: "GRPO optimizer steps", type: "number" }, { key: "grpoTemperature", label: "GRPO temperature", type: "number" }, { key: "grpoMaxTokens", label: "GRPO maximum tokens", type: "number" }, { key: "grpoPromptField", label: "Prompt field", placeholder: "prompt or input.prompt" }, { key: "grpoReferenceField", label: "Reference field", placeholder: "response or answer" }, { key: "grpoRewardProvider", label: "Reward provider", options: ["builtin", "script", "http"] }, { key: "grpoBuiltinReward", label: "Built-in verifier", options: ["exact", "numeric", "regex", "json-valid"] }, { key: "grpoRewardScript", label: "Python reward worker", type: "resource", resourceTypes: ["dataset"], placeholder: "Select a .py JSONL reward worker" }, { key: "grpoRewardUrl", label: "Reward service URL", placeholder: "http://host:port/reward" }, { key: "grpoRewardTimeout", label: "Reward timeout (seconds)", type: "number" }, { key: "grpoCaseSensitive", label: "Case-sensitive verification", type: "boolean" }, { key: "grpoNumericTolerance", label: "Numeric tolerance", type: "number" }, { key: "contextSize", label: "Context size (0 = model native)", type: "number" }, { key: "batchSize", label: "Logical batch size", type: "number" }, { key: "ubatchSize", label: "Physical micro-batch", type: "number" }, { key: "threads", label: "Threads", type: "number" }, { key: "datasetThreads", label: "Dataset workers", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "verboseLoss", label: "Verbose loss logging", type: "boolean" }, { key: "trainOnPrompt", label: "Train on prompt tokens", type: "boolean" }, { key: "shuffleDataset", label: "Shuffle dataset", type: "boolean" }],
	"export-lora": [{ key: "adapters", label: "LoRA adapters", type: "resourceList", resourceTypes: ["adapter", "artifact"] }, { key: "scaledAdapters", label: "Scaled adapters", type: "scaledResourceList", resourceTypes: ["adapter", "artifact"], placeholder: "adapter.gguf:0.75" }, { key: "output", label: "Output" }, { key: "tensorType", label: "Tensor type", options: ["F32", "F16", "BF16", "Q8_0", "Q8_1", "Q6_K", "Q5_K", "Q5_1", "Q5_0", "Q4_K", "Q4_1", "Q4_0", "Q3_K", "Q2_K", "IQ4_XS", "IQ4_NL", "IQ3_S", "IQ3_XXS", "IQ2_S", "TQ1_0", "TQ2_0", "MXFP4", "NVFP4", "Q1_0", "Q2_0"] }],
	evaluate: [{ key: "mode", label: "Mode", options: ["benchmark", "perplexity"] }, { key: "dataset", label: "Evaluation dataset", type: "resource", resourceTypes: ["dataset"] }, { key: "pplTask", label: "Perplexity task", options: ["perplexity", "hellaswag", "winogrande", "multiple-choice", "kl-divergence"] }, { key: "taskCount", label: "Evaluation task count", type: "number" }, { key: "klBase", label: "KL base logits", type: "resource", resourceTypes: ["artifact"] }, { key: "saveAllLogits", label: "Save all logits", type: "boolean" }, { key: "pplStride", label: "Perplexity stride", type: "number" }, { key: "pplOutputType", label: "Perplexity output type", options: ["0", "1"] }, { key: "promptTokens", label: "Prompt tokens", type: "number" }, { key: "genTokens", label: "Generated tokens", type: "number" }, { key: "repetitions", label: "Repetitions", type: "number" }, { key: "depth", label: "Benchmark depth", type: "number" }, { key: "delay", label: "Delay between tests", type: "number" }, { key: "priority", label: "Process priority", options: ["-1", "0", "1", "2", "3"] }, { key: "embeddings", label: "Benchmark embeddings", type: "boolean" }, { key: "chunks", label: "Perplexity chunks", type: "number" }, { key: "contextSize", label: "Context size", type: "number" }, { key: "batchSize", label: "Logical batch size", type: "number" }, { key: "ubatchSize", label: "Physical micro-batch", type: "number" }, { key: "threads", label: "Threads", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "cacheTypeK", label: "K cache type", options: ["f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"] }, { key: "cacheTypeV", label: "V cache type", options: ["f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"] }, { key: "flashAttention", label: "Flash attention", options: ["auto", "on", "off"] }, { key: "device", label: "Devices" }, { key: "loadMode", label: "Load mode", options: ["auto", "none", "mmap", "mlock", "mmap+mlock", "dio"] }, { key: "splitMode", label: "GPU split mode", options: ["none", "layer", "row", "tensor"] }, { key: "tensorSplit", label: "Tensor split" }, { key: "mainGpu", label: "Main GPU", type: "number" }, { key: "numa", label: "NUMA mode", options: ["", "distribute", "isolate", "numactl"] }, { key: "fitTarget", label: "Fit target margin MiB", type: "number" }, { key: "fitContext", label: "Minimum fit context", type: "number" }, { key: "noWarmup", label: "Skip warmup", type: "boolean" }, { key: "noKvOffload", label: "Disable KV offload", type: "boolean" }, { key: "noOpOffload", label: "Disable operation offload", type: "boolean" }, { key: "noHost", label: "Bypass host buffer", type: "boolean" }, { key: "baselineJobID", label: "Baseline job ID" }, { key: "maxRegressionPercent", label: "Maximum regression %", type: "number" }],
	utility: [{ key: "tool", label: "Tool", options: ["imatrix", "tokenize", "template-analysis", "control-vector", "lookup-create", "lookup-merge", "lookup-stats", "fit-params", "results", "finetune"] }, { key: "model", label: "Model", type: "resource", resourceTypes: ["model", "artifact"] }, { key: "input", label: "Dataset / input", type: "resource", resourceTypes: ["dataset", "artifact"] }, { key: "inputs", label: "Additional / merge inputs", type: "resourceList", resourceTypes: ["artifact"] }, { key: "output", label: "Managed output" }, { key: "positive", label: "Positive prompts", type: "resource", resourceTypes: ["dataset"] }, { key: "negative", label: "Negative prompts", type: "resource", resourceTypes: ["dataset"] }, { key: "prompt", label: "Inline tokenize prompt" }, { key: "predict", label: "Lookup prediction tokens", type: "number" }, { key: "templateFile", label: "Template file", type: "resource", resourceTypes: ["dataset", "artifact"] }, { key: "method", label: "Method", options: ["pca", "mean"] }, { key: "outputFormat", label: "Importance output format", options: ["gguf", "dat"] }, { key: "chunks", label: "Chunks", type: "number" }, { key: "fromChunk", label: "Starting chunk", type: "number" }, { key: "outputFrequency", label: "Output frequency", type: "number" }, { key: "saveFrequency", label: "Save frequency", type: "number" }, { key: "pcaBatch", label: "PCA batch", type: "number" }, { key: "pcaIterations", label: "PCA iterations", type: "number" }, { key: "epochs", label: "Fine-tuning epochs", type: "number" }, { key: "learningRate", label: "Learning rate", type: "number" }, { key: "learningRateMin", label: "Minimum learning rate", type: "number" }, { key: "decayEpochs", label: "Decay epochs", type: "number" }, { key: "weightDecay", label: "Weight decay", type: "number" }, { key: "validationSplit", label: "Validation split", type: "number" }, { key: "optimizer", label: "Optimizer", options: ["sgd", "adamw", "adamw_f16", "adamw_q8_0", "adamw_q6_k", "adamw_iq4_nl"] }, { key: "contextSize", label: "Context size", type: "number" }, { key: "batchSize", label: "Batch size", type: "number" }, { key: "ubatchSize", label: "Micro-batch size", type: "number" }, { key: "threads", label: "Threads", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "ids", label: "Token IDs only", type: "boolean" }, { key: "noBos", label: "Do not add BOS", type: "boolean" }, { key: "noParseSpecial", label: "Do not parse special tokens", type: "boolean" }, { key: "showCount", label: "Show token count", type: "boolean" }, { key: "processOutput", label: "Process output tensor", type: "boolean" }, { key: "noPpl", label: "Skip perplexity", type: "boolean" }, { key: "parseSpecial", label: "Parse special tokens", type: "boolean" }, { key: "showStatistics", label: "Show statistics", type: "boolean" }, { key: "check", label: "Check results", type: "boolean" }],
	register: [{ key: "modelID", label: "Serving model ID" }, { key: "name", label: "Display name" }, { key: "description", label: "Description" }, { key: "contextSize", label: "Context size", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "ttl", label: "TTL seconds", type: "number" }, { key: "overwrite", label: "Replace existing ID", type: "boolean" }],
	distill: [{ key: "sourceDataset", label: "Source dataset", type: "resource", resourceTypes: ["dataset"] }, { key: "promptField", label: "Prompt field (non-chat datasets)", placeholder: "prompt or input.prompt" }, { key: "output", label: "Output dataset" }, { key: "shuffle", label: "Shuffle source records", type: "boolean" }, { key: "seed", label: "Shuffle seed", type: "number" }, { key: "maxSamples", label: "Maximum records", type: "number" }, { key: "serverUrl", label: "Server URL", placeholder: "http://127.0.0.1:8080/v1/chat/completions" }, { key: "apiKey", label: "API key" }, { key: "model", label: "Model" }, { key: "systemPrompt", label: "System prompt (used when a record has none)" }, { key: "temperature", label: "Temperature", type: "number" }, { key: "topP", label: "Top P", type: "number" }, { key: "topK", label: "Top K", type: "number" }, { key: "maxTokens", label: "Maximum tokens", type: "number" }, { key: "reasoningEffort", label: "Reasoning effort", options: ["", "none", "low", "medium", "high"] }, { key: "reasoningBudgetTokens", label: "Reasoning budget (tokens)", type: "number", placeholder: "blank = server default" }, { key: "stop", label: "Stop sequences", type: "list" }, { key: "concurrency", label: "Concurrency", type: "number" }, { key: "timeoutSeconds", label: "Request timeout (seconds)", type: "number" }, { key: "retries", label: "Retries per turn", type: "number" }, { key: "lastTurnOnly", label: "Regenerate only the final turn", type: "boolean" }],
};

export const numericHints: Partial<Record<Operation, Record<string, string>>> = {
	quantize: {
		threads: "0 omits the thread argument and uses llama-quantize's automatic thread count.",
	},
	split: {
		maxTensors: "0 omits this limit; llama-gguf-split uses its own limit or the maximum-size constraint.",
	},
	merge: {
		density: "0 is normalized to Studio's default of 0.5; valid explicit values are greater than 0 and at most 1.",
		threads: "0 omits the argument and uses the executable's thread default.",
		population: "0 omits the evolutionary population argument and uses the executable default.",
		generations: "0 omits the evolutionary generation count and uses the executable default.",
		eliteCount: "0 omits the elite count and uses the executable default.",
		sigma0: "0 omits the initial CMA-ES sigma and uses the executable default.",
		seed: "0 omits the seed and uses the executable default.",
		contextSize: "0 omits the fitness context size and uses the executable default.",
		gpuLayers: "0 omits GPU-layer placement; -1 is forwarded explicitly.",
	},
	prune: {
		maxPplDeltaPercent: "0 disables Studio's maximum-perplexity-delta gate.",
		maxLayerRatio: "0 omits the maximum layer ratio; explicit values must be between 0 and 1.",
		seed: "0 omits the seed and uses llama-prune's default.",
		contextSize: "0 omits the argument and uses the model/tool context default.",
		batchSize: "0 omits the logical batch size and uses the executable default.",
		ubatchSize: "0 omits the physical micro-batch size and uses the executable default.",
		threads: "0 omits the thread argument and uses the executable default.",
		datasetThreads: "0 omits dataset workers and uses the executable default.",
		gpuLayers: "0 omits GPU-layer placement; -1 is forwarded explicitly.",
	},
	"train-qlora": {
		epochs: "0 omits the argument and uses llama-finetune-qlora's default epoch count.",
		learningRate: "0 omits the argument and uses the optimizer's tool default.",
		learningRateMin: "0 omits minimum learning rate; the executable decides whether decay has a floor.",
		decayEpochs: "0 omits learning-rate decay epochs.",
		weightDecay: "0 omits the argument; the executable's default is no weight decay.",
		validationSplit: "0 omits the argument and therefore uses the executable's validation-split default; it does not explicitly disable validation.",
		rank: "0 omits LoRA rank and uses the executable default.",
		alpha: "0 omits LoRA alpha and uses the executable default.",
		optimizerRestartEvery: "0 omits optimizer restarts.",
		warmupSteps: "0 omits warmup steps.",
		warmupInitRatio: "0 omits the warmup initial ratio and uses the executable default.",
		saveEvery: "0 omits the checkpoint interval and uses the executable default.",
		freezeLayers: "0 omits layer freezing.",
		gradCheckpoint: "0 omits the setting and uses the executable default; use a positive value to override it.",
		criticalTokenWeight: "0 omits the critical-token weight and uses the executable default.",
		criticalConfidenceThreshold: "0 omits the confidence threshold and uses the executable default.",
		criticalWarmupSteps: "0 omits critical-token warmup.",
		criticalMaxFraction: "0 omits the maximum fraction and uses the executable default.",
		criticalStatsEvery: "0 omits the statistics interval and uses the executable default.",
		nGen: "GRPO requires at least 2 generations per prompt.",
		nSteps: "GRPO requires a positive optimizer-step count.",
		grpoTemperature: "0 omits GRPO temperature and uses the executable default.",
		grpoMaxTokens: "GRPO requires a positive generation-token limit.",
		grpoRewardTimeout: "0 uses Studio's 60-second reward-provider timeout.",
		grpoNumericTolerance: "0 requires an exact numeric match; positive values allow that absolute difference.",
		contextSize: "0 omits the argument and uses the model's native context.",
		batchSize: "0 omits the logical batch size and uses the executable default.",
		ubatchSize: "0 omits the physical micro-batch size and uses the executable default.",
		threads: "0 omits the thread argument and uses the executable default.",
		datasetThreads: "0 omits dataset workers and uses the executable default.",
		gpuLayers: "0 omits GPU-layer placement; -1 is forwarded explicitly.",
	},
	evaluate: {
		taskCount: "0 omits the task limit and evaluates all available tasks.",
		pplStride: "0 omits stride and uses llama-perplexity's default.",
		pplOutputType: "0 omits the argument and uses llama-perplexity's output mode 0; 1 is forwarded explicitly.",
		promptTokens: "0 omits the benchmark prompt workload and uses llama-bench's default matrix.",
		genTokens: "0 omits the generation workload and uses llama-bench's default matrix.",
		repetitions: "0 omits repetitions and uses llama-bench's default.",
		depth: "0 omits benchmark depth and uses llama-bench's default.",
		delay: "0 omits delay; no extra delay is requested.",
		priority: "0 is normal priority and is omitted because that is the executable default.",
		chunks: "0 omits the chunk limit and processes all chunks.",
		contextSize: "0 omits context size and uses the model/tool default.",
		batchSize: "0 omits the logical batch size and uses the executable default.",
		ubatchSize: "0 omits the physical micro-batch size and uses the executable default.",
		threads: "0 omits the thread argument and uses the executable default.",
		gpuLayers: "0 omits GPU-layer placement; -1 is forwarded explicitly.",
		mainGpu: "0 omits the argument because GPU 0 is the executable default.",
		fitTarget: "0 disables automatic fit-target adjustment.",
		fitContext: "0 omits minimum fit context and uses the executable default.",
		maxRegressionPercent: "0 disables baseline regression gating.",
	},
	utility: {
		predict: "0 is invalid for lookup creation; choose a positive bounded token count.",
		chunks: "0 omits the chunk limit and processes all chunks.",
		fromChunk: "0 starts from the first chunk and is omitted because that is the tool default.",
		outputFrequency: "0 omits the argument and uses llama-imatrix's default output frequency.",
		saveFrequency: "0 omits periodic intermediate copies.",
		pcaBatch: "0 omits PCA batch size and uses the executable default.",
		pcaIterations: "0 omits PCA iterations and uses the executable default.",
		epochs: "0 omits epochs and uses llama-finetune's default.",
		learningRate: "0 omits learning rate and uses the optimizer's tool default.",
		learningRateMin: "0 omits minimum learning rate.",
		decayEpochs: "0 omits learning-rate decay epochs.",
		weightDecay: "0 omits the argument; llama-finetune defaults to no weight decay.",
		validationSplit: "0 omits the argument and uses llama-finetune's validation-split default; it does not explicitly disable validation.",
		contextSize: "0 omits context size and uses the model/tool default.",
		batchSize: "0 omits the logical batch size and uses the executable default.",
		ubatchSize: "0 omits the physical micro-batch size and uses the executable default.",
		threads: "0 omits the thread argument and uses the executable default.",
		gpuLayers: "0 omits GPU-layer placement; -1 is forwarded explicitly.",
	},
	register: {
		contextSize: "0 omits the serving context override; the generated model entry uses its configured/default context behavior.",
		gpuLayers: "0 omits GPU-layer placement; -1 is forwarded explicitly to the serving command.",
		ttl: "0 omits TTL, so no automatic model expiry is configured.",
	},
	distill: {
		seed: "0 uses a time-based shuffle seed instead of a fixed one.",
		maxSamples: "0 processes every record in the source dataset.",
		temperature: "0 omits the argument and uses the server's default temperature.",
		topP: "0 omits the argument and uses the server's default top-p.",
		topK: "0 omits the argument and uses the server's default top-k.",
		maxTokens: "0 omits the argument and uses the server's default generation length.",
		concurrency: "0 uses Studio's default of 4 concurrent conversations.",
		timeoutSeconds: "0 uses Studio's 120-second per-request timeout.",
		retries: "0 disables retries; a failed turn is skipped and counted in the job summary.",
		reasoningBudgetTokens: "Leave blank to use the server's own reasoning budget. Unlike the other 0-default fields here, 0 is a real value: it forces an immediate end to thinking (no reasoning), separate from omitting the field. -1 is explicitly unrestricted; a positive value caps thinking tokens.",
	},
};

const grpoFieldKeys = new Set(["nGen", "nSteps", "grpoTemperature", "grpoMaxTokens", "grpoPromptField", "grpoReferenceField", "grpoRewardProvider", "grpoBuiltinReward", "grpoRewardScript", "grpoRewardUrl", "grpoRewardTimeout", "grpoCaseSensitive", "grpoNumericTolerance"]);
const sftOnlyFieldKeys = new Set(["epochs", "validationSplit", "trainOnPrompt", "shuffleDataset", "criticalTokenMode", "criticalTokenWeight", "criticalConfidenceThreshold", "criticalWeightShape", "criticalWarmupSteps", "criticalMaxFraction", "criticalStatsEvery"]);

export function fieldHint(operation: Operation, field: FieldSpec): string {
	return numericHints[operation]?.[field.key] ?? "";
}

export function draft(operation: Operation, usePrevious: boolean): DraftStep {
	return { operation, usePrevious, requestText: JSON.stringify(defaults[operation], null, 2), variantsText: "", continueOnFailure: false, gateMetric: "", gateMin: "", gateMax: "" };
}

export function requestObject(step: DraftStep): Record<string, unknown> {
	try { const value = JSON.parse(step.requestText); return value && !Array.isArray(value) && typeof value === "object" ? value : {}; }
	catch { return {}; }
}

export function visibleFields(step: DraftStep): FieldSpec[] {
	const configured = fields[step.operation];
	if (step.operation !== "train-qlora") return configured;
	const request = requestObject(step);
	if (!request.grpoMode) return configured.filter((field) => !grpoFieldKeys.has(field.key));
	const provider = String(request.grpoRewardProvider ?? "builtin");
	const builtin = String(request.grpoBuiltinReward ?? "exact");
	return configured.filter((field) => {
		if (sftOnlyFieldKeys.has(field.key)) return false;
		if (field.key === "grpoRewardScript") return provider === "script";
		if (field.key === "grpoRewardUrl") return provider === "http";
		if (["grpoBuiltinReward", "grpoCaseSensitive", "grpoNumericTolerance"].includes(field.key) && provider !== "builtin") return false;
		if (field.key === "grpoReferenceField" && provider === "builtin" && builtin === "json-valid") return false;
		if (field.key === "grpoNumericTolerance") return provider === "builtin" && builtin === "numeric";
		if (field.key === "grpoCaseSensitive") return provider === "builtin" && ["exact", "regex"].includes(builtin);
		return true;
	});
}

export function fieldLabel(step: DraftStep, field: FieldSpec): string {
	if (step.operation === "train-qlora" && field.key === "dataset" && requestObject(step).grpoMode) return "GRPO prompt dataset";
	return field.label;
}

export function fieldValue(step: DraftStep, field: FieldSpec): string | number | boolean {
	const value = requestObject(step)[field.key];
	if (field.type === "scaledResourceList") return Array.isArray(value) ? value.map((item) => typeof item === "object" && item ? `${String((item as {path?: unknown}).path ?? "")}:${Number((item as {scale?: unknown}).scale ?? 1)}` : "").filter(Boolean).join(", ") : "";
	if (["list", "numberList", "resourceList"].includes(field.type ?? "")) return Array.isArray(value) ? value.join(", ") : "";
	if (field.type === "boolean") return Boolean(value);
	return typeof value === "string" || typeof value === "number" ? value : "";
}

export function setField(step: DraftStep, field: FieldSpec, value: string | boolean) {
	const request = requestObject(step);
	if (field.type === "number" || field.key === "priority" || field.key === "pplOutputType") request[field.key] = value === "" ? undefined : Number(value);
	else if (field.type === "numberList") request[field.key] = String(value).split(",").map((item) => Number(item.trim())).filter((item) => Number.isFinite(item));
	else if (field.type === "scaledResourceList") request[field.key] = String(value).split(",").map((item) => item.trim()).filter(Boolean).map((item) => { const split = item.lastIndexOf(":"); return { path: split > 0 ? item.slice(0, split) : item, scale: split > 0 ? Number(item.slice(split + 1)) : 1 }; });
	else if (field.type === "list" || field.type === "resourceList") request[field.key] = String(value).split(",").map((item) => item.trim()).filter(Boolean);
	else request[field.key] = value;
	if (request[field.key] === "" || request[field.key] === undefined) delete request[field.key];
	step.requestText = JSON.stringify(request, null, 2);
}

export function addListResource(step: DraftStep, field: FieldSpec, path: string) {
	if (!path) return;
	const current = String(fieldValue(step, field)).split(",").map((item) => item.trim()).filter(Boolean);
	const value = field.type === "scaledResourceList" ? `${path}:1` : path;
	if (!current.includes(value)) current.push(value);
	setField(step, field, current.join(", "));
}

export function stepsFromTemplate(steps: StudioPipelineStep[]): DraftStep[] {
	return steps.map((step) => ({
		operation: step.operation,
		usePrevious: step.usePrevious ?? false,
		requestText: JSON.stringify(step.request, null, 2),
		variantsText: step.variants?.length ? JSON.stringify(step.variants, null, 2) : "",
		continueOnFailure: step.continueOnFailure ?? false,
		gateMetric: step.gate?.metric ?? "",
		gateMin: step.gate?.min?.toString() ?? "",
		gateMax: step.gate?.max?.toString() ?? "",
	}));
}

export function buildPipelineSteps(steps: DraftStep[]): StudioPipelineStep[] {
	return steps.map((step, index) => {
		let request: Record<string, unknown>;
		try { request = JSON.parse(step.requestText); }
		catch { throw new Error(`Step ${index + 1} contains invalid JSON`); }
		if (!request || Array.isArray(request) || typeof request !== "object") throw new Error(`Step ${index + 1} request must be a JSON object`);
		let variants: Record<string, unknown>[] | undefined;
		if (step.variantsText.trim()) { const parsed = JSON.parse(step.variantsText); if (!Array.isArray(parsed) || parsed.some((item) => !item || Array.isArray(item) || typeof item !== "object")) throw new Error(`Step ${index + 1} variants must be a JSON array of request objects`); variants = parsed; }
		const gate = step.gateMetric.trim() ? { metric: step.gateMetric.trim(), min: step.gateMin === "" ? undefined : Number(step.gateMin), max: step.gateMax === "" ? undefined : Number(step.gateMax) } : undefined;
		return { operation: step.operation, usePrevious: step.usePrevious, request, variants, continueOnFailure: step.continueOnFailure || undefined, gate };
	});
}
