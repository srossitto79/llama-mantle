<script lang="ts">
  import { onMount } from "svelte";
  import { link } from "svelte-spa-router";
  import { Button } from "$lib/components/ui/button/index.js";
  import { intelligenceRequest, intelligenceURL, REASONING_EFFORTS, type IntelligenceConfig, type IntelligenceRun, type IntelligenceDownload } from "$lib/intelligenceApi";

  let config = $state<IntelligenceConfig | null>(null);
  let selected = $state<string[]>([]);
  let profile = $state("quick");
  let useCustomTemperature = $state(true);
  let temperature = $state(0);
  let timeout = $state(5400);
  let maxTokens = $state(65536);
  let reasoningEffort = $state("");
  let resume = $state(false);
  let run = $state<IntelligenceRun | null>(null);
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

  async function loadConfig() {
    const loaded = await intelligenceRequest<IntelligenceConfig>("config");
    if (!config) {
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
  <div class="grid gap-6 lg:grid-cols-2">
    <section class="space-y-4 rounded-xl border p-5">
      <h2 class="font-semibold">Models and profile</h2>
      {#if !config}<p class="text-muted-foreground">Waiting for the Intelligence service…</p>
      {:else}
        <fieldset disabled={blocked} class="space-y-3">
          <legend class="mb-2 text-sm">Models</legend>
          <div class="max-h-64 space-y-2 overflow-auto">
            {#each config.models as model (model.id)}
              <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:group={selected} value={model.id} />{model.name}</label>
            {:else}<p class="text-muted-foreground text-sm">No models available. Configure models in Mantle, then refresh this page.</p>{/each}
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
          </details>
        </fieldset>
        <Button disabled={blocked || !selected.length || !(timeout > 0) || !(maxTokens > 0)} onclick={() => action("run", { models: selected, profile, temperature: useCustomTemperature ? temperature : false, reasoning_effort: reasoningEffort, timeout, max_tokens: maxTokens, only_missing: resume, stream: true })}>Start run</Button>
      {/if}
    </section>
    <section class="space-y-4 rounded-xl border p-5">
      <h2 class="font-semibold">External datasets</h2>
      <p class="text-muted-foreground text-sm">Prepare polyglot exercises and SWE-bench tasks before using external profiles. Preparation and benchmark runs execute one at a time.</p>
      <div class="flex flex-wrap gap-2"><Button variant="outline" disabled={blocked || !config} onclick={() => action("download/polyglot")}>Download Polyglot</Button><Button variant="outline" disabled={blocked || !config} onclick={() => action("download/swebench")}>Download SWE-bench</Button></div>
      <p class="text-sm">{download.kind ?? "Preparation"}: {download.state}{download.finished_at ? ` · ${new Date(download.finished_at * 1000).toLocaleString()}` : ""}</p>
      {#if download.error}<p class="text-destructive text-sm">{download.error}</p>{/if}
      {#if download.logs.length}<pre class="bg-muted max-h-56 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{download.logs.join("\n")}</pre>{/if}
    </section>
  </div>
  <section class="space-y-3 rounded-xl border p-5">
    <div class="flex items-center justify-between"><h2 class="font-semibold">Live progress · {run?.state ?? "idle"}</h2>{#if active}<Button variant="outline" disabled={busy || stopping} onclick={() => action("run/stop")}>{stopping ? "Stopping…" : "Stop run"}</Button>{/if}</div>
    {#if run?.totals}<progress class="w-full" max={run.totals.items_total || 1} value={run.totals.items_done}></progress><p class="text-sm">{run.totals.items_done} / {run.totals.items_total} items</p>{/if}
    {#if stopping}<p class="text-muted-foreground text-sm">Stopping at the next runner checkpoint. An active request or code test may finish first.</p>{/if}
    {#if run?.error}<p class="text-destructive">{run.error}</p>{/if}
    {#if run?.current}<p class="text-sm font-medium">{run.current.title}</p><pre class="bg-muted max-h-72 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{run.current.content || run.current.reasoning || "Waiting for model response…"}</pre>{/if}
    <a class="text-primary text-sm underline" href="/studio/intelligence/results" use:link>View results</a>
  </section>
</div>
