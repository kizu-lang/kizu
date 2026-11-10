package ir

import (
	"fmt"
	"math/rand"
	"testing"
)

// TestVerifyAppendedAgreesWithVerify checks that verifying functions one by
// one, in module order, reports what verifying the module at once does: the
// first function that fails names the same failure, and a module that passes
// has no function that fails alone.
func TestVerifyAppendedAgreesWithVerify(t *testing.T) {
	cases := append(typeRejections(), ssaRejections()...)
	cases = append(cases,
		rejection{name: "borrowed value", module: borrowedArgument("i64")},
		rejection{name: "absorbed error set", module: absorbedErrorSetReturn()},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			whole := Verify(tc.module)
			var first error
			for _, fn := range tc.module.Functions {
				if err := VerifyAppended(tc.module, fn); err != nil {
					first = err
					break
				}
			}
			if fmt.Sprint(whole) != fmt.Sprint(first) {
				t.Fatalf("Verify says %v, one function at a time says %v", whole, first)
			}
		})
	}
}

// TestDominatesAgreesWithReachability compares the tree against the
// definition of dominance on random control-flow graphs: d dominates b when b
// is unreachable, or d is b, or the entry cannot reach b without passing d.
func TestDominatesAgreesWithReachability(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for round := 0; round < 300; round++ {
		fn := randomCFG(random, 1+random.Intn(12))
		at := make(map[string]int, len(fn.Blocks))
		for position, block := range fn.Blocks {
			at[block.Name] = position
		}
		tree := buildDominatorTree(fn, blockPredecessors(fn, at))
		reachable := reachableAvoiding(fn, at, -1)
		for dominator := range fn.Blocks {
			avoiding := reachableAvoiding(fn, at, dominator)
			for block := range fn.Blocks {
				want := !reachable[block] || dominator == block ||
					(reachable[dominator] && !avoiding[block])
				if got := tree.dominates(dominator, block); got != want {
					t.Fatalf("round %d: dominates(%d, %d) = %v, want %v in %v",
						round, dominator, block, got, want, successorsOf(fn))
				}
			}
		}
	}
}

// randomCFG builds a function of count blocks, each ending in a return, a jump
// or a branch to blocks picked anywhere, so back edges and unreachable blocks
// both occur.
func randomCFG(random *rand.Rand, count int) *Function {
	fn := &Function{Name: "f"}
	for position := 0; position < count; position++ {
		block := &Block{Name: fmt.Sprintf("b%d", position)}
		switch random.Intn(4) {
		case 0:
			block.Terminator = Terminator{Op: "return"}
		case 1:
			block.Terminator = Terminator{Op: "br", Target: fmt.Sprintf("b%d", random.Intn(count))}
		default:
			block.Terminator = Terminator{
				Op:     "condbr",
				Target: fmt.Sprintf("b%d", random.Intn(count)),
				Else:   fmt.Sprintf("b%d", random.Intn(count)),
			}
		}
		fn.Blocks = append(fn.Blocks, block)
	}
	return fn
}

// reachableAvoiding marks the blocks the entry reaches without entering the
// block at avoid; -1 avoids nothing.
func reachableAvoiding(fn *Function, at map[string]int, avoid int) []bool {
	seen := make([]bool, len(fn.Blocks))
	if avoid == 0 {
		return seen
	}
	stack := []int{0}
	seen[0] = true
	for len(stack) > 0 {
		position := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, target := range fn.Blocks[position].Terminator.Successors() {
			to := at[target]
			if to == avoid || seen[to] {
				continue
			}
			seen[to] = true
			stack = append(stack, to)
		}
	}
	return seen
}

// successorsOf spells the graph for a failure message.
func successorsOf(fn *Function) [][]string {
	out := make([][]string, len(fn.Blocks))
	for position, block := range fn.Blocks {
		out[position] = block.Terminator.Successors()
	}
	return out
}
