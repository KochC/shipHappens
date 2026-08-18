package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStrconvI(t *testing.T) {
	cases := map[int64]string{0: "0", 5: "5", 123: "123", -7: "-7", -1000: "-1000"}
	for in, want := range cases {
		if got := strconvI(in); got != want {
			t.Errorf("strconvI(%d)=%q want %q", in, got, want)
		}
	}
}

func TestPrunedDir(t *testing.T) {
	for _, d := range []string{".git", ".pio", "node_modules", "__pycache__", "managed_components", ".venv", ".mypy_cache", ".ship-artifacts", ".ship-overlay"} {
		if !prunedDir(d) {
			t.Errorf("%s should be pruned", d)
		}
	}
	for _, d := range []string{"src", "apps", "vendor-libs"} {
		if prunedDir(d) {
			t.Errorf("%s should NOT be pruned", d)
		}
	}
}

func TestStatSignatureMissing(t *testing.T) {
	if _, err := statSignature("/no/such/file/xyz"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHashFileMissing(t *testing.T) {
	if _, err := hashFile("/no/such/file/xyz"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExpandGlobsPrunesHeavyDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("x"), 0o644)

	files, err := expandGlobs(dir, []string{"**/*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Base(filepath.Dir(f)) == ".git" {
			t.Fatalf(".git should be pruned, found %s", f)
		}
	}
}

func TestExpandGlobsPlainGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755) // dir should be excluded
	files, err := expandGlobs(dir, []string{"*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %v", files)
	}
}

func TestExpandGlobsBadPattern(t *testing.T) {
	if _, err := expandGlobs(t.TempDir(), []string{"[bad"}); err == nil {
		t.Fatal("expected error for malformed glob pattern")
	}
}

func TestHashInputsWithEnvAndFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644)
	h1, err := HashInputs("cmd", dir, map[string]string{"B": "2", "A": "1"}, []string{"*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == "" {
		t.Fatal("empty hash")
	}
}

func TestHashInputsMissingFileErrors(t *testing.T) {
	// A glob that matches a file we then remove mid-flight is hard to force;
	// instead verify a bad glob pattern surfaces the error.
	if _, err := HashInputs("c", t.TempDir(), nil, []string{"[bad"}); err == nil {
		t.Fatal("expected error from bad glob")
	}
}

func TestJobFingerprintBadGlob(t *testing.T) {
	_, err := JobFingerprint(JobFingerprintInput{
		JobID: "j", Workdir: t.TempDir(), InputGlobs: []string{"[bad"},
	})
	if err == nil {
		t.Fatal("expected error from bad glob in fingerprint")
	}
}

func TestSaveBadOutputGlob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	if err := s.Save("k", t.TempDir(), []string{"[bad"}); err == nil {
		t.Fatal("expected save error for bad glob")
	}
}

func TestRestoreUnknownKeyNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	if err := s.Restore("missing", t.TempDir()); err != nil {
		t.Fatalf("restore of unknown key should be a no-op, got %v", err)
	}
}

func TestJobDoneRestoreJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "o"), []byte("r"), 0o644)
	if s.JobDone("fp") {
		t.Fatal("not done yet")
	}
	if err := s.MarkJobDone("fp", work, []string{"o"}); err != nil {
		t.Fatal(err)
	}
	if !s.JobDone("fp") {
		t.Fatal("should be done")
	}
	os.Remove(filepath.Join(work, "o"))
	if err := s.RestoreJob("fp", work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "o")); err != nil {
		t.Fatal("output not restored")
	}
}
