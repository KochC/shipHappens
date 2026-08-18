package cache

import (
	"archive/tar"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// Store is a content-addressed cache on disk.
type Store struct {
	root    string // ~/.ship/cache
	index   map[string]string // cache key -> object filename (outputs tarball)
	indexFP string
}

// Open initializes (creating if needed) the cache store.
func Open() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".ship", "cache")
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{root: root, index: map[string]string{}, indexFP: filepath.Join(root, "index.json")}
	if b, err := os.ReadFile(s.indexFP); err == nil {
		_ = json.Unmarshal(b, &s.index)
	}
	return s, nil
}

// Has reports whether a result for key is cached.
func (s *Store) Has(key string) bool {
	name, ok := s.index[key]
	if !ok {
		return false
	}
	_, err := os.Stat(filepath.Join(s.root, "objects", name))
	return err == nil
}

// Save stores the declared output files (relative to workdir) under key.
func (s *Store) Save(key, workdir string, outputGlobs []string) error {
	files, err := expandGlobs(workdir, outputGlobs)
	if err != nil {
		return err
	}
	obj := filepath.Join(s.root, "objects", key+".tar")
	f, err := os.Create(obj)
	if err != nil {
		return err
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	for _, file := range files {
		if err := addFile(tw, workdir, file); err != nil {
			tw.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	s.index[key] = key + ".tar"
	return s.flush()
}

// Restore extracts cached outputs for key into workdir.
func (s *Store) Restore(key, workdir string) error {
	name, ok := s.index[key]
	if !ok {
		return nil
	}
	f, err := os.Open(filepath.Join(s.root, "objects", name))
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		dest := filepath.Join(workdir, hdr.Name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode).Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
		// Ensure the mode is applied even if the file pre-existed with other perms.
		_ = os.Chmod(dest, os.FileMode(hdr.Mode).Perm())
	}
	return nil
}

func addFile(tw *tar.Writer, workdir, file string) error {
	fi, err := os.Stat(file)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(workdir, file)
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: rel, Mode: int64(fi.Mode().Perm()), Size: fi.Size()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	in, err := os.Open(file)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(tw, in)
	return err
}

func (s *Store) flush() error {
	// json.MarshalIndent over a map[string]string cannot fail, so the error is
	// intentionally not checked here.
	b, _ := json.MarshalIndent(s.index, "", "  ")
	return os.WriteFile(s.indexFP, b, 0o644)
}
