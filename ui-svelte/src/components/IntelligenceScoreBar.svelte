<script lang="ts">
  import { tokenColor } from "$lib/themeColors";
  import { onMount } from "svelte";
  import { Chart, BarController, BarElement, LinearScale, CategoryScale, Tooltip } from "chart.js";
  import * as Card from "$lib/components/ui/card/index.js";

  Chart.register(BarController, BarElement, LinearScale, CategoryScale, Tooltip);

  // `max` present plots a percentage bar (the original behaviour, unchanged for
  // every existing caller). `max` absent plots the raw value instead -- an
  // efficiency figure like tokens-per-point has no natural ceiling to plot
  // against, and forcing one would be fabricated, not measured.
  interface Datum { label: string; value: number; max?: number; color: string; meta?: string; display?: string }
  interface Props { title: string; data: Datum[]; unit?: string }
  let { title, data, unit }: Props = $props();
  let percentMode = $derived(data.every(d => d.max != null));

  let canvas: HTMLCanvasElement;
  let chart: Chart;

  // Chart chrome follows the active theme: tokens are read from the document and
  // resolved to rgba() through a canvas, since chart.js cannot parse oklch() or
  // color-mix(). `themeTick` bumps whenever <html> changes class or data-theme.

  function chartChrome() {
    return {
      surface: tokenColor("--card", "#ffffff"),
      primary: tokenColor("--foreground", "#111111"),
      secondary: tokenColor("--muted-foreground", "#666666"),
      muted: tokenColor("--muted-foreground", "#666666"),
      grid: tokenColor("--border", "#dddddd"),
    };
  }
  let themeTick = $state(0);

  // One legend entry per distinct color, in first-seen order — mirrors whichever
  // models/runs are actually plotted, without chart.js trying to legend per-bar colors.
  // The full label is kept, not just its first segment: a " · " suffix now carries
  // real identity (reasoning effort, temperature, ...), not just a disposable date.
  let legend = $derived.by(() => {
    const seen = new Map<string, string>();
    for (const d of data) if (!seen.has(d.color)) seen.set(d.color, d.label);
    return [...seen.entries()].map(([color, label]) => ({ color, label }));
  });

  function buildOptions() {
    const c = chartChrome();
    return {
      indexAxis: "y" as const,
      responsive: true,
      maintainAspectRatio: false,
      animation: false as const,
      plugins: {
        // Chart.js's plugin registry is shared across every chart on the page, so
        // the radar chart's Legend registration would otherwise apply here too —
        // this chart uses its own HTML legend above the canvas instead.
        legend: { display: false },
        tooltip: {
          backgroundColor: c.surface, titleColor: c.primary, bodyColor: c.secondary,
          borderColor: c.grid, borderWidth: 1,
          callbacks: {
            label: (ctx: { dataIndex: number }) => {
              const d = data[ctx.dataIndex];
              const body = d.max != null
                ? `${d.value} / ${d.max} - ${pct(d)}% success`
                : `${d.display ?? d.value}${unit ? ` ${unit}` : ""}`;
              return ` ${body}${d.meta ? ` · ${d.meta}` : ""}`;
            },
          },
        },
      },
      scales: {
        x: percentMode
          ? {
              min: 0, max: 100, ticks: { color: c.muted, font: { size: 10 }, callback: (v: string | number) => v + "%" },
              grid: { color: c.grid }, border: { color: c.grid },
            }
          : {
              beginAtZero: true,
              ticks: { color: c.muted, font: { size: 10 }, callback: (v: string | number) => unit ? `${v} ${unit}` : String(v) },
              grid: { color: c.grid }, border: { color: c.grid },
            },
        y: {
          ticks: { color: c.secondary, font: { size: 11 } },
          grid: { display: false }, border: { color: c.grid },
        },
      },
    };
  }

  function pct(d: Datum) {
    return d.max ? Math.round((d.value / d.max) * 1000) / 10 : 0;
  }
  function barValue(d: Datum) {
    return d.max != null ? pct(d) : d.value;
  }

  onMount(() => {
    chart = new Chart(canvas, {
      type: "bar",
      data: {
        labels: data.map(d => d.label),
        datasets: [{
          data: data.map(barValue),
          backgroundColor: data.map(d => d.color),
          borderRadius: 4,
          maxBarThickness: 22,
          categoryPercentage: 0.8,
          barPercentage: 0.9,
        }],
      },
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
    chart.data.labels = data.map(d => d.label);
    chart.data.datasets[0].data = data.map(barValue);
    chart.data.datasets[0].backgroundColor = data.map(d => d.color);
    chart.update("none");
  });
</script>

<Card.Root class="gap-0 py-0">
  <div class="bg-(--panel-header-bg) flex flex-wrap items-center justify-between gap-2 border-b px-4 py-2.5">
    <h2 class="text-heading">{title}</h2>
    {#if legend.length > 1}
      <div class="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs">
        {#each legend as entry}
          <span class="flex items-center gap-1.5"><span class="inline-block size-2.5 rounded-full" style="background:{entry.color}"></span>{entry.label}</span>
        {/each}
      </div>
    {/if}
  </div>
  <Card.Content class="p-4">
    <div style="height:{Math.max(data.length * 28 + 20, 80)}px">
      <canvas bind:this={canvas}></canvas>
    </div>
  </Card.Content>
</Card.Root>
