// Loaded state of config.yaml shared by the config pages: the raw text, the
// source view and the effective view, all read from the same file version.
import { writable, type Readable } from "svelte/store";
import { configApi, type ConfigReader, type EffectiveModel, type SourceDocument } from "./configApi";

export interface ConfigSnapshot {
  raw: string;
  hash: string;
  source: SourceDocument;
  effective: Record<string, EffectiveModel>;
}

export interface ConfigSessionState {
  snapshot: ConfigSnapshot | null;
  loading: boolean;
  error: string | null;
}

export interface ConfigSession extends Readable<ConfigSessionState> {
  /** Loads (or reloads) all views; retries when the file changed between reads. */
  load(): Promise<void>;
}

const MAX_ATTEMPTS = 3;

export function createConfigSession(reader: ConfigReader = configApi): ConfigSession {
  const store = writable<ConfigSessionState>({ snapshot: null, loading: false, error: null });

  async function load(): Promise<void> {
    store.update((s) => ({ ...s, loading: true, error: null }));
    try {
      for (let attempt = 1; ; attempt++) {
        const [raw, source, effective] = await Promise.all([reader.readRaw(), reader.readSource(), reader.readEffectiveModels()]);
        if (raw.hash === source.hash || attempt >= MAX_ATTEMPTS) {
          const byId = Object.fromEntries(effective.map((m) => [m.id, m]));
          store.set({ snapshot: { raw: raw.text, hash: source.hash, source, effective: byId }, loading: false, error: null });
          return;
        }
      }
    } catch (e) {
      store.update((s) => ({ ...s, loading: false, error: e instanceof Error ? e.message : String(e) }));
    }
  }

  return { subscribe: store.subscribe, load };
}

export const configSession = createConfigSession();
