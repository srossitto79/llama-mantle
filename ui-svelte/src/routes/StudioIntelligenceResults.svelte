<script lang="ts">
  import { onMount } from "svelte";
  import { Trash2 } from "@lucide/svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Badge, type BadgeVariant } from "$lib/components/ui/badge/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import SegmentedControl from "../components/SegmentedControl.svelte";
  import IntelligenceScoreBar from "../components/IntelligenceScoreBar.svelte";
  import IntelligenceQualityCost from "../components/IntelligenceQualityCost.svelte";
  import IntelligenceCategoryRadar from "../components/IntelligenceCategoryRadar.svelte";
  import IntelligenceBestAt from "../components/IntelligenceBestAt.svelte";
  import IntelligenceMeter from "../components/IntelligenceMeter.svelte";
  import { CategoricalAssignment, OUTCOME } from "$lib/intelligenceColors";
  import { isDarkMode } from "../stores/theme";
  import { configKey, resolveModels, runEffort, splitForeignResults, type ForeignResult, type Coverage, type DuplicateMode, type ResolvedModel } from "$lib/intelligenceMetrics";
  import { NativeSelect } from "$lib/components/ui/native-select/index.js";
  import Tag from "../components/Tag.svelte";
  import { deleteIntelligenceRun, intelligenceRequest, intelligenceURL, loadHumanScores, needsHumanScore, saveHumanScore, type HumanScores, type IntelligenceItem, type IntelligenceRun, type IntelligenceResult } from "$lib/intelligenceApi";

  let runs = $state<IntelligenceRun[]>([]);
  let selected = $state<string[]>([]);
  let results = $state<IntelligenceResult[]>([]);
  // Model results a run holds that were answered under other settings; left
  // out of every panel (see splitForeignResults).
  let foreign = $state<ForeignResult[]>([]);
  let filter = $state("");
  let error = $state("");
  let busy = $state(false);
  let deletingID = $state("");
  let humanScores = $state<HumanScores>({});
  let savingScore = $state("");
  let duplicateMode = $state<DuplicateMode>("latest");
  let generation = 0;
  // Effort is part of what a run measured: the same model at low and at high
  // effort are different subjects, so the list can be narrowed to one effort.
  let effortFilter = $state("");
  let runEfforts = $derived([...new Set(runs.map(runEffort))].sort());
  let visible = $derived(runs.filter(r =>
    (!effortFilter || runEffort(r) === effortFilter)
    && `${r.params.profile} ${r.state} ${runEffort(r)} ${runModelNames(r)} ${new Date(r.started_at * 1000).toLocaleString()}`.toLowerCase().includes(filter.toLowerCase())));

  function runModelNames(run: IntelligenceRun): string {
    return run.models?.length ? run.models.map(m => m.name).join(", ") : `${run.params.models.length} models`;
  }
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

  const DUPLICATE_MODES: { label: string; mode: DuplicateMode }[] = [
    { label: "Latest", mode: "latest" }, { label: "Average", mode: "average" }, { label: "Show all", mode: "all" },
  ];

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
      if (request === generation) {
        const split = loaded.map(splitForeignResults);
        results = split.map(s => s.result);
        foreign = split.flatMap(s => s.foreign);
        error = "";
      }
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
  import * as Page from "$lib/components/ui/page/index.js";
</script>

<Page.Root title="Intelligence Results" width="full">
  {#snippet actions()}<Button variant="outline" size="sm" disabled={busy} onclick={refresh}>Refresh</Button>{/snippet}
  {#if error}<Alert.Root variant="destructive"><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}

  {#if kpis.runs}
    <Card.Root class="gap-0 py-0">
      <div class="grid grid-cols-2 gap-px bg-border sm:grid-cols-3 lg:grid-cols-6 [&>div]:bg-card [&>div]:px-4 [&>div]:py-3">
        <div>
          <p class="text-label text-muted-foreground uppercase">Runs compared</p>
          <p class="text-xl font-semibold tabular-nums">{kpis.runs}</p>
        </div>
        <div>
          <p class="text-label text-muted-foreground uppercase">Models compared</p>
          <p class="text-xl font-semibold tabular-nums">{kpis.models}</p>
        </div>
        <div>
          <p class="text-label text-muted-foreground uppercase">Items covered</p>
          <p class="text-xl font-semibold tabular-nums">{kpis.items}</p>
          {#if kpis.awaiting}<p class="text-muted-foreground text-xs">{kpis.awaiting} awaiting a score</p>{/if}
        </div>
        <div>
          <p class="text-label text-muted-foreground uppercase">Best attempted score</p>
          <p class="text-xl font-semibold tabular-nums">{kpis.best?.pct ?? 0}%</p>
          {#if kpis.best}<p class="text-muted-foreground truncate text-xs" title={kpis.best.label}>{kpis.best.label} · {formatDuration(kpis.best.wallMs)}</p>{/if}
        </div>
        <div>
          <p class="text-label text-muted-foreground uppercase">Total time</p>
          <p class="text-xl font-semibold tabular-nums">{formatDuration(kpis.totalWallMs)}</p>
        </div>
        <div>
          <p class="text-label text-muted-foreground uppercase">Best value (tok/pt)</p>
          <p class="text-xl font-semibold tabular-nums">{kpis.cheapest?.tokensPerPoint ?? "—"}</p>
          {#if kpis.cheapest}<p class="text-muted-foreground truncate text-xs" title={kpis.cheapest.label}>{kpis.cheapest.label}</p>{/if}
        </div>
      </div>
    </Card.Root>
  {/if}

  <Card.Root class="gap-0 py-0">
    <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-3 border-b px-4 py-2.5">
      <h2 class="text-heading">Runs</h2>
      <div class="ml-auto flex w-full flex-wrap gap-2 sm:w-auto">
        {#if runEfforts.length > 1}
          <NativeSelect class="w-full sm:w-40" aria-label="Effort" bind:value={effortFilter}>
            <option value="">All efforts</option>
            {#each runEfforts as e (e)}<option value={e}>{e}</option>{/each}
          </NativeSelect>
        {/if}
        <Input class="h-8 w-full sm:w-64" aria-label="Filter runs" placeholder="Model, profile, date or status" bind:value={filter} />
      </div>
    </div>
    <div class="max-h-56 overflow-auto">
      <ul class="divide-y">
        {#each visible as run (run.run_id)}
          <li>
            <label class="hover:bg-muted/50 flex flex-wrap items-center gap-3 px-4 py-2.5 text-sm">
              <input type="checkbox" class="accent-primary" bind:group={selected} value={run.run_id} onchange={() => void compare()} />
              <span class="tabular-nums">{new Date(run.started_at * 1000).toLocaleString()}</span>
              <span class="font-medium">{run.params.profile}</span>
              <Badge variant={STATE_VARIANT[run.state] ?? "outline"}>{run.state}</Badge>
              <span title="Thinking effort"><Tag class="font-mono text-[0.6875rem]">{runEffort(run)}</Tag></span>
              <span class="text-muted-foreground min-w-0 flex-1 basis-48 truncate" title={runModelNames(run)}>{runModelNames(run)}</span>
              <Button size="icon-sm" variant="ghost" title="Delete run" aria-label="Delete run" disabled={deletingID === run.run_id || run.state === "starting" || run.state === "running"} onclick={() => removeRun(run.run_id)}><Trash2 class="text-destructive size-4" /></Button>
            </label>
          </li>
        {:else}
          <li><EmptyState title="No runs" /></li>
        {/each}
      </ul>
    </div>
  </Card.Root>

  <!-- Attempted totals exclude unsupported, diagnostic and unscored rubric items; profiles or coverage that differ are not directly comparable. -->
  {#if foreign.length}
    <Alert.Root>
      <Alert.Title>{foreign.length === 1 ? "1 result excluded" : `${foreign.length} results excluded`}: answered under other settings than their run</Alert.Title>
      <Alert.Description>
        <ul class="font-mono text-xs">
          {#each foreign as f (f.runID + f.modelName)}
            <li>{f.modelName} · effort {f.recordedEffort}, run {f.expectedEffort} · ({new Date((runs.find(r => r.run_id === f.runID)?.started_at ?? 0) * 1000).toLocaleString()})</li>
          {/each}
        </ul>
      </Alert.Description>
    </Alert.Root>
  {/if}
  {#if differentSuites}<Alert.Root><Alert.Description>These runs cover different items. Check coverage before comparing scores.</Alert.Description></Alert.Root>{/if}

  {#if hasDuplicates}
    <SegmentedControl
      label="Duplicates"
      items={DUPLICATE_MODES}
      selected={DUPLICATE_MODES.findIndex(d => d.mode === duplicateMode)}
      onSelect={(i) => duplicateMode = DUPLICATE_MODES[i].mode}
    />
  {/if}

  {#if leaderboard.length}
    <IntelligenceScoreBar title="Attempted score, best to worst" data={leaderboard} />
    <Card.Root class="gap-0 py-0">
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead class="bg-(--panel-header-bg) text-muted-foreground border-b text-xs"><tr>
            {#each COLUMNS as [key, label] (key)}
              <th class="px-4 py-2.5 font-medium"><button type="button" class="flex items-center gap-1 whitespace-nowrap font-medium" onclick={() => toggleSort(key)}>{label}{#if sortKey === key}<span>{sortDir === "asc" ? "▲" : "▼"}</span>{/if}</button></th>
            {/each}
          </tr></thead>
          <tbody class="divide-y">
            {#each sortedRows as row (row.key)}
              <tr class="hover:bg-muted/50">
                <td class="px-4 py-2.5 font-medium whitespace-nowrap">{row.label}</td>
                <td class="px-4 py-2.5 whitespace-nowrap">{runLabel(row)}</td>
                <td class="px-4 py-2.5 font-mono text-[0.8125rem] whitespace-nowrap">{round1(row.attempted.score)} / {round1(row.attempted.max)}</td>
                <td class="px-4 py-2.5 tabular-nums">{pct(row.attempted.score, row.attempted.max)}%</td>
                <td class="px-4 py-2.5 whitespace-nowrap tabular-nums" title={coverageTooltip(row.coverage)}>{Math.round(row.coverage.attempted)} / {row.coverage.totalUncertain ? "≥" : ""}{Math.round(row.coverage.total)}</td>
                <td class="px-4 py-2.5 whitespace-nowrap tabular-nums">{formatDuration(row.cost.wallMs)}</td>
                <td class="px-4 py-2.5 tabular-nums">{formatTokens(row.cost.completionTokens)}</td>
                <td class="px-4 py-2.5 tabular-nums">{row.cost.tokensPerPoint != null ? Math.round(row.cost.tokensPerPoint) : "—"}</td>
                <td class="px-4 py-2.5 tabular-nums">{row.speed.tokensPerSecond != null ? round1(row.speed.tokensPerSecond) : "—"}{#if row.speed.basis === "endToEnd" && row.speed.tokensPerSecond != null}<span class="text-muted-foreground" title="End-to-end throughput where time to first token was not recorded.">†</span>{/if}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="text-muted-foreground border-t px-4 py-2 text-xs">† End-to-end throughput where time to first token was not recorded.</p>
    </Card.Root>
  {/if}

  {#if qualityCostPoints.length}<IntelligenceQualityCost title="Quality vs cost" points={qualityCostPoints} />{/if}
  {#if costLeaderboard.length}<IntelligenceScoreBar title="Tokens per point (lower is better)" data={costLeaderboard} unit="tok/pt" />{/if}

  {#if bestAt.length}
    <IntelligenceBestAt categories={bestAt} />
  {/if}

  {#if resolvedModels.length}
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-heading">Model results</h2>
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

    <Card.Root class="gap-0 py-0">
      <div class="divide-y">
        {#each sortedResolvedModels as rm (rm.key)}
          <details>
            <summary class="hover:bg-muted/50 cursor-pointer px-4 py-2.5 text-sm font-medium">{rm.label} · {round1(rm.attempted.score)} / {round1(rm.attempted.max)} points ({pct(rm.attempted.score, rm.attempted.max)}% attempted) · {formatDuration(rm.cost.wallMs)} · {formatTokens(rm.cost.completionTokens)} tok{#if rm.speed.tokensPerSecond != null} · {round1(rm.speed.tokensPerSecond)} tok/s{/if}</summary>
            <div class="space-y-4 border-t p-4">
              <IntelligenceMeter value={rm.attempted.score} max={rm.attempted.max} color={modelColors.get(rm.configKey) ?? "#898781"} />
              <div class="grid gap-x-6 gap-y-2 sm:grid-cols-2 lg:grid-cols-3">{#each Object.entries(rm.byCategory) as [category, score]}<IntelligenceMeter label={allCategories.find(c => c.id === category)?.name ?? category} value={score} max={rm.byCategoryMax[category] ?? 0} color={modelColors.get(rm.configKey) ?? "#898781"} />{/each}</div>
              <div class="divide-y rounded-lg border">
                {#each rm.items as item, index (item.item_id + "-" + index)}
                  <details>
                    <summary class="hover:bg-muted/50 flex cursor-pointer items-center gap-2 px-3 py-2 text-sm"><span class="size-2.5 shrink-0 rounded-[3px]" style="background:{outcomeColor(rm.modelID, item)}"></span><span class="truncate">{item.title ?? item.item_id}</span><span class="text-muted-foreground shrink-0 tabular-nums">{needsHumanScore(item) ? scoreFor(rm.modelID, item) ?? "—" : round1(item.score ?? 0)} / {item.max_score}</span>{#if itemCost(item)}<span class="text-muted-foreground shrink-0 text-xs tabular-nums">{itemCost(item)}</span>{/if}{#if item.role === "diagnostic"}<span class="text-muted-foreground shrink-0 text-xs">diagnostic</span>{/if}{#if item.unsupported || item.grade?.unsupported}<span class="text-muted-foreground shrink-0 text-xs">unsupported</span>{/if}</summary>
                    <div class="space-y-3 border-t p-3">
                      {#if needsHumanScore(item)}
                        <label class="flex flex-wrap items-center gap-2 text-sm">Rubric score
                          <Input type="number" min="0" max={item.max_score} step="1" class="h-8 w-24"
                            disabled={savingScore === `${item.item_id}:${rm.modelID}`}
                            value={scoreFor(rm.modelID, item) ?? ""}
                            onchange={(e) => void setHumanScore(rm.modelID, item, e.currentTarget.value)} />
                          <span class="text-muted-foreground">of {item.max_score}</span>
                        </label>
                      {/if}
                      {#if item.raw?.error || item.error}<p class="text-destructive text-sm">{item.raw?.error || item.error}</p>{/if}
                      {#if item.grade?.note}<p class="text-muted-foreground text-sm">{item.grade.note}</p>{/if}
                      <h3 class="text-label text-muted-foreground font-semibold uppercase">Model answer</h3>
                      <pre class="bg-muted max-h-72 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap">{item.answer || "No answer returned."}</pre>
                      <details><summary class="text-muted-foreground cursor-pointer text-sm">Full diagnostics</summary><pre class="bg-muted mt-3 max-h-96 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap">{JSON.stringify(item, null, 2)}</pre></details>
                    </div>
                  </details>
                {/each}
              </div>
            </div>
          </details>
        {/each}
      </div>
    </Card.Root>
  {:else if results.length}
    <EmptyState title="No item results" />
  {/if}
</Page.Root>
