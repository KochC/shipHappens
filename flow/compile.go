package flow

import (
	"strings"

	"github.com/KochC/shipHappens/internal/compiler"
	"github.com/KochC/shipHappens/internal/validator"
)

// CompileError wraps validation diagnostics.
type CompileError struct {
	Diags []validator.Diagnostic
}

func (e *CompileError) Error() string {
	var b strings.Builder
	b.WriteString("compilation failed:\n")
	for _, d := range e.Diags {
		b.WriteString("  " + strings.ReplaceAll(d.String(), "\n", "\n  ") + "\n")
	}
	return b.String()
}

// compile validates a raw plan (with source locations) and returns it, or a
// *CompileError if any diagnostics were produced.
func compile(p *compiler.RunPlan, lines map[string]string) (*compiler.RunPlan, error) {
	diags := validator.Validate(p, lines)
	if len(diags) > 0 {
		return nil, &CompileError{Diags: diags}
	}
	return p, nil
}
