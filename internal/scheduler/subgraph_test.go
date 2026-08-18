package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

func TestStepDAGParallelAndOrder(t *testing.T) {
	work := t.TempDir()
	// setup + compile run in parallel; smoke needs both; push needs smoke.
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "deploy", Steps: []compiler.StepPlan{
			{ID: "setup", Run: `sleep 0.3 && echo setup >> order.txt`},
			{ID: "compile", Run: `sleep 0.3 && echo compile >> order.txt`},
			{ID: "smoke", Run: `echo smoke >> order.txt`, Needs: []string{"setup", "compile"}},
			{ID: "push", Run: `echo push >> order.txt`, Needs: []string{"smoke"}},
		}},
	}}
	start := time.Now()
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	// parallel setup+compile → total < sum (0.6s) — allow slack
	if time.Since(start) > 550*time.Millisecond {
		t.Errorf("setup+compile should run in parallel, took %s", time.Since(start))
	}
	b, _ := os.ReadFile(filepath.Join(work, "order.txt"))
	lines := strings.Fields(string(b))
	// smoke must come after setup+compile; push last
	pos := map[string]int{}
	for i, l := range lines {
		pos[l] = i
	}
	if pos["smoke"] < pos["setup"] || pos["smoke"] < pos["compile"] {
		t.Errorf("smoke ran before its deps: %v", lines)
	}
	if pos["push"] != len(lines)-1 {
		t.Errorf("push should be last: %v", lines)
	}
}

func TestStepDAGDependencyFailureSkips(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "j", Steps: []compiler.StepPlan{
			{ID: "a", Run: "exit 1"},
			{ID: "b", Run: `echo b > b.txt`, Needs: []string{"a"}},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if !res.Failed {
		t.Fatal("job should fail when a step fails")
	}
	if _, err := os.Stat(filepath.Join(work, "b.txt")); err == nil {
		t.Error("b should have been skipped (dependency failed)")
	}
}

func TestOnFailureHandlerRuns(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "j", Steps: []compiler.StepPlan{
			{ID: "suite", Run: "exit 1", ContinueOnError: true,
				OnFailure: []compiler.StepPlan{
					{ID: "report", Run: `echo reported > report.txt`},
				}},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("continue-on-error should keep run green: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(work, "report.txt")); err != nil {
		t.Error("onFailure handler should have run")
	}
}

func TestOnFailureNotRunOnSuccess(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "j", Steps: []compiler.StepPlan{
			{ID: "ok", Run: "true",
				OnFailure: []compiler.StepPlan{{ID: "report", Run: `echo x > report.txt`}}},
		}},
	}}
	Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if _, err := os.Stat(filepath.Join(work, "report.txt")); err == nil {
		t.Error("onFailure should not run when the step succeeds")
	}
}

func TestStepDAGSkipConditionInGraph(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "j", Steps: []compiler.StepPlan{
			{ID: "a", Run: "true"},
			{ID: "b", Run: `echo b > b.txt`, Needs: []string{"a"}, If: "false"},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("skipped step should not fail: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(work, "b.txt")); err == nil {
		t.Error("b should be skipped by if=false even in a DAG")
	}
}
