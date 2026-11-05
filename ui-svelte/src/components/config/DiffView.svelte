<script lang="ts">
  import type { DiffHunk } from "$lib/config/diff";

  let { hunks }: { hunks: DiffHunk[] } = $props();
</script>

<div class="overflow-x-auto rounded-(--panel-radius) border font-mono text-xs leading-5">
  {#each hunks as h, hi (hi)}
    <div class="bg-muted text-muted-foreground border-b px-3 py-0.5 {hi > 0 ? 'border-t' : ''}">line {h.oldStart}</div>
    {#each h.lines as l, li (li)}
      <div
        class={[
          "grid grid-cols-[3rem_3rem_1rem_1fr] whitespace-pre",
          l.kind === "added" && "bg-success/12",
          l.kind === "removed" && "bg-destructive/10",
        ]}
      >
        <span class="text-muted-foreground px-1 text-right select-none">{"oldNo" in l ? l.oldNo : ""}</span>
        <span class="text-muted-foreground px-1 text-right select-none">{"newNo" in l ? l.newNo : ""}</span>
        <span class="text-muted-foreground select-none">{l.kind === "added" ? "+" : l.kind === "removed" ? "−" : ""}</span>
        <span class="pr-3">{l.text}</span>
      </div>
    {/each}
  {/each}
</div>
