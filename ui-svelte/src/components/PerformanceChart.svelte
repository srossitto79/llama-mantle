<script lang="ts">
  import { resolveColor } from "$lib/themeColors";
  import { onMount } from "svelte";
  import {
    Chart,
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    CategoryScale,
    Legend,
    Tooltip,
  } from "chart.js";
  import * as Card from "$lib/components/ui/card/index.js";

  // Only the line-chart pieces this component actually uses, instead of
  // Chart.js's full `registerables` (every chart type/scale/plugin).
  Chart.register(
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    CategoryScale,
    Legend,
    Tooltip,
  );

  interface Dataset {
    label: string;
    data: number[];
    borderColor: string;
  }

  interface Props {
    title: string;
    labels: string[];
    datasets: Dataset[];
    yMin?: number;
    yMax?: number;
    yLabel?: string;
    showLegend?: boolean;
  }

  let { title, labels, datasets, yMin, yMax, yLabel, showLegend = true }: Props = $props();

  let canvas: HTMLCanvasElement;
  let chart: Chart;

  // Colours come from the active theme. Any CSS colour expression (a token, a
  // var() reference, color-mix()) is resolved by the browser and then read back as
  // rgba() through a canvas, since chart.js cannot parse oklch() or color-mix().
  // `themeTick` bumps whenever <html> changes class or data-theme.

  let themeTick = $state(0);

  function getChartColors() {
    return {
      grid: resolveColor("var(--border)", "#dddddd"),
      tick: resolveColor("var(--muted-foreground)", "#666666"),
      legend: resolveColor("var(--foreground)", "#111111"),
      tooltipBg: resolveColor("var(--card)", "#ffffff"),
      tooltipText: resolveColor("var(--foreground)", "#111111"),
      tooltipBorder: resolveColor("var(--border)", "#dddddd"),
    };
  }

  function chartDatasets() {
    return datasets.map((ds) => {
      const color = resolveColor(ds.borderColor, "#888888");
      return {
        label: ds.label,
        data: [...ds.data],
        borderColor: color,
        backgroundColor: color,
        borderWidth: 1.5,
        pointRadius: 0,
        tension: 0.4,
        fill: false,
      };
    });
  }

  function buildOptions() {
    const colors = getChartColors();
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false as const,
      interaction: {
        mode: "index" as const,
        intersect: false,
      },
      plugins: {
        legend: {
          display: showLegend,
          position: "top" as const,
          labels: {
            color: colors.legend,
            usePointStyle: true,
            pointStyle: "circle" as const,
            padding: 12,
            font: { size: 11 },
          },
        },
        tooltip: {
          backgroundColor: colors.tooltipBg,
          titleColor: colors.tooltipText,
          bodyColor: colors.tooltipText,
          borderColor: colors.tooltipBorder,
          borderWidth: 1,
        },
      },
      scales: {
        x: {
          bounds: "data" as const,
          offset: false,
          ticks: { color: colors.tick, maxRotation: 0, font: { size: 10 }, maxTicksLimit: 10 },
          grid: { color: colors.grid },
        },
        y: {
          min: yMin,
          max: yMax,
          ticks: { color: colors.tick, font: { size: 10 } },
          grid: { color: colors.grid },
          title: yLabel
            ? { display: true, text: yLabel, color: colors.tick }
            : undefined,
        },
      },
    };
  }

  onMount(() => {
    chart = new Chart(canvas, {
      type: "line",
      data: { labels: [...labels], datasets: chartDatasets() },
      options: buildOptions(),
    });
    const observer = new MutationObserver(() => themeTick++);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-theme"] });

    return () => {
      observer.disconnect();
      chart.destroy();
    };
  });

  $effect(() => {
    if (!chart) return;
    void themeTick;
    chart.options = buildOptions();
    chart.update("none");
  });

  $effect(() => {
    if (!chart) return;
    void themeTick;
    const _l = labels;
    chart.data.labels = [..._l];
    chart.data.datasets = chartDatasets();
    chart.update("none");
  });
</script>

<Card.Root class="h-[300px] gap-0 py-0">
  <div class="bg-(--panel-header-bg) border-b px-4 py-2.5">
    <h2 class="text-heading">{title}</h2>
  </div>
  <Card.Content class="min-h-0 flex-1 p-4">
    <canvas bind:this={canvas}></canvas>
  </Card.Content>
</Card.Root>
