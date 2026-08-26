package ir

import (
	"fmt"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
)

type matchArmResult struct {
	block   string
	env     *env
	value   Value
	reaches bool
}

type matchSubject struct {
	value      Value
	unionValue Value
	enum       Enum
	union      Union
}

// lowerMatchStmt lowers a checked enum match statement to a branch chain.
func (l *lowerer) lowerMatchStmt(stmt *ast.MatchStmt) error {
	subject, err := l.lowerMatchValue(stmt.Value)
	if err != nil {
		return err
	}
	return l.lowerMatchBody(subject, stmt)
}

// lowerMatchBody lowers the arms of a match whose value is already lowered. A
// `comptime match` enters here because the value is what names the variants
// its arms are built from, so it is lowered before the arms exist.
func (l *lowerer) lowerMatchBody(subject matchSubject, stmt *ast.MatchStmt) error {
	saved := l.env
	end := l.newBlock(l.nextBlockName("match.end"))
	results, err := l.lowerMatchArms(subject, stmt, end.Name, saved, false)
	if err != nil {
		return err
	}
	l.block = end
	l.env = l.mergeMatchEnvs(results, saved)
	if !matchHasReachableArm(results) {
		l.block.Terminator = Terminator{Op: "unreachable"}
	}
	return nil
}

// lowerMatchExpr lowers a checked enum match expression to a value phi.
func (l *lowerer) lowerMatchExpr(stmt *ast.MatchStmt) (Value, error) {
	subject, err := l.lowerMatchValue(stmt.Value)
	if err != nil {
		return Value{}, err
	}
	saved := l.env
	end := l.newBlock(l.nextBlockName("match.end"))
	results, err := l.lowerMatchArms(subject, stmt, end.Name, saved, true)
	if err != nil {
		return Value{}, err
	}
	l.block = end
	l.env = saved
	incoming := []Incoming{}
	resultType := ""
	for _, result := range results {
		if !result.reaches {
			continue
		}
		if resultType == "" {
			resultType = result.value.Type
		}
		incoming = append(incoming, Incoming{Block: result.block, Value: result.value})
	}
	if len(incoming) == 0 {
		return Value{}, fmt.Errorf("ir error: match expression has no value arms")
	}
	return l.addPhi(end, resultType, incoming), nil
}

// lowerMatchValue lowers and validates the enum or union value used by a match.
func (l *lowerer) lowerMatchValue(expr ast.Expression) (matchSubject, error) {
	value, err := l.lowerExpr(expr)
	if err != nil {
		return matchSubject{}, err
	}
	enumType, ok := l.module.Enums[value.Type]
	if !ok {
		enumType, ok = l.module.ErrorSets[value.Type]
	}
	if ok {
		return matchSubject{value: value, enum: enumType}, nil
	}
	unionType, ok := l.module.Unions[value.Type]
	if ok {
		tag := l.emit("union.tag", "i64", []Value{value}, "")
		return matchSubject{value: tag, unionValue: value, union: unionType}, nil
	}
	if isReferenceType(value.Type) {
		unionName := derefType(value.Type)
		if unionType, ok := l.module.Unions[unionName]; ok {
			unionValue := l.emit("union.load", unionName, []Value{value}, "")
			tag := l.emit("union.tag", "i64", []Value{unionValue}, "")
			return matchSubject{value: tag, unionValue: unionValue, union: unionType}, nil
		}
	}
	return matchSubject{}, fmt.Errorf("ir error: match lowering supports enums and unions, got `%s`",
		value.Type)
}

// lowerMatchArms emits check and arm blocks for one enum match.
func (l *lowerer) lowerMatchArms(
	subject matchSubject,
	stmt *ast.MatchStmt,
	endLabel string,
	saved *env,
	wantValue bool,
) ([]matchArmResult, error) {
	arms := stmt.Arms
	armVariants, err := l.matchArmVariants(stmt)
	if err != nil {
		return nil, err
	}
	results := []matchArmResult{}
	check := l.newBlock(l.nextBlockName("match.check"))
	l.block.Terminator = Terminator{Op: "jump", Target: check.Name}
	for index, arm := range arms {
		armBlock := l.newBlock(l.nextBlockName("match.arm"))
		var nextCheck *Block
		if index+1 < len(arms) {
			nextCheck = l.newBlock(l.nextBlockName("match.check"))
		}
		l.block = check
		if err := l.lowerMatchCheck(subject, arm, armBlock, nextCheck); err != nil {
			return nil, err
		}
		restore := l.bindMetaField(stmt.MetaCapture, armVariants[arm.Tag])
		result, err := l.lowerMatchArmBody(subject, arm, armBlock, endLabel, saved, wantValue)
		restore()
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		if nextCheck != nil {
			check = nextCheck
		}
	}
	return results, nil
}

// lowerMatchCheck emits the condition for one enum match arm.
func (l *lowerer) lowerMatchCheck(
	subject matchSubject,
	arm ast.MatchArm,
	armBlock *Block,
	nextCheck *Block,
) error {
	if arm.IsWildcard() {
		l.block.Terminator = Terminator{Op: "jump", Target: armBlock.Name}
		return nil
	}
	index, err := l.matchTagIndex(subject, arm)
	if err != nil {
		return err
	}
	tag := Value{Name: strconv.Itoa(index), Type: subject.value.Type}
	cond := l.emit("binary.==", "bool", []Value{subject.value, tag}, "")
	var elseLabel string
	if nextCheck != nil {
		elseLabel = nextCheck.Name
	} else {
		unreachable := l.newBlock(l.nextBlockName("match.unreachable"))
		unreachable.Terminator = Terminator{Op: "unreachable"}
		elseLabel = unreachable.Name
	}
	l.block.Terminator = Terminator{Op: "branch", Cond: cond, Target: armBlock.Name, Else: elseLabel}
	return nil
}

// matchTagIndex resolves an enum or union match tag. An error match arm may
// qualify its declaring set (`FsError::NotFound =>`), and a bare arm on a
// combined set resolves through the sets it combines — the checker keeps a
// bare arm unambiguous, so the first declaring set that knows the name is it.
func (l *lowerer) matchTagIndex(subject matchSubject, arm ast.MatchArm) (int, error) {
	tag := arm.Tag
	if arm.TagSet != "" {
		origin, ok := l.module.ErrorSets[arm.TagSet]
		if !ok {
			return 0, fmt.Errorf("ir error: unknown error set `%s`", arm.TagSet)
		}
		index, ok := origin.Tags[tag]
		if !ok {
			return 0, fmt.Errorf("ir error: unknown error `%s::%s`", arm.TagSet, tag)
		}
		return index, nil
	}
	if subject.enum.Name != "" {
		if index, ok := subject.enum.Tags[tag]; ok {
			return index, nil
		}
		for _, origin := range subject.enum.Origins {
			if index, ok := l.module.ErrorSets[origin].Tags[tag]; ok {
				return index, nil
			}
		}
		return 0, fmt.Errorf("ir error: unknown enum tag `%s::%s`", subject.enum.Name, tag)
	}
	variant, ok := subject.union.Variants[tag]
	if !ok {
		return 0, fmt.Errorf("ir error: unknown union tag `%s::%s`", subject.union.Name, tag)
	}
	return variant.Index, nil
}

// lowerMatchArmBody lowers one arm in an isolated environment.
func (l *lowerer) lowerMatchArmBody(
	subject matchSubject,
	arm ast.MatchArm,
	block *Block,
	endLabel string,
	saved *env,
	wantValue bool,
) (matchArmResult, error) {
	l.env = saved.clone()
	l.block = block
	unbindPayload := l.bindMatchPayload(subject, arm)
	var value Value
	var err error
	if wantValue {
		value, err = l.lowerMatchArmValue(arm.Body)
	} else {
		err = l.lowerStmt(arm.Body)
	}
	if err != nil {
		return matchArmResult{}, err
	}
	unbindPayload()
	result := matchArmResult{block: l.block.Name, env: l.env, value: value}
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "jump", Target: endLabel}
		result.reaches = true
	}
	l.env = saved
	return result, nil
}

// bindMatchPayload binds a union payload for `Tag(name)` arms and returns a
// function that takes the binding back out of scope, the way a block does with
// its declarations. A binding left in the arm environment reaches the merge,
// where a later match would phi over a value its own arms never defined.
func (l *lowerer) bindMatchPayload(subject matchSubject, arm ast.MatchArm) func() {
	if arm.Binding == "" || arm.IsWildcard() || subject.union.Name == "" {
		return func() {}
	}
	variant, ok := subject.union.Variants[arm.Tag]
	if !ok || variant.Payload == "" {
		return func() {}
	}
	previous, bound := l.env.get(arm.Binding)
	l.bindCapture(arm.Binding, l.emit(
		"union.payload",
		variant.Payload,
		[]Value{subject.unionValue},
		variant.Name,
	))
	return func() {
		if bound {
			l.env.set(arm.Binding, previous)
			return
		}
		l.env.remove(arm.Binding)
	}
}

// lowerMatchArmValue lowers the value of a match expression arm: a block with
// a trailing expression, or a bare expression.
func (l *lowerer) lowerMatchArmValue(stmt ast.Statement) (Value, error) {
	if block, ok := stmt.(*ast.BlockStmt); ok {
		return l.lowerBlockBody(block, true)
	}
	expr, ok := statementValue(stmt)
	if !ok {
		return Value{}, fmt.Errorf("ir error: match expression arms must be expressions")
	}
	return l.lowerExpr(expr)
}

// mergeMatchEnvs creates phi nodes for bindings changed by reachable arms.
func (l *lowerer) mergeMatchEnvs(results []matchArmResult, fallback *env) *env {
	incomingResults := []matchArmResult{}
	for _, result := range results {
		if result.reaches {
			incomingResults = append(incomingResults, result)
		}
	}
	if len(incomingResults) == 0 {
		return fallback
	}
	merged := incomingResults[0].env.clone()
	for _, name := range fallback.names() {
		first := armValue(incomingResults[0], name, fallback)
		allSame := true
		incoming := make([]Incoming, 0, len(incomingResults))
		for _, result := range incomingResults {
			value := armValue(result, name, fallback)
			if !sameValue(first, value) {
				allSame = false
			}
			incoming = append(incoming, Incoming{Block: result.block, Value: value})
		}
		if !allSame {
			merged.set(name, l.addPhi(l.block, first.Type, incoming))
		}
	}
	return merged
}

// armValue returns what name holds when an arm reaches the merge. An arm that
// never touched the name leaves it at what the match started from.
func armValue(result matchArmResult, name string, fallback *env) Value {
	if value, ok := result.env.get(name); ok {
		return value
	}
	value, _ := fallback.get(name)
	return value
}

// matchHasReachableArm reports whether any arm jumps to the match merge block.
func matchHasReachableArm(results []matchArmResult) bool {
	for _, result := range results {
		if result.reaches {
			return true
		}
	}
	return false
}
