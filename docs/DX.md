# Ship Happens — Developer Experience (DX) Reference

The complete guide to authoring and running pipelines. For the system design see
[SPEC.md](../SPEC.md); for the GitHub Actions comparison see
[gha-gap-analysis.md](gha-gap-analysis.md).

Pipelines can be authored three ways — a **Go DSL**, **Pkl**, or a raw **JSON
plan** — all of which lower to the same validated plan and run through the same
engine. Start with the Go DSL.

---

## Table of contents

- [1. Hello, pipeline](#1-hello-pipeline)
- [2. Running a pipeline (CLI flags)](#2-running-a-pipeline-cli-flags)
- [3. Jobs & the DAG](#3-jobs--the-dag)
- [4. Steps](#4-steps)
- [5. Containers & execution backends](#5-containers--execution-backends)
- [6. Variables & secrets](#6-variables--secrets)
- [7. Caching & incremental resume](#7-caching--incremental-resume)
- [8. Matrix (fan-out)](#8-matrix-fan-out)
- [9. Timeouts, retries & error handling](#9-timeouts-retries--error-handling)
- [10. Preheating & storage hygiene](#10-preheating--storage-hygiene)
- [11. Observability (logs & TUI)](#11-observability-logs--tui)
- [12. Compiled plans & the `ship` CLI](#12-compiled-plans--the-ship-cli)
- [13. Authoring in Pkl](#13-authoring-in-pkl)
- [14. Full DSL reference](#14-full-dsl-reference)

---

## 1. Hello, pipeline

A pipeline is a Go program: build a `*Workflow`, hand it to an entry point.

```go
package main

import "github.com/chris/shiphappens/flow"

func main() {
    wf := flow.New("CI")

    build := wf.Job("build").
        Run("compile", "go build ./...")

    wf.Job("test").Needs(build).
        Run("unit", "go test ./...")

    flow.Main(wf) // parses flags, compiles, runs; exits with status
}
```

```bash
go run .          # compile → validate → run
go run . --graph  # just print the DAG
```

Entry points:

| Function | Behavior |
|---|---|
| `flow.Main(wf)` | Streams logs. |
| `flow.RunWithTUI(wf)` | Defaults the live TUI on (respects `--no-tui`). |
| `flow.RunWithTUIResume(wf)` | Defaults TUI **and** `--resume` on. |

---

## 2. Running a pipeline (CLI flags)

Every entry point accepts:

| Flag | Description |
|---|---|
| `--graph` | Print the execution DAG and exit. |
| `--compile <path>` | Write the compiled plan as JSON and exit. |
| `--job <id>` | Run only this job **and its transitive dependencies**. |
| `--changed[=<ref>]` | Run only jobs affected by git changes vs `<ref>` (default `main`). |
| `--no-cache` | Disable step caching (and resume). |
| `--resume` | Skip jobs whose fingerprint matches a prior success; restore outputs. |
| `--engine <docker\|podman\|apple>` | Container engine for image jobs. |
| `--mount <spec>` | Extra container volume (repeatable), e.g. `vol:/root/.cache`. |
| `--var <K=V>` | Set/override a workflow variable (repeatable). |
| `--no-preheat` | Skip preheating. |
| `--tui` / `--no-tui` | Force the dashboard on / off. |

Exit codes: `0` success, `1` any failure (compile error, unknown job, or job
failure).

---

## 3. Jobs & the DAG

Jobs are DAG nodes; `Needs` declares edges. Jobs with satisfied dependencies run
in parallel (bounded by CPU count).

```go
checkout := wf.Job("checkout").Run("clone", "git rev-parse HEAD")

lint := wf.Job("lint").Needs(checkout).Run("vet", "go vet ./...")
test := wf.Job("test").Needs(checkout).Run("unit", "go test ./...")

wf.Job("build").Needs(lint, test).Run("compile", "go build ./...")
```

`checkout` runs first; `lint` and `test` run **concurrently**; `build` waits for
both. `NeedsID("some-id")` declares a dependency by raw id (useful with matrix
expansions).

The compiler statically rejects duplicate ids, missing/​self dependencies, and
**dependency cycles** before anything runs, with `file:line` diagnostics.

---

## 4. Steps

A job runs its steps sequentially; the first failure stops the job (unless
continue-on-error). Steps are `Run(name, command)`, executed via a shell.

Per-step options attach to the **most recently added** step (fluent chaining):

```go
wf.Job("build").
    Run("gen", "go generate ./...").
        StepEnv("MODE", "release").      // step-level env (overrides job env)
        WorkingDir("cmd/app").           // dir relative to the workdir
        Shell("bash").                   // sh (default) | bash | python | node | any
    Run("compile", "go build ./...").
        StepTimeout(120).                // per-step timeout (seconds)
        Retry(2, 5).                     // 2 retries, 5s backoff
    Run("smoke", "./app --version").
        StepContinueOnError()            // failure doesn't fail the job
```

---

## 5. Containers & execution backends

By default steps run **natively** (`sh -c` on the host). Add `Image(...)` to run
a job in a container; the working tree is bind-mounted at `/ship/work`.

```go
wf.Job("lint").Image("golang:1.22-alpine").
    Run("vet", "go vet ./...")
```

- **Engines:** `--engine docker` (default), `podman`, or `apple` (Apple's
  `container` CLI).
- **Networking:** `.Offline()` runs the job with `--network none` (default-secure
  for offline compiles); `.Network(true)` forces it on.
- **Volumes:** `--mount vol:/path` (e.g. a shared toolchain cache).
- **Overlay isolation:** `.Overlay()` runs a container job in an overlayfs upper
  layer (Linux; falls back gracefully where unsupported).

---

## 6. Variables & secrets

**Variables** are plain config; **secrets** are host-sourced and protected.

```go
wf := flow.New("deploy").
    Var("REGION", "eu-west").                     // workflow var (all jobs)
    Vars(map[string]string{"APP": "widget-svc"})

wf.Job("release").
    Env("CGO_ENABLED", "0").                       // job env (overrides vars)
    Secret("NPM_TOKEN").                           // from $NPM_TOKEN
    SecretFrom("AWS_KEY", "AWS_ACCESS_KEY_ID").    // from a different host var
    Run("publish", "npm publish")                  // $REGION $APP $NPM_TOKEN $AWS_KEY set
```

Precedence: **workflow vars < job env < secrets**. Override a var at run time
with `--var REGION=us-east`.

Secrets are **safe by construction**:
- Resolved at run time from the host environment — never in source or the plan.
- **Fail-fast** if a required secret is missing (before any step runs).
- **Masked** in all log output (`***`).
- **Excluded** from the compiled plan JSON (only the name/ref is serialized).
- Fingerprinted non-reversibly in cache keys, so changing a secret invalidates
  caches without leaking it.

---

## 7. Caching & incremental resume

Two complementary, content-addressed mechanisms backed by `~/.ship/cache`.

**Step cache** (opt-in per step): skips a step whose inputs are unchanged.

```go
wf.Job("build").
    Run("compile", "go build -o bin/app ./...").
    Cache(flow.Inputs("**/*.go", "go.mod"), flow.Outputs("bin/**"))
```

**Job resume** (`--resume`): skips an entire job whose fingerprint matches a
prior success and restores its declared outputs — turning a failed-then-fixed
pipeline into an incremental one.

```go
wf.Job("build").
    Run("compile", "go build -o bin/app ./...").
    Cache(flow.Inputs("**/*.go")).
    Outputs("bin/**")   // the job's result, restored on resume
```

```bash
go run . --resume   # unchanged jobs are restored instantly ("N resumed")
```

**Change-scoped runs** (`--changed`): `git diff` → only affected jobs + their
dependents.

---

## 8. Matrix (fan-out)

Expand a job over the cartesian product of dimensions — one job per combination,
values exposed as uppercased env vars:

```go
test := wf.Job("test").
    Matrix(map[string][]string{
        "os": {"linux", "mac"},
        "go": {"1.21", "1.22"},
    }).
    Run("run", `echo "testing on $OS with Go $GO" && go test ./...`)

wf.Job("report").Needs(test).   // depends on ALL 4 expansions
    Run("done", "echo all matrix jobs passed")
```

Produces `test/1.21-linux`, `test/1.21-mac`, `test/1.22-linux`,
`test/1.22-mac` — run in parallel, each with `$OS`/`$GO` set.

---

## 9. Timeouts, retries & error handling

```go
wf.Job("integration").
    Timeout(600).                 // whole-job timeout (seconds)
    Run("suite", "pytest -q").
        StepTimeout(120).         // per-step timeout
        Retry(3, 5).              // 3 retries, 5s backoff
        StepContinueOnError()     // step failure doesn't fail the job

wf.Job("optional-scan").
    ContinueOnError().            // job failure doesn't fail the run…
    Run("scan", "trivy fs .")

wf.Job("deploy").Needs(anything). // …and dependents still run
    Run("ship", "./deploy.sh")
```

- **Timeouts** cancel the process on expiry.
- **Retries** re-attempt on non-zero exit (stop early on cancel).
- **Step `continue-on-error`** → job proceeds to the next step.
- **Job `ContinueOnError`** → the run isn't failed and dependents still run (the
  fail-fast opt-out).

Otherwise the scheduler is **fail-fast**: a failure cancels in-flight work and
marks dependents skipped.

---

## 10. Preheating & storage hygiene

**Preheat** warms images/caches concurrently before the DAG (advisory — never
fails the build):

```go
wf.Preheat(flow.Preheat{
    Image: "golang:1.22-alpine",
    Warm:  "go mod download",
    Mounts: []string{"gomod:/go/pkg/mod"},
})
```

**`CleanAfter`** prunes large build intermediates after a job succeeds, keeping
only the small artifacts:

```go
wf.Job("build").Image("golang:1.22-alpine").
    Run("compile", "go build -o bin/app ./...").
    Outputs("bin/**").
    CleanAfter(".gocache", ".gomodcache")   // pruned after success
```

Combine with one shared toolchain volume (`--mount`) reused across all jobs and
engines to download toolchains once.

---

## 11. Observability (logs & TUI)

- **Streaming logs:** per-job prefixed, colored, thread-safe, with per-step
  `✓/✗` status and timing. Secret values are masked.
- **Live TUI** (`--tui` or `RunWithTUI`): an in-place dashboard —
  `▶` running · `✓` done · `✗` failed · `◌` skipped · `·` pending — with current
  step, per-job timers, and a running summary.
- **Summary line:** `✓/✗ <name> in <dur> (N ran, N cached, N resumed)`.

Use `--no-tui` to stream raw tool output when debugging a failing job.

---

## 12. Compiled plans & the `ship` CLI

Compile a pipeline to a validated JSON plan ("Terraform plan, but for CI"):

```bash
go run . --compile plan.json
```

Run any plan (or a Pkl pipeline) with the standalone CLI:

```bash
ship run plan.json                 # run a compiled JSON plan
ship run pipeline.pkl              # evaluate Pkl → run
ship validate pipeline.pkl         # compile + validate only
ship pipeline.pkl --job test --tui # shorthand for `ship run`, with flags
```

The JSON plan is the stable interchange format that all three front-ends emit.

---

## 13. Authoring in Pkl

Prefer a declarative config? Author in [Pkl](https://pkl-lang.org) (typed,
sandboxed — **not** Python's `pickle`) by amending the schema at
[`pkl/ship.pkl`](../pkl/ship.pkl):

```pkl
amends "ship.pkl"

name = "CI"
vars { ["REGION"] = "eu-west" }

jobs {
  ["build"] {
    steps { new { id = "compile"; run = "go build ./..." } }
    outputs { "bin/**" }
  }
  ["test"] {
    needs { "build" }
    steps { new { id = "unit"; run = "go test ./..."; retries = 2 } }
  }
}
```

```bash
ship run pipeline.pkl   # requires the `pkl` CLI on PATH
```

Pkl supports every feature the Go DSL does (env, secrets, cache, matrix via
Pkl's own language, timeouts, retries, continue-on-error, …) and evaluates to
the identical plan.

---

## 14. Full DSL reference

### Workflow (`flow.New(name) *Workflow`)

| Method | Description |
|---|---|
| `Job(id) *Job` | Add a job. |
| `Var(k, v)` / `Vars(map)` | Workflow variables (merged into every job). |
| `Preheat(Preheat)` | Register warm-up work run before the DAG. |

### Job (chainable, all return `*Job`)

| Method | Description |
|---|---|
| `Run(name, cmd)` | Append a shell step. |
| `Needs(...*Job)` / `NeedsID(...string)` | Declare dependencies. |
| `Image(ref)` | Run in a container image. |
| `RunsOn(label)` | Advisory execution label. |
| `Env(k, v)` | Job-scoped env var. |
| `Secret(name)` / `SecretFrom(name, fromEnv)` | Host-sourced, masked secret. |
| `Network(bool)` / `Offline()` | Container networking control. |
| `Overlay()` | overlayfs upper-layer isolation. |
| `Outputs(globs...)` | Job result artifacts (resume). |
| `CleanAfter(globs...)` | Prune paths after success. |
| `Matrix(map[string][]string)` | Fan-out over combinations. |
| `Timeout(seconds)` | Whole-job timeout. |
| `ContinueOnError()` | Non-fatal job (fail-fast opt-out). |

### Step-configuring (attach to the last `Run`; all return `*Job`)

| Method | Description |
|---|---|
| `Cache(Inputs(...), Outputs(...))` | Content-addressed step cache. |
| `StepEnv(k, v)` | Step env (overrides job env). |
| `WorkingDir(dir)` | Step working directory. |
| `Shell(shell)` | `sh`\|`bash`\|`python`\|`node`\|any. |
| `StepTimeout(seconds)` | Per-step timeout. |
| `Retry(n, backoffSec...)` | Retries with optional backoff. |
| `StepContinueOnError()` | Non-fatal step. |

### Entry points

`flow.Main(wf)` · `flow.RunWithTUI(wf)` · `flow.RunWithTUIResume(wf)` ·
`flow.RunFile(path, argv)` · `flow.MainFile(path, argv)`

---

See runnable examples in [`demos/`](../demos/) — including containerized Python,
Go, and Vue pipelines, a secrets demo, a matrix/robustness demo, and a Pkl demo.
