package ir

import (
	"fmt"
	"strconv"

	"github.com/kizu-lang/kizu/internal/ast"
)

type matchArmResult struct {
	block   string
	env     map[string]Value
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
	saved := l.copyEnv(l.env)
	end := l.newBlock(l.nextBlockName("match.end"))
	results, err := l.lowerMatchArms(subject, stmt.Arms, end.Name, saved, false)
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
	saved := l.copyEnv(l.env)
	end := l.newBlock(l.nextBlockName("match.end"))
	results, err := l.lowerMatchArms(subject, stmt.Arms, end.Name, saved, true)
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
	arms []ast.MatchArm,
	endLabel string,
	saved map[string]Value,
	wantValue bool,
) ([]matchArmResult, error) {
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
		if err := l.lowerMatchCheck(subject, arm, armBlock, nextCheck, endLabel); err != nil {
			return nil, err
		}
		result, err := l.lowerMatchArmBody(subject, arm, armBlock, endLabel, saved, wantValue)
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
	endLabel string,
) error {
	if arm.IsWildcard() {
		l.block.Terminator = Terminator{Op: "jump", Target: armBlock.Name}
		return nil
	}
	index, err := l.matchTagIndex(subject, arm.Tag)
	if err != nil {
		return err
	}
	tag := Value{Name: strconv.Itoa(index), Type: subject.value.Type}
	cond := l.emit("binary.==", "bool", []Value{subject.value, tag}, "")
	elseLabel := endLabel
	if nextCheck != nil {
		elseLabel = nextCheck.Name
	}
	l.block.Terminator = Terminator{Op: "branch", Cond: cond, Target: armBlock.Name, Else: elseLabel}
	return nil
}

// matchTagIndex resolves an enum or union match tag.
func (l *lowerer) matchTagIndex(subject matchSubject, tag string) (int, error) {
	if subject.enum.Name != "" {
		index, ok := subject.enum.Tags[tag]
		if !ok {
			return 0, fmt.Errorf("ir error: unknown enum tag `%s::%s`", subject.enum.Name, tag)
		}
		return index, nil
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
	saved map[string]Value,
	wantValue bool,
) (matchArmResult, error) {
	l.env = l.copyEnv(saved)
	l.block = block
	if err := l.bindMatchPayload(subject, arm); err != nil {
		return matchArmResult{}, err
	}
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
	result := matchArmResult{block: l.block.Name, env: l.copyEnv(l.env), value: value}
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "jump", Target: endLabel}
		result.reaches = true
	}
	l.env = saved
	return result, nil
}

// bindMatchPayload binds a union payload for `Tag(name)` arms.
func (l *lowerer) bindMatchPayload(subject matchSubject, arm ast.MatchArm) error {
	if arm.Binding == "" || arm.IsWildcard() || subject.union.Name == "" {
		return nil
	}
	variant, ok := subject.union.Variants[arm.Tag]
	if !ok || variant.Payload == "" {
		return nil
	}
	l.env[arm.Binding] = l.emit(
		"union.payload",
		variant.Payload,
		[]Value{subject.unionValue},
		variant.Name,
	)
	return nil
}

// lowerMatchArmValue lowers the expression value of a match expression arm.
func (l *lowerer) lowerMatchArmValue(stmt ast.Statement) (Value, error) {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok || expr.Semicolon {
		return Value{}, fmt.Errorf("ir error: match expression arms must be expressions")
	}
	return l.lowerExpr(expr.Expr)
}

// mergeMatchEnvs creates phi nodes for bindings changed by reachable arms.
func (l *lowerer) mergeMatchEnvs(
	results []matchArmResult,
	fallback map[string]Value,
) map[string]Value {
	incomingResults := []matchArmResult{}
	for _, result := range results {
		if result.reaches {
			incomingResults = append(incomingResults, result)
		}
	}
	if len(incomingResults) == 0 {
		return fallback
	}
	merged := l.copyEnv(incomingResults[0].env)
	names := map[string]bool{}
	for name := range fallback {
		names[name] = true
	}
	for name := range names {
		first, ok := incomingResults[0].env[name]
		if !ok {
			first = fallback[name]
		}
		allSame := true
		incoming := make([]Incoming, 0, len(incomingResults))
		for _, result := range incomingResults {
			value, ok := result.env[name]
			if !ok {
				value = fallback[name]
			}
			if !sameValue(first, value) {
				allSame = false
			}
			incoming = append(incoming, Incoming{Block: result.block, Value: value})
		}
		if !allSame {
			merged[name] = l.addPhi(l.block, first.Type, incoming)
		}
	}
	return merged
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
