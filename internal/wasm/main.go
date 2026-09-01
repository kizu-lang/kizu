package wasm

import (
	"encoding/binary"
	"fmt"

	"github.com/kizu-lang/kizu/internal/ir"
)

const (
	errorNameDataPrefix = "error-name:"
	exitStatusType      = "std::process::ExitStatus"
)

// mainFunction returns the retained entry point declaration.
func (e *emitter) mainFunction() *ir.Function {
	for _, fn := range e.module.Functions {
		if fn.Name == "main" {
			return fn
		}
	}
	return nil
}

// needsMainExitBoundary reports whether main returns an error union. The
// checker limits its success type to void or std::process::ExitStatus.
func (e *emitter) needsMainExitBoundary() bool {
	main := e.mainFunction()
	if main == nil {
		return false
	}
	_, _, ok := e.errorUnionParts(main.Return)
	return ok
}

// needsProcExit keeps the WASI exit import conditional on a reachable runtime
// path that can terminate with a nonzero process status.
func (e *emitter) needsProcExit() bool {
	return !e.target.isBrowser() &&
		(len(e.panicKinds) > 0 || e.needsMainExitBoundary())
}

// collectMainErrorStrings assigns static storage to every global error
// spelling used by a fallible main.
func (e *emitter) collectMainErrorStrings(offset int) int {
	if !e.needsMainExitBoundary() {
		return offset
	}
	if _, exists := e.strings["panic.prefix"]; !exists {
		const prefix = "runtime error: "
		e.strings["panic.prefix"] = dataRef{offset: offset, length: len(prefix)}
		e.dataOrder = append(e.dataOrder, "panic.prefix")
		offset += len(prefix)
	}

	maxCode := 0
	for _, set := range e.module.ErrorSets {
		for _, code := range set.Tags {
			if code > maxCode {
				maxCode = code
			}
		}
	}
	spellings := make([]string, maxCode+1)
	for _, set := range e.module.ErrorSets {
		for member, code := range set.Tags {
			spellings[code] = set.Name + "::" + member
		}
	}
	e.errorTable.rows = make([]dataRef, len(spellings))
	for code, spelling := range spellings {
		if code == 0 || spelling == "" {
			continue
		}
		key := errorNameDataPrefix + spelling
		ref := dataRef{offset: offset, length: len(spelling)}
		e.strings[key] = ref
		e.dataOrder = append(e.dataOrder, key)
		e.errorTable.rows[code] = ref
		offset += len(spelling)
	}
	return offset
}

// collectMainErrorTable reserves the code-indexed {pointer, length} rows after
// every ordinary data string and enum table has received its offset.
func (e *emitter) collectMainErrorTable(offset int) int {
	if len(e.errorTable.rows) == 0 {
		return offset
	}
	offset = alignUp(offset, 4)
	e.errorTable.offset = offset
	return offset + len(e.errorTable.rows)*8
}

// writeMainErrorTable writes i32 pointer/length pairs indexed by the module's
// global error code. Missing and reserved codes retain an empty row.
func (e *emitter) writeMainErrorTable() {
	if len(e.errorTable.rows) == 0 {
		return
	}
	data := make([]byte, len(e.errorTable.rows)*8)
	for code, row := range e.errorTable.rows {
		binary.LittleEndian.PutUint32(data[code*8:], uint32(row.offset))
		binary.LittleEndian.PutUint32(data[code*8+4:], uint32(row.length))
	}
	fmt.Fprintf(&e.out, "  (data (i32.const %d) \"%s\")\n",
		e.errorTable.offset, stringBytes(string(data)))
}

// writeMainErrorRuntime reports one uncaught error and exits with status 1.
func (e *emitter) writeMainErrorRuntime() {
	prefix := e.strings["panic.prefix"]
	newline := e.strings["newline"]
	e.out.WriteString("  (func $__main_error (param $code i64)\n")
	e.out.WriteString("    (local $row i32)\n")
	e.out.WriteString("    (if (i32.or (i64.le_s (local.get $code) (i64.const 0))\n")
	fmt.Fprintf(&e.out, "        (i64.ge_u (local.get $code) (i64.const %d)))\n",
		len(e.errorTable.rows))
	e.out.WriteString("      (then (unreachable)))\n")
	fmt.Fprintf(&e.out, "    (local.set $row (i32.add (i32.const %d)\n", e.errorTable.offset)
	e.out.WriteString("      (i32.shl (i32.wrap_i64 (local.get $code)) (i32.const 3))))\n")
	fmt.Fprintf(&e.out,
		"    (call $__write_bytes (i32.const 2) (i32.const %d) (i32.const %d))\n",
		prefix.offset, prefix.length)
	e.out.WriteString("    (call $__write_bytes (i32.const 2) (i32.load (local.get $row))\n")
	e.out.WriteString("      (i32.load (i32.add (local.get $row) (i32.const 4))))\n")
	fmt.Fprintf(&e.out,
		"    (call $__write_bytes (i32.const 2) (i32.const %d) (i32.const 1))\n",
		newline.offset)
	if !e.target.isBrowser() {
		e.out.WriteString("    (call $__wasi_proc_exit (i32.const 1))\n")
		e.out.WriteString("    (unreachable)\n")
	}
	e.out.WriteString("  )\n\n")
}

// writeMainResultBoundary maps main's failed error union and successful
// ExitStatus payload to the WASI process boundary.
func (e *emitter) writeMainResultBoundary(main *ir.Function) error {
	_, success, payloadOffset, err := e.errorPayloadOffset(main.Return)
	if err != nil {
		return err
	}
	result := "(local.get $__kizu_main_result)"
	e.out.WriteString("    (if (i64.eq (i64.load " + result + ") (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	fmt.Fprintf(&e.out, "        (call $__main_error (i64.load %s))\n",
		addressAt(result, payloadOffset))
	e.out.WriteString("        (unreachable)))\n")
	if success == exitStatusType {
		return e.writeMainExitStatus(result, payloadOffset)
	}
	return nil
}

// writeBrowserMainResultBoundary reports an uncaught error and records the
// status returned by kizu_start. A successful ExitStatus is mapped without
// terminating the embedding page.
func (e *emitter) writeBrowserMainResultBoundary(main *ir.Function) error {
	_, success, payloadOffset, err := e.errorPayloadOffset(main.Return)
	if err != nil {
		return err
	}
	result := "(local.get $__kizu_main_result)"
	tag := "(i64.load " + result + ")"
	e.out.WriteString("    (if (i64.eq " + tag + " (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	fmt.Fprintf(&e.out, "        (call $__main_error (i64.load %s))\n",
		addressAt(result, payloadOffset))
	e.out.WriteString("        (local.set $__kizu_status (i32.const 1))))\n")
	if success != exitStatusType {
		return nil
	}
	e.out.WriteString("    (if (i64.ne " + tag + " (i64.const 0))\n")
	e.out.WriteString("      (then\n")
	if err := e.writeBrowserMainExitStatus(result, payloadOffset); err != nil {
		return err
	}
	e.out.WriteString("      ))\n")
	return nil
}

// writeBrowserMainExitStatus maps declaration-owned variants to the integer
// returned by kizu_start: Success is 0, Failure is 1, and Specific carries u8.
func (e *emitter) writeBrowserMainExitStatus(result string, errorPayloadOffset int) error {
	declared, ok := e.module.Unions[exitStatusType]
	if !ok {
		return fmt.Errorf("wasm error: unknown union type `%s`", exitStatusType)
	}
	success, ok := declared.Variants["Success"]
	if !ok {
		return fmt.Errorf("wasm error: unknown union variant `%s::Success`", exitStatusType)
	}
	failure, ok := declared.Variants["Failure"]
	if !ok {
		return fmt.Errorf("wasm error: unknown union variant `%s::Failure`", exitStatusType)
	}
	specific, ok := declared.Variants["Specific"]
	if !ok || specific.Payload != "u8" {
		return fmt.Errorf("wasm error: unknown union payload `%s::Specific`", exitStatusType)
	}
	unionOffset, err := e.unionPayloadOffset(exitStatusType)
	if err != nil {
		return err
	}
	status := addressAt(result, errorPayloadOffset)
	tag := "(i64.load " + status + ")"
	fmt.Fprintf(&e.out, "        (if (i64.eq %s (i64.const %d))\n", tag, specific.Index)
	e.out.WriteString("          (then (local.set $__kizu_status (i32.load8_u ")
	e.out.WriteString(addressAt(status, unionOffset) + "))))\n")
	fmt.Fprintf(&e.out, "        (if (i64.eq %s (i64.const %d))\n", tag, failure.Index)
	e.out.WriteString("          (then (local.set $__kizu_status (i32.const 1))))\n")
	fmt.Fprintf(&e.out,
		"        (if (i32.and (i64.ne %s (i64.const %d))\n", tag, success.Index)
	fmt.Fprintf(&e.out,
		"            (i32.and (i64.ne %s (i64.const %d))\n", tag, failure.Index)
	fmt.Fprintf(&e.out,
		"              (i64.ne %s (i64.const %d))))\n", tag, specific.Index)
	e.out.WriteString("          (then (unreachable)))\n")
	return nil
}

// writeMainExitStatus maps declaration-owned variant indexes to WASI status:
// Success returns from _start, Failure exits 1, and Specific carries a u8.
func (e *emitter) writeMainExitStatus(result string, errorPayloadOffset int) error {
	declared, ok := e.module.Unions[exitStatusType]
	if !ok {
		return fmt.Errorf("wasm error: unknown union type `%s`", exitStatusType)
	}
	success, ok := declared.Variants["Success"]
	if !ok {
		return fmt.Errorf("wasm error: unknown union variant `%s::Success`", exitStatusType)
	}
	failure, ok := declared.Variants["Failure"]
	if !ok {
		return fmt.Errorf("wasm error: unknown union variant `%s::Failure`", exitStatusType)
	}
	specific, ok := declared.Variants["Specific"]
	if !ok || specific.Payload != "u8" {
		return fmt.Errorf("wasm error: unknown union payload `%s::Specific`", exitStatusType)
	}
	unionOffset, err := e.unionPayloadOffset(exitStatusType)
	if err != nil {
		return err
	}
	status := addressAt(result, errorPayloadOffset)
	tag := "(i64.load " + status + ")"
	fmt.Fprintf(&e.out, "    (if (i64.eq %s (i64.const %d))\n", tag, specific.Index)
	e.out.WriteString("      (then\n")
	fmt.Fprintf(&e.out, "        (call $__wasi_proc_exit (i32.load8_u %s))\n",
		addressAt(status, unionOffset))
	e.out.WriteString("        (unreachable)))\n")
	fmt.Fprintf(&e.out, "    (if (i64.eq %s (i64.const %d))\n", tag, failure.Index)
	e.out.WriteString("      (then\n")
	e.out.WriteString("        (call $__wasi_proc_exit (i32.const 1))\n")
	e.out.WriteString("        (unreachable)))\n")
	fmt.Fprintf(&e.out, "    (if (i64.ne %s (i64.const %d))\n", tag, success.Index)
	e.out.WriteString("      (then (unreachable)))\n")
	return nil
}
