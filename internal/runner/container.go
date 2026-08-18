package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

// ContainerRunner runs steps inside a Docker container. The job's working
// directory is bind-mounted at /ship/work so file inputs/outputs (and thus the
// content-addressed cache) behave identically to native execution.
type ContainerRunner struct {
	Image  string
	Engine string // "docker" (default) or "podman"
}

// Run executes step.Run via `docker run --rm -v <workdir>:/ship/work -w /ship/work <image> sh -c ...`.
func (c ContainerRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	engine := c.Engine
	if engine == "" {
		engine = "docker"
	}

	args := []string{
		"run", "--rm",
		"-v", workdir + ":/ship/work",
		"-w", "/ship/work",
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, c.Image, "sh", "-c", step.Run)

	cmd := exec.CommandContext(ctx, engine, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = os.Environ()

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
