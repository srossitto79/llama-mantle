import { describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { createConfigSession } from "./session";
import type { ConfigReader, SourceDocument } from "./configApi";

function source(hash: string): SourceDocument {
  return { hash, sections: { models: [], groups: [], profiles: [], peers: [] }, macros: [], settings: {}, anchors: [] };
}

describe("config session", () => {
  it("re-reads when the raw file and the source view come from different versions", async () => {
    let raws = 0;
    const reader: ConfigReader = {
      readRaw: async () => ({ text: `v${++raws}`, hash: raws === 1 ? "old" : "new" }),
      readSource: async () => source("new"),
      readEffectiveModels: async () => [{ id: "m", cmd: "x" }],
    };
    const s = createConfigSession(reader);
    await s.load();
    const st = get(s);
    expect(raws).toBe(2);
    expect(st.snapshot).toMatchObject({ raw: "v2", hash: "new" });
    expect(st.snapshot?.effective.m).toMatchObject({ cmd: "x" });
  });

  it("reports a load failure", async () => {
    const reader: ConfigReader = {
      readRaw: async () => {
        throw new Error("HTTP 500");
      },
      readSource: async () => source("h"),
      readEffectiveModels: async () => [],
    };
    const s = createConfigSession(reader);
    await s.load();
    expect(get(s)).toMatchObject({ loading: false, error: "HTTP 500", snapshot: null });
  });
});
