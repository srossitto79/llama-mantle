# Strata on demand

Swift 1.5 Flash Next IQ3_XXS, 128K context, INT8 KV, CPU vision, GPU 0, from the
upstream image `ghcr.io/niko1221/strata:0.2.7-cuda13` (nothing is built locally).
llama-swap starts `strata-swift` when selected and stops it on unload or idle expiry.
Its OpenAI API stays on the Compose network; no host port is published.

## API key

The image never serves without a key. Put one in `.env` (gitignored) next to the
Compose file:

```bash
printf 'STRATA_API_KEY=%s\n' "$(openssl rand -hex 24)" > .env
```

Compose passes it to llama-swap. The model entry hands it to the container as
`API_KEY` through `env:` (so it is not part of the logged command) and sends it
upstream with `apiKey:`. `GET /health` answers without a key, so the health check
needs nothing extra. llama-swap refuses to load `config.yaml` while
`STRATA_API_KEY` is unset.

## First start and run config

The GGUF folder is mounted read-only at `/models`. The first start prepares the pack
and MTP layer in the `strata-data` volume; a volume from the older 0.1.x image is
migrated in place (nothing downloaded again). Later starts serve in about a minute.

Strata decodes greedily when a request sets no sampling, and its reasoning-loop guard
is off by default. After the first start, seed Qwen's thinking-mode sampling and
`reasoning_loop_recovery: "recover"` into the saved run config:

```bash
bash docker/strata/seed-run-config.sh
```

Requests that set their own sampling (for example `temperature: 0`) still win.
Strata rewrites the run config when its settings change (for example `GPU`); run the
script again after that.

## Use

Merge `model.yaml` under `models:` in the live `config.yaml` (already done on this
installation). For another host, adjust the bind source and Compose network.

Select `swift-1.5-qwen3.8-flash-next-strata-station-128k` in llama-swap or your
OpenAI client. Suffixes `:off`, `:low`, `:medium` and `:high` choose reasoning effort.

```bash
docker logs -f strata-swift
docker exec -it strata-swift strata monitor
curl http://localhost:9292/running
```

## DeepSeek V4.1 Flash

`deepseek.yaml` holds five entries (16K, 32K, 64K, 128K, 256K) for the IST-DAS 2.00 bpw set:
138 GB of shards, the 111 GB Engram n-gram table and the 8 GB DSpark drafter, 257 GB
in total. On a 3090 with 110 GB of RAM, about 20 GB of experts stay on the card and
the rest are computed on the CPU from RAM.

- `~/gguf_models/strata-deepseek-v4.1-flash` is mounted as Strata's models folder.
  Setup downloads into it and checks every file against its pinned SHA-256. The
  Engram is read from disk for every token, so keep the folder off `/mnt/c` and `/mnt/f`.
- Each entry has its own volume (`strata-deepseek-<ctx>`) for its pack, run config and
  DeepSeek engine package (`/data/install/engine-dsv41`, about 660 MB).
- `seed-run-config.sh` does not apply: with no `sampling` block, 0.2.7 uses DeepSeek's
  model-card sampling, and the loop recovery swaps Qwen's effort sentence.

Download once, without loading the model, then prepare each context volume the same
way (it finds the files already there and only checks and packs). The two
`STRATA_*INSTALL*` settings are what `strata launch` passes; without them setup writes
the run config and engine into the container filesystem.

```bash
docker run --rm --gpus all \
  -v strata-deepseek-16k:/data \
  -v ~/gguf_models/strata-deepseek-v4.1-flash:/data/Strata-data/models \
  -w /opt/strata/terminal -e PYTHONPATH=/opt/strata/terminal \
  -e STRATA_CONTROL_INSTALL=1 -e STRATA_INSTALL_DIR=/data/install \
  --entrypoint /opt/strata/terminal/python/bin/python3 \
  ghcr.io/niko1221/strata:0.2.7-cuda13 \
  setup.py --yes --no-start --family deepseek41 --model IST-DAS-2.00bpw \
  --data-dir /data/Strata-data --context 16384 --vision yes --gpu 0
```

`--gpu 0` matches the entries' `GPU=0`; a different card makes the start drop the run
config and prepare it again. Copying `/data/install/engine-dsv41` from a prepared
volume first skips the engine download.

Merge the entries into `config.yaml` only once the download has finished: an entry
started earlier runs the same setup into the same folder. Suffixes `:off`, `:low`,
`:medium`, `:high` and `:max` choose reasoning effort (the template's default is high).

## Updating

Change the image tag in the entry (and `seed-run-config.sh`'s default), then
`docker pull` it. The volume keeps the pack, preset and run config. A new engine can
make an old tuning result less useful; upstream suggests one start with
`-e TUNE=quick -e RETUNE=1`. See upstream `docs/DOCKER.md` for every setting.
