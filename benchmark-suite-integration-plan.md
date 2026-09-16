# Benchmark Suite Integration

## Architecture

Studio Svelte pages call `/api/mantle/studio/intelligence/*`. Mantle forwards the supported routes to the companion service configured by `LLAMA_INTELLIGENCE_URL`. The companion runs the original `dashboard/app.py` from the Measure Model Intelligence repository.

The Python backend owns execution, its single-run lock, streaming, dataset preparation, and persisted results. Go does not duplicate the benchmark scheduler or store intelligence scores in Studio evaluation tables. Existing benchmark/perplexity evaluations remain separate.

## Packaging

The companion repository supplies its Dockerfile, entrypoint, and backend changes. `docker-compose.intelligence.yml` connects it to Mantle. Build from a checked-out revision; no repository cloning or dataset fetching occurs in the Docker build. The image includes Python, Pillow, Node/Jest, Go, Rust, C++, and Docker CLI. The base image is pinned by digest; Debian packages and transitive npm dependencies currently resolve at build time.

The accepted deployment uses the host Docker socket. SWE-bench starts sibling containers and transfers files using `docker cp`. Decoupling Docker execution is deferred.

## Native pages

- `/studio/intelligence/run`: discovered models, profiles, advanced settings, dataset preparation, live progress, and cooperative stop.
- `/studio/intelligence/results`: saved runs, profile/date/status text filtering, multi-run comparison, category scores, item diagnostics, and raw JSON downloads.

## Persistence

The companion stores each invocation under `results/runs/<uuid>/`, including run metadata, a suite snapshot/hash, and per-model raw results. Latest-per-model artifacts remain available for the original dashboard and reuse/retry behavior. Interrupted sessions are identified after restart; they are not automatically restarted. Downloads and runs share an exclusion lock so datasets cannot change during execution.

Volumes preserve results/configuration and benchmark datasets. SWE-bench images live in the Docker daemon's image store.

## Operational contract

The Compose service discovers model aliases from Mantle's `/v1/models` endpoint. A configured JSON file remains supported for custom providers and model-specific settings. The backend API remains internal to the Compose network. Mantle forwards streaming responses but removes browser authorization and cookies.

Stop uses the existing runner checkpoints. An in-flight model request or language test can finish before cancellation takes effect. Separate Studio heavy jobs and interactive model traffic are not coordinated with this backend; avoid competing workloads while measuring models.

## Follow-up work

- Optional project associations and links from the general Studio jobs view.
- Historical import for artifacts produced before per-run history existed.
- Stronger runtime dependency locking and image release automation across both repositories.
- A separate execution worker/remote Docker endpoint, if needed later.

See [operations and setup](docs/intelligence.md).
