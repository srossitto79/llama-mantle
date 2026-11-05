# Reasoning loop guard in the proxy — build brief

Brief for an agent starting cold. Read `AGENTS.md` first; its testing, formatting and commit rules apply.

## Goal

Add a per-model, opt-in guard to llama-swap that notices when a model's **reasoning** starts repeating whole
passages, stops that generation, and continues the same reply in a way that breaks the loop. The client sees one
normal response (streamed or not). It must work for any OpenAI-compatible backend served through llama-swap
(llama.cpp first). Strata already does this inside its server and must not get it twice.

Background: on Groot-65B-A6B GyroM (Strata, `reasoning_loop_recovery: "recover"`), 18 of 201 benchmark requests
had their reasoning restarted, clustered on the harder tests. llama.cpp models (e.g. `groot-65b-a6b-station-128k`)
have no such guard and burn their whole token budget when they loop.

## Reference behaviour: what Strata does

Source: `/opt/strata/terminal/serve/server.py` in `ghcr.io/niko1221/strata:0.2.9-cuda13` (engine 0.1.55). To read it:

```bash
wsl -d Ubuntu-24.04 -- bash -c "docker run --rm --entrypoint cat ghcr.io/niko1221/strata:0.2.9-cuda13 /opt/strata/terminal/serve/server.py > /tmp/strata-server.py"
```

Relevant parts: constants around line 226–236, `reasoning_repeat_coverage` (~238), `focused_recovery_prompt` (~265),
the check in the generation loop (~5720), the recovery step (~5797).

**Detection** (`reasoning_repeat_coverage`), run only on reasoning text, at most every 512 generated tokens, only
while the model is still in the reasoning section and at a clean token boundary:

1. Tail check: if the last 2,048 chars (lowercased) are periodic with a period of 1–64 chars
   (`tail[p:] == tail[:-p]`), coverage = 1.0. Catches one-"word" loops such as a cycling digit string.
2. Otherwise split the lowercased reasoning into tokens with `\w+|[^\w\s]`, keep the last 30,000.
   If fewer than 2,000, coverage = 0.
3. Build all 12-token n-grams over that history and count them.
4. Over the last 2,000 tokens, mark every position covered by an n-gram whose count is ≥ 3.
5. coverage = covered positions / 2,000. Loop when coverage ≥ 0.25.

**Recovery**, at most once per request, never for requests with images:

- New prompt = original prompt with the system message's xhigh effort sentence replaced by the low-effort one
  (a token-id splice, no re-tokenisation), followed by **every token generated so far**, loop included.
  Nothing is inserted between them; no `</think>` is added.
- Sampling for the rest of the reply: temperature = max(1.0, request), presence_penalty = max(1.5, request);
  top_p / top_k untouched.
- Generation continues into the same client stream. Log line:
  `[strata] repeated reasoning detected at N tokens (coverage=X); resuming the same output with the low-effort instruction`.
- Mode `"stop"` instead ends the reply there with finish_reason `length`.

Strata also has a wrap-up sentence used for its thinking budget:
`"\n\nI have thought about this long enough; time to give my answer.\n</think>\n\n"`.

## Proposed design

### Where it lives

`internal/server`, as a new middleware in `modelChain` (`internal/server/server.go`, `routes()`), placed **after**
`CreateMetricsMiddleware` so metrics and captures see the merged, client-facing response. It acts only on
`POST /v1/chat/completions` with a JSON body, for models whose loop guard is enabled. Look at
`internal/server/filters.go` for the pattern (body buffering, `shared.FetchContext`, Content-Length cleanup) and at
`internal/server/metrics.go` (`responseBodyCopier`, `processStreamingResponse`) for SSE handling.

### Contracts first (user's rule: interface before implementation, small role-specific interfaces)

Define these before writing any implementation; keep implementations unexported.

- `LoopDetector` — fed reasoning text incrementally; reports `(looping bool, coverage float64)` when asked at a
  check point. Pure, no I/O. Implementation: the Strata algorithm above.
- `ReplyContinuer` — given the original request body, the reasoning generated so far, and the recovery policy,
  returns the upstream request that continues the reply, plus a decoder that turns the upstream's response into
  chat-completion deltas. One implementation per continuation strategy (see spike below).
- `StreamSplicer` (or similar) — writes the client-facing stream: first upstream's chunks until the cut, then the
  continuation's chunks rewritten to the same `id` / `model` / `created`, with one final `usage` that sums both
  completions and counts the first prompt once. Non-streaming requests: the guard sends `stream: true` upstream
  (with `stream_options.include_usage`) and assembles one normal JSON response at the end.

### Config

Per model, under the model entry (`internal/config/model_config.go`), default off:

```yaml
reasoningLoopGuard:
  mode: recover          # off | stop | recover
  recovery: wrapUp       # wrapUp | lowEffort
  coverage: 0.25         # optional overrides of the Strata constants
  checkEvery: 512        # tokens (approximate from deltas / usage) between checks
```

Validate on load; reject the guard on models whose `proxy` points at a Strata server only if there is a reliable way
to tell, otherwise just document "leave it off for Strata models". Update `docs/configuration.md` and the
config schema/examples the repo keeps alongside it.

### Recovery policy — decide with data, default `wrapUp`

- `lowEffort` (Strata's approach): resend with `chat_template_kwargs.reasoning_effort: low` (let the template render
  the low sentence — no prompt splicing in Go). Cost on llama.cpp: the system prompt changes, so the **whole prompt
  is processed again** (no prefix-cache reuse). On long agent prompts with Groot Q4_K_M that is minutes.
- `wrapUp` (cache-friendly): keep the prompt identical, append the reasoning so far plus a short closing sentence
  (Strata's wrap-up text above, or a softer "I am repeating myself; let me settle on an answer.") inside the
  reasoning, and continue. The prefix cache is reused.
- Both raise temperature to ≥ 1.0 and presence_penalty to ≥ 1.5 for the continuation, like Strata.

### Spike first (unverified — do this before building the continuer)

1. **Chat prefill**: does the llama.cpp build in `/backends/qwen4exp_mtp/llama-server` continue a final
   `assistant` message that carries `reasoning_content` (and no `content`) **inside** the reasoning, and does the
   Groot / Qwen3.8 chat template keep that reasoning in the rendered prompt? Check with `POST /apply-template`
   (renders without generating) and one short generation. If yes: strategy A, `ReplyContinuer` stays on
   `/v1/chat/completions` and llama.cpp keeps parsing reasoning, content and tool calls.
2. If not: strategy B, render with `/apply-template`, append the reasoning text, call `/completion` (raw), and parse
   `</think>`, content and tool calls in Go. Much more code and model-specific; stop and report to the user before
   going down this path.
3. Measure the cost of each policy on a long prompt (prefill time with and without cache reuse).

### Rules carried over from Strata

- Check only while the reply is still in reasoning (no `content` or `tool_calls` delta seen yet).
- Once per request. A second loop after recovery → let it run (or `stop` if configured).
- Skip requests with images by default (a resend re-encodes them).
- Respect client disconnects and llama-swap's in-flight cancel (`handleAPICancelInflight`): cancel the continuation
  too.
- Log one line per recovery to the proxy log with model, generated tokens at the cut and coverage; add a boolean /
  count to the activity entry if it fits `ActivityLogEntry` cleanly.
- Go's `\w` is ASCII-only, Python's is Unicode: use `[\p{L}\p{N}_]+|[^\p{L}\p{N}_\s]` to match Strata.

## Tests

Naming: `TestLoopGuard_<name>`, `TestLoopDetector_<name>`. Run new tests with `go test -v -run <pattern>`, then
`make test-dev`, and `make test-all` before finishing. `gofmt -w` every touched file.

- Detector: no loop on normal varied text; loop on a passage repeated 3+ times; periodic-tail case; < 2,000 tokens.
  Compare a few inputs against the Python function's output (generate the expected values once with the Strata
  source and commit them as fixtures).
- Middleware with a fake upstream (see `cmd/simple-responder`, `cmd/fake-model`): a stream that loops gets cut and
  continued; the client receives one well-formed SSE stream ending in `[DONE]`; ids consistent; usage summed;
  non-streaming request gets one JSON body; guard off → bytes pass through unchanged; client disconnect cancels both
  upstream requests.
- Do not mock the seam being changed: at least one test drives the real middleware chain end to end.

## Done means

- Builds, tests pass (`make test-all`), staticcheck clean.
- **Seen working for real** on the dev PC with `groot-65b-a6b-station-128k`: a prompt known to loop (take one of the
  18 restarted GyroM requests from the benchmark) recovers and finishes; a normal prompt is unaffected. Report
  tokens, time and whether the answer was correct.
- `docs/configuration.md` updated; enabling it in `config.yaml` is left to the user.

## Constraints

- `config.yaml` is gitignored and watched: saving it reloads llama-swap and unloads the running model. Check
  `curl localhost:9292/running` and ask the user before saving it.
- Deploy and verify per the "Local deployment" section of `AGENTS.md`.
- Commit on `main`, English only, message format per `AGENTS.md`, author and trailer per the user's global rules.
- Do not post to upstream issues or PRs.
