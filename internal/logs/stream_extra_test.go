package logs

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a concurrency-safe buffer for capturing output written from the
// Prefixed goroutine.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := SetOutput(buf)
	t.Cleanup(func() { SetOutput(prev) })
	return buf
}

func TestInfoHeaderSuccessFailure(t *testing.T) {
	buf := capture(t)
	Info("info %d", 1)
	Header("hdr %s", "x")
	Success("ok %d", 2)
	Failure("bad %d", 3)
	s := buf.String()
	for _, want := range []string{"info 1", "hdr x", "ok 2", "bad 3"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
}

func TestStepOKFailedCached(t *testing.T) {
	buf := capture(t)
	Step("job", "s1", "1.2s", true, false) // ok, not cached
	Step("job", "s2", "0s", false, false)  // failed branch
	Step("job", "s3", "0s", true, true)    // cached branch
	s := buf.String()
	if !strings.Contains(s, "s1") || !strings.Contains(s, "✓") {
		t.Error("ok step not rendered")
	}
	if !strings.Contains(s, "✗") {
		t.Error("failed mark not rendered")
	}
	if !strings.Contains(s, "cached") {
		t.Error("cached tag not rendered")
	}
}

func TestStepQuietSuppressed(t *testing.T) {
	buf := capture(t)
	SetQuiet(true)
	defer SetQuiet(false)
	Step("j", "s", "x", true, false)
	if buf.Len() != 0 {
		t.Fatalf("quiet Step should print nothing, got %q", buf.String())
	}
}

func TestColorHelper(t *testing.T) {
	// with color
	noColor = false
	if got := c(green, "hi"); !strings.Contains(got, "hi") || got == "hi" {
		t.Errorf("color path should wrap: %q", got)
	}
	// without color
	noColor = true
	defer func() { noColor = false }()
	if got := c(green, "hi"); got != "hi" {
		t.Errorf("no-color path should be plain: %q", got)
	}
}

func TestPrefixedWritesLines(t *testing.T) {
	SetQuiet(false)
	sb := &syncBuf{}
	prev := SetOutput(sb)
	defer SetOutput(prev)
	w := Prefixed("job")
	io.WriteString(w, "line1\nline2\n")
	if wc, ok := w.(io.WriteCloser); ok {
		wc.Close()
	}
	time.Sleep(50 * time.Millisecond)
	s := sb.String()
	if !strings.Contains(s, "job") || !strings.Contains(s, "line1") || !strings.Contains(s, "line2") {
		t.Errorf("prefixed output wrong: %q", s)
	}
}
