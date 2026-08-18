// Package toolchain resolves pinned tool versions (e.g. go 1.22.5, node 20.11)
// into a per-run PATH for NATIVE steps, giving container-grade reproducibility
// without containers.
//
// It uses mise (https://mise.jdx.dev) as the backend when available: mise
// installs the requested versions (cached under ~/.local/share/mise) and reports
// their bin directories, which we prepend to the step PATH. When mise is not
// installed, resolution is a graceful no-op (a warning), so pipelines still run
// against the host toolchain.
package toolchain

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"errors"
	"strings"
)

// lookPath / runCmd are indirection points for tests.
var lookPath = exec.LookPath

var runCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return out, err
}

// Available reports whether a supported backend (mise) is installed.
func Available() bool {
	_, err := lookPath("mise")
	return err == nil
}

// Merge combines a base (workflow) and override (job) tool map; override wins.
func Merge(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

// specArgs renders a tool map into deterministic "tool@version" args.
func specArgs(tools map[string]string) []string {
	keys := make([]string, 0, len(tools))
	for k := range tools {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, k := range keys {
		args = append(args, k+"@"+tools[k])
	}
	return args
}

// Resolve ensures the requested tool versions are installed and returns the
// bin directories to prepend to PATH. An empty tools map returns nil, nil.
// If no backend is available, it returns nil with an error the caller may treat
// as advisory (fall back to host tools).
func Resolve(ctx context.Context, tools map[string]string) (binPaths []string, err error) {
	if len(tools) == 0 {
		return nil, nil
	}
	if !Available() {
		return nil, fmt.Errorf("no toolchain backend (install mise: https://mise.jdx.dev) — using host tools")
	}
	specs := specArgs(tools)

	// Install (idempotent; cached). e.g. `mise install go@1.22.5 node@20.11.0`.
	if out, e := runCmd(ctx, "mise", append([]string{"install"}, specs...)...); e != nil {
		return nil, fmt.Errorf("mise install failed: %s", strings.TrimSpace(string(out)))
	}

	// Collect each tool's bin dir via `mise where <tool>@<version>`.
	for _, spec := range specs {
		out, e := runCmd(ctx, "mise", "where", spec)
		if e != nil {
			return nil, fmt.Errorf("mise where %s failed: %s", spec, strings.TrimSpace(string(out)))
		}
		dir := strings.TrimSpace(string(out))
		if dir != "" {
			binPaths = append(binPaths, dir+"/bin")
		}
	}
	return binPaths, nil
}

// PrependPath returns a PATH value with dirs prepended (highest priority first).
func PrependPath(current string, dirs []string) string {
	if len(dirs) == 0 {
		return current
	}
	parts := append(append([]string(nil), dirs...), current)
	return strings.Join(parts, ":")
}

var errNoBackend = errors.New("no toolchain backend")
