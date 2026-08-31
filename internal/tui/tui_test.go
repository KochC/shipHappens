package tui

import (
	"testing"
	"time"

	"github.com/KochC/shipHappens/internal/scheduler"
)

func TestObserverTracksJobLifecycle(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New("W", []string{"a", "b"})
	obs := m.Observer()

	obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "a"})
	if m.jobs["a"].status != "running" {
		t.Fatalf("a should be running, got %q", m.jobs["a"].status)
	}

	obs(scheduler.Event{Kind: scheduler.StepStarted, Job: "a", Step: "compile"})
	if m.jobs["a"].step != "compile" {
		t.Fatalf("a step should be compile, got %q", m.jobs["a"].step)
	}

	obs(scheduler.Event{Kind: scheduler.StepFinished, Job: "a", Step: "compile", OK: true})
	if m.jobs["a"].ran != 1 {
		t.Fatalf("a ran count should be 1, got %d", m.jobs["a"].ran)
	}

	obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "a", OK: true, Duration: time.Second})
	if m.jobs["a"].status != "ok" {
		t.Fatalf("a should be ok, got %q", m.jobs["a"].status)
	}

	obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "b", OK: false})
	if m.jobs["b"].status != "failed" {
		t.Fatalf("b should be failed, got %q", m.jobs["b"].status)
	}
}

func TestObserverCachedCounts(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New("W", []string{"a"})
	obs := m.Observer()
	obs(scheduler.Event{Kind: scheduler.StepFinished, Job: "a", OK: true, Cached: true})
	if m.jobs["a"].cached != 1 {
		t.Fatalf("cached count should be 1, got %d", m.jobs["a"].cached)
	}
}

func TestObserverSkipped(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New("W", []string{"a"})
	m.Observer()(scheduler.Event{Kind: scheduler.JobSkipped, Job: "a"})
	if m.jobs["a"].status != "skipped" {
		t.Fatalf("a should be skipped, got %q", m.jobs["a"].status)
	}
}

func TestObserverUnknownJobAdded(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New("W", []string{"a"})
	m.Observer()(scheduler.Event{Kind: scheduler.JobStarted, Job: "dynamic"})
	if _, ok := m.jobs["dynamic"]; !ok {
		t.Fatal("observer should register a previously-unknown job")
	}
}
