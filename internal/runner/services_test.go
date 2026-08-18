package runner

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

// stubServiceExec swaps the ctx-aware exec indirections for services tests.
func stubServiceExec(t *testing.T,
	outFn func(ctx context.Context, name string, args ...string) ([]byte, error),
	runFn func(ctx context.Context, name string, args []string, out io.Writer) (int, error)) {
	t.Helper()
	po, pr := execOutputCtx, execRunCollect
	execOutputCtx = outFn
	execRunCollect = runFn
	t.Cleanup(func() { execOutputCtx = po; execRunCollect = pr })
}

func TestStartServicesEmpty(t *testing.T) {
	s, net, err := StartServices(context.Background(), "docker", "job", nil, io.Discard)
	if err != nil || net != "" || s == nil {
		t.Fatalf("empty services should no-op: %v %q", err, net)
	}
	s.Stop(context.Background()) // safe no-op
}

func TestStartServicesSuccess(t *testing.T) {
	var networkCreated bool
	var ranImages []string
	stubServiceExec(t,
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) >= 2 && args[0] == "network" && args[1] == "create" {
				networkCreated = true
			}
			return nil, nil
		},
		func(_ context.Context, _ string, args []string, _ io.Writer) (int, error) {
			if args[0] == "run" {
				ranImages = append(ranImages, args[len(args)-1])
			}
			return 0, nil // run + health-exec both succeed
		},
	)
	specs := []compiler.ServiceSpec{
		{Name: "db", Image: "postgres:16", Env: map[string]string{"POSTGRES_PASSWORD": "x"},
			Ports: []string{"5432:5432"}, Health: "pg_isready", Timeout: 5},
	}
	s, net, err := StartServices(context.Background(), "docker", "job", specs, io.Discard)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !networkCreated || net == "" {
		t.Fatal("network not created")
	}
	if len(ranImages) != 1 || ranImages[0] != "postgres:16" {
		t.Fatalf("service not started: %v", ranImages)
	}
	s.Stop(context.Background())
}

func TestStartServicesNetworkCreateFails(t *testing.T) {
	stubServiceExec(t,
		func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) >= 2 && args[1] == "create" {
				return nil, errors.New("no network")
			}
			return nil, nil
		},
		func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) { return 0, nil },
	)
	_, _, err := StartServices(context.Background(), "docker", "job",
		[]compiler.ServiceSpec{{Name: "db", Image: "x"}}, io.Discard)
	if err == nil {
		t.Fatal("expected network create error")
	}
}

func TestStartServicesRunFails(t *testing.T) {
	stubServiceExec(t,
		func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		func(_ context.Context, _ string, args []string, _ io.Writer) (int, error) {
			if args[0] == "run" {
				return 1, errors.New("image pull failed")
			}
			return 0, nil
		},
	)
	_, _, err := StartServices(context.Background(), "docker", "job",
		[]compiler.ServiceSpec{{Name: "db", Image: "x"}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "start service") {
		t.Fatalf("expected start error, got %v", err)
	}
}

func TestServiceHealthTimeout(t *testing.T) {
	stubServiceExec(t,
		func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		func(_ context.Context, _ string, args []string, _ io.Writer) (int, error) {
			if len(args) > 0 && args[0] == "exec" {
				return 1, nil // health check never passes
			}
			return 0, nil
		},
	)
	_, _, err := StartServices(context.Background(), "docker", "job",
		[]compiler.ServiceSpec{{Name: "db", Image: "x", Health: "false", Timeout: 1}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not healthy") {
		t.Fatalf("expected health timeout, got %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	if sanitizeName("my/job:1") != "my-job-1" {
		t.Errorf("sanitizeName wrong: %q", sanitizeName("my/job:1"))
	}
}

func TestServiceNoHealthReadyImmediately(t *testing.T) {
	stubServiceExec(t,
		func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		func(_ context.Context, _ string, _ []string, _ io.Writer) (int, error) { return 0, nil },
	)
	// no Health → ready immediately; also exercises Stop network-rm path
	s, _, err := StartServices(context.Background(), "docker", "job",
		[]compiler.ServiceSpec{{Name: "cache", Image: "redis"}}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	s.Stop(context.Background())
}

func TestServiceHealthEventuallyPasses(t *testing.T) {
	n := 0
	stubServiceExec(t,
		func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		func(_ context.Context, _ string, args []string, _ io.Writer) (int, error) {
			if len(args) > 0 && args[0] == "exec" {
				n++
				if n < 2 {
					return 1, nil // fail first poll
				}
				return 0, nil // pass second
			}
			return 0, nil
		},
	)
	_, _, err := StartServices(context.Background(), "docker", "job",
		[]compiler.ServiceSpec{{Name: "db", Image: "x", Health: "check", Timeout: 5}}, io.Discard)
	if err != nil {
		t.Fatalf("health should eventually pass: %v", err)
	}
	if n < 2 {
		t.Fatalf("expected at least 2 health polls, got %d", n)
	}
}

func TestDefaultExecFns(t *testing.T) {
	// exercise the real execOutputCtx / execRunCollect against sh
	if _, err := execOutputCtx(context.Background(), "sh", "-c", "exit 0"); err != nil {
		t.Errorf("execOutputCtx success: %v", err)
	}
	if _, err := execOutputCtx(context.Background(), "sh", "-c", "exit 1"); err == nil {
		t.Error("execOutputCtx should error on nonzero")
	}
	if code, _ := execRunCollect(context.Background(), "sh", []string{"-c", "exit 3"}, io.Discard); code != 3 {
		t.Errorf("execRunCollect code = %d", code)
	}
}
