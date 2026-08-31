package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KochC/shipHappens/internal/scheduler"
)

// stubFailRun makes the manager run a fake pipeline that fails a job with rich
// failure detail, and persists a log the JobLog reader can find.
func TestShipStatusReportsFailureDetail(t *testing.T) {
	s := NewServer()
	// Redirect run logs to a temp dir.
	prev := runsRoot
	dir := t.TempDir()
	runsRoot = func() string { return dir }
	defer func() { runsRoot = prev }()

	s.mgr.runFn = func(ctx context.Context, file, logDir string, obs func(scheduler.Event)) (scheduler.Result, error) {
		obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "build"})
		obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "build", OK: true})
		obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "test"})
		obs(scheduler.Event{
			Kind: scheduler.JobFinished, Job: "test", OK: false,
			ExitCode: 3, FailKind: scheduler.FailExit, ErrMsg: "exited with code 3",
			Step: "unit", Tail: []string{"assert failed", "boom"},
		})
		return scheduler.Result{Ran: 2, Failed: true}, nil
	}
	f := writeTempPlan(t, `{"name":"R","jobs":[{"id":"build","runsOn":"native","steps":[{"id":"s","run":"true"}]}]}`)
	r := call(t, s, "tools/call", map[string]any{"name": "ship_run", "arguments": map[string]any{"file": f}})
	var started map[string]any
	json.Unmarshal([]byte(toolResultText(t, r)), &started)
	id := started["runId"].(string)

	// Poll to completion.
	var txt string
	for i := 0; i < 100; i++ {
		rr := call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": id}})
		txt = toolResultText(t, rr)
		if strings.Contains(txt, `"state": "failed"`) {
			break
		}
	}
	if !strings.Contains(txt, `"failure"`) {
		t.Fatalf("status missing failure block: %s", txt)
	}
	for _, want := range []string{`"kind": "exit"`, `"exitCode": 3`, `"step": "unit"`, "assert failed"} {
		if !strings.Contains(txt, want) {
			t.Errorf("status missing %q in:\n%s", want, txt)
		}
	}
}

func TestShipLogsReadsPersistedLog(t *testing.T) {
	s := NewServer()
	prev := runsRoot
	dir := t.TempDir()
	runsRoot = func() string { return dir }
	defer func() { runsRoot = prev }()

	// runFn writes a log file into the per-run logDir it is handed.
	s.mgr.runFn = func(ctx context.Context, file, logDir string, obs func(scheduler.Event)) (scheduler.Result, error) {
		obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "job1"})
		if err := writeLog(logDir, "job1", "line one\nthe error line\n"); err != nil {
			t.Errorf("write log: %v", err)
		}
		obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "job1", OK: true})
		return scheduler.Result{Ran: 1}, nil
	}
	f := writeTempPlan(t, `{"name":"R","jobs":[{"id":"job1","runsOn":"native","steps":[{"id":"s","run":"true"}]}]}`)
	r := call(t, s, "tools/call", map[string]any{"name": "ship_run", "arguments": map[string]any{"file": f}})
	var started map[string]any
	json.Unmarshal([]byte(toolResultText(t, r)), &started)
	id := started["runId"].(string)

	// Wait for the run goroutine to finish writing.
	for i := 0; i < 100; i++ {
		rr := call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": id}})
		if strings.Contains(toolResultText(t, rr), `"state": "passed"`) {
			break
		}
	}

	// ship_logs returns the persisted content.
	lr := call(t, s, "tools/call", map[string]any{"name": "ship_logs", "arguments": map[string]any{"runId": id, "job": "job1"}})
	if !strings.Contains(toolResultText(t, lr), "the error line") {
		t.Errorf("ship_logs missing content: %s", toolResultText(t, lr))
	}

	// Unknown job → error.
	er := call(t, s, "tools/call", map[string]any{"name": "ship_logs", "arguments": map[string]any{"runId": id, "job": "ghost"}})
	if !strings.Contains(strings.ToLower(toolResultText(t, er)), "no log") {
		t.Errorf("expected no-log error, got: %s", toolResultText(t, er))
	}

	// Unknown run → error.
	ur := call(t, s, "tools/call", map[string]any{"name": "ship_logs", "arguments": map[string]any{"runId": "nope", "job": "x"}})
	if !strings.Contains(strings.ToLower(toolResultText(t, ur)), "unknown runid") {
		t.Errorf("expected unknown-runId error, got: %s", toolResultText(t, ur))
	}
}

// writeLog is a test helper that writes a job log the way the scheduler would.
func writeLog(dir, job, content string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, job+".log"), []byte(content), 0o644)
}
