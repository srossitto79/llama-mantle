<script lang="ts">
  import { onMount } from "svelte";
  import { startBuild, cancelBuild, listBackends, deleteBackend, updateBackend } from "../lib/mantleApi";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Page from "$lib/components/ui/page/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { toast } from "$lib/components/ui/toast/index.js";
  import type { BackendEntry } from "../lib/types";
  import { activeBuilds, trackBuild, removeBuild, syncTasks } from "../stores/tasks";

  let backendName = $state("");
  let repo = $state("https://github.com/ggerganov/llama.cpp");
  let branch = $state("main");
  let cmakeFlags = $state("");
  let building = $state(false);

  let backends = $state<BackendEntry[]>([]);
  let loadingBackends = $state(false);
  let builtinBackends = $derived(backends.filter((b) => b.builtin));
  let compiledBackends = $derived(backends.filter((b) => !b.builtin));

  let adoptingBackend = $state<string | null>(null);
  let adoptRepo = $state("");
  let adoptBranch = $state("main");

  async function doBuild() {
    if (!repo.trim() || !backendName.trim()) return;
    building = true;
    const task = await startBuild(repo.trim(), branch.trim() || "main", cmakeFlags.trim(), backendName.trim());
    building = false;
    if (!task) {
      toast.show({ kind: "error", title: "Build failed" });
      return;
    }
    trackBuild(task);
  }

  async function doCancelBuild(taskID: string) {
    await cancelBuild(taskID);
    removeBuild(taskID);
  }

  async function refreshBackends() {
    loadingBackends = true;
    backends = await listBackends();
    loadingBackends = false;
  }

  async function doDeleteBackend(name: string) {
    if (!confirm(`Delete backend "${name}"?`)) return;
    const ok = await deleteBackend(name);
    if (!ok) toast.show({ kind: "error", title: "Delete failed", description: name });
    await refreshBackends();
  }

  async function doUpdateBackend(be: BackendEntry) {
    if (!be.repo) {
      adoptingBackend = be.name;
      adoptRepo = "";
      adoptBranch = "main";
      return;
    }
    const label = `${shortRepo(be.repo)}@${be.branch || "main"}`;
    if (!confirm(`Rebuild "${be.name}" from ${label}? The existing backend will be replaced.`)) return;
    const task = await updateBackend(be.name);
    if (!task) {
      toast.show({ kind: "error", title: "Update failed", description: be.name });
      return;
    }
    trackBuild(task);
    await refreshBackends();
  }

  function cancelAdopt() {
    adoptingBackend = null;
  }

  async function confirmAdopt(name: string) {
    if (!adoptRepo.trim()) return;
    if (!confirm(`Rebuild "${name}" from ${adoptRepo.trim()}@${adoptBranch.trim() || "main"}? The existing backend will be replaced.`)) return;
    const task = await updateBackend(name, adoptRepo.trim(), adoptBranch.trim() || "main");
    adoptingBackend = null;
    if (!task) {
      toast.show({ kind: "error", title: "Update failed", description: name });
      return;
    }
    trackBuild(task);
    await refreshBackends();
  }

  function formatSize(bytes: number): string {
    if (bytes === 0) return "0 B";
    const units = ["B", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return (bytes / Math.pow(1024, i)).toFixed(1) + " " + units[i];
  }

  function shortRepo(url: string): string {
    const parts = url.replace(/https?:\/\//, "").split("/");
    return parts.slice(0, 2).join("/");
  }

  onMount(() => {
    refreshBackends();
    syncTasks();
    const interval = window.setInterval(syncTasks, 5000);
    return () => window.clearInterval(interval);
  });
</script>

<Page.Root title="Backends" width="wide">
  <!-- Build trigger -->
  <Card.Root class="gap-0 p-4 mb-4">
    <h3 class="text-sm font-semibold mb-3">Build llama.cpp Fork</h3>
    <div class="flex flex-col gap-2">
      <div class="flex gap-2 items-center">
        <label for="backend-name" class="text-sm text-muted-foreground w-16">Name:</label>
        <Input id="backend-name" type="text" class="flex-1" placeholder="beellama-cuda" bind:value={backendName} />
      </div>
      <div class="flex gap-2 items-center">
        <label for="backend-repo" class="text-sm text-muted-foreground w-16">Repo:</label>
        <Input id="backend-repo" type="text" class="flex-1" placeholder="https://github.com/user/llama.cpp" bind:value={repo} />
      </div>
      <div class="flex gap-2 items-center">
        <label for="backend-branch" class="text-sm text-muted-foreground w-16">Branch:</label>
        <Input id="backend-branch" type="text" class="flex-1" placeholder="main" bind:value={branch} />
      </div>
      <div class="flex gap-2 items-center">
        <label for="backend-cmake" class="text-sm text-muted-foreground w-16">CMake:</label>
        <Input id="backend-cmake" type="text" class="flex-1" placeholder='-DGGML_CUDA_FORCE_MMQ=ON -DLLAMA_CURL=OFF' bind:value={cmakeFlags} />
      </div>
      <Button class="self-end" onclick={doBuild} disabled={building || !repo.trim() || !backendName.trim()}>
        {building ? "Starting..." : "Build"}
      </Button>
    </div>
  </Card.Root>

  <!-- Active builds -->
  {#if $activeBuilds.size > 0}
    <div class="mb-4">
      <h3 class="text-sm font-semibold mb-2">Active Builds</h3>
      {#each [...$activeBuilds.entries()] as [id, entry]}
        <div class="flex items-center gap-3 mb-2 text-sm p-2 border border-border rounded">
          <div class="flex-1 min-w-0">
            <div class="flex justify-between">
              <span class="truncate font-medium">{shortRepo(entry.task.repo || "")}@{entry.task.branch || "main"}</span>
              <span class="text-muted-foreground">{entry.task.pct >= 0 ? `${entry.task.pct}%` : ""}</span>
            </div>
            <p class="text-xs text-muted-foreground truncate">{entry.task.message}</p>
            {#if entry.task.pct >= 0}
              <div class="w-full bg-muted rounded h-1.5 mt-1">
                <div class="bg-success h-1.5 rounded" style="width: {entry.task.pct}%"></div>
              </div>
            {/if}
          </div>
          {#if entry.task.state === "running"}
            <Button variant="destructive" size="sm" onclick={() => doCancelBuild(id)}>Cancel</Button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  <!-- Built-in backends (baked into the image, on PATH) -->
  {#if builtinBackends.length > 0}
    <div class="mb-4">
      <h3 class="text-sm font-semibold mb-2">Built-in Backends</h3>
      {#each builtinBackends as be (be.name)}
        <div class="py-2 px-2 border-b border-border text-sm flex items-center gap-2">
          <span class="font-medium">{be.name}</span>
          <span class="font-mono text-xs text-muted-foreground">{be.path}</span>
          {#if be.repo}
            <span class="text-muted-foreground text-xs">{shortRepo(be.repo)}{be.branch ? `@${be.branch.slice(0, 12)}` : ""}</span>
          {/if}
          {#if be.size}
            <span class="text-muted-foreground">{formatSize(be.size)}</span>
          {/if}
          <span class="ml-auto text-xs text-muted-foreground italic">{be.kind ?? ""} · part of the image</span>
        </div>
      {/each}
    </div>
  {/if}

  <!-- Compiled backends -->
  <div class="flex-1 min-h-0">
    <div class="flex items-center justify-between mb-2">
      <h3 class="text-sm font-semibold">Compiled Backends</h3>
      <Button variant="outline" size="sm" onclick={refreshBackends}>Refresh</Button>
    </div>
    {#if loadingBackends}
      <p class="text-sm text-muted-foreground">Loading...</p>
    {:else if compiledBackends.length === 0}
      <EmptyState title="No compiled backends" />
    {:else}
      <div class="overflow-y-auto max-h-64">
        {#each compiledBackends as be}
          <div class="py-2 px-2 border-b border-border hover:bg-muted text-sm">
            <div class="flex items-center justify-between">
              <div class="flex-1 min-w-0">
                <span class="font-medium">{be.name}</span>
                {#if be.repo}
                  <span class="text-muted-foreground text-xs ml-2">{shortRepo(be.repo)}@{be.branch || "main"}</span>
                {:else}
                  <span class="text-muted-foreground text-xs ml-2 italic">source unknown</span>
                {/if}
                {#if be.size}
                  <span class="text-muted-foreground ml-2">{formatSize(be.size)}</span>
                {/if}
              </div>
              <div class="flex items-center gap-1 shrink-0 ml-2">
                <Button variant="outline" size="sm" onclick={() => doUpdateBackend(be)}>Update</Button>
                <Button variant="destructive" size="sm" onclick={() => doDeleteBackend(be.name)}>Delete</Button>
              </div>
            </div>
            {#if adoptingBackend === be.name}
              <div class="flex gap-2 items-center mt-2">
                <Input type="text" class="flex-1 h-8 text-xs" placeholder="https://github.com/user/llama.cpp" bind:value={adoptRepo} />
                <Input type="text" class="w-24 h-8 text-xs" placeholder="main" bind:value={adoptBranch} />
                <Button variant="outline" size="sm" onclick={() => confirmAdopt(be.name)} disabled={!adoptRepo.trim()}>Rebuild</Button>
                <Button variant="outline" size="sm" onclick={cancelAdopt}>Cancel</Button>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</Page.Root>
