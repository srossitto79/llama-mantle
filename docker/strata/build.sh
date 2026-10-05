#!/bin/bash
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_dir="$project_dir/build/strata-source"
ref=6f32ec070f23ced9f50e704d854d775da52591ab
image="${STRATA_IMAGE:-strata:6f32ec0}"
if [[ ! -d "$source_dir/.git" ]]; then
    git clone --no-checkout https://github.com/Niko1221/Strata.git "$source_dir"
fi
if ! git -C "$source_dir" cat-file -e "$ref^{commit}" 2>/dev/null; then
    git -C "$source_dir" fetch --depth 1 origin "$ref"
fi
git -c core.filemode=false -C "$source_dir" checkout --quiet --detach "$ref"
cp "$project_dir/docker/strata/entrypoint.sh" "$source_dir/llama-swap-entrypoint.sh"
docker build -f "$project_dir/docker/strata/Dockerfile" \
    --build-arg "CUDA_ARCHITECTURES=${CUDA_ARCHITECTURES:-86}" \
    -t "$image" "$source_dir"
