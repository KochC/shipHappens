package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func writePlanFile(t *testing.T, name, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

const validPlanJSON = `{
  "name": "FromFile",
  "jobs": [
    {"id": "a", "runsOn": "native", "steps": [{"id": "s", "run": "true"}]},
    {"id": "b", "runsOn": "native", "needs": ["a"], "steps": [{"id": "s", "run": "true"}]}
  ]
}`

func TestRunFileGraphOnly(t *testing.T) {
	quietLogs(t)
	f := writePlanFile(t, "plan.json", validPlanJSON)
	if code := RunFile(f, []string{"--graph"}); code != 0 {
		t.Fatalf("RunFile --graph should exit 0, got %d", code)
	}
}

func TestRunFileExecutes(t *testing.T) {
	quietLogs(t)
	wd := t.TempDir()
	prev := getwd
	getwd = func() (string, error) { return wd, nil }
	t.Cleanup(func() { getwd = prev })

	f := writePlanFile(t, "plan.json", validPlanJSON)
	if code := RunFile(f, []string{"--no-cache"}); code != 0 {
		t.Fatalf("RunFile run should exit 0, got %d", code)
	}
}

func TestRunFileLoadError(t *testing.T) {
	quietLogs(t)
	if code := RunFile(filepath.Join(t.TempDir(), "missing.json"), nil); code != 1 {
		t.Fatal("missing file should exit 1")
	}
}

func TestRunFileValidationError(t *testing.T) {
	quietLogs(t)
	// A cycle → validator rejects it.
	bad := `{"name":"Bad","jobs":[
      {"id":"x","runsOn":"native","needs":["y"],"steps":[{"id":"s","run":"e"}]},
      {"id":"y","runsOn":"native","needs":["x"],"steps":[{"id":"s","run":"e"}]}]}`
	f := writePlanFile(t, "bad.json", bad)
	if code := RunFile(f, nil); code != 1 {
		t.Fatal("cyclic plan should exit 1")
	}
}

func TestMainFileExits(t *testing.T) {
	quietLogs(t)
	var got int
	prevExit := osExit
	osExit = func(c int) { got = c }
	t.Cleanup(func() { osExit = prevExit })

	f := writePlanFile(t, "plan.json", validPlanJSON)
	MainFile(f, []string{"--graph"})
	if got != 0 {
		t.Fatalf("MainFile --graph should exit 0, got %d", got)
	}
}
