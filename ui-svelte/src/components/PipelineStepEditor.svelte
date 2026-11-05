<script lang="ts">
  import * as NativeSelect from "$lib/components/ui/native-select/index.js";
  import { ArrowDown, ArrowUp, Plus, Trash2 } from "@lucide/svelte";
  import * as Label from "$lib/components/ui/label/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
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
  <div class="space-y-3 border-t pt-4 first:border-t-0 first:pt-0">
    <div class="flex items-center gap-2">
      <span class="text-muted-foreground w-6 text-sm tabular-nums">{index + 1}</span>
      <NativeSelect.Root class="flex-1" value={step.operation} onchange={(event) => changeOperation(index, event.currentTarget.value as Operation)}>{#each operations as operation}<option value={operation}>{operation}</option>{/each}</NativeSelect.Root>
      <Button variant="ghost" size="icon-sm" aria-label="Move up" onclick={() => move(index, -1)} disabled={index === 0}><ArrowUp class="size-4" /></Button>
      <Button variant="ghost" size="icon-sm" aria-label="Move down" onclick={() => move(index, 1)} disabled={index === steps.length - 1}><ArrowDown class="size-4" /></Button>
      <Button variant="ghost" size="icon-sm" aria-label="Remove step" onclick={() => steps = steps.filter((_, i) => i !== index)} disabled={steps.length === 1}><Trash2 class="size-4" /></Button>
    </div>
    <label class="flex items-center gap-2 text-sm"><Switch.Root checked={step.usePrevious} onCheckedChange={(value) => step.usePrevious = value} />Use previous output as input</label>
    <div class="grid gap-3 sm:grid-cols-2">
      {#each visibleFields(step) as field}
        {#if field.type === "boolean"}
          <label class="flex items-center gap-2 self-end py-2 text-sm"><Switch.Root checked={Boolean(fieldValue(step, field))} onCheckedChange={(value) => setField(step, field, value)} />{fieldLabel(step, field)}</label>
        {:else if field.type === "resource"}
          <StudioResourcePicker id={`${idPrefix}-${index}-${field.key}`} label={fieldLabel(step, field)} value={String(fieldValue(step, field))} {resources} types={field.resourceTypes ?? []} placeholder={field.placeholder ?? "Search or enter a path"} onValueChange={(value) => setField(step, field, value)} />
        {:else if field.type === "resourceList" || field.type === "scaledResourceList"}
          <div class="space-y-1"><Label.Root for={`${idPrefix}-${index}-${field.key}`}>{field.label}</Label.Root><Input id={`${idPrefix}-${index}-${field.key}`} value={String(fieldValue(step, field))} placeholder="Comma-separated paths" oninput={(event) => setField(step, field, event.currentTarget.value)} /><NativeSelect.Root aria-label={`Add ${field.label}`} value="" onchange={(event) => { addListResource(step, field, event.currentTarget.value); event.currentTarget.value = ""; }}><option value="">Add from catalog…</option>{#each resources.filter((resource) => resource.exists && (!field.resourceTypes?.length || field.resourceTypes.includes(resource.type))) as resource (resource.path)}<option value={resource.path}>{resource.path}</option>{/each}</NativeSelect.Root></div>
        {:else}
          <div class="space-y-1"><Label.Root>{fieldLabel(step, field)}</Label.Root>
            {#if field.options}<NativeSelect.Root value={String(fieldValue(step, field))} onchange={(event) => setField(step, field, event.currentTarget.value)}>{#each field.options as option}<option value={option}>{option}</option>{/each}</NativeSelect.Root>
            {:else}<Input type={field.type === "number" ? "number" : "text"} placeholder={field.placeholder} value={fieldValue(step, field) as string | number} oninput={(event) => setField(step, field, event.currentTarget.value)} />{/if}
            {#if fieldHint(step.operation, field)}<p class="text-muted-foreground text-xs leading-snug">{fieldHint(step.operation, field)}</p>{/if}
          </div>
        {/if}
      {/each}
    </div>
    <details><summary class="text-muted-foreground cursor-pointer text-xs">Advanced request JSON</summary><Textarea class="mt-2 min-h-32 font-mono text-xs" bind:value={step.requestText} spellcheck="false" /></details>
    <details><summary class="text-muted-foreground cursor-pointer text-xs">Variants and gate</summary><div class="mt-2 space-y-2"><Label.Root>Variants (optional, maximum 8)</Label.Root><Textarea class="min-h-28 font-mono text-xs" bind:value={step.variantsText} placeholder={variantsPlaceholder} /><label class="flex items-center gap-2 text-sm"><Switch.Root checked={step.continueOnFailure} onCheckedChange={(value) => step.continueOnFailure = value} />Continue on variant failure</label>{#if step.operation === "evaluate"}<div class="grid gap-2 sm:grid-cols-3"><Input bind:value={step.gateMetric} placeholder="Metric, e.g. perplexity" /><Input bind:value={step.gateMin} type="number" placeholder="Minimum" /><Input bind:value={step.gateMax} type="number" placeholder="Maximum" /></div>{/if}</div></details>
  </div>
{/each}
<Button size="sm" variant="outline" onclick={() => steps = [...steps, draft("evaluate", steps.length > 0)]}><Plus class="size-4" />Add step</Button>
