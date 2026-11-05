<script lang="ts">
  // Picks the tests (items) of an Intelligence run, by category or one by one.
  import { Checkbox } from "$lib/components/ui/checkbox/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { ChevronRight } from "@lucide/svelte";
  import {
    filterItems,
    groupByCategory,
    groupState,
    toggleGroup,
    type SuiteCategory,
    type SuiteItem,
  } from "$lib/intelligenceSelection";

  let {
    items,
    categories,
    allTests = $bindable(true),
    selected = $bindable(new Set<string>()),
    disabled = false,
  }: {
    /** Items of the chosen profile. */
    items: SuiteItem[];
    categories: SuiteCategory[];
    allTests?: boolean;
    selected?: Set<string>;
    disabled?: boolean;
  } = $props();

  let query = $state("");
  let expanded = $state<Set<string>>(new Set());
  const groups = $derived(groupByCategory(filterItems(items, query), categories));
  const selectedCount = $derived(items.filter((i) => selected.has(i.id)).length);

  function toggleItem(id: string, on: boolean): void {
    const next = new Set(selected);
    if (on) next.add(id);
    else next.delete(id);
    selected = next;
  }

  function toggleExpanded(id: string): void {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }
</script>

<div class="space-y-2">
  <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
    <span class="text-label text-muted-foreground font-semibold uppercase">Tests</span>
    <label class="flex items-center gap-2 text-sm">
      <Checkbox checked={allTests} {disabled} onCheckedChange={(c) => (allTests = c === true)} />
      All in profile ({items.length})
    </label>
    {#if !allTests}
      <span class="text-muted-foreground text-sm tabular-nums">{selectedCount} selected</span>
      <div class="ml-auto flex gap-1">
        <Button variant="ghost" size="xs" {disabled} onclick={() => (selected = new Set(items.map((i) => i.id)))}>Select all</Button>
        <Button variant="ghost" size="xs" {disabled} onclick={() => (selected = new Set())}>Clear</Button>
      </div>
    {/if}
  </div>

  {#if !allTests}
    <Input bind:value={query} placeholder="Filter tests" aria-label="Filter tests" class="h-8 max-w-sm" {disabled} />
    <ul class="max-h-80 divide-y overflow-auto rounded-lg border">
      {#each groups as group (group.id)}
        {@const state = groupState(group, selected)}
        {@const open = expanded.has(group.id) || query.trim() !== ""}
        <li>
          <div class="hover:bg-muted/50 flex items-center gap-2 px-3 py-2 text-sm">
            <Checkbox
              checked={state === "all"}
              indeterminate={state === "some"}
              {disabled}
              aria-label="Select {group.name}"
              onCheckedChange={() => (selected = toggleGroup(group, selected))}
            />
            <button type="button" class="flex min-w-0 flex-1 items-center gap-1.5 text-left" onclick={() => toggleExpanded(group.id)} aria-expanded={open}>
              <ChevronRight class={["text-muted-foreground size-4 shrink-0 transition-transform", open && "rotate-90"]} />
              <span class="truncate font-medium">{group.name}</span>
              <span class="text-muted-foreground ml-auto text-xs tabular-nums">
                {group.items.filter((i) => selected.has(i.id)).length}/{group.items.length}
              </span>
            </button>
          </div>
          {#if open}
            <ul class="bg-(--panel-header-bg) divide-y border-t">
              {#each group.items as item (item.id)}
                <li>
                  <label class="hover:bg-muted/50 flex items-center gap-2 py-1.5 pr-3 pl-10 text-sm">
                    <Checkbox checked={selected.has(item.id)} {disabled} onCheckedChange={(c) => toggleItem(item.id, c === true)} />
                    <span class="min-w-0 flex-1 truncate">{item.title}</span>
                    <span class="text-muted-foreground hidden truncate font-mono text-xs sm:inline">{item.id}</span>
                  </label>
                </li>
              {/each}
            </ul>
          {/if}
        </li>
      {:else}
        <li class="text-muted-foreground px-3 py-6 text-center text-sm">{query.trim() ? "No matching tests" : "No tests in this profile"}</li>
      {/each}
    </ul>
  {/if}
</div>
