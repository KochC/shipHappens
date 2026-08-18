# Ship Happens — TUI demos

Three small, self-contained pipelines that show the live terminal dashboard
(`--tui`). Run each from the repo root in a real terminal to see the in-place
repainting, colored status marks, and live timers.

```bash
go run ./demos/demo1   # parallel fan-out/fan-in build
go run ./demos/demo2   # fail-fast: a job fails, dependents are skipped
go run ./demos/demo3   # resume: run twice — second run restores from cache
```

Status marks: `▶` running · `✓` done · `✗` failed · `◌` skipped (dep failed) ·
`·` pending.

## demo1 — Parallel build
`lint`, `test`, and `typecheck` run concurrently after `checkout`; `build` waits
for all three; then `package`. Total wall time is bounded by the longest branch,
not the sum — watch the three run in parallel.

## demo2 — Fail-fast
The `test` job fails mid-DAG. The scheduler cancels in-flight work and marks the
downstream `build`/`deploy` jobs skipped, while the independent `docs` branch
still finishes. The process exits non-zero.

## demo3 — Resume / incremental
Every job declares `Outputs` and a source-file cache input. Run it once (all
jobs execute), then again — unchanged jobs resume instantly from
`~/.ship/cache` and the summary reports `N resumed`. Editing `demos/demo3/main.go`
invalidates the affected jobs on the next run. `--no-cache` forces a full re-run.

> Tip: any pipeline gets the dashboard with the `--tui` flag; these demos just
> enable it programmatically via `flow.RunWithTUI` / `flow.RunWithTUIResume`.
