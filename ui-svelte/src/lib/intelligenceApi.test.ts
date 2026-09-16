import { describe, expect, it, vi } from "vitest";
import { attemptedScore, deleteIntelligenceRun } from "./intelligenceApi";

describe("intelligence scoring", () => {
  it("excludes diagnostic and unsupported items while counting failed attempts", () => {
    const item = { item_id: "item", category: "coding", max_score: 10 };
    expect(attemptedScore([
      { ...item, score: 7 },
      { ...item, score: 0 },
      { ...item, score: 10, role: "diagnostic" },
      { ...item, score: 0, unsupported: true },
    ])).toEqual({ score: 7, max: 20, awaitingHuman: 0 });
  });

  it("keeps unscored rubric items out of both sides of the ratio", () => {
    const item = { item_id: "item", category: "coding", max_score: 10 };
    expect(attemptedScore([
      { ...item, score: 7 },
      { ...item, item_id: "rubric", score: 0, needs_human: true },
      { ...item, item_id: "rubric-nested", score: 0, grade: { needs_human: true } },
    ])).toEqual({ score: 7, max: 10, awaitingHuman: 2 });
  });

  it("credits a rubric item once the operator has scored it for that model", () => {
    const item = { item_id: "rubric", category: "coding", max_score: 10, score: 0, needs_human: true };
    const humanScores = { rubric: { "model-a": 8 } };
    expect(attemptedScore([item], { humanScores, modelID: "model-a" }))
      .toEqual({ score: 8, max: 10, awaitingHuman: 0 });
    expect(attemptedScore([item], { humanScores, modelID: "model-b" }))
      .toEqual({ score: 0, max: 0, awaitingHuman: 1 });
  });
});

describe("deleteIntelligenceRun", () => {
  it("sends a DELETE request to the run's URL", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) });
    vi.stubGlobal("fetch", mockFetch);

    await deleteIntelligenceRun("abc123");

    expect(mockFetch).toHaveBeenCalledWith(
      "/api/mantle/studio/intelligence/runs/abc123",
      expect.objectContaining({ method: "DELETE" }),
    );
    vi.unstubAllGlobals();
  });

  it("throws with the server's error message on failure", async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: false, status: 404, json: async () => ({ error: "run not found" }) });
    vi.stubGlobal("fetch", mockFetch);

    await expect(deleteIntelligenceRun("missing")).rejects.toThrow("run not found");
    vi.unstubAllGlobals();
  });
});
