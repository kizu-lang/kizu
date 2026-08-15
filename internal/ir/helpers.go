package ir

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/typ"
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

// emit appends a generic instruction. The result type is resolved against the
// instantiation in force, so a type parameter never reaches the backend: every
// instruction in a generic body carries the type its instance was built for.
func (l *lowerer) emit(op string, typ string, args []Value, immediate string) Value {
	result := l.next(l.resolveType(typ))
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
	case "and", "or", "==", "!=", "<", "<=", ">", ">=":
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

// arrayElementType returns T for std::array::Array<T>.
func arrayElementType(name string) (string, bool) {
	const prefix = "std::array::Array<"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ">") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, prefix), ">"), true
}

// mapValueType returns V for the supported std::map::Map<[]u8, V> shape.
func mapValueType(name string) (string, bool) {
	const prefix = "std::map::Map<"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ">") {
		return "", false
	}
	args := splitStaticArgs(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ">"))
	if len(args) != 2 || args[0] != "[]u8" {
		return "", false
	}
	return args[1], true
}

// splitStaticArgs splits a static argument list, and reports no arguments for a
// spelling that is not one. Every caller checks the count it wanted, so a
// malformed list falls through the same path a wrong count does.
func splitStaticArgs(args string) []string {
	parts, err := typ.SplitArgs(args)
	if err != nil {
		return nil
	}
	return parts
}

// mapTypeName returns std::map::Map<[]u8, V>.
func mapTypeName(typeArg string) (string, string, bool) {
	args := splitStaticArgs(typeArg)
	if len(args) != 2 || args[0] != "[]u8" {
		return "", "", false
	}
	valueType := args[1]
	return "std::map::Map<[]u8, " + valueType + ">", valueType, true
}

// isReferenceType reports whether a type is a local borrow.
func isReferenceType(name string) bool {
	return strings.HasPrefix(name, "&") || strings.HasPrefix(name, "&var ")
}

// isMutableReferenceType reports whether name is `&var T`, the borrow a write
// can go through.
func isMutableReferenceType(name string) bool {
	return strings.HasPrefix(name, "&var ")
}

// derefType returns T for &T and &var T.
func derefType(name string) string {
	if strings.HasPrefix(name, "&var ") {
		return strings.TrimPrefix(name, "&var ")
	}
	if strings.HasPrefix(name, "&") {
		return strings.TrimPrefix(name, "&")
	}
	return name
}

// implMethodName returns the symbol used for a concrete impl method.
func implMethodName(typeName string, method string) string {
	return typeName + "." + method
}

// runtimeBuiltinReturnType records checked host-runtime builtin result types.
func runtimeBuiltinReturnType(name string) (string, bool) {
	switch name {
	case "std::internal::builtin::io_blocking", "std::internal::builtin::io_failing":
		return "Io", true
	case "std::internal::builtin::fs_read_file":
		return "std::fs::Error![]u8", true
	case "std::internal::builtin::fs_write_file",
		"std::internal::builtin::fs_create_dir",
		"std::internal::builtin::fs_rename",
		"std::internal::builtin::fs_remove_dir",
		"std::internal::builtin::fs_remove_file":
		return "std::fs::Error!void", true
	case "std::internal::builtin::fs_exists":
		return "std::fs::Error!bool", true
	case "std::internal::builtin::fs_metadata":
		return "std::fs::Error!std::fs::Metadata", true
	case "std::internal::builtin::fs_read_dir":
		return "std::fs::Error!std::array::Array<std::fs::DirEntry>", true
	default:
		return "", false
	}
}

// handleType returns std::arena::Handle<T> for std::arena::Arena<T>.
func handleType(arena string) string {
	elem := arenaElementType(arena)
	if elem == "unknown" {
		return "std::arena::Handle<unknown>"
	}
	return "std::arena::Handle<" + elem + ">"
}

// arenaElementType returns T for std::arena::Arena<T>.
func arenaElementType(arena string) string {
	const prefix = "std::arena::Arena<"
	if !strings.HasPrefix(arena, prefix) || !strings.HasSuffix(arena, ">") {
		return "unknown"
	}
	return strings.TrimSuffix(strings.TrimPrefix(arena, prefix), ">")
}

// errorUnionElementType returns T for !T or Error!T.
func errorUnionElementType(result string) string {
	success, ok := errorUnionSuccessType(result)
	if !ok {
		return "unknown"
	}
	return success
}

// errorUnionSuccessType returns T for !T or Error!T.
func errorUnionSuccessType(result string) (string, bool) {
	_, success, ok := errorUnionParts(result)
	return success, ok
}

// errorUnionParts returns Error and T for Error!T, or empty Error and T for !T.
func errorUnionParts(result string) (string, string, bool) {
	return typ.ErrorUnionParts(result)
}
