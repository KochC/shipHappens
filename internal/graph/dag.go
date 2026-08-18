// Package graph provides DAG utilities over a compiled RunPlan.
package graph

import "github.com/chris/shiphappens/internal/compiler"

// DAG is an execution graph derived from a RunPlan.
type DAG struct {
	Nodes      []string            // job ids in definition order
	Needs      map[string][]string // job -> deps
	Dependents map[string][]string // job -> jobs that depend on it
}

// Build constructs a DAG from a plan.
func Build(p *compiler.RunPlan) *DAG {
	d := &DAG{Needs: map[string][]string{}, Dependents: map[string][]string{}}
	for _, j := range p.Jobs {
		d.Nodes = append(d.Nodes, j.ID)
		d.Needs[j.ID] = append([]string(nil), j.Needs...)
	}
	for _, j := range p.Jobs {
		for _, n := range j.Needs {
			d.Dependents[n] = append(d.Dependents[n], j.ID)
		}
	}
	return d
}

// Roots returns nodes with no dependencies.
func (d *DAG) Roots() []string {
	var r []string
	for _, n := range d.Nodes {
		if len(d.Needs[n]) == 0 {
			r = append(r, n)
		}
	}
	return r
}

// Subgraph returns the set of jobs required to run the target jobs (targets +
// all transitive dependencies).
func (d *DAG) Subgraph(targets ...string) map[string]bool {
	keep := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if keep[n] {
			return
		}
		keep[n] = true
		for _, dep := range d.Needs[n] {
			visit(dep)
		}
	}
	for _, t := range targets {
		visit(t)
	}
	return keep
}

// Affected returns the given jobs plus all their transitive dependents.
func (d *DAG) Affected(changed map[string]bool) map[string]bool {
	keep := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if keep[n] {
			return
		}
		keep[n] = true
		for _, dep := range d.Dependents[n] {
			visit(dep)
		}
	}
	for n := range changed {
		visit(n)
	}
	return keep
}

// TopoOrder returns a topological ordering of node ids (deps before dependents).
func (d *DAG) TopoOrder() []string {
	indeg := map[string]int{}
	for _, n := range d.Nodes {
		indeg[n] = len(d.Needs[n])
	}
	var queue, order []string
	for _, n := range d.Nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		for _, dep := range d.Dependents[n] {
			indeg[dep]--
			if indeg[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}
	return order
}
