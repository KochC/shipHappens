package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/runner"
)

func TestCleanAfterPrunesOnSuccess(t *testing.T) {
	work := t.TempDir()
	// job creates a build dir then it should be pruned by CleanAfter.
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a",
			Steps:      []compiler.StepPlan{{ID: "s", Run: "mkdir -p buildtmp && echo x > buildtmp/f"}},
			CleanAfter: []string{"buildtmp"}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(work, "buildtmp")); !os.IsNotExist(err) {
		t.Fatalf("CleanAfter should have removed buildtmp; stat err=%v", err)
	}
}

func TestCleanAfterKeptOnFailure(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a",
			Steps:      []compiler.StepPlan{{ID: "s", Run: "mkdir -p buildtmp && exit 1"}},
			CleanAfter: []string{"buildtmp"}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if !res.Failed {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(filepath.Join(work, "buildtmp")); err != nil {
		t.Fatalf("failed job's dir should be kept for inspection; stat err=%v", err)
	}
}

func TestRunnerForDispatch(t *testing.T) {
	s := &scheduler{opts: Options{Engine: "docker", Workdir: "/w"}}

	if _, ok := s.runnerFor(&compiler.JobPlan{ID: "n"}, "").(runner.NativeRunner); !ok {
		t.Error("no image -> NativeRunner")
	}
	if _, ok := s.runnerFor(&compiler.JobPlan{ID: "c", Image: "img"}, "").(runner.ContainerRunner); !ok {
		t.Error("image -> ContainerRunner")
	}
	ov := s.runnerFor(&compiler.JobPlan{ID: "o", Image: "img", Overlay: true}, "")
	if or, ok := ov.(runner.OverlayRunner); !ok {
		t.Error("image+overlay -> OverlayRunner")
	} else if or.UpperHost == "" {
		t.Error("OverlayRunner should have an UpperHost path")
	}
}

func TestRunnerForOfflineByDefault(t *testing.T) {
	no := false
	plan := &compiler.RunPlan{
		Security: &compiler.SecurityPolicy{OfflineByDefault: true},
	}
	s := &scheduler{plan: plan, opts: Options{Engine: "docker"}}

	// offline-by-default container job → Network false
	r := s.runnerFor(&compiler.JobPlan{ID: "a", Image: "img"}, "")
	cr, ok := r.(runner.ContainerRunner)
	if !ok || cr.Network == nil || *cr.Network != false {
		t.Fatalf("offline default should set Network=false, got %+v", cr)
	}

	// explicit network=true overrides policy
	r = s.runnerFor(&compiler.JobPlan{ID: "b", Image: "img", Network: &[]bool{true}[0]}, "")
	cr = r.(runner.ContainerRunner)
	if cr.Network == nil || *cr.Network != true {
		t.Fatalf("explicit network=true should win, got %+v", cr)
	}

	// allow-list opts into network + carries allow
	r = s.runnerFor(&compiler.JobPlan{ID: "c", Image: "img", Allow: []string{"npm.example"}}, "")
	cr = r.(runner.ContainerRunner)
	if cr.Network == nil || *cr.Network != true || len(cr.Allow) != 1 {
		t.Fatalf("allow-list should opt into network with allow, got %+v", cr)
	}

	// services network overrides policy
	r = s.runnerFor(&compiler.JobPlan{ID: "d", Image: "img"}, "svc-net")
	cr = r.(runner.ContainerRunner)
	if cr.NetworkName != "svc-net" {
		t.Fatalf("services network should be used, got %+v", cr)
	}
	_ = no
}
