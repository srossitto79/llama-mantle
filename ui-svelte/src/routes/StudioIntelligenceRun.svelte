<script lang="ts">
  import { onMount } from "svelte";
  import { link } from "svelte-spa-router";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Field from "$lib/components/ui/field/index.js";
  import { NativeSelect } from "$lib/components/ui/native-select/index.js";
  import IntelligenceRunHeatmap from "../components/IntelligenceRunHeatmap.svelte";
  import IntelligenceTestPicker from "../components/IntelligenceTestPicker.svelte";
  import { profileItems, runItems } from "$lib/intelligenceSelection";
  import { shouldAutoScroll } from "$lib/scroll";
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
    allTests?: boolean; tests?: string[];
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
  let allTests = $state(cachedForm?.allTests ?? true);
  let selectedTests = $state<Set<string>>(new Set(cachedForm?.tests ?? []));
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
  let previewElement: HTMLPreElement | undefined = $state();
  let wasAtBottom = $state(true);
  let active = $derived(run?.state === "starting" || run?.state === "running");
  let blocked = $derived(busy || active || download.state === "running");
  let testsInProfile = $derived(config ? profileItems(config.items ?? [], profile) : []);
  let testsToRun = $derived(runItems(allTests, selectedTests, testsInProfile));
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

  function updatePreviewScrollState() {
    if (!previewElement) return;
    wasAtBottom = shouldAutoScroll(previewElement);
  }

  $effect(() => {
    if (previewElement && run?.current) {
      const text = run.current.content || run.current.reasoning || "Waiting for model response…";
      if (wasAtBottom) {
        previewElement.scrollTop = previewElement.scrollHeight;
      }
      if (text.length === 0) return;
    }
  });

  $effect(() => {
    saveCache(FORM_KEY, { selected, profile, useCustomTemperature, temperature, timeout, maxTokens, reasoningEffort, resume, retryFailed, retryUnsupported, allTests, tests: [...selectedTests] } satisfies FormCache);
  });
  onMount(() => {
    void loadConfig().catch(e => configError = String(e));
    void refresh();
    const timer = setInterval(() => void refresh(), 2000);
    return () => { disposed = true; clearInterval(timer); source?.close(); };
  });
  import * as Page from "$lib/components/ui/page/index.js";
</script>

<Page.Root title="Intelligence Run" width="full">
  {#if error}
    <Alert.Root variant="destructive"><Alert.Description>{error}</Alert.Description></Alert.Root>
  {/if}
  {#if configError}
    <Alert.Root variant="destructive">
      <Alert.Description>
        {configError}
        <button class="underline" onclick={() => loadConfig().catch(e => configError = String(e))}>Retry model discovery</button>
      </Alert.Description>
    </Alert.Root>
  {/if}

  <Card.Root class="gap-0 py-0">
    <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
      <h2 class="text-heading">Models and profile</h2>
    </div>
    <div class="space-y-4 p-4">
      {#if !config}<p class="text-muted-foreground text-sm">Waiting for the Intelligence service…</p>
      {:else}
        {@const profiles = config.profiles}
        <fieldset disabled={blocked} class="space-y-4">
          <div class="space-y-2">
            <legend class="text-label text-muted-foreground font-semibold uppercase">Models</legend>
            {#if config.models.length}
              <Input type="text" placeholder="Filter models…" aria-label="Filter models" class="h-8 max-w-sm" bind:value={modelFilter} />
            {/if}
            <div class="max-h-64 divide-y overflow-auto rounded-lg border">
              {#each filteredModels as model (model.id)}
                <label class="hover:bg-muted/50 flex items-center gap-2 px-3 py-2 text-sm"><input type="checkbox" class="accent-primary" bind:group={selected} value={model.id} />{model.name}</label>
              {:else}<p class="text-muted-foreground px-3 py-6 text-center text-sm">{config.models.length ? "No models match the filter." : "No models available. Configure models in Mantle, then refresh this page."}</p>{/each}
            </div>
          </div>
          <Field.Root
            label="Profile"
            id="intelligence-profile"
            description={profiles.find(p => p.id === profile)?.description}
            class="max-w-md"
          >
            {#snippet control({ id, describedBy })}
              <NativeSelect {id} aria-describedby={describedBy} bind:value={profile}>{#each profiles as p}<option value={p.id}>{p.id}</option>{/each}</NativeSelect>
            {/snippet}
          </Field.Root>
          {#if config.items?.length}
            <IntelligenceTestPicker items={testsInProfile} categories={config.categories ?? []} bind:allTests bind:selected={selectedTests} disabled={blocked} />
          {/if}
          <details open class="space-y-3">
            <summary class="text-label text-muted-foreground cursor-pointer font-semibold uppercase">Advanced settings</summary>
            <div class="mt-3 grid gap-3 sm:grid-cols-2">
              <div class="grid gap-1.5 text-sm">
                <label class="flex items-center gap-2 font-medium"><input type="checkbox" class="accent-primary" bind:checked={useCustomTemperature} />Temperature</label>
                {#if useCustomTemperature}
                  <Input type="number" min="0" max="2" step="0.1" aria-label="Temperature" class="h-8" bind:value={temperature} />
                {:else}
                  <Input type="text" value="Default" disabled aria-label="Temperature" class="h-8" />
                {/if}
              </div>
              <Field.Root label="Thinking effort" id="intelligence-effort">
                {#snippet control({ id })}
                  <NativeSelect {id} bind:value={reasoningEffort}>
                    <option value="">Model default</option>
                    {#each REASONING_EFFORTS as effort}<option value={effort}>{effort}</option>{/each}
                  </NativeSelect>
                {/snippet}
              </Field.Root>
              <Field.Root label="Timeout (seconds)" id="intelligence-timeout">
                {#snippet control({ id })}<Input {id} type="number" min="1" class="h-8" bind:value={timeout} />{/snippet}
              </Field.Root>
              <Field.Root label="Maximum tokens" id="intelligence-max-tokens">
                {#snippet control({ id })}<Input {id} type="number" min="1" class="h-8" bind:value={maxTokens} />{/snippet}
              </Field.Root>
            </div>
            <label class="mt-3 flex items-center gap-2 text-sm"><input type="checkbox" class="accent-primary" bind:checked={resume} />Reuse matching answers from the latest results</label>
            <label class="mt-2 flex items-center gap-2 text-sm"><input type="checkbox" class="accent-primary" bind:checked={retryFailed} />Retry failed items</label>
            {#if retryFailed}
              <label class="mt-2 ml-6 flex items-center gap-2 text-sm"><input type="checkbox" class="accent-primary" bind:checked={retryUnsupported} />Also retry unsupported items</label>
            {/if}
          </details>
        </fieldset>
        <Button size="sm" disabled={blocked || !selected.length || (testsToRun !== undefined && testsToRun.length === 0) || !(timeout > 0) || !(maxTokens > 0)} onclick={() => action("run", { models: selected, profile, items: testsToRun, temperature: useCustomTemperature ? temperature : false, reasoning_effort: reasoningEffort, timeout, max_tokens: maxTokens, only_missing: resume, retry_failed: retryFailed, retry_unsupported: retryFailed && retryUnsupported, stream: true })}>Start run</Button>
      {/if}
    </div>
  </Card.Root>

  <Card.Root class="gap-0 py-0">
    <details>
      <summary class="bg-(--panel-header-bg) text-heading cursor-pointer px-4 py-2.5">External datasets</summary>
      <div class="space-y-4 border-t p-4">
        <div class="divide-y rounded-lg border">
          {#each download.suites ?? [] as suite (suite.kind)}
            <div class="flex flex-wrap items-center gap-3 px-3 py-2">
              <Button variant="outline" size="sm" disabled={blocked || !config} onclick={() => action(`download/${suite.kind}`)}>Download {suiteLabel(suite.kind)}</Button>
              <span class="text-muted-foreground text-sm">{suite.cached ? "Cached" : "Not downloaded"}</span>
            </div>
          {:else}
            <p class="text-muted-foreground px-3 py-6 text-center text-sm">No downloadable datasets.</p>
          {/each}
        </div>
        <p class="text-sm">{download.kind ?? "Preparation"}: {download.state}{download.finished_at ? ` · ${new Date(download.finished_at * 1000).toLocaleString()}` : ""}</p>
        {#if download.error}<p class="text-destructive text-sm">{download.error}</p>{/if}
        {#if download.logs.length}<pre class="bg-muted max-h-56 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap">{download.logs.join("\n")}</pre>{/if}
      </div>
    </details>
  </Card.Root>

  <Card.Root class="gap-0 py-0">
    <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
      <h2 class="text-heading">Live progress · {run?.state ?? "idle"}</h2>
      <div class="ml-auto flex items-center gap-2">
        {#if active}<Button variant="outline" size="sm" disabled={busy || stopping} onclick={() => action("run/stop")}>{stopping ? "Stopping…" : "Stop run"}</Button>{/if}
        <a class="text-primary text-sm underline" href="/studio/intelligence/results" use:link>View results</a>
      </div>
    </div>
    <div class="space-y-3 p-4">
      {#if run?.totals}
        <progress class="accent-primary h-2 w-full" max={run.totals.items_total || 1} value={run.totals.items_done}></progress>
        <p class="text-sm tabular-nums">{run.totals.items_done} / {run.totals.items_total} items</p>
      {/if}
      {#if run?.models?.length}<IntelligenceRunHeatmap {run} />{/if}
      <!-- Stopping happens at the next runner checkpoint; an active request or code test may finish first. -->
      {#if stopping}<p class="text-muted-foreground text-sm">Stopping</p>{/if}
      {#if run?.error}<p class="text-destructive text-sm">{run.error}</p>{/if}
      {#if run?.current}
        <p class="text-sm font-medium">{run.current.title}</p>
        <pre
          bind:this={previewElement}
          onscroll={updatePreviewScrollState}
          class="bg-muted max-h-72 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap"
        >{run.current.content || run.current.reasoning || "Waiting for model response…"}</pre>
      {/if}
    </div>
  </Card.Root>
</Page.Root>
