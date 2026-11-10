// Client for the tuner API (internal/mantle/tuner). The types mirror the JSON
// tags of internal/mantle/tuner/tuner.go. Callers depend on TunerApi;
// createTunerApi is the only implementation and takes fetch as a dependency so
// tests can drive it.
import type { ConfigChange } from "./configApi";

export interface GPUInfo {
  name: string;
  totalMiB: number;
  usedMiB: number;
  freeMiB: number;
  pcieGen: number;
  pcieWidth: number;
}

export interface SystemInfo {
  gpus: GPUInfo[] | null;
  ramMiB: number;
  threads: number;
  avx512: boolean;
  hash: string;
  warnings: string[] | null;
}

export interface ModelInfo {
  path: string;
  arch: string;
  layers: number;
  kvLayers: number;
  embeddingLength: number;
  headCountKV: number;
  keyLength: number;
  valueLength: number;
  trainedContext: number;
  expertCount: number;
  expertUsedCount: number;
  nextnLayers: number;
  hasTokenEmbd: boolean;
  totalBytes: number;
  expertBytes: number;
  denseBytes: number;
  kvBytesPerTokenQ8: number;
  shards: number;
}

export interface DraftCompat {
  path: string;
  ok: boolean;
  reason?: string;
}

export interface BackendFit {
  name: string;
  binary: string;
  /** Feature name to availability, e.g. "moe-cache", "spec-type". */
  features: Record<string, boolean>;
  draft?: DraftCompat;
  blocked?: string;
  cacheTypes: string[] | null;
}

export type Layout = "full-gpu" | "moe-cache" | "n-cpu-moe" | "fit";

/** llama.cpp's projection of the launch's device memory on the first GPU. */
export interface Projection {
  freeMiB: number;
  modelMiB: number;
  contextMiB: number;
  computeMiB: number;
  totalMiB: number;
}

/** moe-cache-tune.py's pick for one MoE-cache candidate. */
export interface CacheTune {
  cacheMiB: number;
  cpuShare: number;
  /** Predicted generation speed with a warm cache. */
  tokensPerSec: number;
  shareZeroTps?: number;
  /** Every expert fits in VRAM: the cache is not needed. */
  allFit?: boolean;
  report: string;
}

export interface Candidate {
  id: string;
  tier: number;
  backend: string;
  layout: Layout;
  args: string[] | null;
  reason: string;
  blocked?: string;
  /** moe-cache-tune can refine the cache size and CPU share. */
  tunable: boolean;
  cacheTune?: CacheTune;
  memory?: Projection;
  entry?: ConfigChange;
}

export type TaskState = "queued" | "running" | "completed" | "failed" | "cancelled";

/** A cache tune task, as GET /api/mantle/tasks/{id} returns it. */
export interface CacheTuneTask {
  id: string;
  state: TaskState;
  message: string;
  pct: number;
  logs?: string[] | null;
  /** Set when the task completes. */
  parameters?: { candidate?: Candidate } | null;
}

export interface TierPlan {
  tier: number;
  skipped?: string;
  candidates: Candidate[] | null;
}

export interface Analysis {
  system: SystemInfo;
  model: ModelInfo;
  backends: BackendFit[] | null;
  tiers: TierPlan[] | null;
}

export interface AnalyseRequest {
  model: string;
  draft?: string;
  tiers?: number[];
  name?: string;
}

export interface LocalModel {
  name: string;
  path: string;
  size: number;
  kind: string;
}

export class TunerApiError extends Error {
  readonly status: number;

  constructor(status: number, message?: string) {
    super(message || `HTTP ${status}`);
    this.name = "TunerApiError";
    this.status = status;
  }
}

export interface TunerApi {
  analyse(req: AnalyseRequest): Promise<Analysis>;
  localModels(): Promise<LocalModel[]>;
  /** Starts moe-cache-tune on a candidate of the last analysis. */
  startCacheTune(candidateId: string): Promise<CacheTuneTask>;
  task(id: string): Promise<CacheTuneTask>;
  cancelCacheTune(id: string): Promise<void>;
}

type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

const BASE = "/api/mantle";

async function failure(res: Response): Promise<TunerApiError> {
  const body = (await res.json().catch(() => ({}))) as { error?: string };
  return new TunerApiError(res.status, body.error);
}

export function createTunerApi(fetchFn: FetchFn = (i, init) => fetch(i, init)): TunerApi {
  return {
    async analyse(req) {
      const res = await fetchFn(`${BASE}/tuner/analyse`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
      });
      if (!res.ok) throw await failure(res);
      return (await res.json()) as Analysis;
    },
    async localModels() {
      const res = await fetchFn(`${BASE}/models/local`);
      if (!res.ok) throw await failure(res);
      return ((await res.json()) as LocalModel[] | null) ?? [];
    },
    async startCacheTune(candidateId) {
      const res = await fetchFn(`${BASE}/tuner/cache-tune`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ candidate: candidateId }),
      });
      if (!res.ok) throw await failure(res);
      return (await res.json()) as CacheTuneTask;
    },
    async task(id) {
      const res = await fetchFn(`${BASE}/tasks/${encodeURIComponent(id)}`);
      if (!res.ok) throw await failure(res);
      return (await res.json()) as CacheTuneTask;
    },
    async cancelCacheTune(id) {
      const res = await fetchFn(`${BASE}/tuner/cache-tune/${encodeURIComponent(id)}`, { method: "DELETE" });
      if (!res.ok) throw await failure(res);
    },
  };
}

export const tunerApi = createTunerApi();

/**
 * Model files the tuner can analyse: GGUF only, no multimodal projectors, and
 * one entry per split file (its first shard).
 */
export function tunableModels(list: LocalModel[]): LocalModel[] {
  return list.filter((m) => {
    if (m.kind !== "gguf" || m.name.toLowerCase().includes("mmproj")) return false;
    const split = /-(\d{5})-of-\d{5}/.exec(m.name);
    return !split || split[1] === "00001";
  });
}
