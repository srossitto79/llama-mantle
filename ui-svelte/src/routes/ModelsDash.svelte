<script lang="ts">
  import { link } from "svelte-spa-router";
  import { models, unloadAllModels } from "../stores/api";
  import { statusDotColor } from "../stores/modelLoad";
  import { showUnlistedModels as showUnlisted } from "../stores/modelDisplay";
  import ModelLoadButton from "../components/ModelLoadButton.svelte";
  import Tag from "../components/Tag.svelte";
  import * as Page from "$lib/components/ui/page/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { PowerOff, Loader2, ExternalLink, Search } from "@lucide/svelte";

  let unloadingAll = $state(false);
  let query = $state("");

  let listed = $derived($showUnlisted ? $models : $models.filter((m) => !m.unlisted));
  let visibleModels = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return listed;
    return listed.filter(
      (m) =>
        m.id.toLowerCase().includes(q) ||
        (m.name ?? "").toLowerCase().includes(q) ||
        (m.aliases ?? []).some((a) => a.toLowerCase().includes(q)),
    );
  });
  let readyCount = $derived($models.filter((m) => m.state === "ready").length);
  let unlistedCount = $derived($models.filter((m) => m.unlisted).length);

  async function handleUnloadAll(): Promise<void> {
    unloadingAll = true;
    try {
      await unloadAllModels();
    } catch (e) {
      console.error(e);
    } finally {
      unloadingAll = false;
    }
  }
</script>

<Page.Root title="Models" description="{$models.length} configured · {readyCount} running" fill>
  {#snippet actions()}
    <Button variant="outline" size="sm" onclick={handleUnloadAll} disabled={readyCount === 0 || unloadingAll}>
      {#if unloadingAll}
        <Loader2 class="size-3.5 animate-spin" />
      {:else}
        <PowerOff class="size-3.5" />
      {/if}
      Unload all
    </Button>
  {/snippet}

  <Card.Root class="min-h-0 flex-1 gap-0 py-0">
    <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-3 border-b px-4 py-2.5">
      <div class="relative w-full max-w-xs">
        <Search class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
        <Input bind:value={query} placeholder="Filter models" class="h-8 pl-8" aria-label="Filter models" />
      </div>
      <div class="ml-auto flex items-center gap-2">
        <Switch.Root
          id="show-unlisted-toggle"
          checked={$showUnlisted}
          onCheckedChange={(v) => showUnlisted.set(v)}
        />
        <Label.Root for="show-unlisted-toggle" class="text-muted-foreground text-sm font-normal">
          Unlisted ({unlistedCount})
        </Label.Root>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto">
      {#if visibleModels.length === 0}
        <div class="text-muted-foreground px-4 py-12 text-center text-sm">
          {query ? "No matching models" : "No models configured"}
        </div>
      {:else}
        <ul class="divide-y">
          {#each visibleModels as model (model.id)}
            <li class="hover:bg-muted/50 flex items-center gap-3 px-4 py-2.5">
              <span class="size-2 shrink-0 rounded-full {statusDotColor(model)}" aria-hidden="true"></span>
              <a href="/models/{encodeURIComponent(model.id)}" use:link class="min-w-0 flex-1">
                <div class="truncate text-sm font-medium">{model.name || model.id}</div>
                <div class="text-muted-foreground flex min-w-0 items-center gap-1.5 text-xs">
                  <span class="truncate font-mono">{model.id}</span>
                  {#if model.aliases?.length}
                    <span class="shrink-0" title={model.aliases.join("\n")}>
                      · {model.aliases.length} {model.aliases.length === 1 ? "alias" : "aliases"}
                    </span>
                  {/if}
                </div>
              </a>
              {#if model.unlisted}
                <Tag class="hidden px-1.5 text-[0.6875rem] sm:inline">Unlisted</Tag>
              {/if}
              <span class="text-muted-foreground hidden w-16 text-right text-xs capitalize sm:inline">{model.state}</span>
              <a
                href="/upstream/{encodeURIComponent(model.id)}/"
                target="_blank"
                rel="noopener noreferrer"
                class="text-muted-foreground hover:text-foreground hidden sm:inline"
                title="Open model server"
                aria-label="Open model server"
              >
                <ExternalLink class="size-4" />
              </a>
              <ModelLoadButton {model} />
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </Card.Root>
</Page.Root>
