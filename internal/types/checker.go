package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/stdprim"
	"github.com/kizu-lang/kizu/internal/typ"
	"github.com/kizu-lang/kizu/internal/unsafecap"
)

// Type is the static type name used by the v0 checker.
type Type string

const (
	typeBool       Type = "bool"
	typeFunction   Type = "Function"
	typeI64        Type = "i64"
	typeU8         Type = "u8"
	typeByteString Type = "[]u8"
	typeSelf       Type = "Self"
	typeType       Type = "type"
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
	typeFunction:          true,
	typeType:              true,
	"Io":                  true,
	"Allocator":           true,
	"std::fs::Metadata":   true,
	"std::fs::DirEntry":   true,
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
	"std::fs::DirEntry": true,
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

type unsafeCapability string

const (
	unsafePtrRead    unsafeCapability = "ptr_read"
	unsafePtrWrite   unsafeCapability = "ptr_write"
	unsafePtrDeref   unsafeCapability = "ptr_deref"
	unsafePtrCast    unsafeCapability = "ptr_cast"
	unsafePtrIntCast unsafeCapability = "ptr_int_cast"
	unsafeExternCall unsafeCapability = "extern_call"
	unsafeUnsafeCall unsafeCapability = "unsafe_call"
	unsafeVolatile   unsafeCapability = "volatile"
)

var knownUnsafeCapabilities = map[string]unsafeCapability{
	string(unsafePtrRead):    unsafePtrRead,
	string(unsafePtrWrite):   unsafePtrWrite,
	string(unsafePtrDeref):   unsafePtrDeref,
	string(unsafePtrCast):    unsafePtrCast,
	string(unsafePtrIntCast): unsafePtrIntCast,
	string(unsafeExternCall): unsafeExternCall,
	string(unsafeUnsafeCall): unsafeUnsafeCall,
	string(unsafeVolatile):   unsafeVolatile,
}

type unsafeCaps map[unsafeCapability]bool

// has reports whether an unsafe capability is available in the current scope.
func (caps unsafeCaps) has(cap unsafeCapability) bool {
	return caps != nil && caps[cap]
}

// with returns a scope containing the receiver capabilities plus the parsed names.
func (caps unsafeCaps) with(names []string) (unsafeCaps, error) {
	next := unsafeCaps{}
	for cap := range caps {
		next[cap] = true
	}
	for _, name := range names {
		if _, ok := unsafecap.Lookup(name); !ok {
			return nil, errorf("unsafe error: unknown capability `%s`", name)
		}
		cap, ok := knownUnsafeCapabilities[name]
		if !ok {
			return nil, errorf("unsafe error: unknown capability `%s`", name)
		}
		next[cap] = true
	}
	return next, nil
}

// requireUnsafeCapability rejects an unsafe operation outside its capability scope.
// requireUnsafeCapabilityAt reports a missing unsafe capability at a source span.
func requireUnsafeCapabilityAt(
	caps unsafeCaps,
	cap unsafeCapability,
	operation string,
	span ast.Span,
) error {
	if caps.has(cap) {
		return nil
	}
	message := fmt.Sprintf("unsafe error: %s requires @unsafe(%s)", operation, cap)
	if info, ok := unsafecap.Lookup(string(cap)); ok {
		message += "\nhelp: " + unsafecap.Hint(info)
	}
	if !span.IsZero() {
		return errorAtCode(span, "unsafe.missing_capability", "%s", message)
	}
	return errorf("%s", message)
}

// Checker validates type rules for a parsed program.
type Checker struct {
	functions       map[string]*functionType
	structs         map[string]*ast.StructDecl
	enums           map[string]*enumType
	unions          map[string]*unionType
	contracts       map[string]*contractType
	impls           map[string]map[string]*functionType
	satisfactions   map[string]map[string]bool
	declaredTypes   map[string]bool
	currentReturn   Type
	currentFunction *functionType
	currentStd      bool
	typeParams      map[string]bool
	typeArgValues   map[string]Type
	// staticParams holds the compile-time value parameters of the generic being
	// checked, by declared type. A runtime local of the same name is not one of
	// these, which is what separates forwarding a static value from reading a
	// value that only exists at run time.
	staticParams map[string]Type
	loopLabels   []string
	// stdMethods indexes the signatures std declares for its container methods,
	// so this checker reads them instead of restating them.
	stdMethods stdmethod.MethodIndex
}

type enumType struct {
	name   string
	tags   map[string]bool
	public bool
}

type unionType struct {
	name       string
	typeParams []string
	variants   map[string]string
	public     bool
}

type functionType struct {
	name            string
	params          []Type
	borrowParams    []bool
	mutBorrowParams []bool
	typeParams      []string
	returnBorrow    string
	returnType      Type
	decl            *ast.FunctionDecl
	requiresUnsafe  bool
	externABI       string
	implicitReturn  bool
}

type contractType struct {
	name    string
	methods map[string]*functionType
	public  bool
}

type scope struct {
	parent       *scope
	values       map[string]Type
	mutable      map[string]bool
	borrowed     map[string]bool
	mutBorrow    map[string]bool
	borrowSource map[string]string
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
		declaredTypes: map[string]bool{},
	}
}

// Check validates the program and returns the first type error.
func (c *Checker) Check(program *ast.Program) error {
	c.stdMethods = stdmethod.IndexMethods(program.Decls)
	if err := c.collectFunctions(program); err != nil {
		return err
	}
	if err := c.checkPublicAPI(program); err != nil {
		return err
	}
	if err := c.checkOwnerUnionContracts(program); err != nil {
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
		case *ast.ImplDecl:
			if err := c.checkImpl(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckAll validates the program like Check but accumulates one error per
// top-level declaration instead of stopping at the first, so editors can show
// every independent type error at once. Setup phases that the body checks
// depend on still fail fast, since later errors would be noise without them.
func (c *Checker) CheckAll(program *ast.Program) []error {
	if err := c.collectFunctions(program); err != nil {
		return []error{err}
	}
	if err := c.checkPublicAPI(program); err != nil {
		return []error{err}
	}
	if err := c.checkOwnerUnionContracts(program); err != nil {
		return []error{err}
	}
	var errs []error
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
		case *ast.ImplDecl:
			if err := c.checkImpl(d); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// collectFunctions registers top-level function signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	if err := c.collectTypesAndMethods(program); err != nil {
		return err
	}
	if err := c.collectTopLevelFunctions(program); err != nil {
		return err
	}
	return nil
}

// collectTypesAndMethods registers declarations needed before function signatures.
func (c *Checker) collectTypesAndMethods(program *ast.Program) error {
	if err := c.predeclareTypeNames(program); err != nil {
		return err
	}
	for _, decl := range program.Decls {
		if err := c.collectTypeDecl(decl); err != nil {
			return err
		}
	}
	for _, decl := range program.Decls {
		if err := c.collectMethodDecl(decl); err != nil {
			return err
		}
	}
	return nil
}

// collectTypeDecl registers one type declaration before methods are validated.
func (c *Checker) collectTypeDecl(decl ast.Decl) error {
	switch d := decl.(type) {
	case *ast.StructDecl:
		return c.collectStruct(d)
	case *ast.EnumDecl:
		return c.collectEnum(d)
	case *ast.UnionDecl:
		return c.collectUnion(d)
	case *ast.ContractDecl:
		return c.collectContract(d)
	case *ast.ImportDecl, *ast.FunctionDecl, *ast.TestDecl, *ast.ImplDecl:
		return nil
	default:
		return errorf("type error: unsupported declaration %T", decl)
	}
}

// collectMethodDecl registers one method declaration after contracts are known.
func (c *Checker) collectMethodDecl(decl ast.Decl) error {
	impl, ok := decl.(*ast.ImplDecl)
	if !ok {
		return nil
	}
	return c.collectImpl(impl)
}

// predeclareTypeNames lets recursive fields refer to later declarations through Box.
func (c *Checker) predeclareTypeNames(program *ast.Program) error {
	for _, decl := range program.Decls {
		name, ok := declaredTypeName(decl)
		if !ok {
			continue
		}
		c.declaredTypes[name] = true
	}
	return nil
}

// declaredTypeName returns the user type introduced by a declaration.
func declaredTypeName(decl ast.Decl) (string, bool) {
	switch d := decl.(type) {
	case *ast.StructDecl:
		return d.Name, true
	case *ast.EnumDecl:
		return d.Name, true
	case *ast.UnionDecl:
		return d.Name, true
	case *ast.ContractDecl:
		return d.Name, true
	default:
		return "", false
	}
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
			return errorf("type error: duplicate function `%s`", fn.Name)
		}
		fnType, err := c.newFunctionType(fn)
		if err != nil {
			return err
		}
		c.functions[fn.Name] = fnType
	}
	return nil
}

// collectContract registers a required method set.
func (c *Checker) collectContract(decl *ast.ContractDecl) error {
	if _, exists := c.contracts[decl.Name]; exists {
		return errorf("type error: duplicate contract `%s`", decl.Name)
	}
	methods := map[string]*functionType{}
	for _, method := range decl.Methods {
		if _, exists := methods[method.Name]; exists {
			return errorf("type error: duplicate contract method `%s.%s`", decl.Name, method.Name)
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
	if decl.ContractName != "" && c.contracts[decl.ContractName] == nil {
		return errorf("type error: unknown contract `%s`", decl.ContractName)
	}
	methods := c.impls[decl.TypeName]
	if methods == nil {
		methods = map[string]*functionType{}
		c.impls[decl.TypeName] = methods
	}
	for _, method := range decl.Methods {
		if _, exists := methods[method.Name]; exists {
			return errorf("type error: duplicate impl method `%s.%s`", decl.TypeName, method.Name)
		}
		fnType, err := c.newImplFunctionType(decl.TypeName, method)
		if err != nil {
			return err
		}
		fnType.name = fmt.Sprintf("%s.%s", decl.TypeName, method.Name)
		methods[method.Name] = fnType
	}
	if decl.ContractName == "" {
		return nil
	}
	return c.recordContractImpl(decl)
}

// recordContractImpl validates and records explicit contract implementation.
func (c *Checker) recordContractImpl(decl *ast.ImplDecl) error {
	contract := c.contracts[decl.ContractName]
	for name, want := range contract.methods {
		got := c.implMethod(decl.TypeName, name)
		if got == nil {
			return errorf("type error: `%s` does not satisfy `%s`: missing method `%s`",
				decl.TypeName, decl.ContractName, name)
		}
		if !methodMatches(decl.TypeName, want, got) {
			return errorf("type error: `%s.%s` does not match contract `%s`",
				decl.TypeName, name, decl.ContractName)
		}
	}
	if c.satisfactions[decl.ContractName] == nil {
		c.satisfactions[decl.ContractName] = map[string]bool{}
	}
	c.satisfactions[decl.ContractName][decl.TypeName] = true
	return nil
}

// newImplFunctionType converts a method declaration and binds Self to its receiver.
func (c *Checker) newImplFunctionType(
	typeName string,
	method *ast.FunctionDecl,
) (*functionType, error) {
	fnType, err := c.newFunctionType(method)
	if err != nil {
		return nil, err
	}
	for idx, param := range fnType.params {
		fnType.params[idx] = substituteSelfType(param, typeName)
	}
	fnType.returnType = substituteSelfType(fnType.returnType, typeName)
	return fnType, nil
}

// collectEnum registers and validates a tag enum declaration.
func (c *Checker) collectEnum(decl *ast.EnumDecl) error {
	if _, exists := c.enums[decl.Name]; exists {
		return errorf("type error: duplicate enum `%s`", decl.Name)
	}
	if _, exists := c.structs[decl.Name]; exists {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	if _, exists := c.unions[decl.Name]; exists {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	enum := &enumType{name: decl.Name, tags: map[string]bool{}, public: decl.Public}
	for _, tag := range decl.Tags {
		if enum.tags[tag] {
			return errorf("type error: duplicate enum tag `%s::%s`", decl.Name, tag)
		}
		enum.tags[tag] = true
	}
	c.enums[decl.Name] = enum
	return nil
}

// collectUnion registers and validates a tagged union declaration.
func (c *Checker) collectUnion(decl *ast.UnionDecl) error {
	if _, exists := c.unions[decl.Name]; exists {
		return errorf("type error: duplicate union `%s`", decl.Name)
	}
	if _, exists := c.structs[decl.Name]; exists {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	if _, exists := c.enums[decl.Name]; exists {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	previousTypeParams := c.typeParams
	c.typeParams = typeParamSet(decl.TypeParams)
	defer func() {
		c.typeParams = previousTypeParams
	}()
	union := &unionType{
		name: decl.Name, typeParams: decl.TypeParams,
		variants: map[string]string{}, public: decl.Public,
	}
	for _, variant := range decl.Variants {
		if _, exists := union.variants[variant.Name]; exists {
			return errorf("type error: duplicate union variant `%s::%s`",
				decl.Name, variant.Name)
		}
		if variant.Payload != "" {
			typ, err := c.parseType(variant.Payload)
			if err != nil {
				return err
			}
			if err := checkBorrowFieldPolicy(decl.Name, variant.Name, variant.Payload); err != nil {
				return err
			}
			if containsTypeValue(typ) {
				return errorf("type error: union variant `%s::%s` cannot store type value",
					decl.Name, variant.Name)
			}
			if containsDynType(typ) {
				return errorf("type error: union variant `%s::%s` cannot store dyn value",
					decl.Name, variant.Name)
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
		return errorf("type error: duplicate struct `%s`", decl.Name)
	}
	if _, exists := c.enums[decl.Name]; exists {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	if _, exists := c.unions[decl.Name]; exists {
		return errorf("type error: duplicate type `%s`", decl.Name)
	}
	c.structs[decl.Name] = decl
	previousTypeParams := c.typeParams
	c.typeParams = typeParamSet(decl.TypeParams)
	defer func() {
		c.typeParams = previousTypeParams
	}()
	for _, field := range decl.Fields {
		typ, err := c.parseType(field.TypeName)
		if err != nil {
			return err
		}
		if err := checkStructFieldBorrowPolicy(decl, field); err != nil {
			return err
		}
		if typ == typeFunction {
			return errorf("type error: struct field `%s.%s` cannot store Function",
				decl.Name, field.Name)
		}
		if containsTypeValue(typ) {
			return errorf("type error: struct field `%s.%s` cannot store type value",
				decl.Name, field.Name)
		}
		if containsDynType(typ) {
			return errorf("type error: struct field `%s.%s` cannot store dyn value",
				decl.Name, field.Name)
		}
	}
	return nil
}

// ownedContainerBases lists the inline-stored std containers that own heap
// storage and must be released through their explicit `deinit`. A payload
// built from one of these (directly or nested) is an owner payload of the
// v0.2 inline tagged-union payload ABI.
//
// std::mem::Box is intentionally excluded: a boxed (heap-indirected) or
// recursive union payload is deferred to #495, so it is outside this contract
// and is left to ordinary move/borrow checking.
var ownedContainerBases = map[string]bool{
	"std::array::Array":   true,
	"std::string::String": true,
	"std::map::Map":       true,
	"std::arena::Arena":   true,
}

// typeContainsOwner reports whether typeName transitively holds an owned
// container. Scalars, enums, copy AST nodes, and arena handles are not owners;
// structs and unions are owners only when a field or variant payload is one.
func (c *Checker) typeContainsOwner(typeName string, visited map[string]bool) bool {
	name := strings.TrimSpace(typeName)
	name = strings.TrimPrefix(name, "!")
	base := name
	if b, _, ok := splitGenericType(name); ok {
		base = b
	}
	if ownedContainerBases[base] {
		return true
	}
	if visited[base] {
		return false
	}
	if st, ok := c.structs[base]; ok {
		visited[base] = true
		for _, field := range st.Fields {
			if c.typeContainsOwner(field.TypeName, visited) {
				return true
			}
		}
		return false
	}
	if union, ok := c.unions[base]; ok {
		visited[base] = true
		for _, payload := range union.variants {
			if payload != "" && c.typeContainsOwner(payload, visited) {
				return true
			}
		}
		return false
	}
	return false
}

// unionHasOwnerPayload reports whether any variant payload is an owner payload.
func (c *Checker) unionHasOwnerPayload(decl *ast.UnionDecl) bool {
	for _, variant := range decl.Variants {
		if variant.Payload == "" {
			continue
		}
		if c.typeContainsOwner(variant.Payload, map[string]bool{decl.Name: true}) {
			return true
		}
	}
	return false
}

// checkOwnerUnionContracts enforces the #991 / ADR-0075 cleanup contract for
// every owner-payload union declared in the program.
func (c *Checker) checkOwnerUnionContracts(program *ast.Program) error {
	for _, decl := range program.Decls {
		union, ok := decl.(*ast.UnionDecl)
		if !ok {
			continue
		}
		if err := c.validateOwnerUnionCleanup(union); err != nil {
			return err
		}
	}
	return nil
}

// validateOwnerUnionCleanup classifies a union and, when it carries an owner
// payload, requires a source-visible active-variant `deinit(self: T) -> void`.
// A union with only copy/scalar or payload-free variants stays a non-owner value
// and needs no deinit.
func (c *Checker) validateOwnerUnionCleanup(decl *ast.UnionDecl) error {
	if !c.unionHasOwnerPayload(decl) {
		return nil
	}
	if len(decl.TypeParams) > 0 {
		return errorf(
			"type error: generic owner-payload union `%s` is unsupported in v0.2; "+
				"use a concrete owner union with explicit `deinit`",
			decl.Name)
	}
	method := c.implMethod(decl.Name, "deinit")
	if method == nil {
		return errorf(
			"type error: owner-payload union `%s` requires explicit `deinit(self: %s) -> void`",
			decl.Name, decl.Name)
	}
	if err := c.checkOwnerUnionDeinitSignature(decl, method); err != nil {
		return err
	}
	return c.checkOwnerUnionDeinitBody(decl, method.decl)
}

// checkOwnerUnionDeinitSignature enforces the consuming `deinit(self: T) -> void`
// receiver shape required for owner aggregates.
func (c *Checker) checkOwnerUnionDeinitSignature(decl *ast.UnionDecl, method *functionType) error {
	if method.returnType != typeVoid {
		return errorf("type error: owner-payload union `%s` deinit must return void", decl.Name)
	}
	if len(method.params) != 1 {
		return errorf("type error: owner-payload union `%s` deinit must take only `self: %s`",
			decl.Name, decl.Name)
	}
	if method.borrowParams[0] || method.mutBorrowParams[0] {
		return errorf("type error: owner-payload union `%s` deinit must take `self` by value", decl.Name)
	}
	if string(method.params[0]) != decl.Name {
		return errorf("type error: owner-payload union `%s` deinit receiver must be `%s`",
			decl.Name, decl.Name)
	}
	return nil
}

// checkOwnerUnionDeinitBody validates the accepted active-variant cleanup shape:
// an exhaustive `match self` whose every owner-payload variant binds and cleans
// its payload. Inactive variants are never cleaned because only the active arm
// runs, so copy and payload-free variants need no cleanup.
func (c *Checker) checkOwnerUnionDeinitBody(decl *ast.UnionDecl, fn *ast.FunctionDecl) error {
	if fn == nil || len(fn.Params) == 0 {
		return errorf("type error: owner-payload union `%s` deinit must take `self`", decl.Name)
	}
	selfName := fn.Params[0].Name
	match := ownerUnionSelfMatch(fn.Body, selfName)
	if match == nil {
		return errorf(
			"type error: owner-payload union `%s` deinit must dispatch on `%s` with an exhaustive `match`",
			decl.Name, selfName)
	}
	armByTag := map[string]ast.MatchArm{}
	for _, arm := range match.Arms {
		if arm.IsWildcard() {
			return errorf(
				"type error: owner-payload union `%s` deinit `match` cannot use `_`; "+
					"clean each variant explicitly",
				decl.Name)
		}
		armByTag[arm.Tag] = arm
	}
	for _, variant := range decl.Variants {
		if variant.Payload == "" ||
			!c.typeContainsOwner(variant.Payload, map[string]bool{decl.Name: true}) {
			continue
		}
		arm, ok := armByTag[variant.Name]
		if !ok {
			return errorf("type error: owner-payload union `%s` deinit must handle variant `%s`",
				decl.Name, variant.Name)
		}
		if arm.Binding == "" {
			return errorf(
				"type error: owner-payload union variant `%s::%s` must bind its payload to clean it in deinit",
				decl.Name, variant.Name)
		}
		if !matchArmCleansPayload(arm.Body, arm.Binding) {
			return errorf(
				"type error: owner-payload union variant `%s::%s` must clean its payload via `%s.deinit()`",
				decl.Name, variant.Name, arm.Binding)
		}
	}
	return nil
}

// ownerUnionSelfMatch returns the `match self { ... }` statement only when it is
// the first executable statement of the deinit body. Requiring it first keeps the
// v0.2 shape simple and guarantees the active-variant cleanup always runs: a
// statement (such as an early `return`) cannot precede and skip it.
func ownerUnionSelfMatch(body *ast.BlockStmt, selfName string) *ast.MatchStmt {
	if body == nil || len(body.Statements) == 0 {
		return nil
	}
	switch s := body.Statements[0].(type) {
	case *ast.MatchStmt:
		if ownerUnionIdentName(s.Value) == selfName {
			return s
		}
	case *ast.ExprStmt:
		if m, ok := s.Expr.(*ast.MatchStmt); ok && ownerUnionIdentName(m.Value) == selfName {
			return m
		}
	}
	return nil
}

// matchArmCleansPayload reports whether an arm body is exactly the direct cleanup
// call `<binding>.deinit()`. Only the direct form is accepted in v0.2 so the
// active payload is always cleaned without path-sensitive analysis of the arm.
func matchArmCleansPayload(body ast.Statement, binding string) bool {
	expr, ok := body.(*ast.ExprStmt)
	if !ok {
		return false
	}
	return ownerUnionDeinitCall(expr.Expr, binding)
}

// ownerUnionDeinitCall reports whether expr is the cleanup call `binding.deinit()`.
func ownerUnionDeinitCall(expr ast.Expression, binding string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace || field.Name != "deinit" {
		return false
	}
	return ownerUnionIdentName(field.Receiver) == binding
}

// ownerUnionIdentName returns the identifier name of expr, or "" when not a name.
func ownerUnionIdentName(expr ast.Expression) string {
	if ident, ok := expr.(*ast.IdentExpr); ok {
		return ident.Name
	}
	return ""
}

// functionParamInfo keeps parsed parameter types aligned with function metadata.
type functionParamInfo struct {
	params          []Type
	borrowParams    []bool
	mutBorrowParams []bool
}

// newFunctionType converts a parsed function declaration into its static type.
func (c *Checker) newFunctionType(fn *ast.FunctionDecl) (*functionType, error) {
	previousTypeParams := c.typeParams
	c.typeParams = typeParamSet(fn.TypeParamNames())
	defer func() {
		c.typeParams = previousTypeParams
	}()

	if err := checkStaticParamPolicy(fn); err != nil {
		return nil, err
	}
	paramInfo, err := c.collectFunctionParams(fn)
	if err != nil {
		return nil, err
	}
	ret := typeVoid
	if fn.ReturnType != "" {
		var err error
		ret, err = c.parseType(fn.ReturnType)
		if err != nil {
			return nil, err
		}
	}
	if ret == typeFunction {
		return nil, errorf("type error: function `%s` cannot return Function", fn.Name)
	}
	if containsTypeValue(ret) {
		return nil, errorf("type error: function `%s` cannot return type", fn.Name)
	}
	if containsDynType(ret) {
		return nil, errorf("type error: function `%s` cannot return dyn", fn.Name)
	}
	if err := checkReturnBorrowPolicy(fn); err != nil {
		return nil, err
	}
	return &functionType{
		name: fn.Name, params: paramInfo.params, borrowParams: paramInfo.borrowParams,
		mutBorrowParams: paramInfo.mutBorrowParams,
		typeParams:      fn.TypeParamNames(),
		returnBorrow:    fn.ReturnBorrow,
		returnType:      ret, decl: fn, requiresUnsafe: fn.RequiresUnsafe,
		externABI: fn.ExternABI,
	}, nil
}

// collectFunctionParams validates function parameters and records call-time metadata.
func (c *Checker) collectFunctionParams(fn *ast.FunctionDecl) (functionParamInfo, error) {
	info := functionParamInfo{
		params:          make([]Type, 0, len(fn.Params)),
		borrowParams:    make([]bool, 0, len(fn.Params)),
		mutBorrowParams: make([]bool, 0, len(fn.Params)),
	}
	for _, param := range fn.Params {
		paramType, err := c.parseType(param.TypeName)
		if err != nil {
			return functionParamInfo{}, err
		}
		if err := c.checkFunctionParam(param, paramType); err != nil {
			return functionParamInfo{}, err
		}
		info.params = append(info.params, paramType)
		info.borrowParams = append(info.borrowParams, param.Borrow)
		info.mutBorrowParams = append(info.mutBorrowParams, param.MutBorrow)
	}
	return info, nil
}

// checkFunctionParam validates one function parameter type and lifetime boundary.
func (c *Checker) checkFunctionParam(
	param ast.Param,
	paramType Type,
) error {
	if paramType == typeVoid {
		return errorf("type error: parameter `%s` cannot have type void", param.Name)
	}
	if containsTypeValue(paramType) {
		return errorf("type error: parameter `%s` cannot have type", param.Name)
	}
	if err := checkFunctionParamPolicy(param, paramType); err != nil {
		return err
	}
	if _, ok := dynContract(paramType); ok {
		if !param.Borrow {
			return errorf("type error: dyn parameter `%s` must be borrowed", param.Name)
		}
		if param.MutBorrow {
			return errorf("type error: dyn parameter `%s` must use immutable borrow in v0", param.Name)
		}
	} else if containsDynType(paramType) {
		return errorf("type error: dyn parameter `%s` must use &dyn Contract", param.Name)
	}
	return nil
}

// checkStaticParamPolicy validates what a `<...>` entry may declare. `Function`
// is a function-name token a std wrapper forwards to a trusted primitive, not a
// value a body can call, so declaring one outside std is rejected where it is
// written rather than where the name fails to resolve.
func checkStaticParamPolicy(fn *ast.FunctionDecl) error {
	for _, param := range fn.StaticParams {
		if Type(param.Type) != typeFunction || fn.Std {
			continue
		}
		return errorf(
			"type error: Function static parameter `%s` is reserved for std", param.Name)
	}
	return nil
}

// checkFunctionParamPolicy keeps Function out of the runtime argument list. A
// function name is a compile-time value, so it is a static argument.
func checkFunctionParamPolicy(param ast.Param, typ Type) error {
	if typ != typeFunction {
		return nil
	}
	return errorf(
		"type error: Function parameter `%s` belongs in `<...>`, not `(...)`", param.Name)
}

// checkReturnBorrowPolicy validates source provenance for borrowed returns.
func checkReturnBorrowPolicy(fn *ast.FunctionDecl) error {
	if fn.ReturnType == "" {
		if fn.ReturnBorrow != "" {
			return errorf("type error: function `%s` `borrows` requires return type", fn.Name)
		}
		return nil
	}
	if fn.ReturnBorrow == "" {
		if isBorrowReturnType(Type(fn.ReturnType)) {
			return errorf(
				"type error: function `%s` borrow return requires `borrows <source>`",
				fn.Name)
		}
		return nil
	}
	if !isBorrowedViewReturnType(Type(fn.ReturnType)) {
		return errorf("type error: function `%s` `borrows` requires borrowed view return",
			fn.Name)
	}
	for _, param := range fn.Params {
		if param.Name == fn.ReturnBorrow {
			return nil
		}
	}
	return errorf("type error: function `%s` borrows unknown source `%s`",
		fn.Name, fn.ReturnBorrow)
}

// checkStructFieldBorrowPolicy rejects borrow fields until a non-lifetime model exists.
func checkStructFieldBorrowPolicy(decl *ast.StructDecl, field ast.Field) error {
	if !field.Borrow {
		return nil
	}
	return errorf("type error: borrow field `%s.%s` cannot store borrow",
		decl.Name, field.Name)
}

// checkBorrowFieldPolicy rejects borrowed payloads.
func checkBorrowFieldPolicy(typeName string, fieldName string, payload string) error {
	if !strings.HasPrefix(payload, "&") {
		return nil
	}
	return errorf("type error: borrow payload `%s.%s` cannot store borrow",
		typeName, fieldName)
}

// isBorrowReturnType reports whether typ is an explicit local borrow return.
func isBorrowReturnType(typ Type) bool {
	success := unwrapReturnSuccessType(typ)
	return strings.HasPrefix(string(success), "&")
}

// isBorrowedViewReturnType reports whether typ returns a non-owned view.
func isBorrowedViewReturnType(typ Type) bool {
	success := unwrapReturnSuccessType(typ)
	text := string(success)
	return strings.HasPrefix(text, "&") || strings.HasPrefix(text, "[]")
}

// unwrapReturnSuccessType extracts the success payload of !T-like return types.
func unwrapReturnSuccessType(typ Type) Type {
	if elem, ok := errorUnionElement(typ); ok {
		return Type(elem)
	}
	if _, elem, ok := errorUnionParts(typ); ok {
		return Type(elem)
	}
	return typ
}

// borrowWrappedType returns the full spelling for a borrow-bearing field or parameter.
func borrowWrappedType(borrow bool, mutable bool, typ string) string {
	if !borrow {
		return typ
	}
	if mutable {
		return "&var " + typ
	}
	return "&" + typ
}

// typeParamSet returns a lookup for function-level type parameters.
func typeParamSet(params []string) map[string]bool {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]bool, len(params))
	for _, param := range params {
		out[param] = true
	}
	return out
}

// sameType reports exact type equality.
func sameType(left Type, right Type) bool {
	return left == right
}

// substituteTypeParams instantiates a generic type spelling. A parameter is
// replaced where the whole name matches, so `T` leaves `Timer` alone; a
// spelling this checker cannot parse is left as it stands, because rejecting it
// belongs to parseType and its diagnostic.
func substituteTypeParams(declared Type, subst map[string]Type) Type {
	if replacement, ok := subst[string(declared)]; ok {
		return replacement
	}
	parsed, err := typ.Parse(string(declared))
	if err != nil {
		return declared
	}
	return Type(typ.Substitute(parsed, parsedSubst(subst)).String())
}

// parsedSubst parses the replacement types once per substitution.
func parsedSubst(subst map[string]Type) map[string]typ.Type {
	out := make(map[string]typ.Type, len(subst))
	for name, replacement := range subst {
		parsed, err := typ.Parse(string(replacement))
		if err != nil {
			continue
		}
		out[name] = parsed
	}
	return out
}

// instantiateTypeArgText replaces in-scope generic type parameters in a type-apply list.
func (c *Checker) instantiateTypeArgText(typeArg string) string {
	if len(c.typeArgValues) == 0 {
		return typeArg
	}
	args, err := typ.SplitArgs(typeArg)
	if err != nil {
		return string(substituteTypeParams(Type(typeArg), c.typeArgValues))
	}
	for idx, arg := range args {
		args[idx] = string(substituteTypeParams(Type(arg), c.typeArgValues))
	}
	return strings.Join(args, ", ")
}

// parseType validates a source-level type name. The spelling is read once, so
// which type this is comes from its structure rather than from where a byte
// happens to sit: the `!` in `Array<!i64>` belongs to the argument, not to this
// type.
func (c *Checker) parseType(name string) (Type, error) {
	parsed, err := typ.Parse(name)
	if err != nil {
		return "", errorf("type error: unknown type `%s`", name)
	}
	switch node := parsed.(type) {
	case *typ.ErrorUnion:
		return c.parseErrorUnionType(name, node)
	case *typ.Borrow:
		return c.parseWrappingType(name, node.Elem)
	case *typ.Slice:
		return c.parseWrappingType(name, node.Elem)
	case *typ.Optional:
		return c.parseNullableType(name, node.Elem)
	case *typ.Dyn:
		return c.parseDynType(name, node.Contract)
	case *typ.Name:
		if len(node.Args) == 0 {
			return c.parseNamedType(name)
		}
		return c.parseGenericType(name, strings.Join(node.Path, "::"), argTexts(node.Args))
	default:
		return "", errorf("type error: unknown type `%s`", name)
	}
}

// argTexts returns the spelling of each static argument.
func argTexts(args []typ.Type) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.String())
	}
	return out
}

// parseWrappingType validates the element of a type that wraps one.
func (c *Checker) parseWrappingType(name string, elem typ.Type) (Type, error) {
	if _, err := c.parseType(elem.String()); err != nil {
		return "", err
	}
	return Type(name), nil
}

// parseNamedType validates primitive, declared, and type-parameter names.
func (c *Checker) parseNamedType(name string) (Type, error) {
	typ := Type(name)
	if typ == typeSelf {
		return typ, nil
	}
	if c.typeParams[name] {
		return typ, nil
	}
	if !knownTypes[typ] && !c.declaredTypes[name] && c.structs[name] == nil &&
		c.enums[name] == nil && c.unions[name] == nil {
		return "", errorf("type error: unknown type `%s`", name)
	}
	return typ, nil
}

// parseErrorUnionType validates `!T` and the typed `Error!T` spelling.
func (c *Checker) parseErrorUnionType(name string, node *typ.ErrorUnion) (Type, error) {
	if node.Err != nil {
		if _, err := c.parseType(node.Err.String()); err != nil {
			return "", err
		}
	}
	return c.parseWrappingType(name, node.Ok)
}

// parseGenericType validates supported generic-like type spellings.
func (c *Checker) parseGenericType(name string, base string, args []string) (Type, error) {
	switch base {
	case "std::mem::Box":
		arg, err := singleGenericArg(base, args)
		if err != nil {
			return "", err
		}
		if _, err := c.parseType(arg); err != nil {
			return "", err
		}
		return Type(name), nil
	case "std::map::Map":
		return c.parseMapType(name, args)
	case "ptr":
		arg, err := singleGenericArg(base, args)
		if err != nil {
			return "", err
		}
		return c.parsePointerType(name, arg)
	}

	if c.structs[base] != nil {
		return c.parseUserGenericType(name, base, args, c.structs[base].TypeParams)
	}
	if union := c.unions[base]; union != nil {
		return c.parseUserGenericType(name, base, args, union.typeParams)
	}

	if !isKnownSingleArgGeneric(base) {
		return "", errorf("type error: unknown generic type `%s`", base)
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

// parseUserGenericType validates static type arguments for user declarations.
func (c *Checker) parseUserGenericType(
	name string,
	base string,
	args []string,
	types []string,
) (Type, error) {
	want := len(types)
	if len(args) != want {
		return "", errorf("type error: `%s` expects %d static arguments", base, want)
	}
	for _, arg := range args {
		if _, err := c.parseType(arg); err != nil {
			return "", err
		}
	}
	return Type(name), nil
}

// parseDynType validates dynamic contract object type spellings.
func (c *Checker) parseDynType(name string, contract typ.Type) (Type, error) {
	contractName := contract.String()
	if c.contracts[contractName] == nil {
		return "", errorf("type error: unknown contract `%s`", contractName)
	}
	return Type(name), nil
}

// parseMapType validates the v0.2 symbol-table map spelling.
func (c *Checker) parseMapType(name string, args []string) (Type, error) {
	if len(args) != 2 {
		return "", errorf("type error: std::map::Map expects 2 static arguments")
	}
	if !sameType(Type(args[0]), typeByteString) && !c.typeParams[args[0]] {
		return "", errorf("type error: std::map::Map key type must be []u8 in v0.2")
	}
	if _, err := c.parseType(args[1]); err != nil {
		return "", err
	}
	if !c.typeParams[args[1]] && !c.isCopyType(Type(args[1])) {
		return "", errorf("type error: std::map::Map value type must be copy in v0.2")
	}
	return Type(name), nil
}

// isKnownSingleArgGeneric reports whether base currently takes exactly one static argument.
func isKnownSingleArgGeneric(base string) bool {
	switch base {
	case "std::arena::Arena", "std::arena::Handle", "option",
		"std::array::Array", "Task", "Channel", "Mutex", "Atomic", "std::mem::Box":
		return true
	default:
		return false
	}
}

// parseNullableType validates nullable pointer types.
func (c *Checker) parseNullableType(name string, elem typ.Type) (Type, error) {
	inner, ok := elem.(*typ.Name)
	if !ok || len(inner.Path) != 1 || inner.Path[0] != "ptr" || len(inner.Args) != 1 {
		return "", errorf("type error: nullable type `%s` must wrap ptr<T>", name)
	}
	if _, err := c.parsePointerType(elem.String(), inner.Args[0].String()); err != nil {
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
		return errorf("type error: public %s exposes private type `%s`", context, name)
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
	typeName = strings.TrimPrefix(typeName, "&var ")
	typeName = strings.TrimPrefix(typeName, "&")
	typeName = strings.TrimPrefix(typeName, "dyn ")
	if base, arg, ok := splitGenericType(typeName); ok {
		names := []string{base}
		for _, part := range splitPublicTypeArgs(arg) {
			names = append(names, referencedTypeNames(part)...)
		}
		return names
	}
	return []string{typeName}
}

// splitPublicTypeArgs splits a static argument list for public API checks.
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

// checkMainReturnType keeps the entry point returning `void` or an error union
// over void (ADR-0085). A program does not choose its own exit status: an exit
// status is platform-shaped, and a value returned from `main` cannot express it
// portably.
func checkMainReturnType(fn *functionType) error {
	if fn.name != "main" || fn.decl == nil {
		return nil
	}
	returned := strings.TrimSpace(fn.decl.ReturnType)
	if returned == "" || returned == "void" || strings.HasSuffix(returned, "!void") {
		return nil
	}
	return errorf("type error: `main` returns `%s`, expected `void` or `!void`", returned)
}

// defineStaticValueParams puts the compile-time values a `<...>` list declares
// into scope, and returns them by declared type. A body reads them like any
// other name, and a static argument list needs to tell them apart from a
// runtime local, so both callers set up a generic body through here.
func defineStaticValueParams(env *scope, decl *ast.FunctionDecl) (map[string]Type, error) {
	staticParams := map[string]Type{}
	for _, param := range decl.StaticParams {
		if param.IsType() {
			continue
		}
		if err := env.defineParam(param.Name, Type(param.Type), false, false); err != nil {
			return nil, err
		}
		staticParams[param.Name] = Type(param.Type)
	}
	return staticParams, nil
}

// checkFunction validates one function body against its signature.
func (c *Checker) checkFunction(fn *functionType) error {
	if fn.externABI != "" {
		return nil
	}
	if err := checkMainReturnType(fn); err != nil {
		return err
	}
	env := newScope(nil)
	staticParams, err := defineStaticValueParams(env, fn.decl)
	if err != nil {
		return err
	}
	for idx, param := range fn.decl.Params {
		if err := env.defineParam(param.Name, fn.params[idx], param.Borrow, param.MutBorrow); err != nil {
			return err
		}
	}
	previousReturn := c.currentReturn
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeParams := c.typeParams
	previousStaticParams := c.staticParams
	previousLoops := c.loopLabels
	c.currentReturn = fn.returnType
	c.currentFunction = fn
	c.currentStd = fn.decl.Std
	c.typeParams = typeParamSet(fn.typeParams)
	c.staticParams = staticParams
	c.loopLabels = nil
	defer func() {
		c.currentReturn = previousReturn
		c.currentFunction = previousFunction
		c.currentStd = previousStd
		c.typeParams = previousTypeParams
		c.staticParams = previousStaticParams
		c.loopLabels = previousLoops
	}()
	returns, err := c.checkBlock(fn.decl.Body, env, fn.returnType, nil)
	if err != nil {
		return err
	}
	if fn.returnType != typeVoid && !fn.implicitReturn && !returns {
		return errorf("type error: function `%s` must return %s", fn.name, fn.returnType)
	}
	return nil
}

// checkTestDecl validates a top-level test block as an errorable, parameterless body.
func (c *Checker) checkTestDecl(decl *ast.TestDecl) error {
	fn := &ast.FunctionDecl{Name: "test " + strconv.Quote(decl.Name), Body: decl.Body}
	return c.checkFunction(&functionType{
		name:           fn.Name,
		returnType:     "!void",
		decl:           fn,
		implicitReturn: true,
	})
}

// checkImpl validates method bodies in an impl block.
func (c *Checker) checkImpl(decl *ast.ImplDecl) error {
	for _, method := range decl.Methods {
		fnType := c.implMethod(decl.TypeName, method.Name)
		if fnType == nil {
			return errorf("type error: missing impl method `%s.%s`", decl.TypeName, method.Name)
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
	unsafe unsafeCaps,
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
	unsafe unsafeCaps,
) (bool, error) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return c.checkLetStmt(s, env, unsafe)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env, unsafe)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(s, env, wantReturn, unsafe)
	case *ast.DeferStmt:
		return c.checkDeferStmt(s, env, unsafe)
	case *ast.ErrDeferStmt:
		return c.checkErrDeferStmt(s, env, unsafe)
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
		return c.checkUnsafeStmt(s, env, wantReturn, unsafe)
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env, wantReturn, unsafe)
	default:
		return false, errorf("type error: unsupported statement %T", stmt)
	}
}

// checkDeferStmt validates the first supported block cleanup registration form.
func (c *Checker) checkDeferStmt(stmt *ast.DeferStmt, env *scope, unsafe unsafeCaps) (bool, error) {
	if err := validateDeferCleanupExpr(stmt.Expr); err != nil {
		return false, err
	}
	got, err := c.checkExpr(stmt.Expr, env, unsafe)
	if err != nil {
		return false, err
	}
	if got != typeVoid {
		return false, errorf("type error: defer cleanup must return void, got %s", got)
	}
	return false, nil
}

// validateDeferCleanupExpr restricts defer to explicit cleanup method calls.
func validateDeferCleanupExpr(expr ast.Expression) error {
	return validateCleanupCallExpr("defer", expr)
}

// checkUnsafeStmt validates an unsafe capability block body.
func (c *Checker) checkUnsafeStmt(
	stmt *ast.UnsafeStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeCaps,
) (bool, error) {
	caps, err := unsafe.with(stmt.Capabilities)
	if err != nil {
		return false, err
	}
	return c.checkBlock(stmt.Body, env.child(), wantReturn, caps)
}

// checkErrDeferStmt validates an error-path cleanup registration. It shares the
// cleanup-call shape with defer; the path-sensitive timing difference is handled
// by lowering and the runtime, not by the type surface.
func (c *Checker) checkErrDeferStmt(
	stmt *ast.ErrDeferStmt,
	env *scope,
	unsafe unsafeCaps,
) (bool, error) {
	if err := validateCleanupCallExpr("errdefer", stmt.Expr); err != nil {
		return false, err
	}
	got, err := c.checkExpr(stmt.Expr, env, unsafe)
	if err != nil {
		return false, err
	}
	if got != typeVoid {
		return false, errorf("type error: errdefer cleanup must return void, got %s", got)
	}
	return false, nil
}

// validateCleanupExpr restricts defer/errdefer to explicit cleanup method calls.
func validateCleanupCallExpr(keyword string, expr ast.Expression) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return errorf("type error: %s expects cleanup method call", keyword)
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "deinit" {
		return errorf("type error: %s expects cleanup method call", keyword)
	}
	return nil
}

// checkLetStmt validates a let or var declaration.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope, unsafe unsafeCaps) (bool, error) {
	handled, err := c.defineSpecialLetInitializer(stmt, env, unsafe)
	if handled || err != nil {
		return false, err
	}
	typ, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if containsTypeValue(typ) {
		return false, errorf("type error: type value cannot be stored in local `%s`", stmt.Name)
	}
	if _, mutable, inner, ok := explicitBorrowType(typ); ok {
		source := c.singleBorrowSource(stmt.Value, env, unsafe)
		return false, env.defineParamWithSource(stmt.Name, inner, true, mutable, source)
	}
	if isBorrowedViewReturnType(typ) {
		source := c.singleBorrowSource(stmt.Value, env, unsafe)
		if source != "" {
			return false, env.defineWithSource(stmt.Name, typ, stmt.Mutable, source)
		}
	}
	return false, env.define(stmt.Name, typ, stmt.Mutable)
}

// defineSpecialLetInitializer records local borrow/view initializers with source data.
func (c *Checker) defineSpecialLetInitializer(
	stmt *ast.LetStmt,
	env *scope,
	unsafe unsafeCaps,
) (bool, error) {
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		typ, mutable, err := c.checkBorrowPrefix(borrow, env, unsafe)
		if err != nil {
			return true, err
		}
		source := c.singleBorrowSource(borrow.Right, env, unsafe)
		return true, env.defineParamWithSource(stmt.Name, typ, true, mutable, source)
	}
	typ, mutable, ok, err := c.checkArrayBorrowInitializer(stmt.Value, env, unsafe)
	if ok || err != nil {
		if err != nil {
			return true, err
		}
		// `Array.at` is declared `-> !&T borrows self`, so the element view is backed by
		// whatever backs the array. Binding it without that provenance made
		// `self.parts.at(index)` read as a fresh owner and refused a view off the element
		// that is in fact tied to `self`. Falling back to the bound name keeps the older,
		// owner-of-itself answer whenever the initializer names no single source.
		source := c.singleBorrowSource(stmt.Value, env, unsafe)
		if source == "" {
			source = stmt.Name
		}
		return true, env.defineParamWithSource(stmt.Name, typ, true, mutable, source)
	}
	typ, mutable, ok, err = c.checkBoxBorrowInitializer(stmt.Value, env, unsafe)
	if ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, env.defineParam(stmt.Name, typ, true, mutable)
	}
	if source, ok, err := c.checkStringViewInitializer(stmt.Value, env, unsafe); ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, env.defineParamWithSource(stmt.Name, typeByteString, true, false, source)
	}
	return false, nil
}

// checkBoxBorrowInitializer recognizes box.borrow/borrow_mut local borrow initializers.
func (c *Checker) checkBoxBorrowInitializer(
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false, false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "borrow" && field.Name != "borrow_mut") {
		return "", false, false, nil
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", false, true, err
	}
	base, elem, ok := splitGenericType(string(receiver))
	if !ok || base != "std::mem::Box" {
		return "", false, true, errorf("type error: `Box.%s` expects Box receiver",
			field.Name)
	}
	if len(call.Args) != 0 {
		return "", false, true, errorf("type error: `Box.%s` expects 0 args, got %d",
			field.Name, len(call.Args))
	}
	if field.Name == "borrow_mut" && !boxBorrowMutReceiverIsMutable(field.Receiver, env) {
		return "", false, true, errorf("type error: `Box.borrow_mut` requires mutable Box receiver")
	}
	return Type(elem), field.Name == "borrow_mut", true, nil
}

// boxBorrowMutReceiverIsMutable accepts a mutable local Box or direct field owner.
func boxBorrowMutReceiverIsMutable(expr ast.Expression, env *scope) bool {
	switch receiver := expr.(type) {
	case *ast.IdentExpr:
		return env.isMutable(receiver.Name)
	case *ast.FieldExpr:
		ident, ok := receiver.Receiver.(*ast.IdentExpr)
		return ok && env.isMutable(ident.Name)
	default:
		return false
	}
}

// checkStringViewInitializer recognizes string.as_bytes() local byte views.
func (c *Checker) checkStringViewInitializer(
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (string, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "as_bytes" {
		return "", false, nil
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", true, err
	}
	if receiver != "std::string::String" {
		return "", true, errorf("type error: `String.as_bytes` expects String receiver")
	}
	if len(call.Args) != 0 {
		return "", true, errorf("type error: `String.as_bytes` expects 0 args, got %d",
			len(call.Args))
	}
	source := c.singleBorrowSource(field.Receiver, env, unsafe)
	return source, true, nil
}

// checkArrayBorrowInitializer recognizes try array.at/at_mut(index) local borrows.
func (c *Checker) checkArrayBorrowInitializer(
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
		return "", false, true, errorf("type error: `Array.%s` expects Array receiver", field.Name)
	}
	if field.Name == "at_mut" {
		if ident, ok := field.Receiver.(*ast.IdentExpr); !ok || !env.isMutable(ident.Name) {
			return "", false, true, errorf("type error: `Array.at_mut` requires mutable array binding")
		}
	}
	if err := c.checkArrayIndexArg(field.Name, call.Args, env, unsafe); err != nil {
		return "", false, true, err
	}
	return Type(elem), field.Name == "at_mut", true, nil
}

// checkAssignStmt validates assignment to an existing binding.
func (c *Checker) checkAssignStmt(
	stmt *ast.AssignStmt,
	env *scope,
	unsafe unsafeCaps,
) (bool, error) {
	want, err := c.checkAssignableTarget(stmt.Target, env, unsafe)
	if err != nil {
		return false, err
	}
	got, err := c.checkContextualExpr(stmt.Value, want, env, unsafe)
	if err != nil {
		return false, err
	}
	if !sameType(got, want) {
		return false, errorAt(expressionSpan(stmt.Target),
			"type error: assignment to `%s` expects %s, got %s",
			stmt.Target.String(), want, got)
	}
	return false, nil
}

// checkAssignableTarget returns the type that a valid assignment target accepts.
func (c *Checker) checkAssignableTarget(
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
		return "", errorf("type error: invalid assignment target `%s`", expr.String())
	}
}

// checkAssignableCall accepts assignment through trusted mutable slot accessors.
func (c *Checker) checkAssignableCall(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	field, ok := expr.Callee.(*ast.FieldExpr)
	if !ok {
		return "", errorf("type error: invalid assignment target `%s`", expr.String())
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if receiver != "Partition" || field.Name != "at" {
		return "", errorf("type error: invalid assignment target `%s`", expr.String())
	}
	return c.checkPartitionMethod(field.Name, expr.Args, env, unsafe)
}

// checkAssignableIdent validates direct binding assignment.
func checkAssignableIdent(expr *ast.IdentExpr, env *scope) (Type, error) {
	want, ok := env.lookup(expr.Name)
	if !ok {
		return "", errorAt(expr.Span, "type error: undefined variable `%s`", expr.Name)
	}
	if !env.isMutable(expr.Name) {
		return "", errorf("type error: cannot assign to immutable binding `%s`", expr.Name)
	}
	return want, nil
}

// checkReturnStmt validates that return value type matches the function result.
func (c *Checker) checkReturnStmt(
	stmt *ast.ReturnStmt,
	env *scope,
	want Type,
	unsafe unsafeCaps,
) (bool, error) {
	if stmt.Value == nil {
		if acceptsBareReturn(want) {
			return true, nil
		}
		return false, errorf("type error: return expects %s, got void", want)
	}
	if ident, ok := stmt.Value.(*ast.IdentExpr); ok && ident.Name == "void" {
		return false, errorf("type error: void is not a value; use `return;`")
	}
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	return c.checkReturnValue(stmt.Value, env, want, got, unsafe)
}

// checkReturnValue validates a non-void return expression against the result type.
func (c *Checker) checkReturnValue(
	expr ast.Expression,
	env *scope,
	want Type,
	got Type,
	unsafe unsafeCaps,
) (bool, error) {
	if ok, err := c.checkErrorUnionReturn(expr, env, want, got, unsafe); ok || err != nil {
		return ok, err
	}
	if c.returnValueMatchesBorrowParam(expr, env, want, got) {
		if err := c.checkReturnBorrowSources(expr, env, want, unsafe); err != nil {
			return false, err
		}
		return true, nil
	}
	coerced, err := c.coerceContextualIntegerLiteral(expr, want, got)
	if err != nil {
		return false, err
	}
	got = coerced
	if !sameType(got, want) {
		return false, errorf("type error: return expects %s, got %s", want, got)
	}
	if err := c.checkReturnBorrowSources(expr, env, want, unsafe); err != nil {
		return false, err
	}
	return true, nil
}

// checkErrorUnionReturn accepts success or error payloads for !T returns.
func (c *Checker) checkErrorUnionReturn(
	expr ast.Expression,
	env *scope,
	want Type,
	got Type,
	unsafe unsafeCaps,
) (bool, error) {
	if elem, ok := errorUnionElement(want); ok {
		success := Type(elem)
		coerced, err := c.coerceContextualIntegerLiteral(expr, success, got)
		if err != nil {
			return false, err
		}
		if sameType(coerced, success) {
			if err := c.checkReturnBorrowSources(expr, env, success, unsafe); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if errorType, elem, ok := errorUnionParts(want); ok {
		success := Type(elem)
		coerced, err := c.coerceContextualIntegerLiteral(expr, success, got)
		if err != nil {
			return false, err
		}
		if sameType(coerced, success) || sameType(got, Type(errorType)) {
			if sameType(coerced, success) {
				if err := c.checkReturnBorrowSources(expr, env, success, unsafe); err != nil {
					return false, err
				}
			}
			return true, nil
		}
	}
	return false, nil
}

// acceptsBareReturn reports whether return without a value satisfies a result type.
func acceptsBareReturn(want Type) bool {
	if want == typeVoid {
		return true
	}
	if elem, ok := errorUnionElement(want); ok && elem == string(typeVoid) {
		return true
	}
	if _, elem, ok := errorUnionParts(want); ok && elem == string(typeVoid) {
		return true
	}
	return false
}

// returnValueMatchesBorrowParam permits returning a named borrow source as &T.
func (c *Checker) returnValueMatchesBorrowParam(
	expr ast.Expression,
	env *scope,
	want Type,
	got Type,
) bool {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok || !env.isBorrowed(ident.Name) {
		return false
	}
	_, mutable, inner, ok := explicitBorrowType(want)
	if !ok {
		return false
	}
	if mutable && !env.isMutBorrowed(ident.Name) {
		return false
	}
	if !sameType(got, inner) {
		return false
	}
	return c.currentFunction != nil && c.currentFunction.returnBorrow == ident.Name
}

// checkReturnBorrowSources rejects returned views not tied to the declared source.
func (c *Checker) checkReturnBorrowSources(
	expr ast.Expression,
	env *scope,
	_ Type,
	unsafe unsafeCaps,
) error {
	if c.currentFunction == nil || c.currentFunction.returnBorrow == "" {
		return nil
	}
	if isErrorConstruction(expr) {
		return nil
	}
	if c.trustedStdBorrowReturn(expr) {
		return nil
	}
	sources, err := c.exprBorrowSources(expr, env, unsafe)
	if err != nil {
		return err
	}
	if sources["$static"] {
		return nil
	}
	source := c.currentFunction.returnBorrow
	if sources[source] {
		return nil
	}
	return errorf("type error: return borrows `%s` but returned value is not tied to that source",
		source)
}

// exprBorrowSources reports parameter names that can back a returned view.
func (c *Checker) exprBorrowSources(
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (map[string]bool, error) {
	switch e := expr.(type) {
	case *ast.StringExpr:
		return map[string]bool{"$static": true}, nil
	case *ast.IdentExpr:
		return c.identBorrowSources(e.Name, env), nil
	case *ast.IndexExpr:
		if !e.Slice {
			return map[string]bool{}, nil
		}
		return c.exprBorrowSources(e.Target, env, unsafe)
	case *ast.TryExpr:
		return c.exprBorrowSources(e.Value, env, unsafe)
	case *ast.CallExpr:
		return c.callBorrowSources(e, env, unsafe)
	case *ast.FieldExpr:
		return c.fieldBorrowSources(e, env, unsafe)
	case *ast.StructLiteralExpr:
		return c.structLiteralBorrowSources(e, env, unsafe)
	default:
		return map[string]bool{}, nil
	}
}

// identBorrowSources derives a local value's borrow provenance from its binding.
func (c *Checker) identBorrowSources(name string, env *scope) map[string]bool {
	source := name
	seen := map[string]bool{}
	for !seen[source] {
		seen[source] = true
		next, ok := env.lookupBorrowSource(source)
		if !ok || next == source {
			return map[string]bool{source: true}
		}
		source = next
	}
	return map[string]bool{}
}

// callBorrowSources maps returned borrow provenance back to call arguments.
func (c *Checker) callBorrowSources(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeCaps,
) (map[string]bool, error) {
	if sources, ok, err := c.methodBorrowSources(expr, env, unsafe); ok || err != nil {
		return sources, err
	}
	fn := c.calledFunction(expr.Callee)
	if fn == nil {
		return map[string]bool{}, nil
	}
	if fn.returnBorrow == "" {
		return map[string]bool{}, nil
	}
	idx := borrowReturnParamIndex(fn)
	if idx < 0 || idx >= len(expr.Args) {
		return map[string]bool{}, errorf("type error: `%s` borrows unknown source `%s`",
			fn.name, fn.returnBorrow)
	}
	return c.exprBorrowSources(expr.Args[idx], env, unsafe)
}

// borrowReturnParamIndex finds the parameter named by a return-borrow annotation.
func borrowReturnParamIndex(fn *functionType) int {
	if fn == nil || fn.decl == nil || fn.returnBorrow == "" {
		return -1
	}
	for idx, param := range fn.decl.Params {
		if param.Name == fn.returnBorrow {
			return idx
		}
	}
	return -1
}

// methodBorrowSources handles built-in method-style view returns.
func (c *Checker) methodBorrowSources(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeCaps,
) (map[string]bool, bool, error) {
	field, ok := expr.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace {
		return nil, false, nil
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return nil, true, err
	}
	if method := c.implMethod(string(receiver), field.Name); method != nil {
		if method.returnBorrow == "" {
			return map[string]bool{}, true, nil
		}
		idx := borrowReturnParamIndex(method)
		if idx < 0 {
			return map[string]bool{}, true, errorf(
				"type error: `%s` borrows unknown source `%s`",
				method.name, method.returnBorrow)
		}
		if idx == 0 {
			sources, err := c.exprBorrowSources(field.Receiver, env, unsafe)
			return sources, true, err
		}
		arg := idx - 1
		if arg >= len(expr.Args) {
			return map[string]bool{}, true, errorf(
				"type error: `%s` borrowed return has no source argument",
				method.name)
		}
		sources, err := c.exprBorrowSources(expr.Args[arg], env, unsafe)
		return sources, true, err
	}
	switch field.Name {
	case "as_bytes", "borrow", "borrow_mut", "at", "at_mut":
		sources, err := c.exprBorrowSources(field.Receiver, env, unsafe)
		return sources, true, err
	default:
		return nil, false, nil
	}
}

// singleBorrowSource extracts a deterministic source name when one source is known.
func (c *Checker) singleBorrowSource(expr ast.Expression, env *scope, unsafe unsafeCaps) string {
	sources, err := c.exprBorrowSources(expr, env, unsafe)
	if err != nil {
		return ""
	}
	source := ""
	for candidate := range sources {
		if candidate == "$static" {
			continue
		}
		if source != "" {
			return ""
		}
		source = candidate
	}
	return source
}

// fieldBorrowSources preserves the receiver provenance through direct fields.
func (c *Checker) fieldBorrowSources(
	expr *ast.FieldExpr,
	env *scope,
	unsafe unsafeCaps,
) (map[string]bool, error) {
	if expr.Namespace {
		return map[string]bool{}, nil
	}
	return c.exprBorrowSources(expr.Receiver, env, unsafe)
}

// structLiteralBorrowSources returns borrow sources from stored field values.
func (c *Checker) structLiteralBorrowSources(
	expr *ast.StructLiteralExpr,
	env *scope,
	unsafe unsafeCaps,
) (map[string]bool, error) {
	decl := c.structs[expr.TypeName]
	if decl == nil {
		return map[string]bool{}, nil
	}
	fields := map[string]ast.Expression{}
	for _, field := range expr.Fields {
		fields[field.Name] = field.Value
	}
	out := map[string]bool{}
	for _, field := range decl.Fields {
		value := fields[field.Name]
		if value == nil {
			continue
		}
		want := fieldDeclaredType(field)
		_ = want
		valueSources, err := c.exprBorrowSources(value, env, unsafe)
		if err != nil {
			return nil, err
		}
		addBorrowSources(out, valueSources)
	}
	return out, nil
}

// calledFunction resolves direct and namespace-qualified source function calls.
func (c *Checker) calledFunction(callee ast.Expression) *functionType {
	switch e := callee.(type) {
	case *ast.IdentExpr:
		return c.functions[e.Name]
	case *ast.FieldExpr:
		name, ok := qualifiedName(e)
		if !ok {
			return nil
		}
		return c.functions[name]
	default:
		return nil
	}
}

// trustedStdBorrowReturn accepts std wrappers around provenance-aware primitives.
func (c *Checker) trustedStdBorrowReturn(expr ast.Expression) bool {
	if !c.currentStd {
		return false
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, ok := callCalleeName(call.Callee)
	if !ok {
		return false
	}
	switch name {
	case "std.builtin.box_borrow", "std.builtin.box_borrow_mut",
		"std.builtin.array_at", "std.builtin.array_at_mut":
		return true
	default:
		return false
	}
}

// callCalleeName resolves direct, qualified, and type-applied call names.
func callCalleeName(callee ast.Expression) (string, bool) {
	if typeApply, ok := callee.(*ast.TypeApplyExpr); ok {
		return callCalleeName(typeApply.Callee)
	}
	switch e := callee.(type) {
	case *ast.IdentExpr:
		return e.Name, true
	case *ast.FieldExpr:
		return qualifiedName(e)
	default:
		return "", false
	}
}

// isErrorConstruction reports whether expr constructs a recoverable error payload.
func isErrorConstruction(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Callee.(*ast.IdentExpr)
	return ok && ident.Name == "error"
}

// explicitBorrowType extracts &T and &var T spellings.
func explicitBorrowType(typ Type) (string, bool, Type, bool) {
	text := string(typ)
	if !strings.HasPrefix(text, "&") {
		return "", false, "", false
	}
	rest := strings.TrimPrefix(text, "&")
	mutable := false
	if strings.HasPrefix(rest, "var ") {
		mutable = true
		rest = strings.TrimPrefix(rest, "var ")
	}
	if rest == "" {
		return "", false, "", false
	}
	return "", mutable, Type(rest), true
}

// fieldDeclaredType returns the full field type, including borrow prefixes.
func fieldDeclaredType(field ast.Field) Type {
	return Type(borrowWrappedType(field.Borrow, field.MutBorrow, field.TypeName))
}

// addBorrowSources unions src into dst.
func addBorrowSources(dst map[string]bool, src map[string]bool) {
	for source := range src {
		dst[source] = true
	}
}

// checkIfStmt validates a branch and tracks whether both arms return.
func (c *Checker) checkIfStmt(
	stmt *ast.IfStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeCaps,
) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, errorf("type error: if condition must be bool, got %s", cond)
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
	unsafe unsafeCaps,
) (bool, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return false, err
	}
	if cond != typeBool {
		return false, errorf("type error: while condition must be bool, got %s", cond)
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
	unsafe unsafeCaps,
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
func (c *Checker) checkForBounds(stmt *ast.ForStmt, env *scope, unsafe unsafeCaps) error {
	start, err := c.checkExpr(stmt.Start, env, unsafe)
	if err != nil {
		return err
	}
	end, err := c.checkExpr(stmt.End, env, unsafe)
	if err != nil {
		return err
	}
	if start != typeI64 || end != typeI64 {
		return errorf("type error: for range expects i64 bounds, got %s..%s", start, end)
	}
	return nil
}

// enterLoop records an active loop label for branch target validation.
func (c *Checker) enterLoop(label string) (func(), error) {
	if label != "" && c.hasLoopLabel(label) {
		return nil, errorf("type error: duplicate loop label `%s`", label)
	}
	c.loopLabels = append(c.loopLabels, label)
	return func() {
		c.loopLabels = c.loopLabels[:len(c.loopLabels)-1]
	}, nil
}

// checkLoopBranch validates break and continue placement.
func (c *Checker) checkLoopBranch(kind string, label string) error {
	if len(c.loopLabels) == 0 {
		return errorf("type error: `%s` used outside loop", kind)
	}
	if label != "" && !c.hasLoopLabel(label) {
		return errorf("type error: unknown loop label `%s`", label)
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
	unsafe unsafeCaps,
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
	return false, errorf("type error: match expects enum or union, got %s", valueType)
}

// checkMatchArms validates tag patterns and return flow for match arms.
func (c *Checker) checkMatchArms(
	arms []ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	env *scope,
	wantReturn Type,
	unsafe unsafeCaps,
) (bool, error) {
	seen := map[string]bool{}
	wildcard := false
	allReturn := len(arms) > 0
	for idx, arm := range arms {
		payload := ""
		if arm.IsWildcard() {
			if err := validateWildcardMatchArm(arm, idx, len(arms)); err != nil {
				return false, err
			}
			if wildcard {
				return false, errorf("type error: duplicate wildcard match arm")
			}
			wildcard = true
		} else {
			got, err := matchPayloadType(enumType, unionType, arm)
			if err != nil {
				return false, err
			}
			typeName := matchTypeName(enumType, unionType)
			if seen[arm.Tag] {
				return false, errorf("type error: duplicate match tag `%s::%s`",
					typeName, arm.Tag)
			}
			seen[arm.Tag] = true
			payload = got
		}
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
	if !wildcard && len(seen) != matchVariantCount(enumType, unionType) {
		return false, errorf("type error: match on `%s` is not exhaustive: missing %s",
			matchTypeName(enumType, unionType),
			strings.Join(missingMatchVariants(enumType, unionType, seen), ", "))
	}
	return allReturn, nil
}

// validateWildcardMatchArm checks the restricted fallback pattern shape.
func validateWildcardMatchArm(arm ast.MatchArm, idx int, count int) error {
	if arm.Binding != "" {
		return errorf("type error: wildcard match arm cannot bind payload")
	}
	if idx != count-1 {
		return errorf("type error: wildcard match arm must be last")
	}
	return nil
}

// matchPayloadType validates a match arm pattern and returns its payload type.
func matchPayloadType(enumType *enumType, unionType *unionType, arm ast.MatchArm) (string, error) {
	if enumType != nil {
		if !enumType.tags[arm.Tag] {
			return "", errorf("type error: unknown match tag `%s::%s`", enumType.name, arm.Tag)
		}
		if arm.Binding != "" {
			return "", errorf("type error: enum tag `%s::%s` has no payload",
				enumType.name, arm.Tag)
		}
		return "", nil
	}
	payload, ok := unionType.variants[arm.Tag]
	if !ok {
		return "", errorf("type error: unknown match tag `%s::%s`", unionType.name, arm.Tag)
	}
	if payload == "" && arm.Binding != "" {
		return "", errorf("type error: union variant `%s::%s` has no payload",
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

// missingMatchVariants lists the variants a match does not cover, sorted so the message is
// stable. Match arms carry no source position, so the type name alone left the reader searching
// every match on that type; naming what is missing points at the arm to add instead.
func missingMatchVariants(
	enumType *enumType,
	unionType *unionType,
	seen map[string]bool,
) []string {
	var missing []string
	if enumType != nil {
		for tag := range enumType.tags {
			if !seen[tag] {
				missing = append(missing, tag)
			}
		}
	} else {
		for variant := range unionType.variants {
			if !seen[variant] {
				missing = append(missing, variant)
			}
		}
	}
	sort.Strings(missing)
	return missing
}

// checkExpr computes the static type of an expression.
func (c *Checker) checkExpr(expr ast.Expression, env *scope, unsafe unsafeCaps) (Type, error) {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.TypeExpr:
		return c.checkScalarExpr(e)
	case *ast.ComptimeExpr:
		return c.checkComptimeExpr(e, env, unsafe)
	case *ast.IdentExpr:
		return c.checkIdentExpr(e, env)
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
		return c.checkArenaNewExpr(e, env, unsafe)
	case *ast.StructLiteralExpr:
		return c.checkStructLiteralExpr(e, env, unsafe)
	case *ast.FieldExpr:
		return c.checkFieldExpr(e, env, unsafe)
	case *ast.DerefExpr:
		return c.checkDerefExpr(e, env, unsafe)
	default:
		return c.checkControlExpr(expr, env, unsafe)
	}
}

// checkScalarExpr computes types for literal-like scalar expressions.
func (c *Checker) checkScalarExpr(expr ast.Expression) (Type, error) {
	if typeExpr, ok := expr.(*ast.TypeExpr); ok {
		if _, err := c.parseType(typeExpr.TypeName); err != nil {
			return "", err
		}
		return typeType, nil
	}
	return literalType(expr)
}

// checkControlExpr validates statement-compatible control flow used as expressions.
func (c *Checker) checkControlExpr(
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	switch e := expr.(type) {
	case *ast.IfStmt:
		return c.checkIfExpr(e, env, unsafe)
	case *ast.MatchStmt:
		return c.checkMatchExpr(e, env, unsafe)
	default:
		return "", errorf("type error: unsupported expression %T", expr)
	}
}

// checkIfExpr validates an if expression and returns the common branch type.
func (c *Checker) checkIfExpr(stmt *ast.IfStmt, env *scope, unsafe unsafeCaps) (Type, error) {
	cond, err := c.checkExpr(stmt.Condition, env, unsafe)
	if err != nil {
		return "", err
	}
	if cond != typeBool {
		return "", errorf("type error: if condition must be bool, got %s", cond)
	}
	if stmt.Alternative == nil {
		return "", errorf("type error: if expression requires else branch")
	}
	left, err := c.checkBlockValue(stmt.Consequence, env.child(), unsafe)
	if err != nil {
		return "", err
	}
	right, err := c.checkBlockValue(stmt.Alternative, env.child(), unsafe)
	if err != nil {
		return "", err
	}
	if left != right {
		return "", errorf("type error: if expression branch types differ: %s vs %s",
			left, right)
	}
	return left, nil
}

// checkBlockValue validates a block used as an expression branch.
func (c *Checker) checkBlockValue(
	block *ast.BlockStmt,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if block == nil || len(block.Statements) == 0 {
		return "", errorf("type error: expression block must end with a value")
	}
	for _, stmt := range block.Statements[:len(block.Statements)-1] {
		returns, err := c.checkStmt(stmt, env, c.currentReturn, unsafe)
		if err != nil {
			return "", err
		}
		if returns {
			return "", errorf("type error: expression block cannot contain early return")
		}
	}
	return c.checkStmtValue(block.Statements[len(block.Statements)-1], env, unsafe)
}

// checkStmtValue computes the value type of a statement in expression-tail position.
func (c *Checker) checkStmtValue(stmt ast.Statement, env *scope, unsafe unsafeCaps) (Type, error) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if s.Semicolon {
			return "", errorf("type error: expression block must end with a value")
		}
		return c.checkExpr(s.Expr, env, unsafe)
	case *ast.IfStmt:
		return c.checkIfExpr(s, env, unsafe)
	case *ast.MatchStmt:
		return c.checkMatchExpr(s, env, unsafe)
	default:
		return "", errorf("type error: expression block must end with a value")
	}
}

// checkMatchExpr validates an exhaustive match expression and its arm result type.
func (c *Checker) checkMatchExpr(stmt *ast.MatchStmt, env *scope, unsafe unsafeCaps) (Type, error) {
	valueType, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	enumType := c.enums[string(valueType)]
	unionType := c.unions[string(valueType)]
	if enumType == nil && unionType == nil {
		return "", errorf("type error: match expects enum or union, got %s", valueType)
	}
	return c.checkMatchExprArms(stmt.Arms, enumType, unionType, env, unsafe)
}

// checkMatchExprArms validates match expression arms and returns their common type.
func (c *Checker) checkMatchExprArms(
	arms []ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	seen := map[string]bool{}
	wildcard := false
	var result Type
	for idx, arm := range arms {
		var got Type
		if arm.IsWildcard() {
			if err := validateWildcardMatchArm(arm, idx, len(arms)); err != nil {
				return "", err
			}
			if wildcard {
				return "", errorf("type error: duplicate wildcard match arm")
			}
			wildcard = true
			var err error
			got, err = c.checkStmtValue(arm.Body, env.child(), unsafe)
			if err != nil {
				return "", err
			}
		} else {
			var err error
			got, err = c.checkMatchExprArm(arm, enumType, unionType, env, unsafe)
			if err != nil {
				return "", err
			}
			if seen[arm.Tag] {
				return "", errorf("type error: duplicate match tag `%s::%s`",
					matchTypeName(enumType, unionType), arm.Tag)
			}
			seen[arm.Tag] = true
		}
		if idx == 0 {
			result = got
		} else if got != result {
			return "", errorf("type error: match arm types differ: %s vs %s", result, got)
		}
	}
	if !wildcard && len(seen) != matchVariantCount(enumType, unionType) {
		return "", errorf("type error: match on `%s` is not exhaustive: missing %s",
			matchTypeName(enumType, unionType),
			strings.Join(missingMatchVariants(enumType, unionType, seen), ", "))
	}
	return result, nil
}

// checkMatchExprArm validates one match expression arm and returns its value type.
func (c *Checker) checkMatchExprArm(
	arm ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if arm.IsWildcard() {
		return c.checkStmtValue(arm.Body, env.child(), unsafe)
	}
	payload, err := matchPayloadType(enumType, unionType, arm)
	if err != nil {
		return "", err
	}
	armEnv := env.child()
	if payload != "" && arm.Binding != "" {
		if err := armEnv.define(arm.Binding, Type(payload), false); err != nil {
			return "", err
		}
	}
	return c.checkStmtValue(arm.Body, armEnv, unsafe)
}

// checkIndexExpr validates checked one-dimensional byte indexing and slicing.
func (c *Checker) checkIndexExpr(expr *ast.IndexExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	target, err := c.checkExpr(expr.Target, env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(target, typeByteString) {
		return "", errorf("type error: index/slice target expects []u8, got %s", target)
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
	return target, nil
}

// checkIndexBound validates one i64 index or slice bound.
func (c *Checker) checkIndexBound(
	name string,
	expr ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if expr == nil {
		return errorf("type error: %s is missing", name)
	}
	got, err := c.checkExpr(expr, env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return errorf("type error: %s expects i64, got %s", name, got)
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
		return "", errorf("type error: unsupported literal %T", expr)
	}
}

// checkContextualExpr validates an expression and narrows integer literals to want.
func (c *Checker) checkContextualExpr(
	expr ast.Expression,
	want Type,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	got, err := c.checkExpr(expr, env, unsafe)
	if err != nil {
		return "", err
	}
	return c.coerceContextualIntegerLiteral(expr, want, got)
}

// coerceContextualIntegerLiteral treats fit-checked integer literals as want.
func (c *Checker) coerceContextualIntegerLiteral(
	expr ast.Expression,
	want Type,
	got Type,
) (Type, error) {
	if sameType(got, want) || got != typeI64 || !integerTypes[want] {
		return got, nil
	}
	value, ok := integerLiteralValue(expr)
	if !ok {
		return got, nil
	}
	if !integerLiteralFitsType(value, want) {
		return "", errorf("type error: integer literal `%s` does not fit %s",
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

// integerLiteralFitsType checks fixed-width integer bounds used by contextual typing.
func integerLiteralFitsType(value int64, typ Type) bool {
	switch typ {
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

// checkIdentExpr resolves variables and instantiated compile-time type params.
func (c *Checker) checkIdentExpr(expr *ast.IdentExpr, env *scope) (Type, error) {
	typ, ok := env.lookup(expr.Name)
	if ok {
		return typ, nil
	}
	if _, ok := c.typeArgValues[expr.Name]; ok {
		return typeType, nil
	}
	if expr.Name == "void" {
		return "", errorAt(expr.Span, "type error: void is not a value")
	}
	return "", errorAt(expr.Span, "type error: undefined variable `%s`", expr.Name)
}

// checkPrefixExpr validates unary operators.
func (c *Checker) checkPrefixExpr(
	expr *ast.PrefixExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if expr.Operator == "&" || expr.Operator == "&var" {
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
			return "", errorf("type error: unary - expects signed numeric, got %s", right)
		}
		return right, nil
	case "!":
		if right != typeBool {
			return "", errorf("type error: unary ! expects bool, got %s", right)
		}
		return typeBool, nil
	default:
		return "", errorf("type error: unsupported unary `%s`", expr.Operator)
	}
}

// checkBorrowPrefix validates an explicit local borrow expression.
func (c *Checker) checkBorrowPrefix(
	expr *ast.PrefixExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	mutable := expr.Operator == "&var"
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
func (c *Checker) checkBinaryExpr(
	expr *ast.BinaryExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	left, err := c.checkExpr(expr.Left, env, unsafe)
	if err != nil {
		return "", err
	}
	right, err := c.checkExpr(expr.Right, env, unsafe)
	if err != nil {
		return "", err
	}
	if expr.Operator == "and" || expr.Operator == "or" {
		return checkLogical(expr.Operator, left, right, expr.OperatorSpan)
	}
	if expr.Operator == "==" || expr.Operator == "!=" {
		return checkEquality(expr.Operator, left, right, expr.OperatorSpan)
	}
	if left != right {
		return "", operatorTypeMismatch(expr.Operator, left, right, expr.OperatorSpan)
	}
	if !numericTypes[left] {
		return "", operatorOperandKindError(expr.Operator, "numeric", left, right, expr.OperatorSpan)
	}
	if expr.Operator == "%" && !integerTypes[left] {
		return "", operatorOperandKindError(expr.Operator, "integer", left, right, expr.OperatorSpan)
	}
	if isComparison(expr.Operator) {
		return typeBool, nil
	}
	return left, nil
}

// checkLogical validates boolean logical operands.
func checkLogical(op string, left Type, right Type, span ast.Span) (Type, error) {
	if left != typeBool || right != typeBool {
		return "", errorAt(span,
			"type error: operator `%s` expects bool operands\n"+
				"note: left operand has type %s\n"+
				"note: right operand has type %s",
			op, left, right)
	}
	return typeBool, nil
}

// checkEquality validates equality operands.
func checkEquality(op string, left Type, right Type, span ast.Span) (Type, error) {
	if left != right {
		return "", operatorTypeMismatch(op, left, right, span)
	}
	return typeBool, nil
}

// operatorTypeMismatch reports both operand types for binary mismatch errors.
func operatorTypeMismatch(op string, left Type, right Type, span ast.Span) error {
	return errorAtCode(span,
		"type.operator_type_mismatch",
		"type error: operator `%s` operands must have same type\n"+
			"note: left operand has type %s\n"+
			"note: right operand has type %s",
		op, left, right)
}

// operatorOperandKindError reports non-matching operand categories.
func operatorOperandKindError(op string, want string, left Type, right Type, span ast.Span) error {
	return errorAt(span,
		"type error: operator `%s` expects %s operands\n"+
			"note: left operand has type %s\n"+
			"note: right operand has type %s",
		op, want, left, right)
}

// expressionSpan returns the best source span stored on an expression.
func expressionSpan(expr ast.Expression) ast.Span {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Span
	case *ast.BinaryExpr:
		return e.OperatorSpan
	case *ast.CallExpr:
		return expressionSpan(e.Callee)
	case *ast.TypeApplyExpr:
		return expressionSpan(e.Callee)
	case *ast.CastExpr:
		return e.KeywordSpan
	case *ast.FieldExpr:
		return e.Span
	case *ast.DerefExpr:
		return e.OperatorSpan
	default:
		return ast.Span{}
	}
}

// isComparison reports whether op returns bool for numeric operands.
func isComparison(op string) bool {
	return op == "<" || op == "<=" || op == ">" || op == ">="
}

// checkCastExpr validates explicit low-level casts.
func (c *Checker) checkCastExpr(expr *ast.CastExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	target, err := c.parseType(expr.TargetType)
	if err != nil {
		return "", err
	}
	source, err := c.checkExpr(expr.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	if ok, err := c.checkErrorUnionCast(source, target); ok || err != nil {
		return target, err
	}
	if numericTypes[source] && numericTypes[target] {
		return target, nil
	}
	if isPointerType(source) && isPointerType(target) {
		if err := requireUnsafeCapabilityAt(
			unsafe,
			unsafePtrCast,
			"pointer cast",
			expr.KeywordSpan,
		); err != nil {
			return "", err
		}
		return target, nil
	}
	return "", errorf("type error: cannot cast %s to %s", source, target)
}

// checkErrorUnionCast validates explicit untyped-to-typed error adaptation.
func (c *Checker) checkErrorUnionCast(source Type, target Type) (bool, error) {
	targetError, targetSuccess, targetOK := errorUnionParts(target)
	if !targetOK || targetError == "" {
		return false, nil
	}
	sourceError, sourceSuccess, sourceOK := errorUnionParts(source)
	if !sourceOK || sourceError != "" || !sameType(Type(sourceSuccess), Type(targetSuccess)) {
		return false, nil
	}
	if !c.unionHasMessageVariant(targetError) {
		return true, errorf(
			"type error: typed error cast requires %s::Message([]u8)",
			targetError,
		)
	}
	return true, nil
}

// unionHasMessageVariant reports whether a union can hold an untyped error message.
func (c *Checker) unionHasMessageVariant(name string) bool {
	union := c.unions[name]
	if union == nil {
		return false
	}
	return sameType(Type(union.variants["Message"]), typeByteString)
}

// checkTryExpr validates error-union propagation and returns the success type.
func (c *Checker) checkTryExpr(expr *ast.TryExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if _, _, ok := errorUnionParts(c.currentReturn); !ok {
		return "", errorf("type error: try requires function to return !T")
	}
	source, err := c.checkExpr(expr.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	sourceError, elem, ok := errorUnionParts(source)
	if !ok {
		return "", errorf("type error: try expects !T, got %s", source)
	}
	targetError, _, _ := errorUnionParts(c.currentReturn)
	if sourceError != targetError {
		return "", errorf("type error: try cannot propagate %s from %s", sourceError, source)
	}
	return Type(elem), nil
}

// checkCallExpr validates builtin and user function calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return c.checkFieldCallExpr(field, expr.Args, env, unsafe)
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return c.checkTypeApplyCallExpr(typeApply, expr.Args, env, unsafe)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", errorf("type error: callee must be a function name")
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
	if name.Name == "volatile_read" {
		return c.checkVolatileRead(expr, env, unsafe)
	}
	if name.Name == "volatile_write" {
		return c.checkVolatileWrite(expr, env, unsafe)
	}
	if name.Name == "error" {
		return c.checkErrorCall(expr, env, unsafe)
	}
	if name.Name == "Io" {
		return "", errorf("type error: use `std::io::blocking()`")
	}
	if name.Name == "TaskGroup" {
		return "", errorf("type error: use `std::task::Group(io)`")
	}
	return c.checkUserCall(name.Name, name.Span, expr.Args, env, unsafe)
}

// checkFieldCallExpr validates qualified, union, and method calls.
func (c *Checker) checkFieldCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
	unsafe unsafeCaps,
) (Type, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	fn, ok := c.functions[name]
	if !ok {
		return "", false, nil
	}
	if err := c.rejectPrivateStdFunction(name, fn); err != nil {
		return "", true, err
	}
	if len(fn.typeParams) > 0 {
		return "", false, nil
	}
	typ, err := c.checkUserCall(name, expressionSpan(field), args, env, unsafe)
	return typ, true, err
}

// rejectPrivateStdFunction blocks non-std source from std-private helpers.
func (c *Checker) rejectPrivateStdFunction(name string, fn *functionType) error {
	if !fn.decl.Std || fn.decl.Public || c.currentStd {
		return nil
	}
	sourceName := strings.ReplaceAll(name, ".", "::")
	return errorf("type error: function `%s` is private", sourceName)
}

// checkQualifiedBuiltin validates std:: namespace prototype calls.
func (c *Checker) checkQualifiedBuiltin(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
	unsafe unsafeCaps,
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
	return c.checkSimpleCoreBuiltin(name, args, env, unsafe)
}

// checkStdRuntimeBuiltin validates task and constructor std calls.
func (c *Checker) checkStdRuntimeBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if replacement, ok := stdprim.RemovedBuiltinReplacement(name); ok {
		return "", true, errorf("type error: `%s` was removed; use %s", name, replacement)
	}
	if typ, ok, err := c.checkTaskBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	return c.checkStdConstructorBuiltin(name, args)
}

// checkStdConstructorBuiltin validates miscellaneous std constructor calls.
func (c *Checker) checkStdConstructorBuiltin(
	name string,
	args []ast.Expression,
) (Type, bool, error) {
	switch name {
	case "std.builtin.io_blocking", "std.builtin.io_threaded", "std.builtin.io_failing":
		typ, err := checkNoArgConstructor(name, args, "Io")
		return typ, true, err
	case "std.io.evented", "std.builtin.io_evented":
		return "", true, errorf("type error: `std::io::evented` is not implemented in v0.1")
	case "std.array.Array":
		return "", true, errorf("type error: use `std::array::Array<T>(allocator)`")
	case "std.map.Map":
		return "", true, errorf("type error: use `std::map::Map<K, V>(allocator)`")
	case "std.channel.Channel":
		return "", true, errorf("type error: use `std::channel::Channel<T>()`")
	case "std.atomic.Atomic":
		return "", true, errorf("type error: use `std::atomic::Atomic<T>(value)`")
	case "std.atomic.AtomicI64":
		return "", true, errorf("type error: use `std::atomic::Atomic<i64>(value)`")
	case "std.sync.Mutex":
		return "", true, errorf("type error: use `std::sync::Mutex<T>(value)`")
	default:
		return "", false, nil
	}
}

// checkIoBuiltin validates explicit-Io stdio helpers.
func (c *Checker) checkIoBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	return c.checkSimpleCoreBuiltin(name, args, env, unsafe)
}

// checkProcessBuiltin validates minimal process helpers for CLI prototypes.
func (c *Checker) checkProcessBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	return c.checkSimpleCoreBuiltin(name, args, env, unsafe)
}

// checkSimpleCoreBuiltin validates declarative core primitive signatures.
func (c *Checker) checkSimpleCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	signature, ok := stdprim.SimpleCoreSignatures[name]
	if !ok {
		return "", false, nil
	}
	if len(args) != len(signature.Args) {
		return "", true, errorf("type error: `%s` expects %s", name,
			coreSignatureArgsText(signature.Args))
	}
	for idx, arg := range args {
		if err := c.checkCoreArg(name, idx, signature.Args[idx], arg, env, unsafe); err != nil {
			return "", true, err
		}
	}
	return Type(signature.Return), true, nil
}

// checkCoreArg validates one declarative primitive argument.
func (c *Checker) checkCoreArg(
	name string,
	index int,
	want stdprim.ArgKind,
	arg ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if want == stdprim.ArgIo {
		return c.checkIoArg(arg, env, unsafe, name)
	}
	got, err := c.checkContextualExpr(arg, Type(want), env, unsafe)
	if err != nil {
		return err
	}
	if !sameType(got, Type(want)) {
		return errorf("type error: `%s` arg %d expects %s, got %s",
			name, index+1, want, got)
	}
	return nil
}

// coreSignatureArgsText renders declarative primitive arguments for diagnostics.
func coreSignatureArgsText(args []stdprim.ArgKind) string {
	if len(args) == 0 {
		return "0 args"
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case stdprim.ArgIo:
			parts = append(parts, "io")
		case stdprim.ArgBytes:
			parts = append(parts, "[]u8")
		default:
			parts = append(parts, string(arg))
		}
	}
	return strings.Join(parts, " and ")
}

// checkMemBuiltin validates allocation-free std::mem byte-slice helpers.
func (c *Checker) checkMemBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	return c.checkSimpleCoreBuiltin(name, args, env, unsafe)
}

// checkFsBuiltin validates filesystem host primitives with explicit Io.
func (c *Checker) checkFsBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	switch name {
	case "std.builtin.fs_read_file":
		return c.checkFsReadFile(args, env, unsafe)
	case "std.builtin.fs_write_file":
		return c.checkFsWriteFile(args, env, unsafe)
	case "std.builtin.fs_exists":
		return c.checkFsExists(args, env, unsafe)
	case "std.builtin.fs_metadata":
		return c.checkFsMetadata(args, env, unsafe)
	case "std.builtin.fs_read_dir":
		return c.checkFsReadDir(args, env, unsafe)
	case "std.builtin.fs_create_dir", "std.builtin.fs_remove_dir", "std.builtin.fs_remove_file":
		return c.checkFsPathOnly(name, args, env, unsafe, "!void")
	case "std.builtin.fs_rename":
		return c.checkFsRename(args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkTaskBuiltin validates structured task and data-parallel std calls.
func (c *Checker) checkTaskBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if strings.HasPrefix(name, "std.builtin.task_") && !c.currentStd {
		return "", true, errorf("type error: `%s` is reserved; use std::task", name)
	}
	switch name {
	case "std.builtin.task_group":
		return c.checkTaskGroup(args, env, unsafe)
	case "std.builtin.task_queue":
		typ, err := checkNoArgConstructor(name, args, "Queue")
		return typ, true, err
	case "std.builtin.task_partition_mut":
		return c.checkPartitionMut(args, env, unsafe)
	case "std.builtin.task_local_buffer":
		return c.checkLocalBuffer(args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkBuiltinTaskTypeApply validates the task primitives that take a worker as
// a static argument.
func (c *Checker) checkBuiltinTaskTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	switch name {
	case "std.builtin.task_parallel_for", "std.builtin.task_parallel_map":
		if !c.currentStd {
			return "", true, errorf("type error: `%s` is reserved; use std::task", name)
		}
	}
	switch name {
	case "std.builtin.task_parallel_for":
		return c.checkParallelFor(typeArg, args, env, unsafe)
	case "std.builtin.task_parallel_map":
		return c.checkParallelMap(typeArg, args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkFsReadFile validates std::fs::read_file.
func (c *Checker) checkFsReadFile(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("type error: `std::fs::read_file` expects io and path")
	}
	if err := c.checkIoArg(args[0], env, unsafe, "std::fs::read_file"); err != nil {
		return "", true, err
	}
	path, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if !sameType(path, typeByteString) {
		return "", true, errorf("type error: `std::fs::read_file` expects []u8 path, got %s",
			path)
	}
	return "![]u8", true, nil
}

// checkFsWriteFile validates std::fs::write_file.
func (c *Checker) checkFsWriteFile(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("type error: `std::fs::write_file` expects io, path, and bytes")
	}
	if err := c.checkIoArg(args[0], env, unsafe, "std::fs::write_file"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"path", "bytes"} {
		got, err := c.checkExpr(args[idx+1], env, unsafe)
		if err != nil {
			return "", true, err
		}
		if !sameType(got, typeByteString) {
			return "", true, errorf(
				"type error: `std::fs::write_file` expects []u8 %s, got %s", label, got)
		}
	}
	return "!void", true, nil
}

// checkFsRename validates std::fs::rename.
func (c *Checker) checkFsRename(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("type error: `std::fs::rename` expects io, from, and to")
	}
	if err := c.checkIoArg(args[0], env, unsafe, "std::fs::rename"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"from", "to"} {
		got, err := c.checkExpr(args[idx+1], env, unsafe)
		if err != nil {
			return "", true, err
		}
		if !sameType(got, typeByteString) {
			return "", true, errorf(
				"type error: `std::fs::rename` expects []u8 %s, got %s", label, got)
		}
	}
	return "!void", true, nil
}

// checkFsExists validates std::fs::exists.
func (c *Checker) checkFsExists(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::exists", args, env, unsafe)
	return "!bool", true, err
}

// checkFsMetadata validates std::fs::metadata.
func (c *Checker) checkFsMetadata(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::metadata", args, env, unsafe)
	return "!std::fs::Metadata", true, err
}

// checkFsReadDir validates std::fs::read_dir.
func (c *Checker) checkFsReadDir(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::read_dir", args, env, unsafe)
	return "!std::array::Array<std::fs::DirEntry>", true, err
}

// checkFsPathOnly validates an Io plus path API and returns result.
func (c *Checker) checkFsPathOnly(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
	unsafe unsafeCaps,
) (Type, Type, error) {
	if len(args) != 2 {
		return "", "", errorf("type error: `%s` expects io and path", name)
	}
	if err := c.checkIoArg(args[0], env, unsafe, name); err != nil {
		return "", "", err
	}
	path, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", "", err
	}
	if !sameType(path, typeByteString) {
		return "", "", errorf("type error: `%s` expects []u8 path, got %s", name, path)
	}
	return "Io", path, nil
}

// checkArrayConstructor validates std::array::Array<T>(allocator).
func (c *Checker) checkArrayConstructor(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if !c.typeParams[string(elem)] {
		if err := c.rejectArrayElementType(elem); err != nil {
			return "", true, err
		}
	}
	if len(args) != 1 {
		return "", true, errorf("type error: `std::array::Array<%s>` expects allocator", elem)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Allocator" {
		return "", true, errorf("type error: `std::array::Array<%s>` expects Allocator, got %s",
			elem, got)
	}
	return Type(fmt.Sprintf("std::array::Array<%s>", elem)), true, nil
}

// rejectArrayElementType rejects element types that need unresolved lifetime rules.
func (c *Checker) rejectArrayElementType(elem Type) error {
	if err := c.rejectArrayStorageType(elem, map[Type]bool{}); err != nil {
		return errorf("type error: Array element is not safe in v0.2: %w", err)
	}
	return nil
}

// rejectArrayStorageType rejects values that are not Array-safe yet.
func (c *Checker) rejectArrayStorageType(typ Type, seen map[Type]bool) error {
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	if isAstNodeIDType(typ) {
		return nil
	}
	if isPointerType(typ) {
		return errorf("type error: Array element cannot be raw pointer in v0.2")
	}
	if _, ok := dynContract(typ); ok {
		return errorf("type error: Array element cannot be dyn in v0.2")
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
	case "std::arena::Arena", "std::arena::Handle", "std::array::Array", "std::map::Map":
		return nil
	case "Task", "Channel", "Mutex", "Atomic":
		return errorf("type error: Array element cannot be %s in v0.2", base)
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
			return errorf("type error: struct `%s.%s` cannot be Array element: %w",
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
			return errorf("type error: union `%s::%s` cannot be Array element: %w",
				typ, variant, err)
		}
	}
	return nil
}

// checkIoArg validates an explicit Io argument for a std call.
func (c *Checker) checkIoArg(arg ast.Expression, env *scope, unsafe unsafeCaps, name string) error {
	got, err := c.checkExpr(arg, env, unsafe)
	if err != nil {
		return err
	}
	if got != "Io" {
		return errorf("type error: `%s` expects Io, got %s", name, got)
	}
	return nil
}

// checkTypeApplyCallExpr validates typed std constructor calls.
func (c *Checker) checkTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return "", errorf("type error: unsupported type application `%s`", expr.String())
	}
	typeArg := c.instantiateTypeArgText(expr.TypeArg)
	if name == "ptr_from_int" {
		return c.checkPtrFromInt(typeArg, expressionSpan(expr.Callee), args, env, unsafe)
	}
	if name == "int_from_ptr" {
		return c.checkIntFromPtr(typeArg, expressionSpan(expr.Callee), args, env, unsafe)
	}
	if name == "std.arena.Arena" {
		return c.checkArenaTypeApply(typeArg, args, env, unsafe)
	}
	if typ, ok, err := c.checkGenericUserTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinMapTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinThreadScopedTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, err
	}
	switch name {
	case "std.sync.Mutex":
		arg, err := c.parseType(typeArg)
		if err != nil {
			return "", err
		}
		typ, _, err := c.checkMutex(arg, args, env, unsafe)
		return typ, err
	default:
		return "", errorf("type error: `%s` does not take static arguments", name)
	}
}

// checkArenaTypeApply validates std::arena::Arena<T>(allocator).
func (c *Checker) checkArenaTypeApply(
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	parts, ok := splitGenericArgs(typeArg)
	if !ok || len(parts) != 1 {
		return "", errorf("type error: std::arena::Arena expects 1 type argument")
	}
	elem, err := c.parseType(parts[0])
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", errorf(
			"type error: `std::arena::Arena<%s>` expects exactly one allocator argument",
			elem)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("type error: `std::arena::Arena<%s>` expects Allocator, got %s",
			elem, got)
	}
	return Type(fmt.Sprintf("std::arena::Arena<%s>", elem)), nil
}

// checkBuiltinTypeApply validates std-only generic runtime primitives.
func (c *Checker) checkBuiltinTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if typ, ok, err := c.checkBuiltinMethodTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkBuiltinTestingTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkBuiltinTaskTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, ok, err
	}
	return c.checkBuiltinConstructorTypeApply(name, typeArg, args, env, unsafe)
}

// checkBuiltinTestingTypeApply validates typed std::testing primitives.
func (c *Checker) checkBuiltinTestingTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if name != "std.builtin.test_fail_equal" {
		return "", false, nil
	}
	if !c.currentStd {
		return "", true, errorf("type error: `%s` is reserved; use std::testing", name)
	}
	arg, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	typ, err := c.checkBuiltinTestFailEqual(arg, args, env, unsafe)
	return typ, true, err
}

// checkBuiltinConstructorTypeApply validates typed std constructor primitives.
func (c *Checker) checkBuiltinConstructorTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	switch name {
	case "std.builtin.channel":
		if !c.currentStd {
			return "", true, errorf("type error: `%s` is reserved; use std::channel", name)
		}
		arg, err := c.parseType(typeArg)
		if err != nil {
			return "", true, err
		}
		typ, err := checkNoArgConstructor(name, args, Type(fmt.Sprintf("Channel<%s>", arg)))
		return typ, true, err
	case "std.builtin.atomic":
		if !c.currentStd {
			return "", true, errorf("type error: `%s` is reserved; use std::atomic", name)
		}
		arg, err := c.parseType(typeArg)
		if err != nil {
			return "", true, err
		}
		typ, _, err := c.checkAtomic(arg, args, env, unsafe)
		return typ, true, err
	case "std.builtin.mutex":
		if !c.currentStd {
			return "", true, errorf("type error: `%s` is reserved; use std::sync", name)
		}
		arg, err := c.parseType(typeArg)
		if err != nil {
			return "", true, err
		}
		typ, _, err := c.checkMutex(arg, args, env, unsafe)
		return typ, true, err
	case "std.builtin.array":
		if !c.currentStd {
			return "", true, errorf("type error: `%s` is reserved; use std::array", name)
		}
		arg, err := c.parseType(typeArg)
		if err != nil {
			return "", true, err
		}
		typ, _, err := c.checkArrayConstructor(arg, args, env, unsafe)
		return typ, true, err
	default:
		return "", false, nil
	}
}

// checkBuiltinTestFailEqual validates the std::testing typed failure primitive.
func (c *Checker) checkBuiltinTestFailEqual(
	typ Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if len(args) != 2 {
		return "", errorf("type error: `std::testing::expect_equal<%s>` expects 2 args", typ)
	}
	for idx, arg := range args {
		got, err := c.checkContextualExpr(arg, typ, env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, typ) {
			return "", errorf(
				"type error: arg %d of `std::testing::expect_equal<%s>` expects %s, got %s",
				idx+1,
				typ,
				typ,
				got,
			)
		}
	}
	return typeVoid, nil
}

// checkBuiltinMethodTypeApply validates std-only method primitive calls.
func (c *Checker) checkBuiltinMethodTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	switch name {
	case "std.builtin.box", "std.builtin.box_borrow", "std.builtin.box_borrow_mut",
		"std.builtin.box_deinit":
		return c.checkBuiltinBoxTypeApply(name, typeArg, args, env, unsafe)
	case "std.builtin.channel_send", "std.builtin.channel_recv":
		return c.checkBuiltinChannelMethod(name, typeArg, args, env, unsafe)
	case "std.builtin.atomic_load", "std.builtin.atomic_store":
		return c.checkBuiltinAtomicMethod(name, typeArg, args, env, unsafe)
	case "std.builtin.mutex_get":
		return c.checkBuiltinMutexMethod(name, typeArg, args, env, unsafe)
	default:
		return c.checkBuiltinArrayMethodTypeApply(name, typeArg, args, env, unsafe)
	}
}

// checkBuiltinBoxTypeApply validates std-only Box runtime primitives.
func (c *Checker) checkBuiltinBoxTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	switch name {
	case "std.builtin.box":
		return c.checkBoxConstructor(elem, args, env, unsafe)
	case "std.builtin.box_borrow":
		return c.checkBuiltinBoxMethod(name, elem, "borrow", args, env, unsafe)
	case "std.builtin.box_borrow_mut":
		return c.checkBuiltinBoxMethod(name, elem, "borrow_mut", args, env, unsafe)
	default:
		return c.checkBuiltinBoxMethod(name, elem, "deinit", args, env, unsafe)
	}
}

// checkBoxConstructor validates std::mem::Box<T>(allocator, value).
func (c *Checker) checkBoxConstructor(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if !c.currentStd {
		return "", true, errorf("type error: `std.builtin.box` is reserved; use std::mem::Box")
	}
	if len(args) != 2 {
		return "", true, errorf("type error: `std::mem::Box<%s>` expects allocator and value",
			elem)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Allocator" {
		return "", true, errorf("type error: `std::mem::Box<%s>` expects Allocator, got %s",
			elem, got)
	}
	got, err = c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	got, err = c.coerceContextualIntegerLiteral(args[1], elem, got)
	if err != nil {
		return "", true, err
	}
	if !sameType(got, elem) {
		return "", true, errorf("type error: `std::mem::Box<%s>` expects %s value, got %s",
			elem, elem, got)
	}
	return Type(fmt.Sprintf("!std::mem::Box<%s>", elem)), true, nil
}

// checkBuiltinBoxMethod validates Box primitives that back source wrappers.
func (c *Checker) checkBuiltinBoxMethod(
	name string,
	elem Type,
	method string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	receiver := Type(fmt.Sprintf("std::mem::Box<%s>", elem))
	return c.checkBuiltinReceiverMethod(name, receiver, func(rest []ast.Expression) (Type, error) {
		if len(rest) != 0 {
			return "", errorf("type error: `Box.%s` expects 0 args, got %d",
				method, len(rest))
		}
		switch method {
		case "borrow":
			return Type("&" + string(elem)), nil
		case "borrow_mut":
			return Type("&var " + string(elem)), nil
		default:
			return typeVoid, nil
		}
	}, args, env, unsafe)
}

// checkBuiltinArrayMethodTypeApply validates std-only Array method primitives.
func (c *Checker) checkBuiltinArrayMethodTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	switch name {
	case "std.builtin.array_append", "std.builtin.array_len", "std.builtin.array_capacity",
		"std.builtin.array_pop", "std.builtin.array_pop_or_panic",
		"std.builtin.array_get", "std.builtin.array_get_or_panic",
		"std.builtin.array_at", "std.builtin.array_at_mut",
		"std.builtin.array_set", "std.builtin.array_deinit":
		return c.checkBuiltinArrayMethod(name, typeArg, args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkBuiltinChannelMethod validates std-only channel method primitives.
func (c *Checker) checkBuiltinChannelMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	return c.checkBuiltinReceiverMethod(name, Type(fmt.Sprintf("Channel<%s>", elem)),
		func(rest []ast.Expression) (Type, error) {
			method := strings.TrimPrefix(name, "std.builtin.channel_")
			return c.checkChannelMethod(elem, method, rest, env, unsafe)
		}, args, env, unsafe)
}

// checkBuiltinAtomicMethod validates std-only atomic method primitives.
func (c *Checker) checkBuiltinAtomicMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	method := strings.TrimPrefix(name, "std.builtin.atomic_")
	return c.checkBuiltinReceiverMethod(name, Type(fmt.Sprintf("Atomic<%s>", elem)),
		func(rest []ast.Expression) (Type, error) {
			return c.checkAtomicMethod(elem, method, rest, env, unsafe)
		}, args, env, unsafe)
}

// checkBuiltinMutexMethod validates std-only mutex method primitives.
func (c *Checker) checkBuiltinMutexMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	return c.checkBuiltinReceiverMethod(name, Type(fmt.Sprintf("Mutex<%s>", elem)),
		func(rest []ast.Expression) (Type, error) {
			return c.checkMutexMethod(elem, "get", rest)
		}, args, env, unsafe)
}

// checkBuiltinArrayMethod validates std-only Array method primitives.
func (c *Checker) checkBuiltinArrayMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	method := strings.TrimPrefix(name, "std.builtin.array_")
	return c.checkBuiltinReceiverMethod(name, Type(fmt.Sprintf("std::array::Array<%s>", elem)),
		func(rest []ast.Expression) (Type, error) {
			return c.checkArrayPrimitiveMethod(elem, method, rest, env, unsafe)
		}, args, env, unsafe)
}

// checkBuiltinReceiverMethod validates a trusted primitive receiver argument.
func (c *Checker) checkBuiltinReceiverMethod(
	name string,
	receiver Type,
	checkRest func([]ast.Expression) (Type, error),
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if !c.currentStd {
		return "", true, errorf("type error: `%s` is reserved", name)
	}
	if len(args) == 0 {
		return "", true, errorf("type error: `%s` expects receiver", name)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != receiver {
		return "", true, errorf("type error: `%s` expects %s receiver, got %s",
			name, receiver, got)
	}
	typ, err := checkRest(args[1:])
	return typ, true, err
}

// checkArrayPrimitiveMethod validates Array primitives that back source wrappers.
func (c *Checker) checkArrayPrimitiveMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	switch name {
	case "pop":
		if len(args) != 0 {
			return "", errorf("type error: `Array.pop` expects 0 args, got %d", len(args))
		}
		return Type("!" + string(elem)), nil
	case "pop_or_panic":
		if len(args) != 0 {
			return "", errorf("type error: `Array.pop_or_panic` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "at":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("!&" + string(elem)), nil
	case "at_mut":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("!&var " + string(elem)), nil
	case "get", "get_or_panic":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		if !isGenericParamType(elem) && !c.isCopyType(elem) {
			return "", errorf("type error: `Array.%s` requires copy element in v0.2", name)
		}
		if name == "get" {
			return Type("!" + string(elem)), nil
		}
		return elem, nil
	default:
		return c.checkArrayMethod(elem, name, args, env, unsafe)
	}
}

// isGenericParamType reports whether a type is a std generic wrapper parameter.
func isGenericParamType(typ Type) bool {
	name := string(typ)
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// checkBuiltinMapTypeApply validates the std-only Map runtime primitive.
func (c *Checker) checkBuiltinMapTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if strings.HasPrefix(name, "std.builtin.map_") {
		return c.checkBuiltinMapMethod(name, typeArg, args, env, unsafe)
	}
	if name != "std.builtin.map" {
		return "", false, nil
	}
	if !c.currentStd {
		return "", true, errorf("type error: `%s` is reserved; use std::map", name)
	}
	mapArgs, err := c.checkedMapArgsAllowTypeParams(typeArg)
	if err != nil {
		return "", true, err
	}
	typ, _, err := c.checkMapConstructorForArgs(
		Type(mapArgs[0]), Type(mapArgs[1]), args, env, unsafe,
	)
	return typ, true, err
}

// checkBuiltinMapMethod validates std-only Map method primitives.
func (c *Checker) checkBuiltinMapMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	mapArgs, err := c.checkedMapArgsAllowTypeParams(typeArg)
	if err != nil {
		return "", true, err
	}
	receiver := Type(fmt.Sprintf("std::map::Map<%s, %s>", mapArgs[0], mapArgs[1]))
	method := strings.TrimPrefix(name, "std.builtin.map_")
	return c.checkBuiltinReceiverMethod(name, receiver,
		func(rest []ast.Expression) (Type, error) {
			return c.checkMapPrimitiveMethod(mapArgs[0], Type(mapArgs[1]), method,
				rest, env, unsafe)
		}, args, env, unsafe)
}

// checkMapPrimitiveMethod validates Map primitives that back source wrappers.
func (c *Checker) checkMapPrimitiveMethod(
	keyType string,
	valueType Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if !isGenericParamType(Type(keyType)) && !isGenericParamType(valueType) {
		return c.checkMapMethod(valueType, name, args, env, unsafe)
	}
	switch name {
	case "insert":
		if len(args) != 2 {
			return "", errorf("type error: `Map.insert` expects 2 args, got %d", len(args))
		}
		if got, err := c.checkExpr(args[0], env, unsafe); err != nil {
			return "", err
		} else if !sameType(got, Type(keyType)) {
			return "", errorf("type error: `Map.insert` expects %s key, got %s", keyType, got)
		}
		got, err := c.checkContextualExpr(args[1], valueType, env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, valueType) {
			return "", errorf("type error: `Map.insert` expects %s value, got %s",
				valueType, got)
		}
		return "!void", nil
	case "get":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("!" + string(valueType)), nil
	case "contains":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env, unsafe); err != nil {
			return "", err
		}
		return typeBool, nil
	default:
		return c.checkMapMethod(valueType, name, args, env, unsafe)
	}
}

// checkMapPrimitiveKeyArg validates a generic Map wrapper key argument.
func (c *Checker) checkMapPrimitiveKeyArg(
	name string,
	keyType string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(args) != 1 {
		return errorf("type error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkContextualExpr(args[0], Type(keyType), env, unsafe)
	if err != nil {
		return err
	}
	if !sameType(got, Type(keyType)) {
		return errorf("type error: `Map.%s` expects %s key, got %s", name, keyType, got)
	}
	return nil
}

// checkBuiltinThreadScopedTypeApply validates the std-only scoped thread primitive.
func (c *Checker) checkBuiltinThreadScopedTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if name != "std.builtin.thread_scoped" {
		return "", false, nil
	}
	if !c.currentStd {
		return "", true, errorf("type error: `%s` is reserved; use std::thread", name)
	}
	staticArgs, ok := splitGenericArgs(typeArg)
	if !ok || len(staticArgs) != 2 {
		return "", true, errorf(
			"type error: `std::thread::scoped` expects a type and a function name")
	}
	arg, err := c.parseType(strings.TrimSpace(staticArgs[0]))
	if err != nil {
		return "", true, err
	}
	if err := c.checkWorkerName(strings.TrimSpace(staticArgs[1]), env); err != nil {
		return "", true, err
	}
	typ, _, err := c.checkThreadScopedTyped(arg, args, env, unsafe)
	return typ, true, err
}

// forwardsWorker reports whether this name is the wrapper's own static value
// rather than a function to check here. The caller already checked the real
// worker, and a wrapper parameter may share a name with a top-level function.
func (c *Checker) forwardsWorker(target string, env *scope) bool {
	typ, ok := env.lookup(target)
	return ok && typ == typeFunction
}

// checkWorkerName validates a `Function` static argument: it names a top-level
// function, or forwards one this std wrapper received as its own static value.
func (c *Checker) checkWorkerName(target string, env *scope) error {
	if typ, ok := env.lookup(target); ok && typ == typeFunction {
		return nil
	}
	if c.functions[target] == nil {
		return errorf("type error: undefined function `%s`", target)
	}
	return nil
}

// checkGenericUserTypeApply validates source-defined std generic wrappers.
func (c *Checker) checkGenericUserTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	fn := c.functions[name]
	if fn == nil || len(fn.decl.StaticParams) == 0 {
		return "", false, nil
	}
	if err := c.rejectPrivateStdFunction(name, fn); err != nil {
		return "", true, err
	}
	argsText, ok := splitGenericArgs(typeArg)
	if !ok || len(argsText) != len(fn.decl.StaticParams) {
		return "", true, errorf("type error: `%s` expects %d static arguments",
			name, len(fn.decl.StaticParams))
	}
	typeArgsText, err := c.checkStaticArgs(name, fn, argsText)
	if err != nil {
		return "", true, err
	}
	typeArgs, err := c.parseGenericWrapperTypeArgs(typeArgsText)
	if err != nil {
		return "", true, err
	}
	if err := c.checkGenericWrapperTypeArgs(name, typeArgs); err != nil {
		return "", true, err
	}
	if len(args) != len(fn.params) {
		return "", true, userCallArityError(name, fn, len(args))
	}
	subst := map[string]Type{}
	for idx, param := range fn.typeParams {
		subst[param] = typeArgs[idx]
	}
	for idx, expr := range args {
		if err := c.checkGenericUserArg(name, fn, subst, idx, expr, env, unsafe); err != nil {
			return "", true, err
		}
	}
	if err := c.checkGenericInstantiation(fn, subst); err != nil {
		return "", true, err
	}
	return substituteTypeParams(fn.returnType, subst), true, nil
}

// checkStaticArgs validates each `<...>` argument against what its parameter
// declared, and returns the subset that are types, in declaration order.
func (c *Checker) checkStaticArgs(
	name string,
	fn *functionType,
	argsText []string,
) ([]string, error) {
	typeArgs := []string{}
	for idx, param := range fn.decl.StaticParams {
		if param.IsType() {
			typeArgs = append(typeArgs, strings.TrimSpace(argsText[idx]))
		}
	}
	// Values are checked after the types, because a worker contract can depend
	// on what the type parameters were bound to.
	for idx, param := range fn.decl.StaticParams {
		if param.IsType() {
			continue
		}
		arg := strings.TrimSpace(argsText[idx])
		if err := c.checkStaticValueArg(name, param, arg); err != nil {
			return nil, err
		}
		if Type(param.Type) == typeFunction {
			if err := c.checkStdWorkerContract(name, param.Name, arg, typeArgs); err != nil {
				return nil, err
			}
		}
	}
	return typeArgs, nil
}

// checkStdWorkerContract preserves the worker signatures the std concurrency
// wrappers require. A wrapper forwarding its own static value has nothing to
// check here -- the caller already checked the real worker.
func (c *Checker) checkStdWorkerContract(
	name string,
	paramName string,
	target string,
	typeArgs []string,
) error {
	targetFn := c.functions[target]
	if targetFn == nil {
		if c.currentStd {
			// A wrapper forwarding its own static value; the caller checked it.
			return nil
		}
		return errorf("type error: undefined function `%s`", target)
	}
	switch {
	case name == "std.task.parallel_for" && paramName == "worker":
		if len(targetFn.params) != 1 || targetFn.params[0] != typeI64 {
			return errorf("type error: parallel worker `%s` must accept i64", target)
		}
		_, err := c.parallelReturnType(targetFn)
		return err
	case name == "std.task.parallel_map" && paramName == "worker":
		return c.checkParallelMapWorker(target, targetFn)
	case name == "std.thread.scoped" && paramName == "worker":
		if len(typeArgs) != 1 {
			return errorf("type error: `std::thread::scoped` expects one type argument")
		}
		typ, err := c.parseType(typeArgs[0])
		if err != nil {
			return err
		}
		return c.checkThreadScopedWorker(typ, target, targetFn)
	}
	return nil
}

// checkStaticValueArg validates one compile-time value argument. The value is a
// literal or, for a `Function` parameter, a top-level function name. A generic
// may also pass on a static parameter of its own, which is how one wrapper
// forwards to another; the caller of the outer generic checked the real value.
func (c *Checker) checkStaticValueArg(name string, param ast.StaticParam, arg string) error {
	if c.staticParams[arg] == Type(param.Type) && param.Type != "" {
		return nil
	}
	switch Type(param.Type) {
	case typeFunction:
		if !isIdentifierText(arg) {
			return errorf("type error: `%s` static argument `%s` expects a function name, got `%s`",
				name, param.Name, arg)
		}
		return nil
	case typeBool:
		if arg != "true" && arg != "false" {
			return errorf("type error: `%s` static argument `%s` expects bool, got `%s`",
				name, param.Name, arg)
		}
		return nil
	default:
		if _, err := strconv.ParseInt(arg, 10, 64); err != nil {
			return errorf("type error: `%s` static argument `%s` expects %s, got `%s`",
				name, param.Name, param.Type, arg)
		}
		return nil
	}
}

// isIdentifierText reports whether text is a bare identifier.
func isIdentifierText(text string) bool {
	if text == "" {
		return false
	}
	for i, r := range text {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// checkGenericInstantiation checks a generic function body for one static type set.
func (c *Checker) checkGenericInstantiation(fn *functionType, subst map[string]Type) error {
	env := newScope(nil)
	staticParams, err := defineStaticValueParams(env, fn.decl)
	if err != nil {
		return err
	}
	for idx, param := range fn.decl.Params {
		typ := substituteTypeParams(fn.params[idx], subst)
		if err := env.defineParam(param.Name, typ, param.Borrow, param.MutBorrow); err != nil {
			return err
		}
	}
	returnType := substituteTypeParams(fn.returnType, subst)
	previousReturn := c.currentReturn
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeParams := c.typeParams
	previousTypeArgValues := c.typeArgValues
	previousStaticParams := c.staticParams
	previousLoops := c.loopLabels
	c.currentReturn = returnType
	c.currentFunction = fn
	c.currentStd = fn.decl.Std
	c.typeParams = typeParamSet(fn.typeParams)
	c.typeArgValues = subst
	c.staticParams = staticParams
	c.loopLabels = nil
	defer func() {
		c.currentReturn = previousReturn
		c.currentFunction = previousFunction
		c.currentStd = previousStd
		c.typeParams = previousTypeParams
		c.typeArgValues = previousTypeArgValues
		c.staticParams = previousStaticParams
		c.loopLabels = previousLoops
	}()
	returns, err := c.checkBlock(fn.decl.Body, env, returnType, nil)
	if err != nil {
		return err
	}
	if returnType != typeVoid && !returns {
		return errorf("type error: function `%s` must return %s", fn.name, returnType)
	}
	return nil
}

// parseGenericWrapperTypeArgs validates v0.2 static type arguments for wrappers.
func (c *Checker) parseGenericWrapperTypeArgs(args []string) ([]Type, error) {
	out := make([]Type, 0, len(args))
	for _, arg := range args {
		typ, err := c.parseType(arg)
		if err != nil {
			return nil, err
		}
		out = append(out, typ)
	}
	return out, nil
}

// checkGenericWrapperTypeArgs validates std wrapper-specific static type contracts.
func (c *Checker) checkGenericWrapperTypeArgs(name string, args []Type) error {
	switch name {
	case "std.channel.Channel":
		return nil
	case "std.array.Array":
		return c.rejectArrayElementType(args[0])
	case "std.atomic.Atomic":
		if !isAtomicSupportedType(args[0]) {
			return errorf("type error: unsupported atomic type `%s` in v0.1", args[0])
		}
	case "std.sync.Mutex":
		if !c.isCopyType(args[0]) {
			return errorf(
				"type error: `std::sync::Mutex<%s>` requires copy value in v0.1", args[0])
		}
	case "std.map.Map":
		return c.checkMapTypeArgContract(args)
	}
	return nil
}

// checkGenericUserArg validates an instantiated generic wrapper argument.
func (c *Checker) checkGenericUserArg(
	name string,
	fn *functionType,
	subst map[string]Type,
	idx int,
	arg ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	want := substituteTypeParams(fn.params[idx], subst)
	checkedArg, err := prepareBorrowArgument(arg, fn.borrowParams[idx], fn.mutBorrowParams[idx], env)
	if err != nil {
		return err
	}
	if fn.mutBorrowParams[idx] {
		if err := requireMutableBorrowArg(checkedArg, env); err != nil {
			return err
		}
	}
	if name == "std.sync.Mutex" {
		if err := c.rejectThreadBoundaryArg(checkedArg, env, unsafe); err != nil {
			return err
		}
	}
	// The worker moved to the static list, so the value crossing the boundary
	// is now the second runtime argument.
	if name == "std.thread.scoped" && idx == 1 {
		if err := c.rejectThreadBoundaryArg(checkedArg, env, unsafe); err != nil {
			return err
		}
	}
	got, err := c.checkContextualExpr(checkedArg, want, env, unsafe)
	if err != nil {
		return err
	}
	if !sameType(got, want) {
		if name == "std.sync.Mutex" {
			return errorf("type error: `std::sync::Mutex<%s>` expects %s, got %s",
				want, want, got)
		}
		return userCallArgError(name, fn, idx, want, got)
	}
	return nil
}

// checkErrorCall validates error-union error construction.
func (c *Checker) checkErrorCall(expr *ast.CallExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if len(expr.Args) != 1 {
		return "", errorf("type error: `error` expects 1 arg, got %d", len(expr.Args))
	}
	got, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(got, typeByteString) {
		return "", errorf("type error: `error` expects []u8, got %s", got)
	}
	errorType, _, ok := errorUnionParts(c.currentReturn)
	if !ok {
		return "", errorf("type error: `error` requires function to return !T")
	}
	if errorType != "" {
		return "", errorf("type error: `error` cannot construct typed error %s", errorType)
	}
	return c.currentReturn, nil
}

// checkUserCall validates a declared function call.
func (c *Checker) checkUserCall(
	name string,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	fn, ok := c.functions[name]
	if !ok {
		return "", errorf("type error: undefined function `%s`", name)
	}
	operation := fmt.Sprintf("call to `%s`", name)
	if fn.externABI != "" {
		if err := requireUnsafeCapabilityAt(unsafe, unsafeExternCall, operation, span); err != nil {
			return "", err
		}
	}
	if fn.requiresUnsafe {
		if err := requireUnsafeCapabilityAt(unsafe, unsafeUnsafeCall, operation, span); err != nil {
			return "", err
		}
	}
	if len(fn.typeParams) > 0 {
		return "", errorf("type error: `%s` requires explicit static arguments", name)
	}
	if len(args) != len(fn.params) {
		return "", userCallArityError(name, fn, len(args))
	}
	for idx, arg := range args {
		if err := c.checkUserCallArg(name, fn, idx, arg, env, unsafe); err != nil {
			return "", err
		}
	}
	return fn.returnType, nil
}

// checkUserCallArg validates one declared function argument.
func (c *Checker) checkUserCallArg(
	name string,
	fn *functionType,
	idx int,
	arg ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	checkedArg, err := prepareBorrowArgument(arg, fn.borrowParams[idx], fn.mutBorrowParams[idx], env)
	if err != nil {
		return err
	}
	if fn.mutBorrowParams[idx] {
		if err := requireMutableBorrowArg(checkedArg, env); err != nil {
			return err
		}
	}
	got, err := c.checkExpr(checkedArg, env, unsafe)
	if err != nil {
		return err
	}
	got, err = c.coerceContextualIntegerLiteral(checkedArg, fn.params[idx], got)
	if err != nil {
		return err
	}
	if contractName, ok := dynContract(fn.params[idx]); ok {
		if !c.satisfies(contractName, got) {
			return errorf("type error: %s does not satisfy `%s`", got, contractName)
		}
		return nil
	}
	if !sameType(got, fn.params[idx]) {
		return userCallArgError(name, fn, idx, fn.params[idx], got)
	}
	return nil
}

// userCallArityError reports declared function arity using source signatures when useful.
func userCallArityError(name string, fn *functionType, got int) error {
	if name == "std.string.String" {
		return errorf("type error: `std::string::String` expects allocator")
	}
	if len(fn.params) == 1 && fn.decl != nil {
		paramName := fn.decl.Params[0].Name
		if paramName != "" {
			return errorf("type error: `%s` expects %s",
				diagnosticName(name), paramName)
		}
	}
	return errorf("type error: `%s` expects %d args, got %d", name, len(fn.params), got)
}

// userCallArgError reports source-call argument type mismatches. want is the
// type this call expects, which for a generic is the parameter with its static
// arguments filled in: a caller writing `id<i64>` is told i64, not T.
func userCallArgError(name string, fn *functionType, idx int, want Type, got Type) error {
	if strings.HasPrefix(name, "std.") && fn.decl != nil && idx < len(fn.decl.Params) {
		paramName := fn.decl.Params[idx].Name
		if paramName != "" {
			if strings.HasPrefix(name, "std.fs.") {
				return errorf("type error: `%s` expects %s %s, got %s",
					diagnosticName(name), want, paramName, got)
			}
			return errorf("type error: `%s` %s expects %s, got %s",
				diagnosticName(name), paramName, want, got)
		}
	}
	return errorf("type error: arg %d of `%s` expects %s, got %s",
		idx+1, name, want, got)
}

// diagnosticName formats internal qualified names as user-facing paths.
func diagnosticName(name string) string {
	if strings.HasPrefix(name, "std.") {
		return strings.ReplaceAll(name, ".", "::")
	}
	return name
}

// checkUnionConstructorCall validates Union.Variant(payload) construction.
func (c *Checker) checkUnionConstructorCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if !field.Namespace {
		if enumType, ok := enumReceiver(field.Receiver, c.enums); ok {
			return "", true, errorf("type error: enum tag `%s.%s` must use `::`",
				enumType.name, field.Name)
		}
		if unionType, ok := unionReceiver(field.Receiver, c.unions); ok {
			return "", true, errorf("type error: union variant `%s.%s` must use `::`",
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
		return "", true, errorf("type error: unknown union variant `%s::%s`",
			unionType.name, field.Name)
	}
	if payload == "" {
		return "", true, errorf("type error: union variant `%s::%s` expects 0 args",
			unionType.name, field.Name)
	}
	if len(args) != 1 {
		return "", true, errorf("type error: union variant `%s::%s` expects 1 arg, got %d",
			unionType.name, field.Name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	got, err = c.coerceContextualIntegerLiteral(args[0], Type(payload), got)
	if err != nil {
		return "", true, err
	}
	if !sameType(got, Type(payload)) &&
		!c.returnValueMatchesBorrowParam(args[0], env, Type(payload), got) {
		return "", true, errorf("type error: union variant `%s::%s` expects %s, got %s",
			unionType.name, field.Name, payload, got)
	}
	return Type(unionType.name), true, nil
}

// checkArenaNewExpr validates std::arena::Arena<T>(allocator) and returns the arena type.
func (c *Checker) checkArenaNewExpr(
	expr *ast.ArenaNewExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if _, err := c.parseType(expr.TypeName); err != nil {
		return "", err
	}
	if expr.Allocator == nil {
		return "", errorf(
			"type error: `std::arena::Arena<%s>` expects exactly one allocator argument",
			expr.TypeName)
	}
	got, err := c.checkExpr(expr.Allocator, env, unsafe)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("type error: `std::arena::Arena<%s>` expects Allocator, got %s",
			expr.TypeName, got)
	}
	return Type(fmt.Sprintf("std::arena::Arena<%s>", expr.TypeName)), nil
}

// checkStructLiteralExpr validates field names and initializer types.
//
// A literal names each declared field exactly once (ADR-0079). A repeated name is
// reported where it is written, before the declared fields are measured, because a
// literal that writes one field twice carries two initializers for one slot and no
// rule picks between them.
func (c *Checker) checkStructLiteralExpr(
	expr *ast.StructLiteralExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	decl := c.structs[expr.TypeName]
	if decl == nil {
		return "", errorf("type error: unknown struct `%s`", expr.TypeName)
	}
	values := map[string]Type{}
	exprs := map[string]ast.Expression{}
	for _, field := range expr.Fields {
		if _, written := values[field.Name]; written {
			return "", errorf("type error: duplicate field `%s.%s`", expr.TypeName, field.Name)
		}
		got, err := c.checkExpr(field.Value, env, unsafe)
		if err != nil {
			return "", err
		}
		values[field.Name] = got
		exprs[field.Name] = field.Value
	}
	for _, field := range decl.Fields {
		got, ok := values[field.Name]
		if !ok {
			return "", errorf("type error: missing field `%s.%s`", expr.TypeName, field.Name)
		}
		if err := c.checkPrivateFieldAccess(expr.TypeName, field); err != nil {
			return "", err
		}
		want := fieldDeclaredType(field)
		got, err := c.coerceContextualIntegerLiteral(exprs[field.Name], want, got)
		if err != nil {
			return "", err
		}
		if !sameType(got, want) &&
			!c.returnValueMatchesBorrowParam(exprs[field.Name], env, want, got) {
			return "", errorf("type error: field `%s.%s` expects %s, got %s",
				expr.TypeName, field.Name, want, got)
		}
		delete(values, field.Name)
	}
	for name := range values {
		return "", errorf("type error: unknown field `%s.%s`", expr.TypeName, name)
	}
	return Type(expr.TypeName), nil
}

// checkFieldExpr returns the declared type of a struct field access.
func (c *Checker) checkFieldExpr(expr *ast.FieldExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if expr.Namespace {
		return c.checkNamespaceExpr(expr)
	}
	if enumType, ok := enumReceiver(expr.Receiver, c.enums); ok {
		return "", errorf("type error: enum tag `%s.%s` must use `::`",
			enumType.name, expr.Name)
	}
	if unionType, ok := unionReceiver(expr.Receiver, c.unions); ok {
		return "", errorf("type error: union variant `%s.%s` must use `::`",
			unionType.name, expr.Name)
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if receiver == "std::fs::Metadata" {
		return checkFsMetadataField(expr.Name)
	}
	if receiver == "std::fs::DirEntry" {
		return checkFsDirEntryField(expr.Name)
	}
	decl := c.structs[string(receiver)]
	if decl == nil {
		return "", errorf("type error: `%s` has no fields", receiver)
	}
	for _, field := range decl.Fields {
		if field.Name == expr.Name {
			if err := c.checkPrivateFieldAccess(string(receiver), field); err != nil {
				return "", err
			}
			return Type(field.TypeName), nil
		}
	}
	return "", errorf("type error: unknown field `%s.%s`", receiver, expr.Name)
}

// checkPrivateFieldAccess enforces std and user module field visibility.
func (c *Checker) checkPrivateFieldAccess(typeName string, field ast.Field) error {
	if field.Public {
		return nil
	}
	if isStdType(Type(typeName)) {
		if c.currentStd {
			return nil
		}
		return errorf("type error: field `%s.%s` is private", typeName, field.Name)
	}
	if c.sameUserModule(typeName) {
		return nil
	}
	return errorf("type error: field `%s.%s` is private", typeName, field.Name)
}

// sameUserModule reports whether the current function belongs to typeName's module.
func (c *Checker) sameUserModule(typeName string) bool {
	typeModule, ok := userModulePath(typeName)
	if !ok {
		return true
	}
	if c.currentFunction == nil {
		return false
	}
	fnModule, ok := userModulePath(c.currentFunction.name)
	return ok && fnModule == typeModule
}

// userModulePath returns the module prefix for package-qualified user names.
func userModulePath(name string) (string, bool) {
	index := strings.LastIndex(name, "::")
	if index < 0 {
		return "", false
	}
	return name[:index], true
}

// checkFsMetadataField returns builtin metadata field types.
func checkFsMetadataField(name string) (Type, error) {
	switch name {
	case "size":
		return typeI64, nil
	case "is_dir":
		return typeBool, nil
	default:
		return "", errorf("type error: unknown field `std::fs::Metadata.%s`", name)
	}
}

// checkFsDirEntryField returns builtin directory entry field types.
func checkFsDirEntryField(name string) (Type, error) {
	switch name {
	case "name", "path":
		return typeByteString, nil
	case "is_dir":
		return typeBool, nil
	default:
		return "", errorf("type error: unknown field `std::fs::DirEntry.%s`", name)
	}
}

// checkNamespaceExpr returns the type of enum or payload-free union namespace lookup.
func (c *Checker) checkNamespaceExpr(expr *ast.FieldExpr) (Type, error) {
	if enumType, ok := enumReceiver(expr.Receiver, c.enums); ok {
		if !enumType.tags[expr.Name] {
			return "", errorf("type error: unknown enum tag `%s::%s`",
				enumType.name, expr.Name)
		}
		return Type(enumType.name), nil
	}
	if unionType, ok := unionReceiver(expr.Receiver, c.unions); ok {
		payload, exists := unionType.variants[expr.Name]
		if !exists {
			return "", errorf("type error: unknown union variant `%s::%s`",
				unionType.name, expr.Name)
		}
		if payload != "" {
			return "", errorf("type error: union variant `%s::%s` expects payload",
				unionType.name, expr.Name)
		}
		return Type(unionType.name), nil
	}
	return "", c.unknownNamespaceError(expr.Receiver.String(), expressionSpan(expr.Receiver))
}

// unknownNamespaceError suggests the two valid namespace sources in Kizu.
func (c *Checker) unknownNamespaceError(name string, span ast.Span) error {
	message := fmt.Sprintf("type error: unknown namespace `%s`", name)
	message += "\nhelp: use an enum/union name or import a module that defines this namespace"
	if known := c.knownNamespaceSummary(); known != "" {
		message += "\nnote: known namespaces: " + known
	}
	if !span.IsZero() {
		return errorAtCode(span, "type.unknown_namespace", "%s", message)
	}
	return errorf("%s", message)
}

// knownNamespaceSummary returns a short display list of enum and union namespaces.
func (c *Checker) knownNamespaceSummary() string {
	names := make([]string, 0, len(c.enums)+len(c.unions))
	for name := range c.enums {
		names = append(names, name)
	}
	for name := range c.unions {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = append(names[:5], "...")
	}
	return strings.Join(names, ", ")
}

// checkDerefExpr returns the value type behind a local borrow or raw pointer.
func (c *Checker) checkDerefExpr(expr *ast.DerefExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && env.isBorrowed(ident.Name) {
		typ, _ := env.lookup(ident.Name)
		return typ, nil
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if isPointerType(receiver) {
		if err := requireUnsafeCapabilityAt(
			unsafe,
			unsafePtrDeref,
			"raw pointer dereference",
			expr.OperatorSpan,
		); err != nil {
			return "", err
		}
		typ, err := rawPointerDerefType(receiver)
		if err != nil {
			return "", err
		}
		return typ, nil
	}
	return "", errorf(
		"type error: `%s` is not a borrow or raw pointer and cannot be dereferenced",
		receiver,
	)
}

// checkAssignableField validates mutation of a field on a mutable value.
func (c *Checker) checkAssignableField(
	expr *ast.FieldExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if env.isBorrowed(ident.Name) {
			if !env.isMutBorrowed(ident.Name) {
				return "", errorf(
					"type error: cannot assign field through shared borrow `%s`",
					ident.Name,
				)
			}
		} else if !env.isMutable(ident.Name) {
			return "", errorf(
				"type error: cannot assign field of immutable binding `%s`",
				ident.Name,
			)
		}
	}
	if _, ok := expr.Receiver.(*ast.DerefExpr); ok {
		if _, err := c.checkAssignableDeref(expr.Receiver.(*ast.DerefExpr), env, unsafe); err != nil {
			return "", err
		}
	}
	return c.checkFieldExpr(expr, env, unsafe)
}

// checkAssignableDeref validates mutation through an &var borrow or raw pointer.
func (c *Checker) checkAssignableDeref(
	expr *ast.DerefExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && env.isMutBorrowed(ident.Name) {
		return c.checkDerefExpr(expr, env, unsafe)
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if isPointerType(receiver) {
		if err := requireUnsafeCapabilityAt(
			unsafe,
			unsafePtrDeref,
			"raw pointer dereference",
			expr.OperatorSpan,
		); err != nil {
			return "", err
		}
		return assignableRawPointerDerefType(receiver)
	}
	return "", errorf("type error: `%s` is not a mutable borrow", expr.Receiver.String())
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
	unsafe unsafeCaps,
) (Type, error) {
	if err := c.checkMethodReceiverPath(field, env); err != nil {
		return "", err
	}
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

// checkMethodReceiverPath enforces the v0.2 direct field receiver boundary.
func (c *Checker) checkMethodReceiverPath(field *ast.FieldExpr, env *scope) error {
	receiver, ok := field.Receiver.(*ast.FieldExpr)
	if !ok {
		return nil
	}
	if _, ok := receiver.Receiver.(*ast.IdentExpr); !ok {
		return errorf("type error: field method receiver only supports one direct field")
	}
	if field.Name != "deinit" || c.allowsDirectFieldCleanup(receiver, env) {
		return nil
	}
	return errorf(
		"type error: field cleanup `%s.deinit` is only allowed inside owner deinit",
		receiver.String(),
	)
}

// allowsDirectFieldCleanup reports whether owner.field.deinit is in owner deinit.
func (c *Checker) allowsDirectFieldCleanup(field *ast.FieldExpr, env *scope) bool {
	fn := c.currentFunction
	if fn == nil || fn.decl == nil || fn.decl.Name != "deinit" || fn.returnType != typeVoid {
		return false
	}
	owner, ok := field.Receiver.(*ast.IdentExpr)
	if !ok || len(fn.params) == 0 || len(fn.decl.Params) == 0 {
		return false
	}
	ownerType, ok := env.lookup(owner.Name)
	if !ok || fn.decl.Params[0].Name != owner.Name {
		return false
	}
	return sameType(fn.params[0], ownerType)
}

// checkKnownReceiverMethod validates non-arena builtin receiver families.
func (c *Checker) checkKnownReceiverMethod(
	field *ast.FieldExpr,
	receiver Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if contractName, ok := dynContract(receiver); ok {
		typ, err := c.checkDynMethodCall(
			contractName,
			field.Name,
			expressionSpan(field),
			args,
			env,
			unsafe,
		)
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
	if ok && base == "std::mem::Box" {
		typ, err := c.checkBoxReceiverMethod(field, Type(arg), args, env, unsafe)
		return typ, true, err
	}
	if receiver == "std::string::String" {
		typ, err := c.checkStringReceiverMethod(field, args, env, unsafe)
		return typ, true, err
	}
	return "", false, nil
}

// checkBoxReceiverMethod validates receiver-sensitive Box<T> methods.
func (c *Checker) checkBoxReceiverMethod(
	field *ast.FieldExpr,
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	switch field.Name {
	case "borrow":
		return "", errorf(
			"type error: `Box.borrow` must be bound with `let name = box.borrow()`")
	case "borrow_mut":
		return "", errorf(
			"type error: `Box.borrow_mut` must be bound with `let name = box.borrow_mut()`")
	case "deinit":
		if _, ok := field.Receiver.(*ast.IdentExpr); !ok &&
			!c.directFieldCleanupReceiver(field.Receiver, env) {
			return "", errorf("type error: `Box.deinit` requires local Box receiver")
		}
		if len(args) != 0 {
			return "", errorf("type error: `Box.deinit` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		receiver := Type(fmt.Sprintf("std::mem::Box<%s>", elem))
		method := c.implMethod(string(receiver), field.Name)
		if method != nil {
			return c.checkMethodArgs(method, receiver, expressionSpan(field), args, env, unsafe)
		}
		return "", errorf("type error: Box has no method `%s`", field.Name)
	}
}

// checkArenaOrImplMethod validates arena methods or user-defined impl methods.
func (c *Checker) checkArenaOrImplMethod(
	field *ast.FieldExpr,
	receiver Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	base, arg, ok := splitGenericType(string(receiver))
	if !ok || base != "std::arena::Arena" {
		method := c.implMethod(string(receiver), field.Name)
		if method != nil {
			return c.checkMethodArgs(method, receiver, expressionSpan(field), args, env, unsafe)
		}
		return "", errorf("type error: `%s` has no method `%s`", receiver, field.Name)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arg, args, env, unsafe)
	case "get":
		return c.checkArenaGet(arg, args, env, unsafe)
	case "deinit":
		return c.checkArenaDeinit(field, args, env)
	default:
		return "", errorf("type error: unknown arena method `%s`", field.Name)
	}
}

// checkStringReceiverMethod validates receiver-sensitive String methods.
func (c *Checker) checkStringReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if field.Name == "deinit" && ok && env.isBorrowed(ident.Name) {
		return "", errorf("type error: `String.deinit` requires owned String receiver")
	}
	if isStringMutatingMethod(field.Name) && ok &&
		env.isBorrowed(ident.Name) && !env.isMutBorrowed(ident.Name) {
		return "", errorf("type error: `String.%s` requires mutable String receiver", field.Name)
	}
	return c.checkStringMethod(field.Name, args, env, unsafe)
}

// checkStringMethod validates owned String prototype methods.
func (c *Checker) checkStringMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
			return "", errorf("type error: `String.%s` expects 0 args, got %d", name, len(args))
		}
		return typeI64, nil
	case "as_bytes":
		return "", errorf(
			"type error: `String.as_bytes` must be bound with `let name = string.as_bytes()`")
	case "truncate":
		if err := c.checkStringReserveArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "clear", "deinit":
		if len(args) != 0 {
			return "", errorf("type error: `String.%s` expects 0 args, got %d", name, len(args))
		}
		return typeVoid, nil
	default:
		return "", errorf("type error: String has no method `%s`", name)
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

// checkStringBytesArg validates a []u8 String method argument.
func (c *Checker) checkStringBytesArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(args) != 1 {
		return errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if !sameType(got, typeByteString) {
		return errorf("type error: `String.%s` expects []u8, got %s", name, got)
	}
	return nil
}

// checkStringReserveArg validates a reserve additional byte count.
func (c *Checker) checkStringReserveArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(args) != 1 {
		return errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return errorf("type error: `String.%s` expects i64, got %s", name, got)
	}
	return nil
}

// checkStringByteArg validates a u8 String method argument.
func (c *Checker) checkStringByteArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(args) != 1 {
		return errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkContextualExpr(args[0], typeU8, env, unsafe)
	if err != nil {
		return err
	}
	if got != typeU8 {
		return errorf("type error: `String.%s` expects u8, got %s", name, got)
	}
	return nil
}

// checkArrayReceiverMethod validates receiver-sensitive Array<T> methods.
func (c *Checker) checkArrayReceiverMethod(
	field *ast.FieldExpr,
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if field.Name == "at_mut" {
		if ident, ok := field.Receiver.(*ast.IdentExpr); !ok || !env.isMutable(ident.Name) {
			return "", errorf("type error: `Array.at_mut` requires mutable array binding")
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
	unsafe unsafeCaps,
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
		return errorf("type error: `Map.deinit` requires owned Map receiver")
	}
	if field.Name == "insert" && env.isBorrowed(ident.Name) && !env.isMutBorrowed(ident.Name) {
		return errorf("type error: `Map.insert` requires mutable Map receiver")
	}
	return nil
}

// checkConcurrencyMethod validates std concurrency prototype instance methods.
func (c *Checker) checkConcurrencyMethod(
	receiver Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
	unsafe unsafeCaps,
) (Type, error) {
	if isStdArrayStorageMethod(name) {
		return c.checkStdArrayStorageMethod(elem, name, args, env, unsafe)
	}
	// Rules the declaration cannot state: `at`/`at_mut` hand out a borrow, and
	// `get` copies out of the array.
	switch name {
	case "at", "at_mut":
		return "", errorf("type error: `Array.%s` must be bound with `let name = try array.%s(...)`",
			name, name)
	case "get", "get_or_panic":
		if !c.isCopyType(elem) {
			return "", errorf("type error: `Array.%s` requires copy element in v0.2", name)
		}
	}
	return c.checkStdMethod("std::array::Array", []Type{elem}, "Array", name, args, env, unsafe)
}

// checkStdMethod checks a receiver call against the signature std declares for
// it in std/src/*.kizu, with the receiver's static arguments substituted in.
func (c *Checker) checkStdMethod(
	receiver string,
	typeArgs []Type,
	label string,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	method, ok := c.stdMethods[receiver][name]
	if !ok {
		return "", errorf("type error: %s has no method `%s`", label, name)
	}
	if len(args) != len(method.Params) {
		return "", errorf("type error: `%s.%s` expects %d args, got %d",
			label, name, len(method.Params), len(args))
	}
	subst := make([]string, 0, len(typeArgs))
	for _, arg := range typeArgs {
		subst = append(subst, string(arg))
	}
	for idx, arg := range args {
		want := Type(method.Substitute(method.Params[idx].TypeName, subst))
		got, err := c.checkContextualExpr(arg, want, env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, want) {
			return "", errorf("type error: `%s.%s` expects %s, got %s", label, name, want, got)
		}
	}
	return Type(method.Substitute(method.Return, subst)), nil
}

// checkStdArrayStorageMethod validates Array helpers reserved to std source.
func (c *Checker) checkStdArrayStorageMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if !c.currentStd {
		return "", errorf("type error: Array has no method `%s`", name)
	}
	switch name {
	case "truncate":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "clear":
		if len(args) != 0 {
			return "", errorf("type error: `Array.clear` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return checkArrayAsBytes(elem, args)
	}
}

// isStdArrayStorageMethod reports methods reserved for std-owned storage wrappers.
func isStdArrayStorageMethod(name string) bool {
	return name == "truncate" || name == "clear" || name == "as_bytes"
}

// checkArrayAsBytes validates Array<u8> to byte-slice view conversion.
func checkArrayAsBytes(elem Type, args []ast.Expression) (Type, error) {
	if elem != typeU8 {
		return "", errorf("type error: `Array.as_bytes` requires Array<u8>")
	}
	if len(args) != 0 {
		return "", errorf("type error: `Array.as_bytes` expects 0 args, got %d", len(args))
	}
	return typeByteString, nil
}

// isStdType reports whether a type belongs to the reserved std namespace.
func isStdType(typ Type) bool {
	return strings.HasPrefix(string(typ), "std::")
}

// checkArrayIndexArg validates one i64 index argument.
func (c *Checker) checkArrayIndexArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(args) != 1 {
		return errorf("type error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return errorf("type error: `Array.%s` expects i64 index, got %s", name, got)
	}
	return nil
}

// checkMapConstructorForArgs validates Map allocator construction for key and value types.
func (c *Checker) checkMapConstructorForArgs(
	keyType Type,
	valueType Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, errorf("type error: `std::map::Map` expects allocator")
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Allocator" {
		return "", true, errorf("type error: `std::map::Map` expects Allocator, got %s", got)
	}
	return Type(fmt.Sprintf("std::map::Map<%s, %s>", keyType, valueType)), true, nil
}

// checkMapMethod validates owned Map<[]u8, V> prototype methods.
func (c *Checker) checkMapMethod(
	valueType Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
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
			return "", errorf("type error: `Map.len` expects 0 args, got %d", len(args))
		}
		return typeI64, nil
	case "deinit":
		if len(args) != 0 {
			return "", errorf("type error: `Map.deinit` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", errorf("type error: Map has no method `%s`", name)
	}
}

// checkMapInsert validates copy-only Map.insert arguments.
func (c *Checker) checkMapInsert(
	valueType Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if len(args) != 2 {
		return "", errorf("type error: `Map.insert` expects 2 args, got %d", len(args))
	}
	if got, err := c.checkExpr(args[0], env, unsafe); err != nil {
		return "", err
	} else if !sameType(got, typeByteString) {
		return "", errorf("type error: `Map.insert` expects []u8 key, got %s", got)
	}
	got, err := c.checkContextualExpr(args[1], valueType, env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(got, valueType) {
		return "", errorf("type error: `Map.insert` expects %s value, got %s", valueType, got)
	}
	return "!void", nil
}

// checkMapKeyArg validates one []u8 lookup key.
func (c *Checker) checkMapKeyArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(args) != 1 {
		return errorf("type error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if !sameType(got, typeByteString) {
		return errorf("type error: `Map.%s` expects []u8 key, got %s", name, got)
	}
	return nil
}

// checkedMapArgs validates and returns the two Map static type arguments.
func (c *Checker) checkedMapArgs(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, errorf("type error: std::map::Map expects 2 static arguments")
	}
	if _, err := c.parseMapType(fmt.Sprintf("std::map::Map<%s>", arg), args); err != nil {
		return nil, err
	}
	return args, nil
}

// checkedMapArgsAllowTypeParams validates Map static arguments inside std wrappers.
func (c *Checker) checkedMapArgsAllowTypeParams(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, errorf("type error: std::map::Map expects 2 static arguments")
	}
	if _, err := c.parseMapType(fmt.Sprintf("std::map::Map<%s>", arg), args); err != nil {
		return nil, err
	}
	return args, nil
}

// checkMapTypeArgContract enforces v0.2 public Map constructor restrictions.
func (c *Checker) checkMapTypeArgContract(args []Type) error {
	if len(args) != 2 {
		return errorf("type error: std::map::Map expects 2 static arguments")
	}
	if !sameType(args[0], typeByteString) {
		return errorf("type error: std::map::Map key type must be []u8 in v0.2")
	}
	if !c.isCopyType(args[1]) {
		return errorf("type error: std::map::Map value type must be copy in v0.2")
	}
	return nil
}

// checkTaskGroupMethod validates structured task spawning.
func (c *Checker) checkTaskGroupMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if name != "spawn" {
		return "", errorf("type error: TaskGroup has no method `%s`", name)
	}
	if len(args) < 1 {
		return "", errorf("type error: `TaskGroup.spawn` expects function and args")
	}
	target, ok := args[0].(*ast.IdentExpr)
	if !ok {
		return "", errorf("type error: `TaskGroup.spawn` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		if _, ok := env.lookup(target.Name); ok {
			return "", errorf("type error: `TaskGroup.spawn` expects function name")
		}
		return "", errorf("type error: undefined function `%s`", target.Name)
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
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, errorf("type error: `std::task::Group` expects io")
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if got != "Io" {
		return "", true, errorf("type error: `std::task::Group` expects Io, got %s", got)
	}
	return "TaskGroup", true, nil
}

// checkSpawnArgs validates spawned function arguments after the implicit Io.
func (c *Checker) checkSpawnArgs(
	name string,
	fn *functionType,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	if len(fn.params) == 0 || fn.params[0] != "Io" ||
		fn.borrowParams[0] || fn.mutBorrowParams[0] {
		return errorf("type error: spawned function `%s` must accept owned Io as first parameter",
			name)
	}
	if len(args) != len(fn.params)-1 {
		return errorf("type error: `%s` expects %d args, got %d",
			name, len(fn.params)-1, len(args))
	}
	for idx, arg := range args {
		paramIdx := idx + 1
		if fn.borrowParams[paramIdx] || fn.mutBorrowParams[paramIdx] {
			return errorf("type error: task cannot capture borrow parameter `%s`", name)
		}
		if err := c.rejectThreadBoundaryArg(arg, env, unsafe); err != nil {
			return err
		}
		got, err := c.checkExpr(arg, env, unsafe)
		if err != nil {
			return err
		}
		if !sameType(got, fn.params[paramIdx]) {
			return errorf("type error: arg %d of `%s` expects %s, got %s",
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
	unsafe unsafeCaps,
) (Type, error) {
	switch name {
	case "enqueue":
		return c.checkQueueEnqueue(args, env, unsafe)
	case "drain":
		if len(args) != 0 {
			return "", errorf("type error: `queue.drain` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", errorf("type error: Queue has no method `%s`", name)
	}
}

// checkQueueEnqueue validates one deferred function call.
func (c *Checker) checkQueueEnqueue(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if len(args) < 2 {
		return "", errorf("type error: `queue.enqueue` expects io, function, and args")
	}
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if ioType != "Io" {
		return "", errorf("type error: `queue.enqueue` expects Io, got %s", ioType)
	}
	target, ok := args[1].(*ast.IdentExpr)
	if !ok {
		return "", errorf("type error: `queue.enqueue` expects function name")
	}
	fn := c.functions[target.Name]
	if fn == nil {
		return "", errorf("type error: undefined function `%s`", target.Name)
	}
	spawnArgs := append([]ast.Expression{args[0]}, args[2:]...)
	if len(spawnArgs) != len(fn.params) {
		return "", errorf("type error: `%s` expects %d args, got %d",
			target.Name, len(fn.params), len(spawnArgs))
	}
	for idx, arg := range spawnArgs {
		if fn.borrowParams[idx] || fn.mutBorrowParams[idx] {
			return "", errorf("type error: queue cannot capture borrow parameter `%s`", target.Name)
		}
		if err := c.rejectThreadBoundaryArg(arg, env, unsafe); err != nil {
			return "", err
		}
		got, err := c.checkContextualExpr(arg, fn.params[idx], env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, fn.params[idx]) {
			return "", errorf("type error: arg %d of `%s` expects %s, got %s",
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
	unsafe unsafeCaps,
) (Type, error) {
	switch name {
	case "send":
		if len(args) != 1 {
			return "", errorf("type error: `channel.send` expects 1 arg, got %d", len(args))
		}
		if err := c.rejectThreadBoundaryArg(args[0], env, unsafe); err != nil {
			return "", err
		}
		got, err := c.checkContextualExpr(args[0], elem, env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, elem) {
			return "", errorf("type error: `channel.send` expects %s, got %s", elem, got)
		}
		return typeVoid, nil
	case "recv":
		if len(args) != 0 {
			return "", errorf("type error: `channel.recv` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "close":
		if len(args) != 0 {
			return "", errorf("type error: `channel.close` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return "", errorf("type error: Channel has no method `%s`", name)
	}
}

// checkPartitionMethod validates access to one disjoint partition index.
func (c *Checker) checkPartitionMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if name != "at" {
		return "", errorf("type error: Partition has no method `%s`", name)
	}
	if len(args) != 1 {
		return "", errorf("type error: `partition.at` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != typeI64 {
		return "", errorf("type error: `partition.at` expects i64, got %s", got)
	}
	return typeI64, nil
}

// checkLocalBufferMethod validates worker-local scratch access.
func (c *Checker) checkLocalBufferMethod(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if name != "get" {
		return "", errorf("type error: LocalBuffer has no method `%s`", name)
	}
	if len(args) != 1 {
		return "", errorf("type error: `LocalBuffer.get` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != typeI64 {
		return "", errorf("type error: `LocalBuffer.get` expects i64, got %s", got)
	}
	return typeI64, nil
}

// checkAtomicMethod validates seq_cst-only atomic operations.
func (c *Checker) checkAtomicMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	switch name {
	case "load":
		if len(args) != 0 {
			return "", errorf("type error: `atomic.load` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "store":
		if len(args) != 1 {
			return "", errorf("type error: `atomic.store` expects 1 arg, got %d", len(args))
		}
		got, err := c.checkContextualExpr(args[0], elem, env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, elem) {
			return "", errorf("type error: `atomic.store` expects %s, got %s", elem, got)
		}
		return typeVoid, nil
	default:
		return "", errorf("type error: Atomic has no method `%s`", name)
	}
}

// checkMutexMethod validates the minimal synchronized wrapper API.
func (c *Checker) checkMutexMethod(elem Type, name string, args []ast.Expression) (Type, error) {
	if name != "get" {
		return "", errorf("type error: Mutex has no method `%s`", name)
	}
	if len(args) != 0 {
		return "", errorf("type error: `mutex.get` expects 0 args, got %d", len(args))
	}
	return elem, nil
}

// checkTaskMethod validates await/cancel on a task value.
func checkTaskMethod(name string, elem string, args []ast.Expression) (Type, error) {
	if len(args) != 0 {
		return "", errorf("type error: `task.%s` expects 0 args, got %d", name, len(args))
	}
	switch name {
	case "await":
		return Type(elem), nil
	case "cancel":
		return typeVoid, nil
	default:
		return "", errorf("type error: Task has no method `%s`", name)
	}
}

// checkPartitionMut validates a trusted disjoint partition constructor.
func (c *Checker) checkPartitionMut(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("type error: `std::task::partition_mut` expects 2 args")
	}
	init, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if init != typeI64 {
		return "", true, errorf("type error: partition init expects i64, got %s", init)
	}
	count, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if count != typeI64 {
		return "", true, errorf("type error: partition count expects i64, got %s", count)
	}
	return "Partition", true, nil
}

// checkLocalBuffer validates worker-local scratch construction.
func (c *Checker) checkLocalBuffer(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("type error: `std::task::LocalBuffer` expects 2 args")
	}
	count, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", true, err
	}
	if count != typeI64 {
		return "", true, errorf("type error: LocalBuffer count expects i64, got %s", count)
	}
	if _, err := c.checkExpr(args[1], env, unsafe); err != nil {
		return "", true, err
	}
	return "LocalBuffer", true, nil
}

// checkParallelFor validates the safe data-parallel prototype.
func (c *Checker) checkParallelFor(
	worker string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("type error: `std::task::parallel_for` expects 3 args")
	}
	if err := c.checkIoAndRange(args, env, unsafe, "std::task::parallel_for"); err != nil {
		return "", true, err
	}
	target := strings.TrimSpace(worker)
	if err := c.checkWorkerName(target, env); err != nil {
		return "", true, err
	}
	if c.forwardsWorker(target, env) {
		return "!void", true, nil
	}
	targetFn := c.functions[target]
	if len(targetFn.params) != 1 || targetFn.params[0] != typeI64 {
		return "", true, errorf("type error: parallel worker `%s` must accept i64", target)
	}
	typ, err := c.parallelReturnType(targetFn)
	return typ, true, err
}

// checkParallelMap validates disjoint partition output from a worker result.
func (c *Checker) checkParallelMap(
	worker string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 4 {
		return "", true, errorf("type error: `std::task::parallel_map` expects 4 args")
	}
	if err := c.checkIoAndPartitionRange(args, env, unsafe); err != nil {
		return "", true, err
	}
	target := strings.TrimSpace(worker)
	if err := c.checkWorkerName(target, env); err != nil {
		return "", true, err
	}
	if c.forwardsWorker(target, env) {
		return typeVoid, true, nil
	}
	targetFn := c.functions[target]
	if err := c.checkParallelMapWorker(target, targetFn); err != nil {
		return "", true, err
	}
	return typeVoid, true, nil
}

// checkParallelMapWorker validates the disjoint-output map worker signature.
func (c *Checker) checkParallelMapWorker(target string, targetFn *functionType) error {
	if len(targetFn.params) != 1 || targetFn.params[0] != typeI64 {
		return errorf("type error: parallel map worker `%s` must accept i64", target)
	}
	if targetFn.returnType != typeI64 {
		return errorf("type error: parallel map worker `%s` must return i64", target)
	}
	return nil
}

// checkThreadScopedTyped validates the std-only one-argument scoped thread primitive.
func (c *Checker) checkThreadScopedTyped(
	argType Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("type error: `std::thread::scoped` expects io and arg")
	}
	if err := c.checkIoArg(args[0], env, unsafe, "std::thread::scoped"); err != nil {
		return "", true, err
	}
	got, err := c.checkContextualExpr(args[1], argType, env, unsafe)
	if err != nil {
		return "", true, err
	}
	if !sameType(got, argType) {
		return "", true, errorf("type error: arg 1 of `std::thread::scoped` expects %s, got %s",
			argType, got)
	}
	return argType, true, nil
}

// checkAtomic validates the v0.1 seq_cst atomic constructor.
func (c *Checker) checkAtomic(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if !c.typeParams[string(elem)] && !isAtomicSupportedType(elem) {
		return "", true, errorf("type error: unsupported atomic type `%s` in v0.1", elem)
	}
	if len(args) != 1 {
		return "", true, errorf("type error: `std::atomic::Atomic<%s>` expects 1 arg", elem)
	}
	got, err := c.checkContextualExpr(args[0], elem, env, unsafe)
	if err != nil {
		return "", true, err
	}
	if !sameType(got, elem) {
		return "", true, errorf("type error: `std::atomic::Atomic<%s>` expects %s, got %s",
			elem, elem, got)
	}
	return Type(fmt.Sprintf("Atomic<%s>", elem)), true, nil
}

// checkMutex validates an explicit synchronized ownership wrapper.
func (c *Checker) checkMutex(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, bool, error) {
	if len(args) != 1 {
		return "", true, errorf("type error: `std::sync::Mutex<%s>` expects 1 arg", elem)
	}
	if !c.typeParams[string(elem)] {
		if err := c.rejectThreadBoundaryArg(args[0], env, unsafe); err != nil {
			return "", true, err
		}
	}
	got, err := c.checkContextualExpr(args[0], elem, env, unsafe)
	if err != nil {
		return "", true, err
	}
	if !sameType(got, elem) {
		return "", true, errorf("type error: `std::sync::Mutex<%s>` expects %s, got %s",
			elem, elem, got)
	}
	if !c.typeParams[string(elem)] && !c.isCopyType(elem) {
		return "", true, errorf(
			"type error: `std::sync::Mutex<%s>` requires copy value in v0.1", elem)
	}
	return Type(fmt.Sprintf("Mutex<%s>", elem)), true, nil
}

// checkDynMethodCall validates a method call through &dyn Contract.
func (c *Checker) checkDynMethodCall(
	contractName string,
	name string,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	contract := c.contracts[contractName]
	if contract == nil || contract.methods[name] == nil {
		return "", errorf("type error: `dyn %s` has no method `%s`", contractName, name)
	}
	return c.checkMethodArgs(contract.methods[name], typeSelf, span, args, env, unsafe)
}

// checkMethodArgs validates method-call arguments after the implicit self receiver.
func (c *Checker) checkMethodArgs(
	method *functionType,
	receiver Type,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if len(method.params) == 0 {
		return "", errorf("type error: method `%s` must have self parameter", method.name)
	}
	if len(args) != len(method.params)-1 {
		return "", errorf("type error: `%s` expects %d args, got %d",
			method.name, len(method.params)-1, len(args))
	}
	if method.params[0] != receiver && method.params[0] != typeSelf {
		return "", errorf("type error: method `%s` self expects %s, got %s",
			method.name, method.params[0], receiver)
	}
	if method.requiresUnsafe {
		if err := requireUnsafeCapabilityAt(
			unsafe,
			unsafeUnsafeCall,
			fmt.Sprintf("call to `%s`", method.name),
			span,
		); err != nil {
			return "", err
		}
	}
	for idx, arg := range args {
		want := method.params[idx+1]
		checkedArg, err := prepareBorrowArgument(
			arg,
			method.borrowParams[idx+1],
			method.mutBorrowParams[idx+1],
			env,
		)
		if err != nil {
			return "", err
		}
		if method.mutBorrowParams[idx+1] {
			if err := requireMutableBorrowArg(checkedArg, env); err != nil {
				return "", err
			}
		}
		got, err := c.checkContextualExpr(checkedArg, want, env, unsafe)
		if err != nil {
			return "", err
		}
		if !sameType(got, want) {
			return "", errorf("type error: arg %d of `%s` expects %s, got %s",
				idx+1, method.name, want, got)
		}
	}
	return method.returnType, nil
}

// requireMutableBorrowArg restricts &var arguments to mutable locals or reborrowed &var params.
func requireMutableBorrowArg(expr ast.Expression, env *scope) error {
	ident, ok := expr.(*ast.IdentExpr)
	if ok {
		if env.isMutable(ident.Name) || env.isMutBorrowed(ident.Name) {
			return nil
		}
		return errorf("type error: &var argument `%s` must be mutable", ident.Name)
	}
	field, fieldOK := expr.(*ast.FieldExpr)
	if !fieldOK {
		return errorf("type error: &var argument must be a mutable local binding")
	}
	ident, ok = field.Receiver.(*ast.IdentExpr)
	if !ok {
		return errorf("type error: &var argument must be a mutable local binding")
	}
	if env.isMutable(ident.Name) || env.isMutBorrowed(ident.Name) {
		return nil
	}
	return errorf("type error: &var argument `%s` must be mutable", ident.Name)
}

// prepareBorrowArgument unwraps explicit &/&var syntax only for borrowed parameters.
func prepareBorrowArgument(
	arg ast.Expression,
	wantBorrow bool,
	wantMutable bool,
	env *scope,
) (ast.Expression, error) {
	prefix, ok := borrowPrefix(arg)
	if !ok {
		return arg, nil
	}
	if !wantBorrow {
		return nil, errorf("type error: borrow argument cannot be passed to owning parameter")
	}
	if err := checkBorrowTargetShape(prefix.Right); err != nil {
		return nil, err
	}
	if prefix.Operator == "&var" && !wantMutable {
		return nil, errorf("type error: argument expects &T, got &var")
	}
	if prefix.Operator == "&" && wantMutable {
		return nil, errorf("type error: argument expects &var T, got &T")
	}
	if wantMutable {
		if err := requireMutableBorrowArg(prefix.Right, env); err != nil {
			return nil, err
		}
	}
	return prefix.Right, nil
}

// borrowPrefix reports whether an expression is &T or &var T syntax.
func borrowPrefix(expr ast.Expression) (*ast.PrefixExpr, bool) {
	prefix, ok := expr.(*ast.PrefixExpr)
	if !ok || (prefix.Operator != "&" && prefix.Operator != "&var") {
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
		return errorf("type error: v0.1 field borrow only supports one direct field")
	default:
		return errorf("type error: borrow target must be a local binding or direct field")
	}
}

// checkArenaAdd validates std::arena::Arena<T>.add(value).
func (c *Checker) checkArenaAdd(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if len(args) != 1 {
		return "", errorf("type error: `arena.add` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(got, Type(arg)) {
		return "", errorf("type error: `arena.add` expects %s, got %s", arg, got)
	}
	return Type(fmt.Sprintf("std::arena::Handle<%s>", arg)), nil
}

// checkArenaGet validates std::arena::Arena<T>.get(std::arena::Handle<T>).
func (c *Checker) checkArenaGet(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if len(args) != 1 {
		return "", errorf("type error: `arena.get` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	want := Type(fmt.Sprintf("std::arena::Handle<%s>", arg))
	if !sameType(got, want) {
		return "", errorf("type error: `arena.get` expects %s, got %s", want, got)
	}
	return Type(arg), nil
}

// checkArenaDeinit validates explicit arena cleanup syntax.
func (c *Checker) checkArenaDeinit(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (Type, error) {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok && !c.directFieldCleanupReceiver(field.Receiver, env) {
		return "", errorf("type error: `arena.deinit` requires local arena receiver")
	}
	if ok && env.isBorrowed(ident.Name) {
		return "", errorf("type error: `arena.deinit` requires owned arena receiver")
	}
	if len(args) != 0 {
		return "", errorf("type error: `arena.deinit` expects 0 args, got %d", len(args))
	}
	return typeVoid, nil
}

// directFieldCleanupReceiver reports whether expr is an allowed field cleanup target.
func (c *Checker) directFieldCleanupReceiver(expr ast.Expression, env *scope) bool {
	field, ok := expr.(*ast.FieldExpr)
	return ok && c.allowsDirectFieldCleanup(field, env)
}

// checkPtrRead validates unsafe raw pointer reads.
func (c *Checker) checkPtrRead(expr *ast.CallExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if err := requireUnsafeCapabilityAt(
		unsafe,
		unsafePtrRead,
		"`ptr_read`",
		expressionSpan(expr.Callee),
	); err != nil {
		return "", err
	}
	if len(expr.Args) != 1 {
		return "", errorf("type error: `ptr_read` expects 1 arg, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") {
		return "", errorf("type error: `ptr_read` expects non-null raw pointer, got %s", ptrType)
	}
	return Type(strings.TrimPrefix(elem, "const ")), nil
}

// checkPtrWrite validates unsafe raw pointer writes.
func (c *Checker) checkPtrWrite(expr *ast.CallExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if err := requireUnsafeCapabilityAt(
		unsafe,
		unsafePtrWrite,
		"`ptr_write`",
		expressionSpan(expr.Callee),
	); err != nil {
		return "", err
	}
	if len(expr.Args) != 2 {
		return "", errorf("type error: `ptr_write` expects 2 args, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") || strings.HasPrefix(elem, "const ") {
		return "", errorf("type error: `ptr_write` expects mutable non-null raw pointer")
	}
	valueType, err := c.checkContextualExpr(expr.Args[1], Type(elem), env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(valueType, Type(elem)) {
		return "", errorf("type error: `ptr_write` expects %s, got %s", elem, valueType)
	}
	return typeVoid, nil
}

// checkPtrFromInt validates unsafe integer-to-pointer conversion.
func (c *Checker) checkPtrFromInt(
	typeArg string,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if err := requireUnsafeCapabilityAt(unsafe, unsafePtrIntCast, "`ptr_from_int`", span); err != nil {
		return "", err
	}
	target, err := c.parseType(typeArg)
	if err != nil {
		return "", err
	}
	if !isPointerType(target) || strings.HasPrefix(string(target), "?") {
		return "", errorf("type error: `ptr_from_int` target must be non-null raw pointer")
	}
	if len(args) != 1 {
		return "", errorf("type error: `ptr_from_int` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if !integerTypes[got] {
		return "", errorf("type error: `ptr_from_int` expects integer, got %s", got)
	}
	return target, nil
}

// checkIntFromPtr validates unsafe pointer-to-integer conversion.
func (c *Checker) checkIntFromPtr(
	typeArg string,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if err := requireUnsafeCapabilityAt(unsafe, unsafePtrIntCast, "`int_from_ptr`", span); err != nil {
		return "", err
	}
	target, err := c.parseType(typeArg)
	if err != nil {
		return "", err
	}
	if !integerTypes[target] {
		return "", errorf("type error: `int_from_ptr` target must be integer")
	}
	if len(args) != 1 {
		return "", errorf("type error: `int_from_ptr` expects 1 arg, got %d", len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if !isPointerType(got) {
		return "", errorf("type error: `int_from_ptr` expects raw pointer, got %s", got)
	}
	return target, nil
}

// checkVolatileRead validates unsafe volatile raw pointer reads.
func (c *Checker) checkVolatileRead(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if err := requireUnsafeCapabilityAt(
		unsafe,
		unsafeVolatile,
		"`volatile_read`",
		expressionSpan(expr.Callee),
	); err != nil {
		return "", err
	}
	if len(expr.Args) != 1 {
		return "", errorf("type error: `volatile_read` expects 1 arg, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") {
		return "", errorf("type error: `volatile_read` expects non-null raw pointer, got %s", ptrType)
	}
	return Type(strings.TrimPrefix(elem, "const ")), nil
}

// checkVolatileWrite validates unsafe volatile raw pointer writes.
func (c *Checker) checkVolatileWrite(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeCaps,
) (Type, error) {
	if err := requireUnsafeCapabilityAt(
		unsafe,
		unsafeVolatile,
		"`volatile_write`",
		expressionSpan(expr.Callee),
	); err != nil {
		return "", err
	}
	if len(expr.Args) != 2 {
		return "", errorf("type error: `volatile_write` expects 2 args, got %d", len(expr.Args))
	}
	ptrType, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := pointerElement(ptrType)
	if !ok || strings.HasPrefix(string(ptrType), "?") || strings.HasPrefix(elem, "const ") {
		return "", errorf("type error: `volatile_write` expects mutable non-null raw pointer")
	}
	valueType, err := c.checkContextualExpr(expr.Args[1], Type(elem), env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(valueType, Type(elem)) {
		return "", errorf("type error: `volatile_write` expects %s, got %s", elem, valueType)
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

// rawPointerDerefType returns the value type read by raw pointer dereference.
func rawPointerDerefType(typ Type) (Type, error) {
	elem, ok := pointerElement(typ)
	if !ok {
		return "", errorf("type error: `%s` is not a raw pointer", typ)
	}
	if strings.HasPrefix(string(typ), "?") {
		return "", errorf("type error: nullable raw pointer `%s` cannot be dereferenced", typ)
	}
	return Type(strings.TrimPrefix(elem, "const ")), nil
}

// assignableRawPointerDerefType returns the value type written through a raw pointer.
func assignableRawPointerDerefType(typ Type) (Type, error) {
	elem, ok := pointerElement(typ)
	if !ok {
		return "", errorf("type error: `%s` is not a raw pointer", typ)
	}
	if strings.HasPrefix(string(typ), "?") {
		return "", errorf("type error: nullable raw pointer `%s` cannot be dereferenced", typ)
	}
	if strings.HasPrefix(elem, "const ") {
		return "", errorf("type error: cannot assign through const raw pointer `%s`", typ)
	}
	return Type(elem), nil
}

// isPointerType reports whether typ is ptr<T> or ?ptr<T>.
func isPointerType(typ Type) bool {
	_, ok := pointerElement(typ)
	return ok
}

// isCopyType reports whether values of typ can be duplicated in v0.1 safe code.
func (c *Checker) isCopyType(typ Type) bool {
	if isAstNodeIDType(typ) || isAstScalarType(typ) {
		return true
	}
	if isDiagnosticScalarType(typ) {
		return true
	}
	if typ == "ParseNode" || typ == "std::kizu::parser::ParseNode" {
		return true
	}
	if typ == typeByteString {
		return true
	}
	if c.enums[string(typ)] != nil {
		return true
	}
	return copyTypes[typ]
}

// isAstNodeIDType reports the std::kizu AST id wrapper allowed in child lists.
func isAstNodeIDType(typ Type) bool {
	return typ == "NodeId" || typ == "std::kizu::ast::NodeId"
}

// isAstScalarType reports small std::kizu AST metadata wrappers with copy fields.
func isAstScalarType(typ Type) bool {
	switch typ {
	case "SourceFile", "std::kizu::ast::SourceFile",
		"AstNode", "std::kizu::ast::AstNode",
		"AstData", "std::kizu::ast::AstData",
		"ProgramNode", "std::kizu::ast::ProgramNode",
		"IntNode", "std::kizu::ast::IntNode",
		"StringNode", "std::kizu::ast::StringNode",
		"TypeNameNode", "std::kizu::ast::TypeNameNode",
		"VarNode", "std::kizu::ast::VarNode",
		"BoolNode", "std::kizu::ast::BoolNode",
		"PrefixNode", "std::kizu::ast::PrefixNode",
		"BinaryNode", "std::kizu::ast::BinaryNode",
		"FieldExprNode", "std::kizu::ast::FieldExprNode",
		"DerefExprNode", "std::kizu::ast::DerefExprNode",
		"CallNode", "std::kizu::ast::CallNode",
		"TypeApplyExprNode", "std::kizu::ast::TypeApplyExprNode",
		"CastExprNode", "std::kizu::ast::CastExprNode",
		"IndexExprNode", "std::kizu::ast::IndexExprNode",
		"StructLiteralExprNode", "std::kizu::ast::StructLiteralExprNode",
		"StructFieldInitNode", "std::kizu::ast::StructFieldInitNode",
		"ArenaNewExprNode", "std::kizu::ast::ArenaNewExprNode",
		"TryExprNode", "std::kizu::ast::TryExprNode",
		"ComptimeExprNode", "std::kizu::ast::ComptimeExprNode",
		"BlockNode", "std::kizu::ast::BlockNode",
		"IfNode", "std::kizu::ast::IfNode",
		"LetNode", "std::kizu::ast::LetNode",
		"AssignNode", "std::kizu::ast::AssignNode",
		"ReturnNode", "std::kizu::ast::ReturnNode",
		"DeferNode", "std::kizu::ast::DeferNode",
		"ErrDeferNode", "std::kizu::ast::ErrDeferNode",
		"ExprStmtNode", "std::kizu::ast::ExprStmtNode",
		"WhileNode", "std::kizu::ast::WhileNode",
		"ForNode", "std::kizu::ast::ForNode",
		"BreakNode", "std::kizu::ast::BreakNode",
		"ContinueNode", "std::kizu::ast::ContinueNode",
		"ParamNode", "std::kizu::ast::ParamNode",
		"ImportDeclNode", "std::kizu::ast::ImportDeclNode",
		"FieldNode", "std::kizu::ast::FieldNode",
		"StructDeclNode", "std::kizu::ast::StructDeclNode",
		"EnumDeclNode", "std::kizu::ast::EnumDeclNode",
		"UnionDeclNode", "std::kizu::ast::UnionDeclNode",
		"ImplDeclNode", "std::kizu::ast::ImplDeclNode",
		"UnionVariantNode", "std::kizu::ast::UnionVariantNode",
		"MatchNode", "std::kizu::ast::MatchNode",
		"MatchArmNode", "std::kizu::ast::MatchArmNode",
		"UnsafeNode", "std::kizu::ast::UnsafeNode",
		"ComptimeIfNode", "std::kizu::ast::ComptimeIfNode",
		"FnDeclNode", "std::kizu::ast::FnDeclNode",
		"ContractDeclNode", "std::kizu::ast::ContractDeclNode",
		"SynthLatchNode", "std::kizu::ast::SynthLatchNode",
		"Span", "std::kizu::ast::Span",
		"TokenId", "std::kizu::ast::TokenId",
		"SymbolId", "std::kizu::ast::SymbolId",
		"PrefixOp", "std::kizu::ast::PrefixOp",
		"BinaryOp", "std::kizu::ast::BinaryOp",
		"ChildRange", "std::kizu::ast::ChildRange",
		"Position", "std::kizu::lexer::Position",
		"std::kizu::lexer::Token":
		return true
	default:
		return false
	}
}

// isDiagnosticScalarType reports copyable compiler diagnostic metadata.
func isDiagnosticScalarType(typ Type) bool {
	switch typ {
	case "std::kizu::diagnostic::FileSpan", "std::kizu::diagnostic::RelatedSpan":
		return true
	default:
		return false
	}
}

// isAtomicSupportedType reports whether Atomic<T> is available in v0.1.
func isAtomicSupportedType(typ Type) bool {
	return typ == typeBool || typ == typeI64
}

// checkNoArgConstructor validates a zero-argument builtin constructor.
func checkNoArgConstructor(name string, args []ast.Expression, typ Type) (Type, error) {
	if len(args) != 0 {
		return "", errorf("type error: `%s` expects 0 args, got %d", name, len(args))
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
	unsafe unsafeCaps,
	name string,
) error {
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if ioType != "Io" {
		return errorf("type error: `%s` expects Io, got %s", name, ioType)
	}
	for idx := 1; idx <= 2; idx++ {
		got, err := c.checkExpr(args[idx], env, unsafe)
		if err != nil {
			return err
		}
		if got != typeI64 {
			return errorf("type error: `%s` range expects i64, got %s", name, got)
		}
	}
	return nil
}

// checkIoAndPartitionRange validates io, partition, start, and end arguments.
func (c *Checker) checkIoAndPartitionRange(
	args []ast.Expression,
	env *scope,
	unsafe unsafeCaps,
) error {
	ioType, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if ioType != "Io" {
		return errorf("type error: `std::task::parallel_map` expects Io, got %s", ioType)
	}
	partitionType, err := c.checkExpr(args[1], env, unsafe)
	if err != nil {
		return err
	}
	if partitionType != "Partition" {
		return errorf("type error: `std::task::parallel_map` expects Partition, got %s",
			partitionType)
	}
	for idx := 2; idx <= 3; idx++ {
		got, err := c.checkExpr(args[idx], env, unsafe)
		if err != nil {
			return err
		}
		if got != typeI64 {
			return errorf("type error: `std::task::parallel_map` range expects i64, got %s", got)
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
	return "", errorf("type error: parallel worker `%s` must return void or !void", fn.name)
}

// checkThreadScopedWorker validates the one-argument scoped worker signature.
func (c *Checker) checkThreadScopedWorker(
	typ Type,
	target string,
	targetFn *functionType,
) error {
	if len(targetFn.params) != 1 || targetFn.params[0] != typ {
		return errorf("type error: thread worker `%s` must accept %s", target, typ)
	}
	if targetFn.borrowParams[0] || targetFn.mutBorrowParams[0] {
		return errorf("type error: thread cannot capture borrow parameter `%s`", target)
	}
	if targetFn.returnType != typ {
		return errorf("type error: thread worker `%s` must return %s", target, typ)
	}
	return nil
}

// rejectThreadBoundaryArg rejects values unsafe to move across concurrency boundaries.
func (c *Checker) rejectThreadBoundaryArg(arg ast.Expression, env *scope, unsafe unsafeCaps) error {
	if ident, ok := arg.(*ast.IdentExpr); ok {
		if env.isBorrowed(ident.Name) {
			return errorf("type error: borrow cannot cross concurrency boundary")
		}
		if _, borrowedView := env.lookupBorrowSource(ident.Name); borrowedView {
			return errorf("type error: borrow cannot cross concurrency boundary")
		}
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
		return errorf("type error: raw pointer cannot cross concurrency boundary")
	}
	if _, ok := dynContract(typ); ok {
		return errorf("type error: dyn cannot cross concurrency boundary")
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
	case "std::arena::Arena":
		return errorf("type error: arena cannot cross concurrency boundary")
	case "std::array::Array":
		return errorf("type error: Array cannot cross concurrency boundary in v0.2")
	case "std::map::Map":
		return errorf("type error: Map cannot cross concurrency boundary in v0.2")
	case "std::arena::Handle":
		return errorf("type error: handle cannot cross concurrency boundary")
	case "Mutex":
		return errorf("type error: Mutex cannot cross concurrency boundary in v0.1")
	case "Task":
		return errorf("type error: Task cannot cross concurrency boundary")
	case "Channel", "option":
		return c.rejectThreadBoundaryNamedArg(arg, seen)
	case "Atomic":
		return c.rejectThreadBoundaryAtomic(arg, seen)
	default:
		return nil
	}
}

// rejectThreadBoundaryNamedArg parses and checks a nested static type argument.
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
		return errorf("type error: Atomic<%s> cannot cross concurrency boundary in v0.1", typ)
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
			return errorf("type error: struct `%s.%s` cannot cross concurrency boundary: %w",
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
			return errorf("type error: union `%s::%s` cannot cross concurrency boundary: %w",
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

// dynContract extracts C from dyn C.
func dynContract(typ Type) (string, bool) {
	text := string(typ)
	if !strings.HasPrefix(text, "dyn ") {
		return "", false
	}
	contractName := strings.TrimPrefix(text, "dyn ")
	return contractName, contractName != ""
}

// containsDynType reports whether a type spelling contains a dynamic object.
func containsDynType(typ Type) bool {
	return containsWrappedType(typ, dynTypeMatch)
}

// containsTypeValue reports whether a type spelling contains comptime-only type.
func containsTypeValue(typ Type) bool {
	return containsWrappedType(typ, func(typ Type) bool {
		return typ == typeType
	})
}

// dynTypeMatch reports whether typ is a direct dynamic contract object spelling.
func dynTypeMatch(typ Type) bool {
	_, ok := dynContract(typ)
	return ok
}

// containsWrappedType recursively checks prefixes and static type arguments.
func containsWrappedType(typ Type, match func(Type) bool) bool {
	text := string(typ)
	for {
		switch {
		case strings.HasPrefix(text, "!"):
			text = strings.TrimPrefix(text, "!")
		case strings.HasPrefix(text, "&var "):
			text = strings.TrimPrefix(text, "&var ")
		case strings.HasPrefix(text, "&"):
			text = strings.TrimPrefix(text, "&")
		case strings.HasPrefix(text, "?"):
			text = strings.TrimPrefix(text, "?")
		case strings.HasPrefix(text, "[]"):
			text = strings.TrimPrefix(text, "[]")
		case strings.HasPrefix(text, "const "):
			text = strings.TrimPrefix(text, "const ")
		default:
			if match(Type(text)) {
				return true
			}
			base, arg, ok := splitGenericType(text)
			if !ok {
				return false
			}
			if match(Type(base)) {
				return true
			}
			args, ok := splitGenericArgs(arg)
			if !ok {
				return false
			}
			for _, item := range args {
				if containsWrappedType(Type(item), match) {
					return true
				}
			}
			return false
		}
	}
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
	wantReturn := substituteSelfType(want.returnType, typeName)
	if len(want.params) != len(got.params) || !sameType(wantReturn, got.returnType) {
		return false
	}
	for idx, wantParam := range want.params {
		expected := substituteSelfType(wantParam, typeName)
		if !sameType(expected, got.params[idx]) ||
			want.borrowParams[idx] != got.borrowParams[idx] ||
			want.mutBorrowParams[idx] != got.mutBorrowParams[idx] {
			return false
		}
	}
	return true
}

// substituteSelfType replaces the Self type segment inside one impl signature type.
func substituteSelfType(typ Type, typeName string) Type {
	return Type(substituteSelfTypeName(string(typ), typeName))
}

// substituteSelfTypeName replaces only standalone Self segments in a type spelling.
func substituteSelfTypeName(name string, typeName string) string {
	out, err := typ.SubstituteText(name, map[string]string{"Self": typeName})
	if err != nil {
		return name
	}
	return out
}

// errorUnionElement extracts T from legacy !T.
func errorUnionElement(typ Type) (string, bool) {
	_, success, ok := errorUnionParts(typ)
	return success, ok
}

// errorUnionParts extracts error and success types from !T or Error!T.
func errorUnionParts(union Type) (string, string, bool) {
	return typ.ErrorUnionParts(string(union))
}

// typedErrorUnionParts extracts Error and T from Error!T.
func typedErrorUnionParts(name string) (string, string, bool) {
	errorType, ok, isUnion := typ.ErrorUnionParts(name)
	if !isUnion || errorType == "" {
		return "", "", false
	}
	return errorType, ok, true
}

// checkPrintCall validates the print builtin.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope, unsafe unsafeCaps) (Type, error) {
	if len(expr.Args) != 1 {
		return "", errorf("type error: `print` expects 1 arg, got %d", len(expr.Args))
	}
	got, err := c.checkExpr(expr.Args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got == typeVoid {
		return "", errorf("type error: `print` cannot print void")
	}
	return typeVoid, nil
}

// newScope creates a lexical type scope.
func newScope(parent *scope) *scope {
	return &scope{
		parent: parent, values: map[string]Type{}, mutable: map[string]bool{},
		borrowed: map[string]bool{}, mutBorrow: map[string]bool{},
		borrowSource: map[string]string{},
	}
}

// child creates a nested lexical type scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// define binds a local name to a type in the current scope.
func (s *scope) define(name string, typ Type, mutable bool) error {
	if _, exists := s.values[name]; exists {
		return errorf("type error: duplicate variable `%s`", name)
	}
	s.values[name] = typ
	s.mutable[name] = mutable
	return nil
}

// defineWithSource binds a non-borrow local while preserving view provenance.
func (s *scope) defineWithSource(name string, typ Type, mutable bool, source string) error {
	if err := s.define(name, typ, mutable); err != nil {
		return err
	}
	if source != "" {
		s.borrowSource[name] = source
	}
	return nil
}

// defineParam binds a function parameter and records borrow capabilities.
func (s *scope) defineParam(name string, typ Type, borrowed bool, mutBorrow bool) error {
	source := ""
	if borrowed || isBorrowedViewReturnType(typ) {
		source = name
	}
	return s.defineParamWithSource(name, typ, borrowed, mutBorrow, source)
}

// defineParamWithSource binds a borrowed local and records its source owner.
func (s *scope) defineParamWithSource(
	name string,
	typ Type,
	borrowed bool,
	mutBorrow bool,
	source string,
) error {
	if err := s.define(name, typ, false); err != nil {
		return err
	}
	s.borrowed[name] = borrowed
	s.mutBorrow[name] = mutBorrow
	if source != "" {
		s.borrowSource[name] = source
	}
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

// isBorrowed reports whether a local name is an &T or &var T parameter.
func (s *scope) isBorrowed(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			return cur.borrowed[name]
		}
	}
	return false
}

// isMutBorrowed reports whether a local name is an &var T parameter.
func (s *scope) isMutBorrowed(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			return cur.mutBorrow[name]
		}
	}
	return false
}

// lookupBorrowSource resolves the provenance source for a borrowed local.
func (s *scope) lookupBorrowSource(name string) (string, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			source := cur.borrowSource[name]
			return source, source != ""
		}
	}
	return "", false
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

// singleGenericArg returns the only argument for one-parameter generic types.
func singleGenericArg(base string, args []string) (string, error) {
	if len(args) != 1 {
		return "", errorf("type error: `%s` expects 1 static argument", base)
	}
	return args[0], nil
}
