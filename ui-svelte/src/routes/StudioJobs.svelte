<script lang="ts">
  import { onMount } from "svelte";
  import { push } from "svelte-spa-router";
  import { Loader2, RefreshCw, Square, Timer } from "@lucide/svelte";
  import * as Page from "$lib/components/ui/page/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { cancelStudioJob, getStudioScheduler, listTasks, retryStudioPipeline } from "../lib/mantleApi";
  import type { MantleTask, StudioSchedulerStatus } from "../lib/types";
  import { activeStudioProject } from "../stores/studioProject";

  let jobs = $state<MantleTask[]>([]);
  let scheduler = $state<StudioSchedulerStatus | null>(null);
  let loading = $state(true);
  let error = $state("");

  async function refresh() {
    try {
      const [tasks, status] = await Promise.all([listTasks(), getStudioScheduler()]);
      jobs = tasks.filter((task) => task.type === "studio" && (!$activeStudioProject || task.projectID === $activeStudioProject)).sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
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

  // Studio's single-operation form always treats "input" as a GGUF file, so a
  // job whose output is a dataset (e.g. distill, download-dataset) has
  // nowhere useful to go there.
  function isGGUFOutput(job: MantleTask): boolean {
    return !!job.output && job.output.toLowerCase().endsWith(".gguf");
  }

  function useOutput(job: MantleTask) {
	if (job.output) void push(`/studio?model=${encodeURIComponent(job.output)}`);
  }

  function useOutputInPipeline(job: MantleTask) {
	if (job.output) void push(`/studio/pipelines?model=${encodeURIComponent(job.output)}`);
  }

  onMount(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 2000);
    return () => window.clearInterval(timer);
  });
</script>

<Page.Root title="Studio Jobs" width="full" fill={true}>
  {#snippet actions()}
    <Button variant="outline" size="sm" onclick={refresh}><RefreshCw class="size-4" />Refresh</Button>
  {/snippet}

  {#if scheduler}
    <div class="grid gap-(--page-gap) sm:grid-cols-4">
      {#each [["Running", `${scheduler.running} / ${scheduler.maxRunning}`], ["Heavy", `${scheduler.heavyRunning} / ${scheduler.maxHeavy}`], ["Queued", `${scheduler.queued}`], ["Blocked", `${scheduler.blocked}`]] as [label, value]}
        <Card.Root class="gap-1 px-4 py-3"><span class="text-label text-muted-foreground uppercase">{label}</span><span class="text-xl font-semibold tabular-nums">{value}</span></Card.Root>
      {/each}
    </div>
    {#if scheduler.blockedReason}<Alert.Root variant="warning"><Alert.Description>{scheduler.blockedReason}</Alert.Description></Alert.Root>{/if}
  {/if}

  {#if error}<Alert.Root variant="destructive"><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}

  <Card.Root class="min-h-0 flex-1 gap-0 overflow-hidden py-0">
    <div class="min-h-0 flex-1 overflow-auto">
      {#if loading}
        <div class="text-muted-foreground flex h-40 items-center justify-center gap-2"><Loader2 class="size-4 animate-spin" />Loading…</div>
      {:else if jobs.length === 0}
        <EmptyState title="No jobs" />
      {:else}
        <table class="w-full text-left text-sm">
          <thead class="bg-(--panel-header-bg) text-muted-foreground sticky top-0 border-b text-xs">
            <tr><th class="px-3 py-2 font-medium">Operation</th><th class="px-3 py-2 font-medium">Class</th><th class="px-3 py-2 font-medium">State</th><th class="px-3 py-2 font-medium">Progress</th><th class="px-3 py-2 font-medium">Duration</th><th class="px-3 py-2 font-medium">Created</th><th class="px-3 py-2 font-medium"></th></tr>
          </thead>
          <tbody class="divide-y">
            {#each jobs as job (job.id)}
              <tr class="hover:bg-muted/50">
                <td class="max-w-64 px-3 py-2"><div class="font-medium">{job.operation ?? "Studio"}</div><div class="text-muted-foreground truncate font-mono text-xs">{job.input}{job.output ? " → " + job.output : ""}</div></td>
                <td class="px-3 py-2 uppercase text-xs">{job.jobClass ?? "—"}</td>
                <td class="px-3 py-2"><span class="inline-flex items-center gap-2 text-xs capitalize"><span class="size-2 rounded-full {job.state === 'running' ? 'bg-primary' : job.state === 'completed' ? 'bg-success' : job.state === 'failed' ? 'bg-destructive' : 'bg-muted-foreground/40'}" aria-hidden="true"></span>{job.state}</span></td>
                <td class="min-w-28 px-3 py-2"><div class="bg-muted h-1.5 overflow-hidden rounded-full"><div class="bg-primary h-full" style:width={Math.max(0, job.pct) + "%"}></div></div><div class="text-muted-foreground mt-1 truncate text-xs">{job.message}</div></td>
                <td class="px-3 py-2 tabular-nums"><span class="inline-flex items-center gap-1"><Timer class="size-3" />{duration(job)}</span></td>
                <td class="px-3 py-2 text-xs">{new Date(job.createdAt).toLocaleString()}</td>
                <td class="px-3 py-2"><div class="flex gap-1">{#if isActive(job)}<Button variant="destructive" size="sm" onclick={() => cancel(job)}><Square class="size-3" />Cancel</Button>{:else if job.state === "failed" && job.operation === "pipeline"}<Button variant="outline" size="sm" onclick={() => retryPipeline(job)}>Retry failed step</Button>{:else if job.state === "completed" && job.output}{#if isGGUFOutput(job)}<Button variant="outline" size="sm" onclick={() => useOutput(job)}>Use output</Button>{/if}<Button variant="outline" size="sm" onclick={() => useOutputInPipeline(job)}>Use in pipeline</Button>{/if}</div></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </Card.Root>
</Page.Root>
