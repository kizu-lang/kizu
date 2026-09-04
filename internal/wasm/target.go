package wasm

import (
	"fmt"
	"strings"
)

// Target names the host boundary attached to a common WebAssembly module.
type Target uint8

const (
	// TargetWASI imports WASI preview1 capabilities and exports _start.
	TargetWASI Target = iota
	// TargetBrowser imports the Kizu browser host ABI and exports kizu_start.
	TargetBrowser
)

// name returns the CLI spelling used in target diagnostics.
func (t Target) name() string {
	if t == TargetBrowser {
		return "wasm32-browser"
	}
	return "wasm32-wasi"
}

// isBrowser reports whether browser-specific imports and exports are needed.
func (t Target) isBrowser() bool {
	return t == TargetBrowser
}

// validateTarget rejects reached host capabilities the selected boundary does
// not provide before any module text is written.
func (e *emitter) validateTarget() error {
	if err := e.validateForeignBoundary(); err != nil {
		return err
	}
	if e.target.isBrowser() && e.usesFSRuntime() {
		return e.unsupportedCapability("std::fs")
	}
	if err := e.validateRuntimeCapabilities(); err != nil {
		return err
	}
	if !e.target.isBrowser() {
		return e.validateProcessTarget()
	}
	if e.usesBuiltinCall("std::internal::builtin::io_read_stdin_into") {
		return e.unsupportedCapability("std::io::read_stdin")
	}
	unsupportedProcess := []struct {
		builtin string
		public  string
	}{
		{processArgCountBuiltin, "std::process::arg_count"},
		{processArgBuiltin, "std::process::arg"},
		{processEnvBuiltin, "std::process::env"},
		{processExecutableBuiltin, "std::process::executable_path"},
		{processMonotonicBuiltin, "std::process::monotonic_millis"},
		{processUnixBuiltin, "std::process::unix_millis"},
		{processSpawnWaitBuiltin, "std::process::spawn_wait8"},
	}
	for _, unsupported := range unsupportedProcess {
		if e.usesBuiltinCall(unsupported.builtin) {
			return e.unsupportedCapability(unsupported.public)
		}
	}
	return nil
}

// validateRuntimeCapabilities rejects runtime families neither supported Wasm
// host can currently provide. Keeping this ahead of text or binary emission
// gives both renderers the same target-boundary failure.
func (e *emitter) validateRuntimeCapabilities() error {
	if e.usesAnyBuiltin(eventedBuiltins...) {
		return e.unsupportedCapability("evented std::io")
	}
	if e.usesAnyBuiltin(coroBuiltins...) {
		return e.unsupportedCapability("std::coro")
	}
	if e.usesAnyBuiltin(netBuiltins...) {
		return e.unsupportedCapability("std::net")
	}
	if e.usesFloatTypes() {
		return e.unsupportedCapability("f32 / f64")
	}
	return nil
}

// usesFloatTypes reports whether any function, value, or struct field in the
// module has a floating-point type. Every integer here lives in an i64
// local, and a float would need a local of its own kind.
func (e *emitter) usesFloatTypes() bool {
	for _, st := range e.module.Structs {
		for _, field := range st.Fields {
			if mentionsFloatType(field.Type) {
				return true
			}
		}
	}
	for _, fn := range e.module.Functions {
		if mentionsFloatType(fn.Return) {
			return true
		}
		for _, param := range fn.Params {
			if mentionsFloatType(param.Type) {
				return true
			}
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if mentionsFloatType(instr.Result.Type) {
					return true
				}
				for _, arg := range instr.Args {
					if mentionsFloatType(arg.Type) {
						return true
					}
				}
			}
		}
	}
	return false
}

// mentionsFloatType reports whether a type spelling names `f32` or `f64`,
// on its own or inside a composite such as `?f64` or `std::array::Array<f32>`.
func mentionsFloatType(typ string) bool {
	for _, name := range []string{"f32", "f64"} {
		start := 0
		for {
			at := strings.Index(typ[start:], name)
			if at < 0 {
				break
			}
			at += start
			end := at + len(name)
			before := at == 0 || !isTypeNameByte(typ[at-1])
			after := end == len(typ) || !isTypeNameByte(typ[end])
			if before && after {
				return true
			}
			start = end
		}
	}
	return false
}

// isTypeNameByte reports whether b can continue an identifier in a type.
func isTypeNameByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// unsupportedCapability formats one explicit target-boundary refusal.
func (e *emitter) unsupportedCapability(name string) error {
	return fmt.Errorf(
		"wasm error: target %s does not support %s",
		e.target.name(), name,
	)
}

// validateForeignBoundary checks the ABI facts lowering retained. No backend
// infers foreignness from a callee name.
func (e *emitter) validateForeignBoundary() error {
	for _, function := range e.module.Functions {
		if function.ExportABI != "" &&
			(!e.target.isBrowser() || function.ExportABI != "browser") {
			return fmt.Errorf(
				"wasm error: target %s does not support export `%s`",
				e.target.name(), function.ExportABI,
			)
		}
		for _, block := range function.Blocks {
			for _, instr := range block.Instrs {
				if err := e.validateExternABI(instr.ExternABI); err != nil {
					return err
				}
				for _, cleanup := range instr.Cleanups {
					if err := e.validateExternABI(cleanup.ExternABI); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// validateExternABI applies one target rule to a direct or deferred call.
func (e *emitter) validateExternABI(abi string) error {
	if abi == "" || (e.target.isBrowser() && abi == "browser") {
		return nil
	}
	if e.target.isBrowser() && abi == "c" {
		return fmt.Errorf(
			"wasm error: target %s does not support extern C",
			e.target.name(),
		)
	}
	return fmt.Errorf(
		"wasm error: target %s does not support extern `%s`",
		e.target.name(), abi,
	)
}

// usesAnyBuiltin reports whether one capability group remains reachable.
func (e *emitter) usesAnyBuiltin(names ...string) bool {
	for _, name := range names {
		if e.usesBuiltinCall(name) {
			return true
		}
	}
	return false
}

var eventedBuiltins = []string{
	"std::internal::builtin::io_loop_new",
	"std::internal::builtin::io_loop_close",
	"std::internal::builtin::io_evented",
	"std::internal::builtin::task_finished",
	"std::internal::builtin::task_await",
	"std::internal::builtin::task_cancel",
	"std::internal::builtin::task_close",
	"std::internal::builtin::task_new",
	"std::internal::builtin::task_set_new",
	"std::internal::builtin::task_set_spawn",
	"std::internal::builtin::task_set_close",
}

var coroBuiltins = []string{
	"std::internal::builtin::coro_new",
	"std::internal::builtin::coro_resume",
	"std::internal::builtin::coro_suspend",
	"std::internal::builtin::coro_finished",
	"std::internal::builtin::coro_close",
}

var netBuiltins = []string{
	"std::internal::builtin::net_listen",
	"std::internal::builtin::net_connect",
	"std::internal::builtin::net_accept",
	"std::internal::builtin::net_read",
	"std::internal::builtin::net_write_all",
	"std::internal::builtin::net_write_some",
	"std::internal::builtin::net_local_port",
	"std::internal::builtin::net_close",
	"std::internal::builtin::net_poller_new",
	"std::internal::builtin::net_poller_add",
	"std::internal::builtin::net_poller_remove",
	"std::internal::builtin::net_poller_wait",
	"std::internal::builtin::net_poller_token",
	"std::internal::builtin::net_poller_flags",
	"std::internal::builtin::net_poller_close",
}
