# Intelligence suite

The Intelligence pages use the companion backend from the **Measure Model Intelligence** repository. The companion owns benchmark runs and files; Mantle supplies native pages and an API proxy.

## Run with Compose

Place the companion checkout beside this repository as `../Measure Model Intelligence`, or set `MMI_SOURCE_DIR` to its path. Both checkouts must include the integration changes.

```sh
docker compose build intelligence llama-swap
docker compose up -d intelligence llama-swap
```

The existing Mantle Compose configuration still controls models, GPU access, and ports. Open Studio → Intelligence → Run Suite. Models are discovered from `http://llama-swap:8080/v1/models`. If Mantle requires an API key, set `MMI_MODEL_API_KEY` for the companion.

The companion image includes Python/Pillow, Node and the Jest harness, Go, Rust, C++, and the Docker CLI. Its base image is pinned by digest; apt packages and transitive npm versions are resolved during build. Dataset preparation happens only when requested, not during image build. Use the download buttons before external profiles; the suite is rebuilt after successful preparation and at container startup.

The `iron-tensor` profile is offered only when all of its required external tasks have been prepared. Built-in profiles remain available on a fresh installation.

## Run Mantle outside Docker

Build the image in the companion checkout, then start it with a loopback port:

```sh
docker build -t measure-model-intelligence:local .
docker run -d --name mantle-intelligence --init \
  -p 127.0.0.1:8000:8000 \
  --add-host host.docker.internal:host-gateway \
  -e MMI_MODEL_BASE_URL=http://host.docker.internal:9292/v1 \
  -v mantle-intelligence-data:/data \
  -v mantle-intelligence-benchmarks:/app/benchmarks \
  -v /var/run/docker.sock:/var/run/docker.sock \
  measure-model-intelligence:local
```

Change port 9292 to your Mantle listener port. Set `LLAMA_INTELLIGENCE_URL=http://127.0.0.1:8000` in Mantle's environment and restart Mantle. In PowerShell, set it with `$env:LLAMA_INTELLIGENCE_URL = "http://127.0.0.1:8000"` before launching the binary. Linux Docker daemon/socket access is required, including when using Docker Desktop on Windows.

For custom providers, mount your model JSON and set `MMI_CONFIG` to its container path. Omit `MMI_MODEL_BASE_URL` to disable automatic discovery. Never use `localhost` in a model URL to refer to another container or the host.

## Data and execution

- `/data/results/runs/<uuid>/`: metadata, suite snapshot/hash, and per-model JSON for each invocation.
- `/data/results/raw/`: latest model artifacts, retained for compatibility and answer reuse.
- `/data/models.json`: discovered model configuration, unless `MMI_CONFIG` overrides it.
- `/app/benchmarks`: fetched tasks, manifests, and harness metadata.
- SWE-bench images: the host Docker daemon's image store.

Back up both named volumes. Recreating the container rebuilds the suite from the persisted cache. The original dashboard's `/api/results` remains a latest-per-model view; native Results uses `/api/runs` and `/api/runs/<uuid>`.

One benchmark invocation or dataset preparation runs at a time. Models within an invocation run sequentially. Other Studio jobs and interactive inference are not reserved by this scheduler; avoid concurrent GPU-heavy work when collecting comparable scores.

Stop is cooperative: requests and code tests may finish before the next checkpoint. Finished items are persisted. Restarted services mark incomplete sessions as interrupted. To reuse matching answers, enable the advanced reuse option when starting another run; it uses latest-per-model results, not an arbitrary historical run.

The Docker socket is an accepted trusted-service dependency. The companion can manage the host daemon, and SWE-bench containers are siblings, not nested Docker daemons. Ordinary cleanup removes each SWE-bench container in a `finally` block; a forced service termination can leave an `mmi-swe-*` container requiring manual removal. A separate executor is deferred.

## Validation

In Mantle: `go test -v -run TestIntelligence_ ./internal/mantle`, `make test-all`, and `make test-ui`.

In the companion: `python -m unittest discover -s dashboard -p test_studio.py -v`. The lifecycle test runs the actual benchmark runner against a local test endpoint and verifies saved history. It does not require a GPU or paid model provider.

Build the companion image and verify `/api/health`, model discovery, a small run, and a dataset preparation operation before deploying a new image. Pin both repository revisions when publishing a release.
