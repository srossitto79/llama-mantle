<script lang="ts">
  import { onMount } from "svelte";
  import { Download, Loader2, Play, Save, Trash2, Upload, Workflow } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import StudioResourcePicker from "../components/StudioResourcePicker.svelte";
  import PipelineStepEditor from "../components/PipelineStepEditor.svelte";
  import { deleteStudioPipelineTemplate, listStudioPipelineTemplates, listStudioResources, saveStudioPipelineTemplate, startStudioPipeline } from "../lib/mantleApi";
  import { buildPipelineSteps, draft, stepsFromTemplate, type DraftStep } from "../lib/pipelineSteps";
  import type { StudioPipelineTemplate, StudioResource } from "../lib/types";
  import { activeStudioProject } from "../stores/studioProject";

  let resources = $state<StudioResource[]>([]);
  let templates = $state<StudioPipelineTemplate[]>([]);
  let input = $state("");
  let name = $state("My pipeline");
  let templateID = $state("");
  let steps = $state<DraftStep[]>([draft("quantize", true), draft("evaluate", true)]);
  let busy = $state(false);
  let error = $state("");
  let message = $state("");
  let importInput: HTMLInputElement;
  let visibleTemplates = $derived(templates.filter((template) => !$activeStudioProject || template.projectID === $activeStudioProject));

  async function run() {
    busy = true; error = ""; message = "";
    try {
      const task = await startStudioPipeline({ name, input: input || undefined, projectID: $activeStudioProject || undefined, steps: buildPipelineSteps(steps) });
      message = `Pipeline queued as ${task.id}. Follow it on Studio Jobs.`;
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function save() {
    busy = true; error = ""; message = "";
    try {
      const saved = await saveStudioPipelineTemplate({ id: templateID, projectID: $activeStudioProject || undefined, name, pipeline: { name, input: input || undefined, projectID: $activeStudioProject || undefined, steps: buildPipelineSteps(steps) } });
      templateID = saved.id;
      templates = await listStudioPipelineTemplates();
      message = $activeStudioProject ? "Recipe saved to the active project." : "Recipe template saved.";
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  function load(template: StudioPipelineTemplate) {
    templateID = template.id; name = template.name; input = template.pipeline.input ?? "";
    steps = stepsFromTemplate(template.pipeline.steps);
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
      const payload = { version: 1, name, pipeline: { name, input: input || undefined, steps: buildPipelineSteps(steps) } };
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
    const params = new URLSearchParams(window.location.search);
    const preselectedModel = params.get("model") ?? "";
    const preselectedTemplate = params.get("template") ?? "";
    void Promise.all([listStudioResources(), listStudioPipelineTemplates()]).then(([foundResources, foundTemplates]) => {
      resources = foundResources; templates = foundTemplates;
      const selected = foundTemplates.find((template) => template.id === preselectedTemplate);
      if (selected) load(selected);
      if (preselectedModel && foundResources.some((resource) => resource.path === preselectedModel)) input = preselectedModel;
    });
  });
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-2">
  <div class="flex items-center gap-2"><Workflow class="size-5" /><h2 class="text-lg font-semibold">Recipes &amp; pipelines</h2><span class="text-muted-foreground text-sm">One operation is a recipe; add steps to build a pipeline.</span></div>
  <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_18rem]">
    <Card.Root>
      <Card.Header><Card.Title>Custom recipe</Card.Title><Card.Description>Common settings have controls. Advanced request JSON exposes every argument supported by the Studio operation API and is validated by the backend before execution.</Card.Description></Card.Header>
      <Card.Content class="space-y-4">
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="space-y-2"><Label.Root for="pipeline-name">Name</Label.Root><Input id="pipeline-name" bind:value={name} /></div>
          <StudioResourcePicker id="pipeline-input" label="Initial model" bind:value={input} {resources} types={["model", "artifact"]} placeholder="Search models and generated artifacts" />
        </div>
        <PipelineStepEditor bind:steps {resources} idPrefix="recipe" />
        {#if error}<div class="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-sm">{error}</div>{/if}
        {#if message}<div class="border-primary/30 bg-primary/5 rounded-md border px-3 py-2 text-sm">{message}</div>{/if}
        <input class="hidden" type="file" accept="application/json,.json" bind:this={importInput} onchange={importTemplate} />
        <div class="flex flex-wrap justify-end gap-2"><Button variant="outline" onclick={() => importInput.click()}><Upload class="size-4" />Import</Button><Button variant="outline" onclick={exportTemplate}><Download class="size-4" />Export</Button><Button variant="outline" onclick={save} disabled={busy || !name.trim()}><Save class="size-4" />Save template</Button><Button onclick={run} disabled={busy}>{#if busy}<Loader2 class="size-4 animate-spin" />{:else}<Play class="size-4" />{/if}Run pipeline</Button></div>
      </Card.Content>
    </Card.Root>
    <Card.Root class="h-fit"><Card.Header><Card.Title>Saved recipes</Card.Title><Card.Description>{#if $activeStudioProject}Showing the active project only.{:else}Showing all unscoped and project recipes.{/if}</Card.Description></Card.Header><Card.Content class="space-y-2">
      {#if visibleTemplates.length === 0}<p class="text-muted-foreground text-sm">No saved recipes in this scope.</p>{/if}
      {#each visibleTemplates as template}<div class="border-border flex items-center gap-2 rounded-md border p-2"><button class="min-w-0 flex-1 truncate text-left text-sm" onclick={() => load(template)}>{template.name}<span class="text-muted-foreground block text-xs">{template.pipeline.steps.length === 1 ? "1 operation" : `${template.pipeline.steps.length} steps`}</span></button><Button variant="ghost" size="icon" onclick={() => remove(template)}><Trash2 class="size-4" /></Button></div>{/each}
    </Card.Content></Card.Root>
  </div>
</div>
