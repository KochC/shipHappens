package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandGlobsExcludesDirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "adir"), 0o755) // matched by * but is a dir
	os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o644)
	files, err := expandGlobs(dir, []string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "afile" {
		t.Fatalf("directories should be excluded from plain glob: %v", files)
	}
}

func TestExpandGlobsDoubleStarMatchesNested(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "x", "y"), 0o755)
	os.WriteFile(filepath.Join(dir, "x", "y", "deep.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "top.txt"), []byte("x"), 0o644)
	files, err := expandGlobs(dir, []string{"**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "deep.go" {
		t.Fatalf("double-star should match only nested .go: %v", files)
	}
}

func TestSaveOutputGlobMatchesDirIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	work := t.TempDir()
	os.MkdirAll(filepath.Join(work, "outdir"), 0o755)
	os.WriteFile(filepath.Join(work, "outdir", "f.bin"), []byte("x"), 0o644)
	// glob matches files under outdir; directories themselves are skipped.
	if err := s.Save("k", work, []string{"outdir/**"}); err != nil {
		t.Fatalf("save with dir-containing glob should succeed: %v", err)
	}
	if !s.Has("k") {
		t.Fatal("key should be present")
	}
}
