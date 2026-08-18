package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// PreheatSpec describes warm-up work: pull an image and optionally run a
// cache-priming command in it.
type PreheatSpec struct {
	Image   string
	Warm    string
	Mounts  []string
	Engine  string
	Workdir string
}

// Preheat pulls the image and, if Warm is set, runs it once (mounting the given
// volumes) to prime shared caches. Errors are returned but treated as advisory
// by callers.
func Preheat(ctx context.Context, p PreheatSpec, out io.Writer) error {
	bin := engineBinary(p.Engine)

	pull := exec.CommandContext(ctx, bin, "pull", p.Image)
	pull.Stdout = out
	pull.Stderr = out
	pull.Env = os.Environ()
	if err := pull.Run(); err != nil {
		return err
	}

	if p.Warm == "" {
		return nil
	}

	args := []string{"run", "--rm"}
	if p.Workdir != "" {
		args = append(args, "-v", p.Workdir+":/ship/work", "-w", "/ship/work")
	}
	for _, m := range p.Mounts {
		args = append(args, "-v", m)
	}
	args = append(args, p.Image, "sh", "-c", p.Warm)

	warm := exec.CommandContext(ctx, bin, args...)
	warm.Stdout = out
	warm.Stderr = out
	warm.Env = os.Environ()
	return warm.Run()
}
