# Porting Docker backends to another server

Covers the two Docker-based entries in `config.yaml`:

- `swift-1.5-qwen3.8-27b-hyperqwen-station-{150k,240k}` → remote image `ghcr.io/syv-ai/hyperqwen:latest`
- `swift-1.5-qwen3.8-flash-next-strata-station-128k` → remote image `ghcr.io/niko1221/strata:0.2.7-cuda13` (a former 256k twin was removed: it shared `--name`/`strata-data` with the 128k entry, so it could neither coexist nor hold a different `CONTEXT`)

Copying the YAML alone is not enough. The image, volumes, model files, and Docker network must be recreated on the new host.

## Common prerequisites (both backends)

1. Docker Engine + `nvidia-container-toolkit`, working `--gpus all`.
2. llama-swap with host Docker access (see `docker-compose.llama-swap.yml`):
   `- /var/run/docker.sock:/var/run/docker.sock`
3. A user-defined network. The entries use `--network llama-mantle_default`, which is the Compose project network for a directory named `llama-mantle`. On the new host check the real name and adjust every `cmd`:
   ```bash
   docker network ls | grep _default
   ```

## HyperQwen (remote image)

`docker run` auto-pulls `ghcr.io/syv-ai/hyperqwen:latest` on first use, so no build step. Manual alternative: `docker pull ghcr.io/syv-ai/hyperqwen:latest`.

What does NOT transfer automatically:

1. **Model payload.** The entry mounts `hyperqwen-models:/app/models` and sets `MODEL=/app/models/Swift-1.5-Qwen3.8-27B-W4A16-HyperQwen-INT4` with `PREPARE=0` (skips download/requantize). Populate the named volume before first use, e.g.:
   ```bash
   docker volume create hyperqwen-models
   docker run --rm -v hyperqwen-models:/dst -v /path/on/new/host:/src:ro alpine cp -a /src/Swift-1.5-Qwen3.8-27B-W4A16-HyperQwen-INT4 /dst/
   ```
   Or swap the `-v` for a bind mount pointing at the new host path.
2. **Kernel cache.** `hyperqwen-cache:/cache` (`HOME=/cache`) is rebuilt on first boot (up to ~15 min per the `config.yaml` comment). Just create it; don't copy it unless the GPU/driver matches exactly:
   ```bash
   docker volume create hyperqwen-cache
   ```
3. **Host-specific flags.** `--gpus all --ipc host`, `-e GPU_UTIL / MAX_SEQS / CTX / SPEC / VISION*` stay valid only if the new GPU has similar VRAM. Re-tune `GPU_UTIL` and `MAX_SEQS` for smaller cards. Keep distinct `--name` / `proxy` hostnames per variant (`hyperqwen-swift` vs `hyperqwen-swift-huge`).

Verify with `docker logs -f hyperqwen-swift` and `curl http://localhost:9292/running` after the first request.

## Strata (remote image)

`docker run` pulls `ghcr.io/niko1221/strata:0.2.7-cuda13` on first use. It needs NVIDIA driver 580+ (use the `-cuda12` tag for 575-579 or Pascal/Volta cards) and about 50 GB free for `strata-data`.

What does NOT transfer automatically:

1. **API key.** Create `.env` with `STRATA_API_KEY=...` next to the Compose file (`docker/strata/README.md`). llama-swap will not load `config.yaml` without it.
2. **Prepared files.** The first start on the new host builds the pack and fetches the MTP layer into `strata-data` from the mounted GGUF shards. Copy the volume instead to skip that.
3. **Run config.** Run `bash docker/strata/seed-run-config.sh` once after the first start, otherwise requests without sampling decode greedily and the reasoning-loop guard stays off.

Per-entry adjustments in `config.yaml`:

- Bind source: replace `/home/sross/gguf_models/...` with the new host path; it is mounted read-only at `/models`.
- `--network`: see Common section above.
- Container name: keep `--name` / `proxy` hostname / volume unique per entry. Two entries sharing `--name strata-swift` and `strata-data` with different `CONTEXT` cannot run side by side.

DeepSeek V4.1 Flash (`deepseek-v4.1-flash-strata-station-{16k,32k,64k,128k,256k}`, `docker/strata/deepseek.yaml`) also needs:

- About 260 GB free under the bind source `/home/sross/gguf_models/strata-deepseek-v4.1-flash` (shards, Engram, drafter). Copy that folder to skip the download.
- One `strata-deepseek-<ctx>` volume per entry (pack, run config, DeepSeek engine), prepared as in `docker/strata/README.md`.

## Checklist before serving

- [ ] `docker network ls` name matches every `--network`
- [ ] GGUF/model payload present at the new bind path or inside the named volume
- [ ] Strata: `STRATA_API_KEY` in `.env`, first start finished, `seed-run-config.sh` run; HyperQwen: first-boot compile awaited (`/health` may take minutes)
- [ ] Proxy hostnames resolve on the Docker network
- [ ] `docker logs -f <container>` + `/running` + one test chat completion per entry
