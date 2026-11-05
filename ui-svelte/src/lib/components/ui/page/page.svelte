<script lang="ts">
  import type { Snippet } from "svelte";
  import { cn } from "$lib/utils.js";

  // Page layout used by every route: a header (title, optional description
  // and actions) above the content, at one of three content widths.
  let {
    title,
    description,
    actions,
    width = "wide",
    fill = false,
    class: className,
    children,
  }: {
    title: string;
    description?: string;
    actions?: Snippet;
    /** full: edge to edge; wide: capped at --page-max-width; narrow: forms and settings. */
    width?: "full" | "wide" | "narrow";
    /** Content fills the remaining height (lists and editors that scroll inside). */
    fill?: boolean;
    class?: string;
    children: Snippet;
  } = $props();
</script>

<div
  data-slot="page"
  class={cn(
    "mx-auto flex w-full flex-col gap-(--page-gap)",
    width === "wide" && "max-w-(--page-max-width)",
    width === "narrow" && "max-w-3xl",
    fill && "h-full min-h-0",
    className,
  )}
>
  <header class="flex flex-wrap items-start gap-x-4 gap-y-2">
    <div class="min-w-[min(100%,14rem)] flex-1">
      <h1 class="truncate text-title tracking-tight">{title}</h1>
      {#if description}
        <p class="text-muted-foreground mt-0.5 text-sm">{description}</p>
      {/if}
    </div>
    {#if actions}
      <div class="flex shrink-0 flex-wrap items-center gap-2">{@render actions()}</div>
    {/if}
  </header>
  <div class={cn("flex min-w-0 flex-col gap-(--page-gap)", fill && "min-h-0 flex-1")}>
    {@render children()}
  </div>
</div>
