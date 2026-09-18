<script lang="ts">
  import * as Card from "$lib/components/ui/card/index.js";
  import { RANK_TIERS } from "$lib/intelligenceColors";

  interface Ranked { label: string; pct: number; color: string; time?: string }
  interface CategoryBest { name: string; ranked: Ranked[] }
  interface Props { categories: CategoryBest[] }
  let { categories }: Props = $props();
</script>

<Card.Root class="py-0">
  <Card.Content class="space-y-3 p-4">
    <h3 class="text-sm font-medium">Best at</h3>
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each categories as category (category.name)}
        <div class="rounded-lg border p-3">
          <p class="mb-2 truncate text-xs font-medium" title={category.name}>{category.name}</p>
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
        </div>
      {/each}
    </div>
  </Card.Content>
</Card.Root>
