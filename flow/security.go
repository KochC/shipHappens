package flow

import "github.com/KochC/shipHappens/internal/security"

// Sanitize neutralizes shell-injection metacharacters in an untrusted string
// (e.g. a PR title, commit message, or branch name) before it is exposed to a
// step. Prefer passing untrusted values via Env/StepEnv (referenced as $VAR in
// commands, never interpolated into the command string); Sanitize is
// defense-in-depth for when they are interpolated anyway.
func Sanitize(s string) string { return security.SanitizeEnvValue(s) }

// SafeIdentifier reports whether s is a conservative, injection-free token
// (letters, digits, and -._/), useful for validating branch names or tags.
func SafeIdentifier(s string) bool { return security.IsSafeIdentifier(s) }
