// Command demo1 shows the Ship Happens live TUI on a parallel build pipeline:
// a fan-out/fan-in DAG where lint, test, and typecheck run concurrently after
// checkout, then build waits for all three, then package.
//
//	go run ./demos/demo1
//
// Watch the ▶ (running) marks light up in parallel and flip to ✓ as each job
// finishes, with live elapsed timers. The --tui flag is enabled automatically.
package main

import "github.com/KochC/shipHappens/flow"

func main() {
	wf := flow.New("Parallel Build")

	checkout := wf.Job("checkout").
		Run("clone", "sleep 0.6")

	lint := wf.Job("lint").Needs(checkout).
		Run("eslint", "sleep 1.5")
	test := wf.Job("test").Needs(checkout).
		Run("unit", "sleep 2.2")
	typecheck := wf.Job("typecheck").Needs(checkout).
		Run("tsc", "sleep 1.1")

	build := wf.Job("build").Needs(lint, test, typecheck).
		Run("compile", "sleep 1.3")

	wf.Job("package").Needs(build).
		Run("bundle", "sleep 0.8")

	flow.RunWithTUI(wf)
}
