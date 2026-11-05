<script lang="ts">
  // Selectable list of entries (models, groups, ...) with filter and add.
  import { Input } from "$lib/components/ui/input/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Search, Plus } from "@lucide/svelte";
  import type { ListItem } from "$lib/config/fields";


  let {
    items,
    selected = $bindable(null),
    addLabel,
    onadd,
  }: { items: ListItem[]; selected?: string | null; addLabel?: string; onadd?: () => void } = $props();

  let query = $state("");
  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return q ? items.filter((i) => i.label.toLowerCase().includes(q)) : items;
  });
</script>

<Card.Root class="max-h-72 min-h-0 gap-0 py-0 md:max-h-none">
  <div class="bg-(--panel-header-bg) flex gap-2 border-b p-2">
    <div class="relative flex-1">
      <Search class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
      <Input bind:value={query} placeholder="Filter" class="h-8 pl-8" aria-label="Filter" />
    </div>
    {#if onadd}
      <Button variant="outline" size="icon" aria-label={addLabel ?? "Add"} title={addLabel} onclick={onadd}><Plus /></Button>
    {/if}
  </div>
  <ul class="min-h-0 flex-1 overflow-y-auto p-1">
    {#each shown as item (item.key)}
      <li>
        <button
          type="button"
          onclick={() => (selected = item.key)}
          class={[
            "flex w-full items-center gap-2 rounded-[calc(var(--radius)-2px)] px-2.5 py-1.5 text-left",
            item.key === selected ? "bg-accent text-accent-foreground" : "hover:bg-muted/60",
          ]}
        >
          {#if item.dot}<span class="size-1.5 shrink-0 rounded-full {item.dot}" aria-hidden="true"></span>{/if}
          <span class={["min-w-0 flex-1 truncate font-mono text-[0.8125rem]", item.staged === "deleted" && "text-muted-foreground line-through"]}>{item.label}</span>
          {#if item.staged === "new"}
            <span class="text-label text-primary uppercase">new</span>
          {:else if item.staged === "modified"}
            <span class="bg-primary size-1.5 shrink-0 rounded-full" title="Modified"></span>
          {/if}
        </button>
      </li>
    {:else}
      <li class="text-muted-foreground px-2.5 py-3 text-sm">{query.trim() ? "No matches" : "None"}</li>
    {/each}
  </ul>
</Card.Root>
