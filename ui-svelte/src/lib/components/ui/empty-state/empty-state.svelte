<script lang="ts">
  import type { Component, Snippet } from "svelte";
  import { cn } from "$lib/utils.js";

  let {
    title,
    description,
    icon: Icon,
    action,
    full = false,
    class: className,
  }: {
    title: string;
    description?: string;
    icon?: Component;
    action?: Snippet;
    /** Fill the parent's height instead of flexing. */
    full?: boolean;
    class?: string;
  } = $props();
</script>

<div
  data-slot="empty-state"
  class={cn("flex flex-col items-center justify-center gap-2 px-4 py-10 text-center", full ? "h-full" : "flex-1", className)}
>
  {#if Icon}
    <Icon class="text-muted-foreground mb-1 size-6" />
  {/if}
  <p class="text-sm font-medium">{title}</p>
  {#if description}
    <p class="text-muted-foreground max-w-sm text-sm">{description}</p>
  {/if}
  {#if action}
    <div class="mt-2">{@render action()}</div>
  {/if}
</div>
