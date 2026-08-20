export type ConnectionState = "connected" | "connecting" | "disconnected";

export type ModelStatus = "ready" | "starting" | "stopping" | "stopped" | "shutdown" | "unknown";
export type PlaygroundModelType = "model" | "peer" | "selector" | "profile";

export interface ModelCapabilities {
  vision?: boolean;
  audio_transcriptions?: boolean;
  audio_speech?: boolean;
  image_generation?: boolean;
  image_to_image?: boolean;
  function_calling?: boolean;
  reranker?: boolean;
}

export interface Model {
  id: string;
  state: ModelStatus;
  name: string;
  description: string;
  unlisted: boolean;
  peerID: string;
  playgroundType?: PlaygroundModelType;
  aliases?: string[];
  capabilities?: ModelCapabilities;
}

export interface ModelEstimate {
  weightsBytes: number;
  kvCacheBytes: number;
  totalBytes: number;
  nCtx: number;
  nLayers: number;
  cacheTypeK: string;
  cacheTypeV: string;
  slidingWindow: number;
}

export interface Profile {
  id: string;
  description: string;
  pins: Record<string, string>;
}

export interface ProfileState {
  active: string | null;
  profiles: Profile[];
}

export interface TokenMetrics {
  cache_tokens: number;
  draft_tokens: number;
  draft_acc_tokens: number;
  input_tokens: number;
  output_tokens: number;
  prompt_per_second: number;
  tokens_per_second: number;
}

export interface ActivityLogEntry {
  id: number;
  timestamp: string;
  model: string;
  req_path: string;
  resp_content_type: string;
  resp_status_code: number;
  tokens: TokenMetrics;
  duration_ms: number;
  has_capture: boolean;
  error_msg?: string;
  metadata?: Record<string, string>;
}

export interface ActivityPage {
  data: ActivityLogEntry[];
  page: number;
  limit: number;
  total: number;
  total_pages: number;
}

export interface ReqRespCapture {
  id: number;
  req_path: string;
  req_headers: Record<string, string>;
  req_body: string; // base64 encoded bytes
  resp_headers: Record<string, string>;
  resp_body: string; // base64 encoded bytes
}

export interface LogData {
  source: "upstream" | "proxy";
  data: string;
}

export interface InflightRequestEntry {
  id: string;
  timestamp: string;
  model: string;
  req_path: string;
  method: string;
  req_headers: Record<string, string>;
  remote_ip: string;
  resp_headers: Record<string, string>;
  resp_bytes: number;
  elapsed_ms: number;
  client_received_at_ms?: number;
  metadata?: Record<string, string>;
}

export interface InFlightStats {
  operation: "snapshot" | "upsert" | "remove";
  requests?: InflightRequestEntry[];
  request?: InflightRequestEntry;
  id?: string;
}

export interface UIConfig {
  activity: {
    session_id: string[];
  };
}

export interface NetIOStat {
  name: string;
  bytes_recv: number;
  bytes_sent: number;
}

export interface SysStat {
  timestamp: string;
  cpu_util_per_core: number[];
  mem_total_mb: number;
  mem_used_mb: number;
  mem_free_mb: number;
  swap_total_mb: number;
  swap_used_mb: number;
  load_avg_1: number;
  load_avg_5: number;
  load_avg_15: number;
  net_io: NetIOStat[];
}

export interface GpuStat {
  timestamp: string;
  id: number;
  name: string;
  uuid: string;
  temp_c: number;
  vram_temp_c: number;
  gpu_util_pct: number;
  mem_util_pct: number;
  mem_used_mb: number;
  mem_total_mb: number;
  fan_speed_pct: number;
  power_draw_w: number;
}

export interface PerformanceResponse {
  sys_stats: SysStat[];
  gpu_stats: GpuStat[];
}

export interface APIEventEnvelope {
  type: "modelStatus" | "logData" | "activity" | "inflight" | "uiConfig" | "profileChanged" | "perfsys" | "perfgpu";
  data: string;
}

export interface HistogramData {
  bins: number[];
  min: number;
  max: number;
  binSize: number;
  p99: number;
  p95: number;
  p50: number;
}

export interface ActivityStatsData {
  total_requests: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_tokens: number;
  prompt_histogram: HistogramData | null;
  gen_histogram: HistogramData | null;
}

export interface VersionInfo {
  build_date: string;
  commit: string;
  version: string;
}

export type ScreenWidth = "xs" | "sm" | "md" | "lg" | "xl" | "2xl";

export type TextContentPart = {
  type: "text";
  text: string;
};

export type ImageContentPart = {
  type: "image_url";
  image_url: { url: string };
};

export type ContentPart = TextContentPart | ImageContentPart;

export interface ChatMessage {
  role: "user" | "assistant" | "system";
  content: string | ContentPart[];
  reasoning_content?: string;
  reasoningTimeMs?: number;
  // Generation stats for assistant messages
  genTokens?: number;
  genMs?: number;
}

export function getTextContent(content: string | ContentPart[]): string {
  if (typeof content === "string") {
    return content;
  }
  const textParts = content.filter((part): part is TextContentPart => part.type === "text");
  return textParts.map((part) => part.text).join("\n");
}

export function getImageUrls(content: string | ContentPart[]): string[] {
  if (typeof content === "string") {
    return [];
  }
  return content
    .filter((part): part is ImageContentPart => part.type === "image_url")
    .map((part) => part.image_url.url);
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  stream: boolean;
  temperature?: number;
  max_tokens?: number;
}

export interface ImageGenerationRequest {
  model: string;
  prompt: string;
  n?: number;
  size?: string;
}

export interface ImageGenerationResponse {
  created: number;
  data: Array<{
    url?: string;
    b64_json?: string;
  }>;
}

// SDAPI types (stable-diffusion.cpp)
export type ImageApiMode = "openai" | "sdapi";

export interface SdApiLora {
  name: string;
  path: string;
}

export interface SdApiLoraRef {
  path: string;
  multiplier: number;
}

export interface SdApiTxt2ImgRequest {
  model?: string;
  prompt: string;
  negative_prompt?: string;
  width?: number;
  height?: number;
  steps?: number;
  cfg_scale?: number;
  seed?: number;
  batch_size?: number;
  sampler_name?: string;
  scheduler?: string;
  lora?: SdApiLoraRef[];
}

export interface SdApiResponse {
  images: string[];
  parameters: Record<string, unknown>;
  info: string;
}

export interface AudioTranscriptionRequest {
  file: File;
  model: string;
}

export interface AudioTranscriptionResponse {
  text: string;
}

export interface SpeechGenerationRequest {
	model: string;
	input: string;
	voice: string;
}

// --- Mantle management types ---

export type TaskState = "queued" | "running" | "completed" | "failed" | "cancelled";

export interface MantleTask {
	id: string;
	type: "download" | "build" | "studio";
	state: TaskState;
	message: string;
	pct: number;
	createdAt: string;
	updatedAt: string;
	queuedAt?: string;
	startedAt?: string;
	finishedAt?: string;
	repo?: string;
	branch?: string;
	modelID?: string;
	operation?: string;
	projectID?: string;
	input?: string;
	output?: string;
	parameters?: Record<string, unknown>;
	logs?: string[];
	exitCode?: number;
	artifacts?: StudioArtifact[];
	jobClass?: "light" | "io" | "heavy";
}

export interface StudioArtifact {
	name: string;
	path: string;
	size: number;
	kind: string;
}

export interface StudioSchedulerStatus {
	maxRunning: number;
	maxHeavy: number;
	running: number;
	heavyRunning: number;
	queued: number;
	blocked: number;
	blockedReason?: string;
}

export interface StudioPipelineStep {
	operation: "quantize" | "hash" | "split" | "merge" | "prune" | "train-qlora" | "export-lora" | "evaluate" | "utility" | "register" | "distill";
	usePrevious?: boolean;
	request: Record<string, unknown>;
	variants?: Record<string, unknown>[];
	continueOnFailure?: boolean;
	gate?: { metric: string; min?: number; max?: number };
}

export interface StudioPipelineRequest {
	name?: string;
	input?: string;
	projectID?: string;
	steps: StudioPipelineStep[];
}

export interface StudioPipelineTemplate {
	id: string;
	projectID?: string;
	name: string;
	pipeline: StudioPipelineRequest;
	createdAt?: string;
	updatedAt?: string;
}

export interface RegisterStudioModelRequest {
	model: string;
	modelID: string;
	name?: string;
	description?: string;
	contextSize?: number;
	gpuLayers?: number;
	ttl?: number;
	overwrite?: boolean;
}

export interface StudioCatalogArtifact {
	name: string;
	path: string;
	size: number;
	kind: string;
	metadata?: Record<string, unknown>;
	jobID: string;
	projectID?: string;
	operation: string;
	input?: string;
	createdAt: string;
	exists: boolean;
	sha256?: string;
	ggufValid?: boolean;
	verificationError?: string;
	tags?: string[];
	notes?: string;
	verifiedAt?: string;
	registered?: boolean;
}

export interface StudioLineageEdge {
	jobID: string;
	input: string;
	output: string;
	relation: string;
	createdAt: string;
}

export interface StudioEvaluation {
	jobID: string;
	projectID?: string;
	model: string;
	mode: "benchmark" | "perplexity";
	metrics: Record<string, unknown>;
	parameters: Record<string, unknown>;
	createdAt: string;
}

export interface StudioRetentionPolicy {
	maxAgeDays: number;
	kinds?: string[];
	includeTagged?: boolean;
}

export interface StudioRetentionPreview {
	token: string;
	candidates: StudioCatalogArtifact[];
	totalBytes: number;
}

export interface StudioModelInspection {
	name: string;
	size: number;
	modifiedAt: string;
	version: number;
	metadata: Record<string, unknown>;
}

export interface StudioPreflightReport {
	operation: string;
	model?: string;
	modelBytes?: number;
	datasetBytes?: number;
	estimatedOutputBytes?: number;
	estimatedRamBytes?: number;
	estimatedVramBytes?: number;
	fits: boolean;
	hardware: { cpuThreads: number; gpuCount: number; totalRamBytes?: number; freeRamBytes?: number; totalVramBytes?: number; freeVramBytes?: number; diskFreeBytes?: number; ramKnown: boolean; vramKnown: boolean };
	recommendations: Record<string, string | number | boolean>;
	warnings?: string[];
}

export interface StudioResource {
	name: string;
	path: string;
	type: "model" | "dataset" | "artifact" | "adapter" | "checkpoint";
	kind: string;
	size: number;
	exists: boolean;
	jobID?: string;
	operation?: string;
	createdAt?: string;
}

export interface StudioProject {
	id: string;
	name: string;
	description?: string;
	resources: string[];
	createdAt?: string;
	updatedAt?: string;
}

export interface QuantizeRequest {
	input: string;
	output: string;
	type: string;
	importanceMatrix?: string;
	allowRequantize?: boolean;
	leaveOutputTensor?: boolean;
	pure?: boolean;
	dryRun?: boolean;
	threads?: number;
	includeWeights?: string[];
	excludeWeights?: string[];
	outputTensorType?: string;
	tokenEmbeddingType?: string;
	tensorTypes?: string[];
	tensorTypeFile?: string;
	pruneLayers?: number[];
	keepSplit?: boolean;
	overrideKV?: string[];
}

export interface HashRequest {
	input: string;
	algorithm: "xxh64" | "sha1" | "sha256" | "all";
	noLayer?: boolean;
	uuid?: boolean;
	manifest?: string;
}

export interface SplitRequest {
	mode?: "split" | "merge";
	input: string;
	output: string;
	maxTensors?: number;
	maxSize?: string;
	noTensorFirstSplit?: boolean;
	dryRun?: boolean;
}

export interface MergeRequest {
	base: string;
	models: string[];
	output: string;
	method: "ties" | "evo";
	density?: number;
	threads?: number;
	memoryBudget?: string;
	calibration?: string;
	targetType?: "q4_0" | "q3_k" | "q4_k" | "mxfp4";
	population?: number;
	generations?: number;
	gpuLayers?: number;
	device?: string;
	mergeGpu?: boolean;
	eliteCount?: number;
	sigma0?: number;
	seed?: number;
	contextSize?: number;
}

export interface PruneRequest {
	phase: "analyze" | "profiles" | "inspect" | "hard";
	model?: string;
	dataset?: string;
	ratios?: number[];
	outputDir?: string;
	importanceCache?: string;
	profile?: string;
	output?: string;
	validate?: boolean;
	maxPplDeltaPercent?: number;
	metric?: string;
	pplMask?: string;
	maxLayerRatio?: number;
	evaluate?: boolean;
	seed?: number;
	contextSize?: number;
	batchSize?: number;
	ubatchSize?: number;
	threads?: number;
	datasetThreads?: number;
	gpuLayers?: number;
}

export interface TrainQLoRARequest {
	model: string;
	dataset: string;
	output: string;
	resume?: string;
	epochs?: number;
	learningRate?: number;
	learningRateMin?: number;
	decayEpochs?: number;
	weightDecay?: number;
	validationSplit?: number;
	rank?: number;
	alpha?: number;
	targets?: string;
	optimizer?: string;
	saveEvery?: number;
	freezeLayers?: number;
	gradCheckpoint?: number;
	loraQat?: string;
	scheduler?: string;
	warmupSteps?: number;
	warmupInitRatio?: number;
	optimizerRestartEvery?: number;
	verboseLoss?: boolean;
	trainOnPrompt?: boolean;
	shuffleDataset?: boolean;
	criticalTokenMode?: string;
	criticalTokenWeight?: number;
	criticalConfidenceThreshold?: number;
	criticalWeightShape?: string;
	criticalWarmupSteps?: number;
	criticalMaxFraction?: number;
	criticalStatsEvery?: number;
	grpoMode?: boolean;
	nGen?: number;
	nSteps?: number;
	grpoTemperature?: number;
	grpoMaxTokens?: number;
	grpoPromptField?: string;
	grpoReferenceField?: string;
	grpoRewardProvider?: "builtin" | "script" | "http";
	grpoBuiltinReward?: "exact" | "numeric" | "regex" | "json-valid";
	grpoRewardScript?: string;
	grpoRewardUrl?: string;
	grpoRewardTimeout?: number;
	grpoCaseSensitive?: boolean;
	grpoNumericTolerance?: number;
	contextSize?: number;
	batchSize?: number;
	ubatchSize?: number;
	threads?: number;
	datasetThreads?: number;
	gpuLayers?: number;
}

export interface DistillRequest {
	sourceDataset: string;
	promptField?: string;
	output: string;
	shuffle?: boolean;
	seed?: number;
	maxSamples?: number;
	serverUrl: string;
	apiKey?: string;
	model: string;
	systemPrompt?: string;
	temperature?: number;
	topP?: number;
	topK?: number;
	maxTokens?: number;
	reasoningEffort?: "" | "low" | "medium" | "high";
	stop?: string[];
	concurrency?: number;
	timeoutSeconds?: number;
	retries?: number;
	lastTurnOnly?: boolean;
}

export interface DatasetInspection {
	name: string;
	size: number;
	recordsScanned: number;
	formats: Record<string, number>;
	truncated: boolean;
}

export interface StudioDataset {
	name: string;
	path: string;
	size: number;
	format: string;
	modifiedAt: string;
}

export interface DatasetPreview extends DatasetInspection {
	records: Record<string, unknown>[];
}

export interface HFDataset {
	id: string;
	downloads: number;
	likes: number;
	updatedAt: string;
	tags: string[];
}

export interface ExportLoRARequest {
	base: string;
	adapters: string[];
	output: string;
	tensorType?: string;
	scaledAdapters?: Array<{ path: string; scale: number }>;
}

export interface EvaluateRequest {
	mode: "benchmark" | "perplexity";
	model: string;
	dataset?: string;
	promptTokens?: number;
	genTokens?: number;
	repetitions?: number;
	chunks?: number;
	contextSize?: number;
	batchSize?: number;
	ubatchSize?: number;
	threads?: number;
	gpuLayers?: number;
	baselineJobID?: string;
	maxRegressionPercent?: number;
	noWarmup?: boolean;
	priority?: number;
	delay?: number;
	depth?: number;
	embeddings?: boolean;
	cacheTypeK?: string;
	cacheTypeV?: string;
	flashAttention?: string;
	device?: string;
	loadMode?: string;
	splitMode?: string;
	tensorSplit?: string;
	mainGpu?: number;
	noKvOffload?: boolean;
	noOpOffload?: boolean;
	noHost?: boolean;
	fitTarget?: number;
	fitContext?: number;
	numa?: string;
	pplTask?: string;
	taskCount?: number;
	pplStride?: number;
	pplOutputType?: number;
	klBase?: string;
	saveAllLogits?: boolean;
}

export interface StudioUtilityRequest {
	tool: "imatrix" | "tokenize" | "template-analysis" | "control-vector" | "lookup-create" | "lookup-merge" | "lookup-stats" | "fit-params" | "results" | "finetune";
	model?: string; input?: string; inputs?: string[]; output?: string;
	positive?: string; negative?: string; prompt?: string; predict?: number; template?: string; templateFile?: string;
	method?: string; outputFormat?: string; chunks?: number; fromChunk?: number;
	outputFrequency?: number; saveFrequency?: number; pcaBatch?: number; pcaIterations?: number;
	contextSize?: number; batchSize?: number; ubatchSize?: number; threads?: number; gpuLayers?: number;
	epochs?: number; learningRate?: number; learningRateMin?: number; decayEpochs?: number;
	weightDecay?: number; validationSplit?: number; optimizer?: string;
	ids?: boolean; noBos?: boolean; noParseSpecial?: boolean; showCount?: boolean;
	processOutput?: boolean; noPpl?: boolean; parseSpecial?: boolean; showStatistics?: boolean; check?: boolean;
}

export interface HFModel {
	id: string;
	name: string;
	description: string;
	downloads: number;
	likes: number;
	updatedAt: string;
	tags: string[];
	gguf: boolean;
}

export interface HFFile {
	path: string;
	size: number;
}

export type LocalModelKind = "gguf" | "safetensors" | "whisper" | "repo";

export interface LocalModel {
	name: string;
	path: string;
	size: number;
	kind: LocalModelKind;
}

export interface BackendEntry {
	name: string;
	path: string;
	size: number;
	taskID?: string;
	repo?: string;
	branch?: string;
}

export interface BuildRequest {
	backendName?: string;
	repo: string;
	branch?: string;
	cmakeFlags?: string;
	cmakeArgs?: string[];
}

export interface DownloadRequest {
	modelID: string;
	filename: string;
}

// --- Backend flag schema (guided config editor) ---

export type FlagValueType = "boolean" | "enum" | "number" | "path" | "string";

export interface FlagSpec {
	names: string[];
	canonicalEnv?: string;
	value?: string;
	type: FlagValueType;
	choices?: string[];
	default?: string;
	help: string;
	section: string;
}

export interface BackendSchema {
	backend: string;
	parsedAt: string;
	flags: FlagSpec[];
}

// --- Structured model/group config editing ---

export interface ModelFilters {
	stripParams?: string;
	setParams?: Record<string, unknown>;
	setParamsByID?: Record<string, Record<string, unknown>>;
}

export interface ModelTimeouts {
	connect?: number;
	keepalive?: number;
	responseHeader?: number;
	tlsHandshake?: number;
	expectContinue?: number;
	idleConn?: number;
}

export interface ModelCapabilitiesConfig {
	in?: string[];
	out?: string[];
	tools?: boolean;
	reranker?: boolean;
	context?: number;
}

export interface ModelMacro {
	name: string;
	value: unknown;
}

// Mirrors internal/config.ModelConfig's JSON shape. `ttl` matches
// llama-swap's YAML field name for UnloadAfter (-1 = use global default).
export interface ConfigModelConfig {
	cmd: string;
	cmdStop?: string;
	proxy?: string;
	aliases?: string[];
	env?: string[];
	checkEndpoint?: string;
	ttl: number;
	unloadTimeout?: number;
	unlisted?: boolean;
	useModelName?: string;
	name?: string;
	description?: string;
	concurrencyLimit?: number;
	filters?: ModelFilters;
	macros?: ModelMacro[];
	metadata?: Record<string, unknown>;
	sendLoadingState?: boolean;
	timeouts?: ModelTimeouts;
	capabilities?: ModelCapabilitiesConfig;
	healthCheckTimeout?: number;
}

export interface NamedModelConfig extends ConfigModelConfig {
	id: string;
}

// Defaults mirroring ModelConfig.UnmarshalYAML in internal/config/model_config.go,
// for pre-populating a brand new model in the guided editor (a plain JSON PUT
// bypasses that YAML-side defaulting logic, so the client fills them in).
export const DEFAULT_MODEL_CONFIG: ConfigModelConfig = {
	cmd: "",
	proxy: "http://localhost:${PORT}",
	checkEndpoint: "/health",
	ttl: -1,
};

export interface ConfigGroupConfig {
	swap: boolean;
	exclusive: boolean;
	persistent: boolean;
	members?: string[];
}

export interface NamedGroupConfig extends ConfigGroupConfig {
	name: string;
}

export const DEFAULT_GROUP_CONFIG: ConfigGroupConfig = {
	swap: true,
	exclusive: true,
	persistent: false,
	members: [],
};
