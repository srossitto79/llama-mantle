<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";

  interface Props {
    items: readonly { readonly label: string }[];
    selected: number;
    onSelect: (index: number) => void;
    /** Optional leading label rendered before the buttons. */
    label?: string;
  }

  let { items, selected, onSelect, label }: Props = $props();
</script>

<div class="flex flex-wrap items-center gap-1" role="group" aria-label={label}>
  {#if label}
    <span class="text-muted-foreground mr-1 text-xs">{label}</span>
  {/if}
  {#each items as item, i (i)}
    <Button
      variant={selected === i ? "secondary" : "ghost"}
      size="sm"
      aria-pressed={selected === i}
      onclick={() => onSelect(i)}
    >
      {item.label}
    </Button>
  {/each}
</div>
