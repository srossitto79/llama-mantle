<script lang="ts">
  // Tune: analyse a model file and stage the picked llama-server entries into
  // the config draft. Nothing is written here; the review sheet applies them.
  import { configDraft } from "$lib/config/draft";
  import { configSession } from "$lib/config/session";
  import { tunerApi, tunableModels, type Analysis, type CacheTuneTask, type Candidate, type LocalModel } from "$lib/config/tunerApi";
  import * as Sheet from "$lib/components/ui/sheet/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Field from "$lib/components/ui/field/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Checkbox } from "$lib/components/ui/checkbox/index.js";
  import { Label } from "$lib/components/ui/label/index.js";
  import { NativeSelect } from "$lib/components/ui/native-select/index.js";
  import TierTable from "./TierTable.svelte";
  import { Loader2, CircleAlert } from "@lucide/svelte";

  let { open = $bindable(false), onstaged }: { open?: boolean; onstaged?: () => void } = $props();

  const TIERS = [65536, 131072, 262144];

  let files = $state<LocalModel[]>([]);
  let model = $state("");
  let draft = $state("");
  let tiers = $state<Record<number, boolean>>({ 65536: true, 131072: true, 262144: true });
  let analysis = $state<Analysis | null>(null);
  let picks = $state<Record<number, string | null>>({});
  let busy = $state(false);
  let error = $state<string | null>(null);

  // Load the file list each time the sheet opens.
  $effect(() => {
    if (!open) return;
    tunerApi
      .localModels()
      .then((list) => {
        files = tunableModels(list);
        if (!model && files.length) model = files[0].path;
      })
      .catch((e) => (error = e instanceof Error ? e.message : String(e)));
  });

  const existing = $derived(new Set(($configSession.snapshot?.source.sections.models ?? []).map((e) => e.id)));
  const staged = $derived(new Set($configDraft.filter((it) => it.change.op === "add" && it.change.section === "models").map((it) => it.change.id)));
  const plans = $derived(analysis?.tiers ?? []);
  const selectedTiers = $derived(TIERS.filter((t) => tiers[t]));

  /** Problem text by candidate id, for entry ids already in the config or the draft. */
  const problems = $derived.by(() => {
    const out: Record<string, string> = {};
    for (const plan of plans) {
      for (const c of plan.candidates ?? []) {
        if (c.entry && (existing.has(c.entry.id) || staged.has(c.entry.id))) out[c.id] = `${c.entry.id} exists`;
      }
    }
    return out;
  });

  const pickedCandidates = $derived(
    plans.flatMap((p) => {
      const c = (p.candidates ?? []).find((x) => x.id === picks[p.tier]);
      return c ? [c] : [];
    }),
  );
  const canStage = $derived(pickedCandidates.length > 0 && pickedCandidates.every((c) => !problems[c.id]));

  async function analyse(): Promise<void> {
    busy = true;
    error = null;
    analysis = null;
    try {
      const a = await tunerApi.analyse({ model, draft: draft || undefined, tiers: selectedTiers });
      analysis = a;
      // Default pick: the first candidate that can be staged.
      picks = Object.fromEntries((a.tiers ?? []).map((p) => [p.tier, (p.candidates ?? []).find((c) => !c.blocked && c.entry)?.id ?? null]));
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  // Cache tune: one at a time, polled until it ends. The server keeps it
  // running if the sheet closes.
  let tuneTask = $state<CacheTuneTask | null>(null);
  const tuning = $derived(tuneTask !== null && (tuneTask.state === "running" || tuneTask.state === "queued"));
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  $effect(() => () => clearTimeout(pollTimer));

  function replaceCandidate(c: Candidate): void {
    if (!analysis) return;
    for (const plan of analysis.tiers ?? []) {
      const i = (plan.candidates ?? []).findIndex((x) => x.id === c.id);
      if (i >= 0) plan.candidates![i] = c;
    }
  }

  async function poll(id: string): Promise<void> {
    try {
      const t = await tunerApi.task(id);
      tuneTask = t;
      if (t.state === "completed") {
        const c = t.parameters?.candidate;
        if (c) replaceCandidate(c);
        return;
      }
      if (t.state === "failed" || t.state === "cancelled") return;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
    pollTimer = setTimeout(() => poll(id), 2000);
  }

  async function tuneCache(id: string): Promise<void> {
    error = null;
    try {
      tuneTask = await tunerApi.startCacheTune(id);
      pollTimer = setTimeout(() => poll(tuneTask!.id), 2000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function cancelTune(): Promise<void> {
    if (!tuneTask) return;
    clearTimeout(pollTimer);
    try {
      await tunerApi.cancelCacheTune(tuneTask.id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
    await poll(tuneTask.id);
  }

  function stage(): void {
    for (const c of pickedCandidates) {
      if (c.entry) configDraft.add(c.entry.section, c.entry.id, c.entry.value);
    }
    open = false;
    onstaged?.();
  }

  const gib = (bytes: number): string => (bytes / 1024 ** 3).toFixed(1);
  const mibGiB = (mib: number): string => (mib / 1024).toFixed(1);
  const featureList = (f: Record<string, boolean>): string =>
    Object.entries(f)
      .filter(([, on]) => on)
      .map(([k]) => k)
      .join(", ");
</script>

{#snippet fact(label: string, value: string)}
  <div class="flex justify-between gap-3"><dt class="text-muted-foreground">{label}</dt><dd class="text-right">{value}</dd></div>
{/snippet}

<Sheet.Root bind:open>
  <Sheet.Content side="right" class="flex w-full flex-col gap-0 p-0 sm:max-w-4xl">
    <Sheet.Header class="border-b px-5 py-4">
      <Sheet.Title>Tune</Sheet.Title>
      <Sheet.Description>Proposed llama-server entries for a model file</Sheet.Description>
    </Sheet.Header>

    <div class="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <Field.Root label="Model" required>
          {#snippet control({ id })}
            <NativeSelect {id} bind:value={model} class="font-mono">
              {#each files as f (f.path)}<option value={f.path}>{f.name}</option>{/each}
            </NativeSelect>
          {/snippet}
        </Field.Root>
        <Field.Root label="Draft">
          {#snippet control({ id })}
            <NativeSelect {id} bind:value={draft} class="font-mono">
              <option value="">None</option>
              {#each files as f (f.path)}<option value={f.path}>{f.name}</option>{/each}
            </NativeSelect>
          {/snippet}
        </Field.Root>
      </div>
      <div class="flex flex-wrap items-center gap-4">
        {#each TIERS as t (t)}
          <div class="flex items-center gap-2">
            <Checkbox id="tune-tier-{t}" bind:checked={tiers[t]} />
            <Label for="tune-tier-{t}">{t / 1024}K</Label>
          </div>
        {/each}
        <Button class="ml-auto" disabled={busy || tuning || !model || selectedTiers.length === 0} onclick={analyse}>
          {#if busy}<Loader2 class="animate-spin" />{/if}Analyse
        </Button>
      </div>

      {#if error}
        <Alert.Root variant="destructive">
          <CircleAlert />
          <Alert.Title>{error}</Alert.Title>
        </Alert.Root>
      {/if}

      {#if analysis}
        {@const gpu = analysis.system.gpus?.[0]}
        {@const m = analysis.model}
        <div class="grid gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
          <dl class="space-y-0.5">
            {@render fact("GPU", gpu?.name ?? "none")}
            {#if gpu}{@render fact("VRAM", `${mibGiB(gpu.totalMiB)} GiB total, ${mibGiB(gpu.freeMiB)} GiB free`)}{/if}
            {@render fact("RAM", `${mibGiB(analysis.system.ramMiB)} GiB`)}
            {@render fact("Threads", String(analysis.system.threads))}
          </dl>
          <dl class="space-y-0.5">
            {@render fact("Architecture", m.arch)}
            {@render fact("Layers", String(m.layers))}
            {@render fact("MoE", m.expertCount > 0 ? `yes, ${m.expertCount} experts` : "no")}
            {@render fact("Size", `${gib(m.totalBytes)} GiB`)}
            {#if m.expertBytes > 0}{@render fact("Experts", `${gib(m.expertBytes)} GiB`)}{/if}
            {@render fact("Trained context", String(m.trainedContext))}
            {@render fact("NextN layers", String(m.nextnLayers))}
          </dl>
        </div>
        {#each analysis.system.warnings ?? [] as w (w)}
          <p class="text-muted-foreground text-sm">{w}</p>
        {/each}

        <section class="space-y-1.5">
          <h3 class="text-label text-muted-foreground font-semibold uppercase">Backends</h3>
          <ul class="divide-y rounded-(--panel-radius) border text-sm">
            {#each analysis.backends ?? [] as b (b.name)}
              <li class={["px-3 py-1.5", b.blocked && "text-muted-foreground"]}>
                <span class="font-mono">{b.name}</span>
                <span class="text-muted-foreground"> · {b.blocked ?? featureList(b.features)}</span>
                {#if b.draft}<span class="text-muted-foreground"> · draft {b.draft.ok ? "compatible" : (b.draft.reason ?? "incompatible")}</span>{/if}
              </li>
            {/each}
          </ul>
        </section>

        <p class="text-muted-foreground text-sm">VRAM projected by llama.cpp. Speed not measured.</p>
        {#if tuneTask}
          <div class="space-y-1.5 rounded-(--panel-radius) border px-3 py-2 text-sm">
            <div class="flex items-center gap-3">
              {#if tuning}<Loader2 class="size-4 animate-spin" />{/if}
              <span class="font-semibold">Cache tune · {tuneTask.state}</span>
              <span class="text-muted-foreground tabular-nums">{tuneTask.pct}%</span>
              {#if tuning}<Button size="sm" variant="outline" class="ml-auto" onclick={cancelTune}>Cancel</Button>{/if}
            </div>
            <p class={tuneTask.state === "failed" ? "text-destructive" : "text-muted-foreground"}>{tuneTask.message}</p>
            {#if tuning}<p class="text-muted-foreground">Local model requests wait until the tune ends.</p>{/if}
          </div>
        {/if}
        {#each plans as plan (plan.tier)}
          <TierTable {plan} bind:picked={picks[plan.tier]} {problems} ontune={tuneCache} tuneDisabled={tuning || busy} />
        {/each}
      {/if}
    </div>

    <Sheet.Footer class="flex-row justify-end gap-2 border-t px-5 py-3">
      <Button variant="outline" onclick={() => (open = false)}>Close</Button>
      <Button disabled={!canStage} onclick={stage}>Stage entries{#if pickedCandidates.length}&nbsp;({pickedCandidates.length}){/if}</Button>
    </Sheet.Footer>
  </Sheet.Content>
</Sheet.Root>
