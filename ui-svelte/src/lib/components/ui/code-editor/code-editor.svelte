<script lang="ts">
  // CodeMirror 6 editor: YAML mode, line numbers, search, and problems shown
  // inline as lint diagnostics. Colours come from the theme tokens.
  import { onMount } from "svelte";
  import { EditorState, Compartment, type Extension } from "@codemirror/state";
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection } from "@codemirror/view";
  import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
  import { syntaxHighlighting, HighlightStyle, indentOnInput, bracketMatching, foldGutter } from "@codemirror/language";
  import { yaml } from "@codemirror/lang-yaml";
  import { linter, lintGutter, setDiagnostics, type Diagnostic } from "@codemirror/lint";
  import { search, searchKeymap, highlightSelectionMatches } from "@codemirror/search";
  import { tags as t } from "@lezer/highlight";
  import { cn } from "$lib/utils.js";
  import { problemsToDiagnostics, type EditorProblem } from "./diagnostics.js";

  let {
    value = $bindable(""),
    problems = [],
    readonly = false,
    language = "yaml",
    class: className,
    ariaLabel = "Code editor",
    onchange,
  }: {
    value?: string;
    problems?: EditorProblem[];
    readonly?: boolean;
    language?: "yaml" | "text";
    class?: string;
    ariaLabel?: string;
    onchange?: (value: string) => void;
  } = $props();

  let host: HTMLDivElement;
  let view: EditorView | null = null;
  const readonlyConf = new Compartment();

  const highlight = HighlightStyle.define([
    { tag: [t.propertyName, t.definition(t.propertyName)], color: "var(--primary)" },
    { tag: [t.string, t.special(t.string)], color: "var(--foreground)" },
    { tag: [t.number, t.bool, t.null, t.atom], color: "var(--chart-4)" },
    { tag: t.comment, color: "var(--muted-foreground)", fontStyle: "italic" },
    { tag: [t.labelName, t.meta, t.typeName], color: "var(--chart-3)" },
    { tag: [t.punctuation, t.separator, t.bracket], color: "var(--muted-foreground)" },
  ]);

  const theme = EditorView.theme({
    "&": { height: "100%", backgroundColor: "var(--card)", color: "var(--foreground)", fontSize: "0.8125rem" },
    ".cm-scroller": { fontFamily: "var(--mono-stack)", lineHeight: "1.55" },
    ".cm-content": { caretColor: "var(--foreground)" },
    ".cm-gutters": { backgroundColor: "var(--panel-header-bg)", color: "var(--muted-foreground)", borderRight: "1px solid var(--border)" },
    ".cm-activeLine": { backgroundColor: "color-mix(in oklch, var(--muted) 60%, transparent)" },
    ".cm-activeLineGutter": { backgroundColor: "var(--muted)", color: "var(--foreground)" },
    "&.cm-focused": { outline: "none" },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection": { backgroundColor: "color-mix(in oklch, var(--primary) 25%, transparent) !important" },
    ".cm-cursor": { borderLeftColor: "var(--foreground)" },
    ".cm-panels": { backgroundColor: "var(--popover)", color: "var(--popover-foreground)", borderColor: "var(--border)" },
    ".cm-tooltip": { backgroundColor: "var(--popover)", color: "var(--popover-foreground)", border: "1px solid var(--border)", borderRadius: "var(--radius)" },
    ".cm-diagnostic-error": { borderLeftColor: "var(--destructive)" },
    ".cm-lintRange-error": { backgroundImage: "none", textDecoration: "underline wavy var(--destructive)" },
    ".cm-searchMatch": { backgroundColor: "color-mix(in oklch, var(--warning) 30%, transparent)" },
  });

  function extensions(): Extension[] {
    return [
      lineNumbers(),
      foldGutter(),
      lintGutter(),
      highlightActiveLineGutter(),
      highlightActiveLine(),
      drawSelection(),
      history(),
      indentOnInput(),
      bracketMatching(),
      highlightSelectionMatches(),
      search({ top: true }),
      language === "yaml" ? yaml() : [],
      syntaxHighlighting(highlight),
      linter(null),
      keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
      theme,
      readonlyConf.of(EditorState.readOnly.of(readonly)),
      EditorView.contentAttributes.of({ "aria-label": ariaLabel }),
      EditorView.updateListener.of((u) => {
        if (!u.docChanged) return;
        value = u.state.doc.toString();
        onchange?.(value);
      }),
    ];
  }

  onMount(() => {
    view = new EditorView({ state: EditorState.create({ doc: value, extensions: extensions() }), parent: host });
    return () => view?.destroy();
  });

  // External value changes (load, discard) replace the document.
  $effect(() => {
    const v = value;
    if (view && v !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } });
    }
  });

  $effect(() => {
    const ro = readonly;
    view?.dispatch({ effects: readonlyConf.reconfigure(EditorState.readOnly.of(ro)) });
  });

  $effect(() => {
    const p = problems;
    if (!view) return;
    const diagnostics: Diagnostic[] = problemsToDiagnostics(view.state.doc, p);
    view.dispatch(setDiagnostics(view.state, diagnostics));
  });

  /** Moves the cursor to a 1-based line and focuses the editor. */
  export function goToLine(line: number): void {
    if (!view) return;
    const l = view.state.doc.line(Math.min(Math.max(line, 1), view.state.doc.lines));
    view.dispatch({ selection: { anchor: l.from }, scrollIntoView: true });
    view.focus();
  }
</script>

<div
  bind:this={host}
  data-slot="code-editor"
  class={cn("min-h-0 overflow-hidden rounded-(--panel-radius) border", className)}
></div>
