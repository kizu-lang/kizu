package interp

import "fmt"

type valueKind int

const (
	kindVoid valueKind = iota
	kindInt
	kindBool
	kindString
)

// Value is a runtime value produced by the Phase 2 interpreter.
type Value struct {
	kind valueKind
	i    int64
	b    bool
	s    string
}

// String formats a value for the print builtin and test assertions.
func (v Value) String() string {
	switch v.kind {
	case kindVoid:
		return "void"
	case kindInt:
		return fmt.Sprintf("%d", v.i)
	case kindBool:
		if v.b {
			return "true"
		}
		return "false"
	case kindString:
		return v.s
	default:
		return "<invalid>"
	}
}

// voidValue returns the singleton void runtime value.
func voidValue() Value {
	return Value{kind: kindVoid}
}

// intValue returns an integer runtime value.
func intValue(v int64) Value {
	return Value{kind: kindInt, i: v}
}

// boolValue returns a boolean runtime value.
func boolValue(v bool) Value {
	return Value{kind: kindBool, b: v}
}

// stringValue returns a string runtime value.
func stringValue(v string) Value {
	return Value{kind: kindString, s: v}
}
