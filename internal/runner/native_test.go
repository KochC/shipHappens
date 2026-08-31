package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/KochC/shipHappens/internal/compiler"
)

func TestNativeSuccess(t *testing.T) {
	var buf bytes.Buffer
	r := NativeRunner{}
	res := r.Run(context.Background(), compiler.StepPlan{ID: "s", Run: "echo hello"}, ".", nil, &buf)
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("expected success, got %+v", res)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("expected output, got %q", buf.String())
	}
}

func TestNativeFailureExitCode(t *testing.T) {
	var buf bytes.Buffer
	res := NativeRunner{}.Run(context.Background(), compiler.StepPlan{ID: "s", Run: "exit 3"}, ".", nil, &buf)
	if res.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %d", res.ExitCode)
	}
	if res.Err == nil {
		t.Fatal("expected error")
	}
}

func TestNativeEnv(t *testing.T) {
	var buf bytes.Buffer
	NativeRunner{}.Run(context.Background(), compiler.StepPlan{ID: "s", Run: "echo $FOO"}, ".", map[string]string{"FOO": "bar"}, &buf)
	if !strings.Contains(buf.String(), "bar") {
		t.Fatalf("expected env var in output, got %q", buf.String())
	}
}
