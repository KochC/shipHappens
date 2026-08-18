package changed

import (
	"errors"
	"testing"
)

func stubGit(t *testing.T, fn func(workdir string, args ...string) ([]byte, error)) {
	t.Helper()
	prev := gitDiff
	gitDiff = fn
	t.Cleanup(func() { gitDiff = prev })
}

func TestFilesPrimarySuccess(t *testing.T) {
	stubGit(t, func(_ string, args ...string) ([]byte, error) {
		return []byte("a.go\n\nsrc/b.go\n"), nil
	})
	files, err := Files("/w", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "a.go" || files[1] != "src/b.go" {
		t.Fatalf("unexpected files: %v", files)
	}
}

func TestFilesFallbackToUncommitted(t *testing.T) {
	calls := 0
	stubGit(t, func(_ string, args ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("no such ref") // primary fails
		}
		return []byte("dirty.txt\n"), nil // fallback succeeds
	})
	files, err := Files("/w", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "dirty.txt" {
		t.Fatalf("fallback files wrong: %v", files)
	}
}

func TestFilesBothFail(t *testing.T) {
	stubGit(t, func(_ string, args ...string) ([]byte, error) {
		return nil, errors.New("not a git repo")
	})
	if _, err := Files("/w", "main"); err == nil {
		t.Fatal("expected error when both git invocations fail")
	}
}

func TestFilesEmptyOutput(t *testing.T) {
	stubGit(t, func(_ string, args ...string) ([]byte, error) { return []byte("\n"), nil })
	files, err := Files("/w", "main")
	if err != nil || len(files) != 0 {
		t.Fatalf("empty diff should yield no files, got %v err=%v", files, err)
	}
}
