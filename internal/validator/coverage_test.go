package validator

import (
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func TestDiagnosticString(t *testing.T) {
	// with location
	d := Diagnostic{Loc: "main.go:12", Msg: "boom"}
	if !strings.Contains(d.String(), "main.go:12") || !strings.Contains(d.String(), "boom") {
		t.Errorf("located diagnostic: %q", d.String())
	}
	// without location
	d2 := Diagnostic{Msg: "no loc"}
	if d2.String() != "no loc" {
		t.Errorf("unlocated diagnostic: %q", d2.String())
	}
	// "?" location treated as none
	d3 := Diagnostic{Loc: "?", Msg: "m"}
	if d3.String() != "m" {
		t.Errorf("? loc: %q", d3.String())
	}
}

func TestValidateStepNoRunCommand(t *testing.T) {
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "a", RunsOn: "native", Steps: []compiler.StepPlan{{ID: "s", Run: ""}}},
	}}
	diags := Validate(p, nil)
	if !containsMsg(diags, "no run command") {
		t.Fatalf("expected no-run-command diagnostic: %v", diags)
	}
}

func TestValidateNoRunsOn(t *testing.T) {
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "a", RunsOn: "", Steps: []compiler.StepPlan{{ID: "s", Run: "x"}}},
	}}
	if !containsMsg(Validate(p, nil), "no runs-on") {
		t.Fatal("expected no runs-on diagnostic")
	}
}

func TestValidateUnknownNeedNoSuggestion(t *testing.T) {
	// A totally different need name so no close suggestion is offered.
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "a", RunsOn: "native", Needs: []string{"zzzzzzzz"},
			Steps: []compiler.StepPlan{{ID: "s", Run: "x"}}},
	}}
	if !containsMsg(Validate(p, nil), "unknown job") {
		t.Fatal("expected unknown-job diagnostic")
	}
}

func containsMsg(ds []Diagnostic, sub string) bool {
	for _, d := range ds {
		if strings.Contains(d.Msg, sub) {
			return true
		}
	}
	return false
}

func TestStepGraphUnknownAndCycle(t *testing.T) {
	// unknown step need
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "j", RunsOn: "native", Steps: []compiler.StepPlan{
			{ID: "a", Run: "x", Needs: []string{"ghost"}},
		}},
	}}
	if !containsMsg(Validate(p, nil), "unknown step") {
		t.Error("expected unknown-step diagnostic")
	}

	// self-reference
	p = &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "j", RunsOn: "native", Steps: []compiler.StepPlan{
			{ID: "a", Run: "x", Needs: []string{"a"}},
		}},
	}}
	if !containsMsg(Validate(p, nil), "needs itself") {
		t.Error("expected step self-need diagnostic")
	}

	// cycle
	p = &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "j", RunsOn: "native", Steps: []compiler.StepPlan{
			{ID: "a", Run: "x", Needs: []string{"b"}},
			{ID: "b", Run: "x", Needs: []string{"a"}},
		}},
	}}
	if !containsMsg(Validate(p, nil), "step cycle") {
		t.Error("expected step cycle diagnostic")
	}
}
