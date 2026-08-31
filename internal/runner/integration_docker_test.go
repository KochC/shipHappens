//go:build docker

// Package runner integration tests. These require a real container engine and
// are excluded from the default `go test`. Run with:
//
//	make integration                 # docker (default)
//	make integration ENGINE=podman
//	make integration ENGINE=apple
//
// or directly:
//
//	SHIP_TEST_ENGINE=docker go test -tags=docker -run Integration ./internal/runner/ -v
//
// The tests skip gracefully if the engine binary is missing or the daemon is
// unreachable, so they never produce false failures on machines without one.
package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KochC/shipHappens/internal/compiler"
)

// testImage is a tiny image that has a POSIX shell. Override with SHIP_TEST_IMAGE.
func testImage() string {
	if v := os.Getenv("SHIP_TEST_IMAGE"); v != "" {
		return v
	}
	return "alpine:3.20"
}

func testEngine() string {
	if v := os.Getenv("SHIP_TEST_ENGINE"); v != "" {
		return v
	}
	return "docker"
}

// mountableTempDir returns a fresh directory the container engine can actually
// bind-mount. macOS VM backends (Colima/Lima) only share specific host paths
// (typically the user's home), and NOT $TMPDIR — so t.TempDir() bind mounts come
// up empty. We therefore create the dir under the current working directory
// (the repo, which lives under $HOME) and register cleanup. Override the base
// with SHIP_TEST_MOUNT_BASE if your engine shares a different path.
func mountableTempDir(t *testing.T) string {
	t.Helper()
	base := os.Getenv("SHIP_TEST_MOUNT_BASE")
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		base = wd
	}
	dir, err := os.MkdirTemp(base, ".itest-")
	if err != nil {
		t.Fatalf("mkdir temp under %s: %v", base, err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// requireEngine skips the test unless the engine binary exists and responds.
func requireEngine(t *testing.T) string {
	t.Helper()
	eng := testEngine()
	bin := engineBinary(eng)
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("engine %q (%s) not installed", eng, bin)
	}
	// Cheap liveness probe.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, bin, "version").Run(); err != nil {
		t.Skipf("engine %q not responsive: %v", eng, err)
	}
	return eng
}

func TestIntegrationContainerRunsAndMountsWorkdir(t *testing.T) {
	eng := requireEngine(t)
	work := mountableTempDir(t)
	if err := os.WriteFile(filepath.Join(work, "input.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	c := ContainerRunner{Image: testImage(), Engine: eng}
	res := c.Run(context.Background(),
		compiler.StepPlan{Run: "cat input.txt && echo written > output.txt"},
		work, map[string]string{"GREETING": "hi"}, &out)

	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("container run failed: %+v\noutput:\n%s", res, out.String())
	}
	if !strings.Contains(out.String(), "payload") {
		t.Errorf("bind-mounted input not visible in container; output:\n%s", out.String())
	}
	// A write inside the container must appear on the host bind mount.
	if b, err := os.ReadFile(filepath.Join(work, "output.txt")); err != nil || !strings.Contains(string(b), "written") {
		t.Errorf("container write not reflected on host: err=%v content=%q", err, b)
	}
}

func TestIntegrationContainerEnvAndExitCode(t *testing.T) {
	eng := requireEngine(t)
	var out bytes.Buffer
	c := ContainerRunner{Image: testImage(), Engine: eng}

	// env propagation
	res := c.Run(context.Background(), compiler.StepPlan{Run: `test "$FOO" = "bar"`},
		mountableTempDir(t), map[string]string{"FOO": "bar"}, &out)
	if res.ExitCode != 0 {
		t.Errorf("env not propagated; exit=%d out=%s", res.ExitCode, out.String())
	}

	// non-zero exit code is surfaced
	res = c.Run(context.Background(), compiler.StepPlan{Run: "exit 7"}, t.TempDir(), nil, &out)
	if res.ExitCode != 7 || res.Err == nil {
		t.Errorf("expected exit 7, got %+v", res)
	}
}

func TestIntegrationContainerOffline(t *testing.T) {
	eng := requireEngine(t)
	no := false
	var out bytes.Buffer
	c := ContainerRunner{Image: testImage(), Engine: eng, Network: &no}
	// With --network none, loopback exists but external routing does not; assert
	// the container still runs local commands (isolation shouldn't break exec).
	res := c.Run(context.Background(), compiler.StepPlan{Run: "echo offline-ok"}, t.TempDir(), nil, &out)
	if res.ExitCode != 0 {
		t.Fatalf("offline container should still run local commands: %+v out=%s", res, out.String())
	}
	if !strings.Contains(out.String(), "offline-ok") {
		t.Errorf("missing output: %s", out.String())
	}
}

func TestIntegrationOverlayOrFallback(t *testing.T) {
	eng := requireEngine(t)
	work := mountableTempDir(t)
	os.WriteFile(filepath.Join(work, "base.txt"), []byte("base"), 0o644)

	var out bytes.Buffer
	o := OverlayRunner{Image: testImage(), Engine: eng, UpperHost: filepath.Join(mountableTempDir(t), "upper")}
	res := o.Run(context.Background(),
		compiler.StepPlan{Run: "cat base.txt && echo built > artifact.txt && echo DONE"},
		work, nil, &out)

	// Overlay may be unavailable in the kernel; the runner falls back to direct
	// execution. Either way the command must succeed and read the base file.
	if res.ExitCode != 0 {
		t.Fatalf("overlay run failed: %+v\n%s", res, out.String())
	}
	if !strings.Contains(out.String(), "base") || !strings.Contains(out.String(), "DONE") {
		t.Errorf("overlay/fallback output wrong:\n%s", out.String())
	}
}

func TestIntegrationPreheatPull(t *testing.T) {
	eng := requireEngine(t)
	var out bytes.Buffer
	err := Preheat(context.Background(),
		PreheatSpec{Image: testImage(), Engine: eng,
			Warm: "echo warm-cache-primed"}, &out)
	if err != nil {
		t.Fatalf("preheat pull+warm failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "warm-cache-primed") {
		t.Errorf("warm command did not run:\n%s", out.String())
	}
}
