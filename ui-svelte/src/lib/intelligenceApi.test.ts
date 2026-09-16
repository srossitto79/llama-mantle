import { describe, expect, it } from "vitest";
import { attemptedScore } from "./intelligenceApi";

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
