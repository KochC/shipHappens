package runner

import (
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func argsStr(a []string) string { return strings.Join(a, " ") }

func TestContainerBuildArgsBasic(t *testing.T) {
	c := ContainerRunner{Image: "alpine:3.20"}
	args := c.buildArgs(compiler.StepPlan{Run: "echo hi"}, "/work", nil)
	got := argsStr(args)
	for _, want := range []string{
		"run --rm", "-v /work:/ship/work", "-w /ship/work", "alpine:3.20 sh -c echo hi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q; got: %s", want, got)
		}
	}
	if strings.Contains(got, "--network") {
		t.Errorf("default should have no --network; got: %s", got)
	}
}

func TestContainerBuildArgsOfflineAndMounts(t *testing.T) {
	no := false
	c := ContainerRunner{Image: "img", Network: &no, Mounts: []string{"vol:/root/.cache"}}
	got := argsStr(c.buildArgs(compiler.StepPlan{Run: "x"}, "/w", nil))
	if !strings.Contains(got, "--network none") {
		t.Errorf("offline should add --network none; got: %s", got)
	}
	if !strings.Contains(got, "-v vol:/root/.cache") {
		t.Errorf("mount not present; got: %s", got)
	}
}

func TestContainerBuildArgsNetworkOnOmitsFlag(t *testing.T) {
	yes := true
	c := ContainerRunner{Image: "img", Network: &yes}
	if strings.Contains(argsStr(c.buildArgs(compiler.StepPlan{Run: "x"}, "/w", nil)), "--network") {
		t.Error("Network(true) should not emit --network none")
	}
}

func TestContainerBuildArgsEnvSortedDeterministic(t *testing.T) {
	c := ContainerRunner{Image: "img"}
	env := map[string]string{"B": "2", "A": "1", "C": "3"}
	got := argsStr(c.buildArgs(compiler.StepPlan{Run: "x"}, "/w", env))
	ai := strings.Index(got, "A=1")
	bi := strings.Index(got, "B=2")
	ci := strings.Index(got, "C=3")
	if !(ai < bi && bi < ci) {
		t.Errorf("env not sorted deterministically; got: %s", got)
	}
}

func TestShQuote(t *testing.T) {
	cases := map[string]string{
		"echo hi": "'echo hi'",
		"it's":    `'it'\''s'`,
		"":        "''",
		"a 'b' c": `'a '\''b'\'' c'`,
	}
	for in, want := range cases {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContainerBuildArgsProxyEnv(t *testing.T) {
	c := ContainerRunner{Image: "img", Allow: []string{"github.com"}, ProxyEnv: map[string]string{
		"HTTP_PROXY": "http://host.docker.internal:12345",
		"NO_PROXY":   "localhost,127.0.0.1,::1",
	}}
	got := argsStr(c.buildArgs(compiler.StepPlan{Run: "npm ci"}, "/w", nil))
	if !strings.Contains(got, "HTTP_PROXY=http://host.docker.internal:12345") {
		t.Errorf("proxy env not injected: %s", got)
	}
	if !strings.Contains(got, "--add-host host.docker.internal:host-gateway") {
		t.Errorf("host-gateway add-host missing: %s", got)
	}
	if strings.Contains(got, "SHIP_ALLOW") {
		t.Errorf("SHIP_ALLOW should no longer be used: %s", got)
	}
}

func TestContainerBuildArgsNoProxyByDefault(t *testing.T) {
	c := ContainerRunner{Image: "img"}
	got := argsStr(c.buildArgs(compiler.StepPlan{Run: "echo hi"}, "/w", nil))
	if strings.Contains(got, "HTTP_PROXY") || strings.Contains(got, "add-host") {
		t.Errorf("no proxy expected without allow-list: %s", got)
	}
}
