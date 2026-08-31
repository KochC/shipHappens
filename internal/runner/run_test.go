package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

// withStubExec swaps execRun for the duration of a test.
func withStubExec(t *testing.T, fn func(ctx context.Context, bin string, args []string, out io.Writer) (int, error)) {
	t.Helper()
	prev := execRun
	execRun = fn
	t.Cleanup(func() { execRun = prev })
}

func TestContainerRunSuccessAndError(t *testing.T) {
	var gotBin string
	var gotArgs []string
	withStubExec(t, func(_ context.Context, bin string, args []string, out io.Writer) (int, error) {
		gotBin, gotArgs = bin, args
		io.WriteString(out, "hello")
		return 0, nil
	})
	c := ContainerRunner{Image: "img", Engine: "podman"}
	res := c.Run(context.Background(), compiler.StepPlan{Run: "echo hi"}, "/w", map[string]string{"A": "1"}, io.Discard)
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("want success, got %+v", res)
	}
	if gotBin != "podman" {
		t.Fatalf("engine binary = %q", gotBin)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "run" {
		t.Fatalf("args not passed through: %v", gotArgs)
	}

	// error path
	withStubExec(t, func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		return 3, errors.New("boom")
	})
	res = c.Run(context.Background(), compiler.StepPlan{Run: "x"}, "/w", nil, io.Discard)
	if res.Err == nil || res.ExitCode != 3 {
		t.Fatalf("want exit 3 error, got %+v", res)
	}
}

func TestOverlayRunSuccessErrorAndMkdirFail(t *testing.T) {
	// success
	withStubExec(t, func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		return 0, nil
	})
	o := OverlayRunner{Image: "img", UpperHost: t.TempDir()}
	if res := o.Run(context.Background(), compiler.StepPlan{Run: "x"}, "/r", nil, io.Discard); res.Err != nil {
		t.Fatalf("want success, got %+v", res)
	}

	// exec error
	withStubExec(t, func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		return 2, errors.New("fail")
	})
	if res := o.Run(context.Background(), compiler.StepPlan{Run: "x"}, "/r", nil, io.Discard); res.ExitCode != 2 {
		t.Fatalf("want exit 2, got %+v", res)
	}

	// mkdir failure path
	prev := mkdirAll
	mkdirAll = func(string, os.FileMode) error { return errors.New("nomkdir") }
	t.Cleanup(func() { mkdirAll = prev })
	if res := o.Run(context.Background(), compiler.StepPlan{Run: "x"}, "/r", nil, io.Discard); res.Err == nil || res.ExitCode != 1 {
		t.Fatalf("want mkdir error, got %+v", res)
	}
}

func TestPreheatPullOnly(t *testing.T) {
	var calls [][]string
	withStubExec(t, func(_ context.Context, _ string, args []string, _ io.Writer) (int, error) {
		calls = append(calls, args)
		return 0, nil
	})
	err := Preheat(context.Background(), PreheatSpec{Image: "img"}, io.Discard)
	if err != nil {
		t.Fatalf("preheat pull-only: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != "pull" {
		t.Fatalf("expected a single pull call, got %v", calls)
	}
}

func TestPreheatPullFails(t *testing.T) {
	withStubExec(t, func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		return 1, errors.New("pull failed")
	})
	if err := Preheat(context.Background(), PreheatSpec{Image: "img", Warm: "prime"}, io.Discard); err == nil {
		t.Fatal("expected pull error")
	}

	// pull returns nonzero without error object
	withStubExec(t, func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		return 7, nil
	})
	if err := Preheat(context.Background(), PreheatSpec{Image: "img"}, io.Discard); err == nil {
		t.Fatal("expected non-zero pull to error")
	}
}

func TestPreheatWarmRuns(t *testing.T) {
	var calls [][]string
	withStubExec(t, func(_ context.Context, _ string, args []string, _ io.Writer) (int, error) {
		calls = append(calls, args)
		return 0, nil
	})
	err := Preheat(context.Background(), PreheatSpec{
		Image: "img", Warm: "prime", Workdir: "/w", Mounts: []string{"v:/c"},
	}, io.Discard)
	if err != nil {
		t.Fatalf("warm preheat: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected pull+warm (2 calls), got %d", len(calls))
	}
	// warm args must include workdir + mount + command
	warm := calls[1]
	joined := argsStr(warm)
	for _, want := range []string{"-v /w:/ship/work", "-w /ship/work", "-v v:/c", "sh -c prime"} {
		if !contains(joined, want) {
			t.Errorf("warm args missing %q; got %s", want, joined)
		}
	}
}

func TestPreheatWarmFails(t *testing.T) {
	n := 0
	withStubExec(t, func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		n++
		if n == 1 {
			return 0, nil // pull ok
		}
		return 5, nil // warm nonzero, no error obj
	})
	if err := Preheat(context.Background(), PreheatSpec{Image: "img", Warm: "prime"}, io.Discard); err == nil {
		t.Fatal("expected warm failure to surface")
	}
}

func TestWarmArgsNoWorkdir(t *testing.T) {
	p := PreheatSpec{Image: "img", Warm: "x"}
	if contains(argsStr(p.warmArgs()), "/ship/work") {
		t.Error("no workdir should omit work mount")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
