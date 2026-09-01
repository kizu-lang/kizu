package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

const (
	ioBlockingBuiltin    = "std::internal::builtin::io_blocking"
	ioFailingBuiltin     = "std::internal::builtin::io_failing"
	ioWriteStdoutBuiltin = "std::internal::builtin::io_write_stdout"
	ioWriteStderrBuiltin = "std::internal::builtin::io_write_stderr"
	ioErrorSet           = "std::io::Error"
	ioWriteFailed        = "WriteFailed"
	ioFailingError       = "IoFailing"
	ioWorkingToken       = 1
	ioFailingToken       = 2
)

// writeIOBuiltinCall lowers the two Io capability constructors. Blocking Io
// carries no hidden handle; it is the working token paired with failing Io.
func (e *emitter) writeIOBuiltinCall(name string, instr *ir.Instr) (bool, error) {
	var token int
	switch name {
	case ioBlockingBuiltin:
		token = ioWorkingToken
	case ioFailingBuiltin:
		token = ioFailingToken
	default:
		return false, nil
	}
	if len(instr.Args) != 0 || instr.Result.Type != "Io" {
		return true, fmt.Errorf("wasm error: %s expects no args -> Io",
			strings.TrimPrefix(name, "std::internal::builtin::"))
	}
	return true, e.writeScalarResult(instr.Result, fmt.Sprintf("(i32.const %d)", token))
}

// usesIOWrite reports whether either explicit stdout/stderr primitive remains
// reachable after function pruning.
func (e *emitter) usesIOWrite() bool {
	return e.usesBuiltinCall(ioWriteStdoutBuiltin) || e.usesBuiltinCall(ioWriteStderrBuiltin)
}

// writeIORuntime emits only the explicit stream writers the guest reaches.
func (e *emitter) writeIORuntime() error {
	if !e.usesIOWrite() {
		return nil
	}
	writeFailed, err := e.wasmErrorCode(ioErrorSet, ioWriteFailed)
	if err != nil {
		return err
	}
	ioFailing, err := e.wasmErrorCode(ioErrorSet, ioFailingError)
	if err != nil {
		return err
	}
	e.writeIOWriteHelper(writeFailed, ioFailing)
	if e.usesBuiltinCall(ioWriteStdoutBuiltin) {
		e.writeIOWriteBuiltin(ioWriteStdoutBuiltin, 1)
	}
	if e.usesBuiltinCall(ioWriteStderrBuiltin) {
		e.writeIOWriteBuiltin(ioWriteStderrBuiltin, 2)
	}
	return nil
}

// writeIOWriteHelper writes a whole byte slice, retrying partial WASI writes.
// A zero-progress success is rejected so the loop cannot hide a stuck host.
func (e *emitter) writeIOWriteHelper(writeFailed int, ioFailing int) {
	if e.target.isBrowser() {
		e.writeBrowserIOWriteHelper(writeFailed, ioFailing)
		return
	}
	e.out.WriteString("  (func $__io_write (param $out i32) (param $io i32)\n")
	e.out.WriteString("      (param $bytes i32) (param $fd i32)\n")
	e.out.WriteString("    (local $ptr i32) (local $remaining i32)\n")
	e.out.WriteString("    (local $written i32) (local $errno i32)\n")
	fmt.Fprintf(&e.out, "    (if (i32.eq (local.get $io) (i32.const %d))\n", ioFailingToken)
	e.out.WriteString("      (then\n")
	e.writeErrorResult(ioFailing, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $ptr (i32.load (local.get $bytes)))\n")
	e.out.WriteString("    (local.set $remaining\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $bytes) (i32.const 4))))\n")
	e.out.WriteString("    (block $done\n")
	e.out.WriteString("      (loop $write\n")
	e.out.WriteString("        (br_if $done (i32.eqz (local.get $remaining)))\n")
	fmt.Fprintf(&e.out, "        (i32.store (i32.const %d) (local.get $ptr))\n", scratchOffset)
	fmt.Fprintf(&e.out, "        (i32.store (i32.const %d) (local.get $remaining))\n",
		scratchOffset+4)
	e.out.WriteString("        (local.set $errno (call $__wasi_fd_write\n")
	fmt.Fprintf(&e.out,
		"          (local.get $fd) (i32.const %d) (i32.const 1) (i32.const %d)))\n",
		scratchOffset, scratchOffset+16)
	e.out.WriteString("        (if (local.get $errno)\n")
	e.out.WriteString("          (then\n")
	e.writeErrorResult(writeFailed, "            ")
	e.out.WriteString("            (return)))\n")
	fmt.Fprintf(&e.out, "        (local.set $written (i32.load (i32.const %d)))\n",
		scratchOffset+16)
	e.out.WriteString("        (if (i32.or (i32.eqz (local.get $written))\n")
	e.out.WriteString("            (i32.gt_u (local.get $written) (local.get $remaining)))\n")
	e.out.WriteString("          (then\n")
	e.writeErrorResult(writeFailed, "            ")
	e.out.WriteString("            (return)))\n")
	e.out.WriteString("        (local.set $ptr\n")
	e.out.WriteString("          (i32.add (local.get $ptr) (local.get $written)))\n")
	e.out.WriteString("        (local.set $remaining\n")
	e.out.WriteString("          (i32.sub (local.get $remaining) (local.get $written)))\n")
	e.out.WriteString("        (br $write)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeBrowserIOWriteHelper maps one whole-buffer browser callback status to
// std::io::Error. Unlike a descriptor, the callback has no partial-write state.
func (e *emitter) writeBrowserIOWriteHelper(writeFailed int, ioFailing int) {
	e.out.WriteString("  (func $__io_write (param $out i32) (param $io i32)\n")
	e.out.WriteString("      (param $bytes i32) (param $stream i32)\n")
	e.out.WriteString("    (local $errno i32)\n")
	fmt.Fprintf(&e.out, "    (if (i32.eq (local.get $io) (i32.const %d))\n", ioFailingToken)
	e.out.WriteString("      (then\n")
	e.writeErrorResult(ioFailing, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (local.set $errno (call $__kizu_write (local.get $stream)\n")
	e.out.WriteString("      (i32.load (local.get $bytes))\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $bytes) (i32.const 4)))))\n")
	e.out.WriteString("    (if (local.get $errno)\n")
	e.out.WriteString("      (then\n")
	e.writeErrorResult(writeFailed, "        ")
	e.out.WriteString("        (return)))\n")
	e.out.WriteString("    (i64.store (local.get $out) (i64.const 1))\n")
	e.out.WriteString("  )\n\n")
}

// writeIOWriteBuiltin binds one std primitive to its target stream number.
func (e *emitter) writeIOWriteBuiltin(name string, fd int) {
	fmt.Fprintf(&e.out, "  (func $%s (param $out i32) (param $io i32) (param $bytes i32)\n", name)
	fmt.Fprintf(&e.out,
		"    (call $__io_write (local.get $out) (local.get $io) (local.get $bytes) (i32.const %d))\n",
		fd)
	e.out.WriteString("  )\n\n")
}
