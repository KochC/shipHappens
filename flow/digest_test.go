package flow

import (
	"strings"
	"testing"

	"github.com/KochC/shipHappens/internal/logs"
	"github.com/KochC/shipHappens/internal/scheduler"
)

func TestPrintFailureDigest(t *testing.T) {
	var buf strings.Builder
	prev := logs.SetOutput(&buf)
	defer logs.SetOutput(prev)

	// Empty → no output.
	printFailureDigest(nil)
	if buf.Len() != 0 {
		t.Errorf("empty failures should print nothing, got %q", buf.String())
	}

	printFailureDigest([]scheduler.Event{
		{Kind: scheduler.JobFinished, Job: "build", OK: false, FailKind: scheduler.FailExit, ExitCode: 2, Step: "compile", Tail: []string{"error: boom", "line 2"}},
		{Kind: scheduler.JobFinished, Job: "slow", OK: false, FailKind: scheduler.FailTimeout},
		{Kind: scheduler.JobFinished, Job: "net", OK: false, FailKind: scheduler.FailEgress},
		{Kind: scheduler.JobFinished, Job: "sec", OK: false, FailKind: scheduler.FailSecret},
		{Kind: scheduler.JobFinished, Job: "dep", OK: false, FailKind: scheduler.FailDependency},
	})
	out := buf.String()
	for _, want := range []string{
		"5 failed job(s)", "build", "exit 2", "step: compile", "error: boom",
		"timeout", "egress blocked", "missing secret", "dependency failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("digest missing %q in:\n%s", want, out)
		}
	}
}

func TestLastN(t *testing.T) {
	if got := lastN([]string{"a", "b", "c"}, 2); strings.Join(got, ",") != "b,c" {
		t.Errorf("lastN trim: %v", got)
	}
	if got := lastN([]string{"a"}, 5); strings.Join(got, ",") != "a" {
		t.Errorf("lastN short: %v", got)
	}
}
