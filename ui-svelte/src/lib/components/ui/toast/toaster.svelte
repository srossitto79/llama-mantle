<script lang="ts">
  import { X, CircleCheck, CircleAlert, Info } from "@lucide/svelte";
  import { alertVariants } from "../alert/index.js";
  import { toast, type Toaster } from "./toast.js";

  let { toaster = toast }: { toaster?: Toaster } = $props();
  const items = $derived(toaster.items);
</script>

<div
  class="pointer-events-none fixed right-0 bottom-0 z-50 flex w-full max-w-sm flex-col gap-2 p-4"
  aria-live="polite"
>
  {#each $items as t (t.id)}
    <div
      class={alertVariants({ variant: t.kind === "error" ? "destructive" : t.kind === "success" ? "success" : "info", class: "bg-popover pointer-events-auto pr-9 shadow-(--overlay-shadow)" })}
      role={t.kind === "error" ? "alert" : "status"}
    >
      {#if t.kind === "error"}
        <CircleAlert />
      {:else if t.kind === "success"}
        <CircleCheck />
      {:else}
        <Info />
      {/if}
      <div class="font-medium">{t.title}</div>
      {#if t.description}
        <div class="text-muted-foreground text-sm break-words">{t.description}</div>
      {/if}
      <button
        type="button"
        class="text-muted-foreground hover:text-foreground absolute top-2.5 right-2.5"
        aria-label="Dismiss"
        onclick={() => toaster.dismiss(t.id)}
      >
        <X class="size-4" />
      </button>
    </div>
  {/each}
</div>
