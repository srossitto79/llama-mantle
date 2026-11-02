# Strata and HyperQwen on the 4060 Ti server

Instructions for the agent that maintains the llama-mantle instance on the second server.
They add the Strata Swift model there and explain why HyperQwen is not added. DeepSeek is
out of scope on this server.

Read [docker-backends-porting.md](docker-backends-porting.md) and
[docker/strata/README.md](../docker/strata/README.md) first; this page only covers what is
different on this hardware.

## Target hardware

| Part | Value |
| --- | --- |
| GPUs | RTX 4060 Ti 16 GB (Ada, sm_89) + RTX 2080 8 GB (Turing, sm_75) |
| RAM | 48 GB |
| Disk | 512 GB SSD |

The workstation this was written on has an RTX 3090 24 GB and about 110 GB of RAM. Do not
copy its model sizes or memory settings.

## Decision

| Backend | On this server | Reason |
| --- | --- | --- |
| Strata, Swift 1.5 **IQ2_XS** | Add | Fits 48 GB of RAM; runs on the 4060 Ti alone |
| Strata, Swift 1.5 IQ3_XXS (the workstation's size) | Do not add | Needs 43 GB of experts in RAM plus about 10 GB for the system |
| HyperQwen 27B INT4 | Do not add | Needs a 24 GB card; see [HyperQwen](#hyperqwen-not-deployable) |

## Before you change anything

Run these and stop if one fails:

```bash
nvidia-smi --query-gpu=index,name,memory.total,driver_version,compute_cap --format=csv
free -g
df -h /mnt/storage /var/lib/docker
docker info --format '{{.Runtimes}}'          # must list nvidia
docker run --rm --gpus all ubuntu nvidia-smi -L
```

- **Driver.** 580 or newer: use `ghcr.io/niko1221/strata:0.2.9-cuda13`. 575 to 579: use
  `0.2.9-cuda12` everywhere this page says `cuda13`. Below 575: stop and report.
- **GPU index.** Note the `index` of the 4060 Ti. Strata numbers cards the way `nvidia-smi`
  does. Every `GPU=` below is that index, not a literal `0`.
- **Disk.** You need about 90 GB free on the SSD: 68.2 GB of GGUF files, about 9 GB for
  Strata's prepared pack, 2.3 GB for the image, plus headroom. The model's second shard is
  read from disk while serving, so the GGUF folder must be on the SSD, not on a network or
  spinning disk. If `/mnt/storage` is not the SSD, use a folder that is.

## 1. Update llama-mantle

The model entry uses `apiKey:` (per-model upstream key, commit `82ad344`), and llama-swap
needs Docker access to start Strata as a sibling container. Both are on `main`.

1. `git pull` on `main`.
2. Create `.env` next to `docker-compose-server.yml` with a **new** key, generated on this
   server. Do not copy the workstation's key.
   ```bash
   printf 'STRATA_API_KEY=%s\n' "$(openssl rand -hex 24)" >> .env
   ```
   `docker-compose-server.yml` refuses to start llama-swap while `STRATA_API_KEY` is unset.
3. Rebuild and restart llama-swap:
   ```bash
   docker compose -f docker-compose-server.yml build llama-swap
   docker compose -f docker-compose-server.yml up -d llama-swap
   ```
   The image build also compiles llama.cpp with `MAX_BUILD_JOBS: 1`, so it is slow. llama-swap
   is down only for the `up -d` step.
4. Check that the Docker CLI works inside llama-swap (the host binary is mounted read-only):
   ```bash
   docker compose -f docker-compose-server.yml exec llama-swap docker version
   docker network ls | grep _default    # note the network name for --network
   ```
   The network is `<checkout folder name>_default`, for example `llama-mantle_default`.

## 2. Download Swift 1.5 IQ2_XS

Two shards from `ukisai/Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF`: 39.8 GB + 28.4 GB.

```bash
DEST=/mnt/storage/lm_storage/models/gguf/ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF
mkdir -p "$DEST"
docker run --rm -v "$DEST":/dst python:3.12-slim sh -c '
  pip install -q huggingface_hub &&
  hf download ukisai/Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF \
    --include "*IQ2_XS-*.gguf" --include SHA256SUMS --local-dir /dst'
cd "$DEST" && grep IQ2_XS SHA256SUMS | sha256sum -c -
```

Both lines must say `OK`. Delete `$DEST/.cache` afterwards if it was created.

## 3. Add the model entry

Add this under `models:` in the server's live config
(`/mnt/storage/lm_storage/configs/config.yaml`). Replace `<GPU>` with the 4060 Ti's index and
`<network>` with the network name from step 1. If the server uses a different naming
convention for model IDs, follow it.

```yaml
  swift-1.5-qwen3.8-flash-next-iq2xs-strata-128k:
    name: "Swift 1.5 Flash Next IQ2_XS Strata 128K"
    description: "IQ2_XS with MTP and INT8 KV on the 4060 Ti"
    cmd: >-
      docker run --rm --name strata-swift
      --gpus all --ulimit memlock=-1:-1 --shm-size 2g --network <network>
      -v strata-data:/data
      -v /mnt/storage/lm_storage/models/gguf/ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF:/models:ro
      -e FAMILY=swift -e MODEL=IQ2_XS -e MODEL_PATH=/models
      -e CONTEXT=131072 -e VISION=off -e KV=int8 -e GPU=<GPU> -e PORT=8080
      -e API_KEY
      ghcr.io/niko1221/strata:0.2.9-cuda13
    cmdStop: docker stop -t 120 strata-swift
    env:
      - "API_KEY=${env.STRATA_API_KEY}"
    apiKey: ${env.STRATA_API_KEY}
    proxy: http://strata-swift:8080
    checkEndpoint: /health
    useModelName: swift-1.5-iq2_xs
    filters:
      setParamsByID:
        "${MODEL_ID}:off":
          reasoning_effort: none
        "${MODEL_ID}:low":
          reasoning_effort: low
        "${MODEL_ID}:medium":
          reasoning_effort: medium
        "${MODEL_ID}:high":
          reasoning_effort: high
```

Choices that differ from the workstation:

- **`MODEL=IQ2_XS`.** Strata's own fit table: IQ2_XS keeps 35.5 GB of experts in RAM and fits
  48 GB; IQ3_XXS needs 43 GB and does not.
- **`VISION=off`.** On a card under 24 GB the picture reader runs on the CPU and takes RAM that
  is already tight. Turn it on (`VISION=cpu`) only after a stable run shows spare RAM.
- **`GPU=<GPU>`, the 4060 Ti only.** Strata leaves a card of 8 GB or less off when a bigger card
  is present, because every card holds a copy of the dense weights. Do not add the 2080.
- **`useModelName`.** `swift-1.5-iq2_xs` is inferred from the workstation's `swift-1.5-iq3_xxs`.
  After the first start, confirm it and fix the entry if it differs:
  ```bash
  docker exec strata-swift sh -c 'curl -s -H "Authorization: Bearer $API_KEY" http://127.0.0.1:8080/v1/models'
  ```
- **`healthCheckTimeout`.** The first start prepares the pack and fetches the MTP layer, which
  takes several minutes. The workstation's config uses `healthCheckTimeout: 1800`; make sure
  the server's is at least that long.

While llama-swap runs this model, nothing else should hold large amounts of RAM: llama.cpp
models in the same group or a second Strata entry would push the experts out of memory.

## 4. First start and run config

1. Send one request so llama-swap starts the container:
   ```bash
   curl -s -m 1800 http://localhost:9292/v1/chat/completions -H 'Content-Type: application/json' \
     -d '{"model":"swift-1.5-qwen3.8-flash-next-iq2xs-strata-128k","messages":[{"role":"user","content":"Reply with exactly: ok"}],"max_tokens":400}'
   ```
2. Seed sampling and loop recovery into the run config. Without this, requests that set no
   sampling decode greedily and the reasoning-loop guard is off:
   ```bash
   bash docker/strata/seed-run-config.sh
   ```
   If the image is `cuda12`, run it as `STRATA_IMAGE=ghcr.io/niko1221/strata:0.2.9-cuda12 bash docker/strata/seed-run-config.sh`.
3. Check that the settings stuck:
   ```bash
   docker run --rm -v strata-data:/data:ro alpine \
     grep -HoE '"(reasoning_loop_recovery|temperature)": *[^,]*' /data/install/strata-*.json
   ```
   Expect `"temperature": 1.0` and `"reasoning_loop_recovery": "recover"`. Strata rewrites this
   file when its settings change (a new `GPU`, a new image version). Run the script again after
   any such change.

## 5. Verify

All of these must pass before you report the model as available:

- [ ] `curl http://localhost:9292/running` lists the model while the container is up.
- [ ] The test request returns `ok`, and the `:off` suffix returns without reasoning.
- [ ] Engine log has no errors:
  ```bash
  docker run --rm -v strata-data:/data:ro alpine \
    grep -iE "unknown error|illegal|out of memory|failed" /data/state/strata/launch/docker.engine.log
  ```
  The container runs with `--rm`, so after a crash this file on the volume is the only log left.
- [ ] `free -g` while the model is loaded and answering a long prompt: note the available RAM in
  your report.
- [ ] One long request (a prompt of about 20k tokens, or `max_tokens` of a few thousand) completes,
  and its tokens per second are noted.

Speed references, not targets: Strata's IQ2_XS sizes are described as fast and the 4060 Ti is
reported upstream at about 39 tokens per second with IQ3_XXS and 64 GB of RAM. Nobody has measured
IQ2_XS with 48 GB.

## Known issues

- **MTP drafts may not run on the 4060 Ti.** Strata issue
  [#4575](https://github.com/Niko1221/Strata/issues/4575) reports `drafts accepted 0 of 0` on a 4060 Ti
  (sm_89) with another model. If the engine log shows the same, decoding still works, only slower.
  Note it in your report.
- **A GPU fault leaves the engine failing every request while `/health` stays ok** (Strata
  [#4350](https://github.com/Niko1221/Strata/issues/4350)). On the workstation it appeared when the
  display woke from sleep. Symptoms: empty responses, `no valid JSON data found in stream` in
  llama-swap's log, `unknown error` in the engine log. Fix: `docker stop strata-swift`; llama-swap
  starts it again on the next request.

## HyperQwen: not deployable

Do not add a HyperQwen entry on this server.

- **One 16 GB card is too small.** The model is Qwen3.8-27B in W4A16 INT4: 14.9 GB of weights. On
  the workstation's 3090 the weights and runtime take 14.6 GiB before any KV cache. A 16 GB card has
  no room left for the KV cache. HyperQwen's install guide requires a 24 GB Ampere or newer card, and
  its README lists 16 GB and 12 GB support as future work that needs smaller checkpoints.
- **The two cards cannot share it.** Tensor parallelism splits the weights evenly, about 7.5 GB per
  card, which leaves the 8 GB 2080 nothing for KV cache and activations. The 2080 is also Turing
  (sm_75), which HyperQwen lists as untested, and its int8 attention path (`PTH_FA`) is limited to
  sm_80, sm_86 and sm_89. Pipeline parallelism, which could split the layers unevenly, is not set
  by any HyperQwen launcher and has only one community report (`docs/multi-gpu.md` in the image).

Revisit when HyperQwen publishes a smaller checkpoint
([syv-ai/HyperQwen](https://github.com/syv-ai/HyperQwen)).

## Report back

Send the user:

- Driver version, the image tag used, and the 4060 Ti's GPU index.
- The final model ID and `useModelName`.
- Load time of the first start and of a second start.
- Available RAM while loaded, and tokens per second on the long request.
- Whether MTP drafts were accepted (engine log), and any error lines.
