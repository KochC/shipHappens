package flow

import (
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func TestMatrixExpansion(t *testing.T) {
	wf := New("W")
	wf.Job("test").Matrix(map[string][]string{"os": {"linux", "mac"}, "go": {"1.21", "1.22"}}).
		Run("s", "echo $OS $GO")
	wf.Job("report").NeedsID("test").Run("s", "echo done")

	p := wf.ToPlan()
	// 4 matrix jobs + 1 report
	if len(p.Jobs) != 5 {
		t.Fatalf("expected 5 jobs, got %d: %+v", len(p.Jobs), jobIDs(p))
	}
	want := map[string]bool{
		"test/1.21-linux": true, "test/1.21-mac": true,
		"test/1.22-linux": true, "test/1.22-mac": true,
	}
	for _, j := range p.Jobs {
		if j.ID == "report" {
			// report depends on all 4 expansions
			if len(j.Needs) != 4 {
				t.Fatalf("report should need 4 jobs, got %v", j.Needs)
			}
			continue
		}
		if !want[j.ID] {
			t.Errorf("unexpected job id %q", j.ID)
		}
		// matrix env is set (uppercased keys)
		if j.Env["OS"] == "" || j.Env["GO"] == "" {
			t.Errorf("matrix env missing on %s: %+v", j.ID, j.Env)
		}
	}
}

func TestMatrixEnvMergesJobEnv(t *testing.T) {
	wf := New("W")
	wf.Job("j").Env("BASE", "x").Matrix(map[string][]string{"k": {"a"}}).Run("s", "e")
	p := wf.ToPlan()
	if len(p.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(p.Jobs))
	}
	j := p.Jobs[0]
	if j.Env["BASE"] != "x" || j.Env["K"] != "a" {
		t.Fatalf("env merge wrong: %+v", j.Env)
	}
}

func TestCartesianDeterministic(t *testing.T) {
	combos := cartesian(map[string][]string{"b": {"2"}, "a": {"1", "3"}})
	// keys sorted → a first; suffixes "1-2", "3-2"
	if len(combos) != 2 {
		t.Fatalf("expected 2 combos, got %d", len(combos))
	}
	got := map[string]bool{}
	for _, c := range combos {
		got[c.suffix] = true
		if c.env["A"] == "" || c.env["B"] != "2" {
			t.Errorf("combo env wrong: %+v", c.env)
		}
	}
	if !got["1-2"] || !got["3-2"] {
		t.Errorf("suffixes wrong: %+v", got)
	}
}

func TestLowerStepAndJobOptions(t *testing.T) {
	wf := New("W")
	wf.Job("j").Timeout(30).ContinueOnError().
		Run("s", "make").
		StepEnv("K", "V").WorkingDir("sub").Shell("bash").
		StepTimeout(10).Retry(3, 5).StepContinueOnError()

	j := wf.ToPlan().Jobs[0]
	if j.TimeoutSec != 30 || !j.ContinueOnError {
		t.Fatalf("job opts wrong: timeout=%d coe=%v", j.TimeoutSec, j.ContinueOnError)
	}
	s := j.Steps[0]
	if s.Env["K"] != "V" || s.WorkingDir != "sub" || s.Shell != "bash" {
		t.Fatalf("step env/dir/shell wrong: %+v", s)
	}
	if s.TimeoutSec != 10 || s.Retries != 3 || s.RetryBackoffSec != 5 || !s.ContinueOnError {
		t.Fatalf("step timeout/retry/coe wrong: %+v", s)
	}
}

func TestStepOptionsNoStepNoop(t *testing.T) {
	// calling step-configuring methods before any Run is a safe no-op
	wf := New("W")
	wf.Job("j").StepEnv("K", "V").WorkingDir("d").Shell("bash").
		StepTimeout(1).Retry(1).StepContinueOnError().
		Run("s", "e")
	s := wf.ToPlan().Jobs[0].Steps[0]
	if s.Env != nil || s.WorkingDir != "" || s.Shell != "" || s.Retries != 0 {
		t.Fatalf("options should not have attached to the later step: %+v", s)
	}
}

func jobIDs(p *compiler.RunPlan) []string {
	var ids []string
	for _, j := range p.Jobs {
		ids = append(ids, j.ID)
	}
	return ids
}
