<script lang="ts">
  import { onMount } from "svelte";
  import { Boxes, FileSearch, Loader2, Play, Square, Workflow } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import LogPanel from "../components/LogPanel.svelte";
  import StudioResourcePicker from "../components/StudioResourcePicker.svelte";
  import PipelineStepEditor from "../components/PipelineStepEditor.svelte";
  import { cancelStudioJob, getStudioPreflight, inspectStudioDataset, inspectStudioModel, listStudioPipelineTemplates, listStudioResources, listTasks, startEvaluate, startExportLoRA, startHash, startMerge, startPrune, startQuantize, startSplit, startStudioPipeline, startTrainQLoRA, streamTaskProgress } from "../lib/mantleApi";
  import { buildPipelineSteps, stepsFromTemplate, type DraftStep } from "../lib/pipelineSteps";
  import type { DatasetInspection, MantleTask, StudioModelInspection, StudioPipelineTemplate, StudioPreflightReport, StudioResource } from "../lib/types";

  const quantTypes = [
    "Q4_K_M", "Q5_K_M", "Q6_K", "Q8_0", "Q4_0", "Q5_0",
    "IQ4_XS", "IQ4_NL", "IQ3_M", "IQ2_M", "TQ1_0", "TQ2_0", "F16", "BF16",
  ];
  const recipes = [
    { id: "quantize", title: "Fit a model to my hardware", description: "Inspect, recommend a quantization, then benchmark the result.", operation: "pipeline" as const },
    { id: "train", title: "Fine-tune with QLoRA", description: "Validate a dataset, train an adapter, and retain checkpoints.", operation: "train" as const },
    { id: "merge", title: "Merge model variants", description: "Combine compatible variants with TIES or evolutionary merging.", operation: "merge" as const },
    { id: "prune", title: "Prune with a quality gate", description: "Analyze importance, create profiles, and bound perplexity loss.", operation: "prune" as const },
    { id: "evaluate", title: "Compare a model", description: "Benchmark throughput or measure dataset perplexity against a baseline.", operation: "evaluate" as const },
  ];

  let resources = $state<StudioResource[]>([]);
  let pipelines = $state<StudioPipelineTemplate[]>([]);
  let operation = $state<"pipeline" | "quantize" | "hash" | "split" | "merge" | "prune" | "train" | "export-lora" | "evaluate" | `template:${string}`>("quantize");
  let templateSteps = $state<DraftStep[]>([]);
  let templateName = $state("");
  let isTemplateOperation = $derived(operation.startsWith("template:"));
  let loadingModels = $state(true);
  let input = $state("");
  let output = $state("");
  let quantType = $state("Q4_K_M");
  let threads = $state(0);
  let allowRequantize = $state(false);
  let leaveOutputTensor = $state(false);
  let pure = $state(false);
  let dryRun = $state(true);
  let importanceMatrix = $state("");
  let hashAlgorithm = $state<"xxh64" | "sha1" | "sha256" | "all">("sha256");
  let hashNoLayer = $state(true);
  let hashUUID = $state(false);
  let splitMaxTensors = $state(128);
  let splitMaxSize = $state("");
  let noTensorFirstSplit = $state(false);
  let mergeModels = $state<string[]>([]);
  let mergeMethod = $state<"ties" | "evo">("ties");
  let mergeDensity = $state(0.5);
  let mergeMemoryBudget = $state("2G");
  let mergeCalibration = $state("");
  let mergeTargetType = $state<"q4_0" | "q3_k" | "q4_k" | "mxfp4">("q4_k");
  let prunePhase = $state<"analyze" | "profiles" | "inspect" | "hard">("analyze");
  let pruneDataset = $state("");
  let pruneRatios = $state("0.1, 0.2, 0.3");
  let pruneOutputDir = $state("pruning-output");
  let pruneCache = $state("");
  let pruneProfile = $state("");
  let pruneValidate = $state(false);
  let pruneMaxPPLDelta = $state(5);
  let pruneMaxLayerRatio = $state(0);
  let pruneEvaluate = $state(true);
  let gpuLayers = $state(0);
  let trainDataset = $state("");
  let trainResume = $state("");
  let trainEpochs = $state(2);
  let trainLearningRate = $state(0.00001);
  let trainValidationSplit = $state(0.05);
  let trainRank = $state(16);
  let trainAlpha = $state(0);
  let trainOptimizer = $state("adamw");
  let trainSaveEvery = $state(0);
  let trainGradCheckpoint = $state(0);
  let trainQAT = $state("none");
  let trainScheduler = $state("constant");
  let trainShuffle = $state(true);
  let trainOnPrompt = $state(false);
  let trainVerboseLoss = $state(true);
  let trainContextSize = $state(0);
  let trainBatchSize = $state(256);
  let trainUBatchSize = $state(256);
  let datasetInspection = $state<DatasetInspection | null>(null);
  let inspectingDataset = $state(false);
  let exportAdapters = $state<string[]>([]);
  let exportType = $state("F16");
  let evaluateMode = $state<"benchmark" | "perplexity">("benchmark");
  let pipelineEvaluate = $state(true);
  let evaluateDataset = $state("");
  let baselineJobID = $state("");
  let maxRegressionPercent = $state(0);
  let promptTokens = $state(512);
  let genTokens = $state(128);
  let repetitions = $state(5);
  let chunks = $state(0);
  let contextSize = $state(512);
  let batchSize = $state(2048);
  let ubatchSize = $state(512);
  let inspection = $state<StudioModelInspection | null>(null);
  let inspecting = $state(false);
  let starting = $state(false);
  let error = $state("");
  let job = $state<MantleTask | null>(null);
  let stopStream: (() => void) | null = null;
  let preflight = $state<StudioPreflightReport | null>(null);
  let preflightBusy = $state(false);
  let showWelcome = $state(false);
  let activeRecipe = $state("");

  let jobLogs = $derived(job?.logs?.join("\n") ?? "");
  let trainingMetrics = $derived.by(() => (job?.logs ?? []).flatMap((line, index) => {
    const loss = line.match(/(?:^|[\s"{])(?:train[_ -]?)?loss"?\s*[:=]\s*([0-9]+(?:\.[0-9]+)?(?:e[-+]?\d+)?)/i);
    const rate = line.match(/(?:^|[\s"{])(?:lr|learning[_ -]?rate)"?\s*[:=]\s*([0-9]+(?:\.[0-9]+)?(?:e[-+]?\d+)?)/i);
    return loss ? [{ step: index + 1, loss: Number(loss[1]), learningRate: rate ? Number(rate[1]) : undefined }] : [];
  }));
  let lossPoints = $derived.by(() => {
    if (!trainingMetrics.length) return "";
    const values = trainingMetrics.map((metric) => metric.loss); const minLoss = Math.min(...values); const maxLoss = Math.max(...values); const range = Math.max(maxLoss - minLoss, 0.000001);
    return trainingMetrics.map((metric, index) => `${trainingMetrics.length === 1 ? 0 : index * 300 / (trainingMetrics.length - 1)},${80 - (metric.loss - minLoss) * 72 / range}`).join(" ");
  });
  let latestCheckpoint = $derived(resources.filter((resource) => resource.type === "checkpoint" && resource.exists).sort((a, b) => Date.parse(b.createdAt ?? "") - Date.parse(a.createdAt ?? ""))[0]);
  let jobActive = $derived(job?.state === "queued" || job?.state === "running");
  let canStart = $derived.by(() => {
    if (jobActive) return false;
    if (isTemplateOperation) return Boolean(input) && templateSteps.length > 0;
    if (operation === "prune") {
      if (prunePhase === "profiles") return Boolean(pruneCache.trim() && pruneRatios.trim() && pruneOutputDir.trim());
      if (prunePhase === "inspect") return Boolean(input && pruneProfile.trim());
      if (prunePhase === "analyze") return Boolean(input && pruneDataset.trim() && pruneRatios.trim() && pruneOutputDir.trim());
      return Boolean(input && pruneProfile.trim() && output.trim() && (!pruneValidate || pruneDataset.trim()));
    }
    if (operation === "train") return Boolean(input && trainDataset.trim() && output.trim());
    if (operation === "export-lora") return Boolean(input && exportAdapters.length && output.trim());
    if (operation === "evaluate") return Boolean(input && (evaluateMode === "benchmark" || evaluateDataset.trim()));
    if (operation === "pipeline") return Boolean(input && output.trim() && (!pipelineEvaluate || evaluateMode === "benchmark" || evaluateDataset.trim()));
    return Boolean(input && (operation === "hash" || output.trim()) && (operation !== "merge" || mergeModels.length));
  });

  function defaultOutput(name: string, type: string): string {
    if (!name) return "";
    const dot = name.toLowerCase().lastIndexOf(".gguf");
    const base = dot >= 0 ? name.slice(0, dot) : name;
    return `${base}-${type}.gguf`;
  }

  function preflightOperation(): string {
    if (operation === "pipeline") return "quantize";
    if (operation === "train") return "train-qlora";
    if (operation === "export-lora") return "merge";
    return operation;
  }

  async function runPreflight() {
    const supported = ["quantize", "train-qlora", "merge", "prune", "evaluate", "serve"];
    const target = preflightOperation();
    if (!input || !supported.includes(target)) return;
    preflightBusy = true;
    try { preflight = await getStudioPreflight(target, input, target === "train-qlora" ? trainDataset : target === "prune" ? pruneDataset : target === "evaluate" ? evaluateDataset : mergeCalibration); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { preflightBusy = false; }
  }

  function applyPreflight() {
    if (!preflight) return;
    const recommendation = preflight.recommendations;
    if (typeof recommendation.quantizationType === "string") selectQuantType(recommendation.quantizationType);
    if (typeof recommendation.threads === "number") threads = recommendation.threads;
    if (typeof recommendation.rank === "number") trainRank = recommendation.rank;
    if (typeof recommendation.batchSize === "number") {
      if (preflight.operation === "train-qlora") {
        trainBatchSize = recommendation.batchSize;
        trainUBatchSize = Math.min(trainUBatchSize, trainBatchSize);
      } else batchSize = recommendation.batchSize;
    }
    if (typeof recommendation.gradientCheckpointing === "number") trainGradCheckpoint = recommendation.gradientCheckpointing;
    if (typeof recommendation.gpuLayers === "number") gpuLayers = recommendation.gpuLayers;
    if (typeof recommendation.contextSize === "number") {
      if (preflight.operation === "train-qlora") trainContextSize = recommendation.contextSize;
      else contextSize = recommendation.contextSize;
    }
  }

  function selectOperation(value: typeof operation) {
    operation = value;
    preflight = null;
    if (value.startsWith("template:")) {
      const id = value.slice("template:".length);
      const template = pipelines.find((candidate) => candidate.id === id);
      templateName = template?.name ?? "";
      templateSteps = template ? stepsFromTemplate(template.pipeline.steps) : [];
      return;
    }
    templateSteps = [];
    if (value === "quantize") output = defaultOutput(input, quantType);
    if (value === "split" && input) output = defaultOutput(input, "split");
    if (value === "merge" && input) output = defaultOutput(input, "merged");
    if (value === "prune" && input) output = defaultOutput(input, "pruned");
    if (value === "train" && input) output = defaultOutput(input, "qlora-adapter");
    if (value === "export-lora" && input) output = defaultOutput(input, "lora-merged");
    if (value === "pipeline") {
      dryRun = false;
      if (input) output = defaultOutput(input, quantType);
    }
  }

  function applyRecipe(recipe: (typeof recipes)[number]) {
    activeRecipe = recipe.id;
    selectOperation(recipe.operation);
    if (recipe.id === "quantize") { dryRun = false; pipelineEvaluate = true; evaluateMode = "benchmark"; quantType = "Q4_K_M"; }
    if (recipe.id === "train") { trainEpochs = 2; trainRank = 16; trainValidationSplit = 0.05; trainSaveEvery = 100; trainGradCheckpoint = 1; }
    if (recipe.id === "merge") { mergeMethod = "ties"; mergeDensity = 0.5; }
    if (recipe.id === "prune") { prunePhase = "analyze"; pruneEvaluate = true; pruneValidate = true; }
    if (recipe.id === "evaluate") { evaluateMode = "benchmark"; repetitions = 5; }
  }

  function dismissWelcome() { showWelcome = false; localStorage.setItem("llama-studio-welcome", "dismissed"); }

  async function selectModel(name: string) {
    preflight = null;
    input = name;
    output = defaultOutput(name, quantType);
    inspection = null;
    error = "";
    if (!name) return;
    inspecting = true;
    try {
      inspection = await inspectStudioModel(name);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      inspecting = false;
    }
  }

  function selectQuantType(type: string) {
    quantType = type;
    if (input) output = defaultOutput(input, type);
  }

  function trackJob(task: MantleTask) {
    stopStream?.();
    job = task;
    stopStream = streamTaskProgress(task.id, (update) => {
      if (!job || update.id !== job.id) return;
      job = { ...job, ...update };
      if (update.state && !["queued", "running"].includes(update.state)) {
        stopStream?.();
        stopStream = null;
      }
    });
  }

  async function runTemplatePipeline() {
    if (!canStart) return;
    starting = true;
    error = "";
    try {
      const task = await startStudioPipeline({ name: templateName || "Pipeline", input, steps: buildPipelineSteps(templateSteps) });
      trackJob(task);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      starting = false;
    }
  }

  async function runQuantize() {
    if (!canStart) return;
    starting = true;
    error = "";
    try {
      const task = await startQuantize({
        input,
        output,
        type: quantType,
        importanceMatrix: importanceMatrix.trim() || undefined,
        allowRequantize,
        leaveOutputTensor,
        pure,
        dryRun,
        threads: threads > 0 ? threads : undefined,
      });
      trackJob(task);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      starting = false;
    }
  }

  async function runOperation() {
    if (!canStart) return;
    if (operation === "quantize") return runQuantize();
    if (isTemplateOperation) return runTemplatePipeline();
    starting = true;
    error = "";
    try {
      let task: MantleTask;
      if (operation === "pipeline") task = await startStudioPipeline({
        name: `Quantize ${quantType}${pipelineEvaluate ? ` + ${evaluateMode}` : ""}`,
        input,
        steps: [
          { operation: "quantize" as const, request: {
            input, output, type: quantType, importanceMatrix: importanceMatrix.trim() || undefined,
            allowRequantize, leaveOutputTensor, pure, threads: threads > 0 ? threads : undefined,
          }},
          ...(pipelineEvaluate ? [{ operation: "evaluate" as const, usePrevious: true, request: {
            mode: evaluateMode, dataset: evaluateMode === "perplexity" ? evaluateDataset.trim() : undefined,
            promptTokens: evaluateMode === "benchmark" ? promptTokens : undefined,
            genTokens: evaluateMode === "benchmark" ? genTokens : undefined,
            repetitions: evaluateMode === "benchmark" ? repetitions : undefined,
            chunks: evaluateMode === "perplexity" && chunks > 0 ? chunks : undefined,
            contextSize: evaluateMode === "perplexity" ? contextSize : undefined,
            batchSize, ubatchSize, threads: threads || undefined, gpuLayers,
			baselineJobID: baselineJobID.trim() || undefined, maxRegressionPercent: baselineJobID.trim() && maxRegressionPercent > 0 ? maxRegressionPercent : undefined,
          }}] : []),
        ],
      });
      else if (operation === "hash") task = await startHash({ input, algorithm: hashAlgorithm, noLayer: hashNoLayer, uuid: hashUUID });
      else if (operation === "split") task = await startSplit({
            input, output, maxTensors: splitMaxTensors > 0 ? splitMaxTensors : undefined,
            maxSize: splitMaxSize.trim() || undefined, noTensorFirstSplit, dryRun,
          });
      else if (operation === "merge") task = await startMerge({
            base: input, models: mergeModels, output, method: mergeMethod, density: mergeDensity,
            threads: threads > 0 ? threads : undefined, memoryBudget: mergeMemoryBudget,
            calibration: mergeCalibration.trim() || undefined,
            targetType: mergeMethod === "evo" ? mergeTargetType : undefined,
          });
      else if (operation === "prune") task = await startPrune({
        phase: prunePhase, model: prunePhase === "profiles" ? undefined : input,
        dataset: pruneDataset.trim() || undefined,
        ratios: ["analyze", "profiles"].includes(prunePhase) ? pruneRatios.split(",").map(Number) : undefined,
        outputDir: ["analyze", "profiles"].includes(prunePhase) ? pruneOutputDir.trim() : undefined,
        importanceCache: pruneCache.trim() || undefined, profile: pruneProfile.trim() || undefined,
        output: prunePhase === "hard" ? output : undefined, validate: pruneValidate,
        maxPplDeltaPercent: pruneValidate ? pruneMaxPPLDelta : undefined,
        maxLayerRatio: pruneMaxLayerRatio > 0 ? pruneMaxLayerRatio : undefined,
        evaluate: ["analyze", "profiles"].includes(prunePhase) ? pruneEvaluate : undefined,
        threads: threads > 0 ? threads : undefined, gpuLayers,
      });
      else if (operation === "train") task = await startTrainQLoRA({
        model: input, dataset: trainDataset.trim(), output, resume: trainResume.trim() || undefined,
        epochs: trainEpochs, learningRate: trainLearningRate, validationSplit: trainValidationSplit,
        rank: trainRank, alpha: trainAlpha || undefined, optimizer: trainOptimizer,
        saveEvery: trainSaveEvery || undefined, gradCheckpoint: trainGradCheckpoint || undefined,
        loraQat: trainQAT, scheduler: trainScheduler, shuffleDataset: trainShuffle,
        trainOnPrompt, verboseLoss: trainVerboseLoss,
        contextSize: trainContextSize || undefined, batchSize: trainBatchSize,
        ubatchSize: trainUBatchSize, threads: threads || undefined, gpuLayers,
      });
      else if (operation === "export-lora") task = await startExportLoRA({
        base: input, adapters: exportAdapters, output, tensorType: exportType,
      });
      else task = await startEvaluate({
        mode: evaluateMode, model: input, dataset: evaluateMode === "perplexity" ? evaluateDataset.trim() : undefined,
        promptTokens: evaluateMode === "benchmark" ? promptTokens : undefined,
        genTokens: evaluateMode === "benchmark" ? genTokens : undefined,
        repetitions: evaluateMode === "benchmark" ? repetitions : undefined,
        chunks: evaluateMode === "perplexity" && chunks > 0 ? chunks : undefined,
        contextSize: evaluateMode === "perplexity" ? contextSize : undefined,
        batchSize, ubatchSize, threads: threads || undefined, gpuLayers,
		baselineJobID: baselineJobID.trim() || undefined, maxRegressionPercent: baselineJobID.trim() && maxRegressionPercent > 0 ? maxRegressionPercent : undefined,
      });
      trackJob(task);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      starting = false;
    }
  }

  async function inspectDataset() {
    if (!trainDataset.trim()) return;
    inspectingDataset = true;
    datasetInspection = null;
    error = "";
    try {
      datasetInspection = await inspectStudioDataset(trainDataset.trim());
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      inspectingDataset = false;
    }
  }

  async function cancelJob() {
    if (!job || !["queued", "running"].includes(job.state)) return;
    try {
      await cancelStudioJob(job.id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  function formatSize(bytes: number): string {
    if (!bytes) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
    return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
  }

  onMount(() => {
	showWelcome = localStorage.getItem("llama-studio-welcome") !== "dismissed";
	const preselectedModel = new URLSearchParams(window.location.search).get("model") ?? "";
    void listStudioResources().then((items) => {
      resources = items;
      loadingModels = false;
	  if (preselectedModel && items.some((item) => item.path === preselectedModel && item.type === "model")) void selectModel(preselectedModel);
    });
    void listStudioPipelineTemplates().then((items) => pipelines = items);
    void listTasks().then((tasks) => {
      const latest = tasks
        .filter((task) => task.type === "studio")
        .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))[0];
      if (latest) trackJob(latest);
    });
    return () => stopStream?.();
  });
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-2">
  {#if showWelcome}
    <Card.Root class="border-primary/40 bg-primary/5 shrink-0"><Card.Header><div class="flex gap-3"><div class="min-w-0 flex-1"><Card.Title>Welcome to Llama Studio</Card.Title><Card.Description>Choose an outcome below. Studio will help select resources, check hardware fit, run the job, compare the result, and prepare it for serving.</Card.Description></div><Button size="sm" variant="ghost" onclick={dismissWelcome}>Dismiss</Button></div></Card.Header><Card.Content><ol class="grid gap-2 text-sm md:grid-cols-4"><li>1. Select or download a model</li><li>2. Choose a guided recipe</li><li>3. Check hardware fit</li><li>4. Run and review the artifacts</li></ol></Card.Content></Card.Root>
  {/if}
  <Card.Root class="shrink-0"><Card.Header><Card.Title>Start with an outcome</Card.Title><Card.Description>Recipes fill in safe defaults; every setting remains editable.</Card.Description></Card.Header><Card.Content class="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
    {#each recipes as recipe (recipe.id)}<button type="button" class="hover:bg-muted rounded-md border p-3 text-left" class:border-primary={activeRecipe === recipe.id} onclick={() => applyRecipe(recipe)}><span class="block text-sm font-medium">{recipe.title}</span><span class="text-muted-foreground mt-1 block text-xs">{recipe.description}</span></button>{/each}
  </Card.Content></Card.Root>
  <Card.Root class="shrink-0 gap-0 py-0">
    <Card.Header class="border-b px-4 py-3">
      <div class="flex items-center gap-2">
        <Workflow class="size-5" />
        <Card.Title class="text-lg">Llama Studio</Card.Title>
        <span class="text-muted-foreground text-sm">GGUF model pipelines</span>
      </div>
    </Card.Header>
    <Card.Content class="grid gap-4 px-4 py-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,0.7fr)]">
      <div class="space-y-4">
        <div class="space-y-2">
          <Label.Root for="studio-operation">Tool/Recipe</Label.Root>
          <select id="studio-operation" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
            value={operation} onchange={(event) => selectOperation(event.currentTarget.value as typeof operation)} disabled={jobActive}>
            <option value="quantize">Quantize / requantize</option>
            <option value="pipeline">Pipeline: quantize and evaluate</option>
            <option value="hash">Hash / verify identity</option>
            <option value="split">Split into shards</option>
            <option value="merge">Merge model variants</option>
            <option value="prune">Structured pruning</option>
            <option value="train">QLoRA / SFT training</option>
            <option value="export-lora">Export / merge LoRA</option>
            <option value="evaluate">Benchmark / perplexity</option>
            {#if pipelines.length}
              <optgroup label="Custom pipelines">
                {#each pipelines as pipeline (pipeline.id)}<option value={`template:${pipeline.id}`}>{pipeline.name}</option>{/each}
              </optgroup>
            {/if}
          </select>
        </div>
        <StudioResourcePicker id="studio-model" label="Input model" bind:value={input} {resources} types={["model"]} placeholder={loadingModels ? "Loading models…" : "Search models and generated artifacts"} disabled={loadingModels || jobActive} onValueChange={(value) => void selectModel(value)} />

        {#if input && ["quantize", "pipeline", "merge", "prune", "train", "export-lora", "evaluate"].includes(operation)}
          <div class="border-border space-y-2 rounded-md border p-3">
            <div class="flex items-center gap-2"><span class="text-sm font-medium">Hardware advisor</span><Button class="ml-auto" size="sm" variant="outline" onclick={runPreflight} disabled={preflightBusy}>{preflightBusy ? "Checking…" : "Check fit"}</Button></div>
            {#if preflight}
              <div class="grid gap-2 text-xs sm:grid-cols-3">
                <span>RAM free: {preflight.hardware.ramKnown ? formatSize(preflight.hardware.freeRamBytes ?? 0) : "unknown"}</span>
                <span>VRAM free: {preflight.hardware.vramKnown ? formatSize(preflight.hardware.freeVramBytes ?? 0) : "unknown"}</span>
                <span>Disk free: {formatSize(preflight.hardware.diskFreeBytes ?? 0)}</span>
                {#if preflight.estimatedOutputBytes}<span>Output: ~{formatSize(preflight.estimatedOutputBytes)}</span>{/if}
                {#if preflight.estimatedRamBytes}<span>Peak RAM: ~{formatSize(preflight.estimatedRamBytes)}</span>{/if}
                {#if preflight.estimatedVramBytes}<span>VRAM target: ~{formatSize(preflight.estimatedVramBytes)}</span>{/if}
              </div>
              <p class={preflight.fits ? "text-success text-sm" : "text-destructive text-sm"}>{preflight.fits ? "This operation is expected to fit." : "This operation is not expected to fit with the current resources."}</p>
              {#each preflight.warnings ?? [] as warning}<p class="text-warning text-xs">{warning}</p>{/each}
              {#if Object.keys(preflight.recommendations).length}<div class="flex items-center gap-2"><span class="text-muted-foreground text-xs">Recommended: {Object.entries(preflight.recommendations).map(([key, value]) => `${key}=${value}`).join(" · ")}</span><Button class="ml-auto" size="sm" variant="secondary" onclick={applyPreflight}>Apply</Button></div>{/if}
            {/if}
          </div>
        {/if}

        {#if isTemplateOperation}
          <div class="space-y-2">
            <Label.Root for="template-name">Pipeline name</Label.Root>
            <Input id="template-name" bind:value={templateName} disabled={jobActive} />
          </div>
          <PipelineStepEditor bind:steps={templateSteps} {resources} idPrefix="studio-template" />
        {:else if operation === "quantize" || operation === "pipeline"}
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="space-y-2">
            <Label.Root for="quant-type">Output type</Label.Root>
            <select
              id="quant-type"
              class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
              value={quantType}
              onchange={(event) => selectQuantType(event.currentTarget.value)}
              disabled={jobActive}
            >
              {#each quantTypes as type}<option value={type}>{type}</option>{/each}
            </select>
          </div>
          <div class="space-y-2">
            <Label.Root for="quant-threads">Threads</Label.Root>
            <Input id="quant-threads" type="number" min="0" bind:value={threads} placeholder="Auto" disabled={jobActive} />
          </div>
        </div>

        <div class="space-y-2">
          <Label.Root for="studio-output">Output model</Label.Root>
          <Input id="studio-output" bind:value={output} disabled={dryRun || jobActive} />
          <p class="text-muted-foreground text-xs">Relative to the configured models directory. Existing files are never overwritten.</p>
        </div>

        <div class="space-y-2">
          <Label.Root for="importance-matrix">Importance matrix <span class="text-muted-foreground">(optional)</span></Label.Root>
          <Input id="importance-matrix" bind:value={importanceMatrix} placeholder="path/to/model.imatrix" disabled={jobActive} />
        </div>

        <div class="grid gap-3 sm:grid-cols-2">
          {#if operation === "quantize"}<label class="flex items-center gap-2 text-sm"><Switch.Root checked={dryRun} onCheckedChange={(value) => dryRun = value} />Plan only (dry run)</label>{/if}
          <label class="flex items-center gap-2 text-sm"><Switch.Root checked={allowRequantize} onCheckedChange={(value) => allowRequantize = value} />Allow requantization</label>
          <label class="flex items-center gap-2 text-sm"><Switch.Root checked={leaveOutputTensor} onCheckedChange={(value) => leaveOutputTensor = value} />Leave output tensor</label>
          <label class="flex items-center gap-2 text-sm"><Switch.Root checked={pure} onCheckedChange={(value) => pure = value} />Pure quantization</label>
        </div>

        {#if allowRequantize}
          <div class="border-warning/50 bg-warning/10 rounded-md border px-3 py-2 text-sm">
            Requantizing an already quantized model can significantly reduce quality. Prefer an F16, BF16, or F32 source when available.
          </div>
        {/if}
        {#if operation === "pipeline"}
          <div class="border-border space-y-3 rounded-md border p-3">
            <label class="flex items-center gap-2 text-sm font-medium"><Switch.Root checked={pipelineEvaluate} onCheckedChange={(value) => pipelineEvaluate = value} />Evaluate the generated model</label>
            {#if pipelineEvaluate}
              <div class="space-y-2"><Label.Root for="pipeline-evaluate-mode">Evaluation</Label.Root><select id="pipeline-evaluate-mode" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={evaluateMode}><option value="benchmark">Performance benchmark</option><option value="perplexity">Dataset perplexity</option></select></div>
              {#if evaluateMode === "perplexity"}<StudioResourcePicker id="pipeline-dataset" label="Evaluation dataset" bind:value={evaluateDataset} {resources} types={["dataset"]} placeholder="Search datasets" />{/if}
            {/if}
          </div>
        {/if}
        {:else if operation === "hash"}
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-2">
              <Label.Root for="hash-algorithm">Algorithm</Label.Root>
              <select id="hash-algorithm" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={hashAlgorithm}>
                <option value="sha256">SHA-256</option><option value="sha1">SHA-1</option><option value="xxh64">XXH64</option><option value="all">All</option>
              </select>
            </div>
            <div class="space-y-3 pt-1">
              <label class="flex items-center gap-2 text-sm"><Switch.Root checked={hashNoLayer} onCheckedChange={(value) => hashNoLayer = value} />Skip per-layer hashes</label>
              <label class="flex items-center gap-2 text-sm"><Switch.Root checked={hashUUID} onCheckedChange={(value) => hashUUID = value} />Generate UUIDv5</label>
            </div>
          </div>
        {:else if operation === "split"}
          <div class="space-y-2">
            <Label.Root for="studio-output">Output shard prefix</Label.Root>
            <Input id="studio-output" bind:value={output} disabled={jobActive} />
            <p class="text-muted-foreground text-xs">All matching output shards must be new; Studio never overwrites an existing shard.</p>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-2"><Label.Root for="split-tensors">Maximum tensors per shard</Label.Root><Input id="split-tensors" type="number" min="1" bind:value={splitMaxTensors} /></div>
            <div class="space-y-2"><Label.Root for="split-size">Maximum shard size</Label.Root><Input id="split-size" bind:value={splitMaxSize} placeholder="e.g. 4G" /></div>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={dryRun} onCheckedChange={(value) => dryRun = value} />Plan only (dry run)</label>
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={noTensorFirstSplit} onCheckedChange={(value) => noTensorFirstSplit = value} />Metadata-only first shard</label>
          </div>
        {:else if operation === "merge"}
          <div class="space-y-2">
            <Label.Root for="merge-models">Models to merge</Label.Root>
            <select id="merge-models" multiple class="border-input bg-background min-h-28 w-full rounded-md border px-3 py-2 text-sm" bind:value={mergeModels} disabled={jobActive}>
              {#each resources.filter((resource) => resource.type === "model" && resource.exists && resource.path !== input) as model (model.path)}
                <option value={model.path}>{model.path} · {formatSize(model.size)}</option>
              {/each}
            </select>
            <p class="text-muted-foreground text-xs">Use Ctrl/Cmd to select multiple compatible model variants.</p>
          </div>
          <div class="space-y-2"><Label.Root for="merge-output">Output model</Label.Root><Input id="merge-output" bind:value={output} /></div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-2"><Label.Root for="merge-method">Method</Label.Root><select id="merge-method" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={mergeMethod}><option value="ties">TIES</option><option value="evo">Evolutionary</option></select></div>
            <div class="space-y-2"><Label.Root for="merge-density">TIES density</Label.Root><Input id="merge-density" type="number" min="0.01" max="1" step="0.05" bind:value={mergeDensity} /></div>
            <div class="space-y-2"><Label.Root for="merge-memory">Worker memory budget</Label.Root><Input id="merge-memory" bind:value={mergeMemoryBudget} placeholder="2G" /></div>
            <div class="space-y-2"><Label.Root for="merge-threads">Threads</Label.Root><Input id="merge-threads" type="number" min="0" bind:value={threads} placeholder="Auto" /></div>
          </div>
          {#if mergeMethod === "evo"}
            <div class="grid gap-3 sm:grid-cols-2">
              <StudioResourcePicker id="merge-calibration" label="Calibration dataset" bind:value={mergeCalibration} {resources} types={["dataset"]} placeholder="Search datasets" />
              <div class="space-y-2"><Label.Root for="merge-target">Target type</Label.Root><select id="merge-target" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={mergeTargetType}><option value="q4_k">Q4_K</option><option value="q4_0">Q4_0</option><option value="q3_k">Q3_K</option><option value="mxfp4">MXFP4</option></select></div>
            </div>
          {/if}
        {:else if operation === "prune"}
          <div class="space-y-2">
            <Label.Root for="prune-phase">Pruning phase</Label.Root>
            <select id="prune-phase" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={prunePhase}>
              <option value="analyze">1. Analyze model and dataset</option>
              <option value="profiles">2. Generate ratio profiles</option>
              <option value="inspect">3. Inspect a profile</option>
              <option value="hard">4. Create hard-pruned model</option>
            </select>
          </div>
          {#if prunePhase === "analyze"}
            <StudioResourcePicker id="prune-dataset" label="Training / calibration dataset" bind:value={pruneDataset} {resources} types={["dataset"]} placeholder="Search datasets" />
          {:else if prunePhase === "profiles"}
            <div class="space-y-2"><Label.Root for="prune-cache">Importance cache</Label.Root><Input id="prune-cache" bind:value={pruneCache} placeholder="pruning/importance.cache" /></div>
          {:else}
            <div class="space-y-2"><Label.Root for="prune-profile">Pruning profile</Label.Root><Input id="prune-profile" bind:value={pruneProfile} placeholder="pruning/profile.json" /></div>
          {/if}
          {#if prunePhase === "analyze" || prunePhase === "profiles"}
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-2"><Label.Root for="prune-ratios">Ratios</Label.Root><Input id="prune-ratios" bind:value={pruneRatios} placeholder="0.1, 0.2, 0.3" /></div>
              <div class="space-y-2"><Label.Root for="prune-directory">New output directory</Label.Root><Input id="prune-directory" bind:value={pruneOutputDir} /></div>
            </div>
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={pruneEvaluate} onCheckedChange={(value) => pruneEvaluate = value} />Evaluate generated ratios</label>
          {:else if prunePhase === "hard"}
            <div class="space-y-2"><Label.Root for="prune-output">Output model</Label.Root><Input id="prune-output" bind:value={output} /></div>
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={pruneValidate} onCheckedChange={(value) => pruneValidate = value} />Reject output if perplexity regression exceeds threshold</label>
            {#if pruneValidate}
              <div class="grid gap-3 sm:grid-cols-2">
                <StudioResourcePicker id="prune-validation-dataset" label="Validation dataset" bind:value={pruneDataset} {resources} types={["dataset"]} placeholder="Search datasets" />
                <div class="space-y-2"><Label.Root for="prune-ppl-delta">Maximum perplexity increase (%)</Label.Root><Input id="prune-ppl-delta" type="number" min="0" step="0.5" bind:value={pruneMaxPPLDelta} /></div>
              </div>
            {/if}
          {/if}
          {#if prunePhase !== "inspect"}
            <div class="grid gap-3 sm:grid-cols-3">
              <div class="space-y-2"><Label.Root for="prune-layer-ratio">Maximum layer ratio</Label.Root><Input id="prune-layer-ratio" type="number" min="0" max="1" step="0.05" bind:value={pruneMaxLayerRatio} placeholder="Tool default" /></div>
              <div class="space-y-2"><Label.Root for="prune-threads">Threads</Label.Root><Input id="prune-threads" type="number" min="0" bind:value={threads} placeholder="Auto" /></div>
              <div class="space-y-2"><Label.Root for="prune-gpu-layers">GPU layers</Label.Root><Input id="prune-gpu-layers" type="number" min="-1" bind:value={gpuLayers} /></div>
            </div>
          {/if}
        {:else if operation === "train"}
          <div class="space-y-2">
            <StudioResourcePicker id="train-dataset" label="JSONL training dataset" bind:value={trainDataset} {resources} types={["dataset"]} placeholder="Search datasets" />
            <Button variant="outline" onclick={inspectDataset} disabled={!trainDataset.trim() || inspectingDataset}>{inspectingDataset ? "Inspecting…" : "Inspect selected dataset"}</Button>
            {#if datasetInspection}
              <p class="text-muted-foreground text-xs">{datasetInspection.recordsScanned}{datasetInspection.truncated ? "+" : ""} records checked · {Object.entries(datasetInspection.formats).map(([format, count]) => `${format}: ${count}`).join(" · ")}</p>
            {/if}
          </div>
          <div class="space-y-2"><Label.Root for="train-output">LoRA adapter output</Label.Root><Input id="train-output" bind:value={output} /></div>
          <div class="space-y-2"><StudioResourcePicker id="train-resume" label="Resume checkpoint (optional)" bind:value={trainResume} {resources} types={["checkpoint"]} placeholder="Search training checkpoints" />{#if latestCheckpoint}<Button size="sm" variant="outline" onclick={() => trainResume = latestCheckpoint.path}>Resume latest checkpoint</Button>{/if}</div>
          <div class="grid gap-3 sm:grid-cols-3">
            <div class="space-y-2"><Label.Root for="train-epochs">Epochs</Label.Root><Input id="train-epochs" type="number" min="1" bind:value={trainEpochs} /></div>
            <div class="space-y-2"><Label.Root for="train-rate">Learning rate</Label.Root><Input id="train-rate" type="number" min="0" step="0.000001" bind:value={trainLearningRate} /></div>
            <div class="space-y-2"><Label.Root for="train-validation">Validation split</Label.Root><Input id="train-validation" type="number" min="0" max="0.99" step="0.01" bind:value={trainValidationSplit} /></div>
            <div class="space-y-2"><Label.Root for="train-rank">LoRA rank</Label.Root><Input id="train-rank" type="number" min="1" bind:value={trainRank} /></div>
            <div class="space-y-2"><Label.Root for="train-alpha">LoRA alpha</Label.Root><Input id="train-alpha" type="number" min="0" bind:value={trainAlpha} placeholder="Use rank" /></div>
            <div class="space-y-2"><Label.Root for="train-save">Checkpoint every N windows</Label.Root><Input id="train-save" type="number" min="0" bind:value={trainSaveEvery} /></div>
            <div class="space-y-2"><Label.Root for="train-optimizer">Optimizer</Label.Root><select id="train-optimizer" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={trainOptimizer}><option value="adamw">AdamW</option><option value="adamw_f16">AdamW F16</option><option value="adamw_q8_0">AdamW Q8_0</option><option value="adamw_q6_k">AdamW Q6_K</option><option value="adamw_iq4_nl">AdamW IQ4_NL</option><option value="sgd">SGD</option></select></div>
            <div class="space-y-2"><Label.Root for="train-qat">LoRA fake quantization</Label.Root><select id="train-qat" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={trainQAT}><option value="none">None</option><option value="q4_k">Q4_K</option><option value="q4_0">Q4_0</option><option value="q3_k">Q3_K</option><option value="mxfp4">MXFP4</option><option value="q6_k">Q6_K</option><option value="q8_0">Q8_0</option></select></div>
            <div class="space-y-2"><Label.Root for="train-scheduler">LR scheduler</Label.Root><select id="train-scheduler" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={trainScheduler}><option value="constant">Constant</option><option value="cosine">Cosine</option></select></div>
          </div>
          <div class="grid gap-3 sm:grid-cols-3">
            <div class="space-y-2"><Label.Root for="train-context">Context size</Label.Root><Input id="train-context" type="number" min="0" bind:value={trainContextSize} /><p class="text-muted-foreground text-xs">0 uses the model training context.</p></div>
            <div class="space-y-2"><Label.Root for="train-batch">Logical batch</Label.Root><Input id="train-batch" type="number" min="1" bind:value={trainBatchSize} /><p class="text-muted-foreground text-xs">Must divide the model training context exactly.</p></div>
            <div class="space-y-2"><Label.Root for="train-ubatch">Physical micro-batch</Label.Root><Input id="train-ubatch" type="number" min="1" max={trainBatchSize} bind:value={trainUBatchSize} /><p class="text-muted-foreground text-xs">Lower this to reduce peak VRAM.</p></div>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={trainShuffle} onCheckedChange={(value) => trainShuffle = value} />Shuffle dataset each epoch</label>
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={trainOnPrompt} onCheckedChange={(value) => trainOnPrompt = value} />Train on prompt tokens</label>
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={trainVerboseLoss} onCheckedChange={(value) => trainVerboseLoss = value} />Structured loss logs</label>
          </div>
          <div class="grid gap-3 sm:grid-cols-3">
            <div class="space-y-2"><Label.Root for="train-grad">Gradient checkpoint interval</Label.Root><Input id="train-grad" type="number" min="0" bind:value={trainGradCheckpoint} /></div>
            <div class="space-y-2"><Label.Root for="train-threads">Threads</Label.Root><Input id="train-threads" type="number" min="0" bind:value={threads} placeholder="Auto" /></div>
            <div class="space-y-2"><Label.Root for="train-gpu">GPU layers</Label.Root><Input id="train-gpu" type="number" min="-1" bind:value={gpuLayers} /></div>
          </div>
        {:else if operation === "export-lora"}
          <div class="space-y-2">
            <Label.Root for="export-adapters">LoRA adapters</Label.Root>
            <select id="export-adapters" multiple class="border-input bg-background min-h-28 w-full rounded-md border px-3 py-2 text-sm" bind:value={exportAdapters}>
              {#each resources.filter((resource) => ["adapter", "checkpoint"].includes(resource.type) && resource.exists) as model (model.path)}
                <option value={model.path}>{model.path} · {model.kind} · {formatSize(model.size)}</option>
              {/each}
            </select>
          </div>
          <div class="space-y-2"><Label.Root for="export-output">Standalone GGUF output</Label.Root><Input id="export-output" bind:value={output} /></div>
          <div class="space-y-2"><Label.Root for="export-type">Output tensor type</Label.Root><select id="export-type" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={exportType}><option value="F16">F16</option><option value="BF16">BF16</option><option value="F32">F32</option><option value="Q8_0">Q8_0</option><option value="Q6_K">Q6_K</option><option value="Q5_K">Q5_K</option><option value="Q4_K">Q4_K</option><option value="Q4_0">Q4_0</option><option value="Q3_K">Q3_K</option><option value="IQ4_NL">IQ4_NL</option><option value="MXFP4">MXFP4</option></select></div>
        {:else}
          <div class="space-y-2"><Label.Root for="evaluate-mode">Evaluation</Label.Root><select id="evaluate-mode" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={evaluateMode}><option value="benchmark">Performance benchmark</option><option value="perplexity">Dataset perplexity</option></select></div>
          {#if evaluateMode === "perplexity"}
            <StudioResourcePicker id="evaluate-dataset" label="Evaluation dataset" bind:value={evaluateDataset} {resources} types={["dataset"]} placeholder="Search datasets" />
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-2"><Label.Root for="evaluate-context">Context size</Label.Root><Input id="evaluate-context" type="number" min="1" bind:value={contextSize} /></div>
              <div class="space-y-2"><Label.Root for="evaluate-chunks">Maximum chunks</Label.Root><Input id="evaluate-chunks" type="number" min="0" bind:value={chunks} placeholder="All" /></div>
            </div>
          {:else}
            <div class="grid gap-3 sm:grid-cols-3">
              <div class="space-y-2"><Label.Root for="evaluate-prompt">Prompt tokens</Label.Root><Input id="evaluate-prompt" type="number" min="1" bind:value={promptTokens} /></div>
              <div class="space-y-2"><Label.Root for="evaluate-gen">Generated tokens</Label.Root><Input id="evaluate-gen" type="number" min="1" bind:value={genTokens} /></div>
              <div class="space-y-2"><Label.Root for="evaluate-repetitions">Repetitions</Label.Root><Input id="evaluate-repetitions" type="number" min="1" bind:value={repetitions} /></div>
            </div>
          {/if}
          <div class="grid gap-3 sm:grid-cols-4">
            <div class="space-y-2"><Label.Root for="evaluate-batch">Batch</Label.Root><Input id="evaluate-batch" type="number" min="1" bind:value={batchSize} /></div>
            <div class="space-y-2"><Label.Root for="evaluate-ubatch">Micro-batch</Label.Root><Input id="evaluate-ubatch" type="number" min="1" bind:value={ubatchSize} /></div>
            <div class="space-y-2"><Label.Root for="evaluate-threads">Threads</Label.Root><Input id="evaluate-threads" type="number" min="0" bind:value={threads} /></div>
            <div class="space-y-2"><Label.Root for="evaluate-gpu">GPU layers</Label.Root><Input id="evaluate-gpu" type="number" min="-1" bind:value={gpuLayers} /></div>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-2"><Label.Root for="evaluate-baseline">Baseline evaluation job <span class="text-muted-foreground">(optional)</span></Label.Root><Input id="evaluate-baseline" bind:value={baselineJobID} placeholder="task-…" /></div>
            <div class="space-y-2"><Label.Root for="evaluate-regression">Maximum regression (%)</Label.Root><Input id="evaluate-regression" type="number" min="0" step="0.1" bind:value={maxRegressionPercent} disabled={!baselineJobID.trim()} /></div>
          </div>
        {/if}

        {#if error}<div class="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-sm">{error}</div>{/if}

        <div class="flex justify-end gap-2">
          {#if jobActive}
            <Button variant="destructive" onclick={cancelJob}><Square class="size-4" />Cancel</Button>
          {/if}
          <Button onclick={runOperation} disabled={!canStart || starting}>
            {#if starting}<Loader2 class="size-4 animate-spin" />{:else}<Play class="size-4" />{/if}
            {isTemplateOperation ? "Run pipeline" : operation === "pipeline" ? "Run pipeline" : operation === "hash" ? "Calculate hash" : operation === "merge" ? "Merge models" : operation === "prune" ? `Run ${prunePhase}` : operation === "train" ? "Start training" : operation === "export-lora" ? "Export model" : operation === "evaluate" ? "Run evaluation" : dryRun ? "Build plan" : operation === "split" ? "Split model" : "Quantize"}
          </Button>
        </div>
      </div>

      <div class="min-h-56">
        <div class="mb-2 flex items-center gap-2 text-sm font-medium"><FileSearch class="size-4" />Model inspection</div>
        {#if inspecting}
          <div class="text-muted-foreground flex items-center gap-2 text-sm"><Loader2 class="size-4 animate-spin" />Reading GGUF metadata…</div>
        {:else if inspection}
          <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 text-sm">
            <dt class="text-muted-foreground">Name</dt><dd class="truncate" title={inspection.name}>{inspection.name}</dd>
            <dt class="text-muted-foreground">Size</dt><dd>{formatSize(inspection.size)}</dd>
            <dt class="text-muted-foreground">GGUF</dt><dd>Version {inspection.version}</dd>
            <dt class="text-muted-foreground">Architecture</dt><dd>{String(inspection.metadata["general.architecture"] ?? "Unknown")}</dd>
            <dt class="text-muted-foreground">Model name</dt><dd>{String(inspection.metadata["general.name"] ?? "Unknown")}</dd>
            <dt class="text-muted-foreground">Quantization</dt><dd>{String(inspection.metadata["general.file_type"] ?? "Unknown")}</dd>
            <dt class="text-muted-foreground">Modified</dt><dd>{new Date(inspection.modifiedAt).toLocaleString()}</dd>
          </dl>
        {:else}
          <div class="text-muted-foreground flex h-40 flex-col items-center justify-center gap-2 text-sm"><Boxes class="size-8" />Select a GGUF model to inspect it.</div>
        {/if}
      </div>
    </Card.Content>
  </Card.Root>

  {#if job}
    <Card.Root class="h-[min(40rem,70vh)] min-h-72 shrink-0 gap-0 py-0">
      <Card.Header class="border-b px-4 py-3">
        <div class="flex items-center gap-3">
          <Card.Title class="text-sm capitalize">{job.operation ?? "Studio job"}</Card.Title>
          <span class="text-muted-foreground text-xs uppercase">{job.state}</span>
          <span class="text-muted-foreground ml-auto text-xs">{job.pct >= 0 ? `${job.pct}%` : ""}</span>
        </div>
        <div class="bg-muted h-1.5 overflow-hidden rounded-full">
          <div class="bg-primary h-full transition-all" style={`width: ${Math.max(0, job.pct)}%`}></div>
        </div>
        <p class="text-muted-foreground truncate text-xs">{job.message}</p>
        {#if job.artifacts?.length}
          <div class="flex flex-wrap gap-2">
            {#each job.artifacts as artifact (artifact.path)}
              <span class="bg-muted rounded px-2 py-1 text-xs">{artifact.name} · {formatSize(artifact.size)}</span>
            {/each}
          </div>
        {/if}
        {#if job.operation === "train-qlora" && trainingMetrics.length}
          <div class="border-border rounded-md border p-2"><div class="mb-1 flex text-xs"><span>Training loss</span><span class="text-muted-foreground ml-auto">latest {trainingMetrics.at(-1)?.loss.toPrecision(4)}{trainingMetrics.at(-1)?.learningRate ? ` · lr ${trainingMetrics.at(-1)?.learningRate}` : ""}</span></div><svg viewBox="0 0 300 84" class="h-20 w-full" role="img" aria-label="Training loss over time"><polyline points={lossPoints} fill="none" stroke="currentColor" stroke-width="2" vector-effect="non-scaling-stroke" /></svg></div>
        {/if}
      </Card.Header>
      <Card.Content class="min-h-0 flex-1 p-0">
        <LogPanel id={`studio-${job.id}`} title="Operation log" logData={jobLogs} />
      </Card.Content>
    </Card.Root>
  {/if}
</div>
