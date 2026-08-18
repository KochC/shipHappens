package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintStableAndSensitive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o644)

	base := JobFingerprintInput{
		JobID: "build", Image: "img", StepCommands: []string{"make"},
		Env: map[string]string{"X": "1"}, Workdir: dir,
		InputGlobs: []string{"*.go"}, UpstreamFPs: []string{"up1"},
	}
	fp1, err := JobFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	fp2, _ := JobFingerprint(base)
	if fp1 != fp2 {
		t.Fatal("fingerprint not stable")
	}

	// changing a command changes the fp
	b := base
	b.StepCommands = []string{"make all"}
	if fp, _ := JobFingerprint(b); fp == fp1 {
		t.Fatal("fp should change with command")
	}
	// changing an upstream fp changes the fp
	b = base
	b.UpstreamFPs = []string{"up2"}
	if fp, _ := JobFingerprint(b); fp == fp1 {
		t.Fatal("fp should change with upstream")
	}
	// changing input file content (size) changes the fp
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a // longer content changes size"), 0o644)
	if fp, _ := JobFingerprint(base); fp == fp1 {
		t.Fatal("fp should change with input content")
	}
}

func TestMarkAndRestoreJob(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "out.bin"), []byte("result"), 0o644)

	fp := "deadbeef"
	if s.JobDone(fp) {
		t.Fatal("should not be done yet")
	}
	if err := s.MarkJobDone(fp, work, []string{"out.bin"}); err != nil {
		t.Fatal(err)
	}
	if !s.JobDone(fp) {
		t.Fatal("should be done after mark")
	}

	// wipe and restore
	os.Remove(filepath.Join(work, "out.bin"))
	if err := s.RestoreJob(fp, work); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(work, "out.bin")); string(b) != "result" {
		t.Fatalf("restore failed, got %q", b)
	}
}
