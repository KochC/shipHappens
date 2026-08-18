// Package secrets resolves secret values from the host environment, provides a
// log masker that redacts them from output, and computes the effective
// environment for a job (workflow vars + job env + resolved secrets).
package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"

	"github.com/chris/shiphappens/internal/compiler"
)

// Resolver reads secret values from the host process environment.
type Resolver struct {
	lookup func(string) (string, bool) // overridable for tests
}

// New returns a Resolver backed by the OS environment.
func New() *Resolver { return &Resolver{lookup: os.LookupEnv} }

// NewWith returns a Resolver with a custom lookup (tests).
func NewWith(lookup func(string) (string, bool)) *Resolver { return &Resolver{lookup: lookup} }

// Missing returns the names of a job's secrets that are absent from the host
// environment (so callers can fail fast before running anything).
func (r *Resolver) Missing(job *compiler.JobPlan) []string {
	var missing []string
	for _, s := range job.Secrets {
		if _, ok := r.Lookup(s); !ok {
			missing = append(missing, s.Name)
		}
	}
	return missing
}

// Lookup resolves a single secret ref's value from the host env, or ("", false).
func (r *Resolver) Lookup(ref compiler.SecretRef) (string, bool) {
	src := ref.FromEnv
	if src == "" {
		src = ref.Name
	}
	v, ok := r.lookup(src)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Effective computes the environment a job's steps run with: workflow vars
// (lowest precedence), then job env, then resolved secrets (highest). It also
// returns the set of secret values present, for masking.
func (r *Resolver) Effective(vars, jobEnv map[string]string, job *compiler.JobPlan) (env map[string]string, secretValues []string) {
	env = map[string]string{}
	for k, v := range vars {
		env[k] = v
	}
	for k, v := range jobEnv {
		env[k] = v
	}
	for _, s := range job.Secrets {
		if v, ok := r.Lookup(s); ok {
			env[s.Name] = v
			secretValues = append(secretValues, v)
		}
	}
	return env, secretValues
}

// Fingerprint returns a stable, non-reversible token for a secret value, so a
// changed secret invalidates caches without the value ever being stored.
func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte("ship-secret:" + value))
	return hex.EncodeToString(sum[:8])
}

// Masker redacts secret values from text. Longer values are masked first so a
// value that contains another is fully redacted.
type Masker struct{ values []string }

// NewMasker builds a masker for the given secret values (empties ignored).
func NewMasker(values []string) *Masker {
	var vs []string
	for _, v := range values {
		if len(v) >= 4 { // don't redact trivially short values (too noisy)
			vs = append(vs, v)
		}
	}
	sort.Slice(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	return &Masker{values: vs}
}

// Mask replaces every occurrence of a known secret value with "***".
func (m *Masker) Mask(s string) string {
	if m == nil {
		return s
	}
	for _, v := range m.values {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// Active reports whether the masker has anything to redact.
func (m *Masker) Active() bool { return m != nil && len(m.values) > 0 }
