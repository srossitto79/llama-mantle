<script lang="ts">
  import { onMount } from "svelte";
  import { Plus, Save, Trash2 } from "@lucide/svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import { EmptyState } from "$lib/components/ui/empty-state/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import * as Label from "$lib/components/ui/label/index.js";
  import { deleteStudioProject, listStudioProjects, listStudioResources, saveStudioProject, setStudioProjectResources } from "../lib/mantleApi";
  import type { StudioProject, StudioResource } from "../lib/types";
  import { activeStudioProject } from "../stores/studioProject";

  let projects=$state<StudioProject[]>([]),resources=$state<StudioResource[]>([]),selectedID=$state(""),name=$state(""),description=$state(""),selectedPaths=$state<string[]>([]),error=$state(""),busy=$state(false);
  let selected=$derived(projects.find((item)=>item.id===selectedID));
  let grouped=$derived(Object.entries(Object.groupBy(resources,(resource)=>resource.type)));
  async function refresh(){busy=true;try{[projects,resources]=await Promise.all([listStudioProjects(),listStudioResources()]);error=""}catch(cause){error=cause instanceof Error?cause.message:String(cause)}finally{busy=false}}
  function load(project:StudioProject){selectedID=project.id;name=project.name;description=project.description??"";selectedPaths=[...project.resources]}
  function create(){selectedID="";name="New project";description="";selectedPaths=[]}
  function toggle(path:string,checked:boolean){selectedPaths=checked?[...selectedPaths,path]:selectedPaths.filter((item)=>item!==path)}
  async function save(){busy=true;try{const project=await saveStudioProject({id:selectedID||undefined,name,description});await setStudioProjectResources(project.id,selectedPaths);await refresh();load((await listStudioProjects()).find((item)=>item.id===project.id)??project);error=""}catch(cause){error=cause instanceof Error?cause.message:String(cause)}finally{busy=false}}
  async function remove(){if(!selected||!confirm(`Delete project ${selected.name}? Resources are not deleted.`))return;busy=true;try{await deleteStudioProject(selected.id);if($activeStudioProject===selected.id)activeStudioProject.set("");selectedID="";name="";description="";selectedPaths=[];await refresh()}catch(cause){error=cause instanceof Error?cause.message:String(cause)}finally{busy=false}}
  onMount(refresh);
  import * as Page from "$lib/components/ui/page/index.js";
</script>
<Page.Root title="Studio Projects" width="full">
  {#snippet actions()}<Button size="sm" onclick={create}><Plus class="size-4" />New project</Button>{/snippet}
  {#if error}<Alert.Root variant="destructive"><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
  <!-- An active project groups new jobs, artifacts and saved recipes. Deleting a project never deletes its files or jobs. -->
  <div class="grid gap-(--page-gap) lg:grid-cols-[18rem_1fr]">
    <Card.Root class="h-fit gap-0 py-0">
      <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5"><h2 class="text-heading">Projects</h2></div>
      {#if !projects.length}
        <EmptyState title="No projects" />
      {:else}
        <ul class="divide-y">
          {#each projects as project (project.id)}
            <li><button type="button" class="hover:bg-muted/50 w-full px-4 py-2.5 text-left" class:bg-muted={project.id === selectedID} onclick={() => load(project)}><span class="block text-sm font-medium">{project.name}{#if $activeStudioProject === project.id} · active{/if}</span><span class="text-muted-foreground text-xs">{project.resources.length} resources</span></button></li>
          {/each}
        </ul>
      {/if}
    </Card.Root>
    <Card.Root class="gap-0 py-0">
      <div class="bg-(--panel-header-bg) flex items-center gap-2 border-b px-4 py-2.5"><h2 class="text-heading">{selectedID ? "Edit project" : "New project"}</h2></div>
      <div class="space-y-4 p-4">
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="space-y-1"><Label.Root for="project-name">Name</Label.Root><Input id="project-name" bind:value={name} /></div>
          <div class="space-y-1"><Label.Root for="project-description">Description</Label.Root><Input id="project-description" bind:value={description} /></div>
        </div>
        {#each grouped as [type, items]}
          <div>
            <h3 class="text-label text-muted-foreground mb-2 font-semibold uppercase">{type}s</h3>
            <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
              {#each items ?? [] as resource (resource.path)}
                <label class="hover:bg-muted/50 flex min-w-0 items-center gap-2 rounded-md border p-2 text-sm"><input type="checkbox" checked={selectedPaths.includes(resource.path)} onchange={(event) => toggle(resource.path, event.currentTarget.checked)} /><span class="min-w-0"><span class="block truncate font-mono text-[0.8125rem]">{resource.path}</span><span class="text-muted-foreground text-xs">{resource.kind}</span></span></label>
              {/each}
            </div>
          </div>
        {/each}
        <div class="flex flex-wrap justify-end gap-2">
          {#if selectedID}
            <Button size="sm" variant="outline" onclick={() => activeStudioProject.set(selectedID)} disabled={$activeStudioProject === selectedID}>Use as active project</Button>
            <Button size="sm" variant="destructive" onclick={remove} disabled={busy}><Trash2 class="size-4" />Delete project</Button>
          {/if}
          <Button size="sm" onclick={save} disabled={busy || !name.trim()}><Save class="size-4" />Save project</Button>
        </div>
      </div>
    </Card.Root>
  </div>
</Page.Root>
