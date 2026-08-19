<script lang="ts">
  import { onMount } from "svelte";
  import { BarChart3, RefreshCw, Rocket } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import { listStudioEvaluations, registerStudioModel, streamTaskProgress } from "../lib/mantleApi";
  import type { StudioEvaluation } from "../lib/types";

  let evaluations = $state<StudioEvaluation[]>([]);
  let baselineID = $state("");
  let candidateID = $state("");
  let modelID = $state("");
  let busy = $state(false);
  let error = $state("");
  let status = $state("");
  let baseline = $derived(evaluations.find((item) => item.jobID === baselineID));
  let candidate = $derived(evaluations.find((item) => item.jobID === candidateID));
  let comparable = $derived(baseline && candidate && baseline.mode === candidate.mode);

  function metric(item: StudioEvaluation | undefined, key: string): number | undefined { const value = item?.metrics[key]; return typeof value === "number" ? value : undefined; }
  function delta(key: string, higherBetter: boolean): number | undefined {
    const before = metric(baseline, key), after = metric(candidate, key); if (before === undefined || after === undefined || before === 0) return undefined;
    return (higherBetter ? (after - before) : (before - after)) * 100 / before;
  }
  function formatMetric(value: number | undefined): string { return value === undefined ? "—" : value.toLocaleString(undefined, { maximumFractionDigits: 3 }); }

  async function refresh() {
    busy = true; try { evaluations = await listStudioEvaluations(); if (!candidateID && evaluations[0]) candidateID = evaluations[0].jobID; if (!baselineID && evaluations[1]) baselineID = evaluations[1].jobID; error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); } finally { busy = false; }
  }
  async function promote() {
    if (!candidate || !modelID.trim()) return; busy = true;
    try { const task = await registerStudioModel({ model: candidate.model, modelID: modelID.trim(), name: modelID.trim(), description: `Promoted from evaluation ${candidate.jobID}`, gpuLayers: -1 }); status = task.message;
      const stop = streamTaskProgress(task.id, (update) => { status = update.message ?? status; if (["completed", "failed", "cancelled"].includes(update.state ?? "")) { stop(); busy = false; } }); error = "";
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); busy = false; }
  }
  onMount(refresh);
</script>

<div class="h-full overflow-auto p-4"><div class="mx-auto max-w-6xl space-y-4">
  <div class="flex items-center gap-2"><BarChart3 class="size-5" /><h2 class="text-lg font-semibold">Evaluation workspace</h2><Button class="ml-auto" size="sm" variant="outline" onclick={refresh}><RefreshCw class="size-4" />Refresh</Button></div>
  {#if error}<p class="text-destructive text-sm">{error}</p>{/if}{#if status}<p class="text-muted-foreground text-sm">{status}</p>{/if}
  <Card.Root><Card.Header><Card.Title>Compare variants</Card.Title><Card.Description>Positive deltas indicate an improvement, including perplexity where lower is better.</Card.Description></Card.Header><Card.Content class="space-y-4">
    <div class="grid gap-3 md:grid-cols-2"><div class="space-y-1"><Label.Root for="evaluation-baseline">Baseline</Label.Root><select id="evaluation-baseline" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={baselineID}><option value="">Select baseline</option>{#each evaluations as item (item.jobID)}<option value={item.jobID}>{item.model} · {item.mode} · {new Date(item.createdAt).toLocaleString()}</option>{/each}</select></div><div class="space-y-1"><Label.Root for="evaluation-candidate">Candidate</Label.Root><select id="evaluation-candidate" class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm" bind:value={candidateID}><option value="">Select candidate</option>{#each evaluations as item (item.jobID)}<option value={item.jobID}>{item.model} · {item.mode} · {new Date(item.createdAt).toLocaleString()}</option>{/each}</select></div></div>
    {#if baseline && candidate && !comparable}<p class="text-warning text-sm">Select two evaluations of the same mode.</p>{/if}
    {#if comparable}<div class="overflow-auto rounded-md border"><table class="w-full text-sm"><thead class="bg-muted"><tr><th class="p-2 text-left">Metric</th><th class="p-2 text-right">Baseline</th><th class="p-2 text-right">Candidate</th><th class="p-2 text-right">Improvement</th></tr></thead><tbody>{#each [{key:"generationTokensPerSecond", label:"Generation tok/s", higher:true},{key:"promptTokensPerSecond", label:"Prompt tok/s", higher:true},{key:"perplexity", label:"Perplexity", higher:false}] as row}<tr class="border-t"><td class="p-2">{row.label}</td><td class="p-2 text-right">{formatMetric(metric(baseline, row.key))}</td><td class="p-2 text-right">{formatMetric(metric(candidate, row.key))}</td><td class="p-2 text-right" class:text-success={(delta(row.key, row.higher) ?? 0) > 0} class:text-destructive={(delta(row.key, row.higher) ?? 0) < 0}>{delta(row.key, row.higher) === undefined ? "—" : `${delta(row.key, row.higher)!.toFixed(2)}%`}</td></tr>{/each}</tbody></table></div>{/if}
  </Card.Content></Card.Root>
  <Card.Root><Card.Header><Card.Title>Promote candidate</Card.Title><Card.Description>Register the selected candidate model with the existing serving configuration.</Card.Description></Card.Header><Card.Content class="flex items-end gap-2"><div class="min-w-0 flex-1 space-y-1"><Label.Root for="promotion-model-id">Serving model ID</Label.Root><Input id="promotion-model-id" bind:value={modelID} placeholder="my-model-v2" /></div><Button onclick={promote} disabled={busy || !candidate || !modelID.trim()}><Rocket class="size-4" />Promote {candidate?.model ?? "candidate"}</Button></Card.Content></Card.Root>
</div></div>
