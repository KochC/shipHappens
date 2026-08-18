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

**Any mise backend works as the key** — the key is passed to mise verbatim — so
you can pin far more than the built-in languages:

```pkl
toolchain {
  ["go"] = "1.22.5"                  // core tools
  ["node"] = "20.11.0"
  ["pipx:platformio"] = "6.1.11"     // PlatformIO (firmware) → `pio run`
  ["flutter"] = "3.24.0"             // Flutter + Dart → `flutter build apk|ios`
  ["gem:cocoapods"] = "1.15.2"       // iOS pods
  ["gradle"] = "8.7"                 // Android/Gradle
}
```

Backend-prefixed keys: `pipx:`, `npm:`, `cargo:`, `gem:`, `aqua:`, `vfox:`, … —
anything mise's registry supports (`mise registry` to browse).

> **Reproducible means the *versions*.** The tools/SDKs you pin are exact and
> cached under `~/.local/share/mise`. What a tool then downloads at build time
> (PlatformIO platform packages, Flutter's Android SDK/NDK, Gradle/Pod deps)
> should be pinned in its own manifest (`platformio.ini`, `pubspec.lock`,
> Gradle, `Podfile.lock`) and cached with step `cache` inputs/outputs. iOS
> builds still require macOS + Xcode (Apple licensing keeps Xcode off mise; use
> `aqua:XcodesOrg/xcodes` to select a version). For a pinned OS/libc too, a
> container image remains the stronger guarantee — Ship supports both.

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

Declare a `matrix` on a job to fan it out over the cartesian product of the
named dimensions — one job per combination, with each value injected as an
UPPERCASED env var:

```pkl
jobs {
  ["test"] {
    matrix {
      ["os"] { "linux"; "mac" }
      ["go"] { "1.21"; "1.22" }
    }
    steps { new { id = "run"; run = "echo testing $OS with Go $GO && go test ./..." } }
  }
  ["report"] {
    needs { "test" }   // rewired to all four expansions automatically
    steps { new { id = "done"; run = "echo all matrix jobs passed" } }
  }
}
```

This produces `test/1.21-linux`, `test/1.21-mac`, `test/1.22-linux`,
`test/1.22-mac` (dimension keys sorted; id is `<job>/<v…>`) — run in parallel,
each with `$OS`/`$GO` set. Any job that `needs` a matrix job is automatically
rewired to depend on **all** of its expansions. Expansion happens at plan-load
time, so the validator, scheduler, and runners only ever see concrete jobs.

> The Go DSL offers the equivalent `Matrix(dims)` helper
> ([§13](#13-also-available-go-dsl--json)); both frontends produce identical
> jobs. (Before `matrix` was a first-class field, Pkl users hand-wrote the
> expansion with `for` generators — still valid, but no longer necessary.)

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

## 13.5 MCP server (for agents & IDEs)

`ship-mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server
(JSON-RPC over stdio) so agents and MCP-aware IDEs can drive Ship Happens.
Register it with your MCP client:

```json
{ "mcpServers": { "shiphappens": { "command": "ship-mcp" } } }
```

Tools exposed:

| Tool | What it does |
|---|---|
| `ship_docs` | Authoring docs: a Pkl quickref (default), or `topic=schema`/`templates`/`dx`. |
| `ship_scaffold` | Write a valid starter `pipeline.pkl` into a target dir (any repo). |
| `ship_validate` | Compile + validate a pipeline (diagnostics). |
| `ship_graph` | Return the job dependency graph. |
| `ship_run` | Start a run **in the background**, return a `runId` immediately. |
| `ship_status` | Poll a run's job/step progress & summary — **read-only, never re-triggers work** (no cold caches). |
| `ship_cancel` | Cancel a running run. |
| `ship_runs` | List runs started this session. |
| `ship_cache_du` | Report cache disk usage. |

It also serves the schema, templates, and DX guide as MCP **resources**
(`shiphappens://schema`, `shiphappens://templates`, `shiphappens://dx`,
`shiphappens://quickref`), all embedded in the binary. Together these let an
agent **author a pipeline from scratch in any repo**: read `ship_docs`,
`ship_scaffold` a starter, edit, then `ship_validate`. `ship_scaffold` also
vendors the schema (`.ship/ship.pkl` + `templates.pkl`, embedded in the binary)
beside the pipeline, so it validates immediately with no network or auth.

The async `ship_run` + `ship_status` design is deliberate: an agent starts a run,
then polls status on its own cadence without ever blocking or accidentally
re-running work. Runs stream real scheduler events (JobStarted/Finished/Skipped,
StepStarted) into an in-memory snapshot.

---

## 13.6 Live notifications

Get told when a run finishes (and optionally when it starts or a job fails)
without watching the terminal. Configure `notify` at the workflow level:

```pkl
notify = new Notify {
  desktop = true                       // macOS osascript / Linux notify-send
  webhook = "https://hooks.example/ci" // POST a JSON event
  exec = "say build $SHIP_NOTIFY_LEVEL" // run any command; event via SHIP_NOTIFY_*
  onStart = true                       // also notify when the run starts
  onJob = true                         // also notify on each job failure
}
```

Go DSL: `wf.Notifications(flow.Notify{Desktop: true, OnJob: true})`.

Delivery is **best-effort** — a failed notification never affects the build.
The `exec` sink receives `SHIP_NOTIFY_WORKFLOW`, `SHIP_NOTIFY_TITLE`,
`SHIP_NOTIFY_MESSAGE`, `SHIP_NOTIFY_LEVEL` (`info`/`success`/`failure`), and
`SHIP_NOTIFY_JOB`.

---

## 13.7 Real egress filtering

A container job that opts into the network can be pinned to an allow-list — and
it's **actually enforced**, not just documented:

```pkl
security = new Security { offlineByDefault = true }
jobs {
  ["deps"] = new Job {
    image = "node:20"
    allow = new Listing { "registry.npmjs.org"; "*.github.com" }
    steps { new Step { id = "ci"; run = "npm ci" } }
  }
}
```

Ship starts a filtering forward-proxy (`ship-egress`) for the job and routes the
container's egress through it (`HTTP_PROXY`/`HTTPS_PROXY`). Standard tooling
(curl, git, go, npm, pip, apt) honors those, so any host **not** on the list is
refused with `403` — `npm ci` reaching `evil.example` simply fails. Entries are
exact or wildcard (`*.github.com` also matches the apex). Blocked hosts are
listed in the job log.

---

## 14. Full Pkl schema reference

Authored by amending [`pkl/ship.pkl`](../pkl/ship.pkl).

### Module (top level)

| Field | Type | Description |
|---|---|---|
| `name` | `String` | Workflow name (required). |
| `vars` | `Mapping<String,String>?` | Workflow variables, merged into every job's env. |
| `toolchain` | `Mapping<String,String>?` | Pinned tool versions (mise-backed) for native jobs. |
| `security` | `Security?` | Supply-chain / network policy (`offlineByDefault`, `defaultAllow`). |
| `notify` | `Notify?` | Live run notifications (`desktop`, `webhook`, `exec`, `onStart`, `onJob`). |
| `preheat` | `Listing<Preheat>?` | Warm-up work run concurrently before the DAG. |
| `jobs` | `Mapping<String,Job>` | Jobs by id (required). |

### `Security`

| Field | Type | Default | Description |
|---|---|---|---|
| `offlineByDefault` | `Boolean` | `false` | Container jobs run with `--network none` unless they opt in. |
| `defaultAllow` | `Listing<String>?` | — | Egress allow-list applied to jobs that opt into network without their own. |

### `Notify`

| Field | Type | Default | Description |
|---|---|---|---|
| `desktop` | `Boolean` | `false` | Native desktop notifications (macOS `osascript` / Linux `notify-send`). |
| `webhook` | `String?` | — | POST a JSON event to this URL. |
| `exec` | `String?` | — | Run a shell command; event passed via `SHIP_NOTIFY_*` env. |
| `onStart` | `Boolean` | `false` | Also notify when the run starts. |
| `onJob` | `Boolean` | `false` | Also notify on each job failure. |

### `Job`

| Field | Type | Default | Description |
|---|---|---|---|
| `runsOn` | `String` | `"native"` | Advisory execution label. |
| `image` | `String?` | — | Container image; when set, runs in a container. |
| `needs` | `Listing<String>?` | — | Dependency job ids. |
| `env` | `Mapping<String,String>?` | — | Job-scoped env (overrides vars). |
| `toolchain` | `Mapping<String,String>?` | — | Pinned tool versions for this job (mise-backed). |
| `secrets` | `Listing<Secret>?` | — | Host-sourced, masked secrets. |
| `steps` | `Listing<Step>` | — | Steps (required). |
| `services` | `Listing<Service>?` | — | Sidecar service containers. |
| `cleanAfter` | `Listing<String>?` | — | Path globs pruned after success. |
| `network` | `Boolean?` | — | null=default, true=on, false=isolated. |
| `allow` | `Listing<String>?` | — | Egress allow-list; enforced by a filtering proxy. |
| `outputs` | `Listing<String>?` | — | Result globs persisted for `--resume`. |
| `overlay` | `Boolean` | `false` | overlayfs upper-layer isolation. |
| `timeoutSec` | `Int` | `0` | Whole-job timeout (0 = none). |
| `continueOnError` | `Boolean` | `false` | Non-fatal job (fail-fast opt-out). |
| `matrix` | `Mapping<String,Listing<String>>?` | — | Fan out over the cartesian product; expands to `id/<v…>` jobs with UPPERCASED env vars. |

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
| `needs` | `Listing<String>?` | — | Step-level deps (a DAG within the job). |
| `onFailure` | `Listing<Step>?` | — | Handler steps run only if this step fails. |

### `Cache`, `Secret`, `Preheat`, `Service`

| Class | Fields |
|---|---|
| `Cache` | `inputs: Listing<String>?`, `outputs: Listing<String>?` |
| `Secret` | `name: String`, `fromEnv: String?` |
| `Preheat` | `image: String`, `warm: String?`, `mounts: Listing<String>?` |
| `Service` | `name: String`, `image: String`, `env: Mapping<String,String>?`, `ports: Listing<String>?`, `health: String?`, `timeout: Int` |

---

See runnable examples in [`demos/`](../demos/) — including a Pkl pipeline
(`demos/pkl-app`), containerized Python/Go/Vue pipelines, a secrets demo, and a
matrix/robustness demo.
