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

  function buildOptions(dark: boolean) {
    const c = chartChrome(dark);
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false as const,
      plugins: {
        legend: {
          display: series.length > 1, position: "bottom" as const,
          labels: { color: c.secondary, usePointStyle: true, pointStyle: "circle" as const, font: { size: 11 } },
        },
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
      data: {
        labels: categories,
        datasets: series.map(s => ({
          label: s.label, data: s.values, borderColor: s.color, backgroundColor: s.color + "1a",
          borderWidth: 2, pointRadius: 3, pointBackgroundColor: s.color,
        })),
      },
      options: buildOptions($isDarkMode),
    });
    return () => chart.destroy();
  });

  $effect(() => {
    if (!chart) return;
    const dark = $isDarkMode;
    chart.options = buildOptions(dark) as never;
    chart.data.labels = categories;
    chart.data.datasets = series.map(s => ({
      label: s.label, data: s.values, borderColor: s.color, backgroundColor: s.color + "1a",
      borderWidth: 2, pointRadius: 3, pointBackgroundColor: s.color,
    }));
    chart.update("none");
  });
</script>

<Card.Root class="py-0">
  <Card.Content class="p-4">
    <h3 class="mb-2 text-sm font-medium">{title}</h3>
    <div class="h-[280px]"><canvas bind:this={canvas}></canvas></div>
  </Card.Content>
</Card.Root>
