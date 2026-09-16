package wasm

import (
	"fmt"
	"sort"

	"github.com/kizu-lang/kizu/internal/ir"
)

// A function's blocks are emitted as WebAssembly's structured control flow
// -- `block`, `loop`, `if` and branches to their labels -- rather than as a
// dispatch loop over a program counter. A dispatch loop hides every loop
// from the engine: each iteration of a source loop goes round the dispatch
// once and through a chain of comparisons, and the engine's optimizer,
// which finds loops by their shape, finds none. The translation is the
// one Ramsey gives in "Beyond Relooper" (ICFP 2022) for reducible graphs:
// a block is placed under its immediate dominator; a block reached by two
// or more forward edges (a merge node) gets a `block` that ends where its
// code begins, so its predecessors branch to it; a block reached by a back
// edge (a loop header) gets a `loop` around its code, so the back edge
// branches to it; and a block with one predecessor is written where that
// predecessor branches to it. The graphs here come from structured source,
// so every back edge targets a block that dominates it; one that does not
// is a lowering bug and is refused rather than dispatched.

// structure is what the emission of one function reads: its blocks by
// name, in reverse postorder, with their predecessors, dominators, and
// which of them head a loop or merge forward edges.
type structure struct {
	blocks map[string]*ir.Block
	// order is reverse postorder from the entry; rpo is a block's index in it.
	order []string
	rpo   map[string]int
	preds map[string][]string
	idom  map[string]string
	// loopHeaders are the targets of back edges; merges the blocks with two
	// or more forward predecessors.
	loopHeaders map[string]bool
	merges      map[string]bool
	// mergeChildren lists, for each block, the merge nodes it immediately
	// dominates, latest in reverse postorder first: the block for the
	// latest one is the outermost.
	mergeChildren map[string][]string
}

// analyze computes the structure of one function's control flow graph.
func analyze(fn *ir.Function) (*structure, error) {
	s := &structure{
		blocks:        map[string]*ir.Block{},
		rpo:           map[string]int{},
		preds:         map[string][]string{},
		idom:          map[string]string{},
		loopHeaders:   map[string]bool{},
		merges:        map[string]bool{},
		mergeChildren: map[string][]string{},
	}
	if len(fn.Blocks) == 0 {
		return s, nil
	}
	for _, block := range fn.Blocks {
		s.blocks[block.Name] = block
	}
	s.order = postorderReversed(fn.Blocks[0].Name, s.blocks)
	for index, name := range s.order {
		s.rpo[name] = index
	}
	for _, name := range s.order {
		for _, target := range s.blocks[name].Terminator.Successors() {
			if _, reachable := s.rpo[target]; reachable {
				s.preds[target] = append(s.preds[target], name)
			}
		}
	}
	s.computeDominators()
	for _, name := range s.order {
		forward := 0
		for _, pred := range s.preds[name] {
			if s.rpo[pred] >= s.rpo[name] {
				if !s.dominates(name, pred) {
					return nil, fmt.Errorf(
						"wasm error: irreducible control flow: `%s` jumps back to `%s` without being dominated by it",
						pred, name)
				}
				s.loopHeaders[name] = true
			} else {
				forward++
			}
		}
		if forward >= 2 {
			s.merges[name] = true
		}
	}
	for _, name := range s.order {
		if !s.merges[name] {
			continue
		}
		parent := s.idom[name]
		s.mergeChildren[parent] = append(s.mergeChildren[parent], name)
	}
	for _, children := range s.mergeChildren {
		sort.Slice(children, func(i, j int) bool { return s.rpo[children[i]] > s.rpo[children[j]] })
	}
	return s, nil
}

// postorderReversed lists the blocks reachable from entry in reverse
// postorder: every block before the blocks its forward edges reach.
func postorderReversed(entry string, blocks map[string]*ir.Block) []string {
	visited := map[string]bool{}
	var post []string
	var visit func(name string)
	visit = func(name string) {
		visited[name] = true
		for _, target := range blocks[name].Terminator.Successors() {
			if !visited[target] {
				visit(target)
			}
		}
		post = append(post, name)
	}
	visit(entry)
	for i, j := 0, len(post)-1; i < j; i, j = i+1, j-1 {
		post[i], post[j] = post[j], post[i]
	}
	return post
}

// computeDominators finds each block's immediate dominator by the iterative
// algorithm of Cooper, Harvey and Kennedy over reverse postorder.
func (s *structure) computeDominators() {
	entry := s.order[0]
	s.idom[entry] = entry
	changed := true
	for changed {
		changed = false
		for _, name := range s.order[1:] {
			candidate := ""
			for _, pred := range s.preds[name] {
				if _, done := s.idom[pred]; !done {
					continue
				}
				if candidate == "" {
					candidate = pred
				} else {
					candidate = s.intersect(pred, candidate)
				}
			}
			if candidate != "" && s.idom[name] != candidate {
				s.idom[name] = candidate
				changed = true
			}
		}
	}
}

// intersect walks two blocks up the dominator tree to their common ancestor.
func (s *structure) intersect(a string, b string) string {
	for a != b {
		for s.rpo[a] > s.rpo[b] {
			a = s.idom[a]
		}
		for s.rpo[b] > s.rpo[a] {
			b = s.idom[b]
		}
	}
	return a
}

// dominates reports whether `a` is `b` or an ancestor of `b` in the
// dominator tree.
func (s *structure) dominates(a string, b string) bool {
	for {
		if a == b {
			return true
		}
		parent := s.idom[b]
		if parent == b {
			return false
		}
		b = parent
	}
}

// loopLabel names the `loop` a header opens.
func loopLabel(name string) string {
	return "$loop." + name
}

// blockLabel names the `block` that ends where a merge node begins.
func blockLabel(name string) string {
	return "$block." + name
}
