package outputs

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseKeyValue(t *testing.T) {
	kv, err := Parse(write(t, "version=1.2.3\narch=arm64\n"))
	if err != nil {
		t.Fatal(err)
	}
	if kv["version"] != "1.2.3" || kv["arch"] != "arm64" {
		t.Fatalf("wrong: %+v", kv)
	}
}

func TestParseHeredoc(t *testing.T) {
	kv, err := Parse(write(t, "notes<<EOF\nline one\nline two\nEOF\nk=v\n"))
	if err != nil {
		t.Fatal(err)
	}
	if kv["notes"] != "line one\nline two" {
		t.Fatalf("heredoc wrong: %q", kv["notes"])
	}
	if kv["k"] != "v" {
		t.Fatalf("post-heredoc kv wrong: %+v", kv)
	}
}

func TestParseIgnoresBlankAndMalformed(t *testing.T) {
	kv, err := Parse(write(t, "\n=noval\nnokeyvalue\nok=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(kv) != 1 || kv["ok"] != "1" {
		t.Fatalf("should ignore blank/malformed: %+v", kv)
	}
}

func TestParseMissingFile(t *testing.T) {
	kv, err := Parse(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(kv) != 0 {
		t.Fatalf("missing file should be empty/no-error: %v %v", kv, err)
	}
}

func TestParseValueWithEquals(t *testing.T) {
	kv, _ := Parse(write(t, "url=https://x/y?a=b\n"))
	if kv["url"] != "https://x/y?a=b" {
		t.Fatalf("value with = wrong: %q", kv["url"])
	}
}
