// Package unsafecap names the operation kinds the compiler cannot prove, and
// the help text for each. Source no longer spells these names; they exist so a
// diagnostic can say which kind of unproven operation needed the `unsafe`.
package unsafecap

// Info describes one kind of operation the compiler cannot prove.
type Info struct {
	Name        string
	ShortPermit string
}

var capabilities = []Info{
	{
		Name:        "extern_call",
		ShortPermit: "covers calls to `extern \"c\" fn` declarations",
	},
	{
		Name:        "ptr_cast",
		ShortPermit: "covers raw pointer casts with `cast<ptr<...>>(value)`",
	},
	{
		Name:        "ptr_deref",
		ShortPermit: "covers raw pointer dereference such as `p.*` and `p.*.field`",
	},
	{
		Name:        "ptr_int_cast",
		ShortPermit: "covers `ptr_from_int<ptr<...>>(value)` and `int_from_ptr<usize>(value)`",
	},
	{
		Name:        "ptr_read",
		ShortPermit: "covers raw pointer reads with `ptr_read(p)`",
	},
	{
		Name:        "ptr_write",
		ShortPermit: "covers mutable raw pointer writes with `ptr_write(p, value)`",
	},
	{
		Name:        "unsafe_call",
		ShortPermit: "covers calls to functions or methods declared `unsafe fn`",
	},
	{
		Name:        "volatile",
		ShortPermit: "covers volatile reads and writes with `volatile_read` / `volatile_write`",
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

// Hint renders a one-line help note naming what the `unsafe` would cover.
func Hint(info Info) string {
	return "`unsafe` here " + info.ShortPermit + "."
}
