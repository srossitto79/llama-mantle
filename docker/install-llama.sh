#!/bin/bash
# Install llama.cpp - clone, build, and install binaries
# Usage: BACKEND=cuda|vulkan BUILD_PROFILE=all ./install-llama.sh <commit_hash>
set -euo pipefail

COMMIT_HASH="${1:-master}"
BACKEND="${BACKEND:-cuda}"
BUILD_PROFILE="${BUILD_PROFILE:-all}"
LLAMA_REPO_URL="https://github.com/srossitto79/llama.cpp.git"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/llama-targets.sh"
TARGET_OUTPUT="$(llama_targets_for_profile "${BUILD_PROFILE}")"
mapfile -t TARGETS <<< "${TARGET_OUTPUT}"

mkdir -p /install/bin

# Clone and checkout (init-based so cache-mounted /src/llama.cpp/build dir doesn't break clone)
echo "=== Cloning llama.cpp at ${COMMIT_HASH} ==="
mkdir -p /src/llama.cpp
cd /src/llama.cpp
if [ ! -d .git ]; then
    git init
    git remote add origin "${LLAMA_REPO_URL}"
fi
git fetch --depth=1 origin "${COMMIT_HASH}"
git checkout FETCH_HEAD

# Common cmake flags
CMAKE_FLAGS=(
    -DGGML_NATIVE=OFF
    -DBUILD_SHARED_LIBS=OFF
    -DCMAKE_BUILD_TYPE=Release
    -DCMAKE_C_COMPILER_LAUNCHER=ccache
    -DCMAKE_CXX_COMPILER_LAUNCHER=ccache
    -DLLAMA_BUILD_TESTS=OFF
)

if [ "$BACKEND" = "cuda" ]; then
    CMAKE_FLAGS+=(
        -DGGML_CUDA=ON
        -DGGML_VULKAN=OFF
        "-DCMAKE_CUDA_ARCHITECTURES=${CMAKE_CUDA_ARCHITECTURES:?CMAKE_CUDA_ARCHITECTURES must be set}"
        "-DCMAKE_CUDA_FLAGS=-allow-unsupported-compiler"
        "-DCMAKE_EXE_LINKER_FLAGS=-Wl,-rpath-link,/usr/local/cuda/lib64/stubs -lcuda"
    )
elif [ "$BACKEND" = "vulkan" ]; then
    CMAKE_FLAGS+=(
        -DGGML_CUDA=OFF
        -DGGML_VULKAN=ON
    )
fi

rm -rf build/CMakeCache.txt build/CMakeFiles 2>/dev/null || true

echo "=== Building llama.cpp for ${BACKEND} (${BUILD_PROFILE} profile) ==="
cmake -B build "${CMAKE_FLAGS[@]}"
cmake --build build --config Release -j"${MAX_BUILD_JOBS:-4}" --target "${TARGETS[@]}"

for bin in "${TARGETS[@]}"; do
    if [ ! -f "build/bin/$bin" ]; then
        echo "FATAL: $bin not found in build/bin/" >&2
        exit 1
    fi
    cp "build/bin/$bin" "/install/bin/"
done
echo "=== llama.cpp build complete ==="
ls -la /install/bin/
