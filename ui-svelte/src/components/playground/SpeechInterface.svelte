<script lang="ts">
  import { hasListedModels } from "../../stores/api";
  import { persistentStore } from "../../stores/persistent";
  import { createPlaygroundInterface } from "../../lib/playgroundInterface";
  import { generateSpeech } from "../../lib/speechApi";
  import { playgroundStores } from "../../stores/playgroundActivity";
  import ModelSelector from "./ModelSelector.svelte";
  import ExpandableTextarea from "./ExpandableTextarea.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { NativeSelect } from "$lib/components/ui/native-select/index.js";
  import { Checkbox } from "$lib/components/ui/checkbox/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { CircleAlert, Clock, Download, LoaderCircle, Mic, RefreshCw } from "@lucide/svelte";
  import { playgroundSessionHeaders } from "../../lib/playgroundSession";

  const iface = createPlaygroundInterface("playground-speech-model", playgroundStores.speechGenerating);
  const selectedModelStore = iface.selectedModel;
  const busyStore = iface.busy;
  const errorStore = iface.error;
  const selectedVoiceStore = persistentStore<string>("playground-speech-voice", "coral");
  const autoPlayStore = persistentStore<boolean>("playground-speech-autoplay", false);

  let inputText = $state("");
  let isGenerating = $derived($busyStore);
  let error = $derived($errorStore);
  let generatedAudioUrl = $state<string | null>(null);
  let generatedVoice = $state<string | null>(null);
  let generatedTimestamp = $state<Date | null>(null);
  let audioElement = $state<HTMLAudioElement | null>(null);
  let availableVoices = $state<string[]>(["coral", "alloy", "echo", "fable", "onyx", "nova", "shimmer"]);
  let isLoadingVoices = $state(false);

  const defaultVoices = ["coral", "alloy", "echo", "fable", "onyx", "nova", "shimmer"];
  const CACHE_KEY = "playground-speech-voices-cache";

  // schema detection for openai format or plain list of strings
  function getVoiceIds(data: unknown): string[] {
    if (Array.isArray(data)) {
      return data.filter((voice): voice is string => typeof voice === "string");
    }

    if (!data || typeof data !== "object") return [];

    const response = data as { voices?: unknown };
    if (Array.isArray(response.voices)) {
      return response.voices
        .map((voice) =>
          typeof voice === "string"
            ? voice
            : voice && typeof voice === "object" && typeof (voice as { id?: unknown }).id === "string"
              ? (voice as { id: string }).id
              : null
        )
        .filter((voice): voice is string => voice !== null);
    }

    return [];
  }

  function getVoicesCache(): Record<string, string[]> {
    if (typeof window === "undefined") return {};
    try {
      const saved = localStorage.getItem(CACHE_KEY);
      return saved ? JSON.parse(saved) : {};
    } catch {
      return {};
    }
  }

  function saveVoicesCache(cache: Record<string, string[]>) {
    if (typeof window === "undefined") return;
    try {
      localStorage.setItem(CACHE_KEY, JSON.stringify(cache));
    } catch (e) {
      console.error("Error saving voices cache", e);
    }
  }


  let isInitialLoad = $state(true);

  // On page load, restore cached voices for the selected model if available
  $effect(() => {
    const model = $selectedModelStore;

    if (isInitialLoad) {
      isInitialLoad = false;
      // If we have cached voices for this model, use them
      const cache = getVoicesCache();
      if (model && cache[model]) {
        availableVoices = cache[model];
      }
    }
  });

  async function refreshVoices() {
    const model = $selectedModelStore;
    if (!model || isLoadingVoices) return;

    isLoadingVoices = true;

    try {
      const response = await fetch(`/v1/audio/voices?model=${encodeURIComponent(model)}`, {
        cache: "no-store",
        headers: playgroundSessionHeaders,
      });
      if (!response.ok) {
        // Fall back to default voices if API call fails
        availableVoices = defaultVoices;
        const cache = getVoicesCache();
        cache[model] = defaultVoices;
        saveVoicesCache(cache);
        selectedVoiceStore.set(defaultVoices[0]);
        return;
      }
      const data = await response.json();
      const voices = getVoiceIds(data);
      const newVoices = voices.length > 0 ? voices : defaultVoices;

      availableVoices = newVoices;
      const cache = getVoicesCache();
      cache[model] = newVoices;
      saveVoicesCache(cache);

      // Reset to first available voice
      selectedVoiceStore.set(newVoices[0]);
    } catch {
      // Fall back to default voices on error
      availableVoices = defaultVoices;
      const cache = getVoicesCache();
      cache[model] = defaultVoices;
      saveVoicesCache(cache);
      selectedVoiceStore.set(defaultVoices[0]);
    } finally {
      isLoadingVoices = false;
    }
  }

  function handleVoiceChange(value: string) {
    if (value === "(refresh)") {
      refreshVoices();
    } else {
      selectedVoiceStore.set(value);
    }
  }

  // Auto-play effect when new audio is generated
  $effect(() => {
    if (generatedAudioUrl && $autoPlayStore && audioElement) {
      audioElement.load();
      audioElement.play().catch(() => {
        // Ignore auto-play errors (e.g., browser policy blocks)
      });
    }
  });

  async function generate() {
    const trimmedText = inputText.trim();
    if (!trimmedText || !$selectedModelStore || isGenerating) return;

    await iface.run(async (signal) => {
      const audioBlob = await generateSpeech(
        $selectedModelStore,
        trimmedText,
        $selectedVoiceStore,
        signal
      );

      // Revoke previous URL to prevent memory leaks
      if (generatedAudioUrl) {
        URL.revokeObjectURL(generatedAudioUrl);
      }

      // Create object URL for the audio blob and store metadata
      generatedAudioUrl = URL.createObjectURL(audioBlob);
      generatedVoice = $selectedVoiceStore;
      generatedTimestamp = new Date();
    });
  }

  function cancelGeneration() {
    iface.cancel();
  }

  function clearInput() {
    inputText = "";
  }

  function downloadAudio() {
    if (!generatedAudioUrl) return;

    const timestamp = (generatedTimestamp || new Date()).toISOString().replace(/[:.]/g, '-').slice(0, -5);
    const voice = generatedVoice || 'speech';
    const filename = `${voice}-${timestamp}.mp3`;

    const a = document.createElement('a');
    a.href = generatedAudioUrl;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  function formatTimestamp(date: Date): string {
    return date.toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
      hour12: true
    });
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      generate();
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col gap-3">
  <!-- Toolbar -->
  <div class="bg-(--panel-header-bg) flex shrink-0 flex-wrap items-center gap-2 rounded-lg border px-3 py-2">
    <ModelSelector bind:value={$selectedModelStore} placeholder="Select a speech model..." disabled={isGenerating} capabilities={["audio_speech"]} />
    <NativeSelect
      class="w-40"
      aria-label="Voice"
      value={$selectedVoiceStore}
      onchange={(e) => {
        const v = e.currentTarget.value;
        if (v) handleVoiceChange(v);
        // "(refresh)" is an action, not a value: show the current voice again.
        if (v === "(refresh)") e.currentTarget.value = $selectedVoiceStore;
      }}
    >
      {#each availableVoices as voice (voice)}
        <option value={voice}>{voice}</option>
      {/each}
      <option value="(refresh)">(refresh)</option>
    </NativeSelect>
    {#if $selectedModelStore}
      <Button
        variant="outline"
        size="icon"
        class="shrink-0"
        onclick={refreshVoices}
        disabled={isLoadingVoices}
        aria-label="Load voices"
        title={isLoadingVoices ? "Loading voices..." : "Load voices for this model"}
      >
        <RefreshCw class={isLoadingVoices ? "animate-spin" : ""} />
      </Button>
    {/if}
    <label class="ml-auto flex items-center gap-2 text-sm">
      <Checkbox bind:checked={$autoPlayStore} />
      Auto-play
    </label>
  </div>

  {#if !$hasListedModels}
    <EmptyState title="No models configured" class="rounded-lg border" />
  {:else}
    <!-- Results -->
    <div class="bg-background shrink-0 rounded-lg border">
      {#if isGenerating}
        <div class="text-muted-foreground flex flex-col items-center gap-2 py-10 text-sm">
          <LoaderCircle class="size-6 animate-spin" />
          <p>Generating speech...</p>
        </div>
      {:else if error}
        <div class="p-4">
          <Alert.Root variant="destructive">
            <CircleAlert />
            <Alert.Title>{error}</Alert.Title>
          </Alert.Root>
        </div>
      {:else if generatedAudioUrl}
        <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5">
          <div class="text-muted-foreground flex min-w-0 flex-1 flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            {#if generatedVoice}
              <span class="flex items-center gap-1.5">
                <Mic class="size-4" />
                <span class="font-mono text-[0.8125rem]">{generatedVoice}</span>
              </span>
            {/if}
            {#if generatedTimestamp}
              <span class="flex items-center gap-1.5">
                <Clock class="size-4" />
                {formatTimestamp(generatedTimestamp)}
              </span>
            {/if}
          </div>
          <Button variant="outline" size="icon-sm" class="shrink-0" onclick={downloadAudio} aria-label="Download audio" title="Download audio file">
            <Download />
          </Button>
        </div>
        <div class="p-4">
          <audio bind:this={audioElement} controls class="h-12 w-full">
            <source src={generatedAudioUrl} type="audio/mpeg" />
            Your browser does not support the audio element.
          </audio>
        </div>
      {:else}
        <EmptyState title="No audio" full />
      {/if}
    </div>

    <!-- Input -->
    <div class="flex min-h-0 flex-1 flex-col gap-2 md:flex-row">
      <ExpandableTextarea
        bind:value={inputText}
        placeholder="Enter text to convert to speech..."
        rows={8}
        onkeydown={handleKeyDown}
        disabled={isGenerating || !$selectedModelStore}
      />
      <div class="flex shrink-0 gap-2 md:flex-col">
        {#if isGenerating}
          <Button variant="destructive" class="flex-1 md:flex-none" onclick={cancelGeneration}>
            Cancel
          </Button>
        {:else}
          <Button
            class="flex-1 md:flex-none"
            onclick={generate}
            disabled={!inputText.trim() || !$selectedModelStore}
          >
            Generate
          </Button>
          <Button
            variant="outline"
            class="flex-1 md:flex-none"
            onclick={clearInput}
            disabled={!inputText.trim()}
          >
            Clear
          </Button>
        {/if}
      </div>
    </div>
  {/if}
</div>
