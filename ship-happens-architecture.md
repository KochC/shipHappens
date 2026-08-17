# Ship Happens — Local-First GitHub Actions Architecture

## Goal

**Ship Happens** is a local-first GitHub Actions-compatible runner written in Go, optimized for:

- Fast local execution
- Fail-fast workflow validation
- Runtime-safe configuration
- Parallel job execution
- Proper content-addressed caching
- Incremental reruns
- GitHub Actions compatibility

The core idea is simple:

> Compile and validate the workflow before executing it.

Instead of discovering a bad input, missing secret, broken expression, or invalid matrix after several minutes of CI runtime, Ship Happens should fail locally in milliseconds.

---

## High-Level Architecture

```text
                    ┌─────────────────────────┐
                    │     .github/workflows   │
                    │         *.yml           │
                    └────────────┬────────────┘
                                 │
                           ship validate
                                 │
                    ┌────────────▼────────────┐
                    │    Workflow Compiler    │
                    │                         │
                    │ YAML parsing            │
                    │ Schema validation       │
                    │ Expression validation   │
                    │ Action input validation │
                    │ Secret/env validation   │
                    │ DAG construction        │
                    │ Matrix expansion        │
                    └────────────┬────────────┘
                                 │
                         Compiled Run Plan
                                 │
                    ┌────────────▼────────────┐
                    │      Go Scheduler       │
                    │                         │
                    │ DAG executor            │
                    │ Parallel jobs           │
                    │ Cancellation            │
                    │ Incremental reruns      │
                    └──────┬─────────┬────────┘
                           │         │
              ┌────────────▼─┐     ┌─▼────────────┐
              │ Local Runner │ ... │ Local Runner │
              │              │     │              │
              │ Docker/OCI   │     │ Docker/OCI   │
              │ shell        │     │ shell        │
              │ native exec  │     │ native exec  │
              └──────┬───────┘     └──────┬───────┘
                     │                    │
                     └─────────┬──────────┘
                               │
                    ┌──────────▼───────────┐
                    │ Content Addressable  │
                    │        Cache         │
                    │                      │
                    │ Actions              │
                    │ Container images     │
                    │ Dependencies         │
                    │ Build outputs        │
                    │ Toolchains           │
                    │ Step results         │
                    └──────────────────────┘
```

---

## Compile Before Execute

Do not interpret GitHub workflow YAML incrementally while the job is already running.

Treat the workflow more like source code:

```text
workflow.yml
    ↓
parse
    ↓
validate
    ↓
resolve
    ↓
compile DAG
    ↓
execute
```

Example:

```bash
$ ship run

✓ YAML valid
✓ 4 jobs discovered
✓ 17 actions resolved
✓ all action inputs valid
✓ secrets available
✓ expressions valid
✓ matrix expands to 8 jobs
✓ dependency graph valid

Starting 8 jobs...
```

Instead of discovering this after 11 minutes:

```text
Error: deploy.inputs.environment doesn't exist
```

Ship Happens should fail immediately:

```text
.github/workflows/release.yml:72

uses: company/deploy@v3
with:
  enviroment: production
  ^^^^^^^^^^^

unknown input "enviroment"

Did you mean:
  environment
```

This is one of the key product differentiators.

---

## Go Project Structure

```text
cmd/
    ship/

internal/
    parser/
    validator/
    expressions/
    compiler/
    graph/
    scheduler/
    runner/
    actions/
    cache/
    containers/
    secrets/
    logs/

pkg/
    workflow/
    action/
```

The workflow compiler should translate GitHub YAML into an internal intermediate representation.

```go
type RunPlan struct {
    Jobs []JobPlan
}

type JobPlan struct {
    ID          string
    Needs       []string
    Environment map[string]string
    Steps       []StepPlan
}

type StepPlan struct {
    ID       string
    Uses     string
    Run      string
    Inputs   map[string]Value
    CacheKey string
}
```

The executor should never need to understand YAML directly.

This keeps parsing, validation, execution, and caching cleanly separated.

---

## Validation Layer

Validation should happen before execution whenever possible.

### Syntax Validation

- YAML syntax
- Required fields
- Unsupported fields
- Duplicate job IDs
- Invalid workflow structure

### GitHub Actions Semantics

- `needs`
- `if`
- `strategy.matrix`
- `env`
- `defaults`
- `permissions`
- `uses`
- `with`
- `secrets`
- `outputs`

### Expression Validation

Validate expressions before execution:

```yaml
if: ${{ github.ref == 'refs/heads/main' }}
```

Catch:

- Unknown contexts
- Unknown properties
- Invalid functions
- Invalid operators
- Type mismatches where detectable

### Action Input Validation

Resolve action metadata:

```text
uses: actions/setup-node@v4
```

Then inspect its `action.yml` or `action.yaml`.

Validate:

- Required inputs
- Unknown inputs
- Deprecated inputs
- Required runtime
- Declared outputs

### Secrets Validation

A workflow can declare its expected secrets before execution.

```text
Required secrets:

✓ NPM_TOKEN
✓ AWS_ROLE_ARN
✗ RELEASE_TOKEN
```

Fail before starting expensive jobs.

---

## Proper Caching

Caching should be a first-class subsystem, not an afterthought.

Use a local content-addressable store:

```text
~/.ship/cache/

objects/
    sha256:abc...
    sha256:def...

metadata/
    sqlite.db
```

### Cache Key

A step cache key can be derived from:

```text
SHA256(
    command
  + action version
  + relevant input files
  + dependency lockfile
  + environment
  + runner image
  + toolchain version
)
```

Example:

```text
npm install

package-lock.json unchanged
Node 24 unchanged
container unchanged

→ CACHE HIT
→ 0.08 sec
```

---

## Cache Layers

Ship Happens should have at least four cache levels.

| Cache | Example |
|---|---|
| Action cache | `actions/checkout@v4` |
| Image cache | Ubuntu/build containers |
| Dependency cache | npm, Go, Cargo, pip |
| Step cache | Complete deterministic step outputs |

Step-result caching is particularly important because it allows Ship Happens to skip entire deterministic steps.

Example:

```text
checkout             0.1s   cached
npm ci               0.2s   cached
eslint                0.1s   cached
unit tests            2.3s
build                 0.4s   cached
docker build          1.1s   partial cache
-----------------------------------
total                 4.2s
```

---

## Cache Safety

Step caching should only happen when Ship Happens can reasonably determine that the step is reproducible.

Inputs can include:

- Command
- Working directory
- Environment variables
- Referenced secrets as non-reversible fingerprints
- Input file hashes
- Toolchain versions
- Container digest
- Action version
- Previous step outputs

Allow explicit cache hints:

```yaml
- name: Build
  run: npm run build
  x-ship:
    cache:
      inputs:
        - src/**
        - package-lock.json
      outputs:
        - dist/**
```

---

## Local-First CLI

```bash
ship validate
ship run
ship run test
ship run --changed
ship run --job build
ship run --step integration-tests
ship graph
ship compile
```

### Run Only Changed Jobs

```bash
ship run --changed
```

Flow:

```text
git diff main...HEAD
        ↓
affected files
        ↓
affected jobs
        ↓
only run those jobs
```

This is especially useful for monorepos.

---

## Execution Modes

Support three execution backends:

```text
native
container
remote
```

### Native

```bash
ship run --native
```

Benefits:

- Near-zero startup time
- Direct use of local compilers/toolchains
- Best developer feedback loop

Tradeoff:

- Lower environment isolation

### Container

```text
GitHub YAML
     ↓
ubuntu-latest
     ↓
ship/ubuntu:latest
     ↓
Docker / Podman / containerd
```

Benefits:

- Reproducibility
- Isolation
- Better GitHub Actions compatibility

### Remote

Later, the same run plan can be executed by remote workers.

```text
Ship Server
   │
   ├── Linux runner
   ├── Linux runner
   ├── macOS runner
   └── Windows runner
```

---

## Scheduler

The Go scheduler consumes the compiled DAG.

Responsibilities:

- Dependency resolution
- Parallel execution
- Maximum concurrency
- Resource constraints
- Cancellation
- Retry policy
- Job timeout
- Fail-fast matrices
- Incremental reruns

Example:

```text
              ┌── lint ──────┐
checkout ─────┼── unit-test ──┼── build ── deploy
              └── typecheck ─┘
```

The scheduler should execute `lint`, `unit-test`, and `typecheck` concurrently.

---

## GitHub Compatibility

The initial product should run existing workflows without modification:

```text
.github/workflows/build.yml
```

Development flow:

```text
Developer
   │
   ├── ship validate
   │
   ├── ship run
   │
   └── git push
             │
             ▼
       GitHub Actions
```

This makes Ship Happens initially a local accelerator rather than requiring teams to replace their entire CI platform.

Later:

```text
GitHub
   │ webhook
   ▼
Ship Server
   │
   ├── Linux runner
   ├── Linux runner
   ├── macOS runner
   └── Windows runner
```

At that point it can become a full GitHub Actions replacement.

---

## Workflow Compilation Artifact

A useful feature would be an explicit compiled workflow artifact.

```bash
ship compile
```

```text
.github/workflows/build.yml
        ↓
.ship/build.plan
```

Then:

```bash
ship run .ship/build.plan
```

The plan can contain:

- Resolved action versions
- Expanded matrices
- Validated expressions
- Job DAG
- Cache metadata
- Required secrets
- Runner requirements
- Container references
- Toolchain requirements
- Static environment configuration

Conceptually:

> Terraform plan, but for CI.

This enables validation, reproducibility, inspection, and very fast startup.

---

## Example Compiled Plan

```text
Workflow: CI
Jobs: 6
Steps: 24

Required runners:
  linux/amd64

Required secrets:
  NPM_TOKEN

Execution graph:

checkout
 ├── lint
 ├── typecheck
 └── test
      │
      ▼
     build
      │
      ▼
    package

Cache prediction:

17 steps: HIT
 4 steps: MISS
 3 steps: UNCACHEABLE

Estimated runtime:
GitHub Actions: ~7m 40s
Ship Happens:   ~38s
```

---

## Architecture Principles

### 1. Fail Before Running

Anything that can be validated statically should be validated statically.

### 2. Cache Everything Safe to Cache

Do not repeat deterministic work.

### 3. Local Is the Default

Developers should be able to reproduce CI failures directly on their machine.

### 4. GitHub Compatibility First

Existing workflows should work before introducing proprietary syntax.

### 5. Extensions Without Breaking Compatibility

Ship-specific behavior can live under extension keys such as:

```yaml
x-ship:
```

GitHub ignores unknown extension metadata while Ship can use it for optimizations.

### 6. Deterministic Execution

Use:

- Immutable action versions where possible
- Container digests
- Content hashes
- Explicit toolchain versions
- Compiled run plans

---

## Product Differentiation

```text
              SHIP HAPPENS

         GitHub Actions compatible
                  │
       ┌──────────┼──────────┐
       ▼          ▼          ▼

   COMPILE      CACHE       RUN
   FIRST        EVERYTHING  LOCALLY

   Fail in      Don't       Use all
   milliseconds redo work   your cores
```

Core differentiators:

1. **Workflow compiler**
2. **Runtime and static validation**
3. **Content-addressed caching**
4. **Local-first execution**
5. **Parallel Go scheduler**
6. **Incremental reruns**
7. **GitHub Actions compatibility**

---

## Possible Positioning

**Ship Happens**

> GitHub Actions without waiting around to discover your YAML is broken.

Alternative:

> Ship happens. Your CI shouldn't.

Or:

> Ship happens on push. Not after 14 minutes of waiting for CI.
