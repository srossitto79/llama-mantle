import type { MantleTask, HFModel, HFFile, LocalModel, BackendEntry, DatasetInspection, EvaluateRequest, ExportLoRARequest, HashRequest, MergeRequest, PruneRequest, QuantizeRequest, RegisterStudioModelRequest, SplitRequest, StudioCatalogArtifact, StudioEvaluation, StudioLineageEdge, StudioModelInspection, StudioPipelineRequest, StudioPipelineTemplate, StudioRetentionPolicy, StudioRetentionPreview, StudioSchedulerStatus, TrainQLoRARequest } from "../lib/types";

// --- HF Model search ---

export type HFSort = "relevance" | "trending" | "downloads" | "likes" | "created" | "modified";
export type HFKind = "text" | "image" | "transcription" | "tts";

export async function searchHFModels(query: string, limit = 20, sort: HFSort = "downloads", kind: HFKind = "text"): Promise<HFModel[]> {
  try {
    const res = await fetch(`/api/mantle/models/search?q=${encodeURIComponent(query)}&limit=${limit}&sort=${sort}&kind=${kind}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("HF search failed:", e);
    return [];
  }
}

export async function listHFFiles(modelID: string): Promise<HFFile[]> {
  try {
    const res = await fetch(`/api/mantle/models/files?model=${encodeURIComponent(modelID)}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("HF files list failed:", e);
    return [];
  }
}

// --- Downloads ---

export async function startDownload(modelID: string, filename: string): Promise<MantleTask | null> {
  try {
    const res = await fetch("/api/mantle/models/download", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ modelID, filename }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("Start download failed:", e);
    return null;
  }
}

export async function startRepoDownload(modelID: string): Promise<MantleTask | null> {
  try {
    const res = await fetch("/api/mantle/models/download/repo", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ modelID }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("Start repo download failed:", e);
    return null;
  }
}

export async function cancelDownload(taskID: string): Promise<boolean> {
  try {
    const res = await fetch(`/api/mantle/models/download/${taskID}`, { method: "DELETE" });
    return res.ok;
  } catch {
    return false;
  }
}

// --- Local models ---

export async function listLocalModels(): Promise<LocalModel[]> {
  try {
    const res = await fetch("/api/mantle/models/local");
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("List local models failed:", e);
    return [];
  }
}

export async function deleteLocalModel(name: string): Promise<boolean> {
  try {
    const res = await fetch(`/api/mantle/models/local/${encodeURIComponent(name)}`, { method: "DELETE" });
    return res.ok;
  } catch {
    return false;
  }
}

// --- Config ---

export async function getConfig(): Promise<string | null> {
  try {
    const res = await fetch("/api/mantle/config");
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.text();
  } catch (e) {
    console.error("Get config failed:", e);
    return null;
  }
}

export async function putConfig(yaml: string): Promise<boolean> {
  try {
    const res = await fetch("/api/mantle/config", {
      method: "PUT",
      headers: { "Content-Type": "text/yaml" },
      body: yaml,
    });
    return res.ok;
  } catch {
    return false;
  }
}

// --- Backend builds ---

export async function startBuild(repo: string, branch: string, cmakeFlags = "", backendName = ""): Promise<MantleTask | null> {
  try {
    const res = await fetch("/api/mantle/backends/build", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ repo, branch, cmakeFlags, backendName }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("Start build failed:", e);
    return null;
  }
}

export async function cancelBuild(taskID: string): Promise<boolean> {
  try {
    const res = await fetch(`/api/mantle/backends/build/${taskID}`, { method: "DELETE" });
    return res.ok;
  } catch {
    return false;
  }
}

// --- Backend listing ---

export async function listBackends(): Promise<BackendEntry[]> {
  try {
    const res = await fetch("/api/mantle/backends");
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("List backends failed:", e);
    return [];
  }
}

export async function deleteBackend(name: string): Promise<boolean> {
  try {
    const res = await fetch(`/api/mantle/backends/${encodeURIComponent(name)}`, { method: "DELETE" });
    return res.ok;
  } catch {
    return false;
  }
}

export async function updateBackend(name: string, repo?: string, branch?: string): Promise<MantleTask | null> {
  try {
    const res = await fetch(`/api/mantle/backends/${encodeURIComponent(name)}/update`, {
      method: "POST",
      headers: repo ? { "Content-Type": "application/json" } : undefined,
      body: repo ? JSON.stringify({ repo, branch }) : undefined,
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("Update backend failed:", e);
    return null;
  }
}

// --- Tasks ---

export async function listTasks(): Promise<MantleTask[]> {
  try {
    const res = await fetch("/api/mantle/tasks");
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch (e) {
    console.error("List tasks failed:", e);
    return [];
  }
}

export async function getTask(id: string): Promise<MantleTask | null> {
  try {
    const res = await fetch(`/api/mantle/tasks/${id}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.json();
  } catch {
    return null;
  }
}

// --- Llama Studio ---

export async function inspectStudioModel(name: string): Promise<StudioModelInspection> {
  const res = await fetch(`/api/mantle/studio/models/inspect?name=${encodeURIComponent(name)}`);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return await res.json();
}

export async function inspectStudioDataset(name: string): Promise<DatasetInspection> {
	const res = await fetch(`/api/mantle/studio/datasets/inspect?name=${encodeURIComponent(name)}`);
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return await res.json();
}

export async function startQuantize(request: QuantizeRequest): Promise<MantleTask> {
  const res = await fetch("/api/mantle/studio/quantize", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return await res.json();
}

export async function startHash(request: HashRequest): Promise<MantleTask> {
	return startStudioOperation("hash", request);
}

export async function startSplit(request: SplitRequest): Promise<MantleTask> {
	return startStudioOperation("split", request);
}

export async function startMerge(request: MergeRequest): Promise<MantleTask> {
	return startStudioOperation("merge", request);
}

export async function startPrune(request: PruneRequest): Promise<MantleTask> {
	return startStudioOperation("prune", request);
}

export async function startTrainQLoRA(request: TrainQLoRARequest): Promise<MantleTask> {
	return startStudioOperation("train/qlora", request);
}

export async function startExportLoRA(request: ExportLoRARequest): Promise<MantleTask> {
	return startStudioOperation("export/lora", request);
}

export async function startEvaluate(request: EvaluateRequest): Promise<MantleTask> {
	return startStudioOperation("evaluate", request);
}

export async function startStudioPipeline(request: StudioPipelineRequest): Promise<MantleTask> {
	const res = await fetch("/api/mantle/studio/pipelines", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(request),
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return await res.json();
}

export async function registerStudioModel(request: RegisterStudioModelRequest): Promise<MantleTask> {
	return startStudioOperation("register", request);
}

export async function listStudioPipelineTemplates(): Promise<StudioPipelineTemplate[]> {
	const res = await fetch("/api/mantle/studio/pipeline-templates");
	if (!res.ok) throw new Error(`HTTP ${res.status}`);
	return await res.json();
}

export async function saveStudioPipelineTemplate(template: Omit<StudioPipelineTemplate, "createdAt" | "updatedAt">): Promise<StudioPipelineTemplate> {
	const res = await fetch("/api/mantle/studio/pipeline-templates", {
		method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(template),
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return await res.json();
}

export async function deleteStudioPipelineTemplate(id: string): Promise<void> {
	const res = await fetch(`/api/mantle/studio/pipeline-templates/${encodeURIComponent(id)}`, { method: "DELETE" });
	if (!res.ok) throw new Error(`HTTP ${res.status}`);
}

export async function listStudioArtifacts(kind = ""): Promise<StudioCatalogArtifact[]> {
	const query = kind ? `?kind=${encodeURIComponent(kind)}` : "";
	const res = await fetch(`/api/mantle/studio/artifacts${query}`);
	if (!res.ok) throw new Error(`HTTP ${res.status}`);
	return await res.json();
}

export async function getStudioLineage(path: string): Promise<StudioLineageEdge[]> {
	const res = await fetch(`/api/mantle/studio/lineage?path=${encodeURIComponent(path)}`);
	if (!res.ok) throw new Error(`HTTP ${res.status}`);
	return await res.json();
}

export async function saveStudioArtifactAnnotation(path: string, tags: string[], notes: string): Promise<void> {
	const res = await fetch("/api/mantle/studio/artifacts/annotation", {
		method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path, tags, notes }),
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
}

export async function verifyStudioArtifact(path: string): Promise<MantleTask> {
	return startStudioOperation("artifacts/verify", { path } as unknown as RegisterStudioModelRequest);
}

export async function cleanupStudioArtifact(path: string): Promise<MantleTask> {
	return startStudioOperation("artifacts/cleanup", { path, confirm: true } as unknown as RegisterStudioModelRequest);
}

export async function listStudioEvaluations(model = ""): Promise<StudioEvaluation[]> {
	const query = model ? `?model=${encodeURIComponent(model)}` : "";
	const res = await fetch(`/api/mantle/studio/evaluations${query}`);
	if (!res.ok) throw new Error(`HTTP ${res.status}`);
	return await res.json();
}

export async function verifyStudioArtifacts(paths: string[]): Promise<MantleTask> {
	return startStudioOperation("artifacts/verify-bulk", { paths } as unknown as RegisterStudioModelRequest);
}

export async function previewStudioRetention(policy: StudioRetentionPolicy): Promise<StudioRetentionPreview> {
	const res = await fetch("/api/mantle/studio/artifacts/retention/preview", {
		method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(policy),
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return await res.json();
}

export async function applyStudioRetention(policy: StudioRetentionPolicy, token: string): Promise<MantleTask> {
	const res = await fetch("/api/mantle/studio/artifacts/retention/apply", {
		method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ policy, token }),
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return await res.json();
}

async function startStudioOperation(operation: string, request: HashRequest | SplitRequest | MergeRequest | PruneRequest | TrainQLoRARequest | ExportLoRARequest | EvaluateRequest | RegisterStudioModelRequest): Promise<MantleTask> {
	const res = await fetch(`/api/mantle/studio/${operation}`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(request),
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return await res.json();
}

export async function cancelStudioJob(taskID: string): Promise<void> {
  const res = await fetch(`/api/mantle/studio/jobs/${encodeURIComponent(taskID)}`, { method: "DELETE" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
}

export async function getStudioScheduler(): Promise<StudioSchedulerStatus> {
  const res = await fetch("/api/mantle/studio/scheduler");
  if (!res.ok) throw new Error("HTTP " + res.status);
  return await res.json();
}

export function streamTaskProgress(taskID: string, onProgress: (data: Partial<MantleTask>) => void): () => void {
  const es = new EventSource(`/api/mantle/tasks/${encodeURIComponent(taskID)}/stream`);
  es.onmessage = (event) => {
    try {
      onProgress(JSON.parse(event.data));
    } catch { /* ignore malformed progress lines */ }
  };
  es.onerror = () => es.close();
  return () => es.close();
}

// --- SSE progress stream ---

export function streamDownloadProgress(taskID: string, onProgress: (data: any) => void): () => void {
  const es = new EventSource(`/api/mantle/models/download/${taskID}/stream`);
  es.onmessage = (e) => {
    try {
      onProgress(JSON.parse(e.data));
    } catch { /* skip */ }
  };
  es.onerror = () => { es.close(); };
  return () => es.close();
}

export function streamBuildProgress(taskID: string, onProgress: (data: any) => void): () => void {
  const es = new EventSource(`/api/mantle/backends/build/${taskID}/stream`);
  es.onmessage = (e) => {
    try {
      onProgress(JSON.parse(e.data));
    } catch { /* skip */ }
  };
  es.onerror = () => { es.close(); };
  return () => es.close();
}
