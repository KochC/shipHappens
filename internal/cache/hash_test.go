package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.go")
	os.WriteFile(f, []byte("package a"), 0o644)

	h1, err := HashInputs("go build", dir, nil, []string{"*.go"})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(f, []byte("package a // changed"), 0o644)
	h2, _ := HashInputs("go build", dir, nil, []string{"*.go"})
	if h1 == h2 {
		t.Fatal("hash should change when file content changes")
	}
}

func TestHashStableSameInputs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644)
	h1, _ := HashInputs("cmd", dir, map[string]string{"A": "1"}, []string{"*.go"})
	h2, _ := HashInputs("cmd", dir, map[string]string{"A": "1"}, []string{"*.go"})
	if h1 != h2 {
		t.Fatal("hash should be stable for identical inputs")
	}
}

func TestHashChangesWithCommand(t *testing.T) {
	dir := t.TempDir()
	h1, _ := HashInputs("a", dir, nil, nil)
	h2, _ := HashInputs("b", dir, nil, nil)
	if h1 == h2 {
		t.Fatal("hash should change with command")
	}
}

func TestDoubleStarGlob(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "x.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "readme.md"), []byte("y"), 0o644)

	files, err := expandGlobs(dir, []string{"src/**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 go file, got %v", files)
	}
}
