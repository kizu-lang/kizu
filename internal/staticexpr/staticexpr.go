// Package staticexpr evaluates the integer expression a static argument may
// be written as. A static argument travels through the compiler as text, the
// way a type argument does, so the parser records an expression in one
// canonical spelling -- every operation in its own parentheses, as in
// `((n1 * k2) % 24)` -- and each checker evaluates that text against the
// compile-time names in force where the call is written.
package staticexpr

import (
	"fmt"
	"strconv"
	"strings"
)

// Is reports whether a static argument is an expression rather than a name,
// a literal, or a type. Only an expression starts with a parenthesis.
func Is(text string) bool {
	return strings.HasPrefix(text, "(")
}

// Eval evaluates canonical expression text. lookup answers the integer a name
// stands for; a name it does not know is not a compile-time value, and that
// is the error.
func Eval(text string, lookup func(name string) (int64, bool)) (int64, error) {
	e := evaluator{text: text, lookup: lookup}
	value, err := e.term()
	if err != nil {
		return 0, err
	}
	if e.pos != len(e.text) {
		return 0, fmt.Errorf("unexpected `%s` in static expression `%s`", e.text[e.pos:], text)
	}
	return value, nil
}

type evaluator struct {
	text   string
	pos    int
	lookup func(string) (int64, bool)
}

// term reads one operand: a literal, a name, a negation, or a parenthesized
// binary operation. The canonical spelling needs no precedence: each
// operation carries its own parentheses.
func (e *evaluator) term() (int64, error) {
	if e.pos >= len(e.text) {
		return 0, fmt.Errorf("static expression `%s` ends early", e.text)
	}
	switch c := e.text[e.pos]; {
	case c == '(':
		return e.operation()
	case c == '-':
		e.pos++
		value, err := e.term()
		return -value, err
	case c >= '0' && c <= '9':
		start := e.pos
		for e.pos < len(e.text) && e.text[e.pos] >= '0' && e.text[e.pos] <= '9' {
			e.pos++
		}
		return strconv.ParseInt(e.text[start:e.pos], 10, 64)
	case isNameByte(c):
		start := e.pos
		for e.pos < len(e.text) && isNameByte(e.text[e.pos]) {
			e.pos++
		}
		name := e.text[start:e.pos]
		value, ok := e.lookup(name)
		if !ok {
			return 0, fmt.Errorf("`%s` is not a compile-time integer", name)
		}
		return value, nil
	default:
		return 0, fmt.Errorf("unexpected `%c` in static expression `%s`", c, e.text)
	}
}

// operation reads `(left op right)`.
func (e *evaluator) operation() (int64, error) {
	e.pos++
	left, err := e.term()
	if err != nil {
		return 0, err
	}
	if e.pos+3 > len(e.text) || e.text[e.pos] != ' ' || e.text[e.pos+2] != ' ' {
		return 0, fmt.Errorf("malformed static expression `%s`", e.text)
	}
	op := e.text[e.pos+1]
	e.pos += 3
	right, err := e.term()
	if err != nil {
		return 0, err
	}
	if e.pos >= len(e.text) || e.text[e.pos] != ')' {
		return 0, fmt.Errorf("malformed static expression `%s`", e.text)
	}
	e.pos++
	return apply(op, left, right)
}

// apply performs one binary operation; division and modulo by zero are an
// error rather than a compile-time panic.
func apply(op byte, left int64, right int64) (int64, error) {
	switch op {
	case '+':
		return left + right, nil
	case '-':
		return left - right, nil
	case '*':
		return left * right, nil
	case '/', '%':
		if right == 0 {
			return 0, fmt.Errorf("static expression divides by zero")
		}
		if op == '/' {
			return left / right, nil
		}
		return left % right, nil
	default:
		return 0, fmt.Errorf("static expression operator `%c` is not integer arithmetic", op)
	}
}

// isNameByte reports whether c may appear in a compile-time name.
func isNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
