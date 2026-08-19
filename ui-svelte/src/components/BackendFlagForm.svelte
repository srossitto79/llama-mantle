<script lang="ts">
  import * as Label from "$lib/components/ui/label/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import StudioResourcePicker from "./StudioResourcePicker.svelte";
  import { getBackendSchema } from "../lib/mantleApi";
  import type { BackendSchema, FlagSpec, StudioResource } from "../lib/types";

  // Canonical env names of path flags that get a StudioResourcePicker
  // (filtered to local gguf models) instead of a plain text input.
  const RESOURCE_PATH_ENVS = new Set(["LLAMA_ARG_MODEL", "LLAMA_ARG_MMPROJ", "LLAMA_ARG_SPEC_DRAFT_MODEL"]);

  let { backendName, argv = $bindable([]), resources = [] }: { backendName: string; argv?: string[]; resources?: StudioResource[] } = $props();

  let schema = $state<BackendSchema | null>(null);
  let loading = $state(false);
  let error = $state("");

  $effect(() => {
    const name = backendName;
    if (!name) {
      schema = null;
      return;
    }
    loading = true;
    error = "";
    getBackendSchema(name)
      .then((s) => (schema = s))
      .catch((e) => {
        error = e instanceof Error ? e.message : String(e);
        schema = null;
      })
      .finally(() => (loading = false));
  });

  let flagByName = $derived.by(() => {
    const map = new Map<string, FlagSpec>();
    for (const flag of schema?.flags ?? []) {
      for (const name of flag.names) map.set(name, flag);
    }
    return map;
  });

  let sections = $derived.by(() => {
    const seen: string[] = [];
    for (const flag of schema?.flags ?? []) {
      if (!seen.includes(flag.section)) seen.push(flag.section);
    }
    return seen;
  });

  // Parse the current argv (skipping argv[0], the binary itself) against the
  // known flag schema. Any token that doesn't match a known flag name is
  // left in `extra` rather than dropped, so unrecognized/fork-specific flags
  // are never silently discarded.
  let parsed = $derived.by(() => {
    const values = new Map<string, string | true>();
    const extra: string[] = [];
    const args = argv.slice(1);
    let i = 0;
    while (i < args.length) {
      const token = args[i];
      const flag = flagByName.get(token);
      if (!flag) {
        extra.push(token);
        i++;
        continue;
      }
      const canonical = flag.names[0];
      if (flag.type === "boolean") {
        values.set(canonical, true);
        i++;
      } else {
        values.set(canonical, args[i + 1] ?? "");
        i += 2;
      }
    }
    return { values, extra };
  });

  function fieldValue(flag: FlagSpec): string | boolean {
    const v = parsed.values.get(flag.names[0]);
    if (flag.type === "boolean") return v === true;
    return typeof v === "string" ? v : "";
  }

  function setField(flag: FlagSpec, value: string | boolean) {
    const canonical = flag.names[0];
    const next = new Map(parsed.values);
    if (flag.type === "boolean") {
      if (value) next.set(canonical, true);
      else next.delete(canonical);
    } else if (value === "") {
      next.delete(canonical);
    } else {
      next.set(canonical, value as string);
    }
    rebuildArgv(next, parsed.extra);
  }

  function setExtraText(text: string) {
    rebuildArgv(parsed.values, text.trim() === "" ? [] : text.trim().split(/\s+/));
  }

  function rebuildArgv(values: Map<string, string | true>, extra: string[]) {
    const next: string[] = argv.length > 0 ? [argv[0]] : [];
    for (const [name, value] of values) {
      next.push(name);
      if (typeof value === "string") next.push(value);
    }
    next.push(...extra);
    argv = next;
  }

  function sectionFlags(section: string): FlagSpec[] {
    return (schema?.flags ?? []).filter((f) => f.section === section);
  }

  function isActive(flag: FlagSpec): boolean {
    return parsed.values.has(flag.names[0]);
  }
</script>

{#if loading}
  <p class="text-muted-foreground text-xs">Loading backend flags…</p>
{:else if error}
  <p class="text-destructive text-xs">Failed to load flags for "{backendName}": {error}</p>
{:else if schema}
  <div class="space-y-3">
    {#each sections as section (section)}
      {@const flags = sectionFlags(section)}
      {@const active = flags.filter(isActive)}
      {@const inactive = flags.filter((f) => !isActive(f))}
      <div class="rounded-md border p-3">
        <h4 class="text-muted-foreground mb-2 text-xs font-semibold uppercase">{section}</h4>
        {#if active.length > 0}
          <div class="grid gap-3 sm:grid-cols-2">
            {#each active as flag (flag.names[0])}
              {@render field(flag)}
            {/each}
          </div>
        {/if}
        {#if inactive.length > 0}
          <details class="mt-2">
            <summary class="text-muted-foreground cursor-pointer text-xs">+{inactive.length} more {section} options</summary>
            <div class="mt-2 grid gap-3 sm:grid-cols-2">
              {#each inactive as flag (flag.names[0])}
                {@render field(flag)}
              {/each}
            </div>
          </details>
        {/if}
      </div>
    {/each}

    <details>
      <summary class="text-muted-foreground cursor-pointer text-xs">Extra arguments{parsed.extra.length > 0 ? ` (${parsed.extra.length})` : ""}</summary>
      <textarea
        class="border-input bg-background mt-2 min-h-16 w-full rounded-md border p-3 font-mono text-xs"
        value={parsed.extra.join(" ")}
        oninput={(e) => setExtraText(e.currentTarget.value)}
        spellcheck="false"
      ></textarea>
      <p class="text-muted-foreground mt-1 text-xs leading-snug">
        Flags this backend's schema doesn't recognize (custom fork flags, or flags not yet parsed). Edited as raw text.
      </p>
    </details>
  </div>
{/if}

{#snippet field(flag: FlagSpec)}
  {#if flag.type === "boolean"}
    <label class="flex items-center gap-2 self-end py-2 text-sm">
      <Switch.Root checked={fieldValue(flag) as boolean} onCheckedChange={(v) => setField(flag, v)} />
      {flag.names[0]}
    </label>
  {:else if flag.type === "path" && flag.canonicalEnv && RESOURCE_PATH_ENVS.has(flag.canonicalEnv)}
    <StudioResourcePicker
      id={`flag-${flag.names[0]}`}
      label={flag.names[0]}
      value={fieldValue(flag) as string}
      {resources}
      types={["model"]}
      placeholder={flag.default ? `default: ${flag.default}` : "Search local models"}
      onValueChange={(v) => setField(flag, v)}
    />
  {:else}
    <div class="space-y-1">
      <Label.Root>{flag.names[0]}</Label.Root>
      {#if flag.type === "enum" && flag.choices}
        <select
          class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
          value={fieldValue(flag)}
          onchange={(e) => setField(flag, e.currentTarget.value)}
        >
          <option value="">(unset{flag.default ? `, default ${flag.default}` : ""})</option>
          {#each flag.choices as choice (choice)}<option value={choice}>{choice}</option>{/each}
        </select>
      {:else}
        <Input
          type={flag.type === "number" ? "number" : "text"}
          value={fieldValue(flag) as string}
          placeholder={flag.default ? `default: ${flag.default}` : ""}
          oninput={(e) => setField(flag, e.currentTarget.value)}
        />
      {/if}
      {#if flag.help}<p class="text-muted-foreground text-xs leading-snug">{flag.help}</p>{/if}
    </div>
  {/if}
{/snippet}
