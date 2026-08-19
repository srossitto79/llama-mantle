<script lang="ts">
  import { onMount } from "svelte";
  import { ArrowDown, ArrowUp, Download, Loader2, Play, Plus, Save, Trash2, Upload, Workflow } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import { deleteStudioPipelineTemplate, listLocalModels, listStudioPipelineTemplates, saveStudioPipelineTemplate, startStudioPipeline } from "../lib/mantleApi";
  import type { LocalModel, StudioPipelineStep, StudioPipelineTemplate } from "../lib/types";

  type Operation = StudioPipelineStep["operation"];
  type DraftStep = { operation: Operation; usePrevious: boolean; requestText: string; variantsText: string; continueOnFailure: boolean; gateMetric: string; gateMin: string; gateMax: string };
  type FieldSpec = { key: string; label: string; type?: "text" | "number" | "boolean" | "list"; options?: string[] };
  const operations: Operation[] = ["quantize", "hash", "split", "merge", "prune", "train-qlora", "export-lora", "evaluate", "register"];
  const variantsPlaceholder = '[{"output":"q4.gguf","type":"Q4_K_M"},{"output":"q6.gguf","type":"Q6_K"}]';
  const defaults: Record<Operation, Record<string, unknown>> = {
    quantize: { output: "output-Q4_K_M.gguf", type: "Q4_K_M" },
    hash: { algorithm: "sha256", noLayer: true },
    split: { output: "output-split.gguf", maxTensors: 128 },
    merge: { models: [], output: "merged.gguf", method: "ties", density: 0.5 },
    prune: { phase: "hard", profile: "pruning/profile.json", output: "pruned.gguf" },
    "train-qlora": { dataset: "datasets/train.jsonl", output: "adapter.gguf", epochs: 2, rank: 16 },
    "export-lora": { adapters: [], output: "lora-merged.gguf", tensorType: "F16" },
    evaluate: { mode: "benchmark", promptTokens: 512, genTokens: 128, repetitions: 5 },
    register: { modelID: "studio-model", contextSize: 4096, gpuLayers: -1 },
  };
  const fields: Record<Operation, FieldSpec[]> = {
    quantize: [{ key: "output", label: "Output" }, { key: "type", label: "Tensor type", options: ["Q4_K_M", "Q5_K_M", "Q6_K", "Q8_0", "IQ4_XS", "F16", "BF16"] }, { key: "importanceMatrix", label: "Importance matrix" }, { key: "allowRequantize", label: "Allow requantization", type: "boolean" }],
    hash: [{ key: "algorithm", label: "Algorithm", options: ["sha256", "sha1", "xxh64", "all"] }, { key: "noLayer", label: "Skip layer hashes", type: "boolean" }],
    split: [{ key: "output", label: "Output prefix" }, { key: "maxTensors", label: "Maximum tensors", type: "number" }, { key: "maxSize", label: "Maximum size" }],
    merge: [{ key: "models", label: "Models (comma separated)", type: "list" }, { key: "output", label: "Output" }, { key: "method", label: "Method", options: ["ties", "evo"] }, { key: "density", label: "Density", type: "number" }],
    prune: [{ key: "phase", label: "Phase", options: ["analyze", "profiles", "inspect", "hard"] }, { key: "profile", label: "Profile" }, { key: "dataset", label: "Dataset" }, { key: "output", label: "Output model" }, { key: "outputDir", label: "Output directory" }],
    "train-qlora": [{ key: "dataset", label: "Dataset" }, { key: "output", label: "Adapter output" }, { key: "epochs", label: "Epochs", type: "number" }, { key: "rank", label: "Rank", type: "number" }, { key: "optimizer", label: "Optimizer", options: ["adamw", "adamw_f16", "adamw_q8_0", "sgd"] }],
    "export-lora": [{ key: "adapters", label: "Adapters (comma separated)", type: "list" }, { key: "output", label: "Output" }, { key: "tensorType", label: "Tensor type", options: ["F16", "BF16", "F32", "Q8_0", "Q6_K", "Q4_K", "Q4_0"] }],
    evaluate: [{ key: "mode", label: "Mode", options: ["benchmark", "perplexity"] }, { key: "dataset", label: "Dataset" }, { key: "promptTokens", label: "Prompt tokens", type: "number" }, { key: "genTokens", label: "Generated tokens", type: "number" }, { key: "repetitions", label: "Repetitions", type: "number" }, { key: "baselineJobID", label: "Baseline job ID" }, { key: "maxRegressionPercent", label: "Maximum regression %", type: "number" }],
    register: [{ key: "modelID", label: "Serving model ID" }, { key: "name", label: "Display name" }, { key: "description", label: "Description" }, { key: "contextSize", label: "Context size", type: "number" }, { key: "gpuLayers", label: "GPU layers", type: "number" }, { key: "ttl", label: "TTL seconds", type: "number" }, { key: "overwrite", label: "Replace existing ID", type: "boolean" }],
  };

  let models = $state<LocalModel[]>([]);
  let templates = $state<StudioPipelineTemplate[]>([]);
  let input = $state("");
  let name = $state("My pipeline");
  let templateID = $state("");
  let steps = $state<DraftStep[]>([draft("quantize", true), draft("evaluate", true)]);
  let busy = $state(false);
  let error = $state("");
  let message = $state("");
  let importInput: HTMLInputElement;

  function draft(operation: Operation, usePrevious: boolean): DraftStep {
    return { operation, usePrevious, requestText: JSON.stringify(defaults[operation], null, 2), variantsText: "", continueOnFailure: false, gateMetric: "", gateMin: "", gateMax: "" };
  }

  function changeOperation(index: number, operation: Operation) {
    steps[index] = draft(operation, index > 0);
  }

  function requestObject(step: DraftStep): Record<string, unknown> {
    try { const value = JSON.parse(step.requestText); return value && !Array.isArray(value) && typeof value === "object" ? value : {}; }
    catch { return {}; }
  }

  function fieldValue(step: DraftStep, field: FieldSpec): string | number | boolean {
    const value = requestObject(step)[field.key];
    if (field.type === "list") return Array.isArray(value) ? value.join(", ") : "";
    if (field.type === "boolean") return Boolean(value);
    return typeof value === "string" || typeof value === "number" ? value : "";
  }

  function setField(step: DraftStep, field: FieldSpec, value: string | boolean) {
    const request = requestObject(step);
    if (field.type === "number") request[field.key] = value === "" ? undefined : Number(value);
    else if (field.type === "list") request[field.key] = String(value).split(",").map((item) => item.trim()).filter(Boolean);
    else request[field.key] = value;
    if (request[field.key] === "" || request[field.key] === undefined) delete request[field.key];
    step.requestText = JSON.stringify(request, null, 2);
  }

  function move(index: number, offset: number) {
    const target = index + offset;
    if (target < 0 || target >= steps.length) return;
    [steps[index], steps[target]] = [steps[target], steps[index]];
    steps = [...steps];
  }

  function buildSteps(): StudioPipelineStep[] {
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

  async function run() {
    busy = true; error = ""; message = "";
    try {
      const task = await startStudioPipeline({ name, input: input || undefined, steps: buildSteps() });
      message = `Pipeline queued as ${task.id}. Follow it on Studio Jobs.`;
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function save() {
    busy = true; error = ""; message = "";
    try {
      const saved = await saveStudioPipelineTemplate({ id: templateID, name, pipeline: { name, input: input || undefined, steps: buildSteps() } });
      templateID = saved.id;
      templates = await listStudioPipelineTemplates();
      message = "Pipeline template saved.";
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  function load(template: StudioPipelineTemplate) {
    templateID = template.id; name = template.name; input = template.pipeline.input ?? "";
    steps = template.pipeline.steps.map((step) => ({ operation: step.operation, usePrevious: step.usePrevious ?? false, requestText: JSON.stringify(step.request, null, 2), variantsText: step.variants?.length ? JSON.stringify(step.variants, null, 2) : "", continueOnFailure: step.continueOnFailure ?? false, gateMetric: step.gate?.metric ?? "", gateMin: step.gate?.min?.toString() ?? "", gateMax: step.gate?.max?.toString() ?? "" }));
    error = ""; message = `Loaded ${template.name}.`;
  }

  async function remove(template: StudioPipelineTemplate) {
    try {
      await deleteStudioPipelineTemplate(template.id);
      if (templateID === template.id) templateID = "";
      templates = await listStudioPipelineTemplates();
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
  }

  function exportTemplate() {
    try {
      const payload = { version: 1, name, pipeline: { name, input: input || undefined, steps: buildSteps() } };
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url; anchor.download = `${name.trim().replace(/[^A-Za-z0-9._-]+/g, "-") || "pipeline"}.json`; anchor.click();
      URL.revokeObjectURL(url);
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
  }

  async function importTemplate(event: Event) {
    const file = (event.currentTarget as HTMLInputElement).files?.[0];
    if (!file) return;
    try {
      const parsed = JSON.parse(await file.text()) as { version?: number; name?: string; pipeline?: StudioPipelineTemplate["pipeline"] };
      if (parsed.version !== 1 || !parsed.pipeline || !Array.isArray(parsed.pipeline.steps)) throw new Error("Unsupported pipeline template file");
      load({ id: "", name: parsed.name || parsed.pipeline.name || "Imported pipeline", pipeline: parsed.pipeline });
      templateID = ""; message = "Template imported. Save it to keep it in Studio.";
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { importInput.value = ""; }
  }

  onMount(() => {
    const preselectedModel = new URLSearchParams(window.location.search).get("model") ?? "";
    void Promise.all([listLocalModels(), listStudioPipelineTemplates()]).then(([foundModels, foundTemplates]) => {
      models = foundModels; templates = foundTemplates;
      if (preselectedModel && foundModels.some((model) => model.name === preselectedModel)) input = preselectedModel;
    });
  });
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-2">
  <div class="flex items-center gap-2"><Workflow class="size-5" /><h2 class="text-lg font-semibold">Pipeline builder</h2><span class="text-muted-foreground text-sm">Typed Studio operations only</span></div>
  <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_18rem]">
    <Card.Root>
      <Card.Header><Card.Title>Workflow</Card.Title></Card.Header>
      <Card.Content class="space-y-4">
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="space-y-2"><Label.Root for="pipeline-name">Name</Label.Root><Input id="pipeline-name" bind:value={name} /></div>
          <div class="space-y-2"><Label.Root for="pipeline-input">Initial model</Label.Root><select id="pipeline-input" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={input}><option value="">No initial model</option>{#each models.filter((model) => model.kind === "gguf") as model}<option value={model.name}>{model.name}</option>{/each}</select></div>
        </div>
        {#each steps as step, index}
          <div class="border-border space-y-3 rounded-md border p-3">
            <div class="flex items-center gap-2">
              <span class="text-muted-foreground w-6 text-sm">{index + 1}</span>
              <select class="border-input bg-background h-9 flex-1 rounded-md border px-3 text-sm" value={step.operation} onchange={(event) => changeOperation(index, event.currentTarget.value as Operation)}>{#each operations as operation}<option value={operation}>{operation}</option>{/each}</select>
              <Button variant="ghost" size="icon" onclick={() => move(index, -1)} disabled={index === 0}><ArrowUp class="size-4" /></Button>
              <Button variant="ghost" size="icon" onclick={() => move(index, 1)} disabled={index === steps.length - 1}><ArrowDown class="size-4" /></Button>
              <Button variant="ghost" size="icon" onclick={() => steps = steps.filter((_, i) => i !== index)} disabled={steps.length === 1}><Trash2 class="size-4" /></Button>
            </div>
            <label class="flex items-center gap-2 text-sm"><Switch.Root checked={step.usePrevious} onCheckedChange={(value) => step.usePrevious = value} />Use previous generated model as input</label>
            <div class="grid gap-3 sm:grid-cols-2">
              {#each fields[step.operation] as field}
                {#if field.type === "boolean"}
                  <label class="flex items-center gap-2 self-end py-2 text-sm"><Switch.Root checked={Boolean(fieldValue(step, field))} onCheckedChange={(value) => setField(step, field, value)} />{field.label}</label>
                {:else}
                  <div class="space-y-1"><Label.Root>{field.label}</Label.Root>
                    {#if field.options}<select class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" value={String(fieldValue(step, field))} onchange={(event) => setField(step, field, event.currentTarget.value)}>{#each field.options as option}<option value={option}>{option}</option>{/each}</select>
                    {:else}<Input type={field.type === "number" ? "number" : "text"} value={fieldValue(step, field) as string | number} oninput={(event) => setField(step, field, event.currentTarget.value)} />{/if}
                  </div>
                {/if}
              {/each}
            </div>
            <details><summary class="text-muted-foreground cursor-pointer text-xs">Advanced request JSON</summary><textarea class="border-input bg-background mt-2 min-h-32 w-full rounded-md border p-3 font-mono text-xs" bind:value={step.requestText} spellcheck="false"></textarea></details>
            <details><summary class="text-muted-foreground cursor-pointer text-xs">Fan-out variants and quality gate</summary><div class="mt-2 space-y-2"><Label.Root>Variant request array (optional, maximum 8)</Label.Root><textarea class="border-input bg-background min-h-28 w-full rounded-md border p-3 font-mono text-xs" bind:value={step.variantsText} placeholder={variantsPlaceholder}></textarea><label class="flex items-center gap-2 text-sm"><Switch.Root checked={step.continueOnFailure} onCheckedChange={(value) => step.continueOnFailure = value} />Continue when one variant fails</label>{#if step.operation === "evaluate"}<div class="grid gap-2 sm:grid-cols-3"><Input bind:value={step.gateMetric} placeholder="Metric, e.g. perplexity" /><Input bind:value={step.gateMin} type="number" placeholder="Minimum" /><Input bind:value={step.gateMax} type="number" placeholder="Maximum" /></div>{/if}</div></details>
          </div>
        {/each}
        <Button variant="outline" onclick={() => steps = [...steps, draft("evaluate", steps.length > 0)]}><Plus class="size-4" />Add step</Button>
        {#if error}<div class="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-sm">{error}</div>{/if}
        {#if message}<div class="border-primary/30 bg-primary/5 rounded-md border px-3 py-2 text-sm">{message}</div>{/if}
        <input class="hidden" type="file" accept="application/json,.json" bind:this={importInput} onchange={importTemplate} />
        <div class="flex flex-wrap justify-end gap-2"><Button variant="outline" onclick={() => importInput.click()}><Upload class="size-4" />Import</Button><Button variant="outline" onclick={exportTemplate}><Download class="size-4" />Export</Button><Button variant="outline" onclick={save} disabled={busy || !name.trim()}><Save class="size-4" />Save template</Button><Button onclick={run} disabled={busy}>{#if busy}<Loader2 class="size-4 animate-spin" />{:else}<Play class="size-4" />{/if}Run pipeline</Button></div>
      </Card.Content>
    </Card.Root>
    <Card.Root class="h-fit"><Card.Header><Card.Title>Saved templates</Card.Title></Card.Header><Card.Content class="space-y-2">
      {#if templates.length === 0}<p class="text-muted-foreground text-sm">No saved templates.</p>{/if}
      {#each templates as template}<div class="border-border flex items-center gap-2 rounded-md border p-2"><button class="min-w-0 flex-1 truncate text-left text-sm" onclick={() => load(template)}>{template.name}<span class="text-muted-foreground block text-xs">{template.pipeline.steps.length} steps</span></button><Button variant="ghost" size="icon" onclick={() => remove(template)}><Trash2 class="size-4" /></Button></div>{/each}
    </Card.Content></Card.Root>
  </div>
</div>
