package graph

import (
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func TestSubgraphMultipleTargetsAndSharedDeps(t *testing.T) {
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "root"},
		{ID: "a", Needs: []string{"root"}},
		{ID: "b", Needs: []string{"root"}},
		{ID: "c", Needs: []string{"a", "b"}},
	}}
	d := Build(p)
	// two targets sharing a dep exercises the keep[n] early-return branch.
	got := d.Subgraph("a", "b")
	for _, id := range []string{"root", "a", "b"} {
		if !got[id] {
			t.Errorf("expected %s in subgraph, got %v", id, got)
		}
	}
	if got["c"] {
		t.Errorf("c should not be included, got %v", got)
	}
}

func TestSubgraphRevisitIsIdempotent(t *testing.T) {
	p := &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "root"},
		{ID: "a", Needs: []string{"root"}},
	}}
	d := Build(p)
	// same target twice hits the already-visited branch
	got := d.Subgraph("a", "a")
	if len(got) != 2 {
		t.Fatalf("expected {root,a}, got %v", got)
	}
}
