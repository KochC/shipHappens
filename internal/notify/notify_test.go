package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func stubRun(t *testing.T) *[][]string {
	t.Helper()
	var mu sync.Mutex
	var calls [][]string
	prev := runCmd
	runCmd = func(_ context.Context, name string, args []string, _ []string) error {
		mu.Lock()
		calls = append(calls, append([]string{name}, args...))
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { runCmd = prev })
	return &calls
}

func TestNewNilAndEmpty(t *testing.T) {
	if New(nil) != nil {
		t.Error("nil spec → nil notifier")
	}
	if New(&Spec{}) != nil {
		t.Error("empty spec → nil notifier")
	}
	if New(&Spec{Desktop: true}) == nil {
		t.Error("desktop spec → notifier")
	}
}

func TestEnabledAndToggles(t *testing.T) {
	var n *Notifier
	if n.Enabled() || n.WantStart() || n.WantJob() {
		t.Error("nil notifier should be disabled")
	}
	n = New(&Spec{Exec: "true", OnStart: true, OnJob: true})
	if !n.Enabled() || !n.WantStart() || !n.WantJob() {
		t.Error("toggles not reflected")
	}
}

func TestExecSink(t *testing.T) {
	calls := stubRun(t)
	n := New(&Spec{Exec: "echo hi"})
	n.SendSync(context.Background(), Event{Workflow: "W", Message: "m", Level: "info"})
	if len(*calls) != 1 || (*calls)[0][0] != "sh" {
		t.Fatalf("exec sink not invoked: %v", *calls)
	}
}

func TestDesktopSink(t *testing.T) {
	calls := stubRun(t)
	n := New(&Spec{Desktop: true})
	n.SendSync(context.Background(), Event{Title: "T", Message: "M"})
	if len(*calls) != 1 {
		t.Fatalf("desktop sink not invoked: %v", *calls)
	}
	// darwin → osascript, linux → notify-send
	bin := (*calls)[0][0]
	if bin != "osascript" && bin != "notify-send" {
		t.Errorf("unexpected desktop binary: %s", bin)
	}
}

func TestWebhookSink(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		got = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := New(&Spec{Webhook: srv.URL})
	n.SendSync(context.Background(), Event{Workflow: "W", Message: "hello", Level: "success"})
	if !strings.Contains(got, `"message":"hello"`) || !strings.Contains(got, `"level":"success"`) {
		t.Fatalf("webhook body wrong: %s", got)
	}
}

func TestWebhookErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	w := &webhookSink{url: srv.URL, client: srv.Client()}
	if err := w.Deliver(context.Background(), Event{}); err == nil {
		t.Error("500 should return an error")
	}
}

func TestWebhookBadURL(t *testing.T) {
	w := &webhookSink{url: "http://127.0.0.1:0/nope", client: &http.Client{}}
	if err := w.Deliver(context.Background(), Event{}); err == nil {
		t.Error("unreachable webhook should error")
	}
}

func TestSendAsyncDelivers(t *testing.T) {
	var mu sync.Mutex
	n := New(&Spec{Exec: "x"})
	done := make(chan struct{}, 1)
	prev := runCmd
	runCmd = func(context.Context, string, []string, []string) error {
		mu.Lock()
		select {
		case done <- struct{}{}:
		default:
		}
		mu.Unlock()
		return nil
	}
	defer func() { runCmd = prev }()
	n.Send(context.Background(), Event{Message: "async"})
	<-done // delivered
}

func TestSendDisabledNoop(t *testing.T) {
	var n *Notifier
	n.Send(context.Background(), Event{})     // must not panic
	n.SendSync(context.Background(), Event{}) // must not panic
}

func TestExecSinkError(t *testing.T) {
	prev := runCmd
	runCmd = func(context.Context, string, []string, []string) error { return errors.New("boom") }
	defer func() { runCmd = prev }()
	// error is swallowed by SendSync (advisory) — just ensure no panic
	New(&Spec{Exec: "x"}).SendSync(context.Background(), Event{})
}

func TestDesktopSinkAllOS(t *testing.T) {
	calls := stubRun(t)
	prev := goos
	defer func() { goos = prev }()

	goos = "darwin"
	if err := (desktopSink{}).Deliver(context.Background(), Event{Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[len(*calls)-1][0] != "osascript" {
		t.Error("darwin → osascript")
	}
	goos = "linux"
	if err := (desktopSink{}).Deliver(context.Background(), Event{Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[len(*calls)-1][0] != "notify-send" {
		t.Error("linux → notify-send")
	}
	goos = "plan9"
	if err := (desktopSink{}).Deliver(context.Background(), Event{}); err == nil {
		t.Error("unsupported OS should error")
	}
}

func TestLevelString(t *testing.T) {
	if Info.String() != "info" || Success.String() != "success" || Failure.String() != "failure" {
		t.Error("level strings wrong")
	}
}
