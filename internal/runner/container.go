package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

// ContainerRunner runs steps inside a container. The job's working directory is
// bind-mounted at /ship/work so file inputs/outputs (and thus the
// content-addressed cache) behave identically to native execution.
//
// Supported engines share a Docker-compatible `run` grammar:
//   - "docker" (default)  → `docker`   binary
//   - "podman"            → `podman`   binary
//   - "apple"             → `container` binary (Apple's native macOS container CLI)
type ContainerRunner struct {
	Image   string
	Engine  string
	Mounts  []string // extra "-v" volume specs, e.g. "ship-pio-cache:/root/.platformio"
	Network *bool    // nil=engine default; false=isolated (--network none); true=on
}

// engineBinary maps an engine name to its CLI executable.
func engineBinary(engine string) string {
	switch engine {
	case "", "docker":
		return "docker"
	case "podman":
		return "podman"
	case "apple", "container":
		return "container"
	default:
		// Allow passing an explicit binary name/path through unchanged.
		return engine
	}
}

// Run executes step.Run via `<engine> run --rm -v <workdir>:/ship/work -w /ship/work <image> sh -c ...`.
func (c ContainerRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	bin := engineBinary(c.Engine)

	args := []string{
		"run", "--rm",
		"-v", workdir + ":/ship/work",
		"-w", "/ship/work",
	}
	if c.Network != nil && !*c.Network {
		args = append(args, "--network", "none")
	}
	for _, m := range c.Mounts {
		args = append(args, "-v", m)
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, c.Image, "sh", "-c", step.Run)

	cmd := exec.CommandContext(ctx, bin, args...)
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
