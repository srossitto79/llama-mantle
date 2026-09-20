<script lang="ts">
  import { onMount } from "svelte";
  import { link } from "svelte-spa-router";
  import { Button } from "$lib/components/ui/button/index.js";
  import IntelligenceRunHeatmap from "../components/IntelligenceRunHeatmap.svelte";
  import { intelligenceRequest, intelligenceURL, REASONING_EFFORTS, type IntelligenceConfig, type IntelligenceRun, type IntelligenceDownload } from "$lib/intelligenceApi";

// Display names for known suite kinds; an unrecognised kind (a new suite the companion
// added before this map was updated) still gets a working button, just labelled by its
// raw id instead of a friendly name.
const SUITE_LABELS: Record<string, string> = {
  polyglot: "Polyglot", swebench: "SWE-bench", gsm8k: "GSM8K", mgsm: "MGSM",
  bfcl: "BFCL", ifeval: "IFEval", ruler: "RULER",
};
function suiteLabel(kind: string): string {
  return SUITE_LABELS[kind] ?? kind;
}

  // Per-viewer convenience only: this component is fully remounted on every
  // navigation (svelte-spa-router) and on reload, which would otherwise wipe
  // the form and blank the live-progress section for one round trip. Reads
  // and writes are wrapped because storage can throw (private browsing,
  // disabled site data) and the page must still work without it.
  const FORM_KEY = "intelligence-run-form";
  const RUN_CACHE_KEY = "intelligence-run-snapshot";
  interface FormCache {
    selected: string[]; profile: string; useCustomTemperature: boolean;
    temperature: number; timeout: number; maxTokens: number;
    reasoningEffort: string; resume: boolean; retryFailed: boolean; retryUnsupported: boolean;
  }
  function loadCache<T>(key: string): T | null {
    try {
      const raw = localStorage.getItem(key);
      return raw ? (JSON.parse(raw) as T) : null;
    } catch { return null; }
  }
  function saveCache(key: string, value: unknown) {
    try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* ignore */ }
  }
  const cachedForm = loadCache<FormCache>(FORM_KEY);

  let config = $state<IntelligenceConfig | null>(null);
  let modelFilter = $state("");
  let selected = $state<string[]>(cachedForm?.selected ?? []);
  let profile = $state(cachedForm?.profile ?? "quick");
  let useCustomTemperature = $state(cachedForm?.useCustomTemperature ?? true);
  let temperature = $state(cachedForm?.temperature ?? 0);
  let timeout = $state(cachedForm?.timeout ?? 5400);
  let maxTokens = $state(cachedForm?.maxTokens ?? 65536);
  let reasoningEffort = $state(cachedForm?.reasoningEffort ?? "");
  let resume = $state(cachedForm?.resume ?? false);
  let retryFailed = $state(cachedForm?.retryFailed ?? false);
  let retryUnsupported = $state(cachedForm?.retryUnsupported ?? false);
  // Seeded from cache so the heatmap and progress bar render immediately on
  // mount instead of going blank until the first /run/status resolves.
  let run = $state<IntelligenceRun | null>(loadCache<IntelligenceRun>(RUN_CACHE_KEY));
  let download = $state<IntelligenceDownload>({ state: "idle", logs: [] });
  let error = $state("");
  let configError = $state("");
  let busy = $state(false);
  let stopping = $state(false);
  let source: EventSource | undefined;
  let streamID = "";
  let refreshing = false;
  let disposed = false;
  let active = $derived(run?.state === "starting" || run?.state === "running");
  let blocked = $derived(busy || active || download.state === "running");
  let filteredModels = $derived(
    config?.models.filter(m => m.name.toLowerCase().includes(modelFilter.trim().toLowerCase())) ?? [],
  );

  async function loadConfig() {
    const loaded = await intelligenceRequest<IntelligenceConfig>("config");
    // Server defaults apply only when nothing was restored from the cache —
    // an explicit prior choice must not be overwritten on remount.
    if (!config && !cachedForm) {
      temperature = loaded.defaults.temperature;
      timeout = loaded.defaults.timeout;
      maxTokens = loaded.defaults.max_tokens;
    }
    config = loaded;
    configError = "";
    selected = selected.filter(id => config?.models.some(m => m.id === id));
    if (!config.profiles.some(p => p.id === profile)) profile = config.profiles[0]?.id ?? "default";
  }
  async function refresh() {
    if (refreshing || disposed) return;
    refreshing = true;
    try {
      const [status, prep] = await Promise.all([
        intelligenceRequest<IntelligenceRun>("run/status"),
        intelligenceRequest<IntelligenceDownload>("download/status"),
      ]);
      if (disposed) return;
      const finishedDownload = download.state === "running" && prep.state !== "running";
      run = status;
      // Tied to the full-snapshot refresh, not to per-token streaming updates
      // below (those mutate run.current in place many times a second — caching
      // on every one of them would hammer localStorage for no benefit, since
      // it's the models/totals a remount needs, not in-flight streamed text).
      saveCache(RUN_CACHE_KEY, status);
      download = prep;
      if (finishedDownload) await loadConfig();
      if (status.state === "running" || status.state === "starting") {
        if (streamID !== status.run_id) {
          source?.close();
          streamID = status.run_id;
          source = new EventSource(`${intelligenceURL}/run/stream?from=${status.seq ?? 0}`);
          source.onmessage = (message) => {
            const event = JSON.parse(message.data);
            if (event.seq <= (run?.seq ?? 0)) return;
            if (event.event === "tokens" && run?.current && event.item_id === run.current.item_id) {
              const phase = event.phase === "reasoning" ? "reasoning" : "content";
              const counter = phase === "reasoning" ? "reasoning_chars" : "content_chars";
              const missing = event.chars - run.current[counter];
              if (missing > 0) {
                // Python counts Unicode code points; snapshots can already include
                // part of a token batch that has not yet been published over SSE.
                const suffix = Array.from(event.text as string).slice(-missing).join("");
                run.current[phase] = (run.current[phase] + suffix).slice(-12000);
                run.current[counter] = event.chars;
              }
            } else if (event.event !== "tokens") void refresh();
          };
          // Polling remains available if streaming is interrupted.
          source.onerror = () => { source?.close(); streamID = ""; };
        }
      } else { source?.close(); streamID = ""; stopping = false; }
      error = "";
    } catch (e) { if (!disposed) error = String(e); }
    finally { refreshing = false; }
  }
  async function action(path: string, body: unknown = {}) {
    busy = true; error = "";
    try {
      await intelligenceRequest(path, body);
      if (path === "run/stop") stopping = true;
      await refresh();
    } catch (e) { error = String(e); }
    finally { busy = false; }
  }
  $effect(() => {
    saveCache(FORM_KEY, { selected, profile, useCustomTemperature, temperature, timeout, maxTokens, reasoningEffort, resume, retryFailed, retryUnsupported } satisfies FormCache);
  });
  onMount(() => {
    void loadConfig().catch(e => configError = String(e));
    void refresh();
    const timer = setInterval(() => void refresh(), 2000);
    return () => { disposed = true; clearInterval(timer); source?.close(); };
  });
</script>

<div class="mx-auto w-full max-w-6xl space-y-6 overflow-auto p-6">
  <div><h1 class="text-2xl font-semibold">Run intelligence suite</h1><p class="text-muted-foreground mt-1">Measure model capabilities with reproducible, automatically graded tasks.</p></div>
  {#if error}<p role="alert" class="text-destructive rounded-lg border p-4">{error}</p>{/if}
  {#if configError}<p role="alert" class="text-destructive rounded-lg border p-4">{configError} <button class="underline" onclick={() => loadConfig().catch(e => configError = String(e))}>Retry model discovery</button></p>{/if}

  <section class="space-y-4 rounded-xl border p-5">
    <h2 class="font-semibold">Models and profile</h2>
    {#if !config}<p class="text-muted-foreground">Waiting for the Intelligence service…</p>
    {:else}
      <fieldset disabled={blocked} class="space-y-3">
        <legend class="mb-2 text-sm">Models</legend>
        {#if config.models.length}
          <input type="text" placeholder="Filter models…" class="bg-background block w-full rounded-md border p-2 text-sm" bind:value={modelFilter} />
        {/if}
        <div class="max-h-64 space-y-2 overflow-auto">
          {#each filteredModels as model (model.id)}
            <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:group={selected} value={model.id} />{model.name}</label>
          {:else}<p class="text-muted-foreground text-sm">{config.models.length ? "No models match the filter." : "No models available. Configure models in Mantle, then refresh this page."}</p>{/each}
        </div>
        <label for="intelligence-profile" class="block text-sm">Profile</label><select id="intelligence-profile" class="bg-background block w-full rounded-md border p-2" bind:value={profile}>{#each config.profiles as p}<option value={p.id}>{p.id}</option>{/each}</select>
        <p class="text-muted-foreground text-sm">{config.profiles.find(p => p.id === profile)?.description}</p>
        <details open><summary class="cursor-pointer text-sm">Advanced settings</summary>
          <div class="mt-3 grid grid-cols-2 gap-3">
            <label class="text-sm">
              <span class="flex items-center gap-2"><input type="checkbox" bind:checked={useCustomTemperature} />Temperature</span>
              {#if useCustomTemperature}
                <input type="number" min="0" max="2" step="0.1" class="mt-1 w-full rounded border p-2" bind:value={temperature} />
              {:else}
                <input type="text" value="Default" disabled class="text-muted-foreground mt-1 w-full rounded border p-2" />
              {/if}
            </label>
            <label class="text-sm">Thinking effort
              <select class="bg-background mt-1 block w-full rounded-md border p-2" bind:value={reasoningEffort}>
                <option value="">Model default</option>
                {#each REASONING_EFFORTS as effort}<option value={effort}>{effort}</option>{/each}
              </select>
            </label>
            <label class="text-sm">Timeout (seconds)<input type="number" min="1" class="mt-1 w-full rounded border p-2" bind:value={timeout} /></label>
            <label class="text-sm">Maximum tokens<input type="number" min="1" class="mt-1 w-full rounded border p-2" bind:value={maxTokens} /></label>
          </div>
          <label class="mt-3 flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={resume} />Reuse matching answers from the latest results</label>
          <label class="mt-2 flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={retryFailed} />Retry failed items</label>
          {#if retryFailed}
            <label class="mt-2 ml-6 flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={retryUnsupported} />Also retry unsupported items</label>
          {/if}
        </details>
      </fieldset>
      <Button disabled={blocked || !selected.length || !(timeout > 0) || !(maxTokens > 0)} onclick={() => action("run", { models: selected, profile, temperature: useCustomTemperature ? temperature : false, reasoning_effort: reasoningEffort, timeout, max_tokens: maxTokens, only_missing: resume, retry_failed: retryFailed, retry_unsupported: retryFailed && retryUnsupported, stream: true })}>Start run</Button>
    {/if}
  </section>
  <details class="space-y-4 rounded-xl border p-5">
    <summary class="cursor-pointer font-semibold">External datasets</summary>
    <div class="mt-4 space-y-4">
      <p class="text-muted-foreground text-sm">Prepare external benchmark suites before using external profiles. Preparation and benchmark runs execute one at a time.</p>
      <div class="space-y-2">
        {#each download.suites ?? [] as suite (suite.kind)}
          <div class="flex flex-wrap items-center gap-2">
            <Button variant="outline" disabled={blocked || !config} onclick={() => action(`download/${suite.kind}`)}>Download {suiteLabel(suite.kind)}</Button>
            <span class="text-muted-foreground text-sm">{suite.cached ? "Cached" : "Not downloaded"}</span>
          </div>
        {:else}
          <p class="text-muted-foreground text-sm">No downloadable datasets reported by the Intelligence service.</p>
        {/each}
      </div>
      <p class="text-sm">{download.kind ?? "Preparation"}: {download.state}{download.finished_at ? ` · ${new Date(download.finished_at * 1000).toLocaleString()}` : ""}</p>
      {#if download.error}<p class="text-destructive text-sm">{download.error}</p>{/if}
      {#if download.logs.length}<pre class="bg-muted max-h-56 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{download.logs.join("\n")}</pre>{/if}
    </div>
  </details>
  <section class="space-y-3 rounded-xl border p-5">
    <div class="flex items-center justify-between"><h2 class="font-semibold">Live progress · {run?.state ?? "idle"}</h2>{#if active}<Button variant="outline" disabled={busy || stopping} onclick={() => action("run/stop")}>{stopping ? "Stopping…" : "Stop run"}</Button>{/if}</div>
    {#if run?.totals}<progress class="w-full" max={run.totals.items_total || 1} value={run.totals.items_done}></progress><p class="text-sm">{run.totals.items_done} / {run.totals.items_total} items</p>{/if}
    {#if run?.models?.length}<IntelligenceRunHeatmap {run} />{/if}
    {#if stopping}<p class="text-muted-foreground text-sm">Stopping at the next runner checkpoint. An active request or code test may finish first.</p>{/if}
    {#if run?.error}<p class="text-destructive">{run.error}</p>{/if}
    {#if run?.current}<p class="text-sm font-medium">{run.current.title}</p><pre class="bg-muted max-h-72 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{run.current.content || run.current.reasoning || "Waiting for model response…"}</pre>{/if}
    <a class="text-primary text-sm underline" href="/studio/intelligence/results" use:link>View results</a>
  </section>
</div>
