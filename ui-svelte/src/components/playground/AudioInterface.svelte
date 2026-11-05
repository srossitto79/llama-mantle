<script lang="ts">
  import { hasListedModels } from "../../stores/api";
  import { createPlaygroundInterface } from "../../lib/playgroundInterface";
  import { transcribeAudio } from "../../lib/audioApi";
  import { playgroundStores } from "../../stores/playgroundActivity";
  import ModelSelector from "./ModelSelector.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { CircleAlert, Copy, Check, LoaderCircle } from "@lucide/svelte";
  import { formatFileSize } from "../../lib/format";
  import { copyText } from "../../lib/clipboard";

  const iface = createPlaygroundInterface("playground-audio-model", playgroundStores.audioTranscribing);
  const selectedModelStore = iface.selectedModel;
  const transcribing = iface.busy;
  const error = iface.error;
  let isTranscribing = $derived($transcribing);

  let selectedFile = $state<File | null>(null);
  let transcriptionResult = $state<string | null>(null);
  let isDragging = $state(false);
  let fileInput = $state<HTMLInputElement | null>(null);
  let copied = $state(false);

  const ACCEPTED_FORMATS = ['.mp3', '.wav', '.ogg'];
  const MAX_FILE_SIZE = 25 * 1024 * 1024; // 25MB

  let canTranscribe = $derived(selectedFile !== null && $selectedModelStore !== "" && !isTranscribing);

  function validateFile(file: File): { valid: boolean; error?: string } {
    const ext = '.' + file.name.split('.').pop()?.toLowerCase();

    if (!ACCEPTED_FORMATS.includes(ext)) {
      return { valid: false, error: 'Invalid file type. Accepted: MP3, WAV, OGG' };
    }

    if (file.size > MAX_FILE_SIZE) {
      return { valid: false, error: 'File too large. Maximum: 25MB' };
    }

    return { valid: true };
  }

  function handleFileSelect(event: Event) {
    const target = event.target as HTMLInputElement;
    const file = target.files?.[0];
    if (file) {
      const validation = validateFile(file);
      if (validation.valid) {
        selectedFile = file;
        $error = null;
        transcriptionResult = null;
      } else {
        $error = validation.error || "Invalid file";
        selectedFile = null;
      }
    }
  }

  function handleDragOver(event: DragEvent) {
    event.preventDefault();
    isDragging = true;
  }

  function handleDragLeave() {
    isDragging = false;
  }

  function handleDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;

    const file = event.dataTransfer?.files[0];
    if (file) {
      const validation = validateFile(file);
      if (validation.valid) {
        selectedFile = file;
        $error = null;
        transcriptionResult = null;
      } else {
        $error = validation.error || "Invalid file";
        selectedFile = null;
      }
    }
  }

  async function transcribe() {
    const file = selectedFile;
    if (!file || !$selectedModelStore || isTranscribing) return;

    transcriptionResult = null;
    await iface.run(async (signal) => {
      const response = await transcribeAudio($selectedModelStore, file, signal);
      transcriptionResult = response.text;
    });
  }

  function cancelTranscription() {
    iface.cancel();
  }

  function clearAll() {
    selectedFile = null;
    transcriptionResult = null;
    $error = null;
    if (fileInput) {
      fileInput.value = '';
    }
  }

  async function copyToClipboard() {
    if (transcriptionResult && (await copyText(transcriptionResult))) {
      copied = true;
      setTimeout(() => {
        copied = false;
      }, 2000);
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col gap-3">
  <!-- Toolbar -->
  <div class="bg-(--panel-header-bg) flex shrink-0 flex-wrap items-center gap-2 rounded-lg border px-3 py-2">
    <ModelSelector bind:value={$selectedModelStore} placeholder="Select an audio model..." disabled={isTranscribing} capabilities={["audio_transcriptions"]} />
  </div>

  {#if !$hasListedModels}
    <EmptyState title="No models configured" class="rounded-lg border" />
  {:else}
    <!-- File / result -->
    <div class="bg-background flex min-h-0 flex-1 items-center justify-center overflow-auto rounded-lg border">
      {#if isTranscribing}
        <div class="text-muted-foreground flex flex-col items-center gap-2 text-sm">
          <LoaderCircle class="size-6 animate-spin" />
          <p>Transcribing audio...</p>
        </div>
      {:else if $error}
        <div class="w-full p-4">
          <Alert.Root variant="destructive">
            <CircleAlert />
            <Alert.Title>{$error}</Alert.Title>
          </Alert.Root>
        </div>
      {:else if transcriptionResult}
        <div class="flex h-full w-full flex-col">
          <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5">
            <h2 class="text-heading flex-1">Transcription</h2>
            <Button
              variant="outline"
              size="icon-sm"
              onclick={copyToClipboard}
              aria-label="Copy to clipboard"
              title={copied ? 'Copied' : 'Copy to clipboard'}
            >
              {#if copied}
                <Check class="text-success" />
              {:else}
                <Copy />
              {/if}
            </Button>
          </div>
          <div class="flex-1 overflow-auto p-4 text-sm whitespace-pre-wrap">
            {transcriptionResult}
          </div>
        </div>
      {:else if selectedFile}
        <div class="flex flex-col items-center gap-1 p-4 text-center">
          <h3 class="text-label text-muted-foreground font-semibold uppercase">File</h3>
          <p class="font-mono text-[0.8125rem]">{selectedFile.name}</p>
          <p class="text-muted-foreground text-xs tabular-nums">{formatFileSize(selectedFile.size)}</p>
        </div>
      {:else}
        <div
          role="region"
          aria-label="Audio file drop zone"
          class="flex h-full w-full items-center justify-center {isDragging ? 'bg-primary/10' : ''}"
          ondragover={handleDragOver}
          ondragleave={handleDragLeave}
          ondrop={handleDrop}
        >
          <EmptyState title="Drop an audio file" description="MP3, WAV, OGG. Maximum 25MB." full />
        </div>
      {/if}
    </div>

    <!-- Actions -->
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      <input
        type="file"
        accept=".mp3,.wav,.ogg"
        class="hidden"
        onchange={handleFileSelect}
        bind:this={fileInput}
      />
      <Button variant="outline" onclick={() => fileInput?.click()} disabled={isTranscribing}>
        Browse
      </Button>
      <div class="flex-1"></div>
      {#if isTranscribing}
        <Button variant="destructive" onclick={cancelTranscription}>Cancel</Button>
      {:else}
        <Button onclick={transcribe} disabled={!canTranscribe}>Transcribe</Button>
        <Button
          variant="outline"
          onclick={clearAll}
          disabled={!selectedFile && !transcriptionResult && !$error}
        >
          Clear
        </Button>
      {/if}
    </div>
  {/if}
</div>
