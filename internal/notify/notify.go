// Package notify delivers live notifications about pipeline runs to external
// sinks: the desktop, a webhook, or an arbitrary command. Notifications are
// best-effort — a delivery failure never affects the build.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Level classifies a notification.
type Level int

const (
	Info Level = iota
	Success
	Failure
)

func (l Level) String() string {
	switch l {
	case Success:
		return "success"
	case Failure:
		return "failure"
	default:
		return "info"
	}
}

// Event is a notification payload.
type Event struct {
	Workflow string `json:"workflow"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Level    string `json:"level"`
	Job      string `json:"job,omitempty"`
}

// Sink delivers a notification. Implementations must be safe for advisory use
// (return an error rather than panicking).
type Sink interface {
	Deliver(ctx context.Context, e Event) error
}

// Spec configures notifications (mirrors the IR NotifySpec).
type Spec struct {
	Desktop bool   // native desktop notifications
	Webhook string // POST JSON to this URL
	Exec    string // run this shell command; event is passed via $SHIP_NOTIFY_* env
	OnStart bool   // notify when the run starts
	OnJob   bool   // notify on each job failure
}

// Notifier fans an event out to the configured sinks.
type Notifier struct {
	sinks []Sink
	spec  Spec
}

// New builds a Notifier from a Spec (nil-safe: returns nil if nothing enabled).
func New(spec *Spec) *Notifier {
	if spec == nil || (!spec.Desktop && spec.Webhook == "" && spec.Exec == "") {
		return nil
	}
	n := &Notifier{spec: *spec}
	if spec.Desktop {
		n.sinks = append(n.sinks, desktopSink{})
	}
	if spec.Webhook != "" {
		n.sinks = append(n.sinks, &webhookSink{url: spec.Webhook, client: httpClient})
	}
	if spec.Exec != "" {
		n.sinks = append(n.sinks, execSink{command: spec.Exec})
	}
	return n
}

// Enabled reports whether the notifier has any sinks.
func (n *Notifier) Enabled() bool { return n != nil && len(n.sinks) > 0 }

// WantStart / WantJob expose the corresponding spec toggles.
func (n *Notifier) WantStart() bool { return n != nil && n.spec.OnStart }
func (n *Notifier) WantJob() bool   { return n != nil && n.spec.OnJob }

// Send delivers an event to every sink concurrently; errors are ignored
// (advisory). Bounded by ctx.
func (n *Notifier) Send(ctx context.Context, e Event) {
	if !n.Enabled() {
		return
	}
	for _, s := range n.sinks {
		s := s
		go func() { _ = s.Deliver(ctx, e) }()
	}
}

// SendSync delivers to every sink and waits (used for the final notification,
// where the process may exit right after). Errors are ignored.
func (n *Notifier) SendSync(ctx context.Context, e Event) {
	if !n.Enabled() {
		return
	}
	for _, s := range n.sinks {
		_ = s.Deliver(ctx, e)
	}
}

// ── sinks ────────────────────────────────────────────────────────────────────

// runCmd is the indirection point for exec-based sinks (overridable in tests).
var runCmd = func(ctx context.Context, name string, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	return cmd.Run()
}

// httpClient is overridable in tests.
var httpClient = &http.Client{Timeout: 5 * time.Second}

// goos is the indirection point for the current OS (overridable in tests).
var goos = runtime.GOOS

// desktopSink posts native OS notifications.
type desktopSink struct{}

func (desktopSink) Deliver(ctx context.Context, e Event) error {
	title := e.Title
	if title == "" {
		title = "Ship Happens"
	}
	switch goos {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, e.Message, title)
		return runCmd(ctx, "osascript", []string{"-e", script}, nil)
	case "linux":
		return runCmd(ctx, "notify-send", []string{title, e.Message}, nil)
	default:
		return fmt.Errorf("desktop notifications unsupported on %s", goos)
	}
}

// webhookSink POSTs the event as JSON.
type webhookSink struct {
	url    string
	client *http.Client
}

func (w *webhookSink) Deliver(ctx context.Context, e Event) error {
	body, _ := json.Marshal(e)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

// execSink runs a shell command with SHIP_NOTIFY_* env vars set.
type execSink struct{ command string }

func (x execSink) Deliver(ctx context.Context, e Event) error {
	env := append(os.Environ(),
		"SHIP_NOTIFY_WORKFLOW="+e.Workflow,
		"SHIP_NOTIFY_TITLE="+e.Title,
		"SHIP_NOTIFY_MESSAGE="+e.Message,
		"SHIP_NOTIFY_LEVEL="+e.Level,
		"SHIP_NOTIFY_JOB="+e.Job,
	)
	return runCmd(ctx, "sh", []string{"-c", x.command}, env)
}
