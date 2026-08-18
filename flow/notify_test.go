package flow

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNotificationsFireDuringRun(t *testing.T) {
	quietLogs(t)

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer srv.Close()

	wf := New("Notified").Notifications(Notify{
		Webhook: srv.URL,
		OnStart: true,
		OnJob:   true,
	})
	wf.Job("a").Run("s", "true")

	if code := run(wf, runOpts{noCache: true}); code != 0 {
		t.Fatalf("run failed: %d", code)
	}

	// onStart (async) + final (sync) should both arrive.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "run started") {
		t.Errorf("missing onStart notification: %v", bodies)
	}
	if !strings.Contains(joined, "passed in") {
		t.Errorf("missing final notification: %v", bodies)
	}
}

func TestNotificationsJobFailure(t *testing.T) {
	quietLogs(t)

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer srv.Close()

	wf := New("Failing").Notifications(Notify{Webhook: srv.URL, OnJob: true})
	wf.Job("bad").Run("s", "exit 1")

	if code := run(wf, runOpts{noCache: true}); code != 1 {
		t.Fatalf("expected failure exit, got %d", code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "job bad failed") {
		t.Errorf("missing job-failure notification: %v", bodies)
	}
	if !strings.Contains(joined, "failed in") {
		t.Errorf("missing final-failure notification: %v", bodies)
	}
}

func TestBuildNotifierNil(t *testing.T) {
	if buildNotifier(nil) != nil {
		t.Error("nil spec → nil notifier")
	}
}
