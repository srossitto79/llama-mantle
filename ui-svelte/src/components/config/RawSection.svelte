<script lang="ts">
  // Raw config.yaml editor: CodeMirror with problems from the validate
  // endpoint inline, and the same review step before writing.
  import { configSession } from "$lib/config/session";
  import { configApi, type ConfigProblem } from "$lib/config/configApi";
  import { CodeEditor } from "$lib/components/ui/code-editor/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import ReviewSheet from "./ReviewSheet.svelte";
  import { FileDiff, Undo2, CircleCheck, CircleAlert, Loader2 } from "@lucide/svelte";

  let { dirty = $bindable(false) }: { dirty?: boolean } = $props();

  const snapshot = $derived($configSession.snapshot);
  let text = $state("");
  let baseHash = $state("");
  let problems = $state<ConfigProblem[]>([]);
  let checking = $state(false);
  let reviewOpen = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  // Take the file when it loads, unless the user has unsaved raw edits.
  $effect(() => {
    if (snapshot && (snapshot.hash !== baseHash || !dirty)) {
      if (!dirty) {
        text = snapshot.raw;
        problems = [];
      }
      baseHash = snapshot.hash;
    }
  });

  $effect(() => {
    dirty = !!snapshot && text !== snapshot.raw;
  });

  function scheduleValidate(): void {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      checking = true;
      try {
        problems = (await configApi.validate(text)).problems;
      } catch {
        problems = [];
      } finally {
        checking = false;
      }
    }, 600);
  }

  function revert(): void {
    if (snapshot) text = snapshot.raw;
    problems = [];
  }
</script>

<div class="flex h-full min-h-0 flex-col gap-2">
  <div class="flex flex-wrap items-center gap-2">
    <span class="text-muted-foreground inline-flex items-center gap-1.5 text-sm">
      {#if checking}
        <Loader2 class="size-4 animate-spin" />Checking
      {:else if problems.length}
        <CircleAlert class="text-destructive size-4" />{problems.length === 1 ? "1 problem" : `${problems.length} problems`}
      {:else if dirty}
        <CircleCheck class="text-success size-4" />Valid
      {/if}
    </span>
    <div class="ml-auto flex gap-2">
      <Button variant="ghost" size="sm" disabled={!dirty} onclick={revert}><Undo2 />Revert</Button>
      <Button size="sm" disabled={!dirty || problems.length > 0} onclick={() => (reviewOpen = true)}><FileDiff />Review</Button>
    </div>
  </div>
  <CodeEditor bind:value={text} {problems} onchange={scheduleValidate} class="min-h-[24rem] flex-1" ariaLabel="config.yaml" />
</div>

<ReviewSheet bind:open={reviewOpen} rawText={text} />
