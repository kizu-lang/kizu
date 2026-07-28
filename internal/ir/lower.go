package ir

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdprim"
)

// Lower converts a checked Kizu AST into typed SSA IR.
func Lower(program *ast.Program) (*Module, error) {
	l := newLowerer(program)
	return l.lower()
}

type lowerer struct {
	program     *ast.Program
	module      *Module
	signatures  map[string]Signature
	current     *Function
	block       *Block
	env         map[string]Value
	nextValue   int
	nextBlock   int
	loops       []*loopContext
	deferFrames [][]Cleanup
}

type loopContext struct {
	label         string
	breakTo       string
	continueTo    string
	deferDepth    int
	breakEdges    []loopEdge
	continueEdges []loopEdge
}

type loopEdge struct {
	block string
	env   map[string]Value
}

// newLowerer prepares lookup tables used during lowering.
func newLowerer(program *ast.Program) *lowerer {
	return &lowerer{
		program: program,
		module: &Module{
			Structs: map[string]Struct{},
			Enums:   map[string]Enum{},
			Unions:  map[string]Union{},
		},
		signatures: map[string]Signature{},
	}
}

// lower performs declaration collection and function lowering.
func (l *lowerer) lower() (*Module, error) {
	l.collectDecls()
	for _, decl := range l.program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if fn.ExternABI != "" {
			continue
		}
		if len(fn.TypeParams) > 0 {
			continue
		}
		lowered, err := l.lowerFunction(fn)
		if err != nil {
			return nil, err
		}
		l.module.Functions = append(l.module.Functions, lowered)
	}
	for _, decl := range l.program.Decls {
		impl, ok := decl.(*ast.ImplDecl)
		if !ok {
			continue
		}
		for _, method := range impl.Methods {
			if method.ExternABI != "" || len(method.TypeParams) > 0 {
				continue
			}
			name := implMethodName(impl.TypeName, method.Name)
			lowered, err := l.lowerFunctionNamed(method, name)
			if err != nil {
				return nil, err
			}
			l.module.Functions = append(l.module.Functions, lowered)
		}
	}
	return l.module, nil
}

// collectDecls records signatures and struct layouts.
func (l *lowerer) collectDecls() {
	for _, decl := range l.program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			l.module.Structs[d.Name] = lowerStruct(d)
		case *ast.EnumDecl:
			l.module.Enums[d.Name] = lowerEnum(d)
		case *ast.UnionDecl:
			l.module.Unions[d.Name] = lowerUnion(d)
		}
	}
	for _, decl := range l.program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			l.signatures[d.Name] = l.lowerSignature(d)
		case *ast.ImplDecl:
			for _, method := range d.Methods {
				l.signatures[implMethodName(d.TypeName, method.Name)] = l.lowerSignature(method)
			}
		}
	}
}

// lowerUnion converts an AST union declaration to IR metadata.
func lowerUnion(decl *ast.UnionDecl) Union {
	variants := map[string]UnionVariant{}
	for index, variant := range decl.Variants {
		variants[variant.Name] = UnionVariant{
			Name: variant.Name, Index: index, Payload: variant.Payload,
		}
	}
	return Union{Name: decl.Name, Variants: variants}
}

// lowerEnum converts an AST enum declaration to IR metadata.
func lowerEnum(decl *ast.EnumDecl) Enum {
	tags := map[string]int{}
	for index, tag := range decl.Tags {
		tags[tag] = index
	}
	return Enum{Name: decl.Name, Tags: tags}
}

// lowerStruct converts an AST struct declaration to IR metadata.
func lowerStruct(decl *ast.StructDecl) Struct {
	fields := make([]Field, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		fields = append(fields, Field{Name: field.Name, Type: field.TypeName})
	}
	return Struct{Name: decl.Name, Fields: fields}
}

// lowerSignature extracts the callable type of a function declaration.
func (l *lowerer) lowerSignature(fn *ast.FunctionDecl) Signature {
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, l.paramIRTypeName(param))
	}
	return Signature{Params: params, Return: returnType(fn.ReturnType)}
}

// lowerFunction lowers one function into SSA blocks.
func (l *lowerer) lowerFunction(fn *ast.FunctionDecl) (*Function, error) {
	return l.lowerFunctionNamed(fn, fn.Name)
}

// lowerFunctionNamed lowers one function using an explicit IR symbol name.
func (l *lowerer) lowerFunctionNamed(fn *ast.FunctionDecl, name string) (*Function, error) {
	l.current = &Function{Name: name, Return: returnType(fn.ReturnType)}
	l.env = map[string]Value{}
	l.nextValue = 0
	l.nextBlock = 0
	l.loops = nil
	l.deferFrames = nil
	for _, param := range fn.Params {
		value := Value{Name: "%" + param.Name, Type: l.paramIRTypeName(param)}
		l.current.Params = append(l.current.Params, value)
		l.env[param.Name] = value
	}
	l.block = l.newBlock("entry")
	if err := l.lowerBlock(fn.Body); err != nil {
		return nil, err
	}
	if l.block.Terminator.Op == "" {
		l.block.Terminator = Terminator{Op: "return", Value: Value{Name: "void", Type: "void"}}
	}
	return l.current, nil
}

// paramIRTypeName preserves borrow ABI only for unions that need pointer matching.
func (l *lowerer) paramIRTypeName(param ast.Param) string {
	if !param.Borrow && !param.MutBorrow {
		return param.TypeName
	}
	if _, ok := l.module.Unions[param.TypeName]; !ok {
		return param.TypeName
	}
	if param.MutBorrow {
		return "&var " + param.TypeName
	}
	return "&" + param.TypeName
}

// lowerBlock lowers statements into the current block.
func (l *lowerer) lowerBlock(block *ast.BlockStmt) error {
	frame := l.pushDeferFrame()
	for _, stmt := range block.Statements {
		if l.block.Terminator.Op != "" {
			l.popDeferFrame()
			return nil
		}
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if err := l.lowerDeferStmt(deferStmt); err != nil {
				l.popDeferFrame()
				return err
			}
			continue
		}
		if errDeferStmt, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := l.lowerErrDeferStmt(errDeferStmt); err != nil {
				l.popDeferFrame()
				return err
			}
			continue
		}
		if err := l.lowerStmt(stmt); err != nil {
			l.popDeferFrame()
			return err
		}
	}
	if l.block.Terminator.Op == "" {
		l.emitCleanupFrame(frame)
	}
	l.popDeferFrame()
	return nil
}

// lowerStmt lowers one statement.
func (l *lowerer) lowerStmt(stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		value, err := l.lowerExpr(s.Value)
		l.env[s.Name] = value
		return err
	case *ast.AssignStmt:
		value, err := l.lowerExpr(s.Value)
		if err != nil {
			return err
		}
		return l.lowerAssignTarget(s.Target, value)
	case *ast.ReturnStmt:
		return l.lowerReturnStmt(s)
	case *ast.DeferStmt, *ast.ErrDeferStmt:
		return fmt.Errorf("ir error: defer statement must appear directly in a block")
	case *ast.ExprStmt:
		_, err := l.lowerExpr(s.Expr)
		return err
	case *ast.IfStmt:
		return l.lowerIfStmt(s)
	case *ast.WhileStmt:
		return l.lowerWhileStmt(s)
	case *ast.ForStmt:
		return l.lowerForStmt(s)
	case *ast.BreakStmt:
		return l.lowerLoopBranch("break", s.Label)
	case *ast.ContinueStmt:
		return l.lowerLoopBranch("continue", s.Label)
	case *ast.MatchStmt:
		return l.lowerMatchStmt(s)
	case *ast.UnsafeStmt:
		return l.lowerBlock(s.Body)
	case *ast.ComptimeIfStmt:
		return l.lowerComptimeIfStmt(s)
	default:
		return fmt.Errorf("ir error: unsupported statement %T", stmt)
	}
}

// lowerAssignTarget writes value into an assignment target. An identifier
// rebinds the SSA name. A field of a borrowed receiver stores through the
// borrow, while a field of a value receiver rebuilds the receiver aggregate and
// assigns that back through the same rule, so `a.b.c = v` reaches whichever of
// `a` and `a.b` owns memory. An explicit dereference stores through the
// borrowed pointer. Any other target is rejected: dropping the store would
// silently compile the assignment away.
func (l *lowerer) lowerAssignTarget(target ast.Expression, value Value) error {
	switch t := target.(type) {
	case *ast.IdentExpr:
		l.env[t.Name] = value
		return nil
	case *ast.FieldExpr:
		receiver, err := l.lowerExpr(t.Receiver)
		if err != nil {
			return err
		}
		if isReferenceType(receiver.Type) {
			l.emit("field.ref.set."+t.Name, "void", []Value{receiver, value}, "")
			return nil
		}
		updated := l.emit("field.set."+t.Name, receiver.Type, []Value{receiver, value}, "")
		return l.lowerAssignTarget(t.Receiver, updated)
	case *ast.DerefExpr:
		receiver, err := l.lowerExpr(t.Receiver)
		if err != nil {
			return err
		}
		if !isReferenceType(receiver.Type) {
			return fmt.Errorf("ir error: dereference assignment target `%s` is not a borrow",
				target.String())
		}
		l.emit("ref.store", "void", []Value{receiver, value}, "")
		return nil
	default:
		return fmt.Errorf("ir error: unsupported assignment target `%s`", target.String())
	}
}

// lowerReturnStmt lowers explicit returns and wraps !T success values.
func (l *lowerer) lowerReturnStmt(stmt *ast.ReturnStmt) error {
	if stmt.Value == nil {
		l.emitNormalCleanups()
		l.block.Terminator = Terminator{Op: "return", Value: l.returnVoidValue()}
		return nil
	}
	value, err := l.lowerExpr(stmt.Value)
	if err != nil {
		return err
	}
	errorReturn := l.producesErrorValue(value)
	if errorName, success, ok := errorUnionParts(l.current.Return); ok {
		if value.Type == success {
			value = l.emit("error.ok", l.current.Return, []Value{value}, "")
		} else if errorName != "" && value.Type == errorName {
			value = l.emit("error.error", l.current.Return, []Value{value}, "")
			errorReturn = true
		}
	}
	if errorReturn {
		l.emitErrorCleanups()
	} else {
		l.emitNormalCleanups()
	}
	l.block.Terminator = Terminator{Op: "return", Value: value}
	return nil
}

// producesErrorValue reports whether v was defined by an error.error
// instruction in the current block, such as the `error(...)` builtin. Such a
// return exits through the error path and must run errdefer cleanups.
func (l *lowerer) producesErrorValue(v Value) bool {
	for idx := len(l.block.Instrs) - 1; idx >= 0; idx-- {
		if l.block.Instrs[idx].Result.Name == v.Name {
			return l.block.Instrs[idx].Op == "error.error"
		}
	}
	return false
}

// returnVoidValue returns the correct SSA return value for void-like returns.
func (l *lowerer) returnVoidValue() Value {
	if success, ok := errorUnionSuccessType(l.current.Return); ok && success == "void" {
		return l.emit("error.ok", l.current.Return, nil, "")
	}
	return Value{Name: "void", Type: "void"}
}

// lowerExpr lowers an expression and returns its typed SSA value.
func (l *lowerer) lowerExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return l.lowerLiteralExpr(e)
	case *ast.TypeExpr:
		return l.emitConst("type", e.TypeName), nil
	case *ast.ComptimeExpr:
		return l.lowerExpr(e.Expr)
	case *ast.IdentExpr:
		return l.lowerIdentExpr(e)
	case *ast.PrefixExpr:
		return l.lowerPrefixExpr(e)
	case *ast.BinaryExpr:
		return l.lowerBinaryExpr(e)
	case *ast.CallExpr:
		return l.lowerCallExpr(e)
	case *ast.CastExpr:
		return l.lowerCastExpr(e)
	case *ast.TryExpr:
		return l.lowerTryExpr(e)
	case *ast.MatchStmt:
		return l.lowerMatchExpr(e)
	case *ast.StructLiteralExpr:
		return l.lowerStructLiteralExpr(e)
	case *ast.FieldExpr, *ast.IndexExpr, *ast.DerefExpr:
		return l.lowerAccessExpr(e)
	case *ast.ArenaNewExpr:
		allocator, err := l.lowerExpr(e.Allocator)
		if err != nil {
			return Value{}, err
		}
		return l.emit("arena.new", "std::arena::Arena<"+e.TypeName+">",
			[]Value{allocator}, e.TypeName), nil
	default:
		return Value{}, fmt.Errorf("ir error: unsupported expression `%s`", expr.String())
	}
}

// lowerAccessExpr lowers field, index, and explicit dereference expressions.
func (l *lowerer) lowerAccessExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.FieldExpr:
		return l.lowerFieldExpr(e)
	case *ast.IndexExpr:
		return l.lowerIndexExpr(e)
	case *ast.DerefExpr:
		return l.lowerExpr(e.Receiver)
	default:
		return Value{}, fmt.Errorf("ir error: unsupported access `%s`", expr.String())
	}
}

// lowerLiteralExpr lowers scalar literals.
func (l *lowerer) lowerLiteralExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return l.emitConst("i64", e.Value), nil
	case *ast.StringExpr:
		return l.emitConst("[]u8", fmt.Sprintf("%q", e.Value)), nil
	case *ast.BoolExpr:
		return l.emitConst("bool", e.String()), nil
	default:
		return Value{}, fmt.Errorf("ir error: unsupported literal %T", expr)
	}
}

// lowerIdentExpr lowers a local binding or the built-in void value.
func (l *lowerer) lowerIdentExpr(expr *ast.IdentExpr) (Value, error) {
	value, ok := l.env[expr.Name]
	if ok {
		return value, nil
	}
	if expr.Name == "void" {
		return Value{Name: "void", Type: "void"}, nil
	}
	return Value{}, fmt.Errorf("ir error: undefined value `%s`", expr.Name)
}

// lowerCastExpr lowers an explicit cast as a typed conversion instruction.
func (l *lowerer) lowerCastExpr(expr *ast.CastExpr) (Value, error) {
	value, err := l.lowerExpr(expr.Value)
	if err != nil {
		return Value{}, err
	}
	return l.emit("cast", expr.TargetType, []Value{value}, expr.TargetType), nil
}

// lowerPrefixExpr lowers unary operators.
func (l *lowerer) lowerPrefixExpr(expr *ast.PrefixExpr) (Value, error) {
	if expr.Operator == "&" || expr.Operator == "&var" {
		return l.lowerBorrowExpr(expr.Operator, expr.Right)
	}
	right, err := l.lowerExpr(expr.Right)
	if err != nil {
		return Value{}, err
	}
	resultType := right.Type
	return l.emit("unary."+expr.Operator, resultType, []Value{right}, ""), nil
}

// lowerBorrowExpr preserves the current value-level ABI for checked borrow arguments.
func (l *lowerer) lowerBorrowExpr(_ string, expr ast.Expression) (Value, error) {
	return l.lowerExpr(expr)
}

// lowerBinaryExpr lowers binary operators.
func (l *lowerer) lowerBinaryExpr(expr *ast.BinaryExpr) (Value, error) {
	if expr.Operator == "and" || expr.Operator == "or" {
		return l.lowerLogicalExpr(expr)
	}
	left, err := l.lowerExpr(expr.Left)
	if err != nil {
		return Value{}, err
	}
	right, err := l.lowerExpr(expr.Right)
	if err != nil {
		return Value{}, err
	}
	resultType := binaryResultType(expr.Operator, left.Type)
	return l.emit("binary."+expr.Operator, resultType, []Value{left, right}, ""), nil
}

// lowerCallExpr lowers builtin, user, and method calls.
func (l *lowerer) lowerCallExpr(expr *ast.CallExpr) (Value, error) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok && field.Namespace {
		if value, handled, err := l.lowerUnionConstructor(field, expr.Args); handled || err != nil {
			return value, err
		}
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		if typeApply.Callee.String() == "std::testing::expect_equal" {
			args, err := l.lowerArgs(expr.Args)
			if err != nil {
				return Value{}, err
			}
			return l.emit("test.expect_equal", "void", args, typeApply.TypeArg), nil
		}
		if typeApply.Callee.String() == "std::arena::Arena" {
			return l.lowerArenaConstructor(typeApply.TypeArg, expr.Args)
		}
		if typeApply.Callee.String() == "std::array::Array" {
			return l.lowerArrayConstructor(typeApply.TypeArg, expr.Args)
		}
		if typeApply.Callee.String() == "std::map::Map" {
			return l.lowerMapConstructor(typeApply.TypeArg, expr.Args)
		}
		if name, ok := l.functionCalleeName(typeApply.Callee); ok {
			return l.lowerTypedNamedCallExpr(name, typeApply.TypeArg, expr.Args)
		}
	}
	if name, ok := l.functionCalleeName(expr.Callee); ok {
		return l.lowerNamedCallExpr(name, expr.Args)
	}
	if field, ok := expr.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		return l.lowerMethodCallExpr(field, expr.Args)
	}
	return Value{}, fmt.Errorf("ir error: unsupported callee `%s`", expr.Callee.String())
}

// functionCalleeName resolves direct and namespace-qualified function names.
func (l *lowerer) functionCalleeName(callee ast.Expression) (string, bool) {
	ident, ok := callee.(*ast.IdentExpr)
	if ok {
		return ident.Name, true
	}
	field, ok := callee.(*ast.FieldExpr)
	if !ok || !field.Namespace {
		return "", false
	}
	qualified := strings.ReplaceAll(field.String(), "::", ".")
	if _, ok := l.signatures[qualified]; ok {
		return qualified, true
	}
	if _, ok := stdprim.SimpleCoreSignatures[qualified]; ok {
		return qualified, true
	}
	if _, ok := runtimeBuiltinReturnType(qualified); ok {
		return qualified, true
	}
	return "", false
}

// lowerNamedCallExpr lowers builtins and resolved function calls.
func (l *lowerer) lowerNamedCallExpr(name string, rawArgs []ast.Expression) (Value, error) {
	args, err := l.lowerArgs(rawArgs)
	if err != nil {
		return Value{}, err
	}
	if name == "error" {
		return l.emit("error.error", l.current.Return, args, ""), nil
	}
	if name == "std.builtin.mem_len" {
		if len(args) != 1 {
			return Value{}, fmt.Errorf("ir error: std::builtin::mem_len expects 1 arg")
		}
		return l.emit("slice.len", "i64", args, ""), nil
	}
	if name == "std.builtin.test_fail" {
		return l.emit("test.fail", "void", args, ""), nil
	}
	ret := "void"
	if sig, ok := l.signatures[name]; ok {
		ret = sig.Return
	} else if sig, ok := stdprim.SimpleCoreSignatures[name]; ok {
		ret = sig.Return
	} else if builtinReturn, ok := runtimeBuiltinReturnType(name); ok {
		ret = builtinReturn
	}
	return l.emit("call."+name, ret, args, ""), nil
}

// lowerTypedNamedCallExpr lowers the small typed-call subset with backend support.
func (l *lowerer) lowerTypedNamedCallExpr(
	name string,
	typeArg string,
	rawArgs []ast.Expression,
) (Value, error) {
	args, err := l.lowerArgs(rawArgs)
	if err != nil {
		return Value{}, err
	}
	switch name {
	case "std.testing.expect_equal", "std.builtin.test_fail_equal":
		if len(args) != 2 {
			return Value{}, fmt.Errorf("ir error: std::testing::expect_equal expects 2 args")
		}
		if args[0].Type != args[1].Type {
			return Value{}, fmt.Errorf(
				"ir error: std::testing::expect_equal expects matching arg types, got %s and %s",
				args[0].Type,
				args[1].Type,
			)
		}
		return l.emit("test.expect_equal", "void", args, typeArg), nil
	default:
		return Value{}, fmt.Errorf("ir error: typed call `%s<%s>` is not supported", name, typeArg)
	}
}

// lowerArenaConstructor lowers std::arena::Arena<T>(allocator).
func (l *lowerer) lowerArenaConstructor(typeArg string, args []ast.Expression) (Value, error) {
	if len(args) != 1 {
		return Value{}, fmt.Errorf(
			"ir error: std::arena::Arena<%s> expects exactly one allocator argument",
			typeArg,
		)
	}
	allocator, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	return l.emit("arena.new", "std::arena::Arena<"+typeArg+">", []Value{allocator}, typeArg), nil
}

// lowerArrayConstructor lowers std::array::Array<T>(allocator).
func (l *lowerer) lowerArrayConstructor(typeArg string, args []ast.Expression) (Value, error) {
	if len(args) != 1 {
		return Value{}, fmt.Errorf(
			"ir error: std::array::Array<%s> expects exactly one allocator argument",
			typeArg,
		)
	}
	allocator, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	return l.emit("array.new", "std::array::Array<"+typeArg+">", []Value{allocator}, typeArg), nil
}

// lowerMapConstructor lowers std::map::Map<[]u8, V>(allocator).
func (l *lowerer) lowerMapConstructor(typeArg string, args []ast.Expression) (Value, error) {
	mapType, valueType, ok := mapTypeName(typeArg)
	if !ok {
		return Value{}, fmt.Errorf("ir error: std::map::Map only supports []u8 keys")
	}
	if len(args) != 1 {
		return Value{}, fmt.Errorf("ir error: %s expects exactly one allocator argument", mapType)
	}
	allocator, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	return l.emit("map.new", mapType, []Value{allocator}, valueType), nil
}

// lowerTryExpr lowers error-union propagation as an explicit IR instruction.
func (l *lowerer) lowerTryExpr(expr *ast.TryExpr) (Value, error) {
	value, err := l.lowerExpr(expr.Value)
	if err != nil {
		return Value{}, err
	}
	result := l.emit("error.try", errorUnionElementType(value.Type), []Value{value}, "")
	l.block.Instrs[len(l.block.Instrs)-1].Cleanups = l.errorCleanups()
	return result, nil
}

// lowerMethodCallExpr lowers arena method calls.
func (l *lowerer) lowerMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
) (Value, error) {
	receiver, err := l.lowerExpr(field.Receiver)
	if err != nil {
		return Value{}, err
	}
	loweredArgs, err := l.lowerArgs(args)
	if err != nil {
		return Value{}, err
	}
	allArgs := append([]Value{receiver}, loweredArgs...)
	if elem, ok := arrayElementType(receiver.Type); ok {
		return l.lowerArrayMethod(field.Name, elem, allArgs)
	}
	if valueType, ok := mapValueType(receiver.Type); ok {
		return l.lowerMapMethod(field.Name, valueType, allArgs)
	}
	if methodName, ok := l.implMethodCalleeName(receiver.Type, field.Name); ok {
		return l.lowerImplMethodCall(methodName, allArgs)
	}
	switch field.Name {
	case "add":
		return l.emit("arena.add", handleType(receiver.Type), allArgs, ""), nil
	case "get":
		return l.emit("arena.get", arenaElementType(receiver.Type), allArgs, ""), nil
	case "deinit":
		return l.emit("arena.deinit", "void", allArgs, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown method `%s`", field.Name)
	}
}

// implMethodCalleeName resolves a checked receiver method to its lowered symbol.
func (l *lowerer) implMethodCalleeName(receiver string, method string) (string, bool) {
	name := implMethodName(derefType(receiver), method)
	if _, ok := l.signatures[name]; ok {
		return name, true
	}
	return "", false
}

// lowerImplMethodCall lowers receiver.method(args) as Type.method(receiver, args).
func (l *lowerer) lowerImplMethodCall(name string, args []Value) (Value, error) {
	sig, ok := l.signatures[name]
	if !ok {
		return Value{}, fmt.Errorf("ir error: unknown impl method `%s`", name)
	}
	return l.emit("call."+name, sig.Return, args, ""), nil
}

// lowerMapMethod lowers runtime-backed std::map::Map<[]u8, V> methods.
func (l *lowerer) lowerMapMethod(name string, valueType string, args []Value) (Value, error) {
	switch name {
	case "insert":
		return l.emit("map.insert", "!void", args, valueType), nil
	case "get":
		return l.emit("map.get", "!"+valueType, args, valueType), nil
	case "contains":
		return l.emit("map.contains", "bool", args, valueType), nil
	case "len":
		return l.emit("map.len", "i64", args, valueType), nil
	case "deinit":
		return l.emit("map.deinit", "void", args, valueType), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown map method `%s`", name)
	}
}

// lowerArrayMethod lowers runtime-backed std::array::Array<T> methods.
func (l *lowerer) lowerArrayMethod(name string, elem string, args []Value) (Value, error) {
	switch name {
	case "append":
		return l.emit("array.append", "!void", args, elem), nil
	case "len":
		return l.emit("array.len", "i64", args, elem), nil
	case "capacity":
		return l.emit("array.capacity", "i64", args, elem), nil
	case "reserve":
		return l.emit("array.reserve", "!void", args, elem), nil
	case "pop":
		return l.emit("array.pop", "!"+elem, args, elem), nil
	case "pop_or_panic":
		return l.emit("array.pop_or_panic", elem, args, elem), nil
	case "get":
		return l.emit("array.get", "!"+elem, args, elem), nil
	case "get_or_panic":
		return l.emit("array.get_or_panic", elem, args, elem), nil
	case "at":
		return l.emit("array.at", "!&"+elem, args, elem), nil
	case "at_mut":
		return l.emit("array.at_mut", "!&var "+elem, args, elem), nil
	case "set":
		return l.emit("array.set", "!void", args, elem), nil
	case "truncate":
		return l.emit("array.truncate", "!void", args, elem), nil
	case "clear":
		return l.emit("array.clear", "void", args, elem), nil
	case "as_bytes":
		return l.emit("array.as_bytes", "[]u8", args, elem), nil
	case "deinit":
		return l.emit("array.deinit", "void", args, elem), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown array method `%s`", name)
	}
}

// lowerStructLiteralExpr lowers struct construction.
func (l *lowerer) lowerStructLiteralExpr(expr *ast.StructLiteralExpr) (Value, error) {
	fields := make([]FieldArg, 0, len(expr.Fields))
	for _, field := range expr.Fields {
		value, err := l.lowerExpr(field.Value)
		if err != nil {
			return Value{}, err
		}
		fields = append(fields, FieldArg{Name: field.Name, Value: value})
	}
	instr := &Instr{Result: l.next(expr.TypeName), Op: "struct.new", Fields: fields}
	l.block.Instrs = append(l.block.Instrs, instr)
	return instr.Result, nil
}

// lowerFieldExpr lowers struct field reads.
func (l *lowerer) lowerFieldExpr(expr *ast.FieldExpr) (Value, error) {
	if expr.Namespace {
		if value, ok := l.lowerEnumTagExpr(expr); ok {
			return value, nil
		}
		if value, ok := l.lowerPayloadFreeUnionExpr(expr); ok {
			return value, nil
		}
	}
	receiver, err := l.lowerExpr(expr.Receiver)
	if err != nil {
		return Value{}, err
	}
	fieldType := l.fieldType(derefType(receiver.Type), expr.Name)
	if isReferenceType(receiver.Type) {
		return l.emit("field.ref."+expr.Name, fieldType, []Value{receiver}, ""), nil
	}
	return l.emit("field."+expr.Name, fieldType, []Value{receiver}, ""), nil
}

// lowerUnionConstructor lowers Union::Variant(payload) expressions.
func (l *lowerer) lowerUnionConstructor(
	field *ast.FieldExpr,
	rawArgs []ast.Expression,
) (Value, bool, error) {
	unionType, variant, ok := l.unionVariant(field)
	if !ok {
		return Value{}, false, nil
	}
	if variant.Payload == "" {
		if len(rawArgs) != 0 {
			return Value{}, true, fmt.Errorf("ir error: union variant `%s::%s` expects no payload",
				unionType.Name, variant.Name)
		}
		return l.emit("union.new", unionType.Name, nil, variant.Name), true, nil
	}
	if len(rawArgs) != 1 {
		return Value{}, true, fmt.Errorf("ir error: union variant `%s::%s` expects one payload",
			unionType.Name, variant.Name)
	}
	payload, err := l.lowerExpr(rawArgs[0])
	if err != nil {
		return Value{}, true, err
	}
	return l.emit("union.new", unionType.Name, []Value{payload}, variant.Name), true, nil
}

// lowerPayloadFreeUnionExpr lowers Union::Variant without a payload.
func (l *lowerer) lowerPayloadFreeUnionExpr(expr *ast.FieldExpr) (Value, bool) {
	unionType, variant, ok := l.unionVariant(expr)
	if !ok || variant.Payload != "" {
		return Value{}, false
	}
	return l.emit("union.new", unionType.Name, nil, variant.Name), true
}

// unionVariant resolves a namespace field as a known union variant.
func (l *lowerer) unionVariant(expr *ast.FieldExpr) (Union, UnionVariant, bool) {
	unionName := expr.Receiver.String()
	unionType, ok := l.module.Unions[unionName]
	if !ok {
		return Union{}, UnionVariant{}, false
	}
	variant, ok := unionType.Variants[expr.Name]
	return unionType, variant, ok
}

// lowerEnumTagExpr lowers Enum::Tag namespace expressions to integer tags.
func (l *lowerer) lowerEnumTagExpr(expr *ast.FieldExpr) (Value, bool) {
	enumName := expr.Receiver.String()
	enumType, ok := l.module.Enums[enumName]
	if !ok {
		return Value{}, false
	}
	index, ok := enumType.Tags[expr.Name]
	if !ok {
		return Value{}, false
	}
	return l.emitConst(enumName, fmt.Sprintf("%d", index)), true
}

// lowerIndexExpr lowers checked byte-slice indexing and slicing.
func (l *lowerer) lowerIndexExpr(expr *ast.IndexExpr) (Value, error) {
	target, err := l.lowerExpr(expr.Target)
	if err != nil {
		return Value{}, err
	}
	if !expr.Slice {
		index, err := l.lowerExpr(expr.Index)
		if err != nil {
			return Value{}, err
		}
		return l.emit("slice.index", "u8", []Value{target, index}, ""), nil
	}
	start, err := l.lowerSliceBound(expr.Start, Value{Name: "0", Type: "i64"})
	if err != nil {
		return Value{}, err
	}
	end, err := l.lowerSliceEnd(expr.End, target)
	if err != nil {
		return Value{}, err
	}
	return l.emit("slice.slice", "[]u8", []Value{target, start, end}, ""), nil
}

// lowerSliceBound lowers an optional slice bound or returns the supplied default.
func (l *lowerer) lowerSliceBound(expr ast.Expression, fallback Value) (Value, error) {
	if expr == nil {
		return fallback, nil
	}
	return l.lowerExpr(expr)
}

// lowerSliceEnd lowers an optional end bound, defaulting to the target length.
func (l *lowerer) lowerSliceEnd(expr ast.Expression, target Value) (Value, error) {
	if expr != nil {
		return l.lowerExpr(expr)
	}
	return l.emit("slice.len", "i64", []Value{target}, ""), nil
}

// lowerArgs lowers call arguments from left to right.
func (l *lowerer) lowerArgs(args []ast.Expression) ([]Value, error) {
	values := make([]Value, 0, len(args))
	for _, arg := range args {
		value, err := l.lowerExpr(arg)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}
