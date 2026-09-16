<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { attemptedScore, intelligenceRequest, intelligenceURL, type IntelligenceRun, type IntelligenceResult } from "$lib/intelligenceApi";

  let runs = $state<IntelligenceRun[]>([]);
  let selected = $state<string[]>([]);
  let results = $state<IntelligenceResult[]>([]);
  let filter = $state("");
  let error = $state("");
  let busy = $state(false);
  let generation = 0;
  let visible = $derived(runs.filter(r => `${r.params.profile} ${r.state} ${new Date(r.started_at * 1000).toLocaleString()}`.toLowerCase().includes(filter.toLowerCase())));
  let differentSuites = $derived(new Set(results.map(r => r.run.suite_sha256)).size > 1);

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
      if (!selected.length && runs.length) selected = [runs[0].run_id];
      await compare();
    } catch (e) { error = String(e); busy = false; }
  }
  onMount(() => { void refresh(); });
</script>

<div class="mx-auto w-full max-w-7xl space-y-6 overflow-auto p-6">
  <div class="flex items-center justify-between"><div><h1 class="text-2xl font-semibold">Intelligence results</h1><p class="text-muted-foreground mt-1">Compare saved runs and inspect the answers behind each score.</p></div><Button variant="outline" disabled={busy} onclick={refresh}>Refresh</Button></div>
  {#if error}<p role="alert" class="text-destructive rounded-lg border p-4">{error}</p>{/if}
  <section class="space-y-3 rounded-xl border p-5">
    <label class="block text-sm">Filter runs<input class="bg-background mt-1 block w-full rounded-md border p-2" placeholder="Profile, date or status" bind:value={filter} /></label>
    <div class="max-h-56 space-y-2 overflow-auto">
      {#each visible as run (run.run_id)}
        <label class="flex flex-wrap items-center gap-3 rounded border p-3 text-sm"><input type="checkbox" bind:group={selected} value={run.run_id} onchange={() => void compare()} /><span>{new Date(run.started_at * 1000).toLocaleString()}</span><span class="font-medium">{run.params.profile}</span><span class="text-muted-foreground">{run.state} · {run.params.models.length} models</span></label>
      {:else}<p class="text-muted-foreground text-sm">No runs found. Start a suite from the Run Suite page.</p>{/each}
    </div>
  </section>
  {#if differentSuites}<p class="rounded-lg border p-4 text-sm">These runs use different suite versions. Compare item coverage before interpreting score differences.</p>{/if}
  {#if results.length}
    <div class="overflow-x-auto rounded-xl border"><table class="w-full text-left text-sm"><thead class="bg-muted"><tr><th class="p-3">Model</th><th class="p-3">Run / profile</th><th class="p-3">Score / suite maximum</th><th class="p-3">Score / attempted maximum</th></tr></thead><tbody>
      {#each results as result}{#each result.models as model}{@const attempted = attemptedScore(model.items)}<tr class="border-t"><td class="p-3 font-medium">{model.model_name}</td><td class="p-3">{new Date(result.run.started_at * 1000).toLocaleString()} · {result.run.params.profile}</td><td class="p-3">{model.totals.score} / {model.totals.max_total}</td><td class="p-3">{attempted.score} / {attempted.max}</td></tr>{/each}{/each}
    </tbody></table></div>
    <p class="text-muted-foreground text-sm">Attempted totals exclude unsupported and diagnostic items. Different profiles or coverage are not directly comparable.</p>
  {/if}
  {#each results as result (result.run.run_id)}
    <section class="space-y-4 rounded-xl border p-5">
      <div class="flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold">{result.run.params.profile} · {new Date(result.run.started_at * 1000).toLocaleString()}</h2><a class="text-primary text-sm underline" href={`${intelligenceURL}/runs/${result.run.run_id}`} download={`intelligence-${result.run.run_id}.json`}>Download raw JSON</a></div>
      {#if !result.models.length}<p class="text-muted-foreground text-sm">No item results saved yet. Run status: {result.run.state}.</p>{/if}
      {#each result.models as model (model.model_id)}
        <details class="rounded-lg border p-4"><summary class="cursor-pointer font-medium">{model.model_name} · {model.totals.score} points</summary>
          <div class="my-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{#each Object.entries(model.totals.by_category) as [category, score]}<div class="bg-muted rounded p-3 text-sm">{result.suite.categories.find(c => c.id === category)?.name ?? category}<span class="float-right">{score} / {model.totals.by_category_max[category] ?? 0}</span></div>{/each}</div>
          <div class="space-y-2">{#each model.items as item (item.item_id)}<details class="rounded border p-3"><summary class="cursor-pointer text-sm">{item.title ?? item.item_id} · {item.score ?? 0} / {item.max_score}{item.role === "diagnostic" ? " · diagnostic" : ""}{item.unsupported || item.grade?.unsupported ? " · unsupported" : ""}</summary><div class="mt-3 space-y-3">
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
