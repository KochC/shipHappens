package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// errNonZero reports a non-zero exit without an associated error object.
func errNonZero(code int) error { return fmt.Errorf("exited with code %d", code) }

// execRun is the indirection point for executing an external command. Tests
// override it to avoid invoking a real container engine. It writes combined
// output to out and returns the process exit code and error.
var execRun = func(ctx context.Context, bin string, args []string, out io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

// runResult builds a StepResult from an exit code / error and start time.
func runResult(code int, err error, start time.Time) StepResult {
	res := StepResult{Duration: time.Since(start), ExitCode: code}
	if err != nil {
		res.Err = err
	}
	return res
}
