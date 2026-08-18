package types

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/unsafecap"
)

type unsafeCapability string

const (
	unsafePtrRead         unsafeCapability = "ptr_read"
	unsafePtrWrite        unsafeCapability = "ptr_write"
	unsafePtrDeref        unsafeCapability = "ptr_deref"
	unsafePtrCast         unsafeCapability = "ptr_cast"
	unsafePtrIntCast      unsafeCapability = "ptr_int_cast"
	unsafeExternCall      unsafeCapability = "extern_call"
	unsafeUnsafeCall      unsafeCapability = "unsafe_call"
	unsafeStructInvariant unsafeCapability = "struct_invariant"
	unsafeVolatile        unsafeCapability = "volatile"
)

// unsafeScope is one `unsafe` marker. A use is recorded on the innermost marker
// in scope, which is what lets a marker that covers nothing be reported.
type unsafeScope struct {
	used bool
}

// unsafeMark is the marker state threaded through checking. A nil mark means
// the expression is covered by no `unsafe`, so a function body starts unmarked
// whether or not the function itself is declared `unsafe fn`.
type unsafeMark = *unsafeScope

// requireUnsafeCapabilityAt rejects an operation the compiler cannot prove when
// no `unsafe` marker covers it. Source no longer spells capability names, but
// the diagnostic still names the kind of operation so the reader learns what
// obligation the marker would take on.
func requireUnsafeCapabilityAt(
	mark unsafeMark,
	cap unsafeCapability,
	operation string,
	span ast.Span,
) error {
	if mark.use() {
		return nil
	}
	message := fmt.Sprintf("unsafe error: %s requires `unsafe`", operation)
	if info, ok := unsafecap.Lookup(string(cap)); ok {
		message += "\nhelp: " + unsafecap.Hint(info)
	}
	if !span.IsZero() {
		return errorAtCode(span, "unsafe.missing_marker", "%s", message)
	}
	return errorf("%s", message)
}

// requireUnsafeStructInvariant rejects an operation that establishes or changes
// the invariant of an `unsafe struct` when no `unsafe` marker covers it.
// Construction and field writes are the same rule: both put the struct into a
// state only the author can vouch for.
func requireUnsafeStructInvariant(
	mark unsafeMark,
	decl *ast.StructDecl,
	action string,
	fieldName string,
	span ast.Span,
) error {
	if decl == nil || !decl.RequiresUnsafe {
		return nil
	}
	target := "`unsafe struct " + decl.Name + "`"
	if fieldName != "" {
		target = "`" + decl.Name + "." + fieldName + "`"
	}
	return requireUnsafeCapabilityAt(mark, unsafeStructInvariant, action+" "+target, span)
}

// requireObligationDoc requires a `///` comment where an obligation is created.
// What the obligation says cannot be written in code, so a comment is the only
// place it can live; the compiler cannot judge what is written there, but it
// can tell that nothing was.
func requireObligationDoc(requiresUnsafe bool, doc string, subject string, want string) error {
	if !requiresUnsafe || doc != "" {
		return nil
	}
	return errorf("unsafe error: `%s` needs a `///` comment stating %s"+
		"\nhelp: write `/// <%s>` above the declaration", subject, want, want)
}
