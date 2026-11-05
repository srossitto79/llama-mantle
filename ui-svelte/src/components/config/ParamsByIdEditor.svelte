<script lang="ts">
  // Table editor for filters.setParamsByID: request-model suffix -> params.
  // Keys keep their ${MODEL_ID} prefix as written; params are a YAML flow map.
  import { Input } from "$lib/components/ui/input/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { fromYAML } from "$lib/config/fields";
  import { stringify } from "yaml";
  import { Plus, X } from "@lucide/svelte";

  type Params = Record<string, Record<string, unknown>>;

  let {
    value,
    disabled = false,
    onchange,
  }: { value: Params; disabled?: boolean; onchange: (next: Params) => void } = $props();

  const PREFIX = "${MODEL_ID}";
  let errors = $state<Record<number, string>>({});

  const rows = $derived(Object.entries(value));

  function flow(params: Record<string, unknown>): string {
    return stringify(params, { flowCollectionPadding: false, collectionStyle: "flow", lineWidth: 0 }).trim().replace(/^\{|\}$/g, "");
  }

  function rebuild(index: number, key: string | null, paramsText: string | null): void {
    const next: Params = {};
    rows.forEach(([k, p], i) => {
      if (i !== index) {
        next[k] = p;
        return;
      }
      if (key === null && paramsText === null) return; // removed
      let params = p;
      if (paramsText !== null) {
        const r = fromYAML(`{${paramsText}}`);
        if (!r.ok || typeof r.value !== "object" || r.value === null) {
          errors = { ...errors, [i]: r.ok ? "Expected key: value pairs" : r.error };
          return;
        }
        params = r.value as Record<string, unknown>;
      }
      const { [i]: _, ...rest } = errors;
      errors = rest;
      next[key ?? k] = params;
    });
    if (Object.keys(errors).length === 0) onchange(next);
  }

  function addRow(): void {
    let n = 1;
    while (`${PREFIX}:suffix${n}` in value) n++;
    onchange({ ...value, [`${PREFIX}:suffix${n}`]: {} });
  }
</script>

<div class="grid gap-1.5">
  {#if rows.length > 0}
    <div class="text-muted-foreground grid grid-cols-[minmax(8rem,14rem)_1fr_auto] gap-2 text-xs">
      <span>Suffix</span><span>Parameters</span><span class="w-7"></span>
    </div>
  {/if}
  {#each rows as [key, params], i (key)}
    {@const prefixed = key.startsWith(PREFIX)}
    <div class="grid grid-cols-[minmax(8rem,14rem)_1fr_auto] items-start gap-2">
      <div class="flex min-w-0 items-center">
        {#if prefixed}
          <span class="text-muted-foreground bg-muted rounded-l-md border border-r-0 px-1.5 py-1 font-mono text-xs leading-[1.375rem]" title="Model id">id</span>
        {/if}
        <Input
          value={prefixed ? key.slice(PREFIX.length) : key}
          {disabled}
          aria-label="Suffix"
          class="h-8 min-w-0 font-mono text-[0.8125rem] {prefixed ? 'rounded-l-none' : ''}"
          onchange={(e) => rebuild(i, (prefixed ? PREFIX : "") + (e.currentTarget as HTMLInputElement).value, null)}
        />
      </div>
      <div class="grid gap-1">
        <Input
          value={flow(params)}
          {disabled}
          aria-label="Parameters"
          placeholder="reasoning_effort: low"
          aria-invalid={!!errors[i]}
          class="h-8 font-mono text-[0.8125rem]"
          onchange={(e) => rebuild(i, null, (e.currentTarget as HTMLInputElement).value)}
        />
        {#if errors[i]}<p class="text-destructive text-xs">{errors[i]}</p>{/if}
      </div>
      <Button variant="ghost" size="icon-sm" {disabled} aria-label="Remove {key}" onclick={() => rebuild(i, null, null)}><X /></Button>
    </div>
  {/each}
  <Button variant="outline" size="xs" class="w-fit" {disabled} onclick={addRow}><Plus />Add suffix</Button>
</div>
