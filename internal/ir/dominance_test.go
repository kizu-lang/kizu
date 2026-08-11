package ir

import (
	"testing"
)

// blockScopedLetSource declares the same name in two sibling loop bodies, with
// the second loop inside an if. Before block-scoped bindings, the name stayed in
// the environment after each loop, so the if merge built a phi over one value
// from each loop body -- and a loop that runs zero times never defines its
// value, so neither dominated the merge. clang accepted the module only because
// Apple's toolchain does not run the IR verifier; LLVM 21 rejected it.
const blockScopedLetSource = `fn run(n: i64, flag: bool) -> i64 {
    var total = 0;
    var i = 0;
    while i < n {
        let entry = i * 2;
        total = total + entry;
        i = i + 1;
    }
    if flag {
        var j = 0;
        while j < n {
            let entry = j * 3;
            total = total + entry;
            j = j + 1;
        }
    }
    return total;
}

fn main() {
    print(run(3, true));
}`

// TestLowerPhiIncomingValuesDominateTheirEdges checks the SSA property the LLVM
// verifier enforces: a phi's incoming value must be defined in a block that
// dominates the predecessor it arrives from. Asserting the property rather than
// a block name catches the whole class, not the one shape that exposed it.
func TestLowerPhiIncomingValuesDominateTheirEdges(t *testing.T) {
	for name, source := range map[string]string{
		"block_scoped_let": blockScopedLetSource,
		"while":            whileSource,
		"if":               ifSource,
		"error_union":      errorUnionSource,
	} {
		t.Run(name, func(t *testing.T) {
			module := lowerSource(t, source)
			for _, fn := range module.Functions {
				assertPhiDominance(t, fn)
			}
		})
	}
}

// assertPhiDominance reports every phi incoming whose value is not available on
// the edge it claims to arrive on.
func assertPhiDominance(t *testing.T, fn *Function) {
	t.Helper()
	if len(fn.Blocks) == 0 {
		return
	}
	definedIn := valueDefiningBlocks(fn)
	dominators := computeDominators(fn)
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op != "phi" {
				continue
			}
			for _, incoming := range instr.Incoming {
				definition, ok := definedIn[incoming.Value.Name]
				if !ok {
					continue
				}
				if dominators[incoming.Block][definition] {
					continue
				}
				t.Errorf(
					"%s: phi %s in %s takes %s from %s, but %s is defined in %s, "+
						"which does not dominate %s",
					fn.Name, instr.Result.Name, block.Name, incoming.Value.Name,
					incoming.Block, incoming.Value.Name, definition, incoming.Block,
				)
			}
		}
	}
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

// computeDominators returns, for each block, the set of blocks dominating it.
// Standard iterative dataflow: a block is dominated by itself plus everything
// dominating all of its predecessors.
func computeDominators(fn *Function) map[string]map[string]bool {
	names := make([]string, 0, len(fn.Blocks))
	for _, block := range fn.Blocks {
		names = append(names, block.Name)
	}
	preds := blockPredecessors(fn)
	entry := fn.Blocks[0].Name
	dominators := map[string]map[string]bool{}
	for _, name := range names {
		dominators[name] = map[string]bool{}
		for _, other := range names {
			dominators[name][other] = true
		}
	}
	dominators[entry] = map[string]bool{entry: true}
	for changed := true; changed; {
		changed = false
		for _, name := range names {
			if name == entry {
				continue
			}
			next := intersectDominators(dominators, preds[name], names)
			next[name] = true
			if !sameDominatorSet(dominators[name], next) {
				dominators[name] = next
				changed = true
			}
		}
	}
	return dominators
}

// intersectDominators intersects the dominator sets of a block's predecessors.
// A block with no predecessors is unreachable, so nothing is claimed about it.
func intersectDominators(
	dominators map[string]map[string]bool,
	preds []string,
	names []string,
) map[string]bool {
	if len(preds) == 0 {
		out := map[string]bool{}
		for _, name := range names {
			out[name] = true
		}
		return out
	}
	out := map[string]bool{}
	for name, ok := range dominators[preds[0]] {
		if ok {
			out[name] = true
		}
	}
	for _, pred := range preds[1:] {
		for name := range out {
			if !dominators[pred][name] {
				delete(out, name)
			}
		}
	}
	return out
}

// blockPredecessors derives the CFG edges from each block's terminator.
func blockPredecessors(fn *Function) map[string][]string {
	preds := map[string][]string{}
	for _, block := range fn.Blocks {
		for _, target := range []string{block.Terminator.Target, block.Terminator.Else} {
			if target == "" {
				continue
			}
			preds[target] = append(preds[target], block.Name)
		}
	}
	return preds
}

// sameDominatorSet reports whether two dominator sets hold the same blocks.
func sameDominatorSet(left map[string]bool, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for name := range left {
		if !right[name] {
			return false
		}
	}
	return true
}
