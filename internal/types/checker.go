package types

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Type is the static type name used by the v0 checker.
type Type string

const (
	typeBool       Type = "bool"
	typeI64        Type = "i64"
	typeU8         Type = "u8"
	typeByteString Type = "[]const u8"
	typeSelf       Type = "Self"
	typeVoid       Type = "void"
)

var knownTypes = map[Type]bool{
	typeBool:              true,
	typeI64:               true,
	typeByteString:        true,
	typeVoid:              true,
	"i8":                  true,
	"i16":                 true,
	"i32":                 true,
	typeU8:                true,
	"u16":                 true,
	"u32":                 true,
	"u64":                 true,
	"usize":               true,
	"isize":               true,
	"f32":                 true,
	"f64":                 true,
	"Io":                  true,
	"Allocator":           true,
	"std::fs::Metadata":   true,
	"std::string::String": true,
	"TaskGroup":           true,
	"Queue":               true,
	"Partition":           true,
	"LocalBuffer":         true,
}

var numericTypes = map[Type]bool{
	"i8":    true,
	"i16":   true,
	"i32":   true,
	"i64":   true,
	typeU8:  true,
	"u16":   true,
	"u32":   true,
	"u64":   true,
	"usize": true,
	"isize": true,
	"f32":   true,
	"f64":   true,
}

var copyTypes = map[Type]bool{
	typeBool:            true,
	typeI64:             true,
	typeByteString:      true,
	typeVoid:            true,
	"i8":                true,
	"i16":               true,
	"i32":               true,
	typeU8:              true,
	"u16":               true,
	"u32":               true,
	"u64":               true,
	"usize":             true,
	"isize":             true,
	"f32":               true,
	"f64":               true,
	"Io":                true,
	"Allocator":         true,
	"std::fs::Metadata": true,
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
	loopLabels    []string
}

type enumType struct {
	name   string
	tags   map[string]bool
	public bool
}

type unionType struct {
	name     string
	variants map[string]string
	public   bool
}

type functionType struct {
	name            string
	params          []Type
	borrowParams    []bool
	mutBorrowParams []bool
	comptimeParams  []bool
	returnType      Type
	decl            *ast.FunctionDecl
	unsafe          bool
	externABI       string
}

type contractType struct {
	name    string
	methods map[string]*functionType
	public  bool
}

type scope struct {
	parent    *scope
	values    map[string]Type
	mutable   map[string]bool
	borrowed  map[string]bool
	mutBorrow map[string]bool
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
	if err := c.checkPublicAPI(program); err != nil {
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
		case *ast.ImportDecl:
			continue
		case *ast.FunctionDecl:
			continue
		default:
			return fmt.Errorf("type error: unsupported declaration %T", decl)
		}
	}
	return nil
}

// checkPublicAPI rejects private types exposed through public declarations.
func (c *Checker) checkPublicAPI(program *ast.Program) error {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			if err := c.checkPublicFunctionSignature(d.Public, d.Name, d.Params, d.ReturnType); err != nil {
				return err
			}
		case *ast.StructDecl:
			if err := c.checkPublicStructFields(d); err != nil {
				return err
			}
		case *ast.UnionDecl:
			if err := c.checkPublicUnionVariants(d); err != nil {
				return err
			}
		case *ast.ContractDecl:
			if err := c.checkPublicContract(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkPublicFunctionSignature validates the public boundary of one function.
func (c *Checker) checkPublicFunctionSignature(
	public bool,
	name string,
	params []ast.Param,
	returnType string,
) error {
	if !public {
		return nil
	}
	for _, param := range params {
		if err := c.rejectPrivateType(param.TypeName, "function `"+name+"` parameter"); err != nil {
			return err
		}
	}
	if returnType == "" {
		return nil
	}
	return c.rejectPrivateType(returnType, "function `"+name+"` return type")
}

// checkPublicStructFields validates public fields on one struct.
func (c *Checker) checkPublicStructFields(decl *ast.StructDecl) error {
	for _, field := range decl.Fields {
		if !field.Public {
			continue
		}
		context := "field `" + decl.Name + "." + field.Name + "`"
		if err := c.rejectPrivateType(field.TypeName, context); err != nil {
			return err
		}
	}
	return nil
}

// checkPublicUnionVariants validates payloads exposed by public union variants.
func (c *Checker) checkPublicUnionVariants(decl *ast.UnionDecl) error {
	if !decl.Public {
		return nil
	}
	for _, variant := range decl.Variants {
		if variant.Payload == "" {
			continue
		}
		context := "union variant `" + decl.Name + "::" + variant.Name + "`"
		if err := c.rejectPrivateType(variant.Payload, context); err != nil {
			return err
		}
	}
	return nil
}

// checkPublicContract validates method signatures exposed by one contract.
func (c *Checker) checkPublicContract(decl *ast.ContractDecl) error {
	if !decl.Public {
		return nil
	}
	for _, method := range decl.Methods {
		if err := c.checkPublicFunctionSignature(true, method.Name, method.Params,
			method.ReturnType); err != nil {
			return err
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
	c.contracts[decl.Name] = &contractType{name: decl.Name, methods: methods, public: decl.Public}
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
	enum := &enumType{name: decl.Name, tags: map[string]bool{}, public: decl.Public}
	for _, tag := range decl.Tags {
		if enum.tags[tag] {
			return fmt.Errorf("type error: duplicate enum tag `%s::%s`", decl.Name, tag)
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
	union := &unionType{name: decl.Name, variants: map[string]string{}, public: decl.Public}
	for _, variant := range decl.Variants {
		if _, exists := union.variants[variant.Name]; exists {
			return fmt.Errorf("type error: duplicate union variant `%s::%s`",
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
	mutBorrowParams := make([]bool, 0, len(fn.Params))
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
		mutBorrowParams = append(mutBorrowParams, param.MutBorrow)
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
		name: fn.Name, params: params, borrowParams: borrowParams,
		mutBorrowParams: mutBorrowParams, comptimeParams: comptimeParams,
		returnType: ret, decl: fn, unsafe: fn.Unsafe, externABI: fn.ExternABI,
	}, nil
}

// parseType validates a source-level type name.
func (c *Checker) parseType(name string) (Type, error) {
	if strings.HasPrefix(name, "!") {
		return c.parseErrorUnionType(name)
	}
	if errorType, successType, ok := typedErrorUnionParts(name); ok {
		if _, err := c.parseType(errorType); err != nil {
			return "", err
		}
		if _, err := c.parseType(successType); err != nil {
			return "", err
		}
		return Type(name), nil
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
	args, ok := splitGenericArgs(arg)
	if !ok {
		return "", fmt.Errorf("type error: invalid generic arguments for `%s`", base)
	}
	switch base {
	case "std::map::Map":
		return c.parseMapType(name, args)
	case "ptr":
		arg, err := singleGenericArg(base, args)
		if err != nil {
			return "", err
		}
		return c.parsePointerType(name, arg)
	case "Dyn":
		return c.parseDynType(name, base, args)
	}

	if !isKnownSingleArgGeneric(base) {
		return "", fmt.Errorf("type error: unknown generic type `%s`", base)
	}
	arg, err := singleGenericArg(base, args)
	if err != nil {
		return "", err
	}
	if _, err := c.parseType(arg); err != nil {
		return "", err
	}
	return Type(name), nil
}

// parseDynType validates contract object type spellings.
func (c *Checker) parseDynType(name string, base string, args []string) (Type, error) {
	arg, err := singleGenericArg(base, args)
	if err != nil {
		return "", err
	}
	if c.contracts[arg] == nil {
		return "", fmt.Errorf("type error: unknown contract `%s`", arg)
	}
	return Type(name), nil
}

// parseMapType validates the v0.2 symbol-table map spelling.
func (c *Checker) parseMapType(name string, args []string) (Type, error) {
	if len(args) != 2 {
		return "", fmt.Errorf("type error: std::map::Map expects 2 type arguments")
	}
	if args[0] != string(typeByteString) {
		return "", fmt.Errorf("type error: std::map::Map key type must be []const u8 in v0.2")
	}
	if _, err := c.parseType(args[1]); err != nil {
		return "", err
	}
	if !c.isCopyType(Type(args[1])) {
		return "", fmt.Errorf("type error: std::map::Map value type must be copy in v0.2")
	}
	return Type(name), nil
}

// isKnownSingleArgGeneric reports whether base currently takes exactly one type argument.
func isKnownSingleArgGeneric(base string) bool {
	switch base {
	case "arena", "handle", "option", "std::array::Array", "Task",
		"Channel", "Mutex", "Atomic":
		return true
	default:
		return false
	}
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

// rejectPrivateType reports an error when typeName exposes a private declaration.
func (c *Checker) rejectPrivateType(typeName string, context string) error {
	for _, name := range referencedTypeNames(typeName) {
		if !c.isUserDeclaredType(name) {
			continue
		}
		if c.isPublicType(name) {
			continue
		}
		return fmt.Errorf("type error: public %s exposes private type `%s`", context, name)
	}
	return nil
}

// referencedTypeNames returns source type names used inside one type spelling.
func referencedTypeNames(typeName string) []string {
	typeName = strings.TrimPrefix(typeName, "!")
	if errorType, successType, ok := typedErrorUnionParts(typeName); ok {
		return append(referencedTypeNames(errorType), referencedTypeNames(successType)...)
	}
	typeName = strings.TrimPrefix(typeName, "?")
	typeName = strings.TrimPrefix(typeName, "[]")
	typeName = strings.TrimPrefix(typeName, "const ")
	if base, arg, ok := splitGenericType(typeName); ok {
		names := []string{base}
		for _, part := range splitPublicTypeArgs(arg) {
			names = append(names, referencedTypeNames(part)...)
		}
		return names
	}
	return []string{typeName}
}

// splitPublicTypeArgs splits a generic argument list for public API checks.
func splitPublicTypeArgs(args string) []string {
	parts, ok := splitGenericArgs(args)
	if ok {
		return parts
	}
	return []string{args}
}

// isUserDeclaredType reports whether name is declared by the current program.
func (c *Checker) isUserDeclaredType(name string) bool {
	if c.structs[name] != nil {
		return true
	}
	if c.enums[name] != nil {
		return true
	}
	if c.unions[name] != nil {
		return true
	}
	if c.contracts[name] != nil {
		return true
	}
	return false
}

// isPublicType reports whether name is externally visible.
func (c *Checker) isPublicType(name string) bool {
	if decl := c.structs[name]; decl != nil {
		return decl.Public
	}
	if enum := c.enums[name]; enum != nil {
		return enum.public
	}
	if union := c.unions[name]; union != nil {
		return union.public
	}
	if contract := c.contracts[name]; contract != nil {
		return contract.public
	}
	return false
}

// checkFunction validates one function body against its signature.
func (c *Checker) checkFunction(fn *functionType) error {
	if fn.externABI != "" {
		return nil
	}
	env := newScope(nil)
	for idx, param := range fn.decl.Params {
		if err := env.defineParam(param.Name, fn.params[idx], param.Borrow, param.MutBorrow); err != nil {
			return err
		}
	}
	previousReturn := c.currentReturn
	previousLoops := c.loopLabels
	c.currentReturn = fn.returnType
	c.loopLabels = nil
	defer func() {
		c.currentReturn = previousReturn
		c.loopLabels = previousLoops
	}()
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
		return c.checkLetStmt(s, env, unsafe)
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
	case *ast.ForStmt:
		return c.checkForStmt(s, env, wantReturn, unsafe)
	case *ast.BreakStmt:
		return false, c.checkLoopBranch("break", s.Label)
	case *ast.ContinueStmt:
		return false, c.checkLoopBranch("continue", s.Label)
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

// checkLetStmt validates a let or var declaration.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope, unsafe bool) (bool, error) {
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		typ, mutable, err := c.checkBorrowPrefix(borrow, env, unsafe)
		if err != nil {
			return false, err
		}
		return false, env.defineParam(stmt.Name, typ, true, mutable)
	}
	typ, mutable, ok, err := c.checkArrayBorrowInitializer(stmt.Value, env, unsafe)
	if ok || err != nil {
		if err != nil {
			return false, err
		}
		return false, env.defineParam(stmt.Name, typ, true, mutable)
	}
	if ok, err := c.checkStringViewInitializer(stmt.Value, env, unsafe); ok || err != nil {
		if err != nil {
			return false, err
		}
		return false, env.defineParam(stmt.Name, typeByteString, true, false)
	}
	typ, err = c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	return false, env.define(stmt.Name, typ, stmt.Mutable)
}

// checkStringViewInitializer recognizes string.as_bytes() local byte views.
func (c *Checker) checkStringViewInitializer(
	expr ast.Expression,
	env *scope,
	unsafe bool,
) (bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "as_bytes" {
		return false, nil
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return true, err
	}
	if receiver != "std::string::String" {
		return true, fmt.Errorf("type error: `String.as_bytes` expects String receiver")
	}
	if len(call.Args) != 0 {
		return true, fmt.Errorf("type error: `String.as_bytes` expects 0 args, got %d",
			len(call.Args))
	}
	return true, nil
}

// checkArrayBorrowInitializer recognizes try array.at/at_mut(index) local borrows.
func (c *Checker) checkArrayBorrowInitializer(
	expr ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, bool, error) {
	tryExpr, ok := expr.(*ast.TryExpr)
	if !ok {
		return "", false, false, nil
	}
	call, ok := tryExpr.Value.(*ast.CallExpr)
	if !ok {
		return "", false, false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "at" && field.Name != "at_mut") {
		return "", false, false, nil
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", false, true, err
	}
	base, elem, ok := splitGenericType(string(receiver))
	if !ok || base != "std::array::Array" {
		return "", false, true, fmt.Errorf("type error: `Array.%s` expects Array receiver", field.Name)
	}
	if field.Name == "at_mut" {
		if ident, ok := field.Receiver.(*ast.IdentExpr); !ok || !env.isMutable(ident.Name) {
			return "", false, true, fmt.Errorf("type error: `Array.at_mut` requires mutable array binding")
		}
	}
	if err := c.checkArrayIndexArg(field.Name, call.Args, env, unsafe); err != nil {
		return "", false, true, err
	}
	return Type(elem), field.Name == "at_mut", true, nil
}

// checkAssignStmt validates assignment to an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope, unsafe bool) (bool, error) {
	want, err := c.checkAssignableTarget(stmt.Target, env, unsafe)
	if err != nil {
		return false, err
	}
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, fmt.Errorf("type error: cannot assign %s to `%s` of type %s",
			got, stmt.Target.String(), want)
	}
	return false, nil
}

// checkAssignableTarget returns the type that a valid assignment target accepts.
func (c *Checker) checkAssignableTarget(
	expr ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		return checkAssignableIdent(target, env)
	case *ast.FieldExpr:
		return c.checkAssignableField(target, env, unsafe)
	case *ast.DerefExpr:
		return c.checkAssignableDeref(target, env, unsafe)
	case *ast.CallExpr:
		return c.checkAssignableCall(target, env, unsafe)
	default:
		return "", fmt.Errorf("type error: invalid assignment target `%s`", expr.String())
	}
}

// checkAssignableCall accepts assignment through trusted mutable slot accessors.
func (c *Checker) checkAssignableCall(
	expr *ast.CallExpr,
	env *scope,
	unsafe bool,
) (Type, error) {
	field, ok := expr.Callee.(*ast.FieldExpr)
	if !ok {
		return "", fmt.Errorf("type error: invalid assignment target `%s`", expr.String())
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if receiver != "Partition" || field.Name != "at" {
		return "", fmt.Errorf("type error: invalid assignment target `%s`", expr.String())
	}
	return c.checkPartitionMethod(field.Name, expr.Args, env, unsafe)
}

// checkAssignableIdent validates direct binding assignment.
func checkAssignableIdent(expr *ast.IdentExpr, env *scope) (Type, error) {
	want, ok := env.lookup(expr.Name)
	if !ok {
		return "", fmt.Errorf("type error: undefined variable `%s`", expr.Name)
	}
	if !env.isMutable(expr.Name) {
		return "", fmt.Errorf("type error: cannot assign to immutable binding `%s`", expr.Name)
	}
	return want, nil
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
	if errorType, elem, ok := errorUnionParts(want); ok {
		if got == Type(elem) || got == Type(errorType) {
			return true, nil
		}
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
	leave, err := c.enterLoop(stmt.Label)
	if err != nil {
		return false, err
	}
	defer leave()
	_, err = c.checkBlock(stmt.Body, env.child(), wantReturn, unsafe)
	return false, err
}

// checkForStmt validates a bounded integer range loop.
func (c *Checker) checkForStmt(
	stmt *ast.ForStmt,
	env *scope,
	wantReturn Type,
	unsafe bool,
) (bool, error) {
	if err := c.checkForBounds(stmt, env, unsafe); err != nil {
		return false, err
	}
	leave, err := c.enterLoop(stmt.Label)
	if err != nil {
		return false, err
	}
	defer leave()
	child := env.child()
	if err := child.define(stmt.Name, typeI64, false); err != nil {
		return false, err
	}
	_, err = c.checkBlock(stmt.Body, child, wantReturn, unsafe)
	return false, err
}

// checkForBounds validates the start and end expressions for a range loop.
func (c *Checker) checkForBounds(stmt *ast.ForStmt, env *scope, unsafe bool) error {
	start, err := c.checkExpr(stmt.Start, env, unsafe)
	if err != nil {
		return err
	}
	end, err := c.checkExpr(stmt.End, env, unsafe)
	if err != nil {
		return err
	}
	if start != typeI64 || end != typeI64 {
		return fmt.Errorf("type error: for range expects i64 bounds, got %s..%s", start, end)
	}
	return nil
}

// enterLoop records an active loop label for branch target validation.
func (c *Checker) enterLoop(label string) (func(), error) {
	if label != "" && c.hasLoopLabel(label) {
		return nil, fmt.Errorf("type error: duplicate loop label `%s`", label)
	}
	c.loopLabels = append(c.loopLabels, label)
	return func() {
		c.loopLabels = c.loopLabels[:len(c.loopLabels)-1]
	}, nil
}

// checkLoopBranch validates break and continue placement.
func (c *Checker) checkLoopBranch(kind string, label string) error {
	if len(c.loopLabels) == 0 {
		return fmt.Errorf("type error: `%s` used outside loop", kind)
	}
	if label != "" && !c.hasLoopLabel(label) {
		return fmt.Errorf("type error: unknown loop label `%s`", label)
	}
	return nil
}

// hasLoopLabel reports whether label names an active loop.
func (c *Checker) hasLoopLabel(label string) bool {
	for idx := len(c.loopLabels) - 1; idx >= 0; idx-- {
		if c.loopLabels[idx] == label {
			return true
		}
	}
	return false
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
			return false, fmt.Errorf("type error: duplicate match tag `%s::%s`", typeName, arm.Tag)
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
			return "", fmt.Errorf("type error: unknown match tag `%s::%s`", enumType.name, arm.Tag)
		}
		if arm.Binding != "" {
			return "", fmt.Errorf("type error: enum tag `%s::%s` has no payload",
				enumType.name, arm.Tag)
		}
		return "", nil
	}
	payload, ok := unionType.variants[arm.Tag]
	if !ok {
		return "", fmt.Errorf("type error: unknown match tag `%s::%s`", unionType.name, arm.Tag)
	}
	if payload == "" && arm.Binding != "" {
		return "", fmt.Errorf("type error: union variant `%s::%s` has no payload",
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
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr:
		return literalType(e)
	case *ast.IfExpr:
		return c.checkIfExpr(e, env, unsafe)
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
	case *ast.IndexExpr:
		return c.checkIndexExpr(e, env, unsafe)
	case *ast.ArenaNewExpr:
		return c.checkArenaNewExpr(e)
	case *ast.StructLiteralExpr:
		return c.checkStructLiteralExpr(e, env, unsafe)
	case *ast.FieldExpr:
		return c.checkFieldExpr(e, env, unsafe)
	case *ast.DerefExpr:
		return c.checkDerefExpr(e, env, unsafe)
	default:
		return "", fmt.Errorf("type error: unsupported expression %T", expr)
	}
}

// checkIndexExpr validates checked one-dimensional byte indexing and slicing.
func (c *Checker) checkIndexExpr(expr *ast.IndexExpr, env *scope, unsafe bool) (Type, error) {
	target, err := c.checkExpr(expr.Target, env, unsafe)
	if err != nil {
		return "", err
	}
	if target != typeByteString {
		return "", fmt.Errorf("type error: index/slice target expects []const u8, got %s", target)
	}
	if !expr.Slice {
		if err := c.checkIndexBound("index", expr.Index, env, unsafe); err != nil {
			return "", err
		}
		return typeU8, nil
	}
	if expr.Start != nil {
		if err := c.checkIndexBound("slice start", expr.Start, env, unsafe); err != nil {
			return "", err
		}
	}
	if expr.End != nil {
		if err := c.checkIndexBound("slice end", expr.End, env, unsafe); err != nil {
			return "", err
		}
	}
	return typeByteString, nil
}

// checkIndexBound validates one i64 index or slice bound.
func (c *Checker) checkIndexBound(
	name string,
	expr ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if expr == nil {
		return fmt.Errorf("type error: %s is missing", name)
	}
	got, err := c.checkExpr(expr, env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return fmt.Errorf("type error: %s expects i64, got %s", name, got)
	}
	return nil
}

// literalType returns the static type of scalar literals.
func literalType(expr ast.Expression) (Type, error) {
	switch expr.(type) {
	case *ast.IntExpr:
		return typeI64, nil
	case *ast.StringExpr:
		return typeByteString, nil
	case *ast.BoolExpr:
		return typeBool, nil
	default:
		return "", fmt.Errorf("type error: unsupported literal %T", expr)
	}
}

// checkIfExpr validates a value-producing if expression.
func (c *Checker) checkIfExpr(expr *ast.IfExpr, env *scope, unsafe bool) (Type, error) {
	cond, err := c.checkExpr(expr.Condition, env, unsafe)
	if err != nil {
		return "", err
	}
	if cond != typeBool {
		return "", fmt.Errorf("type error: if expression condition must be bool, got %s", cond)
	}
	if expr.Alternative == nil {
		return "", fmt.Errorf("type error: if expression requires else branch")
	}
	left, err := c.checkIfExprBlock(expr.Consequence, env.child(), unsafe)
	if err != nil {
		return "", err
	}
	right, err := c.checkIfExprBlock(expr.Alternative, env.child(), unsafe)
	if err != nil {
		return "", err
	}
	if left != right {
		return "", fmt.Errorf("type error: if expression branch types differ: %s and %s",
			left, right)
	}
	return left, nil
}

// checkIfExprBlock validates statements before the final branch value.
func (c *Checker) checkIfExprBlock(block *ast.BlockStmt, env *scope, unsafe bool) (Type, error) {
	if block == nil || len(block.Statements) == 0 {
		return "", fmt.Errorf("type error: if expression branch must end with a value")
	}
	last := len(block.Statements) - 1
	for _, stmt := range block.Statements[:last] {
		returns, err := c.checkStmt(stmt, env, c.currentReturn, unsafe)
		if err != nil {
			return "", err
		}
		if returns {
			return "", fmt.Errorf("type error: if expression branch cannot return before value")
		}
	}
	exprStmt, ok := block.Statements[last].(*ast.ExprStmt)
	if !ok {
		return "", fmt.Errorf("type error: if expression branch must end with a value")
	}
	return c.checkExpr(exprStmt.Expr, env, unsafe)
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
	if expr.Operator == "&" || expr.Operator == "&mut" {
		typ, _, err := c.checkBorrowPrefix(expr, env, unsafe)
		return typ, err
	}
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

// checkBorrowPrefix validates an explicit local borrow expression.
func (c *Checker) checkBorrowPrefix(
	expr *ast.PrefixExpr,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	mutable := expr.Operator == "&mut"
	if mutable {
		if err := requireMutableBorrowArg(expr.Right, env); err != nil {
			return "", false, err
		}
	}
	if err := checkBorrowTargetShape(expr.Right); err != nil {
		return "", false, err
	}
	typ, err := c.checkExpr(expr.Right, env, unsafe)
	if err != nil {
		return "", false, err
	}
	return typ, mutable, nil
}

// checkBinaryExpr validates arithmetic, logical, equality, and comparison operators.
func (c *Checker) checkBinaryExpr(expr *ast.BinaryExpr, env *scope, unsafe bool) (Type, error) {
	left, err := c.checkExpr(expr.Left, env, unsafe)
	if err != nil {
		return "", err
	}
	right, err := c.checkExpr(expr.Right, env, unsafe)
	if err != nil {
		return "", err
	}
	if expr.Operator == "and" || expr.Operator == "or" {
		return checkLogical(expr.Operator, left, right)
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

// checkLogical validates boolean logical operands.
func checkLogical(op string, left Type, right Type) (Type, error) {
	if left != typeBool || right != typeBool {
		return "", fmt.Errorf("type error: operator `%s` expects bool operands", op)
	}
	return typeBool, nil
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
	if _, _, ok := errorUnionParts(c.currentReturn); !ok {
		return "", fmt.Errorf("type error: try requires function to return !T")
	}
	source, err := c.checkExpr(expr.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	sourceError, elem, ok := errorUnionParts(source)
	if !ok {
		return "", fmt.Errorf("type error: try expects !T, got %s", source)
	}
	targetError, _, _ := errorUnionParts(c.currentReturn)
	if sourceError != targetError {
		return "", fmt.Errorf("type error: try cannot propagate %s from %s", sourceError, source)
	}
	return Type(elem), nil
}

// checkCallExpr validates builtin and user function calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope, unsafe bool) (Type, error) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return c.checkFieldCallExpr(field, expr.Args, env, unsafe)
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return c.checkTypeApplyCallExpr(typeApply, expr.Args, env, unsafe)
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
		return "", fmt.Errorf("type error: use `std::io::blocking()`")
	}
	if name.Name == "TaskGroup" {
		return "", fmt.Errorf("type error: use `std::task::Group(io)`")
	}
	return c.checkUserCall(name.Name, expr.Args, env, unsafe)
}

// checkFieldCallExpr validates qualified, union, and method calls.
func (c *Checker) checkFieldCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if typ, ok, err := c.checkUnionConstructorCall(field, args, env, unsafe); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkQualifiedUserCall(field, args, env, unsafe); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkQualifiedBuiltin(field, args, env, unsafe); ok || err != nil {
		return typ, err
	}
	return c.checkMethodCallExpr(field, args, env, unsafe)
}

// checkQualifiedUserCall validates module-qualified functions loaded from source.
func (c *Checker) checkQualifiedUserCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	if _, ok := c.functions[name]; !ok {
		return "", false, nil
	}
	typ, err := c.checkUserCall(name, args, env, unsafe)
	return typ, true, err
}

// checkQualifiedBuiltin validates std:: namespace prototype calls.
func (c *Checker) checkQualifiedBuiltin(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	if typ, ok, err := c.checkStdCoreBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	return c.checkStdRuntimeBuiltin(name, args, env, unsafe)
}

// checkStdCoreBuiltin validates pure, filesystem, I/O, and process std calls.
func (c *Checker) checkStdCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if typ, ok, err := c.checkFsBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkMemBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkIoBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkProcessBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// checkStdRuntimeBuiltin validates task, testing, and constructor std calls.
func (c *Checker) checkStdRuntimeBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if typ, ok, err := c.checkTaskBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkTestingBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkStringStorageBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	return c.checkStdConstructorBuiltin(name, args, env, unsafe)
}

// checkTestingBuiltin validates minimal std::testing assertion helpers.
func (c *Checker) checkTestingBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.builtin.testing_expect":
		return c.checkTestingArgs(name, args, env, unsafe, typeBool)
	case "std.builtin.testing_expect_equal_i64":
		return c.checkTestingArgs(name, args, env, unsafe, typeI64, typeI64)
	case "std.builtin.testing_expect_equal_bool":
		return c.checkTestingArgs(name, args, env, unsafe, typeBool, typeBool)
	case "std.builtin.testing_expect_equal_bytes":
		return c.checkTestingArgs(name, args, env, unsafe, typeByteString, typeByteString)
	case "std.builtin.testing_fail":
		return c.checkTestingArgs(name, args, env, unsafe, typeByteString)
	default:
		return "", false, nil
	}
}

// checkTestingArgs validates assertion argument types and returns !void.
func (c *Checker) checkTestingArgs(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	want ...Type,
) (Type, bool, error) {
	if len(args) != len(want) {
		return "", true, fmt.Errorf("type error: `%s` expects %d args, got %d",
			name, len(want), len(args))
	}
	for idx, arg := range args {
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return "", true, err
		}
		if got != want[idx] {
			return "", true, fmt.Errorf("type error: `%s` arg %d expects %s, got %s",
				name, idx+1, want[idx], got)
		}
	}
	return "!void", true, nil
}

// checkStdConstructorBuiltin validates miscellaneous std constructor calls.
func (c *Checker) checkStdConstructorBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.builtin.io_blocking", "std.builtin.io_threaded", "std.builtin.io_failing":
		typ, err := checkNoArgConstructor(name, args, "Io")
		return typ, true, err
	case "std.io.evented", "std.builtin.io_evented":
		return "", true, fmt.Errorf("type error: `std::io::evented` is not implemented in v0.1")
	case "std.array.Array":
		return "", true, fmt.Errorf("type error: use `std::array::Array<T>(allocator)`")
	case "std.map.Map":
		return "", true, fmt.Errorf("type error: use `std::map::Map<K, V>(allocator)`")
	case "std.string.String":
		return c.checkStringConstructor(args, env, unsafe)
	case "std.channel.Channel":
		return "", true, fmt.Errorf("type error: use `std::channel::Channel<T>()`")
	case "std.thread.scoped":
		return c.checkThreadScoped(args, env, unsafe)
	case "std.atomic.Atomic":
		return "", true, fmt.Errorf("type error: use `std::atomic::Atomic<T>(value)`")
	case "std.atomic.AtomicI64":
		return "", true, fmt.Errorf("type error: use `std::atomic::Atomic<i64>(value)`")
	case "std.sync.Mutex":
		return "", true, fmt.Errorf("type error: use `std::sync::Mutex<T>(value)`")
	default:
		return "", false, nil
	}
}

// checkIoBuiltin validates explicit-Io stdio helpers.
func (c *Checker) checkIoBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.builtin.io_write_stdout", "std.builtin.io_write_stderr":
		return c.checkIoBytesCall(name, args, env, unsafe)
	case "std.builtin.io_read_stdin":
		return c.checkIoOnlyCall(name, args, env, unsafe, "![]const u8")
	default:
		return "", false, nil
	}
}

// checkIoBytesCall validates an Io plus []const u8 call.
func (c *Checker) checkIoBytesCall(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("type error: `%s` expects io and bytes", name)
	}
	if err := c.checkIoArg(args[0], env, unsafe, name); err != nil {
		return "", true, err
	}
	got, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != typeByteString {
		return "", true, fmt.Errorf("type error: `%s` expects []const u8 bytes, got %s", name, got)
	}
	return "!void", true, nil
}

// checkIoOnlyCall validates a call that only takes Io.
func (c *Checker) checkIoOnlyCall(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	result Type,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `%s` expects io", name)
	}
	if err := c.checkIoArg(args[0], env, unsafe, name); err != nil {
		return "", true, err
	}
	return result, true, nil
}

// checkProcessBuiltin validates minimal process helpers for CLI prototypes.
func (c *Checker) checkProcessBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.builtin.process_arg_count":
		typ, err := checkNoArgConstructor(name, args, "i64")
		return typ, true, err
	case "std.builtin.process_arg":
		return c.checkOneI64Arg(name, args, env, unsafe, "![]const u8")
	case "std.builtin.process_env":
		return c.checkOneBytesArg(name, args, env, unsafe, "![]const u8")
	case "std.builtin.process_exit_code":
		return c.checkOneI64Arg(name, args, env, unsafe, "i64")
	default:
		return "", false, nil
	}
}

// checkOneI64Arg validates one i64 argument.
func (c *Checker) checkOneI64Arg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	result Type,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `%s` expects i64", name)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != typeI64 {
		return "", true, fmt.Errorf("type error: `%s` expects i64, got %s", name, got)
	}
	return result, true, nil
}

// checkOneBytesArg validates one []const u8 argument.
func (c *Checker) checkOneBytesArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	result Type,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `%s` expects []const u8", name)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != typeByteString {
		return "", true, fmt.Errorf("type error: `%s` expects []const u8, got %s", name, got)
	}
	return result, true, nil
}

// checkStringConstructor validates std::string::String(allocator).
func (c *Checker) checkStringConstructor(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `std::string::String` expects allocator")
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Allocator" {
		return "", true, fmt.Errorf("type error: `std::string::String` expects Allocator, got %s",
			got)
	}
	return "std::string::String", true, nil
}

// checkMemBuiltin validates allocation-free std::mem byte-slice helpers.
func (c *Checker) checkMemBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.builtin.mem_page_allocator":
		typ, err := checkNoArgConstructor(name, args, "Allocator")
		return typ, true, err
	case "std.builtin.mem_len":
		return c.checkMemByteArgs(name, args, env, unsafe, 1, typeI64)
	case "std.builtin.mem_byte_at":
		return c.checkMemByteIndex(name, args, env, unsafe, "!u8")
	case "std.builtin.mem_slice":
		return c.checkMemSlice(args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkMemByteArgs validates std::mem calls that only take byte slices.
func (c *Checker) checkMemByteArgs(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	want int,
	result Type,
) (Type, bool, error) {
	if len(args) != want {
		return "", true, fmt.Errorf("type error: `%s` expects %d args, got %d",
			name, want, len(args))
	}
	for idx, arg := range args {
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return "", true, err
		}
		if got != typeByteString {
			return "", true, fmt.Errorf("type error: `%s` arg %d expects []const u8, got %s",
				name, idx+1, got)
		}
	}
	return result, true, nil
}

// checkMemByteIndex validates std::mem helpers that read one byte by index.
func (c *Checker) checkMemByteIndex(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	result Type,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("type error: `%s` expects bytes and index", name)
	}
	if got, err := c.checkExpr(args[0], env, unsafe); err != nil {
		return "", true, err
	} else if got != typeByteString {
		return "", true, fmt.Errorf("type error: `%s` expects []const u8 bytes, got %s", name, got)
	}
	got, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != typeI64 {
		return "", true, fmt.Errorf("type error: `%s` expects i64 index, got %s", name, got)
	}
	return result, true, nil
}

// checkMemSlice validates recoverable slicing of []const u8.
func (c *Checker) checkMemSlice(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if err := c.checkMemSliceShape("std.builtin.mem_slice", args, env, unsafe); err != nil {
		return "", true, err
	}
	return "![]const u8", true, nil
}

// checkMemSliceShape validates bytes, start, and end/index arguments.
func (c *Checker) checkMemSliceShape(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != 3 {
		return fmt.Errorf("type error: `%s` expects bytes, start, and end", name)
	}
	if got, err := c.checkExpr(args[0], env, unsafe); err != nil {
		return err
	} else if got != typeByteString {
		return fmt.Errorf("type error: `%s` expects []const u8 bytes, got %s", name, got)
	}
	for idx, label := range []string{"start", "end"} {
		got, err := c.checkExpr(args[idx+1], env, unsafe)
		if err != nil {
			return err
		}
		if got != typeI64 {
			return fmt.Errorf("type error: `%s` expects i64 %s, got %s", name, label, got)
		}
	}
	return nil
}

// checkFsBuiltin validates std::fs calls with explicit Io.
func (c *Checker) checkFsBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.fs.read_file":
		return c.checkFsReadFile(args, env, unsafe)
	case "std.fs.write_file":
		return c.checkFsWriteFile(args, env, unsafe)
	case "std.fs.exists":
		return c.checkFsExists(args, env, unsafe)
	case "std.fs.metadata":
		return c.checkFsMetadata(args, env, unsafe)
	case "std.fs.create_dir", "std.fs.remove_dir", "std.fs.remove_file":
		return c.checkFsPathOnly(name, args, env, unsafe, "!void")
	default:
		return "", false, nil
	}
}

// checkTaskBuiltin validates structured task and data-parallel std calls.
func (c *Checker) checkTaskBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.task.Group":
		return c.checkTaskGroup(args, env, unsafe)
	case "std.task.Queue":
		typ, err := checkNoArgConstructor(name, args, "Queue")
		return typ, true, err
	case "std.task.partition_mut":
		return c.checkPartitionMut(args, env, unsafe)
	case "std.task.LocalBuffer":
		return c.checkLocalBuffer(args, env, unsafe)
	case "std.task.parallel_for":
		return c.checkParallelFor(args, env, unsafe)
	case "std.task.parallel_map":
		return c.checkParallelMap(args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkFsReadFile validates std::fs::read_file.
func (c *Checker) checkFsReadFile(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("type error: `std::fs::read_file` expects io and path")
	}
	if err := c.checkIoArg(args[0], env, unsafe, "std::fs::read_file"); err != nil {
		return "", true, err
	}
	path, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if path != typeByteString {
		return "", true, fmt.Errorf("type error: `std::fs::read_file` expects []const u8 path, got %s",
			path)
	}
	return "![]const u8", true, nil
}

// checkFsWriteFile validates std::fs::write_file.
func (c *Checker) checkFsWriteFile(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 3 {
		return "", true, fmt.Errorf("type error: `std::fs::write_file` expects io, path, and bytes")
	}
	if err := c.checkIoArg(args[0], env, unsafe, "std::fs::write_file"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"path", "bytes"} {
		got, err := c.checkExpr(args[idx+1], env, unsafe)
		if err != nil {
			return "", true, err
		}
		if got != typeByteString {
			return "", true, fmt.Errorf(
				"type error: `std::fs::write_file` expects []const u8 %s, got %s", label, got)
		}
	}
	return "!void", true, nil
}

// checkFsExists validates std::fs::exists.
func (c *Checker) checkFsExists(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::exists", args, env, unsafe)
	return "!bool", true, err
}

// checkFsMetadata validates std::fs::metadata.
func (c *Checker) checkFsMetadata(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::metadata", args, env, unsafe)
	return "!std::fs::Metadata", true, err
}

// checkFsPathOnly validates an Io plus path API and returns result.
func (c *Checker) checkFsPathOnly(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	result Type,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs(name, args, env, unsafe)
	return result, true, err
}

// checkFsPathArgs validates common std::fs Io and path arguments.
func (c *Checker) checkFsPathArgs(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, Type, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("type error: `%s` expects io and path", name)
	}
	if err := c.checkIoArg(args[0], env, unsafe, name); err != nil {
		return "", "", err
	}
	path, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", "", err
	}
	if path != typeByteString {
		return "", "", fmt.Errorf("type error: `%s` expects []const u8 path, got %s", name, path)
	}
	return "Io", path, nil
}

// checkArrayConstructor validates std::array::Array<T>(allocator).
func (c *Checker) checkArrayConstructor(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if err := c.rejectArrayElementType(elem); err != nil {
		return "", true, err
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `std::array::Array<%s>` expects allocator", elem)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Allocator" {
		return "", true, fmt.Errorf("type error: `std::array::Array<%s>` expects Allocator, got %s",
			elem, got)
	}
	return Type(fmt.Sprintf("std::array::Array<%s>", elem)), true, nil
}

// rejectArrayElementType rejects element types that need unresolved lifetime rules.
func (c *Checker) rejectArrayElementType(elem Type) error {
	if err := c.rejectArrayStorageType(elem, map[Type]bool{}); err != nil {
		return fmt.Errorf("type error: Array element is not safe in v0.2: %w", err)
	}
	return nil
}

// rejectArrayStorageType rejects values whose lifetime rules are not Array-safe yet.
func (c *Checker) rejectArrayStorageType(typ Type, seen map[Type]bool) error {
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	if isPointerType(typ) {
		return fmt.Errorf("type error: Array element cannot be raw pointer in v0.2")
	}
	if err := c.rejectArrayStorageGeneric(typ, seen); err != nil {
		return err
	}
	if err := c.rejectArrayStorageStruct(typ, seen); err != nil {
		return err
	}
	return c.rejectArrayStorageUnion(typ, seen)
}

// rejectArrayStorageGeneric applies Array-specific generic exclusions.
func (c *Checker) rejectArrayStorageGeneric(typ Type, seen map[Type]bool) error {
	base, arg, ok := splitGenericType(string(typ))
	if !ok {
		return nil
	}
	switch base {
	case "arena":
		return fmt.Errorf("type error: Array element cannot be arena in v0.2")
	case "handle":
		return fmt.Errorf("type error: Array element cannot be handle in v0.2")
	case "std::array::Array":
		return fmt.Errorf("type error: Array element cannot be nested array in v0.2")
	case "std::map::Map":
		return fmt.Errorf("type error: Array element cannot be std::map::Map in v0.2")
	case "Task", "Channel", "Mutex", "Atomic", "Dyn":
		return fmt.Errorf("type error: Array element cannot be %s in v0.2", base)
	case "option":
		argType, err := c.parseType(arg)
		if err != nil {
			return err
		}
		return c.rejectArrayStorageType(argType, seen)
	default:
		return nil
	}
}

// rejectArrayStorageStruct checks struct fields recursively for Array storage.
func (c *Checker) rejectArrayStorageStruct(typ Type, seen map[Type]bool) error {
	decl := c.structs[string(typ)]
	if decl == nil {
		return nil
	}
	for _, field := range decl.Fields {
		fieldType, err := c.parseType(field.TypeName)
		if err != nil {
			return err
		}
		if err := c.rejectArrayStorageType(fieldType, seen); err != nil {
			return fmt.Errorf("type error: struct `%s.%s` cannot be Array element: %w",
				typ, field.Name, err)
		}
	}
	return nil
}

// rejectArrayStorageUnion checks union payloads recursively for Array storage.
func (c *Checker) rejectArrayStorageUnion(typ Type, seen map[Type]bool) error {
	union := c.unions[string(typ)]
	if union == nil {
		return nil
	}
	for variant, payload := range union.variants {
		if payload == "" {
			continue
		}
		payloadType, err := c.parseType(payload)
		if err != nil {
			return err
		}
		if err := c.rejectArrayStorageType(payloadType, seen); err != nil {
			return fmt.Errorf("type error: union `%s::%s` cannot be Array element: %w",
				typ, variant, err)
		}
	}
	return nil
}

// checkIoArg validates an explicit Io argument for a std call.
func (c *Checker) checkIoArg(arg ast.Expression, env *scope, unsafe bool, name string) error {
	got, err := c.checkExpr(arg, env, unsafe)
	if err != nil {
		return err
	}
	if got != "Io" {
		return fmt.Errorf("type error: `%s` expects Io, got %s", name, got)
	}
	return nil
}

// checkTypeApplyCallExpr validates typed std constructor calls.
func (c *Checker) checkTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return "", fmt.Errorf("type error: unsupported type application `%s`", expr.String())
	}
	switch name {
	case "std.array.Array":
		arg, err := c.parseType(expr.TypeArg)
		if err != nil {
			return "", err
		}
		typ, _, err := c.checkArrayConstructor(arg, args, env, unsafe)
		return typ, err
	case "std.map.Map":
		mapArgs, err := c.checkedMapArgs(expr.TypeArg)
		if err != nil {
			return "", err
		}
		typ, _, err := c.checkMapConstructor(Type(mapArgs[1]), args, env, unsafe)
		return typ, err
	case "std.channel.Channel":
		arg, err := c.parseType(expr.TypeArg)
		if err != nil {
			return "", err
		}
		return checkNoArgConstructor(name, args, Type(fmt.Sprintf("Channel<%s>", arg)))
	case "std.atomic.Atomic":
		arg, err := c.parseType(expr.TypeArg)
		if err != nil {
			return "", err
		}
		typ, _, err := c.checkAtomic(arg, args, env, unsafe)
		return typ, err
	case "std.sync.Mutex":
		arg, err := c.parseType(expr.TypeArg)
		if err != nil {
			return "", err
		}
		typ, _, err := c.checkMutex(arg, args, env, unsafe)
		return typ, err
	default:
		return "", fmt.Errorf("type error: `%s` does not take a type argument", name)
	}
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
	errorType, _, ok := errorUnionParts(c.currentReturn)
	if !ok {
		return "", fmt.Errorf("type error: `error` requires function to return !T")
	}
	if errorType != "" {
		return "", fmt.Errorf("type error: `error` cannot construct typed error %s", errorType)
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
		if fn.mutBorrowParams[idx] {
			if err := requireMutableBorrowArg(arg, env); err != nil {
				return "", err
			}
		}
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
	if !field.Namespace {
		if enumType, ok := enumReceiver(field.Receiver, c.enums); ok {
			return "", true, fmt.Errorf("type error: enum tag `%s.%s` must use `::`",
				enumType.name, field.Name)
		}
		if unionType, ok := unionReceiver(field.Receiver, c.unions); ok {
			return "", true, fmt.Errorf("type error: union variant `%s.%s` must use `::`",
				unionType.name, field.Name)
		}
		return "", false, nil
	}
	unionType, ok := unionReceiver(field.Receiver, c.unions)
	if !ok {
		return "", false, nil
	}
	payload, exists := unionType.variants[field.Name]
	if !exists {
		return "", true, fmt.Errorf("type error: unknown union variant `%s::%s`",
			unionType.name, field.Name)
	}
	if payload == "" {
		return "", true, fmt.Errorf("type error: union variant `%s::%s` expects 0 args",
			unionType.name, field.Name)
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: union variant `%s::%s` expects 1 arg, got %d",
			unionType.name, field.Name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != Type(payload) {
		return "", true, fmt.Errorf("type error: union variant `%s::%s` expects %s, got %s",
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
	if expr.Namespace {
		return c.checkNamespaceExpr(expr)
	}
	if enumType, ok := enumReceiver(expr.Receiver, c.enums); ok {
		return "", fmt.Errorf("type error: enum tag `%s.%s` must use `::`",
			enumType.name, expr.Name)
	}
	if unionType, ok := unionReceiver(expr.Receiver, c.unions); ok {
		return "", fmt.Errorf("type error: union variant `%s.%s` must use `::`",
			unionType.name, expr.Name)
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if receiver == "std::fs::Metadata" {
		return checkFsMetadataField(expr.Name)
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

// checkFsMetadataField returns builtin metadata field types.
func checkFsMetadataField(name string) (Type, error) {
	switch name {
	case "size":
		return typeI64, nil
	case "is_dir":
		return typeBool, nil
	default:
		return "", fmt.Errorf("type error: unknown field `std::fs::Metadata.%s`", name)
	}
}

// checkNamespaceExpr returns the type of enum or payload-free union namespace lookup.
func (c *Checker) checkNamespaceExpr(expr *ast.FieldExpr) (Type, error) {
	if enumType, ok := enumReceiver(expr.Receiver, c.enums); ok {
		if !enumType.tags[expr.Name] {
			return "", fmt.Errorf("type error: unknown enum tag `%s::%s`",
				enumType.name, expr.Name)
		}
		return Type(enumType.name), nil
	}
	if unionType, ok := unionReceiver(expr.Receiver, c.unions); ok {
		payload, exists := unionType.variants[expr.Name]
		if !exists {
			return "", fmt.Errorf("type error: unknown union variant `%s::%s`",
				unionType.name, expr.Name)
		}
		if payload != "" {
			return "", fmt.Errorf("type error: union variant `%s::%s` expects payload",
				unionType.name, expr.Name)
		}
		return Type(unionType.name), nil
	}
	return "", fmt.Errorf("type error: unknown namespace `%s`", expr.Receiver.String())
}

// checkDerefExpr returns the value type behind a local borrow parameter.
func (c *Checker) checkDerefExpr(expr *ast.DerefExpr, env *scope, unsafe bool) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && env.isBorrowed(ident.Name) {
		typ, _ := env.lookup(ident.Name)
		return typ, nil
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("type error: `%s` is not a borrow and cannot be dereferenced", receiver)
}

// checkAssignableField validates mutation of a field on a mutable value.
func (c *Checker) checkAssignableField(expr *ast.FieldExpr, env *scope, unsafe bool) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && !env.isMutable(ident.Name) {
		return "", fmt.Errorf("type error: cannot assign field of immutable binding `%s`", ident.Name)
	}
	if _, ok := expr.Receiver.(*ast.DerefExpr); ok {
		if _, err := c.checkAssignableDeref(expr.Receiver.(*ast.DerefExpr), env, unsafe); err != nil {
			return "", err
		}
	}
	return c.checkFieldExpr(expr, env, unsafe)
}

// checkAssignableDeref validates mutation through an &mut local borrow.
func (c *Checker) checkAssignableDeref(expr *ast.DerefExpr, env *scope, unsafe bool) (Type, error) {
	ident, ok := expr.Receiver.(*ast.IdentExpr)
	if !ok || !env.isMutBorrowed(ident.Name) {
		return "", fmt.Errorf("type error: `%s` is not a mutable borrow", expr.Receiver.String())
	}
	return c.checkDerefExpr(expr, env, unsafe)
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
	typ, ok, err := c.checkKnownReceiverMethod(field, receiver, args, env, unsafe)
	if ok || err != nil {
		return typ, err
	}
	return c.checkArenaOrImplMethod(field, receiver, args, env, unsafe)
}

// checkKnownReceiverMethod validates non-arena builtin receiver families.
func (c *Checker) checkKnownReceiverMethod(
	field *ast.FieldExpr,
	receiver Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if contractName, ok := dynContract(receiver); ok {
		typ, err := c.checkDynMethodCall(contractName, field.Name, args, env, unsafe)
		return typ, true, err
	}
	if receiver == "TaskGroup" {
		typ, err := c.checkTaskGroupMethod(field.Name, args, env, unsafe)
		return typ, true, err
	}
	if elem, ok := taskElement(receiver); ok {
		typ, err := checkTaskMethod(field.Name, elem, args)
		return typ, true, err
	}
	if typ, ok, err := c.checkConcurrencyMethod(
		receiver,
		field.Name,
		args,
		env,
		unsafe,
	); ok || err != nil {
		return typ, ok, err
	}
	base, arg, ok := splitGenericType(string(receiver))
	if ok && base == "std::array::Array" {
		typ, err := c.checkArrayReceiverMethod(field, Type(arg), args, env, unsafe)
		return typ, true, err
	}
	if ok && base == "std::map::Map" {
		typ, err := c.checkMapReceiverMethod(field, arg, args, env, unsafe)
		return typ, true, err
	}
	if receiver == "std::string::String" {
		typ, err := c.checkStringReceiverMethod(field, args, env, unsafe)
		return typ, true, err
	}
	return "", false, nil
}

// checkArenaOrImplMethod validates arena methods or user-defined impl methods.
func (c *Checker) checkArenaOrImplMethod(
	field *ast.FieldExpr,
	receiver Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
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

// checkStringStorageBuiltin validates trusted String storage primitives.
func (c *Checker) checkStringStorageBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	switch name {
	case "std.builtin.string_append_bytes":
		return c.checkStringStorageBytes(name, args, env, unsafe)
	case "std.builtin.string_append_byte":
		return c.checkStringStorageByte(name, args, env, unsafe)
	case "std.builtin.string_reserve":
		return c.checkStringStorageReserve(name, args, env, unsafe)
	case "std.builtin.string_truncate":
		return c.checkStringStorageTruncate(name, args, env, unsafe)
	case "std.builtin.string_len", "std.builtin.string_capacity":
		return c.checkStringStorageNoArgResult(name, args, env, unsafe, typeI64)
	case "std.builtin.string_as_bytes":
		return c.checkStringStorageNoArgResult(name, args, env, unsafe, typeByteString)
	case "std.builtin.string_clear", "std.builtin.string_deinit":
		return c.checkStringStorageNoArgResult(name, args, env, unsafe, typeVoid)
	default:
		return "", false, nil
	}
}

// checkStringStorageBytes validates String plus []const u8 primitive calls.
func (c *Checker) checkStringStorageBytes(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if err := c.checkStringStorageArgs(name, args, env, unsafe, typeByteString); err != nil {
		return "", true, err
	}
	return "!void", true, nil
}

// checkStringStorageByte validates String plus u8 primitive calls.
func (c *Checker) checkStringStorageByte(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if err := c.checkStringStorageArgs(name, args, env, unsafe, typeU8); err != nil {
		return "", true, err
	}
	return "!void", true, nil
}

// checkStringStorageReserve validates String plus i64 primitive calls.
func (c *Checker) checkStringStorageReserve(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if err := c.checkStringStorageArgs(name, args, env, unsafe, typeI64); err != nil {
		return "", true, err
	}
	return "!void", true, nil
}

// checkStringStorageTruncate validates String plus i64 primitive calls.
func (c *Checker) checkStringStorageTruncate(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if err := c.checkStringStorageArgs(name, args, env, unsafe, typeI64); err != nil {
		return "", true, err
	}
	return "!void", true, nil
}

// checkStringStorageNoArgResult validates String-only primitive calls.
func (c *Checker) checkStringStorageNoArgResult(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	result Type,
) (Type, bool, error) {
	if err := c.checkStringStorageArgs(name, args, env, unsafe); err != nil {
		return "", true, err
	}
	return result, true, nil
}

// checkStringStorageArgs validates trusted primitive String arguments.
func (c *Checker) checkStringStorageArgs(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
	extra ...Type,
) error {
	if len(args) != 1+len(extra) {
		return fmt.Errorf("type error: `%s` expects %d args", name, 1+len(extra))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != "std::string::String" {
		return fmt.Errorf("type error: `%s` expects String, got %s", name, got)
	}
	for idx, want := range extra {
		got, err := c.checkExpr(args[idx+1], env, unsafe)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("type error: `%s` arg %d expects %s, got %s",
				name, idx+2, want, got)
		}
	}
	return nil
}

// checkStringReceiverMethod validates receiver-sensitive String methods.
func (c *Checker) checkStringReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if field.Name == "deinit" && ok && env.isBorrowed(ident.Name) {
		return "", fmt.Errorf("type error: `String.deinit` requires owned String receiver")
	}
	if isStringMutatingMethod(field.Name) && ok &&
		env.isBorrowed(ident.Name) && !env.isMutBorrowed(ident.Name) {
		return "", fmt.Errorf("type error: `String.%s` requires mutable String receiver", field.Name)
	}
	return c.checkStringMethod(field.Name, args, env, unsafe)
}

// checkStringMethod validates owned String prototype methods.
func (c *Checker) checkStringMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch name {
	case "append_bytes":
		if err := c.checkStringBytesArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "append_byte":
		if err := c.checkStringByteArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "reserve":
		if err := c.checkStringReserveArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "len", "capacity":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `String.%s` expects 0 args, got %d", name, len(args))
		}
		return typeI64, nil
	case "as_bytes":
		return "", fmt.Errorf(
			"type error: `String.as_bytes` must be bound with `let name = string.as_bytes()`")
	case "truncate":
		if err := c.checkStringReserveArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "clear", "deinit":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `String.%s` expects 0 args, got %d", name, len(args))
		}
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: String has no method `%s`", name)
	}
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

// checkStringBytesArg validates a []const u8 String method argument.
func (c *Checker) checkStringBytesArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != 1 {
		return fmt.Errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeByteString {
		return fmt.Errorf("type error: `String.%s` expects []const u8, got %s", name, got)
	}
	return nil
}

// checkStringReserveArg validates a reserve additional byte count.
func (c *Checker) checkStringReserveArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != 1 {
		return fmt.Errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return fmt.Errorf("type error: `String.%s` expects i64, got %s", name, got)
	}
	return nil
}

// checkStringByteArg validates a u8 String method argument.
func (c *Checker) checkStringByteArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != 1 {
		return fmt.Errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != "u8" {
		return fmt.Errorf("type error: `String.%s` expects u8, got %s", name, got)
	}
	return nil
}

// checkArrayReceiverMethod validates receiver-sensitive Array<T> methods.
func (c *Checker) checkArrayReceiverMethod(
	field *ast.FieldExpr,
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if field.Name == "at_mut" {
		if ident, ok := field.Receiver.(*ast.IdentExpr); !ok || !env.isMutable(ident.Name) {
			return "", fmt.Errorf("type error: `Array.at_mut` requires mutable array binding")
		}
	}
	return c.checkArrayMethod(elem, field.Name, args, env, unsafe)
}

// checkMapReceiverMethod validates receiver-sensitive Map<K, V> methods.
func (c *Checker) checkMapReceiverMethod(
	field *ast.FieldExpr,
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if err := checkMapReceiverBorrow(field, env); err != nil {
		return "", err
	}
	mapArgs, err := c.checkedMapArgs(arg)
	if err != nil {
		return "", err
	}
	return c.checkMapMethod(Type(mapArgs[1]), field.Name, args, env, unsafe)
}

// checkMapReceiverBorrow rejects Map methods whose receiver cannot be tracked safely.
func checkMapReceiverBorrow(field *ast.FieldExpr, env *scope) error {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	if field.Name == "deinit" && env.isBorrowed(ident.Name) {
		return fmt.Errorf("type error: `Map.deinit` requires owned Map receiver")
	}
	if field.Name == "insert" && env.isBorrowed(ident.Name) && !env.isMutBorrowed(ident.Name) {
		return fmt.Errorf("type error: `Map.insert` requires mutable Map receiver")
	}
	return nil
}

// checkConcurrencyMethod validates std concurrency prototype instance methods.
func (c *Checker) checkConcurrencyMethod(
	receiver Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	base, arg, generic := splitGenericType(string(receiver))
	if generic {
		switch base {
		case "Channel":
			typ, err := c.checkChannelMethod(Type(arg), name, args, env, unsafe)
			return typ, true, err
		case "Mutex":
			typ, err := c.checkMutexMethod(Type(arg), name, args)
			return typ, true, err
		case "Atomic":
			typ, err := c.checkAtomicMethod(Type(arg), name, args, env, unsafe)
			return typ, true, err
		}
	}
	switch receiver {
	case "Queue":
		typ, err := c.checkQueueMethod(name, args, env, unsafe)
		return typ, true, err
	case "Partition":
		typ, err := c.checkPartitionMethod(name, args, env, unsafe)
		return typ, true, err
	case "LocalBuffer":
		typ, err := c.checkLocalBufferMethod(name, args, env, unsafe)
		return typ, true, err
	default:
		return "", false, nil
	}
}

// checkArrayMethod validates owned Array<T> prototype methods.
func (c *Checker) checkArrayMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch name {
	case "append":
		if len(args) != 1 {
			return "", fmt.Errorf("type error: `Array.append` expects 1 arg, got %d", len(args))
		}
		got, err := c.checkExpr(args[0], env, unsafe)
		if err != nil {
			return "", err
		}
		if got != elem {
			return "", fmt.Errorf("type error: `Array.append` expects %s, got %s", elem, got)
		}
		return "!void", nil
	case "len", "capacity":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `Array.%s` expects 0 args, got %d", name, len(args))
		}
		return typeI64, nil
	case "get":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		if !c.isCopyType(elem) {
			return "", fmt.Errorf("type error: `Array.get` requires copy element in v0.2")
		}
		return Type("!" + string(elem)), nil
	case "at", "at_mut":
		return "", fmt.Errorf("type error: `Array.%s` must be bound with `let name = try array.%s(...)`",
			name, name)
	case "set":
		return c.checkArraySet(elem, args, env, unsafe)
	case "deinit":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `Array.deinit` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: Array has no method `%s`", name)
	}
}

// checkArrayIndexArg validates one i64 index argument.
func (c *Checker) checkArrayIndexArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != 1 {
		return fmt.Errorf("type error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return fmt.Errorf("type error: `Array.%s` expects i64 index, got %s", name, got)
	}
	return nil
}

// checkArraySet validates checked element replacement.
func (c *Checker) checkArraySet(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(args) != 2 {
		return "", fmt.Errorf("type error: `Array.set` expects 2 args, got %d", len(args))
	}
	if got, err := c.checkExpr(args[0], env, unsafe); err != nil {
		return "", err
	} else if got != typeI64 {
		return "", fmt.Errorf("type error: `Array.set` expects i64 index, got %s", got)
	}
	got, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", fmt.Errorf("type error: `Array.set` expects %s value, got %s", elem, got)
	}
	return "!void", nil
}

// checkMapConstructor validates std::map::Map<[]const u8, V>(allocator).
func (c *Checker) checkMapConstructor(
	valueType Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `std::map::Map` expects allocator")
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Allocator" {
		return "", true, fmt.Errorf("type error: `std::map::Map` expects Allocator, got %s", got)
	}
	return Type(fmt.Sprintf("std::map::Map<[]const u8, %s>", valueType)), true, nil
}

// checkMapMethod validates owned Map<[]const u8, V> prototype methods.
func (c *Checker) checkMapMethod(
	valueType Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch name {
	case "insert":
		return c.checkMapInsert(valueType, args, env, unsafe)
	case "get":
		if err := c.checkMapKeyArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("!" + string(valueType)), nil
	case "contains":
		if err := c.checkMapKeyArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return typeBool, nil
	case "len":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `Map.len` expects 0 args, got %d", len(args))
		}
		return typeI64, nil
	case "deinit":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `Map.deinit` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: Map has no method `%s`", name)
	}
}

// checkMapInsert validates copy-only Map.insert arguments.
func (c *Checker) checkMapInsert(
	valueType Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if len(args) != 2 {
		return "", fmt.Errorf("type error: `Map.insert` expects 2 args, got %d", len(args))
	}
	if got, err := c.checkExpr(args[0], env, unsafe); err != nil {
		return "", err
	} else if got != typeByteString {
		return "", fmt.Errorf("type error: `Map.insert` expects []const u8 key, got %s", got)
	}
	got, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != valueType {
		return "", fmt.Errorf("type error: `Map.insert` expects %s value, got %s", valueType, got)
	}
	return "!void", nil
}

// checkMapKeyArg validates one []const u8 lookup key.
func (c *Checker) checkMapKeyArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(args) != 1 {
		return fmt.Errorf("type error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeByteString {
		return fmt.Errorf("type error: `Map.%s` expects []const u8 key, got %s", name, got)
	}
	return nil
}

// checkedMapArgs validates and returns the two Map type arguments.
func (c *Checker) checkedMapArgs(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, fmt.Errorf("type error: std::map::Map expects 2 type arguments")
	}
	if _, err := c.parseMapType(fmt.Sprintf("std::map::Map<%s>", arg), args); err != nil {
		return nil, err
	}
	return args, nil
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
	if len(args) < 1 {
		return "", fmt.Errorf("type error: `TaskGroup.spawn` expects function and args")
	}
	target, ok := args[0].(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("type error: `TaskGroup.spawn` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		if _, ok := env.lookup(target.Name); ok {
			return "", fmt.Errorf("type error: `TaskGroup.spawn` expects function name")
		}
		return "", fmt.Errorf("type error: undefined function `%s`", target.Name)
	}
	if err := c.checkSpawnArgs(target.Name, fn, args[1:], env, unsafe); err != nil {
		return "", err
	}
	return Type(fmt.Sprintf("Task<%s>", fn.returnType)), nil
}

// checkTaskGroup validates a task group bound to one Io implementation.
func (c *Checker) checkTaskGroup(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `std::task::Group` expects io")
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Io" {
		return "", true, fmt.Errorf("type error: `std::task::Group` expects Io, got %s", got)
	}
	return "TaskGroup", true, nil
}

// checkSpawnArgs validates spawned function arguments after the implicit Io.
func (c *Checker) checkSpawnArgs(
	name string,
	fn *functionType,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	if len(fn.params) == 0 || fn.params[0] != "Io" ||
		fn.borrowParams[0] || fn.mutBorrowParams[0] {
		return fmt.Errorf("type error: spawned function `%s` must accept owned Io as first parameter",
			name)
	}
	if len(args) != len(fn.params)-1 {
		return fmt.Errorf("type error: `%s` expects %d args, got %d",
			name, len(fn.params)-1, len(args))
	}
	for idx, arg := range args {
		paramIdx := idx + 1
		if fn.borrowParams[paramIdx] || fn.mutBorrowParams[paramIdx] {
			return fmt.Errorf("type error: task cannot capture borrow parameter `%s`", name)
		}
		if err := c.rejectThreadBoundaryArg(arg, env, unsafe); err != nil {
			return err
		}
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return err
		}
		if got != fn.params[paramIdx] {
			return fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, name, fn.params[paramIdx], got)
		}
	}
	return nil
}

// checkQueueMethod validates deterministic deferred task queue operations.
func (c *Checker) checkQueueMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch name {
	case "enqueue":
		return c.checkQueueEnqueue(args, env, unsafe)
	case "drain":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `queue.drain` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: Queue has no method `%s`", name)
	}
}

// checkQueueEnqueue validates one deferred function call.
func (c *Checker) checkQueueEnqueue(args []ast.Expression, env *scope, unsafe bool) (Type, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("type error: `queue.enqueue` expects io, function, and args")
	}
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if ioType != "Io" {
		return "", fmt.Errorf("type error: `queue.enqueue` expects Io, got %s", ioType)
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", fmt.Errorf("type error: `queue.enqueue` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", fmt.Errorf("type error: undefined function `%s`", target.Name)
	}
	spawnArgs := append([]ast.Expression{args[0]}, args[2:]...)
	if len(spawnArgs) != len(fn.params) {
		return "", fmt.Errorf("type error: `%s` expects %d args, got %d",
			target.Name, len(fn.params), len(spawnArgs))
	}
	for idx, arg := range spawnArgs {
		if fn.borrowParams[idx] || fn.mutBorrowParams[idx] {
			return "", fmt.Errorf("type error: queue cannot capture borrow parameter `%s`", target.Name)
		}
		if err := c.rejectThreadBoundaryArg(arg, env, unsafe); err != nil {
			return "", err
		}
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return "", err
		}
		if got != fn.params[idx] {
			return "", fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, target.Name, fn.params[idx], got)
		}
	}
	return typeVoid, nil
}

// checkChannelMethod validates owned message passing operations.
func (c *Checker) checkChannelMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch name {
	case "send":
		if len(args) != 1 {
			return "", fmt.Errorf("type error: `channel.send` expects 1 arg, got %d", len(args))
		}
		if err := c.rejectThreadBoundaryArg(args[0], env, unsafe); err != nil {
			return "", err
		}
		got, err := c.checkExpr(args[0], env, unsafe)
		if err != nil {
			return "", err
		}
		if got != elem {
			return "", fmt.Errorf("type error: `channel.send` expects %s, got %s", elem, got)
		}
		return typeVoid, nil
	case "recv":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `channel.recv` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "close":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `channel.close` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: Channel has no method `%s`", name)
	}
}

// checkPartitionMethod validates access to one disjoint partition index.
func (c *Checker) checkPartitionMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if name != "at" {
		return "", fmt.Errorf("type error: Partition has no method `%s`", name)
	}
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `partition.at` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != typeI64 {
		return "", fmt.Errorf("type error: `partition.at` expects i64, got %s", got)
	}
	return typeI64, nil
}

// checkLocalBufferMethod validates worker-local scratch access.
func (c *Checker) checkLocalBufferMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	if name != "get" {
		return "", fmt.Errorf("type error: LocalBuffer has no method `%s`", name)
	}
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `LocalBuffer.get` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != typeI64 {
		return "", fmt.Errorf("type error: `LocalBuffer.get` expects i64, got %s", got)
	}
	return typeI64, nil
}

// checkAtomicMethod validates seq_cst-only atomic operations.
func (c *Checker) checkAtomicMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, error) {
	switch name {
	case "load":
		if len(args) != 0 {
			return "", fmt.Errorf("type error: `atomic.load` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "store":
		if len(args) != 1 {
			return "", fmt.Errorf("type error: `atomic.store` expects 1 arg, got %d", len(args))
		}
		got, err := c.checkExpr(args[0], env, unsafe)
		if err != nil {
			return "", err
		}
		if got != elem {
			return "", fmt.Errorf("type error: `atomic.store` expects %s, got %s", elem, got)
		}
		return typeVoid, nil
	default:
		return "", fmt.Errorf("type error: Atomic has no method `%s`", name)
	}
}

// checkMutexMethod validates the minimal synchronized wrapper API.
func (c *Checker) checkMutexMethod(elem Type, name string, args []ast.Expression) (Type, error) {
	if name != "get" {
		return "", fmt.Errorf("type error: Mutex has no method `%s`", name)
	}
	if len(args) != 0 {
		return "", fmt.Errorf("type error: `mutex.get` expects 0 args, got %d", len(args))
	}
	return elem, nil
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

// checkPartitionMut validates a trusted disjoint partition constructor.
func (c *Checker) checkPartitionMut(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("type error: `std::task::partition_mut` expects 2 args")
	}
	init, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if init != typeI64 {
		return "", true, fmt.Errorf("type error: partition init expects i64, got %s", init)
	}
	count, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if count != typeI64 {
		return "", true, fmt.Errorf("type error: partition count expects i64, got %s", count)
	}
	return "Partition", true, nil
}

// checkLocalBuffer validates worker-local scratch construction.
func (c *Checker) checkLocalBuffer(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, fmt.Errorf("type error: `std::task::LocalBuffer` expects 2 args")
	}
	count, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if count != typeI64 {
		return "", true, fmt.Errorf("type error: LocalBuffer count expects i64, got %s", count)
	}
	if _, err := c.checkExpr(args[1], env, unsafe); err != nil {
		return "", true, err
	}
	return "LocalBuffer", true, nil
}

// checkParallelFor validates the safe data-parallel prototype.
func (c *Checker) checkParallelFor(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 4 {
		return "", true, fmt.Errorf("type error: `std::task::parallel_for` expects 4 args")
	}
	if err := c.checkIoAndRange(args, env, unsafe, "std::task::parallel_for"); err != nil {
		return "", true, err
	}
	target, ok := args[3].(*ast.IdentExpr)
	if !ok {
		return "", true, fmt.Errorf("type error: `std::task::parallel_for` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", true, fmt.Errorf("type error: undefined function `%s`", target.Name)
	}
	if len(fn.params) != 1 || fn.params[0] != typeI64 {
		return "", true, fmt.Errorf("type error: parallel worker `%s` must accept i64", target.Name)
	}
	typ, err := c.parallelReturnType(fn)
	return typ, true, err
}

// checkParallelMap validates disjoint partition output from a worker result.
func (c *Checker) checkParallelMap(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 5 {
		return "", true, fmt.Errorf("type error: `std::task::parallel_map` expects 5 args")
	}
	if err := c.checkIoAndPartitionRange(args, env, unsafe); err != nil {
		return "", true, err
	}
	target, ok := args[4].(*ast.IdentExpr)
	if !ok {
		return "", true, fmt.Errorf("type error: `std::task::parallel_map` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", true, fmt.Errorf("type error: undefined function `%s`", target.Name)
	}
	if len(fn.params) != 1 || fn.params[0] != typeI64 {
		return "", true, fmt.Errorf("type error: parallel map worker `%s` must accept i64", target.Name)
	}
	if fn.returnType != typeI64 {
		return "", true, fmt.Errorf("type error: parallel map worker `%s` must return i64", target.Name)
	}
	return typeVoid, true, nil
}

// checkThreadScoped validates explicit low-level scoped thread boundaries.
func (c *Checker) checkThreadScoped(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) < 2 {
		return "", true, fmt.Errorf("type error: `std::thread::scoped` expects io and function")
	}
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if ioType != "Io" {
		return "", true, fmt.Errorf("type error: `std::thread::scoped` expects Io, got %s", ioType)
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", true, fmt.Errorf("type error: `std::thread::scoped` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", true, fmt.Errorf("type error: undefined function `%s`", target.Name)
	}
	return c.checkThreadScopedArgs(target.Name, fn, args[2:], env, unsafe)
}

// checkAtomic validates the v0.1 seq_cst atomic constructor.
func (c *Checker) checkAtomic(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if !isAtomicSupportedType(elem) {
		return "", true, fmt.Errorf("type error: unsupported atomic type `%s` in v0.1", elem)
	}
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `std::atomic::Atomic<%s>` expects 1 arg", elem)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != elem {
		return "", true, fmt.Errorf("type error: `std::atomic::Atomic<%s>` expects %s, got %s",
			elem, elem, got)
	}
	return Type(fmt.Sprintf("Atomic<%s>", elem)), true, nil
}

// checkMutex validates an explicit synchronized ownership wrapper.
func (c *Checker) checkMutex(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, fmt.Errorf("type error: `std::sync::Mutex<%s>` expects 1 arg", elem)
	}
	if err := c.rejectThreadBoundaryArg(args[0], env, unsafe); err != nil {
		return "", true, err
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != elem {
		return "", true, fmt.Errorf("type error: `std::sync::Mutex<%s>` expects %s, got %s",
			elem, elem, got)
	}
	if !c.isCopyType(elem) {
		return "", true, fmt.Errorf(
			"type error: `std::sync::Mutex<%s>` requires copy value in v0.1", elem)
	}
	return Type(fmt.Sprintf("Mutex<%s>", elem)), true, nil
}

// checkDynMethodCall validates a method call through &Dyn<Contract>.
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
		if method.mutBorrowParams[idx+1] {
			if err := requireMutableBorrowArg(arg, env); err != nil {
				return "", err
			}
		}
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

// requireMutableBorrowArg restricts &mut arguments to mutable local bindings.
func requireMutableBorrowArg(expr ast.Expression, env *scope) error {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		field, fieldOK := expr.(*ast.FieldExpr)
		if !fieldOK {
			return fmt.Errorf("type error: &mut argument must be a mutable local binding")
		}
		ident, ok = field.Receiver.(*ast.IdentExpr)
		if !ok {
			return fmt.Errorf("type error: &mut argument must be a mutable local binding")
		}
	}
	if !env.isMutable(ident.Name) {
		return fmt.Errorf("type error: &mut argument `%s` must be mutable", ident.Name)
	}
	return nil
}

// borrowPrefix reports whether an expression is &T or &mut T syntax.
func borrowPrefix(expr ast.Expression) (*ast.PrefixExpr, bool) {
	prefix, ok := expr.(*ast.PrefixExpr)
	if !ok || (prefix.Operator != "&" && prefix.Operator != "&mut") {
		return nil, false
	}
	return prefix, true
}

// checkBorrowTargetShape restricts v0.1 explicit borrows to direct locals or one field.
func checkBorrowTargetShape(expr ast.Expression) error {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		return nil
	case *ast.FieldExpr:
		if _, ok := target.Receiver.(*ast.IdentExpr); ok {
			return nil
		}
		return fmt.Errorf("type error: v0.1 field borrow only supports one direct field")
	default:
		return fmt.Errorf("type error: borrow target must be a local binding or direct field")
	}
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

// isCopyType reports whether values of typ can be duplicated in v0.1 safe code.
func (c *Checker) isCopyType(typ Type) bool {
	if c.enums[string(typ)] != nil {
		return true
	}
	return copyTypes[typ]
}

// isAtomicSupportedType reports whether Atomic<T> is available in v0.1.
func isAtomicSupportedType(typ Type) bool {
	return typ == typeBool || typ == typeI64
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

// checkIoAndRange validates common io/start/end arguments.
func (c *Checker) checkIoAndRange(
	args []ast.Expression,
	env *scope,
	unsafe bool,
	name string,
) error {
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if ioType != "Io" {
		return fmt.Errorf("type error: `%s` expects Io, got %s", name, ioType)
	}
	for idx := 1; idx <= 2; idx++ {
		got, err := c.checkExpr(args[idx], env, unsafe)
		if err != nil {
			return err
		}
		if got != typeI64 {
			return fmt.Errorf("type error: `%s` range expects i64, got %s", name, got)
		}
	}
	return nil
}

// checkIoAndPartitionRange validates io, partition, start, and end arguments.
func (c *Checker) checkIoAndPartitionRange(
	args []ast.Expression,
	env *scope,
	unsafe bool,
) error {
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if ioType != "Io" {
		return fmt.Errorf("type error: `std::task::parallel_map` expects Io, got %s", ioType)
	}
	partitionType, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return err
	}
	if partitionType != "Partition" {
		return fmt.Errorf("type error: `std::task::parallel_map` expects Partition, got %s",
			partitionType)
	}
	for idx := 2; idx <= 3; idx++ {
		got, err := c.checkExpr(args[idx], env, unsafe)
		if err != nil {
			return err
		}
		if got != typeI64 {
			return fmt.Errorf("type error: `std::task::parallel_map` range expects i64, got %s", got)
		}
	}
	return nil
}

// parallelReturnType restricts parallel workers to void or !void.
func (c *Checker) parallelReturnType(fn *functionType) (Type, error) {
	if fn.returnType == typeVoid {
		return typeVoid, nil
	}
	if elem, ok := errorUnionElement(fn.returnType); ok && elem == string(typeVoid) {
		return fn.returnType, nil
	}
	return "", fmt.Errorf("type error: parallel worker `%s` must return void or !void", fn.name)
}

// checkThreadScopedArgs validates explicit low-level thread boundary arguments.
func (c *Checker) checkThreadScopedArgs(
	name string,
	fn *functionType,
	args []ast.Expression,
	env *scope,
	unsafe bool,
) (Type, bool, error) {
	if len(args) != len(fn.params) {
		return "", true, fmt.Errorf("type error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	for idx, arg := range args {
		if fn.borrowParams[idx] || fn.mutBorrowParams[idx] {
			return "", true, fmt.Errorf("type error: thread cannot capture borrow parameter `%s`", name)
		}
		if err := c.rejectThreadBoundaryArg(arg, env, unsafe); err != nil {
			return "", true, err
		}
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return "", true, err
		}
		if got != fn.params[idx] {
			return "", true, fmt.Errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, name, fn.params[idx], got)
		}
	}
	return fn.returnType, true, nil
}

// rejectThreadBoundaryArg rejects values unsafe to move across concurrency boundaries.
func (c *Checker) rejectThreadBoundaryArg(arg ast.Expression, env *scope, unsafe bool) error {
	if ident, ok := arg.(*ast.IdentExpr); ok && env.isBorrowed(ident.Name) {
		return fmt.Errorf("type error: borrow cannot cross concurrency boundary")
	}
	got, err := c.checkExpr(arg, env, unsafe)
	if err != nil {
		return err
	}
	return c.rejectThreadBoundaryType(got, map[Type]bool{})
}

// rejectThreadBoundaryType rejects types that cannot cross safe concurrency boundaries.
func (c *Checker) rejectThreadBoundaryType(typ Type, seen map[Type]bool) error {
	if isPointerType(typ) {
		return fmt.Errorf("type error: raw pointer cannot cross concurrency boundary")
	}
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	if err := c.rejectThreadBoundaryGeneric(typ, seen); err != nil {
		return err
	}
	if err := c.rejectThreadBoundaryStruct(typ, seen); err != nil {
		return err
	}
	return c.rejectThreadBoundaryUnion(typ, seen)
}

// rejectThreadBoundaryGeneric applies boundary rules to generic-like type spellings.
func (c *Checker) rejectThreadBoundaryGeneric(typ Type, seen map[Type]bool) error {
	base, arg, ok := splitGenericType(string(typ))
	if !ok {
		return nil
	}
	switch base {
	case "arena":
		return fmt.Errorf("type error: arena cannot cross concurrency boundary")
	case "std::array::Array":
		return fmt.Errorf("type error: Array cannot cross concurrency boundary in v0.2")
	case "std::map::Map":
		return fmt.Errorf("type error: Map cannot cross concurrency boundary in v0.2")
	case "handle":
		return fmt.Errorf("type error: handle cannot cross concurrency boundary")
	case "Dyn":
		return fmt.Errorf("type error: Dyn cannot cross concurrency boundary")
	case "Mutex":
		return fmt.Errorf("type error: Mutex cannot cross concurrency boundary in v0.1")
	case "Task":
		return fmt.Errorf("type error: Task cannot cross concurrency boundary")
	case "Channel", "option":
		return c.rejectThreadBoundaryNamedArg(arg, seen)
	case "Atomic":
		return c.rejectThreadBoundaryAtomic(arg, seen)
	default:
		return nil
	}
}

// rejectThreadBoundaryNamedArg parses and checks a nested generic argument.
func (c *Checker) rejectThreadBoundaryNamedArg(name string, seen map[Type]bool) error {
	typ, err := c.parseType(name)
	if err != nil {
		return err
	}
	return c.rejectThreadBoundaryType(typ, seen)
}

// rejectThreadBoundaryAtomic checks Atomic<T> boundary eligibility.
func (c *Checker) rejectThreadBoundaryAtomic(name string, seen map[Type]bool) error {
	typ, err := c.parseType(name)
	if err != nil {
		return err
	}
	if !isAtomicSupportedType(typ) {
		return fmt.Errorf("type error: Atomic<%s> cannot cross concurrency boundary in v0.1", typ)
	}
	return c.rejectThreadBoundaryType(typ, seen)
}

// rejectThreadBoundaryStruct checks all struct fields recursively.
func (c *Checker) rejectThreadBoundaryStruct(typ Type, seen map[Type]bool) error {
	decl := c.structs[string(typ)]
	if decl == nil {
		return nil
	}
	for _, field := range decl.Fields {
		fieldType, err := c.parseType(field.TypeName)
		if err != nil {
			return err
		}
		if err := c.rejectThreadBoundaryType(fieldType, seen); err != nil {
			return fmt.Errorf("type error: struct `%s.%s` cannot cross concurrency boundary: %w",
				typ, field.Name, err)
		}
	}
	return nil
}

// rejectThreadBoundaryUnion checks all union payloads recursively.
func (c *Checker) rejectThreadBoundaryUnion(typ Type, seen map[Type]bool) error {
	union := c.unions[string(typ)]
	if union == nil {
		return nil
	}
	for variant, payload := range union.variants {
		if payload == "" {
			continue
		}
		payloadType, err := c.parseType(payload)
		if err != nil {
			return err
		}
		if err := c.rejectThreadBoundaryType(payloadType, seen); err != nil {
			return fmt.Errorf("type error: union `%s::%s` cannot cross concurrency boundary: %w",
				typ, variant, err)
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

// errorUnionElement extracts T from legacy !T.
func errorUnionElement(typ Type) (string, bool) {
	_, success, ok := errorUnionParts(typ)
	return success, ok
}

// errorUnionParts extracts error and success types from !T or Error!T.
func errorUnionParts(typ Type) (string, string, bool) {
	name := string(typ)
	if strings.HasPrefix(name, "!") && len(name) > 1 {
		return "", strings.TrimPrefix(name, "!"), true
	}
	return typedErrorUnionParts(name)
}

// typedErrorUnionParts extracts Error and T from Error!T.
func typedErrorUnionParts(name string) (string, string, bool) {
	idx := strings.Index(name, "!")
	if idx <= 0 || idx == len(name)-1 {
		return "", "", false
	}
	return name[:idx], name[idx+1:], true
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
	return &scope{
		parent: parent, values: map[string]Type{}, mutable: map[string]bool{},
		borrowed: map[string]bool{}, mutBorrow: map[string]bool{},
	}
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

// defineParam binds a function parameter and records borrow capabilities.
func (s *scope) defineParam(name string, typ Type, borrowed bool, mutBorrow bool) error {
	if err := s.define(name, typ, false); err != nil {
		return err
	}
	s.borrowed[name] = borrowed
	s.mutBorrow[name] = mutBorrow
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

// isBorrowed reports whether a local name is an &T or &mut T parameter.
func (s *scope) isBorrowed(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			return cur.borrowed[name]
		}
	}
	return false
}

// isMutBorrowed reports whether a local name is an &mut T parameter.
func (s *scope) isMutBorrowed(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			return cur.mutBorrow[name]
		}
	}
	return false
}

// splitGenericType extracts base and raw arguments from base<args>.
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

// singleGenericArg returns the only argument for one-parameter generic types.
func singleGenericArg(base string, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("type error: `%s` expects 1 type argument", base)
	}
	return args[0], nil
}
