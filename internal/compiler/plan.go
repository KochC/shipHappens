package compiler

// RunPlan is the immutable, validated intermediate representation produced by
// the compiler. The scheduler and runners consume this and never touch the
// author-facing DSL types.
type RunPlan struct {
	Name string
	// Vars are workflow-level variables merged into every job's environment
	// (job Env overrides on key collision). Serialized in the compiled plan.
	Vars map[string]string
	Jobs []JobPlan
}

// SecretRef names a secret a job requires. The value is resolved at run time
// from the host process environment (from FromEnv, defaulting to the secret
// Name) — it is never stored in the plan. Secrets are masked in all output.
type SecretRef struct {
	Name    string // env var name exposed to the job's steps
	FromEnv string // host env var to read from; defaults to Name if empty
}

// JobPlan is a single node in the execution DAG.
type JobPlan struct {
	ID         string
	RunsOn     string
	Image      string // container image; if set, job runs in a container (RunsOn="container")
	Needs      []string
	Env        map[string]string
	Secrets    []SecretRef // secret env vars, resolved from host env at run time
	Steps      []StepPlan
	CleanAfter []string // path globs deleted after the job completes (prune build intermediates)
	// Network controls container networking for image jobs. nil = engine
	// default (on). false = isolated (no network). true = network on.
	Network *bool
	// Outputs are file globs persisted for job-level resume (restored when a
	// job is skipped because its fingerprint matched a prior successful run).
	Outputs []string
	// Overlay, when true (container jobs only), runs the job with an overlayfs
	// upperdir so its writes are captured as an isolated diff layer.
	Overlay bool
}

// StepPlan is one executable unit within a job.
type StepPlan struct {
	ID    string
	Run   string
	Cache *CacheSpec
}

// CacheSpec describes how a step's result may be cached. Only steps with a
// CacheSpec are cached in M1 (explicit = safe).
type CacheSpec struct {
	Inputs  []string
	Outputs []string
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
