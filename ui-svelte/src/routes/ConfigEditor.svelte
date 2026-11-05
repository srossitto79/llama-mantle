<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import * as Page from "$lib/components/ui/page/index.js";
  import { getConfig, putConfig } from "../lib/mantleApi";

  let yaml = $state("");
  let loading = $state(true);
  let saving = $state(false);
  let message = $state("");
  let messageType = $state<"success" | "error" | "">("");

  $effect(() => {
    loadConfig();
  });

  async function loadConfig() {
    loading = true;
    const data = await getConfig();
    if (data !== null) {
      yaml = data;
    } else {
      message = "Failed to load config";
      messageType = "error";
    }
    loading = false;
  }

  async function saveConfig() {
    saving = true;
    message = "";
    const ok = await putConfig(yaml);
    if (ok) {
      message = "Config saved and hot-reloaded";
      messageType = "success";
    } else {
      message = "Failed to save config (check YAML syntax)";
      messageType = "error";
    }
    saving = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === "s") {
      e.preventDefault();
      saveConfig();
    }
  }
</script>

<Page.Root title="Configuration" width="full" fill={true}>
  {#snippet actions()}
    {#if message}
      <span class="text-sm" class:text-success={messageType === "success"} class:text-destructive={messageType === "error"}>
        {message}
      </span>
    {/if}
    <Button size="sm" onclick={saveConfig} disabled={saving || loading}>
      {saving ? "Saving..." : "Save & Reload"}
    </Button>
  {/snippet}

  {#if loading}
    <div class="flex-1 flex items-center justify-center text-muted-foreground">
      Loading config...
    </div>
  {:else}
    <Textarea
      class="flex-1 w-full font-mono text-sm p-4 resize-none"
      bind:value={yaml}
      onkeydown={handleKeydown}
      spellcheck="false"
    />
    <p class="text-xs text-muted-foreground mt-2">
      Ctrl+S to save. Invalid YAML will be rejected before the file is written.
    </p>
  {/if}
</Page.Root>
