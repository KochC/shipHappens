# Ship Happens vs GitHub Actions — Feature Gap Analysis

Status: as of the current `main`. This inventories what Ship Happens implements
today versus GitHub Actions (GHA), and prioritizes the gaps.

Legend: ✅ present · 🟡 partial · ❌ absent

---

## 1. Triggers / events

| GHA feature | Ship Happens | Notes |
|---|---|---|
| `on: push/pull_request/schedule/workflow_dispatch/...` | ❌ | No event system. Runs are started imperatively (`ship run`, or a Go/Pkl program). |
| Webhook/event context | ❌ | — |
| Change-scoped runs | ✅ (`--changed`) | git-diff → affected jobs + dependents. A filter, not a trigger. |

**Assessment:** By design — Ship Happens is *local-first*. Triggers belong to a
future "server/remote" milestone, not the core runner.

## 2. Jobs & dependencies

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Jobs | ✅ | |
| `needs` (DAG) | ✅ | topo order, subgraph, dependents all implemented |
| Parallel execution | ✅ | bounded by `NumCPU` |
| `strategy.matrix` (fan-out) | ✅ | `Matrix(dims)`; compile-time cartesian expansion into N jobs |
| `max-parallel` | ✅ | `--max-parallel N` flag |
| Job-level `if:` (conditional) | ✅ | `If(expr)` / `StepIf(expr)` with an expression evaluator |

## 3. Steps

| GHA feature | Ship Happens | Notes |
|---|---|---|
| `run:` shell steps | ✅ | via `sh -c` |
| Step id / name | ✅ | |
| `uses:` actions / marketplace | ❌ | no action mechanism at all |
| `shell:` selection (bash/python/node) | ✅ | per-step `Shell(...)`; default `sh` |
| Per-step `working-directory` | ✅ | `WorkingDir(...)` per step |
| Step-level `env` | ✅ | `StepEnv(...)` (overrides job env) |
| `continue-on-error` (step) | ✅ | `StepContinueOnError()` |

## 4. Runners / execution

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Hosted/self-hosted runners | ✅ (local) | native + container |
| Container jobs | ✅ | docker/podman/apple engines |
| `services:` sidecar containers | ✅ | `Service{...}`: start, health-wait, network-attach, teardown |
| Container networking control | ✅ | `Offline()`/`Network()` → `--network none` |
| Volume mounts | ✅ | `--mount` |
| `runs-on` labels | 🟡 | stored but runner chosen by `Image` only |
| Overlay/isolation | ✅ (bonus) | overlayfs upper layer, graceful fallback |

## 5. Variables, env, secrets, expressions

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Workflow/job env & vars | ✅ | precedence: vars < job env < secrets |
| Secrets | ✅ | host-sourced, fail-fast, masked, excluded from plan, fingerprinted |
| expressions (env/vars/outputs/needs, ==,!=,&&,\|\|,!, success/failure/always) | ✅ | `internal/expr` evaluator (own syntax, not `${{ }}`) |
| Contexts (`env`, `vars`, `outputs`, `needs.result`) | 🟡 | available in `if:`; no `github`/`matrix` context objects |

## 6. Caching & artifacts

| GHA feature | Ship Happens | Notes |
|---|---|---|
| `actions/cache` | ✅ (better) | content-addressed step cache, automatic |
| Incremental resume | ✅ (bonus) | job fingerprint cache (`--resume`) — GHA has no equivalent |
| `upload/download-artifact` | 🟡 | `Job.Outputs` persisted for resume only; no cross-job/external artifact retrieval |
| Cache GC/eviction | ❌ | store grows unbounded |
| Prune intermediates | ✅ (bonus) | `CleanAfter` |

## 7. Concurrency, timeouts, retries, fail-fast

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Parallelism | ✅ | |
| Fail-fast | ✅ | unconditional |
| `fail-fast: false` opt-out | ✅ | job `ContinueOnError()` makes a job non-fatal |
| `concurrency:` groups / cancel-in-progress | ❌ | roadmap |
| `timeout` (job/step) | ✅ | `Timeout(s)` / `StepTimeout(s)`; ctx cancellation |
| Retries | ✅ | per-step `Retry(n, backoff)` |

## 8. Reusable / composite workflows & actions

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Reusable workflows (`workflow_call`) | ❌ | — |
| Composite actions | ❌ | — |
| Action marketplace (`uses:`) | ❌ | — |
| Multiple authoring front-ends | ✅ (different) | Go DSL + Pkl + JSON, all → same IR |

## 9. Outputs & status checks

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Step outputs (`$SHIP_OUTPUT`) | ✅ | steps write key=value; captured per job |
| Job outputs → dependents | ✅ | exposed as `$OUTPUTS_<JOB>_<KEY>` env + `outputs.<job>.<key>` in `if:` |
| Exit status | ✅ | 0/1 + `Result{Failed,Ran,Cached,Resumed}` |
| SCM status checks / PR reporting | ❌ | no SCM integration |

## 10. Observability

| GHA feature | Ship Happens | Notes |
|---|---|---|
| Streaming logs | ✅ | prefixed, colored, secret-masked |
| Live dashboard | ✅ (bonus) | zero-dep TUI |
| `::error/warning::` annotations | ❌ | (static validation does emit `file:line` diagnostics) |
| `$GITHUB_STEP_SUMMARY` | ❌ | — |
| Persistent per-run logs on disk | ❌ | stdout only |
| Compiled plan artifact | ✅ (bonus) | `--compile` → JSON |

---

## Distinctive Ship Happens features GHA lacks

Compile-to-IR plan (`--compile`), local content-addressed step cache + **job
resume** fingerprints, `--changed` git-affected execution, overlayfs isolation,
multi-engine (docker/podman/apple), preheat warming, `CleanAfter` pruning, a
zero-dependency live TUI, and a **supply-chain security posture** GHA can't
match locally: **offline-by-default steps**, per-job **egress allow-lists**, and
built-in **input sanitizing** (`flow.Sanitize`) for untrusted values like PR
titles / commit messages.

---

## Tier 2 status — ✅ mostly DONE

Implemented since the initial analysis:

- **`if:` conditionals** (job + step) with an expression evaluator
  (`internal/expr`): env/vars/outputs/needs refs, `== != && || !`,
  `success()/failure()/always()`.
- **Step & job outputs**: steps write `key=value` to `$SHIP_OUTPUT`; exposed to
  dependents as `$OUTPUTS_<JOB>_<KEY>` env and `outputs.<job>.<key>` in `if:`.
- **`--max-parallel`** flag.
- **`services:` sidecar containers**: start on a shared network, health-wait,
  reachable by name, torn down after the job.
- **Supply-chain security**: `OfflineByDefault()`, per-job `Allow(...)` egress
  allow-lists, and `Sanitize`/`SafeIdentifier` input helpers.

Still open in Tier 2: **uploadable artifacts** (beyond resume cache) and named
**concurrency groups**.

## Prioritized roadmap to close the gap

Ranked by value-for-effort for a **local CI runner** (some GHA features are
intentionally out of scope for local-first).

### Tier 1 — ✅ DONE (implemented in Go DSL, Pkl schema, and scheduler)
1. **`matrix` / fan-out** — ✅ `Matrix(dims)`; compile/eval-time cartesian
   expansion into N jobs; dependents depend on all expansions.
2. **Job/step `timeout`** — ✅ `Timeout(s)` / `StepTimeout(s)` via
   `context.WithTimeout`.
3. **`continue-on-error` + `fail-fast: false`** — ✅ `StepContinueOnError()` and
   job `ContinueOnError()` (the fail-fast opt-out).
4. **Step-level `env` + per-step `working-directory` + `shell`** — ✅ `StepEnv`,
   `WorkingDir`, `Shell`.
5. **Retries** — ✅ per-step `Retry(n, backoffSec...)`.

### Tier 2 — valuable, moderate effort
6. **Step & job outputs** — capture `key=value` from a step (a `$SHIP_OUTPUT`
   file), expose to dependents. Enables real data flow between jobs.
7. **Conditional `if:` on jobs/steps** — requires a small expression evaluator
   (a subset: env/var refs, `==`, `&&`, `||`, `success()/failure()`), reused by
   `matrix`, outputs, and skips.
8. **Uploadable artifacts** — promote `Job.Outputs` to a first-class artifact
   store retrievable after the run (`ship artifacts <run>`), separate from the
   opaque resume cache.
9. **`services:` sidecar containers** — start a container (e.g. postgres/redis),
   wait for health, expose to job steps, tear down. Common integration-test need.
10. **Named `concurrency` groups** — serialize/replace runs sharing a group key.

### Tier 3 — larger / ecosystem
11. **Reusable/composite pipelines** — compose a pipeline from another
    (Pkl `import` already gives partial config reuse; add job-graph inlining).
12. **Action-like reuse (`uses:`)** — a "step template" concept (not the GHA
    marketplace, but a local/versioned reusable step). Big design surface.
13. **Triggers/events + SCM status checks** — belongs to a server/remote
    milestone (webhooks, PR checks), out of scope for the local runner.
14. **Runtime annotations + step summaries + persistent logs** — observability
    polish; annotations need a `::error::`-style parser.

### Explicit non-goals (for the local-first core)
Marketplace actions, hosted-runner labels/pools, and GHA's event model are
deliberately out of scope until a remote/server milestone.
