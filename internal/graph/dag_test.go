package graph

import (
	"reflect"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func mkPlan() *compiler.RunPlan {
	return &compiler.RunPlan{Jobs: []compiler.JobPlan{
		{ID: "checkout"},
		{ID: "lint", Needs: []string{"checkout"}},
		{ID: "test", Needs: []string{"checkout"}},
		{ID: "build", Needs: []string{"lint", "test"}},
	}}
}

func TestRoots(t *testing.T) {
	d := Build(mkPlan())
	if r := d.Roots(); !reflect.DeepEqual(r, []string{"checkout"}) {
		t.Fatalf("roots = %v", r)
	}
}

func TestSubgraph(t *testing.T) {
	d := Build(mkPlan())
	got := d.Subgraph("lint")
	want := map[string]bool{"lint": true, "checkout": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subgraph = %v", got)
	}
}

func TestAffected(t *testing.T) {
	d := Build(mkPlan())
	got := d.Affected(map[string]bool{"checkout": true})
	for _, id := range []string{"checkout", "lint", "test", "build"} {
		if !got[id] {
			t.Fatalf("expected %s affected, got %v", id, got)
		}
	}
}

func TestTopoOrder(t *testing.T) {
	d := Build(mkPlan())
	order := d.TopoOrder()
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if pos["checkout"] > pos["lint"] || pos["lint"] > pos["build"] || pos["test"] > pos["build"] {
		t.Fatalf("bad topo order: %v", order)
	}
}
