<script lang="ts">
  interface Props { value: number; max: number; color: string; label?: string }
  let { value, max, color, label }: Props = $props();
  let pct = $derived(max > 0 ? Math.min(100, Math.round((value / max) * 1000) / 10) : 0);
</script>

<div class="flex items-center gap-2 text-xs">
  {#if label}<span class="text-muted-foreground w-28 shrink-0 truncate" title={label}>{label}</span>{/if}
  <div class="h-2 flex-1 overflow-hidden rounded-full" style="background:{color}26">
    <div class="h-full rounded-full" style="width:{pct}%;background:{color}"></div>
  </div>
  <span class="text-muted-foreground w-10 shrink-0 text-right tabular-nums">{pct}%</span>
</div>
