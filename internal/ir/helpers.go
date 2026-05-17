package ir

import (
	"fmt"
	"strings"
)

// newBlock appends a basic block to the current function.
func (l *lowerer) newBlock(name string) *Block {
	block := &Block{Name: name}
	l.current.Blocks = append(l.current.Blocks, block)
	return block
}

// nextBlockName creates a stable block label.
func (l *lowerer) nextBlockName(prefix string) string {
	l.nextBlock++
	return fmt.Sprintf("%s.%d", prefix, l.nextBlock)
}

// next creates a fresh SSA value.
func (l *lowerer) next(typ string) Value {
	l.nextValue++
	return Value{Name: fmt.Sprintf("%%%d", l.nextValue), Type: typ}
}

// emitConst appends a typed constant instruction.
func (l *lowerer) emitConst(typ string, immediate string) Value {
	return l.emit("const", typ, nil, immediate)
}

// emit appends a generic instruction.
func (l *lowerer) emit(op string, typ string, args []Value, immediate string) Value {
	result := l.next(typ)
	instr := &Instr{Result: result, Op: op, Args: args, Immediate: immediate}
	l.block.Instrs = append(l.block.Instrs, instr)
	return result
}

// returnType normalizes omitted function returns to void.
func returnType(name string) string {
	if name == "" {
		return "void"
	}
	return name
}

// binaryResultType returns the type produced by a binary operator.
func binaryResultType(op string, left string) string {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return "bool"
	default:
		return left
	}
}

// fieldType resolves a struct field type.
func (l *lowerer) fieldType(structName string, fieldName string) string {
	st, ok := l.module.Structs[structName]
	if !ok {
		return "unknown"
	}
	for _, field := range st.Fields {
		if field.Name == fieldName {
			return field.Type
		}
	}
	return "unknown"
}

// handleType returns handle<T> for arena<T>.
func handleType(arena string) string {
	elem := arenaElementType(arena)
	if elem == "unknown" {
		return "handle<unknown>"
	}
	return "handle<" + elem + ">"
}

// arenaElementType returns T for arena<T>.
func arenaElementType(arena string) string {
	if !strings.HasPrefix(arena, "arena<") || !strings.HasSuffix(arena, ">") {
		return "unknown"
	}
	return strings.TrimSuffix(strings.TrimPrefix(arena, "arena<"), ">")
}

// containerElementType returns T for std collection shapes such as Array<T>.
func containerElementType(container string) string {
	start := strings.Index(container, "<")
	if start < 0 || !strings.HasSuffix(container, ">") {
		return "unknown"
	}
	return strings.TrimSuffix(container[start+1:], ">")
}

// errorUnionElementType returns T for !T.
func errorUnionElementType(result string) string {
	if !strings.HasPrefix(result, "!") || len(result) == 1 {
		return "unknown"
	}
	return strings.TrimPrefix(result, "!")
}

// namespaceConstType returns the enum-like type for a flattened namespace value.
func namespaceConstType(name string) (string, bool) {
	idx := strings.LastIndex(name, ".")
	if idx <= 0 || idx == len(name)-1 {
		return "", false
	}
	return name[:idx], true
}

// builtinReturnType returns the IR type of selected compiler-needed builtins.
func builtinReturnType(name string, _ []Value) (string, bool) {
	switch name {
	case "std.mem.page_allocator":
		return "Allocator", true
	case "std.mem.len":
		return "i64", true
	case "std.mem.byte_at":
		return "!u8", true
	case "std.mem.equal_bytes", "std.mem.starts_with":
		return "bool", true
	case "std.mem.slice":
		return "![]const u8", true
	case "std.mem.trim_ascii":
		return "[]const u8", true
	case "std.testing.expect":
		return "!void", true
	}
	if strings.HasPrefix(name, "std.") {
		return "void", true
	}
	return "", false
}
