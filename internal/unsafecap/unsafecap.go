// Package unsafecap defines reserved @unsafe capabilities and user-facing help.
package unsafecap

import "strings"

// Info describes one reserved @unsafe capability.
type Info struct {
	Name        string
	Detail      string
	Permits     []string
	ShortPermit string
}

var capabilities = []Info{
	{
		Name:        "extern_call",
		Detail:      "extern function call",
		Permits:     []string{"Calling `extern \"c\" fn` declarations."},
		ShortPermit: "permits calls to `extern \"c\" fn` declarations",
	},
	{
		Name:        "ptr_cast",
		Detail:      "raw pointer cast",
		Permits:     []string{"Raw pointer to raw pointer casts with `cast<ptr<...>>(value)`."},
		ShortPermit: "permits raw pointer casts with `cast<ptr<...>>(value)`",
	},
	{
		Name:   "ptr_deref",
		Detail: "raw pointer dereference",
		Permits: []string{
			"Reading through `p.*`.",
			"Writing through `p.* = value` when the pointer is mutable.",
			"Reading or assigning struct fields through `p.*.field`.",
		},
		ShortPermit: "permits raw pointer dereference such as `p.*` and `p.*.field`",
	},
	{
		Name:   "ptr_int_cast",
		Detail: "integer and pointer conversion",
		Permits: []string{
			"Creating raw pointers with `ptr_from_int<ptr<...>>(value)`.",
			"Converting raw pointers to integers with `int_from_ptr<usize>(value)`.",
		},
		ShortPermit: "permits `ptr_from_int<ptr<...>>(value)` and `int_from_ptr<usize>(value)`",
	},
	{
		Name:        "ptr_read",
		Detail:      "raw pointer read",
		Permits:     []string{"Reading a raw pointer with `ptr_read(p)`."},
		ShortPermit: "permits raw pointer reads with `ptr_read(p)`",
	},
	{
		Name:        "ptr_write",
		Detail:      "raw pointer write",
		Permits:     []string{"Writing a mutable raw pointer with `ptr_write(p, value)`."},
		ShortPermit: "permits mutable raw pointer writes with `ptr_write(p, value)`",
	},
	{
		Name:        "unsafe_call",
		Detail:      "caller-obligation function call",
		Permits:     []string{"Calling functions or methods declared with `@requires_unsafe()`."},
		ShortPermit: "permits calls to functions or methods declared with `@requires_unsafe()`",
	},
	{
		Name:   "volatile",
		Detail: "volatile read or write",
		Permits: []string{
			"Volatile raw pointer reads with `volatile_read(p)`.",
			"Volatile raw pointer writes with `volatile_write(p, value)`.",
		},
		ShortPermit: "permits volatile reads and writes with `volatile_read` / `volatile_write`",
	},
}

// All returns the reserved unsafe capabilities in display order.
func All() []Info {
	out := make([]Info, len(capabilities))
	copy(out, capabilities)
	return out
}

// Lookup returns metadata for a reserved unsafe capability.
func Lookup(name string) (Info, bool) {
	for _, capability := range capabilities {
		if capability.Name == name {
			return capability, true
		}
	}
	return Info{}, false
}

// Markdown renders a compact markdown explanation for completion and hover.
func Markdown(info Info) string {
	var builder strings.Builder
	builder.WriteString(info.Detail)
	builder.WriteString("\n\nPermits:\n")
	for _, permit := range info.Permits {
		builder.WriteString("- ")
		builder.WriteString(permit)
		builder.WriteByte('\n')
	}
	builder.WriteString("\n`@unsafe(")
	builder.WriteString(info.Name)
	builder.WriteString(")` does not disable type, move, or borrow checks.")
	return strings.TrimSpace(builder.String())
}

// Hint renders a one-line help note for missing-capability diagnostics.
func Hint(info Info) string {
	return "`@unsafe(" + info.Name + ")` " + info.ShortPermit + "."
}
