// Cost and speed aggregation for the Intelligence results page, plus the
// configuration-identity and duplicate-resolution logic that lets a viewer compare
// several runs without the same model-under-different-settings collapsing together
// or a repeated run being silently doubled.
//
// Every panel on the results page should read `resolveModels` rather than walking
// `IntelligenceResult[]` itself -- that is what keeps the leaderboard, the category
// radar, the table and the Best At panel showing the same set of rows.
import { attemptedScore, type HumanScores, type IntelligenceItem, type IntelligenceResult, type IntelligenceResultModel } from "./intelligenceApi";

// ---------------------------------------------------------------------------
// Configuration identity
// ---------------------------------------------------------------------------

/**
 * Identifies a model *as configured*, not just by model_id. Two runs of the same
 * model at different reasoning efforts are the comparison an operator most wants to
 * make -- they must never collapse into one row.
 */
export function configKey(result: IntelligenceResult, model: IntelligenceResultModel): string {
  return [
    model.model_id,
    model.reasoning_effort ?? result.run.settings?.reasoning_effort ?? "",
    result.run.settings?.temperature ?? "",
    model.context ?? "",
    model.provider ?? "",
    result.run.suite_sha256 ?? "",
  ].join("|");
}

interface ConfigParts {
  modelID: string; effort: string; temperature: string; context: string; provider: string;
}

function configParts(result: IntelligenceResult, model: IntelligenceResultModel): ConfigParts {
  return {
    modelID: model.model_id,
    effort: String(model.reasoning_effort ?? result.run.settings?.reasoning_effort ?? ""),
    temperature: String(result.run.settings?.temperature ?? ""),
    context: String(model.context ?? ""),
    provider: String(model.provider ?? ""),
  };
}

/**
 * A label states what differs across the compared set and nothing else -- a bare
 * model name when every entry shares the same effort, `Model · high` only once
 * efforts actually diverge. Appending every dimension unconditionally (or a date
 * that never repeats) would just be noise on the common case of one run.
 */
function labelFor(name: string, parts: ConfigParts, varying: Set<keyof ConfigParts>): string {
  const suffixes: string[] = [];
  if (varying.has("effort") && parts.effort) suffixes.push(parts.effort);
  if (varying.has("temperature") && parts.temperature) suffixes.push(`t=${parts.temperature}`);
  if (varying.has("context") && parts.context) suffixes.push(parts.context);
  if (varying.has("provider") && parts.provider) suffixes.push(parts.provider);
  return suffixes.length ? `${name} · ${suffixes.join(" · ")}` : name;
}

// ---------------------------------------------------------------------------
// Cost -- what the run actually spent. Counts every scored item, including
// multi-turn items and each model's first item: "how long did this cost me" has
// to include all of it, unlike the speed aggregate below.
// ---------------------------------------------------------------------------

export interface CostTotals {
  wallMs: number;
  promptTokens: number;
  completionTokens: number;
  /** completionTokens / attempted score. undefined when the attempted score is 0. */
  tokensPerPoint?: number;
  /** wallMs/1000 / attempted score. undefined when the attempted score is 0. */
  secondsPerPoint?: number;
}

function isCounted(item: IntelligenceItem): boolean {
  return item.role !== "diagnostic" && !item.unsupported && !item.grade?.unsupported;
}

export function costTotals(items: IntelligenceItem[], attemptedPoints: number): CostTotals {
  let wallMs = 0, promptTokens = 0, completionTokens = 0;
  for (const item of items) {
    if (!isCounted(item)) continue;
    wallMs += item.latency_ms ?? 0;
    promptTokens += item.tokens_prompt ?? item.usage?.prompt_tokens ?? 0;
    completionTokens += item.tokens_completion ?? item.usage?.completion_tokens ?? 0;
  }
  return {
    wallMs, promptTokens, completionTokens,
    tokensPerPoint: attemptedPoints > 0 ? completionTokens / attemptedPoints : undefined,
    secondsPerPoint: attemptedPoints > 0 ? (wallMs / 1000) / attemptedPoints : undefined,
  };
}

// ---------------------------------------------------------------------------
// Speed -- clean inference rates. Excludes items whose wall clock is not purely
// generation: multi-turn items (tool calls and sandboxed test runs share the
// clock), estimated token counts (stream-delta guesses, not measurements), and
// -- for prefill/TTFT only -- each model's first item, which still carries the
// llama-swap load time until it stops being the first request a model answers.
// ---------------------------------------------------------------------------

export interface SpeedTotals {
  /** "decode" once ttft_ms lets prefill be split out; "endToEnd" on older runs. */
  basis: "decode" | "endToEnd";
  /** Ratio of sums (Σ completion tokens / Σ time), never a mean of per-item rates. */
  tokensPerSecond?: number;
  prefillTokensPerSecond?: number;
  ttftMsMedian?: number;
  counted: number;
  excluded: { multiTurn: number; estimated: number; firstItem: number };
}

function isMultiTurn(item: IntelligenceItem): boolean {
  return Boolean(item.episode || item.decision);
}

function isEstimated(item: IntelligenceItem): boolean {
  return Boolean(item.usage_estimated || item.raw?.usage_estimated);
}

function median(values: number[]): number | undefined {
  if (!values.length) return undefined;
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

/**
 * `firstItemID` is the item_id of this model's earliest item by run order (not by
 * score), so its prefill time can be excluded from the TTFT/prefill figures without
 * excluding it from decode speed, where the load time does not land.
 */
export function speedTotals(items: IntelligenceItem[], firstItemID: string | undefined): SpeedTotals {
  let decodeTokens = 0, decodeMs = 0, prefillTokens = 0, prefillMs = 0;
  let endToEndTokens = 0, endToEndMs = 0;
  const ttfts: number[] = [];
  const excluded = { multiTurn: 0, estimated: 0, firstItem: 0 };
  let counted = 0;
  let anyTtft = false;

  for (const item of items) {
    if (!isCounted(item)) continue;
    if (isMultiTurn(item)) { excluded.multiTurn++; continue; }
    if (isEstimated(item)) { excluded.estimated++; continue; }
    counted++;

    const completion = item.tokens_completion ?? item.usage?.completion_tokens ?? 0;
    const latency = item.latency_ms ?? 0;
    if (completion && latency) { endToEndTokens += completion; endToEndMs += latency; }

    if (item.ttft_ms == null) continue;
    anyTtft = true;
    if (item.item_id === firstItemID) { excluded.firstItem++; continue; }

    ttfts.push(item.ttft_ms);
    const prompt = item.tokens_prompt ?? item.usage?.prompt_tokens ?? 0;
    if (prompt && item.ttft_ms) { prefillTokens += prompt; prefillMs += item.ttft_ms; }

    const decodeTime = item.decode_time_ms ?? (latency - item.ttft_ms);
    if (completion && decodeTime > 0) { decodeTokens += completion; decodeMs += decodeTime; }
  }

  if (!anyTtft) {
    return {
      basis: "endToEnd",
      tokensPerSecond: endToEndTokens && endToEndMs ? endToEndTokens / (endToEndMs / 1000) : undefined,
      counted, excluded,
    };
  }
  return {
    basis: "decode",
    tokensPerSecond: decodeTokens && decodeMs ? decodeTokens / (decodeMs / 1000) : undefined,
    prefillTokensPerSecond: prefillTokens && prefillMs ? prefillTokens / (prefillMs / 1000) : undefined,
    ttftMsMedian: median(ttfts),
    counted, excluded,
  };
}

// ---------------------------------------------------------------------------
// Duplicate resolution
// ---------------------------------------------------------------------------

export type DuplicateMode = "all" | "latest" | "average";

export interface ResolvedModel {
  /** Unique per rendered row -- use for #each keys and sorting. */
  key: string;
  /**
   * The underlying configuration (model + effort + temperature + context +
   * provider + suite), independent of which run(s) the row draws from. Two
   * rows with the same configKey are the same configuration shown twice (e.g.
   * "all" mode's un-collapsed repeats) and must render in the same color --
   * `key` alone cannot be used for that, since "all" mode gives every run its
   * own `key` so repeats can appear as separate rows.
   */
  configKey: string;
  label: string;
  modelID: string;
  /** Every source run this row draws from -- one entry unless mode is "average". */
  sourceRunIDs: string[];
  startedAt: number;
  profile: string;
  items: IntelligenceItem[];
  score: number;
  maxTotal: number;
  byCategory: Record<string, number>;
  byCategoryMax: Record<string, number>;
  attempted: { score: number; max: number; awaitingHuman: number };
  coverage: Coverage;
  cost: CostTotals;
  speed: SpeedTotals;
  /**
   * Wall time per category (ms), from the same counted items as `byCategory` --
   * a tied score is a common outcome (see the Best At panel), and the category
   * that took four times as long to reach the same score is the tiebreaker a
   * viewer actually wants to see next to the percentage.
   */
  byCategoryTimeMs: Record<string, number>;
}

interface Entry { result: IntelligenceResult; model: IntelligenceResultModel; key: string }

function firstItemID(items: IntelligenceItem[]): string | undefined {
  return items[0]?.item_id;
}

/**
 * How much of the suite this model actually attempted, as a real fraction rather
 * than the old "score / suite maximum" -- which conflated coverage with quality.
 *
 * `total` reads `run.suite.item_count`: the companion writes this at run start
 * from the profile-sliced item list (`len(suite_sel["items"])`), which is the
 * right denominator -- a `default`-profile run plans 48 items, not the ~200 in
 * the whole catalog. `result.suite.items` (the per-run `suite.json` snapshot)
 * is NOT that: the companion writes the *entire unfiltered* suite there
 * regardless of profile, so its length must never be used as a denominator --
 * it would make a complete `default` run of 48 read as covering a fraction of
 * ~200 and look barely started. `model.items.length` is the last resort, for a
 * run archived before `run.suite.item_count` existed.
 */
export interface Coverage {
  attempted: number; total: number;
  unsupported: number; diagnostic: number; awaitingHuman: number;
}

function coverageFor(result: IntelligenceResult, model: IntelligenceResultModel, humanScores: HumanScores): Coverage {
  let unsupported = 0, diagnostic = 0;
  for (const item of model.items) {
    if (item.role === "diagnostic") diagnostic++;
    else if (item.unsupported || item.grade?.unsupported) unsupported++;
  }
  const awaitingHuman = attemptedScore(model.items, { humanScores, modelID: model.model_id }).awaitingHuman;
  const total = result.run.suite?.item_count ?? model.items.length;
  return {
    attempted: model.items.length - unsupported - diagnostic - awaitingHuman,
    total, unsupported, diagnostic, awaitingHuman,
  };
}

// Grouped the same way `attemptedScore` counts items, so a category's time and
// its score cover exactly the same set of attempts.
function categoryTime(items: IntelligenceItem[]): Record<string, number> {
  const time: Record<string, number> = {};
  for (const item of items) {
    if (!isCounted(item)) continue;
    time[item.category] = (time[item.category] ?? 0) + (item.latency_ms ?? 0);
  }
  return time;
}

function averageCoverage(parts: Coverage[]): Coverage {
  return {
    attempted: average(parts.map(p => p.attempted)),
    total: average(parts.map(p => p.total)),
    unsupported: average(parts.map(p => p.unsupported)),
    diagnostic: average(parts.map(p => p.diagnostic)),
    awaitingHuman: average(parts.map(p => p.awaitingHuman)),
  };
}

function buildRow(rowKey: string, configKeyValue: string, label: string, entries: Entry[], humanScores: HumanScores): ResolvedModel {
  // "average" spans several runs; every other mode has exactly one entry here.
  const items = entries.flatMap(e => e.model.items);
  const modelID = entries[0].model.model_id;
  const attempted = entries.length === 1
    ? attemptedScore(entries[0].model.items, { humanScores, modelID })
    : averageAttempted(entries, humanScores, modelID);
  const byCategory: Record<string, number> = {};
  const byCategoryMax: Record<string, number> = {};
  for (const cat of new Set(entries.flatMap(e => Object.keys(e.model.totals.by_category_max)))) {
    const scores = entries.map(e => e.model.totals.by_category[cat] ?? 0);
    const maxes = entries.map(e => e.model.totals.by_category_max[cat] ?? 0);
    byCategory[cat] = average(scores);
    byCategoryMax[cat] = average(maxes);
  }
  const cost = averageCost(entries.map(e => costTotals(e.model.items, attemptedScore(e.model.items, { humanScores, modelID: e.model.model_id }).score)));
  const speed = averageSpeed(entries.map(e => speedTotals(e.model.items, firstItemID(e.model.items))));
  const coverage = entries.length === 1
    ? coverageFor(entries[0].result, entries[0].model, humanScores)
    : averageCoverage(entries.map(e => coverageFor(e.result, e.model, humanScores)));
  const perEntryCategoryTime = entries.map(e => categoryTime(e.model.items));
  const byCategoryTimeMs: Record<string, number> = {};
  for (const cat of new Set(perEntryCategoryTime.flatMap(t => Object.keys(t)))) {
    byCategoryTimeMs[cat] = average(perEntryCategoryTime.map(t => t[cat] ?? 0));
  }
  return {
    key: rowKey, configKey: configKeyValue, label, modelID,
    sourceRunIDs: entries.map(e => e.result.run.run_id),
    startedAt: Math.max(...entries.map(e => e.result.run.started_at)),
    profile: entries[entries.length - 1].result.run.params.profile,
    items,
    score: average(entries.map(e => e.model.totals.score)),
    maxTotal: average(entries.map(e => e.model.totals.max_total)),
    byCategory, byCategoryMax, byCategoryTimeMs,
    attempted, coverage, cost, speed,
  };
}

function average(values: number[]): number {
  return values.length ? values.reduce((a, b) => a + b, 0) / values.length : 0;
}

// Coverage can differ between runs of the same config, so the attempted score is
// itself an average of ratios' numerator and denominator, not just re-summed --
// a run that skipped an item must not silently shrink another run's max.
function averageAttempted(entries: Entry[], humanScores: HumanScores, modelID: string) {
  const parts = entries.map(e => attemptedScore(e.model.items, { humanScores, modelID }));
  return {
    score: average(parts.map(p => p.score)),
    max: average(parts.map(p => p.max)),
    awaitingHuman: Math.round(average(parts.map(p => p.awaitingHuman))),
  };
}

// A ratio of sums, computed once, then averaged across runs -- never a mean of
// per-run rates, which would weight a short run the same as a long one.
function averageCost(parts: CostTotals[]): CostTotals {
  const wallMs = average(parts.map(p => p.wallMs));
  const promptTokens = average(parts.map(p => p.promptTokens));
  const completionTokens = average(parts.map(p => p.completionTokens));
  const tpp = parts.map(p => p.tokensPerPoint).filter((v): v is number => v != null);
  const spp = parts.map(p => p.secondsPerPoint).filter((v): v is number => v != null);
  return {
    wallMs, promptTokens, completionTokens,
    tokensPerPoint: tpp.length ? average(tpp) : undefined,
    secondsPerPoint: spp.length ? average(spp) : undefined,
  };
}

function averageSpeed(parts: SpeedTotals[]): SpeedTotals {
  const basis: SpeedTotals["basis"] = parts.every(p => p.basis === "decode") ? "decode" : "endToEnd";
  const tps = parts.map(p => p.tokensPerSecond).filter((v): v is number => v != null);
  const pfs = parts.map(p => p.prefillTokensPerSecond).filter((v): v is number => v != null);
  const ttft = parts.map(p => p.ttftMsMedian).filter((v): v is number => v != null);
  return {
    basis,
    tokensPerSecond: tps.length ? average(tps) : undefined,
    prefillTokensPerSecond: pfs.length ? average(pfs) : undefined,
    ttftMsMedian: ttft.length ? average(ttft) : undefined,
    counted: parts.reduce((a, p) => a + p.counted, 0),
    excluded: {
      multiTurn: parts.reduce((a, p) => a + p.excluded.multiTurn, 0),
      estimated: parts.reduce((a, p) => a + p.excluded.estimated, 0),
      firstItem: parts.reduce((a, p) => a + p.excluded.firstItem, 0),
    },
  };
}

/**
 * The single source every panel on the results page should read. Groups the
 * selected runs' models by configuration identity and resolves collisions
 * according to `mode`:
 *
 * - "all": one row per (config, run) -- nothing collapses, labels gain a date
 *   suffix once a key repeats.
 * - "latest" (default): one row per config, from the run with the greatest
 *   started_at. Simple and never inflates a model by cherry-picking a good run.
 * - "average": one row per config, averaging scores, coverage, cost and speed
 *   across every run that shares the key.
 */
export function resolveModels(
  results: IntelligenceResult[],
  mode: DuplicateMode,
  humanScores: HumanScores,
): ResolvedModel[] {
  const entries: Entry[] = results.flatMap(result =>
    result.models.map(model => ({ result, model, key: configKey(result, model) })));

  const byKey = new Map<string, Entry[]>();
  for (const entry of entries) {
    const list = byKey.get(entry.key);
    if (list) list.push(entry); else byKey.set(entry.key, [entry]);
  }

  // Only components that actually differ across the selected set earn a place in
  // the label -- see labelFor.
  const varying = new Set<keyof ConfigParts>();
  const allParts = entries.map(e => configParts(e.result, e.model));
  for (const dim of ["effort", "temperature", "context", "provider"] as const) {
    if (new Set(allParts.map(p => p[dim])).size > 1) varying.add(dim);
  }

  if (mode === "all") {
    return entries.map(entry => {
      const siblings = byKey.get(entry.key)!;
      const dated = siblings.length > 1;
      const base = labelFor(entry.model.model_name, configParts(entry.result, entry.model), varying);
      const label = dated ? `${base} · ${new Date(entry.result.run.started_at * 1000).toLocaleDateString()}` : base;
      return buildRow(`${entry.key}#${entry.result.run.run_id}`, entry.key, label, [entry], humanScores);
    });
  }

  return [...byKey.entries()].map(([key, group]) => {
    const label = labelFor(group[0].model.model_name, configParts(group[0].result, group[0].model), varying);
    if (mode === "latest") {
      const latest = group.reduce((a, b) => (b.result.run.started_at > a.result.run.started_at ? b : a));
      return buildRow(key, key, label, [latest], humanScores);
    }
    // "average"
    return buildRow(key, key, label, group, humanScores);
  });
}

// ---------------------------------------------------------------------------
// Pareto frontier -- for the quality-vs-cost scatter's "which do I run" emphasis.
// ---------------------------------------------------------------------------

export interface FrontierPoint { x: number; y: number }

/**
 * Marks each point as on the frontier (true) or dominated (false): a point is
 * dominated once another point is both no worse on x (lower, since x is a cost
 * a viewer wants to minimize) and no worse on y (higher, a quality a viewer
 * wants to maximize), with a strict improvement on at least one axis. Ties on
 * both axes leave both points on the frontier, since neither actually beats
 * the other.
 */
export function paretoFrontier<T extends FrontierPoint>(points: T[]): boolean[] {
  return points.map(p =>
    !points.some(o => o !== p && o.x <= p.x && o.y >= p.y && (o.x < p.x || o.y > p.y)));
}
