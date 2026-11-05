<script lang="ts">
  import { params, link } from "svelte-spa-router";
  import { models } from "../stores/api";
  import { statusDotColor } from "../stores/modelLoad";
  import type { Model } from "../lib/types";
  import ModelLoadButton from "../components/ModelLoadButton.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Tabs, TabsList, TabsTrigger, TabsContent } from "$lib/components/ui/tabs/index.js";
  import { ExternalLink } from "@lucide/svelte";
  import ModelActivityTab from "../components/model/ModelActivityTab.svelte";
  import ModelLogsTab from "../components/model/ModelLogsTab.svelte";
  import ModelDetailsTab from "../components/model/ModelDetailsTab.svelte";

  let modelId = $derived($params?.id ?? "");

  let model = $derived<Model | undefined>($models.find((m) => m.id === modelId));
  import * as Page from "$lib/components/ui/page/index.js";
</script>

<Page.Root title={model ? model.name || model.id : modelId} width="full" fill>
  {#if !model}
    <Card.Root class="shrink-0 p-6">
      <p class="text-muted-foreground">Model “{modelId}” not found.</p>
      <a href="/models" use:link class="text-primary hover:underline">Back to Models</a>
    </Card.Root>
  {:else}
    <Card.Root class="shrink-0 gap-0 overflow-hidden py-0">
      <Card.Header class="shrink-0 gap-2 border-b px-4 py-3">
        <div class="flex items-center gap-2">
          <span class={`size-2.5 shrink-0 rounded-full ${statusDotColor(model)}`}></span>
          {#if model.name && model.name !== model.id}<span class="text-muted-foreground text-sm">{model.id}</span>{/if}
          <span class="text-muted-foreground text-xs uppercase tracking-wide">{model.state}</span>
          <div class="ml-auto flex items-center gap-2">
            <a
              href={`/upstream/${encodeURIComponent(modelId)}/`}
              target="_blank"
              rel="noopener noreferrer"
              class="text-muted-foreground hover:text-foreground"
              title="Open model server"
              aria-label="Open model server"
            >
              <ExternalLink class="size-4" />
            </a>
            <ModelLoadButton {model} size="sm" />
          </div>
        </div>
        {#if model.description}
          <p class="text-muted-foreground text-sm"><em>{model.description}</em></p>
        {/if}
        {#if model.aliases && model.aliases.length > 0}
          <p class="text-muted-foreground text-xs">Aliases: {model.aliases.join(", ")}</p>
        {/if}
      </Card.Header>
    </Card.Root>

    <Tabs value="activity" class="min-h-0 flex-1">
      <TabsList variant="line">
        <TabsTrigger value="activity">Activity</TabsTrigger>
        <TabsTrigger value="logs">Logs</TabsTrigger>
        <TabsTrigger value="details">Details</TabsTrigger>
      </TabsList>

      <!-- Activity -->
      <TabsContent value="activity">
        <ModelActivityTab modelId={modelId} />
      </TabsContent>

      <!-- Logs -->
      <TabsContent value="logs" class="min-h-0 flex-1">
        <ModelLogsTab modelId={modelId} />
      </TabsContent>

      <!-- Details -->
      <TabsContent value="details">
        <ModelDetailsTab model={model} />
      </TabsContent>
    </Tabs>
  {/if}
</Page.Root>
