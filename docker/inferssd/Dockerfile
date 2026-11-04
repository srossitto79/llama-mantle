# Experimental native-server deployment: Docker owns the cgroup instead of systemd.
FROM nvidia/cuda:12.9.1-devel-ubuntu24.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates cmake git ninja-build python3 python3-venv \
    libcurl4-openssl-dev libssl-dev liburing-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

ARG INFERSSD_REV=93e76a24a225a51391ec7b4a93daf179973c067e
ARG CUDA_ARCHITECTURES=86
ARG BUILD_JOBS=4
RUN git init /opt/inferssd && cd /opt/inferssd \
    && git remote add origin https://github.com/wici-ai/inferssd.git \
    && git fetch --depth 1 origin "${INFERSSD_REV}" \
    && git checkout --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${INFERSSD_REV}"
WORKDIR /opt/inferssd
RUN --mount=type=cache,id=mantle-inferssd-cuda-build,target=/opt/inferssd/build-linux-cuda \
    mkdir -p /opt/cuda-link && ln -sf /usr/local/cuda/lib64/stubs/libcuda.so /opt/cuda-link/libcuda.so.1 \
    && cmake --preset linux-cuda \
    -DCMAKE_CUDA_ARCHITECTURES="${CUDA_ARCHITECTURES}" \
    -DCMAKE_BUILD_RPATH_USE_ORIGIN=ON \
    -DCMAKE_EXE_LINKER_FLAGS=-Wl,-rpath-link,/opt/cuda-link \
    -DGGML_NATIVE=ON -DLLAMA_BUILD_TESTS=OFF \
    && cmake --build build-linux-cuda --parallel "${BUILD_JOBS}" \
       --target inferssd-server inferssd-cpuprobe inferssd-ssdprobe inferssd-memprobe \
    && mkdir -p /opt/inferssd/native-bin && cp -a build-linux-cuda/bin/. /opt/inferssd/native-bin/
RUN python3 -m venv /opt/venv \
    && /opt/venv/bin/pip install --no-cache-dir ./framework
ENV PATH="/opt/venv/bin:${PATH}"
COPY entrypoint.py /usr/local/bin/inferssd-container
ENTRYPOINT ["python3", "/usr/local/bin/inferssd-container"]
