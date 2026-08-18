package scheduler

import (
	"io"
	"os"
	"path/filepath"
)

// jobLogFile opens (truncating) the per-job log file under Options.LogDir.
// Returns nil on any error — persistence is best-effort and never fails a run.
func (s *scheduler) jobLogFile(jobID string) *os.File {
	if s.opts.LogDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.opts.LogDir, 0o755); err != nil {
		return nil
	}
	f, err := os.Create(filepath.Join(s.opts.LogDir, sanitizeLogName(jobID)+".log"))
	if err != nil {
		return nil
	}
	return f
}

// sanitizeLogName makes a filesystem-safe file stem from a job id (which can
// contain '/' from matrix expansion, e.g. "test/1.22-linux").
func sanitizeLogName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			b[i] = '_'
		}
	}
	return string(b)
}

// maskWriter applies the secret masker to bytes before writing them to the
// underlying writer, so persisted logs never contain secret values.
type maskWriter struct {
	w    io.Writer
	mask func(string) string
}

func (m maskWriter) Write(p []byte) (int, error) {
	if m.mask != nil {
		masked := m.mask(string(p))
		if _, err := m.w.Write([]byte(masked)); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return m.w.Write(p)
}
