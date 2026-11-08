#!/bin/bash
# Build the PolyStrata image (strata-poly engine only) from a pinned upstream commit.
# Run inside WSL from the repo root: bash docker/polystrata/build.sh
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_dir="${POLYSTRATA_SOURCE:-$project_dir/build/polystrata-source}"
# v0.1.45-poly.8
ref=4de79331b49e553406860470fa3e96b5256e2e50
image="${POLYSTRATA_IMAGE:-polystrata:4de7933}"
if [[ ! -d "$source_dir/.git" ]]; then
    git clone --no-checkout https://github.com/simplexai-labs/PolyStrata.git "$source_dir"
fi
if ! git -C "$source_dir" cat-file -e "$ref^{commit}" 2>/dev/null; then
    git -C "$source_dir" fetch --depth 1 origin "$ref"
fi
git -c core.filemode=false -C "$source_dir" checkout --quiet --detach "$ref"
docker build -f "$project_dir/docker/polystrata/Dockerfile" \
    --build-arg "CUDA_ARCHITECTURES=${CUDA_ARCHITECTURES:-86}" \
    -t "$image" "$source_dir"
