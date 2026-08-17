package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

// NativeRunner runs steps as local shell commands via `sh -c`.
type NativeRunner struct{}

// Run executes the step's command, streaming stdout+stderr to out. Cancellation
// of ctx kills the process (fail-fast support).
func (NativeRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", step.Run)
	cmd.Dir = workdir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	err := cmd.Run()
	res := StepResult{Duration: time.Since(start)}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = 1
		}
		res.Err = err
	}
	return res
}
