# Ship Happens

**A local-first, GitHub Actions–style CI runner in Go.** Pipelines are code (a
Go DSL or [Pkl](https://pkl-lang.org)), compiled to a validated plan, and run
locally as a parallel DAG — with content-addressed caching, incremental resume,
container or native execution, secrets, and a live terminal dashboard.

> Fail in milliseconds when your pipeline is broken, not after 14 minutes of CI.

---

## The DX in 30 seconds

A pipeline is a **Pkl** file. This one checks out, lints and tests in parallel,
then builds — with caching, a container job, a retry, and a masked secret:

```pkl
amends "pkl/ship.pkl"

name = "CI"
vars { ["REGION"] = "eu-west" }

jobs {
  ["checkout"] { steps { new { id = "clone"; run = "git rev-parse HEAD" } } }

  ["lint"] {
    needs { "checkout" }
    image = "golang:1.22-alpine"       // runs in a container
    steps { new { id = "vet"; run = "go vet ./..."; cache { inputs { "**/*.go" } } } }
  }
  ["test"] {
    needs { "checkout" }
    steps { new { id = "unit"; run = "go test ./..."; retries = 2 } }   // flaky-test resilience
  }
  ["build"] {
    needs { "lint"; "test" }
    secrets { new { name = "NPM_TOKEN" } }   // from $NPM_TOKEN, masked in logs
    steps { new { id = "compile"; run = "go build -o bin/app ./...";
                  cache { inputs { "**/*.go" }; outputs { "bin/**" } } } }
    outputs { "bin/**" }                     // restored instantly on --resume
  }
}
```

Run it — and watch the DAG light up in the live TUI:

```bash
ship run pipeline.pkl            # compile → validate → run (lint ∥ test, then build)
ship run pipeline.pkl --graph    # just print the DAG
ship run pipeline.pkl --resume   # skip unchanged jobs, restore their outputs
ship run pipeline.pkl --job test # run one job (+ its deps)
ship run pipeline.pkl --changed  # only jobs affected by git changes
ship run pipeline.pkl --tui      # live dashboard
```

```
⚓ CI   elapsed 3s
  ✓ checkout   done 120ms
  ✓ lint       done 1.5s
  ▶ test       running · unit 2s
  · build      pending
  ▸ 2 done · 1 running · 1 pending
```

`ship run` evaluates the Pkl with the `pkl` CLI (`brew install pkl`), validates
the DAG, and executes it. Pipelines can **also** be authored as a Go program or
raw JSON plan — all lower to the same engine (see [docs/DX.md §13](docs/DX.md#13-also-available-go-dsl--json)).

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
- **Typed, declarative config** — pipelines in Pkl (sandboxed, reviewable);
  a Go DSL and raw JSON plans also lower to the same engine.

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
