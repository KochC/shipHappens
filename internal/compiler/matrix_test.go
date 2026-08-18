package compiler

import (
	"reflect"
	"sort"
	"testing"
)

func jobIDs(p *RunPlan) []string {
	ids := make([]string, len(p.Jobs))
	for i, j := range p.Jobs {
		ids[i] = j.ID
	}
	return ids
}

func findJob(p *RunPlan, id string) *JobPlan {
	for i := range p.Jobs {
		if p.Jobs[i].ID == id {
			return &p.Jobs[i]
		}
	}
	return nil
}

func TestExpandMatrixNil(t *testing.T) {
	var p *RunPlan
	if p.ExpandMatrix() != nil {
		t.Error("nil plan should expand to nil")
	}
}

func TestExpandMatrixNoMatrixReturnsSame(t *testing.T) {
	p := &RunPlan{Name: "X", Jobs: []JobPlan{
		{ID: "a", Steps: []StepPlan{{ID: "s", Run: "true"}}},
		{ID: "b", Needs: []string{"a"}},
	}}
	got := p.ExpandMatrix()
	if got != p {
		t.Error("a plan with no matrix jobs should be returned as-is (same pointer)")
	}
}

func TestExpandMatrixCartesianAndEnv(t *testing.T) {
	p := &RunPlan{Name: "M", Jobs: []JobPlan{
		{
			ID:     "test",
			Env:    map[string]string{"BASE": "1"},
			Matrix: map[string][]string{"os": {"linux", "mac"}, "go": {"1.21", "1.22"}},
			Steps:  []StepPlan{{ID: "info", Run: "echo $OS $GO"}},
		},
	}}
	out := p.ExpandMatrix()

	// Keys sorted (go, os) → suffix "<go>-<os>".
	want := []string{"test/1.21-linux", "test/1.21-mac", "test/1.22-linux", "test/1.22-mac"}
	got := jobIDs(out)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expanded ids = %v, want %v", got, want)
	}

	// Env: matrix values UPPERCASED + merged over the job env; Matrix cleared.
	j := findJob(out, "test/1.22-linux")
	if j == nil {
		t.Fatal("missing expansion")
	}
	if j.Env["OS"] != "linux" || j.Env["GO"] != "1.22" || j.Env["BASE"] != "1" {
		t.Errorf("env wrong: %v", j.Env)
	}
	if j.Matrix != nil {
		t.Error("expanded job should have no Matrix")
	}
	if j.Steps[0].Run != "echo $OS $GO" {
		t.Error("steps should be preserved")
	}
}

func TestExpandMatrixNeedsRemap(t *testing.T) {
	p := &RunPlan{Name: "M", Jobs: []JobPlan{
		{ID: "test", Matrix: map[string][]string{"os": {"linux", "mac"}}},
		{ID: "build", Needs: []string{"test"}},
		{ID: "deploy", Needs: []string{"build"}},
	}}
	out := p.ExpandMatrix()

	build := findJob(out, "build")
	want := []string{"test/linux", "test/mac"}
	got := append([]string(nil), build.Needs...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("build.needs = %v, want %v (all expansions)", got, want)
	}
	// Non-matrix need is preserved.
	deploy := findJob(out, "deploy")
	if !reflect.DeepEqual(deploy.Needs, []string{"build"}) {
		t.Errorf("deploy.needs = %v, want [build]", deploy.Needs)
	}
}

func TestExpandMatrixUnknownNeedPassthrough(t *testing.T) {
	p := &RunPlan{Name: "M", Jobs: []JobPlan{
		{ID: "a", Matrix: map[string][]string{"x": {"1"}}},
		{ID: "b", Needs: []string{"ghost"}},
	}}
	out := p.ExpandMatrix()
	b := findJob(out, "b")
	if !reflect.DeepEqual(b.Needs, []string{"ghost"}) {
		t.Errorf("unknown need should pass through for the validator: %v", b.Needs)
	}
}

func TestExpandMatrixSingleDimension(t *testing.T) {
	p := &RunPlan{Name: "M", Jobs: []JobPlan{
		{ID: "t", Matrix: map[string][]string{"os": {"linux", "mac", "win"}}},
	}}
	out := p.ExpandMatrix()
	if len(out.Jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d", len(out.Jobs))
	}
	// Suffix is just the value for a single dimension.
	if findJob(out, "t/linux") == nil || findJob(out, "t/win") == nil {
		t.Errorf("ids = %v", jobIDs(out))
	}
}

func TestCartesianDeterministicOrder(t *testing.T) {
	dims := map[string][]string{"b": {"2"}, "a": {"1"}}
	c := cartesian(dims)
	// Keys sorted a,b → suffix "1-2", env {A:1,B:2}.
	if len(c) != 1 || c[0].suffix != "1-2" {
		t.Fatalf("suffix = %q", c[0].suffix)
	}
	if c[0].env["A"] != "1" || c[0].env["B"] != "2" {
		t.Errorf("env = %v", c[0].env)
	}
}

func TestUpperKey(t *testing.T) {
	if upperKey("os") != "OS" || upperKey("go-version") != "GO-VERSION" || upperKey("A1") != "A1" {
		t.Error("upperKey")
	}
}
