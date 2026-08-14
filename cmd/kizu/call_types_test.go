package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
)

// TestLoweredCallsHandOverDeclaredTypes keeps the two readers of a call in
// agreement about what it hands over. The checker narrows an integer literal to
// the type the position it is written in fixes (ADR-0065), so `take_byte(9)`
// passes a u8. Lowering has to reach the same answer from the same declarations,
// and nothing forces it to: a call site that forgets leaves the IR carrying an
// i64 into a u8 parameter, which every backend is then free to read differently.
// This walks every example and asks the module itself, so a call site added
// later is covered without being listed here.
func TestLoweredCallsHandOverDeclaredTypes(t *testing.T) {
	mismatches := []string{}
	for _, path := range kizuSourcePaths(t, "../../examples") {
		program, errs, err := parsePathWithStd(path)
		if err != nil || len(errs) > 0 {
			continue
		}
		// A negative example is a program the checker rejects, so the types it
		// lowers with were never the checker's answer to begin with.
		if err := checkProgram(program); err != nil {
			continue
		}
		module, err := ir.Lower(program)
		if err != nil {
			continue
		}
		mismatches = append(mismatches, callArgumentMismatches(module, path)...)
	}
	for _, mismatch := range mismatches {
		t.Error(mismatch)
	}
}

// callArgumentMismatches reports every call in one module whose argument does
// not have the type the called function declares for it.
func callArgumentMismatches(module *ir.Module, path string) []string {
	params := map[string][]ir.Value{}
	for _, fn := range module.Functions {
		params[fn.Name] = fn.Params
	}
	found := []string{}
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				callee, ok := strings.CutPrefix(instr.Op, "call.")
				if !ok {
					continue
				}
				declared, known := params[callee]
				// A call to a symbol this module does not define, or with an
				// arity the lowerer built for an instruction rather than a
				// function, is not this test's question.
				if !known || len(declared) != len(instr.Args) {
					continue
				}
				for index, param := range declared {
					got := instr.Args[index].Type
					if param.Type == got || param.Type == "&"+got {
						continue
					}
					found = append(found, path+": "+fn.Name+" calls "+callee+
						" arg "+strconv.Itoa(index)+" as "+got+", declared "+param.Type)
				}
			}
		}
	}
	return found
}
