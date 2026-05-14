package ir

import (
	"fmt"

	"tiny-safe/internal/ast"
)

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
	thenEnv, thenEnd, err := l.lowerBranchBlock(thenBlock, stmt.Consequence, mergeBlock.Name)
	if err != nil {
		return err
	}
	elseBody := stmt.Alternative
	if elseBody == nil {
		elseBody = &ast.BlockStmt{}
	}
	elseEnv, elseEnd, err := l.lowerBranchBlock(elseBlock, elseBody, mergeBlock.Name)
	if err != nil {
		return err
	}
	l.block = mergeBlock
	l.env = l.mergeEnvs(mergeBlock, thenEnd, thenEnv, elseEnd, elseEnv)
	return nil
}

// lowerBranchBlock lowers a branch with an isolated environment.
func (l *lowerer) lowerBranchBlock(
	block *Block,
	body *ast.BlockStmt,
	target string,
) (map[string]Value, string, error) {
	saved := l.copyEnv(l.env)
	l.env = l.copyEnv(saved)
	l.block = block
	if err := l.lowerBlock(body); err != nil {
		return nil, "", err
	}
	end := l.block.Name
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "jump", Target: target}
	}
	out := l.copyEnv(l.env)
	l.env = saved
	return out, end, nil
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
	header.Terminator = Terminator{Op: "branch", Cond: cond, Target: body.Name, Else: exit.Name}
	l.block = body
	l.pushLoop(stmt.Label, exit.Name, header.Name)
	if err := l.lowerBlock(stmt.Body); err != nil {
		return err
	}
	l.popLoop()
	bodyEnd := l.block.Name
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "jump", Target: header.Name}
	}
	l.finishLoopPhis(phis, bodyEnd)
	l.block = exit
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
	}
	l.block.Terminator = Terminator{Op: "jump", Target: target}
	return nil
}

// pushLoop records the current loop branch targets.
func (l *lowerer) pushLoop(label string, breakTo string, continueTo string) {
	l.loops = append(l.loops, loopContext{
		label: label, breakTo: breakTo, continueTo: continueTo,
	})
}

// popLoop removes the innermost loop branch target.
func (l *lowerer) popLoop() {
	l.loops = l.loops[:len(l.loops)-1]
}

// findLoop resolves an unlabeled or labeled loop branch target.
func (l *lowerer) findLoop(label string) (loopContext, bool) {
	for idx := len(l.loops) - 1; idx >= 0; idx-- {
		loop := l.loops[idx]
		if label == "" || loop.label == label {
			return loop, true
		}
	}
	return loopContext{}, false
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

// finishLoopPhis adds back-edge values to loop phi nodes.
func (l *lowerer) finishLoopPhis(phis map[string]*Instr, bodyEnd string) {
	for name, phi := range phis {
		value := l.env[name]
		phi.Incoming = append(phi.Incoming, Incoming{Block: bodyEnd, Value: value})
		l.env[name] = phi.Result
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
		if ident, ok := s.Target.(*ast.IdentExpr); ok {
			names[ident.Name] = true
		}
	case *ast.IfStmt:
		collectBlockAssigned(s.Consequence, names)
		if s.Alternative != nil {
			collectBlockAssigned(s.Alternative, names)
		}
	case *ast.WhileStmt:
		collectBlockAssigned(s.Body, names)
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
