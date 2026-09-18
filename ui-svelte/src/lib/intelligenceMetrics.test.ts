import { describe, expect, it } from "vitest";
import { configKey, costTotals, resolveModels, speedTotals, type DuplicateMode } from "./intelligenceMetrics";
import type { IntelligenceItem, IntelligenceResult, IntelligenceResultModel } from "./intelligenceApi";

function item(overrides: Partial<IntelligenceItem> = {}): IntelligenceItem {
  return { item_id: "item", category: "coding", score: 5, max_score: 10, ...overrides };
}

function modelResult(overrides: Partial<IntelligenceResultModel> = {}): IntelligenceResultModel {
  return {
    model_id: "model-a", model_name: "Model A", items: [],
    totals: { score: 5, max_total: 10, by_category: { coding: 5 }, by_category_max: { coding: 10 } },
    ...overrides,
  };
}

function result(overrides: Partial<IntelligenceResult> = {}, models: IntelligenceResultModel[] = [modelResult()]): IntelligenceResult {
  return {
    run: {
      run_id: "run-1", state: "done", started_at: 1000,
      params: { profile: "quick", models: ["model-a"] },
      suite_sha256: "sha-1",
      ...overrides.run,
    },
    suite: { categories: [{ id: "coding", name: "Coding" }] },
    models,
    ...overrides,
  } as IntelligenceResult;
}

describe("configKey", () => {
  it("differs when reasoning effort differs, even for the same model_id", () => {
    const r = result();
    const low = modelResult({ reasoning_effort: "low" });
    const high = modelResult({ reasoning_effort: "high" });
    expect(configKey(r, low)).not.toEqual(configKey(r, high));
  });

  it("matches for the same model, effort, temperature, context, provider and suite", () => {
    const r1 = result({ run: { run_id: "run-1", state: "done", started_at: 1000, params: { profile: "quick", models: ["model-a"] }, suite_sha256: "sha-1" } });
    const r2 = result({ run: { run_id: "run-2", state: "done", started_at: 2000, params: { profile: "quick", models: ["model-a"] }, suite_sha256: "sha-1" } });
    expect(configKey(r1, modelResult())).toEqual(configKey(r2, modelResult()));
  });
});

describe("costTotals", () => {
  it("sums latency and tokens across counted items only", () => {
    const items = [
      item({ item_id: "a", latency_ms: 1000, tokens_completion: 50 }),
      item({ item_id: "b", latency_ms: 2000, tokens_completion: 100 }),
      item({ item_id: "c", latency_ms: 5000, tokens_completion: 999, role: "diagnostic" }),
    ];
    const totals = costTotals(items, 10);
    expect(totals.wallMs).toBe(3000);
    expect(totals.completionTokens).toBe(150);
    expect(totals.tokensPerPoint).toBe(15);
    expect(totals.secondsPerPoint).toBe(0.3);
  });

  it("reports undefined per-point figures rather than Infinity when the attempted score is zero", () => {
    const totals = costTotals([item({ latency_ms: 1000, tokens_completion: 50 })], 0);
    expect(totals.tokensPerPoint).toBeUndefined();
    expect(totals.secondsPerPoint).toBeUndefined();
  });
});

describe("speedTotals", () => {
  it("is a ratio of sums, not a mean of per-item rates", () => {
    // 10 tok / 10s and 100 tok / 1s: a mean of rates would give (1 + 100) / 2 = 50.5;
    // the ratio of sums gives 110 tok / 11s = 10 tok/s.
    const items = [
      item({ item_id: "a", latency_ms: 10000, tokens_completion: 10 }),
      item({ item_id: "b", latency_ms: 1000, tokens_completion: 100 }),
    ];
    const totals = speedTotals(items, undefined);
    expect(totals.tokensPerSecond).toBeCloseTo(10, 5);
  });

  it("reports endToEnd basis when no item has ttft_ms", () => {
    const totals = speedTotals([item({ latency_ms: 1000, tokens_completion: 50 })], undefined);
    expect(totals.basis).toBe("endToEnd");
  });

  it("reports decode basis and excludes multi-turn, estimated and the first item from prefill/TTFT", () => {
    const items = [
      item({ item_id: "first", latency_ms: 1000, tokens_completion: 50, tokens_prompt: 100, ttft_ms: 400 }),
      item({ item_id: "normal", latency_ms: 1000, tokens_completion: 50, tokens_prompt: 100, ttft_ms: 100 }),
      item({ item_id: "episode-item", latency_ms: 90000, tokens_completion: 500, episode: {} }),
      item({ item_id: "estimated", latency_ms: 1000, tokens_completion: 50, usage_estimated: true }),
    ];
    const totals = speedTotals(items, "first");
    expect(totals.basis).toBe("decode");
    expect(totals.excluded).toEqual({ multiTurn: 1, estimated: 1, firstItem: 1 });
    // counted excludes multi-turn and estimated but still counts the first item
    // for decode speed (only prefill/TTFT drop it).
    expect(totals.counted).toBe(2);
  });

  it("excludes unsupported and diagnostic items entirely", () => {
    const items = [
      item({ item_id: "a", latency_ms: 1000, tokens_completion: 50 }),
      item({ item_id: "b", latency_ms: 1000, tokens_completion: 50, unsupported: true }),
      item({ item_id: "c", latency_ms: 1000, tokens_completion: 50, role: "diagnostic" }),
    ];
    const totals = speedTotals(items, undefined);
    expect(totals.counted).toBe(1);
  });
});

describe("resolveModels", () => {
  const low = modelResult({ reasoning_effort: "low", model_name: "Model A" });
  const high = modelResult({ reasoning_effort: "high", model_name: "Model A" });

  it("keeps two reasoning efforts of the same model as two rows under every mode", () => {
    const r = result({}, [low, high]);
    for (const mode of ["all", "latest", "average"] as DuplicateMode[]) {
      const rows = resolveModels([r], mode, {});
      expect(rows).toHaveLength(2);
    }
  });

  it("collapses two runs of an identical config to one row under latest and average, keeps two under all", () => {
    const earlier = result({ run: { run_id: "run-1", state: "done", started_at: 1000, params: { profile: "quick", models: ["model-a"] }, suite_sha256: "sha-1" } });
    const later = result({ run: { run_id: "run-2", state: "done", started_at: 2000, params: { profile: "quick", models: ["model-a"] }, suite_sha256: "sha-1" } });

    expect(resolveModels([earlier, later], "all", {})).toHaveLength(2);
    expect(resolveModels([earlier, later], "latest", {})).toHaveLength(1);
    expect(resolveModels([earlier, later], "average", {})).toHaveLength(1);
  });

  it("latest picks the row from the run with the greatest started_at", () => {
    const earlier = result({ run: { run_id: "run-1", state: "done", started_at: 1000, params: { profile: "quick", models: ["model-a"] }, suite_sha256: "sha-1" } },
      [modelResult({ totals: { score: 1, max_total: 10, by_category: {}, by_category_max: {} } })]);
    const later = result({ run: { run_id: "run-2", state: "done", started_at: 2000, params: { profile: "quick", models: ["model-a"] }, suite_sha256: "sha-1" } },
      [modelResult({ totals: { score: 9, max_total: 10, by_category: {}, by_category_max: {} } })]);

    const [row] = resolveModels([earlier, later], "latest", {});
    expect(row.score).toBe(9);
    expect(row.sourceRunIDs).toEqual(["run-2"]);
  });
});
