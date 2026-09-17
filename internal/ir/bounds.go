package ir

import (
	"fmt"
	"strconv"
	"strings"
)

// EliminateBoundsChecks removes the index checks a function has already
// answered. A check is answered when the index is known not to be negative
// and known to be below a value equal to the length it is checked against:
// the index was compared with that length on the way in, or the same index of
// the same Array was checked since its header last could change.
//
// A check on a view is its own cond_fail and goes. An Array operation carries
// its check inside, so an answered one is written as the borrow its check
// would give and a read or store through it: `get_or_panic` becomes `at`,
// `opt.value` and a load, and `set` becomes `at_mut`, `opt.value`, a store and
// a success. A backend works out an address whose presence nothing reads
// without a branch, and an engine drops the unread comparison. What is known
// of a value holds wherever its definition dominates, since an SSA value does
// not change; what is known of an Array's length holds until an instruction
// that could change the header.
//
// An index is known not to be negative when it is a constant, a length, or a
// loop counter that starts at a constant that is not negative and steps by
// one. The step cannot wrap where it is taken below a known bound, so a
// counter is trusted only when every step is; otherwise the function is walked
// again without it.
func EliminateBoundsChecks(module *Module) {
	for _, fn := range module.Functions {
		eliminateBoundsChecksIn(fn)
	}
}

// boundsFacts is what is known at one point of a function.
type boundsFacts struct {
	// less holds pairs {x, y} with x < y as signed integers.
	less map[[2]string]bool
	// nonneg holds values known not to be negative.
	nonneg map[string]bool
	// length maps an Array reference to a value equal to its current length.
	length map[string]Value
	// viewLength maps a view to its length, which nothing changes.
	viewLength map[string]Value
	// checked holds pairs {array, index} whose check has passed since the
	// header last could change.
	checked map[[2]string]bool
}

// newBoundsFacts returns an empty fact set.
func newBoundsFacts() *boundsFacts {
	return &boundsFacts{
		less:       map[[2]string]bool{},
		nonneg:     map[string]bool{},
		length:     map[string]Value{},
		viewLength: map[string]Value{},
		checked:    map[[2]string]bool{},
	}
}

// copyFacts returns an independent copy of facts.
func (f *boundsFacts) copyFacts() *boundsFacts {
	out := newBoundsFacts()
	for key := range f.less {
		out.less[key] = true
	}
	for key := range f.nonneg {
		out.nonneg[key] = true
	}
	for key, value := range f.length {
		out.length[key] = value
	}
	for key, value := range f.viewLength {
		out.viewLength[key] = value
	}
	for key := range f.checked {
		out.checked[key] = true
	}
	return out
}

// forgetHeaders drops what is known of Array headers.
func (f *boundsFacts) forgetHeaders() {
	clear(f.length)
	clear(f.checked)
}

// boundsPlan is what one walk of a function decided.
type boundsPlan struct {
	// drops are the cond_fail instructions an answered check made.
	drops map[*Instr]bool
	// answered are the Array operations whose check is answered.
	answered map[*Instr]bool
	// aliases replaces a length read with an equal one read before it.
	aliases map[string]Value
	// unsafeSteps are the counters a step was taken for without a bound.
	unsafeSteps map[string]bool
}

// boundsWalker carries the function-wide tables one walk reads.
type boundsWalker struct {
	fn      *Function
	defs    map[string]*Instr
	counter map[string]bool
	// steps maps a step's result to the counters it feeds.
	steps map[string][]string
	// order and tree are the function's reverse postorder and dominator
	// tree, which no walk changes.
	order []int
	tree  dominatorTree
	preds [][]int
	plan  boundsPlan
}

// eliminateBoundsChecksIn plans and applies the removals for one function.
func eliminateBoundsChecksIn(fn *Function) {
	if len(fn.Blocks) == 0 {
		return
	}
	w := &boundsWalker{fn: fn, defs: map[string]*Instr{}}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Result.Name != "" {
				w.defs[instr.Result.Name] = instr
			}
		}
	}
	excluded := map[string]bool{}
	w.counter, w.steps = loopCounters(fn, w.defs, excluded)
	at := make(map[string]int, len(fn.Blocks))
	for position, block := range fn.Blocks {
		at[block.Name] = position
	}
	w.preds = blockPredecessors(fn, at)
	w.tree = buildDominatorTree(fn, w.preds)
	w.order = reversePostorder(fn)
	for {
		w.walk()
		if len(w.plan.unsafeSteps) == 0 {
			break
		}
		for name := range w.plan.unsafeSteps {
			excluded[name] = true
		}
		w.counter, w.steps = loopCounters(fn, w.defs, excluded)
	}
	if len(w.plan.drops) == 0 && len(w.plan.answered) == 0 && len(w.plan.aliases) == 0 {
		return
	}
	applyBoundsPlan(fn, w.plan)
}

// loopCounters finds the i64 phis each of whose incoming is a constant that
// is not negative or a step of one from such a phi, and the steps that feed
// them. Every phi not excluded starts as a candidate and loses its place when
// an incoming is neither, until none does.
func loopCounters(
	fn *Function,
	defs map[string]*Instr,
	excluded map[string]bool,
) (map[string]bool, map[string][]string) {
	counters := map[string]bool{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == "phi" && instr.Result.Type == "i64" && !excluded[instr.Result.Name] {
				counters[instr.Result.Name] = true
			}
		}
	}
	for changed := true; changed; {
		changed = dropCounters(fn, defs, counters)
	}
	steps := map[string][]string{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if !counters[instr.Result.Name] {
				continue
			}
			for _, incoming := range instr.Incoming {
				if _, ok := stepOf(incoming.Value, defs); ok {
					steps[incoming.Value.Name] = append(steps[incoming.Value.Name], instr.Result.Name)
				}
			}
		}
	}
	return counters, steps
}

// dropCounters removes each candidate with an incoming that is neither a
// constant that is not negative nor a step of one from a candidate, and
// reports whether it removed any.
func dropCounters(fn *Function, defs map[string]*Instr, counters map[string]bool) bool {
	changed := false
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if counters[instr.Result.Name] && !countsUp(instr, defs, counters) {
				delete(counters, instr.Result.Name)
				changed = true
			}
		}
	}
	return changed
}

// countsUp reports whether every incoming of a phi is a constant that is not
// negative or a step of one from a candidate counter.
func countsUp(phi *Instr, defs map[string]*Instr, counters map[string]bool) bool {
	for _, incoming := range phi.Incoming {
		if nonNegativeConstant(incoming.Value, defs) {
			continue
		}
		if from, ok := stepOf(incoming.Value, defs); ok && counters[from] {
			continue
		}
		return false
	}
	return true
}

// nonNegativeConstant reports whether value is an i64 constant that is not
// negative.
func nonNegativeConstant(value Value, defs map[string]*Instr) bool {
	number, ok := constantValue(value, defs)
	return ok && number >= 0
}

// constantValue returns the i64 a constant value holds.
func constantValue(value Value, defs map[string]*Instr) (int64, bool) {
	if value.Type != "i64" {
		return 0, false
	}
	if instr, ok := defs[value.Name]; ok {
		if instr.Op != "const" {
			return 0, false
		}
		number, err := strconv.ParseInt(instr.Immediate, 10, 64)
		return number, err == nil
	}
	number, err := strconv.ParseInt(value.Name, 10, 64)
	return number, err == nil
}

// stepOf returns the value a `+ 1` step adds one to.
func stepOf(value Value, defs map[string]*Instr) (string, bool) {
	instr, ok := defs[value.Name]
	if !ok || instr.Op != "binary.+" || len(instr.Args) != 2 || instr.Result.Type != "i64" {
		return "", false
	}
	if one, ok := constantValue(instr.Args[1], defs); ok && one == 1 {
		return instr.Args[0].Name, true
	}
	if one, ok := constantValue(instr.Args[0], defs); ok && one == 1 {
		return instr.Args[1].Name, true
	}
	return "", false
}

// walk visits the blocks down the dominator tree and records a fresh plan.
func (w *boundsWalker) walk() {
	w.plan = boundsPlan{
		drops:       map[*Instr]bool{},
		answered:    map[*Instr]bool{},
		aliases:     map[string]Value{},
		unsafeSteps: map[string]bool{},
	}
	fn := w.fn
	ends := make([]*boundsFacts, len(fn.Blocks))
	for index, position := range w.order {
		block := fn.Blocks[position]
		facts := newBoundsFacts()
		if index > 0 {
			idom := w.order[w.tree.idom[index]]
			facts = ends[idom].copyFacts()
			if len(w.preds[position]) == 1 && w.preds[position][0] == idom {
				w.learnEdge(facts, fn.Blocks[idom].Terminator, block.Name)
			} else if w.pathsMayWriteHeaders(w.preds, position, idom) {
				facts.forgetHeaders()
			}
		}
		for _, instr := range block.Instrs {
			w.visit(facts, instr)
		}
		ends[position] = facts
	}
}

// pathsMayWriteHeaders reports whether a block on a path from idom to the
// block at position holds an instruction that could change an Array header.
func (w *boundsWalker) pathsMayWriteHeaders(preds [][]int, position int, idom int) bool {
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
			return true
		}
		for _, instr := range w.fn.Blocks[block].Instrs {
			if !leavesHeaders(instr.Op) {
				return true
			}
		}
		if block != position {
			stack = append(stack, preds[block]...)
		}
	}
	return false
}

// leavesHeaders reports whether an instruction cannot change an Array header:
// it only computes its result, or it checks, reads or writes an element or a
// field.
func leavesHeaders(op string) bool {
	switch op {
	case "cond_fail", "array.set", "array.swap", "array.get_or_panic", "slice.store":
		return true
	}
	return isPureOp(op) || strings.HasPrefix(op, "field.ref.set.")
}

// learnEdge adds what taking the edge from term to target says about its
// condition.
func (w *boundsWalker) learnEdge(facts *boundsFacts, term Terminator, target string) {
	if term.Op != "branch" || term.Target == term.Else {
		return
	}
	w.learnCondition(facts, term.Cond, term.Target == target)
}

// learnCondition adds what a comparison being holds or truth says.
func (w *boundsWalker) learnCondition(facts *boundsFacts, cond Value, holds bool) {
	instr, ok := w.defs[cond.Name]
	if !ok || len(instr.Args) != 2 || instr.Args[0].Type != "i64" {
		return
	}
	left := w.resolvedName(instr.Args[0].Name)
	right := w.resolvedName(instr.Args[1].Name)
	op := instr.Op
	if !holds {
		op = negatedComparison(op)
	}
	switch op {
	case "binary.<":
		facts.less[[2]string{left, right}] = true
	case "binary.>":
		facts.less[[2]string{right, left}] = true
	case "binary.>=":
		if zero, ok := constantValue(instr.Args[1], w.defs); ok && zero >= 0 {
			facts.nonneg[left] = true
		}
	case "binary.<=":
		if zero, ok := constantValue(instr.Args[0], w.defs); ok && zero >= 0 {
			facts.nonneg[right] = true
		}
	}
}

// negatedComparison returns the comparison that holds when op does not, or ""
// for an operation that is not an ordering.
func negatedComparison(op string) string {
	switch op {
	case "binary.<":
		return "binary.>="
	case "binary.>=":
		return "binary.<"
	case "binary.>":
		return "binary.<="
	case "binary.<=":
		return "binary.>"
	}
	return ""
}

// visit records what one instruction answers and learns.
func (w *boundsWalker) visit(facts *boundsFacts, instr *Instr) {
	switch instr.Op {
	case "cond_fail":
		w.visitCondFail(facts, instr)
		return
	case "array.len", "slice.len":
		w.visitLength(facts, instr)
		return
	case "array.get_or_panic", "array.set", "array.at", "array.at_mut":
		w.visitArrayCheck(facts, instr)
		return
	case "binary.+":
		if fed := w.steps[instr.Result.Name]; len(fed) > 0 {
			from, _ := stepOf(instr.Result, w.defs)
			if !facts.hasUpperBound(from) {
				for _, counter := range fed {
					w.plan.unsafeSteps[counter] = true
				}
			}
		}
	}
	if !leavesHeaders(instr.Op) {
		facts.forgetHeaders()
	}
}

// hasUpperBound reports whether a value a step adds one to is known to be
// below some value, so the step cannot wrap.
func (f *boundsFacts) hasUpperBound(name string) bool {
	for pair := range f.less {
		if pair[0] == name {
			return true
		}
	}
	return false
}

// visitCondFail drops a bounds check whose failing condition cannot hold, and
// learns that it does not from one that stays.
func (w *boundsWalker) visitCondFail(facts *boundsFacts, instr *Instr) {
	if len(instr.Args) == 0 {
		return
	}
	cond, ok := w.defs[instr.Args[0].Name]
	if ok && len(cond.Args) == 2 && cond.Args[0].Type == "i64" {
		left, right := cond.Args[0], cond.Args[1]
		switch cond.Op {
		case "binary.<":
			if zero, ok := constantValue(right, w.defs); ok && zero == 0 && w.nonNegative(facts, left) {
				w.plan.drops[instr] = true
				return
			}
		case "binary.>=":
			if facts.less[[2]string{w.resolvedName(left.Name), w.resolvedName(right.Name)}] {
				w.plan.drops[instr] = true
				return
			}
		}
	}
	w.learnCondition(facts, instr.Args[0], false)
}

// nonNegative reports whether value is known not to be negative.
func (w *boundsWalker) nonNegative(facts *boundsFacts, value Value) bool {
	name := w.resolvedName(value.Name)
	if facts.nonneg[name] || w.counter[name] || nonNegativeConstant(value, w.defs) {
		return true
	}
	instr, ok := w.defs[name]
	return ok && (instr.Op == "array.len" || instr.Op == "slice.len")
}

// visitLength makes a length read stand for the equal one read before it,
// or the length known from now on.
func (w *boundsWalker) visitLength(facts *boundsFacts, instr *Instr) {
	if len(instr.Args) != 1 {
		return
	}
	container := instr.Args[0].Name
	lengths := facts.length
	if instr.Op == "slice.len" {
		lengths = facts.viewLength
	}
	if known, ok := lengths[container]; ok && known.Type == instr.Result.Type {
		w.plan.aliases[instr.Result.Name] = known
		return
	}
	lengths[container] = instr.Result
}

// visitArrayCheck answers an Array operation's check when it can, and learns
// that the index is in bounds from one that stays.
func (w *boundsWalker) visitArrayCheck(facts *boundsFacts, instr *Instr) {
	if len(instr.Args) < 2 {
		return
	}
	array, index := instr.Args[0], instr.Args[1]
	key := [2]string{array.Name, index.Name}
	answered := facts.checked[key]
	if !answered && w.nonNegative(facts, index) {
		if length, ok := facts.length[array.Name]; ok {
			answered = facts.less[[2]string{w.resolvedName(index.Name), w.resolvedName(length.Name)}]
		}
	}
	if answered && strings.HasPrefix(array.Type, "&") {
		w.plan.answered[instr] = true
	}
	if instr.Op == "array.get_or_panic" || instr.Op == "array.set" {
		facts.checked[key] = true
	}
}

// resolvedName follows the length aliases the walk has made.
func (w *boundsWalker) resolvedName(name string) string {
	for {
		next, ok := w.plan.aliases[name]
		if !ok {
			return name
		}
		name = next.Name
	}
}

// applyBoundsPlan rewrites the function the way a walk planned.
func applyBoundsPlan(fn *Function, plan boundsPlan) {
	if len(plan.aliases) > 0 {
		replaceFunctionValues(fn, plan.aliases)
	}
	site := 0
	present := map[string]bool{}
	for _, block := range fn.Blocks {
		out := make([]*Instr, 0, len(block.Instrs))
		for _, instr := range block.Instrs {
			if plan.drops[instr] {
				continue
			}
			if !plan.answered[instr] {
				out = append(out, instr)
				continue
			}
			site++
			out = append(out, answeredAccess(instr, fmt.Sprintf("%%bce%d.", site), present)...)
		}
		block.Instrs = out
	}
	if len(present) == 0 {
		return
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == "opt.has" && len(instr.Args) == 1 && present[instr.Args[0].Name] {
				instr.Op = "const"
				instr.Args = nil
				instr.Immediate = "true"
			}
		}
	}
}

// answeredAccess returns the instructions an Array operation with an answered
// check becomes, and marks the borrow optionals known to be present.
func answeredAccess(instr *Instr, prefix string, present map[string]bool) []*Instr {
	array, index := instr.Args[0], instr.Args[1]
	elem := strings.TrimSuffix(strings.TrimPrefix(derefType(array.Type), "std::array::Array<"), ">")
	switch instr.Op {
	case "array.at", "array.at_mut":
		present[instr.Result.Name] = true
		return []*Instr{instr}
	case "array.get_or_panic":
		borrow := Value{Name: prefix + "at", Type: "?&" + elem}
		ref := Value{Name: prefix + "ref", Type: "&" + elem}
		return []*Instr{
			{Result: borrow, Op: "array.at", Args: []Value{array, index}},
			{Result: ref, Op: "opt.value", Args: []Value{borrow}},
			{Result: instr.Result, Op: "ref.load", Args: []Value{ref}},
		}
	default:
		borrow := Value{Name: prefix + "at", Type: "?&var " + elem}
		ref := Value{Name: prefix + "ref", Type: "&var " + elem}
		return []*Instr{
			{Result: borrow, Op: "array.at_mut", Args: []Value{array, index}},
			{Result: ref, Op: "opt.value", Args: []Value{borrow}},
			{Result: Value{Type: "void"}, Op: "ref.store", Args: []Value{ref, instr.Args[2]}},
			{Result: instr.Result, Op: "error.ok"},
		}
	}
}
