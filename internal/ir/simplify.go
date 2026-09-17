package ir

// SimplifyBranches turns each branch on a constant condition into a jump to
// the arm it takes, and each branch with an arm that holds nothing but
// `unreachable` into a jump to the other arm: control never takes that arm,
// so its test decides nothing. An exhaustive match lowers its last check that
// way, and a backend given the check spends a comparison on every dispatch,
// or, with one more case than it needs, a jump table. It then removes the
// blocks no path reaches any more and the incoming their phis took from a
// block that no longer jumps to them. A phi left with one value, or with one
// value besides itself, becomes that value.
func SimplifyBranches(module *Module) {
	for _, fn := range module.Functions {
		simplifyBranchesIn(fn)
	}
}

// simplifyBranchesIn simplifies one function.
func simplifyBranchesIn(fn *Function) {
	if len(fn.Blocks) == 0 {
		return
	}
	foldKnownResults(fn)
	known := map[string]bool{}
	dead := map[string]bool{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == "const" && instr.Result.Type == "bool" {
				known[instr.Result.Name] = instr.Immediate == "true"
			}
		}
		if len(block.Instrs) == 0 && block.Terminator.Op == "unreachable" {
			dead[block.Name] = true
		}
	}
	changed := false
	for _, block := range fn.Blocks {
		term := block.Terminator
		taken, ok := branchTaken(term, known, dead)
		if !ok {
			continue
		}
		target, dropped := term.Target, term.Else
		if !taken {
			target, dropped = term.Else, term.Target
		}
		block.Terminator = Terminator{Op: "jump", Target: target}
		dropIncoming(fn, dropped, block.Name)
		changed = true
	}
	if !changed {
		return
	}
	removeUnreachableBlocks(fn)
	foldTrivialPhis(fn)
}

// branchTaken reports which arm a branch always takes: the arm its constant
// condition names, or the arm opposite one that holds nothing but
// `unreachable`.
func branchTaken(term Terminator, known map[string]bool, dead map[string]bool) (bool, bool) {
	if term.Op != "branch" {
		return false, false
	}
	if taken, ok := known[term.Cond.Name]; ok {
		return taken, true
	}
	switch {
	case dead[term.Else] && !dead[term.Target]:
		return true, true
	case dead[term.Target] && !dead[term.Else]:
		return false, true
	}
	return false, false
}

// dropIncoming removes one incoming from block from each phi of the block
// named target.
func dropIncoming(fn *Function, target string, from string) {
	for _, block := range fn.Blocks {
		if block.Name != target {
			continue
		}
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				continue
			}
			for index, incoming := range instr.Incoming {
				if incoming.Block == from {
					instr.Incoming = append(instr.Incoming[:index:index], instr.Incoming[index+1:]...)
					break
				}
			}
		}
	}
}

// removeUnreachableBlocks keeps the blocks reachable from the entry, and the
// phi incoming that arrive from them.
func removeUnreachableBlocks(fn *Function) {
	at := make(map[string]int, len(fn.Blocks))
	for position, block := range fn.Blocks {
		at[block.Name] = position
	}
	reached := make([]bool, len(fn.Blocks))
	stack := []int{0}
	for len(stack) > 0 {
		position := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[position] {
			continue
		}
		reached[position] = true
		for _, target := range fn.Blocks[position].Terminator.Successors() {
			if next, ok := at[target]; ok && !reached[next] {
				stack = append(stack, next)
			}
		}
	}
	kept := fn.Blocks[:0]
	live := map[string]bool{}
	for position, block := range fn.Blocks {
		if reached[position] {
			kept = append(kept, block)
			live[block.Name] = true
		}
	}
	fn.Blocks = kept
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				continue
			}
			incoming := instr.Incoming[:0]
			for _, edge := range instr.Incoming {
				if live[edge.Block] {
					incoming = append(incoming, edge)
				}
			}
			instr.Incoming = incoming
		}
	}
}

// foldTrivialPhis replaces each phi whose incoming are one value, apart from
// the phi itself, with that value, until no such phi is left.
func foldTrivialPhis(fn *Function) {
	for {
		replacements := map[string]Value{}
		for _, block := range fn.Blocks {
			kept := block.Instrs[:0]
			for _, instr := range block.Instrs {
				if value, ok := trivialPhiValue(instr); ok {
					replacements[instr.Result.Name] = value
					continue
				}
				kept = append(kept, instr)
			}
			block.Instrs = kept
		}
		if len(replacements) == 0 {
			return
		}
		replaceFunctionValues(fn, replacements)
	}
}

// trivialPhiValue returns the one value a phi merges besides itself.
func trivialPhiValue(instr *Instr) (Value, bool) {
	if instr.Op != "phi" || len(instr.Incoming) == 0 {
		return Value{}, false
	}
	var only *Value
	for index := range instr.Incoming {
		value := instr.Incoming[index].Value
		if value.Name == instr.Result.Name {
			continue
		}
		if only != nil && only.Name != value.Name {
			return Value{}, false
		}
		only = &instr.Incoming[index].Value
	}
	if only == nil {
		return Value{}, false
	}
	return *only, true
}

// foldKnownResults answers the error.try and error.has of results an
// error.ok with no payload made: the try cannot fail and goes, and the test
// is true.
func foldKnownResults(fn *Function) bool {
	succeeded := map[string]bool{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == "error.ok" && len(instr.Args) == 0 {
				succeeded[instr.Result.Name] = true
			}
		}
	}
	if len(succeeded) == 0 {
		return false
	}
	changed := false
	for _, block := range fn.Blocks {
		kept := block.Instrs[:0]
		for _, instr := range block.Instrs {
			if len(instr.Args) == 1 && succeeded[instr.Args[0].Name] {
				switch {
				case instr.Op == "error.try" && instr.Result.Type == "void":
					changed = true
					continue
				case instr.Op == "error.has":
					instr.Op = "const"
					instr.Args = nil
					instr.Immediate = "true"
					changed = true
				}
			}
			kept = append(kept, instr)
		}
		block.Instrs = kept
	}
	return changed
}
