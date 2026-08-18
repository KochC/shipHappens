package runner

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestDefaultExecRunSuccessAndFailure(t *testing.T) {
	// success
	code, err := execRun(context.Background(), "sh", []string{"-c", "exit 0"}, io.Discard)
	if code != 0 || err != nil {
		t.Fatalf("want 0/nil, got %d/%v", code, err)
	}
	// non-zero exit (ExitError branch)
	code, err = execRun(context.Background(), "sh", []string{"-c", "exit 4"}, io.Discard)
	if code != 4 || err == nil {
		t.Fatalf("want 4/err, got %d/%v", code, err)
	}
	// binary not found (non-ExitError branch)
	code, err = execRun(context.Background(), "definitely-not-a-real-binary-xyz", nil, io.Discard)
	if code != 1 || err == nil {
		t.Fatalf("want 1/err for missing binary, got %d/%v", code, err)
	}
}

func TestRunResult(t *testing.T) {
	r := runResult(0, nil, time.Now())
	if r.Err != nil || r.ExitCode != 0 {
		t.Fatal("clean result expected")
	}
	r = runResult(2, errors.New("x"), time.Now())
	if r.Err == nil || r.ExitCode != 2 {
		t.Fatal("error result expected")
	}
}

func TestErrNonZero(t *testing.T) {
	if errNonZero(9) == nil {
		t.Fatal("expected error")
	}
}
