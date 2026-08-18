// Package planfile loads a compiled RunPlan from disk. It supports two formats:
//
//   - .json  — a RunPlan JSON artifact (as produced by `--compile`).
//   - .pkl   — a Pkl pipeline; evaluated to JSON via `pkl eval -f json` and then
//     decoded. Requires the `pkl` CLI on PATH (https://pkl-lang.org).
//
// This lets pipelines be authored in Pkl (typed, sandboxed config) instead of
// Go, while reusing the exact same IR, validator, scheduler, and runners.
package planfile

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chris/shiphappens/internal/compiler"
)

// execOutput runs a command and returns stdout, or a formatted error including
// stderr. Overridable in tests.
var execOutput = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// lookPath is the PATH lookup indirection (overridable in tests).
var lookPath = exec.LookPath

// pklEval evaluates a Pkl file to JSON. Overridable in tests.
var pklEval = defaultPklEval

func defaultPklEval(path string) ([]byte, error) {
	if _, err := lookPath("pkl"); err != nil {
		return nil, fmt.Errorf("pkl CLI not found on PATH; install it from https://pkl-lang.org to author .pkl pipelines")
	}
	return execOutput("pkl", "eval", "-f", "json", path)
}

// Load reads a plan file and returns the parsed RunPlan. The format is chosen by
// extension: ".pkl" is evaluated with the Pkl CLI; anything else is treated as
// RunPlan JSON.
func Load(path string) (*compiler.RunPlan, error) {
	var data []byte
	var err error
	if strings.EqualFold(filepath.Ext(path), ".pkl") {
		data, err = pklEval(path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// Decode parses RunPlan JSON.
func Decode(data []byte) (*compiler.RunPlan, error) {
	var p compiler.RunPlan
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("invalid plan: missing name")
	}
	return &p, nil
}
