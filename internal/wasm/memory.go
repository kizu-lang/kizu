package wasm

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

// addressAt adds a constant byte offset to a linear-memory address expression.
func addressAt(base string, offset int) string {
	if offset == 0 {
		return base
	}
	return fmt.Sprintf("(i32.add %s (i32.const %d))", base, offset)
}

// writeStoreValue stores value at base+offset according to typ's wasm32
// layout. Aggregates are copied byte-for-byte from their addressed value.
func (e *emitter) writeStoreValue(base string, offset int, typ string, value valueInfo) error {
	address := addressAt(base, offset)
	if e.isMemoryType(typ) {
		layout, err := e.typeLayout(typ)
		if err != nil {
			return err
		}
		e.writeMemoryCopy(address, value.expr, layout.size)
		return nil
	}
	op, err := e.storeOp(typ)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "            (%s %s %s)\n", op, address, value.expr)
	return nil
}

// loadExpr returns a scalar load from base+offset. Aggregate callers use
// writeLoadValue so the addressed bytes are copied into result storage.
func (e *emitter) loadExpr(base string, offset int, typ string) (string, error) {
	if e.isMemoryType(typ) {
		return "", fmt.Errorf("wasm error: aggregate `%s` requires addressed load storage", typ)
	}
	op, err := e.loadOp(typ)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(%s %s)", op, addressAt(base, offset)), nil
}

// writeLoadValue loads a scalar into its SSA local or copies an aggregate into
// the result's fixed frame slot.
func (e *emitter) writeLoadValue(result ir.Value, base string, offset int) error {
	if e.isMemoryType(result.Type) {
		slot, err := e.resultSlot(result)
		if err != nil {
			return err
		}
		layout, err := e.typeLayout(result.Type)
		if err != nil {
			return err
		}
		e.writeMemoryCopy(slot, addressAt(base, offset), layout.size)
		e.values[result.Name] = valueInfo{expr: slot}
		return nil
	}
	expr, err := e.loadExpr(base, offset, result.Type)
	if err != nil {
		return err
	}
	symbol := symbolName(result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, expr)
	e.values[result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// writeMemoryCopy copies size bytes between addressed aggregate values.
func (e *emitter) writeMemoryCopy(dst string, src string, size int) {
	if size == 0 {
		return
	}
	fmt.Fprintf(&e.out, "            (memory.copy %s %s (i32.const %d))\n", dst, src, size)
}

// storeOp returns the width-correct WebAssembly store for typ.
func (e *emitter) storeOp(typ string) (string, error) {
	if e.isNamedI64Type(typ) {
		return "i64.store", nil
	}
	switch typ {
	case "bool":
		return "i32.store8", nil
	case "i8", "u8":
		return "i64.store8", nil
	case "i16", "u16":
		return "i64.store16", nil
	case "i32", "u32", "usize", "isize":
		return "i64.store32", nil
	case "i64", "u64":
		return "i64.store", nil
	case "Allocator", "Io":
		return "i32.store", nil
	}
	if e.isAddressValueType(typ) {
		return "i32.store", nil
	}
	return "", fmt.Errorf("wasm error: type `%s` cannot be stored in linear memory", typ)
}

// loadOp returns the width- and sign-correct WebAssembly load for typ.
func (e *emitter) loadOp(typ string) (string, error) {
	if e.isNamedI64Type(typ) {
		return "i64.load", nil
	}
	switch typ {
	case "bool":
		return "i32.load8_u", nil
	case "i8":
		return "i64.load8_s", nil
	case "u8":
		return "i64.load8_u", nil
	case "i16":
		return "i64.load16_s", nil
	case "u16":
		return "i64.load16_u", nil
	case "i32", "isize":
		return "i64.load32_s", nil
	case "u32", "usize":
		return "i64.load32_u", nil
	case "i64", "u64":
		return "i64.load", nil
	case "Allocator", "Io":
		return "i32.load", nil
	}
	if e.isAddressValueType(typ) {
		return "i32.load", nil
	}
	return "", fmt.Errorf("wasm error: type `%s` cannot be loaded from linear memory", typ)
}

// isAddressValueType reports values represented by one linear-memory address.
// A stack-buffer value is its storage address even though its storage layout
// is the full fixed byte count.
func (e *emitter) isAddressValueType(typ string) bool {
	if _, ok := e.bufferSize(typ); ok {
		return true
	}
	return isReferenceType(typ) || isRawPointerType(typ) ||
		isFunctionPointerType(typ) || isBoxWasmType(typ)
}
