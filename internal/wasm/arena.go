package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

const (
	arenaWasmPrefix   = "std::arena::Arena<"
	arenaHandlePrefix = "std::arena::Handle<"
	arenaHeaderSize   = 32
	arenaOriginOffset = 24
	arenaMaxLen       = 1<<32 - 1
	arenaMaxInstances = 1<<32 - 1
)

// isArenaWasmType reports whether name is a direct Arena storage type.
func isArenaWasmType(name string) bool {
	return strings.HasPrefix(name, arenaWasmPrefix) && strings.HasSuffix(name, ">")
}

// arenaElementWasmType returns T through either direct or borrowed Arena<T>.
func arenaElementWasmType(name string) (string, bool) {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "&var "), "&")
	if !isArenaWasmType(name) {
		return "", false
	}
	return name[len(arenaWasmPrefix) : len(name)-1], true
}

// isArenaHandleWasmType reports whether name is an opaque Arena handle.
func isArenaHandleWasmType(name string) bool {
	return strings.HasPrefix(name, arenaHandlePrefix) && strings.HasSuffix(name, ">")
}

// arenaHandleElementWasmType returns T for a direct Handle<T> value.
func arenaHandleElementWasmType(name string) (string, bool) {
	if !isArenaHandleWasmType(name) {
		return "", false
	}
	return name[len(arenaHandlePrefix) : len(name)-1], true
}

// usesArenaRuntime reports whether this module creates or operates on an Arena.
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

// usesArenaOriginRuntime reports whether this module constructs an Arena.
func (e *emitter) usesArenaOriginRuntime() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "arena.new" {
					return true
				}
			}
		}
	}
	return false
}

// writeArenaRuntime emits the monotonic origin allocator shared by Arena values.
func (e *emitter) writeArenaRuntime() {
	e.out.WriteString("  (func $__arena_origin (result i64)\n")
	fmt.Fprintf(&e.out, "    (if (i64.ge_u (global.get $__arena_instances) "+
		"(i64.const %d))\n", arenaMaxInstances)
	e.out.WriteString("      (then (call $__panic_arena_instances " +
		"(i64.const 0) (i64.const 0))))\n")
	e.out.WriteString("    (global.set $__arena_instances\n")
	e.out.WriteString("      (i64.add (global.get $__arena_instances) (i64.const 1)))\n")
	e.out.WriteString("    (i64.add\n")
	e.out.WriteString("      (i64.shl (global.get $__arena_instances) (i64.const 32))\n")
	e.out.WriteString("      (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeArenaInstr dispatches Arena operations to their wasm32 lowerings.
func (e *emitter) writeArenaInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "arena.new":
		return e.writeArenaNew(instr)
	case "arena.add":
		return e.writeArenaAdd(instr)
	case "arena.at":
		return e.writeArenaAt(instr)
	case "arena.at_mut":
		return e.writeArenaAtMut(instr)
	case "arena.len":
		return e.writeArenaLen(instr)
	case "arena.pop_or_panic":
		return e.writeArenaPopOrPanic(instr)
	case "arena.deinit":
		return e.writeArenaDeinit(instr)
	default:
		return fmt.Errorf("wasm error: unsupported arena instruction `%s`", instr.Op)
	}
}

// arenaElementLayout resolves and measures the element of an Arena operation.
func (e *emitter) arenaElementLayout(instr *ir.Instr) (string, wasmLayout, error) {
	container := instr.Result.Type
	if instr.Op != "arena.new" && len(instr.Args) > 0 {
		container = instr.Args[0].Type
	}
	elem, ok := arenaElementWasmType(container)
	if !ok {
		return "", wasmLayout{}, fmt.Errorf(
			"wasm error: `%s` was handed no Arena<T>", instr.Op)
	}
	layout, err := e.typeLayout(elem)
	if err != nil {
		return "", wasmLayout{}, err
	}
	return elem, layout, nil
}

// writeArenaNew initializes an empty header with a never-reused instance origin.
func (e *emitter) writeArenaNew(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "Allocator" ||
		!isArenaWasmType(instr.Result.Type) {
		return fmt.Errorf("wasm error: arena.new expects allocator -> Arena<T>")
	}
	if _, _, err := e.arenaElementLayout(instr); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (memory.fill %s (i32.const 0) (i32.const %d))\n",
		slot, arenaHeaderSize)
	fmt.Fprintf(&e.out, "            (i64.store %s (call $__arena_origin))\n",
		addressAt(slot, arenaOriginOffset))
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// writeArenaAdd appends one element and returns its origin-biased Handle.
func (e *emitter) writeArenaAdd(instr *ir.Instr) error {
	if len(instr.Args) != 3 || instr.Args[1].Type != "Allocator" {
		return fmt.Errorf(
			"wasm error: arena.add expects Arena<T>, Allocator, T -> std::mem::Error!Handle<T>")
	}
	elem, layout, err := e.arenaElementLayout(instr)
	if err != nil {
		return err
	}
	if instr.Args[2].Type != elem {
		return fmt.Errorf(
			"wasm error: arena.add expects Arena<T>, Allocator, T -> std::mem::Error!Handle<T>")
	}
	errorSet, success, payloadOffset, err := e.errorPayloadOffset(instr.Result.Type)
	if err != nil || errorSet != "std::mem::Error" {
		return fmt.Errorf(
			"wasm error: arena.add expects Arena<T>, Allocator, T -> std::mem::Error!Handle<T>")
	}
	handleElem, ok := arenaHandleElementWasmType(success)
	if !ok || handleElem != elem {
		return fmt.Errorf(
			"wasm error: arena.add expects Arena<T>, Allocator, T -> std::mem::Error!Handle<T>")
	}
	code, err := e.wasmErrorCode(errorSet, "OutOfMemory")
	if err != nil {
		return err
	}
	arena := e.value(instr.Args[0]).expr
	lengthAddress := arrayFieldAddress(arena, arrayLenOffset)
	length := fmt.Sprintf("(i64.load %s)", lengthAddress)
	fmt.Fprintf(&e.out, "            (if (i64.ge_u %s (i64.const %d))\n",
		length, arenaMaxLen)
	fmt.Fprintf(&e.out, "              (then (call $__panic_arena_full "+
		"(i64.const %d) (i64.const %d))))\n",
		instr.Span.Start.Line, instr.Span.Start.Column)
	needed := fmt.Sprintf("(i64.add %s (i64.const 1))", length)
	okExpr := fmt.Sprintf("(call $__array_reserve %s %s %s (i32.const %d))",
		e.value(instr.Args[1]).expr, arena, needed, layout.size)
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.extend_i32_u %s))\n", slot, okExpr)
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.const %d))\n",
		addressAt(slot, payloadOffset), code)
	fmt.Fprintf(&e.out, "            (if (i32.wrap_i64 (i64.load %s))\n", slot)
	e.out.WriteString("              (then\n")
	if err := e.writeStoreValue(arrayElementAddress(arena, length, layout.size), 0,
		elem, e.value(instr.Args[2])); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "                (i64.store %s "+
		"(i64.add (i64.load %s) %s))\n",
		addressAt(slot, payloadOffset), addressAt(arena, arenaOriginOffset), length)
	fmt.Fprintf(&e.out, "                (i64.store %s %s)))\n", lengthAddress, needed)
	e.values[instr.Result.Name] = valueInfo{expr: slot}
	return nil
}

// arenaCheckedIndex resolves a Handle's origin-relative index and element layout.
func (e *emitter) arenaCheckedIndex(
	instr *ir.Instr,
) (string, string, string, wasmLayout, error) {
	if len(instr.Args) != 2 {
		return "", "", "", wasmLayout{},
			fmt.Errorf("wasm error: %s expects Arena<T>, Handle<T>", instr.Op)
	}
	elem, layout, err := e.arenaElementLayout(instr)
	if err != nil {
		return "", "", "", wasmLayout{}, err
	}
	handleElem, ok := arenaHandleElementWasmType(instr.Args[1].Type)
	if !ok || handleElem != elem {
		return "", "", "", wasmLayout{},
			fmt.Errorf("wasm error: %s expects Arena<T>, Handle<T>", instr.Op)
	}
	arena := e.value(instr.Args[0]).expr
	index := fmt.Sprintf("(i64.sub %s (i64.load %s))",
		e.value(instr.Args[1]).expr, addressAt(arena, arenaOriginOffset))
	return elem, arena, index, layout, nil
}

// writeArenaAt loads the element named by a valid Handle and traps otherwise.
func (e *emitter) writeArenaAt(instr *ir.Instr) error {
	elem, arena, index, layout, err := e.arenaCheckedIndex(instr)
	if err != nil {
		return err
	}
	if instr.Result.Type != elem &&
		(!isReferenceType(instr.Result.Type) || derefWasmType(instr.Result.Type) != elem) {
		return fmt.Errorf("wasm error: arena.at expects Arena<T>, Handle<T> -> &T")
	}
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(arena, arrayLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.ge_u %s %s)\n", index, length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_arena_handle "+
		"(i64.const %d) (i64.const %d))))\n",
		instr.Span.Start.Line, instr.Span.Start.Column)
	address := arrayElementAddress(arena, index, layout.size)
	if isReferenceType(instr.Result.Type) {
		return e.writeScalarResult(instr.Result, address)
	}
	return e.writeLoadValue(instr.Result, address, 0)
}

// writeArenaAtMut returns a mutable borrow when the Handle belongs to this Arena.
func (e *emitter) writeArenaAtMut(instr *ir.Instr) error {
	elem, arena, index, layout, err := e.arenaCheckedIndex(instr)
	if err != nil {
		return err
	}
	want, payloadOffset, err := e.optionalPayloadOffset(instr.Result.Type)
	if err != nil || want != "&var "+elem {
		return fmt.Errorf("wasm error: arena.at_mut expects Arena<T>, Handle<T> -> ?&var T")
	}
	length := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(arena, arrayLenOffset))
	fmt.Fprintf(&e.out, "            (if (i64.lt_u %s %s)\n", index, length)
	e.out.WriteString("              (then\n")
	if err := e.writeTaggedResult(instr.Result, 1); err != nil {
		return err
	}
	slot, err := e.resultSlot(instr.Result)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "                (i32.store %s %s)\n",
		addressAt(slot, payloadOffset), arrayElementAddress(arena, index, layout.size))
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeTaggedResult(instr.Result, 0); err != nil {
		return err
	}
	e.out.WriteString("              ))\n")
	return nil
}

// writeArenaLen returns the number of initialized elements still owned by an Arena.
func (e *emitter) writeArenaLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "i64" {
		return fmt.Errorf("wasm error: arena.len expects Arena<T> -> i64")
	}
	if _, ok := arenaElementWasmType(instr.Args[0].Type); !ok {
		return fmt.Errorf("wasm error: arena.len expects Arena<T> -> i64")
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s (i64.load %s))\n",
		symbol, arrayFieldAddress(e.value(instr.Args[0]).expr, arrayLenOffset))
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeArenaPopOrPanic moves the last initialized element out for cleanup.
func (e *emitter) writeArenaPopOrPanic(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: arena.pop_or_panic expects Arena<T> -> T")
	}
	elem, layout, err := e.arenaElementLayout(instr)
	if err != nil {
		return err
	}
	if instr.Result.Type != elem {
		return fmt.Errorf("wasm error: arena.pop_or_panic expects Arena<T> -> T")
	}
	arena := e.value(instr.Args[0]).expr
	lengthAddress := arrayFieldAddress(arena, arrayLenOffset)
	length := fmt.Sprintf("(i64.load %s)", lengthAddress)
	fmt.Fprintf(&e.out, "            (if (i64.eqz %s)\n", length)
	fmt.Fprintf(&e.out, "              (then (call $__panic_arena_empty "+
		"(i64.const %d) (i64.const %d))))\n",
		instr.Span.Start.Line, instr.Span.Start.Column)
	fmt.Fprintf(&e.out, "            (i64.store %s (i64.sub %s (i64.const 1)))\n",
		lengthAddress, length)
	source := arrayElementAddress(arena, fmt.Sprintf("(i64.load %s)", lengthAddress), layout.size)
	return e.writeLoadValue(instr.Result, source, 0)
}

// writeArenaDeinit releases an Arena's backing allocation.
func (e *emitter) writeArenaDeinit(instr *ir.Instr) error {
	if len(instr.Args) != 2 || instr.Args[1].Type != "Allocator" ||
		instr.Result.Type != "void" {
		return fmt.Errorf("wasm error: arena.deinit expects Arena<T>, Allocator -> void")
	}
	_, layout, err := e.arenaElementLayout(instr)
	if err != nil {
		return err
	}
	arena := e.value(instr.Args[0]).expr
	capacity := fmt.Sprintf("(i64.load %s)", arrayFieldAddress(arena, arrayCapacityOffset))
	bytes := fmt.Sprintf("(i32.wrap_i64 (i64.mul %s (i64.const %d)))", capacity, layout.size)
	fmt.Fprintf(&e.out, "            (call $__allocator_free %s (i32.load %s) %s)\n",
		e.value(instr.Args[1]).expr, arrayFieldAddress(arena, arrayDataOffset), bytes)
	return nil
}
