# Ship Happens — TUI demos

Three small, self-contained pipelines that show the live terminal dashboard
(`--tui`). Run each from the repo root in a real terminal to see the in-place
repainting, colored status marks, and live timers.

```bash
go run ./demos/demo1   # parallel fan-out/fan-in build
go run ./demos/demo2   # fail-fast: a job fails, dependents are skipped
go run ./demos/demo3   # resume: run twice — second run restores from cache
```

Status marks: `▶` running · `✓` done · `✗` failed · `◌` skipped (dep failed) ·
`·` pending.

## demo1 — Parallel build
`lint`, `test`, and `typecheck` run concurrently after `checkout`; `build` waits
for all three; then `package`. Total wall time is bounded by the longest branch,
not the sum — watch the three run in parallel.

## demo2 — Fail-fast
The `test` job fails mid-DAG. The scheduler cancels in-flight work and marks the
downstream `build`/`deploy` jobs skipped, while the independent `docs` branch
still finishes. The process exits non-zero.

## demo3 — Resume / incremental
Every job declares `Outputs` and a source-file cache input. Run it once (all
jobs execute), then again — unchanged jobs resume instantly from
`~/.ship/cache` and the summary reports `N resumed`. Editing `demos/demo3/main.go`
invalidates the affected jobs on the next run. `--no-cache` forces a full re-run.

> Tip: any pipeline gets the dashboard with the `--tui` flag; these demos just
> enable it programmatically via `flow.RunWithTUI` / `flow.RunWithTUIResume`.

---

## Real language-stack demos (containerized)

These run **actual** lint/test/build tooling for a real project, in containers,
shown in the live TUI. They require a container engine (Docker/Podman/Apple).
Each demo ships a small real app plus its Ship Happens pipeline.

```bash
go run ./demos/python-app   # ruff + pytest + wheel build  (python:3.12-slim)
go run ./demos/go-app       # go vet + go test + go build  (golang:1.22-alpine)
go run ./demos/vue-app      # npm install → vitest + vite build (node:20-alpine)
```

Common flags (all demos): `--job <id>`, `--resume`, `--no-cache`,
`--no-tui` (stream tool logs for debugging), `--graph`.

### python-app
`demos/python-app/` — a tiny library (`slugify`, `fib`) with pytest. Pipeline:
`lint` (ruff) and `test` (pytest) run in parallel, then `build` produces a wheel
in `dist/`. First run installs tools (~15–20s); reruns hit the step cache.

### go-app
`demos/go-app/` — a small `calc` module (in `src/`, its own `go.mod`) with unit
tests. Pipeline: `vet` + `test` in parallel, then `build` compiles a binary.
Go's module/build caches are kept on the shared working tree so parallel jobs
reuse them.

### vue-app
`demos/vue-app/` — a Vue 3 + Vite Counter component with a Vitest test. Pipeline:
`install` (npm) populates `node_modules` on the shared tree, then `test`
(vitest) and `build` (vite) run in parallel. The first `install` is
network-heavy; declaring `node_modules` as the install job's `Outputs` lets
`--resume` skip it on unchanged `package.json`.

> Build artifacts (`node_modules/`, `dist/`, `bin/`, wheels, caches) are written
> by the container into the mounted tree and are git-ignored. `--no-tui` shows
> the real tool output when a job fails.

## secrets-app — variables & secrets

`demos/secrets-app/` — a deploy pipeline that uses a workflow **variable**
(`REGION`) and a **secret** (`DEPLOY_TOKEN`) sourced from the host environment.

```bash
DEPLOY_TOKEN=sk-example-123456 go run ./demos/secrets-app        # succeeds; token shown as ***
go run ./demos/secrets-app                                       # missing secret → fails fast
DEPLOY_TOKEN=sk-example-123456 go run ./demos/secrets-app --var REGION=us-east
DEPLOY_TOKEN=sk-example-123456 go run ./demos/secrets-app --compile plan.json  # value NOT in plan
```

Shows: variable injection + `--var` override, secret masking (`***`) in output,
fail-fast on a missing secret, and exclusion of the secret value from the
compiled plan JSON.


## pkl-app — authoring in Pkl (not Go)

`demos/pkl-app/` — the same pipeline shape authored in **Pkl** (typed, sandboxed
config; not Python's pickle). Requires the `pkl` CLI.

```bash
DEPLOY_TOKEN=sk-example-123 go run ./cmd/ship run demos/pkl-app/pipeline.pkl
go run ./cmd/ship validate demos/pkl-app/pipeline.pkl
```

Evaluates to the same RunPlan JSON as Go pipelines and runs through the identical
engine. See `demos/pkl-app/README.md` and the schema at `pkl/ship.pkl`.

## matrix-app — matrix, retries, timeouts, continue-on-error

`demos/matrix-app/` — Tier-1 features: a build **matrix** (os × go-version →
4 parallel jobs with `$OS`/`$GO`), per-step **retries**, **timeouts**, and
job/step **continue-on-error**.

```bash
go run ./demos/matrix-app
go run ./demos/matrix-app --no-tui   # see per-step output
```
