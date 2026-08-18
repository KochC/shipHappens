package flow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chris/shiphappens/internal/changed"
	"github.com/chris/shiphappens/internal/logs"
)

func quietLogs(t *testing.T) {
	t.Helper()
	prev := logs.SetOutput(&nopWriter{})
	t.Cleanup(func() { logs.SetOutput(prev) })
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func simpleWorkflow() *Workflow {
	wf := New("T")
	a := wf.Job("a").Run("s", "true")
	wf.Job("b").Needs(a).Run("s", "true")
	return wf
}

func TestParseFlags(t *testing.T) {
	o := parseFlags("T", []string{"--graph", "--job", "build", "--engine", "podman",
		"--no-cache", "--resume", "--tui", "--no-preheat", "--mount", "v:/c", "--mount", "w:/d", "--changed=dev"})
	if !o.graphOnly || o.jobFlag != "build" || o.engine != "podman" || !o.noCache ||
		!o.resume || !o.useTUI || !o.noPreheat {
		t.Fatalf("flags not parsed: %+v", o)
	}
	if len(o.mounts) != 2 || o.mounts[0] != "v:/c" {
		t.Fatalf("mounts wrong: %v", o.mounts)
	}
	if !o.changedSet || o.changedRef != "dev" {
		t.Fatalf("changed wrong: set=%v ref=%q", o.changedSet, o.changedRef)
	}
}

func TestParseFlagsBareChanged(t *testing.T) {
	o := parseFlags("T", []string{"--changed"})
	if !o.changedSet {
		t.Fatal("bare --changed should set changedSet")
	}
}

func TestRunCompileError(t *testing.T) {
	quietLogs(t)
	wf := New("T")
	x := wf.Job("x").Run("s", "e")
	y := wf.Job("y").Run("s", "e")
	x.Needs(y)
	y.Needs(x) // cycle -> compile error
	if code := run(wf, runOpts{}); code != 1 {
		t.Fatalf("cycle should exit 1, got %d", code)
	}
}

func TestRunGraphOnly(t *testing.T) {
	quietLogs(t)
	if code := run(simpleWorkflow(), runOpts{graphOnly: true}); code != 0 {
		t.Fatalf("graph-only should exit 0, got %d", code)
	}
}

func TestRunCompileArtifact(t *testing.T) {
	quietLogs(t)
	out := filepath.Join(t.TempDir(), "plan.json")
	if code := run(simpleWorkflow(), runOpts{compileOnly: out}); code != 0 {
		t.Fatal("compile should exit 0")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("plan not written: %v", err)
	}
}

func TestRunCompileWriteError(t *testing.T) {
	quietLogs(t)
	// unwritable path -> writePlan error
	bad := filepath.Join(t.TempDir(), "nope", "plan.json")
	if code := run(simpleWorkflow(), runOpts{compileOnly: bad}); code != 1 {
		t.Fatal("bad compile path should exit 1")
	}
}

func TestRunUnknownJob(t *testing.T) {
	quietLogs(t)
	if code := run(simpleWorkflow(), runOpts{jobFlag: "nope"}); code != 1 {
		t.Fatal("unknown job should exit 1")
	}
}

func TestRunSuccess(t *testing.T) {
	quietLogs(t)
	wd := t.TempDir()
	prev := getwd
	getwd = func() (string, error) { return wd, nil }
	t.Cleanup(func() { getwd = prev })
	if code := run(simpleWorkflow(), runOpts{noCache: true}); code != 0 {
		t.Fatal("simple native run should pass")
	}
}

func TestRunFailingJob(t *testing.T) {
	quietLogs(t)
	wf := New("T")
	wf.Job("a").Run("s", "exit 1")
	if code := run(wf, runOpts{noCache: true}); code != 1 {
		t.Fatal("failing job should exit 1")
	}
}

func TestRunJobSubset(t *testing.T) {
	quietLogs(t)
	if code := run(simpleWorkflow(), runOpts{jobFlag: "a", noCache: true}); code != 0 {
		t.Fatal("job subset run should pass")
	}
}

func TestRunChangedNoFiles(t *testing.T) {
	quietLogs(t)
	prev := stubChangedFiles(t, nil, nil)
	_ = prev
	if code := run(simpleWorkflow(), runOpts{changedSet: true, changedRef: "main"}); code != 0 {
		t.Fatal("no changes should exit 0")
	}
}

func TestRunChangedError(t *testing.T) {
	quietLogs(t)
	stubChangedFiles(t, nil, errors.New("git fail"))
	if code := run(simpleWorkflow(), runOpts{changedSet: true}); code != 1 {
		t.Fatal("git error should exit 1")
	}
}

func TestRunChangedWithFiles(t *testing.T) {
	quietLogs(t)
	wd := t.TempDir()
	prev := getwd
	getwd = func() (string, error) { return wd, nil }
	t.Cleanup(func() { getwd = prev })
	stubChangedFiles(t, []string{"x.go"}, nil)
	if code := run(simpleWorkflow(), runOpts{changedSet: true, noCache: true}); code != 0 {
		t.Fatal("changed run should pass")
	}
}

func TestRunTUI(t *testing.T) {
	quietLogs(t)
	t.Setenv("NO_COLOR", "1")
	wd := t.TempDir()
	prev := getwd
	getwd = func() (string, error) { return wd, nil }
	t.Cleanup(func() { getwd = prev })
	if code := run(simpleWorkflow(), runOpts{useTUI: true, noCache: true}); code != 0 {
		t.Fatal("tui run should pass")
	}
}

func TestRunPreheat(t *testing.T) {
	quietLogs(t)
	wd := t.TempDir()
	prev := getwd
	getwd = func() (string, error) { return wd, nil }
	t.Cleanup(func() { getwd = prev })
	restore := stubPreheat(t, nil)
	defer restore()
	wf := simpleWorkflow()
	wf.Preheat(Preheat{Image: "img"})
	if code := run(wf, runOpts{noCache: true}); code != 0 {
		t.Fatal("run with (stubbed) preheat should pass")
	}
}

func TestMainInvokesRunAndExit(t *testing.T) {
	quietLogs(t)
	wd := t.TempDir()
	prevWd := getwd
	getwd = func() (string, error) { return wd, nil }
	t.Cleanup(func() { getwd = prevWd })

	var gotCode int
	prevExit := osExit
	osExit = func(code int) { gotCode = code }
	t.Cleanup(func() { osExit = prevExit })

	prevArgs := os.Args
	os.Args = []string{"prog", "--graph"}
	t.Cleanup(func() { os.Args = prevArgs })

	Main(simpleWorkflow())
	if gotCode != 0 {
		t.Fatalf("Main --graph should exit 0, got %d", gotCode)
	}
}

// stubChangedFiles swaps changed.Files via the package-level indirection.
func stubChangedFiles(t *testing.T, files []string, err error) func() {
	t.Helper()
	prev := changed.FilesFn
	changed.FilesFn = func(string, string) ([]string, error) { return files, err }
	t.Cleanup(func() { changed.FilesFn = prev })
	return func() { changed.FilesFn = prev }
}
