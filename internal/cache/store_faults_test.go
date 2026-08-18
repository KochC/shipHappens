package cache

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRestoreIntoUnwritableDest forces os.Create(dest) / MkdirAll to fail.
func TestRestoreIntoUnwritableDest(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open()

	// Save a real file so we have a valid tar to restore.
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "out.bin"), []byte("data"), 0o644)
	if err := s.Save("k", src, []string{"out.bin"}); err != nil {
		t.Fatal(err)
	}

	// Restore into an unwritable destination directory.
	dest := t.TempDir()
	os.Chmod(dest, 0o500)
	defer os.Chmod(dest, 0o755)
	if err := s.Restore("k", dest); err == nil {
		t.Fatal("expected restore to fail writing into unwritable dest")
	}
}

// TestSaveUnreadableInput forces addFile's os.Open to fail.
func TestSaveUnreadableInput(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open()

	work := t.TempDir()
	secret := filepath.Join(work, "secret.bin")
	os.WriteFile(secret, []byte("x"), 0o644)
	os.Chmod(secret, 0o000)
	defer os.Chmod(secret, 0o644)

	if err := s.Save("k", work, []string{"secret.bin"}); err == nil {
		t.Fatal("expected save to fail on unreadable input file")
	}
}
