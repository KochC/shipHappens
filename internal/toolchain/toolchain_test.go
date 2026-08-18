package toolchain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func stub(t *testing.T, avail bool, run func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	t.Helper()
	pl, pr := lookPath, runCmd
	lookPath = func(string) (string, error) {
		if avail {
			return "/usr/local/bin/mise", nil
		}
		return "", errors.New("not found")
	}
	if run != nil {
		runCmd = run
	}
	t.Cleanup(func() { lookPath = pl; runCmd = pr })
}

func TestMerge(t *testing.T) {
	if Merge(nil, nil) != nil {
		t.Error("empty merge should be nil")
	}
	m := Merge(map[string]string{"go": "1.22", "node": "18"}, map[string]string{"node": "20"})
	if m["go"] != "1.22" || m["node"] != "20" {
		t.Fatalf("merge/override wrong: %+v", m)
	}
}

func TestSpecArgsDeterministic(t *testing.T) {
	a := specArgs(map[string]string{"node": "20", "go": "1.22"})
	if len(a) != 2 || a[0] != "go@1.22" || a[1] != "node@20" {
		t.Fatalf("spec args not sorted: %v", a)
	}
}

func TestPrependPath(t *testing.T) {
	if PrependPath("/usr/bin", nil) != "/usr/bin" {
		t.Error("no dirs → unchanged")
	}
	got := PrependPath("/usr/bin", []string{"/a/bin", "/b/bin"})
	if got != "/a/bin:/b/bin:/usr/bin" {
		t.Fatalf("prepend wrong: %q", got)
	}
}

func TestResolveEmpty(t *testing.T) {
	dirs, err := Resolve(context.Background(), nil)
	if dirs != nil || err != nil {
		t.Fatalf("empty tools → nil,nil: %v %v", dirs, err)
	}
}

func TestResolveNoBackend(t *testing.T) {
	stub(t, false, nil)
	_, err := Resolve(context.Background(), map[string]string{"go": "1.22"})
	if err == nil {
		t.Fatal("no backend should return an advisory error")
	}
}

func TestResolveSuccess(t *testing.T) {
	var calls [][]string
	stub(t, true, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 0 && args[0] == "where" {
			return []byte("/opt/tools/" + args[1] + "\n"), nil
		}
		return nil, nil // install
	})
	dirs, err := Resolve(context.Background(), map[string]string{"go": "1.22", "node": "20"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || !strings.HasSuffix(dirs[0], "/bin") {
		t.Fatalf("bin paths wrong: %v", dirs)
	}
	// first call is a single `install go@1.22 node@20`
	if calls[0][1] != "install" || calls[0][2] != "go@1.22" {
		t.Fatalf("install call wrong: %v", calls[0])
	}
}

func TestResolveInstallFails(t *testing.T) {
	stub(t, true, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "install" {
			return []byte("boom"), errors.New("fail")
		}
		return nil, nil
	})
	if _, err := Resolve(context.Background(), map[string]string{"go": "1.22"}); err == nil {
		t.Fatal("install failure should error")
	}
}

func TestResolveWhereFails(t *testing.T) {
	stub(t, true, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "where" {
			return []byte("nope"), errors.New("fail")
		}
		return nil, nil
	})
	if _, err := Resolve(context.Background(), map[string]string{"go": "1.22"}); err == nil {
		t.Fatal("where failure should error")
	}
}

func TestResolveWhereEmptySkipped(t *testing.T) {
	stub(t, true, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "where" {
			return []byte("  \n"), nil // empty → skipped
		}
		return nil, nil
	})
	dirs, err := Resolve(context.Background(), map[string]string{"go": "1.22"})
	if err != nil || len(dirs) != 0 {
		t.Fatalf("empty where dir should be skipped: %v %v", dirs, err)
	}
}

func TestAvailable(t *testing.T) {
	stub(t, true, nil)
	if !Available() {
		t.Error("should be available when mise on PATH")
	}
	stub(t, false, nil)
	if Available() {
		t.Error("should be unavailable without mise")
	}
}

func TestDefaultRunCmd(t *testing.T) {
	// exercise the real runCmd against a trivial command
	out, err := runCmd(context.Background(), "sh", "-c", "echo hi")
	if err != nil || !strings.Contains(string(out), "hi") {
		t.Fatalf("runCmd: %q %v", out, err)
	}
	if _, err := runCmd(context.Background(), "definitely-not-a-real-binary-zz"); err == nil {
		t.Error("missing binary should error")
	}
}
