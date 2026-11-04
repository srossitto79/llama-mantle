# InferSSD Docker experiment

Status: existing-files-only trial; image built and CUDA/cgroup/io_uring smoke checks
passed on the workstation. **The current Strata weights cannot run on this InferSSD
revision**. Both rejections below were reproduced inside the built image. No
throughput comparison is available.
This directory does not change the active Mantle configuration or Strata containers.

## Compatibility findings

Source pinned to `93e76a24a225a51391ec7b4a93daf179973c067e`.

| Existing checkpoint | Header-only result |
| --- | --- |
| Swift/Qwen3.8 Flash Next GSQ-RCO IQ3_XXS, both shards | Rejected: `blk.1.ffn_down_exps.weight` uses native type 42. InferSSD's strict alias builder explicitly reserves/rejects this type. |
| DeepSeek V4.1 Flash GSQ-RCO IQ2_XXS / IST-DAS 2.00 bpw, four shards | Expert tensors contain IQ1_M, IQ2_XS and IQ2_XXS. InferSSD's DeepSeek adapter requires **all three expert matrices to be MXFP4**. |

These are real local header inspections, not inference failures or benchmark results.
Do not relabel type 42 or bypass quantization checks: that would misinterpret weights.
InferSSD supports `qwen4exp` (Qwen3.8 Flash Next), but that does not establish support
for every conversion. It does not imply support for the older Qwen3-Next architecture.

The user has selected reuse of existing files only; do not download replacement
checkpoints for this experiment. A future trial would require compatible Qwen weights
and the complete supported DeepSeek checkpoint, or upstream support for the current
formats. Upstream's tested DeepSeek Q8_0 keeps routed experts in MXFP4; “Q8_0”
does not mean all its tensors are eight-bit. It occupies about 473 GiB. The existing
Strata DeepSeek shared first shard is metadata-only and cannot replace that set.
The WSL model filesystem had only about 448 GiB free during investigation, before
this image build: choose a larger SSD/filesystem before downloading DeepSeek.
No weights are downloaded by this image or entrypoint.

Source references:

- [Quick start and supported weight selection](https://github.com/wici-ai/inferssd/blob/93e76a24a225a51391ec7b4a93daf179973c067e/docs/quickstart.md)
- [Model quantization checks](https://github.com/wici-ai/inferssd/blob/93e76a24a225a51391ec7b4a93daf179973c067e/engine/src/ssd/model-adapter.cpp)
- [Strict GGUF/type checks](https://github.com/wici-ai/inferssd/blob/93e76a24a225a51391ec7b4a93daf179973c067e/framework/src/inferssd/source/gguf_alias.py)

## Docker deployment

The upstream `inferssd serve` Python launcher requires systemd and launches its own
memory-limited scope. It also binds to loopback. Running that command in an ordinary
sibling container is insufficient.

This experiment runs the native `inferssd-server` directly, with Docker owning its
cgroup and process lifecycle. The entrypoint builds upstream's strict GGUF expert
alias without duplicating payloads, writes typed SSD settings and records launch
arguments, source metadata and native telemetry in `/state/run-*`.

This is a **manual-settings deployment**, not upstream's measured automatic launch
strategy or supervised runtime. It omits machine autotuning, guardian locks, runtime
resource sampling and persistent prompt caching. Initial settings are 8 threads,
16K context, 512-token batch/microbatch, Flash Attention, one slot, automatic VRAM
expert caching and 4 GiB host headroom. Tune after a successful smoke test; initial
results cannot establish InferSSD's maximum performance. No MTP drafter is configured.

On this Docker Desktop / WSL2 workstation the prerequisite probe confirmed:

- cgroup v2 exposes Docker's finite `memory.max` and `memory.swap.max=0`.
- Default Docker seccomp blocks `io_uring_setup` with `EPERM`.
- With `seccomp=unconfined`, `io_uring_setup` succeeds. The experimental Compose
  service uses that setting, drops all capabilities, and mounts no Docker socket
  or host cgroup tree. A narrower seccomp profile is follow-up work before regular use.
- The workstation GPU is an RTX 3090 (compute capability 8.6). Build on the target
  CPU because the image enables native CPU instructions. The image includes the
  development toolchain for investigation; it is not a reduced production image.

The native `--version` smoke check recognized the 3090 and reported revision
`93e76a2`. The Docker build links against CUDA's supplied driver stub; the stub
directory is not added to the runtime library path, which uses the host driver.

Build from the repo root:

```sh
docker build -t llama-mantle/inferssd:93e76a2 \
  -f docker/inferssd/Dockerfile docker/inferssd
```

Check the container prerequisites without loading a model:

```sh
docker run --rm -i --memory 4g --memory-swap 4g \
  --ulimit memlock=-1:-1 --security-opt seccomp=unconfined --cap-drop ALL \
  --entrypoint python3 llama-mantle/inferssd:93e76a2 - \
  < docker/inferssd/probe.py
```

Run these commands in WSL/Linux, using paths on the Docker host. Inspect the
existing Swift set (currently expected to fail, without using the GPU):

```sh
export INFERSSD_MODELS_DIR=/home/sross/gguf_models
export INFERSSD_MODEL=ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF/Swift-Qwen3.8-Flash-Next-GSQ-RCO-IQ3_XXS-00001-of-00002.gguf
docker compose -f docker/inferssd/compose.yaml run --rm --no-deps inferssd \
  --inspect --model "/models/$INFERSSD_MODEL"
```

For DeepSeek, change `INFERSSD_MODEL` to
`strata-deepseek-v4.1-flash/deepseek-v4.1-flash-IST-DAS-2.00bpw/DeepSeek-V4.1-Flash-1017-GSQ-RCO-IQ2_XXS-00001-of-00004.gguf`.
For compatible replacement weights, set the corresponding first shard instead,
with all shards together under `INFERSSD_MODELS_DIR`, then repeat inspection.
Passing header inspection is necessary but does not prove native kernel support.

Once compatible weights are present, unload Strata and other inference models
through Mantle before starting InferSSD. Never run the two benchmarks concurrently:
both compete for RAM/VRAM and expert caches. Start only one InferSSD service:

```sh
docker compose -f docker/inferssd/compose.yaml up -d
docker compose -f docker/inferssd/compose.yaml logs -f
curl --fail http://127.0.0.1:18080/health
curl --fail http://127.0.0.1:18080/v1/models
```

The default hard memory limit is 90 GiB with container swap disabled. That budget
assumes the large Strata process is stopped; lower `INFERSSD_MEMORY` if other
workloads need the RAM. Set `INFERSSD_CONTEXT`/`INFERSSD_THREADS` as needed.
The API is published only on host loopback, with the alias `inferssd-race`.
On Docker Desktop, a Mantle container can proxy it through
`http://host.docker.internal:18080`; Linux Engine needs an explicitly configured
host gateway or a shared network. Do not add a fixed host port to multiple
simultaneous model entries. Stop the experiment before loading Strata again:

```sh
docker compose -f docker/inferssd/compose.yaml down
```

The named state volume persists; `down` without `-v` preserves evidence.

## Race procedure

First compare each product as deployed: Strata with its existing quantization/MTP
versus InferSSD with its supported weights and manual settings. Label this an
**operational comparison across checkpoints**, not a same-weight engine benchmark.
A controlled engine comparison needs a complete checkpoint both engines accept,
equal context/KV settings and MTP disabled on both (or explicitly compared separately).
Text-only runs avoid differences in vision support.

Use identical prompts, context, generation cap, sampling and reasoning effort.
Keep early EOS results separate from runs that reach the requested cap. Save actual
output and inspect it for corruption/repetition; throughput is not a quality score.
Run one cold-start/first request separately, then repeated warm requests. The script
labels the first request as cache-uncontrolled: it never drops the host page cache.
The second and third rounds reuse the same prompt and may reuse prompt/KV caches;
do not treat their prompt processing rate as uncached prefill performance.

```sh
python3 docker/inferssd/race.py \
  --url http://127.0.0.1:18080/v1 --model inferssd-race \
  --label inferssd-qwen --quant 'REPLACE: checkpoint, quant, no MTP' \
  --prompt-file docker/inferssd/prompt.txt --output build/inferssd-race.jsonl

# Stop InferSSD, then select the Strata model through Mantle.
python3 docker/inferssd/race.py \
  --url http://127.0.0.1:9292/v1 \
  --model swift-1.5-qwen3.8-flash-next-strata-station-128k \
  --label strata-swift --quant 'Swift GSQ-RCO IQ3_XXS, MTP enabled' \
  --prompt-file docker/inferssd/prompt.txt --output build/inferssd-race.jsonl
```

For DeepSeek use the Strata 16K entry and a 16K InferSSD context first; repeat longer
contexts only after the basic run works. Add `--api-key-env NAME` for an authenticated
Mantle endpoint, and the same `--reasoning-effort` where both endpoints support it.
The script records TTFT to the first content/reasoning output, full wall duration,
server-reported usage/timings and output. Wall output tokens/s includes prompt
processing and queue time; compare native decode rates separately when both provide
them. It never counts SSE chunks as tokens, and missing usage leaves throughput null.
Record GPU/host memory and startup time separately with Docker stats, nvidia-smi and
the container logs. Keep model files on the same physical SSD for meaningful I/O comparisons.
