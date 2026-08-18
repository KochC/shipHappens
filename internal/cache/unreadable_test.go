package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashInputsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	os.WriteFile(f, []byte("data"), 0o644)
	os.Chmod(f, 0o000) // exists (Stat ok) but Open/read fails
	defer os.Chmod(f, 0o644)
	if _, err := HashInputs("c", dir, nil, []string{"*.txt"}); err == nil {
		t.Fatal("expected HashInputs to fail on unreadable matched file")
	}
}
