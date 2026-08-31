package validator

import (
	"strings"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func plan(jobs ...compiler.JobPlan) *compiler.RunPlan {
	return &compiler.RunPlan{Name: "T", Jobs: jobs}
}

func job(id string, needs ...string) compiler.JobPlan {
	return compiler.JobPlan{
		ID: id, RunsOn: "native", Needs: needs,
		Steps: []compiler.StepPlan{{ID: "s", Run: "echo hi"}},
	}
}

func msgs(ds []Diagnostic) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(d.Msg)
		b.WriteString("\n")
	}
	return b.String()
}

func TestValidOK(t *testing.T) {
	p := plan(job("a"), job("b", "a"))
	if d := Validate(p, nil); len(d) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", d)
	}
}

func TestNoJobs(t *testing.T) {
	if d := Validate(plan(), nil); len(d) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", d)
	}
}

func TestDuplicateID(t *testing.T) {
	d := Validate(plan(job("a"), job("a")), nil)
	if !strings.Contains(msgs(d), "duplicate job id") {
		t.Fatalf("expected duplicate diagnostic, got: %s", msgs(d))
	}
}

func TestUnknownNeedWithSuggestion(t *testing.T) {
	d := Validate(plan(job("test"), job("build", "tset")), nil)
	m := msgs(d)
	if !strings.Contains(m, `needs "tset"`) {
		t.Fatalf("expected unknown-need, got: %s", m)
	}
	if !strings.Contains(m, "did you mean: test") {
		t.Fatalf("expected suggestion, got: %s", m)
	}
}

func TestSelfNeed(t *testing.T) {
	d := Validate(plan(job("a", "a")), nil)
	if !strings.Contains(msgs(d), "needs itself") {
		t.Fatalf("expected self-need diagnostic, got: %s", msgs(d))
	}
}

func TestEmptySteps(t *testing.T) {
	p := plan(compiler.JobPlan{ID: "a", RunsOn: "native"})
	if !strings.Contains(msgs(Validate(p, nil)), "no steps") {
		t.Fatal("expected no-steps diagnostic")
	}
}

func TestCycle(t *testing.T) {
	d := Validate(plan(job("x", "y"), job("y", "x")), nil)
	if !strings.Contains(msgs(d), "dependency cycle") {
		t.Fatalf("expected cycle diagnostic, got: %s", msgs(d))
	}
}
