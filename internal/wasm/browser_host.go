package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// collectBrowserImports retains each reached browser host function once. Two
// modules may declare the same leaf name, so a mismatch is rejected before a
// WebAssembly import with an ambiguous contract is emitted.
func (e *emitter) collectBrowserImports() error {
	if !e.target.isBrowser() {
		return nil
	}
	for _, function := range e.module.Functions {
		for _, block := range function.Blocks {
			for _, instr := range block.Instrs {
				if instr.ExternABI != "browser" {
					continue
				}
				if previous, ok := e.browserImportIndex[instr.ExternName]; ok {
					if !sameBrowserSignature(e.browserImports[previous], instr) {
						return fmt.Errorf(
							"wasm error: browser host import `%s` has conflicting signatures",
							instr.ExternName,
						)
					}
					continue
				}
				e.browserImportIndex[instr.ExternName] = len(e.browserImports)
				e.browserImports = append(e.browserImports, instr)
			}
		}
	}
	return nil
}

// sameBrowserSignature reports whether two source declarations can share one
// WebAssembly import.
func sameBrowserSignature(left *ir.Instr, right *ir.Instr) bool {
	if left.Result.Type != right.Result.Type || len(left.Args) != len(right.Args) {
		return false
	}
	for index := range left.Args {
		if left.Args[index].Type != right.Args[index].Type {
			return false
		}
	}
	return true
}

// writeBrowserImports declares the explicit JavaScript boundary under the
// stable `host` module. A byte slice expands to pointer and length; every
// other admitted type occupies one WebAssembly scalar.
func (e *emitter) writeBrowserImports() {
	for index, instr := range e.browserImports {
		params := []string{}
		for _, arg := range instr.Args {
			if arg.Type == "[]u8" {
				params = append(params, "i32", "i32")
				continue
			}
			params = append(params, browserBoundaryType(arg.Type))
		}
		paramText := ""
		if len(params) > 0 {
			paramText = " (param " + strings.Join(params, " ") + ")"
		}
		resultText := ""
		if instr.Result.Type != "void" {
			resultText = " (result " + browserBoundaryType(instr.Result.Type) + ")"
		}
		fmt.Fprintf(&e.out,
			"  (import \"host\" %q (func $__kizu_host_%d%s%s))\n",
			instr.ExternName, index, paramText, resultText,
		)
	}
}

// browserBoundaryType maps one admitted Kizu scalar to its public Wasm type.
func browserBoundaryType(typ string) string {
	if typ == "i64" || typ == "u64" {
		return "i64"
	}
	return "i32"
}

// writeBrowserHostCall adapts the compiler's uniform i64 integer storage to
// the narrower public wasm ABI, and expands a borrowed byte slice without
// transferring its ownership.
func (e *emitter) writeBrowserHostCall(instr *ir.Instr) error {
	index, ok := e.browserImportIndex[instr.ExternName]
	if !ok {
		return fmt.Errorf("wasm error: missing browser host import `%s`", instr.ExternName)
	}
	args := []string{}
	for _, arg := range instr.Args {
		expr := e.value(arg).expr
		if arg.Type == "[]u8" {
			pointer, length := byteSliceParts(expr)
			args = append(args, pointer, length)
			continue
		}
		args = append(args, browserToHostExpr(arg.Type, expr))
	}
	call := fmt.Sprintf("(call $__kizu_host_%d", index)
	if len(args) > 0 {
		call += " " + strings.Join(args, " ")
	}
	call += ")"
	if instr.Result.Type == "void" {
		fmt.Fprintf(&e.out, "            %s\n", call)
		return nil
	}
	value := browserFromHostExpr(instr.Result.Type, call)
	symbol := symbolName(instr.Result.Name)
	fmt.Fprintf(&e.out, "            (local.set %s %s)\n", symbol, value)
	e.values[instr.Result.Name] = valueInfo{expr: "(local.get " + symbol + ")"}
	return nil
}

// browserToHostExpr narrows the compiler's internal integer representation to
// the declared public boundary width.
func browserToHostExpr(typ string, expr string) string {
	if browserBoundaryType(typ) == "i32" && isIntegerType(typ) {
		return browserCanonicalI32Expr(typ, "(i32.wrap_i64 "+expr+")")
	}
	return expr
}

// browserFromHostExpr normalizes a host value and widens it to the compiler's
// internal representation.
func browserFromHostExpr(typ string, expr string) string {
	if typ == "bool" {
		return "(i32.ne " + expr + " (i32.const 0))"
	}
	if browserBoundaryType(typ) != "i32" || !isIntegerType(typ) {
		return expr
	}
	expr = browserCanonicalI32Expr(typ, expr)
	suffix := "_u"
	if typ == "i8" || typ == "i16" || typ == "i32" || typ == "isize" {
		suffix = "_s"
	}
	return "(i64.extend_i32" + suffix + " " + expr + ")"
}

// browserCanonicalI32Expr narrows small integer types at the public boundary.
// JavaScript has only the WebAssembly i32 carrier, so the declared Kizu width
// is what makes values such as 255 become either u8(255) or i8(-1).
func browserCanonicalI32Expr(typ string, expr string) string {
	switch typ {
	case "i8":
		return "(i32.shr_s (i32.shl " + expr + " (i32.const 24)) (i32.const 24))"
	case "i16":
		return "(i32.shr_s (i32.shl " + expr + " (i32.const 16)) (i32.const 16))"
	case "u8":
		return "(i32.and " + expr + " (i32.const 255))"
	case "u16":
		return "(i32.and " + expr + " (i32.const 65535))"
	default:
		return expr
	}
}

// writeBrowserExports emits a small stable-ABI wrapper for each explicit
// callback. The Kizu body keeps the compiler's internal representation; only
// this named host boundary narrows and extends integer values.
func (e *emitter) writeBrowserExports() error {
	if !e.target.isBrowser() {
		return nil
	}
	seen := map[string]bool{"memory": true, "kizu_start": true}
	exportIndex := 0
	for _, function := range e.module.Functions {
		if function.ExportABI != "browser" {
			continue
		}
		if seen[function.ExportName] {
			return fmt.Errorf("wasm error: duplicate browser export `%s`", function.ExportName)
		}
		seen[function.ExportName] = true
		if err := e.writeBrowserExport(function, exportIndex); err != nil {
			return err
		}
		exportIndex++
	}
	return nil
}

// writeBrowserExport writes one stable wrapper around an explicitly exported
// Kizu function.
func (e *emitter) writeBrowserExport(function *ir.Function, index int) error {
	params := make([]string, 0, len(function.Params))
	args := make([]string, 0, len(function.Params))
	for paramIndex, param := range function.Params {
		boundaryType := browserBoundaryType(param.Type)
		name := fmt.Sprintf("$__kizu_arg_%d", paramIndex)
		params = append(params, fmt.Sprintf("(param %s %s)", name, boundaryType))
		args = append(args, browserFromHostExpr(param.Type, "(local.get "+name+")"))
	}
	result := ""
	if function.Return != "void" {
		result = " (result " + browserBoundaryType(function.Return) + ")"
	}
	paramText := ""
	if len(params) > 0 {
		paramText = " " + strings.Join(params, " ")
	}
	fmt.Fprintf(&e.out, "  (func $__kizu_export_%d (export %q)%s%s\n",
		index, function.ExportName, paramText, result)
	call := "(call $" + function.Name
	if len(args) > 0 {
		call += " " + strings.Join(args, " ")
	}
	call += ")"
	if function.Return != "void" {
		call = browserToHostExpr(function.Return, call)
	}
	fmt.Fprintf(&e.out, "    %s\n", call)
	e.out.WriteString("  )\n\n")
	return nil
}
