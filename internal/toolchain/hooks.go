package toolchain

import "context"

// SetBackend overrides the availability check and command runner, returning a
// restore function. Intended for tests in other packages; not used in normal
// operation. (Kept dependency-free — no testing import in the shipped binary.)
func SetBackend(available bool, run func(ctx context.Context, name string, args ...string) ([]byte, error)) (restore func()) {
	pl, pr := lookPath, runCmd
	if available {
		lookPath = func(string) (string, error) { return "/usr/local/bin/mise", nil }
	} else {
		lookPath = func(string) (string, error) { return "", errNoBackend }
	}
	if run != nil {
		runCmd = run
	}
	return func() { lookPath = pl; runCmd = pr }
}
