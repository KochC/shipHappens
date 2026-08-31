package runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func TestShellCommand(t *testing.T) {
	cases := []struct {
		in   string
		bin  string
		args []string
	}{
		{"", "sh", []string{"-c"}},
		{"sh", "sh", []string{"-c"}},
		{"bash", "bash", []string{"-c"}},
		{"python", "python", []string{"-c"}},
		{"python3", "python3", []string{"-c"}},
		{"node", "node", []string{"-e"}},
		{"zsh", "zsh", []string{"-c"}}, // passthrough
	}
	for _, c := range cases {
		bin, args := shellCommand(c.in)
		if bin != c.bin || strings.Join(args, ",") != strings.Join(c.args, ",") {
			t.Errorf("shellCommand(%q) = %q %v, want %q %v", c.in, bin, args, c.bin, c.args)
		}
	}
}

func TestStepDir(t *testing.T) {
	if got := stepDir("/work", ""); got != "/work" {
		t.Errorf("empty workingDir should be workdir, got %q", got)
	}
	if got := stepDir("/work", "sub/dir"); got != filepath.Join("/work", "sub/dir") {
		t.Errorf("relative join wrong: %q", got)
	}
	if got := stepDir("/work", "/abs/path"); got != "/abs/path" {
		t.Errorf("absolute workingDir should win: %q", got)
	}
}

func TestContainerBuildArgsShellAndWorkingDir(t *testing.T) {
	c := ContainerRunner{Image: "img"}
	got := argsStr(c.buildArgs(compiler.StepPlan{
		Run: "pytest", Shell: "python", WorkingDir: "app",
	}, "/host", nil))
	if !strings.Contains(got, "-w /ship/work/app") {
		t.Errorf("workingDir not applied: %s", got)
	}
	if !strings.Contains(got, "img python -c pytest") {
		t.Errorf("shell not applied: %s", got)
	}
}

func TestContainerBuildArgsAbsWorkingDir(t *testing.T) {
	c := ContainerRunner{Image: "img"}
	got := argsStr(c.buildArgs(compiler.StepPlan{Run: "x", WorkingDir: "/opt"}, "/host", nil))
	if !strings.Contains(got, "-w /opt") {
		t.Errorf("absolute workingDir should be used as-is: %s", got)
	}
}

func TestNativeShellAndWorkingDir(t *testing.T) {
	dir := t.TempDir()
	var buf strings.Builder
	res := NativeRunner{}.Run(context.Background(), compiler.StepPlan{
		Run: "pwd", WorkingDir: ".",
	}, dir, nil, &buf)
	if res.ExitCode != 0 {
		t.Fatalf("run failed: %+v", res)
	}
}
