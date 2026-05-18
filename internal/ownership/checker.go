package ownership

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Checker validates ownership and move rules for a parsed program.
type Checker struct {
	functions  map[string]*functionInfo
	structs    map[string]map[string]string
	enums      map[string]map[string]bool
	unions     map[string]map[string]string
	nextID     int
	loopDepth  int
	currentStd bool
}

type functionInfo struct {
	name       string
	params     []paramInfo
	returnType string
	decl       *ast.FunctionDecl
}

type paramInfo struct {
	typeName  string
	borrow    bool
	mutBorrow bool
	comptime  bool
}

type binding struct {
	id               int
	name             string
	typeName         string
	mutable          bool
	moved            bool
	borrowedParam    bool
	localBorrow      bool
	borrowTarget     *binding
	borrowField      string
	mutBorrow        bool
	activeBorrows    int
	activeMutBorrows int
	fieldBorrows     map[string]int
	fieldMutBorrows  map[string]int
	arenaID          int
	handleArenaID    int
	taskDone         bool
}

type scope struct {
	parent *scope
	values map[string]*binding
}

// New creates an empty ownership checker.
func New() *Checker {
	return &Checker{
		functions: map[string]*functionInfo{},
		structs:   map[string]map[string]string{},
		enums:     map[string]map[string]bool{},
		unions:    map[string]map[string]string{},
	}
}

// Check validates ownership rules and returns the first move error.
func (c *Checker) Check(program *ast.Program) error {
	if err := c.checkStructs(program); err != nil {
		return err
	}
	c.collectEnums(program)
	c.collectUnions(program)
	if err := c.collectFunctions(program); err != nil {
		return err
	}
	for _, decl := range program.Decls {
		fnDecl, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if err := c.checkFunction(c.functions[fnDecl.Name]); err != nil {
			return err
		}
	}
	return nil
}

// collectEnums records tag enum declarations for enum value reads.
func (c *Checker) collectEnums(program *ast.Program) {
	for _, decl := range program.Decls {
		enumDecl, ok := decl.(*ast.EnumDecl)
		if !ok {
			continue
		}
		tags := map[string]bool{}
		for _, tag := range enumDecl.Tags {
			tags[tag] = true
		}
		c.enums[enumDecl.Name] = tags
	}
}

// collectUnions records tagged union declarations for variant construction and matches.
func (c *Checker) collectUnions(program *ast.Program) {
	for _, decl := range program.Decls {
		unionDecl, ok := decl.(*ast.UnionDecl)
		if !ok {
			continue
		}
		variants := map[string]string{}
		for _, variant := range unionDecl.Variants {
			variants[variant.Name] = variant.Payload
		}
		c.unions[unionDecl.Name] = variants
	}
}

// checkStructs rejects struct fields that would store a local borrow.
func (c *Checker) checkStructs(program *ast.Program) error {
	for _, decl := range program.Decls {
		st, ok := decl.(*ast.StructDecl)
		if !ok {
			continue
		}
		fields := map[string]string{}
		for _, field := range st.Fields {
			if field.Borrow {
				return fmt.Errorf("borrow error: struct field `%s.%s` cannot store borrow",
					st.Name, field.Name)
			}
			fields[field.Name] = field.TypeName
		}
		c.structs[st.Name] = fields
	}
	return nil
}

// collectFunctions registers top-level signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		params := make([]paramInfo, 0, len(fn.Params))
		for _, param := range fn.Params {
			params = append(params, paramInfo{
				typeName: param.TypeName, borrow: param.Borrow, mutBorrow: param.MutBorrow,
				comptime: param.Comptime,
			})
		}
		c.functions[fn.Name] = &functionInfo{
			name: fn.Name, params: params, returnType: fn.ReturnType, decl: fn,
		}
	}
	return nil
}

// checkFunction validates one function body.
func (c *Checker) checkFunction(fn *functionInfo) error {
	if fn.decl.ExternABI != "" {
		return nil
	}
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		value := c.newBinding(param.Name, fn.params[idx].typeName)
		value.borrowedParam = fn.params[idx].borrow
		value.mutBorrow = fn.params[idx].mutBorrow
		env.define(value)
	}
	previousLoopDepth := c.loopDepth
	previousStd := c.currentStd
	c.loopDepth = 0
	c.currentStd = fn.decl.Std
	defer func() { c.loopDepth = previousLoopDepth }()
	defer func() { c.currentStd = previousStd }()
	return c.checkBlock(fn.decl.Body, env)
}

// checkBlock validates statements in a lexical block.
func (c *Checker) checkBlock(block *ast.BlockStmt, env *scope) error {
	lastUses := blockLastUses(block)
	for idx, stmt := range block.Statements {
		if err := c.checkStmt(stmt, env); err != nil {
			return err
		}
		env.releaseLastUseBorrows(idx, lastUses)
	}
	return env.checkPendingTasks()
}

// checkStmt validates one statement.
func (c *Checker) checkStmt(stmt ast.Statement, env *scope) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return c.checkLetStmt(s, env)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(s, env)
	case *ast.ExprStmt:
		return c.checkExprStmt(s, env)
	case *ast.IfStmt:
		return c.checkIfStmt(s, env)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env)
	case *ast.ForStmt:
		return c.checkForStmt(s, env)
	case *ast.BreakStmt:
		return c.checkLoopBranch(s.Label)
	case *ast.ContinueStmt:
		return c.checkLoopBranch(s.Label)
	case *ast.MatchStmt:
		return c.checkMatchStmt(s, env)
	case *ast.UnsafeStmt:
		return c.checkBlock(s.Body, env.child())
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env)
	default:
		return fmt.Errorf("move error: unsupported statement %T", stmt)
	}
}

// checkReturnStmt rejects borrowed values before applying normal move rules.
func (c *Checker) checkReturnStmt(stmt *ast.ReturnStmt, env *scope) error {
	if stmt.Value == nil {
		return nil
	}
	if ident, ok := stmt.Value.(*ast.IdentExpr); ok {
		value, exists := env.lookup(ident.Name)
		if exists && value.borrowedParam {
			return fmt.Errorf("borrow error: borrowed value `%s` cannot escape", ident.Name)
		}
		if exists && value.handleArenaID != 0 {
			return fmt.Errorf("arena error: handle `%s` cannot outlive its arena", ident.Name)
		}
	}
	if arena := c.arenaAddReceiver(stmt.Value, env); arena != nil && arena.arenaID != 0 {
		return fmt.Errorf("arena error: handle from `%s` cannot outlive its arena", arena.name)
	}
	_, err := c.moveExpr(stmt.Value, env)
	return err
}

// checkLetStmt moves the initializer into a new binding when needed.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope) error {
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		return c.checkBorrowLetStmt(stmt, borrow, env)
	}
	if target, elem, mutable, ok := c.arrayBorrowInitializer(stmt.Value, env); ok {
		return c.checkArrayBorrowLetStmt(stmt, target, elem, mutable, env)
	}
	if target, ok := c.stringViewInitializer(stmt.Value, env); ok {
		return c.checkStringViewLetStmt(stmt, target, env)
	}
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	value := c.newBinding(stmt.Name, typeName)
	value.mutable = stmt.Mutable
	c.setArenaProvenance(value, stmt.Value, env)
	env.define(value)
	return nil
}

// checkStringViewLetStmt binds a local byte view and activates the String owner.
func (c *Checker) checkStringViewLetStmt(stmt *ast.LetStmt, target *binding, env *scope) error {
	if err := c.checkStringViewInitializerShape(stmt.Value); err != nil {
		return err
	}
	if err := checkBorrowConflict(target, false); err != nil {
		return err
	}
	c.activateBorrow(target, "", false)
	value := c.newBinding(stmt.Name, "[]const u8")
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTarget = target
	env.define(value)
	return nil
}

// checkStringViewInitializerShape validates string.as_bytes() local view syntax.
func (c *Checker) checkStringViewInitializerShape(expr ast.Expression) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("string error: String view initializer must call String.as_bytes")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "as_bytes" {
		return fmt.Errorf("string error: String view initializer must call String.as_bytes")
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("string error: `String.as_bytes` expects 0 args, got %d", len(call.Args))
	}
	return nil
}

// stringViewInitializer recognizes string.as_bytes() local byte-view initializers.
func (c *Checker) stringViewInitializer(expr ast.Expression, env *scope) (*binding, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "as_bytes" {
		return nil, false
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}
	target, exists := env.lookup(ident.Name)
	if !exists || target.moved || target.typeName != "std::string::String" {
		return nil, false
	}
	return target, true
}

// checkArrayBorrowLetStmt binds an Array element borrow and activates the array owner.
func (c *Checker) checkArrayBorrowLetStmt(
	stmt *ast.LetStmt,
	target *binding,
	elem string,
	mutable bool,
	env *scope,
) error {
	if mutable && !target.mutable {
		return fmt.Errorf("array error: `Array.at_mut` requires mutable array binding")
	}
	if err := c.checkArrayBorrowInitializerIndex(stmt.Value, env); err != nil {
		return err
	}
	if err := checkBorrowConflict(target, mutable); err != nil {
		return err
	}
	c.activateBorrow(target, "", mutable)
	value := c.newBinding(stmt.Name, elem)
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTarget = target
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// checkArrayBorrowInitializerIndex validates the checked index for Array.at/at_mut.
func (c *Checker) checkArrayBorrowInitializerIndex(expr ast.Expression, env *scope) error {
	tryExpr, ok := expr.(*ast.TryExpr)
	if !ok {
		return fmt.Errorf("array error: Array borrow initializer must use try")
	}
	call, ok := tryExpr.Value.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("array error: Array borrow initializer must call Array.at")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "at" && field.Name != "at_mut") {
		return fmt.Errorf("array error: Array borrow initializer must call Array.at")
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("array error: `Array.%s` expects 1 arg, got %d", field.Name, len(call.Args))
	}
	got, err := c.readExpr(call.Args[0], env)
	if err != nil {
		return err
	}
	if got != "i64" {
		return fmt.Errorf("array error: `Array.%s` expects i64 index, got %s", field.Name, got)
	}
	return nil
}

// arrayBorrowInitializer recognizes try array.at/at_mut(index) local borrow initializers.
func (c *Checker) arrayBorrowInitializer(
	expr ast.Expression,
	env *scope,
) (*binding, string, bool, bool) {
	tryExpr, ok := expr.(*ast.TryExpr)
	if !ok {
		return nil, "", false, false
	}
	call, ok := tryExpr.Value.(*ast.CallExpr)
	if !ok {
		return nil, "", false, false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "at" && field.Name != "at_mut") {
		return nil, "", false, false
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil, "", false, false
	}
	array, exists := env.lookup(ident.Name)
	if !exists || array.moved {
		return nil, "", false, false
	}
	base, elem, ok := splitGenericType(array.typeName)
	if !ok || base != "std::array::Array" {
		return nil, "", false, false
	}
	return array, elem, field.Name == "at_mut", true
}

// checkBorrowLetStmt binds a local borrow and activates its owner until last use.
func (c *Checker) checkBorrowLetStmt(
	stmt *ast.LetStmt,
	borrow *ast.PrefixExpr,
	env *scope,
) error {
	target, field, err := c.borrowTarget(borrow.Right, env)
	if err != nil {
		return err
	}
	mutable := borrow.Operator == "&mut"
	if err := checkBorrowConflictForField(target, field, mutable); err != nil {
		return err
	}
	typeName, err := c.readExpr(borrow.Right, env)
	if err != nil {
		return err
	}
	c.activateBorrow(target, field, mutable)
	value := c.newBinding(stmt.Name, typeName)
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTarget = target
	value.borrowField = field
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// borrowTarget resolves a v0.1 explicit borrow target.
func (c *Checker) borrowTarget(expr ast.Expression, env *scope) (*binding, string, error) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		value, ok := env.lookup(target.Name)
		if !ok {
			return nil, "", fmt.Errorf("borrow error: undefined variable `%s`", target.Name)
		}
		if value.moved {
			return nil, "", fmt.Errorf("borrow error: moved value `%s` was borrowed", target.Name)
		}
		return value, "", nil
	case *ast.FieldExpr:
		ident, ok := target.Receiver.(*ast.IdentExpr)
		if !ok {
			return nil, "", fmt.Errorf("borrow error: v0.1 field borrow only supports one direct field")
		}
		value, ok := env.lookup(ident.Name)
		if !ok {
			return nil, "", fmt.Errorf("borrow error: undefined variable `%s`", ident.Name)
		}
		if value.moved {
			return nil, "", fmt.Errorf("borrow error: moved value `%s` was borrowed", ident.Name)
		}
		return value, target.Name, nil
	default:
		return nil, "", fmt.Errorf("borrow error: borrow target must be a local binding or direct field")
	}
}

// activateBorrow records one active whole-value or field borrow on a target.
func (c *Checker) activateBorrow(target *binding, field string, mutable bool) {
	if field == "" {
		if mutable {
			target.activeMutBorrows++
		} else {
			target.activeBorrows++
		}
		return
	}
	if mutable {
		if target.fieldMutBorrows == nil {
			target.fieldMutBorrows = map[string]int{}
		}
		target.fieldMutBorrows[field]++
		return
	}
	if target.fieldBorrows == nil {
		target.fieldBorrows = map[string]int{}
	}
	target.fieldBorrows[field]++
}

// checkAssignStmt moves the assigned value into an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope) error {
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	if target, ok := directAssignmentRoot(stmt.Target, env); ok {
		if target.hasAnyBorrow() && !c.isCopyType(target.typeName) {
			return fmt.Errorf("borrow error: value `%s` cannot be assigned while borrowed",
				target.name)
		}
		target.typeName = typeName
		target.moved = false
		target.arenaID = 0
		target.handleArenaID = 0
		c.setArenaProvenance(target, stmt.Value, env)
		return nil
	}
	if err := c.checkAssignmentBorrowConflict(stmt.Target, env); err != nil {
		return err
	}
	if _, ok := assignmentRoot(stmt.Target, env); !ok {
		_, err := c.readExpr(stmt.Target, env)
		return err
	}
	return nil
}

// checkExprStmt reads standalone expressions, except normal calls handle argument moves.
func (c *Checker) checkExprStmt(stmt *ast.ExprStmt, env *scope) error {
	_, err := c.readExpr(stmt.Expr, env)
	return err
}

// checkIfStmt merges possible moves from either branch into the outer scope.
func (c *Checker) checkIfStmt(stmt *ast.IfStmt, env *scope) error {
	if _, err := c.readExpr(stmt.Condition, env); err != nil {
		return err
	}
	left := env.clone()
	if err := c.checkBlock(stmt.Consequence, left.child()); err != nil {
		return err
	}
	right := env.clone()
	if stmt.Alternative != nil {
		if err := c.checkBlock(stmt.Alternative, right.child()); err != nil {
			return err
		}
	}
	env.mergeMovedFrom(left)
	env.mergeMovedFrom(right)
	return nil
}

// checkWhileStmt treats moves in the body as possible after the loop.
func (c *Checker) checkWhileStmt(stmt *ast.WhileStmt, env *scope) error {
	if _, err := c.readExpr(stmt.Condition, env); err != nil {
		return err
	}
	body := env.clone()
	c.loopDepth++
	defer func() { c.loopDepth-- }()
	if err := c.checkBlock(stmt.Body, body.child()); err != nil {
		return err
	}
	env.mergeMovedFrom(body)
	return nil
}

// checkForStmt treats moves in the body as possible after the loop.
func (c *Checker) checkForStmt(stmt *ast.ForStmt, env *scope) error {
	if _, err := c.readExpr(stmt.Start, env); err != nil {
		return err
	}
	if _, err := c.readExpr(stmt.End, env); err != nil {
		return err
	}
	body := env.clone()
	child := body.child()
	child.define(c.newBinding(stmt.Name, "i64"))
	c.loopDepth++
	defer func() { c.loopDepth-- }()
	if err := c.checkBlock(stmt.Body, child); err != nil {
		return err
	}
	env.mergeMovedFrom(body)
	return nil
}

// checkLoopBranch rejects branch statements outside loops during ownership-only tests.
func (c *Checker) checkLoopBranch(label string) error {
	if c.loopDepth == 0 {
		return fmt.Errorf("move error: loop branch `%s` used outside loop", label)
	}
	return nil
}

// checkMatchStmt merges possible moves from enum or union match arms into the outer scope.
func (c *Checker) checkMatchStmt(stmt *ast.MatchStmt, env *scope) error {
	valueType, err := c.readExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	tags, unionPayloads, ok := c.matchTags(valueType)
	if !ok {
		return fmt.Errorf("move error: match expects enum or union, got %s", valueType)
	}
	for _, arm := range stmt.Arms {
		if !tags[arm.Tag] {
			return fmt.Errorf("move error: unknown match tag `%s::%s`", valueType, arm.Tag)
		}
		armEnv := env.clone()
		child := armEnv.child()
		if payload := unionPayloads[arm.Tag]; payload != "" && arm.Binding != "" {
			value := c.newBinding(arm.Binding, payload)
			value.borrowedParam = true
			child.define(value)
		}
		if err := c.checkStmt(arm.Body, child); err != nil {
			return err
		}
		env.mergeMovedFrom(armEnv)
	}
	return nil
}

// matchTags returns known tags for enum and union match ownership checks.
func (c *Checker) matchTags(typeName string) (map[string]bool, map[string]string, bool) {
	if tags := c.enums[typeName]; tags != nil {
		return tags, nil, true
	}
	payloads := c.unions[typeName]
	if payloads == nil {
		return nil, nil, false
	}
	tags := map[string]bool{}
	for tag := range payloads {
		tags[tag] = true
	}
	return tags, payloads, true
}

// readExpr checks an expression without consuming owned values.
func (c *Checker) readExpr(expr ast.Expression, env *scope) (string, error) {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return readLiteralType(e)
	case *ast.IfExpr:
		return c.readIfExpr(e, env)
	case *ast.ComptimeExpr:
		return c.readComptimeExpr(e, env)
	case *ast.IdentExpr:
		return readIdent(e.Name, env)
	case *ast.PrefixExpr:
		return c.readExpr(e.Right, env)
	case *ast.BinaryExpr:
		return c.readBinaryExpr(e, env)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env)
	case *ast.CastExpr:
		return c.readCastExpr(e, env)
	case *ast.TryExpr:
		return c.readTryExpr(e, env)
	case *ast.IndexExpr:
		return c.readIndexExpr(e, env)
	case *ast.ArenaNewExpr:
		return fmt.Sprintf("arena<%s>", e.TypeName), nil
	case *ast.StructLiteralExpr:
		return c.readStructLiteralExpr(e, env)
	case *ast.FieldExpr:
		return c.readFieldExpr(e, env)
	case *ast.DerefExpr:
		return c.readDerefExpr(e, env)
	default:
		return "", fmt.Errorf("move error: unsupported expression %T", expr)
	}
}

// readIndexExpr reads checked byte indexing and slicing without moving bytes.
func (c *Checker) readIndexExpr(expr *ast.IndexExpr, env *scope) (string, error) {
	target, err := c.readExpr(expr.Target, env)
	if err != nil {
		return "", err
	}
	if target != "[]const u8" {
		return "", fmt.Errorf("move error: index/slice target expects []const u8, got %s", target)
	}
	if !expr.Slice {
		if _, err := c.readExpr(expr.Index, env); err != nil {
			return "", err
		}
		return "u8", nil
	}
	if expr.Start != nil {
		if _, err := c.readExpr(expr.Start, env); err != nil {
			return "", err
		}
	}
	if expr.End != nil {
		if _, err := c.readExpr(expr.End, env); err != nil {
			return "", err
		}
	}
	return "[]const u8", nil
}

// readLiteralType returns the ownership type of scalar literals.
func readLiteralType(expr ast.Expression) (string, error) {
	switch expr.(type) {
	case *ast.IntExpr:
		return "i64", nil
	case *ast.StringExpr:
		return "[]const u8", nil
	case *ast.BoolExpr:
		return "bool", nil
	default:
		return "", fmt.Errorf("move error: unsupported literal %T", expr)
	}
}

// readCastExpr reads the source value and returns the explicit target type.
func (c *Checker) readCastExpr(expr *ast.CastExpr, env *scope) (string, error) {
	if _, err := c.readExpr(expr.Value, env); err != nil {
		return "", err
	}
	return expr.TargetType, nil
}

// moveExpr checks an expression and consumes a non-copy identifier when present.
func (c *Checker) moveExpr(expr ast.Expression, env *scope) (string, error) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return c.moveNonIdentExpr(expr, env)
	}
	value, ok := env.lookup(ident.Name)
	if !ok {
		if ident.Name == "void" {
			return "void", nil
		}
		return "", fmt.Errorf("move error: undefined variable `%s`", ident.Name)
	}
	if value.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", ident.Name)
	}
	if value.borrowedParam {
		return "", fmt.Errorf("borrow error: borrowed value `%s` cannot escape", ident.Name)
	}
	if value.hasAnyBorrow() && !c.isCopyType(value.typeName) {
		return "", fmt.Errorf("borrow error: value `%s` cannot be moved while borrowed", ident.Name)
	}
	if !c.isCopyType(value.typeName) {
		value.moved = true
	}
	return value.typeName, nil
}

// moveNonIdentExpr handles move contexts for compound expressions.
func (c *Checker) moveNonIdentExpr(expr ast.Expression, env *scope) (string, error) {
	if ifExpr, ok := expr.(*ast.IfExpr); ok {
		return c.moveIfExpr(ifExpr, env)
	}
	if deref, ok := expr.(*ast.DerefExpr); ok {
		return c.moveDerefExpr(deref, env)
	}
	if c.isArenaGetExpr(expr) {
		if _, err := c.readExpr(expr, env); err != nil {
			return "", err
		}
		return "", fmt.Errorf("arena error: arena.get returns a local borrow and cannot be moved")
	}
	if st, ok := expr.(*ast.StructLiteralExpr); ok {
		return c.moveStructLiteralExpr(st, env)
	}
	if field, ok := expr.(*ast.FieldExpr); ok {
		return c.moveFieldExpr(field, env)
	}
	return c.readExpr(expr, env)
}

// readIfExpr reads branch values without consuming non-copy branch results.
func (c *Checker) readIfExpr(expr *ast.IfExpr, env *scope) (string, error) {
	return c.checkIfExprOwnership(expr, env, false)
}

// moveIfExpr consumes the selected branch value and merges possible moves.
func (c *Checker) moveIfExpr(expr *ast.IfExpr, env *scope) (string, error) {
	return c.checkIfExprOwnership(expr, env, true)
}

// checkIfExprOwnership validates branch ownership effects.
func (c *Checker) checkIfExprOwnership(
	expr *ast.IfExpr,
	env *scope,
	moveResult bool,
) (string, error) {
	if _, err := c.readExpr(expr.Condition, env); err != nil {
		return "", err
	}
	if expr.Alternative == nil {
		return "", fmt.Errorf("move error: if expression requires else branch")
	}
	left := env.clone()
	leftType, err := c.checkIfExprBlock(expr.Consequence, left.child(), moveResult)
	if err != nil {
		return "", err
	}
	right := env.clone()
	rightType, err := c.checkIfExprBlock(expr.Alternative, right.child(), moveResult)
	if err != nil {
		return "", err
	}
	env.mergeMovedFrom(left)
	env.mergeMovedFrom(right)
	if leftType != rightType {
		return "", fmt.Errorf("move error: if expression branch types differ")
	}
	return leftType, nil
}

// checkIfExprBlock checks statements before the final branch value.
func (c *Checker) checkIfExprBlock(
	block *ast.BlockStmt,
	env *scope,
	moveResult bool,
) (string, error) {
	if block == nil || len(block.Statements) == 0 {
		return "", fmt.Errorf("move error: if expression branch must end with a value")
	}
	last := len(block.Statements) - 1
	for _, stmt := range block.Statements[:last] {
		if err := c.checkStmt(stmt, env); err != nil {
			return "", err
		}
	}
	exprStmt, ok := block.Statements[last].(*ast.ExprStmt)
	if !ok {
		return "", fmt.Errorf("move error: if expression branch must end with a value")
	}
	if moveResult {
		return c.moveExpr(exprStmt.Expr, env)
	}
	return c.readExpr(exprStmt.Expr, env)
}

// moveDerefExpr rejects moving a non-copy value out through a local borrow.
func (c *Checker) moveDerefExpr(expr *ast.DerefExpr, env *scope) (string, error) {
	typeName, err := c.readDerefExpr(expr, env)
	if err != nil {
		return "", err
	}
	if c.isCopyType(typeName) {
		return typeName, nil
	}
	return "", fmt.Errorf("borrow error: value `%s` cannot be moved out of borrow",
		expr.Receiver.String())
}

// readBinaryExpr reads both operands and returns the operator result type.
func (c *Checker) readBinaryExpr(expr *ast.BinaryExpr, env *scope) (string, error) {
	left, err := c.readExpr(expr.Left, env)
	if err != nil {
		return "", err
	}
	if _, err := c.readExpr(expr.Right, env); err != nil {
		return "", err
	}
	if expr.Operator == "and" || expr.Operator == "or" {
		return "bool", nil
	}
	if isBooleanBinaryOperator(expr.Operator) {
		return "bool", nil
	}
	return left, nil
}

// isBooleanBinaryOperator reports whether a binary operator returns bool.
func isBooleanBinaryOperator(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}

// checkCallExpr validates ownership effects of builtin and user calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope) (string, error) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return c.checkFieldCallExpr(field, expr.Args, env)
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return c.checkTypeApplyCallExpr(typeApply, expr.Args, env)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("move error: callee must be a function name")
	}
	if result, ok, err := c.checkBuiltinCall(name.Name, expr, env); ok || err != nil {
		return result, err
	}
	return c.checkUserCall(name.Name, expr.Args, env)
}

// checkUserCall validates ownership effects for a declared function call.
func (c *Checker) checkUserCall(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	fn, ok := c.functions[name]
	if !ok {
		return "", fmt.Errorf("move error: undefined function `%s`", name)
	}
	if len(args) != len(fn.params) {
		return "", fmt.Errorf("move error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	borrowed, err := c.activateBorrowArgs(fn, args, env)
	if err != nil {
		return "", err
	}
	defer releaseBorrows(borrowed)
	for idx, arg := range args {
		if fn.params[idx].comptime {
			_, err = c.readExpr(arg, env)
		} else if fn.params[idx].borrow {
			_, err = c.readExpr(arg, env)
		} else {
			_, err = c.moveExpr(arg, env)
		}
		if err != nil {
			return "", err
		}
	}
	return returnTypeName(fn), nil
}

// checkFieldCallExpr validates calls whose callee is a dotted expression.
func (c *Checker) checkFieldCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if typ, ok, err := c.checkUnionConstructor(field, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkQualifiedUserCall(field, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkQualifiedBuiltin(field, args, env); ok || err != nil {
		return typ, err
	}
	return c.checkMethodCallExpr(field, args, env)
}

// checkQualifiedUserCall validates ownership for source-loaded qualified calls.
func (c *Checker) checkQualifiedUserCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	if _, ok := c.functions[name]; !ok {
		return "", false, nil
	}
	typ, err := c.checkUserCall(name, args, env)
	return typ, true, err
}

// checkQualifiedBuiltin validates ownership for std:: namespace prototype calls.
func (c *Checker) checkQualifiedBuiltin(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	if typ, ok, err := c.checkQualifiedStdCoreBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	return c.checkQualifiedStdRuntimeBuiltin(name, args, env)
}

// checkQualifiedStdCoreBuiltin validates pure, fs, I/O, and process std calls.
func (c *Checker) checkQualifiedStdCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if typ, ok, err := c.checkQualifiedStdBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	switch name {
	case "std.channel.Channel":
		return "", true, fmt.Errorf("move error: use `std::channel::Channel<T>()`")
	case "std.thread.scoped":
		return c.checkThreadScoped(args, env)
	default:
		return "", false, nil
	}
}

// checkQualifiedStdRuntimeBuiltin validates std constructors and runtime helpers.
func (c *Checker) checkQualifiedStdRuntimeBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if typ, ok, err := checkIoBuiltin(name, args); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkIoWriteBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkProcessBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := checkConcurrencyConstructor(name); ok || err != nil {
		return typ, ok, err
	}
	return c.checkQualifiedStdRuntimeStateBuiltin(name, args, env)
}

// checkQualifiedStdRuntimeStateBuiltin validates stateful std runtime helpers.
func (c *Checker) checkQualifiedStdRuntimeStateBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if typ, ok, err := c.checkTaskBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// checkQualifiedStdBuiltin validates fs and mem ownership effects.
func (c *Checker) checkQualifiedStdBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if typ, ok, err := c.checkMemBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkFsBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// checkMemBuiltin validates ownership effects for allocation-free std::mem calls.
func (c *Checker) checkMemBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std.builtin.mem_page_allocator":
		_, err := checkNoArgOwnershipCall(name, args)
		if err != nil {
			return "", true, err
		}
		return "Allocator", true, nil
	case "std.builtin.mem_len":
		return c.checkMemByteArgs(name, args, env, 1, "i64")
	case "std.builtin.mem_byte_at":
		return c.checkMemByteIndex(name, args, env, "!u8")
	case "std.builtin.mem_slice":
		if err := c.checkMemSliceShape("std.builtin.mem_slice", args, env); err != nil {
			return "", true, err
		}
		return "![]const u8", true, nil
	default:
		return "", false, nil
	}
}

// checkMemByteArgs reads byte-slice arguments without consuming them.
func (c *Checker) checkMemByteArgs(
	name string,
	args []ast.Expression,
	env *scope,
	want int,
	result string,
) (string, bool, error) {
	if len(args) != want {
		return "", true, fmt.Errorf("move error: `%s` expects %d args, got %d",
			name, want, len(args))
	}
	for idx, arg := range args {
		got, err := c.readExpr(arg, env)
		if err != nil {
			return "", true, err
		}
		if got != "[]const u8" {
			return "", true, fmt.Errorf("move error: `%s` arg %d expects []const u8, got %s",
				name, idx+1, got)
		}
	}
	return result, true, nil
}

// checkMemByteIndex reads a byte-slice and index without consuming them.
func (c *Checker) checkMemByteIndex(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("move error: `%s` expects bytes and index", name)
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", true, err
	} else if got != "[]const u8" {
		return "", true, fmt.Errorf("move error: `%s` expects []const u8 bytes, got %s", name, got)
	}
	got, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if got != "i64" {
		return "", true, fmt.Errorf("move error: `%s` expects i64 index, got %s", name, got)
	}
	return result, true, nil
}

// checkMemSliceShape reads checked slice arguments without consuming them.
func (c *Checker) checkMemSliceShape(name string, args []ast.Expression, env *scope) error {
	if len(args) != 3 {
		return fmt.Errorf("move error: `%s` expects bytes, start, and end", name)
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return err
	} else if got != "[]const u8" {
		return fmt.Errorf("move error: `%s` expects []const u8 bytes, got %s", name, got)
	}
	for idx, label := range []string{"start", "end"} {
		got, err := c.readExpr(args[idx+1], env)
		if err != nil {
			return err
		}
		if got != "i64" {
			return fmt.Errorf("move error: `%s` expects i64 %s, got %s", name, label, got)
		}
	}
	return nil
}

// checkFsBuiltin validates ownership for filesystem host primitives.
func (c *Checker) checkFsBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std.builtin.fs_read_file":
		return c.checkFsReadFile(args, env)
	case "std.builtin.fs_write_file":
		return c.checkFsWriteFile(args, env)
	case "std.builtin.fs_exists":
		return c.checkFsPathOnly("std::fs::exists", args, env, "!bool")
	case "std.builtin.fs_metadata":
		return c.checkFsPathOnly("std::fs::metadata", args, env, "!std::fs::Metadata")
	case "std.builtin.fs_create_dir", "std.builtin.fs_remove_dir", "std.builtin.fs_remove_file":
		return c.checkFsPathOnly(strings.ReplaceAll(name, ".", "::"), args, env, "!void")
	default:
		return "", false, nil
	}
}

// checkIoWriteBuiltin validates explicit-Io stdio helpers.
func (c *Checker) checkIoWriteBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std.builtin.io_write_stdout", "std.builtin.io_write_stderr":
		return c.checkIoBytesCall(name, args, env)
	case "std.builtin.io_read_stdin":
		return c.checkIoOnlyCall(name, args, env, "![]const u8")
	default:
		return "", false, nil
	}
}

// checkIoBytesCall validates an Io plus bytes call without moving bytes.
func (c *Checker) checkIoBytesCall(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("move error: `%s` expects io and bytes", name)
	}
	if err := c.checkIoArg(args[0], env, name); err != nil {
		return "", true, err
	}
	got, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if got != "[]const u8" {
		return "", true, fmt.Errorf("move error: `%s` expects []const u8 bytes, got %s", name, got)
	}
	return "!void", true, nil
}

// checkIoOnlyCall validates a call that only takes Io.
func (c *Checker) checkIoOnlyCall(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("move error: `%s` expects io", name)
	}
	if err := c.checkIoArg(args[0], env, name); err != nil {
		return "", true, err
	}
	return result, true, nil
}

// checkProcessBuiltin validates minimal process helpers.
func (c *Checker) checkProcessBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std.builtin.process_arg_count":
		_, err := checkNoArgOwnershipCall(name, args)
		return "i64", true, err
	case "std.builtin.process_arg":
		return c.checkProcessI64Arg(name, args, env, "![]const u8")
	case "std.builtin.process_env":
		return c.checkProcessBytesArg(name, args, env, "![]const u8")
	case "std.builtin.process_exit_code":
		return c.checkProcessI64Arg(name, args, env, "i64")
	default:
		return "", false, nil
	}
}

// checkProcessI64Arg validates one i64 process argument.
func (c *Checker) checkProcessI64Arg(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("move error: `%s` expects i64", name)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != "i64" {
		return "", true, fmt.Errorf("move error: `%s` expects i64, got %s", name, got)
	}
	return result, true, nil
}

// checkProcessBytesArg validates one []const u8 process argument.
func (c *Checker) checkProcessBytesArg(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("move error: `%s` expects []const u8", name)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != "[]const u8" {
		return "", true, fmt.Errorf("move error: `%s` expects []const u8, got %s", name, got)
	}
	return result, true, nil
}

// checkTaskBuiltin validates ownership for task and data-parallel std calls.
func (c *Checker) checkTaskBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if strings.HasPrefix(name, "std.builtin.task_") && !c.currentStd {
		return "", true, fmt.Errorf("move error: `%s` is reserved; use std::task", name)
	}
	switch name {
	case "std.builtin.task_group":
		return c.checkTaskGroup(args, env)
	case "std.builtin.task_queue":
		_, err := checkNoArgOwnershipCall(name, args)
		if err != nil {
			return "", true, err
		}
		return "Queue", true, nil
	case "std.builtin.task_partition_mut":
		return c.checkPartitionMut(args, env)
	case "std.builtin.task_local_buffer":
		return c.checkLocalBuffer(args, env)
	case "std.task.parallel_for":
		return c.checkParallelFor(args, env)
	case "std.task.parallel_map":
		return c.checkParallelMap(args, env)
	default:
		return "", false, nil
	}
}

// checkConcurrencyConstructor rejects untyped concurrency constructors.
func checkConcurrencyConstructor(name string) (string, bool, error) {
	switch name {
	case "std.array.Array":
		return "", true, fmt.Errorf("move error: use `std::array::Array<T>(allocator)`")
	case "std.map.Map":
		return "", true, fmt.Errorf("move error: use `std::map::Map<K, V>(allocator)`")
	case "std.atomic.Atomic":
		return "", true, fmt.Errorf("move error: use `std::atomic::Atomic<T>(value)`")
	case "std.atomic.AtomicI64":
		return "", true, fmt.Errorf("move error: use `std::atomic::Atomic<i64>(value)`")
	case "std.sync.Mutex":
		return "", true, fmt.Errorf("move error: use `std::sync::Mutex<T>(value)`")
	default:
		return "", false, nil
	}
}

// checkFsReadFile validates ownership effects for std::fs::read_file.
func (c *Checker) checkFsReadFile(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("move error: `std::fs::read_file` expects io and path")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::read_file"); err != nil {
		return "", true, err
	}
	path, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if path != "[]const u8" {
		return "", true, fmt.Errorf("move error: `std::fs::read_file` expects []const u8 path, got %s",
			path)
	}
	return "![]const u8", true, nil
}

// checkFsWriteFile validates ownership effects for std::fs::write_file.
func (c *Checker) checkFsWriteFile(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 3 {
		return "", true, fmt.Errorf("move error: `std::fs::write_file` expects io, path, and bytes")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::write_file"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"path", "bytes"} {
		got, err := c.readExpr(args[idx+1], env)
		if err != nil {
			return "", true, err
		}
		if got != "[]const u8" {
			return "", true, fmt.Errorf(
				"move error: `std::fs::write_file` expects []const u8 %s, got %s", label, got)
		}
	}
	return "!void", true, nil
}

// checkFsPathOnly validates common std::fs Io and path arguments.
func (c *Checker) checkFsPathOnly(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("move error: `%s` expects io and path", name)
	}
	if err := c.checkIoArg(args[0], env, name); err != nil {
		return "", true, err
	}
	path, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if path != "[]const u8" {
		return "", true, fmt.Errorf("move error: `%s` expects []const u8 path, got %s", name, path)
	}
	return result, true, nil
}

// checkArrayConstructor validates std::array::Array<T>(allocator) ownership.
func (c *Checker) checkArrayConstructor(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := c.rejectArrayElementType(elem); err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", fmt.Errorf("move error: `std::array::Array<%s>` expects allocator", elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", fmt.Errorf("move error: `std::array::Array<%s>` expects Allocator, got %s",
			elem, got)
	}
	return fmt.Sprintf("std::array::Array<%s>", elem), nil
}

// checkMapConstructor validates std::map::Map<[]const u8, V>(allocator) ownership.
func (c *Checker) checkMapConstructor(
	argsText string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	mapArgs, err := c.checkedMapArgs(argsText)
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", fmt.Errorf("map error: `std::map::Map` expects allocator")
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", fmt.Errorf("map error: `std::map::Map` expects Allocator, got %s", got)
	}
	return fmt.Sprintf("std::map::Map<[]const u8, %s>", mapArgs[1]), nil
}

// rejectArrayElementType rejects element types with unresolved ownership hazards.
func (c *Checker) rejectArrayElementType(elem string) error {
	if err := c.rejectArrayStorageType(elem, map[string]bool{}); err != nil {
		return fmt.Errorf("array error: Array element is not safe in v0.2: %w", err)
	}
	return nil
}

// rejectArrayStorageType rejects values whose lifetime rules are not Array-safe yet.
func (c *Checker) rejectArrayStorageType(typeName string, seen map[string]bool) error {
	if seen[typeName] {
		return nil
	}
	seen[typeName] = true
	if isRawPointerType(typeName) {
		return fmt.Errorf("array error: Array element cannot be raw pointer in v0.2")
	}
	if err := c.rejectArrayStorageGeneric(typeName, seen); err != nil {
		return err
	}
	if err := c.rejectArrayStorageStruct(typeName, seen); err != nil {
		return err
	}
	return c.rejectArrayStorageUnion(typeName, seen)
}

// rejectArrayStorageGeneric applies Array-specific generic exclusions.
func (c *Checker) rejectArrayStorageGeneric(typeName string, seen map[string]bool) error {
	base, arg, ok := splitGenericType(typeName)
	if !ok {
		return nil
	}
	switch base {
	case "arena":
		return fmt.Errorf("array error: Array element cannot be arena in v0.2")
	case "handle":
		return fmt.Errorf("array error: Array element cannot be handle in v0.2")
	case "std::array::Array":
		return fmt.Errorf("array error: Array element cannot be nested array in v0.2")
	case "std::map::Map":
		return fmt.Errorf("array error: Array element cannot be std::map::Map in v0.2")
	case "Task", "Channel", "Mutex", "Atomic", "Dyn":
		return fmt.Errorf("array error: Array element cannot be %s in v0.2", base)
	case "option":
		return c.rejectArrayStorageType(arg, seen)
	default:
		return nil
	}
}

// rejectArrayStorageStruct checks struct fields recursively for Array storage.
func (c *Checker) rejectArrayStorageStruct(typeName string, seen map[string]bool) error {
	fields := c.structs[typeName]
	for fieldName, fieldType := range fields {
		if err := c.rejectArrayStorageType(fieldType, seen); err != nil {
			return fmt.Errorf("array error: struct `%s.%s` cannot be Array element: %w",
				typeName, fieldName, err)
		}
	}
	return nil
}

// rejectArrayStorageUnion checks union payloads recursively for Array storage.
func (c *Checker) rejectArrayStorageUnion(typeName string, seen map[string]bool) error {
	variants := c.unions[typeName]
	for variant, payload := range variants {
		if payload == "" {
			continue
		}
		if err := c.rejectArrayStorageType(payload, seen); err != nil {
			return fmt.Errorf("array error: union `%s::%s` cannot be Array element: %w",
				typeName, variant, err)
		}
	}
	return nil
}

// checkIoArg reads and validates an explicit Io argument.
func (c *Checker) checkIoArg(arg ast.Expression, env *scope, name string) error {
	got, err := c.readExpr(arg, env)
	if err != nil {
		return err
	}
	if got != "Io" {
		return fmt.Errorf("move error: `%s` expects Io, got %s", name, got)
	}
	return nil
}

// checkIoBuiltin validates std::io constructor ownership effects.
func checkIoBuiltin(name string, args []ast.Expression) (string, bool, error) {
	switch name {
	case "std.builtin.io_blocking", "std.builtin.io_threaded", "std.builtin.io_failing":
		_, err := checkNoArgOwnershipCall(name, args)
		if err != nil {
			return "", true, err
		}
		return "Io", true, nil
	case "std.io.evented", "std.builtin.io_evented":
		return "", true, fmt.Errorf("move error: `std::io::evented` is not implemented in v0.1")
	default:
		return "", false, nil
	}
}

// checkTypeApplyCallExpr validates typed std constructor ownership effects.
func (c *Checker) checkTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return "", fmt.Errorf("move error: unsupported type application `%s`", expr.String())
	}
	switch name {
	case "std.array.Array":
		return c.checkArrayConstructor(expr.TypeArg, args, env)
	case "std.map.Map":
		return c.checkMapConstructor(expr.TypeArg, args, env)
	case "std.channel.Channel":
		_, err := checkNoArgOwnershipCall(name, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Channel<%s>", expr.TypeArg), nil
	case "std.atomic.Atomic":
		typ, _, err := c.checkAtomic(expr.TypeArg, args, env)
		return typ, err
	case "std.sync.Mutex":
		typ, _, err := c.checkMutex(expr.TypeArg, args, env)
		return typ, err
	default:
		return "", fmt.Errorf("move error: `%s` does not take a type argument", name)
	}
}

// checkBuiltinCall validates ownership effects for builtin calls.
func (c *Checker) checkBuiltinCall(
	name string,
	expr *ast.CallExpr,
	env *scope,
) (string, bool, error) {
	switch name {
	case "print":
		result, err := c.checkPrintCall(expr, env)
		return result, true, err
	case "ptr_read", "ptr_write":
		result, err := c.checkPointerBuiltin(expr, env)
		return result, true, err
	case "error":
		result, err := c.checkErrorCall(expr, env)
		return result, true, err
	case "Io":
		return "", true, fmt.Errorf("move error: use `std::io::blocking()`")
	case "TaskGroup":
		return "", true, fmt.Errorf("move error: use `std::task::Group(io)`")
	default:
		return "", false, nil
	}
}

// checkErrorCall reads and copies the message into the error payload.
func (c *Checker) checkErrorCall(expr *ast.CallExpr, env *scope) (string, error) {
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("move error: `error` expects 1 arg, got %d", len(expr.Args))
	}
	if _, err := c.readExpr(expr.Args[0], env); err != nil {
		return "", err
	}
	return "!unknown", nil
}

// readTryExpr reads a !T expression and returns T.
func (c *Checker) readTryExpr(expr *ast.TryExpr, env *scope) (string, error) {
	got, err := c.readExpr(expr.Value, env)
	if err != nil {
		return "", err
	}
	arg, ok := errorUnionElement(got)
	if !ok {
		return "", fmt.Errorf("move error: try expects !T, got %s", got)
	}
	return arg, nil
}

// checkPointerBuiltin reads raw pointer builtin arguments without moving values.
func (c *Checker) checkPointerBuiltin(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	if name, ok := expr.Callee.(*ast.IdentExpr); ok && name.Name == "ptr_read" {
		return "i64", nil
	}
	return "void", nil
}

// readStructLiteralExpr checks a literal without consuming field values.
func (c *Checker) readStructLiteralExpr(expr *ast.StructLiteralExpr, env *scope) (string, error) {
	for _, field := range expr.Fields {
		if _, err := c.readExpr(field.Value, env); err != nil {
			return "", err
		}
	}
	return expr.TypeName, nil
}

// moveStructLiteralExpr moves field values into a new struct value.
func (c *Checker) moveStructLiteralExpr(expr *ast.StructLiteralExpr, env *scope) (string, error) {
	for _, field := range expr.Fields {
		if _, err := c.moveExpr(field.Value, env); err != nil {
			return "", err
		}
	}
	return expr.TypeName, nil
}

// readFieldExpr reads a field without moving the receiver.
func (c *Checker) readFieldExpr(expr *ast.FieldExpr, env *scope) (string, error) {
	if expr.Namespace {
		return c.readNamespaceExpr(expr)
	}
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if _, exists := c.enums[ident.Name]; exists {
			return "", fmt.Errorf("move error: enum tag `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
		if _, exists := c.unions[ident.Name]; exists {
			return "", fmt.Errorf("move error: union variant `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", err
	}
	if root, field, ok := directFieldRoot(expr, env); ok {
		if root.activeMutBorrows > 0 {
			return "", fmt.Errorf("borrow error: value `%s` cannot be read while mutably borrowed",
				root.name)
		}
		if root.fieldMutBorrows[field] > 0 {
			return "", fmt.Errorf("borrow error: field `%s.%s` cannot be read while mutably borrowed",
				root.name, field)
		}
	}
	if fields := c.structs[receiverType]; fields != nil {
		if typ, ok := fields[expr.Name]; ok {
			return typ, nil
		}
	}
	if receiverType == "std::fs::Metadata" {
		switch expr.Name {
		case "size":
			return "i64", nil
		case "is_dir":
			return "bool", nil
		}
	}
	return receiverType, nil
}

// readNamespaceExpr reads enum or payload-free union namespace lookup.
func (c *Checker) readNamespaceExpr(expr *ast.FieldExpr) (string, error) {
	ident, ok := expr.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("move error: invalid namespace lookup `%s`", expr.String())
	}
	if tags, exists := c.enums[ident.Name]; exists {
		if !tags[expr.Name] {
			return "", fmt.Errorf("move error: unknown enum tag `%s::%s`", ident.Name, expr.Name)
		}
		return ident.Name, nil
	}
	if variants, exists := c.unions[ident.Name]; exists {
		payload, ok := variants[expr.Name]
		if !ok {
			return "", fmt.Errorf("move error: unknown union variant `%s::%s`",
				ident.Name, expr.Name)
		}
		if payload != "" {
			return "", fmt.Errorf("move error: union variant `%s::%s` expects payload",
				ident.Name, expr.Name)
		}
		return ident.Name, nil
	}
	return "", fmt.Errorf("move error: unknown namespace `%s`", ident.Name)
}

// moveFieldExpr rejects partial moves from borrowed or aggregate values.
func (c *Checker) moveFieldExpr(expr *ast.FieldExpr, env *scope) (string, error) {
	typeName, err := c.readFieldExpr(expr, env)
	if err != nil {
		return "", err
	}
	if c.isCopyType(typeName) {
		return typeName, nil
	}
	if name, ok := c.borrowedFieldRoot(expr, env); ok {
		return "", fmt.Errorf(
			"borrow error: field `%s` cannot be moved out of borrowed value `%s`",
			expr.String(),
			name,
		)
	}
	if c.containsArenaGet(expr.Receiver) {
		return "", fmt.Errorf(
			"arena error: arena.get returns a local borrow and its fields cannot be moved",
		)
	}
	return "", fmt.Errorf("move error: field `%s` cannot be moved out of aggregate", expr.String())
}

// checkAssignmentBorrowConflict rejects writes that overlap active borrows.
func (c *Checker) checkAssignmentBorrowConflict(expr ast.Expression, env *scope) error {
	root, field, ok := directFieldRoot(expr, env)
	if !ok {
		return nil
	}
	if field == "" {
		if root.hasAnyBorrow() && !c.isCopyType(root.typeName) {
			return fmt.Errorf("borrow error: value `%s` cannot be assigned while borrowed", root.name)
		}
		return nil
	}
	if root.activeBorrows > 0 || root.activeMutBorrows > 0 {
		return fmt.Errorf("borrow error: field `%s.%s` cannot be assigned while value is borrowed",
			root.name, field)
	}
	if root.fieldBorrows[field] > 0 || root.fieldMutBorrows[field] > 0 {
		return fmt.Errorf("borrow error: field `%s.%s` cannot be assigned while borrowed",
			root.name, field)
	}
	return nil
}

// borrowedFieldRoot returns the borrowed identifier at the root of a field chain.
func (c *Checker) borrowedFieldRoot(expr ast.Expression, env *scope) (string, bool) {
	switch e := expr.(type) {
	case *ast.FieldExpr:
		return c.borrowedFieldRoot(e.Receiver, env)
	case *ast.DerefExpr:
		return c.borrowedFieldRoot(e.Receiver, env)
	case *ast.IdentExpr:
		value, ok := env.lookup(e.Name)
		return e.Name, ok && value.borrowedParam
	default:
		return "", false
	}
}

// readDerefExpr reads the value behind a local borrow parameter.
func (c *Checker) readDerefExpr(expr *ast.DerefExpr, env *scope) (string, error) {
	ident, ok := expr.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("borrow error: dereference expects a local borrow")
	}
	value, ok := env.lookup(ident.Name)
	if !ok {
		return "", fmt.Errorf("move error: undefined variable `%s`", ident.Name)
	}
	if !value.borrowedParam {
		return "", fmt.Errorf("borrow error: `%s` is not a borrow", ident.Name)
	}
	return value.typeName, nil
}

// assignmentRoot finds the binding invalidated by an assignment target.
func assignmentRoot(expr ast.Expression, env *scope) (*binding, bool) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		return env.lookup(target.Name)
	case *ast.FieldExpr:
		return assignmentRoot(target.Receiver, env)
	case *ast.DerefExpr:
		return assignmentRoot(target.Receiver, env)
	default:
		return nil, false
	}
}

// directAssignmentRoot returns a binding only for whole-binding assignment.
func directAssignmentRoot(expr ast.Expression, env *scope) (*binding, bool) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}
	return env.lookup(ident.Name)
}

// checkUnionConstructor validates ownership effects of Union.Variant(payload).
func (c *Checker) checkUnionConstructor(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if !field.Namespace {
		if ident, ok := field.Receiver.(*ast.IdentExpr); ok {
			if _, exists := c.enums[ident.Name]; exists {
				return "", true, fmt.Errorf("move error: enum tag `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
			if _, exists := c.unions[ident.Name]; exists {
				return "", true, fmt.Errorf("move error: union variant `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
		}
		return "", false, nil
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", false, nil
	}
	variants := c.unions[ident.Name]
	if variants == nil {
		return "", false, nil
	}
	payload, exists := variants[field.Name]
	if !exists {
		return "", true, fmt.Errorf("move error: unknown union variant `%s::%s`",
			ident.Name, field.Name)
	}
	if payload == "" {
		return "", true, fmt.Errorf("move error: union variant `%s::%s` expects 0 args",
			ident.Name, field.Name)
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("move error: union variant `%s::%s` expects 1 arg, got %d",
			ident.Name, field.Name, len(args))
	}
	if _, err := c.moveExpr(args[0], env); err != nil {
		return "", true, err
	}
	return ident.Name, true, nil
}

// checkMethodCallExpr validates ownership effects of arena methods.
func (c *Checker) checkMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if typ, ok, err := c.checkStdFieldStorageMethod(field, args, env); ok || err != nil {
		return typ, err
	}
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("arena error: arena method receiver must be a local binding")
	}
	arena, exists := env.lookup(receiver.Name)
	if !exists {
		return "", fmt.Errorf("arena error: undefined arena `%s`", receiver.Name)
	}
	if arena.moved {
		return "", fmt.Errorf("move error: moved value `%s` was used", receiver.Name)
	}
	base, elem, ok := splitGenericType(arena.typeName)
	if ok && base == "std::array::Array" {
		return c.checkArrayMethod(arena, elem, field.Name, args, env)
	}
	if ok && base == "std::map::Map" {
		if err := checkMapReceiverBorrow(arena, field.Name); err != nil {
			return "", err
		}
		return c.checkMapMethod(arena, elem, field.Name, args, env)
	}
	if !ok || base != "arena" {
		return c.checkNonArenaMethod(arena, field.Name, args, env)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arena, args, env)
	case "get":
		return c.checkArenaGet(arena, args, env)
	default:
		return "", fmt.Errorf("arena error: unknown arena method `%s`", field.Name)
	}
}

// checkStdFieldStorageMethod allows std wrappers to mutate private storage fields.
func (c *Checker) checkStdFieldStorageMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if !c.currentStd {
		return "", false, nil
	}
	if _, ok := field.Receiver.(*ast.FieldExpr); !ok {
		return "", false, nil
	}
	receiverType, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return "", true, err
	}
	base, elem, ok := splitGenericType(receiverType)
	if !ok || base != "std::array::Array" {
		return "", false, nil
	}
	array := &binding{typeName: receiverType}
	typ, err := c.checkArrayMethod(array, elem, field.Name, args, env)
	return typ, true, err
}

// checkNonArenaMethod validates methods on non-arena owned values.
func (c *Checker) checkNonArenaMethod(
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if value.typeName == "std::string::String" {
		if err := checkStringReceiverBorrow(value, name); err != nil {
			return "", err
		}
		return c.checkStringMethod(value, name, args, env)
	}
	if err := checkMapReceiverBorrow(value, name); err != nil {
		return "", err
	}
	if value.typeName == "TaskGroup" {
		return c.checkTaskGroupMethod(name, args, env)
	}
	if elem, ok := taskElement(value.typeName); ok {
		return c.checkTaskMethod(value, name, elem, args)
	}
	typ, ok, err := c.checkConcurrencyMethod(value.typeName, name, args, env)
	if ok || err != nil {
		return typ, err
	}
	return c.checkPlainMethodArgs(args, env)
}

// checkStringReceiverBorrow rejects String methods whose receiver cannot be tracked safely.
func checkStringReceiverBorrow(value *binding, name string) error {
	if name == "deinit" && value.borrowedParam {
		return fmt.Errorf("string error: `String.deinit` requires owned String receiver")
	}
	if isStringMutatingMethod(name) && value.borrowedParam && !value.mutBorrow {
		return fmt.Errorf("string error: `String.%s` requires mutable String receiver", name)
	}
	return nil
}

// checkMapReceiverBorrow rejects Map methods whose receiver cannot be tracked safely.
func checkMapReceiverBorrow(value *binding, name string) error {
	base, _, ok := splitGenericType(value.typeName)
	if !ok || base != "std::map::Map" {
		return nil
	}
	if name == "deinit" && value.borrowedParam {
		return fmt.Errorf("map error: `Map.deinit` requires owned Map receiver")
	}
	if name == "insert" && value.borrowedParam && !value.mutBorrow {
		return fmt.Errorf("map error: `Map.insert` requires mutable Map receiver")
	}
	return nil
}

// checkStringMethod validates ownership effects for owned String methods.
func (c *Checker) checkStringMethod(
	str *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "append_bytes", "append_byte", "reserve", "truncate":
		if err := checkStringMutationAllowed(str, name); err != nil {
			return "", err
		}
		return c.checkStringAppendOrReserve(name, args, env)
	case "len", "capacity":
		if err := checkStringNoArgs(name, args); err != nil {
			return "", err
		}
		return "i64", nil
	case "as_bytes":
		return "", fmt.Errorf(
			"string error: `String.as_bytes` must be bound with `let name = string.as_bytes()`")
	case "clear", "deinit":
		if err := checkStringMutationAllowed(str, name); err != nil {
			return "", err
		}
		if err := checkStringNoArgs(name, args); err != nil {
			return "", err
		}
		if name == "deinit" {
			str.moved = true
		}
		return "void", nil
	default:
		return "", fmt.Errorf("string error: String has no method `%s`", name)
	}
}

// checkStringAppendOrReserve validates one-argument String mutators.
func (c *Checker) checkStringAppendOrReserve(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "append_bytes":
		return c.checkStringBytesArg(name, args, env)
	case "append_byte":
		return c.checkStringByteArg(name, args, env)
	default:
		return c.checkStringReserveArg(name, args, env)
	}
}

// checkStringMutationAllowed rejects mutation while a byte view is alive.
func checkStringMutationAllowed(str *binding, name string) error {
	if str.hasAnyBorrow() {
		return fmt.Errorf("string error: `String.%s` cannot run while string is borrowed", name)
	}
	return nil
}

// checkStringNoArgs validates no-argument String methods.
func checkStringNoArgs(name string, args []ast.Expression) error {
	if len(args) != 0 {
		return fmt.Errorf("string error: `String.%s` expects 0 args, got %d", name, len(args))
	}
	return nil
}

// isStringMutatingMethod reports whether a String method can change owned storage.
func isStringMutatingMethod(name string) bool {
	switch name {
	case "append_bytes", "append_byte", "reserve", "truncate", "clear", "deinit":
		return true
	default:
		return false
	}
}

// checkStringBytesArg validates append_bytes without moving the source slice.
func (c *Checker) checkStringBytesArg(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "[]const u8" {
		return "", fmt.Errorf("string error: `String.%s` expects []const u8, got %s", name, got)
	}
	return "!void", nil
}

// checkStringReserveArg validates reserve without moving the count.
func (c *Checker) checkStringReserveArg(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", fmt.Errorf("string error: `String.%s` expects i64, got %s", name, got)
	}
	return "!void", nil
}

// checkStringByteArg validates append_byte without moving the source value.
func (c *Checker) checkStringByteArg(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "u8" {
		return "", fmt.Errorf("string error: `String.%s` expects u8, got %s", name, got)
	}
	return "!void", nil
}

// checkConcurrencyMethod validates std concurrency prototype method moves.
func (c *Checker) checkConcurrencyMethod(
	receiver string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	base, arg, generic := splitGenericType(receiver)
	if generic {
		switch base {
		case "Channel":
			typ, err := c.checkChannelMethod(arg, name, args, env)
			return typ, true, err
		case "Mutex":
			typ, err := c.checkMutexMethod(arg, name, args, env)
			return typ, true, err
		case "Atomic":
			typ, err := c.checkAtomicMethod(arg, name, args, env)
			return typ, true, err
		}
	}
	switch receiver {
	case "Queue":
		typ, err := c.checkQueueMethod(name, args, env)
		return typ, true, err
	case "Partition":
		typ, err := c.checkPartitionMethod(name, args, env)
		return typ, true, err
	case "LocalBuffer":
		typ, err := c.checkLocalBufferMethod(name, args, env)
		return typ, true, err
	default:
		return "", false, nil
	}
}

// checkTaskGroupMethod validates spawn ownership effects.
func (c *Checker) checkTaskGroupMethod(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if name != "spawn" {
		return "", fmt.Errorf("task error: TaskGroup has no method `%s`", name)
	}
	if len(args) < 1 {
		return "", fmt.Errorf("task error: `TaskGroup.spawn` expects function and args")
	}
	target, ok := args[0].(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("task error: `TaskGroup.spawn` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		if _, ok := env.lookup(target.Name); ok {
			return "", fmt.Errorf("task error: `TaskGroup.spawn` expects function name")
		}
		return "", fmt.Errorf("task error: undefined function `%s`", target.Name)
	}
	spawnArgs := args[1:]
	if len(fn.params) == 0 || fn.params[0].typeName != "Io" ||
		fn.params[0].borrow || fn.params[0].mutBorrow {
		return "", fmt.Errorf("task error: spawned function `%s` must accept owned Io as first parameter",
			target.Name)
	}
	if len(spawnArgs) != len(fn.params)-1 {
		return "", fmt.Errorf("task error: `%s` expects %d args, got %d",
			target.Name, len(fn.params)-1, len(spawnArgs))
	}
	for idx, arg := range spawnArgs {
		paramIdx := idx + 1
		if fn.params[paramIdx].borrow {
			return "", fmt.Errorf("task error: task cannot capture borrow parameter `%s`", target.Name)
		}
		if err := c.rejectConcurrencyBoundaryArg(arg, env); err != nil {
			return "", err
		}
		if _, err := c.moveExpr(arg, env); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Task<%s>", returnTypeName(fn)), nil
}

// checkTaskGroup validates a task group bound to one Io implementation.
func (c *Checker) checkTaskGroup(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("task error: `std::task::Group` expects io")
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != "Io" {
		return "", true, fmt.Errorf("task error: `std::task::Group` expects Io, got %s", got)
	}
	return "TaskGroup", true, nil
}

// checkTaskMethod marks task completion for await/cancel.
func (c *Checker) checkTaskMethod(
	task *binding,
	name string,
	elem string,
	args []ast.Expression,
) (string, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("task error: `task.%s` expects 0 args, got %d", name, len(args))
	}
	if task.taskDone {
		return "", fmt.Errorf("task error: task `%s` was already completed", task.name)
	}
	switch name {
	case "await":
		task.taskDone = true
		return elem, nil
	case "cancel":
		task.taskDone = true
		return "void", nil
	default:
		return "", fmt.Errorf("task error: Task has no method `%s`", name)
	}
}

// checkQueueMethod applies deterministic deferred queue move rules.
func (c *Checker) checkQueueMethod(name string, args []ast.Expression, env *scope) (string, error) {
	switch name {
	case "enqueue":
		return c.checkQueueEnqueue(args, env)
	case "drain":
		if len(args) != 0 {
			return "", fmt.Errorf("task error: `queue.drain` expects 0 args, got %d", len(args))
		}
		return "void", nil
	default:
		return "", fmt.Errorf("task error: Queue has no method `%s`", name)
	}
}

// checkQueueEnqueue moves queued function arguments into the queue.
func (c *Checker) checkQueueEnqueue(args []ast.Expression, env *scope) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("task error: `queue.enqueue` expects io, function, and args")
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("task error: `queue.enqueue` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", fmt.Errorf("task error: undefined function `%s`", target.Name)
	}
	spawnArgs := append([]ast.Expression{args[0]}, args[2:]...)
	if len(spawnArgs) != len(fn.params) {
		return "", fmt.Errorf("task error: `%s` expects %d args, got %d",
			target.Name, len(fn.params), len(spawnArgs))
	}
	for idx, arg := range spawnArgs {
		if fn.params[idx].borrow {
			return "", fmt.Errorf("task error: queue cannot capture borrow parameter `%s`", target.Name)
		}
		if err := c.rejectConcurrencyBoundaryArg(arg, env); err != nil {
			return "", err
		}
		if _, err := c.moveExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "void", nil
}

// checkChannelMethod applies owned message passing move rules.
func (c *Checker) checkChannelMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "send":
		if len(args) != 1 {
			return "", fmt.Errorf("channel error: `channel.send` expects 1 arg, got %d", len(args))
		}
		if err := c.rejectConcurrencyBoundaryArg(args[0], env); err != nil {
			return "", err
		}
		got, err := c.moveExpr(args[0], env)
		if err != nil {
			return "", err
		}
		if got != elem {
			return "", fmt.Errorf("channel error: `channel.send` expects %s, got %s", elem, got)
		}
		return "void", nil
	case "recv":
		if len(args) != 0 {
			return "", fmt.Errorf("channel error: `channel.recv` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "close":
		if len(args) != 0 {
			return "", fmt.Errorf("channel error: `channel.close` expects 0 args, got %d", len(args))
		}
		return "void", nil
	default:
		return "", fmt.Errorf("channel error: Channel has no method `%s`", name)
	}
}

// checkPartitionMethod validates disjoint partition marker reads.
func (c *Checker) checkPartitionMethod(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if name != "at" {
		return "", fmt.Errorf("parallel error: Partition has no method `%s`", name)
	}
	return c.checkOneI64Arg("partition.at", args, env)
}

// checkLocalBufferMethod validates worker-local scratch reads.
func (c *Checker) checkLocalBufferMethod(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if name != "get" {
		return "", fmt.Errorf("parallel error: LocalBuffer has no method `%s`", name)
	}
	return c.checkOneI64Arg("LocalBuffer.get", args, env)
}

// checkArrayMethod validates ownership effects for owned Array<T> methods.
func (c *Checker) checkArrayMethod(
	array *binding,
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if isStdArrayStorageMethod(name) {
		return c.checkStdArrayStorageMethod(array, elem, name, args, env)
	}
	switch name {
	case "append":
		return c.checkArrayAppend(array, elem, args, env)
	case "len", "capacity":
		return c.checkArrayReadNoArgs(array, name, args)
	case "get":
		if array.activeMutBorrows > 0 {
			return "", fmt.Errorf("array error: `Array.get` cannot read while mutably borrowed")
		}
		return c.checkArrayGet(elem, args, env)
	case "at", "at_mut":
		return "", fmt.Errorf("array error: `Array.%s` must be bound with `let name = try array.%s(...)`",
			name, name)
	case "set":
		return c.checkArraySet(array, elem, args, env)
	case "deinit":
		if array.hasAnyBorrow() {
			return "", fmt.Errorf("array error: `Array.%s` cannot run while array is borrowed", name)
		}
		if len(args) != 0 {
			return "", fmt.Errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
		}
		if name == "deinit" {
			array.moved = true
		}
		return "void", nil
	default:
		return "", fmt.Errorf("array error: Array has no method `%s`", name)
	}
}

// checkStdArrayStorageMethod validates Array helpers reserved to std source.
func (c *Checker) checkStdArrayStorageMethod(
	array *binding,
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if !c.currentStd {
		return "", fmt.Errorf("array error: Array has no method `%s`", name)
	}
	switch name {
	case "reserve", "truncate":
		return c.checkArrayCountMutation(array, name, args, env)
	case "clear":
		if array.hasAnyBorrow() {
			return "", fmt.Errorf("array error: `Array.clear` cannot run while array is borrowed")
		}
		if len(args) != 0 {
			return "", fmt.Errorf("array error: `Array.clear` expects 0 args, got %d", len(args))
		}
		return "void", nil
	default:
		if elem != "u8" {
			return "", fmt.Errorf("array error: `Array.as_bytes` requires Array<u8>")
		}
		return c.checkArrayReadNoArgs(array, name, args)
	}
}

// isStdArrayStorageMethod reports methods reserved for std-owned storage wrappers.
func isStdArrayStorageMethod(name string) bool {
	return name == "reserve" || name == "truncate" || name == "clear" || name == "as_bytes"
}

// checkArrayCountMutation validates one-count Array mutations.
func (c *Checker) checkArrayCountMutation(
	array *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if array.hasAnyBorrow() {
		return "", fmt.Errorf("array error: `Array.%s` cannot run while array is borrowed", name)
	}
	if len(args) != 1 {
		return "", fmt.Errorf("array error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "i64" {
		return "", fmt.Errorf("array error: `Array.%s` expects i64, got %s", name, got)
	}
	return "!void", nil
}

// checkArrayAppend validates append mutation and element move.
func (c *Checker) checkArrayAppend(
	array *binding,
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if array.hasAnyBorrow() {
		return "", fmt.Errorf("array error: `Array.append` cannot run while array is borrowed")
	}
	if len(args) != 1 {
		return "", fmt.Errorf("array error: `Array.append` expects 1 arg, got %d", len(args))
	}
	got, err := c.moveExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", fmt.Errorf("array error: `Array.append` expects %s, got %s", elem, got)
	}
	return "!void", nil
}

// checkArrayReadNoArgs validates len/capacity reads.
func (c *Checker) checkArrayReadNoArgs(
	array *binding,
	name string,
	args []ast.Expression,
) (string, error) {
	if array.activeMutBorrows > 0 {
		return "", fmt.Errorf("array error: `Array.%s` cannot read while mutably borrowed", name)
	}
	if len(args) != 0 {
		return "", fmt.Errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
	}
	return "i64", nil
}

// checkArraySet validates checked element replacement.
func (c *Checker) checkArraySet(
	array *binding,
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if array.hasAnyBorrow() {
		return "", fmt.Errorf("array error: `Array.set` cannot run while array is borrowed")
	}
	if len(args) != 2 {
		return "", fmt.Errorf("array error: `Array.set` expects 2 args, got %d", len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "i64" {
		return "", fmt.Errorf("array error: `Array.set` expects i64 index, got %s", got)
	}
	got, err := c.moveExpr(args[1], env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", fmt.Errorf("array error: `Array.set` expects %s value, got %s", elem, got)
	}
	return "!void", nil
}

// checkArrayGet validates copy-only Array<T> reads in the v0.2 prototype.
func (c *Checker) checkArrayGet(elem string, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("array error: `Array.get` expects 1 arg, got %d", len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", fmt.Errorf("array error: `Array.get` expects i64 index, got %s", got)
	}
	if !c.isCopyType(elem) {
		return "", fmt.Errorf("array error: `Array.get` requires copy element in v0.2")
	}
	return "!" + elem, nil
}

// checkMapMethod validates ownership effects for owned Map<[]const u8, V> methods.
func (c *Checker) checkMapMethod(
	mapValue *binding,
	argsText string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	mapArgs, err := c.checkedMapArgs(argsText)
	if err != nil {
		return "", err
	}
	valueType := mapArgs[1]
	switch name {
	case "insert":
		return c.checkMapInsert(mapValue, valueType, args, env)
	case "get":
		if err := c.checkMapKeyArg(name, args, env); err != nil {
			return "", err
		}
		return "!" + valueType, nil
	case "contains":
		if err := c.checkMapKeyArg(name, args, env); err != nil {
			return "", err
		}
		return "bool", nil
	case "len":
		return c.checkMapReadNoArgs(name, args)
	case "deinit":
		return c.checkMapDeinit(mapValue, args)
	default:
		return "", fmt.Errorf("map error: Map has no method `%s`", name)
	}
}

// checkMapInsert validates read-only key and copy value insertion.
func (c *Checker) checkMapInsert(
	mapValue *binding,
	valueType string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if mapValue.hasAnyBorrow() {
		return "", fmt.Errorf("map error: `Map.insert` cannot run while map is borrowed")
	}
	if len(args) != 2 {
		return "", fmt.Errorf("map error: `Map.insert` expects 2 args, got %d", len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "[]const u8" {
		return "", fmt.Errorf("map error: `Map.insert` expects []const u8 key, got %s", got)
	}
	got, err := c.readExpr(args[1], env)
	if err != nil {
		return "", err
	}
	if got != valueType {
		return "", fmt.Errorf("map error: `Map.insert` expects %s value, got %s", valueType, got)
	}
	return "!void", nil
}

// checkMapKeyArg validates one []const u8 lookup key.
func (c *Checker) checkMapKeyArg(name string, args []ast.Expression, env *scope) error {
	if len(args) != 1 {
		return fmt.Errorf("map error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	if got != "[]const u8" {
		return fmt.Errorf("map error: `Map.%s` expects []const u8 key, got %s", name, got)
	}
	return nil
}

// checkMapReadNoArgs validates no-argument Map reads.
func (c *Checker) checkMapReadNoArgs(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("map error: `Map.%s` expects 0 args, got %d", name, len(args))
	}
	return "i64", nil
}

// checkMapDeinit validates owned Map cleanup and marks it moved.
func (c *Checker) checkMapDeinit(mapValue *binding, args []ast.Expression) (string, error) {
	if mapValue.hasAnyBorrow() {
		return "", fmt.Errorf("map error: `Map.deinit` cannot run while map is borrowed")
	}
	if len(args) != 0 {
		return "", fmt.Errorf("map error: `Map.deinit` expects 0 args, got %d", len(args))
	}
	mapValue.moved = true
	return "void", nil
}

// checkAtomicMethod validates seq_cst-only integer atomic operations.
func (c *Checker) checkAtomicMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "load":
		if len(args) != 0 {
			return "", fmt.Errorf("atomic error: `atomic.load` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "store":
		if len(args) != 1 {
			return "", fmt.Errorf("atomic error: `atomic.store` expects 1 arg, got %d", len(args))
		}
		got, err := c.readExpr(args[0], env)
		if err != nil {
			return "", err
		}
		if got != elem {
			return "", fmt.Errorf("atomic error: `atomic.store` expects %s, got %s", elem, got)
		}
		return "void", nil
	default:
		return "", fmt.Errorf("atomic error: Atomic has no method `%s`", name)
	}
}

// checkMutexMethod validates the minimal synchronized wrapper API.
func (c *Checker) checkMutexMethod(
	elem string,
	name string,
	args []ast.Expression,
	_ *scope,
) (string, error) {
	if name != "get" {
		return "", fmt.Errorf("sync error: Mutex has no method `%s`", name)
	}
	if len(args) != 0 {
		return "", fmt.Errorf("sync error: `mutex.get` expects 0 args, got %d", len(args))
	}
	return elem, nil
}

// checkOneI64Arg reads one i64 argument.
func (c *Checker) checkOneI64Arg(name string, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("parallel error: `%s` expects 1 arg, got %d", name, len(args))
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return "i64", nil
}

// checkPlainMethodArgs reads non-arena method arguments after type checking.
func (c *Checker) checkPlainMethodArgs(args []ast.Expression, env *scope) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "!unknown", nil
}

// checkArenaAdd moves one value into an arena and returns a handle.
func (c *Checker) checkArenaAdd(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("arena error: `arena.add` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "arena" {
		return "", fmt.Errorf("arena error: `%s` is not an arena", arena.name)
	}
	got, err := c.moveExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != arg {
		return "", fmt.Errorf("arena error: `arena.add` expects %s, got %s", arg, got)
	}
	return fmt.Sprintf("handle<%s>", arg), nil
}

// checkArenaGet reads a handle and returns a local borrow-like value.
func (c *Checker) checkArenaGet(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("arena error: `arena.get` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "arena" {
		return "", fmt.Errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.checkHandleProvenance(arena, args[0], env); err != nil {
		return "", err
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return arg, nil
}

// checkHandleProvenance rejects handles that came from a different known arena.
func (c *Checker) checkHandleProvenance(arena *binding, expr ast.Expression, env *scope) error {
	if arena.arenaID == 0 {
		return fmt.Errorf("arena error: arena `%s` has unknown provenance", arena.name)
	}
	if addArena := c.arenaAddReceiver(expr, env); addArena != nil {
		if addArena.arenaID != arena.arenaID {
			return fmt.Errorf("arena error: handle from `%s` does not belong to arena `%s`",
				addArena.name, arena.name)
		}
		return nil
	}
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return fmt.Errorf("arena error: handle expression has unknown arena provenance")
	}
	handle, exists := env.lookup(ident.Name)
	if !exists {
		return fmt.Errorf("arena error: undefined handle `%s`", ident.Name)
	}
	if handle.handleArenaID == 0 {
		return fmt.Errorf("arena error: handle `%s` has unknown arena provenance", ident.Name)
	}
	if handle.handleArenaID != arena.arenaID {
		return fmt.Errorf("arena error: handle `%s` does not belong to arena `%s`",
			ident.Name, arena.name)
	}
	return nil
}

// activateBorrowArgs marks identifier arguments that are borrowed for this call.
func (c *Checker) activateBorrowArgs(
	fn *functionInfo,
	args []ast.Expression,
	env *scope,
) ([]*binding, error) {
	borrowed := []*binding{}
	for idx, arg := range args {
		if !fn.params[idx].borrow {
			continue
		}
		value, err := borrowedIdent(arg, env)
		if err != nil {
			releaseBorrows(borrowed)
			return nil, err
		}
		if value != nil {
			if err := checkBorrowConflict(value, fn.params[idx].mutBorrow); err != nil {
				releaseBorrows(borrowed)
				return nil, err
			}
			if fn.params[idx].mutBorrow {
				value.activeMutBorrows++
			} else {
				value.activeBorrows++
			}
			borrowed = append(borrowed, value)
		}
	}
	return borrowed, nil
}

// checkBorrowConflict rejects aliasing that would overlap a mutable borrow.
func checkBorrowConflict(value *binding, mutable bool) error {
	return checkBorrowConflictForField(value, "", mutable)
}

// checkBorrowConflictForField rejects overlapping whole-value or field borrows.
func checkBorrowConflictForField(value *binding, field string, mutable bool) error {
	if field != "" {
		if value.activeMutBorrows > 0 {
			return fmt.Errorf(
				"borrow error: value `%s` cannot be borrowed while mutably borrowed",
				value.name,
			)
		}
		if mutable && value.activeBorrows > 0 {
			return fmt.Errorf(
				"borrow error: field `%s.%s` cannot be mutably borrowed while value is borrowed",
				value.name,
				field,
			)
		}
		if mutable && value.fieldBorrows[field] > 0 {
			return fmt.Errorf(
				"borrow error: field `%s.%s` cannot be mutably borrowed while borrowed",
				value.name,
				field,
			)
		}
		if value.fieldMutBorrows[field] > 0 {
			return fmt.Errorf(
				"borrow error: field `%s.%s` cannot be borrowed while mutably borrowed",
				value.name,
				field,
			)
		}
		return nil
	}
	if mutable && value.activeBorrows > 0 {
		return fmt.Errorf(
			"borrow error: value `%s` cannot be mutably borrowed while borrowed",
			value.name,
		)
	}
	if value.activeMutBorrows > 0 || len(value.fieldMutBorrows) > 0 {
		return fmt.Errorf(
			"borrow error: value `%s` cannot be borrowed while mutably borrowed",
			value.name,
		)
	}
	if mutable && len(value.fieldBorrows) > 0 {
		return fmt.Errorf(
			"borrow error: value `%s` cannot be mutably borrowed while field is borrowed",
			value.name,
		)
	}
	return nil
}

// borrowedIdent resolves an identifier borrow target when the argument is a name.
func borrowedIdent(expr ast.Expression, env *scope) (*binding, error) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil, nil
	}
	value, exists := env.lookup(ident.Name)
	if !exists {
		return nil, fmt.Errorf("borrow error: undefined variable `%s`", ident.Name)
	}
	if value.moved {
		return nil, fmt.Errorf("borrow error: moved value `%s` was borrowed", ident.Name)
	}
	return value, nil
}

// releaseBorrows clears temporary borrow state for a completed call.
func releaseBorrows(values []*binding) {
	for _, value := range values {
		if value.activeMutBorrows > 0 {
			value.activeMutBorrows--
		} else {
			value.activeBorrows--
		}
	}
}

// releaseBorrow clears one local borrow from its owner.
func releaseBorrow(value *binding) {
	target := value.borrowTarget
	if target == nil {
		return
	}
	if value.borrowField == "" {
		if value.mutBorrow && target.activeMutBorrows > 0 {
			target.activeMutBorrows--
		} else if target.activeBorrows > 0 {
			target.activeBorrows--
		}
		return
	}
	if value.mutBorrow && target.fieldMutBorrows[value.borrowField] > 0 {
		target.fieldMutBorrows[value.borrowField]--
		if target.fieldMutBorrows[value.borrowField] == 0 {
			delete(target.fieldMutBorrows, value.borrowField)
		}
		return
	}
	if target.fieldBorrows[value.borrowField] > 0 {
		target.fieldBorrows[value.borrowField]--
		if target.fieldBorrows[value.borrowField] == 0 {
			delete(target.fieldBorrows, value.borrowField)
		}
	}
}

// checkPrintCall reads the printed value without taking ownership.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "void", nil
}

// newBinding creates a local ownership binding with a stable ID.
func (c *Checker) newBinding(name string, typeName string) *binding {
	c.nextID++
	return &binding{id: c.nextID, name: name, typeName: typeName}
}

// setArenaProvenance records arena and handle origins for local bindings.
func (c *Checker) setArenaProvenance(value *binding, expr ast.Expression, env *scope) {
	if _, ok := expr.(*ast.ArenaNewExpr); ok {
		value.arenaID = value.id
		return
	}
	arena := c.arenaAddReceiver(expr, env)
	if arena != nil {
		value.handleArenaID = arena.arenaID
	}
}

// arenaAddReceiver returns the arena binding for arena.add(value) expressions.
func (c *Checker) arenaAddReceiver(expr ast.Expression, env *scope) *binding {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "add" {
		return nil
	}
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	arena, _ := env.lookup(receiver.Name)
	return arena
}

// isArenaGetExpr reports whether expr is an arena.get call.
func (c *Checker) isArenaGetExpr(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	return ok && field.Name == "get"
}

// containsArenaGet reports whether an expression reads through arena.get.
func (c *Checker) containsArenaGet(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if c.isArenaGetExpr(e) {
			return true
		}
		for _, arg := range e.Args {
			if c.containsArenaGet(arg) {
				return true
			}
		}
	case *ast.FieldExpr:
		return c.containsArenaGet(e.Receiver)
	case *ast.PrefixExpr:
		return c.containsArenaGet(e.Right)
	case *ast.BinaryExpr:
		return c.containsArenaGet(e.Left) || c.containsArenaGet(e.Right)
	case *ast.CastExpr:
		return c.containsArenaGet(e.Value)
	case *ast.TryExpr:
		return c.containsArenaGet(e.Value)
	case *ast.ComptimeExpr:
		return c.containsArenaGet(e.Expr)
	}
	return false
}

// readIdent resolves a variable reference without moving it.
func readIdent(name string, env *scope) (string, error) {
	value, ok := env.lookup(name)
	if ok {
		if value.moved {
			return "", fmt.Errorf("move error: moved value `%s` was used", name)
		}
		return value.typeName, nil
	}
	if name == "void" {
		return "void", nil
	}
	return "", fmt.Errorf("move error: undefined variable `%s`", name)
}

// errorUnionElement extracts T from !T.
func errorUnionElement(typeName string) (string, bool) {
	_, success, ok := errorUnionParts(typeName)
	return success, ok
}

// errorUnionParts extracts error and success types from !T or Error!T.
func errorUnionParts(typeName string) (string, string, bool) {
	if len(typeName) > 1 && typeName[0] == '!' {
		return "", typeName[1:], true
	}
	for idx, ch := range typeName {
		if ch == '!' && idx > 0 && idx < len(typeName)-1 {
			return typeName[:idx], typeName[idx+1:], true
		}
	}
	return "", "", false
}

// returnTypeName returns void for functions without an explicit return type.
func returnTypeName(fn *functionInfo) string {
	if fn.returnType == "" {
		return "void"
	}
	return fn.returnType
}

// isCopyType reports whether values of typeName can be reused after move contexts.
func (c *Checker) isCopyType(typeName string) bool {
	if isRawPointerType(typeName) {
		return true
	}
	if c.enums[typeName] != nil {
		return true
	}
	switch typeName {
	case "bool", "void", "Io", "Allocator", "std::fs::Metadata",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64", "[]const u8":
		return true
	default:
		return false
	}
}

// isAtomicSupportedType reports whether Atomic<T> is available in v0.1.
func isAtomicSupportedType(typeName string) bool {
	return typeName == "bool" || typeName == "i64"
}

// isRawPointerType reports whether typeName is a raw pointer spelling.
func isRawPointerType(typeName string) bool {
	name := typeName
	if len(name) > 0 && name[0] == '?' {
		name = name[1:]
	}
	base, _, ok := splitGenericType(name)
	return ok && base == "ptr"
}

// checkNoArgOwnershipCall validates a zero-argument builtin constructor.
func checkNoArgOwnershipCall(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("move error: `%s` expects 0 args, got %d", name, len(args))
	}
	return name, nil
}

// checkPartitionMut validates ownership for disjoint partition construction.
func (c *Checker) checkPartitionMut(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("parallel error: `std::task::partition_mut` expects 2 args")
	}
	init, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if !c.isCopyType(init) {
		return "", true, fmt.Errorf("parallel error: partition init must be copy, got %s", init)
	}
	if _, err := c.readExpr(args[1], env); err != nil {
		return "", true, err
	}
	return "Partition", true, nil
}

// checkLocalBuffer validates ownership for worker-local scratch construction.
func (c *Checker) checkLocalBuffer(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("parallel error: `std::task::LocalBuffer` expects 2 args")
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", true, err
	}
	if _, err := c.readExpr(args[1], env); err != nil {
		return "", true, err
	}
	return "LocalBuffer", true, nil
}

// checkParallelFor validates ownership for a safe data-parallel call.
func (c *Checker) checkParallelFor(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 4 {
		return "", true, fmt.Errorf("parallel error: `std::task::parallel_for` expects 4 args")
	}
	for idx := 0; idx < 3; idx++ {
		if _, err := c.readExpr(args[idx], env); err != nil {
			return "", true, err
		}
	}
	target, ok := args[3].(*ast.IdentExpr)
	if !ok {
		return "", true, fmt.Errorf("parallel error: `std::task::parallel_for` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", true, fmt.Errorf("parallel error: undefined function `%s`", target.Name)
	}
	return returnTypeName(fn), true, nil
}

// checkParallelMap validates ownership for disjoint partition output.
func (c *Checker) checkParallelMap(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 5 {
		return "", true, fmt.Errorf("parallel error: `std::task::parallel_map` expects 5 args")
	}
	for idx := 0; idx < 4; idx++ {
		if _, err := c.readExpr(args[idx], env); err != nil {
			return "", true, err
		}
	}
	target, ok := args[4].(*ast.IdentExpr)
	if !ok {
		return "", true, fmt.Errorf("parallel error: `std::task::parallel_map` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", true, fmt.Errorf("parallel error: undefined function `%s`", target.Name)
	}
	return returnTypeName(fn), true, nil
}

// checkThreadScoped validates explicit thread boundary ownership effects.
func (c *Checker) checkThreadScoped(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) < 2 {
		return "", true, fmt.Errorf("thread error: `std::thread::scoped` expects io and function")
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", true, err
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", true, fmt.Errorf("thread error: `std::thread::scoped` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", true, fmt.Errorf("thread error: undefined function `%s`", target.Name)
	}
	if len(args[2:]) != len(fn.params) {
		return "", true, fmt.Errorf("thread error: `%s` expects %d args, got %d",
			target.Name, len(fn.params), len(args[2:]))
	}
	for idx, arg := range args[2:] {
		if fn.params[idx].borrow {
			return "", true, fmt.Errorf("thread error: thread cannot capture borrow parameter")
		}
		if err := c.rejectConcurrencyBoundaryArg(arg, env); err != nil {
			return "", true, err
		}
		if _, err := c.moveExpr(arg, env); err != nil {
			return "", true, err
		}
	}
	return returnTypeName(fn), true, nil
}

// checkAtomic validates ownership for a seq_cst atomic constructor.
func (c *Checker) checkAtomic(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if !isAtomicSupportedType(elem) {
		return "", true, fmt.Errorf("atomic error: unsupported atomic type `%s` in v0.1", elem)
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("atomic error: `std::atomic::Atomic<%s>` expects 1 arg", elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != elem {
		return "", true, fmt.Errorf("atomic error: `std::atomic::Atomic<%s>` expects %s, got %s",
			elem, elem, got)
	}
	return fmt.Sprintf("Atomic<%s>", elem), true, nil
}

// checkMutex validates ownership for a synchronized wrapper constructor.
func (c *Checker) checkMutex(elem string, args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("sync error: `std::sync::Mutex<%s>` expects 1 arg", elem)
	}
	if err := c.rejectConcurrencyBoundaryArg(args[0], env); err != nil {
		return "", true, err
	}
	got, err := c.moveExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != elem {
		return "", true, fmt.Errorf("sync error: `std::sync::Mutex<%s>` expects %s, got %s",
			elem, elem, got)
	}
	if !c.isCopyType(elem) {
		return "", true, fmt.Errorf(
			"sync error: `std::sync::Mutex<%s>` requires copy value in v0.1", elem)
	}
	return fmt.Sprintf("Mutex<%s>", elem), true, nil
}

// rejectConcurrencyBoundaryArg rejects borrows and safe raw pointers at boundaries.
func (c *Checker) rejectConcurrencyBoundaryArg(arg ast.Expression, env *scope) error {
	if ident, ok := arg.(*ast.IdentExpr); ok {
		value, exists := env.lookup(ident.Name)
		if exists && value.borrowedParam {
			return fmt.Errorf("thread error: borrow cannot cross concurrency boundary")
		}
	}
	got, err := c.readExpr(arg, env.clone())
	if err != nil {
		return err
	}
	return c.rejectConcurrencyBoundaryType(got, map[string]bool{})
}

// rejectConcurrencyBoundaryType rejects values unsafe to move across concurrency boundaries.
func (c *Checker) rejectConcurrencyBoundaryType(typeName string, seen map[string]bool) error {
	if isRawPointerType(typeName) {
		return fmt.Errorf("thread error: raw pointer cannot cross concurrency boundary")
	}
	if seen[typeName] {
		return nil
	}
	seen[typeName] = true
	if err := c.rejectConcurrencyBoundaryGeneric(typeName, seen); err != nil {
		return err
	}
	if err := c.rejectConcurrencyBoundaryStruct(typeName, seen); err != nil {
		return err
	}
	return c.rejectConcurrencyBoundaryUnion(typeName, seen)
}

// rejectConcurrencyBoundaryGeneric applies boundary rules to generic-like type spellings.
func (c *Checker) rejectConcurrencyBoundaryGeneric(typeName string, seen map[string]bool) error {
	base, arg, ok := splitGenericType(typeName)
	if !ok {
		return nil
	}
	switch base {
	case "arena":
		return fmt.Errorf("thread error: arena cannot cross concurrency boundary")
	case "std::array::Array":
		return fmt.Errorf("thread error: Array cannot cross concurrency boundary in v0.2")
	case "std::map::Map":
		return fmt.Errorf("thread error: Map cannot cross concurrency boundary in v0.2")
	case "handle":
		return fmt.Errorf("thread error: handle cannot cross concurrency boundary")
	case "Dyn":
		return fmt.Errorf("thread error: Dyn cannot cross concurrency boundary")
	case "Mutex":
		return fmt.Errorf("thread error: Mutex cannot cross concurrency boundary in v0.1")
	case "Task":
		return fmt.Errorf("thread error: Task cannot cross concurrency boundary")
	case "Channel", "option":
		return c.rejectConcurrencyBoundaryType(arg, seen)
	case "Atomic":
		return c.rejectConcurrencyBoundaryAtomic(arg, seen)
	default:
		return nil
	}
}

// rejectConcurrencyBoundaryAtomic checks Atomic<T> boundary eligibility.
func (c *Checker) rejectConcurrencyBoundaryAtomic(typeName string, seen map[string]bool) error {
	if !isAtomicSupportedType(typeName) {
		return fmt.Errorf("thread error: Atomic<%s> cannot cross concurrency boundary in v0.1",
			typeName)
	}
	return c.rejectConcurrencyBoundaryType(typeName, seen)
}

// rejectConcurrencyBoundaryStruct checks all struct fields recursively.
func (c *Checker) rejectConcurrencyBoundaryStruct(typeName string, seen map[string]bool) error {
	fields := c.structs[typeName]
	for fieldName, fieldType := range fields {
		if err := c.rejectConcurrencyBoundaryType(fieldType, seen); err != nil {
			return fmt.Errorf("thread error: struct `%s.%s` cannot cross concurrency boundary: %w",
				typeName, fieldName, err)
		}
	}
	return nil
}

// rejectConcurrencyBoundaryUnion checks all union payloads recursively.
func (c *Checker) rejectConcurrencyBoundaryUnion(typeName string, seen map[string]bool) error {
	variants := c.unions[typeName]
	for variant, payload := range variants {
		if payload == "" {
			continue
		}
		if err := c.rejectConcurrencyBoundaryType(payload, seen); err != nil {
			return fmt.Errorf("thread error: union `%s::%s` cannot cross concurrency boundary: %w",
				typeName, variant, err)
		}
	}
	return nil
}

// qualifiedName renders a namespace chain as an internal key such as std::task::Group.
func qualifiedName(expr ast.Expression) (string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Name, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return "", false
		}
		left, ok := qualifiedName(e.Receiver)
		if !ok {
			return "", false
		}
		return left + "." + e.Name, true
	default:
		return "", false
	}
}

// taskElement extracts T from Task<T>.
func taskElement(typeName string) (string, bool) {
	base, arg, ok := splitGenericType(typeName)
	if !ok || base != "Task" {
		return "", false
	}
	return arg, true
}

// checkedMapArgs validates and returns Map key/value type arguments.
func (c *Checker) checkedMapArgs(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, fmt.Errorf("map error: std::map::Map expects 2 type arguments")
	}
	if args[0] != "[]const u8" {
		return nil, fmt.Errorf("map error: std::map::Map key type must be []const u8 in v0.2")
	}
	if !c.isCopyType(args[1]) {
		return nil, fmt.Errorf("map error: std::map::Map value type must be copy in v0.2")
	}
	return args, nil
}

// splitGenericType extracts base and raw arguments from base<args>.
func splitGenericType(name string) (string, string, bool) {
	for idx, ch := range name {
		if ch != '<' {
			continue
		}
		if len(name) < idx+3 || name[len(name)-1] != '>' {
			return "", "", false
		}
		arg := name[idx+1 : len(name)-1]
		if arg == "" {
			return "", "", false
		}
		return name[:idx], arg, true
	}
	return "", "", false
}

// splitGenericArgs extracts top-level comma-separated generic arguments.
func splitGenericArgs(arg string) ([]string, bool) {
	args := []string{}
	start := 0
	depth := 0
	for idx, ch := range arg {
		switch ch {
		case '<':
			depth++
		case '>':
			if depth == 0 {
				return nil, false
			}
			depth--
		case ',':
			if depth == 0 {
				item := strings.TrimSpace(arg[start:idx])
				if item == "" {
					return nil, false
				}
				args = append(args, item)
				start = idx + 1
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	item := strings.TrimSpace(arg[start:])
	if item == "" {
		return nil, false
	}
	return append(args, item), true
}

// newScope creates a lexical ownership scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, values: map[string]*binding{}}
}

// child creates a nested lexical ownership scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// define binds a local name in the current scope.
func (s *scope) define(value *binding) {
	s.values[value.name] = value
}

// lookup resolves a local name by walking parent scopes.
func (s *scope) lookup(name string) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if value, ok := cur.values[name]; ok {
			return value, true
		}
	}
	return nil, false
}

// checkPendingTasks rejects tasks that leave scope without await or cancel.
func (s *scope) checkPendingTasks() error {
	for name, value := range s.values {
		if _, ok := taskElement(value.typeName); ok && !value.taskDone {
			return fmt.Errorf("task error: task `%s` must be awaited or canceled", name)
		}
	}
	return nil
}

// releaseLastUseBorrows ends local borrows whose binding is no longer used.
func (s *scope) releaseLastUseBorrows(stmtIndex int, lastUses map[string]int) {
	for name, value := range s.values {
		if !value.localBorrow || value.borrowTarget == nil {
			continue
		}
		last, ok := lastUses[name]
		if ok && last > stmtIndex {
			continue
		}
		releaseBorrow(value)
		value.borrowTarget = nil
	}
}

// hasAnyBorrow reports whether a whole value or any direct field is borrowed.
func (b *binding) hasAnyBorrow() bool {
	return b.activeBorrows > 0 || b.activeMutBorrows > 0 ||
		len(b.fieldBorrows) > 0 || len(b.fieldMutBorrows) > 0
}

// directFieldRoot returns a direct local field assignment or read target.
func directFieldRoot(expr ast.Expression, env *scope) (*binding, string, bool) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		value, ok := env.lookup(target.Name)
		return value, "", ok
	case *ast.FieldExpr:
		ident, ok := target.Receiver.(*ast.IdentExpr)
		if !ok {
			return nil, "", false
		}
		value, exists := env.lookup(ident.Name)
		return value, target.Name, exists
	default:
		return nil, "", false
	}
}

// blockLastUses returns the last statement index where each identifier appears.
func blockLastUses(block *ast.BlockStmt) map[string]int {
	lastUses := map[string]int{}
	for idx, stmt := range block.Statements {
		for _, name := range loopIdentUses(stmt) {
			lastUses[name] = len(block.Statements) + 1
		}
		for _, name := range stmtIdentUses(stmt) {
			if lastUses[name] > len(block.Statements) {
				continue
			}
			lastUses[name] = idx
		}
	}
	return lastUses
}

// loopIdentUses returns identifiers used inside loop bodies.
func loopIdentUses(stmt ast.Statement) []string {
	switch s := stmt.(type) {
	case *ast.WhileStmt:
		return blockIdentUses(s.Body)
	case *ast.ForStmt:
		return blockIdentUses(s.Body)
	default:
		return nil
	}
}

// stmtIdentUses collects identifier reads from one statement.
func stmtIdentUses(stmt ast.Statement) []string {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return exprIdentUses(s.Value)
	case *ast.AssignStmt:
		uses := exprIdentUses(s.Value)
		return append(uses, exprIdentUses(s.Target)...)
	case *ast.ReturnStmt:
		return exprIdentUses(s.Value)
	case *ast.ExprStmt:
		return exprIdentUses(s.Expr)
	case *ast.IfStmt:
		uses := exprIdentUses(s.Condition)
		uses = append(uses, blockIdentUses(s.Consequence)...)
		uses = append(uses, blockIdentUses(s.Alternative)...)
		return uses
	case *ast.WhileStmt:
		uses := exprIdentUses(s.Condition)
		return append(uses, blockIdentUses(s.Body)...)
	case *ast.ForStmt:
		uses := exprIdentUses(s.Start)
		uses = append(uses, exprIdentUses(s.End)...)
		return append(uses, blockIdentUses(s.Body)...)
	case *ast.MatchStmt:
		uses := exprIdentUses(s.Value)
		for _, arm := range s.Arms {
			uses = append(uses, stmtIdentUses(arm.Body)...)
		}
		return uses
	case *ast.UnsafeStmt:
		return blockIdentUses(s.Body)
	case *ast.ComptimeIfStmt:
		uses := exprIdentUses(s.Condition)
		uses = append(uses, blockIdentUses(s.Consequence)...)
		uses = append(uses, blockIdentUses(s.Alternative)...)
		return uses
	default:
		return nil
	}
}

// blockIdentUses collects identifier reads inside a nested block.
func blockIdentUses(block *ast.BlockStmt) []string {
	if block == nil {
		return nil
	}
	uses := []string{}
	for _, stmt := range block.Statements {
		uses = append(uses, stmtIdentUses(stmt)...)
	}
	return uses
}

// exprIdentUses collects identifier reads from an expression.
func exprIdentUses(expr ast.Expression) []string {
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.IdentExpr:
		return []string{e.Name}
	case *ast.PrefixExpr:
		return exprIdentUses(e.Right)
	case *ast.BinaryExpr:
		return append(exprIdentUses(e.Left), exprIdentUses(e.Right)...)
	case *ast.CallExpr:
		uses := exprIdentUses(e.Callee)
		for _, arg := range e.Args {
			uses = append(uses, exprIdentUses(arg)...)
		}
		return uses
	case *ast.CastExpr:
		return exprIdentUses(e.Value)
	case *ast.TryExpr:
		return exprIdentUses(e.Value)
	case *ast.StructLiteralExpr:
		uses := []string{}
		for _, field := range e.Fields {
			uses = append(uses, exprIdentUses(field.Value)...)
		}
		return uses
	case *ast.FieldExpr:
		return exprIdentUses(e.Receiver)
	case *ast.DerefExpr:
		return exprIdentUses(e.Receiver)
	case *ast.IfExpr:
		uses := exprIdentUses(e.Condition)
		uses = append(uses, blockIdentUses(e.Consequence)...)
		return append(uses, blockIdentUses(e.Alternative)...)
	case *ast.ComptimeExpr:
		return exprIdentUses(e.Expr)
	default:
		return nil
	}
}

// borrowPrefix reports whether an expression is &T or &mut T syntax.
func borrowPrefix(expr ast.Expression) (*ast.PrefixExpr, bool) {
	prefix, ok := expr.(*ast.PrefixExpr)
	if !ok || (prefix.Operator != "&" && prefix.Operator != "&mut") {
		return nil, false
	}
	return prefix, true
}

// clone copies a scope chain so branch checks do not interfere with each other.
func (s *scope) clone() *scope {
	if s == nil {
		return nil
	}
	cloned := &scope{values: map[string]*binding{}}
	cloned.parent = s.parent.clone()
	for name, value := range s.values {
		copyValue := *value
		cloned.values[name] = &copyValue
	}
	return cloned
}

// mergeMovedFrom marks bindings moved when a checked branch may have moved them.
func (s *scope) mergeMovedFrom(other *scope) {
	byID := map[int]*binding{}
	s.collectBindings(byID)
	other.walkBindings(func(value *binding) {
		if value.moved {
			if target, ok := byID[value.id]; ok {
				target.moved = true
			}
		}
	})
}

// collectBindings stores bindings by stable ID.
func (s *scope) collectBindings(out map[int]*binding) {
	s.walkBindings(func(value *binding) {
		out[value.id] = value
	})
}

// walkBindings visits all bindings in this scope chain.
func (s *scope) walkBindings(visit func(*binding)) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			visit(value)
		}
	}
}
