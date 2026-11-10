import { describe, expect, it } from "vitest";
import { createTunerApi, tunableModels, TunerApiError } from "./tunerApi";

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

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("tuner api client", () => {
  it("posts the analyse request as JSON and returns the analysis", async () => {
    const f = fakeFetch(() => json(200, { system: {}, model: {}, backends: [], tiers: [] }));
    const req = { model: "/models/a.gguf", tiers: [65536], name: "a" };
    const a = await createTunerApi(f.fn).analyse(req);
    expect(a.tiers).toEqual([]);
    expect(f.calls[0].url).toBe("/api/mantle/tuner/analyse");
    expect(f.calls[0].init!.method).toBe("POST");
    expect(JSON.parse(f.calls[0].init!.body as string)).toEqual(req);
  });

  it("maps an error body to TunerApiError", async () => {
    const f = fakeFetch(() => json(400, { error: "cannot read model" }));
    const err = await createTunerApi(f.fn).analyse({ model: "x" }).catch((e) => e);
    expect(err).toBeInstanceOf(TunerApiError);
    expect(err.status).toBe(400);
    expect(err.message).toBe("cannot read model");
  });

  it("falls back to the status when the error body is not JSON", async () => {
    const f = fakeFetch(() => new Response("boom", { status: 502 }));
    const err = await createTunerApi(f.fn).analyse({ model: "x" }).catch((e) => e);
    expect(err.message).toBe("HTTP 502");
  });

  it("lists local models and treats null as empty", async () => {
    const f = fakeFetch(() => json(200, null));
    expect(await createTunerApi(f.fn).localModels()).toEqual([]);
    expect(f.calls[0].url).toBe("/api/mantle/models/local");
  });

  it("starts a cache tune by candidate id", async () => {
    const f = fakeFetch(() => json(202, { id: "task-1", state: "running", message: "", pct: 0 }));
    const task = await createTunerApi(f.fn).startCacheTune("64k-0-1");
    expect(task.id).toBe("task-1");
    expect(f.calls[0].url).toBe("/api/mantle/tuner/cache-tune");
    expect(f.calls[0].init!.method).toBe("POST");
    expect(JSON.parse(f.calls[0].init!.body as string)).toEqual({ candidate: "64k-0-1" });
  });

  it("reads and cancels a cache tune task", async () => {
    const f = fakeFetch(() => json(200, { id: "task-1", state: "completed", message: "", pct: 100 }));
    const api = createTunerApi(f.fn);
    expect((await api.task("task-1")).state).toBe("completed");
    await api.cancelCacheTune("task-1");
    expect(f.calls.map((c) => [c.url, c.init?.method])).toEqual([
      ["/api/mantle/tasks/task-1", undefined],
      ["/api/mantle/tuner/cache-tune/task-1", "DELETE"],
    ]);
  });

  it("maps a held GPU to TunerApiError", async () => {
    const f = fakeFetch(() => json(409, { error: "GPU held by tuner analysis" }));
    const err = await createTunerApi(f.fn).startCacheTune("x").catch((e) => e);
    expect(err.status).toBe(409);
    expect(err.message).toBe("GPU held by tuner analysis");
  });

  it("keeps GGUF files, drops projectors and non-first shards", () => {
    const m = (name: string, kind = "gguf") => ({ name, path: `/m/${name}`, size: 1, kind });
    const out = tunableModels([
      m("a.gguf"),
      m("mmproj-a.gguf"),
      m("b-00001-of-00003.gguf"),
      m("b-00002-of-00003.gguf"),
      m("c.safetensors", "safetensors"),
    ]);
    expect(out.map((x) => x.name)).toEqual(["a.gguf", "b-00001-of-00003.gguf"]);
  });
});
