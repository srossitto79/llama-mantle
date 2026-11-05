// What each config field is and how it is edited. The editor components are
// generic; this table decides labels, controls and grouping.
import { parse as parseYAML, stringify as stringifyYAML } from "yaml";
import { REDACTED, type ChangeSection, type SourceEntry } from "./configApi";
import type { DraftItem } from "./draft";

export type FieldKind =
  | "text"
  | "multiline"
  | "number"
  | "bool"
  /** true / false / not set (inherits a global default) */
  | "tristate"
  | "list"
  | "secret"
  | "yaml"
  /** One-line YAML scalar: numbers and booleans keep their type. */
  | "scalar"
  | "members"
  | "paramsById"
  /** One of `options`; the empty choice means not set. */
  | "select";

export interface FieldMeta {
  name: string;
  label: string;
  kind: FieldKind;
  placeholder?: string;
  /** Choices for "select" fields. */
  options?: string[];
  /** Value the loader uses when the field is absent. */
  defaultValue?: unknown;
}

export interface FieldGroup {
  title: string;
  fields: FieldMeta[];
}

export const modelFieldGroups: FieldGroup[] = [
  {
    title: "Command",
    fields: [
      { name: "cmd", label: "Command", kind: "multiline" },
      { name: "cmdStop", label: "Stop command", kind: "text" },
      { name: "env", label: "Environment", kind: "list", placeholder: "NAME=value" },
      { name: "macros", label: "Model macros", kind: "yaml" },
    ],
  },
  {
    title: "Routing & proxy",
    fields: [
      { name: "proxy", label: "Proxy URL", kind: "text", placeholder: "http://localhost:${PORT}" },
      { name: "checkEndpoint", label: "Health check", kind: "text", placeholder: "/health" },
      { name: "useModelName", label: "Upstream model name", kind: "text" },
      { name: "aliases", label: "Aliases", kind: "list" },
      { name: "apiKey", label: "Upstream API key", kind: "secret", placeholder: "${env.NAME}" },
      { name: "timeouts", label: "Timeouts", kind: "yaml" },
    ],
  },
  {
    title: "Request filters",
    fields: [
      { name: "filters.stripParams", label: "Strip parameters", kind: "text", placeholder: "temperature, top_p" },
      { name: "filters.setParams", label: "Set parameters", kind: "yaml" },
      { name: "filters.setParamsByID", label: "Parameters by suffix", kind: "paramsById" },
    ],
  },
  {
    // Applies to /v1/chat/completions of any local model whose backend streams
    // reasoning as `reasoning_content`; values mirror ReasoningLoopGuardConfig.
    title: "Reasoning loop guard",
    fields: [
      { name: "reasoningLoopGuard.mode", label: "Mode", kind: "select", options: ["off", "stop", "recover"], defaultValue: "off" },
      { name: "reasoningLoopGuard.recovery", label: "Recovery", kind: "select", options: ["wrapUp", "lowEffort"], defaultValue: "wrapUp" },
      { name: "reasoningLoopGuard.coverage", label: "Coverage", kind: "number", defaultValue: 0.25 },
      { name: "reasoningLoopGuard.checkEvery", label: "Check every (tokens)", kind: "number", defaultValue: 512 },
    ],
  },
  {
    title: "Lifecycle",
    fields: [
      { name: "ttl", label: "Unload after (s)", kind: "number" },
      { name: "unloadTimeout", label: "Unload timeout (s)", kind: "number" },
      { name: "concurrencyLimit", label: "Concurrency limit", kind: "number" },
      { name: "sendLoadingState", label: "Send loading state", kind: "tristate" },
    ],
  },
  {
    title: "Metadata",
    fields: [
      { name: "name", label: "Display name", kind: "text" },
      { name: "description", label: "Description", kind: "text" },
      { name: "unlisted", label: "Unlisted", kind: "bool" },
      { name: "capabilities", label: "Capabilities", kind: "yaml" },
      { name: "metadata", label: "Metadata", kind: "yaml" },
    ],
  },
];

export const groupFields: FieldMeta[] = [
  { name: "members", label: "Members", kind: "members" },
  { name: "swap", label: "Swap", kind: "bool" },
  { name: "exclusive", label: "Exclusive", kind: "bool" },
  { name: "persistent", label: "Persistent", kind: "bool" },
];

/** Group field defaults applied by the loader when a key is absent. */
export const groupDefaults: Record<string, unknown> = { swap: true, exclusive: true, persistent: false };

const knownModelFields = new Set(modelFieldGroups.flatMap((g) => g.fields.map((f) => f.name.split(".")[0])));

/** Fields written in the entry that the table does not describe, in file order. */
export function otherFields(order: string[]): FieldMeta[] {
  return order.filter((f) => !knownModelFields.has(f)).map((name) => ({ name, label: name, kind: "yaml" as const }));
}

export function fieldPath(name: string): string[] {
  return name.split(".");
}

/** Reads a dotted path from a plain value. */
export function getPath(value: unknown, path: string[]): unknown {
  let v = value;
  for (const seg of path) {
    if (v === null || typeof v !== "object") return undefined;
    v = (v as Record<string, unknown>)[seg];
  }
  return v;
}

export type StagedState = { staged: false } | { staged: true; removed: true } | { staged: true; removed: false; value: unknown };

/** The staged value of a field, if any change targets exactly this path or an add contains it. */
export function stagedValue(items: DraftItem[], section: ChangeSection, id: string, path: string[]): StagedState {
  let state: StagedState = { staged: false };
  const key = path.join(".");
  for (const { change: c } of items) {
    if (c.section !== section || c.id !== id) continue;
    if (c.op === "add") state = { staged: true, removed: false, value: getPath(c.value, path) };
    else if (c.op === "set" && (c.path ?? []).join(".") === key) state = { staged: true, removed: false, value: c.value };
    else if (c.op === "unset" && (c.path ?? []).join(".") === key) state = { staged: true, removed: true };
  }
  return state;
}

function containsRedacted(v: unknown): boolean {
  if (v === REDACTED) return true;
  if (Array.isArray(v)) return v.some(containsRedacted);
  if (v !== null && typeof v === "object") return Object.values(v).some(containsRedacted);
  return false;
}

function setPathIn(target: Record<string, unknown>, path: string[], value: unknown): void {
  let node = target;
  for (const seg of path.slice(0, -1)) {
    const next = node[seg];
    if (next === null || typeof next !== "object" || Array.isArray(next)) node[seg] = {};
    node = node[seg] as Record<string, unknown>;
  }
  const last = path[path.length - 1];
  if (value === undefined) delete node[last];
  else node[last] = value;
}

/**
 * The value of an entry as it would be written after the staged changes, for
 * duplicating it. Anchors are flattened. Fields holding a redacted secret
 * cannot be copied (the server rejects them) and are returned in `skipped`;
 * model aliases are dropped because they must stay unique.
 */
export function entryCopy(
  entry: SourceEntry,
  items: DraftItem[],
  section: ChangeSection,
): { value: Record<string, unknown>; skipped: string[] } {
  // Ids the entry had over its staged renames; edits target the id of the time.
  const ids = new Set([entry.id]);
  for (const { change: c } of items) {
    if (c.section === section && c.op === "rename" && ids.has(c.id) && c.newId) ids.add(c.newId);
  }

  let value: Record<string, unknown> = {};
  for (const name of entry.order) {
    const f = entry.fields[name];
    if (f) value[name] = structuredClone(f.value);
  }
  for (const { change: c } of items) {
    if (c.section !== section || !ids.has(c.id)) continue;
    if (c.op === "add" || ((c.op === "set" || c.op === "unset") && !c.path?.length)) {
      value = structuredClone((c.op === "unset" ? {} : c.value) ?? {}) as Record<string, unknown>;
    } else if (c.op === "set") {
      setPathIn(value, c.path!, structuredClone(c.value));
    } else if (c.op === "unset") {
      setPathIn(value, c.path!, undefined);
    }
  }

  if (section === "models") delete value.aliases;
  const skipped = Object.keys(value).filter((k) => containsRedacted(value[k]));
  for (const k of skipped) delete value[k];
  return { value, skipped };
}

export function toYAML(value: unknown): string {
  if (value === undefined || value === null) return "";
  return stringifyYAML(value, { lineWidth: 0 }).replace(/\n$/, "");
}

/** Parses a YAML snippet; empty text is "not set". */
export function fromYAML(text: string): { ok: true; value: unknown } | { ok: false; error: string } {
  if (text.trim() === "") return { ok: true, value: undefined };
  try {
    return { ok: true, value: parseYAML(text) };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message.split("\n")[0] : String(e) };
  }
}

/** Splits a list field typed one item per line. */
export function linesToList(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
}

/** Every model id and alias, for duplicate checks. */
export function usedModelNames(models: { id: string; fields: Record<string, { value: unknown }> }[]): Map<string, string> {
  const used = new Map<string, string>();
  for (const m of models) {
    used.set(m.id, m.id);
    const aliases = m.fields.aliases?.value;
    if (Array.isArray(aliases)) for (const a of aliases) if (typeof a === "string") used.set(a, m.id);
  }
  return used;
}

/** A model id must be non-empty, without whitespace or "/" (it appears in URLs). */
export function validateModelId(id: string, used: Map<string, string>, self?: string): string | null {
  if (id.trim() === "") return "Required";
  if (/\s/.test(id)) return "No spaces";
  if (id.includes("/")) return "No \"/\"";
  const owner = used.get(id);
  if (owner !== undefined && owner !== self) return owner === id ? `Model ${id} exists` : `Alias of ${owner}`;
  return null;
}

/** A row in an entry list (models, groups, ...). */
export interface ListItem {
  key: string;
  label: string;
  /** Tailwind class for a leading status dot, if any. */
  dot?: string;
  staged?: "new" | "deleted" | "modified";
}
