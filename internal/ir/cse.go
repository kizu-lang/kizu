package ir

import "strings"

// EliminateCommonSubexpressions replaces an instruction with an earlier one
// that computes the same result from the same values: an arithmetic,
// comparison, cast or bitwise operation, a field of a struct value, a
// constant, or the tag or payload of a union value. None of these reads
// memory, and an SSA value does not change, so an earlier result is the same
// wherever it dominates.
//
// Two reads of one field become one value, which is what lets a later pass see
// that `set(reg)` indexes where `get(reg)` just checked.
func EliminateCommonSubexpressions(module *Module) {
	for _, fn := range module.Functions {
		eliminateCommonSubexpressionsIn(fn)
	}
}

// eliminateCommonSubexpressionsIn walks one function's dominator tree with
// the results each block's dominators computed.
func eliminateCommonSubexpressionsIn(fn *Function) {
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
	children := make([][]int, len(fn.Blocks))
	for index := 1; index < len(order); index++ {
		parent := order[tree.idom[index]]
		children[parent] = append(children[parent], order[index])
	}
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
	available := map[string]Value{}
	var visit func(position int)
	visit = func(position int) {
		added := []string{}
		for _, instr := range fn.Blocks[position].Instrs {
			renameInstrReads(instr, resolve)
			key, ok := expressionKey(instr)
			if !ok {
				continue
			}
			if earlier, seen := available[key]; seen {
				replacements[instr.Result.Name] = earlier
				continue
			}
			available[key] = instr.Result
			added = append(added, key)
		}
		for _, child := range children[position] {
			visit(child)
		}
		for _, key := range added {
			delete(available, key)
		}
	}
	visit(order[0])
	if len(replacements) > 0 {
		replaceFunctionValues(fn, replacements)
	}
}

// computesFromOperands reports whether an instruction's result follows from
// its operands alone. Bytes compared through a view are read where the view
// points, and what is there can change between two comparisons.
func computesFromOperands(instr *Instr) bool {
	op := instr.Op
	switch {
	case op == "const" || op == "cast" || op == "union.tag" || op == "union.payload":
		return true
	case strings.HasPrefix(op, "binary."):
		for _, arg := range instr.Args {
			if strings.HasPrefix(arg.Type, "[]") {
				return false
			}
		}
		return true
	case strings.HasPrefix(op, "unary."):
		return true
	}
	return strings.HasPrefix(op, "field.") && !strings.HasPrefix(op, "field.ref") &&
		!strings.HasPrefix(op, "field.addr.") && !strings.HasPrefix(op, "field.set.")
}

// expressionKey spells what an instruction computes, when it computes it from
// its operands alone.
func expressionKey(instr *Instr) (string, bool) {
	if instr.Result.Name == "" || instr.Result.Type == "void" || len(instr.Cleanups) > 0 ||
		!computesFromOperands(instr) {
		return "", false
	}
	var key strings.Builder
	key.WriteString(instr.Op)
	key.WriteByte('|')
	key.WriteString(instr.Immediate)
	key.WriteByte('|')
	key.WriteString(instr.Result.Type)
	for _, arg := range instr.Args {
		key.WriteByte('|')
		key.WriteString(arg.Name)
		key.WriteByte(':')
		key.WriteString(arg.Type)
	}
	return key.String(), true
}
