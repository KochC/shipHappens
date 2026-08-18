# pkl-app — authoring a pipeline in Pkl

This demo shows Ship Happens with a pipeline authored in **[Pkl](https://pkl-lang.org)**
(Apple's typed, sandboxed configuration language) instead of Go.

> **Pkl is not pickle.** Pkl (pronounced "pickle") is a declarative config
> language that evaluates to JSON/YAML with no code execution or I/O — the safe,
> reviewable opposite of Python's `pickle` serialization.

## Run it

Requires the `pkl` CLI on PATH (`brew install pkl`) and a container engine for
the `lint` job:

```bash
DEPLOY_TOKEN=sk-example-123 go run ../../cmd/ship run pipeline.pkl
go run ../../cmd/ship validate pipeline.pkl        # compile + validate only
go run ../../cmd/ship run pipeline.pkl --job test --tui
```

Or build the binary once: `go build -o ship ../../cmd/ship && ./ship run pipeline.pkl`.

## How it works

```
pipeline.pkl  →  pkl eval -f json  →  RunPlan JSON  →  validate  →  scheduler
   (typed DSL)     (pure eval)          (same IR)       (same)       (same)
```

- `pipeline.pkl` `amends "../../pkl/ship.pkl"` — the typed schema that gives
  autocomplete, type-checking, and validation while authoring.
- `ship run` evaluates it with the `pkl` CLI to the exact RunPlan JSON that the
  Go DSL produces via `--compile`, then runs it through the unchanged engine.
- Everything works identically to Go-authored pipelines: parallel DAG, container
  jobs, caching/resume, variables, and secrets (masked, host-sourced).

You can also inspect the evaluated plan directly:

```bash
pkl eval -f json pipeline.pkl
```

## The schema

The importable schema lives at [`pkl/ship.pkl`](../../pkl/ship.pkl). It defines
`Job`, `Step`, `Cache`, and `Secret` types and renders to the RunPlan shape.
