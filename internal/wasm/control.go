package wasm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// writeTree writes one block and everything it dominates, the Go
// spelling of Ramsey's doTree: a loop header's code sits inside a `loop`,
// and the merge nodes it dominates are given their `block`s by
// writeNodeWithin.
func (e *emitter) writeTree(s *structure, name string) error {
	if s.loopHeaders[name] {
		started := e.writeLoopHoists(name)
		defer e.endLoopHoists(started)
		fmt.Fprintf(&e.out, "        (loop %s\n", loopLabel(name))
	}
	if err := e.writeNodeWithin(s, name, s.mergeChildren[name]); err != nil {
		return err
	}
	if s.loopHeaders[name] {
		e.out.WriteString("        )\n")
	}
	return nil
}

// writeNodeWithin writes a block's code under the `block`s of the merge
// nodes it dominates, latest first so that the earliest is innermost: each
// `block` ends exactly where the code of its merge node begins, which is
// what a branch to it reaches.
func (e *emitter) writeNodeWithin(s *structure, name string, merges []string) error {
	if len(merges) > 0 {
		fmt.Fprintf(&e.out, "        (block %s\n", blockLabel(merges[0]))
		if err := e.writeNodeWithin(s, name, merges[1:]); err != nil {
			return err
		}
		e.out.WriteString("        )\n")
		return e.writeTree(s, merges[0])
	}
	block := s.blocks[name]
	for _, instr := range block.Instrs {
		if instr.Op == "phi" {
			continue
		}
		if err := e.writeInstr(instr); err != nil {
			return err
		}
	}
	return e.writeTerminator(s, block)
}

// writeInstr writes one WebAssembly instruction sequence.
func (e *emitter) writeInstr(instr *ir.Instr) error {
	switch {
	case instr.Op == "const":
		return e.writeConst(instr)
	case strings.HasPrefix(instr.Op, "binary."):
		return e.writeBinary(instr)
	case strings.HasPrefix(instr.Op, "unary."):
		return e.writeUnary(instr)
	case instr.Op == "cond_fail":
		return e.writeCondFail(instr)
	case instr.Op == "panic.fail":
		return e.writePanicFail(instr)
	case strings.HasPrefix(instr.Op, "test."):
		return e.writeTestInstr(instr)
	case instr.Op == "print.line":
		return e.writePrintLine(instr)
	case strings.HasPrefix(instr.Op, "func.addr."), strings.HasPrefix(instr.Op, "call."):
		return e.writeCallableInstr(instr)
	case instr.Op == "cast":
		return e.writeCast(instr)
	case instr.Op == "float.bits", instr.Op == "float.from_bits":
		return e.writeFloatBits(instr)
	case floatUnaryInstructions[instr.Op] != "":
		return e.writeFloatUnary(instr)
	case instr.Op == "table.get":
		return e.writeTableGet(instr)
	case instr.Op == "float.fma":
		// wasm has no fused multiply-add; std::math reaches the primitive
		// only under std::target::is_native().
		return fmt.Errorf("wasm error: %s has no wasm instruction", instr.Op)
	case instr.Op == "buffer.new", instr.Op == "buffer.as_bytes":
		return e.writeBufferInstr(instr)
	default:
		return e.writeMemoryInstr(instr)
	}
}

// writeMemoryInstr writes memory-backed values and the opaque operations that
// remain outside the target subset.
func (e *emitter) writeMemoryInstr(instr *ir.Instr) error {
	switch {
	case instr.Op == "struct.new":
		return e.writeStructNew(instr)
	case strings.HasPrefix(instr.Op, "field."):
		return e.writeFieldInstr(instr)
	case instr.Op == "local.slot":
		return e.writeLocalSlot(instr)
	case instr.Op == "ref.store":
		return e.writeRefStore(instr)
	case instr.Op == "ref.load":
		return e.writeRefLoad(instr)
	case strings.HasPrefix(instr.Op, "union."):
		return e.writeUnionInstr(instr)
	case strings.HasPrefix(instr.Op, "slice."):
		return e.writeSliceInstr(instr)
	case isTaggedOwnerOp(instr.Op):
		return e.writeTaggedOwnerInstr(instr)
	default:
		return fmt.Errorf("wasm error: unsupported instruction `%s`", instr.Op)
	}
}

// isTaggedOwnerOp reports whether an operation uses a generic value runtime.
func isTaggedOwnerOp(op string) bool {
	return strings.HasPrefix(op, "opt.") || strings.HasPrefix(op, "error.") ||
		strings.HasPrefix(op, "array.") || strings.HasPrefix(op, "map.") ||
		strings.HasPrefix(op, "box.") ||
		strings.HasPrefix(op, "arena.")
}

// writeTaggedOwnerInstr selects the tagged-value or owner runtime.
func (e *emitter) writeTaggedOwnerInstr(instr *ir.Instr) error {
	if strings.HasPrefix(instr.Op, "array.") {
		return e.writeArrayInstr(instr)
	}
	if strings.HasPrefix(instr.Op, "map.") {
		return e.writeMapInstr(instr)
	}
	if strings.HasPrefix(instr.Op, "box.") {
		return e.writeBoxInstr(instr)
	}
	if strings.HasPrefix(instr.Op, "arena.") {
		return e.writeArenaInstr(instr)
	}
	return e.writeTaggedInstr(instr)
}

// writeCast preserves integer-width casts in the shared i64 representation and
// converts when a raw wasm32 pointer crosses the integer boundary.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: cast expects 1 arg")
	}
	value := e.value(instr.Args[0])
	if isFloatType(instr.Args[0].Type) || isFloatType(instr.Result.Type) {
		e.values[instr.Result.Name] = valueInfo{
			expr: floatCastExpr(instr.Args[0].Type, instr.Result.Type, value.expr),
		}
		return nil
	}
	source := e.wasmType(instr.Args[0].Type)
	target := e.wasmType(instr.Result.Type)
	expr := value.expr
	if source == "i64" && target == "i32" {
		expr = "(i32.wrap_i64 " + expr + ")"
	} else if source == "i32" && target == "i64" {
		expr = "(i64.extend_i32_u " + expr + ")"
	}
	e.values[instr.Result.Name] = valueInfo{expr: expr}
	return nil
}

// floatCastExpr writes a cast with a floating-point side (SPEC §6.9.3). An
// integer widens to a float by rounding; a float narrows to an integer by
// truncating toward zero and saturating, with NaN going to 0, which is what
// the saturating truncation instructions do; the narrower integer types are
// clamped to their own range afterwards. `f32` and `f64` convert by
// rounding, and a cast to the same type is the value itself.
func floatCastExpr(source string, target string, value string) string {
	switch {
	case source == target:
		return value
	case isFloatType(source) && isFloatType(target):
		if target == "f32" {
			return "(f32.demote_f64 " + value + ")"
		}
		return "(f64.promote_f32 " + value + ")"
	case isFloatType(source):
		sign := "_s"
		if isUnsignedIntegerType(target) {
			sign = "_u"
		}
		truncated := "(i64.trunc_sat_" + source + sign + " " + value + ")"
		return clampToIntegerRange(target, truncated)
	default:
		sign := "_s"
		if isUnsignedIntegerType(source) {
			sign = "_u"
		}
		return "(" + target + ".convert_i64" + sign + " " + value + ")"
	}
}

// clampToIntegerRange saturates an i64 that has already been truncated from a
// float into the range of a narrower integer type, so `cast<u8>(300.0)` is
// 255 the way it is on the native target.
func clampToIntegerRange(typ string, value string) string {
	var low, high int64
	switch typ {
	case "i8":
		low, high = -128, 127
	case "i16":
		low, high = -32768, 32767
	case "i32":
		low, high = -2147483648, 2147483647
	case "u8":
		low, high = 0, 255
	case "u16":
		low, high = 0, 65535
	case "u32":
		low, high = 0, 4294967295
	default:
		return value
	}
	clampedLow := fmt.Sprintf("(select (i64.const %d) %s (i64.lt_s %s (i64.const %d)))",
		low, value, value, low)
	return fmt.Sprintf("(select (i64.const %d) %s (i64.gt_s %s (i64.const %d)))",
		high, clampedLow, clampedLow, high)
}

// writeFloatBits reinterprets a float as its bits or bits as a float, the
// `std::float::bits` and `std::float::from_bits` primitives.
func (e *emitter) writeFloatBits(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: %s expects 1 arg", instr.Op)
	}
	value := e.value(instr.Args[0]).expr
	expr := "(f64.reinterpret_i64 " + value + ")"
	if instr.Op == "float.bits" {
		expr = "(i64.reinterpret_f64 " + value + ")"
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// floatUnaryInstructions names the wasm instruction behind each one-operand
// float instruction of `std::math`. Each is one IEEE 754 operation, so it
// answers the same bits the native target does.
var floatUnaryInstructions = map[string]string{
	"float.sqrt":  "f64.sqrt",
	"float.floor": "f64.floor",
	"float.ceil":  "f64.ceil",
	"float.trunc": "f64.trunc",
}

// writeFloatUnary applies a one-operand float instruction.
func (e *emitter) writeFloatUnary(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: %s expects 1 arg", instr.Op)
	}
	value := e.value(instr.Args[0]).expr
	expr := "(" + floatUnaryInstructions[instr.Op] + " " + value + ")"
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeTableGet reads one value of a table.get from its data segment: the
// index is clamped to the last entry, so a value past the end gets the
// fall-through's.
func (e *emitter) writeTableGet(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: %s expects 1 arg", instr.Op)
	}
	last := len(ir.TableValues(instr)) - 1
	index := e.value(instr.Args[0]).expr
	at := fmt.Sprintf("(select %s (i64.const %d) (i64.lt_u %s (i64.const %d)))",
		index, last, index, last)
	address := fmt.Sprintf("(i32.add (i32.const %d) (i32.wrap_i64 (i64.shl %s (i64.const 3))))",
		e.tables[instr], at)
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s (i64.load %s))\n", symbol, address)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeConst records scalar and string constants.
func (e *emitter) writeConst(instr *ir.Instr) error {
	if isIntegerType(instr.Result.Type) || e.isNamedI64Type(instr.Result.Type) {
		// Every scalar integer is one wasm i64, so a constant of any width is
		// written the same way.
		e.values[instr.Result.Name] = valueInfo{
			expr: "(i64.const " + instr.Immediate + ")",
		}
		return nil
	}
	if isFloatType(instr.Result.Type) {
		expr, ok := floatConstExpr(instr.Result.Type, instr.Immediate)
		if !ok {
			return fmt.Errorf("wasm error: invalid float constant `%s`", instr.Immediate)
		}
		e.values[instr.Result.Name] = valueInfo{expr: expr}
		return nil
	}
	switch instr.Result.Type {
	case "bool":
		e.values[instr.Result.Name] = valueInfo{expr: wasmBool(instr.Immediate)}
	case "[]u8":
		ref := e.strings[instr.Immediate]
		slot, err := e.resultSlot(instr.Result)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.out, "            (i32.store %s (i32.const %d))\n", slot, ref.offset)
		fmt.Fprintf(&e.out, "            (i32.store %s (i32.const %d))\n",
			addressAt(slot, 4), ref.length)
		e.values[instr.Result.Name] = valueInfo{expr: slot}
	default:
		return fmt.Errorf("wasm error: unsupported const type `%s`", instr.Result.Type)
	}
	return nil
}

// writeBinary writes arithmetic and comparison local assignments.
func (e *emitter) writeBinary(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("wasm error: binary expects 2 args")
	}
	op := strings.TrimPrefix(instr.Op, "binary.")
	if instr.Args[0].Type == "[]u8" {
		return e.writeByteEquality(instr, op)
	}
	left := e.value(instr.Args[0]).expr
	right := e.value(instr.Args[1]).expr
	if op == "<<" || op == ">>" {
		return e.writeShift(instr, op, left, right)
	}
	wasmOp := wasmBinaryOp(op, instr.Result.Type)
	if isFloatType(instr.Result.Type) {
		wasmOp = wasmFloatBinaryOp(op, instr.Result.Type)
	}
	if instr.Result.Type == "bool" {
		wasmOp = wasmCompareOp(op, instr.Args[0].Type)
		if instr.Args[0].Type == "bool" {
			switch op {
			case "==":
				wasmOp = "i32.eq"
			case "!=":
				wasmOp = "i32.ne"
			}
		}
		if isFloatType(instr.Args[0].Type) {
			wasmOp = wasmFloatCompareOp(op, instr.Args[0].Type)
		}
	}
	expr := "(" + wasmOp + " " + left + " " + right + ")"
	if wrapsResult(op) && !isFloatType(instr.Result.Type) {
		expr = narrowResult(instr.Result.Type, expr)
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbolName(instr.Result.Name), expr)
	e.values[instr.Result.Name] = valueInfo{
		expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writeUnary writes boolean negation and integer arithmetic negation.
func (e *emitter) writeUnary(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: unary expects 1 arg")
	}
	value := e.value(instr.Args[0]).expr
	var expr string
	switch strings.TrimPrefix(instr.Op, "unary.") {
	case "!":
		if instr.Result.Type != "bool" {
			return fmt.Errorf("wasm error: unary ! expects bool")
		}
		expr = "(i32.eqz " + value + ")"
	case "-":
		if isFloatType(instr.Result.Type) {
			expr = "(" + instr.Result.Type + ".neg " + value + ")"
			break
		}
		if !isIntegerType(instr.Result.Type) {
			return fmt.Errorf("wasm error: unary - expects integer")
		}
		expr = narrowResult(instr.Result.Type, "(i64.sub (i64.const 0) "+value+")")
	case "~":
		if !isIntegerType(instr.Result.Type) {
			return fmt.Errorf("wasm error: unary ~ expects integer")
		}
		expr = narrowResult(instr.Result.Type, "(i64.xor "+value+" (i64.const -1))")
	default:
		return fmt.Errorf("wasm error: unsupported unary `%s`", instr.Op)
	}
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeShift writes `<<` and `>>` with the width rule of SPEC §6.9.2. A
// WebAssembly shift reads its amount modulo 64, so the amount is compared
// with the width first and the result chosen by a select.
func (e *emitter) writeShift(instr *ir.Instr, op string, left string, right string) error {
	bits, ok := integerBitWidth(instr.Result.Type)
	if !ok {
		return fmt.Errorf("wasm error: shift expects an integer, got %s", instr.Result.Type)
	}
	inRange := fmt.Sprintf("(i64.lt_u %s (i64.const %d))", right, bits)
	shiftOp := "i64.shl"
	past := "(i64.const 0)"
	if op == ">>" {
		shiftOp = "i64.shr_u"
		if !isUnsignedIntegerType(instr.Result.Type) {
			shiftOp = "i64.shr_s"
			past = "(i64.shr_s " + left + " (i64.const 63))"
		}
	}
	symbol := symbolName(instr.Result.Name)
	expr := fmt.Sprintf("(select (%s %s %s) %s %s)", shiftOp, left, right, past, inRange)
	if op == "<<" {
		expr = narrowResult(instr.Result.Type, expr)
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeCallableInstr writes the instructions that name or reach a function:
// its table index, a call through one, and a call by name.
func (e *emitter) writeCallableInstr(instr *ir.Instr) error {
	if name, ok := strings.CutPrefix(instr.Op, "func.addr."); ok {
		return e.writeFuncAddr(name, instr)
	}
	if instr.Op == "call.indirect" {
		return e.writeIndirectCall(instr)
	}
	return e.writeCall(instr)
}

// writeFuncAddr writes the table index a function name holds. wasm has no
// address for a function, so a pointer is the position the header's `elem`
// gave it.
func (e *emitter) writeFuncAddr(name string, instr *ir.Instr) error {
	index, ok := e.tableIndex[name]
	if !ok {
		return fmt.Errorf("wasm error: `%s` has no table entry", name)
	}
	e.values[instr.Result.Name] = valueInfo{
		expr: fmt.Sprintf("(i32.const %d)", index),
	}
	return nil
}

// writeCallArg adapts one IR argument to its declared wasm call ABI. A
// PassCopyAddress scalar is stored in its planned frame cell and that address
// is handed to the callee; all already-addressed and by-value arguments pass
// through unchanged.
func (e *emitter) writeCallArg(
	instr *ir.Instr,
	arg ir.Value,
	param ir.Param,
	index int,
) (string, error) {
	value := e.value(arg)
	if !e.callNeedsCopySlot(param, arg) {
		return value.expr, nil
	}
	slot, err := e.callCopySlot(instr.Result, index)
	if err != nil {
		return "", err
	}
	if err := e.writeStoreValue(slot, 0, arg.Type, value); err != nil {
		return "", err
	}
	return slot, nil
}

// writeIndirectCall writes a call through a function pointer. The callee is
// the first operand and reaches wasm as the table index it lowered to, which
// `call_indirect` takes last.
func (e *emitter) writeIndirectCall(instr *ir.Instr) error {
	if len(instr.Args) == 0 {
		return fmt.Errorf("wasm error: call.indirect expects a callee")
	}
	args := make([]string, 0, len(instr.Args)+1)
	var resultSlot string
	if e.isMemoryType(instr.Result.Type) {
		var err error
		resultSlot, err = e.resultSlot(instr.Result)
		if err != nil {
			return err
		}
		args = append(args, resultSlot)
	}
	for index, arg := range instr.Args[1:] {
		param := ir.Param{}
		if index < len(instr.CallParams) {
			param = instr.CallParams[index]
		}
		expr, err := e.writeCallArg(instr, arg, param, index)
		if err != nil {
			return err
		}
		args = append(args, expr)
	}
	args = append(args, e.value(instr.Args[0]).expr)
	call := fmt.Sprintf("(call_indirect (type $sig%d) %s)",
		e.internSignature(instr), strings.Join(args, " "))
	if instr.Result.Type == "void" || e.isMemoryType(instr.Result.Type) {
		fmt.Fprintf(&e.out, "            %s\n", call)
		if e.isMemoryType(instr.Result.Type) {
			e.values[instr.Result.Name] = valueInfo{expr: resultSlot}
		}
		return nil
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n",
		symbolName(instr.Result.Name), call)
	e.values[instr.Result.Name] = valueInfo{
		expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writeCall writes builtin print and user function calls.
func (e *emitter) writeCall(instr *ir.Instr) error {
	if instr.ExternABI != "" {
		if e.target.isBrowser() && instr.ExternABI == "browser" {
			return e.writeBrowserHostCall(instr)
		}
		return fmt.Errorf(
			"wasm error: target %s does not support extern `%s`",
			e.target.name(), instr.ExternABI,
		)
	}
	name := strings.TrimPrefix(instr.Op, "call.")
	if handled, err := e.writeAllocatorBuiltinCall(name, instr); handled {
		return err
	}
	if handled, err := e.writeIOBuiltinCall(name, instr); handled {
		return err
	}
	args := make([]string, 0, len(instr.Args)+1)
	params := e.paramsByFunction[name]
	var resultSlot string
	if e.isMemoryType(instr.Result.Type) {
		var err error
		resultSlot, err = e.resultSlot(instr.Result)
		if err != nil {
			return err
		}
		args = append(args, resultSlot)
	}
	for index, arg := range instr.Args {
		param := ir.Param{}
		if index < len(params) {
			param = params[index]
		}
		expr, err := e.writeCallArg(instr, arg, param, index)
		if err != nil {
			return err
		}
		args = append(args, expr)
	}
	call := fmt.Sprintf("(call $%s %s)", name, strings.Join(args, " "))
	if instr.Result.Type == "void" || e.isMemoryType(instr.Result.Type) {
		fmt.Fprintf(&e.out, "            %s\n", call)
		if e.isMemoryType(instr.Result.Type) {
			e.values[instr.Result.Name] = valueInfo{expr: resultSlot}
		}
		return nil
	}
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbolName(instr.Result.Name), call)
	e.values[instr.Result.Name] = valueInfo{
		expr: "(local.get " + symbolName(instr.Result.Name) + ")",
	}
	return nil
}

// writeAllocatorBuiltinCall lowers allocator factories before ordinary calls.
func (e *emitter) writeAllocatorBuiltinCall(name string, instr *ir.Instr) (bool, error) {
	if name == "std::internal::builtin::mem_page_allocator" {
		if len(instr.Args) != 0 || instr.Result.Type != "Allocator" {
			return true, fmt.Errorf("wasm error: mem_page_allocator expects no args -> Allocator")
		}
		symbol := symbolName(instr.Result.Name)
		fmt.Fprintf(&e.out, "            (local.set %s (i32.const 0))\n", symbol)
		e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
		return true, nil
	}
	if name == "std::internal::builtin::mem_fixed_buffer" {
		if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" ||
			instr.Result.Type != "Allocator" {
			return true, fmt.Errorf("wasm error: mem_fixed_buffer expects []u8 -> Allocator")
		}
		symbol := symbolName(instr.Result.Name)
		fmt.Fprintf(&e.out, "            (local.set %s (call $__fixed_buffer %s))\n",
			symbol, e.value(instr.Args[0]).expr)
		e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
		return true, nil
	}
	if name == "std::internal::builtin::mem_allocator_from" {
		return true, e.writeAllocatorFromCall(instr)
	}
	return false, nil
}

// writePrintLine writes one byte string and a newline to stdout: the one print
// primitive std::fmt::print is built on (SPEC §14.1).
func (e *emitter) writePrintLine(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" {
		return fmt.Errorf("wasm error: print.line expects []u8")
	}
	value := e.value(instr.Args[0])
	fmt.Fprintf(&e.out, "            (call $__write_line (i32.load %s) (i32.load %s))\n",
		value.expr, addressAt(value.expr, 4))
	return nil
}

// writeTerminator writes the control transfer at the end of a block.
func (e *emitter) writeTerminator(s *structure, block *ir.Block) error {
	switch block.Terminator.Op {
	case "return":
		return e.writeReturn(block.Terminator.Value)
	case "jump":
		return e.writeBranchTo(s, block.Name, block.Terminator.Target)
	case "branch":
		return e.writeBranch(s, block)
	case "unreachable":
		e.out.WriteString("            (unreachable)\n")
	default:
		return fmt.Errorf("wasm error: unsupported terminator `%s`", block.Terminator.Op)
	}
	return nil
}

// writeReturn writes a function return or exits a void function.
func (e *emitter) writeReturn(value ir.Value) error {
	if value.Type == "void" {
		e.restoreFrame()
		e.out.WriteString("            (br $exit)\n")
		return nil
	}
	if e.isMemoryType(value.Type) {
		layout, err := e.typeLayout(value.Type)
		if err != nil {
			return err
		}
		e.writeMemoryCopy("(local.get $__kizu_result)", e.value(value).expr, layout.size)
		e.restoreFrame()
		e.out.WriteString("            (br $exit)\n")
		return nil
	}
	e.restoreFrame()
	fmt.Fprintf(&e.out, "            (return %s)\n", e.value(value).expr)
	return nil
}

// restoreFrame releases this invocation's fixed frame before any return.
func (e *emitter) restoreFrame() {
	if e.frame != nil && e.frame.size > 0 {
		e.out.WriteString("            (global.set $__stack_pointer (local.get $__kizu_frame))\n")
	}
}

// writeBranchTo writes the edge from `source` to `target`: the phi copies
// the edge carries, then a branch to the loop the target heads when the
// edge goes back, a branch to the target's block when it is a merge node,
// and otherwise the target's own code, since this edge is the only way in.
func (e *emitter) writeBranchTo(s *structure, source string, target string) error {
	e.writePhiCopies(source, target, s)
	if s.loopHeaders[target] && s.rpo[target] <= s.rpo[source] {
		fmt.Fprintf(&e.out, "            (br %s)\n", loopLabel(target))
		return nil
	}
	if s.merges[target] {
		fmt.Fprintf(&e.out, "            (br %s)\n", blockLabel(target))
		return nil
	}
	return e.writeTree(s, target)
}

// writeBranch writes a conditional transfer as an `if` whose arms each
// take their edge.
func (e *emitter) writeBranch(s *structure, block *ir.Block) error {
	term := block.Terminator
	if written, err := e.writeLoopBranch(s, block); written || err != nil {
		return err
	}
	e.out.WriteString("            (if " + e.value(term.Cond).expr + "\n")
	e.out.WriteString("              (then\n")
	if err := e.writeBranchTo(s, block.Name, term.Target); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("              (else\n")
	if err := e.writeBranchTo(s, block.Name, term.Else); err != nil {
		return err
	}
	e.out.WriteString("              )\n")
	e.out.WriteString("            )\n")
	return nil
}

// writeLoopBranch writes a branch with one arm back to a loop header as that
// arm's phi copies followed by a `br_if` back, and then the other arm. An `if`
// puts the copies and the branch back on an edge of their own, which an engine
// compiles into a block that each iteration jumps through; the copies go ahead
// of the test instead when the other arm cannot tell: neither the condition,
// nor the copies the other arm makes, nor any code written for it reads a phi
// the back edge assigns. It reports whether it wrote the branch.
func (e *emitter) writeLoopBranch(s *structure, block *ir.Block) (bool, error) {
	term := block.Terminator
	back, other, negate := term.Target, term.Else, false
	if !e.isBackEdge(s, block.Name, back) {
		back, other, negate = term.Else, term.Target, true
	}
	if back == other || !e.isBackEdge(s, block.Name, back) {
		return false, nil
	}
	if !e.isBackEdge(s, block.Name, other) && !s.merges[other] &&
		readsPhisBelow(s, other, s.blocks[back]) {
		return false, nil
	}
	cond := e.value(term.Cond).expr
	if e.edgeReadsPhis(s, block.Name, other, s.blocks[back], cond) {
		return false, nil
	}
	e.writePhiCopies(block.Name, back, s)
	if negate {
		cond = "(i32.eqz " + cond + ")"
	}
	fmt.Fprintf(&e.out, "            (br_if %s %s)\n", loopLabel(back), cond)
	return true, e.writeBranchTo(s, block.Name, other)
}

// edgeReadsPhis reports whether the condition, or a copy the edge from source
// to target makes, reads a local a phi of header is held in.
func (e *emitter) edgeReadsPhis(
	s *structure,
	source string,
	target string,
	header *ir.Block,
	cond string,
) bool {
	reads := []string{cond}
	for _, instr := range s.blocks[target].Instrs {
		if instr.Op != "phi" {
			continue
		}
		for _, incoming := range instr.Incoming {
			if incoming.Block == source {
				reads = append(reads, e.value(incoming.Value).expr)
			}
		}
	}
	for _, instr := range header.Instrs {
		if instr.Op != "phi" {
			continue
		}
		assigned := "(local.get " + symbolName(instr.Result.Name) + ")"
		for _, read := range reads {
			if strings.Contains(read, assigned) {
				return true
			}
		}
	}
	return false
}

// branchSubtreeLimit is how many blocks readsPhisBelow looks through before it
// assumes a read.
const branchSubtreeLimit = 64

// readsPhisBelow reports whether the code written for the block named top --
// every block it dominates, and the phi copies on edges leaving them -- may
// read a phi of header. A cast is written as an expression over what it
// casts, so reading a cast of a phi reads the phi.
func readsPhisBelow(s *structure, top string, header *ir.Block) bool {
	phis := phisAndCasts(s, header)
	below := map[string]bool{}
	for _, name := range s.order {
		if s.dominates(top, name) {
			below[name] = true
		}
	}
	if len(below) > branchSubtreeLimit {
		return true
	}
	return readsNamesBelow(s, below, phis)
}

// phisAndCasts names the phis of header and every cast of one of them, the
// values whose reads read a phi's local.
func phisAndCasts(s *structure, header *ir.Block) map[string]bool {
	phis := map[string]bool{}
	for _, instr := range header.Instrs {
		if instr.Op == "phi" {
			phis[instr.Result.Name] = true
		}
	}
	for grown := true; grown; {
		grown = false
		for _, block := range s.blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "cast" && len(instr.Args) == 1 && phis[instr.Args[0].Name] &&
					!phis[instr.Result.Name] {
					phis[instr.Result.Name] = true
					grown = true
				}
			}
		}
	}
	return phis
}

// readsNamesBelow reports whether a block in below reads one of names, or an
// edge leaving one carries one into a phi.
func readsNamesBelow(s *structure, below map[string]bool, names map[string]bool) bool {
	phis := names
	for _, block := range s.blocks {
		for _, instr := range block.Instrs {
			for _, incoming := range instr.Incoming {
				if below[incoming.Block] && phis[incoming.Value.Name] {
					return true
				}
			}
			if !below[block.Name] || instr.Op == "phi" {
				continue
			}
			found := false
			visitInstrReads(instr, func(value ir.Value) { found = found || phis[value.Name] })
			if found {
				return true
			}
		}
		if below[block.Name] && (phis[block.Terminator.Value.Name] || phis[block.Terminator.Cond.Name]) {
			return true
		}
	}
	return false
}

// visitInstrReads calls visit with each value an instruction reads besides its
// phi incoming.
func visitInstrReads(instr *ir.Instr, visit func(ir.Value)) {
	for _, arg := range instr.Args {
		visit(arg)
	}
	for _, field := range instr.Fields {
		visit(field.Value)
	}
	for _, cleanup := range instr.Cleanups {
		for _, arg := range cleanup.Args {
			visit(arg)
		}
	}
}

// isBackEdge reports whether the edge from source to target goes back to the
// header of a loop.
func (e *emitter) isBackEdge(s *structure, source string, target string) bool {
	return s.loopHeaders[target] && s.rpo[target] <= s.rpo[source]
}

// writePhiCopies assigns target phi locals for an edge. The edge stays inside
// one function, so the target is read out of that function's structure.
//
// The phis of one block take their values at once, so a copy that reads
// another phi of the block has to read it before that phi is assigned. Copies
// are written once nothing still waiting reads what they assign; a cycle of
// them, a pair of phis trading values, parks one phi's value in the temporary
// phiTempLocal declares for its type first.
func (e *emitter) writePhiCopies(source string, target string, s *structure) {
	block := s.blocks[target]
	if block == nil {
		return
	}
	pending := []phiCopy{}
	for _, instr := range block.Instrs {
		if instr.Op != "phi" {
			continue
		}
		for _, incoming := range instr.Incoming {
			if incoming.Block == source {
				pending = append(pending, phiCopy{dst: instr.Result, src: e.value(incoming.Value).expr})
			}
		}
	}
	for len(pending) > 0 {
		ready := -1
		for index := range pending {
			if !phiCopyRead(pending, index) {
				ready = index
				break
			}
		}
		if ready < 0 {
			parked := pending[0].dst
			temp := phiTempLocal(e.wasmType(parked.Type))
			read := "(local.get " + symbolName(parked.Name) + ")"
			fmt.Fprintf(&e.out, "            (local.set %s %s)\n", temp, read)
			for index := range pending {
				if pending[index].src == read {
					pending[index].src = "(local.get " + temp + ")"
				}
			}
			continue
		}
		next := pending[ready]
		fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbolName(next.dst.Name), next.src)
		e.values[next.dst.Name] = valueInfo{expr: "(local.get " + symbolName(next.dst.Name) + ")"}
		pending = append(pending[:ready], pending[ready+1:]...)
	}
}

// phiCopy is one assignment an edge makes to a phi of the block it enters.
type phiCopy struct {
	dst ir.Value
	src string
}

// phiCopyRead reports whether a copy other than the one at index still reads
// the phi that copy assigns.
func phiCopyRead(pending []phiCopy, index int) bool {
	read := "(local.get " + symbolName(pending[index].dst.Name) + ")"
	for other, candidate := range pending {
		if other != index && strings.Contains(candidate.src, read) {
			return true
		}
	}
	return false
}

// phiTempLocal names the local a cycle of phi copies parks one value of a wasm
// type in.
func phiTempLocal(wasmType string) string {
	return "$__kizu_phi_" + wasmType
}

// phiTempTypes lists, in first-use order, the wasm types of the phis of fn
// that read another phi of their own block, the ones a cycle of copies can
// hold, so each type's temporary is declared once.
func (e *emitter) phiTempTypes(fn *ir.Function) []string {
	types := []string{}
	for _, block := range fn.Blocks {
		phis := map[string]bool{}
		for _, instr := range block.Instrs {
			if instr.Op == "phi" {
				phis[instr.Result.Name] = true
			}
		}
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				continue
			}
			for _, incoming := range instr.Incoming {
				if !phis[incoming.Value.Name] || incoming.Value.Name == instr.Result.Name {
					continue
				}
				wasmType := e.wasmType(incoming.Value.Type)
				seen := false
				for _, existing := range types {
					seen = seen || existing == wasmType
				}
				if !seen {
					types = append(types, wasmType)
				}
			}
		}
	}
	return types
}

// value resolves a typed IR value to a WebAssembly expression.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	if _, err := strconv.Atoi(value.Name); err == nil {
		return valueInfo{expr: "(i64.const " + value.Name + ")"}
	}
	return valueInfo{expr: "(local.get " + symbolName(value.Name) + ")"}
}

// wasmBool maps bool constants to WebAssembly i32 constants.
func wasmBool(value string) string {
	if value == "true" {
		return "(i32.const 1)"
	}
	return "(i32.const 0)"
}
