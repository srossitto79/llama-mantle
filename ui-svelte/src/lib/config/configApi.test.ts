import { describe, expect, it } from "vitest";
import { ConfigApiError, createConfigApi, envRefs, macroRefs } from "./configApi";

type Call = { url: string; init?: RequestInit };

function fakeFetch(respond: (call: Call) => Response) {
  const calls: Call[] = [];
  const fn = async (url: string, init?: RequestInit) => {
    const call = { url, init };
    calls.push(call);
    return respond(call);
  };
  return { fn, calls };
}

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });

describe("config api client", () => {
  it("reads the raw file with its hash from the ETag", async () => {
    const f = fakeFetch(() => new Response("models: {}\n", { status: 200, headers: { ETag: '"abc123"' } }));
    const raw = await createConfigApi(f.fn).readRaw();
    expect(raw).toEqual({ text: "models: {}\n", hash: "abc123" });
  });

  it("sends changes with the base hash and returns the preview", async () => {
    const f = fakeFetch(() => json(200, { baseHash: "h1", hash: "h2", yaml: "x", problems: [], reload: "all" }));
    const api = createConfigApi(f.fn);
    const p = await api.preview("h1", [{ op: "set", section: "models", id: "m", path: ["ttl"], value: 5 }]);
    expect(p.reload).toBe("all");
    expect(f.calls[0].url).toBe("/api/mantle/config/changes/preview");
    expect(JSON.parse(f.calls[0].init!.body as string)).toEqual({
      baseHash: "h1",
      changes: [{ op: "set", section: "models", id: "m", path: ["ttl"], value: 5 }],
    });
  });

  it("puts raw text with If-Match", async () => {
    const f = fakeFetch(() => json(200, { hash: "h2", reload: "restart" }));
    await createConfigApi(f.fn).putRaw("a: 1\n", "h1");
    expect((f.calls[0].init!.headers as Record<string, string>)["If-Match"]).toBe('"h1"');
  });

  it("surfaces the server error: code, anchor and problems", async () => {
    const f = fakeFetch(() =>
      json(400, {
        error: "models.a.filters is inherited from anchor \"x\"",
        code: "aliased",
        change: { index: 2, code: "aliased", anchor: "x", message: "..." },
      }),
    );
    const err = await createConfigApi(f.fn).apply("h", []).catch((e) => e);
    expect(err).toBeInstanceOf(ConfigApiError);
    expect(err).toMatchObject({ status: 400, code: "aliased", anchor: "x", changeIndex: 2 });
    expect(err.message).toContain("inherited from anchor");

    const g = fakeFetch(() => json(400, { error: "config is invalid", code: "invalid", problems: [{ line: 3, message: "bad" }] }));
    const err2 = await createConfigApi(g.fn).putRaw("x", "h").catch((e) => e);
    expect(err2.problems).toEqual([{ line: 3, message: "bad" }]);
  });

  it("reports a stale base as a 409 with code stale", async () => {
    const f = fakeFetch(() => json(409, { error: "config file changed since it was read", code: "stale" }));
    await expect(createConfigApi(f.fn).apply("old", [])).rejects.toMatchObject({ status: 409, code: "stale" });
  });
});

describe("reference helpers", () => {
  it("finds macro references but not built-ins or env", () => {
    expect(macroRefs("${server} --port ${PORT} -m ${models_dir}/x ${env.KEY} ${MODEL_ID} ${server}")).toEqual(["server", "models_dir"]);
    expect(envRefs("A=${env.KEY} B=${env.OTHER_1}")).toEqual(["KEY", "OTHER_1"]);
  });
});
