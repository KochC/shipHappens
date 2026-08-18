package flow

import "github.com/chris/shiphappens/internal/compiler"

// ToPlan lowers the author DSL into the compiler's raw plan (pre-validation).
// The compiler package validates and returns a *compiler.RunPlan.
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
	for _, j := range w.jobs {
		jp := compiler.JobPlan{
			ID:         j.id,
			RunsOn:     j.runsOn,
			Image:      j.image,
			Needs:      append([]string(nil), j.needs...),
			Env:        j.env,
			CleanAfter: append([]string(nil), j.cleanAfter...),
			Network:    j.network,
			Outputs:    append([]string(nil), j.outputs...),
			Overlay:    j.overlay,
		}
		for _, s := range j.secrets {
			jp.Secrets = append(jp.Secrets, compiler.SecretRef{Name: s.name, FromEnv: s.fromEnv})
		}
		for _, s := range j.steps {
			sp := compiler.StepPlan{ID: s.name, Run: s.run}
			if s.cache != nil {
				sp.Cache = &compiler.CacheSpec{
					Inputs:  append([]string(nil), s.cache.inputs...),
					Outputs: append([]string(nil), s.cache.outputs...),
				}
			}
			jp.Steps = append(jp.Steps, sp)
		}
		p.Jobs = append(p.Jobs, jp)
	}
	return p
}

// Lines returns a map of jobID -> source location for diagnostics.
func (w *Workflow) Lines() map[string]string {
	m := map[string]string{}
	for _, j := range w.jobs {
		m[j.id] = j.line
	}
	return m
}
