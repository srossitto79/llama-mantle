# PolyStrata (github.com/simplexai-labs/PolyStrata) with only its strata-poly engine,
# for GLM-5.3-Flash-family models (llama.cpp's glm5-next) such as Groot-238B-A18B.
#
# Upstream's Dockerfile compiles only the Qwen engine; strata-poly would then be
# compiled by setup.py on the first GLM start, inside a container that llama-swap
# runs with --rm, so on every start. This image compiles strata-poly at build time
# instead, for the architectures given, with no image encoder (the GLM engine reads
# no pictures). Build with build.sh, which pins the source commit.
FROM nvidia/cuda:13.0.0-devel-ubuntu24.04

# STRATA_EXECV=1: setup.py replaces itself with the server, so the server is PID 1
# and docker stop's SIGTERM reaches it.
ENV DEBIAN_FRONTEND=noninteractive PYTHONUNBUFFERED=1 LANG=C.UTF-8 STRATA_EXECV=1

RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential ca-certificates curl git libatomic1 libgomp1 \
        python3 python3-pip python3-venv unzip \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/strata
COPY . .

RUN python3 -m venv .venv \
    && .venv/bin/pip install --no-cache-dir --upgrade pip \
    && .venv/bin/pip install --no-cache-dir -r requirements.txt \
    && chmod +x setup.sh docker-entrypoint.sh

# Semicolon-separated compute capabilities; 86 is the RTX 3090.
ARG CUDA_ARCHITECTURES=86

# setup.build_engine_poly writes engine-poly/strata-poly and the BUILD.json stamp
# that setup.py checks later, so a start finds the engine current and does not
# compile again. The build machine's CPU features are compiled in: build on the
# PC that runs it.
RUN .venv/bin/python - <<'PYEOF'
import os
import setup

archs = [int(a) for a in os.environ["CUDA_ARCHITECTURES"].replace(",", ";").split(";") if a.strip()]
setup.build_engine_poly({"arch": archs[0], "archs": archs}, True, setup.get_llama_cpp(), vision="none")
PYEOF

# Keep the image small, but fail the build if the engine needs a library from the
# CMake tree being removed. libcuda.so.1 is the driver's: the NVIDIA container
# runtime mounts it at start, so it is absent at build time by design.
RUN rm -rf build-poly \
    && if ldd engine-poly/strata-poly | grep -v "libcuda.so.1" | grep -q "not found"; then \
         ldd engine-poly/strata-poly; echo "strata-poly is missing libraries" >&2; exit 1; \
       fi

VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=600s --retries=3 \
  CMD curl -fs "http://127.0.0.1:${PORT:-8080}/health" || exit 1

ENTRYPOINT ["./docker-entrypoint.sh"]
