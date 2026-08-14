package ir

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
)

type branchResult struct {
	env       *env
	end       string
	reachable bool
	value     Value
}

// lowerIfStmt lowers if/else into branches and a merge block.
func (l *lowerer) lowerIfStmt(stmt *ast.IfStmt) error {
	cond, err := l.lowerExpr(stmt.Condition)
	if err != nil {
		return err
	}
	thenBlock := l.newBlock(l.nextBlockName("if.then"))
	elseBlock := l.newBlock(l.nextBlockName("if.else"))
	mergeBlock := l.newBlock(l.nextBlockName("if.end"))
	l.block.Terminator = Terminator{
		Op: "branch", Cond: cond, Target: thenBlock.Name, Else: elseBlock.Name,
	}
	thenResult, err := l.lowerBranchBlock(thenBlock, stmt.Consequence, mergeBlock.Name, false)
	if err != nil {
		return err
	}
	elseBody := stmt.Alternative
	if elseBody == nil {
		elseBody = &ast.BlockStmt{}
	}
	elseResult, err := l.lowerBranchBlock(elseBlock, elseBody, mergeBlock.Name, false)
	if err != nil {
		return err
	}
	l.block = mergeBlock
	switch {
	case thenResult.reachable && elseResult.reachable:
		l.env = l.mergeEnvs(mergeBlock, thenResult.end, thenResult.env, elseResult.end, elseResult.env)
	case thenResult.reachable:
		l.env = thenResult.env
	case elseResult.reachable:
		l.env = elseResult.env
	default:
		mergeBlock.Terminator = Terminator{Op: "unreachable"}
	}
	return nil
}

// lowerLogicalExpr lowers short-circuit boolean operators into control flow.
func (l *lowerer) lowerLogicalExpr(expr *ast.BinaryExpr) (Value, error) {
	left, err := l.lowerExpr(expr.Left)
	if err != nil {
		return Value{}, err
	}
	rightBlock := l.newBlock(l.nextBlockName("logical.right"))
	constBlock := l.newBlock(l.nextBlockName("logical.const"))
	mergeBlock := l.newBlock(l.nextBlockName("logical.end"))
	if expr.Operator == "and" {
		l.block.Terminator = Terminator{
			Op: "branch", Cond: left, Target: rightBlock.Name, Else: constBlock.Name,
		}
	} else {
		l.block.Terminator = Terminator{
			Op: "branch", Cond: left, Target: constBlock.Name, Else: rightBlock.Name,
		}
	}
	rightValue, rightEnd, err := l.lowerLogicalBranch(rightBlock, expr.Right, mergeBlock.Name)
	if err != nil {
		return Value{}, err
	}
	constValue, constEnd := l.lowerLogicalConst(constBlock, expr.Operator, mergeBlock.Name)
	l.block = mergeBlock
	return l.addPhi(mergeBlock, "bool", []Incoming{
		{Block: rightEnd, Value: rightValue},
		{Block: constEnd, Value: constValue},
	}), nil
}

// lowerLogicalBranch lowers the RHS of a short-circuit operator.
func (l *lowerer) lowerLogicalBranch(
	block *Block,
	expr ast.Expression,
	target string,
) (Value, string, error) {
	l.block = block
	value, err := l.lowerExpr(expr)
	if err != nil {
		return Value{}, "", err
	}
	end := l.block.Name
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "jump", Target: target}
	}
	return value, end, nil
}

// lowerLogicalConst lowers the branch that skips the RHS.
func (l *lowerer) lowerLogicalConst(block *Block, op string, target string) (Value, string) {
	l.block = block
	immediate := "true"
	if op == "and" {
		immediate = "false"
	}
	value := l.emitConst("bool", immediate)
	end := l.block.Name
	l.block.Terminator = Terminator{Op: "jump", Target: target}
	return value, end
}

// lowerBranchBlock lowers a branch with an isolated environment.
func (l *lowerer) lowerBranchBlock(
	block *Block,
	body *ast.BlockStmt,
	target string,
	wantValue bool,
) (branchResult, error) {
	saved := l.env
	l.env = saved.clone()
	l.block = block
	value, err := l.lowerBlockBody(body, wantValue)
	if err != nil {
		return branchResult{}, err
	}
	end := l.block.Name
	reachable := l.block.Terminator.Op == ""
	if reachable {
		l.block.Terminator = Terminator{Op: "jump", Target: target}
	}
	out := l.env
	l.env = saved
	return branchResult{env: out, end: end, reachable: reachable, value: value}, nil
}

// lowerIfExpr lowers an if used as a value. It is the branch lowering an if
// statement already does, ended by a phi over the value each side produced,
// which is how a match expression reaches its value too.
func (l *lowerer) lowerIfExpr(stmt *ast.IfStmt) (Value, error) {
	if stmt.Alternative == nil {
		return Value{}, fmt.Errorf("ir error: an if used as a value needs an else branch")
	}
	cond, err := l.lowerExpr(stmt.Condition)
	if err != nil {
		return Value{}, err
	}
	thenBlock := l.newBlock(l.nextBlockName("if.then"))
	elseBlock := l.newBlock(l.nextBlockName("if.else"))
	mergeBlock := l.newBlock(l.nextBlockName("if.end"))
	l.block.Terminator = Terminator{
		Op: "branch", Cond: cond, Target: thenBlock.Name, Else: elseBlock.Name,
	}
	thenResult, err := l.lowerBranchBlock(thenBlock, stmt.Consequence, mergeBlock.Name, true)
	if err != nil {
		return Value{}, err
	}
	elseResult, err := l.lowerBranchBlock(elseBlock, stmt.Alternative, mergeBlock.Name, true)
	if err != nil {
		return Value{}, err
	}
	l.block = mergeBlock
	switch {
	case thenResult.reachable && elseResult.reachable:
		l.env = l.mergeEnvs(mergeBlock, thenResult.end, thenResult.env, elseResult.end, elseResult.env)
	case thenResult.reachable:
		l.env = thenResult.env
	case elseResult.reachable:
		l.env = elseResult.env
	}
	incoming := []Incoming{}
	resultType := ""
	for _, result := range []branchResult{thenResult, elseResult} {
		if !result.reachable {
			continue
		}
		if resultType == "" {
			resultType = result.value.Type
		}
		incoming = append(incoming, Incoming{Block: result.end, Value: result.value})
	}
	if len(incoming) == 0 {
		mergeBlock.Terminator = Terminator{Op: "unreachable"}
		return Value{}, fmt.Errorf("ir error: an if used as a value has no branch that produces one")
	}
	return l.addPhi(mergeBlock, resultType, incoming), nil
}

// mergeEnvs creates phi nodes for values that differ across branches.
func (l *lowerer) mergeEnvs(
	block *Block,
	leftBlock string,
	left *env,
	rightBlock string,
	right *env,
) *env {
	merged := left.clone()
	for _, name := range right.names() {
		rightValue, _ := right.get(name)
		leftValue, ok := left.get(name)
		if !ok {
			merged.set(name, rightValue)
			continue
		}
		if sameValue(leftValue, rightValue) {
			continue
		}
		phi := l.addPhi(block, leftValue.Type, []Incoming{
			{Block: leftBlock, Value: leftValue},
			{Block: rightBlock, Value: rightValue},
		})
		merged.set(name, phi)
	}
	return merged
}

// A loopShape is the part of a loop that `while` and `for` do not share: what
// the header tests, and what runs on every way back to it.
//
// A loop with no advance reaches its header directly, so `continue` jumps
// there. A loop with an advance reaches its header through a latch block that
// runs the advance, and `continue` jumps to the latch instead. That is the one
// difference the phi work has to know about: with a latch, the header is
// reached from a single block, so the values arriving from the body and from
// each `continue` are merged in the latch rather than handed to the header
// phis one by one.
type loopShape struct {
	name    string
	label   string
	body    *ast.BlockStmt
	header  func(preheader string) (Value, error)
	advance func(latch *Block)
}

// lowerLoop lowers one loop. It is the only place a loop's header phis are
// made and closed, so what a name holds around the loop is decided once for
// every loop form rather than once per form.
func (l *lowerer) lowerLoop(shape loopShape) error {
	assigned, err := assignedNames(shape.body)
	if err != nil {
		return err
	}
	preheader := l.block
	header := l.newBlock(l.nextBlockName(shape.name + ".header"))
	body := l.newBlock(l.nextBlockName(shape.name + ".body"))
	var latch *Block
	if shape.advance != nil {
		latch = l.newBlock(l.nextBlockName(shape.name + ".step"))
	}
	exit := l.newBlock(l.nextBlockName(shape.name + ".end"))
	preheader.Terminator = Terminator{Op: "jump", Target: header.Name}
	phis := l.createLoopPhis(header, assigned)
	l.block = header
	cond, err := shape.header(preheader.Name)
	if err != nil {
		return err
	}
	// l.block, not header: a short-circuit condition splits the test across
	// blocks of its own, and the branch belongs to the one it ended in. That
	// block, not the header, is also the one the exit is reached from.
	test := l.block
	test.Terminator = Terminator{Op: "branch", Cond: cond, Target: body.Name, Else: exit.Name}
	back := header
	if latch != nil {
		back = latch
	}
	l.block = body
	loop := l.pushLoop(shape.label, exit.Name, back.Name)
	if err := l.lowerBlock(shape.body); err != nil {
		return err
	}
	l.popLoop()
	edges := loop.continueEdges
	if l.block.Terminator.Op == "" {
		// The body falling through is one more edge back into the loop, and it
		// comes first because it is the one the source reads as the loop.
		// A copy, because closeLoopPhis rebinds the same names in l.env while
		// it reads what the body left them at.
		fallthroughEdge := loopEdge{block: l.block.Name, env: l.env.clone()}
		edges = append([]loopEdge{fallthroughEdge}, edges...)
		l.block.Terminator = Terminator{Op: "jump", Target: back.Name}
	}
	if latch != nil {
		l.block = latch
	}
	// Before the advance, so a merge phi stays at the top of the latch.
	l.closeLoopPhis(phis, latch, edges)
	if latch != nil {
		shape.advance(latch)
		latch.Terminator = Terminator{Op: "jump", Target: header.Name}
	}
	l.block = exit
	l.finishLoopExitPhis(exit, phis, test.Name, loop.breakEdges)
	return nil
}

// lowerWhileStmt lowers a loop that tests a condition and goes straight back.
func (l *lowerer) lowerWhileStmt(stmt *ast.WhileStmt) error {
	return l.lowerLoop(loopShape{
		name:  "while",
		label: stmt.Label,
		body:  stmt.Body,
		header: func(string) (Value, error) {
			return l.lowerExpr(stmt.Condition)
		},
	})
}

// lowerForStmt lowers a bounded i64 range loop. The index is a header phi of
// its own, and the advance that closes it is why the loop needs a latch.
func (l *lowerer) lowerForStmt(stmt *ast.ForStmt) error {
	start, err := l.lowerExpr(stmt.Start)
	if err != nil {
		return err
	}
	end, err := l.lowerExpr(stmt.End)
	if err != nil {
		return err
	}
	previous, hadPrevious := l.env.get(stmt.Name)
	defer l.restoreLoopVar(stmt.Name, previous, hadPrevious)
	var index *Instr
	return l.lowerLoop(loopShape{
		name:  "for",
		label: stmt.Label,
		body:  stmt.Body,
		header: func(preheader string) (Value, error) {
			index = l.createForIndexPhi(l.block, preheader, start)
			l.env.set(stmt.Name, index.Result)
			return l.emit("binary.<", "bool", []Value{index.Result, end}, ""), nil
		},
		advance: func(latch *Block) {
			one := l.emitConst("i64", "1")
			next := l.emit("binary.+", "i64", []Value{index.Result, one}, "")
			index.Incoming = append(index.Incoming, Incoming{Block: latch.Name, Value: next})
		},
	})
}

// createForIndexPhi creates the SSA loop variable for a for range.
func (l *lowerer) createForIndexPhi(block *Block, incoming string, start Value) *Instr {
	phi := &Instr{
		Result:   l.next("i64"),
		Op:       "phi",
		Incoming: []Incoming{{Block: incoming, Value: start}},
	}
	block.Instrs = append(block.Instrs, phi)
	return phi
}

// restoreLoopVar removes the scoped for variable after lowering the loop.
func (l *lowerer) restoreLoopVar(name string, previous Value, hadPrevious bool) {
	if hadPrevious {
		l.env.set(name, previous)
		return
	}
	l.env.remove(name)
}

// lowerLoopBranch lowers break and continue as jumps to loop blocks.
func (l *lowerer) lowerLoopBranch(kind string, label string) error {
	loop, ok := l.findLoop(label)
	if !ok {
		return fmt.Errorf("ir error: unknown loop label `%s`", label)
	}
	target := loop.breakTo
	if kind == "continue" {
		target = loop.continueTo
		loop.continueEdges = append(loop.continueEdges, loopEdge{
			block: l.block.Name,
			env:   l.env.clone(),
		})
	} else {
		loop.breakEdges = append(loop.breakEdges, loopEdge{
			block: l.block.Name,
			env:   l.env.clone(),
		})
	}
	l.emitCleanups(l.cleanupsFrom(loop.deferDepth, false))
	l.block.Terminator = Terminator{Op: "jump", Target: target}
	return nil
}

// pushLoop records the current loop branch targets.
func (l *lowerer) pushLoop(label string, breakTo string, continueTo string) *loopContext {
	loop := &loopContext{
		label: label, breakTo: breakTo, continueTo: continueTo, deferDepth: len(l.deferFrames),
	}
	l.loops = append(l.loops, loop)
	return loop
}

// popLoop removes the innermost loop branch target.
func (l *lowerer) popLoop() {
	l.loops = l.loops[:len(l.loops)-1]
}

// findLoop resolves an unlabeled or labeled loop branch target.
func (l *lowerer) findLoop(label string) (*loopContext, bool) {
	for idx := len(l.loops) - 1; idx >= 0; idx-- {
		loop := l.loops[idx]
		if label == "" || loop.label == label {
			return loop, true
		}
	}
	return nil, false
}

// createLoopPhis creates header phi nodes for assigned loop variables.
func (l *lowerer) createLoopPhis(block *Block, assigned []string) []loopPhi {
	phis := []loopPhi{}
	for _, name := range assigned {
		value, ok := l.env.get(name)
		if !ok {
			continue
		}
		phi := &Instr{
			Result:   l.next(value.Type),
			Op:       "phi",
			Incoming: []Incoming{{Block: l.block.Name, Value: value}},
		}
		block.Instrs = append(block.Instrs, phi)
		l.env.set(name, phi.Result)
		phis = append(phis, loopPhi{name: name, phi: phi})
	}
	return phis
}

// closeLoopPhis adds the back edge to each header phi. With no latch the
// header is reached from every edge directly, so each one is its own incoming.
// With a latch the header is reached only from there, so the edges are merged
// in the latch first.
func (l *lowerer) closeLoopPhis(phis []loopPhi, latch *Block, edges []loopEdge) {
	for _, entry := range phis {
		incoming := loopIncoming(entry.name, edges)
		if latch == nil {
			entry.phi.Incoming = append(entry.phi.Incoming, incoming...)
		} else {
			entry.phi.Incoming = append(entry.phi.Incoming, Incoming{
				Block: latch.Name,
				Value: l.latchValue(latch, entry, incoming),
			})
		}
		l.env.set(entry.name, entry.phi.Result)
	}
}

// loopIncoming returns what name holds on each edge back into the loop.
func loopIncoming(name string, edges []loopEdge) []Incoming {
	incoming := make([]Incoming, 0, len(edges))
	for _, edge := range edges {
		value, ok := edge.env.get(name)
		if ok {
			incoming = append(incoming, Incoming{Block: edge.block, Value: value})
		}
	}
	return incoming
}

// latchValue returns what a name holds where the latch starts. One edge
// arrives with one value and needs nothing; several need a phi. None at all
// means nothing reaches the latch, and the back edge reads the header phi.
func (l *lowerer) latchValue(latch *Block, entry loopPhi, incoming []Incoming) Value {
	switch len(incoming) {
	case 0:
		return entry.phi.Result
	case 1:
		return incoming[0].Value
	default:
		return l.addPhi(latch, entry.phi.Result.Type, incoming)
	}
}

// finishLoopExitPhis merges condition-false exits with explicit break edges.
// test is the block the condition ended in, which is the one the false edge
// leaves from -- the header only when the condition did not split.
func (l *lowerer) finishLoopExitPhis(
	exit *Block,
	phis []loopPhi,
	test string,
	breakEdges []loopEdge,
) {
	if len(breakEdges) == 0 {
		return
	}
	for _, entry := range phis {
		incoming := []Incoming{{Block: test, Value: entry.phi.Result}}
		for _, edge := range breakEdges {
			value, ok := edge.env.get(entry.name)
			if ok {
				incoming = append(incoming, Incoming{Block: edge.block, Value: value})
			}
		}
		if len(incoming) == 1 {
			continue
		}
		l.env.set(entry.name, l.addPhi(exit, entry.phi.Result.Type, incoming))
	}
}

// addPhi appends a phi node to block and returns its result.
func (l *lowerer) addPhi(block *Block, typ string, incoming []Incoming) Value {
	instr := &Instr{Result: l.next(typ), Op: "phi", Incoming: incoming}
	block.Instrs = append(block.Instrs, instr)
	return instr.Result
}

// assignedNames returns names assigned in a block, in the order they are first
// assigned. The order is the order the header phis are created in, so it comes
// from the source rather than from a set.
//
// An unknown node is an error rather than an empty answer, for the reason the
// slot walk makes it one too: a name missed here is a name the loop carries
// without a header phi, so every turn of the loop reads what it held before
// the loop ran.
func assignedNames(block *ast.BlockStmt) ([]string, error) {
	names := []string{}
	if err := collectAssigned(block, &names); err != nil {
		return nil, err
	}
	return names, nil
}

// collectAssigned records the names one statement assigns, walking the
// statements and the expressions it holds. Walking the expressions is what
// finds an assignment written inside an `if` or `match` used as a value.
func collectAssigned(stmt ast.Statement, names *[]string) error {
	if stmt == nil {
		return nil
	}
	if assign, isAssign := stmt.(*ast.AssignStmt); isAssign {
		if name, rooted := assignTargetRoot(assign.Target); rooted {
			addAssigned(names, name)
		}
	}
	exprs, stmts, known := statementChildren(stmt)
	if !known {
		return fmt.Errorf("ir error: loop analysis does not know statement `%T`", stmt)
	}
	for _, expr := range exprs {
		if err := collectAssignedExpr(expr, names); err != nil {
			return err
		}
	}
	for _, inner := range stmts {
		if err := collectAssigned(inner, names); err != nil {
			return err
		}
	}
	return nil
}

// collectAssignedExpr walks one expression for the assignments written inside
// it. `if` and `match` are both a statement and an expression, so in value
// position they are still walked as statements.
func collectAssignedExpr(expr ast.Expression, names *[]string) error {
	if expr == nil {
		return nil
	}
	switch expr.(type) {
	case *ast.IfStmt, *ast.MatchStmt:
		return collectAssigned(expr.(ast.Statement), names)
	}
	children, known := expressionChildren(expr)
	if !known {
		return fmt.Errorf("ir error: loop analysis does not know expression `%T`", expr)
	}
	for _, child := range children {
		if err := collectAssignedExpr(child, names); err != nil {
			return err
		}
	}
	return nil
}

// assignTargetRoot returns the binding an assignment target ultimately rebinds.
// A field of a value receiver rebuilds the receiver aggregate up to its root
// binding, so the root needs a loop header phi exactly like a direct assignment.
// A field of a borrowed receiver stores through the borrow and leaves the root
// SSA value alone; naming it here only adds a phi whose incoming values agree.
func assignTargetRoot(target ast.Expression) (string, bool) {
	for {
		switch t := target.(type) {
		case *ast.IdentExpr:
			return t.Name, true
		case *ast.FieldExpr:
			target = t.Receiver
		case *ast.DerefExpr:
			target = t.Receiver
		default:
			return "", false
		}
	}
}

// addAssigned records name once, keeping the order names are first assigned in.
func addAssigned(names *[]string, name string) {
	for _, existing := range *names {
		if existing == name {
			return
		}
	}
	*names = append(*names, name)
}

// sameValue reports whether two SSA value references are identical.
func sameValue(left Value, right Value) bool {
	return left.Name == right.Name && left.Type == right.Type
}
