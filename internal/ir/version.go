package ir

import (
	"fmt"
	"strings"
)

// versionLimit is how many instructions a loop may hold for
// VersionLoopsForBounds to keep a second copy of it.
const versionLimit = 64

// VersionLoopsForBounds gives a counted loop whose index checks every
// iteration answers the same way a second copy without those checks, and a
// test before the loop that picks the copy. A loop `while i < limit` whose
// counter steps by one from start reads indexes `base + i` for bases that do
// not change in it; when start and every base are not negative and limit is no
// more than each length less its base, every index the loop can read is in
// bounds, and the copy runs. Otherwise the loop runs as it was, checks and
// all, and fails where it would have.
//
// A loop qualifies when nothing in it can change an Array header, it leaves
// only through its header's test, and it is small: the copy doubles its code.
func VersionLoopsForBounds(module *Module) {
	for _, fn := range module.Functions {
		versionLoopsIn(fn)
	}
}

// versionLoopsIn versions every qualifying loop of fn, reading the function's
// shape again after each, since a version adds blocks. A loop is looked at
// once, and so is the copy a version makes.
func versionLoopsIn(fn *Function) {
	seen := map[string]bool{}
	for site := 1; len(fn.Blocks) > 0; site++ {
		at := make(map[string]int, len(fn.Blocks))
		for position, block := range fn.Blocks {
			at[block.Name] = position
		}
		preds := blockPredecessors(fn, at)
		tree := buildDominatorTree(fn, preds)
		versioned := false
		for position, block := range fn.Blocks {
			if seen[block.Name] {
				continue
			}
			seen[block.Name] = true
			loop, ok := versionableLoop(fn, at, preds, tree, position)
			if !ok {
				continue
			}
			prefix := fmt.Sprintf("ver%d.", site)
			seen[prefix+block.Name] = true
			loop.version(prefix)
			versioned = true
			break
		}
		if !versioned {
			return
		}
	}
}

// versionedLoop is one loop VersionLoopsForBounds can version.
type versionedLoop struct {
	fn     *Function
	header int
	pre    int
	latch  int
	exit   string
	// inside names the loop's blocks, and defined the values they define.
	inside  map[string]bool
	defined map[string]bool
	defs    map[string]*Instr
	counter *Instr
	start   Value
	limit   Value
	checks  []boundedCheck
	// answered are the checks the copy leaves out: Array operations it writes
	// unchecked and view checks it drops.
	answered map[*Instr]bool
}

// boundedCheck is one bound the test before the loop establishes: base is at
// least zero, and, when there is a container, the counter's limit is no more
// than the container's length less base.
type boundedCheck struct {
	container Value
	base      Value
	view      bool
}

// versionableLoop reports the loop headed by the block at position when it
// can be versioned.
func versionableLoop(
	fn *Function,
	at map[string]int,
	preds [][]int,
	tree dominatorTree,
	position int,
) (*versionedLoop, bool) {
	header := fn.Blocks[position]
	term := header.Terminator
	if position == 0 || term.Op != "branch" || term.Target == term.Else || len(preds[position]) != 2 {
		return nil, false
	}
	loop := &versionedLoop{fn: fn, header: position, exit: term.Else, pre: -1, latch: -1}
	body, ok := loop.splitEntries(at, preds, tree, term)
	if !ok {
		return nil, false
	}
	if !loop.collectBlocks(tree, body) || !loop.leavesOnlyThroughHeader() ||
		!loop.readDefinitions() || !loop.findCounter() {
		return nil, false
	}
	loop.findChecks()
	return loop, len(loop.answered) > 0
}

// splitEntries finds the header's one preheader and one latch, each a jump,
// and returns the body: the header's first arm, entered only from it,
// dominating the latch and not the exit.
func (loop *versionedLoop) splitEntries(
	at map[string]int,
	preds [][]int,
	tree dominatorTree,
	term Terminator,
) (int, bool) {
	for _, pred := range preds[loop.header] {
		if loop.fn.Blocks[pred].Terminator.Op != "jump" {
			return 0, false
		}
		if tree.dominates(loop.header, pred) {
			loop.latch = pred
		} else {
			loop.pre = pred
		}
	}
	body, ok := at[term.Target]
	exit, okExit := at[term.Else]
	if loop.latch < 0 || loop.pre < 0 || !ok || !okExit {
		return 0, false
	}
	return body, enteredOnlyFrom(preds, body, loop.header) &&
		tree.dominates(body, loop.latch) && !tree.dominates(body, exit)
}

// collectBlocks records the header and every block the body dominates as the
// loop's, and reports whether they hold at most versionLimit instructions.
func (loop *versionedLoop) collectBlocks(tree dominatorTree, body int) bool {
	header := loop.fn.Blocks[loop.header]
	loop.inside = map[string]bool{header.Name: true}
	count := len(header.Instrs)
	for index, block := range loop.fn.Blocks {
		if tree.dominates(body, index) {
			loop.inside[block.Name] = true
			count += len(block.Instrs)
		}
	}
	return count <= versionLimit
}

// leavesOnlyThroughHeader reports whether every edge out of the loop leaves
// from the header to the exit, and the latch is the one block jumping back.
func (loop *versionedLoop) leavesOnlyThroughHeader() bool {
	header := loop.fn.Blocks[loop.header]
	for position, block := range loop.fn.Blocks {
		if !loop.inside[block.Name] {
			continue
		}
		for _, target := range block.Terminator.Successors() {
			switch {
			case target == header.Name:
				if position != loop.latch {
					return false
				}
			case target == loop.exit && position == loop.header:
			case !loop.inside[target]:
				return false
			}
		}
	}
	return true
}

// readDefinitions records every definition of the function and those of the
// loop, and reports whether each instruction of the loop leaves Array headers
// as they are.
func (loop *versionedLoop) readDefinitions() bool {
	loop.defs = map[string]*Instr{}
	loop.defined = map[string]bool{}
	for _, block := range loop.fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Result.Name != "" {
				loop.defs[instr.Result.Name] = instr
			}
			if !loop.inside[block.Name] {
				continue
			}
			if instr.Result.Name != "" {
				loop.defined[instr.Result.Name] = true
			}
			if !instrLeavesHeaders(instr) {
				return false
			}
		}
	}
	return true
}

// instrLeavesHeaders reports whether one instruction cannot change an Array
// header: leavesHeaders says so of its operation, or it stores a scalar
// through a reference, which no header field is.
func instrLeavesHeaders(instr *Instr) bool {
	if leavesHeaders(instr.Op) {
		return true
	}
	return instr.Op == "ref.store" && len(instr.Args) == 2 &&
		strings.HasPrefix(instr.Args[0].Type, "&") && isForwardScalar(instr.Args[1].Type)
}

// findCounter reads the header's test as `counter < limit`, with a counter
// phi of the header that starts from the preheader and steps by one from the
// latch, a start from outside the loop and a limit the loop does not change.
func (loop *versionedLoop) findCounter() bool {
	header := loop.fn.Blocks[loop.header]
	cond, ok := loop.defs[header.Terminator.Cond.Name]
	if !ok || cond.Op != "binary.<" || len(cond.Args) != 2 || cond.Args[0].Type != "i64" {
		return false
	}
	counter, ok := loop.defs[cond.Args[0].Name]
	if !ok || counter.Op != "phi" || !loop.defined[counter.Result.Name] ||
		!loop.invariant(cond.Args[1]) {
		return false
	}
	pre := loop.fn.Blocks[loop.pre].Name
	latch := loop.fn.Blocks[loop.latch].Name
	for _, incoming := range counter.Incoming {
		switch incoming.Block {
		case pre:
			loop.start = incoming.Value
		case latch:
			from, ok := stepOf(incoming.Value, loop.defs)
			if !ok || from != counter.Result.Name {
				return false
			}
		default:
			return false
		}
	}
	loop.counter, loop.limit = counter, cond.Args[1]
	return loop.start.Name != "" && !loop.defined[loop.start.Name]
}

// invariant reports whether value is the same on every iteration: defined
// outside the loop, or computed inside it by arithmetic, a cast, a constant
// or a view's length from values that are.
func (loop *versionedLoop) invariant(value Value) bool {
	if !loop.defined[value.Name] {
		return true
	}
	instr := loop.defs[value.Name]
	switch instr.Op {
	case "const":
		return true
	case "binary.+", "binary.-", "binary.*", "cast", "slice.len":
		for _, arg := range instr.Args {
			if !loop.invariant(arg) {
				return false
			}
		}
		return true
	}
	return false
}

// indexBase reads index as `counter + base` for a base the loop does not
// change, or as the counter itself, whose base is empty.
func (loop *versionedLoop) indexBase(index Value) (Value, bool) {
	if index.Name == loop.counter.Result.Name {
		return Value{}, true
	}
	instr, ok := loop.defs[index.Name]
	if !ok || instr.Op != "binary.+" || len(instr.Args) != 2 || instr.Result.Type != "i64" {
		return Value{}, false
	}
	for side, arg := range instr.Args {
		other := instr.Args[1-side]
		if arg.Name == loop.counter.Result.Name && loop.invariant(other) {
			return other, true
		}
	}
	return Value{}, false
}

// findChecks records the checks of the loop whose index the counter walks: an
// Array read or store through a reference from outside the loop, and a view's
// two bounds checks.
func (loop *versionedLoop) findChecks() {
	loop.answered = map[*Instr]bool{}
	known := map[boundedCheck]bool{}
	add := func(check boundedCheck, instr *Instr) {
		loop.answered[instr] = true
		if !known[check] {
			known[check] = true
			loop.checks = append(loop.checks, check)
		}
	}
	for _, block := range loop.fn.Blocks {
		if !loop.inside[block.Name] {
			continue
		}
		for _, instr := range block.Instrs {
			switch instr.Op {
			case "array.get_or_panic", "array.set":
				array := instr.Args[0]
				if !strings.HasPrefix(array.Type, "&") || loop.defined[array.Name] {
					continue
				}
				if base, ok := loop.indexBase(instr.Args[1]); ok {
					add(boundedCheck{container: array, base: base}, instr)
				}
			case "cond_fail":
				loop.findViewCheck(instr, add)
			}
		}
	}
}

// findViewCheck records a view bounds check `index < 0` or `index >= len`
// whose index the counter walks.
func (loop *versionedLoop) findViewCheck(instr *Instr, add func(boundedCheck, *Instr)) {
	if len(instr.Args) == 0 || instr.Immediate != "bounds" {
		return
	}
	cond, ok := loop.defs[instr.Args[0].Name]
	if !ok || len(cond.Args) != 2 {
		return
	}
	base, ok := loop.indexBase(cond.Args[0])
	if !ok {
		return
	}
	switch cond.Op {
	case "binary.<":
		if zero, ok := constantValue(cond.Args[1], loop.defs); ok && zero == 0 {
			add(boundedCheck{base: base}, instr)
		}
	case "binary.>=":
		length, ok := loop.defs[cond.Args[1].Name]
		if ok && length.Op == "slice.len" && loop.invariant(cond.Args[1]) {
			add(boundedCheck{container: length.Args[0], base: base, view: true}, instr)
		}
	}
}

// version makes the copy and the test that picks it.
func (loop *versionedLoop) version(prefix string) {
	fn := loop.fn
	header := fn.Blocks[loop.header]
	pre := fn.Blocks[loop.pre]
	w := &versionWriter{loop: loop, prefix: prefix, hoisted: map[string]Value{}}
	copyHeader := prefix + header.Name
	checks := w.writeChecks(header.Name, copyHeader)
	pre.Terminator.Target = checks[0].Name
	copies := w.copyLoop(checks[len(checks)-1].Name, pre.Name)
	loop.redirectHeaderPhis(header, pre.Name, checks)
	loop.joinExit(prefix, copyHeader)
	blocks := make([]*Block, 0, len(fn.Blocks)+len(checks)+len(copies))
	for position, block := range fn.Blocks {
		if position == loop.header {
			blocks = append(blocks, checks...)
		}
		blocks = append(blocks, block)
	}
	fn.Blocks = append(blocks, copies...)
}

// versionWriter writes the instructions one version adds.
type versionWriter struct {
	loop    *versionedLoop
	prefix  string
	hoisted map[string]Value
	next    int
}

// fresh names a value the test before the loop defines.
func (w *versionWriter) fresh(role string, typ string) Value {
	w.next++
	return Value{Name: fmt.Sprintf("%%%scheck.%s%d", w.prefix, role, w.next), Type: typ}
}

// writeChecks returns the blocks of the test before the loop: each block
// computes one condition and branches to the next, or to the original loop
// when the condition fails; the last passes to the copy.
func (w *versionWriter) writeChecks(slow string, fast string) []*Block {
	loop := w.loop
	blocks := []*Block{}
	var current *Block
	begin := func() {
		current = &Block{Name: fmt.Sprintf("%scheck%d", w.prefix, len(blocks))}
		blocks = append(blocks, current)
	}
	finish := func(cond Value) {
		current.Terminator = Terminator{Op: "branch", Cond: cond, Else: slow}
	}
	nonNegative := func(value Value) {
		if nonNegativeConstant(value, loop.defs) {
			return
		}
		begin()
		zero := w.fresh("zero", "i64")
		test := w.fresh("nonneg", "bool")
		tested := w.hoist(current, value)
		current.Instrs = append(current.Instrs,
			&Instr{Result: zero, Op: "const", Immediate: "0"},
			&Instr{Result: test, Op: "binary.>=", Args: []Value{tested, zero}})
		finish(test)
	}
	nonNegative(loop.start)
	for _, check := range loop.checks {
		if check.base.Name != "" {
			nonNegative(check.base)
		}
	}
	for _, check := range loop.checks {
		if check.container.Name == "" {
			continue
		}
		begin()
		finish(w.writeFits(current, check))
	}
	if len(blocks) == 0 {
		begin()
		current.Terminator = Terminator{Op: "jump", Target: fast}
		return blocks
	}
	for index, block := range blocks {
		block.Terminator.Target = fast
		if index+1 < len(blocks) {
			block.Terminator.Target = blocks[index+1].Name
		}
	}
	return blocks
}

// writeFits appends to block the test that every index check reads within its
// container while the counter stays below the loop's limit, and returns it.
func (w *versionWriter) writeFits(block *Block, check boundedCheck) Value {
	length := w.fresh("len", "i64")
	op := "array.len"
	if check.view {
		op = "slice.len"
	}
	container := w.hoist(block, check.container)
	block.Instrs = append(block.Instrs,
		&Instr{Result: length, Op: op, Args: []Value{container}})
	room := length
	if check.base.Name != "" {
		room = w.fresh("room", "i64")
		base := w.hoist(block, check.base)
		block.Instrs = append(block.Instrs, &Instr{Result: room, Op: "binary.-",
			Args: []Value{length, base}})
	}
	fits := w.fresh("fits", "bool")
	limit := w.hoist(block, w.loop.limit)
	block.Instrs = append(block.Instrs, &Instr{Result: fits, Op: "binary.<=",
		Args: []Value{limit, room}})
	return fits
}

// hoist returns value as the test before the loop reads it: as it is when the
// loop does not define it, or recomputed there, once, from what the loop
// computes it from.
func (w *versionWriter) hoist(block *Block, value Value) Value {
	loop := w.loop
	if !loop.defined[value.Name] {
		return value
	}
	if known, ok := w.hoisted[value.Name]; ok {
		return known
	}
	instr := loop.defs[value.Name]
	copied := *instr
	copied.Result = Value{
		Name: "%" + w.prefix + "pre." + strings.TrimPrefix(value.Name, "%"),
		Type: value.Type,
	}
	copied.Args = make([]Value, len(instr.Args))
	for index, arg := range instr.Args {
		copied.Args[index] = w.hoist(block, arg)
	}
	block.Instrs = append(block.Instrs, &copied)
	w.hoisted[value.Name] = copied.Result
	return copied.Result
}

// redirectHeaderPhis gives the original header's phis the incoming they took
// from the preheader from each block of the test that falls back to them.
func (loop *versionedLoop) redirectHeaderPhis(header *Block, pre string, checks []*Block) {
	for _, instr := range header.Instrs {
		if instr.Op != "phi" {
			continue
		}
		incoming := []Incoming{}
		for _, edge := range instr.Incoming {
			if edge.Block != pre {
				incoming = append(incoming, edge)
				continue
			}
			for _, check := range checks {
				if check.Terminator.Op == "branch" {
					incoming = append(incoming, Incoming{Block: check.Name, Value: edge.Value})
				}
			}
		}
		instr.Incoming = incoming
	}
}

// renamed returns the name the copy gives a value the loop defines.
func (w *versionWriter) renamed(value Value) Value {
	if !w.loop.defined[value.Name] {
		return value
	}
	return Value{Name: "%" + w.prefix + strings.TrimPrefix(value.Name, "%"), Type: value.Type}
}

// copyLoop returns the copy of the loop's blocks, entered from the last block
// of the test in place of the preheader, with its answered checks left out.
func (w *versionWriter) copyLoop(entry string, pre string) []*Block {
	loop := w.loop
	copies := []*Block{}
	site := 0
	for _, block := range loop.fn.Blocks {
		if !loop.inside[block.Name] {
			continue
		}
		copied := &Block{Name: w.prefix + block.Name}
		for _, instr := range block.Instrs {
			if loop.answered[instr] && instr.Op == "cond_fail" {
				continue
			}
			clone := w.copyInstr(instr, entry, pre)
			if loop.answered[instr] {
				site++
				present := map[string]bool{}
				copied.Instrs = append(copied.Instrs,
					answeredAccess(clone, fmt.Sprintf("%%%sunchecked%d.", w.prefix, site), present)...)
				continue
			}
			copied.Instrs = append(copied.Instrs, clone)
		}
		copied.Terminator = w.copyTerminator(block.Terminator)
		copies = append(copies, copied)
	}
	return copies
}

// copyInstr copies one instruction under the copy's names.
func (w *versionWriter) copyInstr(instr *Instr, entry string, pre string) *Instr {
	copied := *instr
	copied.Result = w.renamed(instr.Result)
	copied.Args = make([]Value, len(instr.Args))
	for index, arg := range instr.Args {
		copied.Args[index] = w.renamed(arg)
	}
	copied.Fields = make([]FieldArg, len(instr.Fields))
	for index, field := range instr.Fields {
		copied.Fields[index] = FieldArg{Name: field.Name, Value: w.renamed(field.Value)}
	}
	copied.Incoming = make([]Incoming, len(instr.Incoming))
	for index, incoming := range instr.Incoming {
		block := incoming.Block
		switch {
		case block == pre:
			block = entry
		case w.loop.inside[block]:
			block = w.prefix + block
		}
		copied.Incoming[index] = Incoming{Block: block, Value: w.renamed(incoming.Value)}
	}
	copied.Cleanups = make([]Cleanup, len(instr.Cleanups))
	for index, cleanup := range instr.Cleanups {
		args := make([]Value, len(cleanup.Args))
		for argIndex, arg := range cleanup.Args {
			args[argIndex] = w.renamed(arg)
		}
		cleanup.Args = args
		copied.Cleanups[index] = cleanup
	}
	copied.CallParams = append([]Param(nil), instr.CallParams...)
	return &copied
}

// copyTerminator copies a terminator under the copy's names.
func (w *versionWriter) copyTerminator(term Terminator) Terminator {
	copied := Terminator{
		Op:     term.Op,
		Value:  w.renamed(term.Value),
		Cond:   w.renamed(term.Cond),
		Target: term.Target,
		Else:   term.Else,
	}
	if w.loop.inside[term.Target] {
		copied.Target = w.prefix + term.Target
	}
	if w.loop.inside[term.Else] {
		copied.Else = w.prefix + term.Else
	}
	return copied
}

// extendExitPhis gives each phi of the exit the incoming from the copy's
// header that it takes from the header, under the copy's names.
func (w *versionWriter) extendExitPhis(exit *Block, header string, copyHeader string) {
	for _, instr := range exit.Instrs {
		if instr.Op != "phi" {
			continue
		}
		for _, incoming := range instr.Incoming {
			if incoming.Block == header {
				instr.Incoming = append(instr.Incoming,
					Incoming{Block: copyHeader, Value: w.renamed(incoming.Value)})
				break
			}
		}
	}
}

// joinExit makes the exit a join of the two loops: each phi there takes from
// the copy's header what it took from the header, and a read after the loop
// of a value the header defines reads a phi of both.
func (loop *versionedLoop) joinExit(prefix string, copyHeader string) {
	fn := loop.fn
	header := fn.Blocks[loop.header]
	w := &versionWriter{loop: loop, prefix: prefix}
	var exit *Block
	for _, block := range fn.Blocks {
		if block.Name == loop.exit {
			exit = block
		}
	}
	w.extendExitPhis(exit, header.Name, copyHeader)
	joins := map[string]Value{}
	added := []*Instr{}
	join := func(value Value) Value {
		if !loop.defined[value.Name] {
			return value
		}
		if phi, ok := joins[value.Name]; ok {
			return phi
		}
		phi := &Instr{
			Result: Value{Name: "%" + prefix + "x." + strings.TrimPrefix(value.Name, "%"), Type: value.Type},
			Op:     "phi",
			Incoming: []Incoming{
				{Block: header.Name, Value: value},
				{Block: copyHeader, Value: w.renamed(value)},
			},
		}
		joins[value.Name] = phi.Result
		added = append(added, phi)
		return phi.Result
	}
	for _, block := range fn.Blocks {
		if loop.inside[block.Name] {
			continue
		}
		for _, instr := range block.Instrs {
			if instr.Op == "phi" {
				for index, incoming := range instr.Incoming {
					if !loop.inside[incoming.Block] && incoming.Block != copyHeader {
						instr.Incoming[index].Value = join(incoming.Value)
					}
				}
				continue
			}
			renameInstrReads(instr, join)
		}
		block.Terminator.Value = join(block.Terminator.Value)
		block.Terminator.Cond = join(block.Terminator.Cond)
	}
	exit.Instrs = append(added, exit.Instrs...)
}
