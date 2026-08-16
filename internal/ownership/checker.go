package ownership

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/stdprim"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Checker validates ownership and move rules for a parsed program.
type Checker struct {
	functions       map[string]*functionInfo
	impls           map[string]map[string]*functionInfo
	structs         map[string]map[string]string
	enums           map[string]map[string]bool
	errorSets       map[string]map[string]bool
	unions          map[string]map[string]string
	nextID          int
	consumeNeeds    map[string]bool
	deinitOwners    map[string]bool
	structOrder     map[string][]string
	loopDepth       int
	currentFunction *functionInfo
	currentStd      bool
	typeArgValues   map[string]string
	liveErrDefers   []errDeferEntry
}

// errDeferEntry records one active errdefer cleanup whose receiver must stay
// valid on every error-return path that can run it.
type errDeferEntry struct {
	receiver ast.Expression
	name     string
}

// A functionInfo is the ownership-facing view of a function: what a caller
// hands over, plus the body this checker walks. The two are separate fields so
// that reading the signature cannot reach the body by accident.
//
// name is not sig.Name: a method is registered under a qualified name, while
// the signature keeps the name as it was declared.
type functionInfo struct {
	name       string
	sig        ast.FunctionSignature
	params     []paramInfo
	returnType string
	body       *ast.BlockStmt
}

type paramInfo struct {
	typeName  string
	borrow    bool
	mutBorrow bool
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
	fieldDeinit      map[string]bool
	fieldArenaIDs    map[string]int
	arenaID          int
	handleArenaID    int
	deinitialized    bool
	consumeExempt    bool
	deferCleanup     bool
	declSpan         ast.Span
}

type scope struct {
	parent *scope
	values map[string]*binding
}

type directFieldReceiver struct {
	owner    *binding
	field    string
	typeName string
	path     string
}

type temporaryBorrow struct {
	value   *binding
	field   string
	mutable bool
}

// New creates an empty ownership checker.
func New() *Checker {
	return &Checker{
		functions:    map[string]*functionInfo{},
		impls:        map[string]map[string]*functionInfo{},
		structs:      map[string]map[string]string{},
		enums:        map[string]map[string]bool{},
		errorSets:    map[string]map[string]bool{},
		unions:       map[string]map[string]string{},
		consumeNeeds: map[string]bool{},
		structOrder:  map[string][]string{},
	}
}

// Check validates ownership rules and returns the first move error.
func (c *Checker) Check(program *ast.Program) error {
	c.deinitOwners = ast.DeinitOwners(program)
	if err := c.checkStructs(program); err != nil {
		return err
	}
	c.collectEnums(program)
	c.collectErrorSets(program)
	c.collectUnions(program)
	if err := c.collectFunctions(program); err != nil {
		return err
	}
	if err := c.checkOwnerAggregatesDeclareDeinit(program); err != nil {
		return err
	}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			if len(d.TypeParamNames()) > 0 {
				continue
			}
			if err := c.checkFunction(c.functions[d.Name]); err != nil {
				return err
			}
		case *ast.TestDecl:
			if err := c.checkTestDecl(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckAll validates ownership like Check but accumulates one error per
// top-level declaration instead of stopping at the first, so editors can show
// every independent move error at once. Setup phases still fail fast.
func (c *Checker) CheckAll(program *ast.Program) []error {
	c.deinitOwners = ast.DeinitOwners(program)
	if err := c.checkStructs(program); err != nil {
		return []error{err}
	}
	c.collectEnums(program)
	c.collectErrorSets(program)
	c.collectUnions(program)
	if err := c.collectFunctions(program); err != nil {
		return []error{err}
	}
	var errs []error
	if err := c.checkOwnerAggregatesDeclareDeinit(program); err != nil {
		errs = append(errs, err)
	}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			if len(d.TypeParamNames()) > 0 {
				continue
			}
			if err := c.checkFunction(c.functions[d.Name]); err != nil {
				errs = append(errs, err)
			}
		case *ast.TestDecl:
			if err := c.checkTestDecl(d); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
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

// collectErrorSets records error set declarations for error value reads. An
// error carries nothing, so reading one moves nothing.
func (c *Checker) collectErrorSets(program *ast.Program) {
	for _, decl := range program.Decls {
		setDecl, ok := decl.(*ast.ErrorSetDecl)
		if !ok {
			continue
		}
		members := map[string]bool{}
		for _, member := range setDecl.Members {
			members[member] = true
		}
		c.errorSets[setDecl.Name] = members
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
			variants[variant.Name] = typ.Text(variant.Payload)
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
		order := make([]string, 0, len(st.Fields))
		for _, field := range st.Fields {
			if field.Borrow {
				return errorf("borrow error: struct field `%s.%s` cannot store borrow",
					st.Name, field.Name)
			}
			fields[field.Name] = fieldOwnershipType(field)
			order = append(order, field.Name)
		}
		c.structs[st.Name] = fields
		c.structOrder[st.Name] = order
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
		c.functions[fn.Name] = functionInfoFromDecl(fn.Name, fn)
		if err := c.collectReceiverMethod(fn); err != nil {
			return err
		}
	}
	return nil
}

// collectReceiverMethod files a `fn (self: T) name(...)` declaration under the
// type it is a method on. Its name already says which that is.
func (c *Checker) collectReceiverMethod(decl *ast.FunctionDecl) error {
	if !decl.Receiver {
		return nil
	}
	receiver, name, ok := stdmethod.SplitMethodName(decl.Name)
	if !ok {
		return nil
	}
	methods := c.impls[receiver]
	if methods == nil {
		methods = map[string]*functionInfo{}
		c.impls[receiver] = methods
	}
	if _, exists := methods[name]; exists {
		return errorf("move error: duplicate method `%s`", decl.Name)
	}
	methods[name] = functionInfoFromDecl(decl.Name, decl)
	return nil
}

// functionInfoFromDecl extracts the ownership-facing signature for a function.
func functionInfoFromDecl(name string, fn *ast.FunctionDecl) *functionInfo {
	params := make([]paramInfo, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, paramInfo{
			typeName: typ.Text(param.TypeName), borrow: param.Borrow, mutBorrow: param.MutBorrow,
		})
	}
	return &functionInfo{
		name: name, sig: fn.FunctionSignature, params: params,
		returnType: typ.Text(fn.ReturnType), body: fn.Body,
	}
}

// checkFunction validates one function body.
func (c *Checker) checkFunction(fn *functionInfo) error {
	if fn.sig.ExternABI != "" {
		return nil
	}
	env := newScope(nil)
	c.defineParams(fn, env, nil)
	previousLoopDepth := c.loopDepth
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeArgValues := c.typeArgValues
	c.loopDepth = 0
	c.currentFunction = fn
	c.currentStd = fn.sig.Std
	c.typeArgValues = nil
	defer func() { c.loopDepth = previousLoopDepth }()
	defer func() { c.currentFunction = previousFunction }()
	defer func() { c.currentStd = previousStd }()
	defer func() { c.typeArgValues = previousTypeArgValues }()
	if err := c.checkBlock(fn.body, env); err != nil {
		return err
	}
	return c.checkDeinitCompleteness(fn, env)
}

// checkOwnerAggregatesDeclareDeinit enforces ADR-0091: a struct or union that
// holds owner fields or payloads is itself an owner and must declare deinit,
// so its cleanup contract is visible in source. Generic declarations wait for
// instantiation, where their spellings become concrete.
func (c *Checker) checkOwnerAggregatesDeclareDeinit(program *ast.Program) error {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			if len(d.TypeParams) > 0 || c.implMethod(d.Name, "deinit") != nil {
				continue
			}
			for _, field := range d.Fields {
				if !c.valueTypeNeedsConsume(typ.Text(field.TypeName)) {
					continue
				}
				return errorf(
					"move error: struct `%s` holds owner field `%s` and must declare deinit",
					d.Name, field.Name)
			}
		case *ast.UnionDecl:
			if len(d.TypeParams) > 0 || c.implMethod(d.Name, "deinit") != nil {
				continue
			}
			for _, variant := range d.Variants {
				if variant.Payload == nil || !c.valueTypeNeedsConsume(typ.Text(variant.Payload)) {
					continue
				}
				return errorf(
					"move error: union `%s` holds owner payload `%s` and must declare deinit",
					d.Name, variant.Name)
			}
		}
	}
	return nil
}

// checkDeinitCompleteness enforces ADR-0091: a struct deinit must consume every
// owner field of its receiver. A union deinit is covered by the match dispatch
// rules, and a body that moves self on has handed the obligation elsewhere.
func (c *Checker) checkDeinitCompleteness(fn *functionInfo, env *scope) error {
	receiver, method, ok := stdmethod.SplitMethodName(fn.name)
	if !ok || method != "deinit" || !fn.sig.Receiver || len(fn.sig.Params) == 0 {
		return nil
	}
	fields := c.structs[receiver]
	if fields == nil {
		return nil
	}
	self, exists := env.lookup(fn.sig.Params[0].Name)
	if !exists || self.moved {
		return nil
	}
	for _, name := range c.structOrder[receiver] {
		if !c.valueTypeNeedsConsume(fields[name]) || self.fieldDeinit[name] {
			continue
		}
		return errorf("move error: deinit of `%s` must consume owner field `%s`",
			receiver, name)
	}
	return nil
}

// defineParams binds a function's params into env, substituting generic type
// spellings when subst is non-nil.
func (c *Checker) defineParams(fn *functionInfo, env *scope, subst map[string]string) {
	// A `<...>` entry that declares a type is a compile-time value, in scope
	// for the body like a parameter but never moved or borrowed.
	for _, param := range fn.sig.StaticParams {
		if param.IsType() {
			continue
		}
		env.define(c.newBinding(param.Name, typ.Text(param.Type)))
	}
	for idx, param := range fn.sig.Params {
		typeName := fn.params[idx].typeName
		if subst != nil {
			typeName = substituteOwnershipType(typeName, subst)
		}
		value := c.newBinding(param.Name, typeName)
		value.borrowedParam = fn.params[idx].borrow
		value.mutBorrow = fn.params[idx].mutBorrow
		// A method receiver is written as a by-value param but is not a
		// consuming transfer (SPEC §14: mutators are callable from owned
		// locals), and a consume primitive keeps its value by design
		// (ADR-0091): neither carries a consume obligation.
		value.consumeExempt = (fn.sig.Receiver && idx == 0) || isConsumePrimitive(fn.name)
		env.define(value)
	}
}

// isConsumePrimitive names the std functions whose whole job is consuming an
// owner: their param carries no obligation and their argument is moved.
func isConsumePrimitive(name string) bool {
	return name == "std::mem::leak"
}

// checkTestDecl validates a top-level test block as an errorable, parameterless body.
func (c *Checker) checkTestDecl(decl *ast.TestDecl) error {
	fn := functionInfoFromDecl("test "+strconv.Quote(decl.Name), &ast.FunctionDecl{
		FunctionSignature: ast.FunctionSignature{
			Name:       "test " + strconv.Quote(decl.Name),
			ReturnType: &typ.ErrorUnion{Ok: &typ.Name{Path: []string{"void"}}},
		},
		Body: decl.Body,
	})
	return c.checkFunction(fn)
}

// checkBlock validates statements in a lexical block.
func (c *Checker) checkBlock(block *ast.BlockStmt, env *scope) error {
	lastUses := blockLastUses(block)
	defers := []ast.Expression{}
	errDeferMark := len(c.liveErrDefers)
	defer c.restoreErrDefers(errDeferMark)
	// Bindings declared inside this block carry IDs above the watermark; the
	// fall-through leak check below is scoped to exactly those.
	bindingMark := c.nextID
	for idx, stmt := range block.Statements {
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if err := c.checkDeferStmt(deferStmt, env); err != nil {
				return err
			}
			defers = append(defers, deferStmt.Expr)
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if errDeferStmt, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := c.checkErrDeferStmt(errDeferStmt, env); err != nil {
				return err
			}
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if err := c.checkStmt(stmt, env); err != nil {
			return err
		}
		env.releaseLastUseBorrows(idx, lastUses)
	}
	if err := c.checkDeferredCleanups(defers, env); err != nil {
		return err
	}
	if blockTerminates(block) {
		return nil
	}
	return c.checkOwnersConsumed(env, bindingMark, false)
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
	case *ast.DeferStmt:
		return errorf("move error: defer statement must appear directly in a block")
	case *ast.ErrDeferStmt:
		return errorf("move error: errdefer statement must appear directly in a block")
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
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env)
	default:
		return errorf("move error: unsupported statement %T", stmt)
	}
}

// checkDeferStmt validates a deferred cleanup registration without running it.
func (c *Checker) checkDeferStmt(stmt *ast.DeferStmt, env *scope) error {
	call, ok := stmt.Expr.(*ast.CallExpr)
	if !ok {
		return errorf("move error: defer expects cleanup method call")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "deinit" {
		return errorf("move error: defer expects cleanup method call")
	}
	if _, err := c.readExpr(field.Receiver, env); err != nil {
		return err
	}
	// A registered defer runs on every later exit of this block, so from here
	// on the receiver's consume obligation (ADR-0091) is discharged.
	if ident, ok := field.Receiver.(*ast.IdentExpr); ok {
		if value, exists := env.lookup(ident.Name); exists {
			value.deferCleanup = true
		}
	}
	return nil
}

// checkErrDeferStmt validates an error-path cleanup registration. The receiver
// must be a readable owned local at registration. Unlike defer, the cleanup is
// not applied at normal block exit, so it never blocks a success-path move; its
// receiver is instead re-validated at each error-return path that can run it.
func (c *Checker) checkErrDeferStmt(stmt *ast.ErrDeferStmt, env *scope) error {
	call, ok := stmt.Expr.(*ast.CallExpr)
	if !ok {
		return errorf("move error: errdefer expects cleanup method call")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "deinit" {
		return errorf("move error: errdefer expects cleanup method call")
	}
	if _, err := c.readExpr(field.Receiver, env); err != nil {
		return err
	}
	name := ""
	if ident, ok := field.Receiver.(*ast.IdentExpr); ok {
		name = ident.Name
	}
	c.liveErrDefers = append(c.liveErrDefers, errDeferEntry{receiver: field.Receiver, name: name})
	return nil
}

// restoreErrDefers drops errdefer entries registered inside an exited block.
func (c *Checker) restoreErrDefers(mark int) {
	c.liveErrDefers = c.liveErrDefers[:mark]
}

// validateErrDeferReceivers rejects active errdefer cleanups whose receiver has
// become invalid on an error-return path: moved, explicitly deinitialized, or
// borrowed. This runs at every error path (try) that could trigger the cleanup.
func (c *Checker) validateErrDeferReceivers(env *scope) error {
	for _, entry := range c.liveErrDefers {
		if entry.name == "" {
			continue
		}
		value, exists := env.lookup(entry.name)
		if !exists {
			continue
		}
		if value.deinitialized {
			return errorf(
				"move error: errdefer cleanup receiver `%s` was deinitialized before an error path",
				entry.name)
		}
		if value.moved {
			return errorf(
				"move error: errdefer cleanup receiver `%s` was moved before an error path",
				entry.name)
		}
		if value.activeBorrows > 0 || value.activeMutBorrows > 0 {
			return errorf(
				"borrow error: errdefer cleanup receiver `%s` is borrowed on an error path",
				entry.name)
		}
	}
	return nil
}

// valueTypeNeedsConsume reports whether typeName carries a deinit contract, the
// class of values ADR-0091 requires every exit path to consume. Owner-ness has
// one definition, ast.DeinitOwners, seeded per program in Check / CheckAll.
func (c *Checker) valueTypeNeedsConsume(typeName string) bool {
	if needs, ok := c.consumeNeeds[typeName]; ok {
		return needs
	}
	base := typeName
	if generic, _, ok := splitGenericType(typeName); ok {
		base = generic
	}
	needs := c.deinitOwners[base]
	c.consumeNeeds[typeName] = needs
	return needs
}

// bindingNeedsConsume reports whether a binding still owes its cleanup: an
// owned deinit-carrying value that no move, deinit, or registered defer has
// discharged yet.
func (c *Checker) bindingNeedsConsume(value *binding) bool {
	if value.moved || value.deinitialized || value.deferCleanup {
		return false
	}
	if value.borrowedParam || value.localBorrow || value.consumeExempt {
		return false
	}
	if value.handleArenaID != 0 {
		return false
	}
	return c.valueTypeNeedsConsume(value.typeName)
}

// checkOwnersConsumed rejects an exit that would leak a live owner. sinceID
// limits the check to bindings declared after that watermark: a function exit
// passes 0 and checks everything live, a block fall-through passes the ID the
// block started at and checks only its own declarations. On an error path a
// registered errdefer cleanup counts as the consume.
func (c *Checker) checkOwnersConsumed(env *scope, sinceID int, errorPath bool) error {
	var leaked *binding
	env.walkBindings(func(value *binding) {
		if value.id <= sinceID || !c.bindingNeedsConsume(value) {
			return
		}
		if errorPath && c.errDeferCovers(value.name) {
			return
		}
		if leaked == nil || value.id > leaked.id {
			leaked = value
		}
	})
	if leaked == nil {
		return nil
	}
	return leakError(leaked)
}

// leakError reports one unconsumed owner at its declaration site.
func leakError(value *binding) error {
	return errorAt(value.declSpan, "move error: owned value `%s` is never deinitialized",
		value.name)
}

// errDeferCovers reports whether an active errdefer cleans up the named owner.
func (c *Checker) errDeferCovers(name string) bool {
	if name == "" {
		return false
	}
	for _, entry := range c.liveErrDefers {
		if entry.name == name {
			return true
		}
	}
	return false
}

// blockTerminates reports whether a block always exits through a return, in
// which case its fall-through path is unreachable and branch effects cannot
// leak past it.
func blockTerminates(block *ast.BlockStmt) bool {
	if block == nil || len(block.Statements) == 0 {
		return false
	}
	return stmtTerminates(block.Statements[len(block.Statements)-1])
}

// stmtTerminates reports whether a statement always returns. Exhaustiveness of
// a match is the type checker's promise, so all arms returning is enough here.
func stmtTerminates(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return blockTerminates(s)
	case *ast.IfStmt:
		return s.Alternative != nil && blockTerminates(s.Consequence) &&
			blockTerminates(s.Alternative)
	case *ast.MatchStmt:
		if len(s.Arms) == 0 {
			return false
		}
		for _, arm := range s.Arms {
			if !stmtTerminates(arm.Body) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// checkDeferredCleanups applies deferred cleanup effects in reverse order.
func (c *Checker) checkDeferredCleanups(defers []ast.Expression, env *scope) error {
	for idx := len(defers) - 1; idx >= 0; idx-- {
		stmt := &ast.ExprStmt{Expr: defers[idx], Semicolon: true}
		if err := c.checkExprStmt(stmt, env); err != nil {
			return err
		}
	}
	return nil
}

// checkReturnStmt rejects borrowed values before applying normal move rules.
func (c *Checker) checkReturnStmt(stmt *ast.ReturnStmt, env *scope) error {
	if stmt.Value == nil {
		return c.checkOwnersConsumed(env, 0, false)
	}
	if ident, ok := stmt.Value.(*ast.IdentExpr); ok {
		value, exists := env.lookup(ident.Name)
		if exists && value.borrowedParam {
			if c.borrowedReturnAllowed(ident.Name, value) {
				return nil
			}
			return errorAt(ident.Span, "borrow error: borrowed value `%s` cannot escape",
				ident.Name)
		}
		if exists && value.handleArenaID != 0 {
			return errorAt(ident.Span, "arena error: handle `%s` cannot outlive its arena",
				ident.Name)
		}
	}
	if arena := c.arenaAddReceiver(stmt.Value, env); arena != nil && arena.arenaID != 0 {
		return errorf("arena error: handle from `%s` cannot outlive its arena", arena.name)
	}
	// An error return runs active errdefer cleanups before the function exits,
	// so their receivers must still be valid here. A success return transfers
	// the owner instead and must not be blocked by the cleanup it skips.
	errorPath := c.returnTakesErrorPath(stmt.Value, env)
	if errorPath {
		if err := c.validateErrDeferReceivers(env); err != nil {
			return err
		}
	}
	if _, err := c.moveExpr(stmt.Value, env); err != nil {
		return err
	}
	return c.checkOwnersConsumed(env, 0, errorPath)
}

// returnTakesErrorPath reports whether returning expr exits through the error
// path. What decides it is the type of the returned value, not the shape of the
// expression: a member of an error set fails, and so does an error union being
// propagated, whether either is written out or bound to a local first.
func (c *Checker) returnTakesErrorPath(expr ast.Expression, env *scope) bool {
	typeName, ok := c.returnedTypeName(expr, env)
	if !ok {
		return false
	}
	if c.errorSets[typeName] != nil {
		return true
	}
	_, _, isUnion := errorUnionParts(typeName)
	return isUnion
}

// returnedTypeName reads the type of a returned expression without moving it.
func (c *Checker) returnedTypeName(expr ast.Expression, env *scope) (string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		value, exists := env.lookup(e.Name)
		if !exists {
			return "", false
		}
		return value.typeName, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return "", false
		}
		name, err := c.readNamespaceExpr(e)
		if err != nil {
			return "", false
		}
		return name, true
	default:
		return "", false
	}
}

// borrowedReturnAllowed permits returning the declared borrowed source parameter.
func (c *Checker) borrowedReturnAllowed(name string, value *binding) bool {
	if c.currentFunction == nil {
		return false
	}
	if c.currentFunction.sig.ReturnBorrow != name {
		return false
	}
	_, mutable, inner, ok := explicitOwnershipBorrowType(returnTypeName(c.currentFunction))
	if !ok {
		return false
	}
	if mutable && !value.mutBorrow {
		return false
	}
	if !sameOwnershipType(value.typeName, inner) {
		return false
	}
	for idx, param := range c.currentFunction.sig.Params {
		if param.Name != name || !c.currentFunction.params[idx].borrow {
			continue
		}
		return !mutable || c.currentFunction.params[idx].mutBorrow
	}
	return false
}

// checkLetStmt moves the initializer into a new binding when needed.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope) error {
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		return c.checkBorrowLetStmt(stmt, borrow, env)
	}
	if target, elem, mutable, ok := c.arrayBorrowInitializer(stmt.Value, env); ok {
		return c.checkArrayBorrowLetStmt(stmt, target, elem, mutable, env)
	}
	target, field, elem, mutable, ok, err := c.boxBorrowInitializer(stmt.Value, env)
	if ok || err != nil {
		if err != nil {
			return err
		}
		return c.checkBoxBorrowLetStmt(stmt, target, field, elem, mutable, env)
	}
	if target, ok := c.stringViewInitializer(stmt.Value, env); ok {
		return c.checkStringViewLetStmt(stmt, target, env)
	}
	target, field, elem, mutable, ok, err = c.returnedBorrowInitializer(stmt.Value, env)
	if ok || err != nil {
		if err != nil {
			return err
		}
		return c.checkReturnedBorrowLetStmt(stmt, target, field, elem, mutable, env)
	}
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	value := c.newBinding(stmt.Name, typeName)
	value.mutable = stmt.Mutable
	value.declSpan = expressionSpan(stmt.Value)
	c.setArenaProvenance(value, stmt.Value, env)
	env.define(value)
	return nil
}

// checkReturnedBorrowLetStmt binds a function-returned borrow to its source owner.
func (c *Checker) checkReturnedBorrowLetStmt(
	stmt *ast.LetStmt,
	target *binding,
	field string,
	elem string,
	mutable bool,
	env *scope,
) error {
	if err := checkBorrowConflictForField(target, field, mutable); err != nil {
		return err
	}
	c.activateBorrow(target, field, mutable)
	value := c.newBinding(stmt.Name, elem)
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTarget = target
	value.borrowField = field
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// returnedBorrowInitializer recognizes calls returning a declared borrowed view.
func (c *Checker) returnedBorrowInitializer(
	expr ast.Expression,
	env *scope,
) (*binding, string, string, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, "", "", false, false, nil
	}
	name, fn := c.calledFunction(call.Callee)
	if fn == nil {
		return nil, "", "", false, false, nil
	}
	_, mutable, elem, ok := explicitOwnershipBorrowType(returnTypeName(fn))
	if !ok {
		return nil, "", "", false, false, nil
	}
	idx := borrowReturnParamIndex(fn, mutable)
	if idx < 0 || idx >= len(call.Args) {
		return nil, "", "", false, true,
			errorf("borrow error: `%s` borrowed return has no source parameter", name)
	}
	target, field, err := c.borrowTarget(call.Args[idx], env)
	if err != nil {
		return nil, "", "", false, true, err
	}
	if _, err := c.checkUserCall(name, call.Args, env); err != nil {
		return nil, "", "", false, true, err
	}
	return target, field, elem, mutable, true, nil
}

// calledFunction resolves direct and namespace-qualified source function calls.
func (c *Checker) calledFunction(callee ast.Expression) (string, *functionInfo) {
	switch e := callee.(type) {
	case *ast.IdentExpr:
		return e.Name, c.functions[e.Name]
	case *ast.FieldExpr:
		name, ok := qualifiedName(e)
		if !ok {
			return "", nil
		}
		return name, c.functions[name]
	default:
		return "", nil
	}
}

// borrowReturnParamIndex finds the parameter that owns a returned borrow.
func borrowReturnParamIndex(fn *functionInfo, mutable bool) int {
	for idx, param := range fn.sig.Params {
		if param.Name != fn.sig.ReturnBorrow || !fn.params[idx].borrow {
			continue
		}
		if mutable && !fn.params[idx].mutBorrow {
			continue
		}
		return idx
	}
	return -1
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
	value := c.newBinding(stmt.Name, "[]u8")
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
		return errorf("string error: String view initializer must call String.as_bytes")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "as_bytes" {
		return errorf("string error: String view initializer must call String.as_bytes")
	}
	if len(call.Args) != 0 {
		return errorf("string error: `String.as_bytes` expects 0 args, got %d", len(call.Args))
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
		return errorf("array error: `Array.at_mut` requires mutable array binding")
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
		return errorf("array error: Array borrow initializer must use try")
	}
	call, ok := tryExpr.Value.(*ast.CallExpr)
	if !ok {
		return errorf("array error: Array borrow initializer must call Array.at")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "at" && field.Name != "at_mut") {
		return errorf("array error: Array borrow initializer must call Array.at")
	}
	if len(call.Args) != 1 {
		return errorf("array error: `Array.%s` expects 1 arg, got %d", field.Name, len(call.Args))
	}
	got, err := c.readExpr(call.Args[0], env)
	if err != nil {
		return err
	}
	if got != "i64" {
		return errorf("array error: `Array.%s` expects i64 index, got %s", field.Name, got)
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

// checkBoxBorrowLetStmt binds a Box payload borrow and activates the Box owner.
func (c *Checker) checkBoxBorrowLetStmt(
	stmt *ast.LetStmt,
	target *binding,
	field string,
	elem string,
	mutable bool,
	env *scope,
) error {
	if mutable && !boxBorrowTargetIsMutable(target, field) {
		return errorf("box error: `Box.borrow_mut` requires mutable Box receiver")
	}
	if err := c.checkBoxBorrowInitializerShape(stmt.Value); err != nil {
		return err
	}
	if err := checkBorrowConflictForField(target, field, mutable); err != nil {
		return err
	}
	c.activateBorrow(target, field, mutable)
	value := c.newBinding(stmt.Name, elem)
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTarget = target
	value.borrowField = field
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// boxBorrowTargetIsMutable reports whether the target can produce a mutable Box view.
func boxBorrowTargetIsMutable(target *binding, field string) bool {
	if field != "" {
		return target.mutable
	}
	return target.mutable
}

// checkBoxBorrowInitializerShape validates Box.borrow/borrow_mut local borrow syntax.
func (c *Checker) checkBoxBorrowInitializerShape(expr ast.Expression) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return errorf("box error: Box borrow initializer must call Box.borrow")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "borrow" && field.Name != "borrow_mut") {
		return errorf("box error: Box borrow initializer must call Box.borrow")
	}
	if len(call.Args) != 0 {
		return errorf("box error: `Box.%s` expects 0 args, got %d", field.Name, len(call.Args))
	}
	return nil
}

// boxBorrowInitializer recognizes box.borrow/borrow_mut local borrow initializers.
func (c *Checker) boxBorrowInitializer(
	expr ast.Expression,
	env *scope,
) (*binding, string, string, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, "", "", false, false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "borrow" && field.Name != "borrow_mut") {
		return nil, "", "", false, false, nil
	}
	target, borrowedField, err := c.borrowTarget(field.Receiver, env)
	if err != nil {
		return nil, "", "", false, true, err
	}
	typeName, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return nil, "", "", false, true, err
	}
	base, elem, ok := splitGenericType(typeName)
	if !ok || base != "std::mem::Box" {
		return nil, "", "", false, true, errorf("box error: `Box.%s` expects Box receiver",
			field.Name)
	}
	return target, borrowedField, elem, field.Name == "borrow_mut", true, nil
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
	mutable := borrow.Operator == "&var"
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

// borrowTarget resolves a explicit borrow target.
func (c *Checker) borrowTarget(expr ast.Expression, env *scope) (*binding, string, error) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		value, ok := env.lookup(target.Name)
		if !ok {
			return nil, "", errorAt(target.Span,
				"borrow error: undefined variable `%s`", target.Name)
		}
		if err := checkDeinitializedBorrow(target.Name, value, target.Span); err != nil {
			return nil, "", err
		}
		if value.moved {
			return nil, "", errorAt(target.Span,
				"borrow error: moved value `%s` was borrowed", target.Name)
		}
		return value, "", nil
	case *ast.FieldExpr:
		ident, ok := target.Receiver.(*ast.IdentExpr)
		if !ok {
			return nil, "", errorAt(target.Span,
				"borrow error: field borrow only supports one direct field")
		}
		value, ok := env.lookup(ident.Name)
		if !ok {
			return nil, "", errorAt(ident.Span,
				"borrow error: undefined variable `%s`", ident.Name)
		}
		if err := checkDeinitializedBorrow(ident.Name, value, ident.Span); err != nil {
			return nil, "", err
		}
		if value.moved {
			return nil, "", errorAt(ident.Span,
				"borrow error: moved value `%s` was borrowed", ident.Name)
		}
		if value.fieldDeinit[target.Name] {
			return nil, "", errorAt(target.Span,
				"move error: field `%s.%s` was deinitialized",
				ident.Name, target.Name)
		}
		return value, target.Name, nil
	default:
		return nil, "", errorf("borrow error: borrow target must be a local binding or direct field")
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
	if target, ok := directAssignmentRoot(stmt.Target, env); ok {
		typeName, err := c.moveContextualExpr(stmt.Value, target.typeName, env)
		if err != nil {
			return err
		}
		if target.hasAnyBorrow() {
			return errorf("borrow error: value `%s` cannot be assigned while borrowed",
				target.name)
		}
		// Overwriting a live owner silently drops it (ADR-0091).
		if c.bindingNeedsConsume(target) {
			return errorAt(expressionSpan(stmt.Value),
				"move error: owned value `%s` is overwritten before cleanup", target.name)
		}
		target.typeName = typeName
		target.moved = false
		target.deinitialized = false
		target.arenaID = 0
		target.handleArenaID = 0
		c.setArenaProvenance(target, stmt.Value, env)
		return nil
	}
	if _, err := c.moveExpr(stmt.Value, env); err != nil {
		return err
	}
	if err := c.checkAssignmentBorrowConflict(stmt.Target, env); err != nil {
		return err
	}
	if root, field, ok := directFieldRoot(stmt.Target, env); ok && field != "" {
		root.clearFieldDeinit(field)
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
	// A branch that always returns cannot affect the code after the if.
	if !blockTerminates(stmt.Consequence) {
		env.mergeMovedFrom(left)
	}
	if stmt.Alternative == nil || !blockTerminates(stmt.Alternative) {
		env.mergeMovedFrom(right)
	}
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
		return errorf("move error: loop branch `%s` used outside loop", label)
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
		return errorf("move error: match expects enum or union, got %s", valueType)
	}
	ownerDeinitDispatch := c.matchesOwnerUnionDeinit(stmt.Value, valueType)
	if ownerDeinitDispatch {
		c.consumeOwnerUnionReceiver(stmt.Value, env)
	}
	ownedMatch := c.matchScrutineeOwned(stmt.Value, env)
	if !ownerDeinitDispatch {
		if err := c.consumeMovedFromScrutinee(stmt, unionPayloads, env); err != nil {
			return err
		}
	}
	for _, arm := range stmt.Arms {
		if arm.IsWildcard() {
			if arm.Binding != "" {
				return errorf("move error: wildcard match arm cannot bind payload")
			}
		} else if !tags[arm.Tag] {
			return errorf("move error: unknown match tag `%s::%s`", valueType, arm.Tag)
		}
		armEnv := env.clone()
		child := armEnv.child()
		c.defineMatchArmPayload(arm, unionPayloads, ownerDeinitDispatch, ownedMatch, child,
			expressionSpan(stmt.Value))
		if err := c.checkStmt(arm.Body, child); err != nil {
			return err
		}
		if err := c.checkArmPayloadConsumed(arm, child); err != nil {
			return err
		}
		if !stmtTerminates(arm.Body) {
			env.mergeMovedFrom(armEnv)
		}
	}
	return nil
}

// checkArmPayloadConsumed rejects a match arm that drops an owned payload it
// moved out of the scrutinee (ADR-0091).
func (c *Checker) checkArmPayloadConsumed(arm ast.MatchArm, child *scope) error {
	if arm.Binding == "" {
		return nil
	}
	value, ok := child.values[arm.Binding]
	if !ok || !c.bindingNeedsConsume(value) {
		return nil
	}
	return leakError(value)
}

// matchScrutineeOwner returns the binding of a matched value that is an owned
// named local, the one thing a moving match must mark consumed.
func (c *Checker) matchScrutineeOwner(value ast.Expression, env *scope) *binding {
	ident, ok := value.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	owner, exists := env.lookup(ident.Name)
	if !exists {
		return nil
	}
	if owner.borrowedParam || owner.localBorrow || owner.handleArenaID != 0 {
		return nil
	}
	return owner
}

// matchScrutineeOwned reports whether the matched value owns its payloads: an
// owned named local, or a call temporary the match consumes for free. Borrows,
// projections, and calls that may return borrows (arena.get, methods, `borrows`
// returns) match in borrow mode and their aggregate payloads cannot move out.
func (c *Checker) matchScrutineeOwned(value ast.Expression, env *scope) bool {
	if c.matchScrutineeOwner(value, env) != nil {
		return true
	}
	call, ok := value.(*ast.CallExpr)
	if !ok {
		return false
	}
	if c.isArenaGetExpr(value, env) {
		return false
	}
	if field, ok := call.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		return false
	}
	if _, fn := c.calledFunction(call.Callee); fn != nil {
		return fn.sig.ReturnBorrow == ""
	}
	// An unresolved namespace call is a variant constructor: a fresh temporary.
	return true
}

// consumeMovedFromScrutinee marks an owned matched value moved when any arm
// binds a payload that moves out. One arm moving is enough: which arm runs is
// not known statically, so the value is unavailable in the arm bodies and
// after the match either way.
func (c *Checker) consumeMovedFromScrutinee(
	stmt *ast.MatchStmt,
	unionPayloads map[string]string,
	env *scope,
) error {
	owner := c.matchScrutineeOwner(stmt.Value, env)
	if owner == nil || !c.matchMovesPayload(stmt.Arms, unionPayloads) {
		return nil
	}
	if owner.hasAnyBorrow() {
		return errorAt(expressionSpan(stmt.Value),
			"borrow error: value `%s` cannot be moved while borrowed", owner.name)
	}
	owner.moved = true
	return nil
}

// matchMovesPayload reports whether any arm binds a payload an owned match
// moves out rather than copies.
func (c *Checker) matchMovesPayload(arms []ast.MatchArm, unionPayloads map[string]string) bool {
	for _, arm := range arms {
		if arm.IsWildcard() || arm.Binding == "" {
			continue
		}
		if c.classifyMatchPayload(unionPayloads[arm.Tag]) == payloadMoves {
			return true
		}
	}
	return false
}

// consumeOwnerUnionReceiver marks an owner union's deinit receiver moved. The
// dispatch moves the active payload out for cleanup, so `self` is unavailable
// inside the arm bodies and after the match; a second read or re-match of the
// deinitialized union is then a use-after-move.
func (c *Checker) consumeOwnerUnionReceiver(value ast.Expression, env *scope) {
	ident, ok := value.(*ast.IdentExpr)
	if !ok {
		return
	}
	if self, found := env.lookup(ident.Name); found {
		self.moved = true
	}
}

// defineMatchArmPayload binds one union variant payload for a match arm.
// Scalar payloads bind as copies from any match; declared aggregates move out
// of an owned scrutinee (ADR-0090); view payloads and aggregate payloads of a
// borrowed scrutinee stay borrowed so they cannot escape. Inside an owner
// union's own `deinit` the active owner payload is bound as an owned local so
// it can be cleaned through its explicit `deinit`. Inactive variants are never
// bound.
func (c *Checker) defineMatchArmPayload(
	arm ast.MatchArm,
	unionPayloads map[string]string,
	ownerDeinitDispatch bool,
	ownedMatch bool,
	child *scope,
	span ast.Span,
) {
	payload := unionPayloads[arm.Tag]
	if arm.IsWildcard() || payload == "" || arm.Binding == "" {
		return
	}
	value := c.newBinding(arm.Binding, payload)
	value.declSpan = span
	class := c.classifyMatchPayload(payload)
	owned := class == payloadCopies ||
		(class == payloadMoves && ownedMatch) ||
		(ownerDeinitDispatch && !c.isCopyType(payload))
	if !owned {
		value.borrowedParam = true
	}
	child.define(value)
}

// matchPayloadClass says what a match on an owned value may do with one bound
// payload: copy it out, move it out, or keep it borrowed.
type matchPayloadClass int

const (
	payloadBorrows matchPayloadClass = iota
	payloadCopies
	payloadMoves
)

// classifyMatchPayload sorts a payload type for a match on an owned value.
// Only types positively known to be provenance-free copies or declared
// aggregates escape the arm; views and anything unclassified stay borrowed, so
// an unhandled type errs toward rejection, never toward escape (ADR-0090).
func (c *Checker) classifyMatchPayload(typeName string) matchPayloadClass {
	parsed, err := typ.Parse(typeName)
	if err != nil {
		return payloadBorrows
	}
	name, ok := parsed.(*typ.Name)
	if !ok {
		// []T, &T, ?T, dyn T, E!T: views and wrappers keep borrow handling.
		return payloadBorrows
	}
	if isRawPointerType(typeName) {
		return payloadBorrows
	}
	if c.isCopyType(typeName) {
		return payloadCopies
	}
	base := strings.Join(name.Path, "::")
	if c.structs[base] != nil || c.unions[base] != nil {
		return payloadMoves
	}
	return payloadBorrows
}

// matchTags returns known tags for enum and union match ownership checks.
func (c *Checker) matchTags(typeName string) (map[string]bool, map[string]string, bool) {
	if tags := c.enums[typeName]; tags != nil {
		return tags, nil, true
	}
	// An error carries nothing, so matching one moves nothing, which is what a
	// tag enum arm already means here.
	if members := c.errorSets[typeName]; members != nil {
		return members, nil, true
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
	if inner, ok := transparentExprValue(expr); ok {
		return c.readExpr(inner, env)
	}
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.TypeExpr:
		return c.readScalarExpr(e)
	case *ast.ComptimeExpr:
		return c.readComptimeExpr(e, env)
	case *ast.IdentExpr:
		if _, ok := c.typeArgValues[e.Name]; ok {
			return "type", nil
		}
		return readIdent(e, env)
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
	case *ast.StructLiteralExpr:
		return c.readStructLiteralExpr(e, env)
	case *ast.FieldExpr:
		return c.readFieldExpr(e, env)
	case *ast.DerefExpr:
		return c.readDerefExpr(e, env)
	default:
		return c.readControlExpr(expr, env)
	}
}

// transparentExprValue returns the operand of a wrapper that reads as its inner
// expression. `unsafe` says who owns an obligation and carries no value of its
// own, and `&` / `!` / `-` read their operand, so ownership sees through both.
func transparentExprValue(expr ast.Expression) (ast.Expression, bool) {
	switch e := expr.(type) {
	case *ast.UnsafeExpr:
		return e.Value, true
	case *ast.PrefixExpr:
		return e.Right, true
	default:
		return nil, false
	}
}

// readScalarExpr reads literal-like scalar expressions without ownership effects.
func (c *Checker) readScalarExpr(expr ast.Expression) (string, error) {
	if _, ok := expr.(*ast.TypeExpr); ok {
		return "type", nil
	}
	return readLiteralType(expr)
}

// readControlExpr checks control flow expressions without consuming owned values.
func (c *Checker) readControlExpr(expr ast.Expression, env *scope) (string, error) {
	switch e := expr.(type) {
	case *ast.IfStmt:
		return c.readIfExpr(e, env)
	case *ast.MatchStmt:
		return c.readMatchExpr(e, env)
	default:
		return "", errorf("move error: unsupported expression %T", expr)
	}
}

// readIndexExpr reads checked byte indexing and slicing without moving bytes.
func (c *Checker) readIndexExpr(expr *ast.IndexExpr, env *scope) (string, error) {
	target, err := c.readExpr(expr.Target, env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(target, "[]u8") {
		return "", errorf("move error: index/slice target expects []u8, got %s", target)
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
	return target, nil
}

// readLiteralType returns the ownership type of scalar literals.
func readLiteralType(expr ast.Expression) (string, error) {
	switch expr.(type) {
	case *ast.IntExpr:
		return "i64", nil
	case *ast.StringExpr:
		return "[]u8", nil
	case *ast.BoolExpr:
		return "bool", nil
	default:
		return "", errorf("move error: unsupported literal %T", expr)
	}
}

// readContextualExpr reads expr and treats fit-checked integer literals as want.
func (c *Checker) readContextualExpr(
	expr ast.Expression,
	want string,
	env *scope,
) (string, error) {
	got, err := c.readExpr(expr, env)
	if err != nil {
		return "", err
	}
	return coerceContextualIntegerLiteral(expr, want, got)
}

// moveContextualExpr moves expr and treats fit-checked integer literals as want.
func (c *Checker) moveContextualExpr(
	expr ast.Expression,
	want string,
	env *scope,
) (string, error) {
	got, err := c.moveExpr(expr, env)
	if err != nil {
		return "", err
	}
	return coerceContextualIntegerLiteral(expr, want, got)
}

// coerceContextualIntegerLiteral narrows only source integer literals.
func coerceContextualIntegerLiteral(expr ast.Expression, want string, got string) (string, error) {
	if got == want || got != "i64" || !isIntegerOwnershipType(want) {
		return got, nil
	}
	value, ok := integerLiteralValue(expr)
	if !ok {
		return got, nil
	}
	if !integerLiteralFitsType(value, want) {
		return "", errorf("move error: integer literal `%s` does not fit %s",
			expr.String(), want)
	}
	return want, nil
}

// integerLiteralValue returns an interpreter-representable integer literal.
func integerLiteralValue(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		value, err := strconv.ParseInt(e.Value, 10, 64)
		return value, err == nil
	case *ast.PrefixExpr:
		if e.Operator != "-" {
			return 0, false
		}
		value, ok := integerLiteralValue(e.Right)
		if !ok {
			return 0, false
		}
		return -value, true
	default:
		return 0, false
	}
}

// isIntegerOwnershipType reports whether a type is a fixed-width integer.
func isIntegerOwnershipType(typeName string) bool {
	switch typeName {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return true
	default:
		return false
	}
}

// integerLiteralFitsType checks the fixed-width bounds for contextual literals.
func integerLiteralFitsType(value int64, typeName string) bool {
	switch typeName {
	case "i8":
		return value >= -128 && value <= 127
	case "i16":
		return value >= -32768 && value <= 32767
	case "i32":
		return value >= -2147483648 && value <= 2147483647
	case "i64", "isize":
		return true
	case "u8":
		return value >= 0 && value <= 255
	case "u16":
		return value >= 0 && value <= 65535
	case "u32":
		return value >= 0 && value <= 4294967295
	case "u64", "usize":
		return value >= 0
	default:
		return false
	}
}

// readCastExpr reads the source value and returns the explicit target type.
func (c *Checker) readCastExpr(expr *ast.CastExpr, env *scope) (string, error) {
	if _, err := c.readExpr(expr.Value, env); err != nil {
		return "", err
	}
	return typ.Text(expr.TargetType), nil
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
			return "", errorAt(ident.Span, "move error: void is not a value")
		}
		return "", errorAt(ident.Span, "move error: undefined variable `%s`", ident.Name)
	}
	if err := checkDeinitializedUse(ident.Name, value, env, ident.Span); err != nil {
		return "", err
	}
	if value.moved {
		return "", errorAt(ident.Span, "move error: moved value `%s` was used", ident.Name)
	}
	if value.borrowedParam {
		return "", errorAt(ident.Span, "borrow error: borrowed value `%s` cannot escape", ident.Name)
	}
	if value.hasAnyBorrow() && !c.isCopyType(value.typeName) {
		return "", errorAt(ident.Span,
			"borrow error: value `%s` cannot be moved while borrowed", ident.Name)
	}
	if !c.isCopyType(value.typeName) {
		value.moved = true
	}
	return value.typeName, nil
}

// moveNonIdentExpr handles move contexts for compound expressions.
func (c *Checker) moveNonIdentExpr(expr ast.Expression, env *scope) (string, error) {
	if deref, ok := expr.(*ast.DerefExpr); ok {
		return c.moveDerefExpr(deref, env)
	}
	if c.isArenaGetExpr(expr, env) {
		typeName, err := c.readExpr(expr, env)
		if err != nil {
			return "", err
		}
		if c.isCopyType(typeName) {
			return typeName, nil
		}
		return "", errorAt(expressionSpan(expr),
			"arena error: arena.get returns a local borrow and cannot be moved")
	}
	if st, ok := expr.(*ast.StructLiteralExpr); ok {
		return c.moveStructLiteralExpr(st, env)
	}
	if field, ok := expr.(*ast.FieldExpr); ok {
		if field.Namespace {
			return c.readFieldExpr(field, env)
		}
		return c.moveFieldExpr(field, env)
	}
	if stmt, ok := expr.(*ast.IfStmt); ok {
		return c.moveIfExpr(stmt, env)
	}
	if stmt, ok := expr.(*ast.MatchStmt); ok {
		return c.moveMatchExpr(stmt, env)
	}
	return c.readExpr(expr, env)
}

// readIfExpr checks ownership effects for an if expression in read context.
func (c *Checker) readIfExpr(stmt *ast.IfStmt, env *scope) (string, error) {
	return c.checkIfExprValue(stmt, env, false)
}

// moveIfExpr checks ownership effects for an if expression in move context.
func (c *Checker) moveIfExpr(stmt *ast.IfStmt, env *scope) (string, error) {
	return c.checkIfExprValue(stmt, env, true)
}

// checkIfExprValue merges possible branch moves from an if expression.
func (c *Checker) checkIfExprValue(stmt *ast.IfStmt, env *scope, moveTail bool) (string, error) {
	if _, err := c.readExpr(stmt.Condition, env); err != nil {
		return "", err
	}
	if stmt.Alternative == nil {
		return "", errorf("move error: if expression requires else branch")
	}
	left := env.clone()
	leftType, err := c.checkBlockValue(stmt.Consequence, left.child(), moveTail)
	if err != nil {
		return "", err
	}
	right := env.clone()
	rightType, err := c.checkBlockValue(stmt.Alternative, right.child(), moveTail)
	if err != nil {
		return "", err
	}
	if leftType != rightType {
		return "", errorf("move error: if expression branch types differ: %s vs %s",
			leftType, rightType)
	}
	env.mergeMovedFrom(left)
	env.mergeMovedFrom(right)
	return leftType, nil
}

// readMatchExpr checks ownership effects for a match expression in read context.
func (c *Checker) readMatchExpr(stmt *ast.MatchStmt, env *scope) (string, error) {
	return c.checkMatchExprValue(stmt, env, false)
}

// moveMatchExpr checks ownership effects for a match expression in move context.
func (c *Checker) moveMatchExpr(stmt *ast.MatchStmt, env *scope) (string, error) {
	return c.checkMatchExprValue(stmt, env, true)
}

// checkMatchExprValue merges possible arm moves from a match expression.
func (c *Checker) checkMatchExprValue(
	stmt *ast.MatchStmt,
	env *scope,
	moveTail bool,
) (string, error) {
	valueType, err := c.readExpr(stmt.Value, env)
	if err != nil {
		return "", err
	}
	tags, unionPayloads, ok := c.matchTags(valueType)
	if !ok {
		return "", errorf("move error: match expects enum or union, got %s", valueType)
	}
	ownedMatch := c.matchScrutineeOwned(stmt.Value, env)
	if err := c.consumeMovedFromScrutinee(stmt, unionPayloads, env); err != nil {
		return "", err
	}
	var result string
	for idx, arm := range stmt.Arms {
		got, err := c.checkMatchExprArmValue(arm, tags, unionPayloads, ownedMatch,
			expressionSpan(stmt.Value), env, moveTail)
		if err != nil {
			return "", err
		}
		if idx == 0 {
			result = got
		} else if got != result {
			return "", errorf("move error: match arm types differ: %s vs %s", result, got)
		}
	}
	return result, nil
}

// checkMatchExprArmValue checks one arm and merges its possible moves.
func (c *Checker) checkMatchExprArmValue(
	arm ast.MatchArm,
	tags map[string]bool,
	unionPayloads map[string]string,
	ownedMatch bool,
	span ast.Span,
	env *scope,
	moveTail bool,
) (string, error) {
	if arm.IsWildcard() {
		if arm.Binding != "" {
			return "", errorf("move error: wildcard match arm cannot bind payload")
		}
	} else if !tags[arm.Tag] {
		return "", errorf("move error: unknown match tag `%s`", arm.Tag)
	}
	armEnv := env.clone()
	child := armEnv.child()
	c.defineMatchArmPayload(arm, unionPayloads, false, ownedMatch, child, span)
	got, err := c.checkStmtValue(arm.Body, child, moveTail)
	if err != nil {
		return "", err
	}
	if err := c.checkArmPayloadConsumed(arm, child); err != nil {
		return "", err
	}
	env.mergeMovedFrom(armEnv)
	return got, nil
}

// checkBlockValue checks a branch block used in expression position.
func (c *Checker) checkBlockValue(block *ast.BlockStmt, env *scope, moveTail bool) (string, error) {
	if block == nil || len(block.Statements) == 0 {
		return "", errorf("move error: expression block must end with a value")
	}
	lastUses := blockLastUses(block)
	defers := []ast.Expression{}
	errDeferMark := len(c.liveErrDefers)
	defer c.restoreErrDefers(errDeferMark)
	bindingMark := c.nextID
	lastIdx := len(block.Statements) - 1
	for idx, stmt := range block.Statements[:lastIdx] {
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if err := c.checkDeferStmt(deferStmt, env); err != nil {
				return "", err
			}
			defers = append(defers, deferStmt.Expr)
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if errDeferStmt, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := c.checkErrDeferStmt(errDeferStmt, env); err != nil {
				return "", err
			}
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if err := c.checkStmt(stmt, env); err != nil {
			return "", err
		}
		env.releaseLastUseBorrows(idx, lastUses)
	}
	valueType, err := c.checkStmtValue(block.Statements[lastIdx], env, moveTail)
	if err != nil {
		return "", err
	}
	env.releaseLastUseBorrows(lastIdx, lastUses)
	if err := c.checkDeferredCleanups(defers, env); err != nil {
		return "", err
	}
	if err := c.checkOwnersConsumed(env, bindingMark, false); err != nil {
		return "", err
	}
	return valueType, nil
}

// checkStmtValue checks a value-producing tail statement.
func (c *Checker) checkStmtValue(stmt ast.Statement, env *scope, moveTail bool) (string, error) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if s.Semicolon {
			return "", errorf("move error: expression block must end with a value")
		}
		if moveTail {
			return c.moveExpr(s.Expr, env)
		}
		return c.readExpr(s.Expr, env)
	case *ast.IfStmt:
		return c.checkIfExprValue(s, env, moveTail)
	case *ast.MatchStmt:
		return c.checkMatchExprValue(s, env, moveTail)
	default:
		return "", errorf("move error: expression block must end with a value")
	}
}

// moveDerefExpr rejects moving non-copy values out through checked borrows.
func (c *Checker) moveDerefExpr(expr *ast.DerefExpr, env *scope) (string, error) {
	if typeName, ok, err := c.rawPointerDerefExprType(expr, env); ok || err != nil {
		return typeName, err
	}
	typeName, err := c.readDerefExpr(expr, env)
	if err != nil {
		return "", err
	}
	if c.isCopyType(typeName) {
		return typeName, nil
	}
	return "", errorf("borrow error: value `%s` cannot be moved out of borrow",
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
		return "", errorf("move error: callee must be a function name")
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
		return "", errorf("move error: undefined function `%s`", name)
	}
	if len(fn.sig.TypeParamNames()) > 0 {
		return "", errorf("move error: `%s` requires explicit static arguments", name)
	}
	if len(args) != len(fn.params) {
		return "", errorf("move error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	borrowed, err := c.activateBorrowArgs(fn, args, env)
	if err != nil {
		return "", err
	}
	defer releaseTemporaryBorrows(borrowed)
	for idx, arg := range args {
		if fn.params[idx].borrow {
			if fn.params[idx].mutBorrow {
				continue
			}
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
	fn, ok := c.functions[name]
	if !ok {
		return "", false, nil
	}
	if len(fn.sig.TypeParamNames()) > 0 {
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
	if err := c.rejectUnknownBuiltin(name); err != nil {
		return "", true, err
	}
	if typ, ok, err := c.checkQualifiedStdBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := checkIoBuiltin(name, args); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// rejectUnknownBuiltin refuses a primitive the Go implementation does not have.
// The type checker rejects these first in a full run; this keeps the rule true
// of this checker on its own, which is how its own tests read it.
func (c *Checker) rejectUnknownBuiltin(name string) error {
	if !strings.HasPrefix(name, "std::internal::builtin::") || stdprim.Primitive(name) {
		return nil
	}
	return errorf("move error: `%s` is not a primitive", name)
}

// checkQualifiedStdBuiltin validates declarative core and fs ownership effects.
func (c *Checker) checkQualifiedStdBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if typ, ok, err := c.checkSimpleCoreBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkFsBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// checkSimpleCoreBuiltin validates declarative core primitive ownership effects.
func (c *Checker) checkSimpleCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	signature, ok := stdprim.SimpleCoreSignatures[name]
	if !ok {
		return "", false, nil
	}
	if len(args) != len(signature.Args) {
		return "", true, errorf("move error: `%s` expects %s", name,
			coreOwnershipArgsText(signature.Args))
	}
	for idx, arg := range args {
		if err := c.checkCoreArg(name, idx, signature.Args[idx], arg, env); err != nil {
			return "", true, err
		}
	}
	return signature.Return, true, nil
}

// checkCoreArg reads one declarative primitive argument without moving it.
func (c *Checker) checkCoreArg(
	name string,
	index int,
	want stdprim.ArgKind,
	arg ast.Expression,
	env *scope,
) error {
	if want == stdprim.ArgIo {
		return c.checkIoArg(arg, env, name)
	}
	got, err := c.readContextualExpr(arg, string(want), env)
	if err != nil {
		return err
	}
	if got != string(want) {
		return errorf("move error: `%s` arg %d expects %s, got %s",
			name, index+1, want, got)
	}
	return nil
}

// coreOwnershipArgsText renders declarative primitive arguments for diagnostics.
func coreOwnershipArgsText(args []stdprim.ArgKind) string {
	if len(args) == 0 {
		return "0 args"
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == stdprim.ArgIo {
			parts = append(parts, "io")
			continue
		}
		parts = append(parts, string(arg))
	}
	return strings.Join(parts, " and ")
}

// checkFsBuiltin validates ownership for filesystem host primitives.
func (c *Checker) checkFsBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std::internal::builtin::fs_read_file":
		return c.checkFsReadFile(args, env)
	case "std::internal::builtin::fs_write_file":
		return c.checkFsWriteFile(args, env)
	case "std::internal::builtin::fs_exists":
		return c.checkFsPathOnly("std::fs::exists", args, env, "std::fs::Error!bool")
	case "std::internal::builtin::fs_metadata":
		return c.checkFsPathOnly("std::fs::metadata", args, env, "std::fs::Error!std::fs::Metadata")
	case "std::internal::builtin::fs_read_dir":
		return c.checkFsPathOnly("std::fs::read_dir", args, env,
			"std::fs::Error!std::array::Array<std::fs::DirEntry>")
	case "std::internal::builtin::fs_create_dir",
		"std::internal::builtin::fs_remove_dir",
		"std::internal::builtin::fs_remove_file":
		return c.checkFsPathOnly(name, args, env, "std::fs::Error!void")
	case "std::internal::builtin::fs_rename":
		return c.checkFsRename(args, env)
	default:
		return "", false, nil
	}
}

// checkFsReadFile validates ownership effects for std::fs::read_file.
func (c *Checker) checkFsReadFile(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("move error: `std::fs::read_file` expects io and path")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::read_file"); err != nil {
		return "", true, err
	}
	path, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if !sameOwnershipType(path, "[]u8") {
		return "", true, errorf("move error: `std::fs::read_file` expects []u8 path, got %s",
			path)
	}
	return "std::fs::Error![]u8", true, nil
}

// checkFsWriteFile validates ownership effects for std::fs::write_file.
func (c *Checker) checkFsWriteFile(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("move error: `std::fs::write_file` expects io, path, and bytes")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::write_file"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"path", "bytes"} {
		got, err := c.readExpr(args[idx+1], env)
		if err != nil {
			return "", true, err
		}
		if !sameOwnershipType(got, "[]u8") {
			return "", true, errorf(
				"move error: `std::fs::write_file` expects []u8 %s, got %s", label, got)
		}
	}
	return "std::fs::Error!void", true, nil
}

// checkFsRename validates ownership effects for std::fs::rename.
func (c *Checker) checkFsRename(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("move error: `std::fs::rename` expects io, from, and to")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::rename"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"from", "to"} {
		got, err := c.readExpr(args[idx+1], env)
		if err != nil {
			return "", true, err
		}
		if !sameOwnershipType(got, "[]u8") {
			return "", true, errorf(
				"move error: `std::fs::rename` expects []u8 %s, got %s", label, got)
		}
	}
	return "std::fs::Error!void", true, nil
}

// checkFsPathOnly validates common std::fs Io and path arguments.
func (c *Checker) checkFsPathOnly(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("move error: `%s` expects io and path", name)
	}
	if err := c.checkIoArg(args[0], env, name); err != nil {
		return "", true, err
	}
	path, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if !sameOwnershipType(path, "[]u8") {
		return "", true, errorf("move error: `%s` expects []u8 path, got %s", name, path)
	}
	return result, true, nil
}

// checkArrayConstructor validates std::array::new<T>(allocator) ownership.
func (c *Checker) checkArrayConstructor(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if elem != "T" {
		if err := c.rejectArrayElementType(elem); err != nil {
			return "", err
		}
	}
	if len(args) != 1 {
		return "", errorf("move error: `std::array::Array<%s>` expects allocator", elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("move error: `std::array::Array<%s>` expects Allocator, got %s",
			elem, got)
	}
	return fmt.Sprintf("std::array::Array<%s>", elem), nil
}

// checkMapConstructorAllowTypeParams validates std source Map wrapper construction.
func (c *Checker) checkMapConstructorAllowTypeParams(
	argsText string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	mapArgs, ok := splitGenericArgs(argsText)
	if !ok || len(mapArgs) != 2 {
		return "", errorf("map error: std::map::Map expects 2 static arguments")
	}
	if len(args) != 1 {
		return "", errorf("map error: `std::map::Map` expects allocator")
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("map error: `std::map::Map` expects Allocator, got %s", got)
	}
	return fmt.Sprintf("std::map::Map<%s, %s>", mapArgs[0], mapArgs[1]), nil
}

// rejectArrayElementType rejects element types with unresolved ownership hazards.
func (c *Checker) rejectArrayElementType(elem string) error {
	if err := c.rejectArrayStorageType(elem, map[string]bool{}); err != nil {
		return errorf("array error: Array element is not safe: %w", err)
	}
	return nil
}

// rejectArrayStorageType rejects values that are not Array-safe yet.
func (c *Checker) rejectArrayStorageType(typeName string, seen map[string]bool) error {
	if seen[typeName] {
		return nil
	}
	seen[typeName] = true
	if isRawPointerType(typeName) {
		return errorf("array error: Array element cannot be raw pointer")
	}
	if isDynType(typeName) {
		return errorf("array error: Array element cannot be dyn")
	}
	if base, arg, ok := splitGenericType(typeName); ok && base == "option" {
		if err := c.rejectArrayStorageType(arg, seen); err != nil {
			return err
		}
	}
	if err := c.rejectArrayStorageStruct(typeName, seen); err != nil {
		return err
	}
	return c.rejectArrayStorageUnion(typeName, seen)
}

// rejectArrayStorageStruct checks struct fields recursively for Array storage.
func (c *Checker) rejectArrayStorageStruct(typeName string, seen map[string]bool) error {
	fields := c.structs[typeName]
	for fieldName, fieldType := range fields {
		if err := c.rejectArrayStorageType(fieldType, seen); err != nil {
			return errorf("array error: struct `%s.%s` cannot be Array element: %w",
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
			return errorf("array error: union `%s::%s` cannot be Array element: %w",
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
		return errorf("move error: `%s` expects Io, got %s", name, got)
	}
	return nil
}

// checkIoBuiltin validates std::io constructor ownership effects.
func checkIoBuiltin(name string, args []ast.Expression) (string, bool, error) {
	switch name {
	case "std::internal::builtin::io_blocking", "std::internal::builtin::io_failing":
		_, err := checkNoArgOwnershipCall(name, args)
		if err != nil {
			return "", true, err
		}
		return "Io", true, nil
	case "std::io::evented", "std::internal::builtin::io_evented":
		return "", true, errorf("move error: `std::io::evented` is not implemented")
	default:
		return "", false, nil
	}
}

// typeApplyTarget resolves the callee and static arguments of a `<...>` call,
// and refuses an unknown primitive before any of them is looked at.
func (c *Checker) typeApplyTarget(expr *ast.TypeApplyExpr) (string, string, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return "", "", errorf("move error: unsupported type application `%s`", expr.String())
	}
	if err := c.rejectUnknownBuiltin(name); err != nil {
		return "", "", err
	}
	return name, c.instantiateTypeArgText(expr.TypeArg), nil
}

// checkTypeApplyCallExpr validates typed std constructor ownership effects.
func (c *Checker) checkTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	name, typeArg, err := c.typeApplyTarget(expr)
	if err != nil {
		return "", err
	}
	if name == "ptr_from_int" || name == "int_from_ptr" {
		return c.checkPointerIntCastBuiltin(name, typeArg, args, env)
	}
	if typ, ok, err := c.checkGenericUserTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinMapTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinContainerTypeApply(
		name, typeArg, args, env,
	); ok || err != nil {
		return typ, err
	}
	return "", errorf("move error: `%s` does not take static arguments", name)
}

// checkArenaTypeApply validates std::arena::new<T>(allocator) ownership.
func (c *Checker) checkArenaTypeApply(
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	parts, ok := splitGenericArgs(typeArg)
	if !ok || len(parts) != 1 {
		return "", errorf("arena error: std::arena::new expects 1 type argument")
	}
	elem := parts[0]
	if len(args) != 1 {
		return "", errorf(
			"arena error: `std::arena::new<%s>` expects exactly one allocator argument",
			elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("arena error: `std::arena::new<%s>` expects Allocator, got %s",
			elem, got)
	}
	return fmt.Sprintf("std::arena::Arena<%s>", elem), nil
}

// checkBuiltinContainerTypeApply validates std-only generic container primitives.
func (c *Checker) checkBuiltinContainerTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if name == "std::internal::builtin::arena" {
		typ, err := c.checkArenaTypeApply(typeArg, args, env)
		return typ, true, err
	}
	if typ, ok, err := c.checkBuiltinBoxTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkBuiltinArrayTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, ok, err
	}
	return c.checkBuiltinTestingTypeApply(name, typeArg, args, env)
}

// checkBuiltinTestingTypeApply validates typed std::testing primitives.
func (c *Checker) checkBuiltinTestingTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if name != "std::internal::builtin::test_fail_equal" {
		return "", false, nil
	}
	typ, err := c.checkBuiltinTestFailEqual(typeArg, args, env)
	return typ, true, err
}

// checkBuiltinTestFailEqual validates ownership for the typed testing failure primitive.
func (c *Checker) checkBuiltinTestFailEqual(
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 2 {
		return "", errorf("move error: `std::testing::expect_equal<%s>` expects 2 args", typeArg)
	}
	for idx, arg := range args {
		got, err := c.readContextualExpr(arg, typeArg, env)
		if err != nil {
			return "", err
		}
		if got != typeArg {
			return "", errorf(
				"move error: arg %d of `std::testing::expect_equal<%s>` expects %s, got %s",
				idx+1,
				typeArg,
				typeArg,
				got,
			)
		}
	}
	return "void", nil
}

// checkBuiltinArrayTypeApply validates std-only generic Array primitives.
func (c *Checker) checkBuiltinArrayTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std::internal::builtin::array":
		typ, err := c.checkArrayConstructor(typeArg, args, env)
		return typ, true, err
	case "std::internal::builtin::array_append",
		"std::internal::builtin::array_len",
		"std::internal::builtin::array_capacity",
		"std::internal::builtin::array_pop", "std::internal::builtin::array_pop_or_panic",
		"std::internal::builtin::array_get", "std::internal::builtin::array_get_or_panic",
		"std::internal::builtin::array_at", "std::internal::builtin::array_at_mut",
		"std::internal::builtin::array_set", "std::internal::builtin::array_deinit":
		return c.checkBuiltinArrayMethod(name, typeArg, args, env)
	default:
		return "", false, nil
	}
}

// checkBuiltinBoxTypeApply validates std-only generic Box primitives.
func (c *Checker) checkBuiltinBoxTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std::internal::builtin::box":
		typ, err := c.checkBoxConstructor(typeArg, args, env)
		return typ, true, err
	case "std::internal::builtin::box_borrow",
		"std::internal::builtin::box_borrow_mut",
		"std::internal::builtin::box_deinit":
		return c.checkBuiltinBoxMethod(name, typeArg, args, env)
	default:
		return "", false, nil
	}
}

// checkBoxConstructor validates std::mem::box<T>(allocator, value) ownership.
func (c *Checker) checkBoxConstructor(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 2 {
		return "", errorf("box error: `std::mem::Box<%s>` expects allocator and value", elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("box error: `std::mem::Box<%s>` expects Allocator, got %s", elem, got)
	}
	got, err = c.moveContextualExpr(args[1], elem, env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", errorf("box error: `std::mem::Box<%s>` expects %s value, got %s",
			elem, elem, got)
	}
	return fmt.Sprintf("!std::mem::Box<%s>", elem), nil
}

// checkBuiltinBoxMethod validates std-only Box method primitives.
func (c *Checker) checkBuiltinBoxMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	method := strings.TrimPrefix(name, "std::internal::builtin::box_")
	if method == "borrow_mut" {
		method = "borrow_mut"
	}
	receiver := fmt.Sprintf("std::mem::Box<%s>", typeArg)
	return c.checkBuiltinReceiverMethod(name, receiver, func(rest []ast.Expression) (string, error) {
		if len(rest) != 0 {
			return "", errorf("box error: `Box.%s` expects 0 args, got %d", method, len(rest))
		}
		switch method {
		case "borrow":
			return "&" + typeArg, nil
		case "borrow_mut":
			return "&var " + typeArg, nil
		case "deinit":
			return "void", nil
		default:
			return "", errorf("box error: Box has no method `%s`", method)
		}
	}, args, env)
}

// checkBuiltinArrayMethod validates std-only Array method primitives.
func (c *Checker) checkBuiltinArrayMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	method := strings.TrimPrefix(name, "std::internal::builtin::array_")
	return c.checkBuiltinReceiverMethod(name, fmt.Sprintf("std::array::Array<%s>", typeArg),
		func(rest []ast.Expression) (string, error) {
			return c.checkArrayPrimitiveMethod(typeArg, method, rest, env)
		}, args, env)
}

// checkBuiltinReceiverMethod validates a trusted primitive receiver argument.
func (c *Checker) checkBuiltinReceiverMethod(
	name string,
	receiver string,
	checkRest func([]ast.Expression) (string, error),
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if len(args) == 0 {
		return "", true, errorf("move error: `%s` expects receiver", name)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != receiver {
		return "", true, errorf("move error: `%s` expects %s receiver, got %s",
			name, receiver, got)
	}
	typ, err := checkRest(args[1:])
	return typ, true, err
}

// checkArrayPrimitiveMethod validates Array primitives that back source wrappers.
func (c *Checker) checkArrayPrimitiveMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "at":
		if _, err := c.checkOneI64Arg("Array.at", args, env); err != nil {
			return "", err
		}
		return "!&" + elem, nil
	case "at_mut":
		if _, err := c.checkOneI64Arg("Array.at_mut", args, env); err != nil {
			return "", err
		}
		return "!&var " + elem, nil
	case "get", "get_or_panic":
		if len(args) != 1 {
			return "", errorf("array error: `Array.%s` expects 1 arg, got %d",
				name, len(args))
		}
		got, err := c.readExpr(args[0], env)
		if err != nil {
			return "", err
		}
		if got != "i64" {
			return "", errorf("array error: `Array.%s` expects i64 index, got %s", name, got)
		}
		if !isGenericParamName(elem) && !c.isCopyType(elem) {
			return "", errorf("array error: `Array.%s` requires copy element", name)
		}
		if name == "get" {
			return "!" + elem, nil
		}
		return elem, nil
	default:
		array := &binding{typeName: fmt.Sprintf("std::array::Array<%s>", elem)}
		return c.checkArrayMethod(array, elem, name, args, env)
	}
}

// checkBuiltinMapTypeApply validates the std-only Map runtime primitive.
func (c *Checker) checkBuiltinMapTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if strings.HasPrefix(name, "std::internal::builtin::map_") {
		return c.checkBuiltinMapMethod(name, typeArg, args, env)
	}
	if name != "std::internal::builtin::map" {
		return "", false, nil
	}
	typ, err := c.checkMapConstructorAllowTypeParams(typeArg, args, env)
	return typ, true, err
}

// checkBuiltinMapMethod validates std-only Map method primitives.
func (c *Checker) checkBuiltinMapMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	mapArgs, err := c.checkedMapArgsAllowTypeParams(typeArg)
	if err != nil {
		return "", true, err
	}
	receiver := fmt.Sprintf("std::map::Map<%s, %s>", mapArgs[0], mapArgs[1])
	method := strings.TrimPrefix(name, "std::internal::builtin::map_")
	return c.checkBuiltinReceiverMethod(name, receiver,
		func(rest []ast.Expression) (string, error) {
			mapValue := &binding{typeName: receiver}
			return c.checkMapPrimitiveMethod(mapValue, mapArgs[0], mapArgs[1], method, rest, env)
		}, args, env)
}

// checkMapPrimitiveMethod validates Map primitives that back source wrappers.
func (c *Checker) checkMapPrimitiveMethod(
	mapValue *binding,
	keyType string,
	valueType string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if !isGenericParamName(keyType) && !isGenericParamName(valueType) {
		argsText := mapValue.typeName[len("std::map::Map<") : len(mapValue.typeName)-1]
		return c.checkMapMethod(mapValue, argsText, name, args, env)
	}
	switch name {
	case "insert":
		if len(args) != 2 {
			return "", errorf("map error: `Map.insert` expects 2 args, got %d", len(args))
		}
		if got, err := c.readExpr(args[0], env); err != nil {
			return "", err
		} else if got != keyType {
			return "", errorf("map error: `Map.insert` expects %s key, got %s", keyType, got)
		}
		got, err := c.readContextualExpr(args[1], valueType, env)
		if err != nil {
			return "", err
		}
		if got != valueType {
			return "", errorf("map error: `Map.insert` expects %s value, got %s", valueType, got)
		}
		return "!void", nil
	case "get":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env); err != nil {
			return "", err
		}
		return "!" + valueType, nil
	case "contains":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env); err != nil {
			return "", err
		}
		return "bool", nil
	default:
		return c.checkMapMethod(mapValue,
			mapValue.typeName[len("std::map::Map<"):len(mapValue.typeName)-1], name, args, env)
	}
}

// checkMapPrimitiveKeyArg validates a generic Map wrapper key argument.
func (c *Checker) checkMapPrimitiveKeyArg(
	name string,
	keyType string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 1 {
		return errorf("map error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	if got != keyType {
		return errorf("map error: `Map.%s` expects %s key, got %s", name, keyType, got)
	}
	return nil
}

// checkedMapArgsAllowTypeParams validates Map arguments inside std generic wrappers.
func (c *Checker) checkedMapArgsAllowTypeParams(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, errorf("map error: std::map::Map expects 2 static arguments")
	}
	if isGenericParamName(args[0]) {
		return args, nil
	}
	if isGenericParamName(args[1]) {
		return args, nil
	}
	return c.checkedMapArgs(arg)
}

// isGenericParamName reports whether a type spelling is a std wrapper parameter.
func isGenericParamName(name string) bool {
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// checkGenericUserTypeApply validates ownership for source std generic wrappers.
func (c *Checker) checkGenericUserTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	fn := c.functions[name]
	if fn == nil || len(fn.sig.StaticParams) == 0 {
		return "", false, nil
	}
	if len(args) != len(fn.params) {
		return "", true, errorf("move error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	staticArgs, ok := splitGenericArgs(typeArg)
	if !ok || len(staticArgs) != len(fn.sig.StaticParams) {
		return "", true, errorf("move error: `%s` expects %d static arguments",
			name, len(fn.sig.StaticParams))
	}
	// Only the entries that declare types take part in substitution; a
	// compile-time value carries no ownership.
	typeArgs := []string{}
	for idx, param := range fn.sig.StaticParams {
		if param.IsType() {
			typeArgs = append(typeArgs, staticArgs[idx])
		}
	}
	if err := c.checkGenericWrapperTypeArgs(name, typeArgs); err != nil {
		return "", true, err
	}
	subst := map[string]string{}
	for idx, param := range fn.sig.TypeParamNames() {
		subst[param] = typeArgs[idx]
	}
	for idx, arg := range args {
		if err := c.checkGenericUserArg(name, fn, subst, idx, arg, env); err != nil {
			return "", true, err
		}
	}
	if err := c.checkGenericInstantiation(fn, subst); err != nil {
		return "", true, err
	}
	return substituteOwnershipType(returnTypeName(fn), subst), true, nil
}

// checkGenericInstantiation checks a generic function body for one static type set.
func (c *Checker) checkGenericInstantiation(fn *functionInfo, subst map[string]string) error {
	env := newScope(nil)
	c.defineParams(fn, env, subst)
	previousLoopDepth := c.loopDepth
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeArgValues := c.typeArgValues
	c.loopDepth = 0
	c.currentFunction = fn
	c.currentStd = fn.sig.Std
	c.typeArgValues = subst
	defer func() {
		c.loopDepth = previousLoopDepth
		c.currentFunction = previousFunction
		c.currentStd = previousStd
		c.typeArgValues = previousTypeArgValues
	}()
	return c.checkBlock(fn.body, env)
}

// checkGenericWrapperTypeArgs validates std wrapper-specific static ownership contracts.
func (c *Checker) checkGenericWrapperTypeArgs(name string, typeArgs []string) error {
	switch name {
	case "std::array::Array":
		return c.rejectArrayElementType(typeArgs[0])
	case "std::map::Map":
		if _, err := c.checkedMapArgs(strings.Join(typeArgs, ", ")); err != nil {
			return err
		}
	}
	return nil
}

// checkGenericUserArg validates an instantiated generic wrapper argument.
func (c *Checker) checkGenericUserArg(
	name string,
	fn *functionInfo,
	subst map[string]string,
	idx int,
	arg ast.Expression,
	env *scope,
) error {
	want := substituteOwnershipType(fn.params[idx].typeName, subst)
	// `std::mem::box<T>(allocator, value)` takes ownership of its value, and a
	// consume primitive moves its argument (ADR-0091); every other generic
	// wrapper argument is read in place.
	read := c.readContextualExpr
	if (name == "std::mem::box" && idx == 1) || (isConsumePrimitive(name) && idx == 0) {
		read = c.moveContextualExpr
	}
	got, err := read(arg, want, env)
	if err != nil {
		return err
	}
	if got != want {
		return errorf("move error: arg %d of `%s` expects %s, got %s",
			idx+1, name, want, got)
	}
	return nil
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
	case "ptr_read", "ptr_write", "volatile_read", "volatile_write":
		result, err := c.checkPointerBuiltin(expr, env)
		return result, true, err
	case "Io":
		return "", true, errorf("move error: use `std::io::blocking()`")
	default:
		return "", false, nil
	}
}

// readTryExpr reads a !T expression and returns T.
func (c *Checker) readTryExpr(expr *ast.TryExpr, env *scope) (string, error) {
	got, err := c.readExpr(expr.Value, env)
	if err != nil {
		return "", err
	}
	arg, ok := errorUnionElement(got)
	if !ok {
		return "", errorf("move error: try expects !T, got %s", got)
	}
	// A try can return early through the error path, which runs any active
	// errdefer cleanups. Their receivers must still be valid at this point.
	if err := c.validateErrDeferReceivers(env); err != nil {
		return "", err
	}
	// The same early exit must not leak a live owner: every owner must be
	// consumed or covered by a defer / errdefer cleanup before the try.
	if err := c.checkOwnersConsumed(env, 0, true); err != nil {
		return "", err
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

// checkPointerIntCastBuiltin reads pointer/integer conversion arguments without moving values.
func (c *Checker) checkPointerIntCastBuiltin(
	_ string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return typeArg, nil
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
			return "", errorAt(expr.Span, "move error: enum tag `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
		if _, exists := c.errorSets[ident.Name]; exists {
			return "", errorAt(expr.Span, "move error: error `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
		if _, exists := c.unions[ident.Name]; exists {
			return "", errorAt(expr.Span, "move error: union variant `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", err
	}
	if root, field, ok := directFieldRoot(expr, env); ok {
		if field != "" && root.fieldDeinit[field] {
			return "", errorAt(expr.Span, "move error: field `%s.%s` was deinitialized",
				root.name, field)
		}
		if root.activeMutBorrows > 0 {
			return "", errorAt(expr.Span,
				"borrow error: value `%s` cannot be read while mutably borrowed",
				root.name)
		}
		if root.fieldMutBorrows[field] > 0 {
			return "", errorAt(expr.Span,
				"borrow error: field `%s.%s` cannot be read while mutably borrowed",
				root.name, field)
		}
	}
	if fields := c.structs[receiverType]; fields != nil {
		if typ, ok := fields[expr.Name]; ok {
			return typ, nil
		}
	}
	if typ, ok := readFsFieldType(receiverType, expr.Name); ok {
		return typ, nil
	}
	return receiverType, nil
}

// readFsFieldType returns ownership types for builtin filesystem structs.
func readFsFieldType(receiverType string, field string) (string, bool) {
	switch receiverType {
	case "std::fs::Metadata":
		return readFsMetadataFieldType(field)
	case "std::fs::DirEntry":
		return readFsDirEntryFieldType(field)
	default:
		return "", false
	}
}

// readFsMetadataFieldType returns ownership types for std::fs::Metadata fields.
func readFsMetadataFieldType(field string) (string, bool) {
	switch field {
	case "size":
		return "i64", true
	case "is_dir":
		return "bool", true
	default:
		return "", false
	}
}

// readFsDirEntryFieldType returns ownership types for std::fs::DirEntry fields.
func readFsDirEntryFieldType(field string) (string, bool) {
	switch field {
	case "name", "path":
		return "[]u8", true
	case "is_dir":
		return "bool", true
	default:
		return "", false
	}
}

// readNamespaceExpr reads enum or payload-free union namespace lookup.
func (c *Checker) readNamespaceExpr(expr *ast.FieldExpr) (string, error) {
	dotted, ok := qualifiedName(expr.Receiver)
	if !ok {
		return "", errorAt(expr.Span, "move error: invalid namespace lookup `%s`", expr.String())
	}
	name := strings.ReplaceAll(dotted, ".", "::")
	if tags, exists := c.enums[name]; exists {
		if !tags[expr.Name] {
			return "", errorAt(expr.Span,
				"move error: unknown enum tag `%s::%s`", name, expr.Name)
		}
		return name, nil
	}
	if members, exists := c.errorSets[name]; exists {
		if !members[expr.Name] {
			return "", errorAt(expr.Span,
				"move error: unknown error `%s::%s`", name, expr.Name)
		}
		return name, nil
	}
	if variants, exists := c.unions[name]; exists {
		payload, ok := variants[expr.Name]
		if !ok {
			return "", errorAt(expr.Span, "move error: unknown union variant `%s::%s`",
				name, expr.Name)
		}
		if payload != "" {
			return "", errorAt(expr.Span,
				"move error: union variant `%s::%s` expects payload",
				name, expr.Name)
		}
		return name, nil
	}
	return "", errorAt(expr.Span, "move error: unknown namespace `%s`", name)
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
		return "", errorAt(
			expr.Span,
			"borrow error: field `%s` cannot be moved out of borrowed value `%s`",
			expr.String(),
			name,
		)
	}
	if c.containsArenaGet(expr.Receiver, env) {
		return "", errorAt(
			expr.Span,
			"arena error: arena.get returns a local borrow and its fields cannot be moved",
		)
	}
	return "", errorAt(expr.Span, "move error: field `%s` cannot be moved out of aggregate",
		expr.String())
}

// checkAssignmentBorrowConflict rejects writes that overlap active borrows.
func (c *Checker) checkAssignmentBorrowConflict(expr ast.Expression, env *scope) error {
	root, field, ok := directFieldRoot(expr, env)
	if !ok {
		return nil
	}
	if field == "" {
		if root.hasAnyBorrow() {
			return errorf("borrow error: value `%s` cannot be assigned while borrowed", root.name)
		}
		return nil
	}
	if root.borrowedParam && !root.mutBorrow {
		return errorf("borrow error: cannot assign field through shared borrow `%s`", root.name)
	}
	if root.activeBorrows > 0 || root.activeMutBorrows > 0 {
		return errorf("borrow error: field `%s.%s` cannot be assigned while value is borrowed",
			root.name, field)
	}
	if root.fieldBorrows[field] > 0 || root.fieldMutBorrows[field] > 0 {
		return errorf("borrow error: field `%s.%s` cannot be assigned while borrowed",
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

// readDerefExpr reads the value behind a local borrow or raw pointer.
func (c *Checker) readDerefExpr(expr *ast.DerefExpr, env *scope) (string, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		value, exists := env.lookup(ident.Name)
		if !exists {
			return "", errorAt(ident.Span, "move error: undefined variable `%s`", ident.Name)
		}
		if value.borrowedParam {
			return value.typeName, nil
		}
	}
	if typeName, ok, err := c.rawPointerDerefExprType(expr, env); ok || err != nil {
		return typeName, err
	}
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		return "", errorAt(ident.Span,
			"borrow error: `%s` is not a borrow or raw pointer", ident.Name)
	}
	return "", errorAt(expr.OperatorSpan,
		"borrow error: dereference expects a local borrow or raw pointer")
}

// rawPointerDerefExprType returns the element type for unchecked pointer dereference.
func (c *Checker) rawPointerDerefExprType(
	expr *ast.DerefExpr,
	env *scope,
) (string, bool, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if value, exists := env.lookup(ident.Name); exists && value.borrowedParam {
			return "", false, nil
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", false, err
	}
	elem, ok := rawPointerElement(receiverType)
	if !ok {
		return "", false, nil
	}
	if strings.HasPrefix(receiverType, "?") {
		return "", true, errorf(
			"borrow error: nullable raw pointer `%s` cannot be dereferenced",
			receiverType,
		)
	}
	return strings.TrimPrefix(elem, "const "), true, nil
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
				return "", true, errorf("move error: enum tag `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
			if _, exists := c.unions[ident.Name]; exists {
				return "", true, errorf("move error: union variant `%s.%s` must use `::`",
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
		return "", true, errorf("move error: unknown union variant `%s::%s`",
			ident.Name, field.Name)
	}
	if payload == "" {
		return "", true, errorf("move error: union variant `%s::%s` expects 0 args",
			ident.Name, field.Name)
	}
	if len(args) != 1 {
		return "", true, errorf("move error: union variant `%s::%s` expects 1 arg, got %d",
			ident.Name, field.Name, len(args))
	}
	if _, err := c.moveExpr(args[0], env); err != nil {
		return "", true, err
	}
	return ident.Name, true, nil
}

// checkMethodCallExpr validates ownership effects of value receiver methods.
func (c *Checker) checkMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if typ, ok, err := c.checkDirectFieldReceiverMethod(field, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBoxReceiverExpr(field, args, env); ok || err != nil {
		return typ, err
	}
	return c.checkLocalReceiverMethod(field, args, env)
}

// checkLocalReceiverMethod validates methods whose receiver must be a local binding.
func (c *Checker) checkLocalReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", errorAt(field.Span, "arena error: arena method receiver must be a local binding")
	}
	arena, exists := env.lookup(receiver.Name)
	if !exists {
		return "", errorAt(receiver.Span, "arena error: undefined arena `%s`", receiver.Name)
	}
	if arena.deinitialized {
		return "", errorAt(receiver.Span, "arena error: arena `%s` was deinitialized",
			receiver.Name)
	}
	if arena.moved {
		return "", errorAt(receiver.Span, "move error: moved value `%s` was used", receiver.Name)
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
	if !ok || base != "std::arena::Arena" {
		return c.checkNonArenaMethod(arena, field.Name, args, env)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arena, args, env)
	case "get":
		return c.checkArenaGet(arena, args, env)
	case "deinit":
		return c.checkArenaDeinit(arena, args)
	default:
		return "", errorf("arena error: unknown arena method `%s`", field.Name)
	}
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
	if base, _, ok := splitGenericType(value.typeName); ok && base == "std::mem::Box" {
		return c.checkBoxMethod(value, name, args)
	}
	if err := checkMapReceiverBorrow(value, name); err != nil {
		return "", err
	}
	if typ, ok, err := c.checkImplMethodCall(value, name, args, env); ok || err != nil {
		return typ, err
	}
	return c.checkPlainMethodArgs(args, env)
}

// checkDirectFieldReceiverMethod validates owner.field.method(...) calls.
func (c *Checker) checkDirectFieldReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	receiverExpr, ok := field.Receiver.(*ast.FieldExpr)
	if !ok {
		return "", false, nil
	}
	receiver, err := c.directFieldReceiver(receiverExpr, env)
	if err != nil {
		return "", true, err
	}
	if field.Name == "deinit" && !c.allowsDirectFieldCleanup(receiver) {
		return "", true, errorf(
			"move error: field cleanup `%s.deinit` is only allowed inside owner deinit",
			receiver.path,
		)
	}
	value := c.bindingForDirectFieldReceiver(receiver)
	typ, err := c.checkDirectFieldReceiverByType(value, field.Name, args, env)
	if err != nil {
		return "", true, err
	}
	if field.Name == "deinit" {
		receiver.owner.markFieldDeinit(receiver.field)
	}
	return typ, true, nil
}

// directFieldReceiver resolves the one-level field path used as a method receiver.
func (c *Checker) directFieldReceiver(
	field *ast.FieldExpr,
	env *scope,
) (*directFieldReceiver, error) {
	ownerIdent, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil, errorAt(field.Span,
			"move error: field method receiver only supports one direct field")
	}
	owner, exists := env.lookup(ownerIdent.Name)
	if !exists {
		return nil, errorAt(ownerIdent.Span,
			"move error: undefined variable `%s`", ownerIdent.Name)
	}
	if err := checkDeinitializedUse(ownerIdent.Name, owner, env, ownerIdent.Span); err != nil {
		return nil, err
	}
	if owner.moved {
		return nil, errorAt(ownerIdent.Span,
			"move error: moved value `%s` was used", ownerIdent.Name)
	}
	typeName, err := c.readFieldExpr(field, env)
	if err != nil {
		return nil, err
	}
	return &directFieldReceiver{
		owner: owner, field: field.Name, typeName: typeName,
		path: ownerIdent.Name + "." + field.Name,
	}, nil
}

// bindingForDirectFieldReceiver projects owner borrow state onto one owned field.
func (c *Checker) bindingForDirectFieldReceiver(receiver *directFieldReceiver) *binding {
	value := &binding{
		name: receiver.path, typeName: receiver.typeName,
		activeBorrows: receiver.owner.activeBorrows +
			receiver.owner.fieldBorrows[receiver.field],
		activeMutBorrows: receiver.owner.activeMutBorrows +
			receiver.owner.fieldMutBorrows[receiver.field],
	}
	base, _, ok := splitGenericType(receiver.typeName)
	if ok && base == "std::arena::Arena" {
		value.arenaID = c.directFieldArenaID(receiver)
	}
	return value
}

// checkDirectFieldReceiverByType dispatches a direct field receiver by its field type.
func (c *Checker) checkDirectFieldReceiverByType(
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if value.typeName == "std::string::String" {
		return c.checkStringMethod(value, name, args, env)
	}
	base, elem, ok := splitGenericType(value.typeName)
	if ok && base == "std::array::Array" {
		return c.checkArrayMethod(value, elem, name, args, env)
	}
	if ok && base == "std::map::Map" {
		return c.checkMapMethod(value, elem, name, args, env)
	}
	if ok && base == "std::mem::Box" {
		return c.checkBoxMethod(value, name, args)
	}
	if ok && base == "std::arena::Arena" {
		return c.checkFieldArenaMethod(value, name, args, env)
	}
	return c.checkNonArenaMethod(value, name, args, env)
}

// checkFieldArenaMethod validates direct field calls on owned arena fields.
func (c *Checker) checkFieldArenaMethod(
	arena *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "add":
		return c.checkArenaAdd(arena, args, env)
	case "get":
		return c.checkFieldArenaGet(arena, args, env)
	case "deinit":
		return c.checkArenaDeinit(arena, args)
	default:
		return "", errorf("arena error: unknown arena method `%s`", name)
	}
}

// checkFieldArenaGet permits typed wrapper methods to unwrap their own arena handles.
func (c *Checker) checkFieldArenaGet(
	arena *binding,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("arena error: `arena.get` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "std::arena::Arena" {
		return "", errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.checkKnownHandleProvenance(arena, args[0], env); err != nil {
		return "", err
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return arg, nil
}

// checkBoxReceiverExpr validates methods on local Box values and direct Box fields.
func (c *Checker) checkBoxReceiverExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	typeName, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return "", false, nil
	}
	base, _, ok := splitGenericType(typeName)
	if !ok || base != "std::mem::Box" {
		return "", false, nil
	}
	target, borrowedField, err := c.borrowTarget(field.Receiver, env)
	if err != nil {
		return "", true, err
	}
	if field.Name == "deinit" && borrowedField != "" {
		return "", true, errorf("box error: `Box.deinit` requires local Box receiver")
	}
	typ, err := c.checkBoxMethodForTarget(target, borrowedField, field.Name, args)
	return typ, true, err
}

// checkBoxMethodForTarget validates Box methods with a tracked borrow root.
func (c *Checker) checkBoxMethodForTarget(
	target *binding,
	field string,
	name string,
	args []ast.Expression,
) (string, error) {
	switch name {
	case "borrow", "borrow_mut":
		return "", errorf("box error: `Box.%s` must be bound with `let name = box.%s()`",
			name, name)
	case "deinit":
		if target.hasAnyBorrow() {
			return "", errorf("box error: `Box.deinit` cannot run while box is borrowed")
		}
		if len(args) != 0 {
			return "", errorf("box error: `Box.deinit` expects 0 args, got %d", len(args))
		}
		if field == "" {
			target.moved = true
		}
		return "void", nil
	default:
		return "", errorf("box error: Box has no method `%s`", name)
	}
}

// checkBoxMethod validates methods on a local Box binding.
func (c *Checker) checkBoxMethod(
	box *binding,
	name string,
	args []ast.Expression,
) (string, error) {
	return c.checkBoxMethodForTarget(box, "", name, args)
}

// checkStringReceiverBorrow rejects String methods whose receiver cannot be tracked safely.
func checkStringReceiverBorrow(value *binding, name string) error {
	if name == "deinit" && value.borrowedParam {
		return errorf("string error: `String.deinit` requires owned String receiver")
	}
	if isStringMutatingMethod(name) && value.borrowedParam && !value.mutBorrow {
		return errorf("string error: `String.%s` requires mutable String receiver", name)
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
		return errorf("map error: `Map.deinit` requires owned Map receiver")
	}
	if name == "insert" && value.borrowedParam && !value.mutBorrow {
		return errorf("map error: `Map.insert` requires mutable Map receiver")
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
		return "", errorf(
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
		return "", errorf("string error: String has no method `%s`", name)
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
		return errorf("string error: `String.%s` cannot run while string is borrowed", name)
	}
	return nil
}

// checkStringNoArgs validates no-argument String methods.
func checkStringNoArgs(name string, args []ast.Expression) error {
	if len(args) != 0 {
		return errorf("string error: `String.%s` expects 0 args, got %d", name, len(args))
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
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(got, "[]u8") {
		return "", errorf("string error: `String.%s` expects []u8, got %s", name, got)
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
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", errorf("string error: `String.%s` expects i64, got %s", name, got)
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
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readContextualExpr(args[0], "u8", env)
	if err != nil {
		return "", err
	}
	if got != "u8" {
		return "", errorf("string error: `String.%s` expects u8, got %s", name, got)
	}
	return "!void", nil
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
	case "reserve":
		return c.checkArrayCountMutation(array, name, args, env)
	case "pop":
		return c.checkArrayPop(array, elem, name, args, true)
	case "pop_or_panic":
		return c.checkArrayPop(array, elem, name, args, false)
	case "len", "capacity":
		return c.checkArrayReadNoArgs(array, name, args)
	case "get", "get_or_panic":
		if array.activeMutBorrows > 0 {
			return "", errorf("array error: `Array.%s` cannot read while mutably borrowed", name)
		}
		return c.checkArrayGet(elem, name, args, env)
	case "at", "at_mut":
		return "", errorf("array error: `Array.%s` must be bound with `let name = try array.%s(...)`",
			name, name)
	case "set":
		return c.checkArraySet(array, elem, args, env)
	case "deinit":
		if array.hasAnyBorrow() {
			return "", errorf("array error: `Array.%s` cannot run while array is borrowed", name)
		}
		if len(args) != 0 {
			return "", errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
		}
		if name == "deinit" {
			array.moved = true
		}
		return "void", nil
	default:
		return "", errorf("array error: Array has no method `%s`", name)
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
		return "", errorf("array error: Array has no method `%s`", name)
	}
	switch name {
	case "truncate":
		return c.checkArrayCountMutation(array, name, args, env)
	case "clear":
		if array.hasAnyBorrow() {
			return "", errorf("array error: `Array.clear` cannot run while array is borrowed")
		}
		if len(args) != 0 {
			return "", errorf("array error: `Array.clear` expects 0 args, got %d", len(args))
		}
		return "void", nil
	default:
		if elem != "u8" {
			return "", errorf("array error: `Array.as_bytes` requires Array<u8>")
		}
		return c.checkArrayReadNoArgs(array, name, args)
	}
}

// isStdArrayStorageMethod reports methods reserved for std-owned storage wrappers.
func isStdArrayStorageMethod(name string) bool {
	return name == "truncate" || name == "clear" || name == "as_bytes"
}

// checkArrayCountMutation validates one-count Array mutations.
func (c *Checker) checkArrayCountMutation(
	array *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if array.hasAnyBorrow() {
		return "", errorf("array error: `Array.%s` cannot run while array is borrowed", name)
	}
	if len(args) != 1 {
		return "", errorf("array error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "i64" {
		return "", errorf("array error: `Array.%s` expects i64, got %s", name, got)
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
		return "", errorf("array error: `Array.append` cannot run while array is borrowed")
	}
	if len(args) != 1 {
		return "", errorf("array error: `Array.append` expects 1 arg, got %d", len(args))
	}
	got, err := c.moveContextualExpr(args[0], elem, env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", errorf("array error: `Array.append` expects %s, got %s", elem, got)
	}
	return "!void", nil
}

// checkArrayPop validates moving one initialized element out of an Array.
func (c *Checker) checkArrayPop(
	array *binding,
	elem string,
	name string,
	args []ast.Expression,
	returnsError bool,
) (string, error) {
	if array.hasAnyBorrow() {
		return "", errorf("array error: `Array.%s` cannot run while array is borrowed", name)
	}
	if len(args) != 0 {
		return "", errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
	}
	if !returnsError {
		return elem, nil
	}
	return "!" + elem, nil
}

// checkArrayReadNoArgs validates len/capacity reads.
func (c *Checker) checkArrayReadNoArgs(
	array *binding,
	name string,
	args []ast.Expression,
) (string, error) {
	if array.activeMutBorrows > 0 {
		return "", errorf("array error: `Array.%s` cannot read while mutably borrowed", name)
	}
	if len(args) != 0 {
		return "", errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
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
		return "", errorf("array error: `Array.set` cannot run while array is borrowed")
	}
	if len(args) != 2 {
		return "", errorf("array error: `Array.set` expects 2 args, got %d", len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "i64" {
		return "", errorf("array error: `Array.set` expects i64 index, got %s", got)
	}
	got, err := c.moveContextualExpr(args[1], elem, env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", errorf("array error: `Array.set` expects %s value, got %s", elem, got)
	}
	return "!void", nil
}

// checkArrayGet validates copy-only Array<T> reads in the prototype.
func (c *Checker) checkArrayGet(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("array error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", errorf("array error: `Array.%s` expects i64 index, got %s", name, got)
	}
	if !c.isCopyType(elem) {
		return "", errorf("array error: `Array.%s` requires copy element", name)
	}
	if name == "get" {
		return "!" + elem, nil
	}
	return elem, nil
}

// checkMapMethod validates ownership effects for owned Map<[]u8, V> methods.
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
		return "", errorf("map error: Map has no method `%s`", name)
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
		return "", errorf("map error: `Map.insert` cannot run while map is borrowed")
	}
	if len(args) != 2 {
		return "", errorf("map error: `Map.insert` expects 2 args, got %d", len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if !sameOwnershipType(got, "[]u8") {
		return "", errorf("map error: `Map.insert` expects []u8 key, got %s", got)
	}
	got, err := c.readContextualExpr(args[1], valueType, env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(got, valueType) {
		return "", errorf("map error: `Map.insert` expects %s value, got %s", valueType, got)
	}
	return "!void", nil
}

// checkMapKeyArg validates one []u8 lookup key.
func (c *Checker) checkMapKeyArg(name string, args []ast.Expression, env *scope) error {
	if len(args) != 1 {
		return errorf("map error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	if !sameOwnershipType(got, "[]u8") {
		return errorf("map error: `Map.%s` expects []u8 key, got %s", name, got)
	}
	return nil
}

// checkMapReadNoArgs validates no-argument Map reads.
func (c *Checker) checkMapReadNoArgs(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", errorf("map error: `Map.%s` expects 0 args, got %d", name, len(args))
	}
	return "i64", nil
}

// checkMapDeinit validates owned Map cleanup and marks it moved.
func (c *Checker) checkMapDeinit(mapValue *binding, args []ast.Expression) (string, error) {
	if mapValue.hasAnyBorrow() {
		return "", errorf("map error: `Map.deinit` cannot run while map is borrowed")
	}
	if len(args) != 0 {
		return "", errorf("map error: `Map.deinit` expects 0 args, got %d", len(args))
	}
	mapValue.moved = true
	return "void", nil
}

// checkOneI64Arg reads one i64 argument.
func (c *Checker) checkOneI64Arg(name string, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", errorf("parallel error: `%s` expects 1 arg, got %d", name, len(args))
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

// checkImplMethodCall applies a concrete impl method signature to a receiver call.
func (c *Checker) checkImplMethodCall(
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	method := c.implMethod(value.typeName, name)
	if method == nil {
		return "", false, nil
	}
	if len(method.params) == 0 {
		return "", true, errorf("move error: method `%s` must have self parameter",
			method.name)
	}
	if len(args) != len(method.params)-1 {
		return "", true, errorf("move error: `%s` expects %d args, got %d",
			method.name, len(method.params)-1, len(args))
	}
	if err := c.checkImplMethodArgs(method, args, env); err != nil {
		return "", true, err
	}
	if name == "deinit" && returnTypeName(method) == "void" {
		value.moved = true
	}
	return returnTypeName(method), true, nil
}

// checkImplMethodArgs applies ownership effects for explicit method arguments.
func (c *Checker) checkImplMethodArgs(
	method *functionInfo,
	args []ast.Expression,
	env *scope,
) error {
	call := &functionInfo{
		name: method.name, sig: method.sig, params: method.params[1:], body: method.body,
	}
	borrowed, err := c.activateBorrowArgs(call, args, env)
	if err != nil {
		return err
	}
	defer releaseTemporaryBorrows(borrowed)
	for idx, arg := range args {
		if err := c.checkImplMethodArg(method, idx+1, arg, env); err != nil {
			return err
		}
	}
	return nil
}

// checkImplMethodArg mirrors user-call argument ownership for one method parameter.
func (c *Checker) checkImplMethodArg(
	method *functionInfo,
	paramIndex int,
	arg ast.Expression,
	env *scope,
) error {
	param := method.params[paramIndex]
	if param.borrow {
		if param.mutBorrow {
			return nil
		}
		_, err := c.readExpr(arg, env)
		return err
	}
	_, err := c.moveExpr(arg, env)
	return err
}

// checkArenaAdd moves one value into an arena and returns a handle.
func (c *Checker) checkArenaAdd(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", errorf("arena error: `arena.add` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "std::arena::Arena" {
		return "", errorf("arena error: `%s` is not an arena", arena.name)
	}
	got, err := c.moveContextualExpr(args[0], arg, env)
	if err != nil {
		return "", err
	}
	if got != arg {
		return "", errorf("arena error: `arena.add` expects %s, got %s", arg, got)
	}
	return fmt.Sprintf("std::arena::Handle<%s>", arg), nil
}

// checkArenaGet reads a handle and returns a local borrow-like value.
func (c *Checker) checkArenaGet(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", errorf("arena error: `arena.get` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "std::arena::Arena" {
		return "", errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.checkHandleProvenance(arena, args[0], env); err != nil {
		return "", err
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return arg, nil
}

// checkArenaDeinit validates explicit arena cleanup and invalidates the binding.
func (c *Checker) checkArenaDeinit(arena *binding, args []ast.Expression) (string, error) {
	if arena.hasAnyBorrow() {
		return "", errorf("arena error: `arena.deinit` cannot run while arena is borrowed")
	}
	if len(args) != 0 {
		return "", errorf("arena error: `arena.deinit` expects 0 args, got %d", len(args))
	}
	arena.deinitialized = true
	arena.moved = true
	return "void", nil
}

// checkHandleProvenance rejects handles that came from a different known arena.
func (c *Checker) checkHandleProvenance(arena *binding, expr ast.Expression, env *scope) error {
	if arena.arenaID == 0 {
		return errorf("arena error: arena `%s` has unknown provenance", arena.name)
	}
	if addArena := c.arenaAddReceiver(expr, env); addArena != nil {
		if addArena.arenaID != arena.arenaID {
			return errorf("arena error: handle from `%s` does not belong to arena `%s`",
				addArena.name, arena.name)
		}
		return nil
	}
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return errorf("arena error: handle expression has unknown arena provenance")
	}
	handle, exists := env.lookup(ident.Name)
	if !exists {
		return errorf("arena error: undefined handle `%s`", ident.Name)
	}
	if handle.handleArenaID == 0 {
		return errorf("arena error: handle `%s` has unknown arena provenance", ident.Name)
	}
	if handle.handleArenaID != arena.arenaID {
		return errorf("arena error: handle `%s` does not belong to arena `%s`",
			ident.Name, arena.name)
	}
	return nil
}

// checkKnownHandleProvenance rejects only known mismatches for field-owned arenas.
func (c *Checker) checkKnownHandleProvenance(
	arena *binding,
	expr ast.Expression,
	env *scope,
) error {
	if arena.arenaID == 0 {
		return nil
	}
	if addArena := c.arenaAddReceiver(expr, env); addArena != nil && addArena.arenaID != 0 {
		if addArena.arenaID != arena.arenaID {
			return errorf("arena error: handle from `%s` does not belong to arena `%s`",
				addArena.name, arena.name)
		}
		return nil
	}
	provenance := c.knownHandleProvenance(expr, env)
	if provenance != 0 && provenance != arena.arenaID {
		return errorf("arena error: handle expression does not belong to arena `%s`",
			arena.name)
	}
	return nil
}

// knownHandleProvenance returns tracked arena identity for handle-like expressions.
func (c *Checker) knownHandleProvenance(expr ast.Expression, env *scope) int {
	ident, ok := expr.(*ast.IdentExpr)
	if ok {
		value, exists := env.lookup(ident.Name)
		if exists {
			return value.handleArenaID
		}
	}
	return 0
}

// activateBorrowArgs marks identifier arguments that are borrowed for this call.
func (c *Checker) activateBorrowArgs(
	fn *functionInfo,
	args []ast.Expression,
	env *scope,
) ([]temporaryBorrow, error) {
	borrowed := []temporaryBorrow{}
	for idx, arg := range args {
		if !fn.params[idx].borrow {
			continue
		}
		value, field, err := c.callBorrowTarget(arg, env)
		if err != nil {
			releaseTemporaryBorrows(borrowed)
			return nil, err
		}
		if value == nil && fn.params[idx].mutBorrow {
			releaseTemporaryBorrows(borrowed)
			return nil, errorf(
				"borrow error: mutable borrow argument must be a local binding or direct field",
			)
		}
		if value != nil {
			if fn.params[idx].mutBorrow && value.borrowedParam && !value.mutBorrow {
				releaseTemporaryBorrows(borrowed)
				return nil, errorf(
					"borrow error: shared borrow `%s` cannot be forwarded as mutable",
					value.name,
				)
			}
			mutable := fn.params[idx].mutBorrow
			if err := checkBorrowConflictForField(value, field, mutable); err != nil {
				releaseTemporaryBorrows(borrowed)
				return nil, err
			}
			c.activateBorrow(value, field, mutable)
			borrowed = append(borrowed, temporaryBorrow{
				value: value, field: field, mutable: mutable,
			})
		}
	}
	return borrowed, nil
}

// callBorrowTarget resolves call-scoped borrowable places and ignores non-place shared values.
func (c *Checker) callBorrowTarget(
	arg ast.Expression,
	env *scope,
) (*binding, string, error) {
	if prefix, ok := borrowPrefix(arg); ok {
		return c.borrowTarget(prefix.Right, env)
	}
	if field, ok := arg.(*ast.FieldExpr); ok && field.Namespace {
		return nil, "", nil
	}
	switch arg.(type) {
	case *ast.IdentExpr, *ast.FieldExpr:
		return c.borrowTarget(arg, env)
	default:
		return nil, "", nil
	}
}

// checkBorrowConflict rejects aliasing that would overlap a mutable borrow.
func checkBorrowConflict(value *binding, mutable bool) error {
	return checkBorrowConflictForField(value, "", mutable)
}

// checkBorrowConflictForField rejects overlapping whole-value or field borrows.
func checkBorrowConflictForField(value *binding, field string, mutable bool) error {
	if field != "" {
		if value.activeMutBorrows > 0 {
			return errorf(
				"borrow error: value `%s` cannot be borrowed while mutably borrowed",
				value.name,
			)
		}
		if mutable && value.activeBorrows > 0 {
			return errorf(
				"borrow error: field `%s.%s` cannot be mutably borrowed while value is borrowed",
				value.name,
				field,
			)
		}
		if mutable && value.fieldBorrows[field] > 0 {
			return errorf(
				"borrow error: field `%s.%s` cannot be mutably borrowed while borrowed",
				value.name,
				field,
			)
		}
		if value.fieldMutBorrows[field] > 0 {
			return errorf(
				"borrow error: field `%s.%s` cannot be borrowed while mutably borrowed",
				value.name,
				field,
			)
		}
		return nil
	}
	if mutable && value.activeBorrows > 0 {
		return errorf(
			"borrow error: value `%s` cannot be mutably borrowed while borrowed",
			value.name,
		)
	}
	if value.activeMutBorrows > 0 || len(value.fieldMutBorrows) > 0 {
		return errorf(
			"borrow error: value `%s` cannot be borrowed while mutably borrowed",
			value.name,
		)
	}
	if mutable && len(value.fieldBorrows) > 0 {
		return errorf(
			"borrow error: value `%s` cannot be mutably borrowed while field is borrowed",
			value.name,
		)
	}
	return nil
}

// releaseTemporaryBorrows clears temporary borrow state for a completed call.
func releaseTemporaryBorrows(values []temporaryBorrow) {
	for _, borrow := range values {
		releaseTemporaryBorrow(borrow)
	}
}

// releaseTemporaryBorrow clears one call-scoped whole-value or field borrow.
func releaseTemporaryBorrow(borrow temporaryBorrow) {
	value := borrow.value
	if borrow.field == "" {
		if borrow.mutable && value.activeMutBorrows > 0 {
			value.activeMutBorrows--
		} else if !borrow.mutable && value.activeBorrows > 0 {
			value.activeBorrows--
		}
		return
	}
	if borrow.mutable && value.fieldMutBorrows[borrow.field] > 0 {
		value.fieldMutBorrows[borrow.field]--
		if value.fieldMutBorrows[borrow.field] == 0 {
			delete(value.fieldMutBorrows, borrow.field)
		}
		return
	}
	if !borrow.mutable && value.fieldBorrows[borrow.field] > 0 {
		value.fieldBorrows[borrow.field]--
		if value.fieldBorrows[borrow.field] == 0 {
			delete(value.fieldBorrows, borrow.field)
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
	if isArenaConstructorExpr(expr) {
		value.arenaID = value.id
		return
	}
	if arena := c.arenaAddReceiver(expr, env); arena != nil {
		value.handleArenaID = arena.arenaID
	}
}

// isArenaConstructorExpr reports the public std::arena::new<T>(allocator) constructor.
func isArenaConstructorExpr(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	typeApply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return false
	}
	name, ok := qualifiedName(typeApply.Callee)
	return ok && name == "std::arena::new"
}

// directFieldArenaID returns a stable arena identity for one owner field.
func (c *Checker) directFieldArenaID(receiver *directFieldReceiver) int {
	if receiver.owner.fieldArenaIDs == nil {
		receiver.owner.fieldArenaIDs = map[string]int{}
	}
	if id := receiver.owner.fieldArenaIDs[receiver.field]; id != 0 {
		return id
	}
	c.nextID++
	receiver.owner.fieldArenaIDs[receiver.field] = c.nextID
	return c.nextID
}

// allowsDirectFieldCleanup reports whether field.deinit is inside owner deinit.
func (c *Checker) allowsDirectFieldCleanup(receiver *directFieldReceiver) bool {
	fn := c.currentFunction
	if fn == nil || stdmethod.CallName(fn.sig.Name) != "deinit" || returnTypeName(fn) != "void" {
		return false
	}
	if len(fn.params) == 0 || len(fn.sig.Params) == 0 {
		return false
	}
	if fn.sig.Params[0].Name != receiver.owner.name {
		return false
	}
	return sameOwnershipType(fn.params[0].typeName, receiver.owner.typeName)
}

// matchesOwnerUnionDeinit reports whether a `match` consumes the active variant
// of a union inside that union's own `deinit(self: T) -> void`. Only there is the
// active payload owned and cleanable; everywhere else it stays a borrow.
func (c *Checker) matchesOwnerUnionDeinit(value ast.Expression, valueType string) bool {
	fn := c.currentFunction
	if fn == nil || stdmethod.CallName(fn.sig.Name) != "deinit" || returnTypeName(fn) != "void" {
		return false
	}
	if len(fn.params) == 0 || len(fn.sig.Params) == 0 {
		return false
	}
	if fn.params[0].borrow || fn.params[0].mutBorrow {
		return false
	}
	if !sameOwnershipType(fn.params[0].typeName, valueType) {
		return false
	}
	ident, ok := value.(*ast.IdentExpr)
	if !ok || ident.Name != fn.sig.Params[0].Name {
		return false
	}
	return c.unions[valueType] != nil
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
	if ok {
		arena, _ := env.lookup(receiver.Name)
		return arena
	}
	fieldReceiver, ok := field.Receiver.(*ast.FieldExpr)
	if !ok {
		return nil
	}
	direct, err := c.directFieldReceiver(fieldReceiver, env)
	if err != nil {
		return nil
	}
	base, _, ok := splitGenericType(direct.typeName)
	if !ok || base != "std::arena::Arena" {
		return nil
	}
	return c.bindingForDirectFieldReceiver(direct)
}

// isArenaGetExpr reports whether expr is an arena.get call.
func (c *Checker) isArenaGetExpr(expr ast.Expression, env *scope) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "get" {
		return false
	}
	receiver, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return false
	}
	base, _, ok := splitGenericType(receiver)
	return ok && base == "std::arena::Arena"
}

// containsArenaGet reports whether an expression reads through arena.get.
func (c *Checker) containsArenaGet(expr ast.Expression, env *scope) bool {
	if inner, ok := transparentExprValue(expr); ok {
		return c.containsArenaGet(inner, env)
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if c.isArenaGetExpr(e, env) {
			return true
		}
		for _, arg := range e.Args {
			if c.containsArenaGet(arg, env) {
				return true
			}
		}
	case *ast.FieldExpr:
		return c.containsArenaGet(e.Receiver, env)
	case *ast.BinaryExpr:
		return c.containsArenaGet(e.Left, env) || c.containsArenaGet(e.Right, env)
	case *ast.CastExpr:
		return c.containsArenaGet(e.Value, env)
	case *ast.TryExpr:
		return c.containsArenaGet(e.Value, env)
	case *ast.ComptimeExpr:
		return c.containsArenaGet(e.Expr, env)
	}
	return false
}

// readIdent resolves a variable reference without moving it.
func readIdent(ident *ast.IdentExpr, env *scope) (string, error) {
	value, ok := env.lookup(ident.Name)
	if ok {
		if err := checkDeinitializedUse(ident.Name, value, env, ident.Span); err != nil {
			return "", err
		}
		if value.moved {
			return "", errorAt(ident.Span, "move error: moved value `%s` was used", ident.Name)
		}
		if value.activeMutBorrows > 0 || len(value.fieldMutBorrows) > 0 {
			return "", errorAt(ident.Span,
				"borrow error: value `%s` cannot be read while mutably borrowed",
				ident.Name)
		}
		return value.typeName, nil
	}
	if ident.Name == "void" {
		return "", errorAt(ident.Span, "move error: void is not a value")
	}
	return "", errorAt(ident.Span, "move error: undefined variable `%s`", ident.Name)
}

// checkDeinitializedUse rejects arenas and known handles after arena cleanup.
func checkDeinitializedUse(name string, value *binding, env *scope, span ast.Span) error {
	if value.deinitialized {
		return errorAt(span, "arena error: arena `%s` was deinitialized", name)
	}
	if value.handleArenaID == 0 {
		return nil
	}
	arena, ok := env.lookupArenaID(value.handleArenaID)
	if ok && arena.deinitialized {
		return errorAt(span, "arena error: handle `%s` cannot be used after arena `%s` deinit",
			name, arena.name)
	}
	return nil
}

// checkDeinitializedBorrow rejects borrowing an arena after cleanup.
func checkDeinitializedBorrow(name string, value *binding, span ast.Span) error {
	if value.deinitialized {
		return errorAt(span, "arena error: arena `%s` was deinitialized", name)
	}
	return nil
}

// errorUnionElement extracts T from !T.
func errorUnionElement(typeName string) (string, bool) {
	_, success, ok := errorUnionParts(typeName)
	return success, ok
}

// errorUnionParts extracts error and success types from !T or Error!T.
func errorUnionParts(typeName string) (string, string, bool) {
	return typ.ErrorUnionParts(typeName)
}

// returnTypeName returns void for functions without an explicit return type.
func returnTypeName(fn *functionInfo) string {
	if fn.returnType == "" {
		return "void"
	}
	return fn.returnType
}

// implMethod returns the concrete method signature for typeName when known.
func (c *Checker) implMethod(typeName string, method string) *functionInfo {
	methods := c.impls[typeName]
	if methods == nil {
		return nil
	}
	return methods[method]
}

// substituteOwnershipType instantiates simple generic wrapper type spellings.
func substituteOwnershipType(typeName string, subst map[string]string) string {
	if replacement, ok := subst[typeName]; ok {
		return replacement
	}
	out, err := typ.SubstituteText(typeName, subst)
	if err != nil {
		// A spelling this checker cannot parse is left as it stands; rejecting
		// it belongs to the type checker and its diagnostic.
		return typeName
	}
	return out
}

// instantiateTypeArgText replaces in-scope generic type parameters in a static list.
func (c *Checker) instantiateTypeArgText(typeArg string) string {
	if len(c.typeArgValues) == 0 {
		return typeArg
	}
	args, err := typ.SplitArgs(typeArg)
	if err != nil {
		return substituteOwnershipType(typeArg, c.typeArgValues)
	}
	for idx, arg := range args {
		args[idx] = substituteOwnershipType(arg, c.typeArgValues)
	}
	return strings.Join(args, ", ")
}

// isCopyType reports whether values of typeName can be reused after move contexts.
func (c *Checker) isCopyType(typeName string) bool {
	if typeName == "[]u8" {
		return true
	}
	if isRawPointerType(typeName) {
		return true
	}
	// An error carries nothing, so reading one leaves nothing behind to move.
	if c.errorSets[typeName] != nil {
		return true
	}
	if c.enums[typeName] != nil {
		return true
	}
	switch typeName {
	case "bool", "void", "Io", "Allocator", "std::fs::Metadata", "std::fs::DirEntry",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64", "[]u8":
		return true
	default:
		return false
	}
}

// sameOwnershipType compares exact type spellings.
func sameOwnershipType(left string, right string) bool {
	return left == right
}

// fieldOwnershipType returns the full field type, including borrow prefixes.
func fieldOwnershipType(field ast.Field) string {
	if !field.Borrow {
		return typ.Text(field.TypeName)
	}
	if field.MutBorrow {
		return "&var " + typ.Text(field.TypeName)
	}
	return "&" + typ.Text(field.TypeName)
}

// explicitOwnershipBorrowType extracts &T and &var T spellings.
func explicitOwnershipBorrowType(typeName string) (string, bool, string, bool) {
	if !strings.HasPrefix(typeName, "&") {
		return "", false, "", false
	}
	rest := strings.TrimPrefix(typeName, "&")
	mutable := false
	if strings.HasPrefix(rest, "var ") {
		mutable = true
		rest = strings.TrimPrefix(rest, "var ")
	}
	if rest == "" {
		return "", false, "", false
	}
	return "", mutable, rest, true
}

// isRawPointerType reports whether typeName is a raw pointer spelling.
func isRawPointerType(typeName string) bool {
	_, ok := rawPointerElement(typeName)
	return ok
}

// isDynType reports whether typeName is a dynamic contract object spelling.
func isDynType(typeName string) bool {
	return strings.HasPrefix(typeName, "dyn ")
}

// rawPointerElement extracts the element spelling from ptr<T> or ?ptr<T>.
func rawPointerElement(typeName string) (string, bool) {
	name := typeName
	if len(name) > 0 && name[0] == '?' {
		name = name[1:]
	}
	base, elem, ok := splitGenericType(name)
	if !ok || base != "ptr" {
		return "", false
	}
	return elem, true
}

// checkNoArgOwnershipCall validates a zero-argument builtin constructor.
func checkNoArgOwnershipCall(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", errorf("move error: `%s` expects 0 args, got %d", name, len(args))
	}
	return name, nil
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
		return left + "::" + e.Name, true
	default:
		return "", false
	}
}

// checkedMapArgs validates and returns Map key/value static type arguments.
func (c *Checker) checkedMapArgs(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, errorf("map error: std::map::Map expects 2 static arguments")
	}
	if !sameOwnershipType(args[0], "[]u8") {
		return nil, errorf("map error: std::map::Map key type must be []u8")
	}
	if isGenericParamName(args[1]) {
		return args, nil
	}
	if !c.isCopyType(args[1]) {
		return nil, errorf("map error: std::map::Map value type must be copy")
	}
	return args, nil
}

// splitGenericType extracts base and raw arguments from base<args>.
func splitGenericType(name string) (string, string, bool) {
	return typ.SplitApply(name)
}

// splitGenericArgs extracts top-level comma-separated static arguments.
func splitGenericArgs(arg string) ([]string, bool) {
	args, err := typ.SplitArgs(arg)
	if err != nil {
		return nil, false
	}
	return args, true
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

// lookupArenaID resolves a tracked arena owner by provenance ID.
func (s *scope) lookupArenaID(arenaID int) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			if value.arenaID == arenaID {
				return value, true
			}
		}
	}
	return nil, false
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

// markFieldDeinit records that an owned direct field has been cleaned up.
func (b *binding) markFieldDeinit(field string) {
	if b.fieldDeinit == nil {
		b.fieldDeinit = map[string]bool{}
	}
	b.fieldDeinit[field] = true
}

// clearFieldDeinit records assignment of a fresh value into a direct field.
func (b *binding) clearFieldDeinit(field string) {
	if b.fieldDeinit == nil {
		return
	}
	delete(b.fieldDeinit, field)
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
	case *ast.DeferStmt:
		return exprIdentUses(s.Expr)
	case *ast.ErrDeferStmt:
		return exprIdentUses(s.Expr)
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
	if inner, ok := transparentExprValue(expr); ok {
		return exprIdentUses(inner)
	}
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.IdentExpr:
		return []string{e.Name}
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
	case *ast.ComptimeExpr:
		return exprIdentUses(e.Expr)
	default:
		return nil
	}
}

// borrowPrefix reports whether an expression is &T or &var T syntax.
func borrowPrefix(expr ast.Expression) (*ast.PrefixExpr, bool) {
	prefix, ok := expr.(*ast.PrefixExpr)
	if !ok || (prefix.Operator != "&" && prefix.Operator != "&var") {
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
		if value.deinitialized {
			if target, ok := byID[value.id]; ok {
				target.deinitialized = true
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
