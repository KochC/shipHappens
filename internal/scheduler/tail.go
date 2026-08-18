package scheduler

import (
	"io"
	"strings"
	"sync"
)

// tailWriter wraps an io.Writer and retains the last maxLines complete lines
// written through it, so a failing step's output can be surfaced (in the TUI,
// the end-of-run digest, and MCP ship_status) without persisting everything in
// memory. It is safe for concurrent writes.
type tailWriter struct {
	w        io.Writer
	maxLines int

	mu      sync.Mutex
	lines   []string // completed lines (most recent maxLines)
	partial strings.Builder
}

// newTailWriter tees writes to w while keeping the last maxLines lines.
func newTailWriter(w io.Writer, maxLines int) *tailWriter {
	if maxLines <= 0 {
		maxLines = 20
	}
	return &tailWriter{w: w, maxLines: maxLines}
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	for _, b := range p {
		if b == '\n' {
			t.push(t.partial.String())
			t.partial.Reset()
			continue
		}
		t.partial.WriteByte(b)
	}
	t.mu.Unlock()
	if t.w != nil {
		return t.w.Write(p)
	}
	return len(p), nil
}

// push appends a completed line, trimming to the tail window.
func (t *tailWriter) push(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
}

// Tail returns a copy of the retained lines, including any trailing partial
// line (output that ended without a newline, e.g. a bare error message).
func (t *tailWriter) Tail() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string(nil), t.lines...)
	if t.partial.Len() > 0 {
		out = append(out, t.partial.String())
	}
	// Bound again in case the partial pushed us over.
	if len(out) > t.maxLines {
		out = out[len(out)-t.maxLines:]
	}
	return out
}
