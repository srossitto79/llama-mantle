<script lang="ts">
  import * as NativeSelect from "$lib/components/ui/native-select/index.js";
  import { onMount } from "svelte";
  import { Download, Eye, RefreshCw, Search, Trash2, Upload } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import { deleteStudioDataset, downloadHFDatasetFile, importStudioDataset, listHFDatasetFiles, listStudioDatasets, previewStudioDataset, searchHFDatasets, streamTaskProgress } from "../lib/mantleApi";
  import type { DatasetPreview, HFDataset, HFFile, StudioDataset } from "../lib/types";

  let datasets = $state<StudioDataset[]>([]);
  let selected = $state("");
  let preview = $state<DatasetPreview | null>(null);
  let uploadFiles = $state<FileList>();
  let destination = $state("");
  let query = $state("");
  let sort = $state("downloads");
  let hubResults = $state<HFDataset[]>([]);
  let hubSelected = $state<HFDataset | null>(null);
  let hubFiles = $state<HFFile[]>([]);
  let loading = $state(false);
  let busy = $state(false);
  let error = $state("");
  let status = $state("");

  function formatSize(bytes: number): string {
    if (!bytes) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
    return `${(bytes / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
  }

  async function refresh() {
    loading = true;
    try { datasets = await listStudioDatasets(); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { loading = false; }
  }

  async function showPreview(path: string) {
    selected = path; preview = null;
    if (path.toLowerCase().endsWith(".py")) {
      error = "";
      status = "Python reward workers are executable resources and are not previewed as datasets.";
      return;
    }
    busy = true;
    try { preview = await previewStudioDataset(path, 10); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function removeDataset(dataset: StudioDataset) {
    if (!window.confirm(`Delete ${dataset.path}? The file cannot be recovered from Studio.`)) return;
    busy = true;
    try {
      if (!(await deleteStudioDataset(dataset.path))) throw new Error("delete failed");
      if (selected === dataset.path) { selected = ""; preview = null; }
      status = `Deleted ${dataset.path}`;
      error = "";
      await refresh();
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function upload() {
    const file = uploadFiles?.item(0); if (!file) return;
    busy = true;
    try { const added = await importStudioDataset(file, destination); status = `Imported ${added.path}`; destination = ""; uploadFiles = undefined; await refresh(); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function searchHub() {
    if (!query.trim()) return; busy = true; hubSelected = null; hubFiles = [];
    try { hubResults = await searchHFDatasets(query.trim(), sort); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function selectHub(item: HFDataset) {
    hubSelected = item; busy = true;
    try { hubFiles = await listHFDatasetFiles(item.id); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
    finally { busy = false; }
  }

  async function download(file: HFFile) {
    if (!hubSelected) return; busy = true;
    try {
      const task = await downloadHFDatasetFile(hubSelected.id, file.path); status = task.message || `Queued ${file.path}`;
      const stop = streamTaskProgress(task.id, (update) => {
        status = update.message ?? status;
        if (update.state === "completed" || update.state === "failed" || update.state === "cancelled") { stop(); busy = false; if (update.state === "completed") refresh(); }
      });
      error = "";
    } catch (cause) { error = cause instanceof Error ? cause.message : String(cause); busy = false; }
  }

  onMount(refresh);
  import * as Page from "$lib/components/ui/page/index.js";
</script>

<Page.Root title="Studio Datasets" width="full">
  {#snippet actions()}<Button size="sm" variant="outline" onclick={refresh} disabled={loading}><RefreshCw class="size-4" />Refresh</Button>{/snippet}
  {#if error}<Alert.Root variant="destructive"><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
  {#if status}<p class="text-muted-foreground text-sm">{status}</p>{/if}

  <div class="grid gap-(--page-gap) xl:grid-cols-[1fr_1.4fr]">
    <div class="flex min-w-0 flex-col gap-(--page-gap)">
      <!-- Import copies atomically below datasets/ and never overwrites an existing file. -->
      <Card.Root class="gap-0 py-0">
        <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5"><h2 class="text-heading">Import</h2></div>
        <div class="space-y-3 p-4">
          <div class="space-y-1"><Label.Root for="dataset-file">Dataset or reward script</Label.Root><Input id="dataset-file" type="file" accept=".jsonl,.json,.txt,.text,.csv,.parquet,.py" bind:files={uploadFiles} /></div>
          <div class="space-y-1"><Label.Root for="dataset-destination">Destination (optional)</Label.Root><Input id="dataset-destination" bind:value={destination} placeholder="datasets/my-project/train.jsonl" /></div>
          <Button size="sm" onclick={upload} disabled={busy || !uploadFiles?.length}><Upload class="size-4" />Import</Button>
        </div>
      </Card.Root>

      <Card.Root class="gap-0 py-0">
        <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5"><h2 class="text-heading">Local datasets</h2><span class="text-muted-foreground ml-auto text-sm tabular-nums">{datasets.length}</span></div>
        {#if loading}
          <p class="text-muted-foreground px-4 py-10 text-center text-sm">Loading…</p>
        {:else if datasets.length === 0}
          <EmptyState title="No datasets" />
        {:else}
          <ul class="divide-y">
            {#each datasets as dataset (dataset.path)}
              <li class="hover:bg-muted/50 flex items-center gap-1 px-4 py-2.5" class:bg-muted={selected === dataset.path}>
                <button type="button" class="flex min-w-0 flex-1 items-center gap-3 text-left" onclick={() => showPreview(dataset.path)}>
                  <span class="min-w-0 flex-1"><span class="block truncate font-mono text-[0.8125rem]">{dataset.path}</span><span class="text-muted-foreground text-xs">{dataset.format.toUpperCase()} · {formatSize(dataset.size)}</span></span><Eye class="text-muted-foreground size-4 shrink-0" />
                </button>
                <Button size="icon-sm" variant="ghost" title="Delete dataset" aria-label="Delete dataset" onclick={() => removeDataset(dataset)} disabled={busy}><Trash2 class="text-destructive size-4" /></Button>
              </li>
            {/each}
          </ul>
        {/if}
      </Card.Root>
    </div>

    <div class="flex min-w-0 flex-col gap-(--page-gap)">
      <Card.Root class="min-w-0 gap-0 py-0">
        <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5"><h2 class="text-heading">Preview</h2></div>
        <div class="min-w-0 p-4">
          {#if preview}
            <div class="min-w-0 space-y-3"><p class="text-sm tabular-nums">{preview.recordsScanned}{preview.truncated ? "+" : ""} records · {Object.entries(preview.formats).map(([key, count]) => `${key}: ${count}`).join(" · ")}</p><div class="max-h-96 min-w-0 space-y-2 overflow-y-auto">{#each preview.records as record, index}<pre class="bg-muted max-w-full overflow-x-auto rounded-md p-3 font-mono text-xs"><span class="text-muted-foreground">#{index + 1}</span>
{JSON.stringify(record, null, 2)}</pre>{/each}</div></div>
          {:else}
            <p class="text-muted-foreground text-sm">No dataset selected</p>
          {/if}
        </div>
      </Card.Root>

      <Card.Root class="gap-0 py-0">
        <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5"><h2 class="text-heading">Hugging Face</h2></div>
        <div class="space-y-3 p-4">
          <div class="flex flex-wrap gap-2"><Input class="min-w-40 flex-1" aria-label="Dataset search" bind:value={query} placeholder="Search datasets…" onkeydown={(event) => event.key === "Enter" && searchHub()} /><NativeSelect.Root class="w-auto" aria-label="Sort datasets" bind:value={sort}><option value="downloads">Downloads</option><option value="likes">Likes</option><option value="modified">Recently updated</option><option value="relevance">Relevance</option></NativeSelect.Root><Button onclick={searchHub} disabled={busy || !query.trim()}><Search class="size-4" />Search</Button></div>
          <div class="grid gap-3 md:grid-cols-2">
            <div class="max-h-80 divide-y overflow-auto rounded-md border">{#each hubResults as item (item.id)}<button type="button" class="hover:bg-muted/50 w-full px-3 py-2 text-left" class:bg-muted={hubSelected?.id === item.id} onclick={() => selectHub(item)}><span class="block truncate font-mono text-[0.8125rem]">{item.id}</span><span class="text-muted-foreground text-xs tabular-nums">{item.downloads.toLocaleString()} downloads · {item.likes.toLocaleString()} likes</span></button>{/each}</div>
            <div class="max-h-80 divide-y overflow-auto rounded-md border">{#if hubSelected && hubFiles.length === 0}<p class="text-muted-foreground p-3 text-sm">No supported files</p>{/if}{#each hubFiles as file (file.path)}<div class="flex items-center gap-2 px-3 py-2"><span class="min-w-0 flex-1"><span class="block truncate font-mono text-xs" title={file.path}>{file.path}</span><span class="text-muted-foreground text-xs">{formatSize(file.size)}</span></span><Button size="icon-sm" variant="outline" title="Download dataset file" aria-label="Download dataset file" onclick={() => download(file)} disabled={busy}><Download class="size-4" /></Button></div>{/each}</div>
          </div>
        </div>
      </Card.Root>
    </div>
  </div>
</Page.Root>
