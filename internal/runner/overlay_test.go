package runner

import (
	"strings"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func TestOverlayBuildArgs(t *testing.T) {
	o := OverlayRunner{Image: "img", UpperHost: "/host/upper", Mounts: []string{"vol:/c"}}
	got := argsStr(o.buildArgs(compiler.StepPlan{Run: "make all"}, "/repo", map[string]string{"K": "V"}))

	for _, want := range []string{
		"--privileged",
		"-v /repo:/ship/work",
		"-v /host/upper:/ship/overlay/upper",
		"-w /ship/merged",
		"-v vol:/c",
		"-e K=V",
		"lowerdir=/ship/work,upperdir=/ship/overlay/upper,workdir=/ship/overlay/work",
		"falling back to direct execution", // graceful fallback embedded
	} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay args missing %q", want)
		}
	}
}

func TestOverlayOffline(t *testing.T) {
	no := false
	o := OverlayRunner{Image: "img", UpperHost: "/u", Network: &no}
	if !strings.Contains(argsStr(o.buildArgs(compiler.StepPlan{Run: "x"}, "/r", nil)), "--network none") {
		t.Error("overlay offline should add --network none")
	}
}
