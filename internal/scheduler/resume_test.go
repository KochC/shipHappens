package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func TestResumeSkipsUnchangedJobs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "src.txt"), []byte("v1"), 0o644)

	mkPlan := func() *compiler.RunPlan {
		return &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
			{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "echo a > out_a.txt",
				Cache: &compiler.CacheSpec{Inputs: []string{"src.txt"}}}}, Outputs: []string{"out_a.txt"}},
			{ID: "b", Needs: []string{"a"}, Steps: []compiler.StepPlan{{ID: "s", Run: "echo b > out_b.txt",
				Cache: &compiler.CacheSpec{Inputs: []string{"src.txt"}}}}, Outputs: []string{"out_b.txt"}},
		}}
	}

	// First run: both jobs execute.
	r1 := Run(context.Background(), mkPlan(), Options{Workdir: work, Resume: true})
	if r1.Failed || r1.Ran == 0 {
		t.Fatalf("run1 unexpected: %+v", r1)
	}
	if r1.Resumed != 0 {
		t.Fatalf("run1 should resume nothing, got %d", r1.Resumed)
	}

	// Second run, no input change: both jobs skipped via resume.
	r2 := Run(context.Background(), mkPlan(), Options{Workdir: work, Resume: true})
	if r2.Failed {
		t.Fatalf("run2 failed: %+v", r2)
	}
	if r2.Resumed != 2 {
		t.Fatalf("run2 should resume 2 jobs, got %d (ran=%d)", r2.Resumed, r2.Ran)
	}

	// Change input to a: a re-runs; b depends on a's fingerprint so it re-runs too.
	os.WriteFile(filepath.Join(work, "src.txt"), []byte("v2"), 0o644)
	r3 := Run(context.Background(), mkPlan(), Options{Workdir: work, Resume: true})
	if r3.Failed {
		t.Fatalf("run3 failed: %+v", r3)
	}
	if r3.Resumed == 2 {
		t.Fatal("run3 should NOT resume both after input change")
	}
}
