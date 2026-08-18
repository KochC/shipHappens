package security

import "strings"

// SanitizeEnvValue makes an untrusted string safe to expose as an environment
// variable value that will later appear in shell commands. It neutralizes the
// characters attackers use to break out of a quoted context or inject commands:
// backticks, $, command substitution, and control characters. Newlines are
// collapsed to spaces so a multi-line PR title can't smuggle extra commands.
//
// This is defense-in-depth: the *correct* pattern is to reference untrusted
// values only via environment variables (never interpolate them into the
// command string), but sanitizing the value blunts injection even if a pipeline
// author interpolates it by mistake.
func SanitizeEnvValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20: // other control characters
			// drop
		case r == '`' || r == '$' || r == '\\':
			// strip shell-active metacharacters
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// SanitizeMap returns a copy of env with every value sanitized. Keys are left
// as-is (they come from the trusted pipeline definition, not user input).
func SanitizeMap(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = SanitizeEnvValue(v)
	}
	return out
}

// IsSafeIdentifier reports whether s is a conservative, injection-free token
// (letters, digits, and -._/ only) — useful for validating things like branch
// names or tags before using them in paths or arguments.
func IsSafeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' || r == '/'
		if !ok {
			return false
		}
	}
	return true
}
