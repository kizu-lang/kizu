// Package stdtarget names the compiler-defined `std::target` predicates and
// the build target they inspect.
//
// These predicates have no runtime implementation. Type checking, ownership
// checking, and IR lowering all evaluate the same registry so a comptime
// branch cannot select a different adapter in different compiler phases.
package stdtarget

// Target is the host contract selected for one build.
type Target uint8

// The build targets visible to Kizu source.
const (
	Native Target = iota
	WasmWASI
	WasmBrowser
)

// Predicate is one compiler-resolved `std::target` spelling.
type Predicate uint8

// The target predicates.
const (
	IsNative Predicate = iota
	IsWASI
	IsBrowser
)

// Identify returns the predicate named by source text.
func Identify(name string) (Predicate, bool) {
	switch name {
	case "std::target::is_native":
		return IsNative, true
	case "std::target::is_wasi":
		return IsWASI, true
	case "std::target::is_browser":
		return IsBrowser, true
	default:
		return 0, false
	}
}

// Evaluate answers one predicate for the selected build target.
func Evaluate(target Target, predicate Predicate) bool {
	switch predicate {
	case IsNative:
		return target == Native
	case IsWASI:
		return target == WasmWASI
	case IsBrowser:
		return target == WasmBrowser
	default:
		return false
	}
}

// Spelling returns the source spelling of one predicate.
func Spelling(predicate Predicate) string {
	switch predicate {
	case IsNative:
		return "std::target::is_native"
	case IsWASI:
		return "std::target::is_wasi"
	case IsBrowser:
		return "std::target::is_browser"
	default:
		return ""
	}
}

// Predicates returns every predicate in declaration order.
func Predicates() []Predicate {
	return []Predicate{IsNative, IsWASI, IsBrowser}
}
