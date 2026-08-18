package logs

import (
	"io"
	"testing"
)

func TestSetQuietPrefixedDiscards(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)
	w := Prefixed("job")
	if w != io.Discard {
		t.Fatalf("in quiet mode Prefixed should return io.Discard, got %T", w)
	}
}

func TestPrefixedActiveWhenNotQuiet(t *testing.T) {
	SetQuiet(false)
	w := Prefixed("job")
	if w == io.Discard {
		t.Fatal("Prefixed should return a real writer when not quiet")
	}
	// closing the underlying pipe writer stops the goroutine cleanly
	if wc, ok := w.(io.WriteCloser); ok {
		_ = wc.Close()
	}
}
