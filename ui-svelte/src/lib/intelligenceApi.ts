export const intelligenceURL = "/api/mantle/studio/intelligence";

export const REASONING_EFFORTS = ["off", "low", "medium", "high", "xhigh", "thinking"] as const;
export type ReasoningEffort = (typeof REASONING_EFFORTS)[number];

export interface IntelligenceConfig {
  models: { id: string; name: string }[];
  profiles: { id: string; description: string }[];
  defaults: { temperature: number; max_tokens: number; timeout: number };
}
export interface IntelligenceRun {
  run_id: string;
  state: string;
  started_at: number;
  suite_sha256?: string;
  params: { profile: string; models: string[] };
  settings?: { temperature: number; max_tokens: number; timeout: number; reasoning_effort?: string };
  totals?: { items_done: number; items_total: number; score: number; max_score: number };
  current?: { title: string; model_id: string; item_id: string; content: string; reasoning: string; content_chars: number; reasoning_chars: number };
  // Per-model progress, appended item by item as the run proceeds.
  models?: {
    id: string; name: string; state: string; score?: number;
    item_count?: number; carried?: number; retry?: unknown; resume?: boolean;
    started_at?: number; finished_at?: number;
    // decode_tokens_per_second is present once the companion is TTFT-aware
    // (see intelligenceMetrics.ts); absent, not zero, on an older one.
    totals?: { tokens_per_second?: number; decode_tokens_per_second?: number };
    items?: IntelligenceItem[];
  }[];
  suite?: { item_count?: number };
  error?: string;
  seq?: number;
}
export interface IntelligenceDownload {
  state: string;
  kind?: string;
  logs: string[];
  error?: string;
  finished_at?: number;
}
export interface IntelligenceItem {
  item_id: string; category: string; score: number; max_score: number;
  title?: string; answer?: string; prompt?: string;
  raw?: { error?: string; usage_estimated?: boolean };
  role?: string; unsupported?: boolean; error?: string;
  needs_human?: boolean; truncated?: boolean; latency_ms?: number; finish_reason?: string | null;
  grade?: { unsupported?: boolean; needs_human?: boolean; note?: string };
  // Wall clock is measured around the whole call; `generation_time_ms` is currently
  // equal to `latency_ms` (not yet split into prefill/decode). ttft_ms and the
  // decode_* fields exist only on runs from a companion new enough to stamp the
  // first streamed token -- absent on older runs, never zero.
  generation_time_ms?: number;
  ttft_ms?: number;
  decode_time_ms?: number;
  decode_tokens_per_second?: number;
  prefill_tokens_per_second?: number;
  tokens_per_second?: number;
  tokens_prompt?: number;
  tokens_completion?: number;
  tokens_total?: number;
  usage?: { prompt_tokens?: number; completion_tokens?: number; total_tokens?: number };
  // Set when the server sent no usage block and completion tokens were estimated
  // from streamed deltas instead of measured -- these counts must not be presented
  // as measured.
  usage_estimated?: boolean;
  // A multi-turn item (tool-loop episode or decision scenario): its latency_ms spans
  // tool calls and sandboxed test execution, not just generation, so it is excluded
  // from inference-speed aggregates while still counting toward cost.
  episode?: unknown;
  decision?: unknown;
  [key: string]: unknown;
}
/** Rubric scores entered by an operator, keyed by item then model. */
export type HumanScores = Record<string, Record<string, number>>;
export interface IntelligenceResultModel {
  model_id: string; model_name: string; items: IntelligenceItem[];
  reasoning_effort?: string | null;
  context?: number | string | null;
  provider?: string | null;
  totals: {
    score: number; max_total: number;
    by_category: Record<string, number>; by_category_max: Record<string, number>;
    total_time_ms?: number; total_tokens?: number; total_completion_tokens?: number;
    tokens_per_second?: number;
    // Present only on runs from a TTFT-aware companion (see intelligenceMetrics.ts).
    total_ttft_ms?: number; total_decode_time_ms?: number; decode_tokens_per_second?: number;
  };
}
export interface IntelligenceResult {
  run: IntelligenceRun;
  // The companion writes the *entire unfiltered* suite catalog here regardless
  // of the run's profile -- categories is safe to read (the same set for every
  // profile). `items` is that same whole catalog, so its length is never a
  // coverage denominator (see run.suite.item_count for that) -- but `id` and
  // `profiles` per item are exactly what scopeToProfile (intelligenceMetrics.ts)
  // needs to tell which of a model's *recorded* items actually belong to this
  // run's own profile, versus carried forward from a broader one.
  suite: { categories: { id: string; name: string }[]; items?: { id: string; role?: string; profiles?: string[] }[] };
  models: IntelligenceResultModel[];
}

export async function intelligenceRequest<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`${intelligenceURL}/${path}`, body === undefined ? undefined : {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  const value = await response.json();
  if (!response.ok) throw new Error(value.error || `HTTP ${response.status}`);
  return value as T;
}

export async function deleteIntelligenceRun(runID: string): Promise<void> {
  const response = await fetch(`${intelligenceURL}/runs/${runID}`, { method: "DELETE" });
  const value = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(value.error || `HTTP ${response.status}`);
}

/** The runner never scores a rubric item; an operator does, through /human-scores. */
export function needsHumanScore(item: IntelligenceItem): boolean {
  return Boolean(item.needs_human ?? item.grade?.needs_human);
}

// Unsupported and diagnostic items must not inflate attempted coverage. A rubric item
// that nobody has scored yet is not a zero either: counting it as one understates every
// total, and penalises the models that are strongest on rubric items. It stays out of
// both sides of the ratio until a human score exists, and is reported separately.
export function attemptedScore(
  items: IntelligenceItem[],
  options: { humanScores?: HumanScores; modelID?: string } = {},
): { score: number; max: number; awaitingHuman: number } {
  const { humanScores, modelID } = options;
  return items.reduce((sum, item) => {
    if (item.role === "diagnostic" || item.unsupported || item.grade?.unsupported) return sum;
    if (needsHumanScore(item)) {
      const human = modelID === undefined ? undefined : humanScores?.[item.item_id]?.[modelID];
      if (typeof human !== "number") return { ...sum, awaitingHuman: sum.awaitingHuman + 1 };
      return { ...sum, score: sum.score + human, max: sum.max + item.max_score };
    }
    return { ...sum, score: sum.score + (item.score || 0), max: sum.max + item.max_score };
  }, { score: 0, max: 0, awaitingHuman: 0 });
}

export async function loadHumanScores(): Promise<HumanScores> {
  return intelligenceRequest<HumanScores>("human-scores");
}

export async function saveHumanScore(itemID: string, scores: Record<string, number>): Promise<void> {
  await intelligenceRequest("human-scores", { item_id: itemID, scores });
}
