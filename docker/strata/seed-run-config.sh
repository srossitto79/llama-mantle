#!/bin/bash
set -euo pipefail
# Strata decodes greedily when neither the request nor the run config sets
# sampling, and its reasoning-loop guard is off by default. Seed Qwen's
# thinking-mode sampling and "recover" loop handling into the run config in
# strata-data, keeping any value already there. Strata rewrites the run config
# when its settings change (e.g. GPU), so run this again after that.
# Needs no GPU and can run while Strata is stopped.
docker run --rm -i -v strata-data:/data \
    --entrypoint /opt/strata/terminal/python/bin/python3 \
    "${STRATA_IMAGE:-ghcr.io/niko1221/strata:0.2.7-cuda13}" - <<'PYEOF'
import json
import pathlib

configs = sorted(pathlib.Path("/data/install").glob("strata-*.json"))
if not configs:
    raise SystemExit("No run config in strata-data yet; start Strata once first.")
for path in configs:
    config = json.loads(path.read_text())
    seeded = []
    if not isinstance(config.get("sampling"), dict):
        config["sampling"] = {
            "temperature": 1.0,
            "top_p": 0.95,
            "top_k": 20,
            "min_p": 0.0,
            "presence_penalty": 0.0,
            "frequency_penalty": 0.0,
            "repetition_penalty": 1.0,
        }
        seeded.append("sampling")
    # Low-bit quants repeat whole reasoning passages more often; "recover"
    # continues with the low-effort instruction once a loop is detected.
    if not config.get("reasoning_loop_recovery"):
        config["reasoning_loop_recovery"] = "recover"
        seeded.append("reasoning_loop_recovery")
    if not seeded:
        print(f"{path.name}: already set, left unchanged")
        continue
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(config, indent=2) + "\n")
    temporary.replace(path)
    print(f"{path.name}: seeded {', '.join(seeded)}")
PYEOF
