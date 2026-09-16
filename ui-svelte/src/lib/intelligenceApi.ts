export const intelligenceURL = "/api/mantle/studio/intelligence";

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
  totals?: { items_done: number; items_total: number; score: number; max_score: number };
  current?: { title: string; model_id: string; item_id: string; content: string; reasoning: string; content_chars: number; reasoning_chars: number };
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
  grade?: { unsupported?: boolean; note?: string };
  [key: string]: unknown;
}
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

// Unsupported and diagnostic items must not inflate attempted coverage.
export function attemptedScore(items: IntelligenceItem[]): { score: number; max: number } {
  return items.reduce((sum, item) => {
    if (item.role === "diagnostic" || item.unsupported || item.grade?.unsupported) return sum;
    return { score: sum.score + (item.score || 0), max: sum.max + item.max_score };
  }, { score: 0, max: 0 });
}
