<script lang="ts">
  // Review step before any write: the staged changes, the validation result,
  // what the reload does to running models, and the exact YAML diff.
  import { configDraft } from "$lib/config/draft";
  import { configSession } from "$lib/config/session";
  import { configApi, ConfigApiError, type ConfigPreview } from "$lib/config/configApi";
  import { describeChange } from "$lib/config/describe";
  import { diffHunks, diffStats } from "$lib/config/diff";
  import { models as runtimeModels } from "../../stores/api";
  import { toast } from "$lib/components/ui/toast/index.js";
  import * as Sheet from "$lib/components/ui/sheet/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import DiffView from "./DiffView.svelte";
  import { Loader2, X, CircleAlert, Info, RefreshCw } from "@lucide/svelte";

  let {
    open = $bindable(false),
    /** Raw-editor mode: review a whole new file instead of the draft. */
    rawText = null,
    onapplied,
  }: { open?: boolean; rawText?: string | null; onapplied?: () => void } = $props();

  let preview = $state<ConfigPreview | null>(null);
  let error = $state<ConfigApiError | Error | null>(null);
  let loading = $state(false);
  let applying = $state(false);

  const snapshot = $derived($configSession.snapshot);
  const items = $derived($configDraft);
  const running = $derived($runtimeModels.filter((m) => m.state === "ready" || m.state === "starting").map((m) => m.id));
  const afterText = $derived(rawText ?? preview?.yaml ?? null);
  const hunks = $derived(snapshot && afterText !== null ? diffHunks(snapshot.raw, afterText) : []);
  const stats = $derived(diffStats(hunks));
  const problems = $derived(preview?.problems ?? []);
  const failedIndex = $derived(error instanceof ConfigApiError ? error.changeIndex : undefined);
  const canApply = $derived(!loading && !applying && !error && problems.length === 0 && hunks.length > 0);

  async function load(): Promise<void> {
    if (!snapshot) return;
    loading = true;
    error = null;
    preview = null;
    try {
      if (rawText !== null) {
        const v = await configApi.validate(rawText);
        preview = { baseHash: snapshot.hash, hash: "", yaml: rawText, problems: v.problems, reload: v.reload };
      } else {
        preview = await configApi.preview(snapshot.hash, configDraft.changes());
      }
    } catch (e) {
      error = e instanceof Error ? e : new Error(String(e));
    } finally {
      loading = false;
    }
  }

  // Re-preview whenever the sheet opens or the staged changes change.
  $effect(() => {
    void items;
    void rawText;
    if (open) void load();
  });

  async function apply(): Promise<void> {
    if (!snapshot) return;
    applying = true;
    try {
      if (rawText !== null) await configApi.putRaw(rawText, snapshot.hash);
      else await configApi.apply(snapshot.hash, configDraft.changes());
      if (rawText === null) configDraft.discardAll();
      toast.show({ kind: "success", title: "Configuration applied" });
      open = false;
      await configSession.load();
      onapplied?.();
    } catch (e) {
      error = e instanceof Error ? e : new Error(String(e));
      if (e instanceof ConfigApiError && e.problems.length && preview) preview = { ...preview, problems: e.problems };
    } finally {
      applying = false;
    }
  }

  async function reloadBase(): Promise<void> {
    await configSession.load();
    await load();
  }

</script>

<Sheet.Root bind:open>
  <Sheet.Content side="right" class="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
    <Sheet.Header class="border-b px-5 py-4">
      <Sheet.Title>Review changes</Sheet.Title>
      <Sheet.Description>
        {#if hunks.length}+{stats.added} −{stats.removed} lines in config.yaml{:else if !loading}No changes to config.yaml{/if}
      </Sheet.Description>
    </Sheet.Header>

    <div class="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
      {#if rawText === null}
        <section class="space-y-1.5">
          <h3 class="text-label text-muted-foreground font-semibold uppercase">Staged · {items.length}</h3>
          <ul class="divide-y rounded-(--panel-radius) border">
            {#each items as it, i (it.key)}
              <li class={["flex items-center gap-2 px-3 py-1.5 text-sm", failedIndex === i && "bg-destructive/8"]}>
                <span class="min-w-0 flex-1 truncate font-mono text-[0.8125rem]">{describeChange(it.change)}</span>
                <Button variant="ghost" size="icon-xs" aria-label="Discard" onclick={() => configDraft.discard(it.key)}><X /></Button>
              </li>
            {:else}
              <li class="text-muted-foreground px-3 py-2 text-sm">Nothing staged</li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if loading}
        <p class="text-muted-foreground flex items-center gap-2 text-sm"><Loader2 class="size-4 animate-spin" />Checking</p>
      {:else if error}
        <Alert.Root variant="destructive">
          <CircleAlert />
          <Alert.Title>{error.message}</Alert.Title>
          {#if error instanceof ConfigApiError && error.code === "stale"}
            <Alert.Description>
              <Button variant="outline" size="xs" class="mt-1" onclick={reloadBase}><RefreshCw />Reload config.yaml</Button>
            </Alert.Description>
          {:else if failedIndex !== undefined}
            <Alert.Description>Highlighted item</Alert.Description>
          {/if}
        </Alert.Root>
      {:else if preview}
        {#if problems.length}
          <Alert.Root variant="destructive">
            <CircleAlert />
            <Alert.Title>{problems.length === 1 ? "1 problem" : `${problems.length} problems`}</Alert.Title>
            <Alert.Description>
              <ul class="space-y-0.5">
                {#each problems as p, i (i)}
                  <li>{#if p.line}<span class="font-mono">line {p.line}</span> · {/if}{p.message}</li>
                {/each}
              </ul>
            </Alert.Description>
          </Alert.Root>
        {/if}
        {#if hunks.length}
          <Alert.Root>
            <Info />
            {#if preview.reload === "all"}
              <Alert.Title>
                {running.length === 0 ? "No models running" : `Reload stops ${running.length === 1 ? "1 running model" : `${running.length} running models`}`}
              </Alert.Title>
              {#if running.length}<Alert.Description class="font-mono text-xs">{running.join(", ")}</Alert.Description>{/if}
            {:else}
              <Alert.Title>Applies on restart</Alert.Title>
            {/if}
          </Alert.Root>
        {/if}
      {/if}

      {#if hunks.length}
        <DiffView {hunks} />
      {/if}
    </div>

    <Sheet.Footer class="flex-row justify-end gap-2 border-t px-5 py-3">
      {#if rawText === null && items.length}
        <Button variant="ghost" class="mr-auto" onclick={() => configDraft.discardAll()}>Discard all</Button>
      {/if}
      <Button variant="outline" onclick={() => (open = false)}>Close</Button>
      <Button disabled={!canApply} onclick={apply}>
        {#if applying}<Loader2 class="animate-spin" />{/if}Apply
      </Button>
    </Sheet.Footer>
  </Sheet.Content>
</Sheet.Root>
