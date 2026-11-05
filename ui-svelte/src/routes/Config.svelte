<script lang="ts">
  // Configuration area: structured editors for every section of config.yaml
  // plus the raw file. Structured edits are staged in the config draft and
  // written together after review; nothing here writes directly.
  import { onMount } from "svelte";
  import { configSession } from "$lib/config/session";
  import { configDraft } from "$lib/config/draft";
  import { groupFields, groupDefaults, usedModelNames, validateModelId, entryCopy, type ListItem } from "$lib/config/fields";
  import type { EntrySection, SourceEntry } from "$lib/config/configApi";
  import { models as runtimeModels } from "../stores/api";
  import { statusDotColor } from "../stores/modelLoad";
  import * as Page from "$lib/components/ui/page/index.js";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import * as Field from "$lib/components/ui/field/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import EntryList from "../components/config/EntryList.svelte";
  import ModelEditor from "../components/config/ModelEditor.svelte";
  import EntryEditor from "../components/config/EntryEditor.svelte";
  import MacrosSection from "../components/config/MacrosSection.svelte";
  import RawSection from "../components/config/RawSection.svelte";
  import ReviewSheet from "../components/config/ReviewSheet.svelte";
  import { FileDiff, CircleAlert, Loader2, RefreshCw } from "@lucide/svelte";

  type Tab = EntrySection | "macros" | "raw";

  let tab = $state<Tab>("models");
  let reviewOpen = $state(false);
  let rawDirty = $state(false);
  const selected = $state<Record<EntrySection, string | null>>({ models: null, groups: null, profiles: null, peers: null });

  onMount(() => {
    if (!$configSession.snapshot) void configSession.load();
  });

  const snapshot = $derived($configSession.snapshot);
  const source = $derived(snapshot?.source ?? null);
  const macros = $derived(Object.fromEntries((source?.macros ?? []).map((m) => [m.name, m.value])));
  const stagedCount = $derived($configDraft.length);

  /** Source entries plus entries staged for addition. */
  function entriesOf(section: EntrySection): SourceEntry[] {
    const list = [...(source?.sections[section] ?? [])];
    for (const { change: c } of $configDraft) {
      if (c.section === section && c.op === "add" && !list.some((e) => e.id === c.id)) {
        list.push({ id: c.id, line: 0, fields: {}, order: [] });
      }
    }
    return list;
  }

  function listItems(section: EntrySection): ListItem[] {
    return entriesOf(section).map((e) => {
      const current = configDraft.currentId(section, e.id);
      const touched = $configDraft.some((it) => it.change.section === section && (it.change.id === e.id || it.change.id === current));
      const isNew = e.line === 0;
      const rt = section === "models" ? $runtimeModels.find((m) => m.id === e.id) : undefined;
      return {
        key: e.id,
        label: current || e.id,
        dot: section === "models" ? statusDotColor(rt) : undefined,
        staged: isNew ? "new" : current === "" ? "deleted" : touched ? "modified" : undefined,
      };
    });
  }

  function selectedEntry(section: EntrySection): SourceEntry | undefined {
    const list = entriesOf(section);
    return list.find((e) => e.id === selected[section]) ?? list[0];
  }

  // Add dialog
  let addSection = $state<EntrySection | null>(null);
  let addId = $state("");
  let addCmd = $state("");
  const addError = $derived.by(() => {
    if (!addSection || addId.trim() === "") return null;
    if (addSection === "models") return validateModelId(addId.trim(), usedModelNames(entriesOf("models")));
    return entriesOf(addSection).some((e) => e.id === addId.trim()) ? `${addId.trim()} exists` : null;
  });
  const addNoun: Record<EntrySection, string> = { models: "model", groups: "group", profiles: "profile", peers: "peer" };

  /** Entry being duplicated by the add dialog, if any. */
  let copyOf = $state<{ id: string; value: Record<string, unknown>; skipped: string[] } | null>(null);

  function openAdd(section: EntrySection): void {
    addSection = section;
    addId = "";
    addCmd = "";
    copyOf = null;
  }

  function openDuplicate(section: EntrySection, entry: SourceEntry): void {
    const current = configDraft.currentId(section, entry.id) || entry.id;
    copyOf = { id: current, ...entryCopy(entry, $configDraft, section) };
    addSection = section;
    const taken = new Set(section === "models" ? usedModelNames(entriesOf("models")).keys() : entriesOf(section).map((e) => e.id));
    let n = 1;
    addId = `${current}-copy`;
    while (taken.has(addId)) addId = `${current}-copy${++n}`;
    addCmd = "";
  }

  function confirmAdd(): void {
    if (!addSection || !addId.trim() || addError) return;
    const id = addId.trim();
    const value: Record<string, unknown> = copyOf
      ? copyOf.value
      : addSection === "models"
        ? { cmd: addCmd.trim() }
        : addSection === "groups"
          ? { members: [] }
          : {};
    configDraft.add(addSection, id, value);
    selected[addSection] = id;
    addSection = null;
    copyOf = null;
  }
</script>

{#snippet sectionPane(section: EntrySection)}
  {@const entry = selectedEntry(section)}
  <div class="grid h-full min-h-0 gap-(--page-gap) md:grid-cols-[18rem_1fr]">
    <EntryList items={listItems(section)} bind:selected={selected[section]} addLabel="Add {addNoun[section]}" onadd={() => openAdd(section)} />
    {#if !entry}
      <EmptyState title="No {addNoun[section]}s" full />
    {:else if section === "models"}
      {#key entry.id}
        <ModelEditor
          {entry}
          models={entriesOf("models")}
          {macros}
          effective={snapshot?.effective[entry.id]}
          running={$runtimeModels.some((m) => m.id === entry.id && m.state === "ready")}
          onduplicate={() => openDuplicate(section, entry)}
        />
      {/key}
    {:else}
      {#key entry.id}
        <EntryEditor
          {section}
          {entry}
          ids={entriesOf(section).map((e) => e.id)}
          fields={section === "groups" ? groupFields : []}
          defaults={section === "groups" ? groupDefaults : {}}
          knownIds={section === "groups" ? entriesOf("models").map((e) => configDraft.currentId("models", e.id)).filter(Boolean) : undefined}
          onduplicate={() => openDuplicate(section, entry)}
        />
      {/key}
    {/if}
  </div>
{/snippet}

<Page.Root title="Configuration" width="full" fill>
  {#snippet actions()}
    {#if $configSession.loading}<Loader2 class="text-muted-foreground size-4 animate-spin" />{/if}
    <Button variant="ghost" size="sm" onclick={() => configSession.load()} disabled={$configSession.loading}><RefreshCw />Reload</Button>
    <Button size="sm" disabled={stagedCount === 0} onclick={() => (reviewOpen = true)}>
      <FileDiff />Review{#if stagedCount}&nbsp;({stagedCount}){/if}
    </Button>
  {/snippet}

  {#if $configSession.error && !snapshot}
    <Alert.Root variant="destructive">
      <CircleAlert />
      <Alert.Title>{$configSession.error}</Alert.Title>
    </Alert.Root>
  {:else if source}
    <Tabs.Root bind:value={tab} class={tab === "raw" ? "flex-none" : "min-h-0 flex-1"}>
      <Tabs.List class="max-w-full justify-start overflow-x-auto">
        <Tabs.Trigger value="models">Models <span class="text-muted-foreground ml-1 text-xs">{source.sections.models.length}</span></Tabs.Trigger>
        <Tabs.Trigger value="groups">Groups <span class="text-muted-foreground ml-1 text-xs">{source.sections.groups.length}</span></Tabs.Trigger>
        <Tabs.Trigger value="profiles">Profiles <span class="text-muted-foreground ml-1 text-xs">{source.sections.profiles.length}</span></Tabs.Trigger>
        <Tabs.Trigger value="peers">Peers <span class="text-muted-foreground ml-1 text-xs">{source.sections.peers.length}</span></Tabs.Trigger>
        <Tabs.Trigger value="macros">Macros <span class="text-muted-foreground ml-1 text-xs">{source.macros.length}</span></Tabs.Trigger>
        <Tabs.Trigger value="raw">Raw{#if rawDirty}<span class="bg-primary ml-1.5 size-1.5 rounded-full"></span>{/if}</Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="models" class="min-h-0 flex-1">{@render sectionPane("models")}</Tabs.Content>
      <Tabs.Content value="groups" class="min-h-0 flex-1">{@render sectionPane("groups")}</Tabs.Content>
      <Tabs.Content value="profiles" class="min-h-0 flex-1">{@render sectionPane("profiles")}</Tabs.Content>
      <Tabs.Content value="peers" class="min-h-0 flex-1">{@render sectionPane("peers")}</Tabs.Content>
      <Tabs.Content value="macros" class="min-h-0 flex-1 overflow-y-auto"><MacrosSection {source} /></Tabs.Content>
    </Tabs.Root>
    <!-- Kept mounted so unsaved raw edits survive tab switches. -->
    <div class={["min-h-0 flex-1", tab !== "raw" && "hidden"]}>
      {#if stagedCount > 0}
        <Alert.Root class="mb-2">
          <FileDiff />
          <Alert.Title>{stagedCount === 1 ? "1 staged change" : `${stagedCount} staged changes`} not in the raw file</Alert.Title>
          <Alert.Description><Button variant="outline" size="xs" class="mt-1" onclick={() => (reviewOpen = true)}>Review</Button></Alert.Description>
        </Alert.Root>
      {/if}
      <RawSection bind:dirty={rawDirty} />
    </div>
  {:else}
    <div class="text-muted-foreground flex items-center gap-2 text-sm"><Loader2 class="size-4 animate-spin" />Loading</div>
  {/if}
</Page.Root>

<ReviewSheet bind:open={reviewOpen} />

<Dialog.Root open={addSection !== null} onOpenChange={(o) => !o && (addSection = null)}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title>{copyOf ? `Duplicate ${copyOf.id}` : `Add ${addSection ? addNoun[addSection] : ""}`}</Dialog.Title>
    </Dialog.Header>
    <div class="grid gap-3">
      <Field.Root label="Id" error={addError} required>
        {#snippet control({ id, describedBy, invalid })}
          <Input {id} bind:value={addId} aria-describedby={describedBy} aria-invalid={invalid} class="font-mono" onkeydown={(e) => e.key === "Enter" && addSection !== "models" && confirmAdd()} />
        {/snippet}
      </Field.Root>
      {#if copyOf && copyOf.skipped.length > 0}
        <p class="text-muted-foreground text-sm">Not copied (secrets): <span class="font-mono">{copyOf.skipped.join(", ")}</span></p>
      {/if}
      {#if addSection === "models" && !copyOf}
        <Field.Root label="Command" required>
          {#snippet control({ id })}
            <Textarea {id} bind:value={addCmd} rows={4} class="font-mono text-[0.8125rem]" placeholder={"llama-server --port ${PORT} -m /models/model.gguf"} spellcheck={false} />
          {/snippet}
        </Field.Root>
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (addSection = null)}>Cancel</Button>
      <Button disabled={!addId.trim() || !!addError || (addSection === "models" && !copyOf && !addCmd.trim())} onclick={confirmAdd}>Stage</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
