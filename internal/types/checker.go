package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/stdprim"
	"github.com/kizu-lang/kizu/internal/typ"
)

// use reports whether an unproven operation is covered here, and records the
// use against the innermost marker.
func (mark unsafeMark) use() bool {
	if mark == nil {
		return false
	}
	mark.used = true
	return true
}

// underMark checks value under a fresh marker and rejects a marker that covers
// no unproven operation. Both the expression and the assignment-target paths go
// through here, so the rule for what a marker must cover lives in one place.
func (c *Checker) underMark(
	expr *ast.UnsafeExpr,
	check func(unsafeMark) (Type, error),
) (Type, error) {
	inner := &unsafeScope{}
	valueType, err := check(inner)
	if err != nil {
		return valueType, err
	}
	if !inner.used {
		return valueType, errorAtCode(expr.Span, "unsafe.unused_marker", "%s",
			"unsafe error: `unsafe` covers no operation that needs it"+
				"\nhelp: remove `unsafe`")
	}
	if expr.Safety == "" {
		return valueType, errorAtCode(expr.Span, "unsafe.missing_safety_comment", "%s",
			"unsafe error: `unsafe` needs a `// SAFETY:` comment on its statement"+
				"\nhelp: write `// SAFETY: <why this holds>` on the line above")
	}
	return valueType, nil
}

// Checker validates type rules for a parsed program.
type Checker struct {
	checkerMetadata
	types typeTable
	// declaredDeinits names the types whose cleanup an author wrote. They hold
	// an obligation of their own, so their fields are not taken one at a time.
	declaredDeinits map[string]bool
	currentReturn   Type
	currentFunction *functionType
	currentStd      bool
	typeParams      typeParamStore
	typeArgValues   map[string]Type
	// staticParams holds the compile-time value parameters of the generic being
	// checked, by declared type. A runtime local of the same name is not one of
	// these, which is what separates forwarding a static value from reading a
	// value that only exists at run time.
	staticParams map[string]Type
	// metaFields binds the captures of the `comptime for` expansions currently
	// open, by capture name. A capture is not a value, so it lives here rather
	// than in a scope: the only thing that may read it is a `std::meta` form.
	metaFields map[string]metaField
	loopLabels []string
	// checkedStdBodies records the std wrapper instantiations already checked,
	// keyed by name and static arguments.
	checkedStdBodies map[string]bool
	// checkedInstances records the generic instantiations already checked, by
	// the same key. One instance means one body with one set of static
	// arguments, so checking it twice can only reach the same answer.
	checkedInstances map[string]bool
	// instantiationDepth counts the generic instantiations open above the one
	// being checked, which is what bounds a body whose own calls grow their
	// type argument (issue #1627).
	instantiationDepth int
	// deinitOwners marks the base type names whose values carry a deinit
	// contract, seeded from ast.DeinitOwners — the one definition of owner-ness.
	deinitOwners map[string]bool
	// captureCondition is set while an if/while capture condition is typed.
	// The borrow-optional accessors (`at` / `at_mut`) return their `?&T` /
	// `?&var T` only in this context and refuse everywhere else.
	captureCondition bool
}

// New creates an empty type checker.
func New() *Checker {
	return &Checker{
		checkerMetadata:  newCheckerMetadata(),
		types:            newTypeTable(),
		checkedStdBodies: map[string]bool{},
		checkedInstances: map[string]bool{},
		metaFields:       map[string]metaField{},
	}
}

// Check validates the program and returns the first type error.
func (c *Checker) Check(program *ast.Program) error {
	c.deinitOwners = ast.DeinitOwners(program)
	c.declaredDeinits = ast.DeclaredDeinits(program)
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
		}
	}
	return nil
}

// CheckAll validates the program like Check but accumulates one error per
// top-level declaration instead of stopping at the first, so editors can show
// every independent type error at once. Setup phases that the body checks
// depend on still fail fast, since later errors would be noise without them.
func (c *Checker) CheckAll(program *ast.Program) []error {
	c.deinitOwners = ast.DeinitOwners(program)
	c.declaredDeinits = ast.DeclaredDeinits(program)
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
	// Combined error sets resolve after every set is collected, so the sets
	// they name may be declared later in the file.
	if err := c.resolveErrorSetCompositions(); err != nil {
		return err
	}
	for _, decl := range program.Decls {
		if err := c.collectMethodDecl(decl); err != nil {
			return err
		}
	}
	// Assertions run last: `impl Writer for File;` says what File already is, so
	// every method has to be registered before one can be answered, wherever in
	// the file the assertion happens to sit.
	for _, decl := range program.Decls {
		impl, ok := decl.(*ast.ImplDecl)
		if !ok {
			continue
		}
		if err := c.collectImpl(impl); err != nil {
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
	case *ast.ErrorSetDecl:
		return c.collectErrorSet(d)
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

// collectMethodDecl registers one method declaration.
func (c *Checker) collectMethodDecl(decl ast.Decl) error {
	if fn, ok := decl.(*ast.FunctionDecl); ok {
		return c.collectReceiverMethod(fn)
	}
	return nil
}

// collectReceiverMethod files a `fn (self: T) name(...)` declaration under the
// type it is a method on. Its name already says which that is: the loader files
// a method under its receiver, so `app::Trace.deinit` is `deinit` on
// `app::Trace` and needs no second reading of the receiver.
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
		methods = map[string]*functionType{}
		c.impls[receiver] = methods
	}
	if _, exists := methods[name]; exists {
		return errorf("type error: duplicate method `%s`", decl.Name)
	}
	fnType, err := c.newDeclaredFunctionType(decl)
	if err != nil {
		return err
	}
	fnType.name = decl.Name
	methods[name] = fnType
	return nil
}

// checkPublicAPI rejects private types exposed through public declarations.
func (c *Checker) checkPublicAPI(program *ast.Program) error {
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			if !d.Public {
				continue
			}
			if err := c.checkPublicSignature(d.FunctionSignature); err != nil {
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

// checkPublicSignature rejects private types named by a signature that is on
// the public boundary. Whether it is there is the caller's to decide: a
// function is when it is declared public, and every method of a public contract
// is whatever the method itself says.
func (c *Checker) checkPublicSignature(sig ast.FunctionSignature) error {
	for _, param := range sig.Params {
		label := "function `" + sig.Name + "` parameter"
		if err := c.rejectPrivateType(typ.Text(param.TypeName), label); err != nil {
			return err
		}
	}
	returnType := typ.Text(sig.ReturnType)
	if returnType == "" {
		return nil
	}
	return c.rejectPrivateType(returnType, "function `"+sig.Name+"` return type")
}

// checkPublicStructFields validates public fields on one struct.
func (c *Checker) checkPublicStructFields(decl *ast.StructDecl) error {
	for _, field := range decl.Fields {
		if !field.Public {
			continue
		}
		// An `unsafe struct` keeps every field private. That is what pins the
		// code able to break its invariant to its declaration file even when a
		// directory module has other implementation files (SPEC §12).
		if decl.RequiresUnsafe {
			return errorf("unsafe error: `unsafe struct %s` cannot have `pub` field `%s`"+
				"\nhelp: drop `pub` so only this file can break the invariant",
				decl.Name, field.Name)
		}
		context := "field `" + decl.Name + "." + field.Name + "`"
		if err := c.rejectPrivateType(typ.Text(field.TypeName), context); err != nil {
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
		if variant.Payload == nil {
			continue
		}
		context := "union variant `" + decl.Name + "::" + variant.Name + "`"
		if err := c.rejectPrivateType(typ.Text(variant.Payload), context); err != nil {
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
		if err := c.checkPublicSignature(method.FunctionSignature); err != nil {
			return err
		}
	}
	return nil
}

// collectTopLevelFunctions registers top-level function signatures. A name a
// type already took is rejected here: `Point(3)` reads as constructing a Point,
// so letting a function claim that spelling makes the call site unreadable.
// Every type name is predeclared before this pass, so the answer does not
// depend on which declaration was written first.
func (c *Checker) collectTopLevelFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if _, exists := c.functions[fn.Name]; exists {
			return errorf("type error: duplicate function `%s`", fn.Name)
		}
		if c.isTypeName(fn.Name) {
			return errorf("type error: `%s` is a type and cannot name a function",
				fn.Name)
		}
		if fn.Receiver {
			receiver, name, ok := stdmethod.SplitMethodName(fn.Name)
			if ok {
				method := c.implMethod(receiver, name)
				if method != nil {
					c.functions[fn.Name] = method
					continue
				}
			}
		}
		fnType, err := c.newDeclaredFunctionType(fn)
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
		fnType, err := c.newDeclaredFunctionType(method)
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
	return c.checkSatisfies(decl.ContractName, Type(decl.TypeName))
}

// checkSatisfies reports why a type does not satisfy a contract, or nil when it
// does. A type satisfies one by having the methods; this is where that is said
// out loud, so the answer arrives at the type rather than at a call far away.
func (c *Checker) checkSatisfies(contractName string, typeName Type) error {
	contract := c.contracts[contractName]
	if contract == nil {
		return errorf("type error: unknown contract `%s`", contractName)
	}
	for _, name := range sortedMethodNames(contract.methods) {
		got := c.implMethod(string(typeName), name)
		if got == nil {
			return errorf("type error: `%s` does not satisfy `%s`: missing method `%s`",
				typeName, contractName, name)
		}
		if !methodMatches(contract.methods[name], got) {
			return errorf("type error: `%s.%s` does not match contract `%s`",
				typeName, name, contractName)
		}
	}
	return nil
}

// rejectDuplicateTypeName reports a name already taken by another declaration.
// Every declaration asks here, so a name is taken whichever kind claimed it
// first: checking only the kinds that came before leaves the answer depending
// on the order two declarations were written in.
func (c *Checker) rejectDuplicateTypeName(name string) error {
	if c.hasDuplicateTypeName(name) {
		return errorf("type error: duplicate type `%s`", name)
	}
	return nil
}

// hasDuplicateTypeName reports a name already retained by a concrete type.
func (c *Checker) hasDuplicateTypeName(name string) bool {
	if _, exists := c.errorSets[name]; exists {
		return true
	}
	if _, exists := c.enums[name]; exists {
		return true
	}
	if _, exists := c.structs[name]; exists {
		return true
	}
	_, exists := c.unions[name]
	return exists
}

// collectUnion registers and validates a tagged union declaration.
func (c *Checker) collectUnion(decl *ast.UnionDecl) error {
	if err := c.rejectDuplicateTypeName(decl.Name); err != nil {
		return err
	}
	previousTypeParams := c.typeParams.enter(decl.TypeParams)
	defer c.typeParams.restore(previousTypeParams)
	union := &unionType{
		name: decl.Name, typeParams: decl.TypeParams,
		variants: map[string]Type{}, public: decl.Public,
	}
	for _, variant := range decl.Variants {
		if _, exists := union.variants[variant.Name]; exists {
			return errorf("type error: duplicate union variant `%s::%s`",
				decl.Name, variant.Name)
		}
		payloadType := Type("")
		if variant.Payload != nil {
			parsed, err := c.parseTypeNode(variant.Payload)
			if err != nil {
				return err
			}
			written := typ.Text(variant.Payload)
			if payload := stdmeta.ResolveElementTypeForms(written); payload != written {
				parsed, err = c.parseType(payload)
				if err != nil {
					return err
				}
			}
			if _, ok := optionalElem(parsed); ok {
				return errorf("type error: union payload `%s::%s` cannot store an optional yet",
					decl.Name, variant.Name)
			}
			if isBorrowPayload(variant.Payload) {
				return errorf("type error: borrow payload `%s.%s` cannot store borrow",
					decl.Name, variant.Name)
			}
			if c.types.containsTypeValue(parsed) {
				return errorf("type error: union variant `%s::%s` cannot store type value",
					decl.Name, variant.Name)
			}
			payloadType = parsed
		}
		union.variants[variant.Name] = payloadType
		union.order = append(union.order, variant.Name)
	}
	c.unions[decl.Name] = union
	return nil
}

// collectStruct registers and validates a struct declaration.
func (c *Checker) collectStruct(decl *ast.StructDecl) error {
	if err := c.rejectDuplicateTypeName(decl.Name); err != nil {
		return err
	}
	if err := requireObligationDoc(decl.RequiresUnsafe, decl.Doc,
		"unsafe struct "+decl.Name, "the invariant its fields carry"); err != nil {
		return err
	}
	c.structs[decl.Name] = decl
	previousTypeParams := c.typeParams.enter(decl.TypeParams)
	defer c.typeParams.restore(previousTypeParams)
	for _, field := range decl.Fields {
		typ, err := c.parseTypeNode(field.TypeName)
		if err != nil {
			return err
		}
		if field.Borrow {
			return errorf("type error: borrow field `%s.%s` cannot store borrow",
				decl.Name, field.Name)
		}
		if rawPointerFieldRequiresUnsafe(&c.types, decl.RequiresUnsafe, typ) {
			return errorf("unsafe error: struct `%s` holds a raw pointer in field `%s`, "+
				"so it must be declared `unsafe struct`"+
				"\nhelp: write `unsafe struct %s` and document the invariant its fields carry",
				decl.Name, field.Name, decl.Name)
		}
		if compileTimeOnlyType(&c.types, typ) {
			return errorf("type error: struct field `%s.%s` cannot store %s",
				decl.Name, field.Name, typ)
		}
		if c.types.containsTypeValue(typ) {
			return errorf("type error: struct field `%s.%s` cannot store type value",
				decl.Name, field.Name)
		}
		if c.types.containsBufferType(typ) {
			return errorf("type error: struct field `%s.%s` cannot store stack buffer",
				decl.Name, field.Name)
		}
		if elem, ok := optionalElem(typ); ok && !c.optionalFieldElemAllowed(elem) {
			return errorf(
				"type error: struct field `%s.%s` cannot store an optional view;"+
					" a capture would be hidden from the rules that read field types",
				decl.Name, field.Name)
		}
	}
	return nil
}

// optionalFieldElemAllowed reports whether ?elem may be a struct field. Plain
// copy data (arena handles included) carries no cleanup obligation, so the
// presence tag is the whole story. An owner carries one on the path where the
// value is there, and that is a contract the type states: the struct declares
// a `deinit` that opens the optional, and the completeness check counts the
// field like any other owner field.
//
// An optional view stays out. A view's obligation is a borrow that outlives
// nothing visible, and a capture opening it would be hidden from the rules
// that read field types.
func (c *Checker) optionalFieldElemAllowed(elem Type) bool {
	return c.isPlainDataType(string(elem), nil) || c.ownerType(elem)
}

// unionHasOwnerPayload reports whether any variant payload is an owner payload.
func (c *Checker) unionHasOwnerPayload(decl *ast.UnionDecl) bool {
	union := c.unions[decl.Name]
	if union == nil {
		return false
	}
	for _, payload := range union.variants {
		if payload != "" && ast.OwnerType(c.deinitOwners, string(payload)) {
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
			"type error: generic owner-payload union `%s` is unsupported; "+
				"use a concrete owner union with explicit `deinit`",
			decl.Name)
	}
	method := c.implMethod(decl.Name, "deinit")
	if method == nil {
		// The union holds an owner and declares nothing, so its cleanup is the
		// derived one (ast.DeriveDeinit) and there is no author's body to
		// validate. What that body does is fixed by the generator.
		return nil
	}
	if err := c.checkOwnerUnionDeinitSignature(decl, method); err != nil {
		return err
	}
	return c.checkOwnerUnionDeinitBody(decl, method)
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
func (c *Checker) checkOwnerUnionDeinitBody(decl *ast.UnionDecl, fn *functionType) error {
	if fn == nil || len(fn.sig.Params) == 0 {
		return errorf("type error: owner-payload union `%s` deinit must take `self`", decl.Name)
	}
	selfName := fn.sig.Params[0].Name
	match := ownerUnionSelfMatch(fn.body, selfName)
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
		payload := c.unions[decl.Name].variants[variant.Name]
		if payload == "" || !ast.OwnerType(c.deinitOwners, string(payload)) {
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
				"type error: owner-payload union variant `%s::%s` must clean its payload "+
					"via `%s.deinit()`",
				decl.Name, variant.Name, arm.Binding)
		}
	}
	return nil
}

// functionParamInfo keeps parsed parameter types aligned with function metadata.
type functionParamInfo struct {
	params          []Type
	borrowParams    []bool
	mutBorrowParams []bool
}

// newDeclaredFunctionType builds the type for a declaration and attaches the
// declaration to it. The attaching happens here rather than inside
// newFunctionType, which never sees a body: the body is what checking and
// instantiation run over, not what a caller is promised.
func (c *Checker) newDeclaredFunctionType(fn *ast.FunctionDecl) (*functionType, error) {
	if err := requireObligationDoc(fn.RequiresUnsafe, fn.Doc,
		"unsafe fn "+fn.Name, "what the caller must uphold"); err != nil {
		return nil, err
	}
	fnType, err := c.newFunctionType(fn.FunctionSignature)
	if err != nil {
		return nil, err
	}
	fnType.body = fn.Body
	return fnType, nil
}

// newFunctionType builds the type a call site is promised. It takes the
// signature rather than the declaration, so what a caller sees cannot be read
// out of the body: a signature that came from a body is one no other package
// can be told without being shipped the body too.
func (c *Checker) newFunctionType(fn ast.FunctionSignature) (*functionType, error) {
	previousTypeParams := c.typeParams.enterSignature(fn)
	defer c.typeParams.restore(previousTypeParams)

	if index, reserved := reservedFunctionStaticParamIndex(fn); reserved {
		param := fn.StaticParams[index]
		return nil, errorf(
			"type error: Function static parameter `%s` is reserved for std", param.Name)
	}
	paramInfo, err := c.collectFunctionParams(fn)
	if err != nil {
		return nil, err
	}
	ret := typeVoid
	if fn.ReturnType != nil {
		var err error
		ret, err = c.parseTypeNode(fn.ReturnType)
		if err != nil {
			return nil, err
		}
	}
	if compileTimeOnlyType(&c.types, ret) {
		return nil, errorf("type error: function `%s` cannot return %s", fn.Name, ret)
	}
	if c.types.containsBufferType(ret) {
		return nil, errorf(
			"type error: function `%s` cannot return a stack buffer; "+
				"return an owned buffer or write through a view", fn.Name)
	}
	if c.types.containsTypeValue(ret) {
		return nil, errorf("type error: function `%s` cannot return type", fn.Name)
	}
	if !fn.Std && c.types.containsBorrowOptional(ret) {
		if _, _, bare := typ.BorrowOptionalElem(string(ret)); !bare {
			return nil, errorf(
				"type error: function `%s` cannot nest a borrow optional in its"+
					" return; declare a bare `?&T` / `?&var T`", fn.Name)
		}
	}
	return &functionType{
		name: fn.Name, sig: fn, params: paramInfo.params,
		borrowParams:    paramInfo.borrowParams,
		mutBorrowParams: paramInfo.mutBorrowParams,
		returnType:      ret,
	}, nil
}

// collectFunctionParams validates function parameters and records call-time metadata.
func (c *Checker) collectFunctionParams(fn ast.FunctionSignature) (functionParamInfo, error) {
	info := functionParamInfo{
		params:          make([]Type, 0, len(fn.Params)),
		borrowParams:    make([]bool, 0, len(fn.Params)),
		mutBorrowParams: make([]bool, 0, len(fn.Params)),
	}
	for _, param := range fn.Params {
		paramType, err := c.parseTypeNode(param.TypeName)
		if err != nil {
			return functionParamInfo{}, err
		}
		if !fn.Std && c.types.containsBorrowOptional(paramType) {
			return functionParamInfo{}, errorf(
				"type error: parameter `%s` cannot hold a borrow optional;"+
					" `?&T` exists only as a capture condition", param.Name)
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
	if c.types.containsTypeValue(paramType) {
		return errorf("type error: parameter `%s` cannot have type", param.Name)
	}
	if c.types.containsBufferType(paramType) {
		return errorf(
			"type error: stack buffer parameter `%s` is not supported; pass a view (`[]u8` or `&var []u8`)",
			param.Name)
	}
	// A function name and a field token are known only at compile time, so they
	// belong in the static argument list rather than the runtime parameter list.
	if compileTimeOnlyType(&c.types, paramType) {
		return errorf(
			"type error: %s parameter `%s` belongs in `<...>`, not `(...)`",
			paramType, param.Name)
	}
	if _, ok := optionalElem(paramType); ok && param.Borrow {
		return errorf(
			"type error: parameter `%s` cannot borrow an optional yet", param.Name)
	}
	return nil
}

// instantiateTypeArgText replaces in-scope generic type parameters in a type-apply list.
func (c *Checker) instantiateTypeArgText(typeArg string) string {
	if len(c.typeArgValues) == 0 {
		return typeArg
	}
	args, err := typ.SplitArgs(typeArg)
	if err != nil {
		return string(c.types.substituteTypeParams(Type(typeArg), c.typeArgValues))
	}
	for idx, arg := range args {
		args[idx] = string(c.types.substituteTypeParams(Type(arg), c.typeArgValues))
	}
	return strings.Join(args, ", ")
}

// parseType validates a source-level type name. The spelling is read once, so
// which type this is comes from its structure rather than from where a byte
// happens to sit: the `!` in `Array<!i64>` belongs to the argument, not to this
// type.
func (c *Checker) parseType(name string) (Type, error) {
	return resolvedType(c.resolveType(name))
}

// parseTypeNode validates a type the parser already read, which is every type a
// declaration writes. Only a type the compiler itself spells still arrives as
// text, and parseType is the entry for those.
func (c *Checker) parseTypeNode(parsed typ.Type) (Type, error) {
	return resolvedType(c.resolveTypeNode(parsed))
}

// rejectPrivateType reports an error when typeName exposes a private declaration.
func (c *Checker) rejectPrivateType(typeName string, context string) error {
	for _, name := range c.types.referencedTypeNames(Type(typeName)) {
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

// referencedTypeNames returns the names a type spelling mentions. The names come
// from the parsed structure, so every wrapper is seen however they nest: reading
// the names off the text instead stopped at the first wrapper it recognized, and
// `&[]Secret` answered `[]Secret`, a name no declaration can have.
func (t *typeTable) referencedTypeNames(typeName Type) []string {
	parsed, ok := t.lookup(typeName)
	if !ok {
		return []string{string(typeName)}
	}
	var names []string
	typ.Walk(parsed, func(node typ.Type) {
		if name, ok := node.(*typ.Name); ok {
			names = append(names, strings.Join(name.Path, "::"))
		}
	})
	return names
}

// checkMainReturnType keeps the entry point returning `void`, an error union
// over void, or an error union over std::process::ExitStatus (ADR-0085). A
// program does not choose its own exit status as an integer: an exit status is
// platform-shaped, and the ExitStatus union is the one value the entry point
// maps to it.
func checkMainReturnType(fn *functionType) error {
	if fn.name != "main" {
		return nil
	}
	returned := strings.TrimSpace(typ.Text(fn.sig.ReturnType))
	if returned == "" || returned == "void" || strings.HasSuffix(returned, "!void") ||
		strings.HasSuffix(returned, "!std::process::ExitStatus") {
		return nil
	}
	return errorf(
		"type error: `main` returns `%s`, expected `void`, `!void` or `!std::process::ExitStatus`",
		returned)
}

// defineScopeParam binds a parameter and derives its root provenance from the
// type policy owned by the checker type table.
func defineScopeParam(
	types *typeTable,
	s *scope,
	name string,
	typ Type,
	borrowed bool,
	mutBorrow bool,
) bool {
	var sources []string
	if borrowed || types.isBorrowedViewReturnType(typ) {
		sources = []string{name}
	}
	return s.defineParamWithSource(name, typ, borrowed, mutBorrow, sources, false)
}

// defineSignatureParam binds one function parameter. Only these bindings are
// the caller's storage, which lets assignment through a `&var` one store there.
func defineSignatureParam(
	types *typeTable,
	s *scope,
	name string,
	typ Type,
	borrowed bool,
	mutBorrow bool,
) bool {
	var sources []string
	if borrowed || types.isBorrowedViewReturnType(typ) {
		sources = []string{name}
	}
	return s.defineParamWithSource(name, typ, borrowed, mutBorrow, sources, true)
}

// defineStaticValueParams puts the compile-time values a `<...>` list declares
// into scope, and returns them by declared type. A body reads them like any
// other name, and a static argument list needs to tell them apart from a
// runtime local, so both callers set up a generic body through here.
func defineStaticValueParams(
	types *typeTable,
	env *scope,
	sig ast.FunctionSignature,
) (map[string]Type, error) {
	staticParams := map[string]Type{}
	for _, param := range sig.StaticParams {
		if param.IsType() {
			continue
		}
		defined := defineScopeParam(types, env, param.Name, Type(typ.Text(param.Type)), false, false)
		if err := requireScopeDefinition(param.Name, defined); err != nil {
			return nil, err
		}
		staticParams[param.Name] = Type(typ.Text(param.Type))
	}
	return staticParams, nil
}

// checkFunction validates one function body against its signature.
func (c *Checker) checkFunction(fn *functionType) error {
	if fn.sig.ExternABI != "" {
		return nil
	}
	if err := checkMainReturnType(fn); err != nil {
		return err
	}
	env := newScope(nil)
	staticParams, err := defineStaticValueParams(&c.types, env, fn.sig)
	if err != nil {
		return err
	}
	for idx, param := range fn.sig.Params {
		defined := defineSignatureParam(
			&c.types, env, param.Name, fn.params[idx], param.Borrow, param.MutBorrow)
		err := requireScopeDefinition(param.Name, defined)
		if err != nil {
			return err
		}
	}
	previousReturn := c.currentReturn
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeParams := c.typeParams.enterSignature(fn.sig)
	previousStaticParams := c.staticParams
	previousLoops := c.loopLabels
	c.currentReturn = fn.returnType
	c.currentFunction = fn
	c.currentStd = fn.sig.Std
	c.staticParams = staticParams
	c.loopLabels = nil
	defer func() {
		c.currentReturn = previousReturn
		c.currentFunction = previousFunction
		c.currentStd = previousStd
		c.typeParams.restore(previousTypeParams)
		c.staticParams = previousStaticParams
		c.loopLabels = previousLoops
	}()
	returns, err := c.checkBlock(fn.body, env, fn.returnType, nil)
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
	fn := &ast.FunctionDecl{
		FunctionSignature: ast.FunctionSignature{Name: "test " + strconv.Quote(decl.Name)},
		Body:              decl.Body,
	}
	return c.checkFunction(&functionType{
		name:           fn.Name,
		sig:            fn.FunctionSignature,
		returnType:     "!void",
		body:           fn.Body,
		implicitReturn: true,
	})
}

// checkBlock validates statements and reports whether the block always returns.
func (c *Checker) checkBlock(
	block *ast.BlockStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	for _, stmt := range block.Statements {
		returns, err := c.checkStmt(stmt, env, wantReturn, unsafe)
		if err != nil {
			return returns, err
		}
		if returns {
			// A block that returns still has to account for what it bound: an
			// early return does not make the bindings before it needed.
			return true, unreadLocalError(env)
		}
	}
	return false, unreadLocalError(env)
}

// requireScopeDefinition turns the scope's copy-only duplicate decision into
// the source-facing checker diagnostic.
func requireScopeDefinition(name string, defined bool) error {
	if defined {
		return nil
	}
	return errorf("type error: duplicate variable `%s`", name)
}

// unreadLocalError turns the stable first unread binding into its diagnostic.
func unreadLocalError(env *scope) error {
	names := make([]string, 0, len(env.bindings))
	for name, binding := range env.bindings {
		if binding.unread {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	name := names[0]
	return errorAtCode(env.bindings[name].declaration, "type.unused_local",
		"type error: local `%s` is never read"+
			"\nhelp: remove it, or write `let _ = ...` to drop the value on purpose", name)
}

// checkStmt validates a statement and reports explicit return flow.
func (c *Checker) checkStmt(
	stmt ast.Statement,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
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
	default:
		return c.checkBodyStmt(stmt, env, wantReturn, unsafe)
	}
}

// checkBodyStmt checks the statements that carry a body of their own, and the
// branches that leave one.
func (c *Checker) checkBodyStmt(
	stmt ast.Statement,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	switch s := stmt.(type) {
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
	case *ast.BlockStmt:
		// Only a match arm body is a bare block statement (SPEC §6.12).
		return c.checkBlock(s, env.child(), wantReturn, unsafe)
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env, wantReturn, unsafe)
	case *ast.ComptimeForStmt:
		return c.checkComptimeForStmt(s, env, wantReturn, unsafe)
	case *ast.ComptimeMatchStmt:
		return c.checkComptimeMatchStmt(s, env, wantReturn, unsafe)
	default:
		return false, errorf("type error: unsupported statement %T", stmt)
	}
}

// checkDeferStmt validates the first supported block cleanup registration form.
func (c *Checker) checkDeferStmt(stmt *ast.DeferStmt, env *scope, unsafe unsafeMark) (bool, error) {
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

// checkErrDeferStmt validates an error-path cleanup registration. It shares the
// cleanup-call shape with defer; the path-sensitive timing difference is handled
// by lowering and the runtime, not by the type surface.
func (c *Checker) checkErrDeferStmt(
	stmt *ast.ErrDeferStmt,
	env *scope,
	unsafe unsafeMark,
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
	if !ok {
		return errorf("type error: %s expects cleanup method call", keyword)
	}
	// The source already holds a method call, so naming the method it wanted
	// beats repeating that one was expected. There is only one (ADR-0119).
	if field.Name != typ.CleanupMethod {
		return errorf("type error: %s cleanup must be `%s`, got `%s`",
			keyword, typ.CleanupMethod, field.Name)
	}
	return nil
}

// checkLetStmt validates a let or var declaration.
// checkLetStmt validates one local binding and records it as one this scope
// will be asked about, so a binding nothing reads can be told apart from one
// that carried a value somewhere.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope, unsafe unsafeMark) (bool, error) {
	returns, err := c.checkLetBinding(stmt, env, unsafe)
	if err != nil {
		return returns, err
	}
	env.declareLocal(stmt.Name, expressionSpan(stmt.Value))
	return returns, nil
}

// checkLetBinding types one local binding and binds its name.
func (c *Checker) checkLetBinding(stmt *ast.LetStmt, env *scope, unsafe unsafeMark) (bool, error) {
	handled, err := c.defineSpecialLetInitializer(stmt, env, unsafe)
	if handled || err != nil {
		return false, err
	}
	typ, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	if c.types.containsTypeValue(typ) {
		return false, errorf("type error: type value cannot be stored in local `%s`", stmt.Name)
	}
	if _, mutable, inner, ok := explicitBorrowType(typ); ok {
		sources := c.exprBorrowSourceList(stmt.Value, env, unsafe)
		return false, requireScopeDefinition(
			stmt.Name, env.defineParamWithSource(stmt.Name, inner, true, mutable, sources, false))
	}
	if c.types.isBorrowedViewReturnType(typ) {
		sources := c.exprBorrowSourceList(stmt.Value, env, unsafe)
		if len(sources) > 0 {
			return false, requireScopeDefinition(
				stmt.Name, env.defineWithSource(stmt.Name, typ, stmt.Mutable, sources))
		}
	}
	return false, requireScopeDefinition(stmt.Name, env.define(stmt.Name, typ, stmt.Mutable))
}

// defineSpecialLetInitializer records local borrow/view initializers with source data.
func (c *Checker) defineSpecialLetInitializer(
	stmt *ast.LetStmt,
	env *scope,
	unsafe unsafeMark,
) (bool, error) {
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		typ, mutable, err := c.checkBorrowPrefix(borrow, env, unsafe)
		if err != nil {
			return true, err
		}
		sources := c.exprBorrowSourceList(borrow.Right, env, unsafe)
		return true, requireScopeDefinition(
			stmt.Name, env.defineParamWithSource(stmt.Name, typ, true, mutable, sources, false))
	}
	typ, mutable, ok, err := c.checkBoxBorrowInitializer(stmt.Value, env, unsafe)
	if ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, requireScopeDefinition(
			stmt.Name, defineScopeParam(&c.types, env, stmt.Name, typ, true, mutable))
	}
	sources, mutable, ok, err := c.checkStringViewInitializer(stmt.Value, env, unsafe)
	if ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, requireScopeDefinition(stmt.Name,
			env.defineParamWithSource(stmt.Name, typeByteString, true, mutable, sources, false))
	}
	return false, nil
}

// checkBoxBorrowInitializer recognizes box.borrow/borrow_mut local borrow initializers.
func (c *Checker) checkBoxBorrowInitializer(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
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

// boxBorrowMutReceiverIsMutable accepts a mutable local Box or a Box reached
// through a field path on a mutable owner.
func boxBorrowMutReceiverIsMutable(expr ast.Expression, env *scope) bool {
	if receiver, ok := expr.(*ast.IdentExpr); ok {
		return env.isMutable(receiver.Name)
	}
	root, _, ok := ast.FieldPathRoot(expr)
	return ok && env.isMutable(root.Name)
}

// checkStringViewInitializer recognizes string.as_bytes() / as_mut_bytes()
// local byte views. The boolean result mutable reports the as_mut_bytes form,
// whose receiver must be a writable String place (ADR-0096).
func (c *Checker) checkStringViewInitializer(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
) ([]string, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false, false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "as_bytes" && field.Name != "as_mut_bytes") {
		return nil, false, false, nil
	}
	mutable := field.Name == "as_mut_bytes"
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return nil, mutable, true, err
	}
	if receiver != "std::string::String" && !c.types.isBufferType(receiver) {
		return nil, mutable, true, errorf(
			"type error: `%s` expects String or stack buffer receiver", field.Name)
	}
	kind := "String"
	if c.types.isBufferType(receiver) {
		kind = "buffer"
	}
	if len(call.Args) != 0 {
		return nil, mutable, true, errorf("type error: `%s.%s` expects 0 args, got %d",
			kind, field.Name, len(call.Args))
	}
	if mutable {
		// A `String` reached through a field path is writable when the local
		// at the root of that path is, which is the rule every other `&var`
		// position reads (ADR-0111).
		place, ok := mutablePlaceBase(field.Receiver)
		if !ok || !(env.isMutable(place.Name) || env.isMutBorrowed(place.Name)) {
			return nil, mutable, true, errorf(
				"type error: `%s.as_mut_bytes` requires mutable %s binding", kind, kind)
		}
	}
	sources := c.exprBorrowSourceList(field.Receiver, env, unsafe)
	return sources, mutable, true, nil
}

// checkContainerBorrowCondition recognizes capture conditions whose call
// produces a borrow optional — std at/at_mut, or a function that declares a
// `?&T` / `?&var T` return. The call is typed through the normal call path
// with the capture context set, so each callee's own checker validates
// receiver and arguments once, and this recognizer only reads the payload
// class of the type that comes back.
func (c *Checker) checkContainerBorrowCondition(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false, false, nil
	}
	switch callee := call.Callee.(type) {
	case *ast.FieldExpr:
		if !captureReceiverShape(callee.Receiver) {
			return "", false, false, nil
		}
	case *ast.IdentExpr:
	default:
		return "", false, false, nil
	}
	saved := c.captureCondition
	c.captureCondition = true
	result, err := c.checkExpr(expr, env, unsafe)
	c.captureCondition = saved
	if err != nil {
		return "", false, true, err
	}
	elem, mutable, ok := typ.BorrowOptionalElem(string(result))
	if !ok {
		// A call producing any other type: the generic condition path types
		// that call.
		return "", false, false, nil
	}
	return Type(elem), mutable, true, nil
}

// checkAssignStmt validates assignment to an existing binding.
func (c *Checker) checkAssignStmt(
	stmt *ast.AssignStmt,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) (Type, error) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		return checkAssignableIdent(target, env)
	case *ast.FieldExpr:
		return c.checkAssignableField(target, env, unsafe)
	case *ast.DerefExpr:
		return c.checkAssignableDeref(target, env, unsafe)
	case *ast.IndexExpr:
		return c.checkAssignableIndex(target, env, unsafe)
	case *ast.UnsafeExpr:
		// `unsafe p.* = value` marks the store, so the marker sits on the
		// target. It covers the target only: the assigned value is a separate
		// expression and needs its own marker if it is unproven too.
		return c.underMark(target, func(inner unsafeMark) (Type, error) {
			return c.checkAssignableTarget(target.Value, env, inner)
		})
	default:
		return "", errorf("type error: invalid assignment target `%s`", expr.String())
	}
}

// checkAssignableIndex validates an element write through a writable slice
// view (ADR-0096): `buf[i] = x` where buf is held as `&var []u8`. A plain
// `[]u8` binding is rejected even when `var`-bound: it does not guarantee a
// writable backing.
func (c *Checker) checkAssignableIndex(
	expr *ast.IndexExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if expr.Slice {
		return "", errorf("type error: invalid assignment target `%s`", expr.String())
	}
	ident, ok := expr.Target.(*ast.IdentExpr)
	if !ok {
		return "", errorf(
			"type error: indexed assignment target must be a local `&var []u8` binding")
	}
	typ, exists := env.lookup(ident.Name)
	if !exists {
		return "", errorf("type error: unknown local `%s`", ident.Name)
	}
	if !sameType(typ, typeByteString) || !env.isMutBorrowed(ident.Name) {
		return "", errorf(
			"type error: indexed assignment requires a writable slice view (`&var []u8`), `%s` is not one",
			ident.Name)
	}
	if err := c.checkIndexBound("index", expr.Index, env, unsafe); err != nil {
		return "", err
	}
	return typeU8, nil
}

// checkAssignableIdent validates direct binding assignment. A `&var T`
// parameter is the caller's storage, so assigning the binding stores there,
// under the same rule that lets a field write through it.
func checkAssignableIdent(expr *ast.IdentExpr, env *scope) (Type, error) {
	want, ok := env.lookup(expr.Name)
	if !ok {
		return "", errorAt(expr.Span, "type error: undefined variable `%s`", expr.Name)
	}
	if !env.isMutable(expr.Name) && !env.isMutBorrowedParam(expr.Name) {
		return "", errorf("type error: cannot assign to immutable binding `%s`", expr.Name)
	}
	return want, nil
}

// checkReturnStmt validates that return value type matches the function result.
func (c *Checker) checkReturnStmt(
	stmt *ast.ReturnStmt,
	env *scope,
	want Type,
	unsafe unsafeMark,
) (bool, error) {
	if stmt.Value == nil {
		if c.acceptsBareReturn(want) {
			return true, nil
		}
		return false, errorf("type error: return expects %s, got void", want)
	}
	if ident, ok := stmt.Value.(*ast.IdentExpr); ok && ident.Name == "void" {
		return false, errorf("type error: void is not a value; use `return;`")
	}
	if _, ok := stmt.Value.(*ast.NullExpr); ok {
		success := want
		if elem, isUnion := c.types.errorUnionElement(success); isUnion {
			success = elem
		}
		if _, isOptional := optionalElem(success); isOptional {
			return true, nil
		}
		return false, errorf("type error: return expects %s, got null", want)
	}
	saved := c.captureCondition
	if _, _, bare := typ.BorrowOptionalElem(string(want)); bare {
		// A declared borrow-optional return is the second consumer of `?&T`:
		// the returned borrow flows on to the caller's capture.
		c.captureCondition = true
	}
	got, err := c.checkExpr(stmt.Value, env, unsafe)
	c.captureCondition = saved
	if err != nil {
		return false, err
	}
	return c.checkReturnValue(stmt.Value, env, want, got)
}

// checkReturnValue validates a non-void return expression against the result type.
func (c *Checker) checkReturnValue(
	expr ast.Expression,
	env *scope,
	want Type,
	got Type,
) (bool, error) {
	if ok, err := c.checkErrorUnionReturn(expr, want, got); ok || err != nil {
		return ok, err
	}
	if _, ok := optionalElem(want); ok {
		// A plain value returned as `?T` wraps implicitly, like `!T` success.
		wrapped, err := c.wrapsIntoOptional(expr, want, got)
		if err != nil {
			return false, err
		}
		if wrapped {
			return true, nil
		}
		return false, errorf("type error: return expects %s, got %s", want, got)
	}
	if c.returnValueMatchesBorrowParam(expr, env, want, got) {
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
	return true, nil
}

// wrapsIntoOptional reports whether got fills a `?T` slot: the element wraps
// implicitly (like `!T` success), and an already-wrapped optional passes as
// is. Every implicit-wrap site asks here so the rule cannot fork.
func (c *Checker) wrapsIntoOptional(
	expr ast.Expression,
	want Type,
	got Type,
) (bool, error) {
	elem, ok := optionalElem(want)
	if !ok {
		return false, nil
	}
	if sameType(got, want) {
		return true, nil
	}
	coerced, err := c.coerceContextualIntegerLiteral(expr, elem, got)
	if err != nil {
		return false, err
	}
	return sameType(coerced, elem), nil
}

// checkErrorUnionReturn accepts success or error payloads for !T returns.
func (c *Checker) checkErrorUnionReturn(
	expr ast.Expression,
	want Type,
	got Type,
) (bool, error) {
	if elem, ok := c.types.errorUnionElement(want); ok {
		success := elem
		coerced, err := c.coerceContextualIntegerLiteral(expr, success, got)
		if err != nil {
			return false, err
		}
		if sameType(coerced, success) {
			return true, nil
		}
		if ok, err := c.wrapsIntoOptional(expr, success, got); ok || err != nil {
			return ok, err
		}
	}
	if c.types.absorbsErrorUnion(want, got) {
		return true, nil
	}
	if errorType, success, ok := c.types.errorUnionParts(want); ok {
		// `!T` declares no error set, so it accepts a member of any of them,
		// the same way it accepts a `try` from any set (ADR-0087).
		if errorType == "" && c.errorSets[string(got)] != nil {
			return true, nil
		}
		coerced, err := c.coerceContextualIntegerLiteral(expr, success, got)
		if err != nil {
			return false, err
		}
		// A member of a set whose values are a subset of E returns as a
		// failure the same way `try` propagates it (ADR-0127).
		if sameType(coerced, success) || c.errorSetFits(got, errorType) {
			return true, nil
		}
		if ok, err := c.wrapsIntoOptional(expr, success, got); ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}

// acceptsBareReturn reports whether return without a value satisfies a result type.
func (c *Checker) acceptsBareReturn(want Type) bool {
	if want == typeVoid {
		return true
	}
	if _, success, ok := c.types.errorUnionParts(want); ok && success == typeVoid {
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
	// Every borrow parameter is a presumed provenance source (ADR-0098), so
	// returning it as the declared borrow type is always in contract.
	return c.currentFunction != nil
}

// exprBorrowSources reports parameter names that can back a returned view.
func (c *Checker) exprBorrowSources(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	case *ast.UnsafeExpr:
		return c.exprBorrowSources(e.Value, env, unsafe)
	case *ast.MoveExpr:
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
// A binding may record several sources (a multi-source `borrows` result), so
// the walk resolves each recorded source to its root and unions the roots.
func (c *Checker) identBorrowSources(name string, env *scope) map[string]bool {
	roots := map[string]bool{}
	seen := map[string]bool{}
	work := []string{name}
	for len(work) > 0 {
		source := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[source] {
			continue
		}
		seen[source] = true
		binding, ok := env.binding(source)
		if !ok || len(binding.borrowSources) == 0 {
			roots[source] = true
			continue
		}
		for _, n := range binding.borrowSources {
			if n == source {
				// A name whose recorded provenance includes itself is a root.
				roots[source] = true
				continue
			}
			work = append(work, n)
		}
	}
	return roots
}

// callBorrowSources maps returned borrow provenance back to call arguments.
func (c *Checker) callBorrowSources(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeMark,
) (map[string]bool, error) {
	if sources, ok, err := c.methodBorrowSources(expr, env, unsafe); ok || err != nil {
		return sources, err
	}
	fn := c.calledFunction(expr.Callee)
	if fn == nil {
		return map[string]bool{}, nil
	}
	return c.structuralReturnSources(fn, env, unsafe,
		func(idx int) ast.Expression {
			if idx >= len(expr.Args) {
				return nil
			}
			return expr.Args[idx]
		})
}

// structuralReturnSources unions the provenance of every tie-capable argument
// of a call whose return type can carry a view. The contract is derived from
// the signature alone (ADR-0098): a view-capable return is conservatively tied
// to every view or borrow argument. argAt maps a parameter index to its call
// expression; nil slots are skipped.
func (c *Checker) structuralReturnSources(
	fn *functionType,
	env *scope,
	unsafe unsafeMark,
	argAt func(idx int) ast.Expression,
) (map[string]bool, error) {
	if !c.types.isBorrowedViewReturnType(fn.returnType) {
		return map[string]bool{}, nil
	}
	union := map[string]bool{}
	for idx := range fn.params {
		if !c.tieCapableParam(fn.params[idx], fn.borrowParams[idx]) {
			continue
		}
		arg := argAt(idx)
		if arg == nil {
			continue
		}
		part, err := c.exprBorrowSources(arg, env, unsafe)
		if err != nil {
			return map[string]bool{}, err
		}
		for candidate := range part {
			union[candidate] = true
		}
	}
	return union, nil
}

// tieCapableParam reports whether an argument in this slot can back a
// returned view: an explicit borrow, or a view-typed parameter.
func (c *Checker) tieCapableParam(param Type, borrowed bool) bool {
	return borrowed || c.types.isBorrowedViewReturnType(param)
}

// methodBorrowSources handles built-in method-style view returns.
func (c *Checker) methodBorrowSources(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeMark,
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
		// A method's params start with self, so index 0 is the receiver
		// expression and the rest offset into the call arguments.
		union, err := c.structuralReturnSources(method, env, unsafe,
			func(idx int) ast.Expression {
				if idx == 0 {
					return field.Receiver
				}
				if idx-1 >= len(expr.Args) {
					return nil
				}
				return expr.Args[idx-1]
			})
		return union, true, err
	}
	switch field.Name {
	case "as_bytes", "as_mut_bytes", "borrow", "borrow_mut", "at", "at_mut":
		sources, err := c.exprBorrowSources(field.Receiver, env, unsafe)
		return sources, true, err
	default:
		return nil, false, nil
	}
}

// exprBorrowSourceList returns the provenance names backing expr. Static
// backing is not a provenance to keep alive and is dropped. Order is not
// significant: the only consumer unions the names back into a set.
func (c *Checker) exprBorrowSourceList(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
) []string {
	sources, err := c.exprBorrowSources(expr, env, unsafe)
	if err != nil {
		return nil
	}
	list := make([]string, 0, len(sources))
	for candidate := range sources {
		if candidate == "$static" {
			continue
		}
		list = append(list, candidate)
	}
	return list
}

// fieldBorrowSources preserves the receiver provenance through direct fields.
func (c *Checker) fieldBorrowSources(
	expr *ast.FieldExpr,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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

// fieldDeclaredType returns the full field type, including borrow prefixes.
func fieldDeclaredType(field ast.Field) Type {
	return Type(borrowWrappedType(field.Borrow, field.MutBorrow, typ.Text(field.TypeName)))
}

// declaredFieldType returns the declared type of a struct field by name, or
// "" when the struct declares no such field.
func declaredFieldType(decl *ast.StructDecl, name string) Type {
	for _, field := range decl.Fields {
		if field.Name == name {
			return fieldDeclaredType(field)
		}
	}
	return ""
}

// structLiteralFieldMatches reports whether a written value fits the declared
// field type. A plain value written where the field wants `?T` wraps
// implicitly, like a `?T` return.
func (c *Checker) structLiteralFieldMatches(
	expr ast.Expression,
	env *scope,
	want Type,
	got Type,
) (bool, error) {
	if sameType(got, want) {
		return true, nil
	}
	if _, isOptional := optionalElem(want); isOptional {
		wrapped, err := c.wrapsIntoOptional(expr, want, got)
		if err != nil || wrapped {
			return wrapped, err
		}
	}
	return c.returnValueMatchesBorrowParam(expr, env, want, got), nil
}

// structLiteralFieldValue types one written field value. `null` has no type
// of its own; the declared optional field is its context, the same way a
// `?T` return is.
func (c *Checker) structLiteralFieldValue(
	decl *ast.StructDecl,
	typeName string,
	field ast.FieldValue,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if _, isNull := field.Value.(*ast.NullExpr); !isNull {
		return c.checkExpr(field.Value, env, unsafe)
	}
	want := declaredFieldType(decl, field.Name)
	if want == "" {
		return "", errorf("type error: unknown field `%s.%s`", typeName, field.Name)
	}
	var err error
	want, err = c.resolveInstanceType(want)
	if err != nil {
		return "", err
	}
	if _, isOptional := optionalElem(want); !isOptional {
		return "", errorf("type error: field `%s.%s` expects %s, got null",
			typeName, field.Name, want)
	}
	return want, nil
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
	unsafe unsafeMark,
) (bool, error) {
	consequence := env.child()
	alternative := env.child()
	handled, err := c.bindContainerBorrowCapture(
		stmt.Capture, stmt.Condition, consequence, env, unsafe)
	if err != nil {
		return false, err
	}
	if handled && stmt.ErrCapture != "" {
		return false, errorf(
			"type error: `else |%s|` requires an error union condition", stmt.ErrCapture)
	}
	if !handled {
		cond, err := c.checkExpr(stmt.Condition, env, unsafe)
		if err != nil {
			return false, err
		}
		if _, _, ok := c.types.errorUnionParts(cond); ok {
			if err := c.bindErrorUnionCaptures(stmt, cond, consequence, alternative); err != nil {
				return false, err
			}
		} else {
			if stmt.ErrCapture != "" {
				return false, errorf(
					"type error: `else |%s|` requires an error union condition, got %s",
					stmt.ErrCapture, cond)
			}
			if err := c.bindConditionCapture("if", stmt.Capture, cond, consequence); err != nil {
				return false, err
			}
		}
	}
	leftReturns, err := c.checkBlock(stmt.Consequence, consequence, wantReturn, unsafe)
	if err != nil {
		return false, err
	}
	if stmt.Alternative == nil {
		return false, nil
	}
	rightReturns, err := c.checkBlock(stmt.Alternative, alternative, wantReturn, unsafe)
	if err != nil {
		return false, err
	}
	return leftReturns && rightReturns, nil
}

// bindErrorUnionCaptures types an error union if condition: the success
// payload binds into the consequence, and `else |err|` binds the error member
// into the alternative (SPEC §11.1). `else |err|` is required because leaving
// it off would drop the failure silently.
func (c *Checker) bindErrorUnionCaptures(
	stmt *ast.IfStmt,
	cond Type,
	consequence *scope,
	alternative *scope,
) error {
	errorType, success, _ := c.types.errorUnionParts(cond)
	if errorType == "" {
		return errorf(
			"type error: if cannot open %s; a `!T` without a declared error set"+
				" propagates with `try`", cond)
	}
	if stmt.ErrCapture == "" {
		return errorf("type error: if on %s requires `else |err|`", cond)
	}
	if success == typeVoid {
		if stmt.Capture != "" {
			return errorf(
				"type error: %s has no success payload to capture `|%s|`",
				cond, stmt.Capture)
		}
	} else if stmt.Capture == "" {
		return errorf("type error: if on %s requires a success capture `|name|`", cond)
	} else if err := requireScopeDefinition(
		stmt.Capture, consequence.define(stmt.Capture, success, false)); err != nil {
		return err
	}
	return requireScopeDefinition(
		stmt.ErrCapture, alternative.define(stmt.ErrCapture, errorType, false))
}

// bindConditionCapture types an if/while condition: with a capture the
// condition must be an optional and the payload binds into the branch scope;
// without one it must be bool.
func (c *Checker) bindConditionCapture(
	kind string,
	capture string,
	cond Type,
	scope *scope,
) error {
	if capture == "" {
		if cond != typeBool {
			return errorf("type error: %s condition must be bool, got %s", kind, cond)
		}
		return nil
	}
	elem, ok := optionalElem(cond)
	if !ok {
		return errorf(
			"type error: %s capture `|%s|` requires an optional condition, got %s",
			kind, capture, cond)
	}
	return requireScopeDefinition(capture, scope.define(capture, elem, false))
}

// bindContainerBorrowCapture handles an array/map at/at_mut capture condition:
// the capture binds the payload borrow directly, carrying the container's
// provenance the way `let view = try array.at(...)` used to.
func (c *Checker) bindContainerBorrowCapture(
	capture string,
	cond ast.Expression,
	branch *scope,
	env *scope,
	unsafe unsafeMark,
) (bool, error) {
	if capture == "" {
		return false, nil
	}
	elem, mutable, ok, err := c.checkContainerBorrowCondition(cond, env, unsafe)
	if !ok || err != nil {
		return ok, err
	}
	sources := c.exprBorrowSourceList(cond, env, unsafe)
	if len(sources) == 0 {
		sources = []string{capture}
	}
	return true, requireScopeDefinition(
		capture, branch.defineParamWithSource(capture, elem, true, mutable, sources, false))
}

// checkWhileStmt validates loop condition and body types.
func (c *Checker) checkWhileStmt(
	stmt *ast.WhileStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	body := env.child()
	handled, err := c.bindContainerBorrowCapture(stmt.Capture, stmt.Condition, body, env, unsafe)
	if err != nil {
		return false, err
	}
	if !handled {
		cond, err := c.checkExpr(stmt.Condition, env, unsafe)
		if err != nil {
			return false, err
		}
		if err := c.bindConditionCapture("while", stmt.Capture, cond, body); err != nil {
			return false, err
		}
	}
	leave, err := c.enterLoop(stmt.Label)
	if err != nil {
		return false, err
	}
	defer leave()
	_, err = c.checkBlock(stmt.Body, body, wantReturn, unsafe)
	return false, err
}

// checkForStmt validates a bounded integer range loop.
func (c *Checker) checkForStmt(
	stmt *ast.ForStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
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
	if err := requireScopeDefinition(stmt.Name, child.define(stmt.Name, typeI64, false)); err != nil {
		return false, err
	}
	_, err = c.checkBlock(stmt.Body, child, wantReturn, unsafe)
	return false, err
}

// checkForBounds validates the start and end expressions for a range loop.
func (c *Checker) checkForBounds(stmt *ast.ForStmt, env *scope, unsafe unsafeMark) error {
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

// taggedType returns the named set of tags a match can run over. An enum is one,
// and so is an error set: matching a failure asks which member of the set it is.
func (c *Checker) taggedType(name Type) *enumType {
	if enum := c.enums[string(name)]; enum != nil {
		return enum
	}
	if set := c.errorSets[string(name)]; set != nil {
		return set.tagged
	}
	return nil
}

// checkMatchStmt validates exhaustive simple enum tag matches.
func (c *Checker) checkMatchStmt(
	stmt *ast.MatchStmt,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	valueType, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return false, err
	}
	valueType = borrowedValueType(valueType)
	if set := c.errorSets[string(valueType)]; set != nil {
		return c.checkMatchArms(stmt, nil, nil, set, env, wantReturn, unsafe)
	}
	if tagged := c.taggedType(valueType); tagged != nil {
		return c.checkMatchArms(stmt, tagged, nil, nil, env, wantReturn, unsafe)
	}
	unionType := c.unions[string(valueType)]
	if unionType != nil {
		return c.checkMatchArms(stmt, nil, unionType, nil, env, wantReturn, unsafe)
	}
	return false, errorf("type error: match expects enum or union, got %s", valueType)
}

// checkMatchArms validates tag patterns and return flow for match arms.
func (c *Checker) checkMatchArms(
	stmt *ast.MatchStmt,
	enumType *enumType,
	unionType *unionType,
	errorSet *errorSetType,
	env *scope,
	wantReturn Type,
	unsafe unsafeMark,
) (bool, error) {
	arms := stmt.Arms
	armVariants, err := c.matchArmVariants(stmt)
	if err != nil {
		return false, err
	}
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
			got, err := c.recordMatchArm(arm, enumType, unionType, errorSet, seen)
			if err != nil {
				return false, err
			}
			payload = got
		}
		armEnv := env.child()
		if payload != "" && arm.Binding != "" {
			if err := requireScopeDefinition(
				arm.Binding, armEnv.define(arm.Binding, Type(payload), false)); err != nil {
				return false, err
			}
		}
		restore := c.bindMetaField(stmt.MetaCapture, armVariants[arm.Tag])
		returns, err := c.checkStmt(arm.Body, armEnv, wantReturn, unsafe)
		restore()
		if err != nil {
			return false, err
		}
		allReturn = allReturn && returns
	}
	if !wildcard {
		if err := matchCoverageError(enumType, unionType, errorSet, seen); err != nil {
			return false, err
		}
	}
	return allReturn, nil
}

// recordMatchArm validates one non-wildcard arm, records what it covers, and
// returns the union payload type it binds. An error arm is counted under the
// member value it resolves to, so two spellings of one member collide and two
// members that share a spelling do not (ADR-0127).
func (c *Checker) recordMatchArm(
	arm ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	errorSet *errorSetType,
	seen map[string]bool,
) (string, error) {
	key := arm.Tag
	payload := ""
	if errorSet != nil {
		resolved, err := c.errorMatchArmValue(errorSet, arm)
		if err != nil {
			return "", err
		}
		key = resolved
	} else {
		got, err := matchPayloadType(enumType, unionType, arm)
		if err != nil {
			return "", err
		}
		payload = got
	}
	if seen[key] {
		return "", errorf("type error: duplicate match tag `%s::%s`",
			matchOwnerName(enumType, unionType, errorSet), arm.Tag)
	}
	seen[key] = true
	return payload, nil
}

// matchOwnerName names the type a match runs over, for diagnostics.
func matchOwnerName(enumType *enumType, unionType *unionType, errorSet *errorSetType) string {
	if errorSet != nil {
		return errorSet.name
	}
	return matchTypeName(enumType, unionType)
}

// matchCoverageError reports the variants a match without a wildcard misses.
func matchCoverageError(
	enumType *enumType,
	unionType *unionType,
	errorSet *errorSetType,
	seen map[string]bool,
) error {
	if errorSet != nil {
		if len(seen) == len(errorSet.values) {
			return nil
		}
		return errorf("type error: match on `%s` is not exhaustive: missing %s",
			errorSet.name,
			strings.Join(missingErrorMatchValues(errorSet, seen), ", "))
	}
	if len(seen) == matchVariantCount(enumType, unionType) {
		return nil
	}
	return errorf("type error: match on `%s` is not exhaustive: missing %s",
		matchTypeName(enumType, unionType),
		strings.Join(missingMatchVariants(enumType, unionType, seen), ", "))
}

// errorMatchArmValue resolves one error match arm to the member value it
// covers. A bare arm must name exactly one declaring set; a qualified arm
// names it explicitly, which a combined set needs when two of its sets
// declare the same member name (SPEC §11.2).
func (c *Checker) errorMatchArmValue(set *errorSetType, arm ast.MatchArm) (string, error) {
	if arm.Binding != "" {
		return "", errorf("type error: error `%s::%s` has no payload", set.name, arm.Tag)
	}
	if arm.TagSet != "" {
		origin := c.errorSets[arm.TagSet]
		if origin == nil {
			return "", errorf(
				"type error: match arm qualifier `%s` is not a declared error set",
				arm.TagSet)
		}
		if origin.combines != nil {
			return "", errorf(
				"type error: `%s` is a combined set; qualify `%s` with the set"+
					" that declares it", arm.TagSet, arm.Tag)
		}
		key := errorValueKey(arm.TagSet, arm.Tag)
		if !origin.members[arm.Tag] || !set.values[key] {
			return "", errorf("type error: `%s::%s` is not a member of `%s`",
				arm.TagSet, arm.Tag, set.name)
		}
		return key, nil
	}
	origins := set.byName[arm.Tag]
	if len(origins) == 0 {
		return "", errorf("type error: unknown match tag `%s::%s`", set.name, arm.Tag)
	}
	if len(origins) > 1 {
		quals := make([]string, 0, len(origins))
		for _, origin := range origins {
			quals = append(quals, "`"+errorValueKey(origin, arm.Tag)+"`")
		}
		return "", errorf(
			"type error: `%s` reaches `%s` from more than one set; write %s",
			arm.Tag, set.name, strings.Join(quals, " or "))
	}
	return errorValueKey(origins[0], arm.Tag), nil
}

// missingErrorMatchValues lists the member values a match does not cover,
// sorted so the message is stable. A member whose bare name is unambiguous is
// listed bare; one that reaches the set from more than one declaring set is
// listed qualified, the way its arm has to be written.
func missingErrorMatchValues(set *errorSetType, seen map[string]bool) []string {
	var missing []string
	for _, key := range set.valueOrder {
		if seen[key] {
			continue
		}
		_, bare := splitErrorValueKey(key)
		if len(set.byName[bare]) > 1 {
			missing = append(missing, key)
			continue
		}
		missing = append(missing, bare)
	}
	sort.Strings(missing)
	return missing
}

// matchArmVariants indexes by tag the variants a `comptime match` arm body is
// written against. A match written by hand carries no capture and gets nil,
// which binds nothing.
func (c *Checker) matchArmVariants(stmt *ast.MatchStmt) (map[string]metaField, error) {
	if stmt.MetaCapture == "" {
		return nil, nil
	}
	variants, err := c.variants(stmt.MetaOwner)
	if err != nil {
		return nil, err
	}
	out := make(map[string]metaField, len(variants))
	for _, variant := range variants {
		out[variant.name] = variant
	}
	return out, nil
}

// bindMetaField binds one capture for the length of one expansion, returning
// the call that unbinds it. An empty name binds nothing, so a caller that has
// no capture in hand needs no branch of its own.
func (c *Checker) bindMetaField(name string, field metaField) func() {
	if name == "" {
		return func() {}
	}
	previous, had := c.metaFields[name]
	c.metaFields[name] = field
	return func() {
		if had {
			c.metaFields[name] = previous
			return
		}
		delete(c.metaFields, name)
	}
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
	if arm.TagSet != "" {
		return "", errorf("type error: qualified match arm `%s::%s` requires an error set value",
			arm.TagSet, arm.Tag)
	}
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
	return string(payload), nil
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
func (c *Checker) checkExpr(expr ast.Expression, env *scope, unsafe unsafeMark) (Type, error) {
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.TypeExpr, *ast.NullExpr:
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
	case *ast.UnsafeExpr:
		return c.checkUnsafeExpr(e, env, unsafe)
	case *ast.MoveExpr:
		// The marker says the obligation leaves here; the value and its type
		// are the ones it covers. Whether a move actually happens is an
		// ownership question, checked there.
		return c.checkExpr(e.Value, env, unsafe)
	case *ast.IndexExpr, *ast.FieldExpr, *ast.DerefExpr:
		return c.checkAccessExpr(e, env, unsafe)
	case *ast.StructLiteralExpr:
		return c.checkStructLiteralExpr(e, env, unsafe)
	case *ast.BufferLiteralExpr:
		return Type(e.TypeText()), nil
	default:
		return c.checkControlExpr(expr, env, unsafe)
	}
}

// checkAccessExpr types the forms that reach into a value: an element, a field,
// and a dereference.
func (c *Checker) checkAccessExpr(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return c.checkIndexExpr(e, env, unsafe)
	case *ast.FieldExpr:
		return c.checkFieldExpr(e, env, unsafe)
	default:
		return c.checkDerefExpr(expr.(*ast.DerefExpr), env, unsafe)
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
	unsafe unsafeMark,
) (Type, error) {
	switch e := expr.(type) {
	case *ast.IfStmt:
		return c.checkIfExpr(e, env, unsafe)
	case *ast.MatchStmt:
		return c.checkMatchExpr(e, env, unsafe)
	case *ast.OrelseGuardExpr:
		return c.checkOrelseGuardExpr(e, env, unsafe)
	case *ast.CatchGuardExpr:
		return c.checkCatchGuardExpr(e, env, unsafe)
	default:
		return "", errorf("type error: unsupported expression %T", expr)
	}
}

// checkOrelseGuardExpr types `cond orelse return/break/continue`: on null the
// guard leaves the enclosing function or loop, so the guard itself always
// yields the payload.
func (c *Checker) checkOrelseGuardExpr(
	expr *ast.OrelseGuardExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	cond, err := c.checkExpr(expr.Cond, env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := optionalElem(cond)
	if !ok {
		return "", errorf("type error: `orelse` expects an optional left operand, got %s", cond)
	}
	switch exit := expr.Exit.(type) {
	case *ast.ReturnStmt:
		if _, err := c.checkReturnStmt(exit, env, c.currentReturn, unsafe); err != nil {
			return "", err
		}
	case *ast.BreakStmt:
		if err := c.checkLoopBranch("break", exit.Label); err != nil {
			return "", err
		}
	case *ast.ContinueStmt:
		if err := c.checkLoopBranch("continue", exit.Label); err != nil {
			return "", err
		}
	default:
		return "", errorf("type error: `orelse` guard must exit with return, break, or continue")
	}
	return elem, nil
}

// checkIfExpr validates an if expression and returns the common branch type.
func (c *Checker) checkIfExpr(stmt *ast.IfStmt, env *scope, unsafe unsafeMark) (Type, error) {
	if stmt.Capture != "" {
		return "", errorf(
			"type error: if capture is a statement form; use `orelse` in expressions")
	}
	if stmt.ErrCapture != "" {
		return "", errorf(
			"type error: `else |%s|` is a statement form; use `catch` in expressions",
			stmt.ErrCapture)
	}
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
	unsafe unsafeMark,
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
func (c *Checker) checkStmtValue(stmt ast.Statement, env *scope, unsafe unsafeMark) (Type, error) {
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
	case *ast.ReturnStmt:
		// ADR-0093 allows `return` arms only in statement matches.
		return "", errorf("type error: expression match arm cannot `return`")
	case *ast.BlockStmt:
		return c.checkBlockValue(s, env.child(), unsafe)
	default:
		return "", errorf("type error: expression block must end with a value")
	}
}

// checkMatchExpr validates an exhaustive match expression and its arm result type.
func (c *Checker) checkMatchExpr(stmt *ast.MatchStmt, env *scope, unsafe unsafeMark) (Type, error) {
	valueType, err := c.checkExpr(stmt.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	valueType = borrowedValueType(valueType)
	if set := c.errorSets[string(valueType)]; set != nil {
		return c.checkMatchExprArms(stmt.Arms, nil, nil, set, env, unsafe)
	}
	tagged := c.taggedType(valueType)
	unionType := c.unions[string(valueType)]
	if tagged == nil && unionType == nil {
		return "", errorf("type error: match expects enum or union, got %s", valueType)
	}
	return c.checkMatchExprArms(stmt.Arms, tagged, unionType, nil, env, unsafe)
}

// checkMatchExprArms validates match expression arms and returns their common type.
func (c *Checker) checkMatchExprArms(
	arms []ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	errorSet *errorSetType,
	env *scope,
	unsafe unsafeMark,
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
			got, err = c.checkRecordedMatchExprArm(arm, enumType, unionType, errorSet, seen, env, unsafe)
			if err != nil {
				return "", err
			}
		}
		if idx == 0 {
			result = got
		} else if got != result {
			return "", errorf("type error: match arm types differ: %s vs %s", result, got)
		}
	}
	if !wildcard {
		if err := matchCoverageError(enumType, unionType, errorSet, seen); err != nil {
			return "", err
		}
	}
	return result, nil
}

// checkRecordedMatchExprArm validates one non-wildcard match expression arm,
// records what it covers, and returns its value type.
func (c *Checker) checkRecordedMatchExprArm(
	arm ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	errorSet *errorSetType,
	seen map[string]bool,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if errorSet != nil {
		key, err := c.errorMatchArmValue(errorSet, arm)
		if err != nil {
			return "", err
		}
		if seen[key] {
			return "", errorf("type error: duplicate match tag `%s::%s`",
				errorSet.name, arm.Tag)
		}
		seen[key] = true
		return c.checkStmtValue(arm.Body, env.child(), unsafe)
	}
	got, err := c.checkMatchExprArm(arm, enumType, unionType, env, unsafe)
	if err != nil {
		return "", err
	}
	if seen[arm.Tag] {
		return "", errorf("type error: duplicate match tag `%s::%s`",
			matchTypeName(enumType, unionType), arm.Tag)
	}
	seen[arm.Tag] = true
	return got, nil
}

// checkMatchExprArm validates one match expression arm and returns its value type.
func (c *Checker) checkMatchExprArm(
	arm ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
	env *scope,
	unsafe unsafeMark,
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
		if err := requireScopeDefinition(
			arm.Binding, armEnv.define(arm.Binding, Type(payload), false)); err != nil {
			return "", err
		}
	}
	return c.checkStmtValue(arm.Body, armEnv, unsafe)
}

// checkIndexExpr validates checked one-dimensional byte indexing and slicing.
func (c *Checker) checkIndexExpr(expr *ast.IndexExpr, env *scope, unsafe unsafeMark) (Type, error) {
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
	unsafe unsafeMark,
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
	case *ast.NullExpr:
		// A bare `null` reached a position with no `?T` to give it a type;
		// contextual positions accept it before ever asking here.
		return "", errorf(
			"type error: `null` needs an optional context (a `?T` return or argument)")
	default:
		return "", errorf("type error: unsupported literal %T", expr)
	}
}

// checkOrelseExpr types `opt orelse default`: the left side must be an
// optional and the result is its element, with the default checked at the
// element type.
func (c *Checker) checkOrelseExpr(
	expr *ast.BinaryExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	left, err := c.checkExpr(expr.Left, env, unsafe)
	if err != nil {
		return "", err
	}
	elem, ok := optionalElem(left)
	if !ok {
		return "", errorf("type error: `orelse` expects an optional left operand, got %s", left)
	}
	right, err := c.checkContextualExpr(expr.Right, elem, env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(right, elem) {
		return "", errorf("type error: `orelse` default must be %s, got %s", elem, right)
	}
	return elem, nil
}

// checkCatchExpr types `union catch default`: the left side must be an error
// union with a declared set and the result is its success type, with the
// default checked at that type. The error member is not bound here; touching
// it is the statement form's job (SPEC §11.1).
func (c *Checker) checkCatchExpr(
	expr *ast.BinaryExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	left, err := c.checkExpr(expr.Left, env, unsafe)
	if err != nil {
		return "", err
	}
	elem, err := c.catchSuccessType(left)
	if err != nil {
		return "", err
	}
	right, err := c.checkContextualExpr(expr.Right, elem, env, unsafe)
	if err != nil {
		return "", err
	}
	if !sameType(right, elem) {
		return "", errorf("type error: `catch` default must be %s, got %s", elem, right)
	}
	return elem, nil
}

// checkCatchGuardExpr types `union catch return/break/continue`: on failure
// the guard leaves the enclosing function or loop, so the guard itself always
// yields the success payload.
func (c *Checker) checkCatchGuardExpr(
	expr *ast.CatchGuardExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	cond, err := c.checkExpr(expr.Cond, env, unsafe)
	if err != nil {
		return "", err
	}
	elem, err := c.catchSuccessType(cond)
	if err != nil {
		return "", err
	}
	switch exit := expr.Exit.(type) {
	case *ast.ReturnStmt:
		if _, err := c.checkReturnStmt(exit, env, c.currentReturn, unsafe); err != nil {
			return "", err
		}
	case *ast.BreakStmt:
		if err := c.checkLoopBranch("break", exit.Label); err != nil {
			return "", err
		}
	case *ast.ContinueStmt:
		if err := c.checkLoopBranch("continue", exit.Label); err != nil {
			return "", err
		}
	default:
		return "", errorf("type error: `catch` guard must exit with return, break, or continue")
	}
	return elem, nil
}

// catchSuccessType returns T for the `E!T` a catch or error capture handles.
// `!T` declares no set, so its failures cannot be enumerated and it stays
// propagation-only (SPEC §11.1).
func (c *Checker) catchSuccessType(union Type) (Type, error) {
	errorType, success, ok := c.types.errorUnionParts(union)
	if !ok {
		return "", errorf("type error: `catch` expects an error union left operand, got %s", union)
	}
	if errorType == "" {
		return "", errorf(
			"type error: `catch` requires a declared error set; `!%s` propagates with `try`",
			success)
	}
	return success, nil
}

// errorSetFits reports whether every member value of the source set is a
// member of the target set. Members keep their per-set identity through a
// union (ADR-0127), so the check runs over declaring-set values, not names.
func (c *Checker) errorSetFits(source Type, target Type) bool {
	if sameType(source, target) {
		return true
	}
	src := c.errorSets[string(source)]
	dst := c.errorSets[string(target)]
	if src == nil || dst == nil {
		return false
	}
	for key := range src.values {
		if !dst.values[key] {
			return false
		}
	}
	return true
}

// checkContextualExpr validates an expression and narrows integer literals to want.
func (c *Checker) checkContextualExpr(
	expr ast.Expression,
	want Type,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if _, ok := expr.(*ast.NullExpr); ok {
		if _, isOptional := optionalElem(want); isOptional {
			return want, nil
		}
		return "", errorf("type error: `null` needs an optional context, expected %s", want)
	}
	got, err := c.checkExpr(expr, env, unsafe)
	if err != nil {
		return "", err
	}
	if _, ok := optionalElem(want); ok {
		// A plain value in an optional context wraps implicitly, the same way a
		// success value wraps into `!T`.
		wrapped, err := c.wrapsIntoOptional(expr, want, got)
		if err != nil {
			return "", err
		}
		if wrapped {
			return want, nil
		}
		return "", errorf("type error: expected %s, got %s", want, got)
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) (Type, bool, error) {
	mutable := expr.Operator == "&var"
	if err := checkBorrowTargetShape(expr.Right); err != nil {
		return "", false, err
	}
	typ, err := c.checkExpr(expr.Right, env, unsafe)
	if err != nil {
		return "", false, err
	}
	if c.types.isBufferType(typ) {
		return "", false, errorf(
			"type error: cannot borrow a stack buffer; use `as_bytes()` / `as_mut_bytes()`")
	}
	if mutable {
		if err := requireMutableBorrowArg(expr.Right, typ, env); err != nil {
			return "", false, err
		}
	}
	return typ, mutable, nil
}

// checkBinaryExpr validates arithmetic, logical, equality, and comparison operators.
func (c *Checker) checkBinaryExpr(
	expr *ast.BinaryExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if expr.Operator == "orelse" {
		return c.checkOrelseExpr(expr, env, unsafe)
	}
	if expr.Operator == "catch" {
		return c.checkCatchExpr(expr, env, unsafe)
	}
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
	case *ast.StructLiteralExpr:
		return e.Span
	default:
		return ast.Span{}
	}
}

// isComparison reports whether op returns bool for numeric operands.
func isComparison(op string) bool {
	return op == "<" || op == "<=" || op == ">" || op == ">="
}

// checkCastExpr validates explicit low-level casts.
func (c *Checker) checkCastExpr(expr *ast.CastExpr, env *scope, unsafe unsafeMark) (Type, error) {
	target, err := c.parseTypeNode(expr.TargetType)
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

// checkUnsafeExpr checks the expression an `unsafe` marker covers. The marker
// carries no type of its own; it says the author owns the obligation for every
// unproven operation inside.
//
// The enclosing marker is deliberately dropped: a use inside this marker belongs
// to this one, which is what makes a redundant outer marker reportable.
func (c *Checker) checkUnsafeExpr(
	expr *ast.UnsafeExpr,
	env *scope,
	_ unsafeMark,
) (Type, error) {
	return c.underMark(expr, func(inner unsafeMark) (Type, error) {
		return c.checkExpr(expr.Value, env, inner)
	})
}

// checkTryExpr validates error-union propagation and returns the success type.
func (c *Checker) checkTryExpr(expr *ast.TryExpr, env *scope, unsafe unsafeMark) (Type, error) {
	if _, _, ok := c.types.errorUnionParts(c.currentReturn); !ok {
		return "", errorf("type error: try requires function to return !T")
	}
	source, err := c.checkExpr(expr.Value, env, unsafe)
	if err != nil {
		return "", err
	}
	sourceError, success, ok := c.types.errorUnionParts(source)
	if !ok {
		return "", errorf("type error: try expects !T, got %s", source)
	}
	targetError, _, _ := c.types.errorUnionParts(c.currentReturn)
	// `!T` declares no error set, so it propagates whatever the body fails
	// with, which is what lets a function call things that fail in different
	// ways without naming every one. A declared `E!T` accepts the sets whose
	// member values are a subset of E: E itself and the sets it combines
	// (ADR-0127).
	if targetError != "" && !c.errorSetFits(sourceError, targetError) {
		return "", errorf("type error: try cannot propagate %s from %s", sourceError, source)
	}
	return success, nil
}

// checkCallExpr validates builtin and user function calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope, unsafe unsafeMark) (Type, error) {
	result, err := c.checkCallExprDispatch(expr, env, unsafe)
	if err != nil {
		return "", err
	}
	// One gate for every call form: a borrow-optional result exists only
	// inside a capture condition or a declared borrow-optional return, so a
	// call producing one anywhere else stops here.
	if _, _, bare := typ.BorrowOptionalElem(string(result)); bare && !c.captureCondition {
		return "", errorf("type error: a call returning %s must be consumed by a capture"+
			" (`if call |name|` or `while call |name|`)", result)
	}
	return result, nil
}

// checkCallExprDispatch routes one call expression to its checker.
func (c *Checker) checkCallExprDispatch(
	expr *ast.CallExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
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
	if name.Name == "Io" {
		return "", errorf("type error: use `std::io::blocking()`")
	}
	return c.checkUserCall(name.Name, name.Span, expr.Args, env, unsafe)
}

// checkFieldCallExpr validates qualified, union, and method calls.
func (c *Checker) checkFieldCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) (Type, bool, error) {
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
	typ, err := c.checkUserCall(name, expressionSpan(field), args, env, unsafe)
	return typ, true, err
}

// checkQualifiedBuiltin validates std:: namespace prototype calls.
func (c *Checker) checkQualifiedBuiltin(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	if err := c.rejectUnknownBuiltin(name); err != nil {
		return "", true, err
	}
	if typ, ok, err := c.checkStdCoreBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	return c.checkStdConstructorBuiltin(name, args)
}

// rejectUnknownBuiltin refuses a primitive the Go implementation does not have.
// What keeps the namespace away from a program outside std is where the module
// sits, so nothing here has to ask who is calling; the registry is what
// primitives there are, and a name outside it is a misspelling std source needs
// told about, because one that does not exist would lower to nothing.
func (c *Checker) rejectUnknownBuiltin(name string) error {
	if !strings.HasPrefix(name, "std::internal::builtin::") || stdprim.Primitive(name) {
		return nil
	}
	return errorf("type error: `%s` is not a primitive", name)
}

// checkStdCoreBuiltin validates pure, filesystem, I/O, and process std calls.
func (c *Checker) checkStdCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	if typ, ok, err := c.checkFsBuiltin(name, args, env, unsafe); ok || err != nil {
		return typ, ok, err
	}
	return c.checkSimpleCoreBuiltin(name, args, env, unsafe)
}

// checkStdConstructorBuiltin validates miscellaneous std constructor calls.
func (c *Checker) checkStdConstructorBuiltin(
	name string,
	args []ast.Expression,
) (Type, bool, error) {
	switch name {
	case "std::internal::builtin::io_blocking", "std::internal::builtin::io_failing":
		typ, err := checkNoArgConstructor(name, args, "Io")
		return typ, true, err
	case "std::io::evented", "std::internal::builtin::io_evented":
		return "", true, errorf("type error: `std::io::evented` is not implemented")
	default:
		return "", false, nil
	}
}

// checkSimpleCoreBuiltin validates declarative core primitive signatures.
func (c *Checker) checkSimpleCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) error {
	if want == stdprim.ArgIo {
		return c.checkIoArg(arg, env, unsafe, name)
	}
	if want == stdprim.ArgStringOut {
		return c.checkStringOutArg(name, arg, env, unsafe)
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

// checkFsBuiltin validates filesystem host primitives with explicit Io.
func (c *Checker) checkFsBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	switch name {
	case "std::internal::builtin::fs_write_file":
		return c.checkFsWriteFile(args, env, unsafe)
	case "std::internal::builtin::fs_exists":
		return c.checkFsExists(args, env, unsafe)
	case "std::internal::builtin::fs_metadata":
		return c.checkFsMetadata(args, env, unsafe)
	case "std::internal::builtin::fs_read_dir":
		return c.checkFsReadDir(args, env, unsafe)
	case "std::internal::builtin::fs_create_dir",
		"std::internal::builtin::fs_remove_dir",
		"std::internal::builtin::fs_remove_file":
		return c.checkFsPathOnly(name, args, env, unsafe, "std::fs::Error!void")
	case "std::internal::builtin::fs_rename":
		return c.checkFsRename(args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkStringOutArg validates a &var std::string::String destination argument:
// either explicit `&var local` syntax or an already-borrowed &var param.
func (c *Checker) checkStringOutArg(
	label string,
	arg ast.Expression,
	env *scope,
	unsafe unsafeMark,
) error {
	const stringType = Type("std::string::String")
	if prefix, ok := borrowPrefix(arg); ok && prefix.Operator == "&var" {
		typ, _, err := c.checkBorrowPrefix(prefix, env, unsafe)
		if err != nil {
			return err
		}
		if sameType(typ, stringType) {
			return nil
		}
		return errorf("type error: `%s` expects &var std::string::String out, got &var %s",
			label, typ)
	}
	typ, err := c.checkExpr(arg, env, unsafe)
	if err != nil {
		return err
	}
	if ident, ok := arg.(*ast.IdentExpr); ok &&
		sameType(typ, stringType) && env.isMutBorrowed(ident.Name) {
		return nil
	}
	return errorf("type error: `%s` expects &var std::string::String out, got %s",
		label, typ)
}

// checkFsWriteFile validates std::fs::write_file.
func (c *Checker) checkFsWriteFile(
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	return "std::fs::Error!void", true, nil
}

// checkFsRename validates std::fs::rename.
func (c *Checker) checkFsRename(
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	return "std::fs::Error!void", true, nil
}

// checkFsExists validates std::fs::exists.
func (c *Checker) checkFsExists(
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::exists", args, env, unsafe)
	return "std::fs::Error!bool", true, err
}

// checkFsMetadata validates std::fs::metadata.
func (c *Checker) checkFsMetadata(
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::metadata", args, env, unsafe)
	return "std::fs::Error!std::fs::Metadata", true, err
}

// checkFsReadDir validates std::fs::read_dir.
func (c *Checker) checkFsReadDir(
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	_, _, err := c.checkFsPathArgs("std::fs::read_dir", args, env, unsafe)
	return "std::fs::Error!std::array::Array<std::fs::DirEntry>", true, err
}

// checkFsPathOnly validates an Io plus path API and returns result.
func (c *Checker) checkFsPathOnly(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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

// checkArrayConstructor validates std::array::new<T>(allocator).
func (c *Checker) checkArrayConstructor(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	if !c.typeParams.contains(string(elem)) {
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
		return errorf("type error: Array element is not safe: %w", err)
	}
	return nil
}

// rejectArrayStorageType rejects values that are not Array-safe yet.
func (c *Checker) rejectArrayStorageType(typ Type, seen map[Type]bool) error {
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	if isPointerType(typ) {
		return errorf("type error: Array element cannot be raw pointer")
	}
	if c.types.containsBufferType(typ) {
		return errorf("type error: Array element cannot be stack buffer")
	}
	if base, arg, ok := splitGenericType(string(typ)); ok && base == "option" {
		argType, err := c.parseType(arg)
		if err != nil {
			return err
		}
		if err := c.rejectArrayStorageType(argType, seen); err != nil {
			return err
		}
	}
	if err := c.rejectArrayStorageStruct(typ, seen); err != nil {
		return err
	}
	return c.rejectArrayStorageUnion(typ, seen)
}

// rejectArrayStorageStruct checks struct fields recursively for Array storage.
func (c *Checker) rejectArrayStorageStruct(target Type, seen map[Type]bool) error {
	decl := c.structs[string(target)]
	if decl == nil {
		return nil
	}
	for _, field := range decl.Fields {
		fieldType, err := c.parseTypeNode(field.TypeName)
		if err != nil {
			return err
		}
		if err := c.rejectArrayStorageType(fieldType, seen); err != nil {
			return errorf("type error: struct `%s.%s` cannot be Array element: %w",
				target, field.Name, err)
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
		if err := c.rejectArrayStorageType(payload, seen); err != nil {
			return errorf("type error: union `%s::%s` cannot be Array element: %w",
				typ, variant, err)
		}
	}
	return nil
}

// checkIoArg validates an explicit Io argument for a std call.
func (c *Checker) checkIoArg(arg ast.Expression, env *scope, unsafe unsafeMark, name string) error {
	got, err := c.checkExpr(arg, env, unsafe)
	if err != nil {
		return err
	}
	if got != "Io" {
		return errorf("type error: `%s` expects Io, got %s", name, got)
	}
	return nil
}

// typeApplyTarget resolves the callee and static arguments of a `<...>` call,
// and refuses an unknown primitive before any of them is looked at.
func (c *Checker) typeApplyTarget(expr *ast.TypeApplyExpr) (string, string, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return "", "", errorf("type error: unsupported type application `%s`", expr.String())
	}
	if err := c.rejectUnknownBuiltin(name); err != nil {
		return "", "", err
	}
	typeArg := c.instantiateTypeArgText(expr.TypeArg)
	if err := rejectOptionalStaticArgs(typeArg); err != nil {
		return "", "", err
	}
	return name, typeArg, nil
}

// rejectOptionalStaticArgs refuses an optional inside a call-site `<...>`
// text, splitting it into the arguments rejectOptionalArgs checks.
func rejectOptionalStaticArgs(typeArg string) error {
	args, err := typ.SplitArgs(typeArg)
	if err != nil {
		args = []string{typeArg}
	}
	return rejectOptionalArgs(args)
}

// rejectOptionalArgs refuses an optional in a static-argument list: an
// optional cannot sit inside another type yet (ADR-0101).
func rejectOptionalArgs(args []string) error {
	for _, arg := range args {
		if _, ok := optionalElem(Type(arg)); ok {
			return typeResolutionError(typeResolutionIssue{
				kind: typeResolutionOptionalStaticArg, subject: Type(arg),
			})
		}
	}
	return nil
}

// checkTypeApplyCallExpr validates typed std constructor calls.
func (c *Checker) checkTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	name, typeArg, err := c.typeApplyTarget(expr)
	if err != nil {
		return "", err
	}
	if typ, ok, err := c.checkMetaApply(name, typeArg, args, env, unsafe); ok || err != nil {
		return typ, err
	}
	if name == "ptr_from_int" {
		return c.checkPtrFromInt(typeArg, expressionSpan(expr.Callee), args, env, unsafe)
	}
	if name == "int_from_ptr" {
		return c.checkIntFromPtr(typeArg, expressionSpan(expr.Callee), args, env, unsafe)
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
	if typ, ok, err := c.checkBuiltinTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, err
	}
	return "", errorf("type error: `%s` does not take static arguments", name)
}

// checkArenaTypeApply validates std::arena::new<T>(allocator).
func (c *Checker) checkArenaTypeApply(
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	parts, ok := splitGenericArgs(typeArg)
	if !ok || len(parts) != 1 {
		return "", errorf("type error: std::arena::new expects 1 type argument")
	}
	elem, err := c.parseType(parts[0])
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", errorf(
			"type error: `std::arena::new<%s>` expects exactly one allocator argument",
			elem)
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("type error: `std::arena::new<%s>` expects Allocator, got %s",
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
	unsafe unsafeMark,
) (Type, bool, error) {
	if strings.HasPrefix(name, "std::internal::builtin::arena") {
		return c.checkBuiltinArenaTypeApply(name, typeArg, args, env, unsafe)
	}
	if typ, ok, err := c.checkBuiltinBoxTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkBuiltinTestingTypeApply(
		name, typeArg, args, env, unsafe,
	); ok || err != nil {
		return typ, ok, err
	}
	return c.checkBuiltinArrayMethodTypeApply(name, typeArg, args, env, unsafe)
}

// checkBuiltinArenaTypeApply validates the Arena constructor and the storage
// primitives used only by std::arena's owner-element cleanup wrapper.
func (c *Checker) checkBuiltinArenaTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	if name == "std::internal::builtin::arena" {
		typ, err := c.checkArenaTypeApply(typeArg, args, env, unsafe)
		return typ, true, err
	}
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	receiver := Type(fmt.Sprintf("std::arena::Arena<%s>", elem))
	method := strings.TrimPrefix(name, "std::internal::builtin::arena_")
	return c.checkBuiltinReceiverMethod(name, receiver,
		func(rest []ast.Expression) (Type, error) {
			if len(rest) != 0 {
				return "", errorf("type error: `Arena.%s` expects 0 args, got %d",
					method, len(rest))
			}
			switch method {
			case "len":
				return typeI64, nil
			case "pop_or_panic":
				return elem, nil
			case "deinit":
				return typeVoid, nil
			default:
				return "", errorf("type error: Arena has no storage primitive `%s`", method)
			}
		}, args, env, unsafe)
}

// checkBuiltinTestingTypeApply validates typed std::testing primitives.
func (c *Checker) checkBuiltinTestingTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	if name != "std::internal::builtin::test_fail_equal" {
		return "", false, nil
	}
	arg, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	typ, err := c.checkBuiltinTestFailEqual(arg, args, env, unsafe)
	return typ, true, err
}

// checkBuiltinTestFailEqual validates the std::testing typed failure primitive.
func (c *Checker) checkBuiltinTestFailEqual(
	typ Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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

// checkBuiltinBoxTypeApply validates std-only Box runtime primitives.
func (c *Checker) checkBuiltinBoxTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	method, ok := boxPrimitiveMethod(name)
	if !ok {
		return "", false, nil
	}
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	if method == "" {
		return c.checkBoxConstructor(elem, args, env, unsafe)
	}
	return c.checkBuiltinBoxMethod(name, elem, method, args, env, unsafe)
}

// boxPrimitiveMethod maps a Box primitive to the method name it reports as, or
// "" for the constructor. ok is false for a name that is not a Box primitive.
func boxPrimitiveMethod(name string) (string, bool) {
	switch name {
	case "std::internal::builtin::box":
		return "", true
	case "std::internal::builtin::box_borrow":
		return "borrow", true
	case "std::internal::builtin::box_borrow_mut":
		return "borrow_mut", true
	case "std::internal::builtin::box_deinit":
		return "deinit", true
	case "std::internal::builtin::box_take":
		return "take", true
	default:
		return "", false
	}
}

// checkBoxConstructor validates std::mem::box<T>(allocator, value).
func (c *Checker) checkBoxConstructor(
	elem Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
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
	unsafe unsafeMark,
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
		case "take":
			return elem, nil
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
	unsafe unsafeMark,
) (Type, bool, error) {
	switch name {
	case "std::internal::builtin::array":
		arg, err := c.parseType(typeArg)
		if err != nil {
			return "", true, err
		}
		typ, _, err := c.checkArrayConstructor(arg, args, env, unsafe)
		return typ, true, err
	case "std::internal::builtin::array_append",
		"std::internal::builtin::array_len",
		"std::internal::builtin::array_capacity",
		"std::internal::builtin::array_pop", "std::internal::builtin::array_pop_or_panic",
		"std::internal::builtin::array_get", "std::internal::builtin::array_get_or_panic",
		"std::internal::builtin::array_at", "std::internal::builtin::array_at_mut",
		"std::internal::builtin::array_reserve",
		"std::internal::builtin::array_set",
		"std::internal::builtin::array_swap",
		"std::internal::builtin::array_deinit",
		"std::internal::builtin::array_truncate",
		"std::internal::builtin::array_clear",
		"std::internal::builtin::array_as_bytes",
		"std::internal::builtin::array_as_mut_bytes":
		return c.checkBuiltinArrayMethod(name, typeArg, args, env, unsafe)
	default:
		return "", false, nil
	}
}

// checkBuiltinArrayMethod validates std-only Array method primitives.
func (c *Checker) checkBuiltinArrayMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	elem, err := c.parseType(typeArg)
	if err != nil {
		return "", true, err
	}
	method := strings.TrimPrefix(name, "std::internal::builtin::array_")
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
	unsafe unsafeMark,
) (Type, bool, error) {
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
	unsafe unsafeMark,
) (Type, error) {
	switch name {
	case "pop":
		if len(args) != 0 {
			return "", errorf("type error: `Array.pop` expects 0 args, got %d", len(args))
		}
		return Type("?" + string(elem)), nil
	case "pop_or_panic":
		if len(args) != 0 {
			return "", errorf("type error: `Array.pop_or_panic` expects 0 args, got %d", len(args))
		}
		return elem, nil
	case "at":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("?&" + string(elem)), nil
	case "at_mut":
		if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("?&var " + string(elem)), nil
	case "get", "get_or_panic":
		return c.checkArrayPrimitiveGet(elem, name, args, env, unsafe)
	case "deinit":
		// The raw primitive frees only the buffer, with no owner-element rule:
		// it is the one escape `Array.deinit` uses after consuming the
		// elements, and only std source can name it.
		if len(args) != 0 {
			return "", errorf("type error: `Array.deinit` expects 0 args, got %d", len(args))
		}
		return typeVoid, nil
	default:
		return c.checkArrayMethod(elem, name, args, env, unsafe)
	}
}

// checkArrayPrimitiveGet validates the copy-only element reads behind the get
// wrappers.
func (c *Checker) checkArrayPrimitiveGet(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if err := c.checkArrayIndexArg(name, args, env, unsafe); err != nil {
		return "", err
	}
	if !isGenericParamType(elem) && !c.isCopyType(elem) {
		return "", errorf("type error: `Array.%s` requires copy element", name)
	}
	if name == "get" {
		return Type("?" + string(elem)), nil
	}
	return elem, nil
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
	unsafe unsafeMark,
) (Type, bool, error) {
	if strings.HasPrefix(name, "std::internal::builtin::map_") {
		return c.checkBuiltinMapMethod(name, typeArg, args, env, unsafe)
	}
	if name != "std::internal::builtin::map" {
		return "", false, nil
	}
	mapArgs, err := c.checkedMapArgs(typeArg)
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
	unsafe unsafeMark,
) (Type, bool, error) {
	mapArgs, err := c.checkedMapArgs(typeArg)
	if err != nil {
		return "", true, err
	}
	receiver := Type(fmt.Sprintf("std::map::Map<%s, %s>", mapArgs[0], mapArgs[1]))
	method := strings.TrimPrefix(name, "std::internal::builtin::map_")
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
	unsafe unsafeMark,
) (Type, error) {
	// at/at_mut come first: only the std wrapper body reaches the primitive,
	// and routing a concrete instantiation to checkMapMethod would hit the
	// user-facing capture-only refusal.
	switch name {
	case "at":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("?&" + string(valueType)), nil
	case "at_mut":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("?&var " + string(valueType)), nil
	case "take_value_at":
		// Reserved to Map.deinit's cascade, so it is answered here and never
		// reaches checkMapMethod: a caller outside std spells no name for it.
		if err := c.checkMapIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return valueType, nil
	}
	if !isGenericParamType(Type(keyType)) && !isGenericParamType(valueType) {
		return c.checkMapMethod(valueType, name, args, env, unsafe)
	}
	return c.checkGenericMapPrimitiveMethod(keyType, valueType, name, args, env, unsafe)
}

// checkGenericMapPrimitiveMethod validates Map primitives applied to generic
// static arguments, which only a std wrapper body can spell.
func (c *Checker) checkGenericMapPrimitiveMethod(
	keyType string,
	valueType Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
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
		return Type("?" + string(valueType)), nil
	case "take_value_at":
		if err := c.checkMapIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return valueType, nil
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
	unsafe unsafeMark,
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

// checkMapIndexArg validates one i64 insertion-position argument.
func (c *Checker) checkMapIndexArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) error {
	if len(args) != 1 {
		return errorf("type error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != typeI64 {
		return errorf("type error: `Map.%s` expects i64 index, got %s", name, got)
	}
	return nil
}

// checkGenericUserTypeApply validates source-defined std generic wrappers.
func (c *Checker) checkGenericUserTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
	fn := c.functions[name]
	if fn == nil || len(fn.sig.StaticParams) == 0 {
		return "", false, nil
	}
	argsText, ok := splitGenericArgs(typeArg)
	if !ok || len(argsText) != len(fn.sig.StaticParams) {
		return "", true, errorf("type error: `%s` expects %d static arguments",
			name, len(fn.sig.StaticParams))
	}
	typeArgsText, fieldArgs, err := c.checkStaticArgs(name, fn, argsText)
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
	for idx, param := range fn.sig.TypeParamNames() {
		subst[param] = typeArgs[idx]
	}
	for idx, expr := range args {
		if err := c.checkGenericUserArg(name, fn, subst, idx, expr, env, unsafe); err != nil {
			return "", true, err
		}
	}
	if err := c.checkGenericInstantiation(fn, subst, fieldArgs); err != nil {
		return "", true, err
	}
	// The result the caller sees is the declaration's type with this call's
	// arguments bound, forms included: `-> std::meta::field_type<T, f>` is a
	// concrete type here even though it is not one where it was written.
	restore := c.bindMetaFields(fieldArgs)
	result, err := c.resolveInstanceType(c.types.substituteTypeParams(fn.returnType, subst))
	restore()
	if err != nil {
		return "", true, err
	}
	return result, true, nil
}

// checkStaticArgs validates each `<...>` argument against what its parameter
// declared, and returns the subset that are types, in declaration order.
func (c *Checker) checkStaticArgs(
	name string,
	fn *functionType,
	argsText []string,
) ([]string, map[string]metaField, error) {
	typeArgs := []string{}
	fieldArgs := map[string]metaField{}
	for idx, param := range fn.sig.StaticParams {
		arg := strings.TrimSpace(argsText[idx])
		if param.IsType() {
			typeArgs = append(typeArgs, arg)
			continue
		}
		if Type(typ.Text(param.Type)) == typeField {
			field, err := c.fieldStaticArg(name, param, arg, idx, argsText, fn)
			if err != nil {
				return nil, nil, err
			}
			if field.name != "" {
				fieldArgs[param.Name] = field
			}
			continue
		}
		if err := c.checkStaticValueArg(name, param, arg, idx, argsText, fn); err != nil {
			return nil, nil, err
		}
	}
	return typeArgs, fieldArgs, nil
}

// checkStaticValueArg validates one compile-time value argument. The value is a
// literal or, for a `Function` parameter, a top-level function name. A generic
// may also pass on a static parameter of its own, which is how one wrapper
// forwards to another; the caller of the outer generic checked the real value.
func (c *Checker) checkStaticValueArg(
	name string,
	param ast.StaticParam,
	arg string,
	idx int,
	argsText []string,
	fn *functionType,
) error {
	if param.Type != nil && c.staticParams[arg] == Type(typ.Text(param.Type)) {
		return nil
	}
	switch Type(typ.Text(param.Type)) {
	case typeField:
		_, err := c.fieldStaticArg(name, param, arg, idx, argsText, fn)
		return err
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

// fieldStaticArg resolves a `Field` static argument to the field it names. The
// owner is the type argument written just before it: `worker<T, f>` pairs the
// two the way `std::meta::field_type<T, f>` does, so a worker reads its field
// out of the type it was instantiated for. A live `comptime for` capture
// resolves the same way, which is how a loop forwards its capture on.
func (c *Checker) fieldStaticArg(
	name string,
	param ast.StaticParam,
	arg string,
	idx int,
	argsText []string,
	fn *functionType,
) (metaField, error) {
	if field, ok := c.metaFields[arg]; ok {
		return field, nil
	}
	if !isIdentifierText(arg) {
		return metaField{}, errorf(
			"type error: `%s` static argument `%s` expects a field name, got `%s`",
			name, param.Name, arg)
	}
	owner, ok := precedingTypeArg(fn.sig.StaticParams, argsText, idx)
	if !ok {
		return metaField{}, errorf(
			"type error: `%s` static argument `%s` has no type parameter before it",
			name, param.Name)
	}
	if c.typeParams.contains(owner) {
		// The owner is the enclosing generic's own parameter, so which fields
		// exist is not known here. The instantiation of that generic checks
		// this call again with the type bound.
		return metaField{}, nil
	}
	fields, err := c.publicFields(owner)
	if err != nil {
		return metaField{}, err
	}
	for _, field := range fields {
		if field.name == arg {
			return field, nil
		}
	}
	return metaField{}, errorf("type error: `%s` has no public field `%s`", owner, arg)
}

// precedingTypeArg returns the type argument written just before the static
// parameter at idx.
func precedingTypeArg(params []ast.StaticParam, argsText []string, idx int) (string, bool) {
	for back := idx - 1; back >= 0; back-- {
		if params[back].IsType() {
			return strings.TrimSpace(argsText[back]), true
		}
	}
	return "", false
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

// checkGenericInstantiation checks a generic function body for one static type
// set. A `Field` static argument instantiates like a type argument rather than
// like the other compile-time values: the body reads it through
// `std::meta::field_name<T, f>` and its return type through
// `std::meta::field_type<T, f>`, so each bound field is its own instance.
func (c *Checker) checkGenericInstantiation(
	fn *functionType,
	subst map[string]Type,
	fieldArgs map[string]metaField,
) error {
	done, err := c.enterInstantiation(fn, subst, fieldArgs)
	if err != nil || done {
		return err
	}
	defer func() { c.instantiationDepth-- }()
	defer c.bindMetaFields(fieldArgs)()
	env := newScope(nil)
	staticParams, err := defineStaticValueParams(&c.types, env, fn.sig)
	if err != nil {
		return err
	}
	for idx, param := range fn.sig.Params {
		typ := c.types.substituteTypeParams(fn.params[idx], subst)
		defined := defineSignatureParam(
			&c.types, env, param.Name, typ, param.Borrow, param.MutBorrow)
		if err := requireScopeDefinition(param.Name, defined); err != nil {
			return err
		}
	}
	returnType, err := c.resolveInstanceType(c.types.substituteTypeParams(fn.returnType, subst))
	if err != nil {
		return err
	}
	for idx := range fn.params {
		paramType := c.types.substituteTypeParams(fn.params[idx], subst)
		if err := c.revalidateSubstituted(paramType); err != nil {
			return err
		}
	}
	if err := c.revalidateSubstituted(returnType); err != nil {
		return err
	}
	defer c.enterInstanceContext(fn, subst, staticParams, returnType)()
	returns, err := c.checkBlock(fn.body, env, returnType, nil)
	if err != nil {
		return err
	}
	if returnType != typeVoid && !returns {
		return errorf("type error: function `%s` must return %s", fn.name, returnType)
	}
	return nil
}

// resolveInstanceType rewrites the `std::meta` forms a declaration wrote into
// the types they name, now that this instantiation has bound what they read.
// A declaration writes `std::meta::field_type<T, f>` because neither name is
// bound where it is written; the instance is where both are.
func (c *Checker) resolveInstanceType(declared Type) (Type, error) {
	resolved, err := c.resolveMetaTypeDeep(string(declared))
	if err != nil {
		return "", err
	}
	if resolved == string(declared) {
		return declared, nil
	}
	return c.parseType(resolved)
}

// enterInstanceContext makes one instantiation the body being checked, and
// returns the call that puts back what was current before it.
func (c *Checker) enterInstanceContext(
	fn *functionType,
	subst map[string]Type,
	staticParams map[string]Type,
	returnType Type,
) func() {
	previousReturn := c.currentReturn
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeParams := c.typeParams.enterSignature(fn.sig)
	previousTypeArgValues := c.typeArgValues
	previousStaticParams := c.staticParams
	previousLoops := c.loopLabels
	c.currentReturn = returnType
	c.currentFunction = fn
	c.currentStd = fn.sig.Std
	c.typeArgValues = subst
	c.staticParams = staticParams
	c.loopLabels = nil
	return func() {
		c.currentReturn = previousReturn
		c.currentFunction = previousFunction
		c.currentStd = previousStd
		c.typeParams.restore(previousTypeParams)
		c.typeArgValues = previousTypeArgValues
		c.staticParams = previousStaticParams
		c.loopLabels = previousLoops
	}
}

// maxInstantiationDepth bounds how deep generic instantiation may nest. A body
// whose own calls grow their type argument never repeats an instance, so no
// memo stops it and the checker would run forever; a bound turns that into a
// diagnostic (#1627). Reflection walks nest by struct depth, so real programs
// stay far below this.
const maxInstantiationDepth = 64

// enterInstantiation records one instantiation and reports whether it has
// already been checked. An instance is one body with one set of static
// arguments, so the second visit can only reach the same answer -- which is
// also what stops a body that reaches itself.
func (c *Checker) enterInstantiation(
	fn *functionType,
	subst map[string]Type,
	fieldArgs map[string]metaField,
) (bool, error) {
	key := instanceKey(fn, subst, fieldArgs)
	if c.checkedInstances[key] {
		return true, nil
	}
	if c.instantiationDepth >= maxInstantiationDepth {
		return true, errorf(
			"type error: generic instantiation nested deeper than %d at `%s`\n"+
				"help: each call grew its static argument, so no instance repeats",
			maxInstantiationDepth, elideTypeText(key))
	}
	c.checkedInstances[key] = true
	c.instantiationDepth++
	return false, nil
}

// elideTypeText shortens a spelling for a diagnostic. A type that grew past
// the instantiation bound is thousands of characters of nesting, and printing
// all of it buries the sentence that says what went wrong.
func elideTypeText(text string) string {
	const budget = 60
	if len(text) <= budget*2 {
		return text
	}
	return text[:budget] + " ... " + text[len(text)-budget:]
}

// instanceKey names one instantiation: the body, and what its static
// parameters were bound to, in declaration order.
func instanceKey(fn *functionType, subst map[string]Type, fieldArgs map[string]metaField) string {
	args := make([]Type, 0, len(subst))
	for _, param := range fn.sig.TypeParamNames() {
		args = append(args, subst[param])
	}
	key := fn.name + "<" + joinTypes(args) + ">"
	for _, param := range fn.sig.StaticParams {
		if field, ok := fieldArgs[param.Name]; ok {
			key += "." + string(field.owner) + "." + field.name
		}
	}
	return key
}

// bindMetaFields makes the `Field` arguments of one instantiation readable by
// the forms written against them, and returns the call that unbinds them.
func (c *Checker) bindMetaFields(fieldArgs map[string]metaField) func() {
	if len(fieldArgs) == 0 {
		return func() {}
	}
	previous := make(map[string]metaField, len(fieldArgs))
	had := make(map[string]bool, len(fieldArgs))
	for name, field := range fieldArgs {
		previous[name], had[name] = c.metaFields[name]
		c.metaFields[name] = field
	}
	return func() {
		for name := range fieldArgs {
			if had[name] {
				c.metaFields[name] = previous[name]
				continue
			}
			delete(c.metaFields, name)
		}
	}
}

// parseGenericWrapperTypeArgs validates static type arguments for wrappers.
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
	case "std::array::Array":
		return c.rejectArrayElementType(args[0])
	case "std::map::Map":
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
	unsafe unsafeMark,
) error {
	want := c.types.substituteTypeParams(fn.params[idx], subst)
	checkedArg, err := prepareBorrowArgument(arg, fn.borrowParams[idx], fn.mutBorrowParams[idx], env)
	if err != nil {
		return err
	}
	if fn.mutBorrowParams[idx] {
		if err := requireMutableBorrowArg(checkedArg, want, env); err != nil {
			return err
		}
	}
	got, err := c.checkContextualExpr(checkedArg, want, env, unsafe)
	if err != nil {
		return err
	}
	got, err = coerceReturnedBorrowArgument(
		got, want, fn.borrowParams[idx], fn.mutBorrowParams[idx],
	)
	if err != nil {
		return err
	}
	if !sameType(got, want) {
		return userCallArgError(name, fn, idx, want, got)
	}
	return nil
}

// checkUserCall validates a declared function call.
func (c *Checker) checkUserCall(
	name string,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	fn, ok := c.functions[name]
	if !ok {
		return "", errorf("type error: undefined function `%s`", name)
	}
	operation := fmt.Sprintf("call to `%s`", name)
	if fn.sig.ExternABI != "" {
		if err := requireUnsafeCapabilityAt(unsafe, unsafeExternCall, operation, span); err != nil {
			return "", err
		}
	}
	if fn.sig.RequiresUnsafe {
		if err := requireUnsafeCapabilityAt(unsafe, unsafeUnsafeCall, operation, span); err != nil {
			return "", err
		}
	}
	if len(fn.sig.TypeParamNames()) > 0 {
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
	unsafe unsafeMark,
) error {
	checkedArg, err := prepareBorrowArgument(arg, fn.borrowParams[idx], fn.mutBorrowParams[idx], env)
	if err != nil {
		return err
	}
	if fn.mutBorrowParams[idx] {
		if err := requireMutableBorrowArg(checkedArg, fn.params[idx], env); err != nil {
			return err
		}
	}
	got, err := c.checkContextualExpr(checkedArg, fn.params[idx], env, unsafe)
	if err != nil {
		return err
	}
	got, err = coerceReturnedBorrowArgument(
		got, fn.params[idx], fn.borrowParams[idx], fn.mutBorrowParams[idx],
	)
	if err != nil {
		return err
	}
	if !sameType(got, fn.params[idx]) {
		return userCallArgError(name, fn, idx, fn.params[idx], got)
	}
	return nil
}

// userCallArityError reports declared function arity using source signatures when useful.
func userCallArityError(name string, fn *functionType, got int) error {
	if len(fn.params) == 1 {
		paramName := fn.sig.Params[0].Name
		if paramName != "" {
			return errorf("type error: `%s` expects %s",
				name, paramName)
		}
	}
	return errorf("type error: `%s` expects %d args, got %d", name, len(fn.params), got)
}

// userCallArgError reports source-call argument type mismatches. want is the
// type this call expects, which for a generic is the parameter with its static
// arguments filled in: a caller writing `id<i64>` is told i64, not T.
func userCallArgError(name string, fn *functionType, idx int, want Type, got Type) error {
	if strings.HasPrefix(name, "std::") && idx < len(fn.sig.Params) {
		paramName := fn.sig.Params[idx].Name
		if paramName != "" {
			if strings.HasPrefix(name, "std::fs::") {
				return errorf("type error: `%s` expects %s %s, got %s",
					name, want, paramName, got)
			}
			return errorf("type error: `%s` %s expects %s, got %s",
				name, paramName, want, got)
		}
	}
	return errorf("type error: arg %d of `%s` expects %s, got %s",
		idx+1, name, want, got)
}

// checkUnionConstructorCall validates Union.Variant(payload) construction.
func (c *Checker) checkUnionConstructorCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	got, err = c.coerceContextualIntegerLiteral(args[0], payload, got)
	if err != nil {
		return "", true, err
	}
	if !sameType(got, payload) &&
		!c.returnValueMatchesBorrowParam(args[0], env, payload, got) {
		return "", true, errorf("type error: union variant `%s::%s` expects %s, got %s",
			unionType.name, field.Name, payload, got)
	}
	return Type(unionType.name), true, nil
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
	unsafe unsafeMark,
) (Type, error) {
	decl := c.structs[expr.TypeName]
	if decl == nil {
		return "", errorf("type error: unknown struct `%s`", expr.TypeName)
	}
	if err := requireUnsafeStructInvariant(unsafe, decl,
		"construction of", "", expr.Span); err != nil {
		return "", err
	}
	values := map[string]Type{}
	exprs := map[string]ast.Expression{}
	for _, field := range expr.Fields {
		if _, written := values[field.Name]; written {
			return "", errorf("type error: duplicate field `%s.%s`", expr.TypeName, field.Name)
		}
		got, err := c.structLiteralFieldValue(decl, expr.TypeName, field, env, unsafe)
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
		want, err := c.resolveInstanceType(fieldDeclaredType(field))
		if err != nil {
			return "", err
		}
		got, err = c.coerceContextualIntegerLiteral(exprs[field.Name], want, got)
		if err != nil {
			return "", err
		}
		matches, err := c.structLiteralFieldMatches(exprs[field.Name], env, want, got)
		if err != nil {
			return "", err
		}
		if !matches {
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

// checkFieldExpr returns the declared type of a struct field read.
func (c *Checker) checkFieldExpr(expr *ast.FieldExpr, env *scope, unsafe unsafeMark) (Type, error) {
	fieldType, _, err := c.resolveFieldExpr(expr, env, unsafe)
	return fieldType, err
}

// resolveFieldExpr resolves a field access to the field's declared type and the
// struct that declares it, or nil for a receiver that is not a user struct.
// Reading is all this does. Writing takes on whatever invariant the struct
// carries, and that rule lives with the other assignment rules.
func (c *Checker) resolveFieldExpr(
	expr *ast.FieldExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, *ast.StructDecl, error) {
	if expr.Namespace {
		fieldType, err := c.checkNamespaceExpr(expr)
		return fieldType, nil, err
	}
	if enumType, ok := enumReceiver(expr.Receiver, c.enums); ok {
		return "", nil, errorf("type error: enum tag `%s.%s` must use `::`",
			enumType.name, expr.Name)
	}
	if set, ok := errorSetReceiver(expr.Receiver, c.errorSets); ok {
		return "", nil, errorf("type error: error `%s.%s` must use `::`", set.name, expr.Name)
	}
	if unionType, ok := unionReceiver(expr.Receiver, c.unions); ok {
		return "", nil, errorf("type error: union variant `%s.%s` must use `::`",
			unionType.name, expr.Name)
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", nil, err
	}
	receiver = borrowedValueType(receiver)
	if receiver == "std::fs::Metadata" {
		fieldType, err := checkFsMetadataField(expr.Name)
		return fieldType, nil, err
	}
	if receiver == "std::fs::DirEntry" {
		fieldType, err := checkFsDirEntryField(expr.Name)
		return fieldType, nil, err
	}
	decl := c.structs[string(receiver)]
	if decl == nil {
		return "", nil, errorf("type error: `%s` has no fields", receiver)
	}
	for _, field := range decl.Fields {
		if field.Name == expr.Name {
			if err := c.checkPrivateFieldAccess(string(receiver), field); err != nil {
				return "", nil, err
			}
			fieldType, err := c.resolveInstanceType(Type(typ.Text(field.TypeName)))
			return fieldType, decl, err
		}
	}
	return "", nil, errorf("type error: unknown field `%s.%s`", receiver, expr.Name)
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
	if set, ok := errorSetReceiver(expr.Receiver, c.errorSets); ok {
		if set.combines != nil {
			// A combined set re-exports nothing: its members stay the values
			// of the sets that declared them, so the reference names the
			// declaring set (ADR-0127).
			return "", errorf(
				"type error: `%s` is a combined set and declares no members;"+
					" write the declaring set's `Origin::%s`", set.name, expr.Name)
		}
		if !set.members[expr.Name] {
			return "", errorf("type error: unknown error `%s::%s`", set.name, expr.Name)
		}
		return Type(set.name), nil
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
func (c *Checker) checkDerefExpr(expr *ast.DerefExpr, env *scope, unsafe unsafeMark) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && env.isBorrowed(ident.Name) {
		typ, _ := env.lookup(ident.Name)
		return typ, nil
	}
	receiver, err := c.checkExpr(expr.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	if _, _, inner, ok := explicitBorrowType(receiver); ok {
		return inner, nil
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
// The receiver must itself be an assignable place — a mutable binding, a
// mutable borrow dereference, or a field chain that bottoms out in one.
// Anything else (a call result, an index) is refused here so the accept set
// matches what assignment lowering can store into.
func (c *Checker) checkAssignableField(
	expr *ast.FieldExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	switch receiver := expr.Receiver.(type) {
	case *ast.IdentExpr:
		if env.isBorrowed(receiver.Name) {
			if !env.isMutBorrowed(receiver.Name) {
				return "", errorf(
					"type error: cannot assign field through shared borrow `%s`",
					receiver.Name,
				)
			}
		} else if !env.isMutable(receiver.Name) {
			return "", errorf(
				"type error: cannot assign field of immutable binding `%s`",
				receiver.Name,
			)
		}
	case *ast.DerefExpr:
		if _, err := c.checkAssignableDeref(receiver, env, unsafe); err != nil {
			return "", err
		}
	case *ast.FieldExpr:
		if _, err := c.checkAssignableField(receiver, env, unsafe); err != nil {
			return "", err
		}
	default:
		return "", errorf("type error: invalid assignment target `%s`", expr.String())
	}
	fieldType, decl, err := c.resolveFieldExpr(expr, env, unsafe)
	if err != nil {
		return "", err
	}
	if err := requireUnsafeStructInvariant(unsafe, decl,
		"write to field", expr.Name, expr.Span); err != nil {
		return "", err
	}
	return fieldType, nil
}

// checkAssignableDeref validates mutation through an &var borrow or raw pointer.
func (c *Checker) checkAssignableDeref(
	expr *ast.DerefExpr,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok && env.isMutBorrowed(ident.Name) {
		// A writable slice view grants element writes only (ADR-0096):
		// re-pointing it would assign the caller's binding, which the flat
		// view representation never reaches.
		if typ, exists := env.lookup(ident.Name); exists && sameType(typ, typeByteString) {
			return "", errorf(
				"type error: `%s.*` cannot re-point a slice view; write elements with `%s[i] = ...`",
				ident.Name, ident.Name)
		}
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

// errorSetReceiver returns an error set namespace used by SetName::Member.
func errorSetReceiver(
	expr ast.Expression,
	sets map[string]*errorSetType,
) (*errorSetType, bool) {
	name, ok := qualifiedName(expr)
	if !ok {
		return nil, false
	}
	set, ok := sets[name]
	return set, ok
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
	unsafe unsafeMark,
) (Type, error) {
	if err := checkMethodReceiverPath(field); err != nil {
		return "", err
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return "", err
	}
	receiver = borrowedValueType(receiver)
	typ, ok, err := c.checkKnownReceiverMethod(field, receiver, args, env, unsafe)
	if ok || err != nil {
		return typ, err
	}
	return c.checkArenaOrImplMethod(field, receiver, args, env, unsafe)
}

// checkMethodReceiverPath enforces the field receiver boundary: any field path
// rooted in a local binding may receive a method, but destructive cleanup stays
// on one direct field so every type cleans its own fields.
func checkMethodReceiverPath(field *ast.FieldExpr) error {
	receiver, ok := field.Receiver.(*ast.FieldExpr)
	if !ok || receiver.Namespace {
		return nil
	}
	if _, _, ok := ast.FieldPathRoot(receiver); !ok {
		return errorAt(receiver.Span,
			"type error: field method receiver must be a field path on a local binding")
	}
	if field.Name != typ.CleanupMethod {
		return nil
	}
	if _, direct := receiver.Receiver.(*ast.IdentExpr); !direct {
		return errorf(
			"type error: field cleanup `%s.%s` is only allowed on one direct field",
			receiver.String(), field.Name,
		)
	}
	// Who may consume a field is an ownership question -- whether the value is
	// held or borrowed, and whether its type declares a cleanup of its own --
	// and the ownership checker answers it. This one keeps the shape rule.
	return nil
}

// checkKnownReceiverMethod validates non-arena builtin receiver families.
func (c *Checker) checkKnownReceiverMethod(
	field *ast.FieldExpr,
	receiver Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, bool, error) {
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
	unsafe unsafeMark,
) (Type, error) {
	switch field.Name {
	case "borrow":
		return "", errorf(
			"type error: `Box.borrow` must be bound with `let name = box.borrow()`")
	case "borrow_mut":
		return "", errorf(
			"type error: `Box.borrow_mut` must be bound with `let name = box.borrow_mut()`")
	case "take":
		if _, ok := field.Receiver.(*ast.IdentExpr); !ok {
			return "", errorf("type error: `Box.take` requires local Box receiver")
		}
		return c.checkStdMethod("std::mem::Box", []Type{elem}, "Box", field.Name,
			args, env, unsafe)
	case "deinit":
		if _, ok := field.Receiver.(*ast.IdentExpr); !ok &&
			!c.directFieldCleanupReceiver(field.Receiver, env) {
			return "", errorf("type error: `Box.%s` requires local Box receiver", field.Name)
		}
		return c.checkStdMethod("std::mem::Box", []Type{elem}, "Box", field.Name,
			args, env, unsafe)
	default:
		receiver := Type(fmt.Sprintf("std::mem::Box<%s>", elem))
		method := c.implMethod(string(receiver), field.Name)
		if method != nil {
			return c.checkMethodArgs(method, receiver, field.Receiver, expressionSpan(field),
				args, env, unsafe)
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
	unsafe unsafeMark,
) (Type, error) {
	base, arg, ok := splitGenericType(string(receiver))
	if !ok || base != "std::arena::Arena" {
		method := c.implMethod(string(receiver), field.Name)
		if method != nil {
			return c.checkMethodArgs(method, receiver, field.Receiver, expressionSpan(field),
				args, env, unsafe)
		}
		return "", errorf("type error: `%s` has no method `%s`", receiver, field.Name)
	}
	switch field.Name {
	case "add":
		return c.checkArenaAdd(arg, args, env, unsafe)
	case "at":
		return c.checkArenaAt(arg, args, env, unsafe)
	case "at_mut":
		return c.checkArenaAtMut(field, arg, args, env, unsafe)
	case "deinit":
		return c.checkArenaDeinit(field, args, env)
	default:
		return "", errorf("type error: unknown arena method `%s`", field.Name)
	}
}

// checkArenaAtMut validates std::arena::Arena<T>.at_mut(handle): capture
// conditions consume the `?&var T` it returns, everywhere else refuses it.
func (c *Checker) checkArenaAtMut(
	field *ast.FieldExpr,
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if !c.captureCondition {
		return "", errorf("type error: `Arena.at_mut` must be consumed by a capture" +
			" (`if a.at_mut(handle) |name|` or `while a.at_mut(handle) |name|`)")
	}
	if !mutableReceiverPlace(field.Receiver, env) {
		return "", errorf("type error: `Arena.at_mut` requires mutable arena binding")
	}
	// The flag covers exactly this call: off while the argument is read —
	// a nested at/at_mut in argument position refuses as usual — and back
	// on for the result the call gate reads.
	c.captureCondition = false
	err := c.checkArenaHandleArg(arg, args, env, unsafe, "Arena.at_mut")
	c.captureCondition = true
	if err != nil {
		return "", err
	}
	return Type("?&var " + arg), nil
}

// checkStringReceiverMethod validates receiver-sensitive String methods.
func (c *Checker) checkStringReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) (Type, error) {
	switch name {
	case "append_bytes", "append_byte", "append_string", "reserve", "truncate":
		if err := c.checkStringMutatorArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return "!void", nil
	case "len", "capacity":
		if len(args) != 0 {
			return "", errorf("type error: `String.%s` expects 0 args, got %d", name, len(args))
		}
		return typeI64, nil
	case "as_bytes", "as_mut_bytes":
		return "", errorf(
			"type error: `String.%s` must be bound with `let name = string.%s()`", name, name)
	case "clear", "deinit":
		if len(args) != 0 {
			return "", errorf("type error: `String.%s` expects 0 args, got %d", name, len(args))
		}
		return typeVoid, nil
	default:
		return "", errorf("type error: String has no method `%s`", name)
	}
}

// checkStringMutatorArg validates the single argument each String mutator takes.
func (c *Checker) checkStringMutatorArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) error {
	switch name {
	case "append_bytes":
		return c.checkStringBytesArg(name, args, env, unsafe)
	case "append_byte":
		return c.checkStringByteArg(name, args, env, unsafe)
	case "append_string":
		return c.checkStringStringArg(name, args, env, unsafe)
	default:
		return c.checkStringReserveArg(name, args, env, unsafe)
	}
}

// isStringMutatingMethod reports whether a String method can change owned storage.
func isStringMutatingMethod(name string) bool {
	switch name {
	case "append_bytes", "append_byte", "append_string", "reserve", "truncate",
		"clear", "deinit":
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
	unsafe unsafeMark,
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

// checkStringStringArg validates a borrowed String source argument.
func (c *Checker) checkStringStringArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) error {
	if len(args) != 1 {
		return errorf("type error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	if got != "std::string::String" {
		return errorf("type error: `String.%s` expects String, got %s", name, got)
	}
	return nil
}

// checkStringReserveArg validates a reserve additional byte count.
func (c *Checker) checkStringReserveArg(
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) (Type, error) {
	if err := checkArrayReceiverBorrow(field, env); err != nil {
		return "", err
	}
	if field.Name == "at_mut" && !mutableReceiverPlace(field.Receiver, env) {
		return "", errorf("type error: `Array.at_mut` requires mutable array binding")
	}
	return c.checkArrayMethod(elem, field.Name, args, env, unsafe)
}

// checkArrayReceiverBorrow rejects slot exchange through a shared Array borrow.
func checkArrayReceiverBorrow(field *ast.FieldExpr, env *scope) error {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok || field.Name != "swap" || !env.isBorrowed(ident.Name) {
		return nil
	}
	if !env.isMutBorrowed(ident.Name) {
		return errorf("type error: `Array.%s` requires mutable Array receiver", field.Name)
	}
	return nil
}

// checkMapReceiverMethod validates receiver-sensitive Map<K, V> methods.
func (c *Checker) checkMapReceiverMethod(
	field *ast.FieldExpr,
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if err := checkMapReceiverBorrow(field, env); err != nil {
		return "", err
	}
	if field.Name == "at_mut" && !mutableReceiverPlace(field.Receiver, env) {
		return "", errorf("type error: `Map.at_mut` requires mutable map binding")
	}
	mapArgs, err := c.checkedMapArgs(arg)
	if err != nil {
		return "", err
	}
	return c.checkMapMethod(Type(mapArgs[1]), field.Name, args, env, unsafe)
}

// mutableReceiverPlace reports whether a method receiver expression names a
// place a mutable borrow may come from: a `var` binding, a `&var` borrow, or
// a field path rooted in either.
func mutableReceiverPlace(receiver ast.Expression, env *scope) bool {
	base, ok := mutablePlaceBase(receiver)
	return ok && (env.isMutable(base.Name) || env.isMutBorrowed(base.Name))
}

// captureReceiverShape reports whether a capture-condition receiver names a
// borrowable place: a local binding, or a field path rooted in one — the same
// shape field method receivers support.
func captureReceiverShape(receiver ast.Expression) bool {
	if _, ok := receiver.(*ast.IdentExpr); ok {
		return true
	}
	_, _, ok := ast.FieldPathRoot(receiver)
	return ok
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

// checkArrayMethod validates owned Array<T> prototype methods.
func (c *Checker) checkArrayMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if isStdArrayStorageMethod(name) {
		return c.checkStdArrayStorageMethod(elem, name, args, env, unsafe)
	}
	// Rules the declaration cannot state: `at`/`at_mut` hand out a borrow,
	// `get` copies out of the array, and owner elements make shallow cleanup a
	// leak (ADR-0091). One cleanup name covers both: `deinit` releases what the
	// container holds along with the container (ADR-0119).
	switch name {
	case "at", "at_mut":
		// In a capture condition the std declaration answers everything:
		// args and the `?&T` / `?&var T` return both come from the signature.
		if !c.captureCondition {
			return "", errorf("type error: `Array.%s` must be consumed by a capture"+
				" (`if array.%s(...) |name|` or `while array.%s(...) |name|`)",
				name, name, name)
		}
		// The flag covers exactly this call: off while the arguments are
		// read — a nested at/at_mut in argument position refuses as usual —
		// and back on for the result the call gate reads.
		c.captureCondition = false
		result, err := c.checkStdMethod("std::array::Array", []Type{elem}, "Array",
			name, args, env, unsafe)
		c.captureCondition = true
		return result, err
	case "get", "get_or_panic":
		if !c.isCopyType(elem) {
			return "", errorf("type error: `Array.%s` requires copy element", name)
		}
	case "clone":
		if !c.isCopyType(elem) {
			return "", errorf("type error: `Array.clone` requires copy element")
		}
	case "set":
		if c.ownerType(elem) {
			return "", errorf(
				"type error: `Array.set` would leak the replaced `%s` element", elem)
		}
	}
	return c.checkStdMethod("std::array::Array", []Type{elem}, "Array", name, args, env, unsafe)
}

// checkStdMethod checks a receiver call against the signature std declares for
// it in std/src/*/*.kizu, with the receiver's static arguments substituted in.
func (c *Checker) checkStdMethod(
	receiver string,
	typeArgs []Type,
	label string,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	method := c.implMethod(receiver, name)
	if method == nil || len(method.params) == 0 {
		return "", errorf("type error: %s has no method `%s`", label, name)
	}
	if err := c.checkStdMethodBody(method, typeArgs); err != nil {
		return "", err
	}
	if len(args) != len(method.params)-1 {
		return "", errorf("type error: `%s.%s` expects %d args, got %d",
			label, name, len(method.params)-1, len(args))
	}
	typeParams := method.sig.TypeParamNames()
	subst := make(map[string]Type, len(typeArgs))
	if len(typeParams) == len(typeArgs) {
		for idx, param := range typeParams {
			subst[param] = typeArgs[idx]
		}
	}
	for idx, arg := range args {
		paramIndex := idx + 1
		want := c.types.substituteTypeParams(method.params[paramIndex], subst)
		got, err := c.checkContextualExpr(arg, want, env, unsafe)
		if err != nil {
			return "", err
		}
		got, err = coerceReturnedBorrowArgument(
			got, want, method.borrowParams[paramIndex], method.mutBorrowParams[paramIndex],
		)
		if err != nil {
			return "", err
		}
		if !sameType(got, want) {
			return "", errorf("type error: `%s.%s` expects %s, got %s", label, name, want, got)
		}
	}
	result := c.types.substituteTypeParams(method.returnType, subst)
	if err := c.revalidateSubstituted(result); err != nil {
		return "", err
	}
	return result, nil
}

// revalidateSubstituted re-parses a type text after generic substitution when
// it carries an optional. Declaration-time checking sees only `?T`, so a
// substitution can mint spellings the source could never write -- `pop<!i64>`
// would return `?!i64` -- and those must fail the same way the literal
// spelling does.
func (c *Checker) revalidateSubstituted(t Type) error {
	if !strings.Contains(string(t), "?") {
		return nil
	}
	_, err := c.parseType(string(t))
	return err
}

// checkStdMethodBody checks the std wrapper a receiver call resolved to, with
// this receiver's static arguments bound.
//
// A generic body is checked when a call instantiates it (ADR-0066), and no call
// instantiates these: a container method is matched against the signature std
// declares and lowered from the method name. That left the body unchecked, so
// `return std::internal::builtin::array_apend<T>(self, value)` -- or anything else -- sat
// in std reading like the implementation while meaning nothing.
func (c *Checker) checkStdMethodBody(fn *functionType, typeArgs []Type) error {
	typeParams := fn.sig.TypeParamNames()
	if fn.body == nil || len(typeParams) != len(typeArgs) {
		return nil
	}
	key := fn.sig.Name + "<" + joinTypes(typeArgs) + ">"
	if c.checkedStdBodies[key] {
		return nil
	}
	// Recorded before checking, so a body that reaches its own method through
	// another wrapper stops here instead of recurring.
	c.checkedStdBodies[key] = true
	subst := make(map[string]Type, len(typeArgs))
	for idx, param := range typeParams {
		subst[param] = typeArgs[idx]
	}
	return c.checkGenericInstantiation(fn, subst, nil)
}

// checkStdArrayStorageMethod validates Array helpers reserved to std source.
func (c *Checker) checkStdArrayStorageMethod(
	elem Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
		return checkArrayAsBytes(elem, name, args)
	}
}

// isStdArrayStorageMethod reports methods reserved for std-owned storage wrappers.
func isStdArrayStorageMethod(name string) bool {
	return name == "truncate" || name == "clear" ||
		name == "as_bytes" || name == "as_mut_bytes"
}

// checkArrayAsBytes validates Array<u8> to byte-slice view conversion. The
// as_mut_bytes form hands back the writable view spelling (ADR-0096); the
// view value itself is the same.
func checkArrayAsBytes(elem Type, name string, args []ast.Expression) (Type, error) {
	if elem != typeU8 {
		return "", errorf("type error: `Array.%s` requires Array<u8>", name)
	}
	if len(args) != 0 {
		return "", errorf("type error: `Array.%s` expects 0 args, got %d", name, len(args))
	}
	if name == "as_mut_bytes" {
		return "&var []u8", nil
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
) (Type, error) {
	// Rules the declaration cannot state: `get` copies the value out, so an
	// owner value has to leave through the `at`/`at_mut` borrows instead, and
	// owner values make shallow cleanup a leak (ADR-0091). Both mirror what
	// Array already answers for owner elements (ADR-0123).
	switch name {
	case "insert":
		return c.checkMapInsert(valueType, args, env, unsafe)
	case "get":
		if err := c.checkMapKeyArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		if !isGenericParamType(valueType) && !c.isCopyType(valueType) {
			return "", errorf("type error: `Map.get` requires copy value")
		}
		return Type("?" + string(valueType)), nil
	case "at", "at_mut":
		return c.checkMapAtCondition(valueType, name, args, env, unsafe)
	case "key_at":
		if err := c.checkMapIndexArg(name, args, env, unsafe); err != nil {
			return "", err
		}
		return Type("?[]u8"), nil
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

// checkMapAtCondition types at/at_mut inside a capture condition and refuses
// them anywhere else: the borrow optional they produce exists only there.
func (c *Checker) checkMapAtCondition(
	valueType Type,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if !c.captureCondition {
		return "", errorf("type error: `Map.%s` must be consumed by a capture"+
			" (`if m.%s(key) |name|` or `while m.%s(key) |name|`)",
			name, name, name)
	}
	// The flag covers exactly this call: off while the argument is read —
	// a nested at/at_mut in argument position refuses as usual — and back
	// on for the result the call gate reads.
	c.captureCondition = false
	err := c.checkMapKeyArg(name, args, env, unsafe)
	c.captureCondition = true
	if err != nil {
		return "", err
	}
	if name == "at_mut" {
		return Type("?&var " + string(valueType)), nil
	}
	return Type("?&" + string(valueType)), nil
}

// checkMapInsert validates Map.insert arguments.
func (c *Checker) checkMapInsert(
	valueType Type,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	if _, err := c.parseType(fmt.Sprintf("std::map::Map<%s>", arg)); err != nil {
		return nil, err
	}
	return args, nil
}

// checkMapTypeArgContract enforces public Map constructor restrictions.
func (c *Checker) checkMapTypeArgContract(args []Type) error {
	if len(args) != 2 {
		return errorf("type error: std::map::Map expects 2 static arguments")
	}
	if !sameType(args[0], typeByteString) {
		return errorf("type error: std::map::Map key type must be []u8")
	}
	return nil
}

// checkMethodArgs validates method-call arguments after the implicit self receiver.
func (c *Checker) checkMethodArgs(
	method *functionType,
	receiver Type,
	receiverExpr ast.Expression,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if len(method.params) == 0 {
		return "", errorf("type error: method `%s` must have self parameter", method.name)
	}
	if method.params[0] != receiver {
		return "", errorf("type error: method `%s` self expects %s, got %s",
			method.name, method.params[0], receiver)
	}
	if method.mutBorrowParams[0] {
		if err := requireMutableSelfReceiver(method, receiverExpr, env); err != nil {
			return "", err
		}
	}
	return c.checkCallableArgs(method, 1, span, args, env, unsafe)
}

// requireMutableSelfReceiver restricts `&var self` methods to receivers whose
// storage the caller can hand over: a mutable place, so a local, a reborrowed
// &var param, or one direct field of either.
func requireMutableSelfReceiver(method *functionType, receiver ast.Expression, env *scope) error {
	base, ok := mutablePlaceBase(receiver)
	if !ok {
		return errorf(
			"type error: method `%s` takes `&var self` and requires a local or direct field receiver",
			method.name)
	}
	if env.isMutable(base.Name) || env.isMutBorrowed(base.Name) {
		return nil
	}
	return errorf("type error: method `%s` takes `&var self`, receiver `%s` must be mutable",
		method.name, base.Name)
}

// mutablePlaceBase resolves the binding whose storage a mutable place hands
// over: the name itself, or the root of a field path. This is the one shape
// rule for every `&var` position -- method receivers and call arguments read
// the same answer.
func mutablePlaceBase(expr ast.Expression) (*ast.IdentExpr, bool) {
	if place, ok := expr.(*ast.IdentExpr); ok {
		return place, true
	}
	root, _, ok := ast.FieldPathRoot(expr)
	return root, ok
}

// checkCallableArgs validates the arguments a call passes, starting at the
// parameter after the ones the call does not write.
func (c *Checker) checkCallableArgs(
	method *functionType,
	offset int,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if len(args) != len(method.params)-offset {
		return "", errorf("type error: `%s` expects %d args, got %d",
			method.name, len(method.params)-offset, len(args))
	}
	if method.sig.RequiresUnsafe {
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
		want := method.params[idx+offset]
		checkedArg, err := prepareBorrowArgument(
			arg,
			method.borrowParams[idx+offset],
			method.mutBorrowParams[idx+offset],
			env,
		)
		if err != nil {
			return "", err
		}
		if method.mutBorrowParams[idx+offset] {
			if err := requireMutableBorrowArg(checkedArg, want, env); err != nil {
				return "", err
			}
		}
		got, err := c.checkContextualExpr(checkedArg, want, env, unsafe)
		if err != nil {
			return "", err
		}
		got, err = coerceReturnedBorrowArgument(
			got,
			want,
			method.borrowParams[idx+offset],
			method.mutBorrowParams[idx+offset],
		)
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
// A `&var []u8` slot is stricter (ADR-0096): only a writable view binding may
// be lent — a `var`-bound plain slice does not guarantee writable backing.
// want may be "" when the slot's type is unknown at the call site.
func requireMutableBorrowArg(expr ast.Expression, want Type, env *scope) error {
	if sameType(want, typeByteString) {
		if ident, ok := expr.(*ast.IdentExpr); ok && env.isMutBorrowed(ident.Name) {
			return nil
		}
		return errorf(
			"type error: `&var []u8` argument must be a writable view binding")
	}
	base, ok := mutablePlaceBase(expr)
	if !ok {
		return errorf("type error: &var argument must be a mutable local binding")
	}
	if env.isMutable(base.Name) || env.isMutBorrowed(base.Name) {
		return nil
	}
	return errorf("type error: &var argument `%s` must be mutable", base.Name)
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
		// The slot's type is unknown here; the caller re-checks with it.
		if err := requireMutableBorrowArg(prefix.Right, "", env); err != nil {
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

// checkBorrowTargetShape restricts explicit borrows to direct locals or a
// field path rooted in one.
func checkBorrowTargetShape(expr ast.Expression) error {
	if _, ok := expr.(*ast.IdentExpr); ok {
		return nil
	}
	if _, _, ok := ast.FieldPathRoot(expr); ok {
		return nil
	}
	return errorAt(expressionSpan(expr),
		"type error: borrow target must be a local binding or field path")
}

// checkArenaAdd validates std::arena::Arena<T>.add(value).
func (c *Checker) checkArenaAdd(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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

// checkArenaAt validates std::arena::Arena<T>.at(std::arena::Handle<T>).
func (c *Checker) checkArenaAt(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	if err := c.checkArenaHandleArg(arg, args, env, unsafe, "arena.at"); err != nil {
		return "", err
	}
	return Type("&" + arg), nil
}

// checkArenaHandleArg validates the one handle argument an arena accessor
// takes against the arena's element type. label names the accessor in errors.
func (c *Checker) checkArenaHandleArg(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
	label string,
) error {
	if len(args) != 1 {
		return errorf("type error: `%s` expects 1 arg, got %d", label, len(args))
	}
	got, err := c.checkExpr(args[0], env, unsafe)
	if err != nil {
		return err
	}
	want := Type(fmt.Sprintf("std::arena::Handle<%s>", arg))
	if !sameType(got, want) {
		return errorf("type error: `%s` expects %s, got %s", label, want, got)
	}
	return nil
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

// directFieldCleanupReceiver reports whether expr names one direct field, the
// shape a field cleanup takes. Whether that field may be consumed here is an
// ownership question, answered there.
func (c *Checker) directFieldCleanupReceiver(expr ast.Expression, env *scope) bool {
	field, ok := expr.(*ast.FieldExpr)
	if !ok {
		return false
	}
	owner, direct := field.Receiver.(*ast.IdentExpr)
	if !direct {
		return false
	}
	_, found := env.lookup(owner.Name)
	return found
}

// checkPtrRead validates unsafe raw pointer reads.
func (c *Checker) checkPtrRead(expr *ast.CallExpr, env *scope, unsafe unsafeMark) (Type, error) {
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
func (c *Checker) checkPtrWrite(expr *ast.CallExpr, env *scope, unsafe unsafeMark) (Type, error) {
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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

// ownerType reports whether values of typ carry a deinit contract (ADR-0091).
func (c *Checker) ownerType(typ Type) bool {
	return ast.OwnerType(c.deinitOwners, string(typ))
}

// isCopyType reports whether values of typ can be duplicated safe code.
func (c *Checker) isCopyType(typ Type) bool {
	if typ == typeByteString {
		return true
	}
	if c.enums[string(typ)] != nil {
		return true
	}
	// An error carries nothing, so reading one leaves nothing behind to move.
	if c.errorSets[string(typ)] != nil {
		return true
	}
	if copyTypes[typ] {
		return true
	}
	return c.isPlainDataType(string(typ), nil)
}

// isPlainDataType reports whether name is plain copy data: a scalar, enum,
// error set, arena handle, or a declared struct / union whose fields and
// payloads are all plain copy data. Duplicating such a value creates no cleanup obligation, so
// copy propagates through it structurally. Views, capabilities, and owners are
// not plain data — aggregates holding them keep their own regimes — and a type
// that declares an explicit deinit stays move-only because the declared
// cleanup contract implies a consumption obligation.
func (c *Checker) isPlainDataType(name string, seen map[string]bool) bool {
	if inner, ok := strings.CutPrefix(name, "?"); ok {
		return c.isPlainDataType(inner, seen)
	}
	t := Type(name)
	if t == typeBool || t == typeVoid || numericTypes[t] {
		return true
	}
	if c.enums[name] != nil || c.errorSets[name] != nil {
		return true
	}
	// An arena handle is an opaque ID; the arena owns the value, so
	// duplicating the ID creates no cleanup obligation.
	if strings.HasPrefix(name, "std::arena::Handle<") {
		return true
	}
	// seen holds the current recursion path only: a revisit is a recursive
	// aggregate, which needs indirection and is never plain data.
	if seen[name] {
		return false
	}
	return c.isPlainDataAggregate(name, seen)
}

// isPlainDataAggregate walks a declared struct / union for isPlainDataType.
// A declared deinit keeps the type move-only regardless of its fields.
func (c *Checker) isPlainDataAggregate(name string, seen map[string]bool) bool {
	st, isStruct := c.structs[name]
	union, isUnion := c.unions[name]
	if (!isStruct && !isUnion) || c.implMethod(name, "deinit") != nil {
		return false
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	seen[name] = true
	defer delete(seen, name)
	if isStruct {
		for _, field := range st.Fields {
			fieldType := stdmeta.ResolveElementTypeForms(typ.Text(field.TypeName))
			if field.Borrow || !c.isPlainDataType(fieldType, seen) {
				return false
			}
		}
		return true
	}
	for _, payload := range union.variants {
		if payload != "" && !c.isPlainDataType(string(payload), seen) {
			return false
		}
	}
	return true
}

// checkNoArgConstructor validates a zero-argument builtin constructor.
func checkNoArgConstructor(name string, args []ast.Expression, typ Type) (Type, error) {
	if len(args) != 0 {
		return "", errorf("type error: `%s` expects 0 args, got %d", name, len(args))
	}
	return typ, nil
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

// implMethod returns a concrete method implementation.
func (c *Checker) implMethod(typeName string, method string) *functionType {
	methods := c.impls[typeName]
	if methods == nil {
		return nil
	}
	return methods[method]
}

// checkPrintCall validates the print builtin.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope, unsafe unsafeMark) (Type, error) {
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
