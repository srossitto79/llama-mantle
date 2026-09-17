<script lang="ts">
  import { onMount } from "svelte";
  import {
    Chart, RadarController, RadialLinearScale, PointElement, LineElement, Filler, Tooltip, Legend,
  } from "chart.js";
  import { isDarkMode } from "../stores/theme";
  import { chartChrome } from "$lib/intelligenceColors";
  import * as Card from "$lib/components/ui/card/index.js";

  Chart.register(RadarController, RadialLinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

  interface Series { label: string; color: string; values: number[] }
  interface Props { title: string; categories: string[]; series: Series[] }
  let { title, categories, series }: Props = $props();

  let canvas: HTMLCanvasElement;
  let chart: Chart;
  // Series a viewer has unchecked out of the legend — kept by label so a
  // selection survives the series list changing shape (e.g. a new run loads).
  let hidden = $state<Set<string>>(new Set());
  function toggle(label: string) {
    const next = new Set(hidden);
    if (next.has(label)) next.delete(label); else next.add(label);
    hidden = next;
  }
  function selectAll() { hidden = new Set(); }
  function clearAll() { hidden = new Set(series.map(s => s.label)); }
  function datasets() {
    return series.map(s => ({
      label: s.label, data: s.values, borderColor: s.color, backgroundColor: s.color + "1a",
      borderWidth: 2, pointRadius: 3, pointBackgroundColor: s.color, hidden: hidden.has(s.label),
    }));
  }

  function buildOptions(dark: boolean) {
    const c = chartChrome(dark);
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false as const,
      plugins: {
        // Chart.js's plugin registry is shared across every chart on the page —
        // this chart uses its own HTML legend (with selection checkboxes) instead.
        legend: { display: false },
        tooltip: {
          backgroundColor: c.surface, titleColor: c.primary, bodyColor: c.secondary,
          borderColor: c.grid, borderWidth: 1,
          callbacks: { label: (ctx: { dataset: { label?: string }; raw: unknown }) => ` ${ctx.dataset.label}: ${ctx.raw}%` },
        },
      },
      scales: {
        r: {
          min: 0, max: 100,
          angleLines: { color: c.grid }, grid: { color: c.grid },
          pointLabels: { color: c.secondary, font: { size: 11 } },
          ticks: { display: false, stepSize: 25 },
        },
      },
    };
  }

  onMount(() => {
    chart = new Chart(canvas, {
      type: "radar",
      data: { labels: categories, datasets: datasets() },
      options: buildOptions($isDarkMode),
    });
    return () => chart.destroy();
  });

  $effect(() => {
    if (!chart) return;
    const dark = $isDarkMode;
    chart.options = buildOptions(dark) as never;
    chart.data.labels = categories;
    chart.data.datasets = datasets();
    chart.update("none");
  });
</script>

<Card.Root class="py-0">
  <Card.Content class="p-4">
    <h3 class="mb-2 text-sm font-medium">{title}</h3>
    <div class="flex gap-4">
      {#if series.length > 1}
        <div class="flex flex-col gap-2">
          <div class="flex gap-2 text-xs">
            <button type="button" class="text-primary underline" onclick={selectAll}>Select all</button>
            <button type="button" class="text-primary underline" onclick={clearAll}>Clear all</button>
          </div>
          <div class="flex max-h-[280px] flex-col flex-wrap gap-x-4 gap-y-1.5 text-xs">
          {#each series as s (s.label)}
            <label class="flex items-center gap-1.5" class:opacity-50={hidden.has(s.label)}>
              <input type="checkbox" checked={!hidden.has(s.label)} onchange={() => toggle(s.label)} />
              <span class="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style="background:{s.color}"></span>
              <span class="truncate">{s.label}</span>
            </label>
          {/each}
          </div>
        </div>
      {/if}
      <div class="h-[280px] min-w-0 flex-1"><canvas bind:this={canvas}></canvas></div>
    </div>
  </Card.Content>
</Card.Root>
