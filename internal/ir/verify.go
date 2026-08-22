package ir

import (
	"errors"
	"fmt"

	"github.com/kizu-lang/kizu/internal/typ"
)

// ErrVerify marks every failure Verify reports, so a caller lowering a corpus
// can tell a malformed module apart from a source that was never meant to
// lower.
var ErrVerify = errors.New("ir verify error")

// Verify reports the first way a module is not well formed.
//
// The rules below are what every backend assumes and none of them checks in
// full: `internal/llvm` writes a function's declared return type over whatever
// value it was handed, and `internal/wasm` does not compare return types at
// all. Ill-typed IR therefore runs correctly on one backend and is found only
// by another, or by a downstream tool -- the phi dominance bug behind
// TestLowerBlockScopedLetDominatesItsMerge shipped because Apple's clang does
// not run LLVM's own verifier, and surfaced only on a toolchain that does.
//
// Checking here instead means the producer is told, in terms of the IR it
// built, rather than each backend rejecting the subset it happens to read.
func Verify(module *Module) error {
	return verifyWithTypes(module, typ.NewTable())
}

// verifyWithTypes verifies module using the type table that produced it.
func verifyWithTypes(module *Module, types *typ.Table) error {
	v := &verifier{module: module, types: types, params: map[string][]Param{}}
	for _, fn := range module.Functions {
		v.params[fn.Name] = fn.Params
	}
	for _, fn := range module.Functions {
		if err := v.function(fn); err != nil {
			return err
		}
	}
	return nil
}

// verifier holds the module a rule needs and the position it is reading, so a
// rule takes only what it checks and reports through one message.
type verifier struct {
	module *Module
	types  *typ.Table
	params map[string][]Param
	fn     *Function
	block  *Block
	blocks map[string]bool
}

// function checks one function's blocks and the SSA property across them.
func (v *verifier) function(fn *Function) error {
	v.fn = fn
	v.blocks = make(map[string]bool, len(fn.Blocks))
	for _, block := range fn.Blocks {
		v.blocks[block.Name] = true
	}
	for _, block := range fn.Blocks {
		v.block = block
		for _, instr := range block.Instrs {
			if err := v.instr(instr); err != nil {
				return err
			}
		}
		if err := v.terminator(); err != nil {
			return err
		}
	}
	preds := blockPredecessors(fn)
	if err := v.phiIncomingMatchesPredecessors(preds); err != nil {
		return err
	}
	if err := v.phiIncomingDominates(preds); err != nil {
		return err
	}
	return v.valuesDominateUses(preds)
}

// instr checks the declared slot an instruction fills, if it fills one.
func (v *verifier) instr(instr *Instr) error {
	switch instr.Op {
	case "struct.new":
		return v.structLiteral(instr)
	case "union.new":
		return v.unionPayload(instr)
	case "error.ok":
		return v.successWrap(instr)
	}
	if callee, ok := directCallee(instr.Op); ok {
		return v.call(instr, callee)
	}
	return nil
}

// call checks what a call hands to the function it names.
func (v *verifier) call(instr *Instr, callee string) error {
	declared, known := v.params[callee]
	// A call to a symbol this module does not define, or with an arity the
	// lowerer built for an instruction rather than a function, has no
	// declaration here to check against.
	if !known || len(declared) != len(instr.Args) {
		return nil
	}
	for index, param := range declared {
		got := instr.Args[index].Type
		if !argumentFits(param, got) {
			return v.fail(fmt.Sprintf("call %s argument %d", callee, index), param.Type, got)
		}
	}
	return nil
}

// argumentFits reports whether a value of type got can fill param. It is the
// one slot not always filled by its own type: a parameter read through an
// address also accepts the value that address is taken of, which is what
// Param.TakesAddressOf names.
func argumentFits(param Param, got string) bool {
	return param.Type == got || param.TakesAddressOf(got)
}

// structLiteral checks what a struct literal puts in each declared field.
func (v *verifier) structLiteral(instr *Instr) error {
	for _, field := range instr.Fields {
		for _, declared := range v.module.Structs[instr.Result.Type].Fields {
			if declared.Name != field.Name {
				continue
			}
			if declared.Type != field.Value.Type {
				return v.fail(instr.Result.Type+"."+field.Name, declared.Type, field.Value.Type)
			}
		}
	}
	return nil
}

// unionPayload checks what a union constructor carries in its variant.
func (v *verifier) unionPayload(instr *Instr) error {
	variant, known := v.module.Unions[instr.Result.Type].Variants[instr.Immediate]
	if !known || variant.Payload == "" || len(instr.Args) != 1 {
		return nil
	}
	if variant.Payload != instr.Args[0].Type {
		return v.fail(instr.Result.Type+"::"+variant.Name+" payload",
			variant.Payload, instr.Args[0].Type)
	}
	return nil
}

// successWrap checks the payload an error union's success carries.
func (v *verifier) successWrap(instr *Instr) error {
	_, success, isUnion := errorUnionParts(v.types, instr.Result.Type)
	if !isUnion || len(instr.Args) != 1 {
		return nil
	}
	if success != instr.Args[0].Type {
		return v.fail("success wrapped as "+instr.Result.Type, success, instr.Args[0].Type)
	}
	return nil
}

// terminator checks that a block ends, that it names blocks that exist, and
// that what it returns is what its function declared.
func (v *verifier) terminator() error {
	term := v.block.Terminator
	if term.Op == "" {
		return fmt.Errorf("%w: %s: block %s has no terminator",
			ErrVerify, v.fn.Name, v.block.Name)
	}
	for _, target := range term.Successors() {
		if !v.blocks[target] {
			return fmt.Errorf("%w: %s: block %s branches to %s, which does not exist",
				ErrVerify, v.fn.Name, v.block.Name, target)
		}
	}
	got := term.Value.Type
	if term.Op != "return" || got == "" || absorbsErrorSet(v.types, v.fn.Return, got) {
		return nil
	}
	if v.fn.Return != got {
		return v.fail("return value", v.fn.Return, got)
	}
	return nil
}

// absorbsErrorSet parses the IR spellings before asking the structural type query.
func absorbsErrorSet(types *typ.Table, want string, got string) bool {
	wantType, err := types.Parse(want)
	if err != nil {
		return false
	}
	gotType, err := types.Parse(got)
	return err == nil && typ.AbsorbsErrorSet(wantType, gotType)
}

// fail names one position that holds a value of a type its declaration did not
// ask for. Every type rule reports through here so the message reads the same.
func (v *verifier) fail(position string, want string, got string) error {
	return fmt.Errorf("%w: %s: %s in block %s is %s, declared %s",
		ErrVerify, v.fn.Name, position, v.block.Name, got, want)
}

// phiIncomingMatchesPredecessors checks the other half of what LLVM's verifier
// asks of a phi: one incoming for each block that jumps to the phi's block, and
// none from a block that does not. An incoming from a block that never reaches
// here reads a value on an edge that does not exist, and a predecessor with no
// incoming leaves the phi undefined on an edge that does.
//
// An edge out of an unreachable block is still an edge, so it is still counted.
// phiIncomingDominates exempts unreachable blocks, which is not a disagreement:
// that rule asks which paths reach a block, and an unreachable block is on
// none, while this one asks which blocks name it as a target.
func (v *verifier) phiIncomingMatchesPredecessors(preds map[string][]string) error {
	for _, block := range v.fn.Blocks {
		arrives := namesIn(preds[block.Name])
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				continue
			}
			if err := v.phiEdges(block, instr, preds[block.Name], arrives); err != nil {
				return err
			}
		}
	}
	return nil
}

// phiEdges checks one phi against the blocks that jump to its own. Counting
// after the two membership passes is what makes it edges rather than names: a
// branch whose arms share a target reaches the block twice and needs two
// incoming, and a phi naming one block twice covers one edge, not two.
func (v *verifier) phiEdges(
	block *Block,
	instr *Instr,
	preds []string,
	arrives map[string]bool,
) error {
	named := make(map[string]bool, len(instr.Incoming))
	for _, incoming := range instr.Incoming {
		if !arrives[incoming.Block] {
			return fmt.Errorf(
				"%w: %s: phi %s in block %s takes a value from %s, "+
					"which does not jump to %s",
				ErrVerify, v.fn.Name, instr.Result.Name, block.Name,
				incoming.Block, block.Name)
		}
		named[incoming.Block] = true
	}
	for _, name := range preds {
		if named[name] {
			continue
		}
		return fmt.Errorf(
			"%w: %s: phi %s in block %s has no value for %s, "+
				"which jumps to %s",
			ErrVerify, v.fn.Name, instr.Result.Name, block.Name,
			name, block.Name)
	}
	if len(instr.Incoming) != len(preds) {
		return fmt.Errorf(
			"%w: %s: phi %s in block %s has %d incoming for %d edges into %s",
			ErrVerify, v.fn.Name, instr.Result.Name, block.Name,
			len(instr.Incoming), len(preds), block.Name)
	}
	return nil
}

// namesIn returns the set of names in a list.
func namesIn(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// phiIncomingDominates checks the SSA property LLVM's verifier enforces: a
// phi's incoming value must be defined in a block that dominates the
// predecessor it arrives from. A value that is not yet defined on that edge is
// read as whatever the register held.
func (v *verifier) phiIncomingDominates(preds map[string][]string) error {
	if len(v.fn.Blocks) == 0 {
		return nil
	}
	definedIn := valueDefiningBlocks(v.fn)
	tree := buildDominatorTree(v.fn, preds)
	for _, block := range v.fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				continue
			}
			for _, incoming := range instr.Incoming {
				definition, ok := definedIn[incoming.Value.Name]
				if !ok || tree.dominates(definition, incoming.Block) {
					continue
				}
				return fmt.Errorf(
					"%w: %s: phi %s in block %s takes %s from %s, but %s is "+
						"defined in %s, which does not dominate %s",
					ErrVerify, v.fn.Name, instr.Result.Name, block.Name,
					incoming.Value.Name, incoming.Block, incoming.Value.Name,
					definition, incoming.Block,
				)
			}
		}
	}
	return nil
}

// valuesDominateUses checks for every operand what phiIncomingDominates checks
// for a phi edge: a value is read only where its definition is on every path
// in, and within one block only after it.
//
// A phi is the exception this rule leaves alone. Its operands arrive on edges
// rather than where it stands, so a value defined in the block a back edge
// leaves from is correct there and would look backwards here.
func (v *verifier) valuesDominateUses(preds map[string][]string) error {
	if len(v.fn.Blocks) == 0 {
		return nil
	}
	definedIn := valueDefiningBlocks(v.fn)
	tree := buildDominatorTree(v.fn, preds)
	for _, block := range v.fn.Blocks {
		above := map[string]bool{}
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				for _, operand := range instrOperands(instr) {
					if err := v.dominatesUse(tree, definedIn, above, block, operand); err != nil {
						return err
					}
				}
			}
			if instr.Result.Name != "" {
				above[instr.Result.Name] = true
			}
		}
		for _, operand := range []Value{block.Terminator.Cond, block.Terminator.Value} {
			if err := v.dominatesUse(tree, definedIn, above, block, operand); err != nil {
				return err
			}
		}
	}
	return nil
}

// dominatesUse checks one operand read in block. above holds what the block
// has defined so far, which is what separates a read of an earlier instruction
// from a read of a later one.
func (v *verifier) dominatesUse(
	tree dominatorTree,
	definedIn map[string]string,
	above map[string]bool,
	block *Block,
	operand Value,
) error {
	definition, defined := definedIn[operand.Name]
	if !defined {
		// A param or a literal has no defining block and is read anywhere.
		return nil
	}
	if definition == block.Name {
		if above[operand.Name] {
			return nil
		}
		return fmt.Errorf("%w: %s: block %s reads %s before it is defined",
			ErrVerify, v.fn.Name, block.Name, operand.Name)
	}
	if tree.dominates(definition, block.Name) {
		return nil
	}
	return fmt.Errorf(
		"%w: %s: block %s reads %s, which is defined in %s, and %s does not "+
			"dominate %s",
		ErrVerify, v.fn.Name, block.Name, operand.Name, definition, definition,
		block.Name)
}

// instrOperands returns the values an instruction reads where it stands.
func instrOperands(instr *Instr) []Value {
	operands := make([]Value, 0, len(instr.Args)+len(instr.Fields))
	operands = append(operands, instr.Args...)
	for _, field := range instr.Fields {
		operands = append(operands, field.Value)
	}
	for _, cleanup := range instr.Cleanups {
		operands = append(operands, cleanup.Args...)
	}
	return operands
}

// valueDefiningBlocks maps each instruction result to the block defining it.
// Params and literals have no defining block and are available everywhere.
func valueDefiningBlocks(fn *Function) map[string]string {
	defined := map[string]string{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Result.Name == "" {
				continue
			}
			defined[instr.Result.Name] = block.Name
		}
	}
	return defined
}

// dominatorTree answers which blocks dominate which. It stores one immediate
// dominator per block rather than a set per block, so the whole tree is two
// slices and a question is a walk up the chain.
type dominatorTree struct {
	// order holds the blocks in reverse postorder, which is the numbering the
	// walk compares against. A block missing from index is unreachable.
	index map[string]int
	// idom[i] is the reverse-postorder position of block i's immediate
	// dominator. The entry is its own.
	idom []int
}

// buildDominatorTree computes immediate dominators by the Cooper-Harvey-Kennedy
// iteration: walk the blocks in reverse postorder, and take each block's
// immediate dominator to be the meet of the ones already known for its
// predecessors. Storing sets instead costs a map per block and the entry count
// grows with the square of the block count, which a function of a few dozen
// blocks already feels.
func buildDominatorTree(fn *Function, preds map[string][]string) dominatorTree {
	order := reversePostorder(fn)
	tree := dominatorTree{index: make(map[string]int, len(order)), idom: make([]int, len(order))}
	for position, name := range order {
		tree.index[name] = position
		tree.idom[position] = -1
	}
	if len(order) == 0 {
		return tree
	}
	tree.idom[0] = 0
	for changed := true; changed; {
		changed = false
		for position := 1; position < len(order); position++ {
			meet := -1
			for _, pred := range preds[order[position]] {
				at, known := tree.index[pred]
				if !known || tree.idom[at] < 0 {
					continue
				}
				if meet < 0 {
					meet = at
					continue
				}
				meet = tree.meet(meet, at)
			}
			if meet >= 0 && tree.idom[position] != meet {
				tree.idom[position] = meet
				changed = true
			}
		}
	}
	return tree
}

// meet walks two blocks up the tree until they arrive at the same one, which is
// the nearest block dominating both. A block's immediate dominator always comes
// earlier in reverse postorder, so each step moves the higher position down and
// the walk ends.
func (d dominatorTree) meet(left int, right int) int {
	for left != right {
		for left > right {
			left = d.idom[left]
		}
		for right > left {
			right = d.idom[right]
		}
	}
	return left
}

// dominates reports whether every path reaching block passes through dominator.
// An unreachable block is reached by no path, so nothing is claimed about it.
func (d dominatorTree) dominates(dominator string, block string) bool {
	at, reachable := d.index[block]
	if !reachable {
		return true
	}
	target, known := d.index[dominator]
	if !known {
		return false
	}
	for at != target {
		next := d.idom[at]
		if next == at {
			return false
		}
		at = next
	}
	return true
}

// reversePostorder lists the blocks reachable from the entry, each after every
// block it can reach, then reversed. Predecessors therefore come first except
// across a back edge, which is what makes one forward pass enough for most of
// the dominator tree.
func reversePostorder(fn *Function) []string {
	blocks := make(map[string]*Block, len(fn.Blocks))
	for _, block := range fn.Blocks {
		blocks[block.Name] = block
	}
	visited := make(map[string]bool, len(fn.Blocks))
	order := make([]string, 0, len(fn.Blocks))
	var visit func(name string)
	visit = func(name string) {
		block, exists := blocks[name]
		if !exists || visited[name] {
			return
		}
		visited[name] = true
		for _, target := range block.Terminator.Successors() {
			visit(target)
		}
		order = append(order, name)
	}
	visit(fn.Blocks[0].Name)
	for head, tail := 0, len(order)-1; head < tail; head, tail = head+1, tail-1 {
		order[head], order[tail] = order[tail], order[head]
	}
	return order
}

// blockPredecessors derives the CFG edges from each block's terminator.
func blockPredecessors(fn *Function) map[string][]string {
	preds := map[string][]string{}
	for _, block := range fn.Blocks {
		for _, target := range block.Terminator.Successors() {
			preds[target] = append(preds[target], block.Name)
		}
	}
	return preds
}
