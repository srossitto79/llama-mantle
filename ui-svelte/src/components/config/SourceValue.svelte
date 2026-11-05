<script lang="ts">
  // Renders a config value as written: ${macro} and ${env.NAME} references stay
  // references (a macro shows its value on hover), redacted secrets show as set.
  import { Lock } from "@lucide/svelte";
  import { REDACTED } from "$lib/config/configApi";

  let {
    value,
    macros = {},
    multiline = false,
  }: { value: unknown; macros?: Record<string, unknown>; multiline?: boolean } = $props();

  type Part = { kind: "text" | "macro" | "env" | "builtin" | "secret"; text: string; title?: string };

  function parts(s: string): Part[] {
    const out: Part[] = [];
    const re = /\$\{(env\.)?([A-Za-z0-9_-]+)\}|\[redacted\]/g;
    let last = 0;
    for (const m of s.matchAll(re)) {
      if (m.index! > last) out.push({ kind: "text", text: s.slice(last, m.index) });
      if (m[0] === REDACTED) out.push({ kind: "secret", text: "secret" });
      else if (m[1]) out.push({ kind: "env", text: `env.${m[2]}`, title: `Environment variable ${m[2]}` });
      else if (m[2] === "PORT" || m[2] === "MODEL_ID") out.push({ kind: "builtin", text: m[2] });
      else out.push({ kind: "macro", text: m[2], title: m[2] in macros ? String(macros[m[2]]) : "Undefined macro" });
      last = m.index! + m[0].length;
    }
    if (last < s.length) out.push({ kind: "text", text: s.slice(last) });
    return out;
  }

  let text = $derived(
    typeof value === "string" ? value : value === undefined || value === null ? "" : JSON.stringify(value),
  );
</script>

{#if value === REDACTED}
  <span class="text-muted-foreground inline-flex items-center gap-1 text-sm"><Lock class="size-3.5" />Set</span>
{:else}
  <span class={multiline ? "font-mono text-[0.8125rem] leading-6 whitespace-pre-wrap break-all" : "font-mono text-[0.8125rem] break-all"}
    >{#each parts(text) as p, i (i)}{#if p.kind === "text"}{p.text}{:else if p.kind === "secret"}<span
          class="text-muted-foreground inline-flex items-center gap-1 align-baseline"><Lock class="size-3" />{p.text}</span
        >{:else}<span
          title={p.title}
          class={[
            "rounded px-1 py-px",
            p.kind === "macro" && "bg-primary/12 text-primary",
            p.kind === "env" && "bg-info/15 text-foreground",
            p.kind === "builtin" && "bg-muted text-muted-foreground",
          ]}>{p.text}</span
        >{/if}{/each}</span
  >
{/if}
