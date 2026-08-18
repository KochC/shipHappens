package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
)

// JobFingerprint deterministically identifies a job's inputs for resume. It
// combines the ordered step commands, the job image, sorted env, hashed input
// files (from all steps' cache input globs), and the fingerprints of upstream
// jobs. If two runs produce the same fingerprint, the job's result is reusable.
type JobFingerprintInput struct {
	JobID        string
	Image        string
	StepCommands []string
	Env          map[string]string
	Workdir      string
	InputGlobs   []string
	UpstreamFPs  []string // fingerprints of jobs this one depends on
}

// JobFingerprint computes the fingerprint hex string.
func JobFingerprint(in JobFingerprintInput) (string, error) {
	h := sha256.New()
	io.WriteString(h, "job:"+in.JobID+"\n")
	io.WriteString(h, "image:"+in.Image+"\n")
	for _, c := range in.StepCommands {
		io.WriteString(h, "step:"+c+"\n")
	}

	keys := make([]string, 0, len(in.Env))
	for k := range in.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		io.WriteString(h, "env:"+k+"="+in.Env[k]+"\n")
	}

	files, err := expandGlobs(in.Workdir, in.InputGlobs)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		fh, err := hashFile(f)
		if err != nil {
			return "", err
		}
		io.WriteString(h, "file:"+f+":"+fh+"\n")
	}

	ups := append([]string(nil), in.UpstreamFPs...)
	sort.Strings(ups)
	for _, u := range ups {
		io.WriteString(h, "up:"+u+"\n")
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// MarkJobDone records a successful job result under its fingerprint so a future
// run with the same fingerprint can skip it (resume). Any declared output globs
// are stored so they can be restored on skip.
func (s *Store) MarkJobDone(fingerprint, workdir string, outputGlobs []string) error {
	return s.Save("job:"+fingerprint, workdir, outputGlobs)
}

// JobDone reports whether a job with this fingerprint completed successfully
// before (and its outputs are cached).
func (s *Store) JobDone(fingerprint string) bool {
	return s.Has("job:" + fingerprint)
}

// RestoreJob restores a completed job's cached outputs into workdir.
func (s *Store) RestoreJob(fingerprint, workdir string) error {
	return s.Restore("job:"+fingerprint, workdir)
}
