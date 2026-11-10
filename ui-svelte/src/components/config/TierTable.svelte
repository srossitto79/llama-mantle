<script lang="ts">
  // Candidates of one context tier with a radio to pick one. Blocked candidates
  // and candidates without an entry are muted and not selectable. MoE-cache
  // candidates offer a cache tune; the sheet runs it.
  import * as Table from "$lib/components/ui/table/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import type { TierPlan } from "$lib/config/tunerApi";

  let {
    plan,
    picked = $bindable(null),
    problems = {},
    ontune,
    tuneDisabled = false,
  }: {
    plan: TierPlan;
    /** Id of the picked candidate, null for none. */
    picked?: string | null;
    /** Per-candidate problem text, by candidate id. */
    problems?: Record<string, string>;
    /** Starts a cache tune of the candidate. */
    ontune?: (id: string) => void;
    /** A tune is running. */
    tuneDisabled?: boolean;
  } = $props();

  const candidates = $derived(plan.candidates ?? []);
  const label = $derived(`${plan.tier / 1024}K`);

  const gib = (mib: number): string => (mib / 1024).toFixed(1);
</script>

<section class="space-y-1.5">
  <h3 class="text-label text-muted-foreground font-semibold uppercase">{label}</h3>
  {#if plan.skipped}
    <p class="text-muted-foreground text-sm">{plan.skipped}</p>
  {:else}
    <div class="rounded-(--panel-radius) border">
      <Table.Root>
        <Table.Header>
          <Table.Row>
            <Table.Head class="w-8"></Table.Head>
            <Table.Head>Backend</Table.Head>
            <Table.Head>Layout</Table.Head>
            <Table.Head>Args</Table.Head>
            <Table.Head class="text-right">VRAM GiB</Table.Head>
            <Table.Head>Reason</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {#each candidates as c (c.id)}
            {@const selectable = !c.blocked && !!c.entry}
            <Table.Row class={!selectable ? "text-muted-foreground" : ""}>
              <Table.Cell>
                <input
                  type="radio"
                  name="tier-{plan.tier}"
                  class="accent-primary"
                  aria-label="{c.backend} {c.layout}"
                  disabled={!selectable}
                  checked={picked === c.id}
                  onchange={() => (picked = c.id)}
                />
              </Table.Cell>
              <Table.Cell>{c.backend}</Table.Cell>
              <Table.Cell>{c.layout}</Table.Cell>
              <Table.Cell class="font-mono text-xs break-words whitespace-normal">{(c.args ?? []).join(" ")}</Table.Cell>
              <Table.Cell class="text-right tabular-nums" title={c.memory ? `weights ${c.memory.modelMiB}, context ${c.memory.contextMiB}, compute ${c.memory.computeMiB} MiB` : undefined}>{c.memory ? gib(c.memory.totalMiB) : "–"}</Table.Cell>
              <Table.Cell class="whitespace-normal">
                {c.blocked ?? c.reason}
                {#if problems[c.id]}<span class="text-destructive block">{problems[c.id]}</span>{/if}
                {#if c.cacheTune}
                  <details class="mt-1">
                    <summary class="cursor-pointer">
                      {c.cacheTune.allFit ? "All experts fit in VRAM" : `${c.cacheTune.tokensPerSec.toFixed(1)} tok/s predicted`}
                    </summary>
                    <pre class="bg-muted mt-1 overflow-x-auto rounded p-2 font-mono text-xs">{c.cacheTune.report}</pre>
                  </details>
                {:else if c.tunable && !c.blocked && ontune}
                  <Button size="sm" variant="outline" class="mt-1" disabled={tuneDisabled} onclick={() => ontune(c.id)}>Tune cache</Button>
                {/if}
              </Table.Cell>
            </Table.Row>
          {/each}
          <Table.Row>
            <Table.Cell>
              <input type="radio" name="tier-{plan.tier}" class="accent-primary" aria-label="None" checked={picked === null} onchange={() => (picked = null)} />
            </Table.Cell>
            <Table.Cell colspan={5} class="text-muted-foreground">None</Table.Cell>
          </Table.Row>
        </Table.Body>
      </Table.Root>
    </div>
  {/if}
</section>
