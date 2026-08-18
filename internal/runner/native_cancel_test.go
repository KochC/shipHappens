package runner

import (
	"context"
	"io"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

// TestNativeContextCanceled forces cmd.Run to fail with a non-ExitError,
// covering the else branch that sets ExitCode=1.
func TestNativeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before start
	res := NativeRunner{}.Run(ctx, compiler.StepPlan{Run: "sleep 5"}, ".", nil, io.Discard)
	if res.Err == nil {
		t.Fatal("canceled context should error")
	}
	if res.ExitCode != 1 {
		t.Fatalf("non-ExitError should map to exit code 1, got %d", res.ExitCode)
	}
}
