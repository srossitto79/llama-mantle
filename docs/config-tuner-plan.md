# llama.cpp config tuner — design brief

Brief for an agent starting cold. Read `AGENTS.md` first; its testing, formatting and commit rules apply.

## Status (2026-11-10)

- **Slice 1, analysis: running on the station.**
  - Code in `internal/mantle/tuner` and `internal/gguf`. The route is `POST /api/mantle/tuner/analyse`.
  - The UI is the Tune sheet on the Configuration page. Proposed entries are staged into the config draft
    and applied through the existing Review sheet.
- **Memory is projected, not estimated.** The first version sized the cache from a fixed compute-buffer
  formula. On Groot 280B (`deepseek41`) the real compute buffer at `-ub 4096` was 6.3 GB at 64K and
  18.9 GB at 256K against an estimate of 1.2 GB, and every staged entry ran out of memory.
  - The sizer (`size.go`) now starts each candidate's backend with `-lv 4`, reads llama.cpp's memory
    breakdown row (printed about 0.3 s after start, before weights load), and kills it.
  - `--moe-cache-mib` = CUDA free − 1024 (llama.cpp's `--fit-target`) − everything else, rounded down to
    256 MiB, then confirmed by a second projection. `-ncmoe` is stepped the same way.
  - CUDA reports 23300 MiB free on the idle 3090, not the 24576 total.
  - Analysis refuses to size while a model is resident (GPU use above 2 GiB).
  - Over-committing VRAM on WSL does not fail: the driver spills to shared system memory and prefill
    drops to about 150 tok/s. An entry that "works" can still be oversized.
- **Fixed after the first run:**
  - Input-layer tables (`token_embd`, `per_layer_token_embd`) stay in host memory. They were counted as
    GPU weights: 27.7 GiB on Swift Flash-Next, 188.8 GiB on Groot.
  - Backends are blocked when their binary does not know the model's architecture (the name followed by
    NUL, matched as a suffix because the linker merges `gemma4` into `peg-gemma4`). Only ggml-org has
    `deepseek41`; beellama-cuda and prism-ml have no `qwen4exp`.
- **Experts larger than RAM:** Groot 280B on ggml-org (138 GiB of experts, 110 GiB RAM) does not
  run with mmap either: 0 of 76 prompt tokens after 9 minutes with a warm page cache, because each
  batch touches nearly every expert. The planner skips such tiers ("Experts exceed RAM").
- **Backend:** `ggml-org` was rebuilt at `7d7f028` and includes #30966. `docker/build-llamacpp.sh` now
  ships `tools/moe-cache-tune.py` and `tools/SOURCE_COMMIT` with every build.
- **Slice 2, cache tune: built.** "Tune cache" on a tunable candidate runs the backend's own
  `tools/moe-cache-tune.py` as a task (`POST /api/mantle/tuner/cache-tune`, cancel with `DELETE
  .../cache-tune/{id}`, progress on `GET /api/mantle/tasks/{id}`).
  - The request names a candidate of the last analysis by id. The server keeps that analysis on the
    TaskManager, so the client never sends a command line to run, and staging (a config reload)
    does not lose it.
  - The script gets the candidate's command without `--moe-cache-mib`/`--moe-cache-cpu-share`, and
    `--max-mib` set to the size the sizer fitted. Its run directory is
    `backends/<name>/tune/<prefix>-<tier>k-<hash of the command>`, so a re-analysis reuses the runs.
  - Its recommendation replaces the cache size, adds `--moe-cache-cpu-share`, is re-projected and gets
    a new entry. The task's `parameters.candidate` carries the result.
  - Cancel sends SIGINT, so the script stops its llama-server; a kill follows after 2 minutes.
- **GPU hold.** Analysis and cache tunes take a hold (`mantle.GPUHold`, on the TaskManager so it
  outlives reloads). A cache tune also unloads every local model first. While held, requests to local
  models wait (the local router is wrapped, covering `/v1`, `/upstream` and preload); peers are not
  affected. A second job gets 409.
- **Next:** slice 3 is the Benchmarker and Recommender.
- **Unverified assumptions, to check on the station:**
  - A plain (non-MTP) draft needs only `--spec-draft-model`.
  - n-gram drafting adds no device memory (the sizer shares one projection between the two variants).
  - The MoE breakdown row covers a separate draft model's buffers.
- **Verified:** `--spec-type ngram-mod` is valid on ggml-org. That build also has `draft-dspark`, and
  Groot ships a `dspark-*.gguf` draft; the tuner does not offer it yet.

## Goal

A UI action that, given a model file and this machine, produces the llama-server entries with the best
**single-user end-to-end speed**, measured rather than guessed, and proposes them as a `config.yaml`
diff.

- **Objective:** one user, one request at a time. The score is the wall time of a reference workload
  (prompt processing plus generation), not decode tok/s alone and not aggregate throughput.
  `--parallel 1` is fixed.
- **Backends:** llama.cpp only. These are the `llama-server` binaries under `backends/` that have a
  `schema.json`. Docker entries (Strata, HyperQwen) and `sd-server` are out of scope: their settings are
  not llama.cpp flags.
- **Context tiers:** one entry per tier, tuned separately. The default tiers are 64K, 128K and 256K
  (`-c 65536 / 131072 / 262144`), and 256K is optional per model. Each tier is tuned on its own because
  the KV cache grows with context, which changes the VRAM left for weights or the expert cache, the
  usable `-ub`, and sometimes the backend. A tier above the model's trained context is skipped unless
  the user enables RoPE scaling.
- **Scope (v1):** the local station only. Peers such as turiserver are out of scope.
- **Output:** a new entry per tier, named `<model>-tuned-station-<tier>`, shown as one diff. Nothing is
  written without the user's approval.

## Background

On 2026-11-08 an entry for Swift 1.5 Qwen3.8-Flash-Next IQ3_XXS was tuned by hand
(`swift-1.5-qwen3.8-flash-next-llamacpp-tuned-station-128k`) and was much faster than the stock entry.
Getting there needed several things that are mechanical and should be automated:

- **Finding which backend supports a feature.** Only `ggml-org` has `--moe-cache-mib`; `qwen4exp_mtp`
  predates it.
- **Placing experts.** Experts in pinned host RAM (`-cmoe` with `-lm none`), with a GPU expert cache sized
  to the VRAM that is left over.
- **Prefill batch against cache VRAM.** A larger `-ub` speeds up prefill but takes VRAM from the expert
  cache.
- **Catching an incompatible draft file before launch.** The shared MTP GGUF has no `token_embd`, which
  upstream requires, so the server died after a 70 s load.

The tuner is meant to reach that entry, or a better one, without a human doing any of this.

## Reference workloads

The score is the sum of wall times. Each workload is run against a server that is already loaded and warm.
The long workload scales with the tier.

| Workload | Prompt | Generation | Why |
|---|---|---|---|
| chat | 2K tokens | 512 | short-turn latency |
| long | half the tier: 32K / 64K / 128K | 1K | prefill-bound, plus decode with a deep KV cache |
| follow-up | +1K on top of the long prompt (cache reuse) | 512 | checks that prompt caching works |

The 128K long workload alone takes about 2–4 minutes per round at 500–1,000 tok/s prefill. Show the
estimated total time before the benchmark starts.

- Report prefill tok/s, decode tok/s and time to first token per workload alongside the score.
- Use fixed prompts, temperature 0 and a fixed seed, with 2 rounds per candidate.
- The workloads cover both prefill and generation speed. The weights are a setting, defaulting to equal.

## Architecture

Each stage is an interface. Implementations stay unexported in `internal/mantle/tuner`, constructed
through `NewTuner(deps)`.

```go
type SystemProbe interface        { Probe(ctx) (SystemInfo, error) }               // VRAM free/total, PCIe gen/width, RAM, cores, AVX-512, disk
type ModelInspector interface     { Inspect(path string) (ModelInfo, error) }       // from gguf.go: arch, dense/MoE, expert bytes, NextN layers, KV bytes/token per type, head dims
type CapabilityResolver interface { Resolve(ModelInfo) ([]BackendFit, error) }      // from backends/*/schema.json: which backends can run which features
type PlanGenerator interface      { Plans(SystemInfo, ModelInfo, []BackendFit) []Candidate }
type Benchmarker interface        { Run(ctx, Candidate, []Workload) (Result, error) }
type Recommender interface        { Pick([]Result) Recommendation }
type ConfigProposer interface     { Propose(ModelInfo, Candidate, Request) (configedit.Change, error) } // never writes
```

## Stage rules

**SystemProbe**
- Free VRAM is valid only when llama-swap has no model resident on the GPU.
  - The planner budgets from total VRAM minus a fixed reserve, so analysis works while a model is
    loaded. The probe warns when more than 25% of VRAM is in use.
  - The benchmark must unload, or refuse to run.
- Record the probe in every result, so cached results are tied to the hardware.

**CapabilityResolver** checks before any launch:
- The flags each candidate needs exist in the backend's schema.
- A draft or MTP file is compatible with the backend. This catches the missing `token_embd` case:
  - read the draft file's tensor list;
  - an MTP-only file must contain `token_embd` for upstream builds;
  - otherwise the main GGUF must have NextN layers.
- The KV cache type is valid for the head dim (the KVarN rule in config.yaml).

**PlanGenerator** (analytical: narrows the options, decides nothing):

1. If weights, KV and compute buffer fit in VRAM: all layers on the GPU (`-ngl 99`), no CPU experts.
2. MoE that does not fit:
   - `-cmoe -lm none`.
   - Expert cache = free VRAM − dense weights − KV − compute buffer(`-ub`) − margin.
   - Without `--moe-cache-mib`, fall back to sweeping `-ncmoe`.
3. Dense model that does not fit: `--fit on`. Note that `--fit` does not count the MoE cache.
4. Batch: `-ub` ∈ {1024, 2048, 4096, 8192} with `-b = max(ub, 8192)`. Each step costs cache VRAM.
5. Drafting, single-user, so cheap drafts pay:
   - MTP if the resolver allows it;
   - otherwise n-gram drafting (`ngram-mod`);
   - `--spec-draft-n-max` ∈ {2, 3}.
6. KV cache: q8_0 by default. Lower only on request (lossy).
7. Output: at most 6 candidates, grouped by memory layout.

**Benchmarker**
- Loads are expensive (about 70 s for the 125B MoE). Group candidates that differ only in request-time
  settings under one load.
- Launch through llama-swap, so the measured path is the served path.
- MoE with the cache: run llama.cpp's `scripts/moe-cache-tune.py` once per tier and layout to pick
  `--moe-cache-mib` and `--moe-cache-cpu-share`. Do not reimplement it. What it does and needs:
  - **What it does:** starts the server 6 times (2 cache sizes × 3 CPU shares), records the expert
    routing through `LLAMA_MOE_CACHE_TRACE`, and replays the routing through a copy of the cache logic
    to predict the other sizes.
    - Cost: 6 loads per tier, about 7 minutes for the 125B MoE.
    - Its `--out` directory reuses runs while the command is unchanged. Keep it per tier under
      `backends/<name>/tune/`.
  - **Backend requirement:** the backend must include llama.cpp #30966 (merged 2026-11-08). It adds
    `LLAMA_MOE_CACHE_TRACE`, `--moe-cache-cpu-share` and the script.
    - Detect support by the `--moe-cache-cpu-share` flag in the schema and the trace variable in the
      binary.
    - `ggml-org` has it since the rebuild of 2026-11-09 (`7d7f028`).
  - **Version match:** the script replays a copy of `src/llama-moe-cache.cpp`, so it must come from the
    same commit as the binary.
    - `docker/build-llamacpp.sh` copies it into `backends/<name>/tools/`, with the built commit in
      `tools/SOURCE_COMMIT`.
    - The tuner runs only that copy, never one from another checkout.
    - The container runs the build script baked into the image. Rebuild the image, or pass
      `-build-script /etc/llama-swap/config/build-llamacpp.sh`, for other builds to ship the tools.
  - **Runtime:** Python 3 standard library only. Python 3.12 is in the llama-swap container.
  - **Without the script** (older builds, forks): measure `--moe-cache-mib` directly at 3 sizes with
    `--moe-cache-cpu-share 0`.
  - **Without the MoE cache at all:** sweep `-ncmoe`.
- A candidate that runs out of memory or exits is a recorded failure with the log tail, not a crash.
- Cache results by (backend build hash, model sha256, probe hash, candidate argv).

**Recommender**
- Lowest score among candidates within 5% of the best on every workload.
- Ties go to fewer flags and more free VRAM.

## Quality rules

- **Lossless only by default.** Tail-skip, KV below q8_0, fewer experts and `--override-kv` expert counts
  are opt-in. Each is listed with its measured or documented quality cost next to its speed gain.
- **Pass on the winner to the quality check.** Offer one click to run the intelligence benchmark on the
  proposed entry against the current one. Speed results alone do not count as done.

## UI

- Pick model file, then Analyse. Show the probe, the model facts and the candidate table with each
  candidate's reason, and flag the ones the resolver blocked.
- Benchmark: progress per candidate. The GPU is unavailable to other models meanwhile, so say so
  before starting.
- Result table: score, prefill, decode and TTFT per workload.
- "Stage entries" adds the picked candidates to the config draft. The Review sheet shows the diff and
  applies it.
- UI copy states facts; reasoning goes in the docs (see the global UI-text rules).

## Out of scope (v1)

- Peers and remote probing.
- Tuning several entries at once.
- Multi-user throughput (`--parallel` > 1).
- Code changes to llama.cpp, such as next-layer expert prefetch or tail-skip. The tuner only uses
  flags a backend already has.

## Test plan

- Unit tests:
  - `PlanGenerator` against recorded probe and model fixtures (`internal/mantle/testdata`);
  - `CapabilityResolver` against the real `schema.json` files of `ggml-org` and `qwen4exp_mtp`;
  - the resolver must reject today's MTP-only file without `token_embd`.
- `Benchmarker` against a fake server that implements the OpenAI timing fields.
- Acceptance: on the station, for Swift Flash-Next IQ3_XXS:
  - the 128K recommendation is at least as fast as
    `swift-1.5-qwen3.8-flash-next-llamacpp-tuned-station-128k` on the reference workloads;
  - the 64K and 256K entries load and complete their workloads;
  - it is seen working in the UI, not only in tests.
