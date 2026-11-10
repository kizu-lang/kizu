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
// instructions the optimizer walks before it can drop them. An optional, an
// error union or a tagged union around such a value is the same aggregate
// with a tag in front, and costs the same.
//
// So a large value that nothing reads whole is kept where it already is.
// Reading it out of storage copies the bytes into a slot of its own with one
// llvm.memcpy, and the address of that slot is the value: nothing writes the
// slot again while the value is live, so it is the snapshot a load would have
// been, and writing the storage it came from afterwards does not reach it. A
// field of it is read through its address, storing it is another copy, and
// passing it by value hands the slot over as it is. Wrapping it builds the
// wrapper in a slot, the tag stored and the payload copied in; a wrapper kept
// in memory has its tag loaded through its address, and its payload is that
// payload's own address inside it.
//
// A large value something does read whole -- a phi, or an instruction that
// does not name it below -- is the aggregate it always was, loaded from its
// source where it is made, and every use of it is written as if this file
// were not here. The aggregate is never loaded back out of a slot the
// function filled itself: the optimizer takes such a load apart into one read
// per leaf and an insertvalue chain it cannot fold again, which costs more
// than the load it replaced. So the two forms do not mix in one value, and
// readsAddress is what decides between them, for the pass that sorts a
// function's values and for every instruction that reads one alike.
//
// A struct or fixed array of such a size never crosses a call whole either.
// It is passed as the address of a slot (functionParamABI), and returned
// through one the caller hands over (returnsThroughSlot): code generators that
// take an aggregate operand apart one leaf at a time -- x86-64 at -O0 is one
// -- spend minutes on a call that returns a value of a hundred thousand bytes,
// and the same bytes moved by one llvm.memcpy cost them nothing. A literal is
// built in a slot of its own, one field at a time, a field written into a
// value is written into a copy of it, and a zero array kept in memory is
// written with one llvm.memset.
//
// Which types this is asked about at all is memoryValueLeaves, one count over
// the type's own nesting, so a type is treated the same way everywhere.

// memoryValueLeaves is the most scalars a value holds while it stays an LLVM
// aggregate whatever reads it. Past it a value nothing reads whole is kept in
// memory.
//
// Sixteen keeps what a program computes with -- a point, a pair, a 4x4
// matrix -- in registers, and is where keeping a value in memory stops
// costing anything: clang's time on an aggregate grows with its leaves at -O0
// and at -O3 alike, and from the low twenties on a value held in memory
// builds faster at either level and runs no slower.
const memoryValueLeaves = 16

// isMemoryValueType reports whether a value of typ may be kept in memory
// rather than as an LLVM aggregate: a struct or fixed array of more leaves
// than memoryValueLeaves, or an optional, error union or tagged union that
// holds one. The answer depends on the type alone and is asked for every
// value that is loaded, stored or passed, so it is kept.
func (e *emitter) isMemoryValueType(typ string) bool {
	if known, ok := e.memoryTypes[typ]; ok {
		return known
	}
	known := e.scalarLeaves(typ, nil) > memoryValueLeaves
	e.memoryTypes[typ] = known
	return known
}

// scalarLeaves counts what a value of typ is made of once its structs and
// fixed arrays are taken apart, and one for everything else. A value that
// wraps another counts as what it wraps -- an optional or an error union as
// its payload, a tagged union as the largest payload a variant has -- since
// the tag rides in front of that payload and does not change what the
// aggregate costs. The count stops at the first answer past
// memoryValueLeaves, which is all isMemoryValueType asks and what keeps an
// array's length from overflowing it. A struct or union already on the path
// counts as one leaf, so a by-value cycle ends instead of recursing.
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
	if payload, ok := e.wrappedType(typ); ok {
		return e.scalarLeaves(payload, seen)
	}
	if union, ok := e.module.Unions[typ]; ok && !containsName(seen, typ) {
		seen = append(seen, typ)
		leaves := 1
		for _, variant := range union.Variants {
			if variant.Payload != "" {
				leaves = max(leaves, e.scalarLeaves(variant.Payload, seen))
			}
		}
		return leaves
	}
	return 1
}

// wrappedType returns the payload of an optional `?T` or an error union
// `E!T`, the two types that are one tag and one payload.
func (e *emitter) wrappedType(typ string) (string, bool) {
	if elem, ok := optionalElemLLVM(typ); ok {
		return elem, true
	}
	if !strings.Contains(typ, "!") {
		return "", false
	}
	return e.errorUnionSuccessType(typ)
}

// readsAddress reports whether instr reads its argument at index through the
// address of the storage it is kept in, rather than as an LLVM operand. It is
// the one answer both sides use: collectWholeValues keeps a value in memory
// only when every use of it says yes, and an instruction takes a value's
// address through addressArg, which asks here. An instruction this does not
// name reads its arguments whole.
//
// Some answers depend on what else is read whole. A field or a payload of a
// value kept in memory is its own address inside that value, so one that is
// read whole needs what it is read out of whole too -- unless that lies in
// storage the caller filled (lent), which a whole value is loaded out of as
// out of any storage. A wrapper built in a slot copies its payload in, and
// built whole it takes the payload whole; a field write built in a slot copies
// the value and the field in, and built whole takes both whole (a literal is
// the same, and collectWholeValues asks it). And storage a whole value is loaded
// from takes what is written to it whole, to any part of it: filled by a copy
// and then loaded, it is the load out of a slot again.
func (e *emitter) readsAddress(instr *ir.Instr, index int) bool {
	if position, ok := copiedOperand[instr.Op]; ok {
		return index == position
	}
	switch {
	case instr.Op == "local.slot":
		return index == 0 && !e.wholeStorage[instr.Result.Name]
	case instr.Op == "ref.store", strings.HasPrefix(instr.Op, "field.ref.set."):
		return index == 1 && !e.wholeStorage[e.storageRoot(instr.Args[0].Name)]
	case instr.Op == "call.indirect":
		return index > 0 && e.passedBehindPointer(instr.CallParams, index-1, instr.Args[index])
	case strings.HasPrefix(instr.Op, "field.set."):
		return !e.wholeValues[instr.Result.Name]
	case strings.HasPrefix(instr.Op, "call."):
		return e.callReadsAddress(instr, index)
	}
	return index == 0 && e.readsFirstAtAddress(instr)
}

// copiedOperand names, for each instruction that copies a value into storage
// of the runtime's or of a view, the operand that value is: the bytes go
// where they are sent whatever else reads the value.
var copiedOperand = map[string]int{
	"box.new":      1,
	"slice.store":  2,
	"array.append": 2,
	"array.set":    2,
	"arena.add":    2,
	"map.insert":   3,
}

// readsFirstAtAddress answers readsAddress for the first argument of an
// instruction that opens or makes a value: the tag of a wrapper is always
// read where the wrapper lies, a wrapper built in a slot copies its payload
// in, and a field or a payload is read at its address by what
// readsPartAtAddress says.
func (e *emitter) readsFirstAtAddress(instr *ir.Instr) bool {
	switch {
	case isTagRead(instr.Op):
		return true
	case isWrap(instr.Op):
		return !e.wholeValues[instr.Result.Name]
	case isFieldRead(instr.Op), isPayloadRead(instr.Op):
		return e.readsPartAtAddress(instr)
	case instr.Op == "error.try":
		// A failure whose union is the function's own is returned as it is.
		return e.readsPartAtAddress(instr) && instr.Args[0].Type != e.currentReturn
	}
	return false
}

// readsPartAtAddress reports whether instr, which reads a field or a payload
// out of its first argument, may read it at its address inside that argument:
// the part is kept in memory too, or the argument lies in storage the caller
// filled.
func (e *emitter) readsPartAtAddress(instr *ir.Instr) bool {
	return !e.wholeValues[instr.Result.Name] || e.lent[instr.Args[0].Name]
}

// isWrap reports whether op puts a payload behind a tag: an optional, an
// error union or a tagged union made of the value it is given.
func isWrap(op string) bool {
	return op == "opt.some" || op == "error.ok" || op == "union.new"
}

// isPayloadRead reports whether op reads the payload out of a wrapper and
// nothing else of it. `error.try` reads one too, and returns on a failure.
func isPayloadRead(op string) bool {
	return op == "opt.value" || op == "error.value" || op == "union.payload"
}

// isTagRead reports whether op reads the tag of a wrapper and nothing else:
// whether an optional holds a value, whether an error union succeeded and
// with which code it failed, and which variant a union is.
func isTagRead(op string) bool {
	return op == "opt.has" || op == "error.has" || op == "error.code" || op == "union.tag"
}

// callReadsAddress answers readsAddress for a call by name, by the way
// writeCall hands that callee its arguments. A module-local callee takes every
// struct and fixed array behind a pointer, the hosted runtime every struct
// and union, and any other callee only the parameters it declares as reading
// through the address of a copy.
func (e *emitter) callReadsAddress(instr *ir.Instr, index int) bool {
	switch name, kind := e.callTarget(instr); kind {
	case callBuiltin:
		return false
	case callHosted:
		return e.hostedRuntimeIndirectABI(instr.Args[index].Type)
	case callInternal:
		return e.passedBehindPointer(e.functionParams[name], index, instr.Args[index])
	}
	return callParamTakesAddressOf(instr, index)
}

// passedBehindPointer reports whether a function of this module is handed
// arg, the index-th it takes, behind a pointer, which is what internalCallArg
// writes: a struct or fixed array always, and anything else when the
// parameter reads through the address of a copy. An optional, an error union
// and a tagged union are passed as the values they are.
func (e *emitter) passedBehindPointer(params []ir.Param, index int, arg ir.Value) bool {
	if index < len(params) && params[index].TakesAddressOf(arg.Type) {
		return true
	}
	return e.usesIndirectStructParamABI(arg.Type)
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

// markLent records the values of fn that lie in storage the caller filled: a
// parameter passed behind a pointer, and a field or a payload read out of
// one. Nothing in fn writes that storage, so loading a whole value out of it
// is a load like any other, and reading one part of it whole is no reason to
// load all of it.
func (e *emitter) markLent(fn *ir.Function, uses []memoryUse) {
	e.lent = map[string]bool{}
	if fn.ExportABI == "c" {
		return
	}
	for _, param := range fn.Params {
		if e.usesIndirectStructParamABI(param.Type) && e.isMemoryValueType(param.Type) {
			e.lent[param.Name] = true
		}
	}
	for changed := len(e.lent) > 0; changed; {
		changed = false
		for _, use := range uses {
			op, result := use.instr.Op, use.instr.Result.Name
			if use.index == 0 && e.lent[use.instr.Args[0].Name] && !e.lent[result] &&
				(isFieldRead(op) || isPayloadRead(op) || op == "error.try") {
				e.lent[result] = true
				changed = true
			}
		}
	}
}

// collectWholeValues records the values of fn that something reads whole, so
// that every other value of a type that may be kept in memory is, and the
// storage a whole value is loaded from: the local, or whatever else the
// address it is loaded through lies in (storageRoot), since a write to one
// part of that storage and a load of another may meet in the same bytes. An
// argument is read whole by what readsAddress says of its instruction, and
// since some of its answers follow from what is already whole, the arguments
// are asked again until none changes.
func (e *emitter) collectWholeValues(fn *ir.Function) {
	e.wholeValues = map[string]bool{}
	e.wholeStorage = map[string]bool{}
	e.partOf = map[string]string{}
	e.useCounts = map[string]int{}
	e.ownSlots = map[string]string{}
	e.misread = ""
	uses, loads, literals := e.memoryUses(fn)
	e.markLent(fn, uses)
	for changed := true; changed; {
		changed = false
		for _, literal := range literals {
			if !e.wholeValues[literal.Result.Name] {
				continue
			}
			for _, field := range literal.Fields {
				if e.isMemoryValueType(field.Value.Type) && !e.wholeValues[field.Value.Name] {
					e.wholeValues[field.Value.Name] = true
					changed = true
				}
			}
		}
		for _, use := range uses {
			arg := use.instr.Args[use.index]
			if !e.wholeValues[arg.Name] && !e.readsAddress(use.instr, use.index) {
				e.wholeValues[arg.Name] = true
				changed = true
			}
		}
		for _, load := range loads {
			source := e.storageRoot(load.Args[0].Name)
			if e.wholeValues[load.Result.Name] && !e.wholeStorage[source] {
				e.wholeStorage[source] = true
				changed = true
			}
		}
	}
}

// memoryUses walks fn once for collectWholeValues. It records what is read
// whole whatever else is -- a value a phi names, a block returns from a
// function that does not return through a slot, or a cleanup reads whole --
// and returns what has to be asked again: every argument of a type that may
// be kept in memory, every load of such a value out of storage, and every
// literal of such a type, whose fields are read whole only when it is. It
// also records which address each address of a part was taken from, and how
// many times each value of such a type is named.
func (e *emitter) memoryUses(fn *ir.Function) ([]memoryUse, []*ir.Instr, []*ir.Instr) {
	var uses []memoryUse
	var loads []*ir.Instr
	var literals []*ir.Instr
	returnsWhole := !e.returnsThroughSlot(fn.Return)
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if isAddressOfPart(instr.Op) && len(instr.Args) > 0 {
				e.partOf[instr.Result.Name] = instr.Args[0].Name
			}
			for index, arg := range instr.Args {
				if e.isMemoryValueType(arg.Type) {
					uses = append(uses, memoryUse{instr, index})
					e.useCounts[arg.Name]++
				}
			}
			if len(instr.Fields) > 0 && e.isMemoryValueType(instr.Result.Type) {
				literals = append(literals, instr)
			}
			for _, field := range instr.Fields {
				e.countUse(field.Value)
			}
			for _, incoming := range instr.Incoming {
				e.markWhole(incoming.Value)
				e.countUse(incoming.Value)
			}
			for _, cleanup := range instr.Cleanups {
				e.markWholeArgs(cleanupInstruction(cleanup))
			}
			if isStorageLoad(instr.Op) && e.isMemoryValueType(instr.Result.Type) {
				loads = append(loads, instr)
			}
		}
		if returnsWhole {
			e.markWhole(block.Terminator.Value)
		}
		e.countUse(block.Terminator.Value)
	}
	return uses, loads, literals
}

// countUse counts one more place value is named, when its type is one that
// may be kept in memory.
func (e *emitter) countUse(value ir.Value) {
	if e.isMemoryValueType(value.Type) {
		e.useCounts[value.Name]++
	}
}

// isStorageLoad reports whether op reads a value out of the storage behind
// the borrow it is given -- all of it, or one field of a struct -- which
// collectWholeValues follows back to that storage.
func isStorageLoad(op string) bool {
	return op == "ref.load" || op == "union.load" ||
		strings.HasPrefix(op, "field.ref.") && !strings.HasPrefix(op, "field.ref.set.")
}

// isAddressOfPart reports whether op takes the address of a part of the
// storage its first argument is the address of: an element of a fixed array,
// a field of a struct, the payload of a union.
func isAddressOfPart(op string) bool {
	return op == "buffer.addr" || op == "union.payload_ref" || strings.HasPrefix(op, "field.addr.")
}

// storageRoot returns the address the storage name points into was first
// known by in the function being written: name itself, or what the address
// of a part was taken from, as far back as that goes.
func (e *emitter) storageRoot(name string) string {
	for {
		whole, ok := e.partOf[name]
		if !ok {
			return name
		}
		name = whole
	}
}

// markWholeArgs records the arguments a cleanup reads whole, and counts each
// it names. A cleanup is a deferred call, whose answer depends on nothing
// else, so it is asked once.
func (e *emitter) markWholeArgs(instr *ir.Instr) {
	for index, arg := range instr.Args {
		if !e.isMemoryValueType(arg.Type) {
			continue
		}
		e.useCounts[arg.Name]++
		if !e.readsAddress(instr, index) {
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
	e.setOwnMemoryValue(result, addr)
	return true
}

// copiedFromMemory writes the argument of instr at index into the storage at
// target when that argument is kept in memory or is a zero array of a type
// that may be, and reports whether it did.
func (e *emitter) copiedFromMemory(instr *ir.Instr, index int, target string) bool {
	addr, ok := e.addressArg(instr, index)
	if !ok {
		return e.zeroedMemory(instr.Args[index], target)
	}
	e.writeMemoryCopy(target, addr, instr.Args[index].Type)
	return true
}

// zeroedMemory sets the storage at target to value with one llvm.memset when
// value is the zero array (writeBufferNew) of a type that may be kept in
// memory, and reports whether it did. Stored as the constant it is, such a
// zero is one store per leaf to the code generator.
func (e *emitter) zeroedMemory(value ir.Value, target string) bool {
	if e.values[value.Name].operand != "zeroinitializer" || !e.isMemoryValueType(value.Type) {
		return false
	}
	e.memorySetUsed = true
	fmt.Fprintf(&e.out, "  call void @llvm.memset.p0.i64(ptr %s, i8 0, i64 %s, i1 false)\n",
		target, e.elementSizeOperand(value.Type))
	return true
}

// writeValueAt writes value into the storage at target: copied there when it
// is kept in memory, set there when it is a zero array, and stored as the
// operand it is otherwise. It is what a literal, a field write and a return
// through a slot put their parts in with, after collectWholeValues has
// decided which of them are read whole.
func (e *emitter) writeValueAt(value ir.Value, target string) {
	if found := e.values[value.Name]; found.addr != "" {
		e.writeMemoryCopy(target, found.addr, value.Type)
		return
	}
	if e.zeroedMemory(value, target) {
		return
	}
	llvmType := e.llvmType(value.Type)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s%s\n",
		llvmType, e.value(value).operand, target, e.valueTag(llvmType))
}

// returnSlot is the parameter a function that returns through a slot is
// handed that slot by. `return` is a keyword, so no local is called it.
const returnSlot = "%kizu.return"

// returnsThroughSlot reports whether a function of this module returning typ
// returns it through a slot its caller hands over, as the first parameter,
// rather than as a value: a struct or fixed array that may be kept in memory.
// What C calls or is called by never returns one (SPEC §12), so the rule
// needs nothing but the type, and a direct call, a call through a function
// pointer and the definition all agree on it.
func (e *emitter) returnsThroughSlot(typ string) bool {
	return e.usesIndirectStructParamABI(typ) && e.isMemoryValueType(typ)
}

// writeSlotCall writes a call of callee, which returns the result of instr
// through a slot, with args after that slot. The result is what the callee
// wrote there: kept in memory at the slot, or loaded out of it when something
// reads it whole.
func (e *emitter) writeSlotCall(instr *ir.Instr, callee string, args []string) {
	name := localName(instr.Result.Name)
	slot := name + ".addr"
	resultType := e.llvmType(instr.Result.Type)
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slot, resultType)
	fmt.Fprintf(&e.out, "  call void %s(%s)\n",
		callee, strings.Join(append([]string{"ptr " + slot}, args...), ", "))
	if e.keptInMemory(instr.Result) {
		e.setOwnMemoryValue(instr.Result, slot)
		return
	}
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", name, resultType, slot)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
}

// writeMemoryLiteral builds the struct literal instr makes in a slot of its
// own, one field at a time, with fields in the order its struct declares them.
func (e *emitter) writeMemoryLiteral(instr *ir.Instr, fields []ir.Value) {
	name := localName(instr.Result.Name)
	slot := name + ".addr"
	structType := e.llvmType(instr.Result.Type)
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slot, structType)
	for index, value := range fields {
		fieldAddr := fmt.Sprintf("%s.field%d", name, index)
		fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 %d\n",
			fieldAddr, structType, slot, index)
		e.writeValueAt(value, fieldAddr)
	}
	e.setOwnMemoryValue(instr.Result, slot)
}

// writeMemoryFieldSet writes the field write instr makes into a copy of its
// receiver in a slot of its own: the receiver copied in, then field index.
// A receiver that is named nowhere else and lies in a slot of its own, filled
// in this same block, is not read again, so its slot is taken over as it is:
// a field written through a borrow, `a.b.c = v`, copies b out and back once
// rather than twice. The block is what keeps a write in a loop from reaching
// a receiver made before it, which every turn reads afresh.
func (e *emitter) writeMemoryFieldSet(instr *ir.Instr, index int) {
	name := localName(instr.Result.Name)
	receiver := instr.Args[0]
	slot := e.values[receiver.Name].addr
	structType := e.llvmType(instr.Result.Type)
	fieldAddr := name + ".field.addr"
	if e.ownSlots[receiver.Name] != e.currentBlock || e.useCounts[receiver.Name] != 1 {
		slot = name + ".addr"
		fmt.Fprintf(&e.out, "  %s = alloca %s\n", slot, structType)
		e.writeValueAt(receiver, slot)
	}
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 %d\n",
		fieldAddr, structType, slot, index)
	e.writeValueAt(instr.Args[1], fieldAddr)
	e.setOwnMemoryValue(instr.Result, slot)
}

// setOwnMemoryValue records that result is the bytes at addr, as
// setMemoryValue does, and that addr is a slot made for result alone, filled
// in the block being written.
func (e *emitter) setOwnMemoryValue(result ir.Value, addr string) {
	e.setMemoryValue(result, addr)
	e.ownSlots[result.Name] = e.currentBlock
}

// writeMemoryField reads field index of a value kept in memory at addr: a
// field of a struct, or the payload of a wrapper, which is the field after
// its tag. A field that is itself kept in memory is its own address inside
// the value, which holds for as long as the value does; any other field is
// loaded.
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

// writeMemoryAlias makes result the value kept in memory at addr, which is
// what an optional without a tag and its payload are to each other: the same
// bytes. A result something reads whole is loaded from there, which
// readsAddress allows only out of storage the caller filled.
func (e *emitter) writeMemoryAlias(result ir.Value, addr string) {
	if e.keptInMemory(result) {
		e.setMemoryValue(result, addr)
		return
	}
	name := localName(result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", name, e.llvmType(result.Type), addr)
	e.values[result.Name] = valueInfo{typ: result.Type, operand: name}
}

// writeMemoryTag reads the tag of a wrapper kept in memory at addr as name:
// the presence byte of an optional, the code of an error union, the variant
// number of a union. The tag is the first field of all three, so it is what
// the address points at.
func (e *emitter) writeMemoryTag(name string, tagType string, addr string) {
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", name, tagType, addr)
}

// writeMemoryWrapper builds the optional or error union instr makes around
// the payload kept in memory at source: a slot of the wrapper's own, the tag
// stored and the payload copied in after it.
func (e *emitter) writeMemoryWrapper(instr *ir.Instr, tag string, source string) {
	name := localName(instr.Result.Name)
	slot := name + ".addr"
	wrapperType := e.llvmType(instr.Result.Type)
	payloadAddr := name + ".payload.addr"
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slot, wrapperType)
	fmt.Fprintf(&e.out, "  store %s, ptr %s\n", tag, slot)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 1\n",
		payloadAddr, wrapperType, slot)
	e.writeMemoryCopy(payloadAddr, source, instr.Args[0].Type)
	e.setMemoryValue(instr.Result, slot)
}

// writeMemoryDecls declares llvm.memcpy for a module that copied a value and
// does not already declare it for the Array runtime, and llvm.memset for one
// that zeroed a value.
func (e *emitter) writeMemoryDecls() {
	if e.memoryCopyUsed && !e.usesArrayRuntime() {
		e.out.WriteString("declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1)\n\n")
	}
	if e.memorySetUsed {
		e.out.WriteString("declare void @llvm.memset.p0.i64(ptr, i8, i64, i1)\n\n")
	}
}

// boolRangeMetadata is the metadata number of the range a bool's byte is read
// with; it follows the tags writeTBAATable writes.
const boolRangeMetadata = 7

// writeValueLoad reads one value of llvmType from addr into name. keyword is
// "volatile " or empty, align is the alignment the address is known to have
// or 0 for the type's own, and tag is the alias tag suffix.
//
// A bool is the byte 0 or 1 in memory, and is read as that byte with its
// range, then narrowed. An i1 load says nothing about the byte's other seven
// bits, so a loop over []bool cannot be widened into vector loads: each
// element is read alone and masked. The byte and its range let it be.
func (e *emitter) writeValueLoad(name, keyword, llvmType, addr string, align int, tag string) {
	if llvmType != "i1" {
		fmt.Fprintf(&e.out, "  %s = load %s%s, ptr %s%s%s\n",
			name, keyword, llvmType, addr, alignSuffix(align), tag)
		return
	}
	e.boolRangeUsed = true
	fmt.Fprintf(&e.out, "  %s.byte = load %si8, ptr %s%s%s, !range !%d\n",
		name, keyword, addr, alignSuffix(align), tag, boolRangeMetadata)
	fmt.Fprintf(&e.out, "  %s = trunc i8 %s.byte to i1\n", name, name)
}

// writeBoolRange writes the range writeValueLoad names, when a bool was read.
func (e *emitter) writeBoolRange() {
	if !e.boolRangeUsed {
		return
	}
	fmt.Fprintf(&e.out, "!%d = !{i8 0, i8 2}\n", boolRangeMetadata)
}
