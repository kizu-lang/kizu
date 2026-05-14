package interp

import "fmt"

type valueKind int

const (
	kindVoid valueKind = iota
	kindInt
	kindBool
	kindString
	kindStruct
	kindArena
	kindHandle
	kindResult
	kindEnum
)

// Value is a runtime value produced by the Phase 2 interpreter.
type Value struct {
	kind   valueKind
	i      int64
	b      bool
	s      string
	fields map[string]Value
	arena  *Arena
	handle Handle
	result *Result
	enum   Enum
}

// Arena stores values and gives out opaque handles.
type Arena struct {
	values []Value
}

// Handle identifies an arena element without exposing a raw pointer.
type Handle struct {
	arena *Arena
	index int
}

// Result stores a success or error value for result<T>.
type Result struct {
	ok      bool
	value   Value
	message string
}

// Enum stores a Zig/C-style enum tag value.
type Enum struct {
	typeName string
	tag      string
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
	case kindStruct:
		return "<struct>"
	case kindArena:
		return "<arena>"
	case kindHandle:
		return "<handle>"
	case kindResult:
		if v.result.ok {
			return "<ok>"
		}
		return "<error: " + v.result.message + ">"
	case kindEnum:
		return v.enum.typeName + "." + v.enum.tag
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

// structValue returns a runtime struct value.
func structValue(fields map[string]Value) Value {
	return Value{kind: kindStruct, fields: fields}
}

// arenaValue returns an empty runtime arena.
func arenaValue() Value {
	return Value{kind: kindArena, arena: &Arena{}}
}

// handleValue returns an opaque handle runtime value.
func handleValue(arena *Arena, index int) Value {
	return Value{kind: kindHandle, handle: Handle{arena: arena, index: index}}
}

// resultOkValue returns a successful result runtime value.
func resultOkValue(value Value) Value {
	return Value{kind: kindResult, result: &Result{ok: true, value: value}}
}

// resultErrorValue returns an error result runtime value.
func resultErrorValue(message string) Value {
	return Value{kind: kindResult, result: &Result{message: message}}
}

// enumValue returns a tag enum runtime value.
func enumValue(typeName string, tag string) Value {
	return Value{kind: kindEnum, enum: Enum{typeName: typeName, tag: tag}}
}
