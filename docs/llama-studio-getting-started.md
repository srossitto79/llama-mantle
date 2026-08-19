# Getting started with Llama Studio

Llama Studio is a local GGUF workspace built into llama-swap. It is designed for
one user on their own workstation, or for a small group on a trusted LAN. It is not
a multi-tenant training service.

Studio can download and inspect models, manage datasets, quantize, merge, prune,
fine-tune with QLoRA, export LoRA adapters, benchmark variants, build pipelines,
organize projects, and register a result for serving.

## Before you start

You need:

- Docker with an NVIDIA CUDA runtime, or a Vulkan-capable host; or a native build
  with the required llama.cpp tools available on `PATH`.
- A writable model directory with enough space for source models, temporary output,
  and generated variants.
- A file-backed SQLite store if job history and Studio metadata must survive restarts.
- A trusted local machine or LAN. Do not expose Studio directly to the public internet.

The unified image defaults to the power-user `all` build profile. It includes every
supported runtime and Studio executable while intentionally excluding examples, PoCs,
debug-only desktop applications, documentation generators, and deprecated wrappers.

## Build the unified image

From the repository root:

```shell
# NVIDIA
./docker/build-image.sh --cuda

# AMD or another Vulkan-capable device
./docker/build-image.sh --vulkan
```

`BUILD_PROFILE=all` is the default. Set it explicitly when you want a reproducible
build command:

```shell
BUILD_PROFILE=all DOCKER_IMAGE_TAG=llama-studio:cuda ./docker/build-image.sh --cuda
```

The narrower profiles are `runtime`, `studio-core`, `studio-training`,
`studio-evaluation`, and `studio-advanced`. They are useful for development images;
the normal Llama Studio image should use `all`.

## Run locally

Create a host model directory and copy `docker/config.example.yaml` to a writable
configuration file. The image stores models, datasets, generated artifacts, backend
builds, and `studio.sqlite` in the mounted `/models` volume.

Both the model directory and configuration file must be writable by the container's
configured `RUN_UID`. Studio writes its database and outputs under `/models`, and
model promotion can update the serving configuration.

```shell
docker run --rm -it --gpus all \
  -p 127.0.0.1:9292:8080 \
  -v /path/to/models:/models \
  -v /path/to/config.yaml:/etc/llama-swap/config/config.yaml \
  llama-swap:unified-cuda
```

Open `http://127.0.0.1:9292/ui/`.

For Vulkan, use the Vulkan image and pass the devices required by your host. Device
arguments vary by operating system and container runtime.

### Existing custom configuration

Add a file-backed store to preserve Studio state:

```yaml
store:
  path: /models/studio.sqlite
```

The SQLite file contains job history, artifact records, annotations, evaluations,
pipeline templates, and projects. Model and dataset contents remain ordinary files
under `/models`; backing up the database without the files does not back up artifacts.

## First successful workflow

The quickest useful path is a quantization comparison:

1. Open **Model Hub**, find a GGUF repository, and download a model file.
2. Open **Llama Studio** and choose **Fit a model to my hardware**.
3. Select the downloaded model from the resource picker.
4. Select **Check fit** in the Hardware advisor.
5. Review and optionally apply the recommended quantization and thread count.
6. Run the pipeline. Follow progress and cancellation from **Studio Jobs**.
7. Open **Evaluations** and compare the generated variant with another benchmark.
8. Promote the preferred candidate to serving, then test it in **Playground**.

Hardware recommendations are conservative estimates. The operation's final admission
checks remain authoritative, and real memory use varies with architecture, context,
batch size, backend, and driver.

## Working with datasets

Open **Datasets** to:

- Import JSONL, JSON, text, CSV, or Parquet files from the browser.
- Search public Hugging Face dataset repositories and download selected files.
- Preview and validate bounded JSONL samples.
- Select discovered datasets directly in training, pruning, merge, and evaluation forms.

JSONL training records currently recognize these shapes:

```json
{"messages":[{"role":"user","content":"Question"},{"role":"assistant","content":"Answer"}]}
{"prompt":"Question","response":"Answer"}
{"text":"Complete training text"}
```

JSON-array, CSV, and Parquet files are cataloged and downloadable, but their structured
preview adapters are still deferred.

Treat downloaded datasets as untrusted. Inspect their contents and licensing before
training, and begin with a small sample before committing substantial compute time.

## Fine-tuning and checkpoints

1. Choose the **Fine-tune with QLoRA** recipe.
2. Select a base GGUF and a validated JSONL dataset.
3. Run Hardware advisor and apply its initial rank, batch, and checkpoint suggestions.
4. Choose a checkpoint interval and start training.
5. Follow loss in the job view. The chart is reconstructed from structured loss lines
   emitted by the fork.
6. If training is interrupted, select a checkpoint or use **Resume latest checkpoint**.
7. Export the adapter, evaluate the resulting model, and promote it only after review.

Checkpoint compatibility is controlled by the bundled llama.cpp fork. Resume with the
same base model and compatible training settings.

## Pipelines, evaluations, and projects

- **Pipelines** compose typed operations, save/import/export templates, fan out up to
  eight variants, apply evaluation metric gates, and retry failed work from Jobs.
- **Evaluations** compare benchmark or perplexity runs. Positive displayed deltas mean
  improvement, including perplexity where a lower value is better.
- **Projects** group references to models, datasets, adapters, checkpoints, and outputs.
  Projects do not move files; deleting a project never deletes its resources.
- **Artifacts** retain provenance, verification results, tags, notes, and lineage.
  Retention previews exclude serving registrations and tagged artifacts by default.

## Storage, interruption, and cleanup

- Transformations publish outputs atomically and do not overwrite existing files.
- Interrupted running or queued jobs are marked failed after restart.
- Training checkpoints may be reusable; partially staged transformation outputs are not.
- The startup cleanup removes abandoned staging files older than the configured limit.
- Artifact cleanup deletes the file but intentionally retains job and lineage history.
- Keep free disk above both the estimated output and `LLAMA_STUDIO_DISK_RESERVE_GB`.

Back up these together:

1. The complete `/models` volume.
2. The active configuration file.
3. Any Hugging Face credentials or reverse-proxy configuration kept outside Studio.

Do not copy a live SQLite file independently while it is being written. Stop the
container or use a SQLite-aware backup method.

## Local and LAN safety boundary

For a single machine, publish the container only on loopback as shown above.

For trusted LAN access, bind the port to the intended LAN interface and restrict it
with the host firewall. Anyone who can reach Studio should be considered an operator:
Studio can start compute-intensive jobs, change serving configuration, download large
files, and remove cataloged artifacts.

Llama Studio does not currently provide:

- Per-user accounts, roles, projects, or filesystem isolation.
- Per-user compute and storage quotas.
- Protection suitable for mutually untrusted users.
- A hardened public-internet deployment boundary.

llama-swap API keys can protect compatible API traffic, but they are not a substitute
for multi-user Studio authorization. If access crosses a trusted LAN, place the service
behind an authenticated TLS reverse proxy or VPN and keep firewall restrictions in place.

## Testing-phase checklist

Before treating a new image or fork revision as stable, test with small disposable
models and datasets:

- Inspect and verify a GGUF artifact.
- Quantize to Q4_K_M and confirm the output opens in Playground.
- Cancel one queued job and one running job.
- Split and merge a small GGUF.
- Run benchmark and perplexity evaluations and compare them.
- Train a short QLoRA run, create a checkpoint, and resume it.
- Export a LoRA and verify the resulting GGUF.
- Run one prune and one model-merge workflow from the fork.
- Exercise pipeline fan-out, a passing gate, a failing gate, and retry.
- Restart the container and confirm jobs, artifacts, evaluations, templates, and projects remain.
- Preview retention, mutate a candidate, and confirm the stale preview is rejected.
- Confirm registered models remain excluded from retention.
- Repeat on each supported backend and GPU family intended for release.

Record the image tag, `/versions.txt`, GPU/driver versions, model, dataset, exact request,
resulting artifact hashes, and observed peak RAM/VRAM for every failure.

## Current boundary

The Studio product surface is feature-complete for the current single-user/local-LAN
scope. Development should now prioritize real-container validation, correctness fixes,
and usability feedback rather than adding more tools. Authentication, multi-tenancy,
scheduled shared infrastructure, and enterprise administration remain intentionally out
of scope unless the deployment model changes.

For the architectural and safety contract, see [Llama Studio](llama-studio.md). For all
llama-swap settings, see [Configuration](configuration.md).
