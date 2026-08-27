package llvm

import (
	"fmt"
	"strconv"
	"strings"
)

// A niche is a bit pattern a type never writes for a live value, and `?T`
// spends no tag when T has one: absence is that pattern and presence is
// everything else. Three types have one. A Box handle is the address of its
// payload and a borrow is the address of what it borrows, so neither is null
// while it exists; an arena handle is an index biased by one, so it is never
// zero either (ADR-0133).
//
// The pattern is always zero, which is what lets a niche sit at a depth rather
// than only at the surface: `zeroinitializer` spells absence for a struct that
// reaches its niche through a field the same way it does for a bare pointer.
// So a niche is only ever a path to the field that carries it.
//
// A struct inherits the niche of its first field that has one. An optional is
// not a source -- a niche optional already spends its zero on being absent,
// and a tagged one would hand out a pattern its tag byte owns.
//
// The walk needs no cycle guard. A struct that reaches itself does so through
// a Box or a container header: the Box answers before recursing and a header
// is not a struct, so a chain of plain fields is as finite as the size the
// type checker already proved it has.

// hasNiche reports whether a value of typ leaves one bit pattern unwritten.
func (e *emitter) hasNiche(typ string) bool {
	if isBoxLLVMType(typ) || strings.HasPrefix(typ, "&") || isArenaHandleType(typ) {
		return true
	}
	if _, ok := optionalElemLLVM(typ); ok {
		return false
	}
	st, ok := e.module.Structs[typ]
	if !ok {
		return false
	}
	for _, field := range st.Fields {
		if e.hasNiche(field.Type) {
			return true
		}
	}
	return false
}

// nichePath returns the field indices from a value of typ to the word its
// niche sits in, and that word's LLVM type. An empty path means the value is
// the word. It is what writeNichePresence reads; every other caller asks
// hasNiche, which walks the same shape without building the path.
func (e *emitter) nichePath(typ string) ([]int, string, bool) {
	if isBoxLLVMType(typ) || strings.HasPrefix(typ, "&") {
		return nil, "ptr", true
	}
	if isArenaHandleType(typ) {
		return nil, "i64", true
	}
	if _, ok := optionalElemLLVM(typ); ok {
		return nil, "", false
	}
	st, ok := e.module.Structs[typ]
	if !ok {
		return nil, "", false
	}
	for index, field := range st.Fields {
		path, fieldType, ok := e.nichePath(field.Type)
		if !ok {
			continue
		}
		return append([]int{index}, path...), fieldType, true
	}
	return nil, "", false
}

// nicheOptionalElem returns the element of an optional that spends no tag, and
// true when there is one.
func (e *emitter) nicheOptionalElem(name string) (string, bool) {
	elem, ok := optionalElemLLVM(name)
	if !ok || !e.hasNiche(elem) {
		return "", false
	}
	return elem, true
}

// nicheAbsent is the operand an absent optional is: the zero of its element,
// whatever shape that element has. Only a pointer-shaped element spells that
// zero `null`; every other one, niche or not, spells it `zeroinitializer`.
func nicheAbsent(elem string) string {
	if nicheAbsentIsNull(elem) {
		return "null"
	}
	return "zeroinitializer"
}

// nicheAbsentIsNull reports whether an element's zero is spelled `null`.
func nicheAbsentIsNull(elem string) bool {
	return isBoxLLVMType(elem) || strings.HasPrefix(elem, "&")
}

// writeNichePresence tests whether a niche optional holds a value by reading
// the word its niche sits in and comparing it against the zero only an absent
// value writes. The read is one extractvalue however deep the word is, so a
// niche a struct inherited costs what a bare pointer's costs.
func (e *emitter) writeNichePresence(elem string, operand string, into string) error {
	path, fieldType, ok := e.nichePath(elem)
	if !ok {
		return fmt.Errorf("llvm error: `%s` has no niche", elem)
	}
	field := operand
	if len(path) > 0 {
		indices := make([]string, 0, len(path))
		for _, index := range path {
			indices = append(indices, strconv.Itoa(index))
		}
		field = "%" + e.nextSyntheticValue("opt.niche")
		fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %s\n",
			field, e.llvmType(elem), operand, strings.Join(indices, ", "))
	}
	zero := "0"
	if fieldType == "ptr" {
		zero = "null"
	}
	fmt.Fprintf(&e.out, "  %s = icmp ne %s %s, %s\n", into, fieldType, field, zero)
	return nil
}
