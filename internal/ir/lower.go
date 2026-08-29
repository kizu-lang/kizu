package ir

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/quote"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/stdprim"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Lower converts a checked Kizu AST into typed SSA IR.
func Lower(program *ast.Program, ownershipResult ownership.Result) (*Module, error) {
	l := newLowerer(program, ownershipResult)
	module, err := l.lower()
	if err != nil {
		return nil, err
	}
	if err := verifyWithTypes(module, l.types); err != nil {
		return nil, err
	}
	return module, nil
}

type lowerer struct {
	program    *ast.Program
	module     *Module
	types      *typ.Table
	signatures map[string]Signature
	// methodSigs groups the signatures by the method name they end with, so a
	// method call reaches the handful of methods that could answer it instead
	// of every declaration in the program.
	methodSigs  map[string][]Signature
	current     *Function
	block       *Block
	env         *env
	nextValue   int
	nextBlock   int
	loops       []*loopContext
	deferFrames [][]Cleanup
	// currentModule is the package module whose body is being lowered, for the
	// one body whose symbol does not carry it: a test block's. A function
	// pointer's value is looked up through the prefix of what is being
	// lowered, and `test.<name>` has none.
	currentModule string
	// typeBindings binds the type parameters in force: those of the generic
	// function being instantiated. Lowering reads a body once per binding rather
	// than rewriting its AST, so `T` resolves through here.
	typeBindings map[string]string
	// instantiated records the generic instances already requested, keyed by
	// the symbol they were given.
	instantiated map[string]bool
	pending      []genericInstance
	// staticValues binds the compile-time values of the instance being lowered.
	staticValues map[string]staticValue
	// slots names the locals of the function being lowered that live in memory
	// because something writes through a `&var` borrow of them, and the `&var`
	// parameters that arrive as such storage. Their entry in env is the
	// storage, not the value.
	slots map[string]bool
	// genericDecls indexes the program's generic function declarations by
	// name, so every call site resolves in one lookup.
	genericDecls map[string]*ast.FunctionDecl
	// structDecls indexes struct declarations, which is what a `comptime for`
	// walks. The lowered Struct carries no visibility, and the loop lists
	// public fields only.
	structDecls map[string]*ast.StructDecl
	// enumDecls and unionDecls index the sum declarations, which is what
	// `std::meta::variants` walks and what a `comptime match` builds its arms
	// from. The lowered Enum and Union key their members by name; only the
	// declarations keep the source order a walk has to emit in.
	enumDecls  map[string]*ast.EnumDecl
	unionDecls map[string]*ast.UnionDecl
	// metaFields binds the captures of the `comptime for` expansions currently
	// being lowered.
	metaFields map[string]metaField
	// externSymbols maps a resolved extern "c" function name to its C symbol.
	// Module resolution qualifies every declared name, but the linker knows the
	// declaration's own identifier, so calls emit that instead.
	externSymbols map[string]string
	// nextErrorCode is the next global code for an error set the program
	// declares itself; std members keep the codes std assigns.
	nextErrorCode int
	// deinitOwners names the types that carry a deinit contract, seeded from
	// ast.DeinitOwners so lowering reads the same owner-ness the checkers do.
	deinitOwners map[string]bool
	// releaseAllocators names the types whose deinit takes an allocator, so a
	// generic cleanup lowers the call the element actually declares.
	releaseAllocators map[string]bool
	// ownership is the preceding phase's output. Keeping it beside the syntax
	// tree makes the phase boundary explicit and leaves AST nodes immutable.
	ownership ownership.Result
}

// genericInstance is one generic function with its static parameters bound.
type genericInstance struct {
	decl     *ast.FunctionDecl
	bindings map[string]string
	values   map[string]staticValue
	symbol   string
}

// staticValue is one compile-time value bound to a static parameter.
type staticValue struct {
	typ  string
	text string
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
	env   *env
}

// A loopPhi is a header phi and the name it carries around the loop.
type loopPhi struct {
	name string
	phi  *Instr
}

// newLowerer prepares lookup tables used during lowering.
func newLowerer(program *ast.Program, ownershipResult ownership.Result) *lowerer {
	generics := map[string]*ast.FunctionDecl{}
	structs := map[string]*ast.StructDecl{}
	enums := map[string]*ast.EnumDecl{}
	unions := map[string]*ast.UnionDecl{}
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FunctionDecl); ok && len(fn.StaticParams) > 0 {
			generics[fn.Name] = fn
		}
		switch typed := decl.(type) {
		case *ast.StructDecl:
			structs[typed.Name] = typed
		case *ast.EnumDecl:
			enums[typed.Name] = typed
		case *ast.UnionDecl:
			unions[typed.Name] = typed
		}
	}
	return &lowerer{
		program: program,
		types:   typ.NewTable(),
		module: &Module{
			Structs:   map[string]Struct{},
			Enums:     map[string]Enum{},
			ErrorSets: map[string]Enum{},
			Unions:    map[string]Union{},
		},
		signatures:        map[string]Signature{},
		typeBindings:      map[string]string{},
		instantiated:      map[string]bool{},
		staticValues:      map[string]staticValue{},
		externSymbols:     map[string]string{},
		genericDecls:      generics,
		structDecls:       structs,
		enumDecls:         enums,
		unionDecls:        unions,
		metaFields:        map[string]metaField{},
		deinitOwners:      ast.DeinitOwners(program),
		releaseAllocators: ast.ReleaseNamesAllocator(program),
		ownership:         ownershipResult,
	}
}

// resolveType binds the type parameters in force, wherever they stand. A
// parameter inside a static argument list counts: `std::array::Array<T>` is the
// receiver of every Array method, and lowering it as written would leave the
// instance carrying the parameter it was instantiated away from.
func (l *lowerer) resolveType(name string) string {
	// A `std::meta` form written where a type goes resolves first: it names a
	// type through the capture in force rather than being one (ADR-0113).
	name = l.resolveMetaTypeDeep(name)
	if bound, ok := l.typeBindings[name]; ok {
		return bound
	}
	if len(l.typeBindings) == 0 {
		return name
	}
	resolved, err := typ.SubstituteText(name, l.typeBindings)
	if err != nil {
		return name
	}
	return resolved
}

// resolveTypeArgs binds the type parameters in force across a static argument
// list. A list is not a type, so each entry is resolved on its own.
func (l *lowerer) resolveTypeArgs(list string) string {
	args := splitStaticArgs(list)
	if len(args) == 0 {
		return l.resolveType(list)
	}
	for idx, arg := range args {
		args[idx] = l.resolveType(arg)
	}
	return strings.Join(args, ", ")
}

// requestGenericInstance records that one generic function is needed for one
// type argument, and returns the symbol it will be lowered under together with
// the signature the caller sees. Lowering happens after the current function
// finishes, so an instantiation never interrupts the function that asked for it.
func (l *lowerer) requestGenericInstance(name string, typeArgs string) (string, Signature, error) {
	decl, instance, err := l.genericBindings(name, typeArgs)
	if err != nil {
		return "", Signature{}, err
	}
	symbol := genericInstanceName(name, instance.order, instance.bindings, instance.values)
	if !l.instantiated[symbol] {
		l.instantiated[symbol] = true
		l.pending = append(l.pending, genericInstance{
			decl: decl, bindings: instance.bindings, values: instance.values, symbol: symbol,
		})
	}
	// The signature the caller sees resolves the forms the declaration wrote,
	// so a `-> std::meta::field_type<T, f>` result arrives as the field's type.
	restore, err := l.bindInstanceFields(decl, instance.bindings, instance.values)
	if err != nil {
		return "", Signature{}, err
	}
	signature := l.instanceSignature(decl.FunctionSignature, instance.bindings)
	restore()
	return symbol, signature, nil
}

// genericArguments is what one generic declaration's static parameters are bound
// to for one call: the type arguments, the compile-time values, and the order
// they were declared in, which is what names the instance.
type genericArguments struct {
	order    []string
	bindings map[string]string
	values   map[string]staticValue
}

// genericBindings binds one generic declaration's static parameters to the
// arguments of one call. Every reader of an instantiation -- the request that
// lowers its body and the caller that hands arguments to it -- reads this, so
// they cannot disagree about what the instance is.
func (l *lowerer) genericBindings(
	name string,
	typeArgs string,
) (*ast.FunctionDecl, genericArguments, error) {
	decl := l.genericDecl(name)
	if decl == nil {
		return nil, genericArguments{}, fmt.Errorf("ir error: `%s` is not a generic function", name)
	}
	args, err := typ.SplitArgs(typeArgs)
	if err != nil {
		return nil, genericArguments{}, fmt.Errorf("ir error: `%s`: %w", name, err)
	}
	if len(args) != len(decl.StaticParams) {
		return nil, genericArguments{}, fmt.Errorf(
			"ir error: `%s` takes %d static parameters, got %d",
			name, len(decl.StaticParams), len(args))
	}
	instance := genericArguments{
		order:    make([]string, 0, len(decl.StaticParams)),
		bindings: map[string]string{},
		values:   map[string]staticValue{},
	}
	for i, param := range decl.StaticParams {
		instance.order = append(instance.order, param.Name)
		if param.IsType() {
			// Resolve first: inside `fn twice<T>` a call to `wrap<T>` needs the
			// argument T is currently bound to, not the parameter name.
			instance.bindings[param.Name] = l.resolveType(args[i])
			continue
		}
		// A compile-time value reaches the body as a constant, or -- for a
		// `Function` parameter -- as the name of the function to forward to.
		instance.values[param.Name] = staticValue{
			typ: typ.Text(param.Type), text: l.resolveStaticValue(args[i]),
		}
	}
	return decl, instance, nil
}

// declaredInstanceParams returns the parameters a generic declaration has at
// these type arguments, without asking for the instance. A callee that lowers to
// an instruction of its own never has its body called, so the caller needs the
// types it hands over but not the instantiation.
func (l *lowerer) declaredInstanceParams(name string, typeArgs string) ([]Param, error) {
	decl, instance, err := l.genericBindings(name, typeArgs)
	if err != nil {
		return nil, err
	}
	return l.instanceSignature(decl.FunctionSignature, instance.bindings).Params, nil
}

// instanceSignature returns the signature a generic declaration has once its
// type arguments are bound. The caller sees the instance's types, not the
// declaration's: `!T` has to come back as `!i64`, and a `u8` parameter has to be
// handed a u8, or the call carries a parameter that no longer exists.
func (l *lowerer) instanceSignature(
	sig ast.FunctionSignature,
	bindings map[string]string,
) Signature {
	previous := l.typeBindings
	l.typeBindings = bindings
	defer func() { l.typeBindings = previous }()
	return l.lowerSignature(sig)
}

// lowerPendingGenerics lowers every requested instantiation, including any an
// instantiation itself requests.
func (l *lowerer) lowerPendingGenerics() error {
	for len(l.pending) > 0 {
		next := l.pending[0]
		l.pending = l.pending[1:]
		l.typeBindings = next.bindings
		l.staticValues = next.values
		// A `Field` static argument names a field of the type bound alongside
		// it, and the body reads it through the `std::meta` forms written
		// against the parameter. Binding it here is what makes each field its
		// own instance rather than one generic body.
		restore, err := l.bindInstanceFields(next.decl, next.bindings, next.values)
		if err != nil {
			return err
		}
		lowered, err := l.lowerFunctionNamed(next.decl, next.symbol)
		restore()
		l.typeBindings = map[string]string{}
		l.staticValues = map[string]staticValue{}
		if err != nil {
			return err
		}
		l.module.Functions = append(l.module.Functions, lowered)
	}
	return nil
}

// bindInstanceFields binds the `Field` static arguments of one instance to the
// fields they name, and returns the call that unbinds them. The owner is the
// type argument declared before the parameter, the pairing
// `std::meta::field_type<T, f>` is written with.
func (l *lowerer) bindInstanceFields(
	decl *ast.FunctionDecl,
	bindings map[string]string,
	values map[string]staticValue,
) (func(), error) {
	bound := map[string]metaField{}
	owner := ""
	for _, param := range decl.StaticParams {
		if param.IsType() {
			owner = bindings[param.Name]
			continue
		}
		if typ.Text(param.Type) != "Field" {
			continue
		}
		fields, err := l.publicFields(owner)
		if err != nil {
			return nil, err
		}
		name := values[param.Name].text
		found := false
		for _, field := range fields {
			if field.name == name {
				bound[param.Name] = field
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("ir error: `%s` has no public field `%s`", owner, name)
		}
	}
	previous := make(map[string]metaField, len(bound))
	had := make(map[string]bool, len(bound))
	for name, field := range bound {
		previous[name], had[name] = l.metaFields[name]
		l.metaFields[name] = field
	}
	return func() {
		for name := range bound {
			if had[name] {
				l.metaFields[name] = previous[name]
				continue
			}
			delete(l.metaFields, name)
		}
	}, nil
}

// genericDecl returns the generic function declaration with this IR name.
func (l *lowerer) genericDecl(name string) *ast.FunctionDecl {
	return l.genericDecls[name]
}

// genericInstanceName is the symbol one instantiation is lowered under. Static
// parameters are listed in declaration order so the symbol is stable.
func genericInstanceName(
	name string,
	order []string,
	bindings map[string]string,
	values map[string]staticValue,
) string {
	parts := make([]string, 0, len(order)+1)
	parts = append(parts, name)
	for _, param := range order {
		if bound, ok := bindings[param]; ok {
			parts = append(parts, encodeStaticArg(bound))
			continue
		}
		parts = append(parts, encodeStaticArg(values[param].text))
	}
	return strings.Join(parts, ".")
}

// encodeStaticArg spells one static argument in the character set every backend
// accepts, so a lowered name needs no further mangling on the way out. A byte a
// source identifier can hold passes through, which keeps `i64` and `4096`
// readable; every other byte is escaped, so two different arguments cannot
// collide on one symbol.
func encodeStaticArg(arg string) string {
	var out strings.Builder
	for _, ch := range []byte(arg) {
		switch {
		case ch == '_':
			out.WriteString("__")
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9'):
			out.WriteByte(ch)
		default:
			fmt.Fprintf(&out, "_%02x", ch)
		}
	}
	return out.String()
}

// resolveStaticValue forwards a static value through an outer binding, so a
// generic passing its own static value on keeps the caller's argument.
func (l *lowerer) resolveStaticValue(text string) string {
	if bound, ok := l.staticValues[text]; ok {
		return bound.text
	}
	return text
}

// lower performs declaration collection and function lowering.
func (l *lowerer) lower() (*Module, error) {
	if err := l.collectDecls(); err != nil {
		return nil, err
	}
	for _, decl := range l.program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if fn.ExternABI != "" {
			continue
		}
		if len(fn.StaticParams) > 0 {
			continue
		}
		lowered, err := l.lowerFunction(fn)
		if err != nil {
			return nil, err
		}
		l.module.Functions = append(l.module.Functions, lowered)
	}
	if err := l.lowerTests(); err != nil {
		return nil, err
	}
	if err := l.lowerPendingGenerics(); err != nil {
		return nil, err
	}
	return l.module, nil
}

// lowerTests lowers each `test "name" { ... }` block into a function, so a
// test runs through the same lowering as the rest of the program.
func (l *lowerer) lowerTests() error {
	for _, decl := range l.program.Decls {
		test, ok := decl.(*ast.TestDecl)
		if !ok {
			continue
		}
		// A test body may `try`, so it lowers as a function returning `!void`.
		fn := &ast.FunctionDecl{
			FunctionSignature: ast.FunctionSignature{
				Name:       TestFunctionName(test.Name),
				ReturnType: &typ.ErrorUnion{Ok: &typ.Name{Path: []string{"void"}}},
			},
			Body: test.Body,
		}
		previousModule := l.currentModule
		l.currentModule = test.Module
		lowered, err := l.lowerFunctionNamed(fn, fn.Name)
		l.currentModule = previousModule
		if err != nil {
			return err
		}
		l.module.Functions = append(l.module.Functions, lowered)
	}
	return nil
}

// TestFunctionName returns the IR symbol a test block lowers to.
func TestFunctionName(name string) string {
	var out strings.Builder
	out.WriteString("test.")
	for _, r := range name {
		if r == '_' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			continue
		}
		out.WriteByte('_')
	}
	return out.String()
}

// TestFunctionNames returns the lowered test symbols in declaration order.
func TestFunctionNames(module *Module) []string {
	names := []string{}
	for _, fn := range module.Functions {
		if strings.HasPrefix(fn.Name, "test.") {
			names = append(names, fn.Name)
		}
	}
	return names
}

// collectDecls records signatures and struct layouts.
func (l *lowerer) collectDecls() error {
	var combined []*ast.ErrorSetDecl
	for _, decl := range l.program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			l.module.Structs[d.Name] = lowerStruct(d)
		case *ast.EnumDecl:
			l.module.Enums[d.Name] = lowerEnum(d)
		case *ast.ErrorSetDecl:
			if len(d.Combines) > 0 {
				combined = append(combined, d)
				continue
			}
			set, err := l.lowerErrorSet(d)
			if err != nil {
				return err
			}
			l.module.ErrorSets[d.Name] = set
		case *ast.UnionDecl:
			l.module.Unions[d.Name] = lowerUnion(d)
		}
	}
	if err := l.lowerCombinedErrorSets(combined); err != nil {
		return err
	}
	for _, decl := range l.program.Decls {
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			l.signatures[fn.Name] = l.lowerSignature(fn.FunctionSignature)
			if fn.ExternABI == "c" {
				l.externSymbols[fn.Name] = externCSymbol(fn.Name)
			}
		}
	}
	l.indexMethodSignatures()
	return nil
}

// indexMethodSignatures groups every declared method under its method name.
func (l *lowerer) indexMethodSignatures() {
	l.methodSigs = make(map[string][]Signature, len(l.signatures))
	for name, sig := range l.signatures {
		if _, method, ok := stdmethod.SplitMethodName(name); ok {
			l.methodSigs[method] = append(l.methodSigs[method], sig)
		}
	}
	// A generic method has no lowered signature until its instance is
	// requested, which happens at the call the slot analysis runs before. Its
	// declaration already says whether the receiver arrives as the binding's
	// storage, so the analysis reads that rather than instantiating to ask.
	for name, decl := range l.genericDecls {
		method, ok := genericMethodName(name)
		if !ok || len(decl.Params) == 0 || !decl.Params[0].MutBorrow {
			continue
		}
		l.methodSigs[method] = append(l.methodSigs[method],
			Signature{Params: []Param{{Passing: PassCallerStorage}}})
	}
}

// genericMethodName returns the method a generic declaration name ends with.
func genericMethodName(name string) (string, bool) {
	_, method, ok := stdmethod.SplitMethodName(name)
	return method, ok
}

// externCSymbol strips the module qualification a resolver added to an extern
// "c" declaration. The C symbol is the identifier the declaration was written
// with; the qualified spelling exists only inside this compiler.
func externCSymbol(name string) string {
	if index := strings.LastIndex(name, "::"); index >= 0 {
		return name[index+2:]
	}
	return name
}

// lowerUnion converts an AST union declaration to IR metadata.
func lowerUnion(decl *ast.UnionDecl) Union {
	variants := map[string]UnionVariant{}
	for index, variant := range decl.Variants {
		variants[variant.Name] = UnionVariant{
			Name: variant.Name, Index: index,
			Payload: stdmeta.ResolveElementTypeForms(typ.Text(variant.Payload)),
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

// lowerErrorSet converts an AST error set to IR metadata. An error value is
// one integer, globally unique across every set: std members keep the codes
// std assigns, and sets a program declares take the codes after them. A
// failure crossing from one union into another is therefore a copy, never a
// conversion.
func (l *lowerer) lowerErrorSet(decl *ast.ErrorSetDecl) (Enum, error) {
	stdSets, err := project.StdErrorSets()
	if err != nil {
		return Enum{}, err
	}
	if codes, ok := stdSets[decl.Name]; ok {
		return Enum{Name: decl.Name, Tags: codes}, nil
	}
	if l.nextErrorCode == 0 {
		base, err := project.StdErrorCodeBase()
		if err != nil {
			return Enum{}, err
		}
		l.nextErrorCode = base
	}
	members := map[string]int{}
	for _, member := range decl.Members {
		members[member] = l.nextErrorCode
		l.nextErrorCode++
	}
	return Enum{Name: decl.Name, Tags: members}, nil
}

// lowerCombinedErrorSets records the `error C = A or B;` declarations once
// every declaring set is filed. A combined set keeps no codes of its own: it
// lists the `{ }`-form sets its members come from, resolved through any
// combined sets it names, so its member set is a code-set union and nothing
// is renumbered (ADR-0127).
func (l *lowerer) lowerCombinedErrorSets(decls []*ast.ErrorSetDecl) error {
	pending := map[string]*ast.ErrorSetDecl{}
	for _, decl := range decls {
		pending[decl.Name] = decl
	}
	var resolve func(decl *ast.ErrorSetDecl) error
	resolve = func(decl *ast.ErrorSetDecl) error {
		delete(pending, decl.Name)
		seen := map[string]bool{}
		var origins []string
		for _, ref := range decl.Combines {
			if next, ok := pending[ref]; ok {
				if err := resolve(next); err != nil {
					return err
				}
			}
			origin, ok := l.module.ErrorSets[ref]
			if !ok {
				return fmt.Errorf("ir error: `%s` combines unknown error set `%s`",
					decl.Name, ref)
			}
			contributed := origin.Origins
			if contributed == nil {
				contributed = []string{ref}
			}
			for _, name := range contributed {
				if seen[name] {
					continue
				}
				seen[name] = true
				origins = append(origins, name)
			}
		}
		l.module.ErrorSets[decl.Name] = Enum{Name: decl.Name, Origins: origins}
		return nil
	}
	for _, decl := range decls {
		if _, open := pending[decl.Name]; !open {
			continue
		}
		if err := resolve(decl); err != nil {
			return err
		}
	}
	return nil
}

// ErrorSetCodes returns the member codes a set carries: its own for a
// declaring set, its origins' for a combined one. Backends ask here so the
// subset checks `try` and `return` generalized to (ADR-0127) cannot drift.
func ErrorSetCodes(module *Module, name string) (map[int]bool, bool) {
	set, ok := module.ErrorSets[name]
	if !ok {
		return nil, false
	}
	codes := map[int]bool{}
	for _, code := range set.Tags {
		codes[code] = true
	}
	for _, origin := range set.Origins {
		for _, code := range module.ErrorSets[origin].Tags {
			codes[code] = true
		}
	}
	return codes, true
}

// lowerStruct converts an AST struct declaration to IR metadata.
func lowerStruct(decl *ast.StructDecl) Struct {
	fields := make([]Field, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		fields = append(fields, Field{
			Name: field.Name,
			Type: stdmeta.ResolveElementTypeForms(typ.Text(field.TypeName)),
		})
	}
	return Struct{Name: decl.Name, Fields: fields}
}

// lowerSignature extracts the callable type of a function declaration.
func (l *lowerer) lowerSignature(sig ast.FunctionSignature) Signature {
	params := make([]Param, 0, len(sig.Params))
	for _, param := range sig.Params {
		params = append(params, l.lowerParam(param))
	}
	returned := l.lowerReturnType(l.resolveType(typ.Text(sig.ReturnType)))
	return Signature{Params: params, Return: returned, Unsafe: sig.RequiresUnsafe}
}

// lowerFunction lowers one function into SSA blocks.
func (l *lowerer) lowerFunction(fn *ast.FunctionDecl) (*Function, error) {
	return l.lowerFunctionNamed(fn, fn.Name)
}

// lowerFunctionNamed lowers one function using an explicit IR symbol name. The
// function it builds wears the signature callers were given, rather than a
// second reading of the same declaration: a body and its callers disagreeing
// about what it takes is the one thing a call site cannot see.
func (l *lowerer) lowerFunctionNamed(fn *ast.FunctionDecl, name string) (*Function, error) {
	signature := l.lowerSignature(fn.FunctionSignature)
	l.current = &Function{Name: name, Params: signature.Params, Return: signature.Return}
	l.env = newEnv()
	slots, err := l.mutablyBorrowedLocals(fn)
	if err != nil {
		return nil, err
	}
	l.slots = slots
	l.nextValue = 0
	l.nextBlock = 0
	l.loops = nil
	l.deferFrames = nil
	valueSlots := make([]string, 0, len(fn.Params))
	for index, param := range fn.Params {
		l.env.set(param.Name, signature.Params[index].Value())
		// A parameter that arrives as the caller's storage is a storage name
		// like a lent local: reads load, assignments store, and address
		// consumers take the pointer itself.
		if signature.Params[index].Passing == PassCallerStorage {
			l.slots[param.Name] = true
			continue
		}
		// One that arrives as a value and is written through needs storage of
		// its own, which is the copy it was handed. One that arrives as an
		// address already reaches storage, and it is the caller's: wrapping it
		// again would hand the callee a borrow of a borrow.
		if l.slots[param.Name] && !isReferenceType(signature.Params[index].Type) {
			valueSlots = append(valueSlots, param.Name)
		}
	}
	l.block = l.newBlock("entry")
	for _, name := range valueSlots {
		value, ok := l.env.get(name)
		if !ok {
			continue
		}
		l.env.set(name, l.emit("local.slot", "&var "+value.Type, []Value{value}, ""))
	}
	if err := l.lowerBlock(fn.Body); err != nil {
		return nil, err
	}
	if l.block.Terminator.Op == "" {
		// A body that runs off its end returns what a written `return;` returns,
		// which for `!void` is a wrapped success rather than a bare void.
		l.block.Terminator = Terminator{Op: "return", Value: l.returnVoidValue()}
	}
	return l.current, nil
}

// lowerParam gives a parameter the type the callee sees and how the call hands
// it over. These are one decision, made here: what the callee receives and what
// the caller has to prepare are two readings of the same fact, and asking them
// separately is how they come apart.
//
// A `&var` parameter is the caller's own storage, because a write through it has
// to land there rather than in a copy. A `&` parameter cannot write, so a copy
// is the same observation and stays the cheaper shape -- except for unions,
// where matching needs an address and the copy is made for the call.
func (l *lowerer) lowerParam(param ast.Param) Param {
	typeName := l.resolveType(typ.Text(param.TypeName))
	lowered := Param{Name: "%" + param.Name, Type: typeName, Passing: PassValue}
	if param.Borrow || param.MutBorrow {
		lowered.Type, lowered.Passing = l.borrowIRType(typeName, param.MutBorrow)
	}
	return lowered
}

// borrowIRType decides how a borrow of elem travels: what it is spelled as, and
// how it is handed over. A parameter and a return ask the same question, so
// they get the same answer -- a borrow that reaches a callee as a value and
// comes back as a pointer is a borrow with two meanings.
func (l *lowerer) borrowIRType(elem string, mutable bool) (string, Passing) {
	if mutable {
		// A mutable slice borrow writes through the view's own pointer, never
		// into the caller's binding — element writes only, no re-pointing
		// (ADR-0096) — so the fat pointer itself is the storage and travels
		// flat, exactly like a shared slice view.
		if elem == "[]u8" {
			return elem, PassValue
		}
		return "&var " + elem, PassCallerStorage
	}
	if _, ok := l.module.Unions[elem]; ok {
		return "&" + elem, PassCopyAddress
	}
	// A borrow of an owner reaches the value the caller has, not a copy of
	// it. An owner is where a container header lives, and the header is the
	// storage it describes (ADR-0131): a copy of one stops seeing what the
	// original goes on to own, so the two answer differently the moment
	// anything writes through the original -- `b.show(b.put(v))` would show
	// the value from before the put. Copy data has no such interior, so a
	// borrow of it travels flat.
	if ast.OwnerType(l.deinitOwners, elem) {
		return "&" + elem, PassCopyAddress
	}
	return elem, PassValue
}

// lowerReturnType gives a function's result the type it travels as, so a
// returned borrow follows the same rule a borrowed parameter does.
func (l *lowerer) lowerReturnType(name string) string {
	parsed, err := l.types.Parse(name)
	if err != nil {
		return returnType(name)
	}
	borrow, ok := parsed.(*typ.Borrow)
	if !ok {
		return returnType(name)
	}
	spelling, _ := l.borrowIRType(borrow.Elem.String(), borrow.Mut)
	return returnType(spelling)
}

// scopedBinding remembers what a name meant before a block rebound it.
type scopedBinding struct {
	name  string
	value Value
	bound bool
}

// scopeBlockBindings makes the declarations written directly in block local to
// it and returns a function that puts the enclosing bindings back. Nested blocks
// scope themselves, so only this level is collected.
//
// Without this, a `let` inside a loop body stayed in the environment after the
// loop, so the next if statement saw two different SSA values for that name --
// one from each sibling loop -- and mergeEnvs built a phi over them. Neither
// value dominates the merge, because either loop can run zero times, so the
// emitted module failed the LLVM verifier.
func (l *lowerer) scopeBlockBindings(block *ast.BlockStmt) func() {
	var saved []scopedBinding
	for _, stmt := range block.Statements {
		declaration, ok := stmt.(*ast.LetStmt)
		if !ok {
			continue
		}
		previous, bound := l.env.get(declaration.Name)
		saved = append(saved, scopedBinding{
			name: declaration.Name, value: previous, bound: bound,
		})
	}
	if len(saved) == 0 {
		return func() {}
	}
	return func() {
		// Reverse order, so a name declared twice in one block ends on the
		// binding that was in scope before either declaration.
		for index := len(saved) - 1; index >= 0; index-- {
			binding := saved[index]
			if binding.bound {
				l.env.set(binding.name, binding.value)
				continue
			}
			l.env.remove(binding.name)
		}
	}
}

// lowerBlock lowers statements into the current block.
func (l *lowerer) lowerBlock(block *ast.BlockStmt) error {
	_, err := l.lowerBlockBody(block, false)
	return err
}

// statementValue returns the expression a statement produces when it stands in
// value position, and reports whether it has one. An expression written without
// a semicolon is the value of what it is written in, which is what separates a
// block that gives a value from one that only runs.
//
// `if` and `match` are both a statement and an expression, so in value position
// they stand on their own rather than inside an ExprStmt. Reading only ExprStmt
// rejects an else branch that is itself an if, which is how anything with three
// cases gets written.
func statementValue(stmt ast.Statement) (ast.Expression, bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if s.Semicolon {
			return nil, false
		}
		return s.Expr, true
	case *ast.IfStmt:
		return s, true
	case *ast.MatchStmt:
		return s, true
	default:
		return nil, false
	}
}

// trailingExpr returns the expression a block ends with, and reports whether it
// has one.
func trailingExpr(block *ast.BlockStmt) (ast.Expression, bool) {
	if block == nil || len(block.Statements) == 0 {
		return nil, false
	}
	return statementValue(block.Statements[len(block.Statements)-1])
}

// lowerBlockBody lowers the statements of a block, and its trailing expression
// as a value when one is wanted.
func (l *lowerer) lowerBlockBody(block *ast.BlockStmt, wantValue bool) (Value, error) {
	frame := l.pushDeferFrame()
	restoreBindings := l.scopeBlockBindings(block)
	defer restoreBindings()
	statements := block.Statements
	var trailing ast.Expression
	if wantValue {
		expr, ok := trailingExpr(block)
		if !ok {
			l.popDeferFrame()
			return Value{}, fmt.Errorf(
				"ir error: a branch used as a value must end in an expression")
		}
		trailing = expr
		statements = statements[:len(statements)-1]
	}
	for _, stmt := range statements {
		if l.block.Terminator.Op != "" {
			l.popDeferFrame()
			return Value{}, nil
		}
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if err := l.lowerDeferStmt(deferStmt); err != nil {
				l.popDeferFrame()
				return Value{}, err
			}
			continue
		}
		if errDeferStmt, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := l.lowerErrDeferStmt(errDeferStmt); err != nil {
				l.popDeferFrame()
				return Value{}, err
			}
			continue
		}
		if err := l.lowerStmt(stmt); err != nil {
			l.popDeferFrame()
			return Value{}, err
		}
	}
	var value Value
	if trailing != nil && l.block.Terminator.Op == "" {
		lowered, err := l.lowerExpr(trailing)
		if err != nil {
			l.popDeferFrame()
			return Value{}, err
		}
		value = lowered
	}
	if l.block.Terminator.Op == "" {
		l.emitCleanupFrame(frame)
	}
	l.popDeferFrame()
	return value, nil
}

// lowerStmt lowers one statement.
func (l *lowerer) lowerStmt(stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return l.lowerLetStmt(s)
	case *ast.AssignStmt:
		value, err := l.lowerContextualExpr(s.Value, l.assignTargetType(s.Target))
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
	default:
		return l.lowerBodyStmt(stmt)
	}
}

// lowerBodyStmt lowers the statements that carry a body of their own, and the
// branches that leave one.
func (l *lowerer) lowerBodyStmt(stmt ast.Statement) error {
	switch s := stmt.(type) {
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
	case *ast.BlockStmt:
		// Only a match arm body is a bare block statement (SPEC §6.12).
		return l.lowerBlock(s)
	case *ast.ComptimeIfStmt:
		return l.lowerComptimeIfStmt(s)
	case *ast.ComptimeForStmt:
		return l.lowerComptimeForStmt(s)
	case *ast.ComptimeMatchStmt:
		return l.lowerComptimeMatchStmt(s)
	default:
		return fmt.Errorf("ir error: unsupported statement %T", stmt)
	}
}

// lowerLetStmt binds one declaration to the value it was written with.
func (l *lowerer) lowerLetStmt(stmt *ast.LetStmt) error {
	value, err := l.lowerExpr(stmt.Value)
	if err != nil {
		return err
	}
	l.bindLocal(stmt.Name, value)
	return nil
}

// assignTargetType returns the declared type an assignment writes into, read
// without emitting anything so the value can be lowered against it. "" means
// the lowerer cannot name the type from the target alone, and the value keeps
// the type it carries itself.
func (l *lowerer) assignTargetType(target ast.Expression) string {
	switch t := target.(type) {
	case *ast.IdentExpr:
		value, ok := l.env.get(t.Name)
		if !ok {
			return ""
		}
		return derefType(value.Type)
	case *ast.FieldExpr:
		receiver := l.assignTargetType(t.Receiver)
		if receiver == "" {
			return ""
		}
		return l.fieldType(receiver, t.Name)
	case *ast.DerefExpr:
		receiver := l.assignTargetType(t.Receiver)
		if elem, ok := rawPointerElem(receiver); ok {
			return elem
		}
		return receiver
	case *ast.UnsafeExpr:
		return l.assignTargetType(t.Value)
	case *ast.IndexExpr:
		if !t.Slice {
			return "u8"
		}
		return ""
	default:
		return ""
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
	case *ast.UnsafeExpr:
		// The marker says who owns the obligation, not where the store goes.
		return l.lowerAssignTarget(t.Value, value)
	case *ast.IdentExpr:
		if slot, ok := l.slotPointer(t); ok {
			l.emit("ref.store", "void", []Value{slot, value}, "")
			return nil
		}
		l.env.set(t.Name, value)
		return nil
	case *ast.FieldExpr:
		receiver, err := l.lowerReceiverAddress(t.Receiver)
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
		// `.*` through a `&var` parameter names the same storage the bare
		// name does, so both spellings assign through the same rule.
		if ident, ok := t.Receiver.(*ast.IdentExpr); ok && l.isStorageParam(ident.Name) {
			return l.lowerAssignTarget(ident, value)
		}
		receiver, err := l.lowerExpr(t.Receiver)
		if err != nil {
			return err
		}
		if _, raw := rawPointerElem(receiver.Type); !raw && !isReferenceType(receiver.Type) {
			return fmt.Errorf(
				"ir error: dereference assignment target `%s` is not a borrow or raw pointer",
				target.String())
		}
		l.emit("ref.store", "void", []Value{receiver, value}, "")
		return nil
	case *ast.IndexExpr:
		return l.lowerIndexAssign(t, value)
	default:
		return fmt.Errorf("ir error: unsupported assignment target `%s`", target.String())
	}
}

// lowerIndexAssign stores one byte through a writable slice view. The bounds
// test is emitted here for the same reason lowerIndexExpr emits it: every
// backend inherits one check from one place.
func (l *lowerer) lowerIndexAssign(target *ast.IndexExpr, value Value) error {
	if target.Slice {
		return fmt.Errorf("ir error: unsupported assignment target `%s`", target.String())
	}
	slice, err := l.lowerExpr(target.Target)
	if err != nil {
		return err
	}
	index, err := l.lowerExpr(target.Index)
	if err != nil {
		return err
	}
	length := l.emit("slice.len", "i64", []Value{slice}, "")
	l.condFail(target.Span, "binary.<", index, zeroIndex, "bounds", index, length)
	l.condFail(target.Span, "binary.>=", index, length, "bounds", index, length)
	l.emit("slice.store", "void", []Value{slice, index, value}, "")
	return nil
}

// lowerReturnStmt lowers explicit returns and wraps !T success values.
func (l *lowerer) lowerReturnStmt(stmt *ast.ReturnStmt) error {
	if stmt.Value == nil {
		l.emitNormalCleanups()
		l.block.Terminator = Terminator{Op: "return", Value: l.returnVoidValue()}
		return nil
	}
	value, err := l.lowerContextualExpr(stmt.Value, l.returnValueType())
	if err != nil {
		return err
	}
	errorReturn := l.producesErrorValue(value)
	if _, success, ok := errorUnionParts(l.types, l.current.Return); ok {
		if value.Type == success {
			// A `!void` success carries no payload, so its wrap takes no operand.
			// `return try f();` on a `!void` callee unwraps to a void value, and
			// handing that to error.ok as a payload made the wrap reject its own
			// arity ("error.ok !void expects 0 args") at emit time.
			args := []Value{value}
			if success == "void" {
				args = nil
			}
			value = l.emit("error.ok", l.current.Return, args, "")
		} else if _, isSet := l.module.ErrorSets[value.Type]; isSet {
			value = l.emit("error.error", l.current.Return, []Value{value}, "")
			errorReturn = true
		}
	}
	if errorReturn {
		l.emitErrorCleanups(l.ownership.RetiredErrDefersForReturn(stmt))
	} else {
		l.emitNormalCleanups()
	}
	l.block.Terminator = Terminator{Op: "return", Value: value}
	return nil
}

// producesErrorValue reports whether v was defined by an error.error
// instruction in the current block. Such a return exits through the error path
// and must run errdefer cleanups.
func (l *lowerer) producesErrorValue(v Value) bool {
	for idx := len(l.block.Instrs) - 1; idx >= 0; idx-- {
		if l.block.Instrs[idx].Result.Name == v.Name {
			return l.block.Instrs[idx].Op == "error.error"
		}
	}
	return false
}

// returnValueType returns the type a returned value has to have: the success
// type of a `!T` function, or the return type itself.
func (l *lowerer) returnValueType() string {
	if success, ok := errorUnionSuccessType(l.types, l.current.Return); ok {
		return success
	}
	return l.current.Return
}

// returnVoidValue returns the correct SSA return value for void-like returns.
func (l *lowerer) returnVoidValue() Value {
	if success, ok := errorUnionSuccessType(l.types, l.current.Return); ok && success == "void" {
		return l.emit("error.ok", l.current.Return, nil, "")
	}
	return Value{Name: "void", Type: "void"}
}

// lowerExpr lowers an expression and returns its typed SSA value.
func (l *lowerer) lowerExpr(expr ast.Expression) (Value, error) {
	if inner, ok := ast.MarkerValue(expr); ok {
		// A marker is a claim about the expression, not an operation, so it
		// lowers to whatever it covers.
		return l.lowerExpr(inner)
	}
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.NullExpr:
		return l.lowerLiteralExpr(e)
	case *ast.TypeExpr:
		return l.emitConst("type", e.TypeName), nil
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
	case *ast.IfStmt, *ast.MatchStmt, *ast.OrelseGuardExpr, *ast.CatchGuardExpr:
		return l.lowerBranchingExpr(e)
	case *ast.StructLiteralExpr:
		return l.lowerStructLiteralExpr(e)
	case *ast.BufferLiteralExpr:
		return l.emit("buffer.new", e.TypeText(), nil, ""), nil
	case *ast.FieldExpr, *ast.IndexExpr, *ast.DerefExpr:
		return l.lowerAccessExpr(e)
	default:
		return Value{}, fmt.Errorf("ir error: unsupported expression `%s`", expr.String())
	}
}

// lowerBranchingExpr lowers the expressions that branch: if and match end in
// a phi over what their branches produced, and an orelse guard's null arm
// leaves the function or loop instead of rejoining.
func (l *lowerer) lowerBranchingExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IfStmt:
		return l.lowerIfExpr(e)
	case *ast.MatchStmt:
		return l.lowerMatchExpr(e)
	case *ast.OrelseGuardExpr:
		return l.lowerOrelseGuardExpr(e)
	case *ast.CatchGuardExpr:
		return l.lowerCatchGuardExpr(e)
	default:
		return Value{}, fmt.Errorf("ir error: unsupported expression `%s`", expr.String())
	}
}

// lowerDerefExpr reads what a borrow or raw pointer points at. The write side
// already stored through the borrow while this side handed the borrow itself
// back, so a dereferenced borrow was compared against the value it pointed at.
func (l *lowerer) lowerDerefExpr(expr *ast.DerefExpr) (Value, error) {
	receiver, err := l.lowerExpr(expr.Receiver)
	if err != nil {
		return Value{}, err
	}
	if elem, ok := rawPointerElem(receiver.Type); ok {
		return l.emit("ref.load", elem, []Value{receiver}, ""), nil
	}
	if !isReferenceType(receiver.Type) {
		return receiver, nil
	}
	return l.emit("ref.load", derefType(receiver.Type), []Value{receiver}, ""), nil
}

// lowerAllocatorFrom lowers `mem_allocator_from<T>`. The state stays a borrow:
// the runtime writes the allocator header into the caller's storage, so what
// crosses is the address, not a copy of the state.
func (l *lowerer) lowerAllocatorFrom(state string, args []ast.Expression) (Value, error) {
	params := []Param{{Type: "&var " + state, Passing: PassCallerStorage}}
	values, err := l.lowerCallArgsAs(params, args)
	if err != nil {
		return Value{}, err
	}
	return l.emit(
		"call.std::internal::builtin::mem_allocator_from", "Allocator", values, ""), nil
}

// lowerTaskNew lowers `task_new<T>`. The state stays a borrow: the worker runs
// on its own stack and writes into the cell the caller moved its value into,
// so what crosses is the address, not a copy.
func (l *lowerer) lowerTaskNew(state string, args []ast.Expression) (Value, error) {
	params := []Param{
		{Type: "Io"},
		{Type: "Allocator"},
		{Type: "fn(Io, Allocator, &var " + state + ") -> void"},
		{Type: "&var " + state, Passing: PassCallerStorage},
		{Type: "i64"},
	}
	values, err := l.lowerCallArgsAs(params, args)
	if err != nil {
		return Value{}, err
	}
	return l.emit("call.std::internal::builtin::task_new", "i64", values, ""), nil
}

// lowerTaskSetSpawn lowers an owned worker. The state crosses as a value in
// IR; the hosted runtime ABI places structs behind a pointer, and the runtime
// copies those bytes only after every task allocation has succeeded. On
// failure the generic wrapper still knows A and releases the moved owner.
func (l *lowerer) lowerTaskSetSpawn(state string, args []ast.Expression) (Value, error) {
	params := []Param{
		{Type: "i64"},
		{Type: "Io"},
		{Type: "Allocator"},
		{Type: "fn(Io, Allocator, " + state + ") -> void"},
		{Type: state},
		{Type: "i64"},
	}
	values, err := l.lowerCallArgsAs(params, args)
	if err != nil {
		return Value{}, err
	}
	result := l.emit(
		"call.std::internal::builtin::task_set_spawn",
		"std::io::Error!void",
		values,
		"",
	)
	return l.releaseOwnerOnFailure(result, values[4], values[2])
}

// lowerTypeApplyCall lowers calls whose callee carries a static argument list.
// The std storage constructors lower to one instruction each, so their std
// bodies are never walked. Every other generic call resolves by name.
func (l *lowerer) lowerTypeApplyCall(
	typeApply *ast.TypeApplyExpr,
	args []ast.Expression,
) (Value, error) {
	switch typeApply.Callee.String() {
	// The element type is resolved before it is handed over: it becomes the
	// instruction's immediate, which is what every backend measures the
	// element by, and a spelling that still names a type parameter or a
	// `std::meta` form measures the wrong thing.
	case "std::arena::new":
		return l.lowerArenaConstructor(l.resolveType(typeApply.TypeArg), args)
	case "std::array::new":
		return l.lowerArrayConstructor(l.resolveType(typeApply.TypeArg), args)
	case "std::map::new":
		return l.lowerMapConstructor(l.resolveTypeArgs(typeApply.TypeArg), args)
	case "ptr_from_int", "int_from_ptr":
		return l.lowerPtrIntCast(typeApply.TypeArg, args)
	case "std::internal::builtin::mem_allocator_from":
		return l.lowerAllocatorFrom(l.resolveType(typeApply.TypeArg), args)
	case "std::internal::builtin::task_new":
		return l.lowerTaskNew(l.resolveType(typeApply.TypeArg), args)
	case "std::internal::builtin::task_set_spawn":
		return l.lowerTaskSetSpawn(l.resolveType(typeApply.TypeArg), args)
	}
	if value, ok, err := l.lowerMetaApply(
		typeApply.Callee.String(), typeApply.TypeArg, args,
	); ok || err != nil {
		return value, err
	}
	if name, ok := l.functionCalleeName(typeApply.Callee); ok {
		return l.lowerTypedNamedCallExpr(name, typeApply.TypeArg, args)
	}
	return Value{}, fmt.Errorf("ir error: unsupported callee `%s`", typeApply.String())
}

// lowerPtrBuiltinCall lowers the raw pointer builtins ptr_read, ptr_write,
// volatile_read, and volatile_write. The plain pair are call spellings of the
// deref the checker already proved, so they lower to the same load and store
// the explicit `p.*` forms use. The volatile pair carries its own ops: a
// volatile access is an effect, not a value read, so no pass may drop or
// merge one.
func (l *lowerer) lowerPtrBuiltinCall(expr *ast.CallExpr) (Value, bool, error) {
	ident, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return Value{}, false, nil
	}
	switch ident.Name {
	case "ptr_read":
		value, err := l.lowerPtrLoad(ident.Name, "ref.load", expr.Args)
		return value, true, err
	case "ptr_write":
		value, err := l.lowerPtrStore(ident.Name, "ref.store", expr.Args)
		return value, true, err
	case "volatile_read":
		value, err := l.lowerPtrLoad(ident.Name, "volatile.load", expr.Args)
		return value, true, err
	case "volatile_write":
		value, err := l.lowerPtrStore(ident.Name, "volatile.store", expr.Args)
		return value, true, err
	default:
		return Value{}, false, nil
	}
}

// lowerPtrLoad lowers a raw pointer read builtin to op.
func (l *lowerer) lowerPtrLoad(name string, op string, args []ast.Expression) (Value, error) {
	if len(args) != 1 {
		return Value{}, fmt.Errorf("ir error: %s expects 1 arg", name)
	}
	pointer, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	elem, ok := rawPointerElem(pointer.Type)
	if !ok {
		return Value{}, fmt.Errorf("ir error: %s expects raw pointer, got %s", name, pointer.Type)
	}
	return l.emit(op, elem, []Value{pointer}, ""), nil
}

// lowerPtrStore lowers a raw pointer write builtin to op.
func (l *lowerer) lowerPtrStore(name string, op string, args []ast.Expression) (Value, error) {
	if len(args) != 2 {
		return Value{}, fmt.Errorf("ir error: %s expects 2 args", name)
	}
	pointer, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	elem, ok := rawPointerElem(pointer.Type)
	if !ok {
		return Value{}, fmt.Errorf("ir error: %s expects raw pointer, got %s", name, pointer.Type)
	}
	value, err := l.lowerContextualExpr(args[1], elem)
	if err != nil {
		return Value{}, err
	}
	return l.emit(op, "void", []Value{pointer, value}, ""), nil
}

// lowerPtrIntCast lowers ptr_from_int<ptr<T>>(v) and int_from_ptr<usize>(p).
// Both are casts the checker proved; the backend reads the operand and result
// types to pick the conversion.
func (l *lowerer) lowerPtrIntCast(target string, args []ast.Expression) (Value, error) {
	if len(args) != 1 {
		return Value{}, fmt.Errorf("ir error: pointer-integer cast expects 1 arg")
	}
	value, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	return l.emit("cast", target, []Value{value}, target), nil
}

// lowerAccessExpr lowers field, index, and explicit dereference expressions.
func (l *lowerer) lowerAccessExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.FieldExpr:
		return l.lowerFieldExpr(e)
	case *ast.IndexExpr:
		return l.lowerIndexExpr(e)
	case *ast.DerefExpr:
		return l.lowerDerefExpr(e)
	default:
		return Value{}, fmt.Errorf("ir error: unsupported access `%s`", expr.String())
	}
}

// lowerContextualExpr lowers an expression whose type the position it appears
// in already fixes: a function return, a struct field, a union payload, a
// parameter. An integer literal takes that type there, the way the checker
// already read it, so `fn byte() -> u8 { return 9; }` returns a u8 rather than
// an i64 a backend has to narrow after the fact. want may be "" or any
// non-integer type; the expression then lowers with the type it carries itself.
func (l *lowerer) lowerContextualExpr(expr ast.Expression, want string) (Value, error) {
	want = l.resolveType(want)
	if elem, ok := optionalElemType(want); ok {
		return l.lowerOptionalContextExpr(expr, want, elem)
	}
	// A `&var` context wants the storage itself, not the value read out of it:
	// a `&var` argument and a returned `&var self` both hand over the same
	// pointer the caller lent. This is the one place that decides it; a
	// mutable reference type and PassCallerStorage are the same fact
	// (borrowIRType), which TestLowerParamAgreesWithItself pins.
	if isMutableReferenceType(want) {
		target := borrowTargetExpr(expr)
		if slot, ok := l.slotPointer(target); ok {
			return slot, nil
		}
		if storage, ok := l.lowerFieldStorage(target); ok {
			return storage, nil
		}
	}
	if !narrowsIntegerLiteral(want) {
		value, err := l.lowerExpr(expr)
		if err != nil {
			return Value{}, err
		}
		return l.readBorrowForContext(value, want), nil
	}
	switch e := expr.(type) {
	case *ast.IntExpr:
		return l.emitConst(want, e.Value), nil
	case *ast.PrefixExpr:
		// A negated literal is still a literal in the position it is written
		// in, and `unary.-` takes the type of its operand.
		if _, ok := e.Right.(*ast.IntExpr); ok && e.Operator == "-" {
			value, err := l.lowerContextualExpr(e.Right, want)
			if err != nil {
				return Value{}, err
			}
			return l.emit("unary."+e.Operator, value.Type, []Value{value}, ""), nil
		}
	}
	return l.lowerExpr(expr)
}

// readBorrowForContext reads a borrow's value where the position wants the
// value itself. A borrow-capture binding (`if xs.at_mut(i) |m|`) is a raw
// reference value, and a `&T` parameter travels as a copy (borrowIRType), so
// handing the capture to such a parameter needs the read a slot-backed borrow
// gets in lowerIdentExpr. A union's `&T` spelling wants the copy's address
// (PassCopyAddress); the read produces the value that address is taken of,
// so a mutable capture degrades through the same load.
func (l *lowerer) readBorrowForContext(value Value, want string) Value {
	if want == "" || !isReferenceType(value.Type) || isMutableReferenceType(want) {
		return value
	}
	if !isReferenceType(want) {
		if derefType(value.Type) != want {
			return value
		}
		return l.emit("ref.load", want, []Value{value}, "")
	}
	if !isMutableReferenceType(value.Type) || derefType(value.Type) != derefType(want) {
		return value
	}
	return l.emit("ref.load", derefType(want), []Value{value}, "")
}

// lowerOptionalContextExpr lowers an expression written where a `?T` is
// expected: `null` becomes the empty optional, a plain T wraps, and a value
// that is already optional passes through — the same shape `!T` success takes.
func (l *lowerer) lowerOptionalContextExpr(
	expr ast.Expression,
	want string,
	elem string,
) (Value, error) {
	if _, ok := expr.(*ast.NullExpr); ok {
		return l.emit("opt.null", want, nil, ""), nil
	}
	value, err := l.lowerContextualExpr(expr, elem)
	if err != nil {
		return Value{}, err
	}
	if value.Type != elem {
		// Not the payload: an already-wrapped optional passes through, and so
		// does an error-set member on its way to the enclosing error path.
		return value, nil
	}
	return l.emit("opt.some", want, []Value{value}, ""), nil
}

// optionalElemType returns T for an optional value type `?T`.
func optionalElemType(typeName string) (string, bool) {
	return typ.OptionalElem(typeName)
}

// narrowsIntegerLiteral reports whether an integer literal written in a
// position of this type lowers as that type. i64 is what a literal lowers as
// on its own, so it needs no contextual form.
func narrowsIntegerLiteral(typ string) bool {
	switch typ {
	case "i8", "i16", "i32", "u8", "u16", "u32", "u64", "usize", "isize":
		return true
	default:
		return false
	}
}

// lowerLiteralExpr lowers scalar literals.
func (l *lowerer) lowerLiteralExpr(expr ast.Expression) (Value, error) {
	switch e := expr.(type) {
	case *ast.IntExpr:
		return l.emitConst("i64", e.Value), nil
	case *ast.StringExpr:
		return l.emitConst("[]u8", quote.Bytes(e.Value)), nil
	case *ast.BoolExpr:
		return l.emitConst("bool", e.String()), nil
	case *ast.NullExpr:
		// A `null` with no `?T` context never reaches a type; the contextual
		// path (lowerOptionalContextExpr) is the one that lowers it.
		return Value{}, fmt.Errorf("ir error: `null` needs an optional context")
	default:
		return Value{}, fmt.Errorf("ir error: unsupported literal %T", expr)
	}
}

// lowerIdentExpr lowers a local binding, a static value, or the built-in void.
func (l *lowerer) lowerIdentExpr(expr *ast.IdentExpr) (Value, error) {
	if slot, ok := l.slotPointer(expr); ok {
		return l.emit("ref.load", derefType(slot.Type), []Value{slot}, ""), nil
	}
	value, ok := l.env.get(expr.Name)
	if ok {
		return value, nil
	}
	// A static value is known at this point, so it lowers to the constant it
	// was instantiated with. `Function` is a name, not a value, and is read by
	// the call that forwards it rather than here.
	if static, ok := l.staticValues[expr.Name]; ok && static.typ != "Function" {
		return l.emitConst(static.typ, static.text), nil
	}
	if expr.Name == "void" {
		return Value{Name: "void", Type: "void"}, nil
	}
	// A top-level function's name is a function pointer value. The address is
	// an instruction rather than a constant so the backend names the symbol
	// once, in the one place that knows how symbols are spelled.
	if name, sig, ok := l.functionByValueName(expr.Name); ok {
		return l.emit("func.addr."+name, functionPointerType(sig), nil, ""), nil
	}
	return Value{}, fmt.Errorf("ir error: undefined value `%s`", expr.Name)
}

// functionByValueName resolves the declaration a name used as a value refers
// to. A callee is qualified by the resolver; a name in any other position is
// not, so a module-local function is looked up under the module the reader is
// inside as well as under the name as written.
func (l *lowerer) functionByValueName(name string) (string, Signature, bool) {
	if sig, ok := l.signatures[name]; ok {
		return name, sig, true
	}
	if l.current == nil {
		return "", Signature{}, false
	}
	if prefix := strings.LastIndex(l.current.Name, "::"); prefix >= 0 {
		qualified := l.current.Name[:prefix+2] + name
		sig, ok := l.signatures[qualified]
		return qualified, sig, ok
	}
	if l.currentModule == "" {
		return "", Signature{}, false
	}
	qualified := l.currentModule + "::" + name
	sig, ok := l.signatures[qualified]
	return qualified, sig, ok
}

// functionPointerType spells the type a function's name has as a value.
func functionPointerType(sig Signature) string {
	node := &typ.Func{Unsafe: sig.Unsafe}
	for _, param := range sig.Params {
		parsed, err := typ.Parse(param.Type)
		if err != nil {
			return ""
		}
		node.Params = append(node.Params, parsed)
	}
	result, err := typ.Parse(sig.Return)
	if err != nil {
		return ""
	}
	node.Result = result
	return node.String()
}

// lowerCastExpr lowers an explicit cast as a typed conversion instruction.
func (l *lowerer) lowerCastExpr(expr *ast.CastExpr) (Value, error) {
	value, err := l.lowerExpr(expr.Value)
	if err != nil {
		return Value{}, err
	}
	return l.emit("cast", typ.Text(expr.TargetType), []Value{value}, typ.Text(expr.TargetType)), nil
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
	if expr.Operator == "orelse" {
		return l.lowerOrelseExpr(expr)
	}
	if expr.Operator == "catch" {
		return l.lowerCatchExpr(expr)
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
		return l.lowerTypeApplyCall(typeApply, expr.Args)
	}
	if value, handled, err := l.lowerPtrBuiltinCall(expr); handled || err != nil {
		return value, err
	}
	// A binding shadows a declaration: a name holding a function pointer is
	// called through the pointer it holds.
	if value, handled, err := l.lowerFuncPointerCall(expr); handled || err != nil {
		return value, err
	}
	if name, ok := l.functionCalleeName(expr.Callee); ok {
		return l.lowerNamedCallExpr(name, expr.Args)
	}
	if field, ok := expr.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		return l.lowerMethodCallExpr(field, expr.Args)
	}
	return Value{}, fmt.Errorf("ir error: unsupported callee `%s`", expr.Callee.String())
}

// functionStaticValue answers the function name a `Function` static parameter
// is bound to in the instance being lowered.
func (l *lowerer) functionStaticValue(name string) (string, bool) {
	value, ok := l.staticValues[name]
	if !ok || value.typ != "Function" {
		return "", false
	}
	return value.text, true
}

// lowerFuncPointerCall lowers a call whose callee is a binding holding a
// function pointer, and reports whether the callee is one. The pointer is the
// first argument of `call.indirect`, so the backend reads the callee where it
// reads every other operand.
func (l *lowerer) lowerFuncPointerCall(expr *ast.CallExpr) (Value, bool, error) {
	ident, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return Value{}, false, nil
	}
	callee, ok := l.env.get(ident.Name)
	if !ok {
		return Value{}, false, nil
	}
	node, ok := funcPointerNode(callee.Type)
	if !ok {
		return Value{}, false, nil
	}
	args := []Value{callee}
	for _, raw := range expr.Args {
		arg, err := l.lowerExpr(raw)
		if err != nil {
			return Value{}, true, err
		}
		args = append(args, arg)
	}
	return l.emit("call.indirect", typ.Text(node.Result), args, ""), true, nil
}

// funcPointerNode parses a function pointer spelling, and reports whether the
// text is one.
func funcPointerNode(text string) (*typ.Func, bool) {
	if !strings.HasPrefix(text, "fn(") && !strings.HasPrefix(text, "unsafe fn(") {
		return nil, false
	}
	parsed, err := typ.Parse(text)
	if err != nil {
		return nil, false
	}
	node, ok := parsed.(*typ.Func)
	return node, ok
}

// functionCalleeName resolves direct and namespace-qualified function names.
func (l *lowerer) functionCalleeName(callee ast.Expression) (string, bool) {
	ident, ok := callee.(*ast.IdentExpr)
	if ok {
		// A `Function` static parameter is the name it was instantiated with.
		// The instance already bound it (genericBindings), so calling through
		// it is reading that binding rather than a new kind of call.
		if name, bound := l.functionStaticValue(ident.Name); bound {
			return name, true
		}
		return ident.Name, true
	}
	field, ok := callee.(*ast.FieldExpr)
	if !ok || !field.Namespace {
		return "", false
	}
	qualified := field.String()
	if _, ok := l.signatures[qualified]; ok {
		return qualified, true
	}
	if _, ok := stdprim.SimpleCoreSignatures[qualified]; ok {
		return qualified, true
	}
	if _, ok := runtimeBuiltinReturnType(qualified); ok {
		return qualified, true
	}
	if _, ok := arrayPrimitives[qualified]; ok {
		return qualified, true
	}
	if _, ok := mapPrimitives[qualified]; ok {
		return qualified, true
	}
	if _, ok := boxPrimitives[qualified]; ok {
		return qualified, true
	}
	if _, ok := arenaPrimitives[qualified]; ok {
		return qualified, true
	}
	return "", false
}

// lowerNamedCallExpr lowers builtins and resolved function calls.
func (l *lowerer) lowerNamedCallExpr(name string, rawArgs []ast.Expression) (Value, error) {
	args, err := l.lowerCallArgs(name, rawArgs)
	if err != nil {
		return Value{}, err
	}
	if name == "std::internal::builtin::mem_len" {
		if len(args) != 1 {
			return Value{}, fmt.Errorf("ir error: std::internal::builtin::mem_len expects 1 arg")
		}
		return l.emit("slice.len", "i64", args, ""), nil
	}
	if name == "std::internal::builtin::test_fail" {
		return l.emit("test.fail", "void", args, ""), nil
	}
	if name == "std::internal::builtin::panic" {
		return l.emit("panic.fail", "void", args, ""), nil
	}
	ret := "void"
	if sig, ok := l.signatures[name]; ok {
		ret = sig.Return
	} else if sig, ok := stdprim.SimpleCoreSignatures[name]; ok {
		ret = sig.Return
	} else if builtinReturn, ok := runtimeBuiltinReturnType(name); ok {
		ret = builtinReturn
	}
	if symbol, ok := l.externSymbols[name]; ok {
		name = symbol
	}
	return l.emit("call."+name, ret, args, ""), nil
}

// lowerTypedNamedCallExpr lowers the small typed-call subset with backend support.
func (l *lowerer) lowerTypedNamedCallExpr(
	name string,
	typeArg string,
	rawArgs []ast.Expression,
) (Value, error) {
	value, handled, err := l.lowerTypedContainerPrimitive(name, typeArg, rawArgs)
	if handled || err != nil {
		return value, err
	}
	// expect_equal lowers to its own instruction, so its std wrapper body is
	// never called. Its arguments still arrive at the types that wrapper
	// declares, which is what makes `expect_equal<u8>(65, byte)` compare bytes.
	if name == "std::testing::expect_equal" {
		params, err := l.declaredInstanceParams(name, typeArg)
		if err != nil {
			return Value{}, err
		}
		args, err := l.lowerCallArgsAs(params, rawArgs)
		if err != nil {
			return Value{}, err
		}
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
	}
	symbol, sig, err := l.requestGenericInstance(name, typeArg)
	if err != nil {
		return Value{}, err
	}
	args, err := l.lowerCallArgsAs(sig.Params, rawArgs)
	if err != nil {
		return Value{}, err
	}
	return l.emit("call."+symbol, sig.Return, args, ""), nil
}

// lowerTypedContainerPrimitive lowers private storage operations whose static
// element type is carried as an instruction immediate.
func (l *lowerer) lowerTypedContainerPrimitive(
	name string,
	typeArg string,
	rawArgs []ast.Expression,
) (Value, bool, error) {
	// A container primitive carries its element type as an immediate the backend
	// reads, so the call itself declares no parameter types to hand over.
	if method, ok := arrayPrimitives[name]; ok {
		elem := l.resolveType(typeArg)
		args, err := l.lowerCallArgsAs(arrayPrimitiveParams(method, elem), rawArgs)
		if err != nil {
			return Value{}, true, err
		}
		value, err := l.lowerArrayMethod(method, elem, args)
		return value, true, err
	}
	if method, ok := mapPrimitives[name]; ok {
		key, value, err := mapPrimitiveTypeArgs(l.resolveTypeArgs(typeArg))
		if err != nil {
			return Value{}, true, err
		}
		args, err := l.lowerCallArgsAs(mapPrimitiveParams(method, key, value), rawArgs)
		if err != nil {
			return Value{}, true, err
		}
		result, err := l.lowerMapMethod(method, key, value, args)
		return result, true, err
	}
	if method, ok := boxPrimitives[name]; ok {
		args, err := l.lowerCallArgsAs(nil, rawArgs)
		if err != nil {
			return Value{}, true, err
		}
		value, err := l.lowerBoxMethod(method, l.resolveType(typeArg), args)
		return value, true, err
	}
	if method, ok := arenaPrimitives[name]; ok {
		elem := l.resolveType(typeArg)
		args, err := l.lowerCallArgsAs(arenaPrimitiveParams(method, elem), rawArgs)
		if err != nil {
			return Value{}, true, err
		}
		value, err := l.lowerArenaPrimitive(method, elem, args)
		return value, true, err
	}
	return Value{}, false, nil
}

// lowerArenaConstructor lowers std::arena::new<T>(allocator).
func (l *lowerer) lowerArenaConstructor(typeArg string, args []ast.Expression) (Value, error) {
	if len(args) != 1 {
		return Value{}, fmt.Errorf(
			"ir error: std::arena::new<%s> expects exactly one allocator argument",
			typeArg,
		)
	}
	allocator, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	return l.emit("arena.new", "std::arena::Arena<"+typeArg+">", []Value{allocator}, ""), nil
}

// lowerArrayConstructor lowers std::array::new<T>(allocator).
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
	return l.emit("array.new", arrayTypeName+"<"+typeArg+">", []Value{allocator}, ""), nil
}

// lowerMapConstructor lowers std::map::new<K, V>(allocator).
func (l *lowerer) lowerMapConstructor(typeArg string, args []ast.Expression) (Value, error) {
	mapType, _, ok := mapInstanceType(typeArg)
	if !ok {
		return Value{}, fmt.Errorf("ir error: std::map::Map key type must be one of %s",
			typ.MapKeyTypeNames())
	}
	if len(args) != 1 {
		return Value{}, fmt.Errorf("ir error: %s expects exactly one allocator argument", mapType)
	}
	allocator, err := l.lowerExpr(args[0])
	if err != nil {
		return Value{}, err
	}
	return l.emit("map.new", mapType, []Value{allocator}, ""), nil
}

// lowerTryExpr lowers error-union propagation as an explicit IR instruction.
func (l *lowerer) lowerTryExpr(expr *ast.TryExpr) (Value, error) {
	value, err := l.lowerExpr(expr.Value)
	if err != nil {
		return Value{}, err
	}
	// The attached cleanups run at this same program point, so a slot-backed
	// receiver is loaded here: Cleanup args are always values, never `&var`
	// slots, and no backend has to re-derive that rule.
	cleanups := retireCleanups(
		l.errorCleanups(),
		l.ownership.RetiredErrDefersForTry(expr),
	)
	for index := range cleanups {
		cleanups[index].Args = l.loadCleanupArgs(cleanups[index].Args)
	}
	result := l.emit("error.try", errorUnionElementType(l.types, value.Type), []Value{value}, "")
	l.block.Instrs[len(l.block.Instrs)-1].Cleanups = cleanups
	return result, nil
}

// lowerMethodReceiver lowers a method call's receiver. A `&var self` method
// on a one-level field path receives the field's own storage, projected out
// of the owner's; every other receiver keeps the slot-or-value shape
// lowerReceiverAddress picks. The method is resolved from the receiver's
// declared type before anything is emitted, so choosing the projection never
// leaves a dead read behind.
func (l *lowerer) lowerMethodReceiver(field *ast.FieldExpr) (Value, error) {
	if receiverType := l.assignTargetType(field.Receiver); receiverType != "" &&
		l.methodReceivesAddress(receiverType, field.Name) {
		if storage, ok := l.lowerFieldStorage(field.Receiver); ok {
			return storage, nil
		}
	}
	return l.lowerReceiverAddress(field.Receiver)
}

// lowerMethodCallExpr lowers receiver method calls.
func (l *lowerer) lowerMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
) (Value, error) {
	receiver, err := l.lowerMethodReceiver(field)
	if err != nil {
		return Value{}, err
	}
	// A method that declares `&var self` receives the receiver's storage; every
	// other method receives the value, so a receiver arriving as a borrow is
	// loaded for it. `&T` reaches here from a borrow optional's capture
	// (`array.at(i) |elem|`), which holds a real address; a `&T` parameter is
	// already erased to its value by the time it is a receiver.
	wantsStorage := l.methodTakesSelfStorage(receiver.Type, field.Name)
	if isReferenceType(receiver.Type) && !l.methodReceivesAddress(receiver.Type, field.Name) {
		receiver = l.emit("ref.load", derefType(receiver.Type), []Value{receiver}, "")
	}
	if wantsStorage && !isMutableReferenceType(receiver.Type) {
		return Value{}, fmt.Errorf(
			"ir error: method `%s` takes `&var self`, receiver `%s` has no storage",
			field.Name, field.Receiver.String())
	}
	if isBufferIRType(receiver.Type) {
		return l.lowerBufferMethod(field.Name, receiver, args)
	}
	params, err := l.methodCalleeParams(receiver.Type, field.Name)
	if err != nil {
		return Value{}, err
	}
	loweredArgs, err := l.lowerCallArgsAs(params, args)
	if err != nil {
		return Value{}, err
	}
	allArgs := append([]Value{receiver}, loweredArgs...)
	return l.lowerResolvedMethod(receiver.Type, field.Name, allArgs)
}

// lowerResolvedMethod dispatches a checked receiver to its std wrapper,
// declared implementation, or compiler-known arena operation.
func (l *lowerer) lowerResolvedMethod(
	receiverType string,
	method string,
	allArgs []Value,
) (Value, error) {
	// A container method that mutates receives the binding's storage, so the
	// receiver arrives spelled as the borrow. The container it names is the
	// same one either way.
	receiverType = derefType(receiverType)
	if elem, ok := arrayElementType(receiverType); ok {
		return l.lowerStdContainerMethod(arrayTypeName, method, elem, allArgs)
	}
	if args, ok := mapStaticArgs(receiverType); ok {
		return l.lowerStdContainerMethod(mapTypeName, method, args, allArgs)
	}
	if elem, ok := boxElementType(receiverType); ok {
		return l.lowerStdContainerMethod(boxTypeName, method, elem, allArgs)
	}
	if elem := arenaElementType(receiverType); elem != "unknown" {
		return l.lowerStdContainerMethod(arenaTypeName, method, elem, allArgs)
	}
	if methodName, ok := l.implMethodCalleeName(receiverType, method); ok {
		return l.lowerImplMethodCall(methodName, allArgs)
	}
	return Value{}, fmt.Errorf(
		"ir error: `%s` has no method `%s`", receiverType, method)
}

// isBufferIRType reports whether an IR type spelling is a fixed-length stack
// buffer (`[N]u8`).
func isBufferIRType(typeName string) bool {
	return len(typeName) > 1 && typeName[0] == '[' &&
		typeName[1] >= '0' && typeName[1] <= '9'
}

// lowerBufferMethod lowers stack buffer view methods (ADR-0097). Both view
// forms hand back the same {ptr, len} value; mutability is a checker-level
// permission, not a runtime representation.
func (l *lowerer) lowerBufferMethod(
	name string,
	receiver Value,
	args []ast.Expression,
) (Value, error) {
	switch name {
	case "as_bytes", "as_mut_bytes":
		if len(args) != 0 {
			return Value{}, fmt.Errorf("ir error: buffer `%s` expects 0 args", name)
		}
		return l.emit("buffer.as_bytes", "[]u8", []Value{receiver}, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown buffer method `%s`", name)
	}
}

// methodCalleeParams returns the parameters a method call's callee declares
// after self, so each argument is handed over at the type the callee receives it
// as. nil means the lowerer cannot name them from the receiver alone, and those
// arguments keep the types they carry themselves.
func (l *lowerer) methodCalleeParams(receiver string, method string) ([]Param, error) {
	receiver = derefType(receiver)
	if elem, ok := arrayElementType(receiver); ok {
		return l.stdContainerParams(arrayTypeName, method, elem)
	}
	if args, ok := mapStaticArgs(receiver); ok {
		return l.stdContainerParams(mapTypeName, method, args)
	}
	if elem, ok := boxElementType(receiver); ok {
		return l.stdContainerParams(boxTypeName, method, elem)
	}
	if name, ok := l.implMethodCalleeName(receiver, method); ok {
		return paramsAfterSelf(l.signatures[name].Params), nil
	}
	return nil, nil
}

// stdContainerParams returns the parameters std declares for one container
// method at this element type.
func (l *lowerer) stdContainerParams(
	receiver string,
	method string,
	typeArg string,
) ([]Param, error) {
	_, sig, err := l.requestGenericInstance(stdmethod.MethodName(receiver, method), typeArg)
	if err != nil {
		return nil, err
	}
	return paramsAfterSelf(sig.Params), nil
}

// paramsAfterSelf drops the receiver, which a method call lowers on its own.
func paramsAfterSelf(params []Param) []Param {
	if len(params) == 0 {
		return nil
	}
	return params[1:]
}

// lowerStdContainerMethod runs the wrapper std declares for a container method,
// so the body in std/src/*/*.kizu is what the call does rather than a description
// of it. There is no second answer to fall back on: a lookup that stops finding
// the declaration fails here, instead of quietly lowering to an instruction the
// lowerer picked out of a list of its own.
func (l *lowerer) lowerStdContainerMethod(
	receiver string,
	method string,
	typeArg string,
	args []Value,
) (Value, error) {
	op, sig, err := l.stdContainerCallOp(receiver, method, typeArg)
	if err != nil {
		return Value{}, err
	}
	return l.emit(op, sig.Return, args, ""), nil
}

// stdContainerCallOp resolves one std container method to the call op of its
// generic instance — the one resolution direct calls and deferred cleanups
// share.
func (l *lowerer) stdContainerCallOp(
	receiver string,
	method string,
	typeArg string,
) (string, Signature, error) {
	symbol, sig, err := l.requestGenericInstance(stdmethod.MethodName(receiver, method), typeArg)
	if err != nil {
		return "", Signature{}, err
	}
	return "call." + symbol, sig, nil
}

// methodTakesSelfStorage reports whether the method this receiver resolves
// declares `&var self`, so the call passes storage instead of a loaded value.
func (l *lowerer) methodTakesSelfStorage(receiverType string, method string) bool {
	return l.methodSelfPassing(receiverType, method) == PassCallerStorage
}

// methodReceivesAddress reports whether a method reads its receiver through an
// address rather than a copy of it. A receiver that already has storage is then
// handed that storage: reading an array costs no copy of the header it reads,
// which for a container wider than a word is the difference between a pointer
// and a memcpy at every call.
func (l *lowerer) methodReceivesAddress(receiverType string, method string) bool {
	passing := l.methodSelfPassing(receiverType, method)
	return passing == PassCallerStorage || passing == PassCopyAddress
}

// methodSelfPassing says how a method receives its receiver.
func (l *lowerer) methodSelfPassing(receiverType string, method string) Passing {
	if name, ok := l.implMethodCalleeName(receiverType, method); ok {
		params := l.signatures[name].Params
		if len(params) == 0 {
			return PassValue
		}
		return params[0].Passing
	}
	return l.genericMethodSelfPassing(receiverType, method)
}

// implMethodCalleeName resolves a checked receiver method to its lowered symbol.
func (l *lowerer) implMethodCalleeName(receiver string, method string) (string, bool) {
	name := stdmethod.MethodName(derefType(receiver), method)
	if _, ok := l.signatures[name]; ok {
		return name, true
	}
	return "", false
}

// genericMethodSelfPassing answers the same question for a method that is only
// known as a generic declaration. A std container method is one: its instance
// is requested at the call, so there is no lowered signature to read before the
// receiver is lowered, and the declaration is what says how the receiver
// arrives.
func (l *lowerer) genericMethodSelfPassing(receiverType string, method string) Passing {
	container := derefType(receiverType)
	base, ok := stdContainerTypeName(container)
	if !ok {
		return PassValue
	}
	decl := l.genericDecl(stdmethod.MethodName(base, method))
	if decl == nil || len(decl.Params) == 0 {
		return PassValue
	}
	if decl.Params[0].MutBorrow {
		return PassCallerStorage
	}
	if decl.Params[0].Borrow {
		_, passing := l.borrowIRType(container, false)
		return passing
	}
	return PassValue
}

// stdContainerTypeName names the generic declaration a container instance was
// applied from, the name lowerResolvedMethod dispatches on.
func stdContainerTypeName(typ string) (string, bool) {
	if _, ok := arrayElementType(typ); ok {
		return arrayTypeName, true
	}
	if _, ok := mapStaticArgs(typ); ok {
		return mapTypeName, true
	}
	if _, ok := boxElementType(typ); ok {
		return boxTypeName, true
	}
	if strings.HasPrefix(typ, arenaTypeName+"<") && strings.HasSuffix(typ, ">") {
		return arenaTypeName, true
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

// arrayPrimitives maps a std::internal::builtin Array primitive to the method it lowers
// as. std/src/array/array.kizu forwards each method to one of these, and lowering the
// forward is what makes that line the implementation rather than a description
// of one.
// arrayMutatingPrimitives names the array primitives that write through the
// header. They receive the binding's storage; the rest receive the address of
// a copy, and `deinit` receives the header itself because releasing it is the
// last thing done with it.
var arrayMutatingPrimitives = map[string]bool{
	"append": true, "append_bytes": true, "reserve": true, "pop": true, "pop_or_panic": true,
	"set": true, "swap": true, "at_mut": true, "clear": true,
	"truncate": true, "as_mut_bytes": true,
}

// arrayPrimitiveParams says how one array primitive receives its array. The
// primitives declare no signature of their own, so this is where the call
// learns whether to hand over storage, an address, or the value.
func arrayPrimitiveParams(method string, elem string) []Param {
	if method == typ.CleanupMethod {
		return nil
	}
	array := arrayTypeName + "<" + elem + ">"
	self := Param{Type: "&" + array, Passing: PassCopyAddress}
	if arrayMutatingPrimitives[method] {
		self = Param{Type: "&var " + array, Passing: PassCallerStorage}
	}
	// The element positions are named too, so an integer literal written at a
	// call narrows to the element type rather than staying i64.
	switch method {
	case "append":
		return []Param{self, {Type: elem}}
	case "append_bytes":
		return []Param{self, {Type: "[]u8"}}
	case "set":
		return []Param{self, {Type: "i64"}, {Type: elem}}
	}
	return []Param{self}
}

// arenaPrimitiveParams says how one arena primitive receives its arena, the
// way arrayPrimitiveParams does for an array -- the two are the same header.
// Everything but `at` is handed the binding's storage: the writers because a
// copy would leave the caller's `data` behind, and `len` because it is the
// other half of std::arena's cleanup loop, which is only right while the count
// it reads and the pop that shortens it reach the same header. `deinit`
// receives the header itself, because releasing it is the last thing done with
// it. The argument positions are named too, so an integer literal written at a
// call narrows to the element type rather than staying i64.
func arenaPrimitiveParams(method string, elem string) []Param {
	if method == typ.CleanupMethod {
		return nil
	}
	arena := arenaTypeName + "<" + elem + ">"
	self := Param{Type: "&var " + arena, Passing: PassCallerStorage}
	if method == "at" {
		self = Param{Type: "&" + arena, Passing: PassCopyAddress}
	}
	switch method {
	case "add":
		return []Param{self, {Type: "Allocator"}, {Type: elem}}
	case "at", "at_mut":
		return []Param{self, {Type: arenaHandleType(elem)}}
	}
	return []Param{self}
}

// primitiveInstanceParams names the parameters of a container primitive, which
// has no declaration for declaredInstanceParams to read. What it answers is
// how the container arrives: a primitive that writes through the header is
// handed the binding's storage, so the binding needs a slot of its own rather
// than a fresh copy per call.
func (l *lowerer) primitiveInstanceParams(name string, typeArg string) []Param {
	if method, ok := arrayPrimitives[name]; ok {
		return arrayPrimitiveParams(method, l.resolveType(typeArg))
	}
	if method, ok := arenaPrimitives[name]; ok {
		return arenaPrimitiveParams(method, l.resolveType(typeArg))
	}
	if method, ok := mapPrimitives[name]; ok {
		key, value, err := mapPrimitiveTypeArgs(l.resolveTypeArgs(typeArg))
		if err != nil {
			return nil
		}
		return mapPrimitiveParams(method, key, value)
	}
	return nil
}

// mapPrimitiveParams says how one map primitive receives its map, the way
// arrayPrimitiveParams does for an array -- a map is its header too. A
// primitive that writes through the header is handed the binding's storage;
// the rest receive the address of a copy, and `deinit` receives the header
// itself, because releasing it is the last thing done with it. The key and
// value positions are named as well, so an integer literal written at a call
// narrows rather than staying i64.
func mapPrimitiveParams(method string, key string, value string) []Param {
	if method == typ.CleanupMethod {
		return nil
	}
	mapType := mapTypeName + "<" + key + ", " + value + ">"
	self := Param{Type: "&" + mapType, Passing: PassCopyAddress}
	if mapMutatingPrimitives[method] {
		self = Param{Type: "&var " + mapType, Passing: PassCallerStorage}
	}
	switch method {
	case "insert":
		return []Param{self, {Type: "Allocator"}, {Type: key}, {Type: value}}
	case "get", "at", "at_mut", "contains":
		return []Param{self, {Type: key}}
	case "key_at", "take_value_at":
		return []Param{self, {Type: "i64"}}
	}
	return []Param{self}
}

// mapMutatingPrimitives names the map primitives that write through the
// header. They receive the binding's storage; the rest receive the address of
// a copy.
var mapMutatingPrimitives = map[string]bool{
	"insert": true, "at_mut": true, "take_value_at": true,
}

var arrayPrimitives = map[string]string{
	"std::internal::builtin::array_append":       "append",
	"std::internal::builtin::array_append_bytes": "append_bytes",
	"std::internal::builtin::array_as_bytes":     "as_bytes",
	"std::internal::builtin::array_as_mut_bytes": "as_mut_bytes",
	"std::internal::builtin::array_at":           "at",
	"std::internal::builtin::array_at_mut":       "at_mut",
	"std::internal::builtin::array_capacity":     "capacity",
	"std::internal::builtin::array_clear":        "clear",
	"std::internal::builtin::array_deinit":       "deinit",
	"std::internal::builtin::array_get":          "get",
	"std::internal::builtin::array_get_or_panic": "get_or_panic",
	"std::internal::builtin::array_len":          "len",
	"std::internal::builtin::array_pop":          "pop",
	"std::internal::builtin::array_pop_or_panic": "pop_or_panic",
	"std::internal::builtin::array_reserve":      "reserve",
	"std::internal::builtin::array_set":          "set",
	"std::internal::builtin::array_swap":         "swap",
	"std::internal::builtin::array_truncate":     "truncate",
}

// mapPrimitives maps a std::internal::builtin Map primitive to the method it lowers as.
var mapPrimitives = map[string]string{
	"std::internal::builtin::map_at":       "at",
	"std::internal::builtin::map_at_mut":   "at_mut",
	"std::internal::builtin::map_contains": "contains",
	"std::internal::builtin::map_deinit":   "deinit",
	"std::internal::builtin::map_get":      "get",
	"std::internal::builtin::map_insert":   "insert",
	"std::internal::builtin::map_key_at":   "key_at",
	"std::internal::builtin::map_len":      "len",

	"std::internal::builtin::map_take_value_at": "take_value_at",
}

// boxPrimitives maps a std::internal::builtin Box primitive to the operation it
// lowers as. std/src/mem/mem.kizu forwards the constructor and each method to one
// of these, reached the same way the Array and Map primitives are.
var boxPrimitives = map[string]string{
	"std::internal::builtin::box":            "new",
	"std::internal::builtin::box_borrow":     "borrow",
	"std::internal::builtin::box_borrow_mut": "borrow_mut",
	"std::internal::builtin::box_deinit":     "deinit",
	"std::internal::builtin::box_take":       "take",
}

// arenaPrimitives maps a std::internal::builtin Arena primitive to the method
// it lowers as. std/src/arena/arena.kizu forwards each method to one of these,
// the way std::array does, so the declaration is what the checkers and the
// slot analysis read and this is only the instruction it becomes.
var arenaPrimitives = map[string]string{
	"std::internal::builtin::arena_add":          "add",
	"std::internal::builtin::arena_at":           "at",
	"std::internal::builtin::arena_at_mut":       "at_mut",
	"std::internal::builtin::arena_len":          "len",
	"std::internal::builtin::arena_pop_or_panic": "pop_or_panic",
	"std::internal::builtin::arena_deinit":       "deinit",
}

// lowerArenaPrimitive lowers the storage operations std::arena's methods
// forward to.
func (l *lowerer) lowerArenaPrimitive(name string, elem string, args []Value) (Value, error) {
	switch name {
	case "add":
		// args are the receiver, the allocator the growth names, and the
		// element; a failed add releases the element through that same
		// allocator, which is the one it was built from (ADR-0132).
		return l.releaseOwnerOnFailure(
			l.emit("arena.add", "std::mem::Error!"+arenaHandleType(elem), args, ""),
			args[2], args[1])
	case "at":
		resultType, _ := l.borrowIRType(elem, false)
		return l.emit("arena.at", resultType, args, ""), nil
	case "at_mut":
		return l.emit("arena.at_mut", "?&var "+elem, args, ""), nil
	case "len":
		return l.emit("arena.len", "i64", args, ""), nil
	case "pop_or_panic":
		return l.emit("arena.pop_or_panic", elem, args, ""), nil
	case "deinit":
		return l.emit("arena.deinit", "void", args, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown arena primitive `%s`", name)
	}
}

// lowerBoxMethod lowers the runtime primitive one std::mem::Box<T> wrapper
// forwards to. Only a wrapper body in std/src/mem/mem.kizu reaches it.
func (l *lowerer) lowerBoxMethod(name string, elem string, args []Value) (Value, error) {
	switch name {
	case "new":
		if len(args) != 2 {
			return Value{}, fmt.Errorf("ir error: box.new expects allocator and value")
		}
		// args are the allocator the cell comes from and the payload; a
		// failed allocation releases the payload through that allocator.
		return l.releaseOwnerOnFailure(
			l.emit("box.new", "std::mem::Error!"+boxTypeName+"<"+elem+">", args, ""),
			args[1], args[0])
	case "borrow":
		// A returned borrow travels under the same rule any borrow return
		// does: unions stay behind a pointer, everything else as a copy.
		spelling, _ := l.borrowIRType(elem, false)
		return l.emit("box.borrow", spelling, args, ""), nil
	case "borrow_mut":
		spelling, _ := l.borrowIRType(elem, true)
		return l.emit("box.borrow_mut", spelling, args, ""), nil
	case "deinit":
		return l.emit("box.deinit", "void", args, ""), nil
	case "take":
		return l.emit("box.take", elem, args, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown box method `%s`", name)
	}
}

// mapPrimitiveTypeArgs returns K and V from the `K, V` static arguments a Map
// primitive is applied to.
func mapPrimitiveTypeArgs(typeArg string) (string, string, error) {
	args := splitStaticArgs(typeArg)
	if len(args) != 2 {
		return "", "", fmt.Errorf(
			"ir error: a std::map::Map primitive takes 2 static arguments, got `%s`", typeArg)
	}
	return args[0], args[1], nil
}

// lowerMapMethod lowers the runtime primitive one std::map::Map<K, V> method
// forwards to. Only a wrapper body in std/src/map/map.kizu reaches it: a `m.get(k)`
// call lowers as a call to that wrapper, and this is what the wrapper does.
func (l *lowerer) lowerMapMethod(
	name string,
	keyType string,
	valueType string,
	args []Value,
) (Value, error) {
	switch name {
	case "insert":
		return l.emit("map.insert", "std::mem::Error!void", args, ""), nil
	case "get":
		return l.emit("map.get", "?"+valueType, args, ""), nil
	case "at":
		return l.emit("map.at", "?&"+valueType, args, ""), nil
	case "at_mut":
		return l.emit("map.at_mut", "?&var "+valueType, args, ""), nil
	case "take_value_at":
		return l.emit("map.take_value_at", valueType, args, ""), nil
	case "key_at":
		return l.emit("map.key_at", "?"+keyType, args, ""), nil
	case "contains":
		return l.emit("map.contains", "bool", args, ""), nil
	case "len":
		return l.emit("map.len", "i64", args, ""), nil
	case "deinit":
		return l.emit("map.deinit", "void", args, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown map method `%s`", name)
	}
}

// lowerArrayMethod lowers the runtime primitive one std::array::Array<T> method
// forwards to, reached the same way lowerMapMethod is. Only the methods whose
// result mentions the element type need to be spelled out here; the rest carry a
// fixed result type and go through arrayMethodResultType.
func (l *lowerer) lowerArrayMethod(name string, elem string, args []Value) (Value, error) {
	if result, ok := arrayMethodResultType(name); ok {
		value := l.emit("array."+name, result, args, "")
		if name == "append" {
			// args are the receiver, the allocator the growth names, and the
			// element; a failed append releases the element through that same
			// allocator, which is the one it was built from (ADR-0132).
			return l.releaseOwnerOnFailure(value, args[2], args[1])
		}
		return value, nil
	}
	switch name {
	case "pop":
		return l.emit("array.pop", "?"+elem, args, ""), nil
	case "pop_or_panic":
		return l.emit("array.pop_or_panic", elem, args, ""), nil
	case "get":
		return l.emit("array.get", "?"+elem, args, ""), nil
	case "get_or_panic":
		return l.emit("array.get_or_panic", elem, args, ""), nil
	case "at":
		return l.emit("array.at", "?&"+elem, args, ""), nil
	case "at_mut":
		return l.emit("array.at_mut", "?&var "+elem, args, ""), nil
	case "as_mut_bytes":
		// The same view value as as_bytes: mutability is a checker-level
		// permission, not a different runtime representation (ADR-0096).
		return l.emit("array.as_bytes", "[]u8", args, ""), nil
	default:
		return Value{}, fmt.Errorf("ir error: unknown array method `%s`", name)
	}
}

// arrayMethodResultType gives the IR result type of the Array methods that do
// not hand back an element, so their lowering is uniform. It reports false for
// element-typed methods, which keeps unknown names on lowerArrayMethod's error
// path rather than lowering them to an instruction.
func arrayMethodResultType(name string) (string, bool) {
	switch name {
	case "append", "append_bytes", "reserve":
		// Growing needs memory and nothing else (ADR-0128).
		return "std::mem::Error!void", true
	case "set", "swap", "truncate":
		return "std::array::Error!void", true
	case "len", "capacity":
		return "i64", true
	case "clear", "deinit":
		return "void", true
	case "as_bytes":
		return "[]u8", true
	default:
		return "", false
	}
}

// releaseOwnerOnFailure gives a fallible runtime primitive the cleanup it
// cannot run itself. The primitive takes an owner by value and stores it only
// after the allocation succeeds, so a failure leaves the value written nowhere
// while the caller has already moved it. The obligation is the callee's, and
// this is where the callee can meet it: the wrapper is generic and `T` is bound
// here, so the element's own cleanup resolves, while the runtime sees only
// bytes and a size and could never call it.
//
// The wrap is `try` plus a re-wrap, so the primitive keeps the `!T` its wrapper
// returns and the error still leaves the wrapper as an error.
func (l *lowerer) releaseOwnerOnFailure(
	result Value,
	owner Value,
	allocator Value,
) (Value, error) {
	if !ast.OwnerType(l.deinitOwners, owner.Type) {
		return result, nil
	}
	rest := []Value{}
	if ast.ReleaseNames(l.releaseAllocators, owner.Type) {
		rest = append(rest, allocator)
	}
	cleanup, err := l.cleanupFromMethod(owner, typ.CleanupMethod, rest)
	if err != nil {
		return Value{}, err
	}
	cleanup.OnError = true
	success := errorUnionElementType(l.types, result.Type)
	value := l.emit("error.try", success, []Value{result}, "")
	l.block.Instrs[len(l.block.Instrs)-1].Cleanups = []Cleanup{cleanup}
	args := []Value{value}
	if success == "void" {
		args = nil
	}
	return l.emit("error.ok", result.Type, args, ""), nil
}

// lowerStructLiteralExpr lowers struct construction.
func (l *lowerer) lowerStructLiteralExpr(expr *ast.StructLiteralExpr) (Value, error) {
	fields := make([]FieldArg, 0, len(expr.Fields))
	for _, field := range expr.Fields {
		value, err := l.lowerContextualExpr(field.Value, l.fieldType(expr.TypeName, field.Name))
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
	// A storage-backed receiver reads the field through its address, so one
	// field read does not load the whole aggregate out of the storage.
	receiver, err := l.lowerReceiverAddress(expr.Receiver)
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
	payload, err := l.lowerContextualExpr(rawArgs[0], variant.Payload)
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
// Error set members resolve the same way; their tags are global error codes.
func (l *lowerer) lowerEnumTagExpr(expr *ast.FieldExpr) (Value, bool) {
	enumName := expr.Receiver.String()
	enumType, ok := l.module.Enums[enumName]
	if !ok {
		enumType, ok = l.module.ErrorSets[enumName]
	}
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
//
// The bounds test lives here rather than in a backend, so every backend
// inherits the same check and the same message from one place, and `kizu ir`
// shows what a program checks before it reads memory.
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
		length := l.emit("slice.len", "i64", []Value{target}, "")
		l.condFail(expr.Span, "binary.<", index, zeroIndex, "bounds", index, length)
		l.condFail(expr.Span, "binary.>=", index, length, "bounds", index, length)
		return l.emit("slice.index", "u8", []Value{target, index}, ""), nil
	}
	start, err := l.lowerSliceBound(expr.Start, zeroIndex)
	if err != nil {
		return Value{}, err
	}
	end, err := l.lowerSliceEnd(expr.End, target)
	if err != nil {
		return Value{}, err
	}
	length := l.emit("slice.len", "i64", []Value{target}, "")
	l.condFail(expr.Span, "binary.<", start, zeroIndex, "range", start, end, length)
	l.condFail(expr.Span, "binary.>", start, end, "range", start, end, length)
	l.condFail(expr.Span, "binary.>", end, length, "range", start, end, length)
	return l.emit("slice.slice", "[]u8", []Value{target, start, end}, ""), nil
}

// zeroIndex is the constant a negative bound is tested against.
var zeroIndex = Value{Name: "0", Type: "i64"}

// condFail emits a comparison and aborts with the named failure when it holds.
//
// The values a failure reports travel with it, so the wording lives in the
// runtime rather than being spelled out here or in a backend.
func (l *lowerer) condFail(
	span ast.Span,
	op string,
	left Value,
	right Value,
	kind string,
	values ...Value,
) {
	bad := l.emit(op, "bool", []Value{left, right}, "")
	args := append([]Value{bad}, values...)
	l.block.Instrs = append(l.block.Instrs, &Instr{
		Op: "cond_fail", Args: args, Immediate: kind, Span: span,
	})
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
