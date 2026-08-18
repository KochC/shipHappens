package cache

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
)

// writeTar creates an object tarball containing one entry with the given name.
func writeTar(t *testing.T, path, entryName string, data []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	_ = tw.WriteHeader(&tar.Header{Name: entryName, Mode: 0o644, Size: int64(len(data))})
	tw.Write(data)
	tw.Close()
}

func TestRestoreMissingObjectFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	// index references an object file that doesn't exist -> os.Open fails.
	s.index["k"] = "ghost.tar"
	if err := s.Restore("k", t.TempDir()); err == nil {
		t.Fatal("expected error when object file is missing")
	}
}

func TestRestoreCreateFailsWhenDestIsDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	objs := filepath.Join(s.root, "objects")
	writeTar(t, filepath.Join(objs, "k.tar"), "collide", []byte("x"))
	s.index["k"] = "k.tar"

	work := t.TempDir()
	// Pre-create a *directory* named "collide" so os.Create(dest) fails.
	os.MkdirAll(filepath.Join(work, "collide"), 0o755)
	if err := s.Restore("k", work); err == nil {
		t.Fatal("expected Create to fail when dest path is a directory")
	}
}

func TestRestoreMkdirFailsUnderFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	objs := filepath.Join(s.root, "objects")
	// entry inside a subdir; we'll make that subdir path a regular file.
	writeTar(t, filepath.Join(objs, "k.tar"), "sub/inner.txt", []byte("x"))
	s.index["k"] = "k.tar"

	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "sub"), []byte("i am a file"), 0o644)
	if err := s.Restore("k", work); err == nil {
		t.Fatal("expected MkdirAll to fail when parent path is a file")
	}
}

func TestFlushWriteFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	// Point indexFP at a path whose parent is a file -> WriteFile fails.
	badParent := filepath.Join(s.root, "afile")
	os.WriteFile(badParent, []byte("x"), 0o644)
	s.indexFP = filepath.Join(badParent, "index.json")
	if err := s.flush(); err == nil {
		t.Fatal("expected flush WriteFile to fail")
	}
}

func TestAddFileStatFails(t *testing.T) {
	f, _ := os.Create(filepath.Join(t.TempDir(), "obj.tar"))
	defer f.Close()
	tw := tar.NewWriter(f)
	defer tw.Close()
	if err := addFile(tw, t.TempDir(), "/no/such/file"); err == nil {
		t.Fatal("expected addFile to fail on missing file")
	}
}
