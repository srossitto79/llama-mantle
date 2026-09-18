<script lang="ts">
  import { OUTCOME, type Outcome } from "$lib/intelligenceColors";
  import { needsHumanScore, type IntelligenceItem, type IntelligenceRun } from "$lib/intelligenceApi";

  interface Props { run: IntelligenceRun }
  let { run }: Props = $props();

  // Pending squares are drawn one per planned item; a full external suite would
  // otherwise paint thousands of nodes on every snapshot poll.
  const MAX_PENDING = 400;

  function outcomeOf(item: IntelligenceItem): Outcome {
    if (item.unsupported || item.grade?.unsupported) return "unsupported";
    if (needsHumanScore(item)) return "human";
    // A truncated or errored attempt scores zero for a reason that is not the
    // model getting it wrong, so it reads differently from a genuine miss.
    if (item.truncated || item.error || item.raw?.error) return "none";
    if (item.finish_reason === null && !item.answer && item.score === 0) return "none";
    if (item.score >= item.max_score) return "pass";
    if (item.score > 0) return "part";
    return "fail";
  }

  function tooltip(item: IntelligenceItem): string {
    const parts = [`${item.title || item.item_id} — ${item.score} / ${item.max_score}`, OUTCOME[outcomeOf(item)].label];
    if (item.latency_ms != null) parts.push(`${(item.latency_ms / 1000).toFixed(1)}s`);
    if (item.ttft_ms != null) parts.push(`${(item.ttft_ms / 1000).toFixed(1)}s to first token`);
    if (item.error) parts.push(String(item.error).slice(0, 120));
    return parts.join(" · ");
  }

  function elapsed(model: NonNullable<IntelligenceRun["models"]>[number]): string {
    if (!model.started_at) return "";
    const seconds = (model.finished_at ?? Date.now() / 1000) - model.started_at;
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
    return `${Math.floor(seconds / 3600)}h ${Math.round((seconds % 3600) / 60)}m`;
  }

  let models = $derived(run.models ?? []);
  // Only the outcomes actually on screen get a legend entry — a run with no
  // rubric items should not advertise a colour it never draws.
  let legend = $derived.by(() => {
    const present = new Set<Outcome>();
    for (const model of models) for (const item of model.items ?? []) present.add(outcomeOf(item));
    return (Object.keys(OUTCOME) as Outcome[]).filter(key => present.has(key));
  });

  function rowFor(model: NonNullable<IntelligenceRun["models"]>[number]) {
    const items = model.items ?? [];
    const planned = model.item_count ?? run.suite?.item_count ?? items.length;
    const live = run.current?.model_id === model.id;
    const attempted = items.reduce((sum, item) => sum + item.max_score, 0);
    const notes: string[] = [];
    if (attempted) notes.push(`${Math.round((model.score ?? 0) * 10) / 10} / ${attempted} attempted`);
    const time = elapsed(model);
    if (time) notes.push(time);
    if (model.totals?.tokens_per_second) notes.push(`${model.totals.tokens_per_second.toFixed(1)} tok/s`);
    if (model.totals?.decode_tokens_per_second) notes.push(`${model.totals.decode_tokens_per_second.toFixed(1)} tok/s decode`);
    if (model.carried) notes.push(`${model.carried} carried over`);
    return {
      items, planned, live, notes,
      pending: Array.from({ length: Math.min(Math.max(0, planned - items.length - (live ? 1 : 0)), MAX_PENDING) }, (_, i) => i),
    };
  }
</script>

<div class="space-y-4">
  {#each models as model (model.id)}
    {@const row = rowFor(model)}
    <div class="space-y-2">
      <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm">
        <span class="font-medium">{model.name}</span>
        {#if model.retry}<span class="bg-muted rounded px-1.5 py-0.5 text-xs">{model.resume ? "resume" : "retry"}</span>{/if}
        <span class="text-muted-foreground ml-auto tabular-nums">{row.items.length} / {row.planned || "?"}</span>
        <span class="text-muted-foreground text-xs capitalize">{model.state}</span>
      </div>
      <div class="flex flex-wrap gap-[3px]">
        {#each row.items as item, index (item.item_id + index)}
          <span class="size-3 rounded-[3px]" style="background:{OUTCOME[outcomeOf(item)].color}" title={tooltip(item)}></span>
        {/each}
        {#if row.live}
          <span class="bg-primary size-3 animate-pulse rounded-[3px]" title={run.current?.title ?? "In flight"}></span>
        {/if}
        {#each row.pending as index (index)}
          <span class="bg-muted size-3 rounded-[3px]"></span>
        {/each}
      </div>
      {#if row.notes.length}<p class="text-muted-foreground text-xs">{row.notes.join(" · ")}</p>{/if}
    </div>
  {:else}
    <p class="text-muted-foreground text-sm">No run yet.</p>
  {/each}

  {#if legend.length}
    <div class="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs">
      {#each legend as key}
        <span class="flex items-center gap-1.5"><span class="size-2.5 rounded-[3px]" style="background:{OUTCOME[key].color}"></span>{OUTCOME[key].label}</span>
      {/each}
    </div>
  {/if}
</div>
