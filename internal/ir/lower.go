package ir

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Lower converts a checked Kizu AST into typed SSA IR.
func Lower(program *ast.Program) (*Module, error) {
	l := newLowerer(program)
	return l.lower()
}

type lowerer struct {
	program    *ast.Program
	module     *Module
	signatures map[string]Signature
	enumTags   map[string]map[string]int
	current    *Function
	block      *Block
	env        map[string]Value
	nextValue  int
	nextBlock  int
	loops      []loopContext
}

type loopContext struct {
	label      string
	breakTo    string
	continueTo string
}

// newLowerer prepares lookup tables used during lowering.
func newLowerer(program *ast.Program) *lowerer {
	return &lowerer{
		program:    program,
		module:     &Module{Structs: map[string]Struct{}},
		signatures: map[string]Signature{},
		enumTags:   map[string]map[string]int{},
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
		lowered, err := l.lowerFunction(fn)
		if err != nil {
			return nil, err
		}
		l.module.Functions = append(l.module.Functions, lowered)
	}
	return l.module, nil
}

// collectDecls records signatures and struct layouts.
func (l *lowerer) collectDecls() {
	for _, decl := range l.program.Decls {
		if d, ok := decl.(*ast.EnumDecl); ok {
			l.collectEnum(d)
		}
	}
	for _, decl := range l.program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			l.module.Structs[d.Name] = l.lowerStruct(d)
		case *ast.FunctionDecl:
			l.signatures[d.Name] = l.lowerSignature(d)
		}
	}
}

// lowerStruct converts an AST struct declaration to IR metadata.
func (l *lowerer) lowerStruct(decl *ast.StructDecl) Struct {
	fields := make([]Field, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		fields = append(fields, Field{Name: field.Name, Type: l.lowerType(field.TypeName)})
	}
	return Struct{Name: decl.Name, Fields: fields}
}

// collectEnum records stable integer tags for one enum declaration.
func (l *lowerer) collectEnum(decl *ast.EnumDecl) {
	tags := map[string]int{}
	for index, tag := range decl.Tags {
		tags[tag] = index
	}
	l.enumTags[decl.Name] = tags
	if idx := strings.LastIndex(decl.Name, "."); idx >= 0 && idx < len(decl.Name)-1 {
		l.enumTags[decl.Name[idx+1:]] = tags
	}
}

// lowerSignature extracts the callable type of a function declaration.
func (l *lowerer) lowerSignature(fn *ast.FunctionDecl) Signature {
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, l.lowerType(param.TypeName))
	}
	return Signature{Params: params, Return: l.lowerReturnType(fn.ReturnType)}
}

// lowerFunction lowers one function into SSA blocks.
func (l *lowerer) lowerFunction(fn *ast.FunctionDecl) (*Function, error) {
	l.current = &Function{Name: fn.Name, Return: l.lowerReturnType(fn.ReturnType)}
	l.env = map[string]Value{}
	l.nextValue = 0
	l.nextBlock = 0
	l.loops = nil
	for _, param := range fn.Params {
		value := Value{Name: "%" + param.Name, Type: l.lowerType(param.TypeName)}
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

// lowerBlock lowers statements into the current block.
func (l *lowerer) lowerBlock(block *ast.BlockStmt) error {
	for _, stmt := range block.Statements {
		if l.block.Terminator.Op != "" {
			return nil
		}
		if err := l.lowerStmt(stmt); err != nil {
			return err
		}
	}
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
		return l.lowerAssignStmt(s)
	case *ast.ReturnStmt:
		if s.Value == nil {
			l.block.Terminator = Terminator{Op: "return", Value: Value{Name: "void", Type: "void"}}
			return nil
		}
		value, err := l.lowerExpr(s.Value)
		l.block.Terminator = Terminator{Op: "return", Value: l.returnValue(value)}
		return err
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
	case *ast.UnsafeStmt:
		return l.lowerBlock(s.Body)
	case *ast.ComptimeIfStmt:
		return l.lowerComptimeIfStmt(s)
	default:
		return fmt.Errorf("ir error: unsupported statement %T", stmt)
	}
}

// lowerAssignStmt lowers variable and field assignment targets.
func (l *lowerer) lowerAssignStmt(stmt *ast.AssignStmt) error {
	value, err := l.lowerExpr(stmt.Value)
	if err != nil {
		return err
	}
	if ident, ok := stmt.Target.(*ast.IdentExpr); ok {
		l.env[ident.Name] = value
		return nil
	}
	if field, ok := stmt.Target.(*ast.FieldExpr); ok {
		return l.lowerFieldAssignStmt(field, value)
	}
	return nil
}

// returnValue wraps a successful !void return in an opaque error-union value.
func (l *lowerer) returnValue(value Value) Value {
	if strings.HasPrefix(l.current.Return, "!") && value.Type == "void" {
		return l.emit("error.ok", l.current.Return, nil, "")
	}
	return value
}

// lowerExpr lowers an expression and returns its typed SSA value.
func (l *lowerer) lowerExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return l.lowerLiteralExpr(e)
	case *ast.IfExpr:
		return l.lowerIfExpr(e)
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
	case *ast.StructLiteralExpr:
		return l.lowerStructLiteralExpr(e)
	case *ast.FieldExpr:
		return l.lowerFieldExpr(e)
	case *ast.DerefExpr:
		return l.lowerExpr(e.Receiver)
	case *ast.ArenaNewExpr:
		return l.emit("arena.new", "arena<"+e.TypeName+">", nil, e.TypeName), nil
	default:
		return Value{}, fmt.Errorf("ir error: unsupported expression %T", expr)
	}
}

// lowerLiteralExpr lowers scalar literals.
func (l *lowerer) lowerLiteralExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return l.emitConst("i64", e.Value), nil
	case *ast.StringExpr:
		return l.emitConst("[]const u8", fmt.Sprintf("%q", e.Value)), nil
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
	if typ, ok := namespaceConstType(expr.Name); ok {
		if value, found := l.lowerEnumIdent(expr.Name, typ); found {
			return value, nil
		}
		return l.emitConst(typ, expr.Name), nil
	}
	return Value{}, fmt.Errorf("ir error: undefined value `%s`", expr.Name)
}

// lowerEnumIdent lowers a flattened Enum.Tag identifier to a stable tag value.
func (l *lowerer) lowerEnumIdent(name string, typ string) (Value, bool) {
	tags, ok := l.enumTags[strings.ReplaceAll(typ, "::", ".")]
	if !ok {
		return Value{}, false
	}
	tag := name[strings.LastIndex(name, ".")+1:]
	index, ok := tags[tag]
	if !ok {
		return Value{}, false
	}
	return l.emitConst("i64", strconv.Itoa(index)), true
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
	right, err := l.lowerExpr(expr.Right)
	if err != nil {
		return Value{}, err
	}
	resultType := right.Type
	return l.emit("unary."+expr.Operator, resultType, []Value{right}, ""), nil
}

// lowerBinaryExpr lowers binary operators.
func (l *lowerer) lowerBinaryExpr(expr *ast.BinaryExpr) (Value, error) {
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
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return l.lowerMethodCallExpr(field, expr.Args)
	}
	if applied, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return l.lowerTypeApplyCallExpr(applied, expr.Args)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return Value{}, fmt.Errorf("ir error: callee must be a function name")
	}
	args, err := l.lowerArgs(expr.Args)
	if err != nil {
		return Value{}, err
	}
	if name.Name == "error" {
		return l.emit("error.error", l.current.Return, args, ""), nil
	}
	ret := "void"
	if sig, ok := l.signatures[name.Name]; ok {
		ret = sig.Return
	} else if builtin, ok := builtinReturnType(name.Name, args); ok {
		ret = builtin
	}
	return l.emit("call."+name.Name, ret, args, ""), nil
}

// lowerTypeApplyCallExpr lowers generic-looking std constructors.
func (l *lowerer) lowerTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
) (Value, error) {
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return Value{}, fmt.Errorf("ir error: type constructor must be a namespace name")
	}
	loweredArgs, err := l.lowerArgs(args)
	if err != nil {
		return Value{}, err
	}
	ret := fmt.Sprintf("%s<%s>", name.Name, expr.TypeArg)
	return l.emit("call."+ret, ret, loweredArgs, ""), nil
}

// lowerTryExpr lowers error-union propagation as an explicit IR instruction.
func (l *lowerer) lowerTryExpr(expr *ast.TryExpr) (Value, error) {
	value, err := l.lowerExpr(expr.Value)
	if err != nil {
		return Value{}, err
	}
	return l.emit("error.try", errorUnionElementType(value.Type), []Value{value}, ""), nil
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
	switch field.Name {
	case "add":
		return l.emit("arena.add", handleType(receiver.Type), allArgs, ""), nil
	case "get":
		return l.emit("arena.get", arenaElementType(receiver.Type), allArgs, ""), nil
	case "append":
		return l.emit("method.append", "!void", allArgs, ""), nil
	case "at":
		return l.emit("method.at", "!"+containerElementType(receiver.Type), allArgs, ""), nil
	case "len":
		return l.emit("method.len", "i64", allArgs, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown method `%s`", field.Name)
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
	instr := &Instr{Result: l.next(l.lowerType(expr.TypeName)), Op: "struct.new", Fields: fields}
	l.block.Instrs = append(l.block.Instrs, instr)
	return instr.Result, nil
}

// lowerFieldExpr lowers struct field reads.
func (l *lowerer) lowerFieldExpr(expr *ast.FieldExpr) (Value, error) {
	if expr.Namespace {
		if value, ok := l.lowerEnumTag(expr); ok {
			return value, nil
		}
	}
	receiver, err := l.lowerExpr(expr.Receiver)
	if err != nil {
		return Value{}, err
	}
	fieldType := l.fieldType(receiver.Type, expr.Name)
	return l.emit("field."+expr.Name, fieldType, []Value{receiver}, ""), nil
}

// lowerEnumTag lowers Enum::Tag namespace constants to stable i64 values.
func (l *lowerer) lowerEnumTag(expr *ast.FieldExpr) (Value, bool) {
	name := namespacePath(expr.Receiver)
	tags, ok := l.enumTags[name]
	if !ok {
		return Value{}, false
	}
	index, ok := tags[expr.Name]
	if !ok {
		return Value{}, false
	}
	return l.emitConst("i64", strconv.Itoa(index)), true
}

// namespacePath returns a flattened namespace path used after package lowering.
func namespacePath(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return strings.ReplaceAll(e.Name, "::", ".")
	case *ast.FieldExpr:
		if e.Namespace {
			return namespacePath(e.Receiver) + "." + e.Name
		}
	}
	return strings.ReplaceAll(expr.String(), "::", ".")
}

// lowerType maps checked enum names to the runtime tag representation.
func (l *lowerer) lowerType(name string) string {
	if _, ok := l.enumTags[name]; ok {
		return "i64"
	}
	return name
}

// lowerReturnType maps declared returns to IR returns.
func (l *lowerer) lowerReturnType(name string) string {
	if name == "" {
		return "void"
	}
	if strings.HasPrefix(name, "!") {
		return "!" + l.lowerType(strings.TrimPrefix(name, "!"))
	}
	return l.lowerType(name)
}

// lowerFieldAssignStmt stores a value into an already-lowered struct field.
func (l *lowerer) lowerFieldAssignStmt(expr *ast.FieldExpr, value Value) error {
	receiver, err := l.lowerExpr(expr.Receiver)
	if err != nil {
		return err
	}
	l.emit("field.store."+expr.Name, "void", []Value{receiver, value}, "")
	return nil
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
