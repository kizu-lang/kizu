package ir

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
)

type branchResult struct {
	env       map[string]Value
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
	saved := l.copyEnv(l.env)
	l.env = l.copyEnv(saved)
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
	out := l.copyEnv(l.env)
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
	left map[string]Value,
	rightBlock string,
	right map[string]Value,
) map[string]Value {
	merged := l.copyEnv(left)
	for name, rightValue := range right {
		leftValue, ok := left[name]
		if !ok {
			merged[name] = rightValue
			continue
		}
		if sameValue(leftValue, rightValue) {
			continue
		}
		phi := l.addPhi(block, leftValue.Type, []Incoming{
			{Block: leftBlock, Value: leftValue},
			{Block: rightBlock, Value: rightValue},
		})
		merged[name] = phi
	}
	return merged
}

// lowerWhileStmt lowers while into header, body, and exit blocks.
func (l *lowerer) lowerWhileStmt(stmt *ast.WhileStmt) error {
	assigned := assignedNames(stmt.Body)
	preheader := l.block
	header := l.newBlock(l.nextBlockName("while.header"))
	body := l.newBlock(l.nextBlockName("while.body"))
	exit := l.newBlock(l.nextBlockName("while.end"))
	preheader.Terminator = Terminator{Op: "jump", Target: header.Name}
	phis := l.createLoopPhis(header, assigned)
	l.block = header
	cond, err := l.lowerExpr(stmt.Condition)
	if err != nil {
		return err
	}
	l.block.Terminator = Terminator{Op: "branch", Cond: cond, Target: body.Name, Else: exit.Name}
	l.block = body
	loop := l.pushLoop(stmt.Label, exit.Name, header.Name)
	if err := l.lowerBlock(stmt.Body); err != nil {
		return err
	}
	l.popLoop()
	bodyEnd := ""
	bodyEnv := map[string]Value{}
	if l.block.Terminator.Op == "" {
		bodyEnd = l.block.Name
		bodyEnv = l.copyEnv(l.env)
		l.block.Terminator = Terminator{Op: "jump", Target: header.Name}
	}
	l.finishLoopPhis(phis, bodyEnd, bodyEnv, loop.continueEdges)
	l.block = exit
	l.finishLoopExitPhis(exit, phis, header.Name, loop.breakEdges)
	return nil
}

// lowerForStmt lowers a bounded i64 range loop.
func (l *lowerer) lowerForStmt(stmt *ast.ForStmt) error {
	start, err := l.lowerExpr(stmt.Start)
	if err != nil {
		return err
	}
	end, err := l.lowerExpr(stmt.End)
	if err != nil {
		return err
	}
	previous, hadPrevious := l.env[stmt.Name]
	defer l.restoreLoopVar(stmt.Name, previous, hadPrevious)
	preheader := l.block
	header := l.newBlock(l.nextBlockName("for.header"))
	body := l.newBlock(l.nextBlockName("for.body"))
	step := l.newBlock(l.nextBlockName("for.step"))
	exit := l.newBlock(l.nextBlockName("for.end"))
	l.block.Terminator = Terminator{Op: "jump", Target: header.Name}
	l.block = header
	index := l.createForIndexPhi(header, preheader.Name, start)
	l.env[stmt.Name] = index.Result
	cond := l.emit("binary.<", "bool", []Value{index.Result, end}, "")
	header.Terminator = Terminator{Op: "branch", Cond: cond, Target: body.Name, Else: exit.Name}
	l.block = body
	l.pushLoop(stmt.Label, exit.Name, step.Name)
	if err := l.lowerBlock(stmt.Body); err != nil {
		return err
	}
	l.popLoop()
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "jump", Target: step.Name}
	}
	l.block = step
	one := l.emitConst("i64", "1")
	next := l.emit("binary.+", "i64", []Value{index.Result, one}, "")
	step.Terminator = Terminator{Op: "jump", Target: header.Name}
	index.Incoming = append(index.Incoming, Incoming{Block: step.Name, Value: next})
	l.block = exit
	return nil
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
		l.env[name] = previous
		return
	}
	delete(l.env, name)
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
			env:   l.copyEnv(l.env),
		})
	} else {
		loop.breakEdges = append(loop.breakEdges, loopEdge{
			block: l.block.Name,
			env:   l.copyEnv(l.env),
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
func (l *lowerer) createLoopPhis(block *Block, assigned map[string]bool) map[string]*Instr {
	phis := map[string]*Instr{}
	for name := range assigned {
		value, ok := l.env[name]
		if !ok {
			continue
		}
		phi := &Instr{
			Result:   l.next(value.Type),
			Op:       "phi",
			Incoming: []Incoming{{Block: l.block.Name, Value: value}},
		}
		block.Instrs = append(block.Instrs, phi)
		l.env[name] = phi.Result
		phis[name] = phi
	}
	return phis
}

// finishLoopPhis adds fallthrough and explicit continue back-edges to loop phi nodes.
func (l *lowerer) finishLoopPhis(
	phis map[string]*Instr,
	bodyEnd string,
	bodyEnv map[string]Value,
	continueEdges []loopEdge,
) {
	for name, phi := range phis {
		if bodyEnd != "" {
			value := bodyEnv[name]
			phi.Incoming = append(phi.Incoming, Incoming{Block: bodyEnd, Value: value})
		}
		for _, edge := range continueEdges {
			value, ok := edge.env[name]
			if ok {
				phi.Incoming = append(phi.Incoming, Incoming{Block: edge.block, Value: value})
			}
		}
		l.env[name] = phi.Result
	}
}

// finishLoopExitPhis merges condition-false exits with explicit break edges.
func (l *lowerer) finishLoopExitPhis(
	exit *Block,
	phis map[string]*Instr,
	header string,
	breakEdges []loopEdge,
) {
	if len(breakEdges) == 0 {
		return
	}
	for name, phi := range phis {
		incoming := []Incoming{{Block: header, Value: phi.Result}}
		for _, edge := range breakEdges {
			value, ok := edge.env[name]
			if ok {
				incoming = append(incoming, Incoming{Block: edge.block, Value: value})
			}
		}
		if len(incoming) == 1 {
			continue
		}
		l.env[name] = l.addPhi(exit, phi.Result.Type, incoming)
	}
}

// addPhi appends a phi node to block and returns its result.
func (l *lowerer) addPhi(block *Block, typ string, incoming []Incoming) Value {
	instr := &Instr{Result: l.next(typ), Op: "phi", Incoming: incoming}
	block.Instrs = append(block.Instrs, instr)
	return instr.Result
}

// assignedNames returns names assigned in a block.
func assignedNames(block *ast.BlockStmt) map[string]bool {
	names := map[string]bool{}
	for _, stmt := range block.Statements {
		collectAssigned(stmt, names)
	}
	return names
}

// collectAssigned records assigned names in nested statements.
func collectAssigned(stmt ast.Statement, names map[string]bool) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if name, ok := assignTargetRoot(s.Target); ok {
			names[name] = true
		}
	case *ast.IfStmt:
		collectBlockAssigned(s.Consequence, names)
		if s.Alternative != nil {
			collectBlockAssigned(s.Alternative, names)
		}
	case *ast.WhileStmt:
		collectBlockAssigned(s.Body, names)
	case *ast.ForStmt:
		collectBlockAssigned(s.Body, names)
	case *ast.MatchStmt:
		for _, arm := range s.Arms {
			collectAssigned(arm.Body, names)
		}
	case *ast.UnsafeStmt:
		collectBlockAssigned(s.Body, names)
	case *ast.ComptimeIfStmt:
		collectBlockAssigned(s.Consequence, names)
		if s.Alternative != nil {
			collectBlockAssigned(s.Alternative, names)
		}
	case *ast.BlockStmt:
		collectBlockAssigned(s, names)
	}
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

// collectBlockAssigned records assignments inside a block.
func collectBlockAssigned(block *ast.BlockStmt, names map[string]bool) {
	for _, stmt := range block.Statements {
		collectAssigned(stmt, names)
	}
}

// sameValue reports whether two SSA value references are identical.
func sameValue(left Value, right Value) bool {
	return left.Name == right.Name && left.Type == right.Type
}

// copyEnv clones the current name-to-value environment.
func (l *lowerer) copyEnv(env map[string]Value) map[string]Value {
	out := make(map[string]Value, len(env))
	for name, value := range env {
		out[name] = value
	}
	return out
}
