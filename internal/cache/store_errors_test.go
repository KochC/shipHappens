package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenWithCorruptIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// pre-create a corrupt index.json
	root := filepath.Join(home, ".ship", "cache")
	os.MkdirAll(filepath.Join(root, "objects"), 0o755)
	os.WriteFile(filepath.Join(root, "index.json"), []byte("{not json"), 0o644)
	s, err := Open()
	if err != nil {
		t.Fatalf("Open should tolerate corrupt index: %v", err)
	}
	if s.Has("anything") {
		t.Fatal("corrupt index should yield empty store")
	}
}

func TestSaveCreateFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open()
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "f"), []byte("x"), 0o644)
	// Make the objects dir unwritable so os.Create fails.
	objs := filepath.Join(home, ".ship", "cache", "objects")
	os.Chmod(objs, 0o500)
	defer os.Chmod(objs, 0o755)
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod won't restrict writes")
	}
	if err := s.Save("k", work, []string{"f"}); err == nil {
		t.Fatal("expected Save to fail when object file cannot be created")
	}
}

func TestRestoreCorruptTar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open()
	// Register a key whose object file is not a valid tar.
	objs := filepath.Join(home, ".ship", "cache", "objects")
	os.WriteFile(filepath.Join(objs, "k.tar"), []byte("not-a-tar-archive-really"), 0o644)
	s.index["k"] = "k.tar"
	if err := s.Restore("k", t.TempDir()); err == nil {
		t.Fatal("expected error restoring corrupt tar")
	}
}

func TestHashFileOnDirectory(t *testing.T) {
	if _, err := hashFile(t.TempDir()); err == nil {
		t.Fatal("hashing a directory should error")
	}
}

func TestSaveEmptyOutputsProducesEmptyTar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open()
	if err := s.Save("empty", t.TempDir(), nil); err != nil {
		t.Fatalf("saving no outputs should still succeed: %v", err)
	}
	if !s.Has("empty") {
		t.Fatal("empty save should still register key")
	}
}
