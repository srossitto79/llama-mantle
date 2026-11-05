<script lang="ts">
  import * as Card from "$lib/components/ui/card/index.js";

  interface Ranked { label: string; pct: number; color: string; time?: string }
  // spread is the gap (percentage points) between the best and worst compared
  // model in this category -- zero once every model is tied, which is the
  // common case once a category is saturated. tiedAtTop counts how many models
  // share the top score, regardless of how the rest are spread out.
  interface CategoryBest { name: string; ranked: Ranked[]; spread: number; tiedAtTop: number; modelCount: number }
  interface Props { categories: CategoryBest[] }
  let { categories }: Props = $props();

  // A podium built from a tie is noise, not a result -- the caller already
  // sorts by spread, but the split into "has something to show" vs "saturated"
  // happens here since it is this component's rendering decision.
  let discriminating = $derived(categories.filter(c => c.spread > 0));
  let saturated = $derived(categories.filter(c => c.spread === 0));
</script>

<Card.Root class="gap-0 py-0">
  <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5">
    <h2 class="text-heading">Best at</h2>
  </div>
  <Card.Content class="space-y-4 p-4">
    <div class="grid gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3">
      {#each discriminating as category (category.name)}
        <div>
          <div class="mb-2 flex items-center justify-between gap-2">
            <h3 class="text-label text-muted-foreground truncate font-semibold uppercase" title={category.name}>{category.name}</h3>
            <span class="text-muted-foreground shrink-0 text-xs tabular-nums" title="Gap between the best and worst compared model">spread {category.spread}</span>
          </div>
          {#if category.tiedAtTop >= 3}
            <p class="text-muted-foreground text-xs">{category.tiedAtTop} tied at {category.ranked[0].pct}%</p>
          {:else}
            <div class="space-y-1.5">
              {#each category.ranked as entry, i (entry.label)}
                <div class="flex items-center gap-2 text-xs">
                  <span class="flex size-4 shrink-0 items-center justify-center rounded-full text-[0.625rem] font-semibold tabular-nums {i === 0 ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}">{i + 1}</span>
                  <span class="size-2 shrink-0 rounded-full" style="background:{entry.color}"></span>
                  <span class="truncate" title={entry.label}>{entry.label}</span>
                  <span class="text-muted-foreground ml-auto shrink-0 tabular-nums">{entry.pct}%{#if entry.time} · {entry.time}{/if}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
    </div>
    {#if saturated.length}
      <details class="text-sm">
        <summary class="text-muted-foreground cursor-pointer">Saturated ({saturated.length})</summary>
        <div class="text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs">
          {#each saturated as category (category.name)}<span>{category.name}</span>{/each}
        </div>
      </details>
    {/if}
  </Card.Content>
</Card.Root>
