package flow

import (
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/planfile"
	"github.com/chris/shiphappens/internal/validator"
)

// RunFile loads a pipeline from a plan file (.pkl or .json), validates it, and
// runs it — the file-based counterpart to Main. CLI flags in argv are honored
// (e.g. --job, --resume, --engine, --tui). Returns the process exit code.
func RunFile(path string, argv []string) int {
	plan, err := planfile.Load(path)
	if err != nil {
		logs.Failure("load %s: %v", path, err)
		return 1
	}
	if diags := validator.Validate(plan, nil); len(diags) > 0 {
		logs.Failure("compilation failed:")
		for _, d := range diags {
			logs.Failure("  %s", d.String())
		}
		return 1
	}
	o := parseFlags(path, argv)
	return runCompiled(plan, o)
}

// MainFile is a convenience wrapper that runs a plan file and exits.
func MainFile(path string, argv []string) { osExit(RunFile(path, argv)) }
