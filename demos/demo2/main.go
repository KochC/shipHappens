// Command demo2 shows the Ship Happens live TUI handling failure: one job in
// the middle of the DAG fails, the scheduler cancels in-flight work (fail-fast),
// and downstream jobs are marked skipped (◌).
//
//	go run ./demos/demo2
//
// Watch: lint ✓, test ✗ (fails), and the jobs depending on test flip to
// skipped, while an unrelated parallel branch still completes. Exits non-zero.
package main

import "github.com/KochC/shipHappens/flow"

func main() {
	wf := flow.New("Fail-Fast Demo")

	checkout := wf.Job("checkout").
		Run("clone", "sleep 0.5")

	// This branch fails.
	test := wf.Job("test").Needs(checkout).
		Run("unit", "sleep 1.0").
		Run("integration", "sleep 0.5 && echo 'boom: assertion failed' && exit 1")

	// These depend on the failing job -> will be skipped.
	build := wf.Job("build").Needs(test).
		Run("compile", "sleep 1.0")
	wf.Job("deploy").Needs(build).
		Run("ship", "sleep 0.5")

	// Independent branch that should still finish.
	wf.Job("docs").Needs(checkout).
		Run("build-docs", "sleep 1.4")

	flow.RunWithTUI(wf)
}
