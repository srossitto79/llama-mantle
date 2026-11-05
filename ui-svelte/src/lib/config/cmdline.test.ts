import { describe, expect, it } from "vitest";
import { flagValue, hasCommentLines, joinCommand, parseCommand, setExtra, setFlag, splitCommand, type FlagRef } from "./cmdline";

const ctx: FlagRef = { name: "--ctx-size", names: ["--ctx-size", "-c"], kind: "value" };
const fa: FlagRef = { name: "--flash-attn", names: ["--flash-attn", "-fa"], kind: "value" };
const mlock: FlagRef = { name: "--mlock", names: ["--mlock"], kind: "boolean" };
const byName = new Map<string, FlagRef>();
for (const r of [ctx, fa, mlock]) for (const n of r.names) byName.set(n, r);

describe("splitCommand", () => {
  it("splits like llama-swap: comments, continuations, quotes", () => {
    const cmd = "${server} --port ${PORT} \\\n  # a comment\n  -m '${models_dir}/my model.gguf'\n  --alias \"a \\\"b\\\"\" x\\ y";
    expect(splitCommand(cmd)).toEqual(["${server}", "--port", "${PORT}", "-m", "${models_dir}/my model.gguf", "--alias", 'a "b"', "x y"]);
    expect(hasCommentLines(cmd)).toBe(true);
  });

  it("round-trips through joinCommand, single and multi line", () => {
    const words = ["llama-server", "--port", "${PORT}", "-m", "/models/a b.gguf", "--reasoning-format", "it's", "${no_mmap}"];
    expect(splitCommand(joinCommand(words, false))).toEqual(words);
    const multi = joinCommand(words, true);
    expect(multi.split("\n")[0]).toBe("llama-server");
    expect(splitCommand(multi)).toEqual(words);
  });
});

describe("flag edits", () => {
  const words = ["${server}", "--port", "${PORT}", "-c", "4096", "${ctk_std}", "--mlock", "-fa", "on"];

  it("finds flags by any of their names and keeps unknown words as extra", () => {
    const p = parseCommand(words, byName);
    expect(flagValue(words, p, ctx)).toBe("4096");
    expect(flagValue(words, p, mlock)).toBe(true);
    expect(p.extra.map((i) => words[i])).toEqual(["--port", "${PORT}", "${ctk_std}"]);
  });

  it("edits values in place and keeps order and macros", () => {
    expect(setFlag(words, byName, ctx, "8192")).toEqual(["${server}", "--port", "${PORT}", "-c", "8192", "${ctk_std}", "--mlock", "-fa", "on"]);
    expect(setFlag(words, byName, fa, "")).toEqual(["${server}", "--port", "${PORT}", "-c", "4096", "${ctk_std}", "--mlock"]);
    expect(setFlag(words, byName, mlock, false)).toEqual(["${server}", "--port", "${PORT}", "-c", "4096", "${ctk_std}", "-fa", "on"]);
  });

  it("appends a new flag at the end", () => {
    const w = ["llama-server", "-m", "x.gguf"];
    expect(setFlag(w, byName, ctx, "2048")).toEqual(["llama-server", "-m", "x.gguf", "--ctx-size", "2048"]);
    expect(setFlag(w, byName, mlock, true)).toEqual(["llama-server", "-m", "x.gguf", "--mlock"]);
  });

  it("replaces extra words with quoted text intact", () => {
    expect(setExtra(words, byName, "--port ${PORT} --alias 'a b'")).toEqual(["${server}", "-c", "4096", "--mlock", "-fa", "on", "--port", "${PORT}", "--alias", "a b"]);
  });
});
