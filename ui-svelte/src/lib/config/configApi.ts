// Client for the config editing API (internal/configedit, internal/mantle/config_api.go).
// Callers depend on the reader/writer interfaces; createConfigApi is the only
// implementation and takes fetch as a dependency so tests can drive it.

export const REDACTED = "[redacted]";

export interface ConfigProblem {
  line?: number;
  column?: number;
  message: string;
}

export interface SourceField {
  value: unknown;
  alias?: string;
  inheritedFrom?: string;
  redacted?: boolean;
  line?: number;
}

export interface SourceEntry {
  id: string;
  line: number;
  alias?: string;
  fields: Record<string, SourceField>;
  order: string[];
}

export interface SourceMacro {
  name: string;
  value: unknown;
  redacted?: boolean;
  line: number;
}

export interface SourceAnchor {
  name: string;
  kind: "scalar" | "sequence" | "mapping";
  line: number;
  value: unknown;
}

export type EntrySection = "models" | "groups" | "profiles" | "peers";
export type ChangeSection = EntrySection | "macros" | "";

export interface SourceDocument {
  hash: string;
  sections: Record<EntrySection, SourceEntry[]>;
  macros: SourceMacro[];
  settings: Record<string, SourceField>;
  anchors: SourceAnchor[];
}

/** Effective model: macros expanded, ${env.*} kept, secrets redacted. */
export type EffectiveModel = { id: string } & Record<string, unknown>;

export type ChangeOp = "set" | "unset" | "add" | "delete" | "rename";

export interface ConfigChange {
  op: ChangeOp;
  section: ChangeSection;
  id: string;
  path?: string[];
  value?: unknown;
  newId?: string;
  detach?: boolean;
}

/** "all": the reload stops every running model. "restart": applies on restart. */
export type ReloadMode = "all" | "restart";

export interface ConfigPreview {
  baseHash: string;
  hash: string;
  yaml: string;
  problems: ConfigProblem[];
  reload: ReloadMode;
}

export interface ApplyResult {
  hash: string;
  reload: ReloadMode;
}

export interface RawConfig {
  text: string;
  hash: string;
}

export type ConfigErrorCode =
  | "not_found"
  | "exists"
  | "aliased"
  | "inherited"
  | "redacted"
  | "invalid"
  | "stale";

export class ConfigApiError extends Error {
  readonly status: number;
  readonly code?: ConfigErrorCode;
  readonly anchor?: string;
  readonly changeIndex?: number;
  readonly problems: ConfigProblem[];

  constructor(status: number, body: ConfigErrorBody) {
    super(body.error || `HTTP ${status}`);
    this.name = "ConfigApiError";
    this.status = status;
    this.code = body.code;
    this.anchor = body.change?.anchor;
    this.changeIndex = body.change?.index;
    this.problems = body.problems ?? [];
  }
}

interface ConfigErrorBody {
  error?: string;
  code?: ConfigErrorCode;
  change?: { index: number; code: ConfigErrorCode; anchor?: string; message: string };
  problems?: ConfigProblem[];
}

export interface ConfigReader {
  readSource(): Promise<SourceDocument>;
  readEffectiveModels(): Promise<EffectiveModel[]>;
  readRaw(): Promise<RawConfig>;
}

export interface ValidationResult {
  problems: ConfigProblem[];
  reload: ReloadMode;
}

export interface ConfigValidator {
  validate(text: string): Promise<ValidationResult>;
}

export interface ConfigWriter {
  preview(baseHash: string, changes: ConfigChange[]): Promise<ConfigPreview>;
  apply(baseHash: string, changes: ConfigChange[]): Promise<ApplyResult>;
  putRaw(text: string, baseHash: string): Promise<ApplyResult>;
}

type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

const BASE = "/api/mantle/config";

async function failure(res: Response): Promise<ConfigApiError> {
  const body = (await res.json().catch(() => ({}))) as ConfigErrorBody;
  return new ConfigApiError(res.status, body);
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw await failure(res);
  return (await res.json()) as T;
}

function postJSON(fetchFn: FetchFn, url: string, body: unknown): Promise<Response> {
  return fetchFn(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

function etagHash(res: Response): string {
  return (res.headers.get("ETag") ?? "").replace(/^"|"$/g, "");
}

export function createConfigApi(fetchFn: FetchFn = (i, init) => fetch(i, init)): ConfigReader & ConfigValidator & ConfigWriter {
  return {
    async readSource() {
      return json<SourceDocument>(await fetchFn(`${BASE}/source`));
    },
    async readEffectiveModels() {
      return json<EffectiveModel[]>(await fetchFn(`${BASE}/models`));
    },
    async readRaw() {
      const res = await fetchFn(BASE);
      if (!res.ok) throw await failure(res);
      return { text: await res.text(), hash: etagHash(res) };
    },
    async validate(text) {
      const res = await fetchFn(`${BASE}/validate`, {
        method: "POST",
        headers: { "Content-Type": "text/yaml" },
        body: text,
      });
      return json<ValidationResult>(res);
    },
    async preview(baseHash, changes) {
      return json<ConfigPreview>(await postJSON(fetchFn, `${BASE}/changes/preview`, { baseHash, changes }));
    },
    async apply(baseHash, changes) {
      return json<ApplyResult>(await postJSON(fetchFn, `${BASE}/changes/apply`, { baseHash, changes }));
    },
    async putRaw(text, baseHash) {
      const res = await fetchFn(BASE, {
        method: "PUT",
        headers: { "Content-Type": "text/yaml", "If-Match": `"${baseHash}"` },
        body: text,
      });
      return json<ApplyResult>(res);
    },
  };
}

export const configApi = createConfigApi();

/** Macro references (${name}) in a string, excluding ${env.*}, ${PORT} and ${MODEL_ID}. */
export function macroRefs(s: string): string[] {
  const out: string[] = [];
  for (const m of s.matchAll(/\$\{([A-Za-z0-9_-]+)\}/g)) {
    if (m[1] !== "PORT" && m[1] !== "MODEL_ID" && !out.includes(m[1])) out.push(m[1]);
  }
  return out;
}

/** Env references (${env.NAME}) in a string. */
export function envRefs(s: string): string[] {
  return [...s.matchAll(/\$\{env\.([A-Za-z_][A-Za-z0-9_]*)\}/g)].map((m) => m[1]);
}
