#!/bin/bash
set -euo pipefail
# Strata decodes greedily when neither the request nor the run config sets
# sampling. Seed Qwen's thinking-mode parameters into the run config in
# strata-data, keeping any sampling block already there. Strata rewrites the
# run config when its settings change (e.g. GPU), so run this again after that.
# Needs no GPU and can run while Strata is stopped.
docker run --rm -v strata-data:/data \
    --entrypoint /opt/strata/terminal/python/bin/python3 \
    "${STRATA_IMAGE:-ghcr.io/niko1221/strata:0.2.3-cuda13}" - <<'PYEOF'
import json
import pathlib

configs = sorted(pathlib.Path("/data/install").glob("strata-*.json"))
if not configs:
    raise SystemExit("No run config in strata-data yet; start Strata once first.")
for path in configs:
    config = json.loads(path.read_text())
    if isinstance(config.get("sampling"), dict):
        print(f"{path.name}: sampling already set, left unchanged")
        continue
    config["sampling"] = {
        "temperature": 1.0,
        "top_p": 0.95,
        "top_k": 20,
        "min_p": 0.0,
        "presence_penalty": 0.0,
        "frequency_penalty": 0.0,
        "repetition_penalty": 1.0,
    }
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(config, indent=2) + "\n")
    temporary.replace(path)
    print(f"{path.name}: sampling seeded")
PYEOF
