<script lang="ts">
  // Global macros: name -> scalar value. Edits go to the config draft.
  import { configDraft } from "$lib/config/draft";
  import { fromYAML } from "$lib/config/fields";
  import type { SourceDocument } from "$lib/config/configApi";
  import ConfigField from "./ConfigField.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Trash2, Undo2, Plus } from "@lucide/svelte";

  let { source }: { source: SourceDocument } = $props();

  const NAME = /^[a-zA-Z0-9_-]{1,63}$/;
  const RESERVED = new Set(["PORT", "MODEL_ID"]);

  const rows = $derived.by(() => {
    const out = source.macros.map((m) => ({ name: m.name, field: { value: m.value, redacted: m.redacted, line: m.line } }));
    for (const { change: c } of $configDraft) {
      if (c.section === "macros" && c.op === "add") out.push({ name: c.id, field: { value: undefined, redacted: false, line: 0 } });
    }
    return out;
  });

  let newName = $state("");
  let newValue = $state("");
  const nameError = $derived.by(() => {
    const n = newName.trim();
    if (!n) return null;
    if (!NAME.test(n)) return "Letters, digits, - and _ only";
    if (RESERVED.has(n)) return `${n} is reserved`;
    if (rows.some((r) => r.name === n)) return `${n} exists`;
    return null;
  });

  function add(): void {
    const n = newName.trim();
    if (!n || nameError) return;
    const r = fromYAML(newValue);
    configDraft.add("macros", n, r.ok && r.value !== undefined ? r.value : newValue);
    newName = newValue = "";
  }

  function deleteKey(name: string): string | undefined {
    return $configDraft.find((it) => it.change.section === "macros" && it.change.op === "delete" && it.change.id === name)?.key;
  }
</script>

<Card.Root class="gap-0 py-0">
  <div class="divide-y px-5">
    {#each rows as row (row.name)}
      {@const delKey = deleteKey(row.name)}
      <div class="flex items-start gap-2">
        <div class="min-w-0 flex-1">
          {#if delKey}
            <div class="text-muted-foreground flex items-center gap-2 py-3 text-sm">
              <span class="font-mono line-through">{row.name}</span>
              <Button variant="ghost" size="xs" onclick={() => configDraft.discard(delKey)}><Undo2 />Undo</Button>
            </div>
          {:else}
            <ConfigField section="macros" id={row.name} meta={{ name: row.name, label: row.name, kind: "scalar" }} path={[]} source={row.field.line ? row.field : undefined} />
          {/if}
        </div>
        {#if !delKey}
          <Button variant="ghost" size="icon-sm" class="mt-3" aria-label="Delete {row.name}" onclick={() => configDraft.remove("macros", row.name)}><Trash2 /></Button>
        {/if}
      </div>
    {/each}
  </div>
  <div class="bg-(--panel-header-bg) grid gap-2 border-t px-5 py-3 sm:grid-cols-[11rem_1fr_auto] sm:gap-4">
    <Input bind:value={newName} placeholder="Name" aria-label="Macro name" aria-invalid={!!nameError} class="h-8 font-mono text-[0.8125rem]" />
    <Input bind:value={newValue} placeholder="Value" aria-label="Macro value" class="h-8 font-mono text-[0.8125rem]"
      onkeydown={(e) => e.key === "Enter" && add()} />
    <Button variant="outline" size="sm" disabled={!newName.trim() || !!nameError} onclick={add}><Plus />Add macro</Button>
    {#if nameError}<p class="text-destructive text-xs sm:col-span-3">{nameError}</p>{/if}
  </div>
</Card.Root>
