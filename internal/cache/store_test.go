package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSaveHasRestore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	os.MkdirAll(filepath.Join(work, "dist"), 0o755)
	os.WriteFile(filepath.Join(work, "dist", "app.bin"), []byte("BINARY"), 0o644)

	key := "abc123"
	if s.Has(key) {
		t.Fatal("should not have key yet")
	}
	if err := s.Save(key, work, []string{"dist/**"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !s.Has(key) {
		t.Fatal("should have key after save")
	}

	// delete then restore
	os.RemoveAll(filepath.Join(work, "dist"))
	if err := s.Restore(key, work); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(work, "dist", "app.bin"))
	if err != nil || string(got) != "BINARY" {
		t.Fatalf("restore failed: got=%q err=%v", got, err)
	}
}

func TestStorePersistsIndexAcrossOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "f"), []byte("x"), 0o644)

	s1, _ := Open()
	if err := s1.Save("k", work, []string{"f"}); err != nil {
		t.Fatal(err)
	}

	// reopen: index.json should be reloaded
	s2, _ := Open()
	if !s2.Has("k") {
		t.Fatal("reopened store should see previously saved key")
	}
}
