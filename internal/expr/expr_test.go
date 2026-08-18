package expr

import "testing"

func ctx() Context {
	vals := map[string]any{
		"env.BRANCH":         "main",
		"env.EMPTY":          "",
		"outputs.build.ver":  "1.2.3",
		"needs.test.result":  "success",
		"flag":               true,
	}
	return Context{
		Lookup: func(path []string) (any, bool) {
			v, ok := vals[joinPath(path)]
			return v, ok
		},
		Success: true,
		Failure: false,
	}
}

func joinPath(p []string) string {
	s := ""
	for i, x := range p {
		if i > 0 {
			s += "."
		}
		s += x
	}
	return s
}

func mustEval(t *testing.T, src string, c Context) bool {
	t.Helper()
	b, err := Eval(src, c)
	if err != nil {
		t.Fatalf("Eval(%q) error: %v", src, err)
	}
	return b
}

func TestLiteralsAndTruthiness(t *testing.T) {
	c := ctx()
	cases := map[string]bool{
		"true":       true,
		"false":      false,
		"'hello'":    true,
		"''":         false,
		"'false'":    false,
		"'0'":        false,
		"1":          true,
		"0":          false,
	}
	for src, want := range cases {
		if got := mustEval(t, src, c); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
}

func TestEqualityAndLogic(t *testing.T) {
	c := ctx()
	cases := map[string]bool{
		"env.BRANCH == 'main'":                true,
		"env.BRANCH == 'dev'":                 false,
		"env.BRANCH != 'dev'":                 true,
		"env.BRANCH == 'main' && flag":        true,
		"env.BRANCH == 'dev' || flag":         true,
		"env.BRANCH == 'dev' && flag":         false,
		"!(env.BRANCH == 'dev')":              true,
		"!flag":                               false,
		"outputs.build.ver == '1.2.3'":        true,
		"needs.test.result == 'success'":      true,
	}
	for src, want := range cases {
		if got := mustEval(t, src, c); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
}

func TestFunctions(t *testing.T) {
	c := ctx() // Success=true, Failure=false
	if !mustEval(t, "success()", c) {
		t.Error("success() should be true")
	}
	if mustEval(t, "failure()", c) {
		t.Error("failure() should be false")
	}
	if !mustEval(t, "always()", c) {
		t.Error("always() should be true")
	}
	// combined
	if !mustEval(t, "success() && env.BRANCH == 'main'", c) {
		t.Error("combined success gate failed")
	}
}

func TestMissingIdentIsFalsey(t *testing.T) {
	c := ctx()
	if mustEval(t, "env.NOPE", c) {
		t.Error("missing identifier should be falsey")
	}
	if !mustEval(t, "env.NOPE == ''", c) {
		t.Error("missing identifier should equal empty string")
	}
}

func TestNumberEqualityCrossType(t *testing.T) {
	c := ctx()
	if !mustEval(t, "1 == '1'", c) {
		t.Error("1 == '1' should be true (string comparison)")
	}
}

func TestPrecedence(t *testing.T) {
	c := ctx()
	// && binds tighter than ||
	if !mustEval(t, "false && false || true", c) {
		t.Error("precedence: (false&&false)||true should be true")
	}
	if mustEval(t, "true && false || false", c) {
		t.Error("precedence wrong")
	}
}

func TestErrors(t *testing.T) {
	c := ctx()
	bad := []string{
		"env. == 'x'",     // ident after dot missing
		"(true",           // missing paren
		"'unterminated",   // bad string
		"1 +",             // unexpected op
		"@bad",            // bad char
		"foo(",            // call missing close
		"nope()",          // unknown function
		"true true",       // trailing token
	}
	for _, src := range bad {
		if _, err := Eval(src, c); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}

func TestEvalValueRaw(t *testing.T) {
	c := ctx()
	v, err := EvalValue("outputs.build.ver", c)
	if err != nil || v != "1.2.3" {
		t.Fatalf("EvalValue = %v, %v", v, err)
	}
}

func TestEmptyExpression(t *testing.T) {
	if _, err := Eval("", ctx()); err == nil {
		t.Error("empty expression should error")
	}
}

func TestNumberValueAndToStr(t *testing.T) {
	c := ctx()
	// numeric value round-trips and compares as its formatted string
	if !mustEval(t, "1.5 == '1.5'", c) {
		t.Error("float formatting mismatch")
	}
	// bool value equality
	if !mustEval(t, "flag == 'true'", c) {
		t.Error("bool toStr should be 'true'")
	}
}

func TestTruthyNumberAndBoolValue(t *testing.T) {
	// truthy over a raw float and bool via Lookup
	c := Context{Lookup: func(p []string) (any, bool) {
		switch joinPath(p) {
		case "n":
			return float64(3), true
		case "z":
			return float64(0), true
		case "b":
			return true, true
		case "other":
			return []int{1}, true // default branch of truthy → true
		}
		return nil, false
	}}
	if !mustEval(t, "n", c) {
		t.Error("nonzero number truthy")
	}
	if mustEval(t, "z", c) {
		t.Error("zero number falsey")
	}
	if !mustEval(t, "b", c) {
		t.Error("bool true truthy")
	}
	if !mustEval(t, "other", c) {
		t.Error("non-nil default truthy")
	}
}

func TestErrorPropagationNested(t *testing.T) {
	c := ctx()
	// errors inside each precedence level's right operand
	bad := []string{
		"true || @",
		"true && @",
		"1 == @",
		"1 != @",
		"!@",
		"(@)",
	}
	for _, src := range bad {
		if _, err := Eval(src, c); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}

func TestInvalidNumber(t *testing.T) {
	if _, err := Eval("1.2.3.4", ctx()); err == nil {
		t.Error("malformed number should error")
	}
}

func TestErrorLeftOperand(t *testing.T) {
	c := ctx()
	bad := []string{
		"@ || true",
		"@ && true",
		"@ == 1",
		"@",         // unary left
	}
	for _, src := range bad {
		if _, err := Eval(src, c); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}

func TestToStrNilAndDefault(t *testing.T) {
	// nil via missing lookup compared to empty; default via slice value
	c := Context{Lookup: func(p []string) (any, bool) {
		if joinPath(p) == "slice" {
			return []int{1, 2}, true
		}
		return nil, false
	}}
	// slice value stringified in equality (default branch of toStr)
	if mustEval(t, "slice == 'x'", c) {
		t.Error("slice should not equal 'x'")
	}
}

func TestNotEqualRightError(t *testing.T) {
	if _, err := Eval("1 != @", ctx()); err == nil {
		t.Error("expected error for '1 != @'")
	}
}

func TestNilTruthyAndToStr(t *testing.T) {
	// A lookup returning nil explicitly exercises truthy(nil) and toStr(nil).
	c := Context{Lookup: func(p []string) (any, bool) {
		if joinPath(p) == "nilval" {
			return nil, true
		}
		return nil, false
	}}
	if mustEval(t, "nilval", c) {
		t.Error("nil value should be falsey")
	}
	if !mustEval(t, "nilval == ''", c) {
		t.Error("nil value should stringify to empty")
	}
}
