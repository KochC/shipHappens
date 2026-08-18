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
	"github.com/chris/shiphappens/internal/expr"
	"github.com/chris/shiphappens/internal/graph"
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/outputs"
	"github.com/chris/shiphappens/internal/runner"
	"github.com/chris/shiphappens/internal/secrets"
	"github.com/chris/shiphappens/internal/security"
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
	fps      map[string]string            // jobID -> computed fingerprint
	skipped  map[string]bool              // jobID -> resumed (skipped) this run
	jobOut   map[string]map[string]string // jobID -> captured outputs
	results  map[string]string            // jobID -> "success"|"failure"|"skipped"
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
		jobOut:   map[string]map[string]string{},
		results:  map[string]string{},
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
func (s *scheduler) runnerFor(job *compiler.JobPlan, netName string) runner.Runner {
	if job.Image != "" {
		// Resolve the effective network policy (offline-by-default + allow-list).
		var policy *compiler.SecurityPolicy
		if s.plan != nil {
			policy = s.plan.Security
		}
		dec := security.Resolve(policy, job)
		net := job.Network
		if netName == "" { // services network overrides policy (services need net)
			switch dec.Mode {
			case security.NetNone:
				no := false
				net = &no
			case security.NetDefault:
				yes := true
				net = &yes
			case security.NetAllow:
				yes := true
				net = &yes // allow-list opts into network (egress scoping is best-effort, see docs)
			}
		}
		if job.Overlay {
			upper := filepath.Join(s.opts.Workdir, ".ship-overlay", job.ID)
			return runner.OverlayRunner{
				Image: job.Image, Engine: s.opts.Engine, Mounts: s.opts.Mounts,
				Network: net, UpperHost: upper,
			}
		}
		return runner.ContainerRunner{
			Image: job.Image, Engine: s.opts.Engine, Mounts: s.opts.Mounts,
			Network: net, NetworkName: netName, Allow: dec.Allow,
		}
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

	// Start sidecar services (if any) on a dedicated network the job container
	// joins. Torn down when the job completes.
	var svcNet string
	if len(job.Services) > 0 {
		svcOut := logs.Prefixed(job.ID + ":services")
		svcs, net, err := runner.StartServices(ctx, s.opts.Engine, job.ID, job.Services, svcOut)
		if err != nil {
			logs.Failure("✗ [%s] services failed: %v", job.ID, err)
			svcs.Stop(context.Background())
			s.finishFailed(ctx, job)
			return
		}
		svcNet = net
		defer svcs.Stop(context.Background())
	}

	run := s.runnerFor(job, svcNet)

	// Conditional: skip the job when its `if` evaluates false. A skipped job is
	// treated as satisfied (not failed); dependents still run.
	if job.If != "" {
		ok, err := s.evalIf(job.If, job, nil)
		if err != nil {
			logs.Failure("✗ [%s] invalid if: %v", job.ID, err)
			s.finishFailed(ctx, job)
			return
		}
		if !ok {
			logs.Info("◌ [%s] skipped (if=false)", job.ID)
			s.mu.Lock()
			s.done[job.ID] = true
			s.results[job.ID] = "skipped"
			s.mu.Unlock()
			s.emit(Event{Kind: JobSkipped, Job: job.ID})
			s.launch(ctx)
			return
		}
	}

	// Resolve effective env (workflow vars + job env + secrets) and fail fast
	// if any required secret is missing from the host environment.
	if missing := s.resolver.Missing(job); len(missing) > 0 {
		logs.Failure("✗ [%s] missing required secret(s): %v", job.ID, missing)
		s.finishFailed(ctx, job)
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
	// Expose upstream job outputs to steps as env: OUTPUTS_<JOB>_<KEY>.
	s.injectUpstreamOutputs(job, effEnv)

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

	// Set up the per-job output file ($SHIP_OUTPUT). Steps append key=value
	// lines; the scheduler accumulates them into this job's outputs.
	outFile, outEnvVal := s.outputFile(job)
	if outFile != "" {
		defer os.Remove(outFile)
	}
	captured := map[string]string{}

	sc := &stepCtx{
		job: job, run: run, effEnv: effEnv, cacheEnv: cacheEnv,
		outFile: outFile, outEnvVal: outEnvVal, captured: captured, out: out,
	}
	jobFailed = s.runSteps(jobCtx, sc, job.Steps)

	// continue-on-error at job level: a failed job does not fail the run or
	// cancel siblings; dependents still run (treated as satisfied).
	jobFatal := jobFailed && !job.ContinueOnError

	s.mu.Lock()
	s.done[job.ID] = true
	if fp != "" {
		s.fps[job.ID] = fp
	}
	if len(captured) > 0 {
		s.jobOut[job.ID] = captured
	}
	if jobFailed {
		s.results[job.ID] = "failure"
	} else {
		s.results[job.ID] = "success"
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

// finishFailed marks a job failed (fail-fast), for the missing-secret and
// invalid-if paths.
func (s *scheduler) finishFailed(ctx context.Context, job *compiler.JobPlan) {
	s.mu.Lock()
	s.done[job.ID] = true
	s.failed[job.ID] = true
	s.results[job.ID] = "failure"
	s.anyFail = true
	s.cancel()
	s.mu.Unlock()
	s.emit(Event{Kind: JobFinished, Job: job.ID, OK: false})
	s.launch(ctx)
}

// evalIf evaluates an `if` expression for a job/step. stepOutputs, when non-nil,
// exposes the current job's own captured outputs as `outputs.self.<key>`.
func (s *scheduler) evalIf(src string, job *compiler.JobPlan, stepOutputs map[string]string) (bool, error) {
	s.mu.Lock()
	// snapshot for a consistent view
	results := map[string]string{}
	for k, v := range s.results {
		results[k] = v
	}
	jobOut := map[string]map[string]string{}
	for j, m := range s.jobOut {
		cp := map[string]string{}
		for k, v := range m {
			cp[k] = v
		}
		jobOut[j] = cp
	}
	anyFail := s.anyFail
	s.mu.Unlock()

	ctx := expr.Context{
		Success: !anyFail,
		Failure: anyFail,
		Lookup: func(path []string) (any, bool) {
			switch path[0] {
			case "env":
				if len(path) == 2 {
					if v, ok := job.Env[path[1]]; ok {
						return v, true
					}
					if v, ok := s.plan.Vars[path[1]]; ok {
						return v, true
					}
				}
			case "vars":
				if len(path) == 2 {
					if v, ok := s.plan.Vars[path[1]]; ok {
						return v, true
					}
				}
			case "needs":
				// needs.<job>.result
				if len(path) == 3 && path[2] == "result" {
					if r, ok := results[path[1]]; ok {
						return r, true
					}
				}
			case "outputs":
				// outputs.<job>.<key>  and  outputs.self.<key>
				if len(path) == 3 {
					if path[1] == "self" && stepOutputs != nil {
						if v, ok := stepOutputs[path[2]]; ok {
							return v, true
						}
					}
					if m, ok := jobOut[path[1]]; ok {
						if v, ok := m[path[2]]; ok {
							return v, true
						}
					}
				}
			}
			return nil, false
		},
	}
	return expr.Eval(src, ctx)
}

// injectUpstreamOutputs exposes dependency job outputs to steps as env vars
// named OUTPUTS_<JOB>_<KEY> (job/key uppercased, non-alnum → underscore).
func (s *scheduler) injectUpstreamOutputs(job *compiler.JobPlan, env map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, dep := range job.Needs {
		for k, v := range s.jobOut[dep] {
			env[envKey("OUTPUTS_"+dep+"_"+k)] = v
		}
	}
}

// outputFile returns (hostPath, envValue) for a job's $SHIP_OUTPUT file. The
// host path is under the workdir so container jobs can write to it via the bind
// mount; envValue is the path as the step sees it (container path for image jobs).
func (s *scheduler) outputFile(job *compiler.JobPlan) (hostPath, envVal string) {
	rel := ".ship-output-" + sanitize(job.ID)
	hostPath = filepath.Join(s.opts.Workdir, rel)
	_ = os.WriteFile(hostPath, nil, 0o644)
	if job.Image != "" {
		return hostPath, "/ship/work/" + rel
	}
	return hostPath, hostPath
}

// withOutput returns a copy of env with SHIP_OUTPUT set.
func withOutput(env map[string]string, path string) map[string]string {
	cp := make(map[string]string, len(env)+1)
	for k, v := range env {
		cp[k] = v
	}
	cp["SHIP_OUTPUT"] = path
	return cp
}

func envKey(s string) string {
	b := []byte(s)
	for i := range b {
		c := b[i]
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		} else if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			b[i] = '_'
		}
	}
	return string(b)
}

func sanitize(s string) string {
	b := []byte(s)
	for i := range b {
		c := b[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			b[i] = '_'
		}
	}
	return string(b)
}


func (s *scheduler) cleanAfter(job *compiler.JobPlan) {
	for _, g := range job.CleanAfter {
		matches, _ := filepath.Glob(filepath.Join(s.opts.Workdir, g))
		for _, m := range matches {
			_ = os.RemoveAll(m)
		}
	}
}

// stepCtx bundles the per-job state needed to run a step (or step sub-graph).
type stepCtx struct {
	job       *compiler.JobPlan
	run       runner.Runner
	effEnv    map[string]string
	cacheEnv  map[string]string
	outFile   string
	outEnvVal string
	captured  map[string]string
	capMu     sync.Mutex
	out       io.Writer
}

// runSteps executes a job's steps. If any step declares Needs, the steps run as
// a DAG (parallel where possible); otherwise they run sequentially in order
// (the default, preserving classic semantics). Returns whether the job failed.
func (s *scheduler) runSteps(ctx context.Context, sc *stepCtx, steps []compiler.StepPlan) bool {
	hasDeps := false
	for _, st := range steps {
		if len(st.Needs) > 0 {
			hasDeps = true
			break
		}
	}
	if !hasDeps {
		return s.runStepsSequential(ctx, sc, steps)
	}
	return s.runStepsDAG(ctx, sc, steps)
}

// runStepsSequential runs steps in order, stopping at the first fatal failure.
func (s *scheduler) runStepsSequential(ctx context.Context, sc *stepCtx, steps []compiler.StepPlan) bool {
	for _, step := range steps {
		st, fatal := s.runJobStep(ctx, sc, step)
		if st == stepSkipped || st == stepOK {
			continue
		}
		if fatal {
			return true
		}
	}
	return false
}

// runStepsDAG runs steps respecting step-level Needs, with parallelism.
func (s *scheduler) runStepsDAG(ctx context.Context, sc *stepCtx, steps []compiler.StepPlan) bool {
	var mu sync.Mutex
	claimed := map[string]bool{} // started or terminal
	finished := map[string]bool{}
	failed := map[string]bool{}
	jobFailed := false

	// depsOK: all deps finished successfully (ready), or a dep failed (blocked).
	depsOK := func(st compiler.StepPlan) (ready, blocked bool) {
		for _, n := range st.Needs {
			if failed[n] {
				return false, true
			}
			if !finished[n] {
				return false, false
			}
		}
		return true, false
	}

	var wg sync.WaitGroup
	var launch func()
	launch = func() {
		mu.Lock()
		defer mu.Unlock()
		for _, st := range steps {
			if claimed[st.ID] {
				continue
			}
			ready, blocked := depsOK(st)
			if blocked {
				claimed[st.ID] = true
				finished[st.ID] = true
				failed[st.ID] = true
				jobFailed = true
				logs.Step(sc.job.ID, st.ID, "skipped (dependency failed)", false, false)
				continue
			}
			if !ready {
				continue
			}
			claimed[st.ID] = true
			st := st
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, fatal := s.runJobStep(ctx, sc, st)
				mu.Lock()
				finished[st.ID] = true
				if status == stepFailed && fatal {
					failed[st.ID] = true
					jobFailed = true
				}
				mu.Unlock()
				launch()
			}()
		}
	}
	launch()
	wg.Wait()
	return jobFailed
}

type stepStatus int

const (
	stepOK stepStatus = iota
	stepSkipped
	stepFailed
)

// runJobStep runs a single step: evaluates its If, executes it (with retries/
// timeout via execStep), captures outputs, handles continue-on-error and the
// onFailure sub-graph. Returns the status and whether a failure is fatal to the
// job.
func (s *scheduler) runJobStep(ctx context.Context, sc *stepCtx, step compiler.StepPlan) (stepStatus, bool) {
	// Conditional.
	if step.If != "" {
		ok, err := s.evalIf(step.If, sc.job, sc.snapshotCaptured())
		if err != nil {
			logs.Step(sc.job.ID, step.ID, fmt.Sprintf("invalid if: %v", err), false, false)
			return stepFailed, true
		}
		if !ok {
			logs.Step(sc.job.ID, step.ID, "skipped (if=false)", true, false)
			return stepSkipped, false
		}
	}

	stepEnv := sc.effEnv
	if sc.outEnvVal != "" {
		stepEnv = withOutput(sc.effEnv, sc.outEnvVal)
	}

	cached, ok := s.execStep(ctx, sc.run, sc.job, step, stepEnv, sc.cacheEnv, sc.out)
	sc.captureOutputs()

	if cached || ok {
		return stepOK, false
	}

	// Failure: run the onFailure sub-graph (best-effort; its outcome doesn't
	// change the step's failure).
	if len(step.OnFailure) > 0 {
		logs.Step(sc.job.ID, step.ID, "running onFailure handlers", false, false)
		_ = s.runStepsSequential(ctx, sc, step.OnFailure)
	}

	if step.ContinueOnError {
		logs.Step(sc.job.ID, step.ID, "failed (continue-on-error)", false, false)
		return stepFailed, false
	}
	return stepFailed, true
}

// captureOutputs merges any $SHIP_OUTPUT the last step wrote into sc.captured.
func (sc *stepCtx) captureOutputs() {
	if sc.outFile == "" {
		return
	}
	if kv, _ := outputs.Parse(sc.outFile); len(kv) > 0 {
		sc.capMu.Lock()
		for k, v := range kv {
			sc.captured[k] = v
		}
		sc.capMu.Unlock()
	}
}

func (sc *stepCtx) snapshotCaptured() map[string]string {
	sc.capMu.Lock()
	defer sc.capMu.Unlock()
	cp := make(map[string]string, len(sc.captured))
	for k, v := range sc.captured {
		cp[k] = v
	}
	return cp
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
