<script lang="ts">
  // Design-direction prototype of the guided config editor (read-only).
  // Phase 2 turns it into the editor; nothing here writes.
  import { onMount } from "svelte";
  import { models as runtimeModels } from "../stores/api";
  import { statusDotColor } from "../stores/modelLoad";
  import { configApi, type SourceDocument, type SourceEntry, type SourceField } from "$lib/config/configApi";
  import SourceValue from "../components/config/SourceValue.svelte";
  import Tag from "../components/Tag.svelte";
  import * as Page from "$lib/components/ui/page/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Search, Link2, Unlink, Plus, FileDiff } from "@lucide/svelte";

  let doc = $state<SourceDocument | null>(null);
  let error = $state<string | null>(null);
  let query = $state("");
  let selectedId = $state<string | null>(null);

  onMount(async () => {
    try {
      doc = await configApi.readSource();
      selectedId = doc.sections.models[0]?.id ?? null;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  });

  let macros = $derived(Object.fromEntries((doc?.macros ?? []).map((m) => [m.name, m.value])));
  let modelList = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const all = doc?.sections.models ?? [];
    return q ? all.filter((m) => m.id.toLowerCase().includes(q)) : all;
  });
  let selected = $derived(doc?.sections.models.find((m) => m.id === selectedId) ?? null);
  let runtime = $derived($runtimeModels.find((m) => m.id === selectedId));

  const sections: { title: string; fields: string[] }[] = [
    { title: "Command", fields: ["cmd", "cmdStop", "env", "macros"] },
    { title: "Routing & proxy", fields: ["proxy", "checkEndpoint", "useModelName", "aliases", "apiKey", "timeouts"] },
    { title: "Request filters", fields: ["filters"] },
    { title: "Lifecycle", fields: ["ttl", "unloadTimeout", "concurrencyLimit", "sendLoadingState"] },
    { title: "Metadata", fields: ["name", "description", "unlisted", "capabilities", "metadata"] },
  ];
  const known = new Set(sections.flatMap((s) => s.fields));

  const labels: Record<string, string> = {
    cmd: "Command",
    cmdStop: "Stop command",
    env: "Environment",
    macros: "Model macros",
    proxy: "Proxy URL",
    checkEndpoint: "Health check",
    useModelName: "Upstream model name",
    aliases: "Aliases",
    apiKey: "Upstream API key",
    timeouts: "Timeouts",
    filters: "Filters",
    ttl: "Unload after (s)",
    unloadTimeout: "Unload timeout (s)",
    concurrencyLimit: "Concurrency limit",
    sendLoadingState: "Send loading state",
    name: "Display name",
    description: "Description",
    unlisted: "Unlisted",
    capabilities: "Capabilities",
    metadata: "Metadata",
  };

  function otherFields(e: SourceEntry): string[] {
    return e.order.filter((f) => !known.has(f));
  }

  function byIdRows(f: SourceField | undefined): [string, Record<string, unknown>][] {
    const v = f?.value as { setParamsByID?: Record<string, Record<string, unknown>> } | undefined;
    return Object.entries(v?.setParamsByID ?? {});
  }
</script>

{#snippet fieldValue(name: string, f: SourceField)}
  {#if Array.isArray(f.value)}
    <div class="flex flex-wrap gap-1.5">
      {#each f.value as item, i (i)}
        <span class="bg-muted rounded px-1.5 py-0.5"><SourceValue value={item} {macros} /></span>
      {/each}
    </div>
  {:else if typeof f.value === "object" && f.value !== null}
    <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
      {#each Object.entries(f.value) as [k, v] (k)}
        <dt class="text-muted-foreground font-mono text-xs leading-6">{k}</dt>
        <dd class="min-w-0"><SourceValue value={v} {macros} /></dd>
      {/each}
    </dl>
  {:else if typeof f.value === "boolean"}
    <span class="text-sm">{f.value ? "Yes" : "No"}</span>
  {:else}
    <SourceValue value={f.value} {macros} multiline={name === "cmd" || name === "cmdStop"} />
  {/if}
{/snippet}

{#snippet field(e: SourceEntry, name: string)}
  {@const f = e.fields[name]}
  {#if f}
    <div class="grid gap-1.5 py-3 sm:grid-cols-[11rem_1fr] sm:gap-4">
      <div class="text-sm font-medium sm:pt-0.5">{labels[name] ?? name}</div>
      <div class="min-w-0">
        {#if f.alias || f.inheritedFrom}
          <div class="mb-2 flex flex-wrap items-center gap-2">
            <span class="text-muted-foreground inline-flex items-center gap-1 text-xs">
              <Link2 class="size-3.5" />Inherited from <code class="text-foreground font-mono">{f.alias ?? f.inheritedFrom}</code>
            </span>
            <Button variant="ghost" size="sm" class="h-6 px-2 text-xs" disabled><Unlink class="size-3" />Detach</Button>
          </div>
        {/if}
        {#if name === "filters" && byIdRows(f).length > 0}
          <table class="w-full text-sm">
            <thead>
              <tr class="text-muted-foreground border-b text-left text-xs">
                <th class="py-1.5 pr-4 pl-0 font-medium">Suffix</th>
                <th class="py-1.5 pl-0 font-medium">Parameters</th>
              </tr>
            </thead>
            <tbody class="divide-y">
              {#each byIdRows(f) as [suffix, params] (suffix)}
                <tr>
                  <td class="py-1.5 pr-4 pl-0 align-top"><SourceValue value={suffix.replace("${MODEL_ID}", "")} {macros} /></td>
                  <td class="py-1.5 pl-0">
                    {#each Object.entries(params) as [k, v] (k)}
                      <span class="bg-muted mr-1.5 inline-block rounded px-1.5 py-0.5 font-mono text-xs">{k}: {JSON.stringify(v)}</span>
                    {/each}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {:else}
          {@render fieldValue(name, f)}
        {/if}
      </div>
    </div>
  {/if}
{/snippet}

<Page.Root title="Configuration" width="full" fill>
  {#snippet actions()}
    <Button variant="outline" size="sm" disabled><Plus class="size-3.5" />Add model</Button>
    <Button size="sm" disabled><FileDiff class="size-3.5" />Review changes</Button>
  {/snippet}

  <Tabs.Root value="models" class="min-h-0 flex-1">
    <Tabs.List class="max-w-full justify-start overflow-x-auto">
      <Tabs.Trigger value="models">Models <span class="text-muted-foreground ml-1 text-xs">{doc?.sections.models.length ?? ""}</span></Tabs.Trigger>
      <Tabs.Trigger value="groups">Groups <span class="text-muted-foreground ml-1 text-xs">{doc?.sections.groups.length ?? ""}</span></Tabs.Trigger>
      <Tabs.Trigger value="profiles">Profiles</Tabs.Trigger>
      <Tabs.Trigger value="macros">Macros <span class="text-muted-foreground ml-1 text-xs">{doc?.macros.length ?? ""}</span></Tabs.Trigger>
      <Tabs.Trigger value="peers">Peers</Tabs.Trigger>
      <Tabs.Trigger value="raw">Raw</Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="models" class="min-h-0 flex-1">
      {#if error}
        <p class="text-destructive text-sm">{error}</p>
      {:else}
        <div class="grid h-full min-h-0 gap-(--page-gap) md:grid-cols-[18rem_1fr]">
          <Card.Root class="max-h-64 min-h-0 gap-0 py-0 md:max-h-none">
            <div class="bg-(--panel-header-bg) border-b p-2">
              <div class="relative">
                <Search class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
                <Input bind:value={query} placeholder="Filter" class="h-8 pl-8" aria-label="Filter models" />
              </div>
            </div>
            <ul class="min-h-0 flex-1 overflow-y-auto p-1">
              {#each modelList as m (m.id)}
                {@const rt = $runtimeModels.find((r) => r.id === m.id)}
                <li>
                  <button
                    type="button"
                    onclick={() => (selectedId = m.id)}
                    class={[
                      "flex w-full items-center gap-2 rounded-[calc(var(--radius)-2px)] px-2.5 py-1.5 text-left text-sm",
                      m.id === selectedId ? "bg-accent text-accent-foreground font-medium" : "hover:bg-muted/60",
                    ]}
                  >
                    <span class="size-1.5 shrink-0 rounded-full {statusDotColor(rt)}" aria-hidden="true"></span>
                    <span class="truncate font-mono text-[0.8125rem]">{m.id}</span>
                  </button>
                </li>
              {/each}
            </ul>
          </Card.Root>

          {#if selected}
            <Card.Root class="min-h-0 gap-0 py-0">
              <div class="bg-(--panel-header-bg) flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-5 py-3">
                <div class="min-w-0 flex-1">
                  <h2 class="truncate pb-0 text-base font-semibold">{selected.fields.name?.value ?? selected.id}</h2>
                  <p class="text-muted-foreground font-mono text-xs">{selected.id} · line {selected.line}</p>
                </div>
                {#if runtime?.state === "ready"}
                  <Tag class="bg-success/15 text-foreground">Running</Tag>
                {/if}
              </div>
              <div class="min-h-0 flex-1 overflow-y-auto px-5 pb-4">
                {#each sections as s (s.title)}
                  {#if s.fields.some((f) => selected.fields[f])}
                    <section class="border-b last:border-b-0">
                      <h3 class="text-muted-foreground pt-4 pb-0 text-xs font-semibold tracking-wide uppercase">{s.title}</h3>
                      <div class="divide-y">
                        {#each s.fields as f (f)}{@render field(selected, f)}{/each}
                      </div>
                    </section>
                  {/if}
                {/each}
                {#if otherFields(selected).length > 0}
                  <section>
                    <h3 class="text-muted-foreground pt-4 pb-0 text-xs font-semibold tracking-wide uppercase">Other</h3>
                    <div class="divide-y">
                      {#each otherFields(selected) as f (f)}{@render field(selected, f)}{/each}
                    </div>
                  </section>
                {/if}
              </div>
            </Card.Root>
          {/if}
        </div>
      {/if}
    </Tabs.Content>
  </Tabs.Root>
</Page.Root>
