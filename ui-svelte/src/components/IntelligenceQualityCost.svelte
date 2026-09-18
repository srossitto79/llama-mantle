<script lang="ts">
  import { onMount } from "svelte";
  import { Chart, ScatterController, PointElement, LinearScale, LogarithmicScale, Tooltip } from "chart.js";
  import { isDarkMode } from "../stores/theme";
  import { chartChrome, CategoricalAssignment } from "$lib/intelligenceColors";
  import { paretoFrontier } from "$lib/intelligenceMetrics";
  import * as Card from "$lib/components/ui/card/index.js";

  Chart.register(ScatterController, PointElement, LinearScale, LogarithmicScale, Tooltip);

  interface Point {
    key: string; label: string; minutes: number; tokens: number; pct: number;
    tokensPerPoint?: number; tokensPerSecond?: number;
  }
  interface Props { title: string; points: Point[] }
  let { title, points }: Props = $props();

  type XMode = "time" | "tokens";
  let xMode = $state<XMode>("time");

  function xValue(p: Point): number {
    // Chart.js's log scale is undefined at zero; a model that spent no time or
    // tokens has nothing meaningful to plot on this axis anyway.
    const raw = xMode === "time" ? p.minutes : p.tokens;
    return raw > 0 ? raw : 0.01;
  }

  // Emphasis, not identity: a scatter is an all-pairs form, and the project's
  // 8-slot categorical palette fails all-pairs CVD separation (validated: green vs
  // orange ΔE 3.2 under protanopia). Highlighting the Pareto frontier -- no other
  // point is both cheaper and at least as good -- against a de-emphasis gray is
  // the two-color form this chart's own question ("which do I run") actually
  // needs, and it passes at ΔE 15.9+ under every check.
  let plotted = $derived.by(() => {
    const withX = points.map(p => ({ ...p, x: xValue(p), y: p.pct }));
    const frontier = paretoFrontier(withX);
    return withX.map((p, i) => ({ ...p, frontier: frontier[i] }));
  });

  let canvas: HTMLCanvasElement;
  let chart: Chart;

  function pointColor(dark: boolean, frontier: boolean): string {
    if (!frontier) return chartChrome(dark).muted;
    // The same validated blue every other chart's first categorical slot uses --
    // requesting one fixed key always lands on slot 0.
    return new CategoricalAssignment().color("frontier", dark);
  }

  function datasetPoints() {
    return plotted.map(p => ({ x: p.x, y: p.pct }));
  }

  // Labels only the frontier -- "selective direct labels, never a number on every
  // point" -- drawn by hand since no datalabels plugin is in the bundle.
  const frontierLabels = {
    id: "frontierLabels",
    afterDatasetsDraw(c: Chart) {
      const meta = c.getDatasetMeta(0);
      const dark = $isDarkMode;
      const ctx = c.ctx;
      ctx.save();
      ctx.font = "11px sans-serif";
      ctx.textBaseline = "middle";
      ctx.fillStyle = chartChrome(dark).secondary;
      meta.data.forEach((el, i) => {
        const p = plotted[i];
        if (!p?.frontier) return;
        ctx.fillText(p.label, el.x + 9, el.y);
      });
      ctx.restore();
    },
  };

  function buildOptions(dark: boolean) {
    const c = chartChrome(dark);
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false as const,
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: c.surface, titleColor: c.primary, bodyColor: c.secondary,
          borderColor: c.grid, borderWidth: 1,
          callbacks: {
            label: (ctx: { dataIndex: number }) => {
              const p = plotted[ctx.dataIndex];
              const parts = [`${p.pct}% attempted`, `${p.minutes < 1 ? "<1m" : Math.round(p.minutes) + "m"}`, `${Math.round(p.tokens).toLocaleString()} tok`];
              if (p.tokensPerPoint != null) parts.push(`${Math.round(p.tokensPerPoint)} tok/pt`);
              if (p.tokensPerSecond != null) parts.push(`${p.tokensPerSecond.toFixed(1)} tok/s`);
              return [` ${p.label}`, ` ${parts.join(" · ")}`];
            },
          },
        },
      },
      scales: {
        x: {
          type: "logarithmic" as const,
          title: { display: true, text: xMode === "time" ? "Wall time (minutes, log)" : "Completion tokens (log)", color: c.muted, font: { size: 10 } },
          ticks: { color: c.muted, font: { size: 10 } },
          grid: { color: c.grid }, border: { color: c.baseline },
        },
        y: {
          min: 0, max: 100,
          title: { display: true, text: "% attempted", color: c.muted, font: { size: 10 } },
          ticks: { color: c.muted, font: { size: 10 }, callback: (v: string | number) => v + "%" },
          grid: { color: c.grid }, border: { color: c.baseline },
        },
      },
    };
  }

  function buildDataset(dark: boolean) {
    const c = chartChrome(dark);
    return {
      data: datasetPoints(),
      backgroundColor: plotted.map(p => pointColor(dark, p.frontier)),
      borderColor: c.surface,
      borderWidth: 2,
      pointRadius: 5,
      pointHoverRadius: 7,
      hitRadius: 12,
    };
  }

  onMount(() => {
    const dark = $isDarkMode;
    chart = new Chart(canvas, {
      type: "scatter",
      data: { datasets: [buildDataset(dark)] },
      options: buildOptions(dark),
      plugins: [frontierLabels],
    });
    return () => chart.destroy();
  });

  $effect(() => {
    if (!chart) return;
    const dark = $isDarkMode;
    // Re-read so this effect also reruns when xMode or points change.
    void xMode; void plotted;
    chart.options = buildOptions(dark) as never;
    chart.data.datasets[0] = buildDataset(dark) as never;
    chart.update("none");
  });
</script>

<Card.Root class="py-0">
  <Card.Content class="space-y-3 p-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-medium">{title}</h3>
      <div class="flex items-center gap-3">
        <div class="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs">
          <span class="flex items-center gap-1.5"><span class="inline-block h-2.5 w-2.5 rounded-full" style="background:{new CategoricalAssignment().color('frontier', $isDarkMode)}"></span>On the frontier</span>
          <span class="flex items-center gap-1.5"><span class="inline-block h-2.5 w-2.5 rounded-full" style="background:{chartChrome($isDarkMode).muted}"></span>Dominated</span>
        </div>
        <div class="inline-flex overflow-hidden rounded-md border text-xs">
          <button type="button" class="px-2 py-1" class:bg-muted={xMode === "time"} class:font-medium={xMode === "time"} onclick={() => xMode = "time"}>Time</button>
          <button type="button" class="px-2 py-1" class:bg-muted={xMode === "tokens"} class:font-medium={xMode === "tokens"} onclick={() => xMode = "tokens"}>Tokens</button>
        </div>
      </div>
    </div>
    <div class="h-[320px]"><canvas bind:this={canvas}></canvas></div>
  </Card.Content>
</Card.Root>
