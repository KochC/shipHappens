package runner

import (
	"context"
	"fmt"
	"io"
	"os"
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

// mkdirAll is the injection point for creating the upper dir (overridable in tests).
var mkdirAll = os.MkdirAll

// Run mounts the overlay and executes step.Run in the merged view.
func (o OverlayRunner) Run(ctx context.Context, step compiler.StepPlan, workdir string, env map[string]string, out io.Writer) StepResult {
	start := time.Now()
	bin := engineBinary(o.Engine)

	if err := mkdirAll(o.UpperHost, 0o755); err != nil {
		return StepResult{ExitCode: 1, Err: err, Duration: time.Since(start)}
	}

	args := o.buildArgs(step, workdir, env)
	code, err := execRun(ctx, bin, args, out)
	return runResult(code, err, start)
}

// buildArgs constructs the engine CLI args (pure/testable). It bootstraps an
// overlayfs mount inside the container, falling back to direct execution when
// overlay is unavailable.
func (o OverlayRunner) buildArgs(step compiler.StepPlan, workdir string, env map[string]string) []string {
	args := []string{
		"run", "--rm",
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
	for _, k := range sortedKeys(env) {
		args = append(args, "-e", k+"="+env[k])
	}

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
	return args
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
