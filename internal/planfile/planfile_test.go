package planfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func stubPkl(t *testing.T, out []byte, err error) {
	t.Helper()
	prev := pklEval
	pklEval = func(string) ([]byte, error) { return out, err }
	t.Cleanup(func() { pklEval = prev })
}

const sampleJSON = `{
  "name": "Pkl CI",
  "vars": {"REGION": "eu-west"},
  "jobs": [
    {"id": "build", "runsOn": "native",
     "steps": [{"id": "compile", "run": "make", "cache": {"inputs": ["**/*.go"]}}],
     "outputs": ["bin/**"]},
    {"id": "deploy", "runsOn": "native", "needs": ["build"],
     "secrets": [{"name": "DEPLOY_TOKEN"}],
     "steps": [{"id": "push", "run": "deploy"}]}
  ]
}`

func TestDecodeValid(t *testing.T) {
	p, err := Decode([]byte(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Pkl CI" || len(p.Jobs) != 2 {
		t.Fatalf("unexpected plan: %+v", p)
	}
	if p.Vars["REGION"] != "eu-west" {
		t.Errorf("vars not decoded: %+v", p.Vars)
	}
	build := p.Job("build")
	if build == nil || build.Steps[0].Cache == nil || build.Steps[0].Cache.Inputs[0] != "**/*.go" {
		t.Errorf("build step cache not decoded: %+v", build)
	}
	if len(build.Outputs) != 1 || build.Outputs[0] != "bin/**" {
		t.Errorf("outputs not decoded: %+v", build.Outputs)
	}
	dep := p.Job("deploy")
	if dep == nil || len(dep.Secrets) != 1 || dep.Secrets[0].Name != "DEPLOY_TOKEN" {
		t.Errorf("secrets not decoded: %+v", dep)
	}
}

func TestDecodeMissingName(t *testing.T) {
	if _, err := Decode([]byte(`{"jobs":[]}`)); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestDecodeUnknownField(t *testing.T) {
	if _, err := Decode([]byte(`{"name":"x","bogus":1,"jobs":[]}`)); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestDecodeBadJSON(t *testing.T) {
	if _, err := Decode([]byte(`{not json`)); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestLoadJSONFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "plan.json")
	os.WriteFile(f, []byte(sampleJSON), 0o644)
	p, err := Load(f)
	if err != nil || p.Name != "Pkl CI" {
		t.Fatalf("load json failed: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadPklViaStub(t *testing.T) {
	stubPkl(t, []byte(sampleJSON), nil)
	dir := t.TempDir()
	f := filepath.Join(dir, "pipeline.pkl")
	os.WriteFile(f, []byte(`amends "ship.pkl"`), 0o644)
	p, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Pkl CI" {
		t.Fatalf("pkl load wrong: %+v", p)
	}
}

func TestLoadPklEvalError(t *testing.T) {
	stubPkl(t, nil, errors.New("pkl boom"))
	dir := t.TempDir()
	f := filepath.Join(dir, "pipeline.pkl")
	os.WriteFile(f, []byte(`x`), 0o644)
	if _, err := Load(f); err == nil {
		t.Fatal("expected pkl eval error to propagate")
	}
}

func TestDefaultPklEvalNotFound(t *testing.T) {
	prevLP := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { lookPath = prevLP }()
	// call the real default via a fresh copy to avoid other tests' stubs
	_, err := defaultPklEval("x.pkl")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestDefaultPklEvalSuccess(t *testing.T) {
	prevLP, prevEO := lookPath, execOutput
	lookPath = func(string) (string, error) { return "/usr/bin/pkl", nil }
	execOutput = func(name string, args ...string) ([]byte, error) {
		if name != "pkl" || args[0] != "eval" {
			t.Fatalf("unexpected exec: %s %v", name, args)
		}
		return []byte(sampleJSON), nil
	}
	defer func() { lookPath, execOutput = prevLP, prevEO }()
	out, err := defaultPklEval("pipeline.pkl")
	if err != nil || len(out) == 0 {
		t.Fatalf("expected eval output, got %v", err)
	}
}

func TestExecOutputMissingBinary(t *testing.T) {
	if _, err := execOutput("definitely-not-a-real-binary-zzz"); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestExecOutputExitError(t *testing.T) {
	if _, err := execOutput("sh", "-c", "echo oops >&2; exit 3"); err == nil {
		t.Fatal("expected exit error")
	}
}

func TestDecodeExpandsMatrix(t *testing.T) {
	// A matrix job in JSON must be fanned out by Decode, and a dependent that
	// needs the matrix job must be rewired to all expansions.
	src := `{
      "name": "M",
      "jobs": [
        {"id":"test","matrix":{"os":["linux","mac"],"go":["1.21","1.22"]},
         "steps":[{"id":"info","run":"echo $OS $GO"}]},
        {"id":"done","needs":["test"],"steps":[{"id":"s","run":"true"}]}
      ]
    }`
	p, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// 4 expansions + done = 5 jobs.
	if len(p.Jobs) != 5 {
		t.Fatalf("want 5 jobs after expansion, got %d", len(p.Jobs))
	}
	if p.Job("test/1.22-linux") == nil {
		t.Error("missing matrix expansion test/1.22-linux")
	}
	j := p.Job("test/1.21-mac")
	if j == nil || j.Env["OS"] != "mac" || j.Env["GO"] != "1.21" {
		t.Errorf("expansion env wrong: %+v", j)
	}
	done := p.Job("done")
	if len(done.Needs) != 4 {
		t.Errorf("done should need all 4 expansions, got %v", done.Needs)
	}
}
