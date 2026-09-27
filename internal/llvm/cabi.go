package llvm

import (
	"fmt"
	"strings"
)

// Arch is the machine a native module is built for, which decides how a C
// struct travels by value.
type Arch int

const (
	// ArchArm64 follows AAPCS64, on Darwin and Linux alike.
	ArchArm64 Arch = iota
	// ArchX86_64 follows the System V AMD64 ABI.
	ArchX86_64
	// ArchOther names a machine whose C convention the emitter does not
	// follow; a C struct by value is refused there.
	ArchOther
)

// A C struct passed or returned by value (SPEC §12.2) travels the way the
// target's C calling convention says. LLVM leaves that to the frontend: clang
// rewrites each struct into the registers or the memory the convention names,
// and so does this file, with the types clang uses. A C struct holds only
// integers, floats, bool, pointers and other C structs, so no leaf is wider or
// more aligned than 8 bytes and no vector or long double reaches the rules.

// cPassKind is where a C struct by value goes.
type cPassKind int

const (
	// cPassDirect loads the struct as the register-sized parts in cPassing.
	cPassDirect cPassKind = iota
	// cPassIndirect hands the address of a copy (AAPCS64 above 16 bytes; a
	// return goes through `sret` storage the caller provides).
	cPassIndirect
	// cPassByval copies the struct onto the stack (x86-64 memory class).
	cPassByval
)

// cPassing is how one C struct crosses a call.
type cPassing struct {
	kind cPassKind
	// parts are the LLVM types of the registers a direct struct fills, in
	// order; each is loaded from the struct's storage at 8-byte steps.
	parts []string
	// alignStack is AAPCS64's `alignstack(8)`, which Linux, unlike Darwin,
	// puts on a float aggregate.
	alignStack bool
	align      int
}

// cLeaf is one scalar of a flattened C struct.
type cLeaf struct {
	offset int
	size   int
	float  bool
}

// isCStructValue reports whether typ is a struct passed to C by value. The
// checker lets only C structs reach a C boundary, so any struct is one.
func (e *emitter) isCStructValue(typ string) bool {
	_, ok := e.module.Structs[typ]
	return ok
}

// cStructLeaves flattens a C struct into its scalars at their byte offsets and
// returns its size and alignment, laid out by the C rules.
func (e *emitter) cStructLeaves(typ string, base int, leaves []cLeaf) ([]cLeaf, int, int) {
	st := e.module.Structs[typ]
	size, align := 0, 1
	for _, field := range st.Fields {
		var fieldSize, fieldAlign int
		if _, nested := e.module.Structs[field.Type]; nested {
			var inner []cLeaf
			inner, fieldSize, fieldAlign = e.cStructLeaves(field.Type, 0, nil)
			offset := roundUp(size, fieldAlign)
			for _, leaf := range inner {
				leaf.offset += base + offset
				leaves = append(leaves, leaf)
			}
			size = offset + fieldSize
		} else {
			fieldSize, fieldAlign = cScalarLayout(field.Type)
			offset := roundUp(size, fieldAlign)
			leaves = append(leaves, cLeaf{
				offset: base + offset,
				size:   fieldSize,
				float:  field.Type == "f32" || field.Type == "f64",
			})
			size = offset + fieldSize
		}
		if fieldAlign > align {
			align = fieldAlign
		}
	}
	return leaves, roundUp(size, align), align
}

// cScalarLayout is the size and alignment of a C struct field that is not a
// struct: an integer, a float, bool, or a pointer of any kind.
func cScalarLayout(typ string) (int, int) {
	if size, align, ok := primitiveLayout(typ); ok {
		return size, align
	}
	return 8, 8
}

// cRegisters counts the x86-64 registers a call has left while its arguments
// are assigned, which decides whether a struct still fits in them.
type cRegisters struct {
	integer int
	sse     int
}

// newCRegisters is the x86-64 argument registers before any is taken; an
// `sret` return takes the first integer one.
func newCRegisters(sret bool) cRegisters {
	regs := cRegisters{integer: 6, sse: 8}
	if sret {
		regs.integer--
	}
	return regs
}

// takeScalar takes the register a scalar argument of type typ uses.
func (r *cRegisters) takeScalar(typ string) {
	if typ == "f32" || typ == "f64" {
		r.sse--
		return
	}
	r.integer--
}

// cArgPassing is how a C struct argument of type typ goes to the callee.
func (e *emitter) cArgPassing(typ string, regs *cRegisters) cPassing {
	leaves, size, align := e.cStructLeaves(typ, 0, nil)
	if e.arch == ArchX86_64 {
		parts, ok := sysVParts(leaves, size)
		if !ok {
			return cPassing{kind: cPassByval, align: align}
		}
		integer, sse := 0, 0
		for _, part := range parts {
			if isSSEPart(part) {
				sse++
			} else {
				integer++
			}
		}
		if integer > regs.integer || sse > regs.sse {
			return cPassing{kind: cPassByval, align: align}
		}
		regs.integer -= integer
		regs.sse -= sse
		return cPassing{kind: cPassDirect, parts: parts, align: align}
	}
	if elem, count, ok := homogeneousFloats(leaves); ok {
		return cPassing{
			kind:       cPassDirect,
			parts:      []string{fmt.Sprintf("[%d x %s]", count, elem)},
			alignStack: !e.darwin,
			align:      align,
		}
	}
	if size > 16 {
		return cPassing{kind: cPassIndirect, align: align}
	}
	return cPassing{kind: cPassDirect, parts: []string{aapcsWords(size)}, align: align}
}

// cReturnPassing is how a C struct result of type typ comes back.
func (e *emitter) cReturnPassing(typ string) cPassing {
	leaves, size, align := e.cStructLeaves(typ, 0, nil)
	if e.arch == ArchX86_64 {
		parts, ok := sysVParts(leaves, size)
		if !ok {
			return cPassing{kind: cPassIndirect, align: align}
		}
		return cPassing{kind: cPassDirect, parts: parts, align: align}
	}
	if elem, count, ok := homogeneousFloats(leaves); ok {
		fields := make([]string, count)
		for index := range fields {
			fields[index] = elem
		}
		return cPassing{
			kind:  cPassDirect,
			parts: []string{"{ " + strings.Join(fields, ", ") + " }"},
			align: align,
		}
	}
	if size > 16 {
		return cPassing{kind: cPassIndirect, align: align}
	}
	return cPassing{kind: cPassDirect, parts: []string{aapcsWords(size)}, align: align}
}

// homogeneousFloats reports an AAPCS64 homogeneous float aggregate: one to
// four leaves, every one the same float type. It travels in float registers.
func homogeneousFloats(leaves []cLeaf) (string, int, bool) {
	if len(leaves) == 0 || len(leaves) > 4 {
		return "", 0, false
	}
	for _, leaf := range leaves {
		if !leaf.float || leaf.size != leaves[0].size {
			return "", 0, false
		}
	}
	if leaves[0].size == 4 {
		return "float", len(leaves), true
	}
	return "double", len(leaves), true
}

// aapcsWords is the integer registers an AAPCS64 struct of at most 16 bytes
// fills: one word, or two.
func aapcsWords(size int) string {
	if size <= 8 {
		return "i64"
	}
	return "[2 x i64]"
}

// sysVParts classifies an x86-64 struct of at most 16 bytes into the register
// each eightbyte goes in: an integer register when it holds any integer or
// pointer, otherwise a float register. ok is false for a larger struct, which
// the convention places in memory.
func sysVParts(leaves []cLeaf, size int) ([]string, bool) {
	if size > 16 {
		return nil, false
	}
	var parts []string
	for start := 0; start < size; start += 8 {
		var floats []cLeaf
		integer := false
		for _, leaf := range leaves {
			if leaf.offset < start || leaf.offset >= start+8 {
				continue
			}
			if !leaf.float {
				integer = true
			}
			floats = append(floats, leaf)
		}
		switch {
		case integer:
			parts = append(parts, sysVIntegerPart(size-start))
		case len(floats) == 2:
			parts = append(parts, "<2 x float>")
		case len(floats) == 1 && floats[0].size == 4:
			parts = append(parts, "float")
		default:
			parts = append(parts, "double")
		}
	}
	return parts, true
}

// sysVIntegerPart is the integer an eightbyte loads as: the bytes left of the
// struct, at most a word.
func sysVIntegerPart(left int) string {
	if left >= 8 {
		return "i64"
	}
	return fmt.Sprintf("i%d", left*8)
}

// isSSEPart reports whether a register part is a float register.
func isSSEPart(part string) bool {
	return part == "float" || part == "double" || part == "<2 x float>"
}

// cStructSlot allocates storage for a C struct of type typ wide enough to load
// its register parts from, and returns its address.
func (e *emitter) cStructSlot(typ string, index int) string {
	_, size, _ := e.cStructLeaves(typ, 0, nil)
	slot := "%" + e.nextSyntheticValue(fmt.Sprintf("cabi.%d", index))
	fmt.Fprintf(&e.out, "  %s = alloca [%d x i64], align 8\n", slot, roundUp(size, 8)/8)
	return slot
}

// cStructArgs writes the storage a C struct argument is passed through and
// returns the call operands that pass it.
func (e *emitter) cStructArgs(operand string, typ string, passing cPassing, index int) []string {
	structType := e.llvmType(typ)
	slot := e.cStructSlot(typ, index)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s, align 8\n", structType, operand, slot)
	switch passing.kind {
	case cPassIndirect:
		return []string{"ptr " + slot}
	case cPassByval:
		return []string{fmt.Sprintf("ptr byval(%s) align %d %s", structType, passing.align, slot)}
	}
	args := make([]string, 0, len(passing.parts))
	for part, partType := range passing.parts {
		address := e.cPartAddress(slot, part, index)
		loaded := "%" + e.nextSyntheticValue(fmt.Sprintf("cabi.%d.part", index))
		fmt.Fprintf(&e.out, "  %s = load %s, ptr %s, align 8\n", loaded, partType, address)
		args = append(args, cParamType(partType, passing)+" "+loaded)
	}
	return args
}

// cPartAddress is the address of register part number part inside slot.
func (e *emitter) cPartAddress(slot string, part int, index int) string {
	if part == 0 {
		return slot
	}
	address := "%" + e.nextSyntheticValue(fmt.Sprintf("cabi.%d.at", index))
	fmt.Fprintf(&e.out, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n",
		address, slot, part*8)
	return address
}

// cParamType spells one parameter part with the attributes it carries.
func cParamType(partType string, passing cPassing) string {
	if passing.alignStack {
		return partType + " alignstack(8)"
	}
	return partType
}

// cDeclParams spells the parameters a C struct argument declares.
func (e *emitter) cDeclParams(typ string, passing cPassing) []string {
	switch passing.kind {
	case cPassIndirect:
		return []string{"ptr"}
	case cPassByval:
		return []string{fmt.Sprintf("ptr byval(%s) align %d", e.llvmType(typ), passing.align)}
	}
	params := make([]string, 0, len(passing.parts))
	for _, part := range passing.parts {
		params = append(params, cParamType(part, passing))
	}
	return params
}

// cReturnType is the LLVM result a direct C struct return comes back as.
func cReturnType(passing cPassing) string {
	if len(passing.parts) == 1 {
		return passing.parts[0]
	}
	return "{ " + strings.Join(passing.parts, ", ") + " }"
}

// cSretParam spells the parameter a C struct returned in memory is written
// through.
func (e *emitter) cSretParam(typ string, passing cPassing) string {
	return fmt.Sprintf("ptr sret(%s) align %d", e.llvmType(typ), passing.align)
}

// cArg is one argument of a C call. A struct by value names its Kizu type in
// structType; any other argument is already spelled as the operand it is.
type cArg struct {
	// typ is the argument's Kizu type, which is what decides the register a
	// scalar takes; a pointer the call makes is spelled "ptr".
	typ string
	// operand is the typed call operand of an argument that is not a struct
	// by value, or the value of one that is.
	operand string
	// param is the declared parameter of an argument that is not a struct by
	// value.
	param   string
	byValue bool
}

// usesCStructValues reports whether a C call passes or returns a struct by
// value, which is what needs the convention's rewriting.
func (e *emitter) usesCStructValues(result string, args []cArg) bool {
	if e.isCStructValue(result) {
		return true
	}
	for _, arg := range args {
		if arg.byValue {
			return true
		}
	}
	return false
}

// cCallSignature is the declared result and parameters of a C function whose
// arguments or result include a struct by value.
func (e *emitter) cCallSignature(result string, args []cArg) (string, []string) {
	resultType := e.llvmType(result)
	var params []string
	sret := false
	if e.isCStructValue(result) {
		passing := e.cReturnPassing(result)
		if passing.kind == cPassIndirect {
			sret = true
			resultType = "void"
			params = append(params, e.cSretParam(result, passing))
		} else {
			resultType = cReturnType(passing)
		}
	}
	regs := newCRegisters(sret)
	for _, arg := range args {
		if !arg.byValue {
			regs.takeScalar(arg.typ)
			params = append(params, arg.param)
			continue
		}
		params = append(params, e.cDeclParams(arg.typ, e.cArgPassing(arg.typ, &regs))...)
	}
	return resultType, params
}

// writeCStructCall writes a C call whose arguments or result include a struct
// by value, rewritten the way the target's convention passes them, and
// returns the operand that holds the result in its Kizu type ("" for void).
func (e *emitter) writeCStructCall(symbol string, result string, args []cArg) (string, error) {
	if e.arch == ArchOther {
		return "", fmt.Errorf("llvm error: C function `%s` passes a struct by value, "+
			"and this target has no C calling convention the compiler follows for one", symbol)
	}
	resultType := e.llvmType(result)
	var operands []string
	sret := ""
	var direct cPassing
	if e.isCStructValue(result) {
		direct = e.cReturnPassing(result)
		if direct.kind == cPassIndirect {
			sret = e.cStructSlot(result, len(args))
			resultType = "void"
			operands = append(operands, e.cSretParam(result, direct)+" "+sret)
		} else {
			resultType = cReturnType(direct)
		}
	}
	regs := newCRegisters(sret != "")
	for index, arg := range args {
		if !arg.byValue {
			regs.takeScalar(arg.typ)
			operands = append(operands, arg.operand)
			continue
		}
		operands = append(operands,
			e.cStructArgs(arg.operand, arg.typ, e.cArgPassing(arg.typ, &regs), index)...)
	}
	call := fmt.Sprintf("call %s @%s(%s)", resultType, symbol, strings.Join(operands, ", "))
	if result == "void" {
		fmt.Fprintf(&e.out, "  %s\n", call)
		return "", nil
	}
	if !e.isCStructValue(result) {
		value := "%" + e.nextSyntheticValue("cabi.result")
		fmt.Fprintf(&e.out, "  %s = %s\n", value, call)
		return value, nil
	}
	if sret == "" {
		sret = e.cStructSlot(result, len(args))
		returned := "%" + e.nextSyntheticValue("cabi.returned")
		fmt.Fprintf(&e.out, "  %s = %s\n", returned, call)
		fmt.Fprintf(&e.out, "  store %s %s, ptr %s, align 8\n", resultType, returned, sret)
	} else {
		fmt.Fprintf(&e.out, "  %s\n", call)
	}
	value := "%" + e.nextSyntheticValue("cabi.result")
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s, align 8\n", value, e.llvmType(result), sret)
	return value, nil
}
