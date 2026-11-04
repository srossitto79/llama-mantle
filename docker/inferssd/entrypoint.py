"""Experimental Docker-owned native launch; no upstream systemd launcher patch."""

import argparse
import csv
import json
import os
from pathlib import Path
import resource
import sys
import tempfile

from inferssd.source import gguf_alias, header


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, help="First GGUF shard inside /models")
    parser.add_argument("--context", type=int, default=16384)
    parser.add_argument("--threads", type=int, default=8)
    parser.add_argument("--inspect", action="store_true", help="Read headers only, without GPU allocation")
    args = parser.parse_args()
    if args.context < 1 or args.threads < 1:
        parser.error("context and threads must be positive")

    model = Path(args.model).resolve(strict=True)
    paths = [Path(path) for path in header.shards(str(model))]
    first = header.read(str(model))
    architecture = first.kv.get("general.architecture")
    if architecture not in ("qwen4exp", "deepseek41"):
        raise ValueError(f"Unsupported race architecture: {architecture!r}")
    # Strict native alias validation checks every shard and payload range, reading headers only.
    rows, summary = gguf_alias.build_rows(paths, {"up", "gate", "down"}, None, False)
    types = summary["by_type"]
    print(json.dumps({"architecture": architecture, "model": str(model),
                      "expert_types": types, "payload_gib": summary["total_payload_gib"]}), flush=True)
    if architecture == "deepseek41" and set(types) != {"MXFP4"}:
        raise ValueError("InferSSD DeepSeek SSD experts require MXFP4; these weights cannot race on this backend")
    if args.inspect:
        return

    # Docker --memory and --memory-swap set to the same value supply a finite,
    # swap-free cgroup. Do not silently fall back to unlimited host memory.
    cgroup = Path("/sys/fs/cgroup")
    memory_max = (cgroup / "memory.max").read_text().strip()
    swap_max = (cgroup / "memory.swap.max").read_text().strip()
    if memory_max == "max" or int(memory_max) < 4 * 1024**3 or swap_max != "0":
        raise ValueError("Use Docker --memory >=4g and an equal --memory-swap (swap.max must be 0)")
    if resource.getrlimit(resource.RLIMIT_MEMLOCK)[0] != resource.RLIM_INFINITY:
        raise ValueError("Use Docker --ulimit memlock=-1:-1")

    state = Path("/state")
    state.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix="run-", dir=state))
    alias = run / "experts.tsv"
    with alias.open("w", newline="", encoding="utf-8") as stream:
        writer = csv.DictWriter(stream, fieldnames=gguf_alias.FIELDS, delimiter="\t")
        writer.writeheader()
        writer.writerows(rows)
    (run / "source.json").write_text(json.dumps(summary, indent=2) + "\n")
    settings = run / "settings.ssd"
    settings.write_text(
        "inferssd-settings 1\n"
        f"expert.source = {json.dumps([str(alias)])}\n"
        "vram.budget = auto\n"
        "host.headroom_mib = 4096\n"
        + ("model.engram_prefetch = 1\n" if architecture == "deepseek41" else "")
    )
    command = [
        "/opt/inferssd/native-bin/inferssd-server", "-m", str(model),
        "--ssd-settings", str(settings), "--ssd-telemetry", str(run / "native-events.jsonl"),
        "--mmap", "--no-repack", "--fit", "off", "--no-warmup", "--defer-experts",
        "-ngl", "99", "-c", str(args.context), "-b", "512", "-ub", "512",
        "-t", str(args.threads), "-tb", str(args.threads), "-fa", "on", "-np", "1",
        "--host", "0.0.0.0", "--port", "8080", "--alias", "inferssd-race",
        "--cache-ram", "0", "--ctx-checkpoints", "1",
    ]
    if architecture == "deepseek41":
        command += ["--chat-template-file", "/opt/inferssd/engine/models/templates/deepseek-ai-DeepSeek-V4.1.jinja",
                    "--chat-template-kwargs", '{"drop_thinking":false}',
                    "--temp", "1.0", "--top-p", "0.95", "--top-k", "0", "--min-p", "0"]
    (run / "launch.json").write_text(json.dumps({"argv": command, "memory_max": memory_max,
                                               "mode": "docker-native-manual-settings"}, indent=2) + "\n")
    print(f"Run evidence: {run}; manual settings, no upstream machine autotuning", flush=True)
    os.execv(command[0], command)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError) as error:
        sys.exit(str(error))
