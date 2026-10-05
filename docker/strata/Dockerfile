# Strata source is the build context supplied by build.sh.
# Reuse HyperQwen's CUDA 13 development base, already present on this host.
ARG STRATA_BASE=ghcr.io/syv-ai/hyperqwen@sha256:34c2e3a408e2a6ec94d3c6434ff14550b4525301f1ad6c06e40e409db1eefd55
FROM ${STRATA_BASE}
USER root
ENV DEBIAN_FRONTEND=noninteractive PYTHONUNBUFFERED=1 LANG=C.UTF-8 STRATA_EXECV=1
WORKDIR /opt/strata
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates curl git libatomic1 libgomp1 \
    python3 python3-pip python3-venv unzip \
    && rm -rf /var/lib/apt/lists/*
COPY . .
RUN python3 -m venv .venv \
    && .venv/bin/pip install --no-cache-dir -r requirements.txt
# HyperQwen ships cuBLAS in its Python environment rather than the toolkit tree.
# Make its headers/libraries visible to CMake's CUDA toolkit discovery.
RUN for header in /app/venv/lib/python3.12/site-packages/nvidia/cu13/include/cublas*.h; do \
        ln -s "$header" "/usr/local/cuda/include/$(basename "$header")"; \
    done \
    && ln -s /app/venv/lib/python3.12/site-packages/nvidia/cu13/lib/libcublas.so.13 /usr/local/cuda/lib64/libcublas.so \
    && ln -s /app/venv/lib/python3.12/site-packages/nvidia/cu13/lib/libcublasLt.so.13 /usr/local/cuda/lib64/libcublasLt.so
ENV LD_LIBRARY_PATH=/app/venv/lib/python3.12/site-packages/nvidia/cu13/lib:/usr/local/cuda/lib64:/usr/local/nvidia/lib:/usr/local/nvidia/lib64
ARG CUDA_ARCHITECTURES=86
# Compile and stamp the engine exactly as upstream's Dockerfile does.
RUN .venv/bin/python - <<'PYEOF'
import json, os, pathlib, shutil
import setup
llama = setup.get_llama_cpp()
nvcc, _ = setup.find_nvcc()
arch = os.environ['CUDA_ARCHITECTURES']
setup.cmake_build(setup.ROOT, setup.ROOT / 'build', 'strata',
    ['-DSTRATA_ENABLE_CUDA=ON', '-DSTRATA_BUILD_TESTS=OFF',
     f'-DCMAKE_CUDA_ARCHITECTURES={arch}', f'-DCMAKE_CUDA_COMPILER={nvcc}',
     f'-DSTRATA_GGML_DIR={llama}'], None, '')
setup.cmake_build(setup.ROOT / 'tools' / 'vision', setup.ROOT / 'build-vision', 'strata-vision',
    [f'-DLLAMA_DIR={llama}', '-DSTRATA_VISION_CUDA=ON',
     f'-DCMAKE_CUDA_ARCHITECTURES={arch}', f'-DCMAKE_CUDA_COMPILER={nvcc}'], None, '')
eng = setup.ROOT / 'engine'
eng.mkdir(exist_ok=True)
shutil.copy2(setup.ROOT / 'build' / setup.EXE, eng / setup.EXE)
shutil.copy2(setup.ROOT / 'build-vision' / 'bin' / setup.VEXE, eng / setup.VEXE)
bindir = pathlib.Path(nvcc).parent
meta = {'source': 'local', 'version': setup.source_version(),
        'archs': [int(a) for a in arch.split(';')], 'vision': 'gpu',
        'cuda_dirs': [str(d) for d in (bindir, bindir.parent / 'lib64') if d.is_dir()],
        'src': setup.source_hash(setup.ENGINE_SOURCES),
        'vision_src': setup.source_hash(setup.VISION_SOURCES)}
(eng / 'BUILD.json').write_text(json.dumps(meta, indent=1))
PYEOF
RUN rm -rf build build-vision
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=600s --retries=3 \
    CMD curl -fsS "http://127.0.0.1:${PORT:-8080}/health" || exit 1
ENTRYPOINT ["/bin/sh", "/opt/strata/llama-swap-entrypoint.sh"]
