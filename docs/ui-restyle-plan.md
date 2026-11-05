# UI restyle and configuration rework — build brief

Brief for an agent starting cold. Read `AGENTS.md` first; its testing, formatting and commit rules apply. The UI is
in `ui-svelte/` (Svelte 5, Vite, Tailwind 4, shadcn-svelte style primitives on bits-ui, lucide icons,
`svelte-spa-router` hash routing). Everything below was surveyed on 2026-11-05; re-check file:line references
before relying on them.

## Goal

1. A serious visual restyle: one consistent design system applied to every page.
2. A real configuration experience: edit `config.yaml` structurally and safely, without losing what the file
   relies on (macros, anchors, comments, `${env.*}` references), with validation, a review step and clear feedback.

Work in the phases below, in order. Each phase ends in a working, committed state.

## Phase 0 — fix the config write path first (server, must fix)

Found by reading the code, not reproduced (calling the endpoint would print a secret). Confirm with a test, then fix.

- `config.LoadConfigFromReader` (`internal/config/load.go:21`) substitutes `${env.VAR}` at string level, then
  expands model macros into `cmd`, `proxy`, `env`, filters, etc.
- `GET /api/mantle/config/models` (`internal/mantle/api.go:790`, `ListModels` in `config_edit.go`) returns that
  **expanded** config. A model with `env: ["STRATA_API_KEY=${env.STRATA_API_KEY}"]` therefore sends the real key to
  the browser.
- `PUT /api/mantle/config/models/{name}` (`api.go:794`, `UpsertModelYAML` in `config_edit.go:123`) encodes the
  posted `ModelConfig` and **replaces the whole model node**. Consequences:
  - the expanded key is written into `config.yaml` in plain text;
  - `ApiKey` has `json:"-"` (`internal/config/model_config.go:128`), so it never round-trips: the saved model loses
    `apiKey` and its upstream auth breaks;
  - macro references (`${models_dir}`, `${fit_margin}` …) and YAML aliases (`filters: *qwen38-effort-with-high`) are
    replaced by literal values; comments inside the model block are lost.
- Groups use the same pattern (`UpsertGroupYAML`).

Fix direction (decide details, keep the interface small, interface before implementation):

- Read API that returns each model's **source** form (the YAML node as written: macro references, alias names,
  `${env.*}` untouched), plus, separately, a read-only **effective** view with secrets redacted (any value that came
  from `${env.*}`, `apiKey`, and `env` entries whose name looks like a secret).
- Write API that **merges field by field** into the existing node: untouched fields keep their source text, aliases
  and comments; an aliased field the user edits is detached explicitly (the UI asks), never silently.
- A dry-run validate endpoint (e.g. `POST /api/mantle/config/validate`) returning errors with line/column, used by
  both the raw editor and the guided editor before any write.
- Tests (`TestConfigEdit_<name>`): round-trip a model that uses macros, an anchor alias, `${env.*}` and a comment;
  editing one field changes only that field; no secret value ever appears in any response or in the written file.
  Use a real `config.yaml`-shaped fixture, not a mock of the seam being changed.

Also note: every config write goes through `applyConfigBytes` (`api.go:762`), which hot-reloads. Per `AGENTS.md`,
saving `config.yaml` unloads the running model. Find out exactly what a reload does to running processes (all of
them, or only changed ones) — the UI work in phase 2 depends on it.

## Phase 1 — design system foundation

Current state (`ui-svelte/src/index.css`, 860 lines):

- shadcn zinc tokens in `:root` / `.dark`, plus `--success`, `--warning`, `--info`; primary teal.
- 10 named themes (`[data-theme=…]`, ~33 copy-pasted lines each, light + dark).
- Base layer gives every `h1–h6` a `pb-4`, which pages undo with `pb-0` hacks.
- `@utility activity-link`: hard-coded purple gradient.
- **52 uses of classes that do not exist**: `.btn`, `.btn--sm`, `.card`, `.input`, `text-txtsecondary`,
  `bg-surface` (leftovers of the old React UI) in `ConfigEditor`, `BackendManager`, `ModelManager`, `CaptureDialog`,
  `ChatMessage`, `ConcurrencyInterface`. Those elements render unstyled today.
- Raw palette colours instead of tokens (`text-red-500`, `bg-green-500`, `bg-gray-200`, `focus:ring-blue-500`),
  a scoped `.input` rule with `#3b82f6` in `BackendManager.svelte`, native `<select>`/`<textarea>` next to the
  `ui/select`/`ui/textarea` primitives, duplicated `formatSize` (use `src/lib/format.ts`), inconsistent page
  wrappers and heading levels.

Do:

1. **Propose the visual direction before applying it.** Build one or two pages (suggest Models dashboard and the
   guided config editor) in the new style, take screenshots in light and dark at desktop and phone width, and show
   them to the user. Wait for their choice. The look is the user's call; everything after this point follows it.
2. Define the token set once (colour roles incl. success/warning/info, radius, spacing scale, type scale, shadow,
   focus ring). Derive themes from a small set of inputs instead of 10 hand-copied blocks; keep the themes the user
   wants to keep (ask; don't silently drop any).
3. Add the missing primitives in `src/lib/components/ui/`: checkbox, native-select wrapper or combobox, alert/banner,
   toast, form field (label + control + description + error), page header, empty state, code editor (below).
4. A page layout component (header, actions, content width) used by every route; remove the `h*` base padding.
5. Replace every undefined class and raw colour with primitives and tokens. Add a check that fails the build or the
   tests if `btn`, `txtsecondary`, `bg-surface`, or `-red-500`/`-green-500`/`-gray-*` style classes reappear.

## Phase 2 — configuration experience

Pages today (sidebar `src/components/AppSidebar.svelte`, routes `src/App.svelte:34-57`):

- `/config` `ConfigEditor.svelte`: one `<textarea>` with the whole file; Save = PUT raw text; on failure always
  "Failed to save config (check YAML syntax)" because `putConfig` (`src/lib/mantleApi.ts:111`) drops the server error.
- `/config/models` `ModelConfigEditor.svelte`: a card per model (id, backend, ttl, aliases, `BackendFlagForm` for
  the command line) and a groups section. Each card saves and goes live on its own. Rename = DELETE then PUT with no
  rollback; renaming/deleting a model does not update groups; no id/alias duplicate check.
- `BackendFlagForm.svelte`: flags from the backend's `--help` schema; unknown flags in an "Extra arguments" box split
  on whitespace (breaks quoted args); flag order can change on save.
- `/models/hub` `ModelManager.svelte`: "+ Config" appends a generated YAML snippet to the raw file (no id check, no
  error shown); deleting a local model edits the config with a line-based text hack (`stripModelFromConfig`).
- Not editable structurally: `proxy`, `checkEndpoint`, `cmdStop`, `env`, `useModelName`, `unloadTimeout`,
  `unlisted`, `name`, `description`, `concurrencyLimit`, `timeouts`, `healthCheckTimeout`, `capabilities`,
  `metadata`, `filters` (`stripParams`, `setParams`, `setParamsByID`), model and global macros, `apiKey`, peers,
  hooks, profiles. Types in `src/lib/types.ts:811-894` cover only part of these.

Target:

- **One config area** with sections: Models, Groups, Profiles, Macros, Peers/hooks (as the config supports), Raw.
  Model Hub's "+ Config" and local-model delete go through the same structured API, never text edits.
- **Model editor** covering every model field, grouped (Command, Routing & proxy, Request filters, Lifecycle,
  Metadata). Show macro references as references (with their expanded value on hover/inline), aliases as
  "inherited from `<anchor>`" with an explicit detach. Secrets are never shown; `${env.*}` stays a reference.
- **Effort/filter editor** for `setParamsByID`: a table of suffix → params, since every reasoning model uses it.
- **Staged changes**: edits across models/groups accumulate as a draft; a "Review" step shows a YAML diff of
  exactly what will be written, the validation result, and which running models the reload affects (from
  `/running` and the phase 0 finding). One Apply. Discard per item or all. Warn on navigation with unsaved changes.
- **Raw editor**: CodeMirror 6 (npm, bundled) with YAML mode, line numbers, inline errors from the validate
  endpoint, the same review/diff step before apply.
- Consistency on rename/delete: update group members, refuse duplicate ids/aliases, do the whole change as one write.
- `BackendManager`: show errors from build/update/delete (today they fail silently).

## Phase 3 — restyle every remaining page

Activity, Playground (all interfaces), Models dashboard and detail, Llama Studio pages, Logs, Performance,
Settings. Fix along the way: `ModelDetail` "not found" says "Back to Playground" but links to Activity.

## UI text rules (from the user's global instructions — apply to every string)

- UI copy is English (the UI is English today). State what a thing is; no explanations of the mechanism, no
  justification, no editorialising. Fact, then the action if there is one.
- Say it once per screen: if a banner gives the cause, fields below show only the outcome.
- Do not style normal behaviour as an error or warning (e.g. "reload will restart model X" is neutral info, not red).
- One concept, one wording, everywhere.
- Reasoning goes in code comments and commit messages, not on screen.

## Tests and verification

- `make test-ui` after UI changes (`npm ci && npm run check && npm test`); Go changes: `go test -v -run <pattern>`,
  `make test-dev`, `make test-all` before finishing; `gofmt -w`.
- Today there are no component tests and nothing covers `mantleApi.ts`. Add tests for the config API client and the
  draft/diff logic; component tests where they pay off.
- **Seen working for real** at the end of each phase: run the UI against the local llama-swap
  (`npm start` in `ui-svelte/` proxies to `LLAMA_SWAP_URL`, default `http://localhost:8080`; the dev PC's llama-swap
  is on `http://localhost:9292`), drive it in a browser, screenshot light/dark, desktop/phone width.
- Config writes against the real `config.yaml` reload llama-swap and unload the running model. Before any real save:
  check `curl localhost:9292/running`, ask the user, and back up the file. Prefer a scratch copy of the config with a
  second llama-swap instance for testing writes.

## Constraints

- English for code, comments, identifiers, docs, commits.
- Interface-first in TypeScript and Go: define the contract (API client interface, config editing service) before the
  implementation; small role-specific interfaces (reader vs writer).
- Never print or log secrets; never put `config.yaml` or `.env` contents in chat, tests or commits.
- Commit on `main`, message format per `AGENTS.md`, author and trailer per the user's global rules. Do not post to
  upstream issues or PRs.
- Ask the user before deleting a theme, a page or a feature.
