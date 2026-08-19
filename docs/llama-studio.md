# Llama Studio

The executable-by-executable coverage audit and implementation sequence are maintained
in [Llama Studio tool capability plan](llama-studio-tool-capability-plan.md).

Llama Studio extends llama-swap from model serving into a GGUF lifecycle
workspace. It treats command-line tools as typed operations rather than exposing
arbitrary process execution.

For installation and the first end-to-end workflow, see
[Getting started with Llama Studio](llama-studio-getting-started.md).

## Product areas

- **Models**: local model inventory, GGUF metadata, variants, hashes, and lineage.
- **Studio**: guided model transformations and training workflows.
- **Jobs**: queued, running, completed, failed, and cancelled operations.
- **Artifacts**: generated models, adapters, profiles, caches, reports, and datasets.
- **Datasets**: safely import, discover, validate, and preview local training data or
  download selected files from public Hugging Face dataset repositories.
- **Playground**: validate generated models using the existing inference interfaces.
- **Serving**: register and launch generated model variants with existing backends.

## Operation contract

Every Studio operation has:

1. A stable operation identifier and versioned typed request.
2. An approved executable and argument builder; requests never contain commands.
3. Model-root-relative input and output paths with symlink-aware containment checks.
4. Preflight validation, disk estimates, resource requirements, and collision checks.
5. A cancellable job with bounded logs, progress, timestamps, and an exit code.
6. Declared output artifacts and provenance linking them to their input artifacts.
7. An operation-specific success policy because llama.cpp tools do not share exit conventions.

## Delivery phases

### 1. Foundation and quantization

- GGUF inspection and local model selection.
- Quantization and requantization planning and execution.
- Importance matrix input, dry-run estimates, progress, logs, and cancellation.
- Persistent jobs, artifacts, lineage, and restart recovery.

### 2. Packaging and verification

- GGUF hashing and verification.
- Split and merge-shards workflows.
- Artifact registration and optional serving configuration generation.

### 3. Model merging

- TIES merges with density and bounded worker memory.
- Evo merges with calibration datasets, population controls, and GPU selection.
- Compatibility preflight and post-merge validation.

### 4. Pruning

- Analyze a model and dataset into an importance cache.
- Generate and inspect ratio profiles.
- Hard-prune to a new artifact.
- Validate perplexity change against an explicit threshold.

### 5. Training and adapters

- Dataset inventory, validation, and JSONL preview.
- QLoRA/SFT training with checkpoints and resume.
- Critical-token SFT as an advanced mode.
- GRPO as a separate IPC-backed advanced workflow.
- LoRA export/merge and resulting-model validation.

### 6. Evaluation and advanced tools

- Benchmarks, perplexity, fit parameters, embeddings, retrieval, and lookup caches.
- Comparable evaluation reports attached to model variants.
- Advanced diagnostics kept separate from normal workflows.

### 7. Pipelines and hardening

- Composable train, merge, prune, quantize, verify, register, and test pipelines.
- CPU, RAM, GPU, VRAM, and disk admission controls.
- Job concurrency and scheduling policies.
- Audit history, retention, cleanup, and orphan recovery.
- Rootless operation and adversarial path/process tests.

Pipeline requests contain only typed Studio operation requests. A step may set
`usePrevious` to inject the preceding generated model into its operation-specific
input field. Pipeline parents retain child job IDs, aggregate artifacts, stop on
the first failed step, and propagate cancellation to the active child.
Steps may also fan out into as many as eight typed request variants. Evaluation
steps can gate continuation on minimum or maximum stored metrics, and failed
pipelines can be retried from the failed child through Jobs.

Pipeline templates are stored in SQLite and managed from the Pipeline Builder.
Templates can also be imported and exported as versioned JSON documents. The
builder presents operation-specific fields and retains an advanced typed-request
JSON editor for less common fork options.
The `register` terminal operation adds a generated GGUF to the existing `models`
configuration with a validated `llama-server` command, preserves YAML comments,
rejects accidental model-ID replacement, and triggers the normal hot reload path.

The artifact catalog deduplicates pipeline roll-ups in favor of the operation that
actually produced each path, reports missing files without discarding history, and
shows the connected upstream and downstream lineage for an artifact. Serving
registration is tracked independently, so it protects a model without replacing its
producing operation in the catalog.

Artifact annotations retain tags and notes independently from job snapshots.
Verification streams SHA-256 calculation with cancellation, validates GGUF structure,
and stores both successful and failed results. Verification can run over the current
catalog selection as a parent job with individually visible child jobs. Explicit cleanup
removes cataloged regular files only and intentionally retains provenance records.
Retention policies filter by artifact age and type, exclude tagged artifacts by default,
and always exclude models registered for serving. Applying a policy requires the token
from an exact preview; changes to the candidate files invalidate that token.

Completed benchmarks and perplexity runs are stored as comparable evaluation records.
Benchmark records normalize prompt and generation throughput while retaining the raw
llama-bench result rows and exact operation parameters. An evaluation may select a prior
job as its baseline and set a maximum regression percentage. Throughput regressions use
generation speed (falling back to prompt speed), while perplexity regressions account for
lower values being better; exceeding the threshold fails the evaluation job.
The evaluation workspace compares same-mode baseline and candidate jobs, normalizes
the direction of improvement, and can promote the candidate into serving.

Studio exposes one resource catalog across local models, generated artifacts,
datasets, adapters, and training checkpoints. Forms use this catalog for searchable
selection and display resource type, size, and producing operation. QLoRA checkpoints
can be selected for resume, while persisted structured-loss logs reconstruct the
training-loss chart whenever the fork emits them.

Outcome recipes provide editable starting points for quantize-and-evaluate, QLoRA,
merge, prune, and comparison workflows. **Recipes & pipelines** is the power-user
editor: a one-step workflow is a custom recipe, while additional steps create a
pipeline. Common arguments have form controls and the advanced request JSON starts
with every argument supported by the selected Studio operation API. Recipes can be
saved, imported, exported, fanned out into variants, and guarded by evaluation gates.

The utility recipe exposes managed artifact workflows for importance matrices,
control vectors, lookup caches, benchmark results, and full-model fine-tuning. It
also provides report jobs for tokenization, template analysis, hardware fitting, and
lookup statistics. Inputs use the resource catalog, outputs are staged and published
atomically, and the UI intentionally provides typed fields instead of arbitrary argv.

### GRPO reward providers

GRPO training is an interactive IPC workflow rather than ordinary dataset SFT.
Studio supplies prompts from a JSONL dataset, collects the trainer's grouped
generations, obtains one reward per generation, normalizes the group-relative
advantages, and returns them to `llama-finetune-qlora`. Every run publishes a
`.rollouts.jsonl` artifact containing prompts, generations, raw rewards, normalized
advantages, and optional provider details.

The built-in provider supports exact text, numeric tolerance, regular-expression,
and valid-JSON verification. The script provider starts a persistent Python worker
and exchanges one JSON object per line; see
[`docs/examples/grpo-reward-worker.py`](examples/grpo-reward-worker.py). Selecting a
script intentionally executes trusted local code as the same user as Llama Studio.
The HTTP provider sends the identical request object to an HTTP(S) endpoint. Reward
providers must return `{"rewards":[...]}` with one finite number per generation and
may include a `details` value. Provider errors and timeouts fail the job before that
optimizer step is applied.

Projects are durable named workspaces and collections of catalog resource paths. The
active project is selected in the sidebar; new jobs, generated artifacts, evaluations,
and saved recipes inherit that project, and their pages are scoped accordingly.
Projects organize resources without moving files or changing provenance. Deleting a
project never deletes its resources or job history.

The dataset manager catalogs JSONL, JSON, text, CSV, and Parquet files under the
model root's `datasets/` directory. Browser imports use a temporary file and atomic
publication, reject overwrites and path escapes, and are limited to 1 GiB per request.
Hugging Face downloads run as cancellable Studio I/O jobs and become dataset artifacts.
JSONL previews are bounded to 50 records and validate the `messages`, `text`, and
`prompt`/`response` training shapes. Other formats are cataloged and downloadable;
structured CSV, JSON-array, and Parquet previews remain future format adapters.

## Persistence model

Studio persistence is centered on three related records:

- `studio_jobs`: operation, state, parameters, progress, exit status, and timestamps.
- `studio_artifacts`: root-relative path, type, size, hash, metadata, and creation time.
- `studio_lineage`: input/output artifact relationships and the producing job.

Additional tables retain annotations, evaluations, pipeline templates, projects,
and project-to-resource membership without duplicating artifact files.

Jobs found in a running state after process restart are marked interrupted. Operations
that support checkpoints can offer resume; transformations with partial outputs must
clean or quarantine those outputs before retrying.

## Hardware advisor

Studio preflight combines the selected model and dataset sizes with live RAM, VRAM,
GPU, CPU, and output-filesystem telemetry. It estimates output size and peak memory,
reports whether the operation is expected to fit, and supplies settings that can be
applied to the current form. Current recommendations cover quantization type and
threads, QLoRA rank/batch/checkpointing, and evaluation or serving offload/context.
These are conservative planning estimates rather than a substitute for the final
operation admission checks.

## Job scheduler configuration

- `LLAMA_STUDIO_MAX_JOBS` controls total concurrent Studio jobs (default `2`).
- `LLAMA_STUDIO_MAX_HEAVY_JOBS` limits concurrent training, merge, prune,
  quantization, export, and evaluation jobs (default `1`).
- `LLAMA_STUDIO_DISK_RESERVE_GB` keeps free space outside an operation's estimated
  output requirement (default `1`).
- `LLAMA_STUDIO_MIN_FREE_RAM_GB` delays heavy jobs until the host has the configured
  free RAM reserve (default `2`).
- `LLAMA_STUDIO_MIN_FREE_VRAM_GB` optionally delays heavy jobs until GPUs have the
  configured aggregate free VRAM (default `0`, disabled).
- `LLAMA_STUDIO_STAGING_MAX_AGE_HOURS` controls cleanup of abandoned hidden staging
  outputs on startup (default `24`; live task IDs are always excluded).

Invalid or non-positive concurrency values use their defaults. A disk reserve of
`0` is allowed. Light and I/O jobs may pass a heavy job that is waiting for the
heavy-job slot, so available global capacity is not left idle.

## Safety rules

- Never accept executable names, shell fragments, or unrestricted arguments from clients.
- Never overwrite an existing model or follow an output path outside configured roots.
- Write transformations to temporary outputs and publish them atomically after validation.
- Require explicit confirmation for requantization, hard pruning, split deletion, and cleanup.
- Treat datasets and calibration files as untrusted input and bound all parsers and logs.
