// Package stdtarget names the compiler-defined `std::target` predicates and
// the build target they inspect.
//
// These predicates have no runtime implementation. Type checking, ownership
// checking, and IR lowering all evaluate the same registry so a comptime
// branch cannot select a different adapter in different compiler phases.
package stdtarget

import (
	"fmt"
	"runtime"
)

// Target is the host contract selected for one build. A native target also
// names its operating system, which is what the OS predicates and the
// manifest's `[native.<os>]` sections read.
type Target uint8

// The build targets visible to Kizu source.
const (
	NativeDarwin Target = iota
	NativeLinux
	WasmWASI
	WasmBrowser
)

// IsNative reports whether the target links a native executable.
func (t Target) IsNative() bool {
	return t == NativeDarwin || t == NativeLinux
}

// OS returns the operating system name a native target is known by in source
// and in the manifest: "darwin" or "linux". A wasm target has none.
func (t Target) OS() string {
	switch t {
	case NativeDarwin:
		return "darwin"
	case NativeLinux:
		return "linux"
	default:
		return ""
	}
}

// NativeOS names the operating systems a native target can select, in the
// spelling the predicates and the manifest share.
func NativeOS() []string {
	return []string{"darwin", "linux"}
}

// NativeFor returns the native target for one operating system name.
func NativeFor(os string) (Target, bool) {
	switch os {
	case "darwin":
		return NativeDarwin, true
	case "linux":
		return NativeLinux, true
	default:
		return 0, false
	}
}

// Host returns the native target the compiler itself runs on. The native
// runtime is built for Darwin and Linux, so any other host is an error rather
// than a guess.
func Host() (Target, error) {
	if target, ok := NativeFor(runtime.GOOS); ok {
		return target, nil
	}
	return 0, fmt.Errorf("native target: unsupported host %s (darwin, linux)", runtime.GOOS)
}

// MustHost is Host for callers that have no error to return: the default
// constructors tests use. Commands resolve the host with Host so an
// unsupported host is reported, not a crash.
func MustHost() Target {
	target, err := Host()
	if err != nil {
		panic(err)
	}
	return target
}

// Predicate is one compiler-resolved `std::target` spelling.
type Predicate uint8

// The target predicates.
const (
	IsNative Predicate = iota
	IsWASI
	IsBrowser
	IsDarwin
	IsLinux
)

// Identify returns the predicate named by source text.
func Identify(name string) (Predicate, bool) {
	for _, predicate := range Predicates() {
		if Spelling(predicate) == name {
			return predicate, true
		}
	}
	return 0, false
}

// Evaluate answers one predicate for the selected build target.
func Evaluate(target Target, predicate Predicate) bool {
	switch predicate {
	case IsNative:
		return target.IsNative()
	case IsWASI:
		return target == WasmWASI
	case IsBrowser:
		return target == WasmBrowser
	case IsDarwin:
		return target == NativeDarwin
	case IsLinux:
		return target == NativeLinux
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
	case IsDarwin:
		return "std::target::is_darwin"
	case IsLinux:
		return "std::target::is_linux"
	default:
		return ""
	}
}

// Predicates returns every predicate in declaration order.
func Predicates() []Predicate {
	return []Predicate{IsNative, IsWASI, IsBrowser, IsDarwin, IsLinux}
}
