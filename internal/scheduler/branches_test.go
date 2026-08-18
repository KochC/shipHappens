package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

// TestExcludedDependencySkipped covers depsState's !included(dep) continue.
func TestExcludedDependencySkipped(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
		{ID: "b", Needs: []string{"a"}, Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	// Only run "b"; its dep "a" is NOT in the Only set, so depsState must skip it.
	res := Run(context.Background(), p, Options{
		Workdir: t.TempDir(), NoCache: true,
		Only: map[string]bool{"b": true},
	})
	if res.Failed || res.Ran != 1 {
		t.Fatalf("only b should run (excluded dep ignored): %+v", res)
	}
}

// TestCachedStepFailsOnRun covers execStep's cache-miss -> run -> error branch
// (a step that has a Cache spec but fails when executed).
func TestCachedStepFailsOnRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "in"), []byte("v"), 0o644)
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "exit 1",
			Cache: &compiler.CacheSpec{Inputs: []string{"in"}, Outputs: []string{"out"}}}}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work})
	if !res.Failed {
		t.Fatal("a cached step that exits non-zero must fail the job")
	}
}
