// Package runner executes individual steps. The interface is container-ready;
// M1 ships a NativeRunner only.
package runner

import (
	"context"
	"io"
	"time"

	"github.com/KochC/shipHappens/internal/compiler"
)

// StepResult is the outcome of executing a step.
type StepResult struct {
	ExitCode int
	Cached   bool
	Duration time.Duration
	Err      error
}

// Runner executes a single step and streams output to out.
type Runner interface {
	Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult
}
