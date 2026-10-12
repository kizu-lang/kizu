package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// A struct or fixed array is an LLVM aggregate, and a load or store of a whole
// aggregate is one scalar operation per leaf by the time the optimizer has
// taken it apart. That is the cheapest form a small value has, since every
// field ends up a register, and it is what a large one cannot afford: a value
// of twenty thousand numbers read forty times is eight hundred thousand
// instructions the optimizer walks before it can drop them.
//
// So a large value that nothing reads whole is kept where it already is.
// Reading it out of storage copies the bytes into a slot of its own with one
// llvm.memcpy, and the address of that slot is the value: nothing writes the
// slot again while the value is live, so it is the snapshot a load would have
// been, and writing the storage it came from afterwards does not reach it. A
// field of it is read through its address, storing it is another copy, and
// passing it by value hands the slot over as it is.
//
// A large value something does read whole -- a return, a literal it becomes a
// field of, a phi -- is the aggregate it always was, loaded from its source
// where it is made, and every use of it is written as if this file were not
// here. The aggregate is never loaded back out of a slot: the optimizer takes
// such a load apart into one read per leaf and an insertvalue chain it cannot
// fold again, which costs more than the load it replaced. So the two forms do
// not mix in one value, and readsAddress is what decides between them, for
// the pass that sorts a function's values and for every instruction that
// reads one alike.
//
// Which types this is asked about at all is memoryValueLeaves, one count over
// the type's own nesting, so a type is treated the same way everywhere.

// memoryValueLeaves is the most scalars a struct or fixed array value holds
// while it stays an LLVM aggregate whatever reads it. Past it a value nothing
// reads whole is kept in memory.
//
// Sixteen keeps what a program computes with -- a point, a pair, a 4x4
// matrix -- in registers, and is where keeping a value in memory stops
// costing anything: clang's time on an aggregate grows with its leaves at -O0
// and at -O3 alike, and from the low twenties on a value held in memory
// builds faster at either level and runs no slower.
const memoryValueLeaves = 16

// isMemoryValueType reports whether a value of typ may be kept in memory
// rather than as an LLVM aggregate: a struct or fixed array of more leaves
// than memoryValueLeaves. The answer depends on the type alone and is asked
// for every value that is loaded, stored or passed, so it is kept.
func (e *emitter) isMemoryValueType(typ string) bool {
	if known, ok := e.memoryTypes[typ]; ok {
		return known
	}
	known := e.scalarLeaves(typ, nil) > memoryValueLeaves
	e.memoryTypes[typ] = known
	return known
}

// scalarLeaves counts what a value of typ is made of once its structs and
// fixed arrays are taken apart: one for everything else, a tagged aggregate
// among it, which keeps its own form. The count stops at the first answer past
// memoryValueLeaves, which is all isMemoryValueType asks and what keeps an
// array's length from overflowing it. A struct already on the path counts as
// one leaf, so a by-value cycle ends instead of recursing.
func (e *emitter) scalarLeaves(typ string, seen []string) int {
	if st, ok := e.module.Structs[typ]; ok {
		if containsName(seen, typ) {
			return 1
		}
		seen = append(seen, typ)
		leaves := 0
		for _, field := range st.Fields {
			leaves += e.scalarLeaves(field.Type, seen)
			if leaves > memoryValueLeaves {
				return memoryValueLeaves + 1
			}
		}
		return leaves
	}
	if count, elem, ok := e.bufferSize(typ); ok {
		each := e.scalarLeaves(elem, seen)
		if each > 0 && count > int64(memoryValueLeaves/each) {
			return memoryValueLeaves + 1
		}
		return int(count) * each
	}
	return 1
}

// readsAddress reports whether instr reads its argument at index through the
// address of the storage it is kept in, rather than as an LLVM operand. It is
// the one answer both sides use: collectWholeValues keeps a value in memory
// only when every use of it says yes, and an instruction takes a value's
// address through addressArg, which asks here first. An instruction this does
// not name reads its arguments whole.
//
// Two answers depend on what else is read whole. The field of a value kept in
// memory is its own address inside that value, so a field that is read whole
// needs its receiver whole too. And storage whose whole value is loaded takes
// what is written to it whole: filled by a copy and then loaded, it is the
// load out of a slot again.
func (e *emitter) readsAddress(instr *ir.Instr, index int) bool {
	switch {
	case instr.Op == "local.slot":
		return index == 0 && !e.wholeStorage[instr.Result.Name]
	case instr.Op == "ref.store", strings.HasPrefix(instr.Op, "field.ref.set."):
		return index == 1 && !e.wholeStorage[instr.Args[0].Name]
	case instr.Op == "box.new":
		return index == 1
	case instr.Op == "slice.store", instr.Op == "array.append",
		instr.Op == "array.set", instr.Op == "arena.add":
		return index == 2
	case instr.Op == "map.insert":
		return index == 3
	case isFieldRead(instr.Op):
		return index == 0 && !e.wholeValues[instr.Result.Name]
	case instr.Op == "call.indirect":
		return index > 0
	case strings.HasPrefix(instr.Op, "call."):
		return e.callReadsAddress(instr, index)
	}
	return false
}

// callReadsAddress answers readsAddress for a call by name, by the way
// writeCall hands that callee its arguments. A module-local callee takes every
// struct and fixed array behind a pointer, the hosted runtime every struct,
// and any other callee only the parameters it declares as reading through the
// address of a copy.
func (e *emitter) callReadsAddress(instr *ir.Instr, index int) bool {
	switch _, kind := e.callTarget(instr); kind {
	case callBuiltin:
		return false
	case callHosted:
		return e.hostedRuntimeIndirectABI(instr.Args[index].Type)
	case callInternal:
		return true
	}
	return callParamTakesAddressOf(instr, index)
}

// keptInMemory reports whether value is kept in memory: its type allows it and
// nothing in the function being written reads it whole.
func (e *emitter) keptInMemory(value ir.Value) bool {
	return !e.wholeValues[value.Name] && e.isMemoryValueType(value.Type)
}

// memoryUse is one argument of a type that may be kept in memory.
type memoryUse struct {
	instr *ir.Instr
	index int
}

// collectWholeValues records the values of fn that something reads whole, so
// that every other value of a type that may be kept in memory is, and the
// storage a whole value is loaded from. An argument is read whole by what
// readsAddress says of its instruction, and since two of its answers follow
// from what is already whole, the arguments are asked again until none
// changes.
func (e *emitter) collectWholeValues(fn *ir.Function) {
	e.wholeValues = map[string]bool{}
	e.wholeStorage = map[string]bool{}
	e.misread = ""
	uses, loads := e.memoryUses(fn)
	for changed := true; changed; {
		changed = false
		for _, use := range uses {
			arg := use.instr.Args[use.index]
			if !e.wholeValues[arg.Name] && !e.readsAddress(use.instr, use.index) {
				e.wholeValues[arg.Name] = true
				changed = true
			}
		}
		for _, load := range loads {
			source := load.Args[0].Name
			if e.wholeValues[load.Result.Name] && !e.wholeStorage[source] {
				e.wholeStorage[source] = true
				changed = true
			}
		}
	}
}

// memoryUses walks fn once for collectWholeValues. It records what is read
// whole whatever else is -- a value a literal takes as a field, a phi names,
// a block returns or a cleanup reads whole -- and returns what has to be
// asked again: every argument of a type that may be kept in memory, and every
// load of such a value out of storage.
func (e *emitter) memoryUses(fn *ir.Function) ([]memoryUse, []*ir.Instr) {
	var uses []memoryUse
	var loads []*ir.Instr
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			for index, arg := range instr.Args {
				if e.isMemoryValueType(arg.Type) {
					uses = append(uses, memoryUse{instr, index})
				}
			}
			for _, field := range instr.Fields {
				e.markWhole(field.Value)
			}
			for _, incoming := range instr.Incoming {
				e.markWhole(incoming.Value)
			}
			for _, cleanup := range instr.Cleanups {
				e.markWholeArgs(cleanupInstruction(cleanup))
			}
			if instr.Op == "ref.load" && e.isMemoryValueType(instr.Result.Type) {
				loads = append(loads, instr)
			}
		}
		e.markWhole(block.Terminator.Value)
	}
	return uses, loads
}

// markWholeArgs records the arguments a cleanup reads whole. A cleanup is a
// deferred call, whose answer depends on nothing else, so it is asked once.
func (e *emitter) markWholeArgs(instr *ir.Instr) {
	for index, arg := range instr.Args {
		if e.isMemoryValueType(arg.Type) && !e.readsAddress(instr, index) {
			e.wholeValues[arg.Name] = true
		}
	}
}

// markWhole records that value is read whole, when its type is one that may
// be kept in memory; no other value is asked about.
func (e *emitter) markWhole(value ir.Value) {
	if e.isMemoryValueType(value.Type) {
		e.wholeValues[value.Name] = true
	}
}

// setMemoryValue records that result is the bytes at addr, which nothing
// writes while result is live. It has no operand: every use reads it through
// addressArg.
func (e *emitter) setMemoryValue(result ir.Value, addr string) {
	e.values[result.Name] = valueInfo{typ: result.Type, addr: addr}
}

// addressArg returns where the argument of instr at index lives when it is
// kept in memory, and false for an argument that is an LLVM operand. A value
// is kept in memory only when readsAddress holds for every use of it, and
// asking it again here is what keeps an instruction from taking an address
// the sorting did not count on.
func (e *emitter) addressArg(instr *ir.Instr, index int) (string, bool) {
	found := e.values[instr.Args[index].Name]
	if found.addr == "" || !e.readsAddress(instr, index) {
		return "", false
	}
	return found.addr, true
}

// writeMemoryCopy copies one value of typ from source to target. The two
// never overlap unless they are the same address: a value's slot is its own,
// and storage is never copied onto itself except in place.
func (e *emitter) writeMemoryCopy(target string, source string, typ string) {
	e.memoryCopyUsed = true
	fmt.Fprintf(&e.out, "  call void @llvm.memcpy.p0.p0.i64(ptr %s, ptr %s, i64 %s, i1 false)\n",
		target, source, e.elementSizeOperand(typ))
}

// copiedIntoMemory reads the storage at source as result when result is kept
// in memory, and reports whether it did. The copy is the snapshot: the
// storage may be written again, the slot is not.
func (e *emitter) copiedIntoMemory(result ir.Value, source string) bool {
	if !e.keptInMemory(result) {
		return false
	}
	addr := localName(result.Name) + ".addr"
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", addr, e.llvmType(result.Type))
	e.writeMemoryCopy(addr, source, result.Type)
	e.setMemoryValue(result, addr)
	return true
}

// copiedFromMemory writes the argument of instr at index into the storage at
// target when that argument is kept in memory, and reports whether it did.
func (e *emitter) copiedFromMemory(instr *ir.Instr, index int, target string) bool {
	addr, ok := e.addressArg(instr, index)
	if !ok {
		return false
	}
	e.writeMemoryCopy(target, addr, instr.Args[index].Type)
	return true
}

// writeMemoryField reads field index of a struct kept in memory at addr. A
// field that is itself kept in memory is its own address inside the struct,
// which holds for as long as the struct does; any other field is loaded.
func (e *emitter) writeMemoryField(instr *ir.Instr, addr string, index int) {
	name := localName(instr.Result.Name)
	fieldAddr := name + ".addr"
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 %d\n",
		fieldAddr, e.llvmType(instr.Args[0].Type), addr, index)
	if e.keptInMemory(instr.Result) {
		e.setMemoryValue(instr.Result, fieldAddr)
		return
	}
	resultType := e.llvmType(instr.Result.Type)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s%s\n",
		name, resultType, fieldAddr, e.valueTag(resultType))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
}

// writeMemoryCopyDecl declares llvm.memcpy for a module that copied a value
// and does not already declare it for the Array runtime.
func (e *emitter) writeMemoryCopyDecl() {
	if !e.memoryCopyUsed || e.usesArrayRuntime() {
		return
	}
	e.out.WriteString("declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1)\n\n")
}
