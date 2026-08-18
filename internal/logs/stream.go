// Package logs provides prefixed, colored, thread-safe log streaming.
package logs

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
)

var mu sync.Mutex

// out is the sink for all log output (overridable in tests).
var out io.Writer = os.Stdout

// SetOutput redirects log output (used by tests); returns the previous sink.
func SetOutput(w io.Writer) io.Writer {
	prev := out
	out = w
	return prev
}

const (
	reset  = "\033[0m"
	green  = "\033[32m"
	red    = "\033[31m"
	dim    = "\033[2m"
	cyan   = "\033[36m"
	bold   = "\033[1m"
)

var noColor = os.Getenv("NO_COLOR") != ""

// quiet suppresses streaming step/prefixed/status output (used by the TUI which
// paints its own dashboard). Summary/info calls still print.
var quiet bool

// SetQuiet toggles suppression of per-line streaming output.
func SetQuiet(q bool) { quiet = q }

func c(color, s string) string {
	if noColor {
		return s
	}
	return color + s + reset
}

// Prefixed returns a writer that prepends "[job] " to every line, synchronized
// across goroutines so parallel job output doesn't interleave mid-line. In quiet
// mode it discards output (the TUI shows status instead).
func Prefixed(job string) io.Writer {
	return MaskedPrefixed(job, nil)
}

// MaskedPrefixed is like Prefixed but applies mask to every line before printing
// (used to redact secret values). A nil mask is a no-op.
func MaskedPrefixed(job string, mask func(string) string) io.Writer {
	if quiet {
		return io.Discard
	}
	pr, pw := io.Pipe()
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if mask != nil {
				line = mask(line)
			}
			mu.Lock()
			fmt.Fprintf(out, "%s %s\n", c(cyan, "["+job+"]"), line)
			mu.Unlock()
		}
	}()
	return pw
}

// Info prints a synchronized info line.
func Info(format string, a ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(out, format+"\n", a...)
}

// Step prints a per-step status line.
func Step(job, name, status string, ok, cached bool) {
	if quiet {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	mark := c(green, "✓")
	if !ok {
		mark = c(red, "✗")
	}
	tag := ""
	if cached {
		tag = " " + c(dim, "(cached)")
	}
	fmt.Fprintf(out, "%s %s %s %s%s\n", mark, c(cyan, "["+job+"]"), name, c(dim, status), tag)
}

// Header prints a bold header.
func Header(format string, a ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(out, "%s\n", c(bold, fmt.Sprintf(format, a...)))
}

// Success / Failure summary lines.
func Success(format string, a ...any) { Info(c(green, fmt.Sprintf(format, a...))) }
func Failure(format string, a ...any) { Info(c(red, fmt.Sprintf(format, a...))) }
