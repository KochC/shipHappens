# Ship Happens — Developer Experience (DX) Reference

The complete guide to authoring and running pipelines. For the system design see
[SPEC.md](../SPEC.md); for the GitHub Actions comparison see
[gha-gap-analysis.md](gha-gap-analysis.md).

Pipelines are authored in **[Pkl](https://pkl-lang.org)** — Apple's typed,
sandboxed configuration language (declarative, reviewable, diffable; **not**
Python's `pickle`). A pipeline `amends` the schema at
[`pkl/ship.pkl`](../pkl/ship.pkl), is evaluated to a validated plan, and runs on
a parallel DAG engine with caching, resume, containers, secrets, and a live TUI.

> A Go DSL and raw JSON plans are also supported and lower to the same engine
> (see [§13](#13-also-available-go-dsl--json)). The direction is Pkl-first — see
> [ADR-0001](adr/0001-authoring-frontends.md).

---

## Table of contents

- [1. Hello, pipeline](#1-hello-pipeline)
- [2. Running a pipeline (the `ship` CLI)](#2-running-a-pipeline-the-ship-cli)
- [3. Jobs & the DAG](#3-jobs--the-dag)
- [4. Steps](#4-steps)
- [5. Containers & execution backends](#5-containers--execution-backends)
- [6. Variables & secrets](#6-variables--secrets)
- [7. Caching & incremental resume](#7-caching--incremental-resume)
- [8. Matrix (fan-out)](#8-matrix-fan-out)
- [9. Timeouts, retries & error handling](#9-timeouts-retries--error-handling)
- [10. Preheating & storage hygiene](#10-preheating--storage-hygiene)
- [11. Observability (logs & TUI)](#11-observability-logs--tui)
- [12. Compiled plans](#12-compiled-plans)
- [13. Also available: Go DSL & JSON](#13-also-available-go-dsl--json)
- [14. Full Pkl schema reference](#14-full-pkl-schema-reference)

---

## 1. Hello, pipeline

A pipeline is a `.pkl` file that `amends` the schema, names itself, and declares
jobs. Job map keys are the job ids.

```pkl
amends "pkl/ship.pkl"

name = "CI"

jobs {
  ["build"] {
    steps {
      new { id = "compile"; run = "go build ./..." }
    }
  }
  ["test"] {
    needs { "build" }
    steps {
      new { id = "unit"; run = "go test ./..." }
    }
  }
}
```

```bash
ship run pipeline.pkl      # compile → validate → run
ship validate pipeline.pkl # compile + validate only
ship run pipeline.pkl --graph
```

`ship run` evaluates the Pkl with the `pkl` CLI (required on PATH;
`brew install pkl`), validates the DAG, and executes it.

---

## 2. Running a pipeline (the `ship` CLI)

```bash
ship run <pipeline.pkl|plan.json> [flags]   # run
ship validate <pipeline.pkl|plan.json>      # compile + validate only
ship <pipeline.pkl> [flags]                 # shorthand for `ship run`
ship cache du                               # cache size / object count
ship cache prune --older-than-days 30       # GC by age
ship cache prune --max-size-gb 5            # cap size (LRU eviction)
ship cache prune --all                      # wipe the cache
ship version
```

Flags:

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
| `--tui` / `--no-tui` | Force the live dashboard on / off. |

Exit codes: `0` success, `1` any failure (compile error, unknown job, or job
failure).

---

## 3. Jobs & the DAG

Jobs are DAG nodes; `needs` declares edges. Jobs with satisfied dependencies run
in parallel (bounded by CPU count).

```pkl
jobs {
  ["checkout"] { steps { new { id = "clone"; run = "git rev-parse HEAD" } } }

  ["lint"] { needs { "checkout" }; steps { new { id = "vet"; run = "go vet ./..." } } }
  ["test"] { needs { "checkout" }; steps { new { id = "unit"; run = "go test ./..." } } }

  ["build"] {
    needs { "lint"; "test" }
    steps { new { id = "compile"; run = "go build ./..." } }
  }
}
```

`checkout` runs first; `lint` and `test` run **concurrently**; `build` waits for
both.

The compiler statically rejects duplicate ids, missing/self dependencies, and
**dependency cycles** before anything runs, with `file:line` diagnostics.

---

## 4. Steps

A job runs its steps sequentially; the first failure stops the job (unless
continue-on-error). A step is `new { id = ...; run = ... }` plus optional fields:

```pkl
jobs {
  ["build"] {
    steps {
      new {
        id = "gen"
        run = "go generate ./..."
        env { ["MODE"] = "release" }   // step-level env (overrides job env)
        workingDir = "cmd/app"         // dir relative to the workdir
        shell = "bash"                 // sh (default) | bash | python | node | any
      }
      new {
        id = "compile"
        run = "go build ./..."
        timeoutSec = 120               // per-step timeout
        retries = 2                    // 2 additional attempts on failure
        retryBackoffSec = 5            // 5s between attempts
      }
      new {
        id = "smoke"
        run = "./app --version"
        continueOnError = true         // failure doesn't fail the job
      }
    }
  }
}
```

---

## 5. Containers & execution backends

By default steps run **natively** (`sh -c` on the host). Set `image` to run a job
in a container; the working tree is bind-mounted at `/ship/work`.

```pkl
jobs {
  ["lint"] {
    image = "golang:1.22-alpine"
    steps { new { id = "vet"; run = "go vet ./..." } }
    network = false   // run with --network none (isolated, default-secure)
  }
}
```

- **Engines:** `ship run … --engine docker` (default), `podman`, or `apple`
  (Apple's `container` CLI).
- **Networking:** `network = false` → `--network none`; `network = true` forces
  it on; omit for the engine default.
- **Volumes:** `ship run … --mount vol:/path` (e.g. a shared toolchain cache).
- **Overlay isolation:** `overlay = true` runs a container job in an overlayfs
  upper layer (Linux; falls back gracefully where unsupported).

### Native toolchains (no container, but reproducible)

Native jobs use the host's tools by default. To pin exact versions — reproducible
builds **without** a container — declare a `toolchain`:

```pkl
name = "CI"
toolchain { ["go"] = "1.22.5" }        // workflow-wide default

jobs {
  ["test"] { steps { new { id = "t"; run = "go test ./..." } } }   // uses go 1.22.5
  ["fe"] {
    toolchain { ["node"] = "20.11.0" }  // job override
    steps { new { id = "b"; run = "node --version && npm ci" } }
  }
}
```

Ship Happens resolves these via [mise](https://mise.jdx.dev) (install `mise` for
this to take effect; otherwise it logs a warning and falls back to host tools)
and prepends the pinned bin dirs to each step's PATH. Container jobs (`image`)
use the image's tools and ignore `toolchain`.

---

## 6. Variables & secrets

**Variables** are plain config; **secrets** are host-sourced and protected.

```pkl
name = "deploy"

vars {
  ["REGION"] = "eu-west"
  ["APP"] = "widget-svc"
}

jobs {
  ["release"] {
    env { ["CGO_ENABLED"] = "0" }          // job env (overrides vars)
    secrets {
      new { name = "NPM_TOKEN" }           // resolved from $NPM_TOKEN
      new { name = "AWS_KEY"; fromEnv = "AWS_ACCESS_KEY_ID" }  // from a different host var
    }
    steps {
      new { id = "publish"; run = "npm publish" }  // $REGION $APP $NPM_TOKEN $AWS_KEY set
    }
  }
}
```

Precedence: **workflow vars < job env < secrets**. Override a var at run time
with `ship run … --var REGION=us-east`.

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

```pkl
["build"] {
  steps {
    new {
      id = "compile"
      run = "go build -o bin/app ./..."
      cache { inputs { "**/*.go"; "go.mod" }; outputs { "bin/**" } }
    }
  }
}
```

**Job resume** (`--resume`): skips an entire job whose fingerprint matches a
prior success and restores its declared `outputs` — turning a failed-then-fixed
pipeline into an incremental one.

```pkl
["build"] {
  steps {
    new { id = "compile"; run = "go build -o bin/app ./..."; cache { inputs { "**/*.go" } } }
  }
  outputs { "bin/**" }   // the job's result, restored on resume
}
```

```bash
ship run pipeline.pkl --resume   # unchanged jobs restored instantly ("N resumed")
```

**Change-scoped runs** (`--changed`): `git diff` → only affected jobs + their
dependents.

---

## 8. Matrix (fan-out)

Pkl generates matrix jobs natively with `for` generators — one job per
combination, values exposed as env vars:

```pkl
local oses = new Listing { "linux"; "mac" }
local gos  = new Listing { "1.21"; "1.22" }

jobs {
  for (os in oses) {
    for (go in gos) {
      ["test-\(os)-\(go)"] = new Job {
        env { ["OS"] = os; ["GO"] = go }
        steps { new { id = "run"; run = "echo testing $OS with Go $GO && go test ./..." } }
      }
    }
  }
  ["report"] {
    // depend on all expansions
    needs { for (os in oses) { for (go in gos) { "test-\(os)-\(go)" } } }
    steps { new { id = "done"; run = "echo all matrix jobs passed" } }
  }
}
```

This produces `test-linux-1.21`, `test-linux-1.22`, `test-mac-1.21`,
`test-mac-1.22` — run in parallel, each with `$OS`/`$GO` set.

> The Go DSL offers a dedicated `Matrix(dims)` helper that does the same
> expansion automatically ([§13](#13-also-available-go-dsl--json)).

---

## 9. Timeouts, retries & error handling

```pkl
jobs {
  ["integration"] {
    timeoutSec = 600                  // whole-job timeout
    steps {
      new {
        id = "suite"
        run = "pytest -q"
        timeoutSec = 120              // per-step timeout
        retries = 3                   // 3 retries…
        retryBackoffSec = 5           // …5s apart
        continueOnError = true        // step failure doesn't fail the job
      }
    }
  }

  ["optional-scan"] {
    continueOnError = true            // job failure doesn't fail the run…
    steps { new { id = "scan"; run = "trivy fs ." } }
  }

  ["deploy"] {
    needs { "integration" }           // …and dependents still run
    steps { new { id = "ship"; run = "./deploy.sh" } }
  }
}
```

- **Timeouts** cancel the process on expiry.
- **Retries** re-attempt on non-zero exit (stop early on cancel).
- **Step `continueOnError`** → job proceeds to the next step.
- **Job `continueOnError`** → the run isn't failed and dependents still run (the
  fail-fast opt-out).

Otherwise the scheduler is **fail-fast**: a failure cancels in-flight work and
marks dependents skipped.

---

## 10. Preheating & storage hygiene

**Preheat** warms images/caches concurrently before the DAG (advisory — never
fails the build):

```pkl
preheat {
  new {
    image = "golang:1.22-alpine"
    warm = "go mod download"
    mounts { "gomod:/go/pkg/mod" }
  }
}
```

**`cleanAfter`** prunes large build intermediates after a job succeeds, keeping
only the small artifacts:

```pkl
["build"] {
  image = "golang:1.22-alpine"
  steps { new { id = "compile"; run = "go build -o bin/app ./..." } }
  outputs { "bin/**" }
  cleanAfter { ".gocache"; ".gomodcache" }   // pruned after success
}
```

Combine with one shared toolchain volume (`--mount`) reused across all jobs and
engines to download toolchains once.

---

## 11. Observability (logs & TUI)

- **Streaming logs:** per-job prefixed, colored, thread-safe, with per-step
  `✓/✗` status and timing. Secret values are masked.
- **Live TUI** (`--tui`): an in-place dashboard —
  `▶` running · `✓` done · `✗` failed · `◌` skipped · `·` pending — with current
  step, per-job timers, and a running summary.
- **Summary line:** `✓/✗ <name> in <dur> (N ran, N cached, N resumed)`.

Use `--no-tui` to stream raw tool output when debugging a failing job.

```
⚓ CI   elapsed 3s
  ✓ checkout   done 120ms
  ✓ lint       done 1.5s
  ▶ test       running · unit 2s
  · build      pending
  ▸ 2 done · 1 running · 1 pending
```

---

## 12. Compiled plans

Evaluate + validate a pipeline to a stable JSON plan ("Terraform plan, but for
CI"):

```bash
ship run pipeline.pkl --compile plan.json   # write the plan, don't run
ship run plan.json                          # run a compiled plan directly
```

The JSON plan is the interchange format all authoring front-ends emit and the
engine consumes. It is deterministic and inspectable, and **never** contains
secret values.

---

## 12.5 Reusable templates

Instead of hand-writing every command, compose pipelines from **pre-built,
parameterized job & step templates** in [`pkl/templates.pkl`](../pkl/templates.pkl).
This is the Ship Happens answer to GitHub Actions' `uses:` / composite actions —
but as plain, typed, sandboxed Pkl functions (no marketplace, no remote code).

```pkl
amends "ship.pkl"
import "ship.pkl" as s
import "templates.pkl" as t

name = "CI"
jobs {
  ["test"]  = t.goTest()                                   // ready-made vet+test
  ["build"] = (t.goBuild("./cmd/app", "bin/app")) {        // amend to add needs
    needs { "test" }
  }
  ["lint-py"] = t.pythonCheck("python:3.12-slim")          // ruff + pytest
}
```

Every template returns a `ship#Job` (or `ship#Step`) you can further customize
by amending it. Built-in templates include `goTest`, `goBuild`,
`goTestContainer`, `npm`, `pythonCheck`, `dockerBuild`, and the step helpers
`checkoutStep`, `outputStep`, `reportFailure`. Write your own the same way — a
Pkl function returning a `ship.Job`/`ship.Step` — and share them across repos via
Pkl imports or a published package (`pkl/PklProject`).

See a runnable example in [`demos/reusable-app`](../demos/reusable-app/pipeline.pkl).

---

## 13. Also available: Go DSL & JSON

Pipelines may also be authored as **Go programs** (a fluent DSL) or as raw
**JSON plans** — both lower to the same engine. The Go DSL adds a dedicated
`Matrix(dims)` helper and is handy when you want the pipeline to be a compiled
binary.

```go
package main

import "github.com/chris/shiphappens/flow"

func main() {
    wf := flow.New("CI").Var("REGION", "eu-west")

    build := wf.Job("build").
        Run("compile", "go build -o bin/app ./...").
        Cache(flow.Inputs("**/*.go"), flow.Outputs("bin/**")).
        Outputs("bin/**")

    wf.Job("test").Needs(build).
        Run("unit", "go test ./...").
        Retry(2)

    wf.Job("matrix").
        Matrix(map[string][]string{"os": {"linux", "mac"}, "go": {"1.21", "1.22"}}).
        Run("t", "echo $OS $GO")

    flow.RunWithTUI(wf)   // or flow.Main(wf) / flow.RunWithTUIResume(wf)
}
```

```bash
go run .            # same flags as `ship run`
go run . --compile plan.json   # then: ship run plan.json
```

The Go DSL surface mirrors the Pkl schema one-to-one; see the method list in the
package docs. Direction is Pkl-first ([ADR-0001](adr/0001-authoring-frontends.md)).

---

## 14. Full Pkl schema reference

Authored by amending [`pkl/ship.pkl`](../pkl/ship.pkl).

### Module (top level)

| Field | Type | Description |
|---|---|---|
| `name` | `String` | Workflow name (required). |
| `vars` | `Mapping<String,String>?` | Workflow variables, merged into every job's env. |
| `preheat` | `Listing<Preheat>?` | Warm-up work run concurrently before the DAG. |
| `jobs` | `Mapping<String,Job>` | Jobs by id (required). |

### `Job`

| Field | Type | Default | Description |
|---|---|---|---|
| `runsOn` | `String` | `"native"` | Advisory execution label. |
| `image` | `String?` | — | Container image; when set, runs in a container. |
| `needs` | `Listing<String>?` | — | Dependency job ids. |
| `env` | `Mapping<String,String>?` | — | Job-scoped env (overrides vars). |
| `secrets` | `Listing<Secret>?` | — | Host-sourced, masked secrets. |
| `steps` | `Listing<Step>` | — | Steps (required). |
| `cleanAfter` | `Listing<String>?` | — | Path globs pruned after success. |
| `network` | `Boolean?` | — | null=default, true=on, false=isolated. |
| `outputs` | `Listing<String>?` | — | Result globs persisted for `--resume`. |
| `overlay` | `Boolean` | `false` | overlayfs upper-layer isolation. |
| `timeoutSec` | `Int` | `0` | Whole-job timeout (0 = none). |
| `continueOnError` | `Boolean` | `false` | Non-fatal job (fail-fast opt-out). |

### `Step`

| Field | Type | Default | Description |
|---|---|---|---|
| `id` | `String` | — | Step id/name (required). |
| `run` | `String` | — | Shell command (required). |
| `cache` | `Cache?` | — | Content-addressed step cache. |
| `env` | `Mapping<String,String>?` | — | Step env (overrides job env). |
| `workingDir` | `String?` | — | Dir relative to the workdir. |
| `shell` | `String?` | `sh` | `sh`\|`bash`\|`python`\|`node`\|any. |
| `timeoutSec` | `Int` | `0` | Per-step timeout. |
| `retries` | `Int` | `0` | Additional attempts on failure. |
| `retryBackoffSec` | `Int` | `0` | Delay between attempts. |
| `continueOnError` | `Boolean` | `false` | Non-fatal step. |

### `Cache`, `Secret`, `Preheat`

| Class | Fields |
|---|---|
| `Cache` | `inputs: Listing<String>?`, `outputs: Listing<String>?` |
| `Secret` | `name: String`, `fromEnv: String?` |
| `Preheat` | `image: String`, `warm: String?`, `mounts: Listing<String>?` |

---

See runnable examples in [`demos/`](../demos/) — including a Pkl pipeline
(`demos/pkl-app`), containerized Python/Go/Vue pipelines, a secrets demo, and a
matrix/robustness demo.
