package changed

import (
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
	"github.com/KochC/shipHappens/internal/graph"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		file, glob string
		want       bool
	}{
		{"src/main.go", "src/**", true},
		{"src/a/b/main.go", "src/**", true},
		{"docs/readme.md", "src/**", false},
		{"package-lock.json", "package-lock.json", true},
		{"a/package-lock.json", "package-lock.json", true}, // basename match
		{"src/x.go", "**/*.go", true},
		{"src/x.txt", "**/*.go", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.file, c.glob); got != c.want {
			t.Errorf("matchGlob(%q,%q)=%v want %v", c.file, c.glob, got, c.want)
		}
	}
}

func mkPlan() *compiler.RunPlan {
	return &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "checkout", Steps: []compiler.StepPlan{
			{Cache: &compiler.CacheSpec{Inputs: []string{".gitref"}}}}},
		{ID: "lint", Needs: []string{"checkout"}, Steps: []compiler.StepPlan{
			{Cache: &compiler.CacheSpec{Inputs: []string{"src/**"}}}}},
		{ID: "docs", Needs: []string{"checkout"}, Steps: []compiler.StepPlan{
			{Cache: &compiler.CacheSpec{Inputs: []string{"docs/**"}}}}},
		{ID: "build", Needs: []string{"lint"}, Steps: []compiler.StepPlan{
			{Cache: &compiler.CacheSpec{Inputs: []string{"src/**"}}}}},
	}}
}

func TestAffectedJobsScopedToInputs(t *testing.T) {
	p := mkPlan()
	d := graph.Build(p)
	got := AffectedJobs(p, d, []string{"src/x.go"})

	// lint matches src/** -> affected; build depends on lint -> affected.
	// docs matches docs/** only and checkout matches .gitref only -> NOT affected.
	for _, id := range []string{"lint", "build"} {
		if !got[id] {
			t.Errorf("expected %s affected; got %v", id, got)
		}
	}
	if got["docs"] {
		t.Errorf("docs should NOT be affected by src change; got %v", got)
	}
	if got["checkout"] {
		t.Errorf("checkout should NOT be affected by src change; got %v", got)
	}
}

func TestAffectedJobsNoInputsIsConservative(t *testing.T) {
	// A job with no declared inputs is always considered affected, and its
	// dependents inherit that (conservative correctness).
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "root"}, // no inputs
		{ID: "child", Needs: []string{"root"}, Steps: []compiler.StepPlan{
			{Cache: &compiler.CacheSpec{Inputs: []string{"never/**"}}}}},
	}}
	d := graph.Build(p)
	got := AffectedJobs(p, d, []string{"unrelated.txt"})
	if !got["root"] || !got["child"] {
		t.Errorf("no-input root and its dependents must be conservatively affected; got %v", got)
	}
}

func TestAffectedJobsDocsChange(t *testing.T) {
	p := mkPlan()
	d := graph.Build(p)
	got := AffectedJobs(p, d, []string{"docs/readme.md"})
	if !got["docs"] {
		t.Error("docs should be affected by docs change")
	}
	if got["lint"] || got["build"] {
		t.Errorf("lint/build should not be affected by docs-only change; got %v", got)
	}
}
