package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// wasmLayout is the size and alignment of one Kizu value in wasm32 linear
// memory. WebAssembly locals still hold scalars directly; aggregates use an
// i32 address whose pointee follows this layout.
type wasmLayout struct {
	size  int
	align int
}

// typeLayout returns the deterministic wasm32 layout of typ.
func (e *emitter) typeLayout(typ string) (wasmLayout, error) {
	return e.typeLayoutVisiting(typ, nil)
}

// typeLayoutVisiting rejects recursive by-value aggregates instead of
// recursing forever. A recursive shape must cross an explicit pointer.
func (e *emitter) typeLayoutVisiting(typ string, seen map[string]bool) (wasmLayout, error) {
	if layout, ok := e.directLayout(typ); ok {
		return layout, nil
	}
	if layout, ok, err := e.taggedTypeLayoutVisiting(typ, seen); ok || err != nil {
		return layout, err
	}
	if _, ok := e.module.Enums[typ]; ok {
		return wasmLayout{size: 8, align: 8}, nil
	}
	if _, ok := e.module.ErrorSets[typ]; ok {
		return wasmLayout{size: 8, align: 8}, nil
	}
	if st, ok := e.module.Structs[typ]; ok {
		if seen[typ] {
			return wasmLayout{}, fmt.Errorf(
				"wasm error: recursive by-value struct `%s` has no finite layout", typ)
		}
		next := copySeen(seen)
		next[typ] = true
		size, align := 0, 1
		for _, field := range st.Fields {
			fieldLayout, err := e.typeLayoutVisiting(field.Type, next)
			if err != nil {
				return wasmLayout{}, err
			}
			size = alignUp(size, fieldLayout.align) + fieldLayout.size
			align = maxInt(align, fieldLayout.align)
		}
		return wasmLayout{size: alignUp(size, align), align: align}, nil
	}
	if union, ok := e.module.Unions[typ]; ok {
		if seen[typ] {
			return wasmLayout{}, fmt.Errorf(
				"wasm error: recursive by-value union `%s` has no finite layout", typ)
		}
		next := copySeen(seen)
		next[typ] = true
		payloadSize, payloadAlign := 0, 1
		for _, variant := range union.Variants {
			if variant.Payload == "" {
				continue
			}
			layout, err := e.typeLayoutVisiting(variant.Payload, next)
			if err != nil {
				return wasmLayout{}, err
			}
			payloadSize = maxInt(payloadSize, layout.size)
			payloadAlign = maxInt(payloadAlign, layout.align)
		}
		align := maxInt(8, payloadAlign)
		payloadOffset := alignUp(8, payloadAlign)
		return wasmLayout{size: alignUp(payloadOffset+payloadSize, align), align: align}, nil
	}
	return wasmLayout{}, fmt.Errorf("wasm error: type `%s` has no wasm32 value layout", typ)
}

// directLayout returns layouts that do not recurse through a declaration.
func (e *emitter) directLayout(typ string) (wasmLayout, bool) {
	if layout, ok := primitiveLayout(typ); ok {
		return layout, true
	}
	if size, ok := e.bufferSize(typ); ok {
		return wasmLayout{size: size, align: 1}, true
	}
	if isArrayWasmType(typ) {
		return wasmLayout{size: arrayHeaderSize, align: 8}, true
	}
	if isArenaWasmType(typ) {
		return wasmLayout{size: arenaHeaderSize, align: 8}, true
	}
	if isArenaHandleWasmType(typ) {
		return wasmLayout{size: 8, align: 8}, true
	}
	if isBoxWasmType(typ) {
		return wasmLayout{size: 4, align: 4}, true
	}
	return wasmLayout{}, false
}

// primitiveLayout returns scalar, pointer, and byte-view layouts.
func primitiveLayout(typ string) (wasmLayout, bool) {
	switch typ {
	case "void":
		return wasmLayout{size: 0, align: 1}, true
	case "bool", "i8", "u8":
		return wasmLayout{size: 1, align: 1}, true
	case "i16", "u16":
		return wasmLayout{size: 2, align: 2}, true
	case "i32", "u32", "usize", "isize":
		return wasmLayout{size: 4, align: 4}, true
	case "i64", "u64":
		return wasmLayout{size: 8, align: 8}, true
	case "[]u8":
		return wasmLayout{size: 8, align: 4}, true
	case "Allocator", "Io":
		return wasmLayout{size: 4, align: 4}, true
	}
	if isReferenceType(typ) || isRawPointerType(typ) || isFunctionPointerType(typ) {
		return wasmLayout{size: 4, align: 4}, true
	}
	return wasmLayout{}, false
}

// isMemoryType reports whether a wasm local represents typ by an i32 address.
func (e *emitter) isMemoryType(typ string) bool {
	if typ == "[]u8" {
		return true
	}
	if _, ok := e.bufferSize(typ); ok {
		return true
	}
	if isArrayWasmType(typ) {
		return true
	}
	if isArenaWasmType(typ) {
		return true
	}
	if _, ok := optionalElemWasm(typ); ok {
		return true
	}
	if _, _, ok := e.errorUnionParts(typ); ok {
		return true
	}
	if _, ok := e.module.Structs[typ]; ok {
		return true
	}
	_, ok := e.module.Unions[typ]
	return ok
}

// isTagType reports whether a tag is represented by an i64 scalar.
func (e *emitter) isTagType(typ string) bool {
	if _, ok := e.module.Enums[typ]; ok {
		return true
	}
	_, ok := e.module.ErrorSets[typ]
	return ok
}

// isNamedI64Type reports whether a declared scalar uses one i64 value.
func (e *emitter) isNamedI64Type(typ string) bool {
	return e.isTagType(typ) || isArenaHandleWasmType(typ)
}

// fieldLayout returns a declared field and its byte offset.
func (e *emitter) fieldLayout(structName string, fieldName string) (ir.Field, int, error) {
	st, ok := e.module.Structs[structName]
	if !ok {
		return ir.Field{}, 0, fmt.Errorf("wasm error: unknown struct type `%s`", structName)
	}
	offset := 0
	for _, field := range st.Fields {
		layout, err := e.typeLayout(field.Type)
		if err != nil {
			return ir.Field{}, 0, err
		}
		offset = alignUp(offset, layout.align)
		if field.Name == fieldName {
			return field, offset, nil
		}
		offset += layout.size
	}
	return ir.Field{}, 0, fmt.Errorf("wasm error: unknown struct field `%s.%s`", structName, fieldName)
}

// unionPayloadOffset returns the common start of a union's inline payload.
func (e *emitter) unionPayloadOffset(unionName string) (int, error) {
	union, ok := e.module.Unions[unionName]
	if !ok {
		return 0, fmt.Errorf("wasm error: unknown union type `%s`", unionName)
	}
	align := 1
	for _, variant := range union.Variants {
		if variant.Payload == "" {
			continue
		}
		layout, err := e.typeLayout(variant.Payload)
		if err != nil {
			return 0, err
		}
		align = maxInt(align, layout.align)
	}
	return alignUp(8, align), nil
}

// derefWasmType removes one reference or raw-pointer wrapper.
func derefWasmType(typ string) string {
	if strings.HasPrefix(typ, "&var ") {
		return strings.TrimPrefix(typ, "&var ")
	}
	if strings.HasPrefix(typ, "&") {
		return strings.TrimPrefix(typ, "&")
	}
	if isRawPointerType(typ) {
		elem := strings.TrimSuffix(strings.TrimPrefix(typ, "ptr<"), ">")
		return strings.TrimPrefix(elem, "const ")
	}
	return typ
}

// isReferenceType reports whether typ is a checked reference.
func isReferenceType(typ string) bool {
	return strings.HasPrefix(typ, "&")
}

// isRawPointerType reports whether typ is a raw pointer.
func isRawPointerType(typ string) bool {
	return strings.HasPrefix(typ, "ptr<") && strings.HasSuffix(typ, ">")
}

// isFunctionPointerType reports whether typ is a function signature value.
func isFunctionPointerType(typ string) bool {
	return strings.HasPrefix(typ, "fn(") || strings.HasPrefix(typ, "unsafe fn(")
}

// copySeen clones one aggregate-layout recursion path.
func copySeen(seen map[string]bool) map[string]bool {
	out := map[string]bool{}
	for name := range seen {
		out[name] = true
	}
	return out
}

// alignUp rounds value to the next multiple of align.
func alignUp(value int, align int) int {
	if align <= 1 {
		return value
	}
	return ((value + align - 1) / align) * align
}

// maxInt returns the larger integer.
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
