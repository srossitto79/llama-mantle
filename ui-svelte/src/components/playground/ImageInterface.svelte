<script lang="ts">
  import { hasListedModels } from "../../stores/api";
  import { persistentStore } from "../../stores/persistent";
  import { createPlaygroundInterface } from "../../lib/playgroundInterface";
  import { generateImage } from "../../lib/imageApi";
  import { generateSdImage, fetchSdLoras } from "../../lib/sdApi";
  import { playgroundStores } from "../../stores/playgroundActivity";
  import ModelSelector from "./ModelSelector.svelte";
  import ExpandableTextarea from "./ExpandableTextarea.svelte";
  import type { ImageApiMode, SdApiLora, SdApiLoraRef } from "../../lib/types";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import { NativeSelect } from "$lib/components/ui/native-select/index.js";
  import * as Field from "$lib/components/ui/field/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { CircleAlert, Download, LoaderCircle, X } from "@lucide/svelte";

  const iface = createPlaygroundInterface("playground-image-model", playgroundStores.imageGenerating);
  const selectedModelStore = iface.selectedModel;
  const busyStore = iface.busy;
  const error = iface.error;
  const selectedSizeStore = persistentStore<string>("playground-image-size", "1024x1024");
  const SAMPLERS = [
    "euler_a", "euler", "heun", "dpm2", "dpmpp2s_a", "dpmpp2m", "dpmpp2mv2", "ipndm", "ipndm_v", "lcm", "ddim_trailing", "tcd",
  ];
  const SCHEDULERS = ["discrete", "karras", "exponential", "ays", "gits"];
  const apiModeStore = persistentStore<ImageApiMode>("playground-image-api-mode", "openai");

  // SDAPI persistent settings
  const sdNegativePromptStore = persistentStore<string>("playground-sdapi-negative-prompt", "");
  const sdStepsStore = persistentStore<number>("playground-sdapi-steps", 20);
  const sdCfgScaleStore = persistentStore<number>("playground-sdapi-cfg-scale", 7);
  const sdSeedStore = persistentStore<number>("playground-sdapi-seed", -1);
  const sdSamplerStore = persistentStore<string>("playground-sdapi-sampler", "");
  const sdSchedulerStore = persistentStore<string>("playground-sdapi-scheduler", "");
  const sdBatchSizeStore = persistentStore<number>("playground-sdapi-batch-size", 1);

  let prompt = $state("");
  let isGenerating = $derived($busyStore);
  let generatedImages = $state<string[]>([]);
  let showFullscreen = $state(false);
  let fullscreenIndex = $state(0);
  let showSettings = $state(false);

  // SDAPI lora state
  let availableLoras = $state<SdApiLora[]>([]);
  let selectedLoras = $state<SdApiLoraRef[]>([]);
  let isLoadingLoras = $state(false);
  let lorasLoaded = $state(false);
  let loraError = $state<string | null>(null);

  let isSdapi = $derived($apiModeStore === "sdapi");

  async function loadLoras() {
    if (!$selectedModelStore || isLoadingLoras) return;
    isLoadingLoras = true;
    loraError = null;
    try {
      const loras = await fetchSdLoras($selectedModelStore);
      availableLoras = loras;
      lorasLoaded = true;
    } catch (err) {
      availableLoras = [];
      loraError = err instanceof Error ? err.message : "Failed to load LoRAs";
      lorasLoaded = false;
    } finally {
      isLoadingLoras = false;
    }
  }

  function removeLora(path: string) {
    selectedLoras = selectedLoras.filter((l) => l.path !== path);
  }

  function updateLoraMultiplier(path: string, multiplier: number) {
    selectedLoras = selectedLoras.map((l) =>
      l.path === path ? { ...l, multiplier } : l
    );
  }

  function getLoraName(path: string): string {
    return availableLoras.find((l) => l.path === path)?.name ?? path;
  }

  async function generate() {
    const trimmedPrompt = prompt.trim();
    if (!trimmedPrompt || !$selectedModelStore || isGenerating) return;

    await iface.run(async (signal) => {
      if (isSdapi) {
        const [w, h] = $selectedSizeStore.split("x").map(Number);
        const request = {
          model: $selectedModelStore,
          prompt: trimmedPrompt,
          negative_prompt: $sdNegativePromptStore || undefined,
          width: w,
          height: h,
          steps: $sdStepsStore,
          cfg_scale: $sdCfgScaleStore,
          seed: $sdSeedStore,
          batch_size: $sdBatchSizeStore,
          sampler_name: $sdSamplerStore || undefined,
          scheduler: $sdSchedulerStore || undefined,
          lora: selectedLoras.length > 0 ? selectedLoras : undefined,
        };

        const response = await generateSdImage(request, signal);
        if (response.images && response.images.length > 0) {
          generatedImages = response.images.map(
            (img) => `data:image/png;base64,${img}`
          );
        }
      } else {
        const response = await generateImage(
          $selectedModelStore,
          trimmedPrompt,
          $selectedSizeStore,
          signal
        );

        if (response.data && response.data.length > 0) {
          const imageData = response.data[0];
          if (imageData.b64_json) {
            generatedImages = [`data:image/png;base64,${imageData.b64_json}`];
          } else if (imageData.url) {
            generatedImages = [imageData.url];
          }
        }
      }
    });
  }

  function cancelGeneration() {
    iface.cancel();
  }

  function clearImage() {
    generatedImages = [];
    $error = null;
    prompt = "";
  }

  function downloadImage(index: number = 0) {
    const img = generatedImages[index];
    if (!img) return;

    const link = document.createElement("a");
    link.href = img;
    link.download = `generated-image-${Date.now()}-${index}.png`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  }

  function openFullscreen(index: number = 0) {
    fullscreenIndex = index;
    showFullscreen = true;
  }

  function closeFullscreen(event?: MouseEvent) {
    if (event && event.target !== event.currentTarget) {
      return;
    }
    showFullscreen = false;
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
    <ModelSelector bind:value={$selectedModelStore} placeholder="Select an image model..." disabled={isGenerating} capabilities={["image_generation", "image_to_image"]} matchAny={true} />

    <NativeSelect class="w-28" aria-label="API" bind:value={$apiModeStore}>
      <option value="openai">OpenAI</option>
      <option value="sdapi">SDAPI</option>
    </NativeSelect>

    <NativeSelect class="w-44" aria-label="Size" bind:value={$selectedSizeStore}>
      <optgroup label="Square">
        <option value="512x512">512x512</option>
        <option value="1024x1024">1024x1024</option>
      </optgroup>
      <optgroup label="Landscape">
        <option value="1024x768">1024x768 (4:3)</option>
        <option value="1280x720">1280x720 (16:9)</option>
        <option value="1792x1024">1792x1024 (SDXL)</option>
      </optgroup>
      <optgroup label="Portrait">
        <option value="768x1024">768x1024 (3:4)</option>
        <option value="720x1280">720x1280 (9:16)</option>
        <option value="1024x1792">1024x1792 (SDXL)</option>
      </optgroup>
    </NativeSelect>

    {#if isSdapi}
      <Button
        variant={showSettings ? "secondary" : "outline"}
        size="sm"
        class="ml-auto"
        aria-pressed={showSettings}
        onclick={() => (showSettings = !showSettings)}
      >
        Settings
      </Button>
    {/if}
  </div>

  <!-- SDAPI settings -->
  {#if isSdapi && showSettings}
    <div class="max-h-[50%] shrink-0 overflow-y-auto rounded-lg border p-4">
      <div class="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4">
        <Field.Root label="Steps">
          {#snippet control({ id })}
            <Input {id} type="number" bind:value={$sdStepsStore} min="1" max="150" />
          {/snippet}
        </Field.Root>
        <Field.Root label="CFG Scale">
          {#snippet control({ id })}
            <Input {id} type="number" bind:value={$sdCfgScaleStore} min="1" max="30" step="0.5" />
          {/snippet}
        </Field.Root>
        <Field.Root label="Seed (-1 = random)">
          {#snippet control({ id })}
            <Input {id} type="number" bind:value={$sdSeedStore} min="-1" />
          {/snippet}
        </Field.Root>
        <Field.Root label="Batch Size">
          {#snippet control({ id })}
            <Input {id} type="number" bind:value={$sdBatchSizeStore} min="1" max="8" />
          {/snippet}
        </Field.Root>
        <Field.Root label="Sampler">
          {#snippet control({ id })}
            <NativeSelect {id} bind:value={$sdSamplerStore}>
              <option value="">Default</option>
              {#each SAMPLERS as sampler (sampler)}
                <option value={sampler}>{sampler}</option>
              {/each}
            </NativeSelect>
          {/snippet}
        </Field.Root>
        <Field.Root label="Scheduler">
          {#snippet control({ id })}
            <NativeSelect {id} bind:value={$sdSchedulerStore}>
              <option value="">Auto for model</option>
              {#each SCHEDULERS as scheduler (scheduler)}
                <option value={scheduler}>{scheduler}</option>
              {/each}
            </NativeSelect>
          {/snippet}
        </Field.Root>
      </div>

      <Field.Root label="Negative Prompt" class="mb-4">
        {#snippet control({ id })}
          <Textarea {id} bind:value={$sdNegativePromptStore} rows={2} placeholder="Elements to avoid..." />
        {/snippet}
      </Field.Root>

      <!-- LoRA selection -->
      <h3 class="text-label text-muted-foreground mb-2 font-semibold uppercase">LoRAs</h3>
      <div class="mb-2 flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onclick={loadLoras}
          disabled={!$selectedModelStore || isLoadingLoras}
        >
          {isLoadingLoras ? "Loading..." : lorasLoaded ? "Reload LoRAs" : "Load LoRAs"}
        </Button>
        {#if lorasLoaded && availableLoras.length > 0}
          <!-- Always shows the placeholder; picking an entry adds it to the list. -->
          <NativeSelect
            class="min-w-0 flex-1 basis-48"
            aria-label="Add a LoRA"
            value=""
            onchange={(e) => {
              const v = e.currentTarget.value;
              if (v) {
                const lora = availableLoras.find((l) => l.path === v);
                if (lora && !selectedLoras.some((s) => s.path === v)) {
                  selectedLoras = [...selectedLoras, { path: lora.path, multiplier: 1.0 }];
                }
              }
              e.currentTarget.value = "";
            }}
          >
            <option value="">Add a LoRA...</option>
            {#each availableLoras.filter((l) => !selectedLoras.some((s) => s.path === l.path)) as lora (lora.path)}
              <option value={lora.path}>{lora.name}</option>
            {/each}
          </NativeSelect>
        {/if}
      </div>
      {#if loraError}
        <p class="text-destructive mb-1 text-xs">{loraError}</p>
      {/if}
      {#if lorasLoaded && availableLoras.length === 0}
        <p class="text-muted-foreground text-xs">No LoRAs available</p>
      {/if}
      {#if selectedLoras.length > 0}
        <div class="divide-y rounded-lg border">
          {#each selectedLoras as lora}
            <div class="flex items-center gap-2 px-3 py-2 text-sm">
              <span class="flex-1 truncate font-mono text-[0.8125rem]">{getLoraName(lora.path)}</span>
              <Input
                type="number"
                class="w-20"
                aria-label="Multiplier"
                value={lora.multiplier}
                oninput={(e) => updateLoraMultiplier(lora.path, parseFloat((e.target as HTMLInputElement).value) || 1)}
                min="0"
                max="2"
                step="0.1"
              />
              <Button
                variant="ghost"
                size="icon-sm"
                class="text-muted-foreground hover:text-destructive"
                onclick={() => removeLora(lora.path)}
                aria-label="Remove LoRA"
              >
                <X />
              </Button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  {#if !$hasListedModels}
    <EmptyState title="No models configured" class="rounded-lg border" />
  {:else}
    <!-- Results -->
    <div class="bg-background flex min-h-0 flex-1 items-center justify-center overflow-auto rounded-lg border">
      {#if isGenerating}
        <div class="text-muted-foreground flex flex-col items-center gap-2 text-sm">
          <LoaderCircle class="size-6 animate-spin" />
          <p>Generating image...</p>
        </div>
      {:else if $error}
        <div class="w-full p-4">
          <Alert.Root variant="destructive">
            <CircleAlert />
            <Alert.Title>{$error}</Alert.Title>
          </Alert.Root>
        </div>
      {:else if generatedImages.length > 1}
        <div class="grid h-full w-full grid-cols-2 gap-2 overflow-auto p-2">
          {#each generatedImages as img, i}
            <div class="relative flex items-center justify-center">
              <button
                class="cursor-pointer border-0 bg-transparent p-0"
                onclick={() => openFullscreen(i)}
                aria-label="View fullscreen"
              >
                <img
                  src={img}
                  alt="AI generated content {i + 1}"
                  class="max-h-full max-w-full object-contain transition-opacity hover:opacity-90"
                />
              </button>
              <Button
                variant="secondary"
                size="icon-sm"
                class="absolute right-2 bottom-2 bg-black/60 text-white hover:bg-black/80"
                onclick={(e) => { e.stopPropagation(); downloadImage(i); }}
                aria-label="Download image"
              >
                <Download />
              </Button>
            </div>
          {/each}
        </div>
      {:else if generatedImages.length === 1}
        <div class="relative flex max-h-full max-w-full items-center justify-center">
          <button
            class="cursor-pointer border-0 bg-transparent p-0"
            onclick={() => openFullscreen(0)}
            aria-label="View fullscreen"
          >
            <img
              src={generatedImages[0]}
              alt="AI generated content"
              class="max-h-full max-w-full object-contain transition-opacity hover:opacity-90"
            />
          </button>
          <Button
            variant="secondary"
            size="icon-sm"
            class="absolute right-2 bottom-2 bg-black/60 text-white hover:bg-black/80"
            onclick={(e) => { e.stopPropagation(); downloadImage(0); }}
            aria-label="Download image"
          >
            <Download />
          </Button>
        </div>
      {:else}
        <EmptyState title="No image" full />
      {/if}
    </div>

    <!-- Prompt -->
    <div class="flex shrink-0 flex-col gap-2 md:flex-row">
      <ExpandableTextarea
        bind:value={prompt}
        placeholder="Describe the image you want to generate..."
        rows={3}
        onkeydown={handleKeyDown}
        disabled={isGenerating || !$selectedModelStore}
      />
      <div class="flex flex-row gap-2 md:flex-col">
        {#if isGenerating}
          <Button variant="destructive" class="flex-1 md:flex-none" onclick={cancelGeneration}>
            Cancel
          </Button>
        {:else}
          <Button
            class="flex-1 md:flex-none"
            onclick={generate}
            disabled={!prompt.trim() || !$selectedModelStore}
          >
            Generate
          </Button>
          <Button
            variant="outline"
            class="flex-1 md:flex-none"
            onclick={clearImage}
            disabled={generatedImages.length === 0 && !$error && !prompt.trim()}
          >
            Clear
          </Button>
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- Fullscreen dialog -->
{#if showFullscreen && generatedImages[fullscreenIndex]}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/90 p-4"
    onclick={(e) => closeFullscreen(e)}
    onkeydown={(e) => e.key === 'Escape' && closeFullscreen()}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
  >
    <Button
      variant="secondary"
      size="icon"
      class="absolute top-4 right-4 bg-black/60 text-white hover:bg-black/80"
      onclick={() => closeFullscreen()}
      aria-label="Close fullscreen"
    >
      <X />
    </Button>
    <img
      src={generatedImages[fullscreenIndex]}
      alt="AI generated content"
      class="pointer-events-none max-h-full max-w-full object-contain"
    />
  </div>
{/if}
