package compiler

// RunPlan is the immutable, validated intermediate representation produced by
// the compiler. The scheduler and runners consume this and never touch the
// author-facing DSL types. JSON tags define the stable on-disk plan format used
// by --compile and by external front-ends (e.g. Pkl) that evaluate to this shape.
type RunPlan struct {
	Name string `json:"name"`
	// Vars are workflow-level variables merged into every job's environment
	// (job Env overrides on key collision). Serialized in the compiled plan.
	Vars map[string]string `json:"vars,omitempty"`
	// Preheat is warm-up work (image pull + optional cache-priming command) run
	// concurrently before the DAG. Advisory — failures never fail the build.
	Preheat []PreheatSpec `json:"preheat,omitempty"`
	Jobs    []JobPlan     `json:"jobs"`
}

// PreheatSpec is one preheat entry in the plan.
type PreheatSpec struct {
	Image  string   `json:"image"`
	Warm   string   `json:"warm,omitempty"`
	Mounts []string `json:"mounts,omitempty"`
}

// SecretRef names a secret a job requires. The value is resolved at run time
// from the host process environment (from FromEnv, defaulting to the secret
// Name) — it is never stored in the plan. Secrets are masked in all output.
type SecretRef struct {
	Name    string `json:"name"`              // env var name exposed to the job's steps
	FromEnv string `json:"fromEnv,omitempty"` // host env var to read from; defaults to Name if empty
}

// JobPlan is a single node in the execution DAG.
type JobPlan struct {
	ID         string            `json:"id"`
	RunsOn     string            `json:"runsOn,omitempty"`
	Image      string            `json:"image,omitempty"`
	Needs      []string          `json:"needs,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Secrets    []SecretRef       `json:"secrets,omitempty"`
	Steps      []StepPlan        `json:"steps"`
	CleanAfter []string          `json:"cleanAfter,omitempty"`
	// Network controls container networking for image jobs. nil = engine
	// default (on). false = isolated (no network). true = network on.
	Network *bool `json:"network,omitempty"`
	// Outputs are file globs persisted for job-level resume (restored when a
	// job is skipped because its fingerprint matched a prior successful run).
	Outputs []string `json:"outputs,omitempty"`
	// Overlay, when true (container jobs only), runs the job with an overlayfs
	// upperdir so its writes are captured as an isolated diff layer.
	Overlay bool `json:"overlay,omitempty"`
	// TimeoutSec, when > 0, bounds the whole job's wall-clock time; on expiry the
	// running step is canceled and the job fails (unless ContinueOnError).
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// ContinueOnError: a failing job does not fail the run or cancel siblings;
	// dependents still run (the job is treated as satisfied).
	ContinueOnError bool `json:"continueOnError,omitempty"`
}

// StepPlan is one executable unit within a job.
type StepPlan struct {
	ID         string            `json:"id"`
	Run        string            `json:"run"`
	Cache      *CacheSpec        `json:"cache,omitempty"`
	Env        map[string]string `json:"env,omitempty"`        // step-level env (overrides job env)
	WorkingDir string            `json:"workingDir,omitempty"` // dir (relative to workdir) to run in
	Shell      string            `json:"shell,omitempty"`      // shell to use (default "sh"); e.g. "bash", "python"
	// TimeoutSec, when > 0, bounds this step's wall-clock time.
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// Retries is the number of additional attempts (total attempts = Retries+1)
	// if the step exits non-zero.
	Retries int `json:"retries,omitempty"`
	// RetryBackoffSec is the delay between retry attempts (default 0).
	RetryBackoffSec int `json:"retryBackoffSec,omitempty"`
	// ContinueOnError: a failing step does not fail the job; execution proceeds.
	ContinueOnError bool `json:"continueOnError,omitempty"`
}

// CacheSpec describes how a step's result may be cached. Only steps with a
// CacheSpec are cached (explicit = safe).
type CacheSpec struct {
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}

// Job returns the JobPlan with the given id, or nil.
func (p *RunPlan) Job(id string) *JobPlan {
	for i := range p.Jobs {
		if p.Jobs[i].ID == id {
			return &p.Jobs[i]
		}
	}
	return nil
}
