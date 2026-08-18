package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

// NativeRunner runs steps as local shell commands.
type NativeRunner struct{}

// Run executes the step's command, streaming stdout+stderr to out. Cancellation
// of ctx kills the process group (fail-fast/timeout support), so child processes
// like `sleep` are terminated too. The step's Shell and WorkingDir are honored.
func (NativeRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	shell, shellArgs := shellCommand(step.Shell)
	args := append(shellArgs, step.Run)
	cmd := exec.Command(shell, args...)
	cmd.Dir = stepDir(workdir, step.WorkingDir)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return StepResult{ExitCode: 1, Err: err, Duration: time.Since(start)}
	}

	// Kill the whole process group when the context is done (timeout / cancel).
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err error
	select {
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-waitErr // reap
		err = ctx.Err()
	case err = <-waitErr:
	}

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
