# Mantle — llama.cpp Management Proxy

## Project Goals

Mantle is a daemon/proxy for llama.cpp built on top of [llama-swap](https://github.com/mostlygeek/llama-swap). It extends the existing proxy with four management layers:

### 1. Model Management — done
- Browse HuggingFace for GGUF models
- Download with progress monitoring and resume support (single files or a whole repo)
- Track download tasks (progress, cancel)
- List and delete downloaded models, estimate memory footprint

### 2. Configuration Management — done
- View and edit the current YAML config in-browser (raw editor, and per-model/per-group editors)
- Edit and save with YAML validation
- Hot-reload on save (no restart required)

### 3. Backend Build Management — done
- Trigger Docker builds of any llama.cpp fork from the UI, with a typed schema per backend
- Monitor build progress in real-time via SSE
- Cancel running builds
- List, update, and delete compiled backends

### 4. Llama Studio — done
A local GGUF lifecycle workspace: download/inspect models, manage datasets, quantize,
merge, prune, fine-tune with QLoRA, distill, export LoRA adapters, run benchmark/perplexity
evaluations, build multi-step pipelines, organize work into projects, track artifacts with
lineage/retention, and promote a result to serving. Scoped for one user on a workstation or
trusted LAN, not a multi-tenant service. See [docs/llama-studio.md](docs/llama-studio.md)
for the architecture/safety contract and
[docs/llama-studio-getting-started.md](docs/llama-studio-getting-started.md) for onboarding.

Includes an **Intelligence** suite (`/studio/intelligence/*`) that proxies to a companion
service (the *Measure Model Intelligence* repository) for running and reviewing broader
model benchmarks. Mantle owns the pages and a thin API proxy; the companion owns benchmark
execution and result storage. See [docs/intelligence.md](docs/intelligence.md).

### Future Goals (not yet implemented)
- Load balancing with peer co-workers (sticky sessions, failover)
- Multi-engine version management
- Daemon install (systemd/launchd)

Per [docs/llama-studio.md](docs/llama-studio.md)'s current boundary, Studio itself is
feature-complete for its single-user/local-LAN scope — further work there should prioritize
real-container validation, correctness fixes, and usability feedback over new tools.

---

## Architecture

```
llama-swap core (Go proxy)  ←  Mantle extensions
     │                              │
     ├── Model lifecycle             ├── HF browse/download (mantle/)
     ├── Config loading              ├── Config editor + hot-reload
     ├── SSE event streaming         ├── Docker build orchestration
     ├── Performance monitoring      ├── Task tracking (progress/cancel)
     └── Svelte 5 UI                 ├── Llama Studio (quantize/merge/prune/train/
                                      │   distill/evaluate/pipelines/projects/artifacts)
                                      ├── Intelligence reverse proxy (companion service)
                                      └── New UI routes
```

### Tech Stack
- **Backend:** Go (extends llama-swap's existing packages)
- **Frontend:** Svelte 5 + TypeScript + Vite (same as existing UI)
- **Events:** Typed in-process event bus → SSE to browser
- **Builds:** Docker (reuses existing `build-llamacpp.sh`)

---

## Package Layout

```
llama-swap.go                 — entry point (modified: sets runtime paths)
internal/
  config/
    config.go                 — added ConfigPath, ModelsDir, BackendsDir, BuildScript fields
  shared/
    events.go                 — added BackendBuildProgressEvent, ModelDownloadProgressEvent, Studio events
  server/
    server.go                 — integrated mantle.Handler (init + routes)
  mantle/                     — all Mantle management logic
    mantle.go                 — TaskManager, Task, HF search API
    download.go               — async GGUF download with resume + progress
    build.go                  — async Docker build via build-llamacpp.sh
    backend_schema.go         — typed per-backend build option schema
    config.go, config_edit.go — local models/backends listing + deletion; config model/group editing
    estimate.go               — memory footprint estimation for local models
    gguf.go                   — GGUF metadata inspection
    intelligence.go           — reverse proxy to the Intelligence companion service
    api.go                    — HTTP handlers + SSE streaming (Model/Config/Backend/Studio/Task endpoints)
    studio.go                 — Studio operation dispatch + job execution
    studio_catalog.go         — recipe/operation catalog
    studio_dataset.go         — dataset import, HF dataset search/download, preview
    studio_distill.go         — distillation jobs
    studio_grpo.go            — GRPO training jobs
    studio_evaluations.go     — benchmark/perplexity evaluation runs
    studio_pipeline.go        — multi-step pipelines, templates, fan-out, gates, retry
    studio_projects.go        — project grouping of models/datasets/adapters/outputs
    studio_register.go        — promote a Studio artifact to serving
    studio_resources*.go      — resource picker catalog
    studio_scheduler.go       — job queue/scheduler + hardware advisor
    studio_cleanup.go         — startup cleanup of abandoned staging files
    studio_operations.go, studio_utilities.go — shared operation plumbing, misc utilities
ui-svelte/
  src/
    lib/
      types.ts                — MantleTask, HFModel, LocalModel, BackendEntry, Studio types, etc.
      mantleApi.ts             — typed API client for /api/mantle/ endpoints
      intelligenceApi.ts       — typed API client for /api/mantle/studio/intelligence/ endpoints
    routes/
      ModelManager.svelte, ModelDetail.svelte, ModelsDash.svelte, ModelConfigEditor.svelte
      ConfigEditor.svelte      — YAML editor with save + hot-reload
      BackendManager.svelte    — build trigger + progress + backend list
      Studio.svelte            — Studio home / recipe runner
      StudioJobs.svelte, StudioPipelines.svelte, StudioArtifacts.svelte, StudioDatasets.svelte
      StudioEvaluations.svelte, StudioProjects.svelte
      StudioIntelligenceRun.svelte, StudioIntelligenceResults.svelte — Intelligence run + results (filters, sorting, retry)
      Activity.svelte, LogViewer.svelte, Performance.svelte, Settings.svelte, Playground.svelte
    App.svelte                 — route table (see New UI Routes below)
    components/
      AppSidebar.svelte        — nav links for all sections above
```

---

## API Reference (`/api/mantle/`)

### HF Model Browsing

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/mantle/models/search?q=<query>&limit=<n>` | Search HF for GGUF models |
| GET | `/api/mantle/models/files?model=<id>` | List GGUF files in a HF repo |

### Download Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/mantle/models/download` | Start download `{modelID, filename}` → task |
| POST | `/api/mantle/models/download/repo` | Start a whole-repo download → task |
| DELETE | `/api/mantle/models/download/{id}` | Cancel download |
| GET | `/api/mantle/models/download/{id}/stream` | SSE progress stream |

### Local Models

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/mantle/models/local` | List downloaded GGUF files |
| DELETE | `/api/mantle/models/local/{name...}` | Delete a model file |
| GET | `/api/mantle/models/estimates` | Memory footprint estimates for local models |

### Config

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/mantle/config` | Get current config YAML |
| PUT | `/api/mantle/config` | Update config (validates YAML, writes, hot-reloads) |
| GET / PUT / DELETE | `/api/mantle/config/models/{name}` | List / upsert / delete a single model entry |
| GET / PUT / DELETE | `/api/mantle/config/groups/{name}` | List / upsert / delete a group entry |
| POST | `/api/mantle/cmd/tokenize` | Tokenize a `cmd` string for editing |
| POST | `/api/mantle/cmd/build` | Build a `cmd` string from tokens |

### Backend Builds

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/mantle/backends/build` | Start build `{repo, branch}` → task |
| DELETE | `/api/mantle/backends/build/{id}` | Cancel build |
| GET | `/api/mantle/backends/build/{id}/stream` | SSE progress stream |
| GET | `/api/mantle/backends` | List compiled backends |
| GET | `/api/mantle/backends/{name}/schema` | Typed build-option schema for a backend |
| POST | `/api/mantle/backends/{name}/update` | Rebuild an existing backend |
| DELETE | `/api/mantle/backends/{name...}` | Delete a backend |

### Task Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/mantle/tasks` | List all tasks |
| GET | `/api/mantle/tasks/{id}` | Get single task status |
| GET | `/api/mantle/tasks/{id}/stream` | SSE progress stream for any task |

### Llama Studio (`/api/mantle/studio/`)

Datasets (`datasets`, `datasets/inspect`, `datasets/preview`, `datasets/import`,
`datasets/hub/search`, `datasets/hub/files`, `datasets/hub/download`), model inspection
(`models/inspect`), operations (`quantize`, `hash`, `split`, `merge`, `prune`,
`train/qlora`, `distill`, `export/lora`, `evaluate`, `utility`), pipelines
(`pipelines`, `pipelines/{id}/retry`, `pipeline-templates`), promotion (`register`),
artifacts (`artifacts`, `artifacts/annotation`, `artifacts/verify[-bulk]`,
`artifacts/cleanup`, `artifacts/retention/preview|apply`, `lineage`), evaluations
(`evaluations`), job control (`jobs/{id}`, `scheduler`, `preflight`), and
`resources`/`projects`. Each mutating call becomes a `Task`, reported the same way as
downloads and builds. Full request/response shapes and the operation contract are in
[docs/llama-studio.md](docs/llama-studio.md).

### Intelligence (`/api/mantle/studio/intelligence/`)

A reverse proxy to the companion service at `LLAMA_INTELLIGENCE_URL` (`config`, `results`,
`run`, `run/status`, `run/stream`, `run/stop`, `runs`, `runs/{id}`, `download/status`,
`download/polyglot`, `download/swebench`, `suite`, `human-scores`, `health`). Returns
`503` until `LLAMA_INTELLIGENCE_URL` is set. See [docs/intelligence.md](docs/intelligence.md).

---

## How It Works

### Task System
Long-running operations (downloads, builds) create a `Task` object stored in `TaskManager`. Each task has:
- A cancel `context.Context` (for cleanup)
- A `cancelCh` (closed on cancel, polled by goroutines)
- Progress state updated atomically with mutex

Progress events are emitted through the existing typed event bus (`internal/event`) using new event types:
- `BackendBuildProgressEvent` (ID 0x09)
- `ModelDownloadProgressEvent` (ID 0x0A)

### SSE Streaming
Each task has a dedicated SSE endpoint (`/api/mantle/models/download/{id}/stream` or `/api/mantle/backends/build/{id}/stream`) that:
1. Sends the initial task state
2. Subscribes to the typed event bus for that task type
3. Forwards events as `data:` lines
4. Watches `task.Done()` for the final state
5. Cleans up subscriptions when the client disconnects

### Config Hot-Reload
`PUT /api/mantle/config`:
1. Reads the raw YAML body
2. Parses via `config.LoadConfigFromReader` (validates all macros, aliases, ports)
3. Preserves runtime paths (ConfigPath, ModelsDir, BackendsDir, BuildScript)
4. Writes to disk
5. Emits `ConfigFileChangedEvent` (start/end) — triggers UI refresh via existing SSE stream

### HuggingFace Integration
- Searches via `huggingface.co/api/models` endpoint (sorted by downloads)
- Filters results for `.gguf` file presence
- Lists individual GGUF files via the model detail API
- Downloads use `Range` header for resume support

### Backend Builds
- Runs the existing `build-llamacpp.sh` inside Docker
- Output goes to `backends/build-{taskID}/` subdirectories
- Progress parsed from Docker step markers `[1/12]` and CMake `[42%]`
- Cancellation kills the Docker process and removes the output directory

---

## New UI Routes

| Route | Component | Purpose |
|-------|-----------|---------|
| `/models/hub` | `ModelManager.svelte` | HF search, file pick, download with progress |
| `/models/:id` | `ModelDetail.svelte` | Local model details |
| `/backends` | `BackendManager.svelte` | Build trigger, live progress, backend list |
| `/config` | `ConfigEditor.svelte` | YAML editor with Ctrl+S save + reload |
| `/config/models` | `ModelConfigEditor.svelte` | Per-model/per-group config editor |
| `/studio` | `Studio.svelte` | Studio home / recipe & pipeline runner |
| `/studio/jobs` | `StudioJobs.svelte` | Running/queued/finished jobs, cancel, retry |
| `/studio/pipelines` | `StudioPipelines.svelte` | Multi-step pipeline builder, templates, fan-out, gates |
| `/studio/artifacts` | `StudioArtifacts.svelte` | Artifact list, lineage, verification, retention |
| `/studio/datasets` | `StudioDatasets.svelte` | Dataset import, HF dataset search/download, preview |
| `/studio/evaluations` | `StudioEvaluations.svelte` | Benchmark/perplexity evaluation comparisons |
| `/studio/projects` | `StudioProjects.svelte` | Project grouping and active-project selection |
| `/studio/intelligence/run` | `StudioIntelligenceRun.svelte` | Trigger a companion Intelligence benchmark run |
| `/studio/intelligence/results` | `StudioIntelligenceResults.svelte` | Browse results, with filters/sorting and retry-failed |

Existing routes (`/models`, `/logs`, `/activity`, `/performance`, playground) are unchanged.

---

## Things to Know

### Compilation
- `internal/mantle/` only depends on `internal/config`, `internal/event`, `internal/shared` — no new external Go deps
- The `go.mod` module path is `github.com/mostlygeek/llama-swap` (not changed, we're in the same module)

### Runtime Paths
Paths are set in `llama-swap.go` right after `config.LoadConfig()`:
- **ConfigPath** — the config file path from `-config` flag
- **ModelsDir** — defaults to `{configDir}/models/`
- **BackendsDir** — defaults to `{configDir}/backends/`
- **BuildScript** — defaults to `{configDir}/../llama-swap-additions/backends/build-llamacpp.sh`

These are re-applied during hot-reload so they survive config file changes.

### Event IDs
Mantle-added event IDs in `internal/shared/events.go`:
- 0x09 = BackendBuildProgressEvent
- 0x0A = ModelDownloadProgressEvent

Llama Studio jobs don't add new event bus types — they report progress through the
generic `Task`/`GET /api/mantle/tasks/{id}/stream` SSE mechanism instead.

### Task States
Tasks transition: `running` → `completed` | `failed` | `cancelled`
Cancelled tasks clean up partial files (`.part` or build output directory).

---

## Current Status

- [x] Backend: Task manager with cancel support
- [x] Backend: HF model search + file listing, single-file and whole-repo download
- [x] Backend: Docker build orchestration + SSE progress, per-backend build schema
- [x] Backend: Config GET/PUT with validation + hot-reload, per-model/per-group editing
- [x] Backend: Local models/backends listing + deletion, memory estimates, GGUF inspection
- [x] Backend: Llama Studio — datasets, quantize/merge/prune/train/distill/export/evaluate,
      pipelines with fan-out and gates, projects, artifacts with lineage/retention, scheduler
- [x] Backend: Intelligence reverse proxy to the companion service
- [x] Frontend: HF browser + download UI, config editors, build trigger/progress UI
- [x] Frontend: Studio pages (jobs, pipelines, artifacts, datasets, evaluations, projects)
- [x] Frontend: Intelligence run + results pages (filters, sorting, retry failed items)
- [x] Tests: 100+ Go tests under `internal/mantle/`

## What a Newcomer Should Do Next

Per the Studio product's [current boundary](docs/llama-studio.md), the feature set is
considered complete for its single-user/local-LAN scope. Priorities are:

1. **Read the docs first** — [docs/llama-studio-getting-started.md](docs/llama-studio-getting-started.md)
   for Studio onboarding, [docs/intelligence.md](docs/intelligence.md) for the companion
   service, [docs/llama-studio.md](docs/llama-studio.md) for the operation contract and
   safety rules.
2. **Real-container validation** — work through the
   [testing-phase checklist](docs/llama-studio-getting-started.md#testing-phase-checklist)
   against actual GPU hardware/backends; this repo's test suite runs without one.
3. **Correctness and usability fixes** over new tools — see open issues/recent commits for
   the current focus area (e.g. Intelligence results filtering/retry, Docker build
   concurrency) before adding scope.
4. **Keep this file current** — when a package or route is added under `internal/mantle/`
   or `ui-svelte/src/routes/`, update the Package Layout, API Reference, and New UI Routes
   sections above in the same change.
