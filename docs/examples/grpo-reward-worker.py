#!/usr/bin/env python3
"""Example persistent Llama Studio GRPO reward worker.

Read one JSON object per line from stdin and write exactly one JSON response line
to stdout. Diagnostic messages belong on stderr so they cannot corrupt the
protocol. Replace score() with task-specific verification.
"""

import json
import sys


def score(request: dict) -> dict:
    reference = str(request.get("reference", "")).strip().casefold()
    generations = request.get("generations", [])
    rewards = [
        1.0 if str(generation).strip().casefold() == reference else 0.0
        for generation in generations
    ]
    return {
        "rewards": rewards,
        "details": [{"exact_match": reward == 1.0} for reward in rewards],
    }


for line in sys.stdin:
    try:
        request = json.loads(line)
        response = score(request)
        print(json.dumps(response, separators=(",", ":")), flush=True)
    except Exception as exc:  # Report a protocol-shaped error and log details.
        print(f"reward worker error: {exc}", file=sys.stderr, flush=True)
        print(json.dumps({"error": str(exc), "rewards": []}), flush=True)
