# Ship Happens

**A local-first, GitHub Actions–style CI runner in Go.** Pipelines are code (a
Go DSL or [Pkl](https://pkl-lang.org)), compiled to a validated plan, and run
locally as a parallel DAG — with content-addressed caching, incremental resume,
container or native execution, secrets, and a live terminal dashboard.

> Fail in milliseconds when your pipeline is broken, not after 14 minutes of CI.

---

## The DX in 30 seconds

A pipeline is a Go program. This one checks out, lints and tests in parallel,
then builds — with caching, a container job, and a masked secret:

```go
package main

import "github.com/chris/shiphappens/flow"

func main() {
    wf := flow.New("CI").Var("REGION", "eu-west")

    checkout := wf.Job("checkout").
        Run("clone", "git rev-parse HEAD")

    lint := wf.Job("lint").Needs(checkout).Image("golang:1.22-alpine").
        Run("vet", "go vet ./...").
        Cache(flow.Inputs("**/*.go"))

    test := wf.Job("test").Needs(checkout).
        Run("unit", "go test ./...").
        Retry(2).                       // flaky-test resilience
        Cache(flow.Inputs("**/*.go"))

    wf.Job("build").Needs(lint, test).
        Secret("NPM_TOKEN").            // resolved from $NPM_TOKEN, masked in logs
        Run("compile", "go build -o bin/app ./...").
        Cache(flow.Inputs("**/*.go"), flow.Outputs("bin/**")).
        Outputs("bin/**")               // restored instantly on --resume

    flow.RunWithTUI(wf)                 // live dashboard
}
```

Run it — and watch the DAG light up in the live TUI:

```bash
go run .                 # compile → validate → run (lint ∥ test, then build)
go run . --graph         # just print the DAG
go run . --resume        # skip unchanged jobs, restore their outputs
go run . --job test      # run one job (+ its deps)
go run . --changed       # only jobs affected by git changes
```

```
⚓ CI   elapsed 3s
  ✓ checkout   done 120ms
  ✓ lint       done 1.5s
  ▶ test       running · unit 2s
  · build      pending
  ▸ 2 done · 1 running · 1 pending
```

**Prefer declarative config?** The same pipeline in Pkl:

```pkl
amends "pkl/ship.pkl"
name = "CI"
jobs {
  ["build"] { steps { new { id = "compile"; run = "go build ./..." } }; outputs { "bin/**" } }
  ["test"]  { needs { "build" }; steps { new { id = "unit"; run = "go test ./..."; retries = 2 } } }
}
```

```bash
ship run pipeline.pkl
```

---

## 📖 Full DX reference

**[docs/DX.md](docs/DX.md)** — the complete guide to every feature: jobs & the
DAG, steps, containers & engines, variables & secrets, caching & resume, matrix
fan-out, timeouts/retries/error-handling, preheating, the TUI, compiled plans,
the `ship` CLI, and Pkl authoring — with a full DSL reference table.

---

## Why Ship Happens

- **Compile before execute** — parse, validate, and check the DAG for cycles up
  front, with `file:line` diagnostics. No discovering a typo after 11 minutes.
- **Local is the default** — reproduce CI on your machine; native or in
  containers (Docker / Podman / Apple `container`).
- **Cache everything safe** — content-addressed step cache **plus** job-level
  resume (skip whole unchanged jobs, restore their artifacts).
- **Fast** — parallel DAG across all your cores; `--changed` runs only what git
  touched.
- **Three authoring formats** — Go DSL, Pkl, or raw JSON plans — all lowering to
  the same engine.

## Feature highlights

Jobs & DAG · matrix fan-out · containers (docker/podman/apple) · overlayfs
isolation · workflow vars & host-sourced **secrets** (masked, fail-fast) ·
content-addressed **step cache** · **job resume** · `--changed` · **timeouts**,
**retries**, **continue-on-error** · step-level env/workdir/shell · preheating ·
`CleanAfter` pruning · live **TUI** · compiled JSON **plan** artifact · a
standalone **`ship`** CLI.

See how it compares to GitHub Actions: [docs/gha-gap-analysis.md](docs/gha-gap-analysis.md).

## Try the demos

```bash
go run ./demos/demo1        # parallel fan-out/fan-in (TUI)
go run ./demos/demo2        # fail-fast: a job fails, dependents skipped
go run ./demos/demo3        # resume: run twice, second is instant
go run ./demos/matrix-app   # matrix, retries, timeouts, continue-on-error
go run ./demos/python-app   # real ruff + pytest + wheel in a container
go run ./demos/go-app       # real go vet + test + build in a container
go run ./demos/vue-app      # real npm + vitest + vite build in a container
DEPLOY_TOKEN=sk-x go run ./demos/secrets-app   # variables + masked secrets
go run ./cmd/ship run demos/pkl-app/pipeline.pkl   # authored in Pkl
```

See [demos/README.md](demos/README.md) for details.

## Build & test

```bash
make            # vet + test
make cover-check   # 95% coverage gate
make integration   # container-backed tests (needs Docker)  [ENGINE=docker|podman|apple]
make pkl-test      # Pkl integration (needs the pkl CLI)
```

## Documentation

- **[docs/DX.md](docs/DX.md)** — developer experience & full feature guide
- **[SPEC.md](SPEC.md)** — system specification (architecture, IR, execution)
- **[docs/gha-gap-analysis.md](docs/gha-gap-analysis.md)** — GitHub Actions comparison
- **[docs/adr/](docs/adr/)** — architecture decision records
