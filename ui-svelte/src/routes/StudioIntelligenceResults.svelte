<script lang="ts">
  import { onMount } from "svelte";
  import { Trash2 } from "@lucide/svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Badge, type BadgeVariant } from "$lib/components/ui/badge/index.js";
  import IntelligenceScoreBar from "../components/IntelligenceScoreBar.svelte";
  import IntelligenceQualityCost from "../components/IntelligenceQualityCost.svelte";
  import IntelligenceCategoryRadar from "../components/IntelligenceCategoryRadar.svelte";
  import IntelligenceBestAt from "../components/IntelligenceBestAt.svelte";
  import IntelligenceMeter from "../components/IntelligenceMeter.svelte";
  import { CategoricalAssignment, OUTCOME } from "$lib/intelligenceColors";
  import { isDarkMode } from "../stores/theme";
  import { configKey, resolveModels, type Coverage, type DuplicateMode, type ResolvedModel } from "$lib/intelligenceMetrics";
  import { deleteIntelligenceRun, intelligenceRequest, intelligenceURL, loadHumanScores, needsHumanScore, saveHumanScore, type HumanScores, type IntelligenceItem, type IntelligenceRun, type IntelligenceResult } from "$lib/intelligenceApi";

  let runs = $state<IntelligenceRun[]>([]);
  let selected = $state<string[]>([]);
  let results = $state<IntelligenceResult[]>([]);
  let filter = $state("");
  let error = $state("");
  let busy = $state(false);
  let deletingID = $state("");
  let humanScores = $state<HumanScores>({});
  let savingScore = $state("");
  let duplicateMode = $state<DuplicateMode>("latest");
  let generation = 0;
  let visible = $derived(runs.filter(r => `${r.params.profile} ${r.state} ${new Date(r.started_at * 1000).toLocaleString()}`.toLowerCase().includes(filter.toLowerCase())));
  // suite_sha256 alone misses the common case: it hashes the companion's entire
  // unfiltered catalog, which is identical across every profile run from the
  // same suite version. Comparing a `quick` run (a handful of items) against a
  // `default` run (four times as many) never trips it, and that's exactly the
  // comparison whose % figures are not directly comparable -- so this also
  // checks profile and the run's own item_count.
  let differentSuites = $derived(
    new Set(results.map(r => r.run.suite_sha256)).size > 1
    || new Set(results.map(r => r.run.params.profile)).size > 1
    || new Set(results.map(r => r.run.suite?.item_count)).size > 1);

  const STATE_VARIANT: Record<string, BadgeVariant> = {
    done: "secondary", running: "default", starting: "default",
    error: "destructive", interrupted: "destructive", stopped: "outline",
  };

  // The duplicate-mode control only needs to appear once two selected models
  // actually share a configuration (same model, effort, temperature, context,
  // provider and suite) -- otherwise there is no choice to make.
  let hasDuplicates = $derived.by(() => {
    const seen = new Set<string>();
    for (const result of results) for (const model of result.models) {
      const key = configKey(result, model);
      if (seen.has(key)) return true;
      seen.add(key);
    }
    return false;
  });

  // The single source every panel below reads. Fixes two problems at once: models
  // that differ only in reasoning effort used to collapse into one color and one
  // row, and re-running the same configuration used to duplicate every chart.
  let resolvedModels = $derived(resolveModels(results, duplicateMode, humanScores));

  let allCategories = $derived.by(() => {
    const map = new Map<string, string>();
    for (const result of results) for (const c of result.suite.categories) map.set(c.id, c.name);
    return [...map.entries()].map(([id, name]) => ({ id, name }));
  });

  // One color per resolved configuration, assigned in first-seen order -- every
  // chart and meter on the page reads the same configuration in the same color.
  let modelColors = $derived.by(() => {
    const assignment = new CategoricalAssignment();
    const map = new Map<string, string>();
    for (const rm of resolvedModels) map.set(rm.configKey, assignment.color(rm.configKey, $isDarkMode));
    return map;
  });

  let leaderboard = $derived(
    resolvedModels
      .map(rm => ({
        label: rm.label, value: round1(rm.attempted.score), max: round1(rm.attempted.max),
        color: modelColors.get(rm.configKey) ?? "#898781",
        // Shown in the tooltip, not the bar itself: a tied or near-tied score is
        // common here, and the time spent reaching it is the detail that explains
        // the tie rather than another number competing with the bar for space.
        meta: rm.cost.wallMs ? formatDuration(rm.cost.wallMs) : undefined,
      }))
      .sort((a, b) => (b.max ? b.value / b.max : 0) - (a.max ? a.value / a.max : 0)));

  // Quality against what it cost to get there -- attempted score has no relation
  // to wall time or tokens spent, so this is the only place on the page that
  // answers "which one do I actually run" rather than "which one scores highest".
  let qualityCostPoints = $derived(
    resolvedModels
      .filter(rm => rm.cost.wallMs > 0)
      .map(rm => ({
        key: rm.key, label: rm.label,
        minutes: rm.cost.wallMs / 60000, tokens: rm.cost.completionTokens,
        pct: pct(rm.attempted.score, rm.attempted.max),
        tokensPerPoint: rm.cost.tokensPerPoint, tokensPerSecond: rm.speed.tokensPerSecond,
      })));

  // The efficiency leaderboard: separates models attempted % cannot, since a slow
  // model that spends few tokens and a fast model that spends many can land on
  // the identical score.
  let costLeaderboard = $derived(
    resolvedModels
      .filter(rm => rm.cost.tokensPerPoint != null)
      .map(rm => ({
        label: rm.label, value: Math.round(rm.cost.tokensPerPoint!),
        color: modelColors.get(rm.configKey) ?? "#898781",
      }))
      .sort((a, b) => a.value - b.value));

  // Top 3 configurations per category, across the resolved set -- a "best at"
  // leaderboard alongside the overall one above. Category time rides along for
  // the same reason as the leaderboard's: ties at 100% are the norm in a
  // saturated category, and time is what actually separates them.
  //
  // Sorted by spread, not by name: with most categories saturated (every model
  // at or near 100%), a podium built from whatever .sort() does with the ties is
  // noise, not a result. The category where models actually differ belongs first.
  let bestAt = $derived.by(() => {
    const scored = allCategories.map(({ id, name }) => {
      const entries = resolvedModels
        .map(rm => {
          const max = rm.byCategoryMax[id] ?? 0;
          if (max <= 0) return null;
          const ms = rm.byCategoryTimeMs[id];
          return {
            label: rm.label, pct: Math.round(((rm.byCategory[id] ?? 0) / max) * 100),
            color: modelColors.get(rm.configKey) ?? "#898781", time: ms ? formatDuration(ms) : undefined,
          };
        })
        .filter((e): e is { label: string; pct: number; color: string; time: string | undefined } => e !== null);
      if (!entries.length) return null;
      const pcts = entries.map(e => e.pct);
      const top = Math.max(...pcts);
      return {
        name, spread: top - Math.min(...pcts), tiedAtTop: pcts.filter(p => p === top).length, modelCount: entries.length,
        ranked: [...entries].sort((a, b) => b.pct - a.pct).slice(0, 3),
      };
    }).filter((c): c is NonNullable<typeof c> => c !== null);
    return scored.sort((a, b) => b.spread - a.spread);
  });
  let saturatedCount = $derived(bestAt.filter(c => c.spread === 0).length);

  type SortKey = "model" | "run" | "score" | "attemptedPct" | "coverage" | "time" | "tokens" | "tokensPerPoint" | "tokensPerSecond";
  const COLUMNS: [SortKey, string][] = [
    ["model", "Model"], ["run", "Run / profile"], ["score", "Score"], ["attemptedPct", "%"],
    ["coverage", "Coverage"], ["time", "Time"], ["tokens", "Tokens"], ["tokensPerPoint", "tok/pt"], ["tokensPerSecond", "tok/s"],
  ];
  // Lower is better for time and tokens-per-point, so clicking them the first
  // time should surface the cheapest row, not the most expensive one.
  const ASCENDING_DEFAULT = new Set<SortKey>(["model", "run", "time", "tokensPerPoint"]);
  let sortKey = $state<SortKey>("attemptedPct");
  let sortDir = $state<"asc" | "desc">("desc");
  function toggleSort(key: SortKey) {
    if (sortKey === key) sortDir = sortDir === "asc" ? "desc" : "asc";
    else { sortKey = key; sortDir = ASCENDING_DEFAULT.has(key) ? "asc" : "desc"; }
  }
  const SORT_VALUE: Record<SortKey, (r: ResolvedModel) => number | string> = {
    model: r => r.label.toLowerCase(),
    run: r => r.startedAt,
    score: r => r.attempted.score,
    attemptedPct: r => pct(r.attempted.score, r.attempted.max),
    coverage: r => (r.coverage.total > 0 ? r.coverage.attempted / r.coverage.total : 0),
    time: r => r.cost.wallMs,
    tokens: r => r.cost.completionTokens,
    tokensPerPoint: r => r.cost.tokensPerPoint ?? Infinity,
    tokensPerSecond: r => r.speed.tokensPerSecond ?? -1,
  };
  let sortedRows = $derived.by(() => {
    const get = SORT_VALUE[sortKey];
    const dir = sortDir === "asc" ? 1 : -1;
    return [...resolvedModels].sort((a, b) => {
      const av = get(a), bv = get(b);
      return av < bv ? -dir : av > bv ? dir : 0;
    });
  });
  let sortedResolvedModels = $derived(
    [...resolvedModels].sort((a, b) =>
      pct(b.attempted.score, b.attempted.max) - pct(a.attempted.score, a.attempted.max)
      || a.label.localeCompare(b.label)));

  let kpis = $derived.by(() => {
    const uniqueModels = new Set(resolvedModels.map(rm => rm.modelID));
    // Distinct items, not rows: comparing three runs of one model must not treble it.
    const uniqueItems = new Set(resolvedModels.flatMap(rm => rm.items.map(i => i.item_id)));
    const awaiting = resolvedModels.reduce((n, rm) => n + rm.attempted.awaitingHuman, 0);
    const totalWallMs = resolvedModels.reduce((n, rm) => n + rm.cost.wallMs, 0);
    // Derived straight from resolvedModels rather than the leaderboard array, so
    // the tile can carry how long the best score took alongside the percentage --
    // the most common question this page exists to answer.
    const best = [...resolvedModels].sort((a, b) => pct(b.attempted.score, b.attempted.max) - pct(a.attempted.score, a.attempted.max))[0];
    const cheapest = [...resolvedModels]
      .filter(rm => rm.cost.tokensPerPoint != null)
      .sort((a, b) => a.cost.tokensPerPoint! - b.cost.tokensPerPoint!)[0];
    return {
      runs: results.length, models: uniqueModels.size, items: uniqueItems.size, awaiting, totalWallMs,
      best: best ? { label: best.label, pct: pct(best.attempted.score, best.attempted.max), wallMs: best.cost.wallMs } : null,
      cheapest: cheapest ? { label: cheapest.label, tokensPerPoint: Math.round(cheapest.cost.tokensPerPoint!) } : null,
    };
  });

  function outcomeColor(modelID: string, item: IntelligenceItem): string {
    if (item.unsupported || item.grade?.unsupported) return OUTCOME.unsupported.color;
    if (needsHumanScore(item)) {
      const human = humanScores[item.item_id]?.[modelID];
      if (typeof human !== "number") return OUTCOME.human.color;
      return human >= item.max_score ? OUTCOME.pass.color : human > 0 ? OUTCOME.part.color : OUTCOME.fail.color;
    }
    if (item.raw?.error || item.error || item.truncated) return OUTCOME.none.color;
    if (item.score >= item.max_score) return OUTCOME.pass.color;
    return item.score > 0 ? OUTCOME.part.color : OUTCOME.fail.color;
  }

  function scoreFor(modelID: string, item: IntelligenceItem): number | undefined {
    return humanScores[item.item_id]?.[modelID];
  }
  function round1(n: number): number {
    return Math.round(n * 10) / 10;
  }
  function pct(value: number, max: number): number {
    return max > 0 ? round1((value / max) * 100) : 0;
  }
  function formatDuration(ms: number): string {
    if (!ms) return "—";
    const seconds = ms / 1000;
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
    return `${Math.floor(seconds / 3600)}h ${Math.round((seconds % 3600) / 60)}m`;
  }
  function formatTokens(n: number): string {
    if (!n) return "0";
    if (n >= 1_000_000) return `${round1(n / 1_000_000)}M`;
    if (n >= 1_000) return `${round1(n / 1_000)}k`;
    return String(Math.round(n));
  }
  function runLabel(rm: ResolvedModel): string {
    if (rm.sourceRunIDs.length > 1) return `${rm.profile} · ${rm.sourceRunIDs.length} runs averaged`;
    return `${new Date(rm.startedAt * 1000).toLocaleString()} · ${rm.profile}`;
  }
  // Only the excluded categories that are actually nonzero earn a place in the
  // tooltip -- an all-zero breakdown would otherwise read "0 unsupported · 0
  // diagnostic · 0 awaiting a score" for nothing.
  function coverageTooltip(c: Coverage): string {
    const parts: string[] = [];
    if (c.unsupported) parts.push(`${Math.round(c.unsupported)} unsupported`);
    if (c.diagnostic) parts.push(`${Math.round(c.diagnostic)} diagnostic`);
    if (c.awaitingHuman) parts.push(`${Math.round(c.awaitingHuman)} awaiting a score`);
    const base = parts.length ? `${Math.round(c.total - c.attempted)} not attempted: ${parts.join(" · ")}` : "";
    if (!c.totalUncertain) return base;
    const note = "Total may be higher than shown.";
    return base ? `${base} · ${note}` : note;
  }
  function itemCost(item: IntelligenceItem): string {
    const parts: string[] = [];
    if (item.latency_ms != null) parts.push(`${(item.latency_ms / 1000).toFixed(1)}s`);
    if (item.tokens_completion) parts.push(`${item.tokens_completion} tok`);
    return parts.join(" · ");
  }
  // The endpoint replaces every score for an item, so the other models' scores
  // have to go back with it.
  async function setHumanScore(modelID: string, item: IntelligenceItem, value: string) {
    const key = `${item.item_id}:${modelID}`;
    const merged = { ...humanScores[item.item_id] };
    if (value === "") delete merged[modelID];
    else merged[modelID] = Math.max(0, Math.min(item.max_score, Number(value)));
    savingScore = key; error = "";
    try {
      await saveHumanScore(item.item_id, merged);
      humanScores = { ...humanScores, [item.item_id]: merged };
    } catch (e) { error = String(e); }
    finally { savingScore = ""; }
  }

  async function compare() {
    const request = ++generation;
    busy = true;
    try {
      const loaded = await Promise.all(selected.map(id => intelligenceRequest<IntelligenceResult>(`runs/${id}`)));
      if (request === generation) { results = loaded; error = ""; }
    } catch (e) { if (request === generation) error = String(e); }
    finally { if (request === generation) busy = false; }
  }
  async function refresh() {
    busy = true;
    try {
      runs = await intelligenceRequest<IntelligenceRun[]>("runs");
      // Rubric scores are optional: an older companion has no such endpoint, and
      // the page is still correct without them.
      humanScores = await loadHumanScores().catch(() => humanScores);
      if (!selected.length && runs.length) selected = [runs[0].run_id];
      await compare();
    } catch (e) { error = String(e); busy = false; }
  }
  async function removeRun(runID: string) {
    if (!window.confirm("Delete this run permanently? This cannot be undone.")) return;
    deletingID = runID; error = "";
    try {
      await deleteIntelligenceRun(runID);
      selected = selected.filter(id => id !== runID);
      await refresh();
    } catch (e) { error = String(e); }
    finally { deletingID = ""; }
  }
  onMount(() => { void refresh(); });
</script>

<div class="mx-auto w-full max-w-7xl space-y-6 overflow-auto p-6">
  <div class="flex items-center justify-between"><div><h1 class="text-2xl font-semibold">Intelligence results</h1><p class="text-muted-foreground mt-1">Compare saved runs and inspect the answers behind each score.</p></div><Button variant="outline" disabled={busy} onclick={refresh}>Refresh</Button></div>
  {#if error}<p role="alert" class="text-destructive rounded-lg border p-4">{error}</p>{/if}

  {#if kpis.runs}
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Runs compared</p><p class="text-2xl font-semibold">{kpis.runs}</p>
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Models compared</p><p class="text-2xl font-semibold">{kpis.models}</p>
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Items covered</p><p class="text-2xl font-semibold">{kpis.items}</p>
        {#if kpis.awaiting}<p class="text-muted-foreground text-xs">{kpis.awaiting} awaiting a score</p>{/if}
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Best attempted score</p>
        <p class="text-2xl font-semibold">{kpis.best?.pct ?? 0}%</p>
        {#if kpis.best}<p class="text-muted-foreground truncate text-xs" title={kpis.best.label}>{kpis.best.label} · {formatDuration(kpis.best.wallMs)}</p>{/if}
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Total time</p>
        <p class="text-2xl font-semibold">{formatDuration(kpis.totalWallMs)}</p>
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Best value (tok/pt)</p>
        <p class="text-2xl font-semibold">{kpis.cheapest?.tokensPerPoint ?? "—"}</p>
        {#if kpis.cheapest}<p class="text-muted-foreground truncate text-xs" title={kpis.cheapest.label}>{kpis.cheapest.label}</p>{/if}
      </Card.Content></Card.Root>
    </div>
  {/if}

  <section class="space-y-3 rounded-xl border p-5">
    <label class="block text-sm">Filter runs<input class="bg-background mt-1 block w-full rounded-md border p-2" placeholder="Profile, date or status" bind:value={filter} /></label>
    <div class="max-h-56 space-y-2 overflow-auto">
      {#each visible as run (run.run_id)}
        <label class="flex flex-wrap items-center gap-3 rounded border p-3 text-sm">
          <input type="checkbox" bind:group={selected} value={run.run_id} onchange={() => void compare()} />
          <span>{new Date(run.started_at * 1000).toLocaleString()}</span>
          <span class="font-medium">{run.params.profile}</span>
          <Badge variant={STATE_VARIANT[run.state] ?? "outline"}>{run.state}</Badge>
          <span class="text-muted-foreground">{run.params.models.length} models</span>
          <span class="grow"></span>
          <Button size="icon-sm" variant="ghost" title="Delete run" disabled={deletingID === run.run_id || run.state === "starting" || run.state === "running"} onclick={() => removeRun(run.run_id)}><Trash2 class="text-destructive size-4" /></Button>
        </label>
      {:else}<p class="text-muted-foreground text-sm">No runs found. Start a suite from the Run Suite page.</p>{/each}
    </div>
  </section>

  {#if differentSuites}<p class="rounded-lg border p-4 text-sm">These runs cover different items. Check coverage before comparing scores.</p>{/if}

  {#if hasDuplicates}
    <div class="flex flex-wrap items-center gap-2 text-sm">
      <span class="text-muted-foreground">Duplicates</span>
      <div class="inline-flex overflow-hidden rounded-md border">
        {#each [["latest", "Latest"], ["average", "Average"], ["all", "Show all"]] as [mode, label] (mode)}
          <button type="button" class="px-3 py-1.5" class:bg-muted={duplicateMode === mode} class:font-medium={duplicateMode === mode} onclick={() => duplicateMode = mode as DuplicateMode}>{label}</button>
        {/each}
      </div>
    </div>
  {/if}

  {#if leaderboard.length}
    <IntelligenceScoreBar title="Attempted score, best to worst" data={leaderboard} />
    <div class="overflow-x-auto rounded-xl border"><table class="w-full text-left text-sm"><thead class="bg-muted"><tr>
      {#each COLUMNS as [key, label] (key)}
        <th class="p-3"><button type="button" class="flex items-center gap-1 font-medium" onclick={() => toggleSort(key)}>{label}{#if sortKey === key}<span class="text-muted-foreground">{sortDir === "asc" ? "▲" : "▼"}</span>{/if}</button></th>
      {/each}
    </tr></thead><tbody>
      {#each sortedRows as row (row.key)}
        <tr class="border-t">
          <td class="p-3 font-medium">{row.label}</td>
          <td class="p-3">{runLabel(row)}</td>
          <td class="p-3">{round1(row.attempted.score)} / {round1(row.attempted.max)}</td>
          <td class="p-3 tabular-nums">{pct(row.attempted.score, row.attempted.max)}%</td>
          <td class="p-3 tabular-nums" title={coverageTooltip(row.coverage)}>{Math.round(row.coverage.attempted)} / {row.coverage.totalUncertain ? "≥" : ""}{Math.round(row.coverage.total)}</td>
          <td class="p-3 tabular-nums">{formatDuration(row.cost.wallMs)}</td>
          <td class="p-3 tabular-nums">{formatTokens(row.cost.completionTokens)}</td>
          <td class="p-3 tabular-nums">{row.cost.tokensPerPoint != null ? Math.round(row.cost.tokensPerPoint) : "—"}</td>
          <td class="p-3 tabular-nums">{row.speed.tokensPerSecond != null ? round1(row.speed.tokensPerSecond) : "—"}{#if row.speed.basis === "endToEnd" && row.speed.tokensPerSecond != null}<span class="text-muted-foreground" title="End-to-end throughput where time to first token was not recorded.">†</span>{/if}</td>
        </tr>
      {/each}
    </tbody></table></div>
    <p class="text-muted-foreground text-sm">Attempted totals exclude unsupported, diagnostic and unscored rubric items. Different profiles or coverage are not directly comparable. † End-to-end throughput where time to first token was not recorded.</p>
  {/if}

  {#if qualityCostPoints.length}<IntelligenceQualityCost title="Quality vs cost" points={qualityCostPoints} />{/if}
  {#if costLeaderboard.length}<IntelligenceScoreBar title="Tokens per point (lower is better)" data={costLeaderboard} unit="tok/pt" />{/if}

  {#if bestAt.length}
    <IntelligenceBestAt categories={bestAt} />
    {#if resolvedModels.length > 1 && saturatedCount}<p class="text-muted-foreground text-sm">{saturatedCount} of {bestAt.length} categories are saturated across the compared models.</p>{/if}
  {/if}

  {#if resolvedModels.length}
    <section class="space-y-4 rounded-xl border p-5">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 class="font-semibold">Model results</h2>
        <div class="flex flex-wrap gap-3 text-sm">
          {#each results as result (result.run.run_id)}
            <a class="text-primary underline" href={`${intelligenceURL}/runs/${result.run.run_id}`} download={`intelligence-${result.run.run_id}.json`}>{new Date(result.run.started_at * 1000).toLocaleDateString()} raw JSON</a>
          {/each}
        </div>
      </div>

      {#if allCategories.length >= 3}
        <IntelligenceCategoryRadar title="Category coverage" categories={allCategories.map(c => c.name)}
          series={resolvedModels.map(rm => ({
            label: rm.label, color: modelColors.get(rm.configKey) ?? "#898781",
            values: allCategories.map(c => {
              const max = rm.byCategoryMax[c.id] ?? 0;
              return max > 0 ? Math.round(((rm.byCategory[c.id] ?? 0) / max) * 100) : 0;
            }),
            times: allCategories.map(c => { const ms = rm.byCategoryTimeMs[c.id]; return ms ? formatDuration(ms) : ""; }),
          }))} />
      {/if}

      {#each sortedResolvedModels as rm (rm.key)}
        <details class="rounded-lg border p-4"><summary class="cursor-pointer font-medium">{rm.label} · {round1(rm.attempted.score)} / {round1(rm.attempted.max)} points ({pct(rm.attempted.score, rm.attempted.max)}% attempted) · {formatDuration(rm.cost.wallMs)} · {formatTokens(rm.cost.completionTokens)} tok{#if rm.speed.tokensPerSecond != null} · {round1(rm.speed.tokensPerSecond)} tok/s{/if}</summary>
          <div class="my-3"><IntelligenceMeter value={rm.attempted.score} max={rm.attempted.max} color={modelColors.get(rm.configKey) ?? "#898781"} /></div>
          <div class="my-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{#each Object.entries(rm.byCategory) as [category, score]}<div class="bg-muted rounded p-3 text-sm"><IntelligenceMeter label={allCategories.find(c => c.id === category)?.name ?? category} value={score} max={rm.byCategoryMax[category] ?? 0} color={modelColors.get(rm.configKey) ?? "#898781"} /></div>{/each}</div>
          <div class="space-y-2">{#each rm.items as item, index (item.item_id + "-" + index)}<details class="rounded border p-3"><summary class="flex cursor-pointer items-center gap-2 text-sm"><span class="size-2.5 shrink-0 rounded-[3px]" style="background:{outcomeColor(rm.modelID, item)}"></span><span class="truncate">{item.title ?? item.item_id}</span><span class="text-muted-foreground shrink-0 tabular-nums">{needsHumanScore(item) ? scoreFor(rm.modelID, item) ?? "—" : round1(item.score ?? 0)} / {item.max_score}</span>{#if itemCost(item)}<span class="text-muted-foreground shrink-0 text-xs tabular-nums">{itemCost(item)}</span>{/if}{#if item.role === "diagnostic"}<span class="text-muted-foreground shrink-0 text-xs">diagnostic</span>{/if}{#if item.unsupported || item.grade?.unsupported}<span class="text-muted-foreground shrink-0 text-xs">unsupported</span>{/if}</summary><div class="mt-3 space-y-3">
            {#if needsHumanScore(item)}
              <label class="flex flex-wrap items-center gap-2 text-sm">Rubric score
                <input type="number" min="0" max={item.max_score} step="1" class="bg-background w-24 rounded-md border p-2"
                  disabled={savingScore === `${item.item_id}:${rm.modelID}`}
                  value={scoreFor(rm.modelID, item) ?? ""}
                  onchange={(e) => void setHumanScore(rm.modelID, item, e.currentTarget.value)} />
                <span class="text-muted-foreground">of {item.max_score}</span>
              </label>
            {/if}
            {#if item.raw?.error || item.error}<p class="text-destructive text-sm">{item.raw?.error || item.error}</p>{/if}
            {#if item.grade?.note}<p class="text-muted-foreground text-sm">{item.grade.note}</p>{/if}
            <h3 class="text-sm font-medium">Model answer</h3><pre class="bg-muted max-h-72 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{item.answer || "No answer returned."}</pre>
            <details><summary class="cursor-pointer text-sm">Full diagnostics</summary><pre class="bg-muted mt-3 max-h-96 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{JSON.stringify(item, null, 2)}</pre></details>
          </div></details>{/each}</div>
        </details>
      {/each}
    </section>
  {:else if results.length}
    <p class="text-muted-foreground text-sm">No item results saved yet for the selected runs.</p>
  {/if}
</div>
