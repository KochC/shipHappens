// Package scheduler executes a compiled RunPlan as a DAG with bounded
// parallelism, fail-fast cancellation, and step-result caching.
package scheduler

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/chris/shiphappens/internal/cache"
	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/graph"
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/runner"
)

// Options configure a run.
type Options struct {
	Workdir string
	NoCache bool
	Only    map[string]bool // if non-nil, only these jobs run
	MaxPar  int
	Engine  string // container engine: "docker" (default) or "podman"
}

// Result summarizes a run.
type Result struct {
	Failed   bool
	Ran      int
	Cached   int
	Duration time.Duration
}

type scheduler struct {
	plan  *compiler.RunPlan
	dag   *graph.DAG
	opts  Options
	store *cache.Store

	cancel context.CancelFunc

	mu       sync.Mutex
	done     map[string]bool
	failed   map[string]bool
	inFlight map[string]bool
	anyFail  bool
	ranCnt   int
	cacheCnt int

	sem chan struct{}
	wg  sync.WaitGroup
}

// Run executes the plan. Returns Result; Failed=true if any job failed.
func Run(ctx context.Context, plan *compiler.RunPlan, opts Options) Result {
	start := time.Now()
	if opts.MaxPar <= 0 {
		opts.MaxPar = runtime.NumCPU()
	}

	s := &scheduler{
		plan:     plan,
		dag:      graph.Build(plan),
		opts:     opts,
		done:     map[string]bool{},
		failed:   map[string]bool{},
		inFlight: map[string]bool{},
		sem:      make(chan struct{}, opts.MaxPar),
	}
	if !opts.NoCache {
		if st, err := cache.Open(); err == nil {
			s.store = st
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer cancel()

	s.launch(ctx)
	s.wg.Wait()

	return Result{Failed: s.anyFail, Ran: s.ranCnt, Cached: s.cacheCnt, Duration: time.Since(start)}
}

func (s *scheduler) included(id string) bool {
	return s.opts.Only == nil || s.opts.Only[id]
}

// launch scans for jobs whose dependencies are satisfied and starts them.
func (s *scheduler) launch(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range s.dag.Nodes {
		if s.done[id] || s.inFlight[id] || !s.included(id) {
			continue
		}
		ready, blocked := s.depsState(id)
		if blocked {
			s.done[id] = true
			s.failed[id] = true
			s.anyFail = true
			logs.Failure("✗ [%s] skipped (dependency failed)", id)
			continue
		}
		if !ready {
			continue
		}
		s.inFlight[id] = true
		s.wg.Add(1)
		go s.runJob(ctx, s.plan.Job(id))
	}
}

// depsState reports whether a job is ready (all deps done+ok) or blocked (a dep failed).
func (s *scheduler) depsState(id string) (ready, blocked bool) {
	for _, n := range s.dag.Needs[id] {
		if !s.included(n) {
			continue
		}
		if !s.done[n] {
			return false, false
		}
		if s.failed[n] {
			return false, true
		}
	}
	return true, false
}

// runnerFor returns the appropriate runner for a job: a ContainerRunner when
// the job declares an image, otherwise the NativeRunner.
func (s *scheduler) runnerFor(job *compiler.JobPlan) runner.Runner {
	if job.Image != "" {
		return runner.ContainerRunner{Image: job.Image, Engine: s.opts.Engine}
	}
	return runner.NativeRunner{}
}

func (s *scheduler) runJob(ctx context.Context, job *compiler.JobPlan) {
	defer s.wg.Done()
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	run := s.runnerFor(job)
	out := logs.Prefixed(job.ID)
	jobFailed := false

	for _, step := range job.Steps {
		cached, ok := s.execStep(ctx, run, job, step, out)
		if cached {
			continue
		}
		if !ok {
			jobFailed = true
			break
		}
	}

	s.mu.Lock()
	s.done[job.ID] = true
	if jobFailed {
		s.failed[job.ID] = true
		s.anyFail = true
		s.cancel() // fail-fast
	}
	s.mu.Unlock()

	s.launch(ctx)
}

// execStep runs one step (consulting cache). Returns (cachedHit, ok).
func (s *scheduler) execStep(ctx context.Context, run runner.Runner, job *compiler.JobPlan, step compiler.StepPlan, out io.Writer) (cached, ok bool) {
	if s.store != nil && step.Cache != nil {
		key, err := cache.HashInputs(step.Run, s.opts.Workdir, job.Env, step.Cache.Inputs)
		if err == nil && s.store.Has(key) {
			_ = s.store.Restore(key, s.opts.Workdir)
			logs.Step(job.ID, step.ID, "cached 0.00s", true, true)
			s.mu.Lock()
			s.cacheCnt++
			s.mu.Unlock()
			return true, true
		}
		res := run.Run(ctx, step, s.opts.Workdir, job.Env, out)
		s.mu.Lock()
		s.ranCnt++
		s.mu.Unlock()
		if res.Err != nil {
			logs.Step(job.ID, step.ID, fmt.Sprintf("failed %s", res.Duration.Round(time.Millisecond)), false, false)
			return false, false
		}
		if err == nil {
			_ = s.store.Save(key, s.opts.Workdir, step.Cache.Outputs)
		}
		logs.Step(job.ID, step.ID, res.Duration.Round(time.Millisecond).String(), true, false)
		return false, true
	}

	res := run.Run(ctx, step, s.opts.Workdir, job.Env, out)
	s.mu.Lock()
	s.ranCnt++
	s.mu.Unlock()
	if res.Err != nil {
		logs.Step(job.ID, step.ID, fmt.Sprintf("failed %s", res.Duration.Round(time.Millisecond)), false, false)
		return false, false
	}
	logs.Step(job.ID, step.ID, res.Duration.Round(time.Millisecond).String(), true, false)
	return false, true
}
