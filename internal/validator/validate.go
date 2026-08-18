package validator

import (
	"fmt"
	"sort"

	"github.com/chris/shiphappens/internal/compiler"
)

// Diagnostic is a single validation problem.
type Diagnostic struct {
	Loc string // "file.go:NN" if known
	Msg string
}

func (d Diagnostic) String() string {
	if d.Loc != "" && d.Loc != "?" {
		return fmt.Sprintf("%s\n  %s", d.Loc, d.Msg)
	}
	return d.Msg
}

// Validate runs all structural checks over a raw plan. lines maps jobID -> loc.
func Validate(p *compiler.RunPlan, lines map[string]string) []Diagnostic {
	var diags []Diagnostic
	loc := func(id string) string { return lines[id] }

	if len(p.Jobs) == 0 {
		diags = append(diags, Diagnostic{Msg: "workflow has no jobs"})
		return diags
	}

	// Duplicate job IDs + build id set.
	seen := map[string]bool{}
	ids := make([]string, 0, len(p.Jobs))
	for _, j := range p.Jobs {
		if seen[j.ID] {
			diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: fmt.Sprintf("duplicate job id %q", j.ID)})
		}
		seen[j.ID] = true
		ids = append(ids, j.ID)
	}

	// Per-job structural checks.
	for _, j := range p.Jobs {
		if j.RunsOn == "" {
			diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: fmt.Sprintf("job %q has no runs-on", j.ID)})
		}
		if len(j.Steps) == 0 {
			diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: fmt.Sprintf("job %q has no steps", j.ID)})
		}
		for _, s := range j.Steps {
			if s.Run == "" {
				diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: fmt.Sprintf("job %q step %q has no run command", j.ID, s.ID)})
			}
		}
		for _, sec := range j.Secrets {
			if sec.Name == "" {
				diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: fmt.Sprintf("job %q has a secret with an empty name", j.ID)})
			}
		}
		// needs resolution + self-ref.
		for _, n := range j.Needs {
			if n == j.ID {
				diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: fmt.Sprintf("job %q needs itself", j.ID)})
				continue
			}
			if !seen[n] {
				msg := fmt.Sprintf("job %q needs %q — unknown job", j.ID, n)
				if sug := suggest(n, ids); sug != "" {
					msg += fmt.Sprintf("\n  did you mean: %s", sug)
				}
				diags = append(diags, Diagnostic{Loc: loc(j.ID), Msg: msg})
			}
		}
	}

	// Cycle detection (only meaningful if needs resolve; still safe otherwise).
	if cyc := findCycle(p); cyc != nil {
		diags = append(diags, Diagnostic{Msg: "dependency cycle: " + joinArrow(cyc)})
	}

	return diags
}

// suggest returns the closest candidate by edit distance (<=2), or "".
func suggest(want string, candidates []string) string {
	best, bestD := "", 3
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)
	for _, c := range sorted {
		if d := levenshtein(want, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func joinArrow(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " -> "
		}
		out += v
	}
	return out
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur := make([]int, lb+1)
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
