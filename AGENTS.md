## Project Description:

llama-swap is a light weight, transparent proxy server that provides automatic model swapping to llama.cpp's server.

## Tech stack

- golang
- typescript, vite and svelte 5 for UI (located in ui-svelte/)

## Workflow Tasks

- when summarizing changes only include details that require further action
- Rules for creating pull requests:
  - keep them short and focused on changes
  - skip the test plan
  - write the summary using the same style rules as commit message

## Testing

- Follow test naming conventions like `TestProxyManager_<test name>`, `TestProcessGroup_<test name>`, etc.
- Use `go test -v -run <name pattern for new tests>` to run any new tests you've written.
- Run `gofmt -w <file>` before committing to fix any formatting
- Build go binaries into the ./build/ subdirectory
- Use `make test-dev` after running new tests for a quick over all test run. This runs `go test` and `staticcheck`. Fix any static checking errors. Use this only when changes are made to any code under the `proxy/` directory
- Use `make test-all` before completing work. This includes long running concurrency tests.
- Use `make test-ui` after making changes to the UI in ui-svelte/

### Commit message example format:

```
internal/server: add new feature

Add new feature that implements functionality X and Y.

- key change 1
- key change 2
- key change 3

fixes #123
```

## Local deployment (Docker Desktop + WSL2 on the dev PC)

- Start or recreate llama-swap only with `docker compose up -d [--force-recreate llama-swap]`
  from the repo root (run it through WSL: `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/w/llama-mantle && ..."`).
  `docker-compose.yml` includes `docker-compose.intelligence.yml`, which sets `LLAMA_INTELLIGENCE_URL`
  on llama-swap. Never use `-f docker-compose.llama-swap.yml` alone: that drops the variable, and
  Compose prints an "orphan containers" warning as the tell.
- `config.yaml` is gitignored and watched (`-watch-config`). Saving it reloads llama-swap and unloads
  the loaded model, interrupting a running benchmark. Check `curl localhost:9292/running` first.
- After recreating the container, verify before reporting it works: `docker ps` inside the container
  (llama-swap starts model containers through it), `LLAMA_INTELLIGENCE_URL` and `STRATA_API_KEY` set,
  F: folders non-empty inside it, `/api/mantle/studio/intelligence/run/status` answers 200.
- After a Docker Desktop restart or a disk reset, bind mounts can go stale and fail container start with
  `error while creating mount source path ... file exists`. The hash in the message is the sha256 of the
  bind source path. Dead mount points live in `/mnt/wsl/docker-desktop-bind-mounts/Ubuntu-24.04/`
  (inside Ubuntu); `stat` on them fails. Fix: `/mnt/f` stale ("No such device") -> remount it
  (`mount -t drvfs F: /mnt/f`); lazy-unmount only the dead entries; recreate the container so Docker
  mounts them fresh. A mount reused from before the restart can also break `/usr/bin/docker` in the
  container ("Input/output error"). Starting a container before the sources are readable leaves empty folders.
- Strata DeepSeek engine follows the newest release, not the image tag; see the project memory on the
  CUDA "unknown error" fault before tuning DeepSeek.

## Code Reviews

- use three levels High, Medium, Low severity
- label each discovered issue with a label like H1, M2, L3 respectively
- High severity are must fix issues (security, race conditions, critical bugs)
- Medium severity are recommended improvements (coding style, missing functionality, inconsistencies)
- Low severity are nice to have changes and nits
- Include a suggestion with each discovered item
- Limit your code review to three items with the highest priority first
- Double check your discovered items and recommended remediations
