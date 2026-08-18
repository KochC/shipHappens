package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// errNonZero reports a non-zero exit without an associated error object.
func errNonZero(code int) error { return fmt.Errorf("exited with code %d", code) }

// shellCommand maps a step's Shell to an executable + its argument list. The
// command string is appended by the caller. Defaults to `sh -c`.
func shellCommand(shell string) (bin string, args []string) {
	switch shell {
	case "", "sh":
		return "sh", []string{"-c"}
	case "bash":
		return "bash", []string{"-c"}
	case "python", "python3":
		return shell, []string{"-c"}
	case "node":
		return "node", []string{"-e"}
	default:
		// Treat any other value as the interpreter, invoked with "-c".
		return shell, []string{"-c"}
	}
}

// stepDir resolves a step's working directory: workingDir joined onto workdir
// (absolute workingDir wins). Empty workingDir means the job workdir.
func stepDir(workdir, workingDir string) string {
	if workingDir == "" {
		return workdir
	}
	if filepath.IsAbs(workingDir) {
		return workingDir
	}
	return filepath.Join(workdir, workingDir)
}

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
