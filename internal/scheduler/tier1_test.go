package scheduler

import (
	"context"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func TestStepContinueOnError(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{
			{ID: "fail", Run: "exit 1", ContinueOnError: true},
			{ID: "after", Run: "true"},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if res.Failed {
		t.Fatalf("step continue-on-error should not fail the job: %+v", res)
	}
	if res.Ran != 2 {
		t.Fatalf("both steps should run, ran=%d", res.Ran)
	}
}

func TestJobContinueOnError(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "opt", ContinueOnError: true, Steps: []compiler.StepPlan{{ID: "s", Run: "exit 1"}}},
		{ID: "next", Needs: []string{"opt"}, Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if res.Failed {
		t.Fatalf("job continue-on-error should not fail the run: %+v", res)
	}
	// "next" depends on the failed-but-tolerated job and must still run.
	var ranNext bool
	// re-run with an observer to confirm next executed
	res2 := Run(context.Background(), p, Options{
		Workdir: t.TempDir(), NoCache: true,
		Observer: func(e Event) {
			if e.Kind == JobFinished && e.Job == "next" && e.OK {
				ranNext = true
			}
		},
	})
	if res2.Failed || !ranNext {
		t.Fatalf("dependent of continue-on-error job should run; ranNext=%v res=%+v", ranNext, res2)
	}
}

func TestStepTimeout(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{
			{ID: "slow", Run: "sleep 5", TimeoutSec: 1},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !res.Failed {
		t.Fatal("step exceeding its timeout should fail")
	}
	if res.Duration.Seconds() > 3 {
		t.Fatalf("timeout should have canceled quickly, took %s", res.Duration)
	}
}

func TestJobTimeout(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", TimeoutSec: 1, Steps: []compiler.StepPlan{
			{ID: "slow", Run: "sleep 5"},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !res.Failed {
		t.Fatal("job exceeding its timeout should fail")
	}
}

func TestStepRetriesEventuallySucceeds(t *testing.T) {
	// Use a marker file to fail on the first attempt, succeed on the second.
	work := t.TempDir()
	script := `if [ -f attempted ]; then exit 0; else touch attempted; exit 1; fi`
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{
			{ID: "flaky", Run: script, Retries: 2},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("step should succeed on retry: %+v", res)
	}
	if res.Ran != 2 {
		t.Fatalf("expected 2 attempts, got %d", res.Ran)
	}
}

func TestStepRetriesExhausted(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{
			{ID: "always", Run: "exit 1", Retries: 2},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !res.Failed {
		t.Fatal("exhausted retries should fail")
	}
	if res.Ran != 3 {
		t.Fatalf("expected 3 attempts (1+2 retries), got %d", res.Ran)
	}
}

func TestStepRetryWithBackoff(t *testing.T) {
	work := t.TempDir()
	// fail once, succeed on retry; backoff exercises the sleep path.
	script := `if [ -f done ]; then exit 0; else touch done; exit 1; fi`
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{
			{ID: "flaky", Run: script, Retries: 1, RetryBackoffSec: 1},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("should succeed after backoff retry: %+v", res)
	}
	if res.Duration.Seconds() < 1 {
		t.Fatalf("backoff should have delayed ~1s, took %s", res.Duration)
	}
}

func TestRetryStopsOnCancel(t *testing.T) {
	// A failing step with retries; the parent context is canceled, so retries
	// must stop rather than loop.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "exit 1", Retries: 5, RetryBackoffSec: 5}}},
	}}
	res := Run(ctx, p, Options{Workdir: t.TempDir(), NoCache: true})
	if res.Duration.Seconds() > 3 {
		t.Fatalf("canceled retries should not wait for backoff, took %s", res.Duration)
	}
	_ = res
}
