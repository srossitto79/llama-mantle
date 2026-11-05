// What each config field is and how it is edited. The editor components are
// generic; this table decides labels, controls and grouping.
import { parse as parseYAML, stringify as stringifyYAML } from "yaml";
import type { ChangeSection } from "./configApi";
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
  | "paramsById";

export interface FieldMeta {
  name: string;
  label: string;
  kind: FieldKind;
  placeholder?: string;
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
