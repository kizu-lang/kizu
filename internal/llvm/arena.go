package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeArenaRuntimeDecls writes declarations for the hosted Arena runtime.
func (e *emitter) writeArenaRuntimeDecls() {
	if !e.usesArenaRuntime() {
		return
	}
	e.out.WriteString("declare ptr @kizu_arena_new(i64)\n")
	e.out.WriteString("declare i64 @kizu_arena_add(ptr, ptr)\n")
	e.out.WriteString("declare ptr @kizu_arena_get(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_arena_deinit(ptr)\n\n")
}

// usesArenaRuntime reports whether this module uses std::arena::Arena lowering.
func (e *emitter) usesArenaRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if strings.HasPrefix(instr.Op, "arena.") {
					return true
				}
			}
		}
	}
	return false
}

// writeArenaInstr dispatches runtime-backed Arena operations.
func (e *emitter) writeArenaInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "arena.new":
		return e.writeArenaNew(instr)
	case "arena.add":
		return e.writeArenaAdd(instr)
	case "arena.get":
		return e.writeArenaGet(instr)
	case "arena.deinit":
		return e.writeArenaDeinit(instr)
	default:
		return fmt.Errorf("llvm error: unsupported arena instruction `%s`", instr.Op)
	}
}

// writeArenaNew lowers std::arena::Arena<T>(allocator).
func (e *emitter) writeArenaNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || !isArenaLLVMType(instr.Result.Type) {
		return fmt.Errorf("llvm error: arena.new expects allocator -> Arena<T>")
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_arena_new(i64 %s)\n",
		resultName, e.elementSizeOperand(instr.Immediate))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaAdd lowers Arena.add(value) to an opaque i64 handle.
func (e *emitter) writeArenaAdd(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Result.Type) {
		return fmt.Errorf("llvm error: arena.add expects Arena<T>, T -> Handle<T>")
	}
	arena := e.value(instr.Args[0])
	valueSlot := e.writeStackValue(localName(instr.Result.Name)+".value", instr.Args[1])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = call i64 @kizu_arena_add(ptr %s, ptr %s)\n",
		resultName, arena.operand, valueSlot)
	badName := resultName + ".bad"
	fmt.Fprintf(&e.out, "  %s = icmp slt i64 %s, 0\n", badName, resultName)
	e.writeBoolTrapInverse(badName, "arena.add")
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaGet lowers Arena.get(handle) to a checked copy load.
func (e *emitter) writeArenaGet(instr *ir.Instr) error {
	if len(instr.Args) != 2 || !isArenaHandleType(instr.Args[1].Type) {
		return fmt.Errorf("llvm error: arena.get expects Arena<T>, Handle<T> -> T")
	}
	arena := e.value(instr.Args[0])
	handle := e.value(instr.Args[1])
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = call ptr @kizu_arena_get(ptr %s, i64 %s)\n",
		ptrName, arena.operand, handle.operand)
	e.writeNullTrap(ptrName, "arena.get")
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n",
		resultName, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeArenaDeinit lowers Arena.deinit().
func (e *emitter) writeArenaDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: arena.deinit expects Arena<T> -> void")
	}
	arena := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "  call void @kizu_arena_deinit(ptr %s)\n", arena.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeBoolTrapInverse traps when badOperand is true.
func (e *emitter) writeBoolTrapInverse(badOperand string, prefix string) {
	trapLabel := helperLabel(badOperand, prefix+".fail")
	okLabel := helperLabel(badOperand, "ok")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", badOperand, trapLabel, okLabel)
	e.writeTrapBlock(trapLabel)
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
}

// isArenaLLVMType reports whether a lowered IR type is a std::arena::Arena<T>.
func isArenaLLVMType(typ string) bool {
	return strings.HasPrefix(typ, "std::arena::Arena<") && strings.HasSuffix(typ, ">")
}
