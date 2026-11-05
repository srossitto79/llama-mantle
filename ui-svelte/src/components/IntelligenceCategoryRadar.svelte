<script lang="ts">
  import { tokenColor } from "$lib/themeColors";
  import { onMount } from "svelte";
  import {
    Chart, RadarController, RadialLinearScale, PointElement, LineElement, Filler, Tooltip, Legend,
  } from "chart.js";
  import * as Card from "$lib/components/ui/card/index.js";

  Chart.register(RadarController, RadialLinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

  // Chart chrome follows the active theme: tokens are read from the document and
  // resolved to rgba() through a canvas, since chart.js cannot parse oklch() or
  // color-mix(). `themeTick` bumps whenever <html> changes class or data-theme.

  function chartChrome() {
    return {
      surface: tokenColor("--card", "#ffffff"),
      primary: tokenColor("--foreground", "#111111"),
      secondary: tokenColor("--muted-foreground", "#666666"),
      grid: tokenColor("--border", "#dddddd"),
    };
  }
  let themeTick = $state(0);

  // `times` is parallel to `values` (one formatted duration per category) and
  // optional -- an older caller passing just values still renders correctly.
  interface Series { label: string; color: string; values: number[]; times?: string[] }
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

  function buildOptions() {
    const c = chartChrome();
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
          callbacks: {
            label: (ctx: { datasetIndex: number; dataIndex: number; dataset: { label?: string }; raw: unknown }) => {
              const time = series[ctx.datasetIndex]?.times?.[ctx.dataIndex];
              return ` ${ctx.dataset.label}: ${ctx.raw}%${time ? ` · ${time}` : ""}`;
            },
          },
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
      options: buildOptions(),
    });
    const observer = new MutationObserver(() => themeTick++);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-theme"] });
    return () => { observer.disconnect(); chart.destroy(); };
  });

  $effect(() => {
    if (!chart) return;
    void themeTick;
    chart.options = buildOptions() as never;
    chart.data.labels = categories;
    chart.data.datasets = datasets();
    chart.update("none");
  });
</script>

<Card.Root class="gap-0 py-0">
  <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5">
    <h2 class="text-heading">{title}</h2>
  </div>
  <Card.Content class="p-4">
    <div class="flex flex-col gap-4 sm:flex-row">
      {#if series.length > 1}
        <div class="flex flex-col gap-2">
          <div class="flex gap-2 text-xs">
            <button type="button" class="text-primary underline" onclick={selectAll}>Select all</button>
            <button type="button" class="text-primary underline" onclick={clearAll}>Clear all</button>
          </div>
          <div class="flex max-h-[280px] flex-col flex-wrap gap-x-4 gap-y-1.5 text-xs">
          {#each series as s (s.label)}
            <label class="flex items-center gap-1.5" class:opacity-50={hidden.has(s.label)}>
              <input type="checkbox" class="accent-primary" checked={!hidden.has(s.label)} onchange={() => toggle(s.label)} />
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
