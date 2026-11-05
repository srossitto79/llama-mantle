<script lang="ts">
  // Styled native <select>: for plain option lists where a native control is
  // the better fit (forms, long lists, mobile). Use ui/select for rich items.
  import type { HTMLSelectAttributes } from "svelte/elements";
  import { ChevronDown } from "@lucide/svelte";
  import { cn, type WithElementRef } from "$lib/utils.js";

  let {
    ref = $bindable(null),
    value = $bindable(),
    class: className,
    children,
    ...restProps
  }: WithElementRef<HTMLSelectAttributes, HTMLSelectElement> = $props();
</script>

<div data-slot="native-select" class={cn("relative w-full min-w-0", className)}>
  <select
    bind:this={ref}
    bind:value
    class="border-input dark:bg-input/30 focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 h-8 w-full min-w-0 appearance-none rounded-lg border bg-transparent py-1 pr-8 pl-2.5 text-sm outline-none focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50 [&>option]:bg-popover [&>option]:text-popover-foreground"
    {...restProps}
  >
    {@render children?.()}
  </select>
  <ChevronDown class="text-muted-foreground pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2" />
</div>
