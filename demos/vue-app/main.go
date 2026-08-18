// Command vue-demo runs a real CI pipeline for the small Vue 3 + Vite project
// in demos/vue-app/app, entirely in a container, shown in the live TUI.
//
//	go run ./demos/vue-app          # install → {test, build}, live dashboard
//	go run ./demos/vue-app --job test
//	go run ./demos/vue-app --resume
//	go run ./demos/vue-app --no-tui # stream tool logs (debug)
//
// It uses node:20-alpine. The install job populates node_modules on the shared
// working tree; test and build depend on it and run in parallel.
package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/chris/shiphappens/flow"
)

const img = "node:20-alpine"

const prelude = `set -e
cd app
`

func main() {
	chdirToDemo()
	wf := flow.New("Vue CI")

	// Install deps once; downstream jobs reuse node_modules from the shared tree.
	install := wf.Job("install").Image(img).
		Run("npm ci", prelude+`npm install --no-audit --no-fund --loglevel=error`).
		Cache(flow.Inputs("app/package.json")).
		Outputs("app/node_modules/**")

	wf.Job("test").Needs(install).Image(img).
		Run("vitest", prelude+`npm test`).
		Cache(flow.Inputs("app/src/**", "app/package.json", "app/vite.config.js"))

	wf.Job("build").Needs(install).Image(img).
		Run("vite build", prelude+`npm run build && ls -la dist`).
		Cache(flow.Inputs("app/src/**", "app/index.html", "app/package.json", "app/vite.config.js")).
		Outputs("app/dist/**")

	flow.RunWithTUI(wf)
}

// chdirToDemo makes demos/vue-app the working tree regardless of launch dir.
func chdirToDemo() {
	if _, err := os.Stat("app/package.json"); err == nil {
		return
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		_ = os.Chdir(filepath.Dir(file))
	}
}
