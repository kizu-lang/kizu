package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// A vector `f64x2` and its kin are the 128-bit `v128` of the simd proposal,
// which every engine the compiler targets has on by default. A vector is a
// value type like a float: it lives in a local, and it is stored and loaded
// whole with `v128.store` / `v128.load`. Lane operations are spelled with the
// shape prefix wasm uses, `f64x2.add`, which is the Kizu type name itself.

// vectorShape returns the wasm shape prefix of a vector type: the type name.
func vectorShape(typ string) string {
	return typ
}

// vectorLaneWasmType returns the wasm value type one lane travels as outside
// the vector: a float lane is itself, an integer lane is the i32 or i64 the
// lane instructions take, before it is widened to the i64 every Kizu integer
// lives in.
func vectorLaneWasmType(elem string) string {
	switch elem {
	case "f32", "f64":
		return elem
	case "i64", "u64":
		return "i64"
	default:
		return "i32"
	}
}

// vectorLaneIn narrows one Kizu integer lane value held in an i64 local to
// what the replace_lane instruction takes.
func vectorLaneIn(elem string, expr string) string {
	if vectorLaneWasmType(elem) == "i32" {
		return "(i32.wrap_i64 " + expr + ")"
	}
	return expr
}

// vectorLaneOut widens one lane read back to the i64 an integer lives in,
// with the sign the lane type has.
func vectorLaneOut(elem string, expr string) string {
	if vectorLaneWasmType(elem) != "i32" {
		return expr
	}
	if strings.HasPrefix(elem, "u") {
		return "(i64.extend_i32_u " + expr + ")"
	}
	return "(i64.extend_i32_s " + expr + ")"
}

// vectorExtractLane spells the lane read of one shape: the 16-bit shapes
// carry the sign in the instruction name, the wider ones do not.
func vectorExtractLane(typ string, elem string) string {
	switch elem {
	case "i16":
		return typ + ".extract_lane_s"
	case "u16":
		return typ + ".extract_lane_u"
	default:
		return typ + ".extract_lane"
	}
}

// writeVectorInstr writes one vector instruction.
func (e *emitter) writeVectorInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "vector.new":
		return e.writeVectorNew(instr)
	case "vector.lane":
		return e.writeVectorLane(instr)
	default:
		return fmt.Errorf("wasm error: unsupported vector instruction `%s`", instr.Op)
	}
}

// writeVectorNew builds a vector from one value per lane: a splat of the
// first lane, then a replace of each later one.
func (e *emitter) writeVectorNew(instr *ir.Instr) error {
	elem, lanes, ok := typ.VectorOf(instr.Result.Type)
	if !ok || len(instr.Args) != lanes {
		return fmt.Errorf("wasm error: vector.new expects %s lanes, got %d",
			instr.Result.Type, len(instr.Args))
	}
	shape := vectorShape(instr.Result.Type)
	expr := "(" + shape + ".splat " + vectorLaneIn(elem, e.value(instr.Args[0]).expr) + ")"
	for i := 1; i < lanes; i++ {
		expr = fmt.Sprintf("(%s.replace_lane %d %s %s)",
			shape, i, expr, vectorLaneIn(elem, e.value(instr.Args[i]).expr))
	}
	return e.writeScalarResult(instr.Result, expr)
}

// writeVectorLane reads one lane; the lane index is the instruction's
// immediate.
func (e *emitter) writeVectorLane(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("wasm error: vector.lane expects one vector")
	}
	elem, _, ok := typ.VectorOf(instr.Args[0].Type)
	if !ok || instr.Result.Type != elem {
		return fmt.Errorf("wasm error: vector.lane expects a vector -> lane, got %s -> %s",
			instr.Args[0].Type, instr.Result.Type)
	}
	read := fmt.Sprintf("(%s %s %s)", vectorExtractLane(instr.Args[0].Type, elem),
		instr.Immediate, e.value(instr.Args[0]).expr)
	return e.writeScalarResult(instr.Result, vectorLaneOut(elem, read))
}

// wasmVectorBinaryOp maps a Kizu arithmetic operator on a vector type to the
// lane-wise wasm operation.
func wasmVectorBinaryOp(op string, typ string) string {
	switch op {
	case "-":
		return typ + ".sub"
	case "*":
		return typ + ".mul"
	case "/":
		return typ + ".div"
	default:
		return typ + ".add"
	}
}
