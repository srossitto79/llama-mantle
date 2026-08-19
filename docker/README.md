# Unified Docker Container

These scripts create a custom llama-swap container that contains:

- llama-server for LLMs, rerank and embedding model support
- sd-server (stable-diffusion.cpp) for image generation
- whisper.cpp for ASR
- Llama Studio's GGUF transformation, training, evaluation, and lifecycle tools

The default `BUILD_PROFILE=all` is the power-user Studio image. It includes supported
runtime, core, training, evaluation, and advanced tools. Examples, PoCs, desktop apps,
documentation generators, and deprecated wrappers are excluded from every profile.

Build and run an NVIDIA image:

```shell
./docker/build-image.sh --cuda

docker run --rm -it --gpus all \
  -p 127.0.0.1:9292:8080 \
  -v /path/to/models:/models \
  -v /path/to/config.yaml:/etc/llama-swap/config/config.yaml \
  llama-swap:unified-cuda
```

The image uses `/models` for models, datasets, outputs, backend builds, and the default
file-backed Studio database configured by `config.example.yaml`. Preserve that volume.

See [Getting started with Llama Studio](../docs/llama-studio-getting-started.md) for
first-run workflows, LAN safety, storage and recovery, and the release-testing checklist.
