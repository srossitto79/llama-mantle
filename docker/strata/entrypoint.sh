#!/bin/sh
set -eu
cd /opt/strata

# Keep the fixed model's config alongside its packs and draft tensors.
data="${STRATA_DATA:-/data}"
cfg="$data/config/strata-swift-iq3_xxs.json"
gguf_dir="${GGUF_DIR:-/models/ukisai_Swift-1.5-Qwen3.8-Flash-Next-GSQ-RCO-GGUF}"
mkdir -p "$data/config"
if [ ! -f "$cfg" ] || [ "${REINSTALL:-0}" = 1 ]; then
    .venv/bin/python setup.py --setup --yes --no-start --no-browser \
        --family swift --model IQ3_XXS --gguf-dir "$gguf_dir" \
        --data-dir "$data" --context "${CONTEXT:-131072}" \
        --vision "${VISION:-cpu}" --kv int8 --kv-streaming off --low-ram off \
        --gpu "${GPU:-0}" --host 0.0.0.0 --port "${PORT:-8080}"
    cp strata-swift-iq3_xxs.json "$cfg"
fi
ln -sf "$cfg" strata-swift-iq3_xxs.json
if [ "${PREPARE_ONLY:-0}" = 1 ]; then
    echo "Strata preparation complete; ready for llama-swap."
    exit 0
fi
# STRATA_EXECV makes the server PID 1, so docker stop reaches it directly.
exec .venv/bin/python setup.py --no-browser --gpu "${GPU:-0}" --port "${PORT:-8080}"
