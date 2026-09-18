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
  // point" -- drawn by hand since no datalabels plugin is in the bundle. Flips to
  // the point's left, or nudges off the top/bottom edge, whenever the default
  // placement would run past the plot area and get clipped by the canvas.
  const frontierLabels = {
    id: "frontierLabels",
    afterDatasetsDraw(c: Chart) {
      const meta = c.getDatasetMeta(0);
      const dark = $isDarkMode;
      const ctx = c.ctx;
      const area = c.chartArea;
      ctx.save();
      ctx.font = "11px sans-serif";
      ctx.fillStyle = chartChrome(dark).secondary;
      meta.data.forEach((el, i) => {
        const p = plotted[i];
        if (!p?.frontier) return;
        const width = ctx.measureText(p.label).width;
        const fitsRight = el.x + 9 + width <= area.right;
        ctx.textAlign = fitsRight ? "left" : "right";
        const x = fitsRight ? el.x + 9 : el.x - 9;
        const y = Math.min(Math.max(el.y, area.top + 6), area.bottom - 6);
        ctx.textBaseline = y === el.y ? "middle" : y < el.y ? "bottom" : "top";
        ctx.fillText(p.label, x, y);
      });
      ctx.restore();
    },
  };

  function buildOptions(dark: boolean) {
    const c = chartChrome(dark);
    const xs = plotted.map(p => p.x);
    // A log scale bounds itself tightly to the data by default, pinning the
    // cheapest and priciest point exactly on the plot boundary where their
    // marker (and label) gets clipped by the canvas edge. A multiplicative
    // margin -- not an additive one, since this is a log axis -- gives both
    // ends room without changing what "cheap" or "expensive" means visually.
    const xMin = xs.length ? Math.min(...xs) / 1.6 : 0.1;
    const xMax = xs.length ? Math.max(...xs) * 1.6 : 100;
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false as const,
      // Blank space around the plotted area itself, on top of the axis margin
      // above -- keeps a point's ring and hover target clear of the canvas edge.
      layout: { padding: { top: 14, right: 12, bottom: 6, left: 6 } },
      // The default (intersect: true) only shows a tooltip when the cursor is
      // exactly over a point's hit area -- unreliable once points sit close
      // together, since the nearer one can eat the hover for its neighbours.
      // Nearest-without-intersect is this chart's equivalent of the "nearest
      // point" hover layer a dense scatter needs, without a zoom control.
      interaction: { mode: "nearest" as const, intersect: false },
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
          min: xMin, max: xMax,
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
      // A point sitting exactly at an axis boundary (0%, 100%, or the cheapest/
      // priciest x) would otherwise have its marker sliced off at the chart
      // area's edge; the axis margin above keeps this from moving the point
      // itself, just from clipping its rendering.
      clip: false,
    };
  }

  onMount(() => {
    const dark = $isDarkMode;
    chart = new Chart(canvas, {
      type: "scatter",
      data: { datasets: [buildDataset(dark) as never] },
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
