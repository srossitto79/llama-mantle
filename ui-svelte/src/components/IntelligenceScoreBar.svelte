<script lang="ts">
  import { onMount } from "svelte";
  import { Chart, BarController, BarElement, LinearScale, CategoryScale, Tooltip } from "chart.js";
  import { isDarkMode } from "../stores/theme";
  import { chartChrome } from "$lib/intelligenceColors";
  import * as Card from "$lib/components/ui/card/index.js";

  Chart.register(BarController, BarElement, LinearScale, CategoryScale, Tooltip);

  interface Datum { label: string; value: number; max: number; color: string }
  interface Props { title: string; data: Datum[] }
  let { title, data }: Props = $props();

  let canvas: HTMLCanvasElement;
  let chart: Chart;
  // One legend entry per distinct color, in first-seen order — mirrors whichever
  // models/runs are actually plotted, without chart.js trying to legend per-bar colors.
  let legend = $derived.by(() => {
    const seen = new Map<string, string>();
    for (const d of data) if (!seen.has(d.color)) seen.set(d.color, d.label.split(" · ")[0]);
    return [...seen.entries()].map(([color, label]) => ({ color, label }));
  });

  function buildOptions(dark: boolean) {
    const c = chartChrome(dark);
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
            label: (ctx: { raw: unknown; dataIndex: number }) =>
              ` ${ctx.raw} / ${data[ctx.dataIndex].max} attempted points`,
          },
        },
      },
      scales: {
        x: {
          min: 0, max: 100, ticks: { color: c.muted, font: { size: 10 }, callback: (v: string | number) => v + "%" },
          grid: { color: c.grid }, border: { color: c.baseline },
        },
        y: {
          ticks: { color: c.secondary, font: { size: 11 } },
          grid: { display: false }, border: { color: c.baseline },
        },
      },
    };
  }

  function pct(d: Datum) {
    return d.max > 0 ? Math.round((d.value / d.max) * 1000) / 10 : 0;
  }

  onMount(() => {
    chart = new Chart(canvas, {
      type: "bar",
      data: {
        labels: data.map(d => d.label),
        datasets: [{
          data: data.map(pct),
          backgroundColor: data.map(d => d.color),
          borderRadius: 4,
          maxBarThickness: 22,
          categoryPercentage: 0.8,
          barPercentage: 0.9,
        }],
      },
      options: buildOptions($isDarkMode),
    });
    return () => chart.destroy();
  });

  $effect(() => {
    if (!chart) return;
    const dark = $isDarkMode;
    chart.options = buildOptions(dark) as never;
    chart.data.labels = data.map(d => d.label);
    chart.data.datasets[0].data = data.map(pct);
    chart.data.datasets[0].backgroundColor = data.map(d => d.color);
    chart.update("none");
  });
</script>

<Card.Root class="py-0">
  <Card.Content class="space-y-3 p-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-medium">{title}</h3>
      {#if legend.length > 1}
        <div class="flex flex-wrap gap-x-3 gap-y-1 text-xs">
          {#each legend as entry}
            <span class="flex items-center gap-1.5"><span class="inline-block h-2.5 w-2.5 rounded-full" style="background:{entry.color}"></span>{entry.label}</span>
          {/each}
        </div>
      {/if}
    </div>
    <div style="height:{Math.max(data.length * 28 + 20, 80)}px">
      <canvas bind:this={canvas}></canvas>
    </div>
  </Card.Content>
</Card.Root>
