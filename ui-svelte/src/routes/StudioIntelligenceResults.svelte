<script lang="ts">
  import { onMount } from "svelte";
  import { Trash2 } from "@lucide/svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Badge, type BadgeVariant } from "$lib/components/ui/badge/index.js";
  import IntelligenceScoreBar from "../components/IntelligenceScoreBar.svelte";
  import IntelligenceCategoryRadar from "../components/IntelligenceCategoryRadar.svelte";
  import IntelligenceMeter from "../components/IntelligenceMeter.svelte";
  import { CategoricalAssignment, OUTCOME } from "$lib/intelligenceColors";
  import { isDarkMode } from "../stores/theme";
  import { attemptedScore, deleteIntelligenceRun, intelligenceRequest, intelligenceURL, loadHumanScores, needsHumanScore, saveHumanScore, type HumanScores, type IntelligenceItem, type IntelligenceRun, type IntelligenceResult } from "$lib/intelligenceApi";

  let runs = $state<IntelligenceRun[]>([]);
  let selected = $state<string[]>([]);
  let results = $state<IntelligenceResult[]>([]);
  let filter = $state("");
  let error = $state("");
  let busy = $state(false);
  let deletingID = $state("");
  let humanScores = $state<HumanScores>({});
  let savingScore = $state("");
  let generation = 0;
  let visible = $derived(runs.filter(r => `${r.params.profile} ${r.state} ${new Date(r.started_at * 1000).toLocaleString()}`.toLowerCase().includes(filter.toLowerCase())));
  let differentSuites = $derived(new Set(results.map(r => r.run.suite_sha256)).size > 1);
  let multiRun = $derived(results.length > 1);

  const STATE_VARIANT: Record<string, BadgeVariant> = {
    done: "secondary", running: "default", starting: "default",
    error: "destructive", interrupted: "destructive", stopped: "outline",
  };

  // One color per model, assigned in first-seen order across every visible run —
  // every chart and meter on the page reads the same model in the same color.
  let modelColors = $derived.by(() => {
    const assignment = new CategoricalAssignment();
    const map = new Map<string, string>();
    for (const result of results) for (const model of result.models)
      if (!map.has(model.model_id)) map.set(model.model_id, assignment.color(model.model_id, $isDarkMode));
    return map;
  });

  let leaderboard = $derived.by(() =>
    results.flatMap(result => result.models.map(model => {
      const attempted = attemptedScore(model.items, { humanScores, modelID: model.model_id });
      return {
        label: multiRun ? `${model.model_name} · ${new Date(result.run.started_at * 1000).toLocaleDateString()}` : model.model_name,
        value: attempted.score, max: attempted.max,
        color: modelColors.get(model.model_id) ?? "#898781",
      };
    })).sort((a, b) => (b.max ? b.value / b.max : 0) - (a.max ? a.value / a.max : 0)));

  let kpis = $derived.by(() => {
    const allModels = results.flatMap(r => r.models);
    const uniqueModels = new Set(allModels.map(m => m.model_id));
    // Distinct items, not rows: comparing three runs of one model must not treble it.
    const uniqueItems = new Set(allModels.flatMap(m => m.items.map(i => i.item_id)));
    const awaiting = allModels.reduce(
      (n, m) => n + attemptedScore(m.items, { humanScores, modelID: m.model_id }).awaitingHuman, 0);
    const best = leaderboard[0];
    return {
      runs: results.length, models: uniqueModels.size, items: uniqueItems.size, awaiting,
      best: best ? { label: best.label, pct: best.max ? Math.round((best.value / best.max) * 100) : 0 } : null,
    };
  });

  function outcomeColor(modelID: string, item: IntelligenceItem): string {
    if (item.unsupported || item.grade?.unsupported) return OUTCOME.unsupported.color;
    if (needsHumanScore(item)) {
      const human = humanScores[item.item_id]?.[modelID];
      if (typeof human !== "number") return OUTCOME.human.color;
      return human >= item.max_score ? OUTCOME.pass.color : human > 0 ? OUTCOME.part.color : OUTCOME.fail.color;
    }
    if (item.raw?.error || item.error || item.truncated) return OUTCOME.none.color;
    if (item.score >= item.max_score) return OUTCOME.pass.color;
    return item.score > 0 ? OUTCOME.part.color : OUTCOME.fail.color;
  }

  function scoreFor(modelID: string, item: IntelligenceItem): number | undefined {
    return humanScores[item.item_id]?.[modelID];
  }
  // The endpoint replaces every score for an item, so the other models' scores
  // have to go back with it.
  async function setHumanScore(modelID: string, item: IntelligenceItem, value: string) {
    const key = `${item.item_id}:${modelID}`;
    const merged = { ...humanScores[item.item_id] };
    if (value === "") delete merged[modelID];
    else merged[modelID] = Math.max(0, Math.min(item.max_score, Number(value)));
    savingScore = key; error = "";
    try {
      await saveHumanScore(item.item_id, merged);
      humanScores = { ...humanScores, [item.item_id]: merged };
    } catch (e) { error = String(e); }
    finally { savingScore = ""; }
  }

  async function compare() {
    const request = ++generation;
    busy = true;
    try {
      const loaded = await Promise.all(selected.map(id => intelligenceRequest<IntelligenceResult>(`runs/${id}`)));
      if (request === generation) { results = loaded; error = ""; }
    } catch (e) { if (request === generation) error = String(e); }
    finally { if (request === generation) busy = false; }
  }
  async function refresh() {
    busy = true;
    try {
      runs = await intelligenceRequest<IntelligenceRun[]>("runs");
      // Rubric scores are optional: an older companion has no such endpoint, and
      // the page is still correct without them.
      humanScores = await loadHumanScores().catch(() => humanScores);
      if (!selected.length && runs.length) selected = [runs[0].run_id];
      await compare();
    } catch (e) { error = String(e); busy = false; }
  }
  async function removeRun(runID: string) {
    if (!window.confirm("Delete this run permanently? This cannot be undone.")) return;
    deletingID = runID; error = "";
    try {
      await deleteIntelligenceRun(runID);
      selected = selected.filter(id => id !== runID);
      await refresh();
    } catch (e) { error = String(e); }
    finally { deletingID = ""; }
  }
  onMount(() => { void refresh(); });
</script>

<div class="mx-auto w-full max-w-7xl space-y-6 overflow-auto p-6">
  <div class="flex items-center justify-between"><div><h1 class="text-2xl font-semibold">Intelligence results</h1><p class="text-muted-foreground mt-1">Compare saved runs and inspect the answers behind each score.</p></div><Button variant="outline" disabled={busy} onclick={refresh}>Refresh</Button></div>
  {#if error}<p role="alert" class="text-destructive rounded-lg border p-4">{error}</p>{/if}

  {#if kpis.runs}
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Runs compared</p><p class="text-2xl font-semibold">{kpis.runs}</p>
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Models compared</p><p class="text-2xl font-semibold">{kpis.models}</p>
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Items covered</p><p class="text-2xl font-semibold">{kpis.items}</p>
        {#if kpis.awaiting}<p class="text-muted-foreground text-xs">{kpis.awaiting} awaiting a score</p>{/if}
      </Card.Content></Card.Root>
      <Card.Root class="py-0"><Card.Content class="space-y-1 p-4">
        <p class="text-muted-foreground text-xs">Best attempted score</p>
        <p class="text-2xl font-semibold">{kpis.best?.pct ?? 0}%</p>
        {#if kpis.best}<p class="text-muted-foreground truncate text-xs" title={kpis.best.label}>{kpis.best.label}</p>{/if}
      </Card.Content></Card.Root>
    </div>
  {/if}

  <section class="space-y-3 rounded-xl border p-5">
    <label class="block text-sm">Filter runs<input class="bg-background mt-1 block w-full rounded-md border p-2" placeholder="Profile, date or status" bind:value={filter} /></label>
    <div class="max-h-56 space-y-2 overflow-auto">
      {#each visible as run (run.run_id)}
        <label class="flex flex-wrap items-center gap-3 rounded border p-3 text-sm">
          <input type="checkbox" bind:group={selected} value={run.run_id} onchange={() => void compare()} />
          <span>{new Date(run.started_at * 1000).toLocaleString()}</span>
          <span class="font-medium">{run.params.profile}</span>
          <Badge variant={STATE_VARIANT[run.state] ?? "outline"}>{run.state}</Badge>
          <span class="text-muted-foreground">{run.params.models.length} models</span>
          <span class="grow"></span>
          <Button size="icon-sm" variant="ghost" title="Delete run" disabled={deletingID === run.run_id || run.state === "starting" || run.state === "running"} onclick={() => removeRun(run.run_id)}><Trash2 class="text-destructive size-4" /></Button>
        </label>
      {:else}<p class="text-muted-foreground text-sm">No runs found. Start a suite from the Run Suite page.</p>{/each}
    </div>
  </section>

  {#if differentSuites}<p class="rounded-lg border p-4 text-sm">These runs use different suite versions. Compare item coverage before interpreting score differences.</p>{/if}

  {#if leaderboard.length}
    <IntelligenceScoreBar title="Attempted score, best to worst" data={leaderboard} />
    <div class="overflow-x-auto rounded-xl border"><table class="w-full text-left text-sm"><thead class="bg-muted"><tr><th class="p-3">Model</th><th class="p-3">Run / profile</th><th class="p-3">Score / suite maximum</th><th class="p-3">Score / attempted maximum</th></tr></thead><tbody>
      {#each results as result}{#each result.models as model}{@const attempted = attemptedScore(model.items, { humanScores, modelID: model.model_id })}<tr class="border-t"><td class="p-3 font-medium">{model.model_name}</td><td class="p-3">{new Date(result.run.started_at * 1000).toLocaleString()} · {result.run.params.profile}</td><td class="p-3">{model.totals.score} / {model.totals.max_total}</td><td class="p-3">{attempted.score} / {attempted.max}</td></tr>{/each}{/each}
    </tbody></table></div>
    <p class="text-muted-foreground text-sm">Attempted totals exclude unsupported, diagnostic and unscored rubric items. Different profiles or coverage are not directly comparable.</p>
  {/if}

  {#each results as result (result.run.run_id)}
    <section class="space-y-4 rounded-xl border p-5">
      <div class="flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold">{result.run.params.profile} · {new Date(result.run.started_at * 1000).toLocaleString()}</h2><a class="text-primary text-sm underline" href={`${intelligenceURL}/runs/${result.run.run_id}`} download={`intelligence-${result.run.run_id}.json`}>Download raw JSON</a></div>
      {#if !result.models.length}<p class="text-muted-foreground text-sm">No item results saved yet. Run status: {result.run.state}.</p>{/if}

      {#if result.suite.categories.length >= 3 && result.models.length}
        <IntelligenceCategoryRadar title="Category coverage" categories={result.suite.categories.map(c => c.name)}
          series={result.models.map(model => ({
            label: model.model_name, color: modelColors.get(model.model_id) ?? "#898781",
            values: result.suite.categories.map(c => {
              const max = model.totals.by_category_max[c.id] ?? 0;
              return max > 0 ? Math.round(((model.totals.by_category[c.id] ?? 0) / max) * 100) : 0;
            }),
          }))} />
      {/if}

      {#each result.models as model (model.model_id)}
        <details class="rounded-lg border p-4"><summary class="cursor-pointer font-medium">{model.model_name} · {model.totals.score} points</summary>
          <div class="my-3"><IntelligenceMeter value={model.totals.score} max={model.totals.max_total} color={modelColors.get(model.model_id) ?? "#898781"} /></div>
          <div class="my-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{#each Object.entries(model.totals.by_category) as [category, score]}<div class="bg-muted rounded p-3 text-sm"><IntelligenceMeter label={result.suite.categories.find(c => c.id === category)?.name ?? category} value={score} max={model.totals.by_category_max[category] ?? 0} color={modelColors.get(model.model_id) ?? "#898781"} /></div>{/each}</div>
          <div class="space-y-2">{#each model.items as item (item.item_id)}<details class="rounded border p-3"><summary class="flex cursor-pointer items-center gap-2 text-sm"><span class="size-2.5 shrink-0 rounded-[3px]" style="background:{outcomeColor(model.model_id, item)}"></span><span class="truncate">{item.title ?? item.item_id}</span><span class="text-muted-foreground shrink-0 tabular-nums">{needsHumanScore(item) ? scoreFor(model.model_id, item) ?? "—" : item.score ?? 0} / {item.max_score}</span>{#if item.role === "diagnostic"}<span class="text-muted-foreground shrink-0 text-xs">diagnostic</span>{/if}{#if item.unsupported || item.grade?.unsupported}<span class="text-muted-foreground shrink-0 text-xs">unsupported</span>{/if}</summary><div class="mt-3 space-y-3">
            {#if needsHumanScore(item)}
              <label class="flex flex-wrap items-center gap-2 text-sm">Rubric score
                <input type="number" min="0" max={item.max_score} step="1" class="bg-background w-24 rounded-md border p-2"
                  disabled={savingScore === `${item.item_id}:${model.model_id}`}
                  value={scoreFor(model.model_id, item) ?? ""}
                  onchange={(e) => void setHumanScore(model.model_id, item, e.currentTarget.value)} />
                <span class="text-muted-foreground">of {item.max_score}</span>
              </label>
            {/if}
            {#if item.raw?.error || item.error}<p class="text-destructive text-sm">{item.raw?.error || item.error}</p>{/if}
            {#if item.grade?.note}<p class="text-muted-foreground text-sm">{item.grade.note}</p>{/if}
            <h3 class="text-sm font-medium">Model answer</h3><pre class="bg-muted max-h-72 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{item.answer || "No answer returned."}</pre>
            <details><summary class="cursor-pointer text-sm">Full diagnostics</summary><pre class="bg-muted mt-3 max-h-96 overflow-auto rounded p-3 text-xs whitespace-pre-wrap">{JSON.stringify(item, null, 2)}</pre></details>
          </div></details>{/each}</div>
        </details>
      {/each}
    </section>
  {/each}
</div>
