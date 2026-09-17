package ir

import (
	"fmt"
	"strings"
)

// rotateLimit is how many instructions besides its phis a loop header may hold
// for RotateLoops to copy them to the end of each iteration.
const rotateLimit = 8

// RotateLoops moves the test of each `while` loop to the end of its body. A
// loop whose header tests and then branches into the body or out runs two
// branches an iteration, the test and the jump back to it; with a copy of the
// header's test at the end of the body, the header runs once as a guard and
// each iteration ends in the one branch that decides whether there is another.
// An engine that compiles a wasm function as it reads it, without rotating
// loops itself, runs the rotated shape as it is written.
//
// The header's instructions run as many times as before: once for the guard
// and once at the end of every iteration, in place of once at the top of
// every iteration and once more on the way out.
func RotateLoops(module *Module) {
	for _, fn := range module.Functions {
		rotateLoopsIn(fn)
	}
}

// rotateLoopsIn rotates every loop of fn that rotatableLoop accepts. Rotating
// one loop leaves every block's immediate dominator where it was -- the edges
// it adds leave blocks the loop body already dominates -- so one dominator
// tree serves the whole function, while predecessors are read again after
// each loop.
func rotateLoopsIn(fn *Function) {
	if len(fn.Blocks) == 0 {
		return
	}
	at := make(map[string]int, len(fn.Blocks))
	for position, block := range fn.Blocks {
		at[block.Name] = position
	}
	preds := blockPredecessors(fn, at)
	tree := buildDominatorTree(fn, preds)
	site := 0
	for position := range fn.Blocks {
		loop, ok := rotatableLoop(fn, at, preds, tree, position)
		if !ok {
			continue
		}
		site++
		rotateLoop(fn, tree, loop, fmt.Sprintf("rot%d.", site))
		preds = blockPredecessors(fn, at)
	}
}

// rotation names the blocks one loop rotation reads and rewrites, by their
// position in fn.Blocks.
type rotation struct {
	header  int
	body    int
	exit    int
	latches []int
	outside []int
}

// rotatableLoop reports the loop headed by the block at position when it can
// be rotated: the header branches to a body that only it enters and to an
// exit, every back edge is a jump from a block the body dominates, the loop is
// entered from outside, and the header holds at most rotateLimit instructions
// besides its phis, each of which only computes its result.
func rotatableLoop(
	fn *Function,
	at map[string]int,
	preds [][]int,
	tree dominatorTree,
	position int,
) (rotation, bool) {
	header := fn.Blocks[position]
	term := header.Terminator
	if position == 0 || term.Op != "branch" || term.Target == term.Else {
		return rotation{}, false
	}
	loop := rotation{header: position}
	for _, pred := range preds[position] {
		if !tree.dominates(position, pred) {
			loop.outside = append(loop.outside, pred)
			continue
		}
		if fn.Blocks[pred].Terminator.Op != "jump" {
			return rotation{}, false
		}
		loop.latches = append(loop.latches, pred)
	}
	if len(loop.latches) == 0 || len(loop.outside) == 0 || !loop.findBody(at, preds, tree, term) {
		return rotation{}, false
	}
	return loop, repeatableHeader(header)
}

// findBody picks, of the header's two arms, the body: the one only the header
// enters, dominating every latch. The other is the exit.
func (loop *rotation) findBody(
	at map[string]int,
	preds [][]int,
	tree dominatorTree,
	term Terminator,
) bool {
	target, okTarget := at[term.Target]
	other, okOther := at[term.Else]
	if !okTarget || !okOther {
		return false
	}
	switch {
	case enteredOnlyFrom(preds, target, loop.header) && dominatesAll(tree, target, loop.latches):
		loop.body, loop.exit = target, other
	case enteredOnlyFrom(preds, other, loop.header) && dominatesAll(tree, other, loop.latches):
		loop.body, loop.exit = other, target
	default:
		return false
	}
	return loop.body != loop.header && loop.exit != loop.header
}

// repeatableHeader reports whether a header holds at most rotateLimit
// instructions besides its phis, each of which can be copied to the latches.
func repeatableHeader(header *Block) bool {
	count := 0
	for _, instr := range header.Instrs {
		if instr.Op == "phi" {
			continue
		}
		count++
		if count > rotateLimit || !isRepeatableOp(instr.Op) || len(instr.Cleanups) > 0 {
			return false
		}
	}
	return true
}

// isRepeatableOp reports whether a header instruction can be copied to the
// latches: it only computes its result, and the result is no address of storage
// the instruction itself sets aside, which a copy would set aside again
// somewhere else.
func isRepeatableOp(op string) bool {
	return isPureOp(op) && op != "local.slot" && op != "buffer.new"
}

// enteredOnlyFrom reports whether the block at position has from as its one
// predecessor.
func enteredOnlyFrom(preds [][]int, position int, from int) bool {
	return len(preds[position]) == 1 && preds[position][0] == from
}

// dominatesAll reports whether the block at dominator dominates every block
// in blocks.
func dominatesAll(tree dominatorTree, dominator int, blocks []int) bool {
	for _, block := range blocks {
		if !tree.dominates(dominator, block) {
			return false
		}
	}
	return true
}

// rotator carries one rotation's renaming. A value the header defines is read
// after the rotation as one of three values: the header's own on the guard,
// a phi at the top of the body inside the loop, and a phi at the exit after
// it. Each latch computes the header's instructions again under names of its
// own.
type rotator struct {
	fn      *Function
	tree    dominatorTree
	loop    rotation
	prefix  string
	defined map[string]*Instr
	latch   []map[string]Value
	bodyPhi map[string]*Instr
	exitPhi map[string]*Instr
	added   []*Instr
	exitAdd []*Instr
}

// rotateLoop rewrites one loop rotatableLoop accepted.
func rotateLoop(fn *Function, tree dominatorTree, loop rotation, prefix string) {
	r := &rotator{
		fn:      fn,
		tree:    tree,
		loop:    loop,
		prefix:  prefix,
		defined: map[string]*Instr{},
		bodyPhi: map[string]*Instr{},
		exitPhi: map[string]*Instr{},
	}
	header := fn.Blocks[loop.header]
	for _, instr := range header.Instrs {
		r.defined[instr.Result.Name] = instr
	}
	r.latch = make([]map[string]Value, len(loop.latches))
	for index := range loop.latches {
		names := map[string]Value{}
		for _, instr := range header.Instrs {
			if instr.Op != "phi" {
				names[instr.Result.Name] = r.renamed(fmt.Sprintf("l%d.", index), instr.Result)
			}
		}
		r.latch[index] = names
	}
	r.renameUses()
	r.extendPhis(loop.body)
	r.extendPhis(loop.exit)
	for index, latch := range loop.latches {
		r.copyHeaderInto(index, fn.Blocks[latch])
	}
	r.fillExitPhis()
	r.fillBodyPhis()
	r.dropLatchIncoming()
	body := fn.Blocks[loop.body]
	body.Instrs = append(append([]*Instr{}, r.added...), body.Instrs...)
	exit := fn.Blocks[loop.exit]
	exit.Instrs = append(append([]*Instr{}, r.exitAdd...), exit.Instrs...)
	r.foldGuardPhis()
}

// renamed names a value the rotation defines under this rotation's prefix.
func (r *rotator) renamed(role string, value Value) Value {
	return Value{
		Name: "%" + r.prefix + role + strings.TrimPrefix(value.Name, "%"),
		Type: value.Type,
	}
}

// inBody returns the value a read inside the loop body takes for value.
func (r *rotator) inBody(value Value) Value {
	if _, ok := r.defined[value.Name]; !ok {
		return value
	}
	if phi, ok := r.bodyPhi[value.Name]; ok {
		return phi.Result
	}
	phi := &Instr{Result: r.renamed("b.", value), Op: "phi"}
	r.bodyPhi[value.Name] = phi
	r.added = append(r.added, phi)
	return phi.Result
}

// inExit returns the value a read the loop's exit dominates takes for value.
func (r *rotator) inExit(value Value) Value {
	if _, ok := r.defined[value.Name]; !ok {
		return value
	}
	if phi, ok := r.exitPhi[value.Name]; ok {
		return phi.Result
	}
	phi := &Instr{Result: r.renamed("x.", value), Op: "phi"}
	r.exitPhi[value.Name] = phi
	r.exitAdd = append(r.exitAdd, phi)
	return phi.Result
}

// atLatch returns the value the latch at index holds for value at its end,
// where the copied header runs.
func (r *rotator) atLatch(index int, value Value) Value {
	instr, ok := r.defined[value.Name]
	if !ok {
		return value
	}
	if instr.Op != "phi" {
		return r.latch[index][value.Name]
	}
	latch := r.fn.Blocks[r.loop.latches[index]].Name
	for _, incoming := range instr.Incoming {
		if incoming.Block == latch {
			return r.inBody(incoming.Value)
		}
	}
	return value
}

// region returns how a read at the end of the block at position sees a
// header value: through the body, through the exit, or as it stands.
func (r *rotator) region(position int) func(Value) Value {
	switch {
	case position == r.loop.header:
		return func(value Value) Value { return value }
	case r.tree.dominates(r.loop.body, position):
		return r.inBody
	case r.tree.dominates(r.loop.exit, position):
		return r.inExit
	default:
		return func(value Value) Value { return value }
	}
}

// renameUses points every read of a header value outside the header at the
// value its region takes. A phi's operand is read at the end of the block it
// arrives from, so that block's region decides.
func (r *rotator) renameUses() {
	at := make(map[string]int, len(r.fn.Blocks))
	for position, block := range r.fn.Blocks {
		at[block.Name] = position
	}
	for position, block := range r.fn.Blocks {
		if position == r.loop.header {
			continue
		}
		rename := r.region(position)
		for _, instr := range block.Instrs {
			if instr.Op == "phi" {
				for index, incoming := range instr.Incoming {
					if from, ok := at[incoming.Block]; ok {
						instr.Incoming[index].Value = r.region(from)(incoming.Value)
					}
				}
				continue
			}
			renameInstrReads(instr, rename)
		}
		block.Terminator.Value = rename(block.Terminator.Value)
		block.Terminator.Cond = rename(block.Terminator.Cond)
	}
}

// renameInstrReads applies rename to every value one instruction reads
// besides phi incoming.
func renameInstrReads(instr *Instr, rename func(Value) Value) {
	for index, arg := range instr.Args {
		instr.Args[index] = rename(arg)
	}
	for index, field := range instr.Fields {
		instr.Fields[index].Value = rename(field.Value)
	}
	for cleanupIndex, cleanup := range instr.Cleanups {
		for argIndex, arg := range cleanup.Args {
			instr.Cleanups[cleanupIndex].Args[argIndex] = rename(arg)
		}
	}
}

// extendPhis gives each phi already standing in the block at position an
// incoming for every latch, which now branches there: the value the phi took
// from the header, as that latch holds it.
func (r *rotator) extendPhis(position int) {
	header := r.fn.Blocks[r.loop.header].Name
	for _, instr := range r.fn.Blocks[position].Instrs {
		if instr.Op != "phi" {
			continue
		}
		var fromHeader *Value
		for index := range instr.Incoming {
			if instr.Incoming[index].Block == header {
				fromHeader = &instr.Incoming[index].Value
			}
		}
		if fromHeader == nil {
			continue
		}
		value := *fromHeader
		for index, latch := range r.loop.latches {
			instr.Incoming = append(instr.Incoming, Incoming{
				Block: r.fn.Blocks[latch].Name,
				Value: r.atLatch(index, value),
			})
		}
	}
}

// copyHeaderInto appends the header's instructions and branch to the latch at
// index, in place of its jump back to the header.
func (r *rotator) copyHeaderInto(index int, block *Block) {
	header := r.fn.Blocks[r.loop.header]
	rename := func(value Value) Value { return r.atLatch(index, value) }
	for _, instr := range header.Instrs {
		if instr.Op == "phi" {
			continue
		}
		copied := *instr
		copied.Result = r.latch[index][instr.Result.Name]
		copied.Args = append([]Value(nil), instr.Args...)
		copied.Fields = append([]FieldArg(nil), instr.Fields...)
		copied.CallParams = append([]Param(nil), instr.CallParams...)
		copied.Incoming = nil
		copied.Cleanups = nil
		renameInstrReads(&copied, rename)
		block.Instrs = append(block.Instrs, &copied)
	}
	block.Terminator = Terminator{
		Op:     "branch",
		Cond:   rename(header.Terminator.Cond),
		Target: header.Terminator.Target,
		Else:   header.Terminator.Else,
	}
}

// fillBodyPhis gives each body phi its incoming: the header's value from the
// guard and the latch's own from each latch. Filling one can ask for another
// body phi, which is filled in turn.
func (r *rotator) fillBodyPhis() {
	header := r.fn.Blocks[r.loop.header].Name
	for filled := 0; filled < len(r.added); filled++ {
		phi := r.added[filled]
		value := r.definedValue(phi, r.bodyPhi)
		phi.Incoming = []Incoming{{Block: header, Value: value}}
		for index, latch := range r.loop.latches {
			phi.Incoming = append(phi.Incoming, Incoming{
				Block: r.fn.Blocks[latch].Name,
				Value: r.atLatch(index, value),
			})
		}
	}
}

// fillExitPhis gives each exit phi one incoming per edge into the exit.
func (r *rotator) fillExitPhis() {
	exit := r.fn.Blocks[r.loop.exit].Name
	header := r.fn.Blocks[r.loop.header].Name
	latchIndex := map[string]int{}
	for index, latch := range r.loop.latches {
		latchIndex[r.fn.Blocks[latch].Name] = index
	}
	for _, phi := range r.exitAdd {
		value := r.definedValue(phi, r.exitPhi)
		for position, block := range r.fn.Blocks {
			for _, target := range block.Terminator.Successors() {
				if target != exit {
					continue
				}
				incoming := value
				if index, ok := latchIndex[block.Name]; ok {
					incoming = r.atLatch(index, value)
				} else if block.Name != header {
					incoming = r.region(position)(value)
				}
				phi.Incoming = append(phi.Incoming, Incoming{Block: block.Name, Value: incoming})
			}
		}
	}
}

// definedValue returns the header value a body or exit phi stands for.
func (r *rotator) definedValue(phi *Instr, phis map[string]*Instr) Value {
	for name, candidate := range phis {
		if candidate == phi {
			return r.defined[name].Result
		}
	}
	return Value{}
}

// dropLatchIncoming removes the incoming the header's phis took from the
// latches, which no longer jump to it.
func (r *rotator) dropLatchIncoming() {
	latches := map[string]bool{}
	for _, latch := range r.loop.latches {
		latches[r.fn.Blocks[latch].Name] = true
	}
	for _, instr := range r.fn.Blocks[r.loop.header].Instrs {
		if instr.Op != "phi" {
			continue
		}
		kept := instr.Incoming[:0]
		for _, incoming := range instr.Incoming {
			if !latches[incoming.Block] {
				kept = append(kept, incoming)
			}
		}
		instr.Incoming = kept
	}
}

// foldGuardPhis replaces the header's phis by their one incoming when the
// loop is entered from one block, which then dominates every read of them.
func (r *rotator) foldGuardPhis() {
	if len(r.loop.outside) != 1 {
		return
	}
	header := r.fn.Blocks[r.loop.header]
	replacements := map[string]Value{}
	kept := header.Instrs[:0]
	for _, instr := range header.Instrs {
		if instr.Op == "phi" && len(instr.Incoming) == 1 {
			replacements[instr.Result.Name] = instr.Incoming[0].Value
			continue
		}
		kept = append(kept, instr)
	}
	header.Instrs = kept
	if len(replacements) > 0 {
		replaceFunctionValues(r.fn, replacements)
	}
}
