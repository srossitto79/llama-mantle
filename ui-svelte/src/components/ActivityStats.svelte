<script lang="ts">
  import type { ActivityStatsData } from "../lib/types";
  import { persistentStore } from "../stores/persistent";
  import TokenHistogram from "./TokenHistogram.svelte";
  import { ChevronDown, X } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";

  interface Props {
    stats: ActivityStatsData | null;
  }

  let { stats }: Props = $props();

  const nf = new Intl.NumberFormat();
  const histogramCollapsed = persistentStore<boolean>("activity-histogram-collapsed", false);
</script>

<Card.Root class="gap-0 overflow-hidden py-0">
  <div class="bg-(--panel-header-bg) flex items-center gap-2 px-4 py-2.5 {$histogramCollapsed ? '' : 'border-b'}">
    <h2 class="text-heading">Statistics</h2>
    <Button
      variant="ghost"
      size="icon-sm"
      class="text-muted-foreground ml-auto"
      onclick={() => ($histogramCollapsed = !$histogramCollapsed)}
      title={$histogramCollapsed ? "Show histograms" : "Hide histograms"}
      aria-label={$histogramCollapsed ? "Show histograms" : "Hide histograms"}
    >
      {#if $histogramCollapsed}
        <ChevronDown />
      {:else}
        <X />
      {/if}
    </Button>
  </div>
  {#if !$histogramCollapsed}
    <div class="flex flex-col gap-6 border-b p-4 sm:flex-row">
      <div class="w-full min-w-0 sm:w-1/2">
        <h3 class="text-label text-muted-foreground font-semibold uppercase">Prompt processing</h3>
        {#if stats?.prompt_histogram}
          <TokenHistogram
            data={stats.prompt_histogram}
            unit="prompt tokens/sec"
            colorClass="text-chart-2"
          />
        {:else}
          <div class="text-muted-foreground py-6 text-center text-sm">No data</div>
        {/if}
      </div>
      <div class="w-full min-w-0 sm:w-1/2">
        <h3 class="text-label text-muted-foreground font-semibold uppercase">Token generation</h3>
        {#if stats?.gen_histogram}
          <TokenHistogram data={stats.gen_histogram} unit="tokens/sec" />
        {:else}
          <div class="text-muted-foreground py-6 text-center text-sm">No data</div>
        {/if}
      </div>
    </div>
  {/if}
  <div class="grid grid-cols-2 gap-x-6 gap-y-4 p-4 sm:grid-cols-4">
    {#each [
      { label: "Requests", value: stats?.total_requests ?? 0, unit: "completed" },
      { label: "Cached", value: stats?.total_cache_tokens ?? 0, unit: "tokens" },
      { label: "Processed", value: stats?.total_input_tokens ?? 0, unit: "tokens" },
      { label: "Generated", value: stats?.total_output_tokens ?? 0, unit: "tokens" },
    ] as tile (tile.label)}
      <div>
        <div class="text-label text-muted-foreground uppercase">{tile.label}</div>
        <div class="text-xl font-semibold tabular-nums">{nf.format(tile.value)}</div>
        <div class="text-muted-foreground text-xs">{tile.unit}</div>
      </div>
    {/each}
  </div>
</Card.Root>
