package cache

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Root returns the cache root directory (~/.ship/cache).
func (s *Store) Root() string { return s.root }

// Stats summarizes the on-disk cache.
type Stats struct {
	Objects int
	Bytes   int64
	Oldest  time.Time
	Newest  time.Time
}

// Stat scans the objects directory and returns aggregate statistics.
func (s *Store) Stat() (Stats, error) {
	var st Stats
	objs := filepath.Join(s.root, "objects")
	entries, err := os.ReadDir(objs)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		st.Objects++
		st.Bytes += info.Size()
		mt := info.ModTime()
		if st.Oldest.IsZero() || mt.Before(st.Oldest) {
			st.Oldest = mt
		}
		if mt.After(st.Newest) {
			st.Newest = mt
		}
	}
	return st, nil
}

// PruneResult reports what a prune removed.
type PruneResult struct {
	Removed int
	Bytes   int64
}

// objMeta is an object file with metadata for pruning decisions.
type objMeta struct {
	name  string
	path  string
	size  int64
	mtime time.Time
}

// listObjects returns object files sorted oldest-first (LRU by mtime).
func (s *Store) listObjects() ([]objMeta, int64, error) {
	objs := filepath.Join(s.root, "objects")
	entries, err := os.ReadDir(objs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	var metas []objMeta
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		metas = append(metas, objMeta{name: e.Name(), path: filepath.Join(objs, e.Name()), size: info.Size(), mtime: info.ModTime()})
		total += info.Size()
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].mtime.Before(metas[j].mtime) })
	return metas, total, nil
}

// Prune removes cache objects to satisfy the given limits (0 = no limit):
//   - maxAge: remove objects older than this duration.
//   - maxBytes: after age pruning, if the cache still exceeds this size, remove
//     the oldest objects (LRU) until it fits.
//
// The index is rewritten to drop references to removed objects.
func (s *Store) Prune(maxAge time.Duration, maxBytes int64) (PruneResult, error) {
	metas, total, err := s.listObjects()
	if err != nil {
		return PruneResult{}, err
	}
	removed := map[string]bool{}
	var res PruneResult

	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	// Age-based pass.
	remaining := total
	kept := metas[:0:0]
	for _, m := range metas {
		if maxAge > 0 && m.mtime.Before(cutoff) {
			if err := os.Remove(m.path); err == nil {
				removed[m.name] = true
				res.Removed++
				res.Bytes += m.size
				remaining -= m.size
			}
			continue
		}
		kept = append(kept, m)
	}

	// Size-based (LRU) pass over what remains (already oldest-first).
	if maxBytes > 0 && remaining > maxBytes {
		for _, m := range kept {
			if remaining <= maxBytes {
				break
			}
			if err := os.Remove(m.path); err == nil {
				removed[m.name] = true
				res.Removed++
				res.Bytes += m.size
				remaining -= m.size
			}
		}
	}

	if len(removed) > 0 {
		s.dropIndexRefs(removed)
	}
	return res, nil
}

// PruneAll removes every cached object and empties the index.
func (s *Store) PruneAll() (PruneResult, error) {
	return s.Prune(time.Nanosecond, 0) // everything older than "now" → removed
}

// dropIndexRefs removes index entries pointing at removed object files.
func (s *Store) dropIndexRefs(removed map[string]bool) {
	for k, name := range s.index {
		if removed[name] {
			delete(s.index, k)
		}
	}
	_ = s.flush()
}
