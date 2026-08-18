package validator

import "github.com/chris/shiphappens/internal/compiler"

// findCycle returns a cycle path (job ids) if the needs-graph has one, else nil.
// Edges point from a job to each job it needs.
func findCycle(p *compiler.RunPlan) []string {
	adj := map[string][]string{}
	exists := map[string]bool{}
	for _, j := range p.Jobs {
		exists[j.ID] = true
	}
	for _, j := range p.Jobs {
		for _, n := range j.Needs {
			if exists[n] {
				adj[j.ID] = append(adj[j.ID], n)
			}
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string

	var dfs func(string) []string
	dfs = func(u string) []string {
		color[u] = gray
		stack = append(stack, u)
		for _, v := range adj[u] {
			switch color[v] {
			case white:
				if c := dfs(v); c != nil {
					return c
				}
			case gray:
				// found back-edge; extract cycle from stack.
				start := 0
				for i, s := range stack {
					if s == v {
						start = i
						break
					}
				}
				cyc := append([]string(nil), stack[start:]...)
				cyc = append(cyc, v)
				return cyc
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return nil
	}

	for _, j := range p.Jobs {
		if color[j.ID] == white {
			if c := dfs(j.ID); c != nil {
				return c
			}
		}
	}
	return nil
}

// validateStepGraph checks step-level Needs: unknown targets, self-reference,
// and cycles within a single job's step DAG.
func validateStepGraph(j compiler.JobPlan, loc string) []Diagnostic {
	var diags []Diagnostic
	ids := map[string]bool{}
	for _, s := range j.Steps {
		ids[s.ID] = true
	}
	adj := map[string][]string{}
	for _, s := range j.Steps {
		for _, n := range s.Needs {
			if n == s.ID {
				diags = append(diags, Diagnostic{Loc: loc, Msg: "job \"" + j.ID + "\" step \"" + s.ID + "\" needs itself"})
				continue
			}
			if !ids[n] {
				diags = append(diags, Diagnostic{Loc: loc, Msg: "job \"" + j.ID + "\" step \"" + s.ID + "\" needs unknown step \"" + n + "\""})
				continue
			}
			adj[s.ID] = append(adj[s.ID], n)
		}
	}
	if cyc := findGraphCycle(j.Steps, adj); cyc != nil {
		diags = append(diags, Diagnostic{Loc: loc, Msg: "job \"" + j.ID + "\" step cycle: " + joinArrow(cyc)})
	}
	return diags
}

// findGraphCycle does DFS cycle detection over a step adjacency map.
func findGraphCycle(steps []compiler.StepPlan, adj map[string][]string) []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var dfs func(string) []string
	dfs = func(u string) []string {
		color[u] = gray
		stack = append(stack, u)
		for _, v := range adj[u] {
			switch color[v] {
			case white:
				if c := dfs(v); c != nil {
					return c
				}
			case gray:
				start := 0
				for i, s := range stack {
					if s == v {
						start = i
						break
					}
				}
				return append(append([]string(nil), stack[start:]...), v)
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return nil
	}
	for _, s := range steps {
		if color[s.ID] == white {
			if c := dfs(s.ID); c != nil {
				return c
			}
		}
	}
	return nil
}
