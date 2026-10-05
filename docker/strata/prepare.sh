#!/bin/bash
set -euo pipefail
# This prepares files only; it does not load the inference engine onto the GPU.
model_dir="${STRATA_GGUF_DIR:-/home/sross/gguf_models/ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF}"
docker run --rm --name strata-prepare --gpus all --ulimit memlock=-1 \
    --mount "type=bind,source=$model_dir,target=/models/ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF" \
    -v strata-data:/data -e PREPARE_ONLY=1 -e GPU=0 \
    -e "REINSTALL=${REINSTALL:-0}" -e "CONTEXT=${CONTEXT:-131072}" \
    -e "VISION=${VISION:-cpu}" \
    "${STRATA_IMAGE:-strata:6f32ec0}"
