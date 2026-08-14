package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// auditedPositions names every declared slot the audit checks a value against.
// A pass means something only if the corpus reached each of them.
var auditedPositions = []string{
	"call argument", "struct literal field", "union payload",
	"error union success", "return value",
}

// TestLoweredValuesHaveTheirDeclaredTypes keeps the two readers of a Kizu type
// in agreement. The checker resolves what type a value has where it is written
// -- an integer literal takes the type its position fixes (ADR-0065), `Self` is
// the type its impl block is written for -- and lowering has to reach the same
// answer from the same declarations. Nothing forces it to. A position lowering
// forgets leaves a value of one type in a slot declared as another, and the
// program still runs whenever the backend writes the declared type over what it
// was handed, so the IR stays wrong until some other backend reads it.
//
// The positions checked are the ones in auditedPositions. New call sites in
// those positions are covered without being listed; a new kind of position is
// not, and adding one here is what closing the next gap looks like.
func TestLoweredValuesHaveTheirDeclaredTypes(t *testing.T) {
	audit := &typeAudit{inspected: map[string]int{}}
	for _, path := range kizuSourcePaths(t, "../../examples") {
		// lowerFile is the pipeline `kizu run` uses. It fails on the negative
		// examples, whose types were never the checker's answer to begin with.
		module, err := lowerFile(path, false)
		if err != nil {
			continue
		}
		audit.checkModule(module, path)
	}
	for _, mismatch := range audit.found {
		t.Error(mismatch)
	}
	// An op this audit names by string can be renamed in the lowerer without
	// breaking the build, which would leave a check matching nothing and passing.
	for _, position := range auditedPositions {
		if audit.inspected[position] == 0 {
			t.Errorf("no %s was audited: the op it matches may have been renamed", position)
		}
	}
}

// typeAudit collects the positions where a value and the declaration it flows
// into disagree, and counts what it looked at.
type typeAudit struct {
	module    *ir.Module
	params    map[string][]ir.Value
	found     []string
	inspected map[string]int
}

// checkModule audits every declared slot one lowered module fills.
func (a *typeAudit) checkModule(module *ir.Module, path string) {
	a.module = module
	a.params = map[string][]ir.Value{}
	for _, fn := range module.Functions {
		a.params[fn.Name] = fn.Params
	}
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				a.checkInstr(path, fn, instr)
			}
			a.checkReturn(path, fn, block)
		}
	}
}

// report records that one position was audited, and how it disagreed if it did.
// Agreement is the common case and says nothing, so every check calls this.
func (a *typeAudit) report(position string, where string, want string, got string) {
	a.inspected[position]++
	if want != got {
		a.found = append(a.found, where+" is "+got+", declared "+want)
	}
}

// checkInstr audits the declared slot an instruction fills, if it fills one.
func (a *typeAudit) checkInstr(path string, fn *ir.Function, instr *ir.Instr) {
	switch {
	case strings.HasPrefix(instr.Op, "call."):
		a.checkCall(path, fn, instr)
	case instr.Op == "struct.new":
		a.checkStructLiteral(path, fn, instr)
	case instr.Op == "union.new":
		a.checkUnionPayload(path, fn, instr)
	case instr.Op == "error.ok":
		a.checkSuccessWrap(path, fn, instr)
	}
}

// checkCall audits what a call hands to the function it names.
func (a *typeAudit) checkCall(path string, fn *ir.Function, instr *ir.Instr) {
	callee := strings.TrimPrefix(instr.Op, "call.")
	declared, known := a.params[callee]
	// A call to a symbol this module does not define, or with an arity the
	// lowerer built for an instruction rather than a function, is not this
	// test's question.
	if !known || len(declared) != len(instr.Args) {
		return
	}
	for index, param := range declared {
		got := instr.Args[index].Type
		// A callee that borrows its argument declares `&T` and is handed the T
		// whose address the call takes. The IR does not name that address, so
		// the two spellings differ here and the LLVM emitter re-derives the
		// borrow from them.
		if param.Type == "&"+got {
			continue
		}
		a.report("call argument",
			path+": "+fn.Name+" calls "+callee+" arg "+strconv.Itoa(index),
			param.Type, got)
	}
}

// checkStructLiteral audits what a struct literal puts in each declared field.
func (a *typeAudit) checkStructLiteral(path string, fn *ir.Function, instr *ir.Instr) {
	for _, field := range instr.Fields {
		for _, declared := range a.module.Structs[instr.Result.Type].Fields {
			if declared.Name != field.Name {
				continue
			}
			a.report("struct literal field",
				path+": "+fn.Name+" builds "+instr.Result.Type+"."+field.Name,
				declared.Type, field.Value.Type)
		}
	}
}

// checkUnionPayload audits what a union constructor carries in its variant.
func (a *typeAudit) checkUnionPayload(path string, fn *ir.Function, instr *ir.Instr) {
	variant, known := a.module.Unions[instr.Result.Type].Variants[instr.Immediate]
	if !known || variant.Payload == "" || len(instr.Args) != 1 {
		return
	}
	a.report("union payload",
		path+": "+fn.Name+" builds "+instr.Result.Type+"::"+variant.Name,
		variant.Payload, instr.Args[0].Type)
}

// checkSuccessWrap audits the payload an error union's success carries.
func (a *typeAudit) checkSuccessWrap(path string, fn *ir.Function, instr *ir.Instr) {
	_, success, isUnion := typ.ErrorUnionParts(instr.Result.Type)
	if !isUnion || len(instr.Args) != 1 {
		return
	}
	a.report("error union success", path+": "+fn.Name+" wraps a success",
		success, instr.Args[0].Type)
}

// checkReturn audits what a block returns against what its function declares.
func (a *typeAudit) checkReturn(path string, fn *ir.Function, block *ir.Block) {
	got := block.Terminator.Value.Type
	if block.Terminator.Op != "return" || got == "" || absorbsErrorSet(fn.Return, got) {
		return
	}
	a.report("return value", path+": "+fn.Name+" returns", fn.Return, got)
}

// absorbsErrorSet reports whether returning a value of type got from a function
// declaring want is the absorption `try` does, written as a return. `!T` infers
// its error set (ADR-0086), so an `E!T` returned from it arrives as `!T` with E
// absorbed -- the rule the LLVM emitter applies as absorbsErrorUnionReturn.
func absorbsErrorSet(want string, got string) bool {
	wantSet, wantSuccess, isUnion := typ.ErrorUnionParts(want)
	if !isUnion || wantSet != "" {
		return false
	}
	gotSet, gotSuccess, isUnion := typ.ErrorUnionParts(got)
	return isUnion && gotSet != "" && gotSuccess == wantSuccess
}
