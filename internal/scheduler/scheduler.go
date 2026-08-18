// Package scheduler executes a compiled RunPlan as a DAG with bounded
// parallelism, fail-fast cancellation, and step-result caching.
package scheduler

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/chris/shiphappens/internal/cache"
	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/graph"
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/runner"
	"github.com/chris/shiphappens/internal/secrets"
)

// Options configure a run.
type Options struct {
	Workdir string
	NoCache bool
	Only    map[string]bool // if non-nil, only these jobs run
	MaxPar  int
	Engine  string   // container engine: "docker" (default) or "podman"
	Mounts  []string // extra container volume specs applied to all image jobs
	// Observer, if set, receives job/step lifecycle events for UIs (e.g. a TUI).
	// It is called from multiple goroutines; implementations must be safe.
	Observer func(Event)
	// Resume skips jobs whose fingerprint matches a prior successful run,
	// restoring their declared Outputs instead of re-executing (incremental).
	Resume bool
	// Resolver resolves/masks secrets; defaults to the host environment.
	Resolver *secrets.Resolver
}

// EventKind enumerates lifecycle events.
type EventKind int

const (
	JobStarted EventKind = iota
	JobFinished
	JobSkipped
	StepStarted
	StepFinished
)

// Event is a scheduler progress notification.
type Event struct {
	Kind     EventKind
	Job      string
	Step     string
	OK       bool
	Cached   bool
	Duration time.Duration
}

// Result summarizes a run.
type Result struct {
	Failed   bool
	Ran      int
	Cached   int
	Resumed  int
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
	fps      map[string]string // jobID -> computed fingerprint
	skipped  map[string]bool   // jobID -> resumed (skipped) this run
	anyFail  bool
	ranCnt   int
	cacheCnt int
	resumeN  int

	resolver *secrets.Resolver

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
		fps:      map[string]string{},
		skipped:  map[string]bool{},
		sem:      make(chan struct{}, opts.MaxPar),
		resolver: secrets.New(),
	}
	if opts.Resolver != nil {
		s.resolver = opts.Resolver
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

	return Result{Failed: s.anyFail, Ran: s.ranCnt, Cached: s.cacheCnt, Resumed: s.resumeN, Duration: time.Since(start)}
}

func (s *scheduler) included(id string) bool {
	return s.opts.Only == nil || s.opts.Only[id]
}

// emit sends an event to the observer if one is configured.
func (s *scheduler) emit(e Event) {
	if s.opts.Observer != nil {
		s.opts.Observer(e)
	}
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
			s.emit(Event{Kind: JobSkipped, Job: id})
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
		if job.Overlay {
			upper := filepath.Join(s.opts.Workdir, ".ship-overlay", job.ID)
			return runner.OverlayRunner{
				Image: job.Image, Engine: s.opts.Engine, Mounts: s.opts.Mounts,
				Network: job.Network, UpperHost: upper,
			}
		}
		return runner.ContainerRunner{Image: job.Image, Engine: s.opts.Engine, Mounts: s.opts.Mounts, Network: job.Network}
	}
	return runner.NativeRunner{}
}

// fingerprint computes a job's resume fingerprint. Deps are done, so their
// fingerprints are available in s.fps.
func (s *scheduler) fingerprint(job *compiler.JobPlan) string {
	var cmds, inputs []string
	for _, st := range job.Steps {
		cmds = append(cmds, st.Run)
		if st.Cache != nil {
			inputs = append(inputs, st.Cache.Inputs...)
		}
	}
	s.mu.Lock()
	var ups []string
	for _, n := range job.Needs {
		if fp := s.fps[n]; fp != "" {
			ups = append(ups, fp)
		}
	}
	s.mu.Unlock()

	// Env fed to the fingerprint = workflow vars + job env. Secrets are added
	// only as non-reversible fingerprints so a changed secret invalidates the
	// cache without the value ever being hashed/stored in plaintext.
	fpEnv := map[string]string{}
	for k, v := range s.plan.Vars {
		fpEnv[k] = v
	}
	for k, v := range job.Env {
		fpEnv[k] = v
	}
	for _, sec := range job.Secrets {
		if v, ok := s.resolver.Lookup(sec); ok {
			fpEnv["__secret:"+sec.Name] = secrets.Fingerprint(v)
		} else {
			fpEnv["__secret:"+sec.Name] = "absent"
		}
	}

	fp, err := cache.JobFingerprint(cache.JobFingerprintInput{
		JobID:        job.ID,
		Image:        job.Image,
		StepCommands: cmds,
		Env:          fpEnv,
		Workdir:      s.opts.Workdir,
		InputGlobs:   inputs,
		UpstreamFPs:  ups,
	})
	if err != nil {
		return ""
	}
	return fp
}

func (s *scheduler) runJob(ctx context.Context, job *compiler.JobPlan) {
	defer s.wg.Done()
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	// Resume: if this job's fingerprint matches a prior success, skip it and
	// restore its outputs instead of re-executing. Only jobs that declare
	// Outputs participate in resume (others are cheap/side-effecting and not
	// worth fingerprinting a potentially large input tree for).
	fp := ""
	if s.opts.Resume && s.store != nil && len(job.Outputs) > 0 {
		fp = s.fingerprint(job)
		if fp != "" && s.store.JobDone(fp) {
			_ = s.store.RestoreJob(fp, s.opts.Workdir)
			s.mu.Lock()
			s.fps[job.ID] = fp
			s.done[job.ID] = true
			s.skipped[job.ID] = true
			s.resumeN++
			s.mu.Unlock()
			s.emit(Event{Kind: JobFinished, Job: job.ID, OK: true, Cached: true})
			s.launch(ctx)
			return
		}
	}

	run := s.runnerFor(job)

	// Resolve effective env (workflow vars + job env + secrets) and fail fast
	// if any required secret is missing from the host environment.
	if missing := s.resolver.Missing(job); len(missing) > 0 {
		logs.Failure("✗ [%s] missing required secret(s): %v", job.ID, missing)
		s.mu.Lock()
		s.done[job.ID] = true
		s.failed[job.ID] = true
		s.anyFail = true
		s.cancel()
		s.mu.Unlock()
		s.emit(Event{Kind: JobFinished, Job: job.ID, OK: false})
		s.launch(ctx)
		return
	}
	effEnv, secretVals := s.resolver.Effective(s.plan.Vars, job.Env, job)
	masker := secrets.NewMasker(secretVals)
	// cacheEnv is used for step-cache keys: identical to effEnv but with secret
	// values replaced by non-reversible fingerprints, so plaintext secrets never
	// enter a cache key.
	cacheEnv := map[string]string{}
	for k, v := range effEnv {
		cacheEnv[k] = v
	}
	for _, sec := range job.Secrets {
		if v, ok := effEnv[sec.Name]; ok {
			cacheEnv[sec.Name] = secrets.Fingerprint(v)
		}
	}

	out := logs.MaskedPrefixed(job.ID, masker.Mask)
	jobFailed := false
	jobStart := time.Now()
	s.emit(Event{Kind: JobStarted, Job: job.ID})

	// Apply a job-level timeout if set.
	jobCtx := ctx
	var cancelJob context.CancelFunc
	if job.TimeoutSec > 0 {
		jobCtx, cancelJob = context.WithTimeout(ctx, time.Duration(job.TimeoutSec)*time.Second)
		defer cancelJob()
	}

	for _, step := range job.Steps {
		cached, ok := s.execStep(jobCtx, run, job, step, effEnv, cacheEnv, out)
		if cached || ok {
			continue
		}
		// Step failed. continue-on-error at step level lets the job proceed.
		if step.ContinueOnError {
			logs.Step(job.ID, step.ID, "failed (continue-on-error)", false, false)
			continue
		}
		jobFailed = true
		break
	}

	// continue-on-error at job level: a failed job does not fail the run or
	// cancel siblings; dependents still run (treated as satisfied).
	jobFatal := jobFailed && !job.ContinueOnError

	s.mu.Lock()
	s.done[job.ID] = true
	if fp != "" {
		s.fps[job.ID] = fp
	}
	if jobFatal {
		s.failed[job.ID] = true
		s.anyFail = true
		s.cancel() // fail-fast
	}
	s.mu.Unlock()

	// Prune build intermediates to minimize disk usage (only on success, so a
	// failed build can still be inspected).
	if !jobFailed {
		// Record success for resume BEFORE pruning, so declared Outputs are
		// captured while they still exist on disk.
		if s.opts.Resume && s.store != nil && fp != "" && len(job.Outputs) > 0 {
			_ = s.store.MarkJobDone(fp, s.opts.Workdir, job.Outputs)
		}
		s.cleanAfter(job)
	}

	s.emit(Event{Kind: JobFinished, Job: job.ID, OK: !jobFailed, Duration: time.Since(jobStart)})
	s.launch(ctx)
}

// cleanAfter deletes the job's CleanAfter globs relative to the workdir.
func (s *scheduler) cleanAfter(job *compiler.JobPlan) {
	for _, g := range job.CleanAfter {
		matches, _ := filepath.Glob(filepath.Join(s.opts.Workdir, g))
		for _, m := range matches {
			_ = os.RemoveAll(m)
		}
	}
}

// execStep runs one step (consulting cache). Returns (cachedHit, ok). effEnv is
// passed to the runner; cacheEnv (secrets fingerprinted) is used for cache keys.
func (s *scheduler) execStep(ctx context.Context, run runner.Runner, job *compiler.JobPlan, step compiler.StepPlan, effEnv, cacheEnv map[string]string, out io.Writer) (cached, ok bool) {
	s.emit(Event{Kind: StepStarted, Job: job.ID, Step: step.ID})
	if s.store != nil && step.Cache != nil {
		key, err := cache.HashInputs(step.Run, s.opts.Workdir, cacheEnv, step.Cache.Inputs)
		if err == nil && s.store.Has(key) {
			_ = s.store.Restore(key, s.opts.Workdir)
			logs.Step(job.ID, step.ID, "cached 0.00s", true, true)
			s.mu.Lock()
			s.cacheCnt++
			s.mu.Unlock()
			s.emit(Event{Kind: StepFinished, Job: job.ID, Step: step.ID, OK: true, Cached: true})
			return true, true
		}
		res := s.runStep(ctx, run, step, effEnv, out)
		if res.Err != nil {
			logs.Step(job.ID, step.ID, fmt.Sprintf("failed %s", res.Duration.Round(time.Millisecond)), false, false)
			s.emit(Event{Kind: StepFinished, Job: job.ID, Step: step.ID, OK: false, Duration: res.Duration})
			return false, false
		}
		if err == nil {
			_ = s.store.Save(key, s.opts.Workdir, step.Cache.Outputs)
		}
		logs.Step(job.ID, step.ID, res.Duration.Round(time.Millisecond).String(), true, false)
		s.emit(Event{Kind: StepFinished, Job: job.ID, Step: step.ID, OK: true, Duration: res.Duration})
		return false, true
	}

	res := s.runStep(ctx, run, step, effEnv, out)
	if res.Err != nil {
		logs.Step(job.ID, step.ID, fmt.Sprintf("failed %s", res.Duration.Round(time.Millisecond)), false, false)
		s.emit(Event{Kind: StepFinished, Job: job.ID, Step: step.ID, OK: false, Duration: res.Duration})
		return false, false
	}
	logs.Step(job.ID, step.ID, res.Duration.Round(time.Millisecond).String(), true, false)
	s.emit(Event{Kind: StepFinished, Job: job.ID, Step: step.ID, OK: true, Duration: res.Duration})
	return false, true
}

// runStep executes a step once (or with retries), applying a per-step timeout,
// and counts each attempt as a run.
func (s *scheduler) runStep(ctx context.Context, run runner.Runner, step compiler.StepPlan, effEnv map[string]string, out io.Writer) runner.StepResult {
	attempts := step.Retries + 1
	var res runner.StepResult
	for attempt := 1; attempt <= attempts; attempt++ {
		stepCtx := ctx
		var cancel context.CancelFunc
		if step.TimeoutSec > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutSec)*time.Second)
		}
		res = run.Run(stepCtx, step, s.opts.Workdir, effEnv, out)
		if cancel != nil {
			cancel()
		}
		s.mu.Lock()
		s.ranCnt++
		s.mu.Unlock()

		if res.Err == nil {
			return res
		}
		// Don't retry if the parent context was canceled (fail-fast/shutdown).
		if ctx.Err() != nil {
			return res
		}
		if attempt < attempts {
			logs.Info("  retrying %s (attempt %d/%d)", step.ID, attempt+1, attempts)
			if step.RetryBackoffSec > 0 {
				select {
				case <-time.After(time.Duration(step.RetryBackoffSec) * time.Second):
				case <-ctx.Done():
					return res
				}
			}
		}
	}
	return res
}
