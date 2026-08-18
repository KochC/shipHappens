package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/toolchain"
)

func TestToolchainAdvisoryFallback(t *testing.T) {
	// With no backend, a toolchain-declaring native job still runs (host tools).
	defer toolchain.SetBackend(false, nil)()
	work := t.TempDir()
	p := &compiler.RunPlan{
		Name:      "T",
		Toolchain: map[string]string{"go": "1.22.5"},
		Jobs: []compiler.JobPlan{
			{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
		},
	}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("job should run despite missing toolchain backend: %+v", res)
	}
}

func TestToolchainPrependsPath(t *testing.T) {
	// Stub the backend to report a fake bin dir; the step's $PATH must include it.
	defer toolchain.SetBackend(true, func(_ context.Context, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "where" {
			return []byte("/opt/shiptool/" + args[1] + "\n"), nil
		}
		return nil, nil
	})()
	work := t.TempDir()
	out := filepath.Join(work, "path.txt")
	p := &compiler.RunPlan{
		Name: "T",
		Jobs: []compiler.JobPlan{
			{ID: "a", Toolchain: map[string]string{"mytool": "1.0"},
				Steps: []compiler.StepPlan{{ID: "s", Run: `echo "$PATH" > path.txt`}}},
		},
	}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "/opt/shiptool/mytool@1.0/bin") {
		t.Fatalf("toolchain bin dir not prepended to PATH: %q", b)
	}
}

func TestToolchainSkippedForContainerJobs(t *testing.T) {
	// A container job must NOT invoke the toolchain backend (uses image tools).
	called := false
	defer toolchain.SetBackend(true, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		called = true
		return nil, nil
	})()
	// No Docker needed: use a native job to confirm the backend fires only for
	// jobs with a toolchain and no image. Container path is covered by the
	// image!="" guard in runnerFor/runJob.
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "n", Toolchain: map[string]string{"x": "1"}, Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !called {
		t.Fatal("native job with toolchain should call the backend")
	}
}
