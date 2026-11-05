<script lang="ts">
  // Form field: label, control, optional description, optional error. The
  // control snippet receives the ids to wire up for label and aria.
  import type { Snippet } from "svelte";
  import { cn } from "$lib/utils.js";

  let {
    label,
    id = `field-${Math.random().toString(36).slice(2, 9)}`,
    description,
    error,
    required = false,
    layout = "stacked",
    class: className,
    control,
    labelExtra,
  }: {
    label: string;
    id?: string;
    description?: string;
    error?: string | null;
    required?: boolean;
    /** stacked: label above control; inline: label left of control from sm up. */
    layout?: "stacked" | "inline";
    class?: string;
    control: Snippet<[{ id: string; describedBy: string | undefined; invalid: boolean }]>;
    labelExtra?: Snippet;
  } = $props();

  const descId = $derived(description ? `${id}-desc` : undefined);
  const errId = $derived(error ? `${id}-err` : undefined);
  const describedBy = $derived([descId, errId].filter(Boolean).join(" ") || undefined);
</script>

<div
  data-slot="field"
  class={cn(
    "grid gap-1.5",
    layout === "inline" && "sm:grid-cols-[11rem_1fr] sm:items-start sm:gap-x-4",
    className,
  )}
>
  <div class={cn("flex items-center gap-2", layout === "inline" && "sm:min-h-8")}>
    <label for={id} class="text-sm font-medium">
      {label}{#if required}<span class="text-muted-foreground"> *</span>{/if}
    </label>
    {@render labelExtra?.()}
  </div>
  <div class="grid min-w-0 gap-1.5">
    {@render control({ id, describedBy, invalid: !!error })}
    {#if description}
      <p id={descId} class="text-muted-foreground text-xs">{description}</p>
    {/if}
    {#if error}
      <p id={errId} class="text-destructive text-xs">{error}</p>
    {/if}
  </div>
</div>
