package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

// OverlayRunner runs a container job with an overlayfs mount: the bind-mounted
// repo is the read-only lowerdir, and a per-job upperdir captures all writes as
// an isolated diff layer. This isolates parallel jobs' writes and makes the
// upper layer a reusable, cacheable result.
//
// Layout inside the container:
//
//	/ship/work            → repo bind mount (lowerdir, read-only view)
//	/ship/overlay/upper   → host-persisted upperdir (job's writes)
//	/ship/overlay/work    → overlayfs workdir (scratch)
//	/ship/merged          → the overlay mount the command runs in
//
// The upperdir is bind-mounted from a host path so the diff survives the
// container and can be inspected/cached.
type OverlayRunner struct {
	Image     string
	Engine    string
	Mounts    []string
	Network   *bool
	UpperHost string // host directory to persist the upper diff layer
}

// Run mounts the overlay and executes step.Run in the merged view.
func (o OverlayRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	bin := engineBinary(o.Engine)

	if err := os.MkdirAll(o.UpperHost, 0o755); err != nil {
		return StepResult{ExitCode: 1, Err: err, Duration: time.Since(start)}
	}

	args := []string{
		"run", "--rm",
		// Overlay requires the ability to mount inside the container.
		"--privileged",
		"-v", workdir + ":/ship/work",
		"-v", o.UpperHost + ":/ship/overlay/upper",
		"-w", "/ship/merged",
	}
	if o.Network != nil && !*o.Network {
		args = append(args, "--network", "none")
	}
	for _, m := range o.Mounts {
		args = append(args, "-v", m)
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}

	// The bootstrap script sets up the overlay then runs the user command in it.
	bootstrap := fmt.Sprintf(`set -e
mkdir -p /ship/overlay/upper /ship/overlay/work /ship/merged
mount -t overlay overlay -o lowerdir=/ship/work,upperdir=/ship/overlay/upper,workdir=/ship/overlay/work /ship/merged 2>/dev/null || {
  echo "overlay mount unavailable; falling back to direct execution in /ship/work" >&2
  cd /ship/work
  exec sh -c %s
}
cd /ship/merged
sh -c %s
`, shQuote(step.Run), shQuote(step.Run))

	args = append(args, o.Image, "sh", "-c", bootstrap)

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

// shQuote single-quotes a string for safe embedding inside a `sh -c '...'`.
func shQuote(s string) string {
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += `'\''`
		} else {
			out += string(r)
		}
	}
	return out + "'"
}
