package flow

import (
	"sort"

	"github.com/KochC/shipHappens/internal/compiler"
)

// ToPlan lowers the author DSL into the compiler's raw plan (pre-validation).
// Matrix jobs are expanded here into one JobPlan per combination.
func (w *Workflow) ToPlan() *compiler.RunPlan {
	p := &compiler.RunPlan{Name: w.Name}
	if w.offlineByDefault || len(w.defaultAllow) > 0 {
		p.Security = &compiler.SecurityPolicy{
			OfflineByDefault: w.offlineByDefault,
			DefaultAllow:     append([]string(nil), w.defaultAllow...),
		}
	}
	if len(w.vars) > 0 {
		p.Vars = map[string]string{}
		for k, v := range w.vars {
			p.Vars[k] = v
		}
	}
	if len(w.toolchain) > 0 {
		p.Toolchain = map[string]string{}
		for k, v := range w.toolchain {
			p.Toolchain[k] = v
		}
	}
	if w.notify != nil {
		p.Notify = &compiler.NotifySpec{
			Desktop: w.notify.Desktop,
			Webhook: w.notify.Webhook,
			Exec:    w.notify.Exec,
			OnStart: w.notify.OnStart,
			OnJob:   w.notify.OnJob,
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
		Toolchain:       cloneMap(j.toolchain),
		CleanAfter:      append([]string(nil), j.cleanAfter...),
		Network:         j.network,
		Outputs:         append([]string(nil), j.outputs...),
		Overlay:         j.overlay,
		Allow:           append([]string(nil), j.allow...),
		TimeoutSec:      j.timeoutSec,
		ContinueOnError: j.continueOnError,
		If:              j.ifExpr,
	}
	for _, s := range j.secrets {
		jp.Secrets = append(jp.Secrets, compiler.SecretRef{Name: s.name, FromEnv: s.fromEnv})
	}
	for _, sv := range j.services {
		jp.Services = append(jp.Services, compiler.ServiceSpec{
			Name:    sv.Name,
			Image:   sv.Image,
			Env:     sv.Env,
			Ports:   append([]string(nil), sv.Ports...),
			Health:  sv.Health,
			Timeout: sv.Timeout,
		})
	}
	for _, s := range j.steps {
		jp.Steps = append(jp.Steps, lowerStep(s))
	}
	return jp
}

// lowerStep lowers a DSL Step (recursively, for onFailure handlers).
func lowerStep(s *Step) compiler.StepPlan {
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
		If:              s.ifExpr,
		Needs:           append([]string(nil), s.needs...),
	}
	if s.cache != nil {
		sp.Cache = &compiler.CacheSpec{
			Inputs:  append([]string(nil), s.cache.inputs...),
			Outputs: append([]string(nil), s.cache.outputs...),
		}
	}
	for _, h := range s.onFailure {
		sp.OnFailure = append(sp.OnFailure, lowerStep(h))
	}
	return sp
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

// cloneMap returns a copy of m, or nil if empty.
func cloneMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
