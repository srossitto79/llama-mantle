<script lang="ts">
  import { onMount } from "svelte";
  import { Loader2, RefreshCw, Square, Timer, Workflow } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { cancelStudioJob, getStudioScheduler, listTasks, retryStudioPipeline } from "../lib/mantleApi";
  import type { MantleTask, StudioSchedulerStatus } from "../lib/types";

  let jobs = $state<MantleTask[]>([]);
  let scheduler = $state<StudioSchedulerStatus | null>(null);
  let loading = $state(true);
  let error = $state("");

  async function refresh() {
    try {
      const [tasks, status] = await Promise.all([listTasks(), getStudioScheduler()]);
      jobs = tasks.filter((task) => task.type === "studio").sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
      scheduler = status;
      error = "";
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  async function cancel(job: MantleTask) {
    try {
      await cancelStudioJob(job.id);
      await refresh();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    }
  }

  async function retryPipeline(job: MantleTask) {
    try { const childIDs = Array.isArray(job.parameters?.childTaskIDs) ? job.parameters.childTaskIDs : []; const fromStep = Math.max(0, childIDs.length - 1); await retryStudioPipeline(job.id, fromStep); await refresh(); }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
  }

  function duration(job: MantleTask): string {
    const start = Date.parse(job.startedAt ?? job.queuedAt ?? job.createdAt);
    const end = Date.parse(job.finishedAt ?? job.updatedAt);
    const seconds = Math.max(0, Math.round((end - start) / 1000));
    if (seconds < 60) return seconds + "s";
    return Math.floor(seconds / 60) + "m " + (seconds % 60) + "s";
  }

  function isActive(job: MantleTask): boolean {
    return job.state === "queued" || job.state === "running";
  }

  function useOutput(job: MantleTask) {
	if (job.output) window.location.href = `/studio?model=${encodeURIComponent(job.output)}`;
  }

  onMount(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 2000);
    return () => window.clearInterval(timer);
  });
</script>

<div class="flex h-full min-h-0 flex-col gap-4">
  <div class="flex items-center gap-2">
    <Workflow class="size-5" />
    <h2 class="text-lg font-semibold">Studio jobs</h2>
    <Button class="ml-auto" variant="outline" size="sm" onclick={refresh}><RefreshCw class="size-4" />Refresh</Button>
  </div>

  {#if scheduler}
    <div class="grid gap-3 sm:grid-cols-4">
      <Card.Root class="gap-1 px-4 py-3"><span class="text-muted-foreground text-xs">Running</span><span class="text-xl font-semibold">{scheduler.running} / {scheduler.maxRunning}</span></Card.Root>
      <Card.Root class="gap-1 px-4 py-3"><span class="text-muted-foreground text-xs">Heavy jobs</span><span class="text-xl font-semibold">{scheduler.heavyRunning} / {scheduler.maxHeavy}</span></Card.Root>
      <Card.Root class="gap-1 px-4 py-3"><span class="text-muted-foreground text-xs">Queued</span><span class="text-xl font-semibold">{scheduler.queued}</span></Card.Root>
      <Card.Root class="gap-1 px-4 py-3"><span class="text-muted-foreground text-xs">Resource blocked</span><span class="text-xl font-semibold">{scheduler.blocked}</span></Card.Root>
    </div>
    {#if scheduler.blockedReason}<div class="border-amber-500/40 bg-amber-500/10 rounded-md border px-3 py-2 text-sm">{scheduler.blockedReason}</div>{/if}
  {/if}

  {#if error}<div class="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-sm">{error}</div>{/if}

  <Card.Root class="min-h-0 flex-1 gap-0 overflow-hidden py-0">
    <div class="min-h-0 flex-1 overflow-auto">
      {#if loading}
        <div class="text-muted-foreground flex h-40 items-center justify-center gap-2"><Loader2 class="size-4 animate-spin" />Loading jobs…</div>
      {:else if jobs.length === 0}
        <div class="text-muted-foreground flex h-40 items-center justify-center">No Studio jobs yet.</div>
      {:else}
        <table class="w-full text-left text-sm">
          <thead class="bg-muted/60 sticky top-0">
            <tr><th class="px-3 py-2">Operation</th><th class="px-3 py-2">Class</th><th class="px-3 py-2">State</th><th class="px-3 py-2">Progress</th><th class="px-3 py-2">Duration</th><th class="px-3 py-2">Created</th><th class="px-3 py-2"></th></tr>
          </thead>
          <tbody>
            {#each jobs as job (job.id)}
              <tr class="border-t">
                <td class="max-w-64 px-3 py-2"><div class="font-medium">{job.operation ?? "Studio"}</div><div class="text-muted-foreground truncate text-xs">{job.input}{job.output ? " → " + job.output : ""}</div></td>
                <td class="px-3 py-2 uppercase text-xs">{job.jobClass ?? "—"}</td>
                <td class="px-3 py-2"><span class="bg-muted rounded px-2 py-1 text-xs uppercase">{job.state}</span></td>
                <td class="min-w-28 px-3 py-2"><div class="bg-muted h-1.5 overflow-hidden rounded-full"><div class="bg-primary h-full" style:width={Math.max(0, job.pct) + "%"}></div></div><div class="text-muted-foreground mt-1 truncate text-xs">{job.message}</div></td>
                <td class="px-3 py-2"><span class="inline-flex items-center gap-1"><Timer class="size-3" />{duration(job)}</span></td>
                <td class="px-3 py-2 text-xs">{new Date(job.createdAt).toLocaleString()}</td>
                <td class="px-3 py-2"><div class="flex gap-1">{#if isActive(job)}<Button variant="destructive" size="sm" onclick={() => cancel(job)}><Square class="size-3" />Cancel</Button>{:else if job.state === "failed" && job.operation === "pipeline"}<Button variant="outline" size="sm" onclick={() => retryPipeline(job)}>Retry failed step</Button>{:else if job.state === "completed" && job.output}<Button variant="outline" size="sm" onclick={() => useOutput(job)}>Use output</Button>{/if}</div></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </Card.Root>
</div>
