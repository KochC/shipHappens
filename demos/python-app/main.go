// Command python-demo runs a real CI pipeline for the small Python app in
// demos/python-app, entirely in a container, shown in the live TUI.
//
//	go run ./demos/python-app        # lint + test + build, live dashboard
//	go run ./demos/python-app --job test
//	go run ./demos/python-app --resume
//	go run ./demos/python-app --no-tui   # stream tool logs (debug)
//
// The pipeline uses python:3.12-slim and installs ruff/pytest/build per job.
// It operates on the demos/python-app directory (its own working tree).
package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/KochC/shipHappens/flow"
)

const img = "python:3.12-slim"

// setup installs the toolchain inside the fresh container (each step is a new
// container, so tools are installed in the same step that uses them).
const setup = `set -e
export PIP_DISABLE_PIP_VERSION_CHECK=1 PIP_ROOT_USER_ACTION=ignore
pip install --quiet ruff pytest build >/dev/null
`

func main() {
	chdirToDemo()
	wf := flow.New("Python CI")

	lint := wf.Job("lint").Image(img).
		Run("ruff", setup+`ruff check .`).
		Cache(flow.Inputs("**/*.py", "pyproject.toml"))

	test := wf.Job("test").Image(img).
		Run("pytest", setup+`pytest -q`).
		Cache(flow.Inputs("**/*.py", "pyproject.toml"))

	wf.Job("build").Needs(lint, test).Image(img).
		Run("wheel", setup+`python -m build --wheel --outdir dist . >/dev/null && ls -la dist`).
		Cache(flow.Inputs("**/*.py", "pyproject.toml")).
		Outputs("dist/**").
		CleanAfter("build", "*.egg-info")

	flow.RunWithTUI(wf)
}

// chdirToDemo makes the demos/python-app directory the working tree regardless
// of where the program is launched from.
func chdirToDemo() {
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		_ = os.Chdir(filepath.Dir(file))
	}
}
