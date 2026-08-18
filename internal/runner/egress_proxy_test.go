package runner

import (
	"context"
	"strings"
	"testing"
)

func TestStartEgressProxyEmpty(t *testing.T) {
	ep, err := StartEgressProxy(context.Background(), nil)
	if err != nil || ep != nil {
		t.Fatalf("empty allow-list → nil proxy, got %v %v", ep, err)
	}
	// nil methods must be safe.
	ep.Stop()
	if ep.Blocked() != nil {
		t.Error("nil Blocked should be nil")
	}
	if ep.ProxyEnv("host") != nil {
		t.Error("nil ProxyEnv should be nil")
	}
}

func TestStartEgressProxyAndEnv(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ep, err := StartEgressProxy(ctx, []string{"github.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Stop()

	env := ep.ProxyEnv("host.docker.internal")
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if !strings.HasPrefix(env[k], "http://host.docker.internal:") {
			t.Errorf("%s = %q, want host proxy URL", k, env[k])
		}
	}
	if !strings.Contains(env["NO_PROXY"], "localhost") {
		t.Errorf("NO_PROXY missing localhost: %q", env["NO_PROXY"])
	}
	if ep.port == 0 {
		t.Error("proxy should bind a real port")
	}
}

func TestContainerHost(t *testing.T) {
	if ContainerHost("docker") != "host.docker.internal" {
		t.Error("unexpected container host")
	}
}
