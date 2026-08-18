package security

import (
	"strings"
	"testing"

	"github.com/chris/shiphappens/internal/compiler"
)

func b(v bool) *bool { return &v }

func TestResolvePrecedence(t *testing.T) {
	offline := &compiler.SecurityPolicy{OfflineByDefault: true}
	defAllow := &compiler.SecurityPolicy{DefaultAllow: []string{"reg.example"}}

	cases := []struct {
		name   string
		policy *compiler.SecurityPolicy
		job    *compiler.JobPlan
		want   NetMode
	}{
		{"explicit network=false wins", offline, &compiler.JobPlan{Network: b(false)}, NetNone},
		{"job allow-list opts in", offline, &compiler.JobPlan{Allow: []string{"h"}}, NetAllow},
		{"explicit network=true", offline, &compiler.JobPlan{Network: b(true)}, NetDefault},
		{"offline-by-default", offline, &compiler.JobPlan{}, NetNone},
		{"policy default-allow", defAllow, &compiler.JobPlan{}, NetAllow},
		{"no policy", nil, &compiler.JobPlan{}, NetDefault},
		{"network=false beats allow", offline, &compiler.JobPlan{Network: b(false), Allow: []string{"h"}}, NetNone},
	}
	for _, c := range cases {
		got := Resolve(c.policy, c.job)
		if got.Mode != c.want {
			t.Errorf("%s: got mode %d, want %d", c.name, got.Mode, c.want)
		}
	}
}

func TestResolveAllowValues(t *testing.T) {
	d := Resolve(nil, &compiler.JobPlan{Allow: []string{"a.com", "b.com"}})
	if d.Mode != NetAllow || len(d.Allow) != 2 {
		t.Fatalf("allow not carried: %+v", d)
	}
	d = Resolve(&compiler.SecurityPolicy{DefaultAllow: []string{"x"}}, &compiler.JobPlan{})
	if d.Mode != NetAllow || d.Allow[0] != "x" {
		t.Fatalf("default-allow not used: %+v", d)
	}
}

func TestSanitizeEnvValue(t *testing.T) {
	cases := map[string]string{
		"normal title":                 "normal title",
		"$(rm -rf /)":                   "(rm -rf /)",       // $ stripped
		"`whoami`":                      "whoami",           // backticks stripped
		"line1\nline2; evil":           "line1 line2; evil", // newline → space
		"a\\b":                         "ab",               // backslash stripped
		"  trim me  ":                  "trim me",
		"tab\there":                    "tab here",
	}
	for in, want := range cases {
		if got := SanitizeEnvValue(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	// control characters dropped
	if strings.ContainsRune(SanitizeEnvValue("a\x00\x07b"), '\x00') {
		t.Error("control chars should be dropped")
	}
}

func TestSanitizeMap(t *testing.T) {
	out := SanitizeMap(map[string]string{"PR_TITLE": "$(evil)", "OK": "fine"})
	if out["PR_TITLE"] != "(evil)" || out["OK"] != "fine" {
		t.Fatalf("SanitizeMap wrong: %+v", out)
	}
}

func TestIsSafeIdentifier(t *testing.T) {
	safe := []string{"main", "feature/x-1", "v1.2.3", "release_2024"}
	for _, s := range safe {
		if !IsSafeIdentifier(s) {
			t.Errorf("%q should be safe", s)
		}
	}
	unsafe := []string{"", "a b", "$(x)", "a;b", "a`b", "a\nb"}
	for _, s := range unsafe {
		if IsSafeIdentifier(s) {
			t.Errorf("%q should be unsafe", s)
		}
	}
}
