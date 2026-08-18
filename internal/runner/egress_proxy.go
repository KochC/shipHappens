package runner

import (
	"context"
	"fmt"
	"net"
	"runtime"

	"github.com/chris/shiphappens/internal/egress"
)

// EgressProxy is a host-side filtering forward-proxy that enforces a job's
// egress allow-list. The job container is pointed at it via HTTP_PROXY /
// HTTPS_PROXY, so standard tooling (curl, git, go, npm, apt, pip, …) can only
// reach allow-listed hosts. Any other host is refused with 403 — real
// enforcement rather than advisory scoping.
type EgressProxy struct {
	ln     net.Listener
	cancel context.CancelFunc
	port   int
	blocks []string
}

// StartEgressProxy launches a proxy on an ephemeral host port for the given
// allow-list. Returns nil (no error) when allow is empty. Callers must Stop it.
func StartEgressProxy(ctx context.Context, allow []string) (*EgressProxy, error) {
	if len(allow) == 0 {
		return nil, nil
	}
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("egress proxy listen: %w", err)
	}
	p := egress.New(allow)
	ep := &EgressProxy{ln: ln, port: ln.Addr().(*net.TCPAddr).Port}
	p.OnBlock(func(host string) { ep.blocks = append(ep.blocks, host) })

	pctx, cancel := context.WithCancel(ctx)
	ep.cancel = cancel
	go func() { _ = p.Serve(pctx, ln) }()
	return ep, nil
}

// Stop shuts the proxy down. Safe on nil.
func (e *EgressProxy) Stop() {
	if e == nil {
		return
	}
	e.cancel()
}

// Blocked returns the hosts that were refused (for diagnostics).
func (e *EgressProxy) Blocked() []string {
	if e == nil {
		return nil
	}
	return e.blocks
}

// ProxyEnv returns the HTTP(S)_PROXY environment a job container must use to
// route egress through the proxy. host is the address containers use to reach
// the host (e.g. "host.docker.internal" or a bridge gateway IP).
func (e *EgressProxy) ProxyEnv(host string) map[string]string {
	if e == nil {
		return nil
	}
	url := fmt.Sprintf("http://%s:%d", host, e.port)
	return map[string]string{
		"HTTP_PROXY":  url,
		"HTTPS_PROXY": url,
		"http_proxy":  url,
		"https_proxy": url,
		// Never proxy loopback / service-mesh names.
		"NO_PROXY": "localhost,127.0.0.1,::1",
		"no_proxy": "localhost,127.0.0.1,::1",
	}
}

// ContainerHost returns the hostname a container uses to reach the host running
// the proxy. On Docker Desktop / Podman this is host.docker.internal; the runner
// also adds --add-host to guarantee it resolves (see buildArgs).
func ContainerHost(engine string) string {
	// host.docker.internal is provided automatically on macOS/Windows Docker
	// Desktop, and via --add-host=host.docker.internal:host-gateway elsewhere.
	_ = runtime.GOOS
	return "host.docker.internal"
}
