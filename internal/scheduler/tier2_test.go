package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func TestJobIfSkips(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Vars: map[string]string{"BRANCH": "dev"}, Jobs: []compiler.JobPlan{
		{ID: "a", If: "env.BRANCH == 'main'", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
		{ID: "b", If: "env.BRANCH == 'dev'", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	var skipped, ran []string
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true,
		Observer: func(e Event) {
			if e.Kind == JobSkipped {
				skipped = append(skipped, e.Job)
			}
			if e.Kind == JobStarted {
				ran = append(ran, e.Job)
			}
		}})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	if len(skipped) != 1 || skipped[0] != "a" {
		t.Fatalf("a should be skipped: %v", skipped)
	}
	if len(ran) != 1 || ran[0] != "b" {
		t.Fatalf("only b should run: %v", ran)
	}
}

func TestJobIfInvalidFails(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", If: "@bad@", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !res.Failed {
		t.Fatal("invalid if expression should fail the job")
	}
}

func TestStepIf(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{
			{ID: "set", Run: `echo "mode=fast" >> "$SHIP_OUTPUT"`},
			{ID: "fast", Run: `echo fast > fast.txt`, If: "outputs.self.mode == 'fast'"},
			{ID: "slow", Run: `echo slow > slow.txt`, If: "outputs.self.mode == 'slow'"},
		}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(work, "fast.txt")); err != nil {
		t.Error("fast step should have run")
	}
	if _, err := os.Stat(filepath.Join(work, "slow.txt")); err == nil {
		t.Error("slow step should have been skipped")
	}
}

func TestOutputsFlowToDependent(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "build", Steps: []compiler.StepPlan{
			{ID: "ver", Run: `echo "version=9.9.9" >> "$SHIP_OUTPUT"`},
		}},
		{ID: "deploy", Needs: []string{"build"},
			If: "outputs.build.version == '9.9.9'",
			Steps: []compiler.StepPlan{
				{ID: "go", Run: `echo "$OUTPUTS_BUILD_VERSION" > deployed.txt`},
			}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(work, "deployed.txt"))
	if err != nil {
		t.Fatal("deploy should have run (if matched via upstream output)")
	}
	if strings.TrimSpace(string(b)) != "9.9.9" {
		t.Fatalf("upstream output env wrong: %q", b)
	}
}

func TestNeedsResultInIf(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "true"}}},
		{ID: "b", Needs: []string{"a"}, If: "needs.a.result == 'success'",
			Steps: []compiler.StepPlan{{ID: "s", Run: `echo ok > b.txt`}}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(work, "b.txt")); err != nil {
		t.Error("b should run when needs.a.result == success")
	}
}

func TestOutputFileContainerPathAndSanitize(t *testing.T) {
	s := &scheduler{opts: Options{Workdir: t.TempDir()}}
	// container job with a special-char id → sanitized filename + container path
	host, env := s.outputFile(&compiler.JobPlan{ID: "build/x-1", Image: "img"})
	if !strings.HasPrefix(env, "/ship/work/") {
		t.Errorf("container output env should be a /ship/work path, got %q", env)
	}
	if strings.Contains(filepath.Base(host), "/") {
		t.Errorf("host filename should be sanitized, got %q", host)
	}
	// native job → env equals host path
	host2, env2 := s.outputFile(&compiler.JobPlan{ID: "native"})
	if host2 != env2 {
		t.Errorf("native output path should equal env, got %q vs %q", host2, env2)
	}
}

func TestInjectUpstreamOutputsEnvKey(t *testing.T) {
	s := &scheduler{jobOut: map[string]map[string]string{
		"build-step": {"ver-tag": "1.0"},
	}}
	env := map[string]string{}
	s.injectUpstreamOutputs(&compiler.JobPlan{ID: "d", Needs: []string{"build-step"}}, env)
	// key uppercased, non-alnum → underscore
	if env["OUTPUTS_BUILD_STEP_VER_TAG"] != "1.0" {
		t.Fatalf("upstream output env key wrong: %+v", env)
	}
}

func TestIfVarsAndFailureContext(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{Name: "T", Vars: map[string]string{"TIER": "prod"}, Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "exit 1"}}, ContinueOnError: true},
		// vars.TIER reference + failure() after a's tolerated failure
		{ID: "b", Needs: []string{"a"}, If: "vars.TIER == 'prod'",
			Steps: []compiler.StepPlan{{ID: "s", Run: `echo ok > b.txt`}}},
	}}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("continue-on-error job should not fail run: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(work, "b.txt")); err != nil {
		t.Error("b should run (vars.TIER == prod)")
	}
}

func TestStepIfInvalidFailsJob(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "true", If: "@bad"}}},
	}}
	res := Run(context.Background(), p, Options{Workdir: t.TempDir(), NoCache: true})
	if !res.Failed {
		t.Fatal("invalid step if should fail the job")
	}
}
