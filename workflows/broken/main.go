// Command broken is an intentionally-invalid pipeline used to demonstrate
// Ship Happens' fail-fast static validation. It has a missing dependency and a
// dependency cycle. Running it should exit non-zero before executing anything.
//
//	go run ./workflows/broken
package main

import "github.com/chris/shiphappens/flow"

func main() {
	wf := flow.New("Broken")

	a := wf.Job("build").
		Run("compile", "go build ./...")

	// references an unknown job "tset" (typo of "test")
	wf.Job("deploy").Needs(a).NeedsID("tset").
		Run("ship", "echo deploying")

	// cycle: x needs y, y needs x
	x := wf.Job("x").Run("s", "echo x")
	y := wf.Job("y").Run("s", "echo y")
	x.Needs(y)
	y.Needs(x)

	flow.Main(wf)
}
