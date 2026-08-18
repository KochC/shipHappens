package runner

import (
	"context"
	"io"
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

// warmArgs builds the `run` args for the warm command (pure/testable).
func (p PreheatSpec) warmArgs() []string {
	args := []string{"run", "--rm"}
	if p.Workdir != "" {
		args = append(args, "-v", p.Workdir+":/ship/work", "-w", "/ship/work")
	}
	for _, m := range p.Mounts {
		args = append(args, "-v", m)
	}
	args = append(args, p.Image, "sh", "-c", p.Warm)
	return args
}

// Preheat pulls the image and, if Warm is set, runs it once (mounting the given
// volumes) to prime shared caches. Errors are returned but treated as advisory
// by callers.
func Preheat(ctx context.Context, p PreheatSpec, out io.Writer) error {
	bin := engineBinary(p.Engine)

	if code, err := execRun(ctx, bin, []string{"pull", p.Image}, out); err != nil || code != 0 {
		if err == nil {
			err = errNonZero(code)
		}
		return err
	}

	if p.Warm == "" {
		return nil
	}

	if code, err := execRun(ctx, bin, p.warmArgs(), out); err != nil || code != 0 {
		if err == nil {
			err = errNonZero(code)
		}
		return err
	}
	return nil
}
