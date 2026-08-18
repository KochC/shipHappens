package runner

import (
	"context"
	"io"
	"path/filepath"
	"sort"
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
	Image       string
	Engine      string
	Mounts      []string // extra "-v" volume specs, e.g. "ship-pio-cache:/root/.platformio"
	Network     *bool    // nil=engine default; false=isolated (--network none); true=on
	NetworkName string   // join a named network (e.g. a services network); overrides Network
	// Allow is a job's egress host allow-list, retained for diagnostics.
	Allow []string
	// ProxyEnv, when set, routes the container's egress through Ship's filtering
	// forward-proxy (HTTP_PROXY/HTTPS_PROXY/NO_PROXY) so only allow-listed hosts
	// are reachable. This is real enforcement; SHIP_ALLOW is no longer used.
	ProxyEnv map[string]string
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

// buildArgs constructs the engine CLI args for a step. Pure and deterministic
// (env keys are sorted) so it can be unit-tested without invoking a container.
func (c ContainerRunner) buildArgs(step compiler.StepPlan, workdir string, env map[string]string) []string {
	// The container's working dir is /ship/work; a step WorkingDir maps to a
	// path relative to it (absolute paths are used as-is inside the container).
	wd := "/ship/work"
	if step.WorkingDir != "" {
		if filepath.IsAbs(step.WorkingDir) {
			wd = step.WorkingDir
		} else {
			wd = "/ship/work/" + step.WorkingDir
		}
	}
	args := []string{
		"run", "--rm",
		"-v", workdir + ":/ship/work",
		"-w", wd,
	}
	if c.NetworkName != "" {
		args = append(args, "--network", c.NetworkName)
	} else if c.Network != nil && !*c.Network {
		args = append(args, "--network", "none")
	}
	for _, m := range c.Mounts {
		args = append(args, "-v", m)
	}
	if len(c.ProxyEnv) > 0 {
		// Ensure the container can resolve the host running the proxy, then route
		// its egress through the proxy (real allow-list enforcement).
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
		for _, k := range sortedKeys(c.ProxyEnv) {
			args = append(args, "-e", k+"="+c.ProxyEnv[k])
		}
	}
	for _, k := range sortedKeys(env) {
		args = append(args, "-e", k+"="+env[k])
	}
	shell, shellArgs := shellCommand(step.Shell)
	args = append(args, c.Image, shell)
	args = append(args, shellArgs...)
	args = append(args, step.Run)
	return args
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Run executes step.Run via `<engine> run --rm -v <workdir>:/ship/work -w /ship/work <image> sh -c ...`.
func (c ContainerRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	bin := engineBinary(c.Engine)
	args := c.buildArgs(step, workdir, env)
	code, err := execRun(ctx, bin, args, out)
	return runResult(code, err, start)
}
