# ADR-0001: Authoring front-ends and the path to Pkl-only

Status: Accepted (interim). Direction agreed to migrate toward Pkl-only if
limitations arise.

## Context

Ship Happens separates **authoring** (how a pipeline is written) from the
**engine** (validate → schedule → run). The engine consumes an immutable IR,
`compiler.RunPlan`, and never touches any authoring format. Today three
front-ends all target that same IR:

1. **Go DSL** (`flow.New/Job/Run/...`) — the original authoring API.
2. **Pkl** (`pkl/ship.pkl` → `pkl eval -f json` → RunPlan) — typed, sandboxed
   config; the `ship` CLI loads it via `internal/planfile`.
3. **Raw JSON plans** (the `--compile` artifact) — run directly with
   `ship run plan.json`.

Pkl is a declarative configuration language (no code execution/IO); it is **not**
Python `pickle`. It is safe, reviewable, and diffable, and is the preferred
long-term authoring format.

## Decision

- Keep all three front-ends for now (they share the IR at zero engine cost).
- **If maintaining multiple front-ends becomes a limitation, consolidate on Pkl
  as the sole authoring format and drop the Go DSL and raw-JSON authoring.**
  The `RunPlan` IR remains the internal contract regardless.

## Consequences / migration checklist (for the Pkl-only future)

Doing this later is cheap and reversible because nothing in the engine depends on
the Go DSL or JSON. To make Pkl the *sole* format:

1. **Close schema gaps first** so Pkl can express everything the Go DSL can:
   - `Preheat` — **DONE.** Preheat is now part of the `RunPlan` IR
     (`plan.preheat`), the Pkl schema (`pkl/ship.pkl`), and is read by the
     scheduler front-end from the plan. All three front-ends (Go DSL, Pkl, JSON)
     round-trip preheats; verified against the firmware pipeline and via
     `pkl eval`.
   - Audit any other DSL-only affordances against `pkl/ship.pkl` (none known).
2. **Port existing Go pipelines to `.pkl`:**
   - `workflows/ci`, `workflows/broken` (samples)
   - `demos/*` (demo1–3, python/go/vue/secrets apps)
   - **`mtrust-urp-firmware/ci/ship/main.go`** — the real firmware pipeline
     (largest consumer; ~11 jobs). This is the main effort.
3. **Trim the code:**
   - Remove `flow/flow.go`, `flow/lower.go`, and the builder methods; keep
     `flow/file.go` (RunFile/MainFile), `flow/main.go`'s `runCompiled` path, and
     the run options/flag parsing.
   - Make `internal/planfile` require `.pkl` (or `.pkl`+`.json`) only.
   - `cmd/ship` becomes the single entry point.
4. **Docs:** update `SPEC.md` (§3 becomes Pkl-only), `README`/demos.

## Notes

- Until then, `RunPlan` JSON stays the interchange format; the Go DSL and Pkl
  both emit it, and `ship run` consumes it. This keeps the switch low-risk.
- Requiring Pkl means requiring the `pkl` CLI on PATH at author/run time (already
  handled with a graceful error when absent).
