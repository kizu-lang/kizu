package llvm

import (
	"fmt"
	"strings"
)

// Type-based alias tags let the optimizer keep an Array's length and storage
// pointer in registers across a loop that writes elements. Without them, a
// store of an i64 element could, as far as LLVM can tell, have written the
// i64 length of the header the loop reads, so every iteration reads the
// header again.
//
// Three types hang off one root: the header's counts, the header's storage
// pointer, and every other scalar a load or store names. The first two are
// only ever read or written as themselves -- a header field is reached through
// the Array operations that name it and through whole-header copies, which
// carry no tag and so may overlap anything -- and no reference a program can
// hold points into a header, so an element, a field, or a value behind a
// borrow is never a header field. What carries no tag, aggregates and raw
// pointers among it, is still taken to overlap everything.
const (
	tbaaCount = iota
	tbaaPointer
	tbaaValue
)

// tbaaFirstAccessTag is the metadata number of the access tag of tbaaCount;
// the table writeTBAATable writes puts the tags of the other kinds after it.
const tbaaFirstAccessTag = 4

// tbaaTag returns the suffix that tags a load or store with one kind.
func (e *emitter) tbaaTag(kind int) string {
	e.tbaaUsed = true
	return fmt.Sprintf(", !tbaa !%d", tbaaFirstAccessTag+kind)
}

// valueTag tags a load or store of llvmType as a value, when the type is a
// scalar; an aggregate stays untagged.
func (e *emitter) valueTag(llvmType string) string {
	if strings.HasPrefix(llvmType, "%") || strings.HasPrefix(llvmType, "{") ||
		strings.HasPrefix(llvmType, "[") || llvmType == "void" {
		return ""
	}
	return e.tbaaTag(tbaaValue)
}

// referenceTag tags a load or store through a borrow as a value. A raw
// pointer can be made to point anywhere, a header among it, and a volatile
// access names a device register, so neither is tagged.
func (e *emitter) referenceTag(op string, receiverType string, llvmType string) string {
	if volatileKeyword(op) != "" || !strings.HasPrefix(receiverType, "&") {
		return ""
	}
	return e.valueTag(llvmType)
}

// writeTBAATable writes the type table the tags name, when a tag was written.
func (e *emitter) writeTBAATable() {
	if !e.tbaaUsed {
		return
	}
	e.out.WriteString("!0 = !{!\"kizu tbaa\"}\n")
	e.out.WriteString("!1 = !{!\"kizu array count\", !0, i64 0}\n")
	e.out.WriteString("!2 = !{!\"kizu array data\", !0, i64 0}\n")
	e.out.WriteString("!3 = !{!\"kizu value\", !0, i64 0}\n")
	e.out.WriteString("!4 = !{!1, !1, i64 0}\n")
	e.out.WriteString("!5 = !{!2, !2, i64 0}\n")
	e.out.WriteString("!6 = !{!3, !3, i64 0}\n")
}
