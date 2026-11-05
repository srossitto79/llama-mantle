<script lang="ts">
  import { playgroundModels, selectorModels } from "../../stores/api";
  import { connectionState } from "../../stores/theme";
  import { persistentStore } from "../../stores/persistent";
  import { streamChatCompletion } from "../../lib/chatApi";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import * as Field from "$lib/components/ui/field/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { ChevronDown, GripVertical, Play, Plus, Square, X } from "@lucide/svelte";

  type Status = "waiting" | "streaming" | "done" | "error";
  type Phase = "waiting" | "loading" | "reasoning" | "content";
  type RunState = {
    status: Status;
    loadingText: string;
    reasoningContent: string;
    content: string;
    loadingDone: boolean;
    waitingMs: number;
    loadingMs: number;
    reasoningMs: number;
    contentMs: number;
    phase: Phase;
    elapsedMs: number;
    completionTokens?: number;
    error?: string;
  };
  type TestEntry = { id: string; model: string };

  const LOAD_MARKER = "━━━━━";

  const DEFAULT_PROMPT = "Write a few sentences about the history of computing.";
  const DEFAULT_MAX_TOKENS = 256;

  const promptStore = persistentStore<string>("concurrency-prompt", DEFAULT_PROMPT);
  const maxTokensStore = persistentStore<number>("concurrency-max-tokens", DEFAULT_MAX_TOKENS);
  const testListStore = persistentStore<TestEntry[]>("concurrency-test-list", []);

  let runs = $state<Record<string, RunState>>({});
  let isRunning = $state(false);
  let abortController: AbortController | null = null;
  let dragIndex = $state<number | null>(null);
  let dragOverIndex = $state<number | null>(null);

  const timelineCollapsedStore = persistentStore<boolean>("concurrency-timeline-collapsed", false);

  let timelineMaxMs = $derived(Math.max(100, ...Object.values(runs).map((r) => r.elapsedMs)));

  let availableModels = $derived($playgroundModels.filter((model) => model.playgroundType !== "selector"));
  let modeSections = $derived([
    { label: "Selectors", ids: $selectorModels.map((model) => model.id) },
    { label: "Models", ids: availableModels.map((model) => model.id) },
  ].filter((section) => section.ids.length > 0));
  let hasModels = $derived(modeSections.length > 0);
  let canRun = $derived(!isRunning && $testListStore.length > 0 && $promptStore.trim() !== "");

  function newId(): string {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
      return crypto.randomUUID();
    }
    return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  }

  function addModel(modelId: string) {
    if (isRunning) return;
    testListStore.update((list) => [...list, { id: newId(), model: modelId }]);
  }

  function removeEntry(id: string) {
    if (isRunning) return;
    testListStore.update((list) => list.filter((e) => e.id !== id));
    const next = { ...runs };
    delete next[id];
    runs = next;
  }

  function clearAll() {
    if (isRunning) return;
    testListStore.set([]);
    runs = {};
  }

  function onDragStart(i: number, e: DragEvent) {
    if (isRunning) return;
    dragIndex = i;
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", String(i));
    }
  }

  function onDragOver(i: number, e: DragEvent) {
    if (isRunning || dragIndex === null) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dragOverIndex = i;
  }

  function onDrop(i: number, e: DragEvent) {
    if (isRunning || dragIndex === null) return;
    e.preventDefault();
    const from = dragIndex;
    const to = i;
    dragIndex = null;
    dragOverIndex = null;
    if (from === to) return;
    testListStore.update((list) => {
      const next = [...list];
      const [moved] = next.splice(from, 1);
      next.splice(to, 0, moved);
      return next;
    });
  }

  function onDragEnd() {
    dragIndex = null;
    dragOverIndex = null;
  }

  function emptyRun(): RunState {
    return {
      status: "waiting",
      loadingText: "",
      reasoningContent: "",
      content: "",
      loadingDone: false,
      waitingMs: 0,
      loadingMs: 0,
      reasoningMs: 0,
      contentMs: 0,
      phase: "waiting",
      elapsedMs: 0,
    };
  }

  // Detect and split the llama-swap loading block (wrapped in ━━━━━ markers,
  // delivered as reasoning_content) from the model's own reasoning tokens.
  function ingestReasoning(
    prev: RunState,
    chunk: string
  ): { loadingText: string; reasoningContent: string; loadingDone: boolean; nowPhase: Phase } {
    if (prev.loadingDone) {
      return {
        loadingText: prev.loadingText,
        reasoningContent: prev.reasoningContent + chunk,
        loadingDone: true,
        nowPhase: "reasoning",
      };
    }

    const combined = prev.loadingText + chunk;
    // Not enough to decide whether this is a loading marker
    if (combined.length < LOAD_MARKER.length) {
      if (LOAD_MARKER.startsWith(combined)) {
        return { loadingText: combined, reasoningContent: prev.reasoningContent, loadingDone: false, nowPhase: "loading" };
      }
      return {
        loadingText: "",
        reasoningContent: prev.reasoningContent + combined,
        loadingDone: true,
        nowPhase: "reasoning",
      };
    }

    if (!combined.startsWith(LOAD_MARKER)) {
      return {
        loadingText: "",
        reasoningContent: prev.reasoningContent + combined,
        loadingDone: true,
        nowPhase: "reasoning",
      };
    }

    // We're inside a loading block — look for the closing marker
    const closingIdx = combined.indexOf(LOAD_MARKER, LOAD_MARKER.length);
    if (closingIdx < 0) {
      return { loadingText: combined, reasoningContent: prev.reasoningContent, loadingDone: false, nowPhase: "loading" };
    }
    const newlineIdx = combined.indexOf("\n", closingIdx);
    const sliceEnd = newlineIdx >= 0 ? newlineIdx + 1 : combined.length;
    const loadingPart = combined.substring(0, sliceEnd);
    // Strip the trailing " \n" the loader sends after the closing marker
    const remainder = combined.substring(sliceEnd).replace(/^[ \t]*\n?/, "");
    return {
      loadingText: loadingPart,
      reasoningContent: prev.reasoningContent + remainder,
      loadingDone: true,
      nowPhase: remainder ? "reasoning" : "waiting",
    };
  }

  async function runOne(entry: TestEntry, signal: AbortSignal) {
    const start = performance.now();
    let phaseStart = start;
    runs[entry.id] = { ...emptyRun(), status: "streaming" };

    const accrue = (
      prev: RunState,
      now: number
    ): { waitingMs: number; loadingMs: number; reasoningMs: number; contentMs: number } => {
      const delta = now - phaseStart;
      const base = {
        waitingMs: prev.waitingMs,
        loadingMs: prev.loadingMs,
        reasoningMs: prev.reasoningMs,
        contentMs: prev.contentMs,
      };
      if (prev.phase === "waiting") return { ...base, waitingMs: base.waitingMs + delta };
      if (prev.phase === "loading") return { ...base, loadingMs: base.loadingMs + delta };
      if (prev.phase === "reasoning") return { ...base, reasoningMs: base.reasoningMs + delta };
      if (prev.phase === "content") return { ...base, contentMs: base.contentMs + delta };
      return base;
    };

    const ticker = window.setInterval(() => {
      const prev = runs[entry.id];
      if (!prev || prev.status !== "streaming") return;
      const now = performance.now();
      const accrued = accrue(prev, now);
      phaseStart = now;
      runs[entry.id] = { ...prev, ...accrued, elapsedMs: now - start };
    }, 50);

    try {
      const stream = streamChatCompletion(entry.model, [{ role: "user", content: $promptStore }], signal, {
        endpoint: "v1/chat/completions",
        max_tokens: $maxTokensStore,
      });
      for await (const chunk of stream) {
        if (chunk.done) break;
        const prev = runs[entry.id];
        if (!prev) break;
        const now = performance.now();
        const accrued = accrue(prev, now);
        phaseStart = now;

        let nextPhase: Phase = prev.phase;
        let loadingText = prev.loadingText;
        let reasoningContent = prev.reasoningContent;
        let loadingDone = prev.loadingDone;

        if (chunk.reasoning_content) {
          const parsed = ingestReasoning(prev, chunk.reasoning_content);
          loadingText = parsed.loadingText;
          reasoningContent = parsed.reasoningContent;
          loadingDone = parsed.loadingDone;
          nextPhase = parsed.nowPhase;
        }
        if (chunk.content) nextPhase = "content";

        runs[entry.id] = {
          ...prev,
          ...accrued,
          loadingText,
          reasoningContent,
          content: prev.content + (chunk.content ?? ""),
          loadingDone,
          phase: nextPhase,
          elapsedMs: now - start,
          completionTokens: chunk.completionTokens ?? prev.completionTokens,
        };
      }
      const prev = runs[entry.id];
      if (prev) {
        const now = performance.now();
        const accrued = accrue(prev, now);
        runs[entry.id] = { ...prev, ...accrued, status: "done", elapsedMs: now - start };
      }
    } catch (err) {
      const prev = runs[entry.id] ?? emptyRun();
      const now = performance.now();
      const accrued = accrue(prev, now);
      const aborted = err instanceof Error && err.name === "AbortError";
      runs[entry.id] = {
        ...prev,
        ...accrued,
        status: "error",
        elapsedMs: now - start,
        error: aborted ? "aborted" : err instanceof Error ? err.message : String(err),
      };
    } finally {
      window.clearInterval(ticker);
    }
  }

  async function run() {
    if (!canRun) return;
    const entries = $testListStore;
    const initial: Record<string, RunState> = {};
    for (const e of entries) {
      initial[e.id] = emptyRun();
    }
    runs = initial;
    isRunning = true;
    abortController = new AbortController();
    try {
      await Promise.allSettled(entries.map((e) => runOne(e, abortController!.signal)));
    } finally {
      isRunning = false;
      abortController = null;
    }
  }

  function stop() {
    abortController?.abort();
  }

  function waitingBarClass(run: RunState): string {
    if (run.status === "error" && run.phase === "waiting") return "bg-destructive";
    return "bg-muted";
  }

  function loadingBarClass(run: RunState): string {
    if (run.status === "error" && run.phase === "loading") return "bg-destructive";
    return "bg-muted-foreground/50";
  }

  function reasoningBarClass(run: RunState): string {
    if (run.status === "error" && run.phase === "reasoning") return "bg-destructive";
    return "bg-chart-1";
  }

  function contentBarClass(run: RunState): string {
    if (run.status === "error" && run.phase === "content") return "bg-destructive";
    if (run.status === "done") return "bg-success";
    return "bg-chart-5";
  }

  function niceStepMs(maxMs: number): number {
    if (maxMs <= 500) return 100;
    if (maxMs <= 2000) return 500;
    if (maxMs <= 5000) return 1000;
    if (maxMs <= 20000) return 5000;
    if (maxMs <= 60000) return 10000;
    return 30000;
  }

  function formatTickMs(ms: number): string {
    if (ms < 1000) return `${ms}`;
    return `${(ms / 1000).toFixed(ms % 1000 === 0 ? 0 : 1)}s`;
  }

  let timelineTicks = $derived.by(() => {
    const step = niceStepMs(timelineMaxMs);
    const ticks: number[] = [];
    for (let t = 0; t <= timelineMaxMs; t += step) ticks.push(t);
    return ticks;
  });

  function statusBadgeClass(status: Status): string {
    switch (status) {
      case "waiting":
        return "bg-muted text-muted-foreground";
      case "streaming":
        return "bg-warning/20 text-foreground";
      case "done":
        return "bg-success/20 text-foreground";
      case "error":
        return "bg-destructive/20 text-foreground";
    }
  }

  function formatElapsed(ms: number): string {
    if (ms < 1000) return `${Math.round(ms)}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  }

  function resetDefaults() {
    promptStore.set(DEFAULT_PROMPT);
    maxTokensStore.set(DEFAULT_MAX_TOKENS);
  }
</script>

<!--
  Load test: queue models on the left (click a model to queue it; queue the same
  model more than once to send parallel requests to it), then run all requests
  at once. Each request streams into its own result card and timeline row; drag
  a card header to reorder.
-->
<div class="flex h-full min-h-0 flex-col gap-3 md:flex-row">
  <!-- Left panel: run controls, model picker, settings -->
  <div class="flex min-h-0 shrink-0 flex-col overflow-hidden rounded-lg border md:w-72">
    <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5">
      {#if isRunning}
        <Button variant="destructive" size="sm" onclick={stop}>
          <Square class="fill-current" />Stop
        </Button>
      {:else}
        <Button
          size="sm"
          onclick={run}
          disabled={!canRun}
          title={$testListStore.length === 0 ? "Add models from the list" : "Run concurrent requests"}
        >
          <Play class="fill-current" />Go
        </Button>
      {/if}
      <Button variant="outline" size="sm" onclick={clearAll} disabled={isRunning || $testListStore.length === 0}>
        Clear ({$testListStore.length})
      </Button>
    </div>

    <!-- Available models -->
    <h3 class="text-label text-muted-foreground border-b px-4 py-2 font-semibold uppercase">Models</h3>
    <div class="max-h-64 min-h-0 flex-1 overflow-y-auto md:max-h-none">
      {#if $connectionState !== "connected"}
        <div class="text-muted-foreground p-4 text-center text-sm">Connecting…</div>
      {:else if !hasModels}
        <div class="text-muted-foreground p-4 text-center text-sm">No models configured</div>
      {:else}
        {#each modeSections as section (section.label)}
          <div>
            <div class="bg-muted/50 text-label text-muted-foreground border-b px-4 py-1 font-semibold uppercase">
              {section.label}
            </div>
            <div class="divide-y border-b last:border-b-0">
              {#each section.ids as id (id)}
                <button
                  type="button"
                  class="hover:bg-muted/50 flex w-full items-center gap-2 px-4 py-2 text-left font-mono text-[0.8125rem] transition-colors disabled:pointer-events-none disabled:opacity-50"
                  onclick={() => addModel(id)}
                  disabled={isRunning}
                  title="Add {id}"
                >
                  <Plus class="text-primary size-3.5 shrink-0" aria-hidden="true" />
                  <span class="flex-1 truncate">{id}</span>
                </button>
              {/each}
            </div>
          </div>
        {/each}
      {/if}
    </div>

    <!-- Settings -->
    <div class="flex flex-col gap-3 border-t p-4">
      <Field.Root label="Prompt">
        {#snippet labelExtra()}
          <Button
            variant="link"
            size="xs"
            class="ml-auto h-auto p-0"
            onclick={resetDefaults}
            disabled={isRunning}
          >
            Reset
          </Button>
        {/snippet}
        {#snippet control({ id })}
          <Textarea {id} class="resize-none" rows={3} bind:value={$promptStore} disabled={isRunning} />
        {/snippet}
      </Field.Root>
      <Field.Root label="max_tokens">
        {#snippet control({ id })}
          <Input {id} type="number" min="1" class="font-mono" bind:value={$maxTokensStore} disabled={isRunning} />
        {/snippet}
      </Field.Root>
    </div>
  </div>

  <!-- Right: results (cards reorder by drag) -->
  <div class="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-y-auto">
    {#if $testListStore.length === 0}
      <EmptyState
        title="No models queued"
        description="Select models to queue. Add a model more than once for parallel requests."
        class="rounded-lg border"
      />
    {:else}
      <!-- Timeline -->
      <div class="shrink-0 overflow-hidden rounded-lg border">
        <button
          class="bg-(--panel-header-bg) hover:bg-muted/50 flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 text-left transition-colors {$timelineCollapsedStore ? '' : 'border-b'}"
          onclick={() => timelineCollapsedStore.update((v) => !v)}
          aria-expanded={!$timelineCollapsedStore}
        >
          <ChevronDown class="text-muted-foreground size-4 transition-transform {$timelineCollapsedStore ? '-rotate-90' : ''}" aria-hidden="true" />
          <span class="text-heading">Timeline</span>
          {#if !$timelineCollapsedStore}
            <span class="text-muted-foreground flex flex-wrap items-center gap-3 text-[0.6875rem]" aria-hidden="true">
              <span class="flex items-center gap-1"><span class="bg-muted border-border inline-block size-2.5 rounded-sm border"></span>waiting</span>
              <span class="flex items-center gap-1"><span class="bg-muted-foreground/50 inline-block size-2.5 rounded-sm"></span>loading</span>
              <span class="flex items-center gap-1"><span class="bg-chart-1 inline-block size-2.5 rounded-sm"></span>reasoning</span>
              <span class="flex items-center gap-1"><span class="bg-chart-5 inline-block size-2.5 rounded-sm"></span>streaming</span>
              <span class="flex items-center gap-1"><span class="bg-success inline-block size-2.5 rounded-sm"></span>done</span>
              <span class="flex items-center gap-1"><span class="bg-destructive inline-block size-2.5 rounded-sm"></span>error</span>
            </span>
          {/if}
          <span class="text-muted-foreground ml-auto text-xs tabular-nums">
            max {formatElapsed(timelineMaxMs)} · {$testListStore.length} request{$testListStore.length === 1 ? "" : "s"}
          </span>
        </button>
        {#if !$timelineCollapsedStore}
          <div class="overflow-x-auto px-4 py-3">
            <div class="min-w-[32rem]">
              <!-- X axis ticks -->
              <div class="flex" aria-hidden="true">
                <div class="w-40 shrink-0"></div>
                <div class="border-border relative h-4 flex-1 border-b">
                  {#each timelineTicks as t (t)}
                    <div
                      class="border-border absolute top-0 bottom-0 border-l"
                      style="left: {(t / timelineMaxMs) * 100}%;"
                    >
                      <span class="text-muted-foreground absolute -top-0.5 left-1 text-[0.625rem] tabular-nums">{formatTickMs(t)}</span>
                    </div>
                  {/each}
                </div>
                <div class="w-16 shrink-0"></div>
              </div>
              <!-- Bars -->
              <div class="mt-1 flex flex-col gap-1">
                {#each $testListStore as entry, i (entry.id)}
                  {@const run = runs[entry.id]}
                  {@const waitingPct = run ? (run.waitingMs / timelineMaxMs) * 100 : 0}
                  {@const loadingPct = run ? (run.loadingMs / timelineMaxMs) * 100 : 0}
                  {@const reasoningPct = run ? (run.reasoningMs / timelineMaxMs) * 100 : 0}
                  {@const contentPct = run ? (run.contentMs / timelineMaxMs) * 100 : 0}
                  <div class="flex items-center text-xs">
                    <div class="text-muted-foreground flex w-40 shrink-0 items-center gap-1 pr-2">
                      <span class="w-5 text-right tabular-nums">{i + 1}.</span>
                      <span class="truncate font-mono" title={entry.model}>{entry.model}</span>
                    </div>
                    <div class="relative h-4 flex-1">
                      {#each timelineTicks as t (t)}
                        <div
                          class="border-border absolute top-0 bottom-0 border-l"
                          style="left: {(t / timelineMaxMs) * 100}%;"
                          aria-hidden="true"
                        ></div>
                      {/each}
                      {#if run && run.waitingMs > 0}
                        <div
                          class="absolute top-0.5 bottom-0.5 rounded-l-sm transition-all {waitingBarClass(run)}"
                          style="left: 0; width: {waitingPct}%;"
                          title="waiting {formatElapsed(run.waitingMs)}"
                        ></div>
                      {/if}
                      {#if run && run.loadingMs > 0}
                        <div
                          class="absolute top-0.5 bottom-0.5 transition-all {loadingBarClass(run)} {run.waitingMs === 0 ? 'rounded-l-sm' : ''}"
                          style="left: {waitingPct}%; width: {loadingPct}%;"
                          title="loading {formatElapsed(run.loadingMs)}"
                        ></div>
                      {/if}
                      {#if run && run.reasoningMs > 0}
                        <div
                          class="absolute top-0.5 bottom-0.5 transition-all {reasoningBarClass(run)} {run.waitingMs === 0 && run.loadingMs === 0 ? 'rounded-l-sm' : ''}"
                          style="left: {waitingPct + loadingPct}%; width: {reasoningPct}%;"
                          title="reasoning {formatElapsed(run.reasoningMs)}"
                        ></div>
                      {/if}
                      {#if run && run.contentMs > 0}
                        <div
                          class="absolute top-0.5 bottom-0.5 transition-all {contentBarClass(run)} {run.waitingMs === 0 && run.loadingMs === 0 && run.reasoningMs === 0 ? 'rounded-l-sm' : ''} {run.status === 'done' || run.status === 'error' ? 'rounded-r-sm' : ''}"
                          style="left: {waitingPct + loadingPct + reasoningPct}%; width: {contentPct}%;"
                          title="content {formatElapsed(run.contentMs)}"
                        ></div>
                      {/if}
                    </div>
                    <div class="text-muted-foreground w-16 shrink-0 pl-2 text-right tabular-nums">
                      {run ? formatElapsed(run.elapsedMs) : "—"}
                    </div>
                  </div>
                {/each}
              </div>
            </div>
          </div>
        {/if}
      </div>

      <!-- Result cards -->
      <div class="grid grid-cols-1 gap-3 lg:grid-cols-2 xl:grid-cols-3" role="list">
        {#each $testListStore as entry, i (entry.id)}
          {@const run = runs[entry.id]}
          {@const status = run?.status ?? "waiting"}
          <div
            class="flex min-h-0 flex-col overflow-hidden rounded-lg border transition-colors {dragOverIndex === i && dragIndex !== i
              ? 'border-primary ring-primary/40 ring-2'
              : ''} {dragIndex === i ? 'opacity-40' : ''}"
            style="height: 280px;"
            role="listitem"
            ondragover={(e) => onDragOver(i, e)}
            ondrop={(e) => onDrop(i, e)}
          >
            <div
              class="bg-(--panel-header-bg) flex shrink-0 items-center gap-2 border-b px-3 py-2"
              draggable={!isRunning}
              role="button"
              tabindex="-1"
              aria-label="Drag to reorder {entry.model}"
              ondragstart={(e) => onDragStart(i, e)}
              ondragend={onDragEnd}
              class:cursor-grab={!isRunning}
              title={isRunning ? "" : "Drag to reorder"}
            >
              <GripVertical class="text-muted-foreground size-4 shrink-0" aria-hidden="true" />
              <span class="text-muted-foreground w-5 text-right text-xs tabular-nums">{i + 1}.</span>
              <span class="flex-1 truncate font-mono text-[0.8125rem]" title={entry.model}>{entry.model}</span>
              <span class="text-muted-foreground text-xs tabular-nums">
                {run ? formatElapsed(run.elapsedMs) : "—"}
              </span>
              <span class="rounded-full px-2 py-0.5 text-[0.6875rem] {statusBadgeClass(status)}">{status}</span>
              <Button
                variant="ghost"
                size="icon-xs"
                class="text-muted-foreground hover:text-destructive"
                onclick={() => removeEntry(entry.id)}
                disabled={isRunning}
                aria-label="Remove"
                tabindex={-1}
              >
                <X />
              </Button>
            </div>
            <div class="min-h-0 flex-1 overflow-y-auto px-3 py-2 font-mono text-xs">
              {#if run?.loadingText}
                <div class="bg-muted/50 text-muted-foreground mb-2 rounded-md px-2 py-1 whitespace-pre-wrap">{run.loadingText.trim()}</div>
              {/if}
              {#if run?.reasoningContent}
                <div class="text-chart-1 whitespace-pre-wrap">{run.reasoningContent}</div>
              {/if}
              {#if run?.content}
                <div class="whitespace-pre-wrap {run.reasoningContent ? 'mt-2' : ''}">{run.content}</div>
              {/if}
              {#if run?.status === "error" && run?.error}
                <div class="text-destructive mt-2">[error] {run.error}</div>
              {/if}
            </div>
            {#if run?.status === "done"}
              {@const ttftMs = (run.waitingMs) + (run.loadingMs) + (run.reasoningMs)}
              {@const tps = run.completionTokens && run.contentMs > 0 ? run.completionTokens / (run.contentMs / 1000) : 0}
              <div class="text-muted-foreground flex shrink-0 items-center gap-3 border-t px-3 py-1.5 text-[0.6875rem] tabular-nums">
                {#if run.loadingMs > 0}
                  <span title="Model load time">load {formatElapsed(run.loadingMs)}</span>
                {/if}
                <span title="Time to first content token">TTFT {formatElapsed(ttftMs)}</span>
                {#if tps > 0}
                  <span title="Generation speed">{tps.toFixed(1)} tk/s</span>
                {/if}
                {#if run.completionTokens}
                  <span class="ml-auto">{run.completionTokens} tok</span>
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
