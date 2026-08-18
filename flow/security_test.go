package flow

import "testing"

func TestLowerSecurityAndAllow(t *testing.T) {
	wf := New("W").OfflineByDefault().AllowHosts("registry.npmjs.org")
	wf.Job("build").Allow("github.com").Run("s", "x")

	p := wf.ToPlan()
	if p.Security == nil || !p.Security.OfflineByDefault {
		t.Fatal("offline-by-default not lowered")
	}
	if len(p.Security.DefaultAllow) != 1 || p.Security.DefaultAllow[0] != "registry.npmjs.org" {
		t.Fatalf("default allow not lowered: %+v", p.Security)
	}
	if len(p.Jobs[0].Allow) != 1 || p.Jobs[0].Allow[0] != "github.com" {
		t.Fatalf("job allow not lowered: %+v", p.Jobs[0].Allow)
	}
}

func TestLowerNoSecurityPolicy(t *testing.T) {
	wf := New("W")
	wf.Job("a").Run("s", "x")
	if wf.ToPlan().Security != nil {
		t.Fatal("no policy should leave Security nil")
	}
}

func TestLowerIfAndStepIf(t *testing.T) {
	wf := New("W")
	wf.Job("j").If("env.BRANCH == 'main'").
		Run("s", "x").StepIf("outputs.self.mode == 'fast'")
	j := wf.ToPlan().Jobs[0]
	if j.If != "env.BRANCH == 'main'" {
		t.Fatalf("job if not lowered: %q", j.If)
	}
	if j.Steps[0].If != "outputs.self.mode == 'fast'" {
		t.Fatalf("step if not lowered: %q", j.Steps[0].If)
	}
}

func TestLowerService(t *testing.T) {
	wf := New("W")
	wf.Job("test").Image("golang:1.22").
		Service(Service{
			Name:   "db",
			Image:  "postgres:16",
			Env:    map[string]string{"POSTGRES_PASSWORD": "x"},
			Ports:  []string{"5432:5432"},
			Health: "pg_isready", Timeout: 10,
		}).
		Run("s", "go test ./...")

	svcs := wf.ToPlan().Jobs[0].Services
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	sv := svcs[0]
	if sv.Name != "db" || sv.Image != "postgres:16" || sv.Health != "pg_isready" || sv.Timeout != 10 {
		t.Fatalf("service not lowered: %+v", sv)
	}
	if sv.Env["POSTGRES_PASSWORD"] != "x" || len(sv.Ports) != 1 {
		t.Fatalf("service env/ports wrong: %+v", sv)
	}
}

func TestSanitizeHelpers(t *testing.T) {
	if Sanitize("$(evil)") != "(evil)" {
		t.Errorf("Sanitize wrong: %q", Sanitize("$(evil)"))
	}
	if !SafeIdentifier("feature/x-1") || SafeIdentifier("a b") {
		t.Error("SafeIdentifier wrong")
	}
}

func TestIfStepIfNoStepNoop(t *testing.T) {
	wf := New("W")
	// StepIf before any Run is a safe no-op
	wf.Job("j").StepIf("x").Run("s", "cmd")
	if wf.ToPlan().Jobs[0].Steps[0].If != "" {
		t.Error("StepIf before Run should not attach to later step")
	}
}

func TestLowerStepNeedsAndOnFailure(t *testing.T) {
	wf := New("W")
	wf.Job("j").
		Run("setup", "s1").
		Run("compile", "s2").
		Run("smoke", "s3").StepNeeds("setup", "compile").
		OnFailure(Handler("report", "echo fail"), Handler("cleanup", "echo clean"))
	j := wf.ToPlan().Jobs[0]
	smoke := j.Steps[2]
	if len(smoke.Needs) != 2 || smoke.Needs[0] != "setup" {
		t.Fatalf("step needs not lowered: %+v", smoke.Needs)
	}
	if len(smoke.OnFailure) != 2 || smoke.OnFailure[0].ID != "report" || smoke.OnFailure[1].Run != "echo clean" {
		t.Fatalf("onFailure not lowered: %+v", smoke.OnFailure)
	}
}

func TestStepNeedsOnFailureNoStepNoop(t *testing.T) {
	wf := New("W")
	wf.Job("j").StepNeeds("x").OnFailure(Handler("h", "c")).Run("s", "cmd")
	st := wf.ToPlan().Jobs[0].Steps[0]
	if len(st.Needs) != 0 || len(st.OnFailure) != 0 {
		t.Error("step-graph options before Run should not attach to later step")
	}
}

func TestLowerToolchain(t *testing.T) {
	wf := New("W").Tool("go", "1.22.5").Toolchain(map[string]string{"node": "20.11.0"})
	wf.Job("build").Tool("python", "3.12.1").Run("s", "x")

	p := wf.ToPlan()
	if p.Toolchain["go"] != "1.22.5" || p.Toolchain["node"] != "20.11.0" {
		t.Fatalf("workflow toolchain not lowered: %+v", p.Toolchain)
	}
	if p.Jobs[0].Toolchain["python"] != "3.12.1" {
		t.Fatalf("job toolchain not lowered: %+v", p.Jobs[0].Toolchain)
	}
}
