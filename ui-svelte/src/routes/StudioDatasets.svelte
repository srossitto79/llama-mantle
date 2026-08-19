<script lang="ts">
  import { onMount } from "svelte";
  import { Database, Download, Eye, RefreshCw, Search, Upload } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import { downloadHFDatasetFile, importStudioDataset, listHFDatasetFiles, listStudioDatasets, previewStudioDataset, searchHFDatasets, streamTaskProgress } from "../lib/mantleApi";
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
    selected = path; preview = null; busy = true;
    try { preview = await previewStudioDataset(path, 10); error = ""; }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
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
</script>

<div class="h-full overflow-auto p-4">
  <div class="mx-auto max-w-7xl space-y-4">
    <div class="flex items-center gap-2"><Database class="size-5" /><h2 class="text-lg font-semibold">Datasets</h2><Button class="ml-auto" size="sm" variant="outline" onclick={refresh} disabled={loading}><RefreshCw class="size-4" />Refresh</Button></div>
    {#if error}<p class="text-destructive text-sm">{error}</p>{/if}
    {#if status}<p class="text-muted-foreground text-sm">{status}</p>{/if}

    <div class="grid gap-4 xl:grid-cols-[1fr_1.4fr]">
      <div class="space-y-4">
        <Card.Root><Card.Header><Card.Title>Import local file</Card.Title><Card.Description>Files are copied atomically below <code>datasets/</code>. Existing files are never overwritten.</Card.Description></Card.Header><Card.Content class="space-y-3">
          <div class="space-y-1"><Label.Root for="dataset-file">Dataset file</Label.Root><Input id="dataset-file" type="file" accept=".jsonl,.json,.txt,.text,.csv,.parquet" bind:files={uploadFiles} /></div>
          <div class="space-y-1"><Label.Root for="dataset-destination">Destination (optional)</Label.Root><Input id="dataset-destination" bind:value={destination} placeholder="datasets/my-project/train.jsonl" /></div>
          <Button onclick={upload} disabled={busy || !uploadFiles?.length}><Upload class="size-4" />Import</Button>
        </Card.Content></Card.Root>

        <Card.Root><Card.Header><Card.Title>Local datasets</Card.Title><Card.Description>{datasets.length} recognized files</Card.Description></Card.Header><Card.Content class="space-y-2">
          {#if loading}<p class="text-muted-foreground text-sm">Loading…</p>{:else if datasets.length === 0}<p class="text-muted-foreground text-sm">No datasets imported yet.</p>{/if}
          {#each datasets as dataset (dataset.path)}
            <button type="button" class="hover:bg-muted flex w-full items-center gap-3 rounded-md border p-2 text-left" class:bg-muted={selected === dataset.path} onclick={() => showPreview(dataset.path)}>
              <span class="min-w-0 flex-1"><span class="block truncate text-sm font-medium">{dataset.path}</span><span class="text-muted-foreground text-xs">{dataset.format.toUpperCase()} · {formatSize(dataset.size)}</span></span><Eye class="size-4 shrink-0" />
            </button>
          {/each}
        </Card.Content></Card.Root>
      </div>

      <div class="space-y-4">
        <Card.Root><Card.Header><Card.Title>Preview and validation</Card.Title><Card.Description>JSONL records are parsed and checked for messages, text, or prompt/response structure.</Card.Description></Card.Header><Card.Content>
          {#if preview}<div class="space-y-3"><p class="text-sm">{preview.recordsScanned}{preview.truncated ? "+" : ""} records · {Object.entries(preview.formats).map(([key, count]) => `${key}: ${count}`).join(" · ")}</p><div class="max-h-96 space-y-2 overflow-auto">{#each preview.records as record, index}<pre class="bg-muted overflow-auto rounded-md p-3 text-xs"><span class="text-muted-foreground">#{index + 1}</span>
{JSON.stringify(record, null, 2)}</pre>{/each}</div></div>{:else}<p class="text-muted-foreground text-sm">Select a JSONL dataset to preview it.</p>{/if}
        </Card.Content></Card.Root>

        <Card.Root><Card.Header><Card.Title>Hugging Face datasets</Card.Title><Card.Description>Search public dataset repositories and download only the files you need.</Card.Description></Card.Header><Card.Content class="space-y-3">
          <div class="flex gap-2"><Input aria-label="Dataset search" bind:value={query} placeholder="Search datasets…" onkeydown={(event) => event.key === "Enter" && searchHub()} /><select aria-label="Sort datasets" class="border-input bg-background h-9 rounded-md border px-2 text-sm" bind:value={sort}><option value="downloads">Downloads</option><option value="likes">Likes</option><option value="modified">Recently updated</option><option value="relevance">Relevance</option></select><Button onclick={searchHub} disabled={busy || !query.trim()}><Search class="size-4" />Search</Button></div>
          <div class="grid gap-3 md:grid-cols-2">
            <div class="max-h-80 space-y-1 overflow-auto">{#each hubResults as item (item.id)}<button type="button" class="hover:bg-muted w-full rounded-md border p-2 text-left" class:bg-muted={hubSelected?.id === item.id} onclick={() => selectHub(item)}><span class="block truncate text-sm font-medium">{item.id}</span><span class="text-muted-foreground text-xs">{item.downloads.toLocaleString()} downloads · {item.likes.toLocaleString()} likes</span></button>{/each}</div>
            <div class="max-h-80 space-y-1 overflow-auto">{#if hubSelected && hubFiles.length === 0}<p class="text-muted-foreground text-sm">No supported JSON, text, CSV, or Parquet files found.</p>{/if}{#each hubFiles as file (file.path)}<div class="flex items-center gap-2 rounded-md border p-2"><span class="min-w-0 flex-1"><span class="block truncate text-xs font-medium" title={file.path}>{file.path}</span><span class="text-muted-foreground text-xs">{formatSize(file.size)}</span></span><Button size="icon-sm" variant="outline" title="Download dataset file" onclick={() => download(file)} disabled={busy}><Download class="size-4" /></Button></div>{/each}</div>
          </div>
        </Card.Content></Card.Root>
      </div>
    </div>
  </div>
</div>
