package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

// TestObserverReceivesEvents covers the emit-with-observer branch.
func TestObserverReceivesEvents(t *testing.T) {
	var kinds []EventKind
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	Run(context.Background(), p, Options{
		Workdir: t.TempDir(), NoCache: true,
		Observer: func(e Event) { kinds = append(kinds, e.Kind) },
	})
	var started, finished bool
	for _, k := range kinds {
		if k == JobStarted {
			started = true
		}
		if k == JobFinished {
			finished = true
		}
	}
	if !started || !finished {
		t.Fatalf("observer should see start+finish, got %v", kinds)
	}
}

// TestStepCacheHitAndSave covers execStep's cache branches (miss->save, hit->restore).
func TestStepCacheHitAndSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "in.txt"), []byte("v1"), 0o644)

	mk := func() *compiler.RunPlan {
		return &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
			{ID: "a", Steps: []compiler.StepPlan{{ID: "s",
				Run:   "echo out > out.txt",
				Cache: &compiler.CacheSpec{Inputs: []string{"in.txt"}, Outputs: []string{"out.txt"}}}}},
		}}
	}
	// run 1: miss -> executes + saves
	r1 := Run(context.Background(), mk(), Options{Workdir: work})
	if r1.Failed || r1.Ran != 1 {
		t.Fatalf("run1: %+v", r1)
	}
	// delete output; run 2: cache hit -> restores without running
	os.Remove(filepath.Join(work, "out.txt"))
	r2 := Run(context.Background(), mk(), Options{Workdir: work})
	if r2.Failed || r2.Cached != 1 {
		t.Fatalf("run2 should be a cache hit: %+v", r2)
	}
	if _, err := os.Stat(filepath.Join(work, "out.txt")); err != nil {
		t.Fatal("cache hit should restore output")
	}
}

// TestBlockedDependencyCascades covers depsState blocked branch + JobSkipped.
func TestBlockedDependencyCascades(t *testing.T) {
	var skipped []string
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "exit 1"}}},
		{ID: "b", Needs: []string{"a"}, Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
		{ID: "c", Needs: []string{"b"}, Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	res := Run(context.Background(), p, Options{
		Workdir: t.TempDir(), NoCache: true,
		Observer: func(e Event) {
			if e.Kind == JobSkipped {
				skipped = append(skipped, e.Job)
			}
		},
	})
	if !res.Failed {
		t.Fatal("expected failure")
	}
	if len(skipped) == 0 {
		t.Fatal("dependents of a failed job should be skipped")
	}
}

// TestResumeFingerprintErrorFallsThrough: a bad input glob makes fingerprint
// return "", so the job runs normally rather than resuming.
func TestResumeFingerprintErrorRunsJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a",
			Steps:   []compiler.StepPlan{{ID: "s", Run: "true", Cache: &compiler.CacheSpec{Inputs: []string{"[bad"}}}},
			Outputs: []string{"none"}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), Resume: true})
	if res.Failed {
		t.Fatalf("job with unfingerprintable inputs should still run: %+v", res)
	}
}
