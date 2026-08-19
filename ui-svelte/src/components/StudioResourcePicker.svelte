<script lang="ts">
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import type { StudioResource } from "../lib/types";

  let { id, label, value = $bindable(""), resources, types = [], kinds = [], placeholder = "Search or enter a path", disabled = false, onValueChange }:
    { id: string; label: string; value?: string; resources: StudioResource[]; types?: string[]; kinds?: string[]; placeholder?: string; disabled?: boolean; onValueChange?: (value: string) => void } = $props();
  let options = $derived(resources.filter((resource) => resource.exists && (!types.length || types.includes(resource.type)) && (!kinds.length || kinds.includes(resource.kind))));
  let selected = $derived(resources.find((resource) => resource.path === value));

  function formatSize(bytes: number): string {
    if (!bytes) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
    return `${(bytes / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
  }
</script>

<div class="space-y-2">
  <Label.Root for={id}>{label}</Label.Root>
  <Input {id} list={`${id}-resources`} bind:value {placeholder} {disabled} autocomplete="off" onchange={() => onValueChange?.(value)} />
  <datalist id={`${id}-resources`}>{#each options as resource (resource.path)}<option value={resource.path}>{resource.type} · {resource.kind} · {formatSize(resource.size)}</option>{/each}</datalist>
  {#if selected}<p class="text-muted-foreground text-xs">{selected.type} · {selected.kind} · {formatSize(selected.size)}{selected.operation ? ` · from ${selected.operation}` : ""}</p>{/if}
</div>
