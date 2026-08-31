package wasm

import (
	"fmt"
)

const (
	processArgCountBuiltin     = "std::internal::builtin::process_arg_count"
	processArgBuiltin          = "std::internal::builtin::process_arg"
	processEnvBuiltin          = "std::internal::builtin::process_env"
	processExecutableBuiltin   = "std::internal::builtin::process_executable_path_into"
	processMonotonicBuiltin    = "std::internal::builtin::process_monotonic_millis"
	processUnixBuiltin         = "std::internal::builtin::process_unix_millis"
	processSpawnWaitBuiltin    = "std::internal::builtin::process_spawn_wait8"
	processArgBoundsError      = "ArgIndexOutOfBounds"
	processExecutableUnknown   = "ExecutablePathUnknown"
	processOutOfMemory         = "OutOfMemory"
	processErrorSet            = "std::process::Error"
	processTaggedPayloadOffset = 8
	processSliceLengthOffset   = 4
	processArrayLengthOffset   = 8
	processClockRealtime       = 0
	processClockMonotonic      = 1
	processClockPrecisionNanos = 1000000
	processNanosPerMillisecond = 1000000
)

// usesBuiltinCall reports whether the pruned module reaches one host builtin.
func (e *emitter) usesBuiltinCall(name string) bool {
	op := "call." + name
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == op {
					return true
				}
				for _, cleanup := range instr.Cleanups {
					if cleanup.Op == op {
						return true
					}
				}
			}
		}
	}
	return false
}

// usesProcessArgCount reports whether the guest reads its argument count.
func (e *emitter) usesProcessArgCount() bool {
	return e.usesBuiltinCall(processArgCountBuiltin)
}

// usesProcessArg reports whether the guest reads an argument value.
func (e *emitter) usesProcessArg() bool {
	return e.usesBuiltinCall(processArgBuiltin)
}

// usesProcessEnv reports whether the guest reads its environment.
func (e *emitter) usesProcessEnv() bool {
	return e.usesBuiltinCall(processEnvBuiltin)
}

// usesProcessExecutablePath reports whether the guest reads WASI argv[0].
func (e *emitter) usesProcessExecutablePath() bool {
	return e.usesBuiltinCall(processExecutableBuiltin)
}

// usesProcessMonotonicClock reports whether the guest reads the steady clock.
func (e *emitter) usesProcessMonotonicClock() bool {
	return e.usesBuiltinCall(processMonotonicBuiltin)
}

// usesProcessUnixClock reports whether the guest reads the realtime clock.
func (e *emitter) usesProcessUnixClock() bool {
	return e.usesBuiltinCall(processUnixBuiltin)
}

// usesProcessArgsMetadata reports whether the guest needs WASI argv sizes.
func (e *emitter) usesProcessArgsMetadata() bool {
	return e.usesProcessArgCount() || e.usesProcessArg() || e.usesProcessExecutablePath()
}

// usesProcessArgsData reports whether the guest needs the WASI argv bytes.
func (e *emitter) usesProcessArgsData() bool {
	return e.usesProcessArg() || e.usesProcessExecutablePath()
}

// usesProcessClock reports whether the guest needs a WASI clock import.
func (e *emitter) usesProcessClock() bool {
	return e.usesProcessMonotonicClock() || e.usesProcessUnixClock()
}

// validateProcessTarget refuses process operations WASI preview1 cannot host.
func (e *emitter) validateProcessTarget() error {
	if e.usesBuiltinCall(processSpawnWaitBuiltin) {
		return fmt.Errorf("wasm error: target wasm32-wasi does not support std::process::spawn_wait8")
	}
	return nil
}

// writeProcessImports declares only the WASI process capabilities reached from main.
func (e *emitter) writeProcessImports() {
	if e.usesProcessArgsMetadata() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"args_sizes_get\"\n")
		e.out.WriteString("    (func $__wasi_args_sizes_get (param i32 i32) (result i32)))\n")
	}
	if e.usesProcessArgsData() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"args_get\"\n")
		e.out.WriteString("    (func $__wasi_args_get (param i32 i32) (result i32)))\n")
	}
	if e.usesProcessEnv() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"environ_sizes_get\"\n")
		e.out.WriteString("    (func $__wasi_environ_sizes_get (param i32 i32) (result i32)))\n")
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"environ_get\"\n")
		e.out.WriteString("    (func $__wasi_environ_get (param i32 i32) (result i32)))\n")
	}
	if e.usesProcessClock() {
		e.out.WriteString("  (import \"wasi_snapshot_preview1\" \"clock_time_get\"\n")
		e.out.WriteString("    (func $__wasi_clock_time_get (param i32 i64 i32) (result i32)))\n")
	}
}

// writeProcessGlobals keeps host-provided byte storage alive until _start returns.
func (e *emitter) writeProcessGlobals() {
	if e.usesProcessArgsMetadata() {
		e.out.WriteString("  (global $__process_argc (mut i32) (i32.const 0))\n")
	}
	if e.usesProcessArgsData() {
		e.out.WriteString("  (global $__process_argv (mut i32) (i32.const 0))\n")
	}
	if e.usesProcessEnv() {
		e.out.WriteString("  (global $__process_envc (mut i32) (i32.const 0))\n")
		e.out.WriteString("  (global $__process_envp (mut i32) (i32.const 0))\n")
	}
}

// writeProcessRuntime emits the reached std::process host boundary.
func (e *emitter) writeProcessRuntime() error {
	if e.usesProcessArgsMetadata() {
		e.writeProcessArgsInit()
	}
	if e.usesProcessArgsData() || e.usesProcessEnv() {
		e.writeProcessCStrLen()
	}
	if e.usesProcessArgCount() {
		e.writeProcessArgCount()
	}
	if e.usesProcessArg() {
		if err := e.writeProcessArg(); err != nil {
			return err
		}
	}
	if e.usesProcessEnv() {
		e.writeProcessEnvInit()
		e.writeProcessEnv()
	}
	if e.usesProcessExecutablePath() {
		if err := e.writeProcessExecutablePath(); err != nil {
			return err
		}
	}
	if e.usesProcessMonotonicClock() {
		e.writeProcessClock(processMonotonicBuiltin, processClockMonotonic)
	}
	if e.usesProcessUnixClock() {
		e.writeProcessClock(processUnixBuiltin, processClockRealtime)
	}
	return nil
}

// writeProcessArgsInit snapshots WASI argv into process-owned linear memory.
func (e *emitter) writeProcessArgsInit() {
	e.out.WriteString("  (func $__process_init_args\n")
	if e.usesProcessArgsData() {
		e.out.WriteString("    (local $table_size i32) (local $buffer_size i32)\n")
		e.out.WriteString("    (local $total i32) (local $base i32)\n")
	}
	e.out.WriteString("    (if (call $__wasi_args_sizes_get (i32.const 0) (i32.const 4))\n")
	e.out.WriteString("      (then (unreachable)))\n")
	e.out.WriteString("    (global.set $__process_argc (i32.load (i32.const 0)))\n")
	if e.usesProcessArgsData() {
		e.out.WriteString("    (if (i32.gt_u (global.get $__process_argc) (i32.const 1073741823))\n")
		e.out.WriteString("      (then (unreachable)))\n")
		e.out.WriteString("    (local.set $table_size\n")
		e.out.WriteString("      (i32.shl (global.get $__process_argc) (i32.const 2)))\n")
		e.out.WriteString("    (local.set $buffer_size (i32.load (i32.const 4)))\n")
		e.out.WriteString("    (local.set $total (i32.add (local.get $table_size) " +
			"(local.get $buffer_size)))\n")
		e.out.WriteString("    (if (i32.lt_u (local.get $total) (local.get $table_size))\n")
		e.out.WriteString("      (then (unreachable)))\n")
		e.out.WriteString("    (if (local.get $total)\n")
		e.out.WriteString("      (then\n")
		e.out.WriteString("        (local.set $base (call $__stack_alloc (local.get $total)))\n")
		e.out.WriteString("        (global.set $__process_argv (local.get $base))\n")
		e.out.WriteString("        (if (call $__wasi_args_get\n")
		e.out.WriteString("              (local.get $base)\n")
		e.out.WriteString("              (i32.add (local.get $base) (local.get $table_size)))\n")
		e.out.WriteString("          (then (unreachable)))))\n")
	}
	e.out.WriteString("  )\n\n")
}

// writeProcessCStrLen measures one NUL-terminated string supplied by WASI.
func (e *emitter) writeProcessCStrLen() {
	e.out.WriteString("  (func $__process_cstr_len (param $ptr i32) (result i32)\n")
	e.out.WriteString("    (local $length i32)\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $bytes\n")
	e.out.WriteString("        (br_if $done (i32.eqz\n")
	e.out.WriteString("          (i32.load8_u (i32.add (local.get $ptr) (local.get $length)))))\n")
	e.out.WriteString("        (local.set $length (i32.add (local.get $length) (i32.const 1)))\n")
	e.out.WriteString("        (br $bytes)))\n")
	e.out.WriteString("    (local.get $length)\n")
	e.out.WriteString("  )\n\n")
}

// writeProcessArgCount returns the guest-visible count without WASI argv[0].
func (e *emitter) writeProcessArgCount() {
	fmt.Fprintf(&e.out, "  (func $%s (result i64)\n", processArgCountBuiltin)
	e.out.WriteString("    (if (result i64) (i32.gt_u (global.get $__process_argc) (i32.const 0))\n")
	e.out.WriteString("      (then (i64.extend_i32_u\n")
	e.out.WriteString("        (i32.sub (global.get $__process_argc) (i32.const 1))))\n")
	e.out.WriteString("      (else (i64.const 0)))\n")
	e.out.WriteString("  )\n\n")
}

// writeProcessArg returns one guest argument or ArgIndexOutOfBounds.
func (e *emitter) writeProcessArg() error {
	code, err := e.wasmErrorCode(processErrorSet, processArgBoundsError)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $index i64)\n", processArgBuiltin)
	e.out.WriteString("    (local $ptr i32)\n")
	e.out.WriteString("    (if (i32.or\n")
	e.out.WriteString("          (i32.le_u (global.get $__process_argc) (i32.const 1))\n")
	e.out.WriteString("          (i32.or (i64.lt_s (local.get $index) (i64.const 0))\n")
	e.out.WriteString("            (i64.ge_u (local.get $index)\n")
	e.out.WriteString("              (i64.extend_i32_u\n")
	e.out.WriteString("                (i32.sub (global.get $__process_argc) (i32.const 1))))))\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (i64.store (local.get $out) (i64.const 0))\n")
	fmt.Fprintf(&e.out, "        (i64.store (i32.add (local.get $out) "+
		"(i32.const %d)) (i64.const %d))\n",
		processTaggedPayloadOffset, code)
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $ptr\n")
	e.out.WriteString("      (i32.load (i32.add (global.get $__process_argv)\n")
	e.out.WriteString("        (i32.shl (i32.wrap_i64 (i64.add (local.get $index) (i64.const 1)))\n")
	e.out.WriteString("          (i32.const 2)))))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	fmt.Fprintf(&e.out, "    (i32.store (i32.add (local.get $out) (i32.const %d)) (local.get $ptr))\n",
		processTaggedPayloadOffset)
	fmt.Fprintf(&e.out, "    (i32.store (i32.add (local.get $out) (i32.const %d))\n",
		processTaggedPayloadOffset+processSliceLengthOffset)
	e.out.WriteString("      (call $__process_cstr_len (local.get $ptr)))\n")
	e.out.WriteString("  )\n\n")
	return nil
}

// writeProcessEnvInit snapshots WASI environ into process-owned linear memory.
func (e *emitter) writeProcessEnvInit() {
	e.out.WriteString("  (func $__process_init_env\n")
	e.out.WriteString("    (local $table_size i32) (local $buffer_size i32)\n")
	e.out.WriteString("    (local $total i32) (local $base i32)\n")
	e.out.WriteString("    (if (call $__wasi_environ_sizes_get (i32.const 0) (i32.const 4))\n")
	e.out.WriteString("      (then (unreachable)))\n")
	e.out.WriteString("    (global.set $__process_envc (i32.load (i32.const 0)))\n")
	e.out.WriteString("    (if (i32.gt_u (global.get $__process_envc) (i32.const 1073741823))\n")
	e.out.WriteString("      (then (unreachable)))\n")
	e.out.WriteString("    (local.set $table_size\n")
	e.out.WriteString("      (i32.shl (global.get $__process_envc) (i32.const 2)))\n")
	e.out.WriteString("    (local.set $buffer_size (i32.load (i32.const 4)))\n")
	e.out.WriteString("    (local.set $total (i32.add (local.get $table_size) " +
		"(local.get $buffer_size)))\n")
	e.out.WriteString("    (if (i32.lt_u (local.get $total) (local.get $table_size))\n")
	e.out.WriteString("      (then (unreachable)))\n")
	e.out.WriteString("    (if (local.get $total)\n")
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (local.set $base (call $__stack_alloc (local.get $total)))\n")
	e.out.WriteString("        (global.set $__process_envp (local.get $base))\n")
	e.out.WriteString("        (if (call $__wasi_environ_get\n")
	e.out.WriteString("              (local.get $base)\n")
	e.out.WriteString("              (i32.add (local.get $base) (local.get $table_size)))\n")
	e.out.WriteString("          (then (unreachable)))))\n")
	e.out.WriteString("  )\n\n")
}

// writeProcessEnv looks up NAME=VALUE without copying host-owned bytes.
func (e *emitter) writeProcessEnv() {
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $name i32)\n", processEnvBuiltin)
	e.out.WriteString("    (local $index i32) (local $entry i32) (local $offset i32)\n")
	e.out.WriteString("    (local $name_ptr i32) (local $name_len i32) (local $value i32)\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 0))\n")
	e.out.WriteString("    (local.set $name_ptr (i32.load (local.get $name)))\n")
	e.out.WriteString("    (local.set $name_len (i32.load (i32.add (local.get $name) " +
		"(i32.const 4))))\n")
	e.out.WriteString("    (block $missing\n")
	e.out.WriteString("      (loop $entries\n")
	e.out.WriteString("        (br_if $missing (i32.ge_u (local.get $index) " +
		"(global.get $__process_envc)))\n")
	e.out.WriteString("        (local.set $entry (i32.load (i32.add (global.get $__process_envp)\n")
	e.out.WriteString("          (i32.shl (local.get $index) (i32.const 2)))))\n")
	e.out.WriteString("        (local.set $offset (i32.const 0))\n")
	e.out.WriteString("        (block $different\n")
	e.out.WriteString("          (loop $name_bytes\n")
	e.out.WriteString("            (if (i32.eq (local.get $offset) (local.get $name_len))\n")
	e.out.WriteString("              (then\n")
	e.out.WriteString("                (br_if $different (i32.ne\n")
	e.out.WriteString("                  (i32.load8_u (i32.add (local.get $entry) " +
		"(local.get $offset)))\n")
	e.out.WriteString("                  (i32.const 61)))\n")
	e.out.WriteString("                (local.set $value\n")
	e.out.WriteString("                  (i32.add (i32.add (local.get $entry) " +
		"(local.get $offset)) (i32.const 1)))\n")
	e.out.WriteString("                (i64.store (local.get $out) (i64.const 1))\n")
	fmt.Fprintf(&e.out, "                (i32.store (i32.add (local.get $out) "+
		"(i32.const %d)) (local.get $value))\n",
		processTaggedPayloadOffset)
	fmt.Fprintf(&e.out, "                (i32.store (i32.add (local.get $out) (i32.const %d))\n",
		processTaggedPayloadOffset+processSliceLengthOffset)
	e.out.WriteString("                  (call $__process_cstr_len (local.get $value)))\n")
	e.out.WriteString("                (return)))\n")
	e.out.WriteString("            (br_if $different (i32.ne\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $name_ptr) " +
		"(local.get $offset)))\n")
	e.out.WriteString("              (i32.load8_u (i32.add (local.get $entry) " +
		"(local.get $offset)))))\n")
	e.out.WriteString("            (local.set $offset (i32.add (local.get $offset) (i32.const 1)))\n")
	e.out.WriteString("            (br $name_bytes)))\n")
	e.out.WriteString("        (local.set $index (i32.add (local.get $index) (i32.const 1)))\n")
	e.out.WriteString("        (br $entries)))\n")
	e.out.WriteString("  )\n\n")
}

// writeProcessExecutablePath appends WASI argv[0] to caller-owned storage.
func (e *emitter) writeProcessExecutablePath() error {
	unknown, err := e.wasmErrorCode(processErrorSet, processExecutableUnknown)
	if err != nil {
		return err
	}
	outOfMemory, err := e.wasmErrorCode(processErrorSet, processOutOfMemory)
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $allocator i32) (param $dst i32)\n",
		processExecutableBuiltin)
	e.out.WriteString("    (local $ptr i32) (local $length i32) (local $old_len i64)\n")
	e.out.WriteString("    (local $needed i64)\n")
	e.out.WriteString("    (if (i32.eqz (global.get $__process_argc))\n")
	e.out.WriteString("      (then\n")
	e.writeProcessErrorResult(unknown, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $ptr (i32.load (global.get $__process_argv)))\n")
	e.out.WriteString("    (local.set $length (call $__process_cstr_len (local.get $ptr)))\n")
	e.out.WriteString("    (if (i32.eqz (local.get $length))\n")
	e.out.WriteString("      (then\n")
	e.writeProcessErrorResult(unknown, "        ")
	e.out.WriteString("        (return)))\n")
	fmt.Fprintf(&e.out, "    (local.set $old_len (i64.load (i32.add (local.get $dst) "+
		"(i32.const %d))))\n",
		processArrayLengthOffset)
	e.out.WriteString("    (local.set $needed\n")
	e.out.WriteString("      (i64.add (local.get $old_len) (i64.extend_i32_u (local.get $length))))\n")
	e.out.WriteString("    (if (i32.eqz (call $__array_reserve\n")
	e.out.WriteString("          (local.get $allocator) (local.get $dst) " +
		"(local.get $needed) (i32.const 1)))\n")
	e.out.WriteString("      (then\n")
	e.writeProcessErrorResult(outOfMemory, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (memory.copy\n")
	e.out.WriteString("      (i32.add (i32.load (local.get $dst)) " +
		"(i32.wrap_i64 (local.get $old_len)))\n")
	e.out.WriteString("      (local.get $ptr) (local.get $length))\n")
	fmt.Fprintf(&e.out, "    (i64.store (i32.add (local.get $dst) "+
		"(i32.const %d)) (local.get $needed))\n",
		processArrayLengthOffset)
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
	return nil
}

// writeProcessErrorResult stores one failed process error union.
func (e *emitter) writeProcessErrorResult(code int, indent string) {
	fmt.Fprintf(&e.out, "%s(i64.store (local.get $out) (i64.const 0))\n", indent)
	fmt.Fprintf(&e.out, "%s(i64.store (i32.add (local.get $out) (i32.const %d)) (i64.const %d))\n",
		indent, processTaggedPayloadOffset, code)
}

// writeProcessClock returns one WASI nanosecond clock converted to milliseconds.
func (e *emitter) writeProcessClock(name string, clockID int) {
	fmt.Fprintf(&e.out, "  (func $%s (result i64)\n", name)
	fmt.Fprintf(&e.out, "    (if (result i64) (i32.eqz (call $__wasi_clock_time_get\n")
	fmt.Fprintf(&e.out, "          (i32.const %d) (i64.const %d) (i32.const %d)))\n",
		clockID, processClockPrecisionNanos, scratchOffset)
	fmt.Fprintf(&e.out, "      (then (i64.div_u (i64.load (i32.const %d)) (i64.const %d)))\n",
		scratchOffset, processNanosPerMillisecond)
	e.out.WriteString("      (else (i64.const 0)))\n")
	e.out.WriteString("  )\n\n")
}

// writeProcessStartInit obtains host state before any Kizu function can read it.
func (e *emitter) writeProcessStartInit() {
	if e.usesProcessArgsMetadata() {
		e.out.WriteString("    (call $__process_init_args)\n")
	}
	if e.usesProcessEnv() {
		e.out.WriteString("    (call $__process_init_env)\n")
	}
}
