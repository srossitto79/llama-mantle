<script lang="ts">
  import { onMount } from "svelte";
  import { push } from "svelte-spa-router";
  import { BarChart3, Boxes, GitBranch, RefreshCw, Save, ShieldCheck, Trash2 } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import { applyStudioRetention, cleanupStudioArtifact, getStudioLineage, listStudioArtifacts, listStudioEvaluations, previewStudioRetention, saveStudioArtifactAnnotation, streamTaskProgress, verifyStudioArtifact, verifyStudioArtifacts } from "../lib/mantleApi";
  import type { StudioCatalogArtifact, StudioEvaluation, StudioLineageEdge, StudioRetentionPreview } from "../lib/types";
  import { activeStudioProject } from "../stores/studioProject";

  let artifacts = $state<StudioCatalogArtifact[]>([]);
  let lineage = $state<StudioLineageEdge[]>([]);
  let selected = $state("");
  let filter = $state("");
  let loading = $state(true);
  let error = $state("");
  let tags = $state("");
  let notes = $state("");
  let busy = $state(false);
  let evaluations = $state<StudioEvaluation[]>([]);
  let retentionDays = $state(30);
  let includeTagged = $state(false);
  let retentionPreview = $state<StudioRetentionPreview | null>(null);
  let projectArtifacts = $derived(artifacts.filter((artifact) => !$activeStudioProject || artifact.projectID === $activeStudioProject));
  let kinds = $derived([...new Set(projectArtifacts.map((artifact) => artifact.kind))].sort());
  let visible = $derived(filter ? projectArtifacts.filter((artifact) => artifact.kind === filter) : projectArtifacts);
  let selectedArtifact = $derived(artifacts.find((artifact) => artifact.path === selected));

  function formatSize(bytes: number): string {
    if (!bytes) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
    return `${(bytes / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
  }

  async function refresh() {
    loading = true;
    try { artifacts = await listStudioArtifacts(); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { loading = false; }
  }

  async function select(path: string) {
    selected = path;
    const artifact = artifacts.find((item) => item.path === path);
    tags = artifact?.tags?.join(", ") ?? "";
    notes = artifact?.notes ?? "";
    try { [lineage, evaluations] = await Promise.all([getStudioLineage(path), listStudioEvaluations(path)]); }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
  }

  async function saveAnnotation() {
    if (!selected) return;
    busy = true;
    try { await saveStudioArtifactAnnotation(selected, tags.split(",").map((tag) => tag.trim()).filter(Boolean), notes); await refresh(); }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  function refreshAfter(taskID: string) {
    const stop = streamTaskProgress(taskID, (update) => {
      if (update.state && !["queued", "running"].includes(update.state)) { stop(); busy = false; void refresh(); }
    });
  }

  async function verify() {
    if (!selected) return;
    busy = true;
    try { const task = await verifyStudioArtifact(selected); refreshAfter(task.id); }
    catch (cause) { busy = false; error = cause instanceof Error ? cause.message : String(cause); }
  }

  async function cleanup() {
    if (!selected) return;
    await removeArtifact(selected);
  }

  async function removeArtifact(path: string) {
    if (!window.confirm(`Delete ${path}? The file cannot be recovered from Studio.`)) return;
    busy = true;
    try { const task = await cleanupStudioArtifact(path); refreshAfter(task.id); }
    catch (cause) { busy = false; error = cause instanceof Error ? cause.message : String(cause); }
  }

  async function verifyVisible() {
    const paths = visible.filter((artifact) => artifact.exists).map((artifact) => artifact.path);
    if (!paths.length) return;
    busy = true;
    try { const task = await verifyStudioArtifacts(paths); refreshAfter(task.id); }
    catch (cause) { busy = false; error = cause instanceof Error ? cause.message : String(cause); }
  }

  async function previewRetention() {
    busy = true;
    try { retentionPreview = await previewStudioRetention({ maxAgeDays: retentionDays, kinds: filter ? [filter] : undefined, includeTagged }); }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function applyRetention() {
    if (!retentionPreview || !window.confirm(`Delete ${retentionPreview.candidates.length} files (${formatSize(retentionPreview.totalBytes)})?`)) return;
    busy = true;
    try { const task = await applyStudioRetention({ maxAgeDays: retentionDays, kinds: filter ? [filter] : undefined, includeTagged }, retentionPreview.token); retentionPreview = null; refreshAfter(task.id); }
    catch (cause) { busy = false; error = cause instanceof Error ? cause.message : String(cause); }
  }

  // Studio's single-operation form always treats "input" as a GGUF file (base
  // model, adapter, or checkpoint) to quantize/hash/train/etc. — datasets and
  // other non-GGUF artifacts (reports, rollouts, caches) can never be used
  // there, only in a pipeline step's own resource picker.
  function isGGUFArtifact(path: string): boolean {
    return path.toLowerCase().endsWith(".gguf");
  }
  function openStudio(path: string) { void push(`/studio?model=${encodeURIComponent(path)}`); }
  function openPipeline(path: string) { void push(`/studio/pipelines?model=${encodeURIComponent(path)}`); }
  function evaluationSummary(evaluation: StudioEvaluation): string {
    if (evaluation.mode === "perplexity") return evaluation.metrics.perplexity === undefined ? "No parsed result" : `PPL ${Number(evaluation.metrics.perplexity).toFixed(3)}`;
    const prompt = evaluation.metrics.promptTokensPerSecond;
    const generation = evaluation.metrics.generationTokensPerSecond;
    return [prompt === undefined ? "" : `PP ${Number(prompt).toFixed(1)} t/s`, generation === undefined ? "" : `TG ${Number(generation).toFixed(1)} t/s`].filter(Boolean).join(" · ") || "No parsed result";
  }
  onMount(() => { void refresh(); });
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-2">
  <div class="flex items-center gap-2"><Boxes class="size-5" /><h2 class="text-lg font-semibold">Artifacts</h2><Button class="ml-auto" variant="outline" size="sm" onclick={verifyVisible} disabled={busy || visible.every((artifact) => !artifact.exists)}><ShieldCheck class="size-4" />Verify visible</Button><Button variant="outline" size="sm" onclick={refresh}><RefreshCw class="size-4" />Refresh</Button></div>
  {#if error}<div class="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-sm">{error}</div>{/if}
  <div class="grid min-h-0 gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
    <Card.Root class="min-h-0 gap-0 overflow-hidden py-0">
      <div class="border-b p-3"><select class="border-input bg-background h-9 rounded-md border px-3 text-sm" bind:value={filter}><option value="">All artifact types</option>{#each kinds as kind}<option value={kind}>{kind}</option>{/each}</select></div>
      <div class="overflow-auto">{#if loading}<div class="text-muted-foreground p-8 text-center">Loading artifacts…</div>{:else if visible.length === 0}<div class="text-muted-foreground p-8 text-center">No generated artifacts yet.</div>{:else}<table class="w-full text-left text-sm"><thead class="bg-muted/60 sticky top-0"><tr><th class="px-3 py-2">Artifact</th><th class="px-3 py-2">Type</th><th class="px-3 py-2">Producer</th><th class="px-3 py-2">Size</th><th class="px-3 py-2">Status</th><th></th></tr></thead><tbody>{#each visible as artifact (artifact.path)}<tr class:bg-accent={selected === artifact.path} class="border-t"><td class="max-w-72 px-3 py-2"><button class="w-full truncate text-left font-medium" title={artifact.path} onclick={() => select(artifact.path)}>{artifact.path}</button></td><td class="px-3 py-2 text-xs">{artifact.kind}</td><td class="px-3 py-2 text-xs">{artifact.operation}</td><td class="px-3 py-2">{formatSize(artifact.size)}</td><td class="px-3 py-2"><span class:text-destructive={!artifact.exists}>{artifact.exists ? "Available" : "Missing"}</span></td><td class="px-3 py-2"><div class="flex gap-1"><Button size="sm" variant="outline" title={isGGUFArtifact(artifact.path) ? "" : "Only GGUF files (models, adapters, checkpoints) can be opened in Studio"} disabled={!artifact.exists || !isGGUFArtifact(artifact.path)} onclick={() => openStudio(artifact.path)}>Use</Button><Button size="sm" variant="outline" disabled={!artifact.exists} onclick={() => openPipeline(artifact.path)}>Pipeline</Button><Button size="icon-sm" variant="ghost" title="Delete artifact" disabled={busy || !artifact.exists} onclick={() => removeArtifact(artifact.path)}><Trash2 class="text-destructive size-4" /></Button></div></td></tr>{/each}</tbody></table>{/if}</div>
    </Card.Root>
    <div class="space-y-4">
      <Card.Root class="h-fit"><Card.Header><Card.Title class="flex items-center gap-2"><ShieldCheck class="size-4" />Verification</Card.Title></Card.Header><Card.Content class="space-y-3">{#if !selectedArtifact}<p class="text-muted-foreground text-sm">Select an artifact to inspect it.</p>{:else}<div class="text-xs"><div class="text-muted-foreground">SHA-256</div><div class="mt-1 break-all font-mono">{selectedArtifact.sha256 || "Not verified"}</div></div>{#if selectedArtifact.ggufValid !== undefined}<div class:text-destructive={!selectedArtifact.ggufValid} class="text-sm">GGUF: {selectedArtifact.ggufValid ? "Valid" : `Invalid — ${selectedArtifact.verificationError ?? "parse failed"}`}</div>{/if}<div class="space-y-1"><Label.Root for="artifact-tags">Tags</Label.Root><Input id="artifact-tags" bind:value={tags} placeholder="release, favorite" /></div><div class="space-y-1"><Label.Root for="artifact-notes">Notes</Label.Root><textarea id="artifact-notes" class="border-input bg-background min-h-24 w-full rounded-md border p-2 text-sm" bind:value={notes}></textarea></div><div class="flex flex-wrap gap-2"><Button size="sm" variant="outline" onclick={saveAnnotation} disabled={busy}><Save class="size-4" />Save</Button><Button size="sm" variant="outline" onclick={verify} disabled={busy || !selectedArtifact.exists}><ShieldCheck class="size-4" />Verify</Button><Button size="sm" variant="destructive" onclick={cleanup} disabled={busy || !selectedArtifact.exists}><Trash2 class="size-4" />Delete file</Button></div>{/if}</Card.Content></Card.Root>
      <Card.Root class="h-fit"><Card.Header><Card.Title class="flex items-center gap-2"><GitBranch class="size-4" />Lineage</Card.Title></Card.Header><Card.Content class="space-y-2">{#if !selected}<p class="text-muted-foreground text-sm">Select an artifact to inspect its connected history.</p>{:else if lineage.length === 0}<p class="text-muted-foreground text-sm">No recorded parents or descendants.</p>{:else}{#each lineage as edge}<div class="border-border rounded-md border p-2 text-xs"><div class="font-medium">{edge.relation}</div><div class="text-muted-foreground mt-1 break-all">{edge.input}</div><div class="my-1">↓</div><div class="break-all">{edge.output}</div></div>{/each}{/if}</Card.Content></Card.Root>
      <Card.Root class="h-fit"><Card.Header><Card.Title class="flex items-center gap-2"><BarChart3 class="size-4" />Evaluations</Card.Title></Card.Header><Card.Content class="space-y-2">{#if !selected}<p class="text-muted-foreground text-sm">Select an artifact to compare evaluations.</p>{:else if evaluations.length === 0}<p class="text-muted-foreground text-sm">No evaluations recorded for this artifact.</p>{:else}{#each evaluations as evaluation}<div class="border-border rounded-md border p-2 text-sm"><div class="flex justify-between gap-2"><span class="font-medium capitalize">{evaluation.mode}</span><span class="text-muted-foreground text-xs">{new Date(evaluation.createdAt).toLocaleString()}</span></div><div class="mt-1">{evaluationSummary(evaluation)}</div><div class="text-muted-foreground mt-1 text-xs">Job {evaluation.jobID}</div></div>{/each}{/if}</Card.Content></Card.Root>
      <Card.Root class="h-fit"><Card.Header><Card.Title>Retention preview</Card.Title></Card.Header><Card.Content class="space-y-3"><div class="space-y-1"><Label.Root for="retention-days">Remove artifacts older than days</Label.Root><Input id="retention-days" type="number" min="1" bind:value={retentionDays} /></div><label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={includeTagged} />Include tagged artifacts</label><p class="text-muted-foreground text-xs">The current type filter is applied. Serving registrations are always excluded.</p><Button size="sm" variant="outline" onclick={previewRetention} disabled={busy}>Preview</Button>{#if retentionPreview}<div class="border-border rounded-md border p-2 text-sm"><div>{retentionPreview.candidates.length} files · {formatSize(retentionPreview.totalBytes)}</div><div class="text-muted-foreground mt-1 max-h-28 whitespace-pre-wrap overflow-auto text-xs">{retentionPreview.candidates.map((artifact) => artifact.path).join("\n")}</div><Button class="mt-2" size="sm" variant="destructive" onclick={applyRetention} disabled={busy || retentionPreview.candidates.length === 0}>Apply exact preview</Button></div>{/if}</Card.Content></Card.Root>
    </div>
  </div>
</div>
