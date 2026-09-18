<script lang="ts">
  import * as Card from "$lib/components/ui/card/index.js";
  import { RANK_TIERS } from "$lib/intelligenceColors";

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

<Card.Root class="py-0">
  <Card.Content class="space-y-3 p-4">
    <h3 class="text-sm font-medium">Best at</h3>
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each discriminating as category (category.name)}
        <div class="rounded-lg border p-3">
          <div class="mb-2 flex items-center justify-between gap-2">
            <p class="truncate text-xs font-medium" title={category.name}>{category.name}</p>
            <span class="text-muted-foreground shrink-0 text-[10px] tabular-nums" title="Gap between the best and worst compared model">spread {category.spread}</span>
          </div>
          {#if category.tiedAtTop >= 3}
            <p class="text-muted-foreground text-xs">{category.tiedAtTop} tied at {category.ranked[0].pct}%</p>
          {:else}
            <div class="space-y-1.5">
              {#each category.ranked as entry, i (entry.label)}
                {@const tier = RANK_TIERS[i]}
                <div class="flex items-center gap-2 text-xs">
                  <span class="flex size-4 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold text-white" style="background:{tier.color}" title={tier.label}>{i + 1}</span>
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
