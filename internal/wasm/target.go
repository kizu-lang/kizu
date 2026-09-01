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
	if !e.target.isBrowser() {
		return e.validateProcessTarget()
	}
	if e.usesFSRuntime() {
		return fmt.Errorf("wasm error: target %s does not support std::fs", e.target.name())
	}
	if e.usesAnyBuiltin(browserEventedBuiltins...) {
		return fmt.Errorf(
			"wasm error: target %s does not support evented std::io",
			e.target.name(),
		)
	}
	if e.usesAnyBuiltin(browserCoroBuiltins...) {
		return fmt.Errorf(
			"wasm error: target %s does not support std::coro",
			e.target.name(),
		)
	}
	if e.usesAnyBuiltin(browserNetBuiltins...) {
		return fmt.Errorf(
			"wasm error: target %s does not support std::net",
			e.target.name(),
		)
	}
	if e.usesBuiltinCall("std::internal::builtin::io_read_stdin_into") {
		return fmt.Errorf(
			"wasm error: target %s does not support std::io::read_stdin",
			e.target.name(),
		)
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
			return fmt.Errorf(
				"wasm error: target %s does not support %s",
				e.target.name(),
				unsupported.public,
			)
		}
	}
	if e.usesExternalCall() {
		return fmt.Errorf(
			"wasm error: target %s does not support extern C",
			e.target.name(),
		)
	}
	return nil
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

// usesExternalCall reports whether a reached direct call names an extern C
// symbol. Module-local and std builtin calls have an ABI owned by the compiler;
// every other resolved direct call came from an extern declaration.
func (e *emitter) usesExternalCall() bool {
	for _, function := range e.module.Functions {
		for _, block := range function.Blocks {
			for _, instr := range block.Instrs {
				if e.isExternalCall(instr.Op) {
					return true
				}
				for _, cleanup := range instr.Cleanups {
					if e.isExternalCall(cleanup.Op) {
						return true
					}
				}
			}
		}
	}
	return false
}

// isExternalCall classifies one lowered operation without guessing from the
// external symbol's spelling.
func (e *emitter) isExternalCall(op string) bool {
	name, ok := strings.CutPrefix(op, "call.")
	if !ok || name == "indirect" || name == "print" ||
		strings.HasPrefix(name, "std::internal::builtin::") {
		return false
	}
	_, local := e.paramsByFunction[name]
	return !local
}

var browserEventedBuiltins = []string{
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

var browserCoroBuiltins = []string{
	"std::internal::builtin::coro_new",
	"std::internal::builtin::coro_resume",
	"std::internal::builtin::coro_suspend",
	"std::internal::builtin::coro_finished",
	"std::internal::builtin::coro_close",
}

var browserNetBuiltins = []string{
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
