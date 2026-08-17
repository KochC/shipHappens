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

const (
	reset  = "\033[0m"
	green  = "\033[32m"
	red    = "\033[31m"
	dim    = "\033[2m"
	cyan   = "\033[36m"
	bold   = "\033[1m"
)

var noColor = os.Getenv("NO_COLOR") != ""

func c(color, s string) string {
	if noColor {
		return s
	}
	return color + s + reset
}

// Prefixed returns a writer that prepends "[job] " to every line, synchronized
// across goroutines so parallel job output doesn't interleave mid-line.
func Prefixed(job string) io.Writer {
	pr, pw := io.Pipe()
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			mu.Lock()
			fmt.Fprintf(os.Stdout, "%s %s\n", c(cyan, "["+job+"]"), sc.Text())
			mu.Unlock()
		}
	}()
	return pw
}

// Info prints a synchronized info line.
func Info(format string, a ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(os.Stdout, format+"\n", a...)
}

// Step prints a per-step status line.
func Step(job, name, status string, ok, cached bool) {
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
	fmt.Fprintf(os.Stdout, "%s %s %s %s%s\n", mark, c(cyan, "["+job+"]"), name, c(dim, status), tag)
}

// Header prints a bold header.
func Header(format string, a ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(os.Stdout, "%s\n", c(bold, fmt.Sprintf(format, a...)))
}

// Success / Failure summary lines.
func Success(format string, a ...any) { Info(c(green, fmt.Sprintf(format, a...))) }
func Failure(format string, a ...any) { Info(c(red, fmt.Sprintf(format, a...))) }
