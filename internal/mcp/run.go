// Package mcp implements a Model Context Protocol server for Ship Happens over
// stdio, so agents and IDEs can validate, inspect, and run pipelines and poll
// their status without blocking.
package mcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/chris/shiphappens/internal/planfile"
	"github.com/chris/shiphappens/internal/scheduler"
)

// runState is the observable state of a background pipeline run.
type runState int

const (
	runRunning runState = iota
	runPassed
	runFailed
	runCanceled
)

func (s runState) String() string {
	switch s {
	case runPassed:
		return "passed"
	case runFailed:
		return "failed"
	case runCanceled:
		return "canceled"
	default:
		return "running"
	}
}

// jobState mirrors a job's lifecycle for status reporting.
type jobState struct {
	Status string `json:"status"` // pending|running|done|failed|skipped
	Step   string `json:"step,omitempty"`
}

// Run holds one background run's state. Reads are guarded by mu.
type Run struct {
	ID      string
	File    string
	mu      sync.Mutex
	state   runState
	jobs    map[string]*jobState
	order   []string
	started time.Time
	ended   time.Time
	result  scheduler.Result
	cancel  context.CancelFunc
}

// snapshot returns a consistent, serializable view of the run.
func (r *Run) snapshot() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]map[string]any, 0, len(r.order))
	var done, running, failed, pending int
	for _, id := range r.order {
		js := r.jobs[id]
		jobs = append(jobs, map[string]any{"id": id, "status": js.Status, "step": js.Step})
		switch js.Status {
		case "done":
			done++
		case "running":
			running++
		case "failed":
			failed++
		default:
			pending++
		}
	}
	out := map[string]any{
		"runId":   r.ID,
		"file":    r.File,
		"state":   r.state.String(),
		"jobs":    jobs,
		"summary": map[string]int{"done": done, "running": running, "failed": failed, "pending": pending},
		"elapsedSeconds": int(r.elapsed().Seconds()),
	}
	if r.state != runRunning {
		out["ran"] = r.result.Ran
		out["cached"] = r.result.Cached
		out["resumed"] = r.result.Resumed
	}
	return out
}

func (r *Run) elapsed() time.Duration {
	if r.ended.IsZero() {
		return time.Since(r.started)
	}
	return r.ended.Sub(r.started)
}

// Manager owns background runs.
type Manager struct {
	mu   sync.Mutex
	runs map[string]*Run
	seq  int
	// runFn is the scheduler entrypoint (overridable in tests).
	runFn func(ctx context.Context, file string, obs func(scheduler.Event)) (scheduler.Result, error)
}

// NewManager returns a Manager backed by the real scheduler.
func NewManager() *Manager {
	return &Manager{
		runs:  map[string]*Run{},
		runFn: defaultRunFn,
	}
}

// defaultRunFn loads a plan file and runs it, forwarding events to obs.
func defaultRunFn(ctx context.Context, file string, obs func(scheduler.Event)) (scheduler.Result, error) {
	plan, err := planfile.Load(file)
	if err != nil {
		return scheduler.Result{}, err
	}
	res := scheduler.Run(ctx, plan, scheduler.Options{Observer: obs})
	return res, nil
}

// Start launches a background run for the given pipeline file and returns its id.
func (m *Manager) Start(file string, jobIDs []string) *Run {
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("run-%d", m.seq)
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	r := &Run{
		ID: id, File: file, jobs: map[string]*jobState{}, order: append([]string(nil), jobIDs...),
		started: time.Now(), cancel: cancel,
	}
	for _, jid := range jobIDs {
		r.jobs[jid] = &jobState{Status: "pending"}
	}

	m.mu.Lock()
	m.runs[id] = r
	m.mu.Unlock()

	go func() {
		res, err := m.runFn(ctx, file, r.observe)
		r.mu.Lock()
		r.result = res
		r.ended = time.Now()
		switch {
		case ctx.Err() != nil:
			r.state = runCanceled
		case err != nil || res.Failed:
			r.state = runFailed
		default:
			r.state = runPassed
		}
		r.mu.Unlock()
	}()
	return r
}

// observe updates run state from scheduler events.
func (r *Run) observe(e scheduler.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	js := r.jobs[e.Job]
	if js == nil {
		js = &jobState{Status: "pending"}
		r.jobs[e.Job] = js
		r.order = append(r.order, e.Job)
	}
	switch e.Kind {
	case scheduler.JobStarted:
		js.Status = "running"
	case scheduler.JobFinished:
		if e.OK {
			js.Status = "done"
		} else {
			js.Status = "failed"
		}
		js.Step = ""
	case scheduler.JobSkipped:
		js.Status = "skipped"
	case scheduler.StepStarted:
		js.Step = e.Step
	}
}

// Get returns a run by id.
func (m *Manager) Get(id string) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	return r, ok
}

// List returns all run ids, sorted.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.runs))
	for id := range m.runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Cancel stops a run by id.
func (m *Manager) Cancel(id string) bool {
	r, ok := m.Get(id)
	if !ok {
		return false
	}
	r.cancel()
	return true
}
