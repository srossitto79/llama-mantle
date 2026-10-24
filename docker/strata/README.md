# Strata on demand

Swift 1.5 Flash Next IQ3_XXS, 128K context, INT8 KV, CPU vision, GPU 0, from the
upstream image `ghcr.io/niko1221/strata:0.2.3-cuda13` (nothing is built locally).
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

## First start and sampling

The GGUF folder is mounted read-only at `/models`. The first start prepares the pack
and MTP layer in the `strata-data` volume; a volume from the older 0.1.x image is
migrated in place (nothing downloaded again). Later starts serve in about a minute.

Strata decodes greedily when a request sets no sampling. After the first start, seed
Qwen's thinking-mode defaults into the saved run config:

```bash
bash docker/strata/seed-sampling.sh
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

## Updating

Change the image tag in the entry (and `seed-sampling.sh`'s default), then
`docker pull` it. The volume keeps the pack, preset and run config. A new engine can
make an old tuning result less useful; upstream suggests one start with
`-e TUNE=quick -e RETUNE=1`. See upstream `docs/DOCKER.md` for every setting.
