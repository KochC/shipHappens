package flow

import "testing"

func TestLowerBasicPlan(t *testing.T) {
	wf := New("W")
	a := wf.Job("a").Run("s1", "echo a")
	wf.Job("b").Needs(a).Run("s2", "echo b")

	p := wf.ToPlan()
	if p.Name != "W" || len(p.Jobs) != 2 {
		t.Fatalf("unexpected plan: %+v", p)
	}
	if p.Jobs[0].ID != "a" || p.Jobs[1].ID != "b" {
		t.Fatalf("job order/ids wrong: %+v", p.Jobs)
	}
	if len(p.Jobs[1].Needs) != 1 || p.Jobs[1].Needs[0] != "a" {
		t.Fatalf("needs not lowered: %+v", p.Jobs[1].Needs)
	}
	if p.Jobs[0].RunsOn != "native" {
		t.Fatalf("default runs-on should be native, got %q", p.Jobs[0].RunsOn)
	}
}

func TestLowerImageSetsContainer(t *testing.T) {
	wf := New("W")
	wf.Job("j").Image("alpine:3.20").Run("s", "true")
	j := wf.ToPlan().Jobs[0]
	if j.Image != "alpine:3.20" || j.RunsOn != "container" {
		t.Fatalf("image/runs-on wrong: image=%q runs-on=%q", j.Image, j.RunsOn)
	}
}

func TestLowerNetworkOfflineAndOn(t *testing.T) {
	wf := New("W")
	wf.Job("off").Image("i").Offline().Run("s", "x")
	wf.Job("on").Image("i").Network(true).Run("s", "x")
	p := wf.ToPlan()
	if p.Jobs[0].Network == nil || *p.Jobs[0].Network != false {
		t.Fatal("Offline should lower Network=false")
	}
	if p.Jobs[1].Network == nil || *p.Jobs[1].Network != true {
		t.Fatal("Network(true) should lower Network=true")
	}
}

func TestLowerOutputsCleanAfterOverlay(t *testing.T) {
	wf := New("W")
	wf.Job("j").Image("i").Overlay().
		Run("s", "build").
		Outputs("dist/**").
		CleanAfter(".pio/build")
	j := wf.ToPlan().Jobs[0]
	if !j.Overlay {
		t.Error("Overlay not lowered")
	}
	if len(j.Outputs) != 1 || j.Outputs[0] != "dist/**" {
		t.Errorf("Outputs not lowered: %+v", j.Outputs)
	}
	if len(j.CleanAfter) != 1 || j.CleanAfter[0] != ".pio/build" {
		t.Errorf("CleanAfter not lowered: %+v", j.CleanAfter)
	}
}

func TestLowerStepCacheHints(t *testing.T) {
	wf := New("W")
	wf.Job("j").Run("s", "make").Cache(Inputs("src/**"), Outputs("bin/**"))
	step := wf.ToPlan().Jobs[0].Steps[0]
	if step.Cache == nil {
		t.Fatal("cache spec not lowered")
	}
	if len(step.Cache.Inputs) != 1 || step.Cache.Inputs[0] != "src/**" {
		t.Errorf("inputs wrong: %+v", step.Cache.Inputs)
	}
	if len(step.Cache.Outputs) != 1 || step.Cache.Outputs[0] != "bin/**" {
		t.Errorf("outputs wrong: %+v", step.Cache.Outputs)
	}
}

func TestLowerEnvAndRunsOn(t *testing.T) {
	wf := New("W")
	wf.Job("j").RunsOn("linux").Env("K", "V").Run("s", "x")
	j := wf.ToPlan().Jobs[0]
	if j.RunsOn != "linux" || j.Env["K"] != "V" {
		t.Fatalf("runs-on/env wrong: %+v", j)
	}
}

func TestLinesCapturesLocations(t *testing.T) {
	wf := New("W")
	wf.Job("a").Run("s", "x")
	lines := wf.Lines()
	if loc, ok := lines["a"]; !ok || loc == "" || loc == "?" {
		t.Fatalf("expected a source location for job a, got %q", loc)
	}
}

func TestPreheatRegistration(t *testing.T) {
	wf := New("W")
	wf.Preheat(Preheat{Image: "img", Warm: "prime"})
	if len(wf.Preheats()) != 1 || wf.Preheats()[0].Image != "img" {
		t.Fatalf("preheat not registered: %+v", wf.Preheats())
	}
}

func TestCompileValidAndInvalid(t *testing.T) {
	ok := New("W")
	ok.Job("a").Run("s", "x")
	if _, err := compile(ok.ToPlan(), ok.Lines()); err != nil {
		t.Fatalf("valid workflow should compile: %v", err)
	}

	bad := New("W")
	x := bad.Job("x").Run("s", "e")
	y := bad.Job("y").Run("s", "e")
	x.Needs(y)
	y.Needs(x) // cycle
	if _, err := compile(bad.ToPlan(), bad.Lines()); err == nil {
		t.Fatal("cyclic workflow should fail to compile")
	} else if _, isCE := err.(*CompileError); !isCE {
		t.Fatalf("expected *CompileError, got %T", err)
	}
}
