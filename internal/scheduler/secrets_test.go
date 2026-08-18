package scheduler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/logs"
	"github.com/chris/shiphappens/internal/secrets"
)

func TestWorkflowVarsReachSteps(t *testing.T) {
	work := t.TempDir()
	out := filepath.Join(work, "out.txt")
	p := &compiler.RunPlan{
		Name: "T",
		Vars: map[string]string{"GREETING": "hello-var"},
		Jobs: []compiler.JobPlan{
			{ID: "a", Steps: []compiler.StepPlan{{ID: "s", Run: "echo $GREETING > out.txt"}}},
		},
	}
	res := Run(context.Background(), p, Options{Workdir: work, NoCache: true})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "hello-var") {
		t.Fatalf("workflow var not visible to step: %q", b)
	}
}

func TestJobEnvOverridesVar(t *testing.T) {
	work := t.TempDir()
	p := &compiler.RunPlan{
		Name: "T",
		Vars: map[string]string{"K": "from-var"},
		Jobs: []compiler.JobPlan{
			{ID: "a", Env: map[string]string{"K": "from-job"},
				Steps: []compiler.StepPlan{{ID: "s", Run: "test \"$K\" = from-job"}}},
		},
	}
	if res := Run(context.Background(), p, Options{Workdir: work, NoCache: true}); res.Failed {
		t.Fatal("job env should override workflow var")
	}
}

func TestMissingSecretFailsFast(t *testing.T) {
	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Secrets: []compiler.SecretRef{{Name: "NO_SUCH_SECRET_XYZ"}},
			Steps: []compiler.StepPlan{{ID: "s", Run: "echo should-not-run"}}},
	}}
	res := Run(context.Background(), p, Options{
		Workdir:  t.TempDir(),
		NoCache:  true,
		Resolver: secrets.NewWith(func(string) (string, bool) { return "", false }),
	})
	if !res.Failed {
		t.Fatal("missing required secret should fail the job")
	}
	if res.Ran != 0 {
		t.Fatalf("no step should have run, ran=%d", res.Ran)
	}
}

func TestSecretResolvedAndMaskedInLogs(t *testing.T) {
	buf := &syncBuf{b: &bytes.Buffer{}}
	prev := logs.SetOutput(buf)
	defer logs.SetOutput(prev)

	p := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "a", Secrets: []compiler.SecretRef{{Name: "API_TOKEN"}},
			Steps: []compiler.StepPlan{{ID: "s", Run: "echo token-is-$API_TOKEN"}}},
	}}
	res := Run(context.Background(), p, Options{
		Workdir: t.TempDir(),
		NoCache: true,
		Resolver: secrets.NewWith(func(k string) (string, bool) {
			if k == "API_TOKEN" {
				return "supersecretvalue", true
			}
			return "", false
		}),
	})
	if res.Failed {
		t.Fatalf("run failed: %+v", res)
	}
	time.Sleep(50 * time.Millisecond) // let the async log pipe flush
	got := buf.String()
	if strings.Contains(got, "supersecretvalue") {
		t.Fatalf("secret leaked into logs: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("expected masked marker in logs: %q", got)
	}
}

// syncBuf makes bytes.Buffer safe for the Prefixed goroutine.
type syncBuf struct {
	mu sync.Mutex
	b  *bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
