# Intelligence results: cost, speed and discrimination

Implementation roadmap for six phases of work across two repositories.

## Context

The Intelligence results page today ranks models on score alone. Two findings from the
23 complete runs in the companion's `results/raw/` drive this work:

**Score is saturated.** Of 13 categories, 11 have most models at exactly 100%
(`multilingual` and `instruction-following`: 22 of 23 models at full marks). Only
`external` discriminates across the whole range (9–92%). Ranking models on a total
dominated by items every model passes measures almost nothing.

> Update: the companion's v3.0 suite redesign removed the `external` category —
> fetched items (Polyglot, SWE-bench) now get real categories (`code`, `debugging`)
> instead of one undifferentiated slot. The discrimination numbers above are a
> point-in-time snapshot from before that change and should be re-measured against
> the current suite rather than assumed to still hold per-category.

**Cost is invisible and enormous.** Measured on the same runs:

```
attempted%   min   tok/pt   s/pt   tok/s   model
      98.9    58       38    3.2    12.0   qwen3.8-flash-next · low
      98.5   246      194   13.7    14.2   qwen3.8-flash-next · high
      97.4   161       52    9.0     5.8   qwen3.8-flash-next · medium
      96.7    14      126    0.8   155.7   deepseek-v4-flash-vision-exp
      69.4     4       18    0.3    58.3   gpt-oss-20b · low
      64.0    50      293    4.2    70.5   gpt-oss-20b · high
```

`qwen3.8-flash-next` at `low` effort ties its own `high` effort and takes 58 minutes
instead of 246. `gpt-oss-20b` at `high` scores *worse* than at `low` and takes 12× longer.
Neither fact is visible anywhere in the UI.

A third, structural finding: the current `Score / suite maximum` and `% of suite` columns
are noise. Every complete run lands between 18% and 27% of suite, because `max_total`
(4077 points) spans items the profile never ran. That column ranks models by profile
coverage, not quality.

## Repositories

Two separate checkouts. Most work is in the first.

| Repo | Path | Role |
|---|---|---|
| llama-mantle | `w:\llama-mantle` | Svelte UI + Go proxy |
| Measure Model Intelligence | `w:\Measure Model Intelligence` | Python companion: runner, storage, dashboard API |

**No Go changes are needed.** `internal/mantle/intelligence.go` is a transparent reverse
proxy, and `dashboard/studio.py:run_results` returns each raw per-model JSON file
verbatim. Every per-item field the runner writes already reaches the browser — the
TypeScript types simply under-declare them. Do not modify `internal/mantle/`.

## What the payload already carries

Confirmed present in `results/raw/*.json`, and therefore in `GET runs/{id}`:

| Field | Level | Notes |
|---|---|---|
| `latency_ms` | item | Wall clock around the request |
| `generation_time_ms` | item | **Currently equal to `latency_ms`** — a misnomer, not a decode time |
| `tokens_per_second` | item | `tokens_completion / latency_ms` — end-to-end, not decode speed |
| `usage`, `tokens_prompt`, `tokens_completion`, `tokens_total` | item | |
| `model_id`, `model_name`, `context`, `provider`, `reasoning_effort` | model file root | Needed for the dedupe key |
| `total_time_ms`, `total_tokens`, `total_completion_tokens`, `tokens_per_second` | model `totals` | `tokens_per_second` is correctly a ratio of sums (`run.py:695-700`) — read it, never recompute as a mean of per-item rates |

The one genuine gap is time to first token. Phase 2 adds it.

---

# Phase 0 — shared metrics module

**New file:** `ui-svelte/src/lib/intelligenceMetrics.ts`
**New file:** `ui-svelte/src/lib/intelligenceMetrics.test.ts`

Pure functions, no UI. Everything in later phases reads from here, so the duplicate-row
problem and the chart-panel duplication are fixed once, not per panel.

### Extend the types first

In `ui-svelte/src/lib/intelligenceApi.ts`, add the fields the payload already has:

```ts
export interface IntelligenceItem {
  // ...existing...
  generation_time_ms?: number;
  ttft_ms?: number;              // Phase 2
  decode_time_ms?: number;       // Phase 2
  decode_tokens_per_second?: number;   // Phase 2
  prefill_tokens_per_second?: number;  // Phase 2
  tokens_per_second?: number;
  tokens_prompt?: number;
  tokens_completion?: number;
  tokens_total?: number;
  usage?: { prompt_tokens?: number; completion_tokens?: number; total_tokens?: number };
  usage_estimated?: boolean;
  episode?: unknown;
  decision?: unknown;
}
```

And on `IntelligenceResult["models"][number]`:

```ts
model_id: string; model_name: string;
reasoning_effort?: string | null;
context?: number | string | null;
provider?: string | null;
items: IntelligenceItem[];
totals: {
  score: number; max_total: number;
  by_category: Record<string, number>; by_category_max: Record<string, number>;
  total_time_ms?: number; total_tokens?: number; total_completion_tokens?: number;
  tokens_per_second?: number;
  total_ttft_ms?: number; total_decode_time_ms?: number;      // Phase 2
  decode_tokens_per_second?: number;                          // Phase 2
};
```

All optional. Old runs lack them and must render as `—`, never as `0`.

### Two aggregates, deliberately different

Do not merge these. They answer different questions and have different exclusion rules.

**Cost — what the run actually spent.** Counts every scored item, including multi-turn
and the first item of each model. "How long did this cost me" must include all of it.

```ts
export interface CostTotals {
  wallMs: number;          // Σ latency_ms
  promptTokens: number;
  completionTokens: number;
  tokensPerPoint?: number; // completionTokens / attempted score
  secondsPerPoint?: number;// wallMs/1000 / attempted score
}
```

**Speed — clean inference rates.** Excludes items whose wall clock is not inference:

| Exclude | Why |
|---|---|
| `role === "diagnostic"`, `unsupported`, `grade.unsupported` | Consistent with `attemptedScore` |
| `item.episode` or `item.decision` truthy | Multi-turn: `latency_ms` spans tool calls and sandboxed test execution, not just generation. Measured at **33% of total wall time** across 10 of 62 items on `qwen3.8-flash-next-high` |
| `usage_estimated` (item or `raw.usage_estimated`) | Token counts are stream-delta guesses, not measurements |
| The model's **first item**, for prefill/TTFT metrics only | Includes llama-swap model load. See Phase 2 |

```ts
export interface SpeedTotals {
  basis: "decode" | "endToEnd";  // "endToEnd" for runs predating Phase 2
  tokensPerSecond?: number;      // ratio of sums, never a mean of rates
  prefillTokensPerSecond?: number;
  ttftMsMedian?: number;
  counted: number;
  excluded: { multiTurn: number; estimated: number; firstItem: number };
}
```

`basis` exists so the UI never labels an end-to-end throughput number as decode speed.
Follow the honesty pattern the companion already sets with `usage_estimated`.

Guard every ratio: a zero attempted score yields `undefined`, not `Infinity`.

### Configuration identity

This is the hard part of the multi-run work, not the aggregation function.

`gpt-oss-20b-high` and `gpt-oss-20b-low` share a `model_id`. They are **not duplicates** —
they are the comparison with the largest payoff in the whole dataset. The key must be:

```ts
export function configKey(result: IntelligenceResult, model: ModelResult): string {
  return [
    model.model_id,
    model.reasoning_effort ?? result.run.settings?.reasoning_effort ?? "",
    result.run.settings?.temperature ?? "",
    model.context ?? "",
    model.provider ?? "",
    result.run.suite_sha256 ?? "",
  ].join("|");
}
```

**Labels** state what differs and nothing else. Compute which key components actually vary
across the selected set, and append only those: `gpt-oss-20b · high` when efforts differ,
plain `gpt-oss-20b` when they do not. Do not append a date unless two entries share a key
after deduplication. The current unconditional `· ${date}` suffix
(`StudioIntelligenceResults.svelte:48`) is noise in the common case.

### Duplicate handling

```ts
export type DuplicateMode = "all" | "latest" | "average";
export function resolveModels(
  results: IntelligenceResult[],
  mode: DuplicateMode,
  humanScores: HumanScores,
): ResolvedModel[];
```

- `all` — one entry per (config, run). Date suffix on the label, since keys now collide.
- `latest` — **default.** One entry per config key, from the run with the greatest `started_at`.
- `average` — one entry per key. Average `by_category` and `by_category_max` **separately**
  across runs (coverage can differ), and aggregate cost/speed as a ratio of sums, never a
  mean of per-run rates.

`best` is deliberately absent. Taking the maximum across repeated runs inflates whichever
model was run most often — it is cherry-picking, not aggregation. If it is wanted anyway it
is a three-line addition, but it should not be the default.

`ResolvedModel` carries everything every panel needs: key, label, color key, attempted
score, `by_category`, `CostTotals`, `SpeedTotals`, source run ids, and the item list.
**Every panel reads this one array.** That is what stops the bottom sections duplicating.

### Tests

`intelligenceMetrics.test.ts`, following the existing `describe`/`it` style in
`intelligenceApi.test.ts`:

- two efforts of one model produce two entries under every mode
- two runs of an identical config collapse to one under `latest` and `average`, stay two under `all`
- `latest` picks the greater `started_at`
- speed excludes an `episode` item, an `unsupported` item and a `usage_estimated` item, and reports the counts in `excluded`
- speed is a ratio of sums: two items of 10 tok in 10 s and 100 tok in 1 s give 10 tok/s, not 55
- zero attempted score yields `undefined` for `tokensPerPoint`, not `Infinity`
- a run without `ttft_ms` reports `basis: "endToEnd"`

**Acceptance:** `make test-ui` passes. No visible change to the app yet.

---

# Phase 1 — table surgery and per-item cost

**File:** `ui-svelte/src/routes/StudioIntelligenceResults.svelte`

Independent of Phase 0 if needed, but cleaner after it.

### Replace the two dead columns

Drop `Score / suite maximum` and `% of suite` (headers at `:256`, cells at `:264-265`). The only real
information in them is coverage, which gets one honest column.

New column set:

| Column | Source |
|---|---|
| Model | label from `configKey` |
| Run / profile | unchanged |
| Score | `attempted.score / attempted.max` |
| % | `attemptedPct` — now the only quality percentage on the page |
| Coverage | `attempted items / suite item count`, e.g. `62 / 78` |
| Time | `CostTotals.wallMs` as `3h 42m` / `58m` / `47s` |
| Tokens | `CostTotals.completionTokens`, compact (`234k`) |
| tok/pt | `tokensPerPoint`, rounded to integer |
| tok/s | `SpeedTotals.tokensPerSecond`, one decimal |

Sorting already generalises through `SORT_VALUE` — add a key per new column.

Coverage tooltip states the breakdown without explaining it:
`16 not attempted: 9 unsupported · 5 diagnostic · 2 awaiting a score`.

Where `basis === "endToEnd"`, mark the tok/s cell — a superscript marker with a single
footnote `End-to-end throughput where time to first token was not recorded.` Do not
repeat that sentence per row.

### KPI row

`:214-232`. Go from four tiles to six, grid `sm:grid-cols-3 lg:grid-cols-6`:

Runs compared · Models compared · Items covered · Best score · **Total time** · **Best value**

"Best value" is the lowest `tokensPerPoint`, with the model name beneath — the same shape
the existing "Best attempted score" tile uses.

### Per-item detail row

In the item `<summary>` (`:297`), append `1.1s · 38 tok` after the score, from
`latency_ms` and `tokens_completion`. Once Phase 2 lands, extend to `0.3s to first token`
on hover. `IntelligenceRunHeatmap.svelte:26` already formats latency this way — match it.

**Acceptance:** the effort tradeoff is readable straight off the table. Selecting the three
`qwen3.8-flash-next` runs shows 98.9/98.5/97.4% against 58/246/161 minutes.

---

# Phase 2 — time to first token (companion)

**Repo:** `w:\Measure Model Intelligence`
**File:** `run.py`

Independent of every other phase — can run in parallel. The UI must keep working against a
companion that has not been updated.

### The change is small and needs no new plumbing

`_run_item` (`run.py:565`) already owns both the `start` timestamp and the `on_token`
callback it hands to `_post_openai`. Wrap the callback; do not touch `_post_openai` or
`_read_stream`.

```python
start = time.time()
first_token_at = None
stamped = on_token
if on_token:
    def stamped(text, phase, _on=on_token):
        nonlocal first_token_at
        if first_token_at is None:
            first_token_at = time.time()
        _on(text, phase)
```

Stamp on the first delta of **either** phase. Reasoning deltas arrive first for thinking
models, and prefill is finished the moment anything comes back.

`ttft_ms` is `None` when `on_token` is absent (the non-streamed CLI path). The dashboard
always streams — `StudioIntelligenceRun.svelte` sends `stream: true` — so dashboard-driven
runs always get it.

### Derived fields

Extract the arithmetic into a testable helper rather than inlining it in the record dict:

```python
def _speed_fields(latency_ms, ttft_ms, usage):
    """Split a call's wall clock into prefill and decode.

    Returns the fields recorded per item. With no ttft the decode figures are omitted
    rather than guessed, so a reader can tell a measured rate from an end-to-end one.
    """
```

producing `ttft_ms`, `decode_time_ms` (`latency_ms - ttft_ms`),
`decode_tokens_per_second` (`tokens_completion / decode_time_ms`) and
`prefill_tokens_per_second` (`tokens_prompt / ttft_ms`).

### Do not redefine `generation_time_ms`

It is a misnomer — it equals `latency_ms` (`run.py:875`) — but `totals.total_time_ms` sums
it and `totals.tokens_per_second` divides by it. Changing its meaning silently changes
every historical comparison. **Leave it.** Add new fields beside it, and add to `totals`:

`total_ttft_ms`, `total_decode_time_ms`, `decode_tokens_per_second` (ratio of sums,
matching the existing `overall_tps` pattern at `run.py:695-700`).

Rename `generation_time_ms` in a separate, deliberate change if it is wanted later.

### Also stamp the multi-turn paths

`_run_episode_item` (`:493`) and `_run_decision_item` (`:530`) call `_post_openai` once per
turn through a `call` closure. TTFT there means "first token of the first turn", which is
worth recording, but their `latency_ms` includes sandboxed code execution — which is why
Phase 0 excludes them from speed aggregates regardless. Record it; do not rely on it.

### What this fixes, precisely

It moves llama-swap model-load time out of the generation figure and into TTFT, where it
belongs. Decode speed becomes clean for **every** item including the first.

It does not make the first item's *prefill* number meaningful — that one still contains the
load. Phase 0's `excluded.firstItem` rule applies to prefill and TTFT aggregates only.

### Tests

Add to `dashboard/test_studio.py`, or a new `test_run.py` beside it. Test `_speed_fields`
directly — no HTTP needed:

- ttft present: decode time and both rates computed
- ttft `None`: decode fields absent, not zero
- ttft equal to latency (nothing decoded): no division by zero
- zero `tokens_prompt`: no prefill rate rather than `0.0`

**Acceptance:** a fresh dashboard run writes `ttft_ms` on every single-turn item; a run from
before this change still loads in the UI with speed columns showing `—` and `basis: "endToEnd"`.

---

# Phase 3 — duplicate handling in the UI

**File:** `ui-svelte/src/routes/StudioIntelligenceResults.svelte`

Depends on Phase 0.

### Control

One segmented control beside the run filter, visible only when the selected runs produce a
colliding `configKey`:

`Duplicates: [ Latest ] [ Average ] [ Show all ]`

No helper paragraph under it. The labels state the behaviour.

### Route every panel through `resolveModels`

Currently `leaderboard` (`:44`), `bestAt` (`:56`), `tableRows` (`:83`), `kpis` (`:119`) and
the per-run sections (`:276`) each walk `results` independently. Replace all of them with
one `$derived` call to `resolveModels(results, duplicateMode, humanScores)`.

### Collapse the per-run sections

`{#each results as result}` at `:276` emits a full section — radar, meters, item list — per
run. That is the panel duplication. Replace with:

- **one** category radar over the resolved model set
- **one** list of model `<details>` blocks, each labelled by config, with a run column

Keep the per-run "Download raw JSON" links together in a small row above, one per selected
run, rather than buried in a duplicated section header.

### Fix the color collision

`modelColors` (`:36-42`) keys on `model_id`, so three efforts of one model receive one
color in every chart. Key it on `configKey`.

Note the consequence: `CategoricalAssignment` has 8 fixed slots and folds the rest to gray
by design — it must not be extended to generate more hues. With many configs selected, most
fold to gray. That is correct behaviour and the reason the radar's existing checkbox legend
matters; give the same select-all / clear-all legend to any new multi-series chart.

### Row keys

`{#each sortedRows as row (row.modelID + row.startedAt)}` (`:260`) collides when two runs
start in the same second. Key on `configKey` plus run id.

**Acceptance:** selecting five runs of overlapping models shows one leaderboard, one radar,
one Best At panel and one model list. Switching `Latest`/`Average`/`Show all` changes row
count and nothing else.

---

# Phase 4 — Best At, ranked by discrimination

**File:** `ui-svelte/src/components/IntelligenceBestAt.svelte`
**Caller:** `StudioIntelligenceResults.svelte:56-78`

Depends on Phase 0.

The panel currently renders a gold/silver/bronze podium per category. Where 22 of 23 models
sit at exactly 100%, the podium order is whatever `.sort()` did with the ties — noise
presented as a result.

### Compute discrimination

Per category, over the resolved model set: `spread = max - min` and
`tiedAtTop = count(pct === max)`.

Extend the props:

```ts
interface CategoryBest {
  name: string; ranked: Ranked[];
  spread: number; tiedAtTop: number; modelCount: number;
}
```

### Render

- **Sort category cards by `spread` descending.** `external` and `decision` rise to the top;
  the saturated ones sink. The panel then answers "where do these models differ", which is
  the question it is being asked.
- `spread === 0` → no podium. One line: `All tied at 100%`.
- `tiedAtTop >= 3` → no podium. One line: `4 tied at 100%`.
- Otherwise the existing podium, showing only the untied entries.
- A small `spread 83` badge on each card header.
- Categories with `spread === 0` go into a collapsed `Saturated (8)` disclosure at the
  bottom of the panel rather than occupying the grid.

Copy states the fact and stops. Not `This category no longer distinguishes between models
because every model reaches full marks` — just `All tied at 100%`.

### Report it once

Add a single line under the panel, not per card:
`8 of 13 categories are saturated across the compared models.`

This is a suite-weighting finding, not a UI defect: `external` is 200 of 4077 points and the
only category anyone loses points in, while `instruction-following` is 50 points nobody can
lose. Surfacing it is the panel's job; acting on it (retiring saturated items, or a
discrimination-weighted score) is separate work and out of scope here.

**Acceptance:** with all 23 runs selected, `external` is the first card and the 11 saturated
categories are collapsed.

---

# Phase 5 — charts

Depends on Phases 0 and 1.

## 5a. Quality vs cost scatter

**New file:** `ui-svelte/src/components/IntelligenceQualityCost.svelte`

The chart that answers "which model do I actually run". One point per resolved config.

- **x** — wall minutes, `LogarithmicScale` (the data spans 4 to 246 minutes). A toggle
  switches x to completion tokens spent. **One x measure at a time.**
- **y** — attempted %, linear, 0–100.
- **Never a second y-axis.** Two measures of different scale go in two charts.

### Color: emphasis, not categorical

This matters and is computable, so it was computed. A scatter is an all-pairs form — any two
points can sit adjacent — so the palette must separate under *all* pairs, not just adjacent
ones. The project's 8-slot categorical palette does not:

```
node scripts/validate_palette.js "#2a78d6,#eb6834,#1baf7a,#eda100,#e87ba4,#008300,#4a3aa7,#e34948" --mode light --pairs all
  [FAIL] CVD separation      worst #008300↔#eb6834 ΔE 3.2 (protan)
  [FAIL] Normal-vision floor worst #e34948↔#eb6834 ΔE 7.1 (normal)
```

Green and orange are indistinguishable to a protan reader; red and orange are hard for
*anyone*. So do not color this scatter by model.

Use the emphasis form instead, which happens to be exactly the chart's job:

- Points on the **Pareto frontier** (no other point is both faster and better) in
  `CATEGORICAL[0]` blue `#2a78d6`, with direct labels.
- Dominated points in the de-emphasis gray `#898781`, unlabelled.
- Validated: `#2a78d6` vs `#898781` separates at ΔE 15.9 protan / 17.8 normal. (The
  validator flags gray on the chroma floor — expected; it is the de-emphasis token, not a
  categorical slot.)

If color by model is wanted later, cap it at **three** models and re-run the validator with
`--pairs all`; the first three slots pass.

### Mechanics

Register `ScatterController, PointElement, LinearScale, LogarithmicScale, Tooltip`. Follow
the existing pattern in `IntelligenceScoreBar.svelte`: build options through a
`buildOptions(dark)` reading `chartChrome($isDarkMode)`, `animation: false`,
`plugins.legend.display: false` with an HTML legend, and rebuild in the `$effect` on theme
change. Chart.js's plugin registry is shared across the page — the existing components
document this; do not register `Legend` here.

`pointRadius: 5`, `pointHoverRadius: 7`, `hitRadius: 12` — the skill requires hit targets
around 24px, and the current data has points that nearly overlap. Give each point a 2px
surface ring so overlapping marks stay separable.

Tooltip per point: model label, attempted %, time, tokens spent, tok/pt, tok/s.

Direct labels for frontier points only — never a label on every point.

## 5b. Cost leaderboard

**File:** `ui-svelte/src/components/IntelligenceScoreBar.svelte` — generalise it.

It currently hardcodes percentage semantics: `pct()`, axis `min: 0, max: 100`, tooltip
`${value} / ${max} - ${pct}% success`. Add an optional raw-value mode rather than copying
100 lines of Chart.js into a second component:

```ts
interface Datum { label: string; value: number; max?: number; color: string; display?: string }
interface Props { title: string; data: Datum[]; unit?: string; lowerIsBetter?: boolean }
```

`max` present → existing percentage behaviour, unchanged for the current call site.
`max` absent → axis auto-scales, tooltip shows `display ?? value` with `unit`.

Then add one instance to the results page: **tokens per point, ascending, lower is better.**
This is the efficiency leaderboard, and it separates models that attempted % cannot —
`qwen3.8-flash-next · low` has a poor 12 tok/s but the best tok/pt in the set, because what
matters is tokens *spent*, not the rate they arrive at.

Title states the direction: `Tokens per point (lower is better)`.

## 5c. Run page

**File:** `ui-svelte/src/components/IntelligenceRunHeatmap.svelte`

Small. Once Phase 2 lands, extend the per-item tooltip (`:26`) with TTFT beside latency, and
the per-model notes line (`:57`) with prefill speed beside tok/s. Same `·`-joined format,
no new layout.

**Acceptance:** the scatter shows `deepseek-v4-flash-vision-exp` and
`qwen3.8-flash-next · low` on the frontier and `qwen3.8-flash-next · high` dominated,
labelled, in one glance.

---

# Sequencing

```
Phase 0 (metrics + types) ──┬── Phase 1 (table)  ──┬── Phase 5 (charts)
                            ├── Phase 3 (dedupe) ──┘
                            └── Phase 4 (best at)

Phase 2 (companion TTFT) ── independent, parallel
```

Phase 0 first and alone — everything else reads from it. Phase 2 can start at any time in
the other repo. Phases 1, 3 and 4 are independent of each other once 0 lands. Phase 5 last.

Each phase is separately shippable. Do not batch them into one commit.

# Conventions

- English for all identifiers, comments and developer-facing strings; UI copy follows the
  same rule here since this UI is English.
- UI text states the fact and the action, nothing else. `All tied at 100%`, not an
  explanation of why ties happen. Say it once per screen: if a footnote explains the
  end-to-end basis, the cells carry a marker, not a repeat of the sentence.
- Every new numeric field is optional. Missing renders `—`. Never render a missing
  measurement as `0`.
- Never present a derived number as a measurement. `basis: "endToEnd"` and the existing
  `usage_estimated` flag both exist for this.
- `gofmt -w` on any Go file — but this work should touch none.

# Verification

| When | Command |
|---|---|
| After each UI phase | `make test-ui` |
| After the companion phase | the companion's own test module, plus one real dashboard run to confirm `ttft_ms` is written |
| Before completing | `make test-all` |

`make test-dev` applies only if something under `proxy/` changes, which this work should not.

Commit messages follow the repo format, e.g.:

```
ui-svelte: rank intelligence results by cost and discrimination

Replace the suite-relative score columns with coverage and cost, and rank
category cards by how much they separate the compared models.

- drop "% of suite", which ranked models by profile coverage
- add time, tokens, tok/pt and tok/s from data already in the payload
- collapse duplicate configurations behind a latest/average/all control
```
