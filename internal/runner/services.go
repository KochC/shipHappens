package runner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
)

// Services manages sidecar containers for a job: it creates a dedicated network,
// starts each service on it (reachable by service name), waits for readiness,
// and tears everything down. Only meaningful for container jobs, which join the
// same network so steps can reach services by hostname.
type Services struct {
	Engine  string
	network string
	ids     []string
}

// StartServices creates a network and starts the given services on it, waiting
// for each to become healthy. Returns a Services handle whose Stop() tears
// everything down, and the network name to attach the job container to.
func StartServices(ctx context.Context, engine, jobID string, specs []compiler.ServiceSpec, out io.Writer) (*Services, string, error) {
	if len(specs) == 0 {
		return &Services{Engine: engine}, "", nil
	}
	bin := engineBinary(engine)
	net := "ship-net-" + sanitizeName(jobID)
	s := &Services{Engine: engine, network: net}

	// Create the network (best-effort remove first to avoid stale collisions).
	_, _ = execOutputCtx(ctx, bin, "network", "rm", net)
	if _, err := execOutputCtx(ctx, bin, "network", "create", net); err != nil {
		return s, "", fmt.Errorf("create network: %w", err)
	}

	for _, svc := range specs {
		name := net + "-" + sanitizeName(svc.Name)
		args := []string{"run", "-d", "--rm", "--name", name,
			"--network", net, "--network-alias", svc.Name}
		for k, v := range svc.Env {
			args = append(args, "-e", k+"="+v)
		}
		for _, p := range svc.Ports {
			args = append(args, "-p", p)
		}
		args = append(args, svc.Image)

		if _, err := execRunCollect(ctx, bin, args, out); err != nil {
			s.Stop(ctx)
			return s, "", fmt.Errorf("start service %s: %w", svc.Name, err)
		}
		s.ids = append(s.ids, name)

		if err := waitHealthy(ctx, bin, name, svc, out); err != nil {
			s.Stop(ctx)
			return s, "", err
		}
	}
	return s, net, nil
}

// Stop tears down all services and the network. Best-effort.
func (s *Services) Stop(ctx context.Context) {
	if s == nil {
		return
	}
	bin := engineBinary(s.Engine)
	for _, id := range s.ids {
		_, _ = execOutputCtx(ctx, bin, "rm", "-f", id)
	}
	if s.network != "" {
		_, _ = execOutputCtx(ctx, bin, "network", "rm", s.network)
	}
}

// waitHealthy polls the service's Health command (run inside the service) until
// it succeeds or the timeout elapses.
func waitHealthy(ctx context.Context, bin, name string, svc compiler.ServiceSpec, out io.Writer) error {
	if svc.Health == "" {
		return nil
	}
	timeout := svc.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		code, _ := execRunCollect(ctx, bin, []string{"exec", name, "sh", "-c", svc.Health}, io.Discard)
		if code == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service %s not healthy after %ds", svc.Name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// sanitizeName makes a docker-safe identifier fragment.
func sanitizeName(s string) string {
	b := []byte(s)
	for i := range b {
		c := b[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			b[i] = '-'
		}
	}
	return string(b)
}
