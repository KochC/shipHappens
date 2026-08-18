package runner

import "testing"

func TestEngineBinary(t *testing.T) {
	cases := map[string]string{
		"":          "docker",
		"docker":    "docker",
		"podman":    "podman",
		"apple":     "container",
		"container": "container",
		"/usr/local/bin/nerdctl": "/usr/local/bin/nerdctl", // passthrough
	}
	for in, want := range cases {
		if got := engineBinary(in); got != want {
			t.Errorf("engineBinary(%q) = %q, want %q", in, got, want)
		}
	}
}
