// Package flow is the author-facing DSL for defining Ship Happens pipelines.
// Pipelines are Go programs: build a *Workflow, then hand it to Main.
package flow

import (
	"runtime"
)

// Workflow is a pipeline definition being built by the DSL.
type Workflow struct {
	Name    string
	vars    map[string]string
	jobs    []*Job
	preheat []Preheat
}

// Preheat is warm-up work performed before the DAG runs: pulling a container
// image and/or running a one-off warm command inside it (e.g. priming a shared
// toolchain cache volume). Preheats run concurrently and their results are not
// part of the graph — a failed preheat is a warning, not a build failure.
type Preheat struct {
	Image  string   // image to pull (required)
	Warm   string   // optional shell command to run in the image to prime caches
	Mounts []string // volume mounts for the warm command (e.g. shared cache vol)
}

// Job is a node in the pipeline being built.
type Job struct {
	id              string
	runsOn          string
	image           string
	needs           []string
	env             map[string]string
	secrets         []secretRef
	cleanAfter      []string
	network         *bool
	outputs         []string
	overlay         bool
	timeoutSec      int
	continueOnError bool
	matrix          map[string][]string
	steps           []*Step
	line            string // "file.go:NN" of the .Job(...) call site
}

type secretRef struct {
	name    string
	fromEnv string
}

// Step is one command within a job.
type Step struct {
	name            string
	run             string
	cache           *cacheSpec
	env             map[string]string
	workingDir      string
	shell           string
	timeoutSec      int
	retries         int
	retryBackoffSec int
	continueOnError bool
}

type cacheSpec struct {
	inputs  []string
	outputs []string
}

// New starts a new workflow with the given name.
func New(name string) *Workflow {
	return &Workflow{Name: name}
}

// Job adds a job with the given id and returns it for further chaining.
func (w *Workflow) Job(id string) *Job {
	j := &Job{id: id, runsOn: "native", env: map[string]string{}, line: callerLoc(2)}
	w.jobs = append(w.jobs, j)
	return j
}

// Jobs returns the defined jobs (read-only use by Main/graph).
func (w *Workflow) Jobs() []*Job { return w.jobs }

// Var sets a workflow-level variable, merged into every job's environment.
// Job-level Env overrides a workflow Var on key collision.
func (w *Workflow) Var(key, val string) *Workflow {
	if w.vars == nil {
		w.vars = map[string]string{}
	}
	w.vars[key] = val
	return w
}

// Vars sets multiple workflow-level variables at once.
func (w *Workflow) Vars(kv map[string]string) *Workflow {
	for k, v := range kv {
		w.Var(k, v)
	}
	return w
}

// Preheat registers warm-up work (image pull + optional cache-priming command)
// to run concurrently before the DAG executes, so jobs don't stall on cold
// image pulls or empty toolchain caches.
func (w *Workflow) Preheat(p Preheat) *Workflow {
	w.preheat = append(w.preheat, p)
	return w
}

// Preheats returns the registered preheat specs.
func (w *Workflow) Preheats() []Preheat { return w.preheat }

// RunsOn sets the execution backend label (default "native").
func (j *Job) RunsOn(label string) *Job { j.runsOn = label; return j }

// Image runs the job inside the given container image (Docker/Podman). Sets
// runs-on to "container". The working tree is bind-mounted so caching works.
func (j *Job) Image(ref string) *Job { j.image = ref; j.runsOn = "container"; return j }

// Needs declares dependencies on other jobs.
func (j *Job) Needs(deps ...*Job) *Job {
	for _, d := range deps {
		j.needs = append(j.needs, d.id)
	}
	return j
}

// NeedsID declares a dependency by raw job id. Useful for testing/validation
// (an unknown id produces a diagnostic).
func (j *Job) NeedsID(ids ...string) *Job {
	j.needs = append(j.needs, ids...)
	return j
}

// Env sets an environment variable for all steps in the job.
func (j *Job) Env(key, val string) *Job { j.env[key] = val; return j }

// Secret exposes a secret env var to the job's steps, resolved at run time from
// the host environment variable of the same name. Secret values are masked in
// all log output, excluded from the compiled plan JSON, and contribute to the
// job's cache/resume fingerprint only via a non-reversible hash.
func (j *Job) Secret(name string) *Job {
	j.secrets = append(j.secrets, secretRef{name: name, fromEnv: name})
	return j
}

// SecretFrom is like Secret but reads the value from a differently-named host
// environment variable (fromEnv) and exposes it to steps as name.
func (j *Job) SecretFrom(name, fromEnv string) *Job {
	j.secrets = append(j.secrets, secretRef{name: name, fromEnv: fromEnv})
	return j
}

// Network enables (true) or disables (false) container networking for this
// image job. When unset, the engine default (network on) applies.
func (j *Job) Network(enabled bool) *Job { j.network = &enabled; return j }

// Offline runs the job's container with no network access — the default-secure
// choice for steps that only compile local sources. Steps that must fetch
// (dependencies, toolchains, registry) opt in via Network(true).
func (j *Job) Offline() *Job { b := false; j.network = &b; return j }

// Outputs declares file globs that constitute this job's result. They are
// persisted for resume: when a rerun's fingerprint matches, the job is skipped
// and these outputs are restored instead of recomputed.
func (j *Job) Outputs(globs ...string) *Job {
	j.outputs = append(j.outputs, globs...)
	return j
}

// Overlay runs a container job with an overlayfs upperdir so its filesystem
// writes are captured as an isolated diff layer (Linux/container jobs only).
func (j *Job) Overlay() *Job { j.overlay = true; return j }

// CleanAfter deletes the given path globs (relative to the workdir) after the
// job finishes — used to prune large build intermediates (e.g. ".pio/build/**")
// once the small final artifacts have been collected, minimizing disk usage.
func (j *Job) CleanAfter(globs ...string) *Job {
	j.cleanAfter = append(j.cleanAfter, globs...)
	return j
}

// Timeout bounds the whole job's wall-clock time (seconds). On expiry the
// running step is canceled and the job fails (unless ContinueOnError).
func (j *Job) Timeout(seconds int) *Job { j.timeoutSec = seconds; return j }

// ContinueOnError marks the job non-fatal: if it fails, the run is not failed
// and siblings/dependents still run (the job is treated as satisfied).
func (j *Job) ContinueOnError() *Job { j.continueOnError = true; return j }

// Matrix expands this job over the cartesian product of the given dimensions:
// one job per combination, with the combination's values exposed as env vars
// (uppercased key) and appended to the job id. E.g.
//
//	Matrix(map[string][]string{"os": {"linux","mac"}, "go": {"1.21","1.22"}})
//
// produces 4 jobs with $OS and $GO set. Dependencies on a matrix job depend on
// all of its expansions.
func (j *Job) Matrix(dims map[string][]string) *Job { j.matrix = dims; return j }

// Run appends a shell-command step to the job.
func (j *Job) Run(name, command string) *Job {
	j.steps = append(j.steps, &Step{name: name, run: command})
	return j
}

// lastStep returns the most recently added step, or nil.
func (j *Job) lastStep() *Step {
	if len(j.steps) == 0 {
		return nil
	}
	return j.steps[len(j.steps)-1]
}

// StepEnv sets an env var on the most recently added step (overrides job env).
func (j *Job) StepEnv(key, val string) *Job {
	if s := j.lastStep(); s != nil {
		if s.env == nil {
			s.env = map[string]string{}
		}
		s.env[key] = val
	}
	return j
}

// WorkingDir sets the working directory (relative to the workdir) for the most
// recently added step.
func (j *Job) WorkingDir(dir string) *Job {
	if s := j.lastStep(); s != nil {
		s.workingDir = dir
	}
	return j
}

// Shell sets the shell/interpreter for the most recently added step
// (e.g. "bash", "python", "node"). Default is "sh".
func (j *Job) Shell(shell string) *Job {
	if s := j.lastStep(); s != nil {
		s.shell = shell
	}
	return j
}

// StepTimeout bounds the most recently added step's wall-clock time (seconds).
func (j *Job) StepTimeout(seconds int) *Job {
	if s := j.lastStep(); s != nil {
		s.timeoutSec = seconds
	}
	return j
}

// Retry configures retries for the most recently added step: n additional
// attempts on failure, with an optional backoff (seconds) between attempts.
func (j *Job) Retry(n int, backoffSec ...int) *Job {
	if s := j.lastStep(); s != nil {
		s.retries = n
		if len(backoffSec) > 0 {
			s.retryBackoffSec = backoffSec[0]
		}
	}
	return j
}

// StepContinueOnError marks the most recently added step non-fatal: if it
// fails, the job continues to the next step.
func (j *Job) StepContinueOnError() *Job {
	if s := j.lastStep(); s != nil {
		s.continueOnError = true
	}
	return j
}

// ID exposes the job id.
func (j *Job) ID() string { return j.id }

// Cache attaches cache inputs/outputs to the most recently added step.
func (j *Job) Cache(opts ...CacheOption) *Job {
	if len(j.steps) == 0 {
		return j
	}
	s := j.steps[len(j.steps)-1]
	if s.cache == nil {
		s.cache = &cacheSpec{}
	}
	for _, o := range opts {
		o(s.cache)
	}
	return j
}

// CacheOption configures a step's cache spec.
type CacheOption func(*cacheSpec)

// Inputs declares file globs whose content contributes to the cache key.
func Inputs(globs ...string) CacheOption {
	return func(c *cacheSpec) { c.inputs = append(c.inputs, globs...) }
}

// Outputs declares file globs to store/restore on cache hit.
func Outputs(globs ...string) CacheOption {
	return func(c *cacheSpec) { c.outputs = append(c.outputs, globs...) }
}

func callerLoc(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?"
	}
	// shorten to base name
	short := file
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' {
			short = file[i+1:]
			break
		}
	}
	return short + ":" + itoa(line)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
