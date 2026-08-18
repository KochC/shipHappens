//go:build pkl

// Integration test for the real Pkl CLI. Requires `pkl` on PATH; run with:
//
//	go test -tags=pkl ./internal/planfile/ -run Integration -v
//
// It evaluates the example pipeline through the actual pkl binary and asserts
// the result decodes into a valid RunPlan matching the schema.
package planfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIntegrationRealPklEval(t *testing.T) {
	if _, err := exec.LookPath("pkl"); err != nil {
		t.Skip("pkl not installed")
	}
	// Locate the demo pipeline relative to the repo root (…/internal/planfile).
	root := filepath.Join("..", "..")
	pipeline := filepath.Join(root, "demos", "pkl-app", "pipeline.pkl")
	if _, err := os.Stat(pipeline); err != nil {
		t.Skipf("demo pipeline not found: %v", err)
	}

	plan, err := Load(pipeline)
	if err != nil {
		t.Fatalf("load real pkl pipeline: %v", err)
	}
	if plan.Name != "Pkl CI" {
		t.Fatalf("unexpected name: %q", plan.Name)
	}
	if plan.Vars["REGION"] != "eu-west" {
		t.Errorf("vars not evaluated: %+v", plan.Vars)
	}
	for _, want := range []string{"build", "test", "lint", "deploy"} {
		if plan.Job(want) == nil {
			t.Errorf("missing job %q", want)
		}
	}
	dep := plan.Job("deploy")
	if len(dep.Needs) != 2 {
		t.Errorf("deploy needs: %+v", dep.Needs)
	}
	if len(dep.Secrets) != 1 || dep.Secrets[0].Name != "DEPLOY_TOKEN" {
		t.Errorf("deploy secret not evaluated: %+v", dep.Secrets)
	}
	if plan.Job("lint").Image != "golang:1.22-alpine" {
		t.Errorf("lint image not evaluated: %q", plan.Job("lint").Image)
	}
	if b := plan.Job("build"); b.Steps[0].Cache == nil || len(b.Steps[0].Cache.Inputs) != 1 {
		t.Errorf("build cache not evaluated: %+v", b.Steps[0])
	}
}

func TestIntegrationReusableTemplates(t *testing.T) {
	if _, err := exec.LookPath("pkl"); err != nil {
		t.Skip("pkl not installed")
	}
	root := filepath.Join("..", "..")
	pipeline := filepath.Join(root, "demos", "reusable-app", "pipeline.pkl")
	if _, err := os.Stat(pipeline); err != nil {
		t.Skipf("reusable demo not found: %v", err)
	}
	plan, err := Load(pipeline)
	if err != nil {
		t.Fatalf("load reusable pipeline: %v", err)
	}
	// jobs composed from templates
	for _, want := range []string{"test", "build", "release-notes"} {
		if plan.Job(want) == nil {
			t.Errorf("missing templated job %q", want)
		}
	}
	// goTest() template → vet + test steps
	test := plan.Job("test")
	if len(test.Steps) != 2 || test.Steps[0].ID != "vet" {
		t.Errorf("goTest template steps wrong: %+v", test.Steps)
	}
	// goBuild() template → outputs + amended needs
	build := plan.Job("build")
	if len(build.Outputs) != 1 || build.Outputs[0] != "bin/ship" {
		t.Errorf("goBuild outputs wrong: %+v", build.Outputs)
	}
	if len(build.Needs) != 1 || build.Needs[0] != "test" {
		t.Errorf("amended needs wrong: %+v", build.Needs)
	}
}

func TestIntegrationToolchainField(t *testing.T) {
	if _, err := exec.LookPath("pkl"); err != nil {
		t.Skip("pkl not installed")
	}
	root := filepath.Join("..", "..")
	pipeline := filepath.Join(root, "demos", "toolchain-app", "pipeline.pkl")
	if _, err := os.Stat(pipeline); err != nil {
		t.Skipf("toolchain demo not found: %v", err)
	}
	plan, err := Load(pipeline)
	if err != nil {
		t.Fatalf("load toolchain pipeline: %v", err)
	}
	if plan.Toolchain["node"] != "20.11.0" {
		t.Errorf("workflow toolchain not evaluated: %+v", plan.Toolchain)
	}
	if j := plan.Job("node18"); j == nil || j.Toolchain["node"] != "18.20.4" {
		t.Errorf("job toolchain override not evaluated: %+v", j)
	}
}
