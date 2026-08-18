package flow

import (
	"sort"

	"github.com/chris/shiphappens/internal/compiler"
)

// ToPlan lowers the author DSL into the compiler's raw plan (pre-validation).
// Matrix jobs are expanded here into one JobPlan per combination.
func (w *Workflow) ToPlan() *compiler.RunPlan {
	p := &compiler.RunPlan{Name: w.Name}
	if len(w.vars) > 0 {
		p.Vars = map[string]string{}
		for k, v := range w.vars {
			p.Vars[k] = v
		}
	}
	for _, ph := range w.preheat {
		p.Preheat = append(p.Preheat, compiler.PreheatSpec{
			Image:  ph.Image,
			Warm:   ph.Warm,
			Mounts: append([]string(nil), ph.Mounts...),
		})
	}

	// First pass: compute each job's expanded ids (matrix fan-out), so a
	// dependent on a matrix job can depend on all of its expansions.
	expandedIDs := map[string][]string{}
	for _, j := range w.jobs {
		if len(j.matrix) == 0 {
			expandedIDs[j.id] = []string{j.id}
			continue
		}
		for _, combo := range cartesian(j.matrix) {
			expandedIDs[j.id] = append(expandedIDs[j.id], j.id+"/"+combo.suffix)
		}
	}

	// resolveNeeds maps declared needs to their expanded ids.
	resolveNeeds := func(needs []string) []string {
		var out []string
		for _, n := range needs {
			if ids, ok := expandedIDs[n]; ok {
				out = append(out, ids...)
			} else {
				out = append(out, n) // unknown; validator will flag it
			}
		}
		return out
	}

	for _, j := range w.jobs {
		if len(j.matrix) == 0 {
			p.Jobs = append(p.Jobs, lowerJob(j, j.id, nil, resolveNeeds(j.needs)))
			continue
		}
		for _, combo := range cartesian(j.matrix) {
			p.Jobs = append(p.Jobs, lowerJob(j, j.id+"/"+combo.suffix, combo.env, resolveNeeds(j.needs)))
		}
	}
	return p
}

// lowerJob builds a JobPlan for a job with a resolved id, extra env (matrix
// values), and resolved needs.
func lowerJob(j *Job, id string, extraEnv map[string]string, needs []string) compiler.JobPlan {
	env := map[string]string{}
	for k, v := range j.env {
		env[k] = v
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	if len(env) == 0 {
		env = nil
	}

	jp := compiler.JobPlan{
		ID:              id,
		RunsOn:          j.runsOn,
		Image:           j.image,
		Needs:           needs,
		Env:             env,
		CleanAfter:      append([]string(nil), j.cleanAfter...),
		Network:         j.network,
		Outputs:         append([]string(nil), j.outputs...),
		Overlay:         j.overlay,
		TimeoutSec:      j.timeoutSec,
		ContinueOnError: j.continueOnError,
	}
	for _, s := range j.secrets {
		jp.Secrets = append(jp.Secrets, compiler.SecretRef{Name: s.name, FromEnv: s.fromEnv})
	}
	for _, s := range j.steps {
		sp := compiler.StepPlan{
			ID:              s.name,
			Run:             s.run,
			Env:             s.env,
			WorkingDir:      s.workingDir,
			Shell:           s.shell,
			TimeoutSec:      s.timeoutSec,
			Retries:         s.retries,
			RetryBackoffSec: s.retryBackoffSec,
			ContinueOnError: s.continueOnError,
		}
		if s.cache != nil {
			sp.Cache = &compiler.CacheSpec{
				Inputs:  append([]string(nil), s.cache.inputs...),
				Outputs: append([]string(nil), s.cache.outputs...),
			}
		}
		jp.Steps = append(jp.Steps, sp)
	}
	return jp
}

type matrixCombo struct {
	suffix string            // e.g. "linux-1.22"
	env    map[string]string // e.g. {"OS":"linux","GO":"1.22"}
}

// cartesian returns the cartesian product of the matrix dimensions as combos,
// in a deterministic order (dimension keys sorted).
func cartesian(dims map[string][]string) []matrixCombo {
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	combos := []matrixCombo{{env: map[string]string{}}}
	for _, k := range keys {
		var next []matrixCombo
		for _, c := range combos {
			for _, v := range dims[k] {
				env := map[string]string{}
				for ek, ev := range c.env {
					env[ek] = ev
				}
				env[upper(k)] = v
				suffix := v
				if c.suffix != "" {
					suffix = c.suffix + "-" + v
				}
				next = append(next, matrixCombo{suffix: suffix, env: env})
			}
		}
		combos = next
	}
	return combos
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

// Lines returns a map of jobID -> source location for diagnostics. Matrix jobs
// share their template job's location under each expanded id.
func (w *Workflow) Lines() map[string]string {
	m := map[string]string{}
	for _, j := range w.jobs {
		if len(j.matrix) == 0 {
			m[j.id] = j.line
			continue
		}
		for _, combo := range cartesian(j.matrix) {
			m[j.id+"/"+combo.suffix] = j.line
		}
	}
	return m
}
