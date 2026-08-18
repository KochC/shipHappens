# Ship Happens — Specification

Version: 0.3 (M1–M3)
Status: Living document — describes the implemented system as of the M3 milestone.

---

## 1. Overview

**Ship Happens** is a local-first, GitHub Actions–style CI runner written in Go.
Pipelines are authored as **Go programs** (no YAML), compiled to a validated
intermediate representation, and executed locally as a parallel DAG with
content-addressed caching, container or native execution, incremental resume,
and a live terminal dashboard.

### 1.1 Design principles

1. **Fail before running.** Everything statically checkable is validated before
   any step executes (structure, dependencies, cycles).
2. **Compile before execute.** The author DSL lowers to an immutable `RunPlan`
   IR; the scheduler and runners never touch the DSL.
3. **Local is the default.** Steps run on the host or in containers on the
   developer's machine.
4. **Cache everything safe to cache.** Deterministic step and job results are
   reused instead of recomputed.
5. **Extensions without breaking compatibility.** New capabilities are additive
   DSL methods; existing pipelines keep working.

### 1.2 Non-goals (current)

- Not a hosted service; no remote workers (future milestone).
- No GitHub Actions YAML ingestion; pipelines are Go programs.
- No distributed scheduling; a single-process scheduler on one machine.

---

## 2. Architecture

```
 author DSL (flow.*)                    pkg: flow
        │  New / Job / Run / Needs / Image / Cache / Outputs / Overlay …
        ▼
 lower  →  RunPlan (IR)                 pkg: internal/compiler
        ▼
 compile: validate + cycle check        pkg: internal/validator
        ▼
 graph: DAG, topo, subgraph, affected   pkg: internal/graph
        ▼
 scheduler: parallel DAG executor       pkg: internal/scheduler
   ├─ resume (job fingerprint cache)     pkg: internal/cache
   ├─ step cache (content-addressed)     pkg: internal/cache
   ├─ change detection (git)             pkg: internal/changed
   ├─ variables & secrets (mask/resolve) pkg: internal/secrets
   ├─ plan file loading (.pkl / .json)   pkg: internal/planfile
   ├─ runners (native / container /       pkg: internal/runner
   │           overlay), engine-agnostic
   ├─ live logs / quiet mode             pkg: internal/logs
   └─ TUI dashboard (observer)           pkg: internal/tui
```

Every layer boundary is a package; the runner is an interface so execution
backends are swappable without scheduler changes.

---

## 3. Authoring DSL (package `flow`)

A pipeline is a Go `main` that builds a `*Workflow` and hands it to an entry
point.

### 3.1 Minimal example

```go
package main

import "github.com/chris/shiphappens/flow"

func main() {
    wf := flow.New("CI")

    checkout := wf.Job("checkout").
        Run("clone", "git rev-parse HEAD")

    lint := wf.Job("lint").Needs(checkout).
        Run("vet", "go vet ./...").
        Cache(flow.Inputs("**/*.go"))

    test := wf.Job("test").Needs(checkout).
        Run("unit", "go test ./...")

    wf.Job("build").Needs(lint, test).
        Image("golang:1.22-alpine").
        Run("compile", "go build ./...").
        Cache(flow.Inputs("**/*.go"), flow.Outputs("bin/**")).
        Outputs("bin/**")

    flow.Main(wf)
}
```

### 3.2 Workflow builders

| Method | Description |
|---|---|
| `flow.New(name) *Workflow` | Start a workflow. |
| `(*Workflow) Job(id) *Job` | Add a job; returns it for chaining. Order preserved. |
| `(*Workflow) Preheat(Preheat) *Workflow` | Register warm-up work (image pull + optional cache-priming command) run concurrently before the DAG. |
| `(*Workflow) Var(k, v)` / `Vars(map)` | Workflow-level variables merged into every job's env (job `Env` overrides on collision). |
| `(*Workflow) Jobs()`, `Preheats()`, `Lines()` | Introspection (used by the runner/graph/diagnostics). |

### 3.3 Job builders (fluent, chainable)

| Method | Description |
|---|---|
| `Run(name, command) *Job` | Append a shell step (`sh -c command`). |
| `Needs(deps ...*Job) *Job` | Declare dependencies (edges in the DAG). |
| `NeedsID(ids ...string) *Job` | Declare a dependency by raw id (used to trigger unknown-need diagnostics/tests). |
| `Image(ref) *Job` | Run the job in a container image; sets runs-on to `container`. |
| `RunsOn(label) *Job` | Set a runs-on label (advisory; runner is selected by `Image`). |
| `Env(key, val) *Job` | Set a job-scoped environment variable for all steps. |
| `Secret(name) *Job` | Expose a secret env var, resolved from the host env var of the same name. Masked in logs, excluded from the plan JSON, fingerprinted non-reversibly. |
| `SecretFrom(name, fromEnv) *Job` | Like `Secret` but reads the value from a differently-named host env var. |
| `Network(enabled bool) *Job` | Explicit container networking (true=on, false=isolated). |
| `Offline() *Job` | Shorthand for `Network(false)` → `--network none`. |
| `Cache(opts ...CacheOption) *Job` | Attach cache inputs/outputs to the **most recent** step. |
| `Outputs(globs ...) *Job` | Declare the **job's** result artifacts for resume (restored when the job is skipped). |
| `Overlay() *Job` | Run a container job in an overlayfs upper layer (Linux; graceful fallback otherwise). |
| `CleanAfter(globs ...) *Job` | Delete path globs after the job succeeds (prune build intermediates). |

Cache options: `flow.Inputs(globs...)`, `flow.Outputs(globs...)`.

> **Note on two `Outputs`:** the package-level `flow.Outputs(...)` is a *step
> cache* option; the `(*Job).Outputs(...)` method declares the *job's* resume
> result. They are distinct mechanisms (see §6, §7).

### 3.4 Entry points

| Function | Behavior |
|---|---|
| `flow.Main(w)` | Parse CLI flags, compile, run; exits with status. Streams logs. |
| `flow.RunWithTUI(w)` | Like `Main` but defaults the TUI on (respects `--no-tui`). |
| `flow.RunWithTUIResume(w)` | Defaults TUI **and** resume on (respects `--no-tui`). |
| `flow.RunFile(path, argv) int` | Load, validate, and run a plan file (`.pkl`/`.json`); honors CLI flags. |
| `flow.MainFile(path, argv)` | Like `RunFile` but exits with the status. |

### 3.5 Alternative authoring: Pkl

Pipelines may be authored in **[Pkl](https://pkl-lang.org)** — a typed,
sandboxed configuration language — instead of Go, using the importable schema at
`pkl/ship.pkl`:

```pkl
amends "ship.pkl"
name = "CI"
vars { ["REGION"] = "eu-west" }
jobs {
  ["build"] { steps { new { id = "compile"; run = "make" } } }
  ["test"]  { needs { "build" }; steps { new { id = "unit"; run = "make test" } } }
}
```

Pkl is a **declarative config language**, not code — evaluation has no arbitrary
I/O or code execution. (It is unrelated to Python's `pickle` serialization
despite the homophone.) A `.pkl` pipeline is evaluated to the RunPlan JSON via
`pkl eval -f json` and then loaded through the **same** validator, scheduler, and
runners as Go-authored pipelines. Requires the `pkl` CLI on PATH.

Run either format with the standalone CLI:

```
ship run pipeline.pkl              # evaluate Pkl → run
ship run plan.json                 # run a compiled JSON plan (from --compile)
ship validate pipeline.pkl         # compile + validate only
ship pipeline.pkl --job test --tui # shorthand for `ship run`, with flags
```

---

## 4. Command-line interface

All entry points accept these flags:

| Flag | Description |
|---|---|
| `--graph` | Print the execution DAG and exit. |
| `--compile <path>` | Write the compiled `RunPlan` as JSON and exit. |
| `--job <id>` | Run only this job **and its transitive dependencies**. |
| `--changed[=<ref>]` | Run only jobs affected by git changes vs `<ref>` (default `main`). |
| `--no-cache` | Disable step caching (and resume). |
| `--resume` | Skip jobs whose fingerprint matches a prior success; restore their outputs. |
| `--engine <docker\|podman\|apple>` | Container engine for image jobs (default `docker`). |
| `--mount <spec>` | Extra container volume (repeatable), e.g. `vol:/root/.cache`. |
| `--var <K=V>` | Set/override a workflow variable (repeatable). |
| `--no-preheat` | Skip preheating. |
| `--tui` | Force the live dashboard on. |
| `--no-tui` | Force streaming logs (overrides a program that defaults to the TUI). |

Exit codes: `0` success, `1` failure (compile error, unknown job, git error, or
any job failure).

---

## 5. Compilation & validation

### 5.1 Lowering

The DSL is lowered to an immutable IR:

```go
type RunPlan struct { Name string; Jobs []JobPlan }

type JobPlan struct {
    ID         string
    RunsOn     string
    Image      string          // container image; empty ⇒ native
    Needs      []string
    Env        map[string]string
    Steps      []StepPlan
    CleanAfter []string        // prune globs (post-success)
    Network    *bool           // nil=default, true=on, false=isolated
    Outputs    []string        // job result globs (resume)
    Overlay    bool
}

type StepPlan struct { ID, Run string; Cache *CacheSpec }
type CacheSpec struct { Inputs, Outputs []string }
```

### 5.2 Static validation (all before execution)

A workflow fails to compile (exit 1, with `file:line` diagnostics and
"did you mean" suggestions) on any of:

- Empty workflow (`workflow has no jobs`).
- Duplicate job id.
- Job with no `runs-on`.
- Job with no steps.
- Step with no run command.
- A secret with an empty name.
- `needs` self-reference.
- `needs` referencing an unknown job.
- A dependency **cycle** (reported as a path, e.g. `x -> y -> x`).

### 5.3 Compiled-plan artifact

`--compile <path>` emits the validated `RunPlan` as JSON — a deterministic,
inspectable, language-neutral artifact ("Terraform plan, but for CI"). It has a
stable, JSON-tagged schema (lowercase, `omitempty`) and is the interchange
format between authoring front-ends and the engine: both the Go DSL
(`--compile`) and Pkl (`pkl eval -f json`) produce it, and `ship run <plan.json>`
consumes it. Executable/binary plan formats (e.g. Python `pickle`) are
explicitly rejected for safety and reviewability; Pkl is a safe, declarative
config language, not executable serialization.

---

## 6. Execution model

### 6.1 Scheduler

- Consumes the `RunPlan` DAG.
- Runs up to `runtime.NumCPU()` jobs concurrently (bounded by a semaphore).
- A job runs its steps **sequentially**; steps stop at the first failure.
- **Fail-fast:** on any job failure the run context is canceled; in-flight jobs
  are allowed to drain, and jobs whose dependency failed are marked *skipped*.
- Emits lifecycle **events** (`JobStarted/Finished/Skipped`,
  `StepStarted/Finished`) to an optional observer (used by the TUI).
- Race-free (verified under `go test -race`).

### 6.2 Runners (engine-agnostic)

`Runner` interface: `Run(ctx, StepPlan, workdir, env, out) StepResult`.

| Runner | When | Behavior |
|---|---|---|
| `NativeRunner` | job has no `Image` | `sh -c` on the host; ctx cancel kills the process. |
| `ContainerRunner` | job has `Image` | `<engine> run --rm -v <workdir>:/ship/work -w /ship/work [-v mounts] [--network none] [-e env] <image> sh -c ...`. |
| `OverlayRunner` | `Image` + `Overlay()` | Bind-mounts repo as overlay `lowerdir`, per-job `upperdir` persisted to `.ship-overlay/<job>`; **falls back** to direct execution when the kernel lacks overlay support. |

**Engines:** `docker` (default), `podman`, `apple` (Apple's `container` CLI);
any other value is treated as a literal binary name. All share the same
Docker-compatible `run` grammar.

### 6.3 Working directory & mounts

- The pipeline's working directory (host CWD) is the single source tree.
- For container jobs it is bind-mounted at `/ship/work`, so file inputs, outputs,
  and the cache behave identically to native execution.
- `--mount vol:/path` adds shared volumes (e.g. a persistent toolchain cache),
  applied to all image jobs.

### 6.4 Preheating

Before the DAG runs, registered `Preheat` specs execute **concurrently**: pull
the image, optionally run a warm command (mounting shared volumes) to prime
caches. Preheat is **advisory** — failures warn but never fail the build.
Skipped with `--no-preheat`.

### 6.5 Variables & secrets

**Variables** are plain configuration values:

- **Workflow vars** — `(*Workflow) Var/Vars`, merged into every job's
  environment.
- **Job env** — `(*Job) Env`, job-scoped; overrides a workflow var on key
  collision.
- **CLI overrides** — `--var K=V` (repeatable) override workflow vars at run
  time.
- Precedence (low → high): workflow vars < job env < resolved secrets. All are
  exported to every step as environment variables.

**Secrets** are sensitive values that are never hardcoded in the pipeline:

- Declared by name with `(*Job) Secret(name)` or `SecretFrom(name, fromEnv)`.
- **Resolved at run time** from the host process environment (`fromEnv`,
  defaulting to `name`) — the value never lives in the pipeline source or the
  compiled plan.
- **Fail-fast:** if a required secret is absent from the host environment, the
  job fails immediately *before any step runs* (reported as
  `missing required secret(s): [...]`).
- **Masked** in all streaming log output — every occurrence of a secret value is
  replaced with `***` (values shorter than 4 chars are left alone to avoid
  noise).
- **Excluded from the compiled plan JSON** (`--compile` serializes only the
  secret *references* — names and source env var — never values).
- **Cache-safe:** secret values enter step-cache keys and resume fingerprints
  only as **non-reversible SHA-256 fingerprints**, so changing a secret
  invalidates caches without the plaintext ever being hashed or stored.

Example:

```go
wf := flow.New("deploy").Var("REGION", "eu-west")

wf.Job("release").
    Secret("NPM_TOKEN").                    // from $NPM_TOKEN
    SecretFrom("AWS_KEY", "AWS_ACCESS_KEY_ID").
    Run("publish", "npm publish")           // $REGION, $NPM_TOKEN, $AWS_KEY set
```

---

## 7. Caching & incremental builds

Two complementary, content-addressed mechanisms backed by `~/.ship/cache`
(objects as tarballs + a JSON index).

### 7.1 Step cache

- Applies to steps with an explicit `Cache(...)` hint (explicit = safe).
- Key = `SHA256(command + workdir + sorted env + content hash of input-glob
  files)`.
- **Hit:** restore declared `Outputs`, skip execution, mark `cached`.
- **Miss:** run the step, then store its declared `Outputs`.

### 7.2 Job resume (`--resume`)

- Applies to jobs that declare `(*Job).Outputs(...)`.
- Fingerprint = `SHA256(job id + image + step commands + sorted env + input-file
  **stat signatures** + upstream job fingerprints)`.
- **Match of a prior success:** the entire job is skipped and its outputs are
  restored; reported as `resumed`.
- Enables resume: a pipeline that failed at job *N* reruns and skips 1..*N*-1.

The fingerprint uses fast **stat signatures** (path + size + mtime) rather than
full content hashing, so it scales to large trees; heavy directories (`.git`,
`.pio`, `node_modules`, `__pycache__`, `managed_components`, …) are pruned from
glob walks.

### 7.3 Change detection (`--changed`)

- `git diff --name-only <ref>...HEAD` (falls back to uncommitted changes).
- A job is directly affected if a changed file matches one of its steps' cache
  input globs; a job with no declared inputs is **conservatively** always
  affected. Affected jobs plus their transitive **dependents** are run.

### 7.4 Storage hygiene

- `CleanAfter(globs...)` prunes large build intermediates after a job succeeds
  (kept on failure for inspection), so only small artifacts persist.
- Recommended pattern: one shared toolchain volume (`--mount`) reused across all
  jobs/engines, plus `CleanAfter` for per-build scratch.

---

## 8. Observability

- **Streaming logs:** per-job prefixed, color, thread-safe (`[job] …`), with
  per-step `✓/✗` status and timing.
- **TUI dashboard** (`--tui`): a live, in-place table of job status
  (`▶` running · `✓` done · `✗` failed · `◌` skipped · `·` pending), current
  step, per-job elapsed timers, and a running summary. Suppresses streaming logs
  (quiet mode) while active.
- **Summary line:** `✓/✗ <name> in <dur> (N ran, N cached, N resumed)`.

---

## 9. On-disk layout

```
~/.ship/cache/
    objects/<key>.tar      # step + job result tarballs
    index.json             # key → object filename

<workdir>/.ship-artifacts/ # (pipeline-defined) collected outputs
<workdir>/.ship-overlay/<job>/  # overlay upper layers (Overlay jobs)
```

---

## 10. Quality bar

- Statement coverage ≥ 95% on every measured package (enforced by
  `make cover-check`); `graph`, `logs`, `runner`, `scheduler`, `validator` at
  100%.
- All tests pass under the Go race detector.
- Zero external runtime dependencies (standard library only).
- `make integration` runs container-backed tests behind a `//go:build docker`
  tag; they skip cleanly when no engine is present.

---

## 11. Roadmap (not yet implemented)

Prioritized from the internal audit:

- **Correctness:** mutex + atomic index write in the cache store; path-traversal
  guard on tar restore; include `Network`/`Overlay`/`Engine`/`Mounts` in the
  resume fingerprint; union `--changed` results with required upstreams.
- **Robustness:** per-job/step **timeouts** and **retries**; container cleanup
  on cancellation (named containers + kill); close log pipe writers (goroutine
  leak); preserve executable bits on restore.
- **Features:** step-level env overrides; named concurrency groups; persistent
  per-run log capture; cache GC/eviction.
- **Performance:** single-walk, parallel, cross-job-deduped input hashing;
  stat-first step-cache check.
- **Platform:** remote runners / distributed execution.

---

## 12. Glossary

- **Job** — a DAG node; runs its steps sequentially on one runner.
- **Step** — one `sh -c` command within a job.
- **RunPlan** — the compiled, validated IR consumed by the scheduler.
- **Fingerprint** — the resume identity of a job (§7.2).
- **Preheat** — concurrent warm-up (image pull + cache priming) before the DAG.
- **Runner** — an execution backend (native/container/overlay).
