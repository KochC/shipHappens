package scheduler

import (
	"context"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func step(run string) compiler.StepPlan { return compiler.StepPlan{ID: "s", Run: run} }

func TestRunSuccess(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{step("true")}},
		{ID: "b", Needs: []string{"a"}, Steps: []compiler.StepPlan{step("true")}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if res.Failed {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.Ran != 2 {
		t.Fatalf("expected 2 steps ran, got %d", res.Ran)
	}
}

func TestFailFastCascades(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{step("exit 1")}},
		{ID: "b", Needs: []string{"a"}, Steps: []compiler.StepPlan{step("true")}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !res.Failed {
		t.Fatal("expected failure to propagate")
	}
}

func TestOnlySubset(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{step("true")}},
		{ID: "b", Steps: []compiler.StepPlan{step("true")}},
	}}
	res := Run(context.Background(), p, Options{
		Workdir: t.TempDir(), NoCache: true,
		Only: map[string]bool{"a": true},
	})
	if res.Failed || res.Ran != 1 {
		t.Fatalf("expected only 1 job to run, got %+v", res)
	}
}
