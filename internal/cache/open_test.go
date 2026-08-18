package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMkdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make ~/.ship a *file* so MkdirAll(~/.ship/cache/objects) fails.
	os.WriteFile(filepath.Join(home, ".ship"), []byte("blocker"), 0o644)
	if _, err := Open(); err == nil {
		t.Fatal("expected Open to fail when ~/.ship is a file")
	}
}
