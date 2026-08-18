package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// putObj creates an object file of the given size with a specific mtime and an
// index entry pointing at it.
func putObj(t *testing.T, s *Store, key string, size int, age time.Duration) string {
	t.Helper()
	name := key + ".tar"
	p := filepath.Join(s.root, "objects", name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	s.index[key] = name
	return p
}

func TestStatEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	st, err := s.Stat()
	if err != nil || st.Objects != 0 || st.Bytes != 0 {
		t.Fatalf("empty stat wrong: %+v %v", st, err)
	}
}

func TestStat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	putObj(t, s, "a", 100, 3*time.Hour)
	putObj(t, s, "b", 200, 1*time.Hour)
	st, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Objects != 2 || st.Bytes != 300 {
		t.Fatalf("stat wrong: %+v", st)
	}
	if !st.Oldest.Before(st.Newest) {
		t.Errorf("oldest/newest wrong: %v %v", st.Oldest, st.Newest)
	}
}

func TestPruneByAge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	putObj(t, s, "old", 100, 48*time.Hour)
	putObj(t, s, "new", 100, 1*time.Hour)

	res, err := s.Prune(24*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || res.Bytes != 100 {
		t.Fatalf("age prune wrong: %+v", res)
	}
	// old object + its index ref gone; new one kept
	if _, ok := s.index["old"]; ok {
		t.Error("old index ref not dropped")
	}
	if !s.Has("new") {
		t.Error("new object should remain")
	}
}

func TestPruneBySize(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	putObj(t, s, "o1", 100, 3*time.Hour) // oldest
	putObj(t, s, "o2", 100, 2*time.Hour)
	putObj(t, s, "o3", 100, 1*time.Hour) // newest

	// cap to 150 bytes → must evict oldest until <=150 (removes o1, o2 → 100 left)
	res, err := s.Prune(0, 150)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("size prune should remove 2 oldest, got %+v", res)
	}
	if !s.Has("o3") {
		t.Error("newest should survive LRU eviction")
	}
}

func TestPruneAll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	putObj(t, s, "a", 10, time.Minute)
	putObj(t, s, "b", 10, time.Minute)
	res, err := s.PruneAll()
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("prune all should remove everything, got %+v", res)
	}
	if len(s.index) != 0 {
		t.Errorf("index should be empty, got %+v", s.index)
	}
}

func TestPruneNoLimitsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	putObj(t, s, "a", 10, time.Hour)
	res, err := s.Prune(0, 0)
	if err != nil || res.Removed != 0 {
		t.Fatalf("no-limit prune should remove nothing: %+v %v", res, err)
	}
}

func TestRootAndStatMissingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	if s.Root() == "" {
		t.Error("Root should be set")
	}
	// remove objects dir → Stat/list handle NotExist gracefully
	os.RemoveAll(filepath.Join(s.root, "objects"))
	if st, err := s.Stat(); err != nil || st.Objects != 0 {
		t.Fatalf("stat over missing dir: %+v %v", st, err)
	}
	if res, err := s.Prune(time.Hour, 0); err != nil || res.Removed != 0 {
		t.Fatalf("prune over missing dir: %+v %v", res, err)
	}
}

func TestStatSkipsSubdirs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open()
	// a subdirectory inside objects/ must be skipped by Stat and listObjects.
	os.MkdirAll(filepath.Join(s.root, "objects", "sub"), 0o755)
	putObj(t, s, "a", 50, time.Hour)
	st, err := s.Stat()
	if err != nil || st.Objects != 1 || st.Bytes != 50 {
		t.Fatalf("subdir should be skipped: %+v %v", st, err)
	}
	// prune also walks listObjects → exercise its subdir skip
	if _, err := s.Prune(0, 40); err != nil {
		t.Fatal(err)
	}
}
