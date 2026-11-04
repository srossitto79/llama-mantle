"""Record serial OpenAI streaming runs without assuming chunks are tokens.

Uses only the standard library. Reported native timing is kept separate from
client wall throughput; missing usage never becomes an invented token rate.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import time
import urllib.request


def run(args, prompt):
    payload = {
        "model": args.model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": args.max_tokens,
        "temperature": 1.0,
        "top_p": 0.95,
        "seed": 42,
        "stream": True,
        "stream_options": {"include_usage": True},
    }
    if args.reasoning_effort:
        payload["reasoning_effort"] = args.reasoning_effort
    headers = {"Content-Type": "application/json"}
    if args.api_key_env:
        headers["Authorization"] = "Bearer " + os.environ[args.api_key_env]
    request = urllib.request.Request(args.url.rstrip("/") + "/chat/completions",
                                     data=json.dumps(payload).encode(), headers=headers)
    first_output = None
    usage, timings, finish, fragments = {}, {}, None, []
    started = time.perf_counter()
    done = False
    with urllib.request.urlopen(request, timeout=args.timeout) as response:
        for line in response:
            if not line.startswith(b"data:"):
                continue
            data = line[5:].strip()
            if data == b"[DONE]":
                done = True
                break
            event = json.loads(data)
            if event.get("error"):
                raise RuntimeError(str(event["error"]))
            usage = event.get("usage") or usage
            timings = event.get("timings") or timings
            for choice in event.get("choices", []):
                delta = choice.get("delta") or {}
                text = (delta.get("reasoning_content") or delta.get("reasoning") or "") + (delta.get("content") or "")
                if text:
                    if first_output is None:
                        first_output = time.perf_counter() - started
                    fragments.append(text)
                finish = choice.get("finish_reason") or finish
    elapsed = time.perf_counter() - started
    if not done or not fragments or finish is None:
        raise RuntimeError("Incomplete or empty streaming response; refusing a throughput result")
    completion_tokens = usage.get("completion_tokens")
    return {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "label": args.label, "model": args.model, "quant": args.quant,
        "url": args.url, "prompt_sha256": hashlib.sha256(prompt.encode()).hexdigest(),
        "request": payload, "ttft_seconds": first_output, "wall_seconds": elapsed,
        "wall_output_tokens_per_second": completion_tokens / elapsed if completion_tokens else None,
        "usage": usage, "native_timings": timings, "finish_reason": finish,
        "output": "".join(fragments),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True, help="Base URL ending in /v1")
    parser.add_argument("--model", required=True)
    parser.add_argument("--label", required=True)
    parser.add_argument("--quant", required=True, help="Actual checkpoint/quant, including MTP status")
    parser.add_argument("--prompt-file", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True, help="Append-only JSONL evidence")
    parser.add_argument("--api-key-env", help="Environment variable containing the API key")
    parser.add_argument("--reasoning-effort", choices=("none", "low", "medium", "high", "max"))
    parser.add_argument("--max-tokens", type=int, default=512)
    parser.add_argument("--rounds", type=int, default=3)
    parser.add_argument("--timeout", type=float, default=1800)
    args = parser.parse_args()
    if args.rounds < 1 or args.max_tokens < 1 or args.timeout <= 0:
        parser.error("rounds, max-tokens and timeout must be positive")
    prompt = args.prompt_file.read_text(encoding="utf-8")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    for number in range(args.rounds):
        result = run(args, prompt)
        result["round"] = number + 1
        result["cache_state"] = "first-request-uncontrolled" if number == 0 else "repeated-prompt-warm"
        with args.output.open("a", encoding="utf-8") as stream:
            stream.write(json.dumps(result) + "\n")
        print(json.dumps({key: result[key] for key in (
            "label", "round", "ttft_seconds", "wall_seconds", "wall_output_tokens_per_second",
            "usage", "native_timings", "finish_reason", "cache_state")}), flush=True)


if __name__ == "__main__":
    main()
