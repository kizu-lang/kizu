package llvm

import "github.com/kizu-lang/kizu/internal/ir"

// maxInlinePayloadAlign bounds the alignment the #991 inline tagged-union
// payload storage can guarantee. A union lowers to `{ i64, [N x i8] }`; the
// i64 tag forces 8-byte struct alignment, so the inline storage starts at an
// 8-aligned offset and can hold any payload whose alignment is at most 8.
const maxInlinePayloadAlign = 8

// typeLayout returns the byte size and alignment of an IR value type when the
// #991 tagged-union payload ABI can store it inline. ok is false for shapes the
// ABI does not support inline: unbounded/recursive by-value aggregates, unknown
// types, error unions, or integer widths outside the value layout table.
func (e *emitter) typeLayout(typ string) (size int, align int, ok bool) {
	return e.typeLayoutVisiting(typ, nil)
}

// typeLayoutVisiting computes a type layout while tracking the named aggregates
// already on the recursion path so a by-value cycle is rejected, not looped. It
// only accepts the shapes the #991 value layout table defines inline; every
// other type (error unions, raw pointers, non-i64/u8 integer widths, and
// unknown type names) reports ok=false so it fails visibly rather than being
// silently treated as a pointer.
func (e *emitter) typeLayoutVisiting(typ string, seen []string) (int, int, bool) {
	if size, align, ok := primitiveLayout(typ); ok {
		return size, align, ok
	}
	if e.lowersToWord(typ) {
		return 8, 8, true
	}
	if st, ok := e.module.Structs[typ]; ok {
		return e.structLayout(typ, st, seen)
	}
	if union, ok := e.module.Unions[typ]; ok {
		payload, _, ok := e.unionPayloadStorage(typ, union, seen)
		if !ok {
			return 0, 0, false
		}
		return roundUp(8+payload, maxInlinePayloadAlign), maxInlinePayloadAlign, true
	}
	return 0, 0, false
}

// primitiveLayout returns the inline layout of a built-in scalar or view type.
func primitiveLayout(typ string) (int, int, bool) {
	switch typ {
	case "void":
		return 0, 1, true
	case "bool", "i8", "u8":
		return 1, 1, true
	case "i64":
		return 8, 8, true
	case "[]u8":
		return 16, 8, true
	default:
		return 0, 0, false
	}
}

// lowersToWord reports whether a type is stored inline as one 8-byte word: an
// owned container or arena handle lowering to a pointer, or a tag lowering to
// an i64. An enum tag and an error code are the same shape here.
func (e *emitter) lowersToWord(typ string) bool {
	if typ == "std::string::String" || isArenaHandleType(typ) {
		return true
	}
	if isArrayLLVMType(typ) || isMapLLVMType(typ) || isArenaLLVMType(typ) {
		return true
	}
	if _, ok := e.module.Enums[typ]; ok {
		return true
	}
	_, ok := e.module.ErrorSets[typ]
	return ok
}

// structLayout computes the C-style layout LLVM uses for a non-packed struct.
func (e *emitter) structLayout(name string, st ir.Struct, seen []string) (int, int, bool) {
	if containsName(seen, name) {
		return 0, 0, false
	}
	seen = append(seen, name)
	size, align := 0, 1
	for _, field := range st.Fields {
		fieldSize, fieldAlign, ok := e.typeLayoutVisiting(field.Type, seen)
		if !ok {
			return 0, 0, false
		}
		size = roundUp(size, fieldAlign) + fieldSize
		if fieldAlign > align {
			align = fieldAlign
		}
	}
	return roundUp(size, align), align, true
}

// unionPayloadStorage returns the inline byte capacity N and payload alignment
// required to hold the largest variant payload of a union, or ok=false when any
// variant payload is an unsupported inline shape.
func (e *emitter) unionPayloadStorage(
	name string,
	union ir.Union,
	seen []string,
) (capacity int, align int, ok bool) {
	if containsName(seen, name) {
		return 0, 0, false
	}
	seen = append(seen, name)
	align = 1
	for _, variant := range union.Variants {
		if variant.Payload == "" {
			continue
		}
		size, payloadAlign, ok := e.typeLayoutVisiting(variant.Payload, seen)
		if !ok {
			return 0, 0, false
		}
		if size > capacity {
			capacity = size
		}
		if payloadAlign > align {
			align = payloadAlign
		}
	}
	return capacity, align, true
}

// roundUp rounds n up to the next multiple of align.
func roundUp(n int, align int) int {
	if align <= 1 {
		return n
	}
	return ((n + align - 1) / align) * align
}

// containsName reports whether a named aggregate is already on the layout path.
func containsName(seen []string, name string) bool {
	for _, candidate := range seen {
		if candidate == name {
			return true
		}
	}
	return false
}
