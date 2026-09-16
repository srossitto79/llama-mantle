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
    ])).toEqual({ score: 7, max: 20 });
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
