<script lang="ts">
  import {
    searchHFModels, listHFFiles, startDownload, startRepoDownload, cancelDownload,
    listLocalModels, deleteLocalModel, listBackends, getConfig, putConfig,
    type HFSort, type HFKind
  } from "../lib/mantleApi";
  import type { HFModel, HFFile, LocalModel, LocalModelKind, BackendEntry } from "../lib/types";
  import { activeDownloads, trackDownload, removeDownload, retryDownload, syncTasks } from "../stores/tasks";

  import * as Page from "$lib/components/ui/page/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import * as NativeSelect from "$lib/components/ui/native-select/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";

  type ConfigType = "text" | "image" | "transcription" | "tts";

  let searchQuery   = $state("");
  let searchResults = $state<HFModel[]>([]);
  let searching     = $state(false);
  let sortBy        = $state<HFSort>("downloads");
  let kind          = $state<HFKind>("text");
  let selectedModel = $state<HFModel | null>(null);
  let modelFiles    = $state<HFFile[]>([]);
  let loadingFiles  = $state(false);

  let localModels    = $state<LocalModel[]>([]);
  let loadingLocal   = $state(false);
  let localExpanded  = $state(true);

  let backends       = $state<BackendEntry[]>([]);

  // per-model add-to-config form; key = model path
  let addConfigFor   = $state<string | null>(null);
  let formModelId    = $state("");
  let formModelPath  = $state("");
  let formType       = $state<ConfigType>("text");
  let formBackend    = $state("llama-server");
  let formGpuLayers  = $state(99);
  let formMmproj     = $state("");
  let formTtl        = $state(300);
  let formYaml       = $state("");
  let savingConfig   = $state(false);

  // ── helpers ──────────────────────────────────────────────────────────────

  function formatSize(bytes: number): string {
    if (bytes === 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return (bytes / Math.pow(1024, i)).toFixed(1) + " " + units[i];
  }

  function suggestModelId(path: string): string {
    const file = path.replace(/\/+$/, "").split("/").pop() ?? path;
    return file.replace(/\.(gguf|safetensors|bin)$/i, "").toLowerCase().replace(/[_\s]+/g, "-");
  }

  // Default config template type for a downloaded local model.
  function defaultType(k: LocalModelKind): ConfigType {
    switch (k) {
      case "whisper": return "transcription";
      case "safetensors": return "image";
      default: return "text"; // gguf | repo
    }
  }

  // Per-type launch command, modeled on docker/config.example.yaml. The result
  // is shown in an editable textarea so the user can tweak it before saving.
  function generateYaml(): string {
    const id = formModelId;
    const path = formModelPath;
    const proxy = `    proxy: http://127.0.0.1:\${PORT}\n`;
    const ttl = `    ttl: ${formTtl}\n`;

    if (formType === "image") {
      return (
        `\n  ${id}:\n` +
        `    checkEndpoint: /\n` +
        `    cmd: >-\n` +
        `      sd-server --listen-port \${PORT}\n` +
        `      --diffusion-model ${path}\n` +
        `      --diffusion-fa\n` +
        proxy + ttl
      );
    }
    if (formType === "transcription") {
      return (
        `\n  ${id}:\n` +
        `    checkEndpoint: /v1/audio/transcriptions/\n` +
        `    cmd: >-\n` +
        `      whisper-server --port \${PORT}\n` +
        `      -m ${path}\n` +
        `      --flash-attn\n` +
        `      --request-path /v1/audio/transcriptions --inference-path ""\n` +
        proxy + ttl
      );
    }
    // text + tts share the llama-server shape
    const mmline = formMmproj.trim() ? `\n      --mmproj ${formMmproj.trim()}` : "";
    return (
      `\n  ${id}:\n` +
      `    cmd: >-\n` +
      `      ${formBackend} --port \${PORT}\n` +
      `      -m ${path}${mmline}\n` +
      `      -ngl ${formGpuLayers}\n` +
      proxy + ttl
    );
  }

  function regenerateYaml() {
    formYaml = generateYaml();
  }

  // ── HF browse ────────────────────────────────────────────────────────────

  async function doSearch() {
    if (!searchQuery.trim()) return;
    searching = true;
    searchResults = await searchHFModels(searchQuery.trim(), 20, sortBy, kind);
    searching = false;
  }

  function changeSort(value: HFSort) {
    sortBy = value;
    if (searchQuery.trim()) doSearch();
  }

  function changeKind(value: HFKind) {
    kind = value;
    if (searchQuery.trim()) doSearch();
  }

  async function selectModel(m: HFModel) {
    selectedModel = m;
    loadingFiles = true;
    modelFiles = await listHFFiles(m.id);
    loadingFiles = false;
  }

  async function doDownload(filename: string) {
    if (!selectedModel) return;
    const task = await startDownload(selectedModel.id, filename);
    if (!task) return;
    trackDownload(task, filename);
  }

  async function doRepoDownload() {
    if (!selectedModel) return;
    const task = await startRepoDownload(selectedModel.id);
    if (!task) return;
    trackDownload(task);
  }

  async function doCancelDownload(taskID: string) {
    await cancelDownload(taskID);
    removeDownload(taskID);
  }

  // ── local models ─────────────────────────────────────────────────────────

  async function refreshLocal() {
    loadingLocal = true;
    try {
      localModels = await listLocalModels();
    } finally {
      loadingLocal = false;
    }
  }

  function openAddConfig(model: LocalModel) {
    addConfigFor  = model.path;
    formModelId   = suggestModelId(model.path);
    formModelPath = model.path;
    formType      = defaultType(model.kind);
    formBackend   = "llama-server";
    formGpuLayers = 99;
    formMmproj    = "";
    formTtl       = 300;
    regenerateYaml();
  }

  function changeType(t: ConfigType) {
    formType = t;
    regenerateYaml();
  }

  async function doAddToConfig() {
    if (!formModelId.trim() || !formYaml.trim()) return;
    savingConfig = true;
    const current = await getConfig();
    if (current !== null) {
      await putConfig(current + formYaml);
    }
    savingConfig = false;
    addConfigFor = null;
  }

  async function doDeleteLocal(model: LocalModel) {
    const cfg = await getConfig();
    const inConfig = cfg !== null && cfg.includes(model.path);
    const question = inConfig
      ? `"${model.name}" is referenced in your config.\nDelete the file AND remove it from config?`
      : `Delete ${model.name}?`;
    if (!confirm(question)) return;

    if (inConfig && cfg !== null) {
      // strip the model block from config — find the 2-space-indented key block
      // simple approach: remove lines that reference the path
      const lines = cfg.split("\n");
      const cleaned = stripModelFromConfig(lines, model.path);
      await putConfig(cleaned);
    }

    await deleteLocalModel(model.name);
    await refreshLocal();
  }

  function stripModelFromConfig(lines: string[], modelPath: string): string {
    // Find the model entry block that contains modelPath and remove it.
    // A block starts with a 2-space-indented key (^  \S) and ends before the next one.
    const result: string[] = [];
    let skip = false;
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      // New top-level model key
      if (/^  \S/.test(line) && !/^  #/.test(line)) {
        // Peek ahead: does this block contain the model path?
        let j = i + 1;
        let found = false;
        while (j < lines.length && !/^  \S/.test(lines[j])) {
          if (lines[j].includes(modelPath)) { found = true; }
          j++;
        }
        skip = found;
      }
      if (!skip) result.push(line);
    }
    return result.join("\n");
  }

  // ── init ─────────────────────────────────────────────────────────────────

  $effect(() => {
    refreshLocal();
    listBackends().then((b) => { backends = b; });
    syncTasks();
  });
</script>

<Page.Root title="Model Hub" width="full" fill>
  <Card.Root class="min-h-0 flex-1 gap-4 overflow-hidden p-4">
    <!-- ── HF browser ── -->
    <div class="flex flex-wrap gap-2 border-b pb-2">
      <Input
        type="text"
        class="min-w-full flex-1 sm:min-w-48"
        placeholder="Search HuggingFace models..."
        bind:value={searchQuery}
        onkeydown={(e) => e.key === "Enter" && doSearch()}
      />
      <NativeSelect.Root
        class="min-w-0 flex-1 sm:w-44 sm:flex-none"
        value={kind}
        onchange={(e) => changeKind(e.currentTarget.value as HFKind)}
        title="Model type"
      >
        <option value="text">Text / LLM</option>
        <option value="image">Image</option>
        <option value="transcription">Transcription</option>
        <option value="tts">Text-to-speech</option>
      </NativeSelect.Root>
      <NativeSelect.Root
        class="min-w-0 flex-1 sm:w-44 sm:flex-none"
        value={sortBy}
        onchange={(e) => changeSort(e.currentTarget.value as HFSort)}
        title="Sort results"
      >
        <option value="relevance">Relevant</option>
        <option value="trending">Trending</option>
        <option value="downloads">Most downloads</option>
        <option value="likes">Most likes</option>
        <option value="created">Newest</option>
        <option value="modified">Recently updated</option>
      </NativeSelect.Root>
      <Button variant="outline" onclick={doSearch} disabled={searching}>
        {searching ? "Searching…" : "Search"}
      </Button>
    </div>

    {#if selectedModel === null}
      <div class="flex-1 overflow-y-auto min-h-0">
        {#each searchResults as model (model.id)}
          <button
            type="button"
            class="flex w-full items-center justify-between border-b border-border p-3 text-left hover:bg-muted cursor-pointer"
            onclick={() => selectModel(model)}
          >
            <div>
              <span class="font-medium text-sm">{model.id}</span>
              {#if kind === "text" && !model.gguf}
                <span class="text-xs ml-2 px-1.5 py-0.5 rounded bg-warning/15 text-warning">no GGUF</span>
              {/if}
            </div>
            <span class="text-xs text-muted-foreground shrink-0">{model.downloads.toLocaleString()} dl · {model.likes.toLocaleString()} ♥</span>
          </button>
        {/each}
        {#if searchResults.length === 0 && !searching}
          <EmptyState title="Search for models to get started" />
        {/if}
      </div>
    {:else}
      <div class="flex-1 overflow-y-auto min-h-0">
        <div class="flex items-center justify-between mb-3 gap-2">
          <Button variant="outline" size="sm" onclick={() => { selectedModel = null; modelFiles = []; }}>← Back</Button>
          {#if modelFiles.length > 0}
            <Button variant="outline" size="sm" class="shrink-0" onclick={doRepoDownload} title="Download every file in this repo into one folder">
              Download all ({formatSize(modelFiles.reduce((s, f) => s + f.size, 0))})
            </Button>
          {/if}
        </div>
        <h3 class="font-semibold mb-2">{selectedModel.id}</h3>
        {#if loadingFiles}
          <p class="text-muted-foreground text-sm">Loading files…</p>
        {:else}
          <p class="text-xs text-muted-foreground mb-2">Files</p>
          {#each modelFiles as file}
            <div class="flex items-center justify-between p-2 border-b border-border gap-2">
              <span class="text-sm truncate mr-2">{file.path}</span>
              <div class="flex items-center gap-2 shrink-0">
                <span class="text-xs text-muted-foreground">{formatSize(file.size)}</span>
                <Button variant="outline" size="sm" onclick={() => doDownload(file.path)}>Download</Button>
              </div>
            </div>
          {/each}
          {#if modelFiles.length === 0}
            <p class="text-muted-foreground text-sm">No files found</p>
          {/if}
        {/if}
      </div>
    {/if}

    <!-- ── active downloads ── -->
    {#if $activeDownloads.size > 0}
      <div class="border-t border-border pt-3 shrink-0">
        <h4 class="text-sm font-semibold mb-2">Downloads</h4>
        {#each [...$activeDownloads.entries()] as [id, entry]}
          <div class="flex items-center gap-3 mb-2 text-sm">
            <div class="flex-1 min-w-0">
              <div class="flex justify-between gap-2">
                <span class="truncate text-xs">{entry.task.message}</span>
                {#if entry.task.pct >= 0}
                  <span class="text-muted-foreground text-xs shrink-0">{entry.task.pct}%</span>
                {/if}
              </div>
              {#if entry.task.pct >= 0 && entry.task.state === "running"}
                <div class="w-full bg-muted rounded h-1 mt-1">
                  <div class="bg-primary h-1 rounded" style="width: {entry.task.pct}%"></div>
                </div>
              {/if}
            </div>
            {#if entry.task.state === "running"}
              <Button variant="destructive" size="sm" class="shrink-0" onclick={() => doCancelDownload(id)}>Cancel</Button>
            {:else if entry.task.state === "failed" || entry.task.state === "cancelled"}
              {#if entry.filename}
                <Button variant="outline" size="sm" class="shrink-0" onclick={() => retryDownload(id)}>Retry</Button>
              {/if}
              <Button variant="ghost" size="icon-sm" class="text-muted-foreground shrink-0" aria-label="Dismiss" onclick={() => removeDownload(id)}>✕</Button>
            {:else if entry.task.state === "completed"}
              <Button variant="ghost" size="icon-sm" class="text-muted-foreground shrink-0" aria-label="Dismiss" onclick={() => removeDownload(id)}>✕</Button>
            {/if}
          </div>
        {/each}
      </div>
    {/if}

    <!-- ── local models (collapsible) ── -->
    <div class="border-t border-border pt-3 shrink-0 min-h-0 flex flex-col">
      <button
        class="flex items-center justify-between w-full text-sm font-semibold mb-2 hover:text-primary"
        onclick={() => localExpanded = !localExpanded}
      >
        <span>Local Models ({localModels.length})</span>
        <span>{localExpanded ? "▲" : "▼"}</span>
      </button>

      {#if localExpanded}
        {#if loadingLocal}
          <p class="text-xs text-muted-foreground">Loading…</p>
        {:else if localModels.length === 0}
          <p class="text-xs text-muted-foreground">No models downloaded yet</p>
        {:else}
          <div class="overflow-y-auto max-h-60 flex flex-col gap-1">
            {#each localModels as model (model.path)}
              <div class="border border-border rounded p-2 text-sm">
                <div class="flex items-center justify-between gap-2">
                  <span class="truncate font-medium text-xs flex items-center gap-1">
                    {#if model.kind && model.kind !== "gguf"}
                      <span class="px-1 py-0.5 rounded bg-info/15 text-info text-[10px] uppercase shrink-0">{model.kind}</span>
                    {/if}
                    <span class="truncate">{model.name}</span>
                  </span>
                  <div class="flex items-center gap-1 shrink-0">
                    <span class="text-muted-foreground text-xs">{formatSize(model.size)}</span>
                    <Button
                      variant="outline"
                      size="sm"
                      onclick={() => addConfigFor === model.path ? addConfigFor = null : openAddConfig(model)}
                    >
                      {addConfigFor === model.path ? "Cancel" : "+ Config"}
                    </Button>
                    <Button variant="destructive" size="sm" onclick={() => doDeleteLocal(model)}>Delete</Button>
                  </div>
                </div>

                {#if addConfigFor === model.path}
                  <div class="mt-2 pt-2 border-t border-border flex flex-col gap-1.5 text-xs">
                    <div class="flex gap-2 items-center">
                      <label for="model-config-id" class="w-20 text-muted-foreground">Model ID</label>
                      <Input id="model-config-id" class="flex-1" bind:value={formModelId} onblur={regenerateYaml} />
                    </div>
                    <div class="flex gap-2 items-center">
                      <label for="model-config-type" class="w-20 text-muted-foreground">Type</label>
                      <NativeSelect.Root id="model-config-type" class="flex-1" value={formType} onchange={(e) => changeType(e.currentTarget.value as ConfigType)}>
                        <option value="text">Text / LLM (llama-server)</option>
                        <option value="image">Image (sd-server)</option>
                        <option value="transcription">Transcription (whisper-server)</option>
                        <option value="tts">Text-to-speech (llama-server)</option>
                      </NativeSelect.Root>
                    </div>
                    {#if formType === "text" || formType === "tts"}
                      <div class="flex gap-2 items-center">
                        <label for="model-config-backend" class="w-20 text-muted-foreground">Backend</label>
                        <NativeSelect.Root id="model-config-backend" class="flex-1" bind:value={formBackend} onchange={regenerateYaml}>
                          {#if !backends.some((b) => b.path === "llama-server")}
                            <option value="llama-server">llama-server (default)</option>
                          {/if}
                          {#each backends.filter((b) => !b.builtin || b.kind === "llm") as be}
                            <option value={be.path}>{be.builtin ? `${be.name} (built-in: ${be.path})` : be.name}</option>
                          {/each}
                        </NativeSelect.Root>
                      </div>
                      <div class="flex gap-2 items-center">
                        <label for="model-config-gpu-layers" class="w-20 text-muted-foreground">GPU Layers</label>
                        <Input id="model-config-gpu-layers" class="w-20" type="number" bind:value={formGpuLayers} onblur={regenerateYaml} />
                      </div>
                      <div class="flex gap-2 items-center">
                        <label for="model-config-mmproj" class="w-20 text-muted-foreground">MMProj</label>
                        <Input id="model-config-mmproj" class="flex-1" placeholder="optional path" bind:value={formMmproj} onblur={regenerateYaml} />
                      </div>
                    {/if}
                    <div class="flex gap-2 items-center">
                      <label for="model-config-ttl" class="w-20 text-muted-foreground">TTL (s)</label>
                      <Input id="model-config-ttl" class="w-24" type="number" bind:value={formTtl} onblur={regenerateYaml} />
                    </div>
                    <div class="flex gap-2 items-start">
                      <label for="model-config-yaml" class="w-20 text-muted-foreground pt-1">YAML</label>
                      <Textarea id="model-config-yaml" class="flex-1 font-mono text-xs" rows={9} bind:value={formYaml} spellcheck={false} />
                    </div>
                    <div class="flex gap-2 self-end mt-1">
                      <Button variant="outline" size="sm" onclick={regenerateYaml} title="Rebuild YAML from the fields above">↻ Regenerate</Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onclick={doAddToConfig}
                        disabled={savingConfig || !formModelId.trim() || !formYaml.trim()}
                      >
                        {savingConfig ? "Saving…" : "Save to Config"}
                      </Button>
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      {/if}
    </div>
  </Card.Root>
</Page.Root>
