// Package outputs handles step/job output values (GitHub Actions'
// $GITHUB_OUTPUT analog). A step writes "key=value" lines (or "key<<DELIM ...
// DELIM" heredocs) to the file named by $SHIP_OUTPUT; the scheduler parses them
// after the step and accumulates them as the job's outputs, exposed to
// dependents.
package outputs

import (
	"bufio"
	"os"
	"strings"
)

// Parse reads a $SHIP_OUTPUT file and returns its key/value pairs. Supported:
//
//	key=value
//	key<<EOF
//	multi
//	line
//	EOF
//
// Unknown/blank lines are ignored. A missing file yields an empty map, no error.
func Parse(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if k, delim, ok := heredocHeader(line); ok {
			var b strings.Builder
			for sc.Scan() {
				if sc.Text() == delim {
					break
				}
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(sc.Text())
			}
			out[k] = b.String()
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			out[line[:i]] = line[i+1:]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// heredocHeader detects a "key<<DELIM" line.
func heredocHeader(line string) (key, delim string, ok bool) {
	i := strings.Index(line, "<<")
	if i <= 0 || i+2 >= len(line) {
		return "", "", false
	}
	return line[:i], line[i+2:], true
}
