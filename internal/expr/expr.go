// Package expr implements a small, safe expression language for Ship Happens
// `if:` conditions. It is intentionally minimal and has no side effects.
//
// Grammar (precedence low→high):
//
//	or      := and ( "||" and )*
//	and     := equality ( "&&" equality )*
//	equality:= unary ( ( "==" | "!=" ) unary )*
//	unary   := "!" unary | primary
//	primary := literal | ident-path | call | "(" or ")"
//	literal := string | number | "true" | "false"
//	ident-path := IDENT ( "." IDENT )*         (e.g. env.FOO, outputs.build.ver)
//	call    := IDENT "(" ")"                    (success(), failure(), always())
//
// Identifier paths and functions are resolved by a Context supplied at
// evaluation time. Truthiness: bool as-is; non-empty string / non-zero number
// are true; missing identifiers are the empty string (false).
package expr

import (
	"fmt"
	"strconv"
	"strings"
)

// Context resolves identifier paths and predefined functions.
type Context struct {
	// Lookup resolves a dotted path (e.g. ["env","FOO"]) to a value, or (nil,false).
	Lookup func(path []string) (any, bool)
	// Success/Failure report run status for success()/failure()/always().
	Success bool
	Failure bool
}

// Eval parses and evaluates src against ctx, returning its truthiness.
func Eval(src string, ctx Context) (bool, error) {
	v, err := EvalValue(src, ctx)
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// EvalValue parses and evaluates src, returning the raw value.
func EvalValue(src string, ctx Context) (any, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, ctx: ctx}
	v, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unexpected token %q", p.toks[p.pos].val)
	}
	return v, nil
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "" && x != "false" && x != "0"
	case float64:
		return x != 0
	case nil:
		return false
	default:
		return true
	}
}

// ── lexer ──────────────────────────────────────────────────────────────────

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tString
	tNumber
	tOp // == != && || ! ( ) .
	tLParen
	tRParen
)

type token struct {
	kind tokKind
	val  string
}

func lex(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")"})
			i++
		case c == '.':
			toks = append(toks, token{tOp, "."})
			i++
		case c == '!' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, token{tOp, "!="})
			i += 2
		case c == '!':
			toks = append(toks, token{tOp, "!"})
			i++
		case c == '=' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, token{tOp, "=="})
			i += 2
		case c == '&' && i+1 < len(s) && s[i+1] == '&':
			toks = append(toks, token{tOp, "&&"})
			i += 2
		case c == '|' && i+1 < len(s) && s[i+1] == '|':
			toks = append(toks, token{tOp, "||"})
			i += 2
		case c == '\'' || c == '"':
			quote := c
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != quote {
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated string")
			}
			toks = append(toks, token{tString, b.String()})
			i = j + 1
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == '.') {
				j++
			}
			toks = append(toks, token{tNumber, s[i:j]})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			toks = append(toks, token{tIdent, s[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q", string(c))
		}
	}
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '-'
}

// ── parser / evaluator ──────────────────────────────────────────────────────

type parser struct {
	toks []token
	pos  int
	ctx  Context
}

func (p *parser) peek() *token {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *parser) acceptOp(op string) bool {
	if t := p.peek(); t != nil && t.kind == tOp && t.val == op {
		p.pos++
		return true
	}
	return false
}

func (p *parser) parseOr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.acceptOp("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = truthy(left) || truthy(right)
	}
	return left, nil
}

func (p *parser) parseAnd() (any, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.acceptOp("&&") {
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = truthy(left) && truthy(right)
	}
	return left, nil
}

func (p *parser) parseEquality() (any, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		if p.acceptOp("==") {
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = equal(left, right)
		} else if p.acceptOp("!=") {
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = !equal(left, right)
		} else {
			return left, nil
		}
	}
}

func (p *parser) parseUnary() (any, error) {
	if p.acceptOp("!") {
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return !truthy(v), nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (any, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	switch t.kind {
	case tLParen:
		p.pos++
		v, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() == nil || p.peek().kind != tRParen {
			return nil, fmt.Errorf("missing closing paren")
		}
		p.pos++
		return v, nil
	case tString:
		p.pos++
		return t.val, nil
	case tNumber:
		p.pos++
		f, err := strconv.ParseFloat(t.val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", t.val)
		}
		return f, nil
	case tIdent:
		return p.parseIdentOrCall()
	default:
		return nil, fmt.Errorf("unexpected token %q", t.val)
	}
}

func (p *parser) parseIdentOrCall() (any, error) {
	first := p.toks[p.pos].val
	p.pos++

	// literal keywords
	switch first {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}

	// function call: ident "(" ")"
	if p.peek() != nil && p.peek().kind == tLParen {
		p.pos++
		if p.peek() == nil || p.peek().kind != tRParen {
			return nil, fmt.Errorf("function %s takes no arguments", first)
		}
		p.pos++
		switch first {
		case "success":
			return p.ctx.Success, nil
		case "failure":
			return p.ctx.Failure, nil
		case "always":
			return true, nil
		default:
			return nil, fmt.Errorf("unknown function %s()", first)
		}
	}

	// dotted identifier path
	path := []string{first}
	for p.acceptOp(".") {
		t := p.peek()
		if t == nil || t.kind != tIdent {
			return nil, fmt.Errorf("expected identifier after '.'")
		}
		path = append(path, t.val)
		p.pos++
	}
	if p.ctx.Lookup != nil {
		if v, ok := p.ctx.Lookup(path); ok {
			return v, nil
		}
	}
	return "", nil // missing identifier → empty string (falsey)
}

func equal(a, b any) bool {
	return toStr(a) == toStr(b)
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}
