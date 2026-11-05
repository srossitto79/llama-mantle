<script lang="ts" module>
  import { getBackendSchema, listBackends, listStudioResources } from "$lib/mantleApi";
  import type { BackendEntry, BackendSchema, StudioResource } from "$lib/types";

  // Shared across editors: backends and resources rarely change while editing.
  let backendsOnce: Promise<BackendEntry[]> | null = null;
  let resourcesOnce: Promise<StudioResource[]> | null = null;
  const schemas = new Map<string, Promise<BackendSchema>>();

  function backendsList(): Promise<BackendEntry[]> {
    backendsOnce ??= listBackends().catch(() => []);
    return backendsOnce;
  }
  function resourcesList(): Promise<StudioResource[]> {
    resourcesOnce ??= listStudioResources().catch(() => []);
    return resourcesOnce;
  }
  function schemaFor(name: string): Promise<BackendSchema> {
    let p = schemas.get(name);
    if (!p) {
      p = getBackendSchema(name);
      p.catch(() => schemas.delete(name));
      schemas.set(name, p);
    }
    return p;
  }
</script>

<script lang="ts">
  // Flag-by-flag view of a model's cmd, from the backend's --help schema. It
  // edits the command's words in place, so flag order, ${macro} words and
  // quoting survive; words it does not recognise stay as extra arguments.
  import {
    flagValue,
    hasCommentLines,
    joinCommand,
    parseCommand,
    quoteWord,
    setExtra,
    setFlag,
    splitCommand,
    type FlagRef,
  } from "$lib/config/cmdline";
  import type { FlagSpec } from "$lib/types";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import * as NativeSelect from "$lib/components/ui/native-select/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import StudioResourcePicker from "../StudioResourcePicker.svelte";

  // Path flags that get a picker of local GGUF models instead of a text input.
  const RESOURCE_PATH_ENVS = new Set(["LLAMA_ARG_MODEL", "LLAMA_ARG_MMPROJ", "LLAMA_ARG_SPEC_DRAFT_MODEL"]);

  let {
    cmd,
    macros = {},
    disabled = false,
    onchange,
  }: { cmd: string; macros?: Record<string, unknown>; disabled?: boolean; onchange: (cmd: string) => void } = $props();

  let backends = $state<BackendEntry[]>([]);
  let resources = $state<StudioResource[]>([]);
  let schema = $state<BackendSchema | null>(null);
  let schemaError = $state("");

  $effect(() => {
    void backendsList().then((b) => (backends = b));
    void resourcesList().then((r) => (resources = r));
  });

  const words = $derived(splitCommand(cmd));
  const multiline = $derived(cmd.includes("\n"));
  const commented = $derived(hasCommentLines(cmd));

  /** argv[0] with ${macro} references expanded, to find the backend. */
  const binary = $derived.by(() => {
    const first = words[0] ?? "";
    const expanded = first.replace(/\$\{([A-Za-z0-9_-]+)\}/g, (m, name) => (name in macros ? String(macros[name]) : m));
    return expanded.trim().split(/\s+/)[0] ?? "";
  });

  const backend = $derived(
    backends.find((b) => b.path === binary || b.name === binary || binary.endsWith("/" + b.name)) ?? null,
  );
  // Image and transcription built-ins have no llama.cpp --help schema.
  const schemaName = $derived(backend && !(backend.builtin && backend.kind !== "llm") ? backend.name : "");

  $effect(() => {
    const name = schemaName;
    schema = null;
    schemaError = "";
    if (!name) return;
    schemaFor(name)
      .then((s) => {
        if (schemaName === name) schema = s;
      })
      .catch((e) => (schemaError = e instanceof Error ? e.message : String(e)));
  });

  const refs = $derived.by(() => {
    const byName = new Map<string, FlagRef>();
    const bySpec = new Map<FlagSpec, FlagRef>();
    for (const f of schema?.flags ?? []) {
      const ref: FlagRef = { name: f.names[0], names: f.names, kind: f.type === "boolean" ? "boolean" : "value" };
      bySpec.set(f, ref);
      for (const n of f.names) byName.set(n, ref);
    }
    return { byName, bySpec };
  });
  const parsed = $derived(parseCommand(words, refs.byName));
  const sections = $derived([...new Set((schema?.flags ?? []).map((f) => f.section))]);
  const extraText = $derived(parsed.extra.map((i) => quoteWord(words[i])).join(" "));

  function commit(next: string[]): void {
    const text = joinCommand(next, multiline);
    if (text !== cmd) onchange(text);
  }

  function value(flag: FlagSpec): string | boolean {
    return flagValue(words, parsed, refs.bySpec.get(flag)!);
  }

  function set(flag: FlagSpec, v: string | boolean): void {
    commit(setFlag(words, refs.byName, refs.bySpec.get(flag)!, v));
  }

  function setBinary(path: string): void {
    commit([path, ...words.slice(1)]);
  }

  function isActive(flag: FlagSpec): boolean {
    return parsed.flags.has(flag.names[0]);
  }
</script>

{#if commented}
  <p class="text-muted-foreground text-sm">Command has comment lines. Edit it as text.</p>
{:else}
  <div class="grid gap-3">
    <div class="grid gap-1.5 sm:grid-cols-[9rem_1fr] sm:items-center sm:gap-3">
      <span class="text-sm font-medium">Backend</span>
      <NativeSelect.Root value={words[0] ?? ""} {disabled} aria-label="Backend" onchange={(e) => setBinary((e.currentTarget as HTMLSelectElement).value)}>
        {#if words[0] && !backends.some((b) => b.path === words[0])}
          <option value={words[0]}>{words[0]}{binary !== words[0] ? ` (${binary})` : ""}</option>
        {/if}
        {#each backends as b (b.path)}
          <option value={b.path}>{b.builtin ? `${b.name} · built-in` : b.name}</option>
        {/each}
      </NativeSelect.Root>
    </div>

    {#if schemaError}
      <p class="text-destructive text-xs">{schemaError}</p>
    {:else if !schemaName}
      <p class="text-muted-foreground text-sm">No flag list for this backend</p>
    {:else if !schema}
      <p class="text-muted-foreground text-sm">Loading flags…</p>
    {:else}
      {#each sections as section (section)}
        {@const flags = (schema?.flags ?? []).filter((f) => f.section === section)}
        {@const active = flags.filter(isActive)}
        {@const inactive = flags.filter((f) => !isActive(f))}
        <section class="rounded-(--panel-radius) border">
          <h4 class="text-label text-muted-foreground bg-(--panel-header-bg) border-b px-3 py-1.5 font-semibold uppercase">{section}</h4>
          <div class="grid gap-3 p-3">
            {#if active.length}
              <div class="grid gap-3 sm:grid-cols-2">
                {#each active as flag (flag.names[0])}{@render field(flag)}{/each}
              </div>
            {/if}
            {#if inactive.length}
              <details>
                <summary class="text-muted-foreground cursor-pointer text-xs">{inactive.length} more</summary>
                <div class="mt-3 grid gap-3 sm:grid-cols-2">
                  {#each inactive as flag (flag.names[0])}{@render field(flag)}{/each}
                </div>
              </details>
            {/if}
          </div>
        </section>
      {/each}
    {/if}

    <div class="grid gap-1.5">
      <span class="text-sm font-medium">Other arguments</span>
      <Textarea
        value={extraText}
        {disabled}
        rows={2}
        spellcheck={false}
        aria-label="Other arguments"
        class="font-mono text-[0.8125rem]"
        onchange={(e) => commit(setExtra(words, refs.byName, (e.currentTarget as HTMLTextAreaElement).value))}
      />
    </div>
  </div>
{/if}

{#snippet field(flag: FlagSpec)}
  {#if flag.type === "boolean"}
    <label class="flex items-center gap-2 self-end py-1.5 font-mono text-[0.8125rem]" title={flag.help}>
      <Switch checked={value(flag) === true} {disabled} onCheckedChange={(v) => set(flag, v)} />
      {flag.names[0]}
    </label>
  {:else if flag.type === "path" && flag.canonicalEnv && RESOURCE_PATH_ENVS.has(flag.canonicalEnv)}
    <StudioResourcePicker
      id={`flag-${flag.names[0]}`}
      label={flag.names[0]}
      value={value(flag) as string}
      {resources}
      types={["model"]}
      placeholder={flag.default ? `default: ${flag.default}` : "Search local models"}
      onValueChange={(v) => set(flag, v)}
    />
  {:else}
    <label class="grid gap-1" title={flag.help}>
      <span class="font-mono text-[0.8125rem]">{flag.names[0]}</span>
      {#if flag.type === "enum" && flag.choices}
        <NativeSelect.Root value={value(flag) as string} {disabled} onchange={(e) => set(flag, (e.currentTarget as HTMLSelectElement).value)}>
          <option value="">{flag.default ? `Default (${flag.default})` : "Not set"}</option>
          {#each flag.choices as choice (choice)}<option value={choice}>{choice}</option>{/each}
        </NativeSelect.Root>
      {:else}
        <Input
          type={flag.type === "number" ? "number" : "text"}
          value={value(flag) as string}
          {disabled}
          placeholder={flag.default ? `default: ${flag.default}` : ""}
          class="h-8 font-mono text-[0.8125rem]"
          onchange={(e) => set(flag, (e.currentTarget as HTMLInputElement).value)}
        />
      {/if}
    </label>
  {/if}
{/snippet}
