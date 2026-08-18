package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempPlan writes a JSON plan file and returns its path.
func writeTempPlan(t *testing.T, json string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(f, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}
