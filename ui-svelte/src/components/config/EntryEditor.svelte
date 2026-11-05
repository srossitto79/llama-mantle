<script lang="ts">
  // Editor for one group, profile or peer entry. Known fields get their own
  // control; the rest are edited as YAML. All edits go to the config draft.
  import { configDraft, resolveId } from "$lib/config/draft";
  import { fromYAML, stagedValue, type FieldMeta } from "$lib/config/fields";
  import type { EntrySection, SourceEntry } from "$lib/config/configApi";
  import ConfigField from "./ConfigField.svelte";
  import Tag from "../Tag.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import { Pencil, Trash2, Undo2, Check, X, Plus, Copy } from "@lucide/svelte";

  let {
    section,
    entry,
    ids,
    fields = [],
    defaults = {},
    knownIds,
    onduplicate,
  }: {
    section: Exclude<EntrySection, "models">;
    onduplicate?: () => void;
    entry: SourceEntry;
    /** Ids in the section, for duplicate checks. */
    ids: string[];
    /** Known fields, shown first, in this order. */
    fields?: FieldMeta[];
    defaults?: Record<string, unknown>;
    knownIds?: string[];
  } = $props();

  const currentId = $derived(resolveId($configDraft, section, entry.id));
  const deleted = $derived(currentId === "");
  const isNew = $derived($configDraft.some((it) => it.change.op === "add" && it.change.section === section && it.change.id === currentId));

  let renaming = $state(false);
  let newId = $state("");
  const renameError = $derived(
    !renaming ? null : newId.trim() === "" ? "Required" : newId !== entry.id && ids.includes(newId) ? `${newId} exists` : null,
  );

  // Fields written in the entry (or staged) that are not in the known list.
  const known = $derived(new Set(fields.map((f) => f.name)));
  const extra = $derived.by(() => {
    const names = [...entry.order];
    for (const { change: c } of $configDraft) {
      if (c.section !== section || c.id !== currentId) continue;
      if (c.op === "set" && c.path?.length === 1 && !names.includes(c.path[0])) names.push(c.path[0]);
      if (c.op === "add" && c.value && typeof c.value === "object") for (const k of Object.keys(c.value)) if (!names.includes(k)) names.push(k);
    }
    return names.filter((n) => !known.has(n)).map((name): FieldMeta => ({ name, label: name, kind: "yaml" }));
  });

  let addingField = $state(false);
  let fieldName = $state("");
  let fieldValue = $state("");
  let fieldError = $state<string | null>(null);

  function addField(): void {
    const name = fieldName.trim();
    if (!name) return void (fieldError = "Name required");
    if (entry.fields[name] || stagedValue($configDraft, section, currentId, [name]).staged) return void (fieldError = `${name} exists`);
    const r = fromYAML(fieldValue);
    if (!r.ok) return void (fieldError = r.error);
    configDraft.set(section, currentId, [name], r.value ?? "", undefined);
    addingField = false;
    fieldName = fieldValue = "";
    fieldError = null;
  }

  function applyRename(): void {
    if (renameError) return;
    configDraft.rename(section, currentId, newId.trim());
    renaming = false;
  }

  function undoDelete(): void {
    const item = $configDraft.find((it) => it.change.op === "delete" && it.change.section === section && it.change.id === entry.id);
    if (item) configDraft.discard(item.key);
  }
</script>

<Card.Root class="min-h-0 gap-0 py-0">
  <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-5 py-3">
    <div class="min-w-0 flex-1">
      {#if renaming}
        <div class="flex items-center gap-2">
          <Input bind:value={newId} aria-label="Id" aria-invalid={!!renameError} class="h-8 max-w-sm font-mono text-[0.8125rem]"
            onkeydown={(e) => { if (e.key === "Enter") applyRename(); if (e.key === "Escape") renaming = false; }} />
          <Button size="icon-sm" variant="outline" aria-label="Rename" disabled={!!renameError} onclick={applyRename}><Check /></Button>
          <Button size="icon-sm" variant="ghost" aria-label="Cancel" onclick={() => (renaming = false)}><X /></Button>
        </div>
        {#if renameError}<p class="text-destructive mt-1 text-xs">{renameError}</p>{/if}
      {:else}
        <h2 class="truncate font-mono">{currentId || entry.id}</h2>
        {#if currentId && currentId !== entry.id}<p class="text-muted-foreground font-mono text-xs">was {entry.id}</p>{/if}
      {/if}
    </div>
    {#if isNew}<Tag>New</Tag>{/if}
    {#if !deleted && !renaming}
      <Button variant="ghost" size="sm" onclick={() => { newId = currentId; renaming = true; }}><Pencil />Rename</Button>
      {#if onduplicate}<Button variant="ghost" size="sm" onclick={onduplicate}><Copy />Duplicate</Button>{/if}
      <Button variant="ghost" size="sm" class="text-destructive" onclick={() => configDraft.remove(section, currentId)}><Trash2 />Delete</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 overflow-y-auto px-5 pb-4">
    {#if deleted}
      <Alert.Root class="mt-4">
        <Trash2 />
        <Alert.Title>Staged for deletion</Alert.Title>
        <Alert.Description><Button variant="outline" size="xs" class="mt-1" onclick={undoDelete}><Undo2 />Undo</Button></Alert.Description>
      </Alert.Root>
    {:else}
      <div class="divide-y">
        {#each fields as meta (meta.name)}
          <ConfigField {section} id={currentId} {meta} path={[meta.name]} source={entry.fields[meta.name]} defaultValue={defaults[meta.name]} {knownIds} />
        {/each}
        {#each extra as meta (meta.name)}
          <ConfigField {section} id={currentId} {meta} path={[meta.name]} source={entry.fields[meta.name]} />
        {/each}
      </div>
      {#if addingField}
        <div class="mt-3 grid gap-2 rounded-(--panel-radius) border p-3">
          <Input bind:value={fieldName} placeholder="Field name" aria-label="Field name" class="h-8 max-w-xs font-mono text-[0.8125rem]" />
          <Textarea bind:value={fieldValue} placeholder="Value (YAML)" aria-label="Field value" rows={3} class="font-mono text-[0.8125rem]" />
          {#if fieldError}<p class="text-destructive text-xs">{fieldError}</p>{/if}
          <div class="flex gap-2">
            <Button size="sm" onclick={addField}>Add</Button>
            <Button size="sm" variant="ghost" onclick={() => { addingField = false; fieldError = null; }}>Cancel</Button>
          </div>
        </div>
      {:else}
        <Button variant="outline" size="xs" class="mt-3" onclick={() => (addingField = true)}><Plus />Add field</Button>
      {/if}
    {/if}
  </div>
</Card.Root>
