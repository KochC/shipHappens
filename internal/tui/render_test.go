package tui

import (
	"bytes"
	"testing"
	"time"

	"github.com/KochC/shipHappens/internal/scheduler"
)

func TestStartStopRendersWithColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	buf := &bytes.Buffer{}
	prev := SetOutput(buf)
	defer SetOutput(prev)

	m := New("W", []string{"a", "b"})
	m.Start()
	// drive some events
	obs := m.Observer()
	obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "a"})
	obs(scheduler.Event{Kind: scheduler.StepStarted, Job: "a", Step: "s"})
	obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "a", OK: true, Duration: time.Second})
	obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "b", OK: false})
	time.Sleep(20 * time.Millisecond)
	m.Stop()

	s := buf.String()
	if len(s) == 0 {
		t.Fatal("expected rendered output")
	}
	// color path should include escape codes (cursor hide/show)
	if !bytes.Contains(buf.Bytes(), []byte(hideCur)) {
		t.Error("Start should hide cursor in color mode")
	}
	if !bytes.Contains(buf.Bytes(), []byte(showCur)) {
		t.Error("Stop should restore cursor in color mode")
	}
}

func TestStartNoColorSkipsCursor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	buf := &bytes.Buffer{}
	prev := SetOutput(buf)
	defer SetOutput(prev)
	m := New("W", []string{"a"})
	m.Start()
	m.Stop()
	if bytes.Contains(buf.Bytes(), []byte(hideCur)) {
		t.Error("no-color mode should not emit cursor-hide")
	}
}

func TestColorHelperBothPaths(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := New("W", nil)
	if got := m.c(green, "x"); got == "x" {
		t.Error("color mode should wrap")
	}
	m.noColor = true
	if got := m.c(green, "x"); got != "x" {
		t.Error("no-color mode should be plain")
	}
}

func TestStartTickerRepaints(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	buf := &bytes.Buffer{}
	prev := SetOutput(buf)
	defer SetOutput(prev)
	m := New("W", []string{"a"})
	m.Observer()(scheduler.Event{Kind: scheduler.JobStarted, Job: "a"})
	m.Start()
	// wait past one 500ms tick so the ticker goroutine repaints, then stop.
	time.Sleep(650 * time.Millisecond)
	m.Stop()
	if buf.Len() == 0 {
		t.Fatal("ticker should have repainted")
	}
}

func TestJobOrderFromIDs(t *testing.T) {
	in := []string{"a", "b"}
	out := JobOrderFromIDs(in)
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("JobOrderFromIDs wrong: %v", out)
	}
	// must be a copy
	out[0] = "z"
	if in[0] != "a" {
		t.Fatal("JobOrderFromIDs should copy, not alias")
	}
}

func TestRenderAllStatusVariants(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	buf := &bytes.Buffer{}
	prev := SetOutput(buf)
	defer SetOutput(prev)

	m := New("W", []string{"run", "ok", "fail", "skip", "pend"})
	obs := m.Observer()
	obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "run"})
	obs(scheduler.Event{Kind: scheduler.StepStarted, Job: "run", Step: "compiling"})
	obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "ok"})
	obs(scheduler.Event{Kind: scheduler.StepFinished, Job: "ok", OK: true, Cached: true})
	obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "ok", OK: true, Duration: time.Millisecond})
	obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "fail", OK: false})
	obs(scheduler.Event{Kind: scheduler.JobSkipped, Job: "skip"})
	// "pend" stays pending
	m.render()
	s := buf.String()
	for _, want := range []string{"run", "ok", "fail", "skip", "pend", "compiling", "done", "failed", "pending"} {
		if !bytes.Contains([]byte(s), []byte(want)) {
			t.Errorf("render missing %q", want)
		}
	}
}
