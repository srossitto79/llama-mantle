import { describe, expect, it } from "vitest";
import { entryCopy } from "./fields";
import { REDACTED, type SourceEntry } from "./configApi";
import type { DraftItem } from "./draft";

function entry(fields: Record<string, unknown>, id = "m1"): SourceEntry {
  return {
    id,
    line: 3,
    order: Object.keys(fields),
    fields: Object.fromEntries(Object.entries(fields).map(([k, v]) => [k, { value: v }])),
  };
}

function item(change: DraftItem["change"]): DraftItem {
  return { key: JSON.stringify(change), change };
}

describe("entryCopy", () => {
  it("applies staged sets and unsets, including nested paths", () => {
    const e = entry({ cmd: "run a", ttl: 60, filters: { stripParams: "top_p" } });
    const { value } = entryCopy(
      e,
      [
        item({ op: "set", section: "models", id: "m1", path: ["cmd"], value: "run b" }),
        item({ op: "unset", section: "models", id: "m1", path: ["ttl"] }),
        item({ op: "set", section: "models", id: "m1", path: ["reasoningLoopGuard", "mode"], value: "stop" }),
        item({ op: "set", section: "models", id: "other", path: ["cmd"], value: "ignored" }),
      ],
      "models",
    );
    expect(value).toEqual({ cmd: "run b", filters: { stripParams: "top_p" }, reasoningLoopGuard: { mode: "stop" } });
  });

  it("follows staged renames", () => {
    const { value } = entryCopy(
      entry({ cmd: "a" }),
      [
        item({ op: "rename", section: "models", id: "m1", newId: "m2" }),
        item({ op: "set", section: "models", id: "m2", path: ["cmd"], value: "b" }),
      ],
      "models",
    );
    expect(value.cmd).toBe("b");
  });

  it("copies a staged add", () => {
    const e: SourceEntry = { id: "new", line: 0, fields: {}, order: [] };
    const { value } = entryCopy(e, [item({ op: "add", section: "models", id: "new", value: { cmd: "x" } })], "models");
    expect(value).toEqual({ cmd: "x" });
  });

  it("drops model aliases and fields holding redacted secrets", () => {
    const e = entry({ cmd: "a", aliases: ["x"], apiKey: REDACTED, env: ["A=1", REDACTED] });
    const r = entryCopy(e, [], "models");
    expect(r.value).toEqual({ cmd: "a" });
    expect(r.skipped).toEqual(["apiKey", "env"]);
  });

  it("does not share objects with the source", () => {
    const e = entry({ filters: { setParams: { a: 1 } } });
    const { value } = entryCopy(e, [], "models");
    (value.filters as { setParams: { a: number } }).setParams.a = 2;
    expect((e.fields.filters.value as { setParams: { a: number } }).setParams.a).toBe(1);
  });

  it("keeps group aliases-like fields outside models", () => {
    const { value } = entryCopy(entry({ members: ["a"], swap: false }, "g"), [], "groups");
    expect(value).toEqual({ members: ["a"], swap: false });
  });
});
