// Package changed maps git-diff file changes to affected jobs.
package changed

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chris/shiphappens/internal/compiler"
	"github.com/chris/shiphappens/internal/graph"
)

// Files returns files changed vs the given base ref (e.g. "main").
func Files(workdir, base string) ([]string, error) {
	cmd := exec.Command("git", "-C", workdir, "diff", "--name-only", base+"...HEAD")
	out, err := cmd.Output()
	if err != nil {
		// fall back to uncommitted changes
		cmd = exec.Command("git", "-C", workdir, "diff", "--name-only")
		out, err = cmd.Output()
		if err != nil {
			return nil, err
		}
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// AffectedJobs returns the set of jobs affected by changed files, plus their
// transitive dependents. A job is directly affected if any changed file matches
// one of its steps' cache input globs. Jobs without input globs are considered
// always affected (conservative).
func AffectedJobs(plan *compiler.RunPlan, dag *graph.DAG, files []string) map[string]bool {
	direct := map[string]bool{}
	for _, j := range plan.Jobs {
		globs := jobInputGlobs(&j)
		if len(globs) == 0 {
			direct[j.ID] = true // no declared inputs -> assume affected
			continue
		}
		for _, f := range files {
			if matchAny(f, globs) {
				direct[j.ID] = true
				break
			}
		}
	}
	return dag.Affected(direct)
}

func jobInputGlobs(j *compiler.JobPlan) []string {
	var globs []string
	for _, s := range j.Steps {
		if s.Cache != nil {
			globs = append(globs, s.Cache.Inputs...)
		}
	}
	return globs
}

func matchAny(file string, globs []string) bool {
	for _, g := range globs {
		if matchGlob(file, g) {
			return true
		}
	}
	return false
}

// matchGlob supports simple "**" (any path segments) plus filepath.Match semantics.
func matchGlob(file, glob string) bool {
	if strings.Contains(glob, "**") {
		idx := strings.Index(glob, "**")
		prefix := strings.TrimSuffix(glob[:idx], "/")
		suffix := strings.TrimPrefix(glob[idx+2:], "/")
		if prefix != "" && !strings.HasPrefix(file, prefix) {
			return false
		}
		if suffix == "" {
			return true
		}
		ok, _ := filepath.Match(suffix, filepath.Base(file))
		return ok
	}
	ok, _ := filepath.Match(glob, file)
	if ok {
		return true
	}
	ok, _ = filepath.Match(glob, filepath.Base(file))
	return ok
}
