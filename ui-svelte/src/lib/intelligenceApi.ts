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
    totals?: { tokens_per_second?: number };
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
  raw?: { error?: string };
  role?: string; unsupported?: boolean; error?: string;
  needs_human?: boolean; truncated?: boolean; latency_ms?: number; finish_reason?: string | null;
  grade?: { unsupported?: boolean; needs_human?: boolean; note?: string };
  [key: string]: unknown;
}
/** Rubric scores entered by an operator, keyed by item then model. */
export type HumanScores = Record<string, Record<string, number>>;
export interface IntelligenceResult {
  run: IntelligenceRun;
  suite: { categories: { id: string; name: string }[] };
  models: {
    model_id: string; model_name: string; items: IntelligenceItem[];
    totals: { score: number; max_total: number; by_category: Record<string, number>; by_category_max: Record<string, number> };
  }[];
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
