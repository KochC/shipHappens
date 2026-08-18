// Command matrix-demo shows Ship Happens Tier-1 features in the live TUI:
// build matrix (fan-out), per-step retries, timeouts, and continue-on-error.
//
//	go run ./demos/matrix-app
//	go run ./demos/matrix-app --no-tui   # see per-step tool output
//
// The `test` job expands over os × go-version into 4 parallel jobs, each with
// $OS and $GO set. `flaky` demonstrates retries; `optional` is a non-fatal job.
package main

import "github.com/chris/shiphappens/flow"

func main() {
	wf := flow.New("Matrix + Robustness")

	// Fan-out: 2 OSes × 2 Go versions = 4 parallel jobs.
	test := wf.Job("test").
		Matrix(map[string][]string{
			"os": {"linux", "mac"},
			"go": {"1.21", "1.22"},
		}).
		Run("info", `echo "testing on $OS with Go $GO" && sleep 0.5`)

	// Retries: fails on the first attempt, succeeds on the second.
	wf.Job("flaky").Needs(test).
		Run("attempt", `if [ -f .flaky-done ]; then echo ok; else touch .flaky-done; echo "fail #1"; exit 1; fi`).
		Retry(2).
		CleanAfter(".flaky-done")

	// Timeout + continue-on-error: a slow step that times out but doesn't fail
	// the run.
	wf.Job("optional").Needs(test).ContinueOnError().
		Run("slow", "sleep 10").StepTimeout(1)

	wf.Job("done").Needs(test).NeedsID("flaky", "optional").
		Run("summary", `echo "matrix complete; optional may have failed harmlessly"`)

	flow.RunWithTUI(wf)
}
