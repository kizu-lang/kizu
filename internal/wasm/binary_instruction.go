package wasm

import (
	"strconv"
	"strings"
)

type binaryInstructionEncoder struct {
	module     *binaryModule
	locals     map[string]uint32
	localCount uint32
	labels     []string
}

type binaryInstructionKind uint8

const (
	binarySimpleInstruction binaryInstructionKind = iota
	binaryControlInstruction
	binaryReferenceInstruction
	binaryImmediateInstruction
)

// encodeCode serializes local declarations and instructions for every defined
// function.
func (m *binaryModule) encodeCode() ([]byte, error) {
	if len(m.functions) == 0 {
		return nil, nil
	}
	out := appendU32(nil, uint32(len(m.functions)))
	for _, function := range m.functions {
		body, err := m.encodeFunctionBody(function)
		if err != nil {
			return nil, err
		}
		out = appendName(out, body)
	}
	return out, nil
}

// encodeFunctionBody builds one function's local index space and emits its
// instruction sequence followed by the function end marker.
func (m *binaryModule) encodeFunctionBody(function binaryFunction) ([]byte, error) {
	encoder := binaryInstructionEncoder{
		module: m,
		locals: map[string]uint32{},
	}
	for _, param := range function.params {
		if err := encoder.addLocal(param.name); err != nil {
			return nil, err
		}
	}
	for _, local := range function.locals {
		if err := encoder.addLocal(local.name); err != nil {
			return nil, err
		}
	}
	body := encodeLocalGroups(function.locals)
	children := m.source.nodes[function.node].children
	for _, child := range children[1:] {
		switch m.source.listHead(child) {
		case "param", "result", "local", "export", "type":
			continue
		}
		if m.source.nodeKind(child) == moduleAtom &&
			strings.HasPrefix(m.source.nodeText(child), "$") {
			continue
		}
		var err error
		body, err = encoder.encode(body, child)
		if err != nil {
			return nil, err
		}
	}
	body = append(body, 0x0b)
	return body, nil
}

// addLocal assigns the next local index to one optional symbolic name.
func (e *binaryInstructionEncoder) addLocal(name string) error {
	index := e.localCount
	e.localCount++
	if name == "" {
		return nil
	}
	if _, exists := e.locals[name]; exists {
		return e.module.errorf(e.module.source.root, "duplicate local %s", name)
	}
	e.locals[name] = index
	return nil
}

// encodeLocalGroups run-length encodes consecutive locals of the same type.
func encodeLocalGroups(locals []binaryNamedType) []byte {
	if len(locals) == 0 {
		return []byte{0x00}
	}
	type group struct {
		count    uint32
		typeCode byte
	}
	groups := make([]group, 0, len(locals))
	for _, local := range locals {
		if len(groups) > 0 && groups[len(groups)-1].typeCode == local.typeCode {
			groups[len(groups)-1].count++
			continue
		}
		groups = append(groups, group{count: 1, typeCode: local.typeCode})
	}
	out := appendU32(nil, uint32(len(groups)))
	for _, groupValue := range groups {
		out = appendU32(out, groupValue.count)
		out = append(out, groupValue.typeCode)
	}
	return out
}

// encode emits one folded WAT instruction by first emitting its operands and
// then the binary opcode and immediates.
func (e *binaryInstructionEncoder) encode(out []byte, index int) ([]byte, error) {
	if e.module.source.nodeKind(index) != moduleList {
		return nil, e.module.errorf(index, "instruction must be a list")
	}
	children := e.module.source.nodes[index].children
	if len(children) == 0 {
		return nil, e.module.errorf(index, "instruction cannot be empty")
	}
	op := e.module.source.nodeText(children[0])
	switch classifyInstruction(op) {
	case binaryControlInstruction:
		return e.encodeControl(out, index, op)
	case binaryReferenceInstruction:
		return e.encodeReference(out, index, op)
	case binaryImmediateInstruction:
		return e.encodeImmediateOperation(out, index, op)
	default:
		opcode, ok := simpleInstructionOpcode(op)
		if !ok {
			return nil, e.module.errorf(index, "unsupported instruction %s", op)
		}
		return e.encodeWithOpcode(out, children[1:], opcode)
	}
}

// classifyInstruction separates immediates and structured control from plain
// trailing opcodes.
func classifyInstruction(op string) binaryInstructionKind {
	switch op {
	case "block", "loop", "if", "br", "br_if", "return", "then", "else":
		return binaryControlInstruction
	case "call", "call_indirect", "local.get", "local.set", "local.tee",
		"global.get", "global.set":
		return binaryReferenceInstruction
	case "i32.const", "i64.const":
		return binaryImmediateInstruction
	case "i32.load", "i64.load", "i32.load8_u", "i64.load8_u",
		"i32.store", "i64.store", "i32.store8", "i64.store8":
		return binaryImmediateInstruction
	case "memory.size", "memory.grow", "memory.copy", "memory.fill":
		return binaryImmediateInstruction
	default:
		return binarySimpleInstruction
	}
}

// encodeControl emits one structured control or branch instruction.
func (e *binaryInstructionEncoder) encodeControl(
	out []byte,
	index int,
	op string,
) ([]byte, error) {
	children := e.module.source.nodes[index].children
	switch op {
	case "block":
		return e.encodeBlock(out, index, 0x02)
	case "loop":
		return e.encodeBlock(out, index, 0x03)
	case "if":
		return e.encodeIf(out, index)
	case "br", "br_if":
		return e.encodeBranch(out, index, op == "br_if")
	case "return":
		return e.encodeWithOpcode(out, children[1:], 0x0f)
	default:
		return e.encodeSequence(out, children[1:])
	}
}

// encodeReference emits one instruction with a function, local, or global
// index immediate.
func (e *binaryInstructionEncoder) encodeReference(
	out []byte,
	index int,
	op string,
) ([]byte, error) {
	switch op {
	case "call":
		return e.encodeCall(out, index)
	case "call_indirect":
		return e.encodeCallIndirect(out, index)
	case "local.get", "local.set", "local.tee":
		return e.encodeLocal(out, index, op)
	default:
		return e.encodeGlobal(out, index, op)
	}
}

// encodeImmediateOperation emits numeric constants and memory immediates.
func (e *binaryInstructionEncoder) encodeImmediateOperation(
	out []byte,
	index int,
	op string,
) ([]byte, error) {
	switch op {
	case "i32.const", "i64.const":
		return e.encodeConstant(out, index, op)
	case "i32.load", "i64.load", "i32.load8_u", "i64.load8_u",
		"i32.store", "i64.store", "i32.store8", "i64.store8":
		return e.encodeMemory(out, index, op)
	case "memory.size", "memory.grow":
		return e.encodeMemorySize(out, index, op)
	default:
		return e.encodeBulkMemory(out, index, op)
	}
}

// encodeSequence emits a list of sibling instructions in order.
func (e *binaryInstructionEncoder) encodeSequence(out []byte, nodes []int) ([]byte, error) {
	for _, node := range nodes {
		var err error
		out, err = e.encode(out, node)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// encodeWithOpcode emits folded operands before their trailing opcode.
func (e *binaryInstructionEncoder) encodeWithOpcode(
	out []byte,
	operands []int,
	opcode byte,
) ([]byte, error) {
	out, err := e.encodeSequence(out, operands)
	if err != nil {
		return nil, err
	}
	return append(out, opcode), nil
}

// encodeBlock emits an empty-result block or loop and tracks its symbolic
// branch label.
func (e *binaryInstructionEncoder) encodeBlock(out []byte, index int, opcode byte) ([]byte, error) {
	children := e.module.source.nodes[index].children
	position := 1
	label := ""
	if position < len(children) && e.module.source.nodeKind(children[position]) == moduleAtom &&
		strings.HasPrefix(e.module.source.nodeText(children[position]), "$") {
		label = e.module.source.nodeText(children[position])
		position++
	}
	blockType, err := e.blockType(children, &position)
	if err != nil {
		return nil, err
	}
	out = append(out, opcode, blockType)
	e.labels = append(e.labels, label)
	out, err = e.encodeSequence(out, children[position:])
	e.labels = e.labels[:len(e.labels)-1]
	if err != nil {
		return nil, err
	}
	return append(out, 0x0b), nil
}

// encodeIf emits one condition and its then/else instruction sequences.
func (e *binaryInstructionEncoder) encodeIf(out []byte, index int) ([]byte, error) {
	children := e.module.source.nodes[index].children
	position := 1
	blockType, err := e.blockType(children, &position)
	if err != nil || position+1 >= len(children) ||
		e.module.source.listHead(children[position+1]) != "then" {
		return nil, e.module.errorf(index, "invalid if instruction")
	}
	out, err = e.encode(out, children[position])
	if err != nil {
		return nil, err
	}
	out = append(out, 0x04, blockType)
	e.labels = append(e.labels, "")
	out, err = e.encode(out, children[position+1])
	if err == nil && len(children) == position+3 {
		if e.module.source.listHead(children[position+2]) != "else" {
			err = e.module.errorf(children[position+2], "invalid else instruction")
		} else {
			out = append(out, 0x05)
			out, err = e.encode(out, children[position+2])
		}
	} else if err == nil && len(children) != position+2 {
		err = e.module.errorf(index, "invalid if instruction")
	}
	e.labels = e.labels[:len(e.labels)-1]
	if err != nil {
		return nil, err
	}
	return append(out, 0x0b), nil
}

// blockType consumes an optional single-result declaration and returns the
// compact block type used by generated control instructions.
func (e *binaryInstructionEncoder) blockType(children []int, position *int) (byte, error) {
	if *position >= len(children) || e.module.source.listHead(children[*position]) != "result" {
		return 0x40, nil
	}
	result := e.module.source.nodes[children[*position]].children
	if len(result) != 2 {
		return 0, e.module.errorf(children[*position], "invalid block result")
	}
	typeCode, err := e.module.valueType(result[1])
	if err != nil {
		return 0, err
	}
	*position++
	return typeCode, nil
}

// encodeBranch resolves a label depth after emitting an optional condition.
func (e *binaryInstructionEncoder) encodeBranch(
	out []byte,
	index int,
	conditional bool,
) ([]byte, error) {
	children := e.module.source.nodes[index].children
	want := 2
	if conditional {
		want = 3
	}
	if len(children) != want {
		return nil, e.module.errorf(index, "invalid branch instruction")
	}
	if conditional {
		var err error
		out, err = e.encode(out, children[2])
		if err != nil {
			return nil, err
		}
	}
	depth, err := e.labelDepth(children[1])
	if err != nil {
		return nil, err
	}
	if conditional {
		out = append(out, 0x0d)
	} else {
		out = append(out, 0x0c)
	}
	return appendU32(out, depth), nil
}

// encodeCall emits arguments followed by a direct function reference.
func (e *binaryInstructionEncoder) encodeCall(out []byte, index int) ([]byte, error) {
	children := e.module.source.nodes[index].children
	if len(children) < 2 {
		return nil, e.module.errorf(index, "call needs a function")
	}
	var err error
	out, err = e.encodeSequence(out, children[2:])
	if err != nil {
		return nil, err
	}
	functionIndex, err := e.module.functionIndex(children[1])
	if err != nil {
		return nil, err
	}
	out = append(out, 0x10)
	return appendU32(out, functionIndex), nil
}

// encodeCallIndirect emits arguments and the table index followed by the
// selected signature and table zero.
func (e *binaryInstructionEncoder) encodeCallIndirect(out []byte, index int) ([]byte, error) {
	children := e.module.source.nodes[index].children
	if len(children) < 3 || e.module.source.listHead(children[1]) != "type" {
		return nil, e.module.errorf(index, "call_indirect needs a type and table index")
	}
	typeChildren := e.module.source.nodes[children[1]].children
	if len(typeChildren) != 2 {
		return nil, e.module.errorf(children[1], "invalid call_indirect type")
	}
	var err error
	out, err = e.encodeSequence(out, children[2:])
	if err != nil {
		return nil, err
	}
	typeIndex, err := e.module.typeIndex(typeChildren[1])
	if err != nil {
		return nil, err
	}
	out = append(out, 0x11)
	out = appendU32(out, typeIndex)
	return appendU32(out, 0), nil
}

// encodeLocal emits one local access and resolves symbolic parameter/local
// names in the function-local index space.
func (e *binaryInstructionEncoder) encodeLocal(out []byte, index int, op string) ([]byte, error) {
	children := e.module.source.nodes[index].children
	want := 2
	if op != "local.get" {
		want = 3
	}
	if len(children) != want {
		return nil, e.module.errorf(index, "invalid %s instruction", op)
	}
	if op != "local.get" {
		var err error
		out, err = e.encode(out, children[2])
		if err != nil {
			return nil, err
		}
	}
	localIndex, err := e.localIndex(children[1])
	if err != nil {
		return nil, err
	}
	switch op {
	case "local.get":
		out = append(out, 0x20)
	case "local.set":
		out = append(out, 0x21)
	case "local.tee":
		out = append(out, 0x22)
	}
	return appendU32(out, localIndex), nil
}

// encodeGlobal emits one global access.
func (e *binaryInstructionEncoder) encodeGlobal(out []byte, index int, op string) ([]byte, error) {
	children := e.module.source.nodes[index].children
	want := 2
	if op == "global.set" {
		want = 3
	}
	if len(children) != want {
		return nil, e.module.errorf(index, "invalid %s instruction", op)
	}
	if op == "global.set" {
		var err error
		out, err = e.encode(out, children[2])
		if err != nil {
			return nil, err
		}
	}
	globalIndex, err := e.module.globalIndex(children[1])
	if err != nil {
		return nil, err
	}
	if op == "global.get" {
		out = append(out, 0x23)
	} else {
		out = append(out, 0x24)
	}
	return appendU32(out, globalIndex), nil
}

// encodeConstant emits one signed i32 or i64 constant immediate.
func (e *binaryInstructionEncoder) encodeConstant(
	out []byte,
	index int,
	op string,
) ([]byte, error) {
	children := e.module.source.nodes[index].children
	if len(children) != 2 {
		return nil, e.module.errorf(index, "invalid %s instruction", op)
	}
	value, err := e.module.signed(children[1], 64)
	if err != nil {
		return nil, err
	}
	if op == "i32.const" {
		out = append(out, 0x41)
		return appendI32(out, int32(value)), nil
	}
	out = append(out, 0x42)
	return appendI64(out, value), nil
}

// encodeMemory emits load/store operands and the natural generated memarg.
func (e *binaryInstructionEncoder) encodeMemory(out []byte, index int, op string) ([]byte, error) {
	children := e.module.source.nodes[index].children
	opcode, alignment := memoryInstruction(op)
	offset := uint32(0)
	var operands []int
	for _, child := range children[1:] {
		if e.module.source.nodeKind(child) == moduleAtom {
			text := e.module.source.nodeText(child)
			if strings.HasPrefix(text, "offset=") {
				value, err := strconv.ParseUint(strings.TrimPrefix(text, "offset="), 10, 32)
				if err != nil {
					return nil, e.module.errorf(child, "invalid memory offset")
				}
				offset = uint32(value)
				continue
			}
			if strings.HasPrefix(text, "align=") {
				value, err := strconv.ParseUint(strings.TrimPrefix(text, "align="), 10, 32)
				if err != nil || value == 0 || value&(value-1) != 0 {
					return nil, e.module.errorf(child, "invalid memory alignment")
				}
				alignment = uint32(0)
				for value > 1 {
					alignment++
					value >>= 1
				}
				continue
			}
		}
		operands = append(operands, child)
	}
	var err error
	out, err = e.encodeSequence(out, operands)
	if err != nil {
		return nil, err
	}
	out = append(out, opcode)
	out = appendU32(out, alignment)
	return appendU32(out, offset), nil
}

// encodeMemorySize emits memory.size or memory.grow for memory zero.
func (e *binaryInstructionEncoder) encodeMemorySize(
	out []byte,
	index int,
	op string,
) ([]byte, error) {
	children := e.module.source.nodes[index].children
	if op == "memory.size" {
		if len(children) != 1 {
			return nil, e.module.errorf(index, "invalid memory.size instruction")
		}
		return append(out, 0x3f, 0x00), nil
	}
	if len(children) != 2 {
		return nil, e.module.errorf(index, "invalid memory.grow instruction")
	}
	var err error
	out, err = e.encode(out, children[1])
	if err != nil {
		return nil, err
	}
	return append(out, 0x40, 0x00), nil
}

// encodeBulkMemory emits memory.copy or memory.fill for memory zero.
func (e *binaryInstructionEncoder) encodeBulkMemory(
	out []byte,
	index int,
	op string,
) ([]byte, error) {
	children := e.module.source.nodes[index].children
	want := 4
	if len(children) != want {
		return nil, e.module.errorf(index, "invalid %s instruction", op)
	}
	var err error
	out, err = e.encodeSequence(out, children[1:])
	if err != nil {
		return nil, err
	}
	out = append(out, 0xfc)
	if op == "memory.copy" {
		out = appendU32(out, 10)
		return append(out, 0x00, 0x00), nil
	}
	out = appendU32(out, 11)
	return append(out, 0x00), nil
}

// localIndex resolves a symbolic or numeric local reference.
func (e *binaryInstructionEncoder) localIndex(index int) (uint32, error) {
	if e.module.source.nodeKind(index) != moduleAtom {
		return 0, e.module.errorf(index, "expected local reference")
	}
	text := e.module.source.nodeText(index)
	if strings.HasPrefix(text, "$") {
		value, ok := e.locals[text]
		if !ok {
			return 0, e.module.errorf(index, "unknown local %s", text)
		}
		return value, nil
	}
	return e.module.unsigned(index)
}

// labelDepth resolves one symbolic or numeric branch target from innermost to
// outermost control frame.
func (e *binaryInstructionEncoder) labelDepth(index int) (uint32, error) {
	if e.module.source.nodeKind(index) != moduleAtom {
		return 0, e.module.errorf(index, "expected branch label")
	}
	text := e.module.source.nodeText(index)
	if !strings.HasPrefix(text, "$") {
		return e.module.unsigned(index)
	}
	for position := len(e.labels) - 1; position >= 0; position-- {
		if e.labels[position] == text {
			return uint32(len(e.labels) - 1 - position), nil
		}
	}
	return 0, e.module.errorf(index, "unknown branch label %s", text)
}

// memoryInstruction returns the opcode and natural alignment exponent for a
// supported memory operation.
func memoryInstruction(op string) (byte, uint32) {
	switch op {
	case "i32.load":
		return 0x28, 2
	case "i64.load":
		return 0x29, 3
	case "i32.load8_u":
		return 0x2d, 0
	case "i64.load8_u":
		return 0x31, 0
	case "i32.store":
		return 0x36, 2
	case "i64.store":
		return 0x37, 3
	case "i32.store8":
		return 0x3a, 0
	case "i64.store8":
		return 0x3c, 0
	default:
		return 0, 0
	}
}

// simpleInstructionOpcode returns the single-byte opcode for instructions
// without immediates.
func simpleInstructionOpcode(op string) (byte, bool) {
	if opcode, ok := basicInstructionOpcode(op); ok {
		return opcode, true
	}
	if opcode, ok := i32ComparisonOpcode(op); ok {
		return opcode, true
	}
	if opcode, ok := i64ComparisonOpcode(op); ok {
		return opcode, true
	}
	if opcode, ok := i32NumericOpcode(op); ok {
		return opcode, true
	}
	if opcode, ok := i64NumericOpcode(op); ok {
		return opcode, true
	}
	return conversionOpcode(op)
}

// basicInstructionOpcode maps control-neutral stack instructions.
func basicInstructionOpcode(op string) (byte, bool) {
	switch op {
	case "unreachable":
		return 0x00, true
	case "nop":
		return 0x01, true
	case "drop":
		return 0x1a, true
	case "select":
		return 0x1b, true
	default:
		return 0, false
	}
}

// i32ComparisonOpcode maps 32-bit integer tests and comparisons.
func i32ComparisonOpcode(op string) (byte, bool) {
	switch op {
	case "i32.eqz":
		return 0x45, true
	case "i32.eq":
		return 0x46, true
	case "i32.ne":
		return 0x47, true
	case "i32.lt_s":
		return 0x48, true
	case "i32.lt_u":
		return 0x49, true
	case "i32.gt_s":
		return 0x4a, true
	case "i32.gt_u":
		return 0x4b, true
	case "i32.le_s":
		return 0x4c, true
	case "i32.le_u":
		return 0x4d, true
	case "i32.ge_s":
		return 0x4e, true
	case "i32.ge_u":
		return 0x4f, true
	default:
		return 0, false
	}
}

// i64ComparisonOpcode maps 64-bit integer tests and comparisons.
func i64ComparisonOpcode(op string) (byte, bool) {
	switch op {
	case "i64.eqz":
		return 0x50, true
	case "i64.eq":
		return 0x51, true
	case "i64.ne":
		return 0x52, true
	case "i64.lt_s":
		return 0x53, true
	case "i64.lt_u":
		return 0x54, true
	case "i64.gt_s":
		return 0x55, true
	case "i64.gt_u":
		return 0x56, true
	case "i64.le_s":
		return 0x57, true
	case "i64.le_u":
		return 0x58, true
	case "i64.ge_s":
		return 0x59, true
	case "i64.ge_u":
		return 0x5a, true
	default:
		return 0, false
	}
}

// i32NumericOpcode maps 32-bit integer arithmetic and bit operations.
func i32NumericOpcode(op string) (byte, bool) {
	if opcode, ok := i32ArithmeticOpcode(op); ok {
		return opcode, true
	}
	return i32BitOpcode(op)
}

// i32ArithmeticOpcode maps 32-bit arithmetic operations.
func i32ArithmeticOpcode(op string) (byte, bool) {
	switch op {
	case "i32.add":
		return 0x6a, true
	case "i32.sub":
		return 0x6b, true
	case "i32.mul":
		return 0x6c, true
	case "i32.div_s":
		return 0x6d, true
	case "i32.div_u":
		return 0x6e, true
	case "i32.rem_s":
		return 0x6f, true
	case "i32.rem_u":
		return 0x70, true
	default:
		return 0, false
	}
}

// i32BitOpcode maps 32-bit bitwise, shift, and rotate operations.
func i32BitOpcode(op string) (byte, bool) {
	switch op {
	case "i32.and":
		return 0x71, true
	case "i32.or":
		return 0x72, true
	case "i32.xor":
		return 0x73, true
	case "i32.shl":
		return 0x74, true
	case "i32.shr_s":
		return 0x75, true
	case "i32.shr_u":
		return 0x76, true
	case "i32.rotl":
		return 0x77, true
	case "i32.rotr":
		return 0x78, true
	default:
		return 0, false
	}
}

// i64NumericOpcode maps 64-bit integer arithmetic and bit operations.
func i64NumericOpcode(op string) (byte, bool) {
	if opcode, ok := i64ArithmeticOpcode(op); ok {
		return opcode, true
	}
	return i64BitOpcode(op)
}

// i64ArithmeticOpcode maps 64-bit arithmetic operations.
func i64ArithmeticOpcode(op string) (byte, bool) {
	switch op {
	case "i64.add":
		return 0x7c, true
	case "i64.sub":
		return 0x7d, true
	case "i64.mul":
		return 0x7e, true
	case "i64.div_s":
		return 0x7f, true
	case "i64.div_u":
		return 0x80, true
	case "i64.rem_s":
		return 0x81, true
	case "i64.rem_u":
		return 0x82, true
	default:
		return 0, false
	}
}

// i64BitOpcode maps 64-bit bitwise, shift, and rotate operations.
func i64BitOpcode(op string) (byte, bool) {
	switch op {
	case "i64.and":
		return 0x83, true
	case "i64.or":
		return 0x84, true
	case "i64.xor":
		return 0x85, true
	case "i64.shl":
		return 0x86, true
	case "i64.shr_s":
		return 0x87, true
	case "i64.shr_u":
		return 0x88, true
	case "i64.rotl":
		return 0x89, true
	case "i64.rotr":
		return 0x8a, true
	default:
		return 0, false
	}
}

// conversionOpcode maps integer-width conversion instructions.
func conversionOpcode(op string) (byte, bool) {
	switch op {
	case "i32.wrap_i64":
		return 0xa7, true
	case "i64.extend_i32_s":
		return 0xac, true
	case "i64.extend_i32_u":
		return 0xad, true
	default:
		return 0, false
	}
}
