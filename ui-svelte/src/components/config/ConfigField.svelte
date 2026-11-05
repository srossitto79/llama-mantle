<script lang="ts">
  // One editable config field. Edits are staged in the config draft, never
  // written directly. Aliased fields stay read-only until detached; secrets
  // are never shown.
  import { configDraft } from "$lib/config/draft";
  import { stagedValue, toYAML, fromYAML, linesToList, type FieldMeta } from "$lib/config/fields";
  import type { ChangeSection, SourceField } from "$lib/config/configApi";
  import { REDACTED } from "$lib/config/configApi";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import { Checkbox } from "$lib/components/ui/checkbox/index.js";
  import * as NativeSelect from "$lib/components/ui/native-select/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import SourceValue from "./SourceValue.svelte";
  import ParamsByIdEditor from "./ParamsByIdEditor.svelte";
  import { Link2, Unlink, Undo2, Lock } from "@lucide/svelte";

  let {
    section,
    id,
    meta,
    path,
    source,
    macros = {},
    defaultValue,
    expanded,
    knownIds,
  }: {
    section: ChangeSection;
    /** Entry id after staged renames. */
    id: string;
    meta: FieldMeta;
    path: string[];
    /** The field as written; undefined when absent. */
    source?: SourceField;
    macros?: Record<string, unknown>;
    /** Value the loader uses when the field is absent. */
    defaultValue?: unknown;
    /** Effective (macro-expanded) value, shown on request. */
    expanded?: unknown;
    /** Valid ids for "members" fields. */
    knownIds?: string[];
  } = $props();

  const fieldId = $derived(`f-${section}-${id}-${path.join("-")}`.replace(/[^\w-]/g, "_"));
  const original = $derived(source?.value);
  const staged = $derived(stagedValue($configDraft, section, id, path));
  const value = $derived(staged.staged ? (staged.removed ? undefined : staged.value) : original);
  const aliasName = $derived(source?.alias);
  let detached = $state(false);
  const locked = $derived(!!aliasName && !detached && !staged.staged);
  let replacingSecret = $state(false);
  let showExpanded = $state(false);
  let parseError = $state<string | null>(null);

  function commit(next: unknown): void {
    parseError = null;
    const detach = !!aliasName;
    if (next === undefined || (original === undefined && defaultValue !== undefined && next === defaultValue)) {
      if (original === undefined) configDraft.set(section, id, path, undefined, undefined);
      else configDraft.unset(section, id, path);
      return;
    }
    configDraft.set(section, id, path, next, original, detach);
  }

  function revert(): void {
    detached = false;
    replacingSecret = false;
    parseError = null;
    configDraft.set(section, id, path, original, original);
  }

  function textValue(v: unknown): string {
    if (v === undefined || v === null) return "";
    return typeof v === "string" ? v : String(v);
  }

  function onText(e: Event): void {
    const t = (e.currentTarget as HTMLInputElement).value;
    commit(t === "" ? undefined : t);
  }

  function onNumber(e: Event): void {
    const t = (e.currentTarget as HTMLInputElement).value.trim();
    if (t === "") return commit(undefined);
    const n = Number(t);
    if (!Number.isFinite(n)) {
      parseError = "Not a number";
      return;
    }
    commit(n);
  }

  function onList(e: Event): void {
    const list = linesToList((e.currentTarget as HTMLTextAreaElement).value);
    commit(list.length ? list : undefined);
  }

  function onYAML(e: Event): void {
    const r = fromYAML((e.currentTarget as HTMLTextAreaElement | HTMLInputElement).value);
    if (!r.ok) {
      parseError = r.error;
      return;
    }
    commit(r.value);
  }

  const unknownMembers = $derived(
    meta.kind === "members" && Array.isArray(value) && knownIds ? (value as string[]).filter((m) => !knownIds.includes(m)) : [],
  );
  const isSecretSet = $derived(meta.kind === "secret" && value === REDACTED);
</script>

<div class="grid gap-1.5 py-3 sm:grid-cols-[11rem_1fr] sm:gap-4" data-field={path.join(".")}>
  <div class="flex items-start gap-1.5 sm:pt-1.5">
    <label for={fieldId} class="text-sm font-medium">{meta.label}</label>
    {#if staged.staged}
      <span class="bg-primary mt-1.5 size-1.5 shrink-0 rounded-full" title="Modified"></span>
    {/if}
  </div>

  <div class="grid min-w-0 gap-1.5">
    {#if aliasName && !staged.staged}
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-muted-foreground inline-flex items-center gap-1 text-xs">
          <Link2 class="size-3.5" />Inherited from <code class="text-foreground font-mono">{aliasName}</code>
        </span>
        {#if !detached}
          <Button variant="ghost" size="xs" onclick={() => (detached = true)}><Unlink />Detach</Button>
        {/if}
      </div>
    {:else if source?.inheritedFrom && !staged.staged}
      <span class="text-muted-foreground inline-flex items-center gap-1 text-xs">
        <Link2 class="size-3.5" />Inherited from <code class="text-foreground font-mono">{source.inheritedFrom}</code>
      </span>
    {/if}

    {#if meta.kind === "text"}
      <Input id={fieldId} value={textValue(value)} placeholder={meta.placeholder} disabled={locked} onchange={onText} class="font-mono text-[0.8125rem]" />
    {:else if meta.kind === "multiline"}
      <Textarea
        id={fieldId}
        value={textValue(value)}
        disabled={locked}
        onchange={onText}
        rows={Math.min(12, Math.max(3, textValue(value).split("\n").length + 1))}
        class="font-mono text-[0.8125rem] leading-6"
        spellcheck={false}
      />
      {#if expanded !== undefined && expanded !== value}
        <button type="button" class="text-muted-foreground hover:text-foreground w-fit text-xs" onclick={() => (showExpanded = !showExpanded)}>
          {showExpanded ? "Hide expanded" : "Show expanded"}
        </button>
        {#if showExpanded}
          <div class="bg-muted rounded-md px-2.5 py-2"><SourceValue value={expanded} {macros} multiline /></div>
        {/if}
      {/if}
    {:else if meta.kind === "scalar"}
      <Input id={fieldId} value={toYAML(value)} disabled={locked} onchange={onYAML} class="font-mono text-[0.8125rem]" aria-invalid={!!parseError} />
    {:else if meta.kind === "number"}
      <Input id={fieldId} type="number" value={textValue(value)} placeholder={defaultValue !== undefined ? String(defaultValue) : undefined} disabled={locked} onchange={onNumber} class="w-40" />
    {:else if meta.kind === "bool"}
      <Checkbox id={fieldId} checked={(value ?? defaultValue ?? false) === true} disabled={locked} onCheckedChange={(c) => commit(c === true)} />
    {:else if meta.kind === "tristate"}
      <NativeSelect.Root
        id={fieldId}
        class="w-40"
        value={value === true ? "yes" : value === false ? "no" : ""}
        disabled={locked}
        onchange={(e) => {
          const v = (e.currentTarget as HTMLSelectElement).value;
          commit(v === "yes" ? true : v === "no" ? false : undefined);
        }}
      >
        <option value="">Default</option>
        <option value="yes">Yes</option>
        <option value="no">No</option>
      </NativeSelect.Root>
    {:else if meta.kind === "list" || meta.kind === "members"}
      <Textarea
        id={fieldId}
        value={Array.isArray(value) ? value.join("\n") : ""}
        placeholder={meta.placeholder ?? "One per line"}
        disabled={locked}
        onchange={onList}
        rows={Math.min(10, Math.max(2, (Array.isArray(value) ? value.length : 0) + 1))}
        class="font-mono text-[0.8125rem]"
        spellcheck={false}
      />
      {#if unknownMembers.length > 0}
        <p class="text-destructive text-xs">Unknown model: {unknownMembers.join(", ")}</p>
      {/if}
    {:else if meta.kind === "secret"}
      {#if isSecretSet && !replacingSecret}
        <div class="flex items-center gap-2">
          <span class="text-muted-foreground inline-flex items-center gap-1 text-sm"><Lock class="size-3.5" />Set</span>
          <Button variant="outline" size="xs" onclick={() => (replacingSecret = true)}>Replace</Button>
          <Button variant="ghost" size="xs" onclick={() => commit(undefined)}>Remove</Button>
        </div>
      {:else}
        <Input
          id={fieldId}
          value={isSecretSet ? "" : textValue(value)}
          placeholder={meta.placeholder}
          autocomplete="off"
          spellcheck={false}
          onchange={onText}
          class="font-mono text-[0.8125rem]"
        />
      {/if}
    {:else if meta.kind === "paramsById"}
      <ParamsByIdEditor value={(value ?? {}) as Record<string, Record<string, unknown>>} disabled={locked} onchange={(v) => commit(Object.keys(v).length ? v : undefined)} />
    {:else}
      <Textarea
        id={fieldId}
        value={toYAML(value)}
        disabled={locked}
        onchange={onYAML}
        rows={Math.min(12, Math.max(2, toYAML(value).split("\n").length + 1))}
        class="font-mono text-[0.8125rem]"
        spellcheck={false}
        aria-invalid={!!parseError}
      />
    {/if}

    {#if parseError}
      <p class="text-destructive text-xs">{parseError}</p>
    {/if}
    {#if staged.staged}
      <Button variant="ghost" size="xs" class="text-muted-foreground w-fit" onclick={revert}><Undo2 />Revert</Button>
    {/if}
  </div>
</div>
