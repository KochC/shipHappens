package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chris/shiphappens/internal/scheduler"
)

// call drives a single JSON-RPC request through the server and returns the
// decoded response.
func call(t *testing.T, s *Server, method string, params any) rpcResponse {
	t.Helper()
	pb, _ := json.Marshal(params)
	req := rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method, Params: pb}
	line, _ := json.Marshal(req)
	var out strings.Builder
	if err := s.Serve(strings.NewReader(string(line)+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode response %q: %v", out.String(), err)
	}
	return resp
}

// toolResultText extracts the text content from a tools/call result.
func toolResultText(t *testing.T, resp rpcResponse) string {
	t.Helper()
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result not a map: %#v", resp.Result)
	}
	content := m["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}

func TestInitializeAndToolsList(t *testing.T) {
	s := NewServer()
	r := call(t, s, "initialize", map[string]any{})
	if r.Error != nil {
		t.Fatalf("initialize error: %+v", r.Error)
	}
	res := r.Result.(map[string]any)
	if res["protocolVersion"] != protocolVersion {
		t.Errorf("protocol version: %v", res["protocolVersion"])
	}

	r = call(t, s, "tools/list", nil)
	tools := r.Result.(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"ship_validate", "ship_graph", "ship_run", "ship_status", "ship_cancel", "ship_runs"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestUnknownMethodAndNotification(t *testing.T) {
	s := NewServer()
	r := call(t, s, "no.such.method", nil)
	if r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("expected method-not-found, got %+v", r.Error)
	}
	// notification (no id) → no response line
	var out strings.Builder
	_ = s.Serve(strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"), &out)
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("notification should produce no response, got %q", out.String())
	}
}

func TestParseErrorAndBlankLines(t *testing.T) {
	s := NewServer()
	var out strings.Builder
	_ = s.Serve(strings.NewReader("\n{not json\n"), &out)
	if !strings.Contains(out.String(), "parse error") {
		t.Errorf("expected parse error, got %q", out.String())
	}
}

func TestValidateTool(t *testing.T) {
	s := NewServer()
	// stub the planfile via a temp valid JSON plan
	f := writeTempPlan(t, `{"name":"T","jobs":[{"id":"a","runsOn":"native","steps":[{"id":"s","run":"true"}]}]}`)
	r := call(t, s, "tools/call", map[string]any{"name": "ship_validate", "arguments": map[string]any{"file": f}})
	txt := toolResultText(t, r)
	if !strings.Contains(txt, `"valid": true`) {
		t.Errorf("expected valid, got %s", txt)
	}

	// invalid: a cycle
	bad := writeTempPlan(t, `{"name":"T","jobs":[
	  {"id":"x","runsOn":"native","needs":["y"],"steps":[{"id":"s","run":"e"}]},
	  {"id":"y","runsOn":"native","needs":["x"],"steps":[{"id":"s","run":"e"}]}]}`)
	r = call(t, s, "tools/call", map[string]any{"name": "ship_validate", "arguments": map[string]any{"file": bad}})
	if !strings.Contains(toolResultText(t, r), `"valid": false`) {
		t.Error("cycle should be invalid")
	}

	// load error (missing file)
	r = call(t, s, "tools/call", map[string]any{"name": "ship_validate", "arguments": map[string]any{"file": "/no/such"}})
	if !strings.Contains(toolResultText(t, r), "invalid:") {
		t.Error("missing file should report invalid")
	}
}

func TestGraphTool(t *testing.T) {
	s := NewServer()
	f := writeTempPlan(t, `{"name":"G","jobs":[
	  {"id":"a","runsOn":"native","steps":[{"id":"s","run":"true"}]},
	  {"id":"b","runsOn":"native","needs":["a"],"steps":[{"id":"s","run":"true"}]}]}`)
	r := call(t, s, "tools/call", map[string]any{"name": "ship_graph", "arguments": map[string]any{"file": f}})
	txt := toolResultText(t, r)
	if !strings.Contains(txt, `"id": "b"`) || !strings.Contains(txt, `"a"`) {
		t.Errorf("graph missing jobs/needs: %s", txt)
	}
}

func TestRunStatusCancelFlow(t *testing.T) {
	s := NewServer()
	// deterministic in-process run: stub the run function.
	done := make(chan struct{})
	s.mgr.runFn = func(ctx context.Context, file string, obs func(scheduler.Event)) (scheduler.Result, error) {
		obs(scheduler.Event{Kind: scheduler.JobStarted, Job: "a"})
		obs(scheduler.Event{Kind: scheduler.StepStarted, Job: "a", Step: "s"})
		<-done // block until the test lets it finish
		obs(scheduler.Event{Kind: scheduler.JobFinished, Job: "a", OK: true})
		return scheduler.Result{Ran: 1}, nil
	}
	f := writeTempPlan(t, `{"name":"R","jobs":[{"id":"a","runsOn":"native","steps":[{"id":"s","run":"true"}]}]}`)

	r := call(t, s, "tools/call", map[string]any{"name": "ship_run", "arguments": map[string]any{"file": f}})
	var started map[string]any
	json.Unmarshal([]byte(toolResultText(t, r)), &started)
	id := started["runId"].(string)

	// poll while running
	time.Sleep(20 * time.Millisecond)
	r = call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": id}})
	if !strings.Contains(toolResultText(t, r), `"state": "running"`) {
		t.Errorf("expected running: %s", toolResultText(t, r))
	}

	// runs list
	r = call(t, s, "tools/call", map[string]any{"name": "ship_runs", "arguments": map[string]any{}})
	if !strings.Contains(toolResultText(t, r), id) {
		t.Error("run should be listed")
	}

	// let it finish, then poll again
	close(done)
	time.Sleep(30 * time.Millisecond)
	r = call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": id}})
	if !strings.Contains(toolResultText(t, r), `"state": "passed"`) {
		t.Errorf("expected passed: %s", toolResultText(t, r))
	}
}

func TestStatusUnknownRun(t *testing.T) {
	s := NewServer()
	r := call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": "nope"}})
	if !strings.Contains(toolResultText(t, r), "error:") {
		t.Error("unknown run should error")
	}
}

func TestCancelTool(t *testing.T) {
	s := NewServer()
	block := make(chan struct{})
	s.mgr.runFn = func(ctx context.Context, _ string, _ func(scheduler.Event)) (scheduler.Result, error) {
		select {
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		case <-block:
			return scheduler.Result{}, nil
		}
	}
	f := writeTempPlan(t, `{"name":"C","jobs":[{"id":"a","runsOn":"native","steps":[{"id":"s","run":"true"}]}]}`)
	r := call(t, s, "tools/call", map[string]any{"name": "ship_run", "arguments": map[string]any{"file": f}})
	var st map[string]any
	json.Unmarshal([]byte(toolResultText(t, r)), &st)
	id := st["runId"].(string)

	r = call(t, s, "tools/call", map[string]any{"name": "ship_cancel", "arguments": map[string]any{"runId": id}})
	if !strings.Contains(toolResultText(t, r), "canceled") {
		t.Errorf("cancel result: %s", toolResultText(t, r))
	}
	time.Sleep(30 * time.Millisecond)
	r = call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": id}})
	if !strings.Contains(toolResultText(t, r), `"state": "canceled"`) {
		t.Errorf("expected canceled: %s", toolResultText(t, r))
	}
	close(block)

	// cancel unknown
	r = call(t, s, "tools/call", map[string]any{"name": "ship_cancel", "arguments": map[string]any{"runId": "nope"}})
	if !strings.Contains(toolResultText(t, r), "error:") {
		t.Error("cancel unknown should error")
	}
}

func TestUnknownToolAndBadParams(t *testing.T) {
	s := NewServer()
	r := call(t, s, "tools/call", map[string]any{"name": "nope", "arguments": map[string]any{}})
	if r.Error == nil {
		t.Error("unknown tool should be an rpc error")
	}
	// bad params (params not an object)
	req := rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: json.RawMessage(`"x"`)}
	line, _ := json.Marshal(req)
	var out strings.Builder
	_ = s.Serve(strings.NewReader(string(line)+"\n"), &out)
	if !strings.Contains(out.String(), "invalid params") {
		t.Errorf("expected invalid params, got %s", out.String())
	}
}

func TestCacheDuTool(t *testing.T) {
	s := NewServer()
	prev := shipCLI
	shipCLI = func(args ...string) (string, error) { return "cache: ok\nsize: 1.0 KiB", nil }
	defer func() { shipCLI = prev }()
	r := call(t, s, "tools/call", map[string]any{"name": "ship_cache_du", "arguments": map[string]any{}})
	if !strings.Contains(toolResultText(t, r), "cache: ok") {
		t.Errorf("cache du: %s", toolResultText(t, r))
	}
	// error path
	shipCLI = func(args ...string) (string, error) { return "", errors.New("no ship") }
	r = call(t, s, "tools/call", map[string]any{"name": "ship_cache_du", "arguments": map[string]any{}})
	if !strings.Contains(toolResultText(t, r), "error:") {
		t.Error("cache du error should surface")
	}
}

func TestPing(t *testing.T) {
	if call(t, NewServer(), "ping", nil).Error != nil {
		t.Error("ping should succeed")
	}
}

func TestStrArg(t *testing.T) {
	if strArg(map[string]any{"k": "v"}, "k") != "v" || strArg(map[string]any{}, "k") != "" || strArg(map[string]any{"k": 1}, "k") != "" {
		t.Error("strArg wrong")
	}
}

func TestDefaultRunFnEndToEnd(t *testing.T) {
	// Real scheduler path via a native pipeline (no stub).
	s := NewServer()
	f := writeTempPlan(t, `{"name":"E2E","jobs":[
	  {"id":"a","runsOn":"native","steps":[{"id":"s","run":"true"}]},
	  {"id":"skip","runsOn":"native","if":"false","steps":[{"id":"s","run":"true"}]}]}`)
	r := call(t, s, "tools/call", map[string]any{"name": "ship_run", "arguments": map[string]any{"file": f}})
	var st map[string]any
	json.Unmarshal([]byte(toolResultText(t, r)), &st)
	id := st["runId"].(string)

	// wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r = call(t, s, "tools/call", map[string]any{"name": "ship_status", "arguments": map[string]any{"runId": id}})
		if strings.Contains(toolResultText(t, r), `"state": "passed"`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	txt := toolResultText(t, r)
	if !strings.Contains(txt, `"state": "passed"`) {
		t.Fatalf("run should pass: %s", txt)
	}
	// the skip branch of observe should have marked "skipped"
	if !strings.Contains(txt, `"skipped"`) {
		t.Errorf("expected a skipped job in status: %s", txt)
	}
}

func TestObserveDynamicJobAndFailure(t *testing.T) {
	r := &Run{jobs: map[string]*jobState{}, started: time.Now()}
	// unknown job → registered
	r.observe(scheduler.Event{Kind: scheduler.JobStarted, Job: "dyn"})
	if r.jobs["dyn"].Status != "running" {
		t.Fatal("dynamic job not registered/started")
	}
	r.observe(scheduler.Event{Kind: scheduler.JobFinished, Job: "dyn", OK: false})
	if r.jobs["dyn"].Status != "failed" {
		t.Fatal("failed status not set")
	}
	r.observe(scheduler.Event{Kind: scheduler.JobSkipped, Job: "sk"})
	if r.jobs["sk"].Status != "skipped" {
		t.Fatal("skipped status not set")
	}
}

func TestRunLoadError(t *testing.T) {
	// defaultRunFn with a missing file → run ends failed.
	s := NewServer()
	// point at a non-existent file via a valid path that Load rejects
	run := s.mgr.Start("/definitely/missing.json", []string{})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snap := run.snapshot(); snap["state"] == "failed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run with unloadable file should end failed")
}

func TestInitializedAliasAndUnknownNotification(t *testing.T) {
	s := NewServer()
	// "initialized" (no slash) is a notification → no response
	var out strings.Builder
	_ = s.Serve(strings.NewReader(`{"jsonrpc":"2.0","method":"initialized"}`+"\n"), &out)
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("initialized should be silent, got %q", out.String())
	}
	// unknown notification (no id) → silent
	out.Reset()
	_ = s.Serve(strings.NewReader(`{"jsonrpc":"2.0","method":"some/other"}`+"\n"), &out)
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("unknown notification should be silent, got %q", out.String())
	}
}

func TestSnapshotAfterEnd(t *testing.T) {
	r := &Run{ID: "r", jobs: map[string]*jobState{"a": {Status: "done"}}, order: []string{"a"},
		started: time.Now().Add(-2 * time.Second)}
	r.ended = time.Now()
	r.state = runPassed
	r.result = scheduler.Result{Ran: 3, Cached: 1}
	snap := r.snapshot()
	if snap["state"] != "passed" || snap["ran"] != 3 {
		t.Fatalf("ended snapshot wrong: %+v", snap)
	}
	if snap["elapsedSeconds"].(int) < 1 {
		t.Errorf("elapsed should reflect ended-started: %v", snap["elapsedSeconds"])
	}
}
