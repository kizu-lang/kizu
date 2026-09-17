package ir

import "strings"

// forwardVisitLimit is how many blocks ForwardFieldLoads walks back from a
// merge block to find what the paths into it may have written. A merge whose
// paths reach further starts with nothing known.
const forwardVisitLimit = 64

// ForwardFieldLoads replaces a read of a struct field through a reference with
// the value already known to be there: the value an earlier write through the
// same reference stored, or what an earlier read of it returned, when nothing
// between them could have changed the field. A backend that writes a field
// and reads it back goes through memory twice, and an engine that compiles
// wasm keeps both, since it cannot tell a store to an element from a store to
// the field.
//
// A write to a field forgets what is known of that field through every
// reference to a struct of the same type, since two references can reach one
// struct; a store through a reference, a call, and any other instruction that
// is not pure forget everything. Knowledge flows down the dominator tree: a
// block starts with what its immediate dominator ended with, less what any
// path from there to it may have written.
func ForwardFieldLoads(module *Module) {
	for _, fn := range module.Functions {
		forwardFieldLoadsIn(fn)
	}
}

// fieldKey names one field of the struct one reference reaches.
type fieldKey struct {
	ref    string
	strct  string
	field  string
	scalar bool
}

// fieldFacts maps a field reachable through a reference to the value it is
// known to hold.
type fieldFacts map[fieldKey]Value

// forwardFieldLoadsIn rewrites the field reads of one function.
func forwardFieldLoadsIn(fn *Function) {
	if len(fn.Blocks) == 0 {
		return
	}
	at := make(map[string]int, len(fn.Blocks))
	for position, block := range fn.Blocks {
		at[block.Name] = position
	}
	preds := blockPredecessors(fn, at)
	tree := buildDominatorTree(fn, preds)
	order := reversePostorder(fn)
	ends := make([]fieldFacts, len(fn.Blocks))
	replacements := map[string]Value{}
	resolve := func(value Value) Value {
		for {
			next, ok := replacements[value.Name]
			if !ok {
				return value
			}
			value = next
		}
	}
	for index, position := range order {
		facts := fieldFacts{}
		if index > 0 {
			idom := order[tree.idom[index]]
			facts = inheritedFacts(fn, preds, position, idom, ends[idom])
		}
		for _, instr := range fn.Blocks[position].Instrs {
			forwardInstr(instr, facts, replacements, resolve)
		}
		ends[position] = facts
	}
	if len(replacements) > 0 {
		replaceFunctionValues(fn, replacements)
	}
}

// inheritedFacts is what a block starts with: its immediate dominator's end,
// less what the blocks on the paths from there may have written. A block the
// dominator jumps to directly inherits everything.
func inheritedFacts(
	fn *Function,
	preds [][]int,
	position int,
	idom int,
	from fieldFacts,
) fieldFacts {
	facts := fieldFacts{}
	if len(from) == 0 {
		return facts
	}
	for key, value := range from {
		facts[key] = value
	}
	if len(preds[position]) == 1 && preds[position][0] == idom {
		return facts
	}
	visited := map[int]bool{}
	stack := append([]int{}, preds[position]...)
	for len(stack) > 0 {
		block := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if block == idom || visited[block] {
			continue
		}
		visited[block] = true
		if len(visited) > forwardVisitLimit {
			return fieldFacts{}
		}
		for _, instr := range fn.Blocks[block].Instrs {
			forgetWrites(instr, facts)
			if len(facts) == 0 {
				return facts
			}
		}
		if block != position {
			stack = append(stack, preds[block]...)
		}
	}
	return facts
}

// forwardInstr applies one instruction to the facts: a read either finds its
// value known and is replaced, or becomes known; a write makes its value
// known after forgetting what it may have changed.
func forwardInstr(
	instr *Instr,
	facts fieldFacts,
	replacements map[string]Value,
	resolve func(Value) Value,
) {
	if field, ok := strings.CutPrefix(instr.Op, "field.ref.set."); ok && len(instr.Args) == 2 {
		forgetWrites(instr, facts)
		key := fieldKey{
			ref:    instr.Args[0].Name,
			strct:  derefType(instr.Args[0].Type),
			field:  field,
			scalar: isForwardScalar(instr.Args[1].Type),
		}
		if key.scalar {
			facts[key] = resolve(instr.Args[1])
		}
		return
	}
	if field, ok := strings.CutPrefix(instr.Op, "field.ref."); ok && len(instr.Args) == 1 {
		key := fieldKey{
			ref:    instr.Args[0].Name,
			strct:  derefType(instr.Args[0].Type),
			field:  field,
			scalar: isForwardScalar(instr.Result.Type),
		}
		if !key.scalar {
			return
		}
		if known, ok := facts[key]; ok && known.Type == instr.Result.Type {
			replacements[instr.Result.Name] = known
			return
		}
		facts[key] = instr.Result
		return
	}
	forgetWrites(instr, facts)
}

// forgetWrites removes from facts what instr may change. A field write
// forgets that field of every struct of its type, and of every struct at all
// when the field holds more than a scalar, since a struct stored whole can
// reach others; any other instruction that is not pure forgets everything.
func forgetWrites(instr *Instr, facts fieldFacts) {
	if field, ok := strings.CutPrefix(instr.Op, "field.ref.set."); ok && len(instr.Args) == 2 {
		if !isForwardScalar(instr.Args[1].Type) {
			clear(facts)
			return
		}
		strct := derefType(instr.Args[0].Type)
		for key := range facts {
			if key.strct == strct && key.field == field {
				delete(facts, key)
			}
		}
		return
	}
	if !isPureOp(instr.Op) {
		clear(facts)
	}
}

// isForwardScalar reports whether a field of typ is one value a later read
// can be replaced with.
func isForwardScalar(typ string) bool {
	return typ == "bool" || typ == "f32" || typ == "f64" || isWrappingInteger(typ)
}
