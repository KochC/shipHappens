package scheduler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KochC/shipHappens/internal/compiler"
	"github.com/KochC/shipHappens/internal/runner"
)

func TestTailWriterKeepsLastLines(t *testing.T) {
	var sink bytes.Buffer
	tw := newTailWriter(&sink, 3)
	for _, l := range []string{"a", "b", "c", "d", "e"} {
		tw.Write([]byte(l + "\n"))
	}
	got := tw.Tail()
	want := []string{"c", "d", "e"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tail = %v, want %v", got, want)
	}
	// Everything is still teed to the underlying writer.
	if sink.String() != "a\nb\nc\nd\ne\n" {
		t.Errorf("tee = %q", sink.String())
	}
}

func TestTailWriterPartialLine(t *testing.T) {
	tw := newTailWriter(nil, 5)
	tw.Write([]byte("done\nboom: no newline"))
	got := tw.Tail()
	if len(got) != 2 || got[1] != "boom: no newline" {
		t.Errorf("partial not captured: %v", got)
	}
}

func TestTailWriterDefaultsAndNilSink(t *testing.T) {
	tw := newTailWriter(nil, 0) // 0 → default
	n, err := tw.Write([]byte("x\n"))
	if err != nil || n != 2 {
		t.Fatalf("write: %d %v", n, err)
	}
}

func TestClassifyFailure(t *testing.T) {
	// timeout via context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, c2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer c2()
	if classifyFailure(deadlineCtx, runner.StepResult{}, nil) != FailTimeout {
		t.Error("deadline ctx → timeout")
	}
	// timeout via err text
	if classifyFailure(context.Background(), runner.StepResult{Err: context.DeadlineExceeded}, nil) != FailTimeout {
		t.Error("deadline err → timeout")
	}
	// egress via tail
	if classifyFailure(context.Background(), runner.StepResult{}, []string{"egress blocked by allow-list: evil.test"}) != FailEgress {
		t.Error("egress tail → egress")
	}
	// default exit
	if classifyFailure(context.Background(), runner.StepResult{ExitCode: 1}, []string{"oops"}) != FailExit {
		t.Error("default → exit")
	}
	_ = ctx
}

func TestFailKindStringAndMark(t *testing.T) {
	cases := map[FailKind][2]string{
		FailExit:       {"exit", "✗"},
		FailTimeout:    {"timeout", "⏱"},
		FailEgress:     {"egress", "⛔"},
		FailSecret:     {"secret", "🔒"},
		FailDependency: {"dependency", "◌"},
		FailSetup:      {"setup", "✗"},
		FailNone:       {"none", "✗"},
	}
	for k, want := range cases {
		if k.String() != want[0] {
			t.Errorf("%d.String()=%q want %q", k, k.String(), want[0])
		}
		if k.Mark() != want[1] {
			t.Errorf("%d.Mark()=%q want %q", k, k.Mark(), want[1])
		}
	}
}

func TestSanitizeLogName(t *testing.T) {
	if sanitizeLogName("test/1.22-linux") != "test_1.22-linux" {
		t.Errorf("got %q", sanitizeLogName("test/1.22-linux"))
	}
	if sanitizeLogName("ok_name-1") != "ok_name-1" {
		t.Error("valid chars should pass through")
	}
}

func TestMaskWriter(t *testing.T) {
	var buf bytes.Buffer
	mw := maskWriter{w: &buf, mask: func(s string) string { return strings.ReplaceAll(s, "sekret", "***") }}
	mw.Write([]byte("token=sekret\n"))
	if buf.String() != "token=***\n" {
		t.Errorf("mask not applied: %q", buf.String())
	}
	// nil mask passes through
	var buf2 bytes.Buffer
	mw2 := maskWriter{w: &buf2}
	mw2.Write([]byte("raw"))
	if buf2.String() != "raw" {
		t.Error("nil mask should pass through")
	}
}

func TestJobLogFilePersistsOutput(t *testing.T) {
	dir := t.TempDir()
	plan := &compiler.RunPlan{Name: "L", Jobs: []compiler.JobPlan{
		{ID: "a", RunsOn: "native", Steps: []compiler.StepPlan{{ID: "s", Run: "echo hello-log"}}},
	}}
	res := Run(context.Background(), plan, Options{Workdir: dir, LogDir: dir})
	if res.Failed {
		t.Fatal("run should pass")
	}
	b, err := os.ReadFile(filepath.Join(dir, "a.log"))
	if err != nil {
		t.Fatalf("log not written: %v", err)
	}
	if !strings.Contains(string(b), "hello-log") {
		t.Errorf("log missing output: %q", b)
	}
}

func TestStepFailureEmitsDetail(t *testing.T) {
	dir := t.TempDir()
	plan := &compiler.RunPlan{Name: "F", Jobs: []compiler.JobPlan{
		{ID: "bad", RunsOn: "native", Steps: []compiler.StepPlan{
			{ID: "s", Run: "echo 'boom: it broke' && exit 3"},
		}},
	}}
	var failEv *Event
	Run(context.Background(), plan, Options{Workdir: dir, Observer: func(e Event) {
		if e.Kind == JobFinished && !e.OK {
			ev := e
			failEv = &ev
		}
	}})
	if failEv == nil {
		t.Fatal("no failing JobFinished event")
	}
	if failEv.FailKind != FailExit {
		t.Errorf("kind = %v, want exit", failEv.FailKind)
	}
	if failEv.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", failEv.ExitCode)
	}
	joined := strings.Join(failEv.Tail, "\n")
	if !strings.Contains(joined, "boom: it broke") {
		t.Errorf("tail missing error: %v", failEv.Tail)
	}
}

func TestStepTimeoutClassified(t *testing.T) {
	dir := t.TempDir()
	plan := &compiler.RunPlan{Name: "T", Jobs: []compiler.JobPlan{
		{ID: "slow", RunsOn: "native", Steps: []compiler.StepPlan{
			{ID: "s", Run: "sleep 5", TimeoutSec: 1},
		}},
	}}
	var kind FailKind
	Run(context.Background(), plan, Options{Workdir: dir, Observer: func(e Event) {
		if e.Kind == StepFinished && !e.OK {
			kind = e.FailKind
		}
	}})
	if kind != FailTimeout {
		t.Errorf("expected timeout classification, got %v", kind)
	}
}
