// Package tui renders a minimal, zero-dependency live dashboard for a Ship
// Happens run using ANSI escape codes. It consumes scheduler events and repaints
// a compact per-job status table in place.
package tui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/chris/shiphappens/internal/scheduler"
)

// out is the render sink (overridable in tests).
var out io.Writer = os.Stdout

// SetOutput redirects TUI output; returns the previous sink.
func SetOutput(w io.Writer) io.Writer { prev := out; out = w; return prev }

type jobState struct {
	status  string // pending, running, ok, failed, skipped
	step    string
	started time.Time
	dur     time.Duration
	cached  int
	ran     int
}

// Model holds live run state and repaints the dashboard.
type Model struct {
	mu      sync.Mutex
	name    string
	order   []string
	jobs    map[string]*jobState
	start   time.Time
	lines   int // lines drawn last frame (for cursor rewind)
	done    bool
	noColor bool
}

// New creates a TUI model for the given workflow name and job IDs (in order).
func New(name string, jobOrder []string) *Model {
	m := &Model{
		name:    name,
		order:   jobOrder,
		jobs:    make(map[string]*jobState, len(jobOrder)),
		start:   time.Now(),
		noColor: os.Getenv("NO_COLOR") != "",
	}
	for _, id := range jobOrder {
		m.jobs[id] = &jobState{status: "pending"}
	}
	return m
}

// Observer returns a scheduler.Observer callback that updates the model.
func (m *Model) Observer() func(scheduler.Event) {
	return func(e scheduler.Event) {
		m.mu.Lock()
		j := m.jobs[e.Job]
		if j == nil {
			j = &jobState{status: "pending"}
			m.jobs[e.Job] = j
			m.order = append(m.order, e.Job)
		}
		switch e.Kind {
		case scheduler.JobStarted:
			j.status = "running"
			j.started = time.Now()
		case scheduler.JobFinished:
			if e.OK {
				j.status = "ok"
			} else {
				j.status = "failed"
			}
			j.dur = e.Duration
			j.step = ""
		case scheduler.JobSkipped:
			j.status = "skipped"
		case scheduler.StepStarted:
			j.step = e.Step
		case scheduler.StepFinished:
			if e.Cached {
				j.cached++
			} else {
				j.ran++
			}
		}
		m.mu.Unlock()
		m.render()
	}
}

const (
	esc     = "\033["
	reset   = "\033[0m"
	green   = "\033[32m"
	red     = "\033[31m"
	yellow  = "\033[33m"
	cyan    = "\033[36m"
	dim     = "\033[2m"
	bold    = "\033[1m"
	hideCur = "\033[?25l"
	showCur = "\033[?25h"
)

func (m *Model) c(color, s string) string {
	if m.noColor {
		return s
	}
	return color + s + reset
}

// Start hides the cursor and paints the first frame.
func (m *Model) Start() {
	if !m.noColor {
		fmt.Fprint(out, hideCur)
	}
	m.render()
	// tick so running timers update even without events
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			m.mu.Lock()
			done := m.done
			m.mu.Unlock()
			if done {
				return
			}
			m.render()
		}
	}()
}

// Stop paints a final frame and restores the cursor.
func (m *Model) Stop() {
	m.mu.Lock()
	m.done = true
	m.mu.Unlock()
	m.render()
	if !m.noColor {
		fmt.Fprint(out, showCur)
	}
}

func (m *Model) mark(status string) string {
	switch status {
	case "ok":
		return m.c(green, "✓")
	case "failed":
		return m.c(red, "✗")
	case "running":
		return m.c(yellow, "▶")
	case "skipped":
		return m.c(dim, "◌")
	default:
		return m.c(dim, "·")
	}
}

func (m *Model) render() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Rewind cursor over the previous frame.
	if m.lines > 0 {
		fmt.Fprintf(out, "%s%dA", esc, m.lines)
	}

	var b []byte
	w := func(s string) { b = append(b, s...) }
	clr := esc + "2K" // clear entire line

	elapsed := time.Since(m.start).Round(time.Second)
	header := fmt.Sprintf("%s  %s  %s\n", m.c(bold, "⚓ "+m.name), m.c(dim, "elapsed"), elapsed)
	w(clr + header)

	// column width from longest job id
	width := 8
	for _, id := range m.order {
		if len(id) > width {
			width = len(id)
		}
	}

	var okN, failN, runN, pendN int
	for _, id := range m.order {
		j := m.jobs[id]
		line := fmt.Sprintf("  %s %-*s ", m.mark(j.status), width, id)
		switch j.status {
		case "running":
			d := time.Since(j.started).Round(time.Second)
			detail := m.c(yellow, "running")
			if j.step != "" {
				detail += m.c(dim, " · "+j.step)
			}
			line += fmt.Sprintf("%s %s", detail, m.c(dim, d.String()))
			runN++
		case "ok":
			line += fmt.Sprintf("%s %s", m.c(green, "done"), m.c(dim, j.dur.Round(time.Millisecond).String()))
			if j.cached > 0 {
				line += m.c(dim, fmt.Sprintf(" (%d cached)", j.cached))
			}
			okN++
		case "failed":
			line += m.c(red, "failed")
			failN++
		case "skipped":
			line += m.c(dim, "skipped (dep failed)")
		default:
			line += m.c(dim, "pending")
			pendN++
		}
		w(clr + line + "\n")
	}

	summary := fmt.Sprintf("  %s %d done · %d running · %d pending",
		m.c(dim, "▸"), okN, runN, pendN)
	if failN > 0 {
		summary += m.c(red, fmt.Sprintf(" · %d failed", failN))
	}
	w(clr + summary + "\n")

	fmt.Fprint(out, string(b))
	m.lines = len(m.order) + 2 // header + jobs + summary
}

// JobOrderFromIDs is a helper for callers that already have topo-sorted IDs.
func JobOrderFromIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	return out
}

