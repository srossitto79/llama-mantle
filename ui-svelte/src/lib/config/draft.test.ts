import { describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { createConfigDraft } from "./draft";
import { diffHunks, diffLines, diffStats } from "./diff";

describe("config draft", () => {
  it("replaces an edit of the same field in place and drops it when reverted", () => {
    const d = createConfigDraft();
    d.set("models", "a", ["ttl"], 60, 300);
    d.set("models", "b", ["ttl"], 10, 300);
    d.set("models", "a", ["ttl"], 90, 300);
    expect(d.changes().map((c) => [c.id, c.value])).toEqual([["a", 90], ["b", 10]]);
    d.set("models", "a", ["ttl"], 300, 300);
    expect(d.changes().map((c) => c.id)).toEqual(["b"]);
  });

  it("folds edits of a staged model into its add", () => {
    const d = createConfigDraft();
    d.add("models", "new", { cmd: "x ${PORT}" });
    d.set("models", "new", ["ttl"], 60, undefined);
    d.set("models", "new", ["filters", "stripParams"], "top_k", undefined);
    expect(d.changes()).toEqual([
      { op: "add", section: "models", id: "new", value: { cmd: "x ${PORT}", ttl: 60, filters: { stripParams: "top_k" } } },
    ]);
  });

  it("follows renames: later edits use the new id; discarding the rename drops them", () => {
    const d = createConfigDraft();
    d.rename("models", "a", "b");
    expect(d.currentId("models", "a")).toBe("b");
    d.set("models", "b", ["ttl"], 5, 300);
    d.set("models", "c", ["ttl"], 6, 300);
    const renameKey = get(d)[0].key;
    d.discard(renameKey);
    expect(d.changes().map((c) => c.id)).toEqual(["c"]);
  });

  it("deleting replaces field edits; deleting a staged add removes it entirely", () => {
    const d = createConfigDraft();
    d.set("models", "a", ["ttl"], 5, 300);
    d.remove("models", "a");
    expect(d.changes()).toEqual([{ op: "delete", section: "models", id: "a" }]);
    expect(d.currentId("models", "a")).toBe("");
    d.add("groups", "g", { members: [] });
    d.set("groups", "g", ["swap"], false, undefined);
    d.remove("groups", "g");
    expect(d.changes()).toHaveLength(1);
  });

  it("keeps detach on the change", () => {
    const d = createConfigDraft();
    d.set("models", "a", ["filters", "setParamsByID"], {}, { x: 1 }, true);
    expect(d.changes()[0]).toMatchObject({ detach: true });
  });
});

describe("diff", () => {
  const before = "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n";

  it("returns no hunks for identical text", () => {
    expect(diffHunks(before, before)).toEqual([]);
  });

  it("finds a single changed line with context", () => {
    const after = before.replace("e\n", "E\n");
    const hunks = diffHunks(after === before ? before : before, after, 2);
    expect(hunks).toHaveLength(1);
    expect(hunks[0].lines.map((l) => `${l.kind[0]}${l.text}`)).toEqual(["cc", "cd", "re", "aE", "cf", "cg"]);
    expect(hunks[0].oldStart).toBe(3);
    expect(diffStats(hunks)).toEqual({ added: 1, removed: 1 });
  });

  it("splits distant changes into separate hunks and numbers lines", () => {
    const after = "A\nb\nc\nd\ne\nf\ng\nh\ni\nJ\nk\n";
    const hunks = diffHunks(before, after, 1);
    expect(hunks).toHaveLength(2);
    const last = hunks[1].lines.at(-1)!;
    expect(last).toMatchObject({ kind: "added", text: "k", newNo: 11 });
  });

  it("treats CRLF and LF alike", () => {
    expect(diffLines("a\r\nb\r\n", "a\nb\n").every((l) => l.kind === "context")).toBe(true);
  });
});
