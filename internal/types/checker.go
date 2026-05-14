package types

import (
	"fmt"
	"strings"

	"tiny-safe/internal/ast"
)

// Type is the static type name used by the v0 checker.
type Type string

const (
	typeBool       Type = "bool"
	typeI64        Type = "i64"
	typeByteString Type = "[]const u8"
	typeSelf       Type = "Self"
	typeVoid       Type = "void"
)

var knownTypes = map[Type]bool{
	typeBool:       true,
	typeI64:        true,
	typeByteString: true,
	typeVoid:       true,
	"i8":           true,
	"i16":          true,
	"i32":          true,
	"u8":           true,
	"u16":          true,
	"u32":          true,
	"u64":          true,
	"usize":        true,
	"isize":        true,
	"f32":          true,
	"f64":          true,
	"Io":           true,
	"TaskGroup":    true,
}

var numericTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"u8":    true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var signedNumericTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var integerTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	"u8":    true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
}

// Checker validates type rules for a parsed program.
type Checker struct {
	functions     map[string]*functionType
	structs       map[string]*ast.StructDecl
	enums         map[string]*enumType
	unions        map[string]*unionType
	contracts     map[string]*contractType
	impls         map[string]map[string]*functionType
	satisfactions map[string]map[string]bool
	currentReturn Type
}

type enumType struct {
	name string
	tags map[string]bool
}

type unionType struct {
	name     string
	variants map[string]string
}

type functionType struct {
	name           string
	params         []Type
	borrowParams   []bool
	comptimeParams []bool
	returnType     Type
	decl           *ast.FunctionDecl
	unsafe         bool
	externABI      string
}

type contractType struct {
	name    string
	methods map[string]*functionType
}

type scope struct {
	parent  *scope
	values  map[string]Type
	mutable map[string]bool
}

// New creates an empty type checker.
func New() *Checker {
	return &Checker{
		functions:     map[string]*functionType{},
		structs:       map[string]*ast.StructDecl{},
		enums:         map[string]*enumType{},
		unions:        map[string]*unionType{},
		contracts:     map[string]*contractType{},
		impls:         map[string]map[string]*functionType{},
		satisfactions: map[string]map[string]bool{},
	}
}

// Check validates the program and returns the first type error.
func (c *Checker) Check(program *ast.Program) error {
	if err := c.collectFunctions(program); err != nil {
		return err
	}
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			if err := c.checkFunction(c.functions[d.Name]); err != nil {
				return err
			}
		case *ast.ImplDecl:
			if err := c.checkImpl(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectFunctions registers top-level function signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	if err := c.collectTypesAndMethods(program); err != nil {
		return err
	}
	if err := c.collectTopLevelFunctions(program); err != nil {
		return err
	}
	return c.collectSatisfyDecls(program)
}

// collectTypesAndMethods registers declarations needed before function signatures.
func (c *Checker) collectTypesAndMethods(program *ast.Program) error {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			if err := c.collectStruct(d); err != nil {
				return err
			}
		case *ast.EnumDecl:
			if err := c.collectEnum(d); err != nil {
				return err
			}
		case *ast.UnionDecl:
			if err := c.collectUnion(d); err != nil {
				return err
			}
		case *ast.ContractDecl:
			if err := c.collectContract(d); err != nil {
				return err
			}
		case *ast.ImplDecl:
			if err := c.collectImpl(d); err != nil {
				return err
			}
		case *ast.SatisfyDecl:
			continue
		case *ast.FunctionDecl:
			continue
		default:
			return fmt.Errorf("type error: unsupported declaration %T", decl)
		}
	}
	return nil
}

// collectTopLevelFunctions registers top-level function signatures.
func (c *Checker) collectTopLevelFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if _, exists := c.functions[fn.Name]; exists {
			return fmt.Errorf("type error: duplicate function `%s`", fn.Name)
		}
		fnType, err := c.newFunctionType(fn)
		if err != nil {
			return err
		}
		c.functions[fn.Name] = fnType
	}
	return nil
}

// collectSatisfyDecls validates explicit satisfy declarations after impls exist.
func (c *Checker) collectSatisfyDecls(program *ast.Program) error {
	for _, decl := range program.Decls {
		satisfy, ok := decl.(*ast.SatisfyDecl)
		if !ok {
			continue
		}
		if err := c.collectSatisfy(satisfy); err != nil {
			return err
		}
	}
	return nil
}

// collectContract registers a required method set.
func (c *Checker) collectContract(decl *ast.ContractDecl) error {
	if _, exists := c.contracts[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate contract `%s`", decl.Name)
	}
	methods := map[string]*functionType{}
	for _, method := range decl.Methods {
		if _, exists := methods[method.Name]; exists {
			return fmt.Errorf("type error: duplicate contract method `%s.%s`", decl.Name, method.Name)
		}
		fnType, err := c.newFunctionType(method)
		if err != nil {
			return err
		}
		methods[method.Name] = fnType
	}
	c.contracts[decl.Name] = &contractType{name: decl.Name, methods: methods}
	return nil
}

// collectImpl registers concrete methods for a type.
func (c *Checker) collectImpl(decl *ast.ImplDecl) error {
	if _, err := c.parseType(decl.TypeName); err != nil {
		return err
	}
	methods := c.impls[decl.TypeName]
	if methods == nil {
		methods = map[string]*functionType{}
		c.impls[decl.TypeName] = methods
	}
	for _, method := range decl.Methods {
		if _, exists := methods[method.Name]; exists {
			return fmt.Errorf("type error: duplicate impl method `%s.%s`", decl.TypeName, method.Name)
		}
		fnType, err := c.newFunctionType(method)
		if err != nil {
			return err
		}
		methods[method.Name] = fnType
	}
	return nil
}

// collectSatisfy validates and records explicit contract satisfaction.
func (c *Checker) collectSatisfy(decl *ast.SatisfyDecl) error {
	contract := c.contracts[decl.ContractName]
	if contract == nil {
		return fmt.Errorf("type error: unknown contract `%s`", decl.ContractName)
	}
	if _, err := c.parseType(decl.TypeName); err != nil {
		return err
	}
	for name, want := range contract.methods {
		got := c.implMethod(decl.TypeName, name)
		if got == nil {
			return fmt.Errorf("type error: `%s` does not satisfy `%s`: missing method `%s`",
				decl.TypeName, decl.ContractName, name)
		}
		if !methodMatches(decl.TypeName, want, got) {
			return fmt.Errorf("type error: `%s.%s` does not match contract `%s`",
				decl.TypeName, name, decl.ContractName)
		}
	}
	if c.satisfactions[decl.ContractName] == nil {
		c.satisfactions[decl.ContractName] = map[string]bool{}
	}
	c.satisfactions[decl.ContractName][decl.TypeName] = true
	return nil
}

// collectEnum registers and validates a tag enum declaration.
func (c *Checker) collectEnum(decl *ast.EnumDecl) error {
	if _, exists := c.enums[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate enum `%s`", decl.Name)
	}
	if _, exists := c.structs[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate type `%s`", decl.Name)
	}
	if _, exists := c.unions[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate type `%s`", decl.Name)
	}
	enum := &enumType{name: decl.Name, tags: map[string]bool{}}
	for _, tag := range decl.Tags {
		if enum.tags[tag] {
			return fmt.Errorf("type error: duplicate enum tag `%s.%s`", decl.Name, tag)
		}
		enum.tags[tag] = true
	}
	c.enums[decl.Name] = enum
	return nil
}

// collectUnion registers and validates a tagged union declaration.
func (c *Checker) collectUnion(decl *ast.UnionDecl) error {
	if _, exists := c.unions[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate union `%s`", decl.Name)
	}
	if _, exists := c.structs[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate type `%s`", decl.Name)
	}
	if _, exists := c.enums[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate type `%s`", decl.Name)
	}
	union := &unionType{name: decl.Name, variants: map[string]string{}}
	for _, variant := range decl.Variants {
		if _, exists := union.variants[variant.Name]; exists {
			return fmt.Errorf("type error: duplicate union variant `%s.%s`",
				decl.Name, variant.Name)
		}
		if variant.Payload != "" {
			if _, err := c.parseType(variant.Payload); err != nil {
				return err
			}
		}
		union.variants[variant.Name] = variant.Payload
	}
	c.unions[decl.Name] = union
	return nil
}

// collectStruct registers and validates a struct declaration.
func (c *Checker) collectStruct(decl *ast.StructDecl) error {
	if _, exists := c.structs[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate struct `%s`", decl.Name)
	}
	if _, exists := c.enums[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate type `%s`", decl.Name)
	}
	if _, exists := c.unions[decl.Name]; exists {
		return fmt.Errorf("type error: duplicate type `%s`", decl.Name)
	}
	c.structs[decl.Name] = decl
	for _, field := range decl.Fields {
		if _, err := c.parseType(field.TypeName); err != nil {
			return err
		}
	}
	return nil
}

// newFunctionType converts a parsed function declaration into its static type.
func (c *Checker) newFunctionType(fn *ast.FunctionDecl) (*functionType, error) {
	params := make([]Type, 0, len(fn.Params))
	borrowParams := make([]bool, 0, len(fn.Params))
	comptimeParams := make([]bool, 0, len(fn.Params))
	for _, param := range fn.Params {
		paramType, err := c.parseType(param.TypeName)
		if err != nil {
			return nil, err
		}
		if paramType == typeVoid {
			return nil, fmt.Errorf("type error: parameter `%s` cannot have type void", param.Name)
		}
		if _, ok := dynContract(paramType); ok && !param.Borrow {
			return nil, fmt.Errorf("type error: Dyn parameter `%s` must be borrowed", param.Name)
		}
		params = append(params, paramType)
		borrowParams = append(borrowParams, param.Borrow)
		comptimeParams = append(comptimeParams, param.Comptime)
	}
	ret := typeVoid
	if fn.ReturnType != "" {
		var err error
		ret, err = c.parseType(fn.ReturnType)
		if err != nil {
			return nil, err
		}
	}
	return &functionType{
		name: fn.Name, params: params, borrowParams: borrowParams, comptimeParams: comptimeParams,
		returnType: ret, decl: fn, unsafe: fn.Unsafe, externABI: fn.ExternABI,
	}, nil
}

// parseType validates a source-level type name.
func (c *Checker) parseType(name string) (Type, error) {
	if strings.HasPrefix(name, "!") {
		return c.parseErrorUnionType(name)
	}
	if strings.HasPrefix(name, "[]") {
		return c.parseSliceType(name)
	}
	if strings.HasPrefix(name, "?") {
		return c.parseNullableType(name)
	}
	if base, arg, ok := splitGenericType(name); ok {
		return c.parseGenericType(name, base, arg)
	}
	typ := Type(name)
	if typ == typeSelf {
		return typ, nil
	}
	if !knownTypes[typ] && c.structs[name] == nil && c.enums[name] == nil &&
		c.unions[name] == nil {
		return "", fmt.Errorf("type error: unknown type `%s`", name)
	}
	return typ, nil
}

// parseSliceType validates v0.1 byte slice spellings.
func (c *Checker) parseSliceType(name string) (Type, error) {
	if name != string(typeByteString) {
		return "", fmt.Errorf("type error: unknown slice type `%s`", name)
	}
	return Type(name), nil
}

// parseErrorUnionType validates Zig-style !T error union types.
func (c *Checker) parseErrorUnionType(name string) (Type, error) {
	inner := strings.TrimPrefix(name, "!")
	if inner == "" {
		return "", fmt.Errorf("type error: ! must wrap a type")
	}
	if _, err := c.parseType(inner); err != nil {
		return "", err
	}
	return Type(name), nil
}

// parseGenericType validates supported generic-like type spellings.
func (c *Checker) parseGenericType(name string, base string, arg string) (Type, error) {
	if base == "ptr" {
		return c.parsePointerType(name, arg)
	}
	if base == "Dyn" {
		if c.contracts[arg] == nil {
			return "", fmt.Errorf("type error: unknown contract `%s`", arg)
		}
		return Type(name), nil
	}
	if base != "arena" && base != "handle" && base != "option" && base != "Task" {
		return "", fmt.Errorf("type error: unknown generic type `%s`", base)
	}
	if _, err := c.parseType(arg); err != nil {
		return "", err
	}
	return Type(name), nil
}

// parseNullableType validates nullable pointer types.
func (c *Checker) parseNullableType(name string) (Type, error) {
	inner := strings.TrimPrefix(name, "?")
	base, arg, ok := splitGenericType(inner)
	if !ok || base != "ptr" {
		return "", fmt.Errorf("type error: nullable type `%s` must wrap ptr<T>", name)
	}
	if _, err := c.parsePointerType(inner, arg); err != nil {
		return "", err
	}
	return Type(name), nil
}

// parsePointerType validates raw pointer element types.
func (c *Checker) parsePointerType(name string, arg string) (Type, error) {
	if strings.HasPrefix(arg, "const ") {
		arg = strings.TrimPrefix(arg, "const ")
	}
	if _, err := c.parseType(arg); err != nil {
		return "", err
	}
	return Type(name), nil
}

// checkFunction validates one function body against its signature.
func (c *Checker) checkFunction(fn *functionType) error {
	if fn.externABI != "" {
		return nil
	}
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		if err := env.define(param.Name, fn.params[idx], false); err != nil {
			return err
		}
	}
	previousReturn := c.currentReturn
	c.currentReturn = fn.returnType
	defer func() { c.currentReturn = previousReturn }()
	returns, err := c.checkBlock(fn.decl.Body, env, fn.returnType, fn.unsafe)
	if err != nil {
		return err
	}
	if fn.returnType != typeVoid && !returns {
		return fmt.Errorf("type error: function `%s` must return %s", fn.name, fn.returnType)
	}
	return nil
}

// checkImpl validates method bodies in an impl block.
func (c *Checker) checkImpl(decl *ast.ImplDecl) error {
	for _, method := range decl.Methods {
		fnType := c.implMethod(decl.TypeName, method.Name)
		if fnType == nil {
			return fmt.Errorf("type error: missing impl method `%s.%s`", decl.TypeName, method.Name)
		}
		if err := c.checkFunction(fnType); err != nil {
			return err
		}
	}
	return nil
}

// checkBlock validates statements and reports whether the block always returns.
func (c *Checker) checkBlock(
	block *ast.BlockStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	for _, stmt := range block.Statements {
		returns, err := c.checkStmt(stmt, env, wantReturn, unsafe)
		if err != nil || returns {
			return returns, err
		}
	}
	return false, nil
}

// checkStmt validates a statement and reports explicit return flow.
func (c *Checker) checkStmt(
	stmt ast.Statement,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		typ, err := c.checkExpr(s.Value, env, unsafe)
		if err != nil {
			return false, err
		}
		return false, env.define(s.Name, typ, s.Mutable)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env, unsafe)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(s, env, wantReturn, unsafe)
	case *ast.ExprStmt:
		_, err := c.checkExpr(s.Expr, env, unsafe)
		return false, err
	case *ast.IfStmt:
		return c.checkIfStmt(s, env, wantReturn, unsafe)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env, wantReturn, unsafe)
	case *ast.MatchStmt:
		return c.checkMatchStmt(s, env, wantReturn, unsafe)
	case *ast.UnsafeStmt:
		return c.checkBlock(s.Body, env.child(), wantReturn, true)
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env, wantReturn, unsafe)
	default:
		return false, fmt.Errorf("type error: unsupported statement %T", stmt)
	}
}

// checkAssignStmt validates assignment to an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope, unsafe bool) (bool, error) {
	want, ok := env.lookup(stmt.Name)
	if !ok {
		return false, fmt.Errorf("type error: undefined variable `%s`", stmt.Name)
	}
	if !env.isMutable(stmt.Name) {
		return false, fmt.Errorf("type error: cannot assign to immutable binding `%s`", stmt.Name)
	}
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, fmt.Errorf("type error: cannot assign %s to `%s` of type %s", got, stmt.Name, want)
	}
	return false, nil
}

// checkReturnStmt validates that return value type matches the function result.
func (c *Checker) checkReturnStmt(
	stmt *ast.ReturnStmt,
	env *scope,
	want Type,
	unsafe bool,
) (bool, error) {
	if stmt.Value == nil {
		if want != typeVoid {
			return false, fmt.Errorf("type error: return expects %s, got void", want)
		}
		return true, nil
	}
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if elem, ok := errorUnionElement(want); ok && got == Type(elem) {
		return true, nil
	}
	if got != want {
		return false, fmt.Errorf("type error: return expects %s, got %s", want, got)
	}
	return true, nil
}

// checkIfStmt validates a branch and tracks whether both arms return.
func (c *Checker) checkIfStmt(
	stmt *ast.IfStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, fmt.Errorf("type error: if condition must be bool, got %s", cond)
	}
	leftReturns, err := c.checkBlock(stmt.Consequence, env.child(), wantReturn, unsafe)
	if err != nil {
		return false, err
	}
	if stmt.Alternative == nil {
		return false, nil
	}
	rightReturns, err := c.checkBlock(stmt.Alternative, env.child(), wantReturn, unsafe)
	if err != nil {
		return false, err
	}
	return leftReturns && rightReturns, nil
}

// checkWhileStmt validates loop condition and body types.
func (c *Checker) checkWhileStmt(
	stmt *ast.WhileStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, fmt.Errorf("type error: while condition must be bool, got %s", cond)
	}
	_, err = c.checkBlock(stmt.Body, env.child(), wantReturn, unsafe)
	return false, err
}

// checkMatchStmt validates exhaustive simple enum tag matches.
func (c *Checker) checkMatchStmt(
	stmt *ast.MatchStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	valueType, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if enumType := c.enums[string(valueType)]; enumType != nil {
		return c.checkMatchArms(stmt.Arms, enumType, nil, env, wantReturn, unsafe)
	}
	unionType := c.unions[string(valueType)]
	if unionType != nil {
		return c.checkMatchArms(stmt.Arms, nil, unionType, env, wantReturn, unsafe)
	}
	return false, fmt.Errorf("type error: match expects enum or union, got %s", valueType)
}

// checkMatchArms validates tag patterns and return flow for match arms.
func (c *Checker) checkMatchArms(
	arms []ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	seen := map[string]bool{}
	allReturn := len(arms) > 0
	for _, arm := range arms {
		payload, err := matchPayloadType(enumType, unionType, arm)
		if err != nil {
			return false, err
		}
		typeName := matchTypeName(enumType, unionType)
		if seen[arm.Tag] {
			return false, fmt.Errorf("type error: duplicate match tag `%s.%s`", typeName, arm.Tag)
		}
		seen[arm.Tag] = true
		armEnv := env.child()
		if payload != "" && arm.Binding != "" {
			if err := armEnv.define(arm.Binding, Type(payload), false); err != nil {
				return false, err
			}
		}
		returns, err := c.checkStmt(arm.Body, armEnv, wantReturn, unsafe)
		if err != nil {
			return false, err
		}
		allReturn = allReturn && returns
	}
	if len(seen) != matchVariantCount(enumType, unionType) {
		return false, fmt.Errorf("type error: match on `%s` is not exhaustive",
			matchTypeName(enumType, unionType))
	}
	return allReturn, nil
}

// matchPayloadType validates a match arm pattern and returns its payload type.
func matchPayloadType(enumType *enumType, unionType *unionType, arm ast.MatchArm) (string, error) {
	if enumType != nil {
		if !enumType.tags[arm.Tag] {
			return "", fmt.Errorf("type error: unknown enum tag `%s.%s`", enumType.name, arm.Tag)
		}
		if arm.Binding != "" {
			return "", fmt.Errorf("type error: enum tag `%s.%s` has no payload",
				enumType.name, arm.Tag)
		}
		return "", nil
	}
	payload, ok := unionType.variants[arm.Tag]
	if !ok {
		return "", fmt.Errorf("type error: unknown union variant `%s.%s`", unionType.name, arm.Tag)
	}
	if payload == "" && arm.Binding != "" {
		return "", fmt.Errorf("type error: union variant `%s.%s` has no payload",
			unionType.name, arm.Tag)
	}
	return payload, nil
}

// matchTypeName returns the matched enum or union type name.
func matchTypeName(enumType *enumType, unionType *unionType) string {
	if enumType != nil {
		return enumType.name
	}
	return unionType.name
}

// matchVariantCount returns the number of variants in a match target.
func matchVariantCount(enumType *enumType, unionType *unionType) int {
	if enumType != nil {
		return len(enumType.tags)
	}
	return len(unionType.variants)
}

// checkExpr computes the static type of an expression.
func (c *Checker) checkExpr(expr ast.Expression, env *scope, unsafe bool) (Type, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return typeI64, nil
	case *ast.StringExpr:
		return typeByteString, nil
	case *ast.BoolExpr:
		return typeBool, nil
	case *ast.ComptimeExpr:
		return c.checkComptimeExpr(e, env, unsafe)
	case *ast.IdentExpr:
		return checkIdentExpr(e, env)
	case *ast.PrefixExpr:
		return c.checkPrefixExpr(e, env, unsafe)
	case *ast.BinaryExpr:
		return c.checkBinaryExpr(e, env, unsafe)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env, unsafe)
	case *ast.CastExpr:
		return c.checkCastExpr(e, env, unsafe)
	case *ast.TryExpr:
		return c.checkTryExpr(e, env, unsafe)
	case *ast.ArenaNewExpr:
		return c.checkArenaNewExpr(e)
	case *ast.StructLiteralExpr:
		return c.checkStructLiteralExpr(e, env, unsafe)
	case *ast.FieldExpr:
		return c.checkFieldExpr(e, env, unsafe)
	default:
		return "", fmt.Errorf("type error: unsupported expression %T", expr)
	}
}

// checkIdentExpr resolves a variable reference in lexical scopes.
func checkIdentExpr(expr *ast.IdentExpr, env *scope) (Type, error) {
	typ, ok := env.lookup(expr.Name)
	if ok {
		return typ, nil
	}
	if expr.Name == "void" {
		return typeVoid, nil
	}
	return "", fmt.Errorf("type error: undefined variable `%s`", expr.Name)
}

// checkPrefixExpr validates unary operators.
func (c *Checker) checkPrefixExpr(expr *ast.PrefixExpr, env *scope, unsafe bool) (Type, error) {
	right, err := c.checkExpr(expr.Right, env, unsafe)
	if err != nil {
		return "", err
	}
	switch expr.Operator {
	case "-":
		if !signedNumericTypes[right] {
			return "", fmt.Errorf("type error: unary - expects signed numeric, got %s", right)
		}
		return right, nil
	case "!":
		if right != typeBool {
			return "", fmt.Errorf("type error: unary ! expects bool, got %s", right)
		}
		return typeBool, nil
	default:
		return "", fmt.Errorf("type error: unsupported unary `%s`", expr.Operator)
	}
}

// checkBinaryExpr validates arithmetic, equality, and comparison operators.
func (c *Checker) checkBinaryExpr(expr *ast.BinaryExpr, env *scope, unsafe bool) (Type, error) {
	left, err := c.checkExpr(expr.Left, env, unsafe)
	if err != nil {
		return "", err
	}
	right, err := c.checkExpr(expr.Right, env, unsafe)
	if err != nil {
		return "", err
	}
	if expr.Operator == "==" || expr.Operator == "!=" {
		return checkEquality(expr.Operator, left, right)
	}
	if left != right {
		return "", fmt.Errorf("type error: operator `%s` operands must have same type", expr.Operator)
	}
	if !numericTypes[left] {
		return "", fmt.Errorf("type error: operator `%s` expects numeric operands", expr.Operator)
	}
	if expr.Operator == "%" && !integerTypes[left] {
		return "", fmt.Errorf("type error: operator `%s` expects integer operands", expr.Operator)
	}
	if isComparison(expr.Operator) {
		return typeBool, nil
	}
	return left, nil
}

// checkEquality validates equality operands.
func checkEquality(op string, left Type, right Type) (Type, error) {
	if left != right {
		return "", fmt.Errorf("type error: operator `%s` operands must have same type", op)
	}
	return typeBool, nil
}

// isComparison reports whether op returns bool for numeric operands.
func isComparison(op string) bool {
	return op == "<" || op == "<=" || op == ">" || op == ">="
}

// checkCastExpr validates explicit low-level casts.
func (c *Checker) checkCastExpr(expr *ast.CastExpr, env *scope, unsafe bool) (Type, error) {
	target, err := c.parseType(expr.TargetType)
	if err != nil {
		return "", err
	}
	source, err := c.checkExpr(expr.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	if numericTypes[source] && numericTypes[target] {
		return target, nil
	}
	if isPointerType(source) && isPointerType(target) {
		if !unsafe {
			return "", fmt.Errorf("unsafe error: pointer cast requires unsafe block")
		}
		return target, nil
	}
	return "", fmt.Errorf("type error: cannot cast %s to %s", source, target)
}

// checkTryExpr validates error-union propagation and returns the success type.
func (c *Checker) checkTryExpr(expr *ast.TryExpr, env *scope, unsafe bool) (Type, error) {
	if _, ok := errorUnionElement(c.currentReturn); !ok {
		return "", fmt.Errorf("type error: try requires function to return !T")
	}
	source, err := c.checkExpr(expr.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := errorUnionElement(source)
	if !ok {
		return "", fmt.Errorf("type error: try expects !T, got %s", source)
	}
	return Type(elem), nil
}

// checkCallExpr validates builtin and user function calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		if typ, ok, err := c.checkUnionConstructorCall(field, expr.Args, env, unsafe); ok || err != nil {
			return typ, err
		}
		return c.checkMethodCallExpr(field, expr.Args, env, unsafe)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("type error: callee must be a function name")
	}
	if name.Name == "print" {
		return c.checkPrintCall(expr, env, unsafe)
	}
	if name.Name == "ptr_read" {
		return c.checkPtrRead(expr, env, unsafe)
	}
	if name.Name == "ptr_write" {
		return c.checkPtrWrite(expr, env, unsafe)
	}
	if name.Name == "error" {
		return c.checkErrorCall(expr, env, unsafe)
	}
	if name.Name == "Io" {
		return checkNoArgConstructor("Io", expr.Args, "Io")
	}
	if name.Name == "TaskGroup" {
		return checkNoArgConstructor("TaskGroup", expr.Args, "TaskGroup")
	}
	return c.checkUserCall(name.Name, expr.Args, env, unsafe)
}

// checkErrorCall validates error-union error construction.
func (c *Checker) checkErrorCall(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("type error: `error` expects 1 arg, got %d", len(expr.Args))
	}
	got, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != typeByteString {
		return "", fmt.Errorf("type error: `error` expects []const u8, got %s", got)
	}
	if _, ok := errorUnionElement(c.currentReturn); !ok {
		return "", fmt.Errorf("type error: `error` requires function to return !T")
	}
	return c.currentReturn, nil
}

// checkUserCall validates a declared function call.
func (c *Checker) checkUserCall(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	fn, ok := c.functions[name]
	if !ok {
		return "", fmt.Errorf("type error: undefined function `%s`", name)
	}
	if (fn.unsafe || fn.externABI != "") && !unsafe {
		return "", fmt.Errorf("unsafe error: call to `%s` requires unsafe block", name)
	}
	if len(args) != len(fn.params) {
		return "", fmt.Errorf("type error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	for idx, arg := range args {
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return "", err
		}
		if fn.comptimeParams[idx] {
			if _, err := evalComptime(arg); err != nil {
				return "", err
			}
		}
		if contractName, ok := dynContract(fn.params[idx]); ok {
			if !c.satisfies(contractName, got) {
				return "", fmt.Errorf("type error: %s does not satisfy `%s`", got, contractName)
			}
			continue
		}
		if got != fn.params[idx] {
			return "", fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, name, fn.params[idx], got)
		}
	}
	return fn.returnType, nil
}

// checkUnionConstructorCall validates Union.Variant(payload) construction.
func (c *Checker) checkUnionConstructorCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	unionType, ok := unionReceiver(field.Receiver, c.unions)
	if !ok {
		return "", false, nil
	}
	payload, exists := unionType.variants[field.Name]
	if !exists {
		return "", true, fmt.Errorf("type error: unknown union variant `%s.%s`",
			unionType.name, field.Name)
	}
	if payload == "" {
		return "", true, fmt.Errorf("type error: union variant `%s.%s` expects 0 args",
			unionType.name, field.Name)
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: union variant `%s.%s` expects 1 arg, got %d",
			unionType.name, field.Name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != Type(payload) {
		return "", true, fmt.Errorf("type error: union variant `%s.%s` expects %s, got %s",
			unionType.name, field.Name, payload, got)
	}
	return Type(unionType.name), true, nil
}

// checkArenaNewExpr validates arena<T>() and returns the arena type.
func (c *Checker) checkArenaNewExpr(expr *ast.ArenaNewExpr) (Type, error) {
	if _, err := c.parseType(expr.TypeName); err != nil {
		return "", err
	}
	return Type(fmt.Sprintf("arena<%s>", expr.TypeName)), nil
}

// checkStructLiteralExpr validates field names and initializer types.
func (c *Checker) checkStructLiteralExpr(
	expr *ast.StructLiteralExpr,
	env *scope,
	unsafe bool,
) (Type, error) {
	decl := c.structs[expr.TypeName]
	if decl == nil {
		return "", fmt.Errorf("type error: unknown struct `%s`", expr.TypeName)
	}
	values := map[string]Type{}
	for _, field := range expr.Fields {
		got, err := c.checkExpr(field.Value, env, unsafe)
		if err != nil {
			return "", err
		}
		values[field.Name] = got
	}
	for _, field := range decl.Fields {
		got, ok := values[field.Name]
		if !ok {
			return "", fmt.Errorf("type error: missing field `%s.%s`", expr.TypeName, field.Name)
		}
		if got != Type(field.TypeName) {
			return "", fmt.Errorf("type error: field `%s.%s` expects %s, got %s",
				expr.TypeName, field.Name, field.TypeName, got)
		}
		delete(values, field.Name)
	}
	for name := range values {
		return "", fmt.Errorf("type error: unknown field `%s.%s`", expr.TypeName, name)
	}
	return Type(expr.TypeName), nil
}

// checkFieldExpr returns the declared type of a struct field access.
func (c *Checker) checkFieldExpr(expr *ast.FieldExpr, env *scope, unsafe bool) (Type, error) {
	if enumType, ok := enumReceiver(expr.Receiver, c.enums); ok {
		if !enumType.tags[expr.Name] {
			return "", fmt.Errorf("type error: unknown enum tag `%s.%s`", enumType.name, expr.Name)
		}
		return Type(enumType.name), nil
	}
	if unionType, ok := unionReceiver(expr.Receiver, c.unions); ok {
		payload, exists := unionType.variants[expr.Name]
		if !exists {
			return "", fmt.Errorf("type error: unknown union variant `%s.%s`",
				unionType.name, expr.Name)
		}
		if payload != "" {
			return "", fmt.Errorf("type error: union variant `%s.%s` expects payload",
				unionType.name, expr.Name)
		}
		return Type(unionType.name), nil
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	decl := c.structs[string(receiver)]
	if decl == nil {
		return "", fmt.Errorf("type error: `%s` has no fields", receiver)
	}
	for _, field := range decl.Fields {
		if field.Name == expr.Name {
			return Type(field.TypeName), nil
		}
	}
	return "", fmt.Errorf("type error: unknown field `%s.%s`", receiver, expr.Name)
}

// enumReceiver returns an enum namespace used by EnumName.Tag expressions.
func enumReceiver(expr ast.Expression, enums map[string]*enumType) (*enumType, bool) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}
	enumType, ok := enums[ident.Name]
	return enumType, ok
}

// unionReceiver returns a union namespace used by UnionName.Variant expressions.
func unionReceiver(expr ast.Expression, unions map[string]*unionType) (*unionType, bool) {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}
	unionType, ok := unions[ident.Name]
	return unionType, ok
}

// checkMethodCallExpr validates arena methods.
func (c *Checker) checkMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if contractName, ok := dynContract(receiver); ok {
		return c.checkDynMethodCall(contractName, field.Name, args, env, unsafe)
	}
	if receiver == "TaskGroup" {
		return c.checkTaskGroupMethod(field.Name, args, env, unsafe)
	}
	if elem, ok := taskElement(receiver); ok {
		return checkTaskMethod(field.Name, elem, args)
	}
	base, arg, ok := splitGenericType(string(receiver))
	if !ok || base != "arena" {
		method := c.implMethod(string(receiver), field.Name)
		if method != nil {
			return c.checkMethodArgs(method, receiver, args, env, unsafe)
		}
		return "", fmt.Errorf("type error: `%s` has no method `%s`", receiver, field.Name)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arg, args, env, unsafe)
	case "get":
		return c.checkArenaGet(arg, args, env, unsafe)
	default:
		return "", fmt.Errorf("type error: unknown arena method `%s`", field.Name)
	}
}

// checkTaskGroupMethod validates structured task spawning.
func (c *Checker) checkTaskGroupMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if name != "spawn" {
		return "", fmt.Errorf("type error: TaskGroup has no method `%s`", name)
	}
	if len(args) < 2 {
		return "", fmt.Errorf("type error: `TaskGroup.spawn` expects io, function, and args")
	}
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if ioType != "Io" {
		return "", fmt.Errorf("type error: `TaskGroup.spawn` expects Io, got %s", ioType)
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("type error: `TaskGroup.spawn` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", fmt.Errorf("type error: undefined function `%s`", target.Name)
	}
	spawnArgs := append([]ast.Expression{args[0]}, args[2:]...)
	if err := c.checkTaskArgs(target.Name, fn, spawnArgs, env, unsafe); err != nil {
		return "", err
	}
	return Type(fmt.Sprintf("Task<%s>", fn.returnType)), nil
}

// checkTaskArgs validates spawned function arguments.
func (c *Checker) checkTaskArgs(
	name string,
	fn *functionType,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != len(fn.params) {
		return fmt.Errorf("type error: `%s` expects %d args, got %d", name, len(fn.params), len(args))
	}
	for idx, arg := range args {
		if fn.borrowParams[idx] {
			return fmt.Errorf("type error: task cannot capture borrow parameter `%s`", name)
		}
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return err
		}
		if got != fn.params[idx] {
			return fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, name, fn.params[idx], got)
		}
	}
	return nil
}

// checkTaskMethod validates await/cancel on a task value.
func checkTaskMethod(name string, elem string, args []ast.Expression) (Type, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("type error: `task.%s` expects 0 args, got %d", name, len(args))
	}
	switch name {
	case "await":
		return Type(elem), nil
	case "cancel":
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: Task has no method `%s`", name)
	}
}

// checkDynMethodCall validates a method call through borrow Dyn<Contract>.
func (c *Checker) checkDynMethodCall(
	contractName string,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	contract := c.contracts[contractName]
	if contract == nil || contract.methods[name] == nil {
		return "", fmt.Errorf("type error: `Dyn<%s>` has no method `%s`", contractName, name)
	}
	return c.checkMethodArgs(contract.methods[name], typeSelf, args, env, unsafe)
}

// checkMethodArgs validates method-call arguments after the implicit self receiver.
func (c *Checker) checkMethodArgs(
	method *functionType,
	receiver Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(method.params) == 0 {
		return "", fmt.Errorf("type error: method `%s` must have self parameter", method.name)
	}
	if len(args) != len(method.params)-1 {
		return "", fmt.Errorf("type error: `%s` expects %d args, got %d",
			method.name, len(method.params)-1, len(args))
	}
	if method.params[0] != receiver && method.params[0] != typeSelf {
		return "", fmt.Errorf("type error: method `%s` self expects %s, got %s",
			method.name, method.params[0], receiver)
	}
	for idx, arg := range args {
		want := method.params[idx+1]
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return "", err
		}
		if got != want {
			return "", fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, method.name, want, got)
		}
	}
	return method.returnType, nil
}

// checkArenaAdd validates arena<T>.add(value).
func (c *Checker) checkArenaAdd(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `arena.add` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != Type(arg) {
		return "", fmt.Errorf("type error: `arena.add` expects %s, got %s", arg, got)
	}
	return Type(fmt.Sprintf("handle<%s>", arg)), nil
}

// checkArenaGet validates arena<T>.get(handle<T>).
func (c *Checker) checkArenaGet(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `arena.get` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	want := Type(fmt.Sprintf("handle<%s>", arg))
	if got != want {
		return "", fmt.Errorf("type error: `arena.get` expects %s, got %s", want, got)
	}
	return Type(arg), nil
}

// checkPtrRead validates unsafe raw pointer reads.
func (c *Checker) checkPtrRead(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if !unsafe {
		return "", fmt.Errorf("unsafe error: ptr_read requires unsafe block")
	}
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("type error: `ptr_read` expects 1 arg, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") {
		return "", fmt.Errorf("type error: `ptr_read` expects non-null raw pointer, got %s", ptrType)
	}
	return Type(strings.TrimPrefix(elem, "const ")), nil
}

// checkPtrWrite validates unsafe raw pointer writes.
func (c *Checker) checkPtrWrite(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if !unsafe {
		return "", fmt.Errorf("unsafe error: ptr_write requires unsafe block")
	}
	if len(expr.Args) != 2 {
		return "", fmt.Errorf("type error: `ptr_write` expects 2 args, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") || strings.HasPrefix(elem, "const ") {
		return "", fmt.Errorf("type error: `ptr_write` expects mutable non-null raw pointer")
	}
	valueType, err := c.checkExpr(expr.Args[1], env, unsafe)
	if err != nil {
		return "", err
	}
	if valueType != Type(elem) {
		return "", fmt.Errorf("type error: `ptr_write` expects %s, got %s", elem, valueType)
	}
	return typeVoid, nil
}

// pointerElement extracts the element type from ptr<T> or ?ptr<T>.
func pointerElement(typ Type) (string, bool) {
	name := strings.TrimPrefix(string(typ), "?")
	base, arg, ok := splitGenericType(name)
	if !ok || base != "ptr" {
		return "", false
	}
	return arg, true
}

// isPointerType reports whether typ is ptr<T> or ?ptr<T>.
func isPointerType(typ Type) bool {
	_, ok := pointerElement(typ)
	return ok
}

// checkNoArgConstructor validates a zero-argument builtin constructor.
func checkNoArgConstructor(name string, args []ast.Expression, typ Type) (Type, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("type error: `%s` expects 0 args, got %d", name, len(args))
	}
	return typ, nil
}

// taskElement extracts T from Task<T>.
func taskElement(typ Type) (string, bool) {
	base, arg, ok := splitGenericType(string(typ))
	if !ok || base != "Task" {
		return "", false
	}
	return arg, true
}

// dynContract extracts C from Dyn<C>.
func dynContract(typ Type) (string, bool) {
	base, arg, ok := splitGenericType(string(typ))
	return arg, ok && base == "Dyn"
}

// implMethod returns a concrete method implementation.
func (c *Checker) implMethod(typeName string, method string) *functionType {
	methods := c.impls[typeName]
	if methods == nil {
		return nil
	}
	return methods[method]
}

// satisfies reports whether a type explicitly satisfies a contract.
func (c *Checker) satisfies(contractName string, typ Type) bool {
	return c.satisfactions[contractName] != nil && c.satisfactions[contractName][string(typ)]
}

// methodMatches checks an impl method against a contract method.
func methodMatches(typeName string, want *functionType, got *functionType) bool {
	if len(want.params) != len(got.params) || want.returnType != got.returnType {
		return false
	}
	for idx, wantParam := range want.params {
		expected := wantParam
		if expected == typeSelf {
			expected = Type(typeName)
		}
		if expected != got.params[idx] || want.borrowParams[idx] != got.borrowParams[idx] {
			return false
		}
	}
	return true
}

// errorUnionElement extracts T from !T.
func errorUnionElement(typ Type) (string, bool) {
	name := string(typ)
	if !strings.HasPrefix(name, "!") || len(name) == 1 {
		return "", false
	}
	return strings.TrimPrefix(name, "!"), true
}

// checkPrintCall validates the print builtin.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if len(expr.Args) != 1 {
		return "", fmt.Errorf("type error: `print` expects 1 arg, got %d", len(expr.Args))
	}
	got, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got == typeVoid {
		return "", fmt.Errorf("type error: `print` cannot print void")
	}
	return typeVoid, nil
}

// newScope creates a lexical type scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, values: map[string]Type{}, mutable: map[string]bool{}}
}

// child creates a nested lexical type scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// define binds a local name to a type in the current scope.
func (s *scope) define(name string, typ Type, mutable bool) error {
	if _, exists := s.values[name]; exists {
		return fmt.Errorf("type error: duplicate variable `%s`", name)
	}
	s.values[name] = typ
	s.mutable[name] = mutable
	return nil
}

// lookup resolves a local name by walking parent scopes.
func (s *scope) lookup(name string) (Type, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if typ, ok := cur.values[name]; ok {
			return typ, true
		}
	}
	return "", false
}

// isMutable reports whether a resolved local name may be assigned.
func (s *scope) isMutable(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			return cur.mutable[name]
		}
	}
	return false
}

// splitGenericType extracts base and argument from base<arg>.
func splitGenericType(name string) (string, string, bool) {
	start := strings.IndexByte(name, '<')
	if start < 1 || !strings.HasSuffix(name, ">") {
		return "", "", false
	}
	arg := name[start+1 : len(name)-1]
	if arg == "" {
		return "", "", false
	}
	return name[:start], arg, true
}
