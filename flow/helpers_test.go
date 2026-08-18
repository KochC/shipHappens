package flow

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/runner"
)

func stubPreheat(t *testing.T, ret error) func() {
	t.Helper()
	prev := preheatFn
	preheatFn = func(context.Context, runner.PreheatSpec, io.Writer) error { return ret }
	t.Cleanup(func() { preheatFn = prev })
	return func() { preheatFn = prev }
}

func TestJobsIDNeedsID(t *testing.T) {
	wf := New("T")
	a := wf.Job("a").Run("s", "x")
	if a.ID() != "a" {
		t.Fatalf("ID()=%q", a.ID())
	}
	a.NeedsID("ghost")
	p := wf.ToPlan()
	if p.Jobs[0].Needs[0] != "ghost" {
		t.Fatalf("NeedsID not applied: %v", p.Jobs[0].Needs)
	}
	if len(wf.Jobs()) != 1 {
		t.Fatalf("Jobs() len=%d", len(wf.Jobs()))
	}
}

func TestCacheNoStepsNoop(t *testing.T) {
	wf := New("T")
	// Cache before any Run should be a no-op (not panic).
	wf.Job("a").Cache(Inputs("x")).Run("s", "y")
	j := wf.ToPlan().Jobs[0]
	if j.Steps[0].Cache != nil {
		t.Fatal("Cache before Run should not attach to a later step")
	}
}

func TestCacheSecondOptionMergesSpec(t *testing.T) {
	wf := New("T")
	wf.Job("a").Run("s", "y").Cache(Inputs("i")).Cache(Outputs("o"))
	c := wf.ToPlan().Jobs[0].Steps[0].Cache
	if c == nil || len(c.Inputs) != 1 || len(c.Outputs) != 1 {
		t.Fatalf("cache merge wrong: %+v", c)
	}
}

func TestJoinComma(t *testing.T) {
	if joinComma(nil) != "" {
		t.Error("empty")
	}
	if joinComma([]string{"a"}) != "a" {
		t.Error("single")
	}
	if joinComma([]string{"a", "b", "c"}) != "a, b, c" {
		t.Error("multi")
	}
}

func TestItoaAndCallerLoc(t *testing.T) {
	if itoa(0) != "0" || itoa(1234) != "1234" {
		t.Fatalf("itoa wrong")
	}
	// callerLoc via a real Job definition (already exercised) — check format.
	wf := New("T")
	wf.Job("a").Run("s", "x")
	loc := wf.Lines()["a"]
	if !strings.Contains(loc, ".go:") {
		t.Fatalf("callerLoc format: %q", loc)
	}
}

func TestRunPreheatsStubbed(t *testing.T) {
	quietLogs(t)
	restore := stubPreheat(t, nil)
	defer restore()
	specs := []Preheat{{Image: "a"}, {Image: "b", Warm: "w", Mounts: []string{"m:/x"}}}
	runPreheats(context.Background(), specs, "docker", "/wd", []string{"g:/y"})
}

func TestRunPreheatsFailureAdvisory(t *testing.T) {
	quietLogs(t)
	restore := stubPreheat(t, context.DeadlineExceeded)
	defer restore()
	// failure is advisory: must not panic / must return
	runPreheats(context.Background(), []Preheat{{Image: "a"}}, "docker", "/wd", nil)
}
