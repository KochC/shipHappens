# Releasing (locally, no CI required)

Ship Happens releases itself with **local pipelines** — you can cut a release
from your machine without depending on a CI runner. The GitHub Actions workflows
in [`.github/workflows/`](../.github/workflows/) are kept only as a thin backup
(they dogfood the same pipelines); the local path below is primary.

| Task | Local pipeline | GHA backup |
|---|---|---|
| CI (vet · test · cover · build · validate + lint) | [`ci/pipeline.pkl`](../ci/pipeline.pkl) | `ci.yml` |
| Release binaries (`ship`, `ship-mcp`, `ship-egress`) | [`ci/release.pkl`](../ci/release.pkl) | `release.yml` (`v*`) |
| Release the Pkl schema package | [`ci/release-pkl.pkl`](../ci/release-pkl.pkl) | `release.yml` (`pkl-v*`) |

## Prerequisites

- `go`, `pkl`, and the [`gh`](https://cli.github.com) CLI on PATH.
- `gh` authenticated: `gh auth status` (or export `GH_TOKEN`). The publish jobs
  run **natively**, so they use your host login — nothing is stored in the plan.

## Cut a binary release

```bash
# 1. sanity-check locally (same gate CI runs)
ship run ci/pipeline.pkl

# 2. build + tag + publish — the version is computed from the git tags
ship run ci/release.pkl                    # bump=patch: latest vX.Y.Z → vX.Y.(Z+1)
ship run ci/release.pkl --var bump=minor   # vX.(Y+1).0
ship run ci/release.pkl --var bump=major   # v(X+1).0.0
ship run ci/release.pkl --var version=v1.2.3   # pin an explicit version
```

`ci/release.pkl` has three jobs:

1. **version** — fetches tags, finds the latest `v*` (local + remote), computes
   the next semver from `bump`, refuses if it already exists, and writes
   `VERSION`. Read-only: it does **not** tag, so `--job build` is a safe dry run.
2. **build** — cross-compiles `ship`/`ship-mcp`/`ship-egress` for
   linux/darwin/windows × amd64/arm64 (stamped with `VERSION`) + `checksums.txt`.
3. **publish** — refuses a dirty tree, then `git tag`, `git push`, and
   `gh release create` with all assets.

Dry run (compute version + build, no tag/publish):

```bash
ship run ci/release.pkl --job build        # runs version + build, stops before publish
```

## Cut a Pkl-package release

The package version lives in [`pkl/PklProject`](../pkl/PklProject); the release
tag must be `pkl-v<version>`. This pipeline versions it automatically from the
`pkl-v*` tags and keeps `PklProject` in sync.

```bash
ship run ci/release-pkl.pkl                 # bump=patch from latest pkl-v* tag
ship run ci/release-pkl.pkl --var bump=minor
ship run ci/release-pkl.pkl --job package   # build only
```

The **version** job computes the next `pkl-v*` and updates `pkl/PklProject`;
**publish** commits that bump, tags `pkl-vX.Y.Z`, pushes, and creates the release
whose URL the package's `packageZipUrl` resolves to.

## Why local?

- **Private repo:** no runner minutes, no token plumbing — your `gh` login is
  already there.
- **Reproducible & fast:** the build jobs declare cache inputs/outputs, so
  reruns are incremental.
- **Inspectable:** if a step fails, the end-of-run digest shows the cause and
  the output tail; full per-job logs are on disk.

Everything the GitHub Actions workflows did, you can now do with `ship run` —
the workflows remain only as an automatic safety net on push/tag.
