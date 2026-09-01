package llvm

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// moduleHasRuntimeHeaderType reports whether an emitted aggregate definition
// or function value refers to the named hosted-runtime container. Reachability
// removes functions but deliberately retains declared structs, so both halves
// must participate in deciding which named LLVM header types to declare.
func (e *emitter) moduleHasRuntimeHeaderType(marker string) bool {
	for _, st := range e.module.Structs {
		for _, field := range st.Fields {
			if hasRuntimeHeaderType(field.Type, marker) {
				return true
			}
		}
	}
	for _, fn := range e.module.Functions {
		if functionHasRuntimeHeaderType(fn, marker) {
			return true
		}
	}
	return false
}

// functionHasRuntimeHeaderType reports whether a function body or signature
// carries one hosted-runtime container, directly or in an aggregate wrapper.
func functionHasRuntimeHeaderType(fn *ir.Function, marker string) bool {
	if hasRuntimeHeaderType(fn.Return, marker) {
		return true
	}
	for _, param := range fn.Params {
		if hasRuntimeHeaderType(param.Type, marker) {
			return true
		}
	}
	for _, block := range fn.Blocks {
		if blockHasRuntimeHeaderType(block, marker) {
			return true
		}
	}
	return false
}

// blockHasRuntimeHeaderType reports whether a block carries one hosted-runtime
// container.
func blockHasRuntimeHeaderType(block *ir.Block, marker string) bool {
	for _, instr := range block.Instrs {
		if instrHasRuntimeHeaderType(instr, marker) {
			return true
		}
	}
	return hasRuntimeHeaderType(block.Terminator.Value.Type, marker) ||
		hasRuntimeHeaderType(block.Terminator.Cond.Type, marker)
}

// instrHasRuntimeHeaderType reports whether one instruction carries a hosted-
// runtime container in any position lowering can read.
func instrHasRuntimeHeaderType(instr *ir.Instr, marker string) bool {
	if hasRuntimeHeaderType(instr.Result.Type, marker) {
		return true
	}
	for _, arg := range instr.Args {
		if hasRuntimeHeaderType(arg.Type, marker) {
			return true
		}
	}
	for _, field := range instr.Fields {
		if hasRuntimeHeaderType(field.Value.Type, marker) {
			return true
		}
	}
	for _, incoming := range instr.Incoming {
		if hasRuntimeHeaderType(incoming.Value.Type, marker) {
			return true
		}
	}
	for _, cleanup := range instr.Cleanups {
		for _, arg := range cleanup.Args {
			if hasRuntimeHeaderType(arg.Type, marker) {
				return true
			}
		}
	}
	return false
}

// hasRuntimeHeaderType reports whether a lowered IR type embeds the named
// hosted-runtime container.
func hasRuntimeHeaderType(name string, marker string) bool {
	return strings.Contains(name, marker)
}
