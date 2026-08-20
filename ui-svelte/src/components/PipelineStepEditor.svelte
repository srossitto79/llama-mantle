<script lang="ts">
  import { ArrowDown, ArrowUp, Plus, Trash2 } from "@lucide/svelte";
  import * as Label from "$lib/components/ui/label/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import StudioResourcePicker from "./StudioResourcePicker.svelte";
  import { addListResource, draft, fieldHint, fieldLabel, fieldValue, operations, setField, variantsPlaceholder, visibleFields, type DraftStep, type Operation } from "../lib/pipelineSteps";
  import type { StudioResource } from "../lib/types";

  let { steps = $bindable(), resources, idPrefix = "pipeline-step" }: { steps: DraftStep[]; resources: StudioResource[]; idPrefix?: string } = $props();

  function changeOperation(index: number, operation: Operation) {
    steps[index] = draft(operation, index > 0);
  }

  function move(index: number, offset: number) {
    const target = index + offset;
    if (target < 0 || target >= steps.length) return;
    [steps[index], steps[target]] = [steps[target], steps[index]];
    steps = [...steps];
  }
</script>

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
      {#each visibleFields(step) as field}
        {#if field.type === "boolean"}
          <label class="flex items-center gap-2 self-end py-2 text-sm"><Switch.Root checked={Boolean(fieldValue(step, field))} onCheckedChange={(value) => setField(step, field, value)} />{fieldLabel(step, field)}</label>
        {:else if field.type === "resource"}
          <StudioResourcePicker id={`${idPrefix}-${index}-${field.key}`} label={fieldLabel(step, field)} value={String(fieldValue(step, field))} {resources} types={field.resourceTypes ?? []} placeholder={field.placeholder ?? "Search catalog or enter a path"} onValueChange={(value) => setField(step, field, value)} />
        {:else if field.type === "resourceList" || field.type === "scaledResourceList"}
          <div class="space-y-1"><Label.Root for={`${idPrefix}-${index}-${field.key}`}>{field.label}</Label.Root><Input id={`${idPrefix}-${index}-${field.key}`} value={String(fieldValue(step, field))} placeholder="Comma-separated paths" oninput={(event) => setField(step, field, event.currentTarget.value)} /><select aria-label={`Add ${field.label}`} class="border-input bg-background h-8 w-full rounded-md border px-2 text-xs" value="" onchange={(event) => { addListResource(step, field, event.currentTarget.value); event.currentTarget.value = ""; }}><option value="">Add from Studio catalog…</option>{#each resources.filter((resource) => resource.exists && (!field.resourceTypes?.length || field.resourceTypes.includes(resource.type))) as resource (resource.path)}<option value={resource.path}>{resource.path}</option>{/each}</select></div>
        {:else}
          <div class="space-y-1"><Label.Root>{fieldLabel(step, field)}</Label.Root>
            {#if field.options}<select class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" value={String(fieldValue(step, field))} onchange={(event) => setField(step, field, event.currentTarget.value)}>{#each field.options as option}<option value={option}>{option}</option>{/each}</select>
            {:else}<Input type={field.type === "number" ? "number" : "text"} placeholder={field.placeholder} value={fieldValue(step, field) as string | number} oninput={(event) => setField(step, field, event.currentTarget.value)} />{/if}
            {#if fieldHint(step.operation, field)}<p class="text-muted-foreground text-xs leading-snug">{fieldHint(step.operation, field)}</p>{/if}
          </div>
        {/if}
      {/each}
    </div>
    <details><summary class="text-muted-foreground cursor-pointer text-xs">Advanced request JSON</summary><textarea class="border-input bg-background mt-2 min-h-32 w-full rounded-md border p-3 font-mono text-xs" bind:value={step.requestText} spellcheck="false"></textarea></details>
    <details><summary class="text-muted-foreground cursor-pointer text-xs">Fan-out variants and quality gate</summary><div class="mt-2 space-y-2"><Label.Root>Variant request array (optional, maximum 8)</Label.Root><textarea class="border-input bg-background min-h-28 w-full rounded-md border p-3 font-mono text-xs" bind:value={step.variantsText} placeholder={variantsPlaceholder}></textarea><label class="flex items-center gap-2 text-sm"><Switch.Root checked={step.continueOnFailure} onCheckedChange={(value) => step.continueOnFailure = value} />Continue when one variant fails</label>{#if step.operation === "evaluate"}<div class="grid gap-2 sm:grid-cols-3"><Input bind:value={step.gateMetric} placeholder="Metric, e.g. perplexity" /><Input bind:value={step.gateMin} type="number" placeholder="Minimum" /><Input bind:value={step.gateMax} type="number" placeholder="Maximum" /></div>{/if}</div></details>
  </div>
{/each}
<Button variant="outline" onclick={() => steps = [...steps, draft("evaluate", steps.length > 0)]}><Plus class="size-4" />Add step</Button>
