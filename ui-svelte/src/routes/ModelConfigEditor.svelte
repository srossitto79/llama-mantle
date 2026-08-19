<script lang="ts">
  import { onMount } from "svelte";
  import { link } from "svelte-spa-router";
  import { Plus, Trash2 } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import * as Switch from "$lib/components/ui/switch/index.js";
  import BackendFlagForm from "../components/BackendFlagForm.svelte";
  import {
    listConfigModels, upsertConfigModel, deleteConfigModel,
    listConfigGroups, upsertConfigGroup, deleteConfigGroup,
    listBackends, listStudioResources, tokenizeCmd, buildCmd,
  } from "../lib/mantleApi";
  import { DEFAULT_MODEL_CONFIG, DEFAULT_GROUP_CONFIG } from "../lib/types";
  import type { BackendEntry, ConfigGroupConfig, ConfigModelConfig, StudioResource } from "../lib/types";

  type ModelDraft = {
    key: string;
    originalId: string | null;
    draftId: string;
    model: ConfigModelConfig;
    argv: string[];
    saving: boolean;
    message: string;
  };

  type GroupDraft = {
    key: string;
    originalName: string | null;
    draftName: string;
    group: ConfigGroupConfig;
    saving: boolean;
    message: string;
  };

  let loading = $state(true);
  let loadError = $state("");
  let backends = $state<BackendEntry[]>([]);
  let resources = $state<StudioResource[]>([]);
  let modelDrafts = $state<ModelDraft[]>([]);
  let groupDrafts = $state<GroupDraft[]>([]);
  let keyCounter = 0;

  function newKey(): string {
    keyCounter += 1;
    return `draft-${keyCounter}`;
  }

  async function load() {
    loading = true;
    loadError = "";
    try {
      const [models, groups, be, res] = await Promise.all([
        listConfigModels(),
        listConfigGroups(),
        listBackends(),
        listStudioResources().catch(() => [] as StudioResource[]),
      ]);
      backends = be;
      resources = res;
      modelDrafts = await Promise.all(
        models.map(async ({ id, ...model }) => ({
          key: newKey(),
          originalId: id,
          draftId: id,
          model,
          argv: await tokenizeCmd(model.cmd || "").catch(() => []),
          saving: false,
          message: "",
        })),
      );
      groupDrafts = groups.map(({ name, ...group }) => ({
        key: newKey(),
        originalName: name,
        draftName: name,
        group,
        saving: false,
        message: "",
      }));
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function addModel() {
    modelDrafts = [
      ...modelDrafts,
      { key: newKey(), originalId: null, draftId: "", model: { ...DEFAULT_MODEL_CONFIG }, argv: ["llama-server"], saving: false, message: "" },
    ];
  }

  async function saveModel(draft: ModelDraft) {
    const id = draft.draftId.trim();
    if (!id) {
      draft.message = "Model ID is required";
      return;
    }
    draft.saving = true;
    draft.message = "";
    try {
      draft.model.cmd = await buildCmd(draft.argv);
      if (draft.originalId && draft.originalId !== id) {
        await deleteConfigModel(draft.originalId);
      }
      await upsertConfigModel(id, draft.model);
      draft.originalId = id;
      draft.message = "Saved";
    } catch (e) {
      draft.message = e instanceof Error ? e.message : String(e);
    } finally {
      draft.saving = false;
    }
  }

  async function removeModel(draft: ModelDraft) {
    if (draft.originalId) {
      if (!confirm(`Delete model "${draft.originalId}"?`)) return;
      try {
        await deleteConfigModel(draft.originalId);
      } catch (e) {
        draft.message = e instanceof Error ? e.message : String(e);
        return;
      }
    }
    modelDrafts = modelDrafts.filter((d) => d.key !== draft.key);
  }

  function addGroup() {
    groupDrafts = [
      ...groupDrafts,
      { key: newKey(), originalName: null, draftName: "", group: { ...DEFAULT_GROUP_CONFIG, members: [] }, saving: false, message: "" },
    ];
  }

  async function saveGroup(draft: GroupDraft) {
    const name = draft.draftName.trim();
    if (!name) {
      draft.message = "Group name is required";
      return;
    }
    draft.saving = true;
    draft.message = "";
    try {
      if (draft.originalName && draft.originalName !== name) {
        await deleteConfigGroup(draft.originalName);
      }
      await upsertConfigGroup(name, draft.group);
      draft.originalName = name;
      draft.message = "Saved";
    } catch (e) {
      draft.message = e instanceof Error ? e.message : String(e);
    } finally {
      draft.saving = false;
    }
  }

  async function removeGroup(draft: GroupDraft) {
    if (draft.originalName) {
      if (!confirm(`Delete group "${draft.originalName}"?`)) return;
      try {
        await deleteConfigGroup(draft.originalName);
      } catch (e) {
        draft.message = e instanceof Error ? e.message : String(e);
        return;
      }
    }
    groupDrafts = groupDrafts.filter((d) => d.key !== draft.key);
  }

  function toggleMember(draft: GroupDraft, modelId: string, checked: boolean) {
    const members = new Set(draft.group.members ?? []);
    if (checked) members.add(modelId);
    else members.delete(modelId);
    draft.group.members = Array.from(members);
  }

  function selectedBackendValue(draft: ModelDraft): string {
    return draft.argv[0] ?? "llama-server";
  }

  function selectedBackendName(draft: ModelDraft): string {
    return backends.find((b) => b.path === draft.argv[0])?.name ?? "";
  }

  function setBackend(draft: ModelDraft, value: string) {
    draft.argv = [value, ...draft.argv.slice(1)];
  }

  function aliasesText(draft: ModelDraft): string {
    return (draft.model.aliases ?? []).join(", ");
  }

  function setAliasesText(draft: ModelDraft, text: string) {
    draft.model.aliases = text.split(",").map((s) => s.trim()).filter(Boolean);
  }

  function modelIdOf(draft: ModelDraft): string {
    return draft.originalId ?? draft.draftId;
  }
</script>

<div class="space-y-4 p-2">
  <div class="flex items-center justify-between">
    <h3 class="text-lg font-semibold">Model Config</h3>
    <a class="text-primary text-sm underline" href="/config" use:link>Edit raw YAML</a>
  </div>

  {#if loading}
    <p class="text-muted-foreground text-sm">Loading…</p>
  {:else if loadError}
    <p class="text-destructive text-sm">Failed to load config: {loadError}</p>
  {:else}
    <section class="space-y-3">
      <div class="flex items-center justify-between">
        <h4 class="text-sm font-semibold">Models</h4>
        <Button size="sm" variant="outline" onclick={addModel}><Plus class="size-4" />Add model</Button>
      </div>

      {#each modelDrafts as draft (draft.key)}
        <Card.Root>
          <Card.Header>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <Input class="max-w-xs font-mono text-sm" bind:value={draft.draftId} placeholder="model-id" />
              <div class="flex items-center gap-2">
                {#if draft.message}
                  <span class={`text-xs ${draft.message === "Saved" ? "text-primary" : "text-destructive"}`}>{draft.message}</span>
                {/if}
                <Button size="sm" onclick={() => saveModel(draft)} disabled={draft.saving}>{draft.saving ? "Saving…" : "Save"}</Button>
                <Button size="sm" variant="outline" onclick={() => removeModel(draft)}><Trash2 class="size-4" /></Button>
              </div>
            </div>
          </Card.Header>
          <Card.Content class="space-y-3">
            <div class="grid gap-3 sm:grid-cols-3">
              <div class="space-y-1">
                <Label.Root for={`backend-${draft.key}`}>Backend</Label.Root>
                <select
                  id={`backend-${draft.key}`}
                  class="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
                  value={selectedBackendValue(draft)}
                  onchange={(e) => setBackend(draft, e.currentTarget.value)}
                >
                  <option value="llama-server">llama-server (default)</option>
                  {#each backends as be (be.path)}<option value={be.path}>{be.name}</option>{/each}
                </select>
              </div>
              <div class="space-y-1">
                <Label.Root for={`ttl-${draft.key}`}>TTL seconds (-1 = global default)</Label.Root>
                <Input id={`ttl-${draft.key}`} type="number" bind:value={draft.model.ttl} />
              </div>
              <div class="space-y-1">
                <Label.Root for={`aliases-${draft.key}`}>Aliases</Label.Root>
                <Input id={`aliases-${draft.key}`} value={aliasesText(draft)} oninput={(e) => setAliasesText(draft, e.currentTarget.value)} placeholder="comma, separated" />
              </div>
            </div>
            <BackendFlagForm backendName={selectedBackendName(draft)} bind:argv={draft.argv} {resources} />
          </Card.Content>
        </Card.Root>
      {/each}
      {#if modelDrafts.length === 0}
        <p class="text-muted-foreground text-sm">No models configured yet.</p>
      {/if}
    </section>

    <section class="space-y-3">
      <div class="flex items-center justify-between">
        <h4 class="text-sm font-semibold">Groups</h4>
        <Button size="sm" variant="outline" onclick={addGroup}><Plus class="size-4" />Add group</Button>
      </div>

      {#each groupDrafts as draft (draft.key)}
        <Card.Root>
          <Card.Header>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <Input class="max-w-xs font-mono text-sm" bind:value={draft.draftName} placeholder="group-name" />
              <div class="flex items-center gap-2">
                {#if draft.message}
                  <span class={`text-xs ${draft.message === "Saved" ? "text-primary" : "text-destructive"}`}>{draft.message}</span>
                {/if}
                <Button size="sm" onclick={() => saveGroup(draft)} disabled={draft.saving}>{draft.saving ? "Saving…" : "Save"}</Button>
                <Button size="sm" variant="outline" onclick={() => removeGroup(draft)}><Trash2 class="size-4" /></Button>
              </div>
            </div>
          </Card.Header>
          <Card.Content class="space-y-3">
            <div class="flex flex-wrap gap-4">
              <label class="flex items-center gap-2 text-sm"><Switch.Root checked={draft.group.swap} onCheckedChange={(v) => (draft.group.swap = v)} />Swap</label>
              <label class="flex items-center gap-2 text-sm"><Switch.Root checked={draft.group.exclusive} onCheckedChange={(v) => (draft.group.exclusive = v)} />Exclusive</label>
              <label class="flex items-center gap-2 text-sm"><Switch.Root checked={draft.group.persistent} onCheckedChange={(v) => (draft.group.persistent = v)} />Persistent</label>
            </div>
            <div class="space-y-1">
              <Label.Root>Members</Label.Root>
              <div class="grid gap-1 sm:grid-cols-3">
                {#each modelDrafts as m (m.key)}
                  {@const id = modelIdOf(m)}
                  {#if id}
                    <label class="flex items-center gap-2 text-sm">
                      <input type="checkbox" checked={(draft.group.members ?? []).includes(id)} onchange={(e) => toggleMember(draft, id, e.currentTarget.checked)} />
                      {id}
                    </label>
                  {/if}
                {/each}
              </div>
            </div>
          </Card.Content>
        </Card.Root>
      {/each}
      {#if groupDrafts.length === 0}
        <p class="text-muted-foreground text-sm">No groups configured yet.</p>
      {/if}
    </section>
  {/if}
</div>
