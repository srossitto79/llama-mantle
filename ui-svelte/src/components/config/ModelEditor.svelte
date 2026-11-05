<script lang="ts">
  // Editor for one model entry. All edits go to the config draft.
  import { configDraft, resolveId } from "$lib/config/draft";
  import { modelFieldGroups, otherFields, fieldPath, getPath, usedModelNames, validateModelId, type FieldMeta } from "$lib/config/fields";
  import type { EffectiveModel, SourceEntry, SourceField } from "$lib/config/configApi";
  import ConfigField from "./ConfigField.svelte";
  import Tag from "../Tag.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Pencil, Trash2, Undo2, Check, X, Link2 } from "@lucide/svelte";

  let {
    entry,
    models,
    macros,
    effective,
    running = false,
  }: {
    /** The entry as written (original id). Undefined fields mean absent. */
    entry: SourceEntry;
    /** All model entries, for duplicate checks. */
    models: SourceEntry[];
    macros: Record<string, unknown>;
    effective?: EffectiveModel;
    running?: boolean;
  } = $props();

  const currentId = $derived(resolveId($configDraft, "models", entry.id));
  const deleted = $derived(currentId === "");
  const isNew = $derived($configDraft.some((it) => it.change.op === "add" && it.change.section === "models" && it.change.id === currentId));
  let renaming = $state(false);
  let newId = $state("");
  const used = $derived(usedModelNames(models));
  const renameError = $derived(renaming ? validateModelId(newId, used, entry.id) : null);

  function startRename(): void {
    newId = currentId;
    renaming = true;
  }

  function applyRename(): void {
    if (renameError) return;
    configDraft.rename("models", currentId, newId.trim());
    renaming = false;
  }

  function undoDelete(): void {
    // The delete targets the id the model had at that point (after renames).
    const ids = new Set([entry.id]);
    for (const { change: c } of $configDraft) {
      if (c.section === "models" && c.op === "rename" && ids.has(c.id) && c.newId) ids.add(c.newId);
    }
    const item = $configDraft.find((it) => it.change.op === "delete" && it.change.section === "models" && ids.has(it.change.id));
    if (item) configDraft.discard(item.key);
  }

  /** The source field for a (possibly dotted) field name. */
  function sourceFor(meta: FieldMeta): SourceField | undefined {
    const [head, ...rest] = fieldPath(meta.name);
    const f = entry.fields[head];
    if (!f) return undefined;
    if (rest.length === 0) return f;
    const v = getPath(f.value, rest);
    return { ...f, value: v, alias: f.alias, inheritedFrom: f.inheritedFrom };
  }

  const others = $derived(otherFields(entry.order));
  const displayName = $derived((entry.fields.name?.value as string | undefined) || currentId);
</script>

<Card.Root class="min-h-0 gap-0 py-0">
  <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-5 py-3">
    <div class="min-w-0 flex-1">
      {#if renaming}
        <div class="flex items-center gap-2">
          <Input
            bind:value={newId}
            aria-label="Model id"
            aria-invalid={!!renameError}
            class="h-8 max-w-sm font-mono text-[0.8125rem]"
            onkeydown={(e) => {
              if (e.key === "Enter") applyRename();
              if (e.key === "Escape") renaming = false;
            }}
          />
          <Button size="icon-sm" variant="outline" aria-label="Rename" disabled={!!renameError} onclick={applyRename}><Check /></Button>
          <Button size="icon-sm" variant="ghost" aria-label="Cancel" onclick={() => (renaming = false)}><X /></Button>
        </div>
        {#if renameError}<p class="text-destructive mt-1 text-xs">{renameError}</p>{/if}
      {:else}
        <h2 class="truncate">{displayName}</h2>
        <p class="text-muted-foreground font-mono text-xs">
          {currentId || entry.id}{#if currentId && currentId !== entry.id}<span> · was {entry.id}</span>{/if}{#if !isNew} · line {entry.line}{/if}
        </p>
      {/if}
    </div>
    {#if running}<Tag class="bg-success/15 text-foreground">Running</Tag>{/if}
    {#if isNew}<Tag>New</Tag>{/if}
    {#if !deleted && !renaming}
      <Button variant="ghost" size="sm" onclick={startRename}><Pencil />Rename</Button>
      <Button variant="ghost" size="sm" class="text-destructive" onclick={() => configDraft.remove("models", currentId)}><Trash2 />Delete</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 overflow-y-auto px-5 pb-4">
    {#if deleted}
      <Alert.Root class="mt-4">
        <Trash2 />
        <Alert.Title>Staged for deletion</Alert.Title>
        <Alert.Description>
          <Button variant="outline" size="xs" class="mt-1" onclick={undoDelete}><Undo2 />Undo</Button>
        </Alert.Description>
      </Alert.Root>
    {:else}
      {#if entry.alias}
        <Alert.Root class="mt-4">
          <Link2 />
          <Alert.Title>Defined by anchor <code class="font-mono">{entry.alias}</code></Alert.Title>
        </Alert.Root>
      {/if}
      {#each modelFieldGroups as group (group.title)}
        <section class="border-b last:border-b-0">
          <h3 class="text-label text-muted-foreground pt-4 font-semibold uppercase">{group.title}</h3>
          <div class="divide-y">
            {#each group.fields as meta (meta.name)}
              <ConfigField
                section="models"
                id={currentId}
                {meta}
                path={fieldPath(meta.name)}
                source={sourceFor(meta)}
                {macros}
                expanded={meta.name === "cmd" ? effective?.cmd : undefined}
                defaultValue={meta.name === "unlisted" ? false : undefined}
              />
            {/each}
          </div>
        </section>
      {/each}
      {#if others.length > 0}
        <section>
          <h3 class="text-label text-muted-foreground pt-4 font-semibold uppercase">Other</h3>
          <div class="divide-y">
            {#each others as meta (meta.name)}
              <ConfigField section="models" id={currentId} {meta} path={[meta.name]} source={entry.fields[meta.name]} {macros} />
            {/each}
          </div>
        </section>
      {/if}
    {/if}
  </div>
</Card.Root>
