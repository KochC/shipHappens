package compiler

import "sort"

// ExpandMatrix returns a copy of the plan with every matrix job fanned out into
// one job per dimension-combination. It is the single, frontend-agnostic
// expansion path: Pkl and JSON pipelines carry a `matrix` on a job and are
// expanded here at load time, so the validator, scheduler, and runners only
// ever see concrete jobs. (The Go DSL performs the equivalent expansion when it
// lowers to a RunPlan, and therefore produces no Matrix fields.)
//
// Semantics (identical to the Go DSL):
//   - Dimension keys are sorted for determinism.
//   - Each combination's id is "<jobID>/<v1>-<v2>-…" in sorted-key order.
//   - Each dimension value is injected as an env var with an UPPERCASED key
//     (e.g. os=linux → OS=linux), merged over the job's own env.
//   - A dependency on a matrix job is rewritten to depend on all its expansions.
func (p *RunPlan) ExpandMatrix() *RunPlan {
	if p == nil {
		return nil
	}
	// First pass: compute expanded ids for every job.
	expandedIDs := make(map[string][]string, len(p.Jobs))
	any := false
	for i := range p.Jobs {
		j := &p.Jobs[i]
		if len(j.Matrix) == 0 {
			expandedIDs[j.ID] = []string{j.ID}
			continue
		}
		any = true
		for _, c := range cartesian(j.Matrix) {
			expandedIDs[j.ID] = append(expandedIDs[j.ID], j.ID+"/"+c.suffix)
		}
	}
	if !any {
		return p // nothing to do; return as-is
	}

	resolveNeeds := func(needs []string) []string {
		if len(needs) == 0 {
			return nil
		}
		var out []string
		for _, n := range needs {
			if ids, ok := expandedIDs[n]; ok {
				out = append(out, ids...)
			} else {
				out = append(out, n) // unknown; the validator will flag it
			}
		}
		return out
	}

	out := *p // shallow copy; we rebuild Jobs
	out.Jobs = make([]JobPlan, 0, len(p.Jobs))
	for i := range p.Jobs {
		j := p.Jobs[i]
		if len(j.Matrix) == 0 {
			j.Needs = resolveNeeds(j.Needs)
			out.Jobs = append(out.Jobs, j)
			continue
		}
		for _, c := range cartesian(j.Matrix) {
			jc := j
			jc.ID = j.ID + "/" + c.suffix
			jc.Matrix = nil
			jc.Needs = resolveNeeds(j.Needs)
			// Merge matrix env over the job's env (matrix wins).
			env := make(map[string]string, len(j.Env)+len(c.env))
			for k, v := range j.Env {
				env[k] = v
			}
			for k, v := range c.env {
				env[k] = v
			}
			jc.Env = env
			out.Jobs = append(out.Jobs, jc)
		}
	}
	return &out
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
				env := make(map[string]string, len(c.env)+1)
				for ek, ev := range c.env {
					env[ek] = ev
				}
				env[upperKey(k)] = v
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

// upperKey uppercases an ASCII dimension key for use as an env var name.
func upperKey(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}
