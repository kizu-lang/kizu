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
	program    *ast.Program
	module     *Module
	signatures map[string]Signature
	enums      map[string]map[string]int
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
		module:     &Module{Structs: map[string]Struct{}, Enums: map[string]Enum{}},
		signatures: map[string]Signature{},
		enums:      map[string]map[string]int{},
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
		switch d := decl.(type) {
		case *ast.StructDecl:
			l.module.Structs[d.Name] = lowerStruct(d)
		case *ast.EnumDecl:
			l.module.Enums[d.Name] = lowerEnum(d)
			l.enums[d.Name] = enumTags(d)
		case *ast.FunctionDecl:
			l.signatures[d.Name] = lowerSignature(d)
		}
	}
}

// enumTags converts one enum declaration to tag ordinals.
func enumTags(decl *ast.EnumDecl) map[string]int {
	tags := map[string]int{}
	for idx, tag := range decl.Tags {
		tags[tag] = idx
	}
	return tags
}

// lowerEnum converts an AST enum declaration to IR metadata.
func lowerEnum(decl *ast.EnumDecl) Enum {
	return Enum{Name: decl.Name, Tags: append([]string(nil), decl.Tags...)}
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
func lowerSignature(fn *ast.FunctionDecl) Signature {
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, param.TypeName)
	}
	return Signature{Params: params, Return: returnType(fn.ReturnType)}
}

// lowerFunction lowers one function into SSA blocks.
func (l *lowerer) lowerFunction(fn *ast.FunctionDecl) (*Function, error) {
	l.current = &Function{Name: fn.Name, Return: returnType(fn.ReturnType)}
	l.env = map[string]Value{}
	l.nextValue = 0
	l.nextBlock = 0
	l.loops = nil
	for _, param := range fn.Params {
		value := Value{Name: "%" + param.Name, Type: param.TypeName}
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
		value, err := l.lowerExpr(s.Value)
		if ident, ok := s.Target.(*ast.IdentExpr); ok {
			l.env[ident.Name] = value
		}
		return err
	case *ast.ReturnStmt:
		if s.Value == nil {
			l.block.Terminator = Terminator{Op: "return", Value: Value{Name: "void", Type: "void"}}
			return nil
		}
		value, err := l.lowerExpr(s.Value)
		l.block.Terminator = Terminator{Op: "return", Value: value}
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

// lowerExpr lowers an expression and returns its typed SSA value.
func (l *lowerer) lowerExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return l.lowerLiteralExpr(e)
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
	case *ast.IndexExpr:
		return l.lowerIndexExpr(e)
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

// lowerIndexExpr lowers byte indexing and keeps slices as the original pointer.
func (l *lowerer) lowerIndexExpr(expr *ast.IndexExpr) (Value, error) {
	target, err := l.lowerExpr(expr.Target)
	if err != nil {
		return Value{}, err
	}
	if expr.Slice {
		return target, nil
	}
	index, err := l.lowerExpr(expr.Index)
	if err != nil {
		return Value{}, err
	}
	return l.emit("index.byte", "u8", []Value{target, index}, ""), nil
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
	right, err := l.lowerExpr(expr.Right)
	if err != nil {
		return Value{}, err
	}
	resultType := right.Type
	return l.emit("unary."+expr.Operator, resultType, []Value{right}, ""), nil
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
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		if field.Namespace {
			if value, ok, err := l.lowerQualifiedCallExpr(field, expr.Args); ok || err != nil {
				return value, err
			}
		}
		return l.lowerMethodCallExpr(field, expr.Args)
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return l.lowerTypeApplyCallExpr(typeApply, expr.Args)
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
	}
	return l.emit("call."+name.Name, ret, args, ""), nil
}

// lowerQualifiedCallExpr lowers namespace-qualified user function calls.
func (l *lowerer) lowerQualifiedCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
) (Value, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return Value{}, false, nil
	}
	if _, ok := l.signatures[name]; !ok {
		if signature, ok := stdprim.SimpleCoreSignatures[name]; ok {
			loweredArgs, err := l.lowerArgs(args)
			if err != nil {
				return Value{}, true, err
			}
			return l.emit("call."+name, signature.Return, loweredArgs, ""), true, nil
		}
		return Value{}, false, nil
	}
	if value, ok, err := l.lowerDirectStdCall(name, args); ok || err != nil {
		return value, ok, err
	}
	if name == "std.string.String" {
		loweredArgs, err := l.lowerArgs(args)
		if err != nil {
			return Value{}, true, err
		}
		return l.emit("string.new", "std::string::String", loweredArgs, ""), true, nil
	}
	loweredArgs, err := l.lowerArgs(args)
	if err != nil {
		return Value{}, true, err
	}
	return l.emit("call."+name, l.signatures[name].Return, loweredArgs, ""), true, nil
}

// lowerDirectStdCall bypasses wrappers whose native behavior is already primitive.
func (l *lowerer) lowerDirectStdCall(
	name string,
	args []ast.Expression,
) (Value, bool, error) {
	switch name {
	case "std.mem.page_allocator":
		return l.emit("call.std.builtin.mem_page_allocator", "Allocator", nil, ""), true, nil
	case "std.mem.len":
		loweredArgs, err := l.lowerArgs(args)
		if err != nil {
			return Value{}, true, err
		}
		return l.emit("call.std.builtin.mem_len", "i64", loweredArgs, ""), true, nil
	default:
		return Value{}, false, nil
	}
}

// lowerTypeApplyCallExpr lowers generic std wrapper constructors.
func (l *lowerer) lowerTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
) (Value, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return Value{}, fmt.Errorf("ir error: unsupported type application `%s`", expr.String())
	}
	loweredArgs, err := l.lowerArgs(args)
	if err != nil {
		return Value{}, err
	}
	switch name {
	case "std.array.Array", "std.builtin.array":
		return l.emit("array.new", "std::array::Array<"+expr.TypeArg+">", loweredArgs, ""), nil
	case "std.map.Map", "std.builtin.map":
		return l.emit("map.new", "std::map::Map<"+expr.TypeArg+">", loweredArgs, ""), nil
	default:
		ret := "void"
		if sig, ok := l.signatures[name]; ok {
			ret = sig.Return
		}
		return l.emit("call."+name, ret, loweredArgs, ""), nil
	}
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
	if receiver.Type == "std::string::String" {
		return l.lowerStringMethodCall(field.Name, allArgs)
	}
	if strings.HasPrefix(receiver.Type, "std::array::Array<") {
		return l.lowerArrayMethodCall(receiver.Type, field.Name, allArgs)
	}
	switch field.Name {
	case "add":
		return l.emit("arena.add", handleType(receiver.Type), allArgs, ""), nil
	case "get":
		return l.emit("arena.get", arenaElementType(receiver.Type), allArgs, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown method `%s`", field.Name)
	}
}

// lowerStringMethodCall lowers native string builder operations.
func (l *lowerer) lowerStringMethodCall(name string, args []Value) (Value, error) {
	switch name {
	case "append_bytes", "append_byte", "reserve", "truncate":
		return l.emit("string."+name, "!void", args, ""), nil
	case "clear", "deinit":
		return l.emit("string."+name, "void", args, ""), nil
	case "as_bytes":
		return l.emit("string.as_bytes", "[]const u8", args, ""), nil
	case "len", "capacity":
		return l.emit("string."+name, "i64", args, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown String method `%s`", name)
	}
}

// lowerArrayMethodCall lowers std Array wrapper methods to trusted primitives.
func (l *lowerer) lowerArrayMethodCall(receiver string, name string, args []Value) (Value, error) {
	elem := strings.TrimSuffix(strings.TrimPrefix(receiver, "std::array::Array<"), ">")
	switch name {
	case "append", "reserve", "truncate":
		return l.emit("call.std.builtin.array_"+name, "!void", args, elem), nil
	case "clear", "deinit":
		return l.emit("call.std.builtin.array_"+name, "void", args, elem), nil
	case "len", "capacity":
		return l.emit("call.std.builtin.array_"+name, "i64", args, elem), nil
	case "as_bytes":
		return l.emit("call.std.builtin.array_as_bytes", "[]const u8", args, elem), nil
	case "get":
		return l.emit("call.std.builtin.array_get", "!"+elem, args, elem), nil
	case "at", "at_mut":
		return l.emit("call.std.builtin.array_"+name, elem, args, elem), nil
	case "set":
		return l.emit("call.std.builtin.array_set", "!void", args, elem), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown Array method `%s`", name)
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
		if value, ok := l.lowerEnumLiteral(expr); ok {
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

// lowerEnumLiteral lowers Enum::Tag namespace expressions.
func (l *lowerer) lowerEnumLiteral(expr *ast.FieldExpr) (Value, bool) {
	ident, ok := expr.Receiver.(*ast.IdentExpr)
	if !ok {
		return Value{}, false
	}
	tags, ok := l.enums[ident.Name]
	if !ok {
		return Value{}, false
	}
	ordinal, ok := tags[expr.Name]
	if !ok {
		return Value{}, false
	}
	return l.emitConst(ident.Name, fmt.Sprint(ordinal)), true
}

// qualifiedName renders a namespace chain as an internal function key.
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
