package types

import (
	"fmt"
	"slices"
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
	unsafePtrRead         unsafeCapability = "ptr_read"
	unsafePtrWrite        unsafeCapability = "ptr_write"
	unsafePtrDeref        unsafeCapability = "ptr_deref"
	unsafePtrCast         unsafeCapability = "ptr_cast"
	unsafePtrIntCast      unsafeCapability = "ptr_int_cast"
	unsafeExternCall      unsafeCapability = "extern_call"
	unsafeUnsafeCall      unsafeCapability = "unsafe_call"
	unsafeStructInvariant unsafeCapability = "struct_invariant"
	unsafeVolatile        unsafeCapability = "volatile"
)

// unsafeScope is one `unsafe` marker. A use is recorded on the innermost marker
// in scope, which is what lets a marker that covers nothing be reported.
type unsafeScope struct {
	used bool
}

// unsafeMark is the marker state threaded through checking. A nil mark means
// the expression is covered by no `unsafe`, so a function body starts unmarked
// whether or not the function itself is declared `unsafe fn`.
type unsafeMark = *unsafeScope

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

// requireUnsafeCapabilityAt rejects an operation the compiler cannot prove when
// no `unsafe` marker covers it. Source no longer spells capability names, but
// the diagnostic still names the kind of operation so the reader learns what
// obligation the marker would take on.
func requireUnsafeCapabilityAt(
	mark unsafeMark,
	cap unsafeCapability,
	operation string,
	span ast.Span,
) error {
	if mark.use() {
		return nil
	}
	message := fmt.Sprintf("unsafe error: %s requires `unsafe`", operation)
	if info, ok := unsafecap.Lookup(string(cap)); ok {
		message += "\nhelp: " + unsafecap.Hint(info)
	}
	if !span.IsZero() {
		return errorAtCode(span, "unsafe.missing_marker", "%s", message)
	}
	return errorf("%s", message)
}

// requireUnsafeStructInvariant rejects an operation that establishes or changes
// the invariant of an `unsafe struct` when no `unsafe` marker covers it.
// Construction and field writes are the same rule: both put the struct into a
// state only the author can vouch for.
func requireUnsafeStructInvariant(
	mark unsafeMark,
	decl *ast.StructDecl,
	action string,
	fieldName string,
	span ast.Span,
) error {
	if decl == nil || !decl.RequiresUnsafe {
		return nil
	}
	target := "`unsafe struct " + decl.Name + "`"
	if fieldName != "" {
		target = "`" + decl.Name + "." + fieldName + "`"
	}
	return requireUnsafeCapabilityAt(mark, unsafeStructInvariant, action+" "+target, span)
}

// Checker validates type rules for a parsed program.
type Checker struct {
	functions       map[string]*functionType
	structs         map[string]*ast.StructDecl
	enums           map[string]*enumType
	errorSets       map[string]*errorSetType
	unions          map[string]*unionType
	contracts       map[string]*contractType
	impls           map[string]map[string]*functionType
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
	// checkedStdBodies records the std wrapper instantiations already checked,
	// keyed by name and static arguments.
	checkedStdBodies map[string]bool
	// deinitOwners marks the base type names whose values carry a deinit
	// contract, seeded from ast.DeinitOwners — the one definition of owner-ness.
	deinitOwners map[string]bool
}

type enumType struct {
	name   string
	tags   map[string]bool
	public bool
}

// errorSetType is a declared set of failures. Its members carry nothing, so the
// set is the whole of what a failure says about itself.
type errorSetType struct {
	name    string
	members map[string]bool
	public  bool
	// tagged is the same set seen as something a match runs over. Asking which
	// failure it is, is the question a match on an enum asks, so it is answered
	// by the same code rather than a second copy of it.
	tagged *enumType
}

type unionType struct {
	name       string
	typeParams []string
	variants   map[string]string
	public     bool
}

// A functionType is what a call site sees, plus the body the passes that check
// or instantiate it run over. The two are separate fields rather than one
// declaration so that reading the signature cannot reach the body by accident.
//
// name is not sig.Name: an impl method is registered under the qualified
// `Type.method`, while the signature keeps the name as it was declared.
type functionType struct {
	name            string
	sig             ast.FunctionSignature
	params          []Type
	borrowParams    []bool
	mutBorrowParams []bool
	returnType      Type
	body            *ast.BlockStmt
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
	borrowSource map[string][]string
	// unread holds the locals this scope declared that nothing has read yet. A
	// read removes one; whatever is left when the scope closes was never needed.
	// Parameters are not in here: they are part of a signature, and a caller can
	// require one the body has no use for.
	unread map[string]ast.Span
}

// New creates an empty type checker.
func New() *Checker {
	return &Checker{
		functions:        map[string]*functionType{},
		structs:          map[string]*ast.StructDecl{},
		enums:            map[string]*enumType{},
		errorSets:        map[string]*errorSetType{},
		unions:           map[string]*unionType{},
		contracts:        map[string]*contractType{},
		impls:            map[string]map[string]*functionType{},
		declaredTypes:    map[string]bool{},
		checkedStdBodies: map[string]bool{},
	}
}

// Check validates the program and returns the first type error.
func (c *Checker) Check(program *ast.Program) error {
	c.stdMethods = stdmethod.IndexMethods(program.Decls)
	c.deinitOwners = ast.DeinitOwners(program)
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
	c.stdMethods = stdmethod.IndexMethods(program.Decls)
	c.deinitOwners = ast.DeinitOwners(program)
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
	case *ast.ErrorSetDecl:
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

// requireObligationDoc requires a `///` comment where an obligation is created.
// What the obligation says cannot be written in code, so a comment is the only
// place it can live; the compiler cannot judge what is written there, but it
// can tell that nothing was.
func requireObligationDoc(requiresUnsafe bool, doc string, subject string, want string) error {
	if !requiresUnsafe || doc != "" {
		return nil
	}
	return errorf("unsafe error: `%s` needs a `///` comment stating %s"+
		"\nhelp: write `/// <%s>` above the declaration", subject, want, want)
}

// checkPublicStructFields validates public fields on one struct.
func (c *Checker) checkPublicStructFields(decl *ast.StructDecl) error {
	for _, field := range decl.Fields {
		if !field.Public {
			continue
		}
		// An `unsafe struct` keeps every field private. That is what pins the
		// code able to break its invariant to this one file: Kizu modules are
		// one file and do not nest their privacy (SPEC §6.6).
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

// sortedMethodNames lists a contract's methods in a stable order, so a type that
// misses two of them is always told about the same one first.
func sortedMethodNames(methods map[string]*functionType) []string {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// collectEnum registers and validates a tag enum declaration.
func (c *Checker) collectEnum(decl *ast.EnumDecl) error {
	if err := c.rejectDuplicateTypeName(decl.Name); err != nil {
		return err
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

// collectErrorSet registers and validates an error set declaration.
func (c *Checker) collectErrorSet(decl *ast.ErrorSetDecl) error {
	if err := c.rejectDuplicateTypeName(decl.Name); err != nil {
		return err
	}
	set := &errorSetType{name: decl.Name, members: map[string]bool{}, public: decl.Public}
	for _, member := range decl.Members {
		if set.members[member] {
			return errorf("type error: duplicate error `%s::%s`", decl.Name, member)
		}
		set.members[member] = true
	}
	set.tagged = &enumType{name: set.name, tags: set.members, public: set.public}
	c.errorSets[decl.Name] = set
	return nil
}

// rejectDuplicateTypeName reports a name already taken by another declaration.
// Every declaration asks here, so a name is taken whichever kind claimed it
// first: checking only the kinds that came before leaves the answer depending
// on the order two declarations were written in.
func (c *Checker) rejectDuplicateTypeName(name string) error {
	if _, exists := c.errorSets[name]; exists {
		return errorf("type error: duplicate type `%s`", name)
	}
	if _, exists := c.enums[name]; exists {
		return errorf("type error: duplicate type `%s`", name)
	}
	if _, exists := c.structs[name]; exists {
		return errorf("type error: duplicate type `%s`", name)
	}
	if _, exists := c.unions[name]; exists {
		return errorf("type error: duplicate type `%s`", name)
	}
	return nil
}

// collectUnion registers and validates a tagged union declaration.
func (c *Checker) collectUnion(decl *ast.UnionDecl) error {
	if err := c.rejectDuplicateTypeName(decl.Name); err != nil {
		return err
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
		if variant.Payload != nil {
			payload := typ.Text(variant.Payload)
			parsed, err := c.parseType(payload)
			if err != nil {
				return err
			}
			if err := checkBorrowFieldPolicy(decl.Name, variant.Name, payload); err != nil {
				return err
			}
			if containsTypeValue(parsed) {
				return errorf("type error: union variant `%s::%s` cannot store type value",
					decl.Name, variant.Name)
			}
			if containsDynType(parsed) {
				return errorf("type error: union variant `%s::%s` cannot store dyn value",
					decl.Name, variant.Name)
			}
		}
		union.variants[variant.Name] = typ.Text(variant.Payload)
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
	previousTypeParams := c.typeParams
	c.typeParams = typeParamSet(decl.TypeParams)
	defer func() {
		c.typeParams = previousTypeParams
	}()
	for _, field := range decl.Fields {
		typ, err := c.parseTypeNode(field.TypeName)
		if err != nil {
			return err
		}
		if err := checkStructFieldBorrowPolicy(decl, field); err != nil {
			return err
		}
		if err := checkRawPointerFieldPolicy(decl, field, typ); err != nil {
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
// inline tagged-union payload ABI.
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
			if c.typeContainsOwner(typ.Text(field.TypeName), visited) {
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
		if variant.Payload == nil {
			continue
		}
		if c.typeContainsOwner(typ.Text(variant.Payload), map[string]bool{decl.Name: true}) {
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
		return errorf(
			"type error: owner-payload union `%s` requires explicit `deinit(self: %s) -> void`",
			decl.Name, decl.Name)
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
		if variant.Payload == nil ||
			!c.typeContainsOwner(typ.Text(variant.Payload), map[string]bool{decl.Name: true}) {
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
					"via `%s.deinit()` or `%s.deinit_all()`",
				decl.Name, variant.Name, arm.Binding, arm.Binding)
		}
	}
	return nil
}

// ownerUnionSelfMatch returns the `match self { ... }` statement only when it is
// the first executable statement of the deinit body. Requiring it first keeps the
// shape simple and guarantees the active-variant cleanup always runs: a
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
// call `<binding>.deinit()`. Only the direct form is accepted so the
// active payload is always cleaned without path-sensitive analysis of the arm.
func matchArmCleansPayload(body ast.Statement, binding string) bool {
	expr, ok := body.(*ast.ExprStmt)
	if !ok {
		return false
	}
	return ownerUnionDeinitCall(expr.Expr, binding)
}

// ownerUnionDeinitCall reports whether expr is the cleanup call
// `binding.deinit()` or `binding.deinit_all()`. Which of the two the payload
// type accepts is enforced where the arm body is checked.
func ownerUnionDeinitCall(expr ast.Expression, binding string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace || !typ.CleanupMethod(field.Name) {
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
	typeParams := fn.TypeParamNames()
	previousTypeParams := c.typeParams
	c.typeParams = typeParamSet(typeParams)
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
	if fn.ReturnType != nil {
		var err error
		ret, err = c.parseTypeNode(fn.ReturnType)
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
func checkStaticParamPolicy(fn ast.FunctionSignature) error {
	for _, param := range fn.StaticParams {
		if Type(typ.Text(param.Type)) != typeFunction || fn.Std {
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
func checkReturnBorrowPolicy(fn ast.FunctionSignature) error {
	if fn.ReturnType == nil {
		if len(fn.ReturnBorrows) != 0 {
			return errorf("type error: function `%s` `borrows` requires return type", fn.Name)
		}
		return nil
	}
	if len(fn.ReturnBorrows) == 0 {
		if isBorrowReturnType(Type(typ.Text(fn.ReturnType))) {
			return errorf(
				"type error: function `%s` borrow return requires `borrows <source>`",
				fn.Name)
		}
		return nil
	}
	if !isBorrowedViewReturnType(Type(typ.Text(fn.ReturnType))) {
		return errorf("type error: function `%s` `borrows` requires borrowed view return",
			fn.Name)
	}
	seen := map[string]bool{}
	for _, source := range fn.ReturnBorrows {
		if seen[source] {
			return errorf("type error: function `%s` borrows source `%s` twice",
				fn.Name, source)
		}
		seen[source] = true
		if !returnBorrowSourceDeclared(fn, source) {
			return errorf("type error: function `%s` borrows unknown source `%s`",
				fn.Name, source)
		}
	}
	return nil
}

// returnBorrowSourceDeclared reports whether a borrow source names a parameter.
func returnBorrowSourceDeclared(fn ast.FunctionSignature, source string) bool {
	if borrowReturnParamIndex(fn, source) >= 0 {
		return true
	}
	// A contract writes no receiver but every method has one, so `self` names it
	// there. A method declared with a receiver slot has it among its parameters
	// already, which is why this only speaks for the ones that do not.
	return source == receiverName && !fn.Receiver
}

// receiverName is what a method calls the value it is called on.
const receiverName = "self"

// checkStructFieldBorrowPolicy rejects borrow fields until a non-lifetime model exists.
func checkStructFieldBorrowPolicy(decl *ast.StructDecl, field ast.Field) error {
	if !field.Borrow {
		return nil
	}
	return errorf("type error: borrow field `%s.%s` cannot store borrow",
		decl.Name, field.Name)
}

// checkRawPointerFieldPolicy rejects a raw pointer field on a struct that has
// not said it carries an invariant the compiler cannot check.
func checkRawPointerFieldPolicy(decl *ast.StructDecl, field ast.Field, fieldType Type) error {
	if decl.RequiresUnsafe || !containsRawPointer(fieldType) {
		return nil
	}
	return errorf("unsafe error: struct `%s` holds a raw pointer in field `%s`, "+
		"so it must be declared `unsafe struct`"+
		"\nhelp: write `unsafe struct %s` and document the invariant its fields carry",
		decl.Name, field.Name, decl.Name)
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
	return c.parseTypeNode(parsed)
}

// parseTypeNode validates a type the parser already read, which is every type a
// declaration writes. Only a type the compiler itself spells still arrives as
// text, and parseType is the entry for those.
func (c *Checker) parseTypeNode(parsed typ.Type) (Type, error) {
	if parsed == nil {
		return "", errorf("type error: missing type")
	}
	name := parsed.String()
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
	if c.typeParams[name] {
		return typ, nil
	}
	if !c.isTypeName(name) {
		return "", errorf("type error: unknown type `%s`", name)
	}
	return typ, nil
}

// isTypeName reports whether a name is a type in this program: one the compiler
// provides or one the program declared. Type parameters are not asked about
// here, since they are scoped to a single declaration rather than to the
// program. Every caller asks the same question, so a name that is a type for
// one of them is a type for all of them.
func (c *Checker) isTypeName(name string) bool {
	if knownTypes[Type(name)] || c.declaredTypes[name] || isKnownGenericBase(name) {
		return true
	}
	return c.structs[name] != nil || c.enums[name] != nil ||
		c.unions[name] != nil || c.errorSets[name] != nil
}

// parseErrorUnionType validates `!T` and the typed `Error!T` spelling.
func (c *Checker) parseErrorUnionType(name string, node *typ.ErrorUnion) (Type, error) {
	if node.Err != nil {
		errName, err := c.parseType(node.Err.String())
		if err != nil {
			return "", err
		}
		if c.errorSets[string(errName)] == nil {
			return "", errorf(
				"type error: the error of `E!T` must be an `error` set, got `%s`",
				errName)
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

	if !isKnownGenericBase(base) {
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

// parseMapType validates the symbol-table map spelling.
func (c *Checker) parseMapType(name string, args []string) (Type, error) {
	if len(args) != 2 {
		return "", errorf("type error: std::map::Map expects 2 static arguments")
	}
	if !sameType(Type(args[0]), typeByteString) && !c.typeParams[args[0]] {
		return "", errorf("type error: std::map::Map key type must be []u8")
	}
	if _, err := c.parseType(args[1]); err != nil {
		return "", err
	}
	if !c.typeParams[args[1]] && !c.isCopyType(Type(args[1])) {
		return "", errorf("type error: std::map::Map value type must be copy")
	}
	return Type(name), nil
}

// isKnownGenericBase reports whether base names a generic type the compiler
// provides. parseGenericType gives each one its own argument rules; this answers
// the prior question of whether the spelling is a type at all, which is also
// what stops a function from taking it.
func isKnownGenericBase(base string) bool {
	switch base {
	case "std::mem::Box", "std::map::Map", "ptr",
		"std::arena::Arena", "std::arena::Handle", "option", "std::array::Array":
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

// referencedTypeNames returns the names a type spelling mentions. The names come
// from the parsed structure, so every wrapper is seen however they nest: reading
// the names off the text instead stopped at the first wrapper it recognized, and
// `&[]Secret` answered `[]Secret`, a name no declaration can have.
func referencedTypeNames(typeName string) []string {
	parsed, err := typ.Parse(typeName)
	if err != nil {
		return []string{typeName}
	}
	var names []string
	typ.Walk(parsed, func(node typ.Type) {
		if name, ok := node.(*typ.Name); ok {
			names = append(names, strings.Join(name.Path, "::"))
		}
	})
	return names
}

// isUserDeclaredType reports whether name is declared by the current program.
func (c *Checker) isUserDeclaredType(name string) bool {
	if c.structs[name] != nil {
		return true
	}
	if c.enums[name] != nil {
		return true
	}
	if c.errorSets[name] != nil {
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
	if set := c.errorSets[name]; set != nil {
		return set.public
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
	if fn.name != "main" {
		return nil
	}
	returned := strings.TrimSpace(typ.Text(fn.sig.ReturnType))
	if returned == "" || returned == "void" || strings.HasSuffix(returned, "!void") {
		return nil
	}
	return errorf("type error: `main` returns `%s`, expected `void` or `!void`", returned)
}

// defineStaticValueParams puts the compile-time values a `<...>` list declares
// into scope, and returns them by declared type. A body reads them like any
// other name, and a static argument list needs to tell them apart from a
// runtime local, so both callers set up a generic body through here.
func defineStaticValueParams(env *scope, sig ast.FunctionSignature) (map[string]Type, error) {
	staticParams := map[string]Type{}
	for _, param := range sig.StaticParams {
		if param.IsType() {
			continue
		}
		if err := env.defineParam(param.Name, Type(typ.Text(param.Type)), false, false); err != nil {
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
	staticParams, err := defineStaticValueParams(env, fn.sig)
	if err != nil {
		return err
	}
	for idx, param := range fn.sig.Params {
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
	c.currentStd = fn.sig.Std
	c.typeParams = typeParamSet(fn.sig.TypeParamNames())
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
			return true, env.checkAllRead()
		}
	}
	return false, env.checkAllRead()
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
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env, wantReturn, unsafe)
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
	if !ok || !typ.CleanupMethod(field.Name) {
		return errorf("type error: %s expects cleanup method call", keyword)
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
	if containsTypeValue(typ) {
		return false, errorf("type error: type value cannot be stored in local `%s`", stmt.Name)
	}
	if _, mutable, inner, ok := explicitBorrowType(typ); ok {
		sources := c.exprBorrowSourceList(stmt.Value, env, unsafe)
		return false, env.defineParamWithSource(stmt.Name, inner, true, mutable, sources)
	}
	if isBorrowedViewReturnType(typ) {
		sources := c.exprBorrowSourceList(stmt.Value, env, unsafe)
		if len(sources) > 0 {
			return false, env.defineWithSource(stmt.Name, typ, stmt.Mutable, sources)
		}
	}
	return false, env.define(stmt.Name, typ, stmt.Mutable)
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
		return true, env.defineParamWithSource(stmt.Name, typ, true, mutable, sources)
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
		sources := c.exprBorrowSourceList(stmt.Value, env, unsafe)
		if len(sources) == 0 {
			sources = []string{stmt.Name}
		}
		return true, env.defineParamWithSource(stmt.Name, typ, true, mutable, sources)
	}
	typ, mutable, ok, err = c.checkBoxBorrowInitializer(stmt.Value, env, unsafe)
	if ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, env.defineParam(stmt.Name, typ, true, mutable)
	}
	if sources, ok, err := c.checkStringViewInitializer(stmt.Value, env, unsafe); ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, env.defineParamWithSource(stmt.Name, typeByteString, true, false, sources)
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
	unsafe unsafeMark,
) ([]string, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "as_bytes" {
		return nil, false, nil
	}
	receiver, err := c.checkExpr(field.Receiver, env, unsafe)
	if err != nil {
		return nil, true, err
	}
	if receiver != "std::string::String" {
		return nil, true, errorf("type error: `String.as_bytes` expects String receiver")
	}
	if len(call.Args) != 0 {
		return nil, true, errorf("type error: `String.as_bytes` expects 0 args, got %d",
			len(call.Args))
	}
	sources := c.exprBorrowSourceList(field.Receiver, env, unsafe)
	return sources, true, nil
}

// checkArrayBorrowInitializer recognizes try array.at/at_mut(index) local borrows.
func (c *Checker) checkArrayBorrowInitializer(
	expr ast.Expression,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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

// absorbsErrorUnion reports whether returning a result that fails one way from a
// function that declares no error set is the same absorption `try` does. A
// declared `E!T` is not this, because it named the one set it accepts.
func absorbsErrorUnion(want Type, got Type) bool {
	return typ.AbsorbsErrorSet(string(want), string(got))
}

// checkErrorUnionReturn accepts success or error payloads for !T returns.
func (c *Checker) checkErrorUnionReturn(
	expr ast.Expression,
	env *scope,
	want Type,
	got Type,
	unsafe unsafeMark,
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
	if absorbsErrorUnion(want, got) {
		return true, nil
	}
	if errorType, elem, ok := errorUnionParts(want); ok {
		// `!T` declares no error set, so it accepts a member of any of them,
		// the same way it accepts a `try` from any set (ADR-0087).
		if errorType == "" && c.errorSets[string(got)] != nil {
			return true, nil
		}
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
	return c.currentFunction != nil &&
		slices.Contains(c.currentFunction.sig.ReturnBorrows, ident.Name)
}

// checkReturnBorrowSources rejects returned views not tied to a declared source.
// Every provenance candidate of the returned value must be declared, and at
// least one must tie the value to the declaration: the caller keeps exactly the
// declared sources alive, so an undeclared candidate would dangle.
func (c *Checker) checkReturnBorrowSources(
	expr ast.Expression,
	env *scope,
	_ Type,
	unsafe unsafeMark,
) error {
	if c.currentFunction == nil || len(c.currentFunction.sig.ReturnBorrows) == 0 {
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
	declared := c.currentFunction.sig.ReturnBorrows
	if len(sources) == 0 {
		return errorf("type error: return borrows `%s` but returned value is not tied to that source",
			strings.Join(declared, ", "))
	}
	// The reported candidate is the lexicographically first undeclared one, so
	// the diagnostic is deterministic without sorting on the success path.
	undeclared := ""
	for candidate := range sources {
		if slices.Contains(declared, candidate) {
			continue
		}
		if undeclared == "" || candidate < undeclared {
			undeclared = candidate
		}
	}
	if undeclared != "" {
		return errorf(
			"type error: returned value may be tied to `%s`, which `borrows %s` does not declare",
			undeclared, strings.Join(declared, ", "))
	}
	return nil
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
		next, ok := env.lookupBorrowSource(source)
		if !ok {
			roots[source] = true
			continue
		}
		for _, n := range next {
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
	return c.returnBorrowSourceUnion(fn.name, fn.sig, env, unsafe,
		func(idx int) ast.Expression {
			if idx >= len(expr.Args) {
				return nil
			}
			return expr.Args[idx]
		})
}

// returnBorrowSourceUnion unions the provenance of every declared borrow
// source's argument — the conservative rule of ADR-0095. argAt maps a
// parameter index to its call expression and returns nil when the call has no
// expression in that slot.
func (c *Checker) returnBorrowSourceUnion(
	name string,
	sig ast.FunctionSignature,
	env *scope,
	unsafe unsafeMark,
	argAt func(idx int) ast.Expression,
) (map[string]bool, error) {
	union := map[string]bool{}
	for _, source := range sig.ReturnBorrows {
		var arg ast.Expression
		if idx := borrowReturnParamIndex(sig, source); idx >= 0 {
			arg = argAt(idx)
		}
		if arg == nil {
			return map[string]bool{}, errorf("type error: `%s` borrows unknown source `%s`",
				name, source)
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

// borrowReturnParamIndex finds the parameter a return-borrow source names.
func borrowReturnParamIndex(sig ast.FunctionSignature, source string) int {
	for idx, param := range sig.Params {
		if param.Name == source {
			return idx
		}
	}
	return -1
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
		if len(method.sig.ReturnBorrows) == 0 {
			return map[string]bool{}, true, nil
		}
		// A method's params start with self, so index 0 is the receiver
		// expression and the rest offset into the call arguments.
		union, err := c.returnBorrowSourceUnion(method.name, method.sig, env, unsafe,
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
	case "as_bytes", "borrow", "borrow_mut", "at", "at_mut":
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
	case "std::internal::builtin::box_borrow", "std::internal::builtin::box_borrow_mut",
		"std::internal::builtin::array_at", "std::internal::builtin::array_at_mut":
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
	return Type(borrowWrappedType(field.Borrow, field.MutBorrow, typ.Text(field.TypeName)))
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
	unsafe unsafeMark,
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
	if err := child.define(stmt.Name, typeI64, false); err != nil {
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
	if tagged := c.taggedType(valueType); tagged != nil {
		return c.checkMatchArms(stmt.Arms, tagged, nil, env, wantReturn, unsafe)
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
	unsafe unsafeMark,
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
func (c *Checker) checkExpr(expr ast.Expression, env *scope, unsafe unsafeMark) (Type, error) {
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
	case *ast.UnsafeExpr:
		return c.checkUnsafeExpr(e, env, unsafe)
	case *ast.IndexExpr:
		return c.checkIndexExpr(e, env, unsafe)
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
	unsafe unsafeMark,
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
func (c *Checker) checkIfExpr(stmt *ast.IfStmt, env *scope, unsafe unsafeMark) (Type, error) {
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
	tagged := c.taggedType(valueType)
	unionType := c.unions[string(valueType)]
	if tagged == nil && unionType == nil {
		return "", errorf("type error: match expects enum or union, got %s", valueType)
	}
	return c.checkMatchExprArms(stmt.Arms, tagged, unionType, env, unsafe)
}

// checkMatchExprArms validates match expression arms and returns their common type.
func (c *Checker) checkMatchExprArms(
	arms []ast.MatchArm,
	enumType *enumType,
	unionType *unionType,
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
		if err := armEnv.define(arm.Binding, Type(payload), false); err != nil {
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
	default:
		return "", errorf("type error: unsupported literal %T", expr)
	}
}

// checkContextualExpr validates an expression and narrows integer literals to want.
func (c *Checker) checkContextualExpr(
	expr ast.Expression,
	want Type,
	env *scope,
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	// `!T` declares no error set, so it propagates whatever the body fails
	// with, which is what lets a function call things that fail in different
	// ways without naming every one. A declared `E!T` accepts only E.
	if targetError != "" && sourceError != targetError {
		return "", errorf("type error: try cannot propagate %s from %s", sourceError, source)
	}
	return Type(elem), nil
}

// checkCallExpr validates builtin and user function calls.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope, unsafe unsafeMark) (Type, error) {
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
	if c.ownerElemContainer(elem) {
		return errorf(
			"type error: Array element `%s` is an owner-element container; wrap it in a struct with deinit",
			elem)
	}
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
	if _, ok := dynContract(typ); ok {
		return errorf("type error: Array element cannot be dyn")
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
	return name, c.instantiateTypeArgText(expr.TypeArg), nil
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
	if c.ownerType(elem) {
		return "", errorf(
			"type error: Arena cannot hold owner values; `%s` needs a deinit Arena never runs",
			elem)
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
	if name == "std::internal::builtin::arena" {
		typ, err := c.checkArenaTypeApply(typeArg, args, env, unsafe)
		return typ, true, err
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
	if c.ownerElemContainer(elem) {
		return "", true, errorf(
			"type error: Box payload `%s` is an owner-element container; wrap it in a struct with deinit",
			elem)
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
		"std::internal::builtin::array_deinit",
		"std::internal::builtin::array_truncate",
		"std::internal::builtin::array_clear",
		"std::internal::builtin::array_as_bytes":
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
		return c.checkArrayPrimitiveGet(elem, name, args, env, unsafe)
	case "deinit":
		// The raw primitive frees only the buffer, with no owner-element rule:
		// it is the one escape `Array.deinit_all` uses after consuming the
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
		return Type("!" + string(elem)), nil
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
	for idx, param := range fn.sig.TypeParamNames() {
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
	for idx, param := range fn.sig.StaticParams {
		arg := strings.TrimSpace(argsText[idx])
		if param.IsType() {
			typeArgs = append(typeArgs, arg)
			continue
		}
		if err := c.checkStaticValueArg(name, param, arg); err != nil {
			return nil, err
		}
	}
	return typeArgs, nil
}

// checkStaticValueArg validates one compile-time value argument. The value is a
// literal or, for a `Function` parameter, a top-level function name. A generic
// may also pass on a static parameter of its own, which is how one wrapper
// forwards to another; the caller of the outer generic checked the real value.
func (c *Checker) checkStaticValueArg(name string, param ast.StaticParam, arg string) error {
	if param.Type != nil && c.staticParams[arg] == Type(typ.Text(param.Type)) {
		return nil
	}
	switch Type(typ.Text(param.Type)) {
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
	staticParams, err := defineStaticValueParams(env, fn.sig)
	if err != nil {
		return err
	}
	for idx, param := range fn.sig.Params {
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
	c.currentStd = fn.sig.Std
	c.typeParams = typeParamSet(fn.sig.TypeParamNames())
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
	returns, err := c.checkBlock(fn.body, env, returnType, nil)
	if err != nil {
		return err
	}
	if returnType != typeVoid && !returns {
		return errorf("type error: function `%s` must return %s", fn.name, returnType)
	}
	return nil
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
	got, err := c.checkContextualExpr(checkedArg, want, env, unsafe)
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
			return Type(typ.Text(field.TypeName)), decl, nil
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
	unsafe unsafeMark,
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

// checkMethodReceiverPath enforces the direct field receiver boundary.
func (c *Checker) checkMethodReceiverPath(field *ast.FieldExpr, env *scope) error {
	receiver, ok := field.Receiver.(*ast.FieldExpr)
	if !ok {
		return nil
	}
	if _, ok := receiver.Receiver.(*ast.IdentExpr); !ok {
		return errorf("type error: field method receiver only supports one direct field")
	}
	if !typ.CleanupMethod(field.Name) || c.allowsDirectFieldCleanup(receiver, env) {
		return nil
	}
	return errorf(
		"type error: field cleanup `%s.%s` is only allowed inside owner deinit",
		receiver.String(), field.Name,
	)
}

// allowsDirectFieldCleanup reports whether owner.field.deinit is in owner deinit.
func (c *Checker) allowsDirectFieldCleanup(field *ast.FieldExpr, env *scope) bool {
	fn := c.currentFunction
	if fn == nil || stdmethod.CallName(fn.sig.Name) != "deinit" || fn.returnType != typeVoid {
		return false
	}
	owner, ok := field.Receiver.(*ast.IdentExpr)
	if !ok || len(fn.params) == 0 || len(fn.sig.Params) == 0 {
		return false
	}
	ownerType, ok := env.lookup(owner.Name)
	if !ok || fn.sig.Params[0].Name != owner.Name {
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
	unsafe unsafeMark,
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
	case "deinit", "deinit_all":
		if _, ok := field.Receiver.(*ast.IdentExpr); !ok &&
			!c.directFieldCleanupReceiver(field.Receiver, env) {
			return "", errorf("type error: `Box.%s` requires local Box receiver", field.Name)
		}
		if err := c.cleanupChoiceError("Box", "payload", field.Name, elem); err != nil {
			return "", err
		}
		return c.checkStdMethod("std::mem::Box", []Type{elem}, "Box", field.Name,
			args, env, unsafe)
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
	unsafe unsafeMark,
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
	unsafe unsafeMark,
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
	// leak (ADR-0091), so the element type picks between deinit and deinit_all.
	switch name {
	case "at", "at_mut":
		return "", errorf("type error: `Array.%s` must be bound with `let name = try array.%s(...)`",
			name, name)
	case "get", "get_or_panic":
		if !c.isCopyType(elem) {
			return "", errorf("type error: `Array.%s` requires copy element", name)
		}
	case "deinit", "deinit_all":
		if err := c.cleanupChoiceError("Array", "elements", name, elem); err != nil {
			return "", err
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
// it in std/src/*.kizu, with the receiver's static arguments substituted in.
func (c *Checker) checkStdMethod(
	receiver string,
	typeArgs []Type,
	label string,
	name string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	method, ok := c.stdMethods[receiver][name]
	if !ok {
		return "", errorf("type error: %s has no method `%s`", label, name)
	}
	if err := c.checkStdMethodBody(method, typeArgs); err != nil {
		return "", err
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

// checkStdMethodBody checks the std wrapper a receiver call resolved to, with
// this receiver's static arguments bound.
//
// A generic body is checked when a call instantiates it (ADR-0066), and no call
// instantiates these: a container method is matched against the signature std
// declares and lowered from the method name. That left the body unchecked, so
// `return std::internal::builtin::array_apend<T>(self, value)` -- or anything else -- sat
// in std reading like the implementation while meaning nothing.
func (c *Checker) checkStdMethodBody(method stdmethod.Method, typeArgs []Type) error {
	fn := c.functions[method.Sig.Name]
	if fn == nil || fn.body == nil || len(method.TypeParams) != len(typeArgs) {
		return nil
	}
	key := method.Sig.Name + "<" + joinTypes(typeArgs) + ">"
	if c.checkedStdBodies[key] {
		return nil
	}
	// Recorded before checking, so a body that reaches its own method through
	// another wrapper stops here instead of recurring.
	c.checkedStdBodies[key] = true
	subst := make(map[string]Type, len(typeArgs))
	for idx, param := range method.TypeParams {
		subst[param] = typeArgs[idx]
	}
	return c.checkGenericInstantiation(fn, subst)
}

// joinTypes renders static arguments for an instantiation key.
func joinTypes(types []Type) string {
	parts := make([]string, 0, len(types))
	for _, typ := range types {
		parts = append(parts, string(typ))
	}
	return strings.Join(parts, ", ")
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
	if _, err := c.parseMapType(fmt.Sprintf("std::map::Map<%s>", arg), args); err != nil {
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
	if !c.isCopyType(args[1]) {
		return errorf("type error: std::map::Map value type must be copy")
	}
	return nil
}

// checkDynMethodCall validates a method call through &dyn Contract.
func (c *Checker) checkDynMethodCall(
	contractName string,
	name string,
	span ast.Span,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
) (Type, error) {
	contract := c.contracts[contractName]
	if contract == nil || contract.methods[name] == nil {
		return "", errorf("type error: `dyn %s` has no method `%s`", contractName, name)
	}
	// A contract writes no receiver, so every parameter it declares is an argument.
	return c.checkCallableArgs(contract.methods[name], 0, span, args, env, unsafe)
}

// checkMethodArgs validates method-call arguments after the implicit self receiver.
func (c *Checker) checkMethodArgs(
	method *functionType,
	receiver Type,
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
	return c.checkCallableArgs(method, 1, span, args, env, unsafe)
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

// checkBorrowTargetShape restricts explicit borrows to direct locals or one field.
func checkBorrowTargetShape(expr ast.Expression) error {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		return nil
	case *ast.FieldExpr:
		if _, ok := target.Receiver.(*ast.IdentExpr); ok {
			return nil
		}
		return errorf("type error: field borrow only supports one direct field")
	default:
		return errorf("type error: borrow target must be a local binding or direct field")
	}
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

// checkArenaGet validates std::arena::Arena<T>.get(std::arena::Handle<T>).
func (c *Checker) checkArenaGet(
	arg string,
	args []ast.Expression,
	env *scope,
	unsafe unsafeMark,
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

// ownerType reports whether values of typ carry a deinit contract (ADR-0091).
func (c *Checker) ownerType(typ Type) bool {
	return ast.OwnerType(c.deinitOwners, string(typ))
}

// ownerElemContainer reports whether typ is a container of owner elements,
// the class whose only cleanup is `deinit_all()`. Such a container cannot
// itself be a container element: `deinit_all` consumes each element through the
// element's own `deinit()`, which owner-element containers reject. The class
// has one definition, ast.CleanupMethodName.
func (c *Checker) ownerElemContainer(typ Type) bool {
	return ast.CleanupMethodName(string(typ), c.deinitOwners) == "deinit_all"
}

// cleanupChoiceError rejects the cleanup name an element type does not accept
// (ADR-0091): shallow `deinit` leaks owner contents, `deinit_all` requires
// them. contents names what the container holds ("elements" or "payload").
func (c *Checker) cleanupChoiceError(
	container string,
	contents string,
	name string,
	elem Type,
) error {
	switch name {
	case "deinit":
		if c.ownerType(elem) {
			return errorf(
				"type error: `%s.deinit` would leak the owner %s `%s`; use `%s.deinit_all`",
				container, contents, elem, container)
		}
	case "deinit_all":
		if !c.ownerType(elem) {
			return errorf(
				"type error: `%s.deinit_all` is for owner %s; `%s` needs no cleanup, use `%s.deinit`",
				container, contents, elem, container)
		}
	}
	return nil
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
	return copyTypes[typ]
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

// containsRawPointer reports whether a type spelling mentions ptr<T> anywhere,
// including behind `?`, `[]`, and static type arguments.
func containsRawPointer(typ Type) bool {
	return containsWrappedType(typ, isPointerType)
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

// satisfies reports whether a type has the methods a contract asks for. Nothing
// has to be declared: a contract names a shape, and a type either has it or does
// not, which is what lets one be satisfied by a type its author never saw.
func (c *Checker) satisfies(contractName string, typ Type) bool {
	return c.checkSatisfies(contractName, typ) == nil
}

// methodMatches checks a method against the contract method it stands for. The
// contract writes no receiver, so the comparison starts after the method's own.
func methodMatches(want *functionType, got *functionType) bool {
	if len(got.params) == 0 {
		return false
	}
	gotParams := got.params[1:]
	if len(want.params) != len(gotParams) || !sameType(want.returnType, got.returnType) {
		return false
	}
	for idx, wantParam := range want.params {
		if !sameType(wantParam, gotParams[idx]) ||
			want.borrowParams[idx] != got.borrowParams[idx+1] ||
			want.mutBorrowParams[idx] != got.mutBorrowParams[idx+1] {
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
func errorUnionParts(union Type) (string, string, bool) {
	return typ.ErrorUnionParts(string(union))
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

// newScope creates a lexical type scope.
func newScope(parent *scope) *scope {
	return &scope{
		parent: parent, values: map[string]Type{}, mutable: map[string]bool{},
		borrowed: map[string]bool{}, mutBorrow: map[string]bool{},
		borrowSource: map[string][]string{}, unread: map[string]ast.Span{},
	}
}

// child creates a nested lexical type scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// declareLocal records a binding this scope will be asked about. `_` is the
// name for a value that is deliberately dropped, so it is never asked about.
func (s *scope) declareLocal(name string, span ast.Span) {
	if name == discardName {
		return
	}
	s.unread[name] = span
}

// discardName is written where a value is produced on purpose and not kept.
const discardName = "_"

// checkAllRead reports a local this scope declared and nothing read.
func (s *scope) checkAllRead() error {
	names := make([]string, 0, len(s.unread))
	for name := range s.unread {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		return errorAtCode(s.unread[name], "type.unused_local",
			"type error: local `%s` is never read"+
				"\nhelp: remove it, or write `let _ = ...` to drop the value on purpose", name)
	}
	return nil
}

// define binds a local name to a type in the current scope.
func (s *scope) define(name string, typ Type, mutable bool) error {
	if name == discardName {
		// `_` is where a value is dropped, not a name anything can read. Binding
		// it twice in one scope is two drops, not a redeclaration.
		return nil
	}
	if _, exists := s.values[name]; exists {
		return errorf("type error: duplicate variable `%s`", name)
	}
	s.values[name] = typ
	s.mutable[name] = mutable
	return nil
}

// defineWithSource binds a non-borrow local while preserving view provenance.
func (s *scope) defineWithSource(name string, typ Type, mutable bool, sources []string) error {
	if err := s.define(name, typ, mutable); err != nil {
		return err
	}
	if len(sources) > 0 {
		s.borrowSource[name] = sources
	}
	return nil
}

// defineParam binds a function parameter and records borrow capabilities.
func (s *scope) defineParam(name string, typ Type, borrowed bool, mutBorrow bool) error {
	var sources []string
	if borrowed || isBorrowedViewReturnType(typ) {
		sources = []string{name}
	}
	return s.defineParamWithSource(name, typ, borrowed, mutBorrow, sources)
}

// defineParamWithSource binds a borrowed local and records its source owners.
func (s *scope) defineParamWithSource(
	name string,
	typ Type,
	borrowed bool,
	mutBorrow bool,
	sources []string,
) error {
	if err := s.define(name, typ, false); err != nil {
		return err
	}
	s.borrowed[name] = borrowed
	s.mutBorrow[name] = mutBorrow
	if len(sources) > 0 {
		s.borrowSource[name] = sources
	}
	return nil
}

// lookup resolves a local name by walking parent scopes.
func (s *scope) lookup(name string) (Type, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if typ, ok := cur.values[name]; ok {
			delete(cur.unread, name)
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

// lookupBorrowSource resolves the provenance sources for a borrowed local.
func (s *scope) lookupBorrowSource(name string) ([]string, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			sources := cur.borrowSource[name]
			return sources, len(sources) > 0
		}
	}
	return nil, false
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
