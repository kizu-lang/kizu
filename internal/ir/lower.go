package ir

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/stdprim"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Lower converts a checked Kizu AST into typed SSA IR.
func Lower(program *ast.Program) (*Module, error) {
	l := newLowerer(program)
	module, err := l.lower()
	if err != nil {
		return nil, err
	}
	if err := Verify(module); err != nil {
		return nil, err
	}
	return module, nil
}

type lowerer struct {
	program     *ast.Program
	module      *Module
	signatures  map[string]Signature
	current     *Function
	block       *Block
	env         *env
	nextValue   int
	nextBlock   int
	loops       []*loopContext
	deferFrames [][]Cleanup
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
	// because something writes through a `&var` borrow of them. Their entry in
	// env is the storage, not the value.
	slots map[string]bool
	// nextErrorCode is the next global code for an error set the program
	// declares itself; std members keep the codes std assigns.
	nextErrorCode int
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
func newLowerer(program *ast.Program) *lowerer {
	return &lowerer{
		program: program,
		module: &Module{
			Structs:   map[string]Struct{},
			Enums:     map[string]Enum{},
			ErrorSets: map[string]Enum{},
			Unions:    map[string]Union{},
		},
		signatures:   map[string]Signature{},
		typeBindings: map[string]string{},
		instantiated: map[string]bool{},
		staticValues: map[string]staticValue{},
	}
}

// resolveType binds the type parameters in force, wherever they stand. A
// parameter inside a static argument list counts: `std::array::Array<T>` is the
// receiver of every Array method, and lowering it as written would leave the
// instance carrying the parameter it was instantiated away from.
func (l *lowerer) resolveType(name string) string {
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
	return symbol, l.instanceSignature(decl.FunctionSignature, instance.bindings), nil
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
		lowered, err := l.lowerFunctionNamed(next.decl, next.symbol)
		l.typeBindings = map[string]string{}
		l.staticValues = map[string]staticValue{}
		if err != nil {
			return err
		}
		l.module.Functions = append(l.module.Functions, lowered)
	}
	return nil
}

// genericDecl returns the generic function declaration with this IR name.
func (l *lowerer) genericDecl(name string) *ast.FunctionDecl {
	for _, decl := range l.program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || len(fn.StaticParams) == 0 {
			continue
		}
		if fn.Name == name {
			return fn
		}
	}
	return nil
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
		lowered, err := l.lowerFunctionNamed(fn, fn.Name)
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
	for _, decl := range l.program.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			l.module.Structs[d.Name] = lowerStruct(d)
		case *ast.EnumDecl:
			l.module.Enums[d.Name] = lowerEnum(d)
		case *ast.ErrorSetDecl:
			set, err := l.lowerErrorSet(d)
			if err != nil {
				return err
			}
			l.module.ErrorSets[d.Name] = set
		case *ast.UnionDecl:
			l.module.Unions[d.Name] = lowerUnion(d)
		}
	}
	for _, decl := range l.program.Decls {
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			l.signatures[fn.Name] = l.lowerSignature(fn.FunctionSignature)
		}
	}
	return nil
}

// lowerUnion converts an AST union declaration to IR metadata.
func lowerUnion(decl *ast.UnionDecl) Union {
	variants := map[string]UnionVariant{}
	for index, variant := range decl.Variants {
		variants[variant.Name] = UnionVariant{
			Name: variant.Name, Index: index, Payload: typ.Text(variant.Payload),
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

// lowerStruct converts an AST struct declaration to IR metadata.
func lowerStruct(decl *ast.StructDecl) Struct {
	fields := make([]Field, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		fields = append(fields, Field{Name: field.Name, Type: typ.Text(field.TypeName)})
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
	return Signature{Params: params, Return: returned}
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
	for index, param := range fn.Params {
		l.env.set(param.Name, signature.Params[index].Value())
	}
	l.block = l.newBlock("entry")
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
	return elem, PassValue
}

// lowerReturnType gives a function's result the type it travels as, so a
// returned borrow follows the same rule a borrowed parameter does.
func (l *lowerer) lowerReturnType(name string) string {
	parsed, err := typ.Parse(name)
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
	case *ast.ComptimeIfStmt:
		return l.lowerComptimeIfStmt(s)
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
		return l.assignTargetType(t.Receiver)
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
	if _, success, ok := errorUnionParts(l.current.Return); ok {
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
		l.emitErrorCleanups()
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
	if success, ok := errorUnionSuccessType(l.current.Return); ok {
		return success
	}
	return l.current.Return
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
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.NullExpr:
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
	case *ast.UnsafeExpr:
		// The marker is a claim about who owns the obligation, not an
		// operation, so it lowers to whatever it covers.
		return l.lowerExpr(e.Value)
	case *ast.IfStmt, *ast.MatchStmt, *ast.OrelseGuardExpr:
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
	default:
		return Value{}, fmt.Errorf("ir error: unsupported expression `%s`", expr.String())
	}
}

// lowerDerefExpr reads what a borrow points at. The write side already stored
// through the borrow while this side handed the borrow itself back, so a
// dereferenced borrow was compared against the value it pointed at.
func (l *lowerer) lowerDerefExpr(expr *ast.DerefExpr) (Value, error) {
	receiver, err := l.lowerExpr(expr.Receiver)
	if err != nil {
		return Value{}, err
	}
	if !isReferenceType(receiver.Type) {
		return receiver, nil
	}
	return l.emit("ref.load", derefType(receiver.Type), []Value{receiver}, ""), nil
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
	if !narrowsIntegerLiteral(want) {
		return l.lowerExpr(expr)
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
		return l.emitConst("[]u8", fmt.Sprintf("%q", e.Value)), nil
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
	return Value{}, fmt.Errorf("ir error: undefined value `%s`", expr.Name)
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
		// The std storage constructors lower to one instruction each, so their
		// std bodies are never walked. Every other generic call falls through.
		switch typeApply.Callee.String() {
		case "std::arena::new":
			return l.lowerArenaConstructor(typeApply.TypeArg, expr.Args)
		case "std::array::new":
			return l.lowerArrayConstructor(typeApply.TypeArg, expr.Args)
		case "std::map::new":
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
	// An array or map primitive carries its element type as an immediate the
	// backend reads, so the call itself declares no parameter types to hand over.
	if method, ok := arrayPrimitives[name]; ok {
		args, err := l.lowerCallArgsAs(nil, rawArgs)
		if err != nil {
			return Value{}, err
		}
		return l.lowerArrayMethod(method, l.resolveType(typeArg), args)
	}
	if method, ok := mapPrimitives[name]; ok {
		args, err := l.lowerCallArgsAs(nil, rawArgs)
		if err != nil {
			return Value{}, err
		}
		return l.lowerMapMethod(method, mapPrimitiveValueType(l.resolveTypeArgs(typeArg)), args)
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
	return l.emit("arena.new", "std::arena::Arena<"+typeArg+">", []Value{allocator}, typeArg), nil
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
	return l.emit("array.new", arrayTypeName+"<"+typeArg+">", []Value{allocator}, typeArg), nil
}

// lowerMapConstructor lowers std::map::new<[]u8, V>(allocator).
func (l *lowerer) lowerMapConstructor(typeArg string, args []ast.Expression) (Value, error) {
	mapType, valueType, ok := mapInstanceType(typeArg)
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
	// The attached cleanups run at this same program point, so a slot-backed
	// receiver is loaded here: Cleanup args are always values, never `&var`
	// slots, and no backend has to re-derive that rule.
	cleanups := l.errorCleanups()
	for index := range cleanups {
		cleanups[index].Args = l.loadCleanupArgs(cleanups[index].Args)
	}
	result := l.emit("error.try", errorUnionElementType(value.Type), []Value{value}, "")
	l.block.Instrs[len(l.block.Instrs)-1].Cleanups = cleanups
	return result, nil
}

// lowerMethodCallExpr lowers arena method calls.
func (l *lowerer) lowerMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
) (Value, error) {
	receiver, err := l.lowerReceiverAddress(field.Receiver)
	if err != nil {
		return Value{}, err
	}
	// A method that declares `&var self` receives the receiver's storage; every
	// other method receives the value, so a receiver arriving as storage is
	// loaded for it.
	wantsStorage := l.methodTakesSelfStorage(receiver.Type, field.Name)
	if isMutableReferenceType(receiver.Type) && !wantsStorage {
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
	if elem, ok := arrayElementType(receiver.Type); ok {
		return l.lowerStdContainerMethod(arrayTypeName, field.Name, elem, allArgs)
	}
	if valueType, ok := mapValueType(receiver.Type); ok {
		return l.lowerStdContainerMethod(mapTypeName, field.Name, valueType, allArgs)
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
	if elem, ok := arrayElementType(receiver); ok {
		return l.stdContainerParams(arrayTypeName, method, elem)
	}
	if valueType, ok := mapValueType(receiver); ok {
		return l.stdContainerParams(mapTypeName, method, valueType)
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
// so the body in std/src/*.kizu is what the call does rather than a description
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
	name, ok := l.implMethodCalleeName(receiverType, method)
	if !ok {
		return false
	}
	params := l.signatures[name].Params
	return len(params) > 0 && params[0].Passing == PassCallerStorage
}

// implMethodCalleeName resolves a checked receiver method to its lowered symbol.
func (l *lowerer) implMethodCalleeName(receiver string, method string) (string, bool) {
	name := stdmethod.MethodName(derefType(receiver), method)
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

// arrayPrimitives maps a std::internal::builtin Array primitive to the method it lowers
// as. std/src/array.kizu forwards each method to one of these, and lowering the
// forward is what makes that line the implementation rather than a description
// of one.
var arrayPrimitives = map[string]string{
	"std::internal::builtin::array_append":       "append",
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
	"std::internal::builtin::array_truncate":     "truncate",
}

// mapPrimitives maps a std::internal::builtin Map primitive to the method it lowers as.
var mapPrimitives = map[string]string{
	"std::internal::builtin::map_contains": "contains",
	"std::internal::builtin::map_deinit":   "deinit",
	"std::internal::builtin::map_get":      "get",
	"std::internal::builtin::map_insert":   "insert",
	"std::internal::builtin::map_key_at":   "key_at",
	"std::internal::builtin::map_len":      "len",
}

// mapPrimitiveValueType returns V from the `[]u8, V` static arguments a Map
// primitive is applied to.
func mapPrimitiveValueType(typeArg string) string {
	args := splitStaticArgs(typeArg)
	if len(args) != 2 {
		return typeArg
	}
	return args[1]
}

// lowerMapMethod lowers the runtime primitive one std::map::Map<[]u8, V> method
// forwards to. Only a wrapper body in std/src/map.kizu reaches it: a `m.get(k)`
// call lowers as a call to that wrapper, and this is what the wrapper does.
func (l *lowerer) lowerMapMethod(name string, valueType string, args []Value) (Value, error) {
	switch name {
	case "insert":
		return l.emit("map.insert", "!void", args, valueType), nil
	case "get":
		return l.emit("map.get", "?"+valueType, args, valueType), nil
	case "key_at":
		return l.emit("map.key_at", "?[]u8", args, valueType), nil
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

// lowerArrayMethod lowers the runtime primitive one std::array::Array<T> method
// forwards to, reached the same way lowerMapMethod is. Only the methods whose
// result mentions the element type need to be spelled out here; the rest carry a
// fixed result type and go through arrayMethodResultType.
func (l *lowerer) lowerArrayMethod(name string, elem string, args []Value) (Value, error) {
	if result, ok := arrayMethodResultType(name); ok {
		return l.emit("array."+name, result, args, elem), nil
	}
	switch name {
	case "pop":
		return l.emit("array.pop", "?"+elem, args, elem), nil
	case "pop_or_panic":
		return l.emit("array.pop_or_panic", elem, args, elem), nil
	case "get":
		return l.emit("array.get", "?"+elem, args, elem), nil
	case "get_or_panic":
		return l.emit("array.get_or_panic", elem, args, elem), nil
	case "at":
		return l.emit("array.at", "?&"+elem, args, elem), nil
	case "at_mut":
		return l.emit("array.at_mut", "?&var "+elem, args, elem), nil
	case "as_mut_bytes":
		// The same view value as as_bytes: mutability is a checker-level
		// permission, not a different runtime representation (ADR-0096).
		return l.emit("array.as_bytes", "[]u8", args, elem), nil
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
	case "append", "reserve", "set", "truncate":
		return "!void", true
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
