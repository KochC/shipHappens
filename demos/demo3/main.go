// Command demo3 shows the Ship Happens live TUI with resume/incremental builds.
//
// Run it twice:
//
//	go run ./demos/demo3            # first run: every job executes (slow steps)
//	go run ./demos/demo3            # second run: unchanged jobs resume instantly
//	go run ./demos/demo3 --no-cache # force a full re-run
//
// Each job declares Outputs, so on the second run their fingerprints match the
// prior success and they are restored from ~/.ship/cache instead of re-running —
// the TUI shows them completing in milliseconds with a "(cached)" tag, and the
// summary reports "N resumed".
//
// Resume keys off this source file's content (declared as each job's cache
// input), so editing this file invalidates the affected jobs on the next run.
package main

import "github.com/KochC/shipHappens/flow"

func main() {
	wf := flow.New("Resume Demo")

	const src = "demos/demo3/main.go"

	compile := wf.Job("compile").
		Run("build", "sleep 1.5 && echo binary > .demo3-app").
		Cache(flow.Inputs(src)).
		Outputs(".demo3-app")

	assets := wf.Job("assets").
		Run("bundle", "sleep 1.2 && echo assets > .demo3-assets").
		Cache(flow.Inputs(src)).
		Outputs(".demo3-assets")

	wf.Job("package").Needs(compile, assets).
		Run("archive", "sleep 1.0 && echo pkg > .demo3-pkg").
		Cache(flow.Inputs(src)).
		Outputs(".demo3-pkg")

	// Resume is enabled programmatically so the demo works without extra flags.
	flow.RunWithTUIResume(wf)
}
