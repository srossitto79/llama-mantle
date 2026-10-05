# Strata on demand

Swift 1.5 Flash Next IQ3_XXS, 128K context, INT8 KV, CPU vision, GPU 0.
llama-swap starts `strata-swift` when selected and stops it on unload or idle expiry.
Its OpenAI API stays on the Compose network; no host port is published.
`STRATA_ALLOWED_HOSTS` includes `strata-swift`, `llama-swap`, `turistation` and
`turistation.local` for health checks, requests through the Compose service name,
and requests using this PC's name. Localhost and IP
addresses are accepted by Strata already. Add any other hostname you use to this
list when adapting the entry for another host.

From Ubuntu/WSL, with this repository as the working directory:

```bash
bash docker/strata/build.sh
bash docker/strata/prepare.sh
```

The build pins Strata source to `6f32ec070f23ced9f50e704d854d775da52591ab`.
It uses the pinned HyperQwen image's CUDA 13 development tools as its base, then
compiles Strata and its image encoder using upstream's build procedure. HyperQwen
is not started inside this image. Set `CUDA_ARCHITECTURES` when building for a GPU
other than the RTX 3090 (default `86`). The host needs NVIDIA driver 580 or newer.

Preparation reuses both existing GGUF shards from
`/home/sross/gguf_models/ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF`.
Override `STRATA_GGUF_DIR` for another Docker host model directory. The bind mount
is writable because upstream setup writes small `.done` markers beside the GGUFs.
It keeps prepared packs, the model-specific vision encoder, MTP tensors and the
install configuration in the `strata-data` volume. Allow roughly 10 GB of extra
space. Interrupted preparation can be run again.

Merge `model.yaml` under `models:` in the live `config.yaml` (already done on this
installation). It uses the same Docker socket and `llama-mantle_default` network
as HyperQwen, so the llama-swap image requires no rebuild or new dependencies.
For another host, adjust the bind source and Compose network in the entry.

Select `swift-1.5-qwen3.8-flash-next-strata-station-128k` in llama-swap or your
OpenAI client. Suffixes `:off`, `:low`, `:medium` and `:high` choose reasoning effort.
The first request after an unload must wait for model loading. Its existing global
health timeout is 1,800 seconds and the idle timeout is 900 seconds.

```bash
docker logs -f strata-swift
curl http://localhost:9292/running
```

To change saved context or vision settings, unload Strata and run, for example,
`REINSTALL=1 CONTEXT=131072 VISION=cpu bash docker/strata/prepare.sh`.
Changing runtime env vars alone does not rewrite the saved configuration. Update
the entry's model ID and display name when changing its context. Keep `strata-data`;
removing it discards the preparation.

KV streaming is explicitly disabled for WSL. Model files stay on the Linux SSD
filesystem, which avoids reading GGUF data through a Windows bind mount.
