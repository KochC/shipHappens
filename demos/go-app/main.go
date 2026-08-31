// Command go-demo runs a real CI pipeline for the small Go module in
// demos/go-app/src, entirely in a container, shown in the live TUI.
//
//	go run ./demos/go-app          # vet + test + build, live dashboard
//	go run ./demos/go-app --job test
//	go run ./demos/go-app --resume
//	go run ./demos/go-app --no-tui # stream tool logs (debug)
//
// The pipeline uses the golang:1.22 image and operates on the src/ module
// (which has its own go.mod, separate from Ship Happens).
package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/KochC/shipHappens/flow"
)

const img = "golang:1.22-alpine"

// prelude keeps the Go build/module caches inside the working tree so parallel
// container jobs share them and don't re-download on every step.
const prelude = `set -e
export GOFLAGS=-mod=mod GOCACHE=/ship/work/.gocache GOMODCACHE=/ship/work/.gomodcache
cd src
`

func main() {
	chdirToDemo()
	wf := flow.New("Go CI")

	vet := wf.Job("vet").Image(img).
		Run("go vet", prelude+`go vet ./...`).
		Cache(flow.Inputs("src/**/*.go", "src/go.mod"))

	test := wf.Job("test").Image(img).
		Run("go test", prelude+`go test ./... -count=1`).
		Cache(flow.Inputs("src/**/*.go", "src/go.mod"))

	wf.Job("build").Needs(vet, test).Image(img).
		Run("go build", prelude+`go build -o ../bin/calcdemo . && ls -la ../bin`).
		Cache(flow.Inputs("src/**/*.go", "src/go.mod")).
		Outputs("bin/**").
		CleanAfter("src/.gocache", "src/.gomodcache")

	flow.RunWithTUI(wf)
}

// chdirToDemo makes demos/go-app the working tree regardless of launch dir.
func chdirToDemo() {
	if _, err := os.Stat("src/go.mod"); err == nil {
		return
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		_ = os.Chdir(filepath.Dir(file))
	}
}
