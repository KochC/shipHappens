// Package cache implements a content-addressed step-result cache under
// ~/.ship/cache. Only steps with an explicit CacheSpec are cached (M1).
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// HashInputs computes a deterministic hash over: the command, sorted env,
// working dir, and the content of all files matching the input globs.
func HashInputs(command, workdir string, env map[string]string, inputGlobs []string) (string, error) {
	h := sha256.New()
	io.WriteString(h, "cmd:"+command+"\n")
	io.WriteString(h, "wd:"+workdir+"\n")

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		io.WriteString(h, "env:"+k+"="+env[k]+"\n")
	}

	files, err := expandGlobs(workdir, inputGlobs)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		fh, err := hashFile(f)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(workdir, f)
		io.WriteString(h, "file:"+rel+":"+fh+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// expandGlobs resolves globs relative to workdir into a sorted, de-duped list
// of regular files. Supports ** via a simple walk when a glob contains it.
func expandGlobs(workdir string, globs []string) ([]string, error) {
	set := map[string]bool{}
	for _, g := range globs {
		full := filepath.Join(workdir, g)
		if containsDoubleStar(g) {
			base, pattern := splitDoubleStar(workdir, g)
			err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return nil // skip unreadable
				}
				if d.IsDir() {
					return nil
				}
				if matchSuffix(p, pattern) {
					set[p] = true
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		matches, err := filepath.Glob(full)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
				set[m] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

func containsDoubleStar(g string) bool {
	for i := 0; i+1 < len(g); i++ {
		if g[i] == '*' && g[i+1] == '*' {
			return true
		}
	}
	return false
}

// splitDoubleStar returns (baseDir, suffixPattern) for a glob like "src/**/*.go".
func splitDoubleStar(workdir, g string) (string, string) {
	// base = everything before the first "**"
	idx := 0
	for i := 0; i+1 < len(g); i++ {
		if g[i] == '*' && g[i+1] == '*' {
			idx = i
			break
		}
	}
	base := g[:idx]
	suffix := g[idx+2:] // after "**"
	// strip leading slash from suffix, e.g. "/*.go" -> "*.go"
	for len(suffix) > 0 && suffix[0] == '/' {
		suffix = suffix[1:]
	}
	return filepath.Join(workdir, base), suffix
}

// matchSuffix matches a path against a trailing pattern like "*.go" or "".
func matchSuffix(path, pattern string) bool {
	if pattern == "" {
		return true
	}
	ok, _ := filepath.Match(pattern, filepath.Base(path))
	return ok
}
