// Command ci is an example Ship Happens pipeline, authored entirely in Go.
//
//	go run ./workflows/ci            # compile, validate, run (parallel + cached)
//	go run ./workflows/ci --graph    # print the DAG only
//	go run ./workflows/ci --job test # run one job and its deps
//	go run ./workflows/ci --no-cache # force a full rerun
//	go run ./workflows/ci --changed  # only run jobs affected by git changes
package main

import "github.com/chris/shiphappens/flow"

func main() {
	wf := flow.New("CI")

	checkout := wf.Job("checkout").
		Run("rev", "git rev-parse HEAD || echo no-git")

	lint := wf.Job("lint").Needs(checkout).
		Run("vet", "go vet ./... && echo lint-ok").
		Cache(flow.Inputs("**/*.go"))

	test := wf.Job("test").Needs(checkout).
		Run("unit", "sleep 1 && echo tests-passed")

	wf.Job("build").Needs(lint, test).
		Run("compile", "go build ./... && echo built").
		Cache(flow.Inputs("**/*.go"), flow.Outputs("bin/**"))

	flow.Main(wf)
}
