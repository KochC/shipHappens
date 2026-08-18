package secrets

import (
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func fixedEnv(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLookupAndMissing(t *testing.T) {
	r := NewWith(fixedEnv(map[string]string{"TOKEN": "abcd1234", "EMPTY": ""}))
	job := &compiler.JobPlan{Secrets: []compiler.SecretRef{
		{Name: "TOKEN"},
		{Name: "ALIAS", FromEnv: "TOKEN"},
		{Name: "EMPTY"},  // present but empty → treated as missing
		{Name: "ABSENT"}, // not set
	}}
	missing := r.Missing(job)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing (EMPTY, ABSENT), got %v", missing)
	}

	v, ok := r.Lookup(compiler.SecretRef{Name: "ALIAS", FromEnv: "TOKEN"})
	if !ok || v != "abcd1234" {
		t.Fatalf("alias lookup failed: %q %v", v, ok)
	}
}

func TestEffectivePrecedence(t *testing.T) {
	r := NewWith(fixedEnv(map[string]string{"TOKEN": "s3cretvalue"}))
	vars := map[string]string{"REGION": "eu", "SHARED": "from-vars"}
	jobEnv := map[string]string{"SHARED": "from-job"} // overrides vars
	job := &compiler.JobPlan{Secrets: []compiler.SecretRef{{Name: "TOKEN"}}}

	env, vals := r.Effective(vars, jobEnv, job)
	if env["REGION"] != "eu" {
		t.Error("workflow var missing")
	}
	if env["SHARED"] != "from-job" {
		t.Errorf("job env should override var, got %q", env["SHARED"])
	}
	if env["TOKEN"] != "s3cretvalue" {
		t.Error("secret not resolved into env")
	}
	if len(vals) != 1 || vals[0] != "s3cretvalue" {
		t.Errorf("secret values wrong: %v", vals)
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a := Fingerprint("value-one")
	if a != Fingerprint("value-one") {
		t.Fatal("fingerprint not stable")
	}
	if a == Fingerprint("value-two") {
		t.Fatal("distinct values should differ")
	}
	if strings.Contains(a, "value-one") {
		t.Fatal("fingerprint must not contain the plaintext")
	}
}

func TestMasker(t *testing.T) {
	m := NewMasker([]string{"s3cretvalue", "ab", "longer-secret-token"})
	// short values (<4) are ignored to avoid noise
	out := m.Mask("using s3cretvalue and longer-secret-token here ab")
	if strings.Contains(out, "s3cretvalue") || strings.Contains(out, "longer-secret-token") {
		t.Fatalf("secrets not masked: %q", out)
	}
	if !strings.Contains(out, "ab") {
		t.Error("short value should not be masked")
	}
	if strings.Count(out, "***") != 2 {
		t.Errorf("expected two redactions, got %q", out)
	}
}

func TestMaskerNilAndInactive(t *testing.T) {
	var m *Masker
	if m.Mask("hello") != "hello" {
		t.Error("nil masker should be a no-op")
	}
	if m.Active() {
		t.Error("nil masker inactive")
	}
	empty := NewMasker(nil)
	if empty.Active() {
		t.Error("empty masker inactive")
	}
}
