package ownership

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdmeta"
	"github.com/kizu-lang/kizu/internal/stdmethod"
	"github.com/kizu-lang/kizu/internal/stdprim"
	"github.com/kizu-lang/kizu/internal/stdtarget"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Checker validates ownership and move rules for a parsed program.
type Checker struct {
	target       stdtarget.Target
	types        *typ.Table
	functions    map[string]*functionInfo
	impls        map[string]map[string]*functionInfo
	structs      map[string]map[string]string
	enums        map[string]map[string]bool
	errorSets    map[string]map[string]bool
	unions       map[string]map[string]string
	nextID       int
	consumeNeeds map[string]bool
	deinitOwners map[string]bool
	// releaseAllocators names the types whose deinit takes an allocator, which
	// a generic cleanup asks before calling one (ADR-0132).
	releaseAllocators map[string]bool
	// declaredDeinits names the types whose cleanup an author wrote. Those hold
	// an obligation of their own, so their fields cannot be taken one at a time.
	declaredDeinits map[string]bool
	structOrder     map[string][]string
	// structPublicOrder lists each struct's public fields in declaration order,
	// which is what `std::meta::public_fields` walks.
	structPublicOrder map[string][]string
	// enumOrder and unionOrder list each declaration's tags and variants in
	// source order, which is what `std::meta::variants` walks and the order a
	// `comptime match` builds its arms in.
	enumOrder  map[string][]string
	unionOrder map[string][]string
	// functionArgs binds the `Function` static arguments of the instantiation
	// being checked, so a call written against the parameter resolves to the
	// function it was given.
	functionArgs map[string]string
	// checkedInstances records the generic instantiations already checked, and
	// instantiationDepth counts those open above the current one (#1627).
	checkedInstances   map[string]bool
	instantiationDepth int
	// metaFields binds the captures of the `comptime for` expansions currently
	// open. A capture is not a value, so it is not a scope binding.
	metaFields map[string]metaField
	// loopStarts is the stack of loops the statement being checked sits in,
	// innermost last: what a `break` or `continue` leaves, and the binding
	// watermark its body's locals start at.
	loopStarts []loopStart
	// pendingOwnerTemps lists the expressions whose owner result the
	// statement being checked still holds and has handed nowhere: a call, a
	// struct literal, a field taken out of a value. An exit taken in the
	// middle of the statement would drop them, so a `try` or guard that can
	// leave while any are pending is refused. Entries are the producing
	// nodes, so a second read of the same expression adds nothing.
	pendingOwnerTemps []ast.Expression
	// pendingMovedPlaces are the bindings the statement being checked has
	// moved out of and no call or literal has taken yet. Their storage still
	// holds the value until the statement completes, so an exit taken in the
	// middle sees them as still held: the cleanup that covers them runs, and
	// one that covers nothing is the leak the exit reports.
	pendingMovedPlaces []*binding
	currentFunction    *functionInfo
	// collectMissingMarkers switches the missing `move` diagnostic from an
	// error into a recorded site, so MissingMoveMarkers can report every one.
	collectMissingMarkers bool
	missingMarkers        []MissingMarker
	currentStd            bool
	typeArgValues         map[string]string
	liveErrDefers         []errDeferEntry
	// pendingAllocTaints holds tied allocators read as call arguments whose
	// call result has not yet reached a `let`. The let attaches them to the new
	// binding; a statement ending with entries left over used the result some
	// other way, which would lose the tie, so checkBlock rejects it.
	pendingAllocTaints []allocTaint
	// captureCondition is set while an if/while borrow-optional condition is
	// checked. viewCaptureCall identifies the one ordinary optional-producing
	// call whose view-carrying payload the capture will tie to its inputs.
	// borrowReturn marks the value of a declared borrow-optional return.
	captureCondition bool
	viewCaptureCall  *ast.CallExpr
	borrowReturn     bool
	// returnedViews holds the expressions of the return being checked whose
	// view leaves the frame on its parameter ties: the value itself and the
	// fields of a struct literal it builds. The escape refusals let these
	// through; everything else the return evaluates is refused as usual.
	returnedViews map[ast.Expression]bool
	result        Result
}

// allocTaint is one tied allocator a call consumed while its result has not
// reached a `let` yet, and where that consumption happened.
type allocTaint struct {
	alloc *binding
	span  ast.Span
}

// errDeferEntry records one active errdefer cleanup whose receiver must stay
// valid on every error-return path that can run it. The binding is held by
// id rather than by name: a later `let` of the same name is a different
// value, and what retires this cleanup is the fate of the one it was written
// for.
type errDeferEntry struct {
	receiver ast.Expression
	name     string
	id       int
}

// A functionInfo is the ownership-facing view of a function: what a caller
// hands over, plus the body this checker walks. The two are separate fields so
// that reading the signature cannot reach the body by accident.
//
// name is not sig.Name: a method is registered under a qualified name, while
// the signature keeps the name as it was declared.
type functionInfo struct {
	name       string
	sig        ast.FunctionSignature
	params     []paramInfo
	returnType string
	body       *ast.BlockStmt
}

type paramInfo struct {
	name      string
	typeName  string
	borrow    bool
	mutBorrow bool
}

type binding struct {
	id               int
	name             string
	typeName         string
	mutable          bool
	moved            bool
	borrowedParam    bool
	localBorrow      bool
	borrowTargets    []borrowSource
	mutBorrow        bool
	activeBorrows    int
	activeMutBorrows int
	fieldBorrows     map[string]int
	fieldMutBorrows  map[string]int
	fieldDeinit      map[string]bool
	fieldArenaIDs    map[string]int
	arenaID          int
	handleArenaID    int
	deinitialized    bool
	consumeExempt    bool
	// ownsTied marks a capture that was handed its owner payload while the
	// payload's views keep it tied to the container it came from: the
	// binding owns the value (its `deinit` is its to call) but cannot move
	// it out of the frame (SPEC §9).
	ownsTied     bool
	deferCleanup bool
	declSpan     ast.Span
	// fieldOwner and fieldOwnerName link a direct-field receiver projection
	// back to the owner binding and its field, so a call-duration receiver
	// borrow lands where argument borrows of the same place land.
	fieldOwner     *binding
	fieldOwnerName string
}

// allocTied reports an owner allocated from a frame-tied allocator: it keeps
// its owner obligations (deinit) but cannot escape the frame. A value can hold
// borrow targets for another reason -- an aggregate that keeps a source view
// holds one too -- and that one escapes under the view rules, not this one, so
// the allocators are read out by type rather than taken to be every source.
func (b *binding) allocTied() bool {
	return !b.borrowedParam && len(b.tiedAllocatorSources()) > 0
}

// tiedAllocatorSources returns the tied allocators this value was built from.
// A value can hold borrow targets for other reasons -- an aggregate that keeps
// a source view holds one too -- so the allocators are read out by type rather
// than taken to be every source.
func (b *binding) tiedAllocatorSources() []*binding {
	sources := make([]*binding, 0, len(b.borrowTargets))
	for _, source := range b.borrowTargets {
		if source.target != nil && source.target.typeName == "Allocator" {
			sources = append(sources, source.target)
		}
	}
	return sources
}

// isMutBorrowParam reports whether the binding is itself a `&var` parameter:
// the caller's storage, which assigning the binding stores into.
func (b *binding) isMutBorrowParam() bool {
	return b.borrowedParam && !b.localBorrow && b.mutBorrow
}

// borrowSource is one owner a local borrow keeps active: the borrowed binding
// and, for a field borrow, the borrowed field name. A multi-source `borrows`
// result holds one entry per declared source.
type borrowSource struct {
	target *binding
	field  string
}

type scope struct {
	parent *scope
	values map[string]*binding
}

type directFieldReceiver struct {
	owner    *binding
	field    string
	typeName string
	path     string
}

type temporaryBorrow struct {
	value   *binding
	field   string
	mutable bool
}

// New creates an empty ownership checker.
func New() *Checker {
	return NewForTarget(stdtarget.Native)
}

// NewForTarget creates an ownership checker for one selected build target.
func NewForTarget(target stdtarget.Target) *Checker {
	return &Checker{
		target:       target,
		types:        typ.NewTable(),
		functions:    map[string]*functionInfo{},
		impls:        map[string]map[string]*functionInfo{},
		structs:      map[string]map[string]string{},
		enums:        map[string]map[string]bool{},
		errorSets:    map[string]map[string]bool{},
		unions:       map[string]map[string]string{},
		consumeNeeds: map[string]bool{},
		structOrder:  map[string][]string{},

		structPublicOrder: map[string][]string{},
		enumOrder:         map[string][]string{},
		unionOrder:        map[string][]string{},
		metaFields:        map[string]metaField{},
		functionArgs:      map[string]string{},
		checkedInstances:  map[string]bool{},
		result:            newResult(),
	}
}

// Result returns the phase output produced by Check. Callers use it only after
// Check succeeds, so lowering never consumes partial ownership facts.
func (c *Checker) Result() Result {
	return c.result
}

// Check validates ownership rules and returns the first move error.
func (c *Checker) Check(program *ast.Program) error {
	c.result = newResult()
	c.deinitOwners = ast.DeinitOwners(program)
	c.releaseAllocators = ast.ReleaseNamesAllocator(program)
	c.declaredDeinits = ast.DeclaredDeinits(program)
	if err := c.checkStructs(program); err != nil {
		return err
	}
	c.collectEnums(program)
	c.collectErrorSets(program)
	c.collectUnions(program)
	if err := c.checkUnionPayloads(); err != nil {
		return err
	}
	if err := c.collectFunctions(program); err != nil {
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

// MissingMarker names a place that hands a value off without the `move`
// marker. One span can be reached by several generic instantiations, so the
// list is deduplicated by position.
type MissingMarker struct {
	Span ast.Span
}

// MissingMoveMarkers reports every place that needs a `move` marker. Check
// stops at the first one because it is an error there; the formatter writes
// them all, so this records them and keeps checking. Recording does not change
// what the checker knows: the hand-off happens whether or not it is marked.
// Every other rule still fails fast, and a program that breaks one is returned
// with its error so callers do not rewrite source the checker rejects.
func (c *Checker) MissingMoveMarkers(program *ast.Program) ([]MissingMarker, error) {
	c.collectMissingMarkers = true
	defer func() { c.collectMissingMarkers = false }()
	err := c.Check(program)
	seen := map[ast.Position]bool{}
	markers := make([]MissingMarker, 0, len(c.missingMarkers))
	for _, marker := range c.missingMarkers {
		if seen[marker.Span.Start] {
			continue
		}
		seen[marker.Span.Start] = true
		markers = append(markers, marker)
	}
	return markers, err
}

// CheckAll validates ownership like Check but accumulates one error per
// top-level declaration instead of stopping at the first, so editors can show
// every independent move error at once. Setup phases still fail fast.
func (c *Checker) CheckAll(program *ast.Program) []error {
	c.result = newResult()
	c.deinitOwners = ast.DeinitOwners(program)
	c.releaseAllocators = ast.ReleaseNamesAllocator(program)
	c.declaredDeinits = ast.DeclaredDeinits(program)
	if err := c.checkStructs(program); err != nil {
		return []error{err}
	}
	c.collectEnums(program)
	c.collectErrorSets(program)
	c.collectUnions(program)
	if err := c.checkUnionPayloads(); err != nil {
		return []error{err}
	}
	if err := c.collectFunctions(program); err != nil {
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

// collectEnums records tag enum declarations for enum value reads.
func (c *Checker) collectEnums(program *ast.Program) {
	for _, decl := range program.Decls {
		enumDecl, ok := decl.(*ast.EnumDecl)
		if !ok {
			continue
		}
		tags := map[string]bool{}
		for _, tag := range enumDecl.Tags {
			tags[tag] = true
		}
		c.enums[enumDecl.Name] = tags
		c.enumOrder[enumDecl.Name] = append([]string(nil), enumDecl.Tags...)
	}
}

// collectErrorSets records error set declarations for error value reads. An
// error carries nothing, so reading one moves nothing. A combined set
// (`error C = A or B;`) records the names of its parts' members; which set a
// name resolves to is the types checker's question, not an ownership one.
func (c *Checker) collectErrorSets(program *ast.Program) {
	var combined []*ast.ErrorSetDecl
	for _, decl := range program.Decls {
		setDecl, ok := decl.(*ast.ErrorSetDecl)
		if !ok {
			continue
		}
		if len(setDecl.Combines) > 0 {
			combined = append(combined, setDecl)
			continue
		}
		members := map[string]bool{}
		for _, member := range setDecl.Members {
			members[member] = true
		}
		c.errorSets[setDecl.Name] = members
	}
	// A combined set may name a set declared later, or another combined set.
	// Each pass of a well-typed program resolves at least one declaration, so
	// the loop settles; an unresolved reference is left for the checker that
	// names it.
	for len(combined) > 0 {
		progress := false
		var remaining []*ast.ErrorSetDecl
		for _, setDecl := range combined {
			members := map[string]bool{}
			ready := true
			for _, ref := range setDecl.Combines {
				part, ok := c.errorSets[ref]
				if !ok {
					ready = false
					break
				}
				for member := range part {
					members[member] = true
				}
			}
			if !ready {
				remaining = append(remaining, setDecl)
				continue
			}
			c.errorSets[setDecl.Name] = members
			progress = true
		}
		combined = remaining
		if !progress {
			return
		}
	}
}

// collectUnions records tagged union declarations for variant construction and matches.
func (c *Checker) collectUnions(program *ast.Program) {
	for _, decl := range program.Decls {
		unionDecl, ok := decl.(*ast.UnionDecl)
		if !ok {
			continue
		}
		variants := map[string]string{}
		order := make([]string, 0, len(unionDecl.Variants))
		for _, variant := range unionDecl.Variants {
			variants[variant.Name] = stdmeta.ResolveElementTypeForms(
				typ.Text(variant.Payload))
			order = append(order, variant.Name)
		}
		c.unions[unionDecl.Name] = variants
		c.unionOrder[unionDecl.Name] = order
	}
}

// checkUnionPayloads rejects a union payload that is an error union around an
// owner. An owner inside `E!T` is released only by the `if` that opens it,
// and a union's cleanup arm is one direct call (SPEC §6.12): there is no
// place to open the payload, so the value could never be released.
func (c *Checker) checkUnionPayloads() error {
	for name, variants := range c.unions {
		for _, variant := range c.unionOrder[name] {
			elem, wrapper, ok := c.wrappedPayloadElem(variants[variant])
			if ok && wrapper == "error union" && c.valueTypeNeedsConsume(elem) {
				return errorf(
					"move error: union payload `%s::%s` cannot store an error union around an owner",
					name, variant)
			}
		}
	}
	return nil
}

// checkStructs rejects struct fields that would store a local borrow.
func (c *Checker) checkStructs(program *ast.Program) error {
	for _, decl := range program.Decls {
		st, ok := decl.(*ast.StructDecl)
		if !ok {
			continue
		}
		fields := map[string]string{}
		order := make([]string, 0, len(st.Fields))
		public := make([]string, 0, len(st.Fields))
		for _, field := range st.Fields {
			if field.Borrow {
				return errorf("borrow error: struct field `%s.%s` cannot store borrow",
					st.Name, field.Name)
			}
			fields[field.Name] = stdmeta.ResolveElementTypeForms(fieldOwnershipType(field))
			order = append(order, field.Name)
			if field.Public {
				public = append(public, field.Name)
			}
		}
		c.structs[st.Name] = fields
		c.structOrder[st.Name] = order
		c.structPublicOrder[st.Name] = public
	}
	return nil
}

// collectFunctions registers top-level signatures before body checks.
func (c *Checker) collectFunctions(program *ast.Program) error {
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		c.functions[fn.Name] = functionInfoFromDecl(fn.Name, fn)
		if err := c.collectReceiverMethod(fn); err != nil {
			return err
		}
	}
	return nil
}

// collectReceiverMethod files a `fn (self: T) name(...)` declaration under the
// type it is a method on. Its name already says which that is.
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
		methods = map[string]*functionInfo{}
		c.impls[receiver] = methods
	}
	if _, exists := methods[name]; exists {
		return errorf("move error: duplicate method `%s`", decl.Name)
	}
	info := functionInfoFromDecl(decl.Name, decl)
	// Method calls have no tied-allocator recognizer, so a method that built a
	// tied allocator from a borrowed buffer would hand it back untracked.
	// Free functions carry this shape; methods refuse it at the declaration.
	if capabilityReturn(returnTypeName(info)) && c.callTiesAllocator(info, nil, nil) {
		return errorf(
			"borrow error: method `%s` cannot return a tied capability; use a free function",
			decl.Name)
	}
	methods[name] = info
	return nil
}

// capabilityReturn reports the return types that are tied when they are built
// from a borrow. A capability is a permission to reach something, so one built
// out of local state cannot leave the frame that state lives in -- the same
// rule for the allocator over a local buffer and for the Io over a local loop.
func capabilityReturn(name string) bool {
	return name == "Allocator" || name == "Io"
}

// tiedStructReturn names the std struct returns that stay tied to what they
// were built from. A Future stands on borrowed state; a TaskSet retains the
// Io and allocator selected when it is made.
func tiedStructReturn(name string) bool {
	return name == "std::io::Future" || name == "io::Future" ||
		strings.HasSuffix(name, "!std::io::Future") || strings.HasSuffix(name, "!io::Future") ||
		taskSetReturn(name)
}

// taskSetReturn reports a TaskSet with or without its fallible-result wrapper.
func taskSetReturn(name string) bool {
	return name == "std::io::TaskSet" || name == "io::TaskSet" ||
		strings.HasSuffix(name, "!std::io::TaskSet") || strings.HasSuffix(name, "!io::TaskSet")
}

// tiedReturn reports the return types that are tied when they are built from a
// borrow.
func tiedReturn(name string) bool {
	return capabilityReturn(name) || tiedStructReturn(name)
}

// functionInfoFromDecl extracts the ownership-facing signature for a function.
func functionInfoFromDecl(name string, fn *ast.FunctionDecl) *functionInfo {
	params := make([]paramInfo, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, paramInfo{
			name: param.Name, typeName: typ.Text(param.TypeName),
			borrow: param.Borrow, mutBorrow: param.MutBorrow,
		})
	}
	return &functionInfo{
		name: name, sig: fn.FunctionSignature, params: params,
		returnType: typ.Text(fn.ReturnType), body: fn.Body,
	}
}

// checkFunction validates one function body.
func (c *Checker) checkFunction(fn *functionInfo) error {
	if fn.sig.ExternABI != "" {
		return nil
	}
	// A body that calls a `Function` static parameter has no callee until the
	// parameter is bound, so it is checked once per instantiation instead.
	if hasFunctionStaticParam(fn.sig) {
		return nil
	}
	env := newScope(nil)
	if err := c.defineParams(fn, env, nil); err != nil {
		return err
	}
	c.pendingAllocTaints = nil
	previousLoopStarts := c.loopStarts
	previousPending := c.pendingOwnerTemps
	previousPlaces := c.pendingMovedPlaces
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeArgValues := c.typeArgValues
	c.loopStarts = nil
	c.pendingOwnerTemps = nil
	c.pendingMovedPlaces = nil
	c.currentFunction = fn
	c.currentStd = fn.sig.Std
	c.typeArgValues = nil
	defer func() {
		c.loopStarts = previousLoopStarts
		c.pendingOwnerTemps = previousPending
		c.pendingMovedPlaces = previousPlaces
	}()
	defer func() { c.currentFunction = previousFunction }()
	defer func() { c.currentStd = previousStd }()
	defer func() { c.typeArgValues = previousTypeArgValues }()
	if err := c.checkBlock(fn.body, env); err != nil {
		return err
	}
	return c.checkDeinitCompleteness(fn, env)
}

// checkDeinitCompleteness enforces ADR-0091: a struct deinit must consume every
// owner field of its receiver. A union deinit is covered by the match dispatch
// rules, and a body that moves self on has handed the obligation elsewhere.
func (c *Checker) checkDeinitCompleteness(fn *functionInfo, env *scope) error {
	receiver, method, ok := stdmethod.SplitMethodName(fn.name)
	if !ok || method != "deinit" || !fn.sig.Receiver || len(fn.sig.Params) == 0 {
		return nil
	}
	fields := c.structs[receiver]
	if fields == nil {
		return nil
	}
	self, exists := env.lookup(fn.sig.Params[0].Name)
	if !exists || self.moved {
		return nil
	}
	for _, name := range c.structOrder[receiver] {
		if !c.fieldTypeNeedsConsume(fields[name]) || self.fieldDeinit[name] {
			continue
		}
		return errorf("move error: deinit of `%s` must consume owner field `%s`",
			receiver, name)
	}
	return nil
}

// defineParams binds a function's params into env, substituting generic type
// spellings when subst is non-nil.
func (c *Checker) defineParams(fn *functionInfo, env *scope, subst map[string]string) error {
	// A `<...>` entry that declares a type is a compile-time value, in scope
	// for the body like a parameter but never moved or borrowed.
	for _, param := range fn.sig.StaticParams {
		if param.IsType() {
			continue
		}
		env.define(c.newBinding(param.Name, typ.Text(param.Type)))
	}
	for idx, param := range fn.sig.Params {
		typeName := fn.params[idx].typeName
		if subst != nil {
			typeName = substituteOwnershipType(typeName, subst)
		}
		// A parameter is storage, so it obeys the same rule as a binding:
		// an optional whose payload owns memory or carries a view cannot
		// cross a call boundary as a value.
		if err := c.rejectStoredOptional(typeName); err != nil {
			return err
		}
		value := c.newBinding(param.Name, typeName)
		value.borrowedParam = fn.params[idx].borrow
		value.mutBorrow = fn.params[idx].mutBorrow
		receiver := fn.sig.Receiver && idx == 0
		if receiver && !value.borrowedParam && !value.mutBorrow && !functionConsumesReceiver(fn) {
			// A by-value receiver of a method that does not consume it is the
			// caller's value read in place (SPEC §8): the body holds it the
			// way a `&T` parameter is held, so nothing can be taken out of it.
			value.borrowedParam = true
		}
		// A method receiver is written as a by-value param but is not a
		// consuming transfer (SPEC §14: mutators are callable from owned
		// locals), and a consume primitive keeps its value by design
		// (ADR-0091): neither carries a consume obligation.
		value.consumeExempt = receiver || isConsumePrimitive(fn.name)
		env.define(value)
	}
	return nil
}

// functionConsumesReceiver reports whether a method takes its receiver over:
// `deinit` by contract (SPEC §8), and a std method written with a by-value
// receiver whose result is not a borrow of it -- `Box.take` hands the payload
// out and releases the cell -- by its compiler-known signature (SPEC §14.4).
// Every other by-value receiver is read in place.
func functionConsumesReceiver(fn *functionInfo) bool {
	if !fn.sig.Receiver || len(fn.params) == 0 || fn.params[0].borrow || fn.params[0].mutBorrow {
		return false
	}
	if stdmethod.CallName(fn.sig.Name) == typ.CleanupMethod && returnTypeName(fn) == "void" {
		return true
	}
	return fn.sig.Std && !strings.HasPrefix(returnTypeName(fn), "&")
}

// isConsumePrimitive names the std functions whose whole job is consuming an
// owner: their param carries no obligation and their argument is moved.
func isConsumePrimitive(name string) bool {
	return name == "std::mem::leak"
}

// checkTestDecl validates a top-level test block as an errorable, parameterless body.
func (c *Checker) checkTestDecl(decl *ast.TestDecl) error {
	// The name carries the module so the body reads its own module's names --
	// a function pointer's value is looked up through the prefix of whatever
	// is being checked, and a synthetic name without one finds nothing.
	name := "test " + strconv.Quote(decl.Name)
	if decl.Module != "" {
		name = decl.Module + "::" + name
	}
	fn := functionInfoFromDecl(name, &ast.FunctionDecl{
		FunctionSignature: ast.FunctionSignature{
			Name:       name,
			ReturnType: &typ.ErrorUnion{Ok: &typ.Name{Path: []string{"void"}}},
		},
		Body: decl.Body,
	})
	return c.checkFunction(fn)
}

// checkBlock validates statements in a lexical block.
func (c *Checker) checkBlock(block *ast.BlockStmt, env *scope) error {
	lastUses := blockLastUses(block)
	defers := []ast.Expression{}
	errDeferMark := len(c.liveErrDefers)
	defer c.restoreErrDefers(errDeferMark)
	// Bindings declared inside this block carry IDs above the watermark; the
	// fall-through leak check below is scoped to exactly those.
	bindingMark := c.nextID
	// A statement starts with the owners its enclosing expression, if any,
	// still holds pending; what the statement itself produces is settled by
	// the time it ends.
	pendingMark, placesMark := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	// The block leaves nothing of its own pending either: a `return` is a
	// statement too, and a body checked once per expansion starts each time
	// from what its enclosing statement held.
	defer func() {
		c.pendingOwnerTemps = c.pendingOwnerTemps[:pendingMark]
		c.pendingMovedPlaces = c.pendingMovedPlaces[:placesMark]
	}()
	for idx, stmt := range block.Statements {
		c.pendingOwnerTemps = c.pendingOwnerTemps[:pendingMark]
		c.pendingMovedPlaces = c.pendingMovedPlaces[:placesMark]
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if err := c.checkDeferStmt(deferStmt, env); err != nil {
				return err
			}
			defers = append(defers, deferStmt.Expr)
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if errDeferStmt, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := c.checkErrDeferStmt(errDeferStmt, env); err != nil {
				return err
			}
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if err := c.checkStmt(stmt, env); err != nil {
			return err
		}
		if len(c.pendingAllocTaints) > 0 {
			span := c.pendingAllocTaints[0].span
			c.pendingAllocTaints = nil
			return errorAt(span,
				"borrow error: a value allocated from a tied allocator must be bound with `let`")
		}
		env.releaseLastUseBorrows(idx, lastUses)
	}
	if err := c.checkDeferredCleanups(defers, env); err != nil {
		return err
	}
	if blockTerminates(block) {
		return nil
	}
	return c.checkOwnersConsumed(env, bindingMark, leakExit{})
}

// checkStmt validates one statement.
func (c *Checker) checkStmt(stmt ast.Statement, env *scope) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return c.checkLetStmt(s, env)
	case *ast.AssignStmt:
		return c.checkAssignStmt(s, env)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(s, env)
	case *ast.DeferStmt:
		return errorf("move error: defer statement must appear directly in a block")
	case *ast.ErrDeferStmt:
		return errorf("move error: errdefer statement must appear directly in a block")
	case *ast.ExprStmt:
		return c.checkExprStmt(s, env)
	default:
		return c.checkBodyStmt(stmt, env)
	}
}

// checkBodyStmt checks the statements that carry a body of their own, and the
// branches that leave one.
func (c *Checker) checkBodyStmt(stmt ast.Statement, env *scope) error {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		return c.checkIfStmt(s, env)
	case *ast.WhileStmt:
		return c.checkWhileStmt(s, env)
	case *ast.ForStmt:
		return c.checkForStmt(s, env)
	case *ast.BreakStmt:
		return c.checkLoopBranch(s.Label, exitBreak, env)
	case *ast.ContinueStmt:
		return c.checkLoopBranch(s.Label, exitContinue, env)
	case *ast.MatchStmt:
		return c.checkMatchStmt(s, env)
	case *ast.BlockStmt:
		// Only a match arm body is a bare block statement (SPEC §6.12).
		return c.checkBlock(s, env)
	case *ast.ComptimeIfStmt:
		return c.checkComptimeIfStmt(s, env)
	case *ast.ComptimeMatchStmt:
		return c.checkComptimeMatchStmt(s, env)
	case *ast.ComptimeForStmt:
		return c.checkComptimeForStmt(s, env)
	default:
		return errorf("move error: unsupported statement %T", stmt)
	}
}

// checkDeferStmt registers a cleanup that runs on every later exit of this
// block.
func (c *Checker) checkDeferStmt(stmt *ast.DeferStmt, env *scope) error {
	return c.registerCleanup(cleanupDefer, stmt.Expr, env)
}

// checkErrDeferStmt registers a cleanup that runs on every later error exit
// of this block.
func (c *Checker) checkErrDeferStmt(stmt *ast.ErrDeferStmt, env *scope) error {
	return c.registerCleanup(cleanupErrDefer, stmt.Expr, env)
}

// cleanupKind is the exit set a registered cleanup runs on.
type cleanupKind int

const (
	// cleanupDefer runs on every exit of the block.
	cleanupDefer cleanupKind = iota
	// cleanupErrDefer runs on every error exit of the block.
	cleanupErrDefer
)

// keyword names the statement that registers a cleanup of this kind.
func (kind cleanupKind) keyword() string {
	if kind == cleanupErrDefer {
		return "errdefer"
	}
	return "defer"
}

// registerCleanup validates one cleanup registration and records it. Both
// kinds name a cleanup method call on a receiver read where the statement is
// written; the receiver's arguments are read there too (ADR-0132). A value
// carries one registration at most. What differs is when the cleanup runs: a
// defer discharges the receiver's consume obligation (ADR-0091) from here on,
// while an errdefer is not applied at normal block exit, so it never blocks a
// success-path move and its receiver is re-validated at each error-return
// path that can run it.
func (c *Checker) registerCleanup(kind cleanupKind, expr ast.Expression, env *scope) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return errorf("move error: %s expects cleanup method call", kind.keyword())
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok {
		return errorf("move error: %s expects cleanup method call", kind.keyword())
	}
	if field.Name != typ.CleanupMethod {
		return errorf("move error: %s cleanup must be `%s`, got `%s`",
			kind.keyword(), typ.CleanupMethod, field.Name)
	}
	receiverType, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return err
	}
	if err := c.checkDeferredReleaseArgs(receiverType, field, call.Args, env); err != nil {
		return err
	}
	var value *binding
	if ident, ok := field.Receiver.(*ast.IdentExpr); ok {
		if found, exists := env.lookup(ident.Name); exists {
			if err := c.checkCleanupNotRegistered(found, field.Receiver); err != nil {
				return err
			}
			value = found
		}
	}
	switch kind {
	case cleanupDefer:
		if value != nil {
			value.deferCleanup = true
		}
	case cleanupErrDefer:
		entry := errDeferEntry{receiver: field.Receiver}
		if value != nil {
			entry.name, entry.id = value.name, value.id
		}
		c.liveErrDefers = append(c.liveErrDefers, entry)
	}
	return nil
}

// checkCleanupNotRegistered rejects a second cleanup registration for one
// value. A `defer` already releases it on every exit and an `errdefer` on
// every error exit, so another of either would release it twice on the path
// both run.
func (c *Checker) checkCleanupNotRegistered(value *binding, receiver ast.Expression) error {
	keyword := ""
	switch {
	case value.deferCleanup:
		keyword = "defer"
	case c.errDeferCoversID(value.id):
		keyword = "errdefer"
	default:
		return nil
	}
	return errorAt(expressionSpan(receiver),
		"move error: `%s` already has a registered `%s` cleanup;"+
			" a second cleanup would release it twice", value.name, keyword)
}

// checkDeferredReleaseArgs reads the arguments a registered cleanup carries.
// They are read where the defer is written, not where it runs, so what runs at
// scope exit is settled at the point the source names (ADR-0132), and the tie
// between a value and the allocator that made it is checked here too.
func (c *Checker) checkDeferredReleaseArgs(
	receiverType string,
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) error {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return err
		}
	}
	if len(args) != 1 {
		return nil
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	receiver, exists := env.lookup(ident.Name)
	if !exists {
		return nil
	}
	return c.checkReleaseTie(releaseLabel(receiverType), receiver, args[0], env)
}

// releaseLabel names a release the way its diagnostics spell it: the receiver
// type's own name, without the module path or static arguments.
func releaseLabel(receiverType string) string {
	name := strings.TrimPrefix(strings.TrimPrefix(receiverType, "&var "), "&")
	if base, _, ok := splitGenericType(name); ok {
		name = base
	}
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	return name + "." + typ.CleanupMethod
}

// restoreErrDefers drops errdefer entries registered inside an exited block.
func (c *Checker) restoreErrDefers(mark int) {
	c.liveErrDefers = c.liveErrDefers[:mark]
}

// validateErrDeferReceivers reports the active errdefer cleanups that this
// error-return path must skip, and rejects the ones it cannot run.
//
// A consumed receiver retires its cleanup (ADR-0114). A move hands the
// obligation to a new owner and an explicit `deinit` discharges it outright;
// either way the value is gone, so running the cleanup here would release what
// this frame no longer holds. A borrowed receiver is different: it is still
// live and cannot be consumed at all, so it stays an error. This runs at every
// error path that could trigger the cleanup.
func (c *Checker) validateErrDeferReceivers(env *scope) ([]ast.Expression, error) {
	var retired []ast.Expression
	for _, entry := range c.liveErrDefers {
		if entry.id == 0 {
			continue
		}
		value, exists := env.lookupID(entry.id)
		if !exists {
			continue
		}
		if bindingConsumed(value) {
			retired = append(retired, entry.receiver)
			continue
		}
		if value.activeBorrows > 0 || value.activeMutBorrows > 0 {
			return nil, errorf(
				"borrow error: errdefer cleanup receiver `%s` is borrowed on an error path",
				entry.name)
		}
	}
	return retired, nil
}

// valueTypeNeedsConsume reports whether typeName carries a deinit contract, the
// class of values ADR-0091 requires every exit path to consume. Owner-ness has
// one definition, ast.DeinitOwners, seeded per program in Check / CheckAll.
func (c *Checker) valueTypeNeedsConsume(typeName string) bool {
	if needs, ok := c.consumeNeeds[typeName]; ok {
		return needs
	}
	needs := ast.OwnerType(c.deinitOwners, typeName)
	c.consumeNeeds[typeName] = needs
	return needs
}

// fieldTypeNeedsConsume reports whether a struct field owes cleanup. A `?Owner`
// field owes it on the path where the value is there, so the field carries the
// obligation the same way a plain owner field does; the deinit that discharges
// it opens the optional first.
func (c *Checker) fieldTypeNeedsConsume(typeName string) bool {
	if elem, ok := typ.OptionalElem(typeName); ok {
		return c.valueTypeNeedsConsume(elem)
	}
	return c.valueTypeNeedsConsume(typeName)
}

// resultTypeNeedsConsume reports whether a produced value owes cleanup once it
// is unwrapped. `?T` and `E!T` are the two ways an owner reaches a caller
// without being the type itself: `pop` hands back `?String`, `box` hands back
// `!Box<T>`. Both are consumed the moment the value is bound, so the obligation
// is the element's.
func (c *Checker) resultTypeNeedsConsume(typeName string) bool {
	for {
		if _, success, isUnion := c.errorUnionParts(typeName); isUnion {
			typeName = success
			continue
		}
		if elem, ok := typ.OptionalElem(typeName); ok {
			typeName = elem
			continue
		}
		return c.valueTypeNeedsConsume(typeName)
	}
}

// bindingNeedsConsume reports whether a binding still owes its cleanup: an
// owned deinit-carrying value that no move, deinit, or registered defer has
// discharged yet.
func (c *Checker) bindingNeedsConsume(value *binding) bool {
	if value.moved || value.deinitialized || value.deferCleanup {
		return false
	}
	if value.borrowedParam || value.localBorrow || value.consumeExempt {
		return false
	}
	if value.handleArenaID != 0 {
		return false
	}
	if c.allOwnerFieldsConsumed(value) {
		return false
	}
	return c.valueTypeNeedsConsume(value.typeName)
}

// partiallyConsumedField names one field already taken out of a value that
// still holds others. Such a value no longer matches its own type: handing it
// on whole, lending it, or running its cleanup would all reach a field that is
// gone, so it stays where it is until the rest are taken too.
func partiallyConsumedField(value *binding) (string, bool) {
	for name := range value.fieldDeinit {
		return name, true
	}
	return "", false
}

// allOwnerFieldsConsumed reports whether a value taken apart field by field has
// nothing left to consume. A type whose obligation is its fields' obligations
// is discharged once each of them is, which is the same thing its derived
// deinit does in one call. A type that declares its own deinit holds one more
// obligation that no field consume reaches, so it never reads as done here.
func (c *Checker) allOwnerFieldsConsumed(value *binding) bool {
	if len(value.fieldDeinit) == 0 || ast.OwnerType(c.declaredDeinits, value.typeName) {
		return false
	}
	fields := c.structs[value.typeName]
	if fields == nil {
		return false
	}
	for name, typeName := range fields {
		if c.fieldTypeNeedsConsume(typeName) && !value.fieldDeinit[name] {
			return false
		}
	}
	return true
}

// leakExit names the exit a leak check guards. An early error exit points its
// diagnostic at the exit that leaks — a later cleanup exists but is never
// reached — while a plain exit points at the declaration nothing ever cleans.
type leakExit struct {
	kind leakExitKind
	span ast.Span
}

type leakExitKind int

const (
	exitPlain leakExitKind = iota
	exitTry
	exitErrorReturn
	exitBreak
	exitContinue
)

// loopStart is one loop the statement being checked sits in: its label, and
// the binding id its body's locals start after. A `break` or `continue`
// leaves that body early, so everything declared past the watermark must
// already be released.
type loopStart struct {
	label   string
	sinceID int
}

// checkOwnersConsumed rejects an exit that would leak a live owner. sinceID
// limits the check to bindings declared after that watermark: a function exit
// passes 0 and checks everything live, a block fall-through passes the ID the
// block started at and checks only its own declarations. On an error path a
// registered errdefer cleanup counts as the consume.
func (c *Checker) checkOwnersConsumed(env *scope, sinceID int, exit leakExit) error {
	errorPath := exit.kind == exitTry || exit.kind == exitErrorReturn
	var leaked *binding
	env.walkBindings(func(value *binding) {
		if value.id <= sinceID || !c.bindingNeedsConsume(value) {
			return
		}
		if errorPath && c.errDeferCoversID(value.id) {
			return
		}
		if leaked == nil || value.id > leaked.id {
			leaked = value
		}
	})
	if leaked == nil {
		return nil
	}
	return leakError(leaked, exit)
}

// leakError reports one unconsumed owner: at the early error exit that skips
// its cleanup, or at its declaration when nothing ever cleans it.
func leakError(value *binding, exit leakExit) error {
	switch exit.kind {
	case exitTry:
		return errorAt(exit.span,
			"move error: owned value `%s` would leak on this `try`'s error exit;"+
				" register `defer` or `errdefer` cleanup before it", value.name)
	case exitErrorReturn:
		return errorAt(exit.span,
			"move error: owned value `%s` would leak on this error return;"+
				" register `defer` or `errdefer` cleanup before it", value.name)
	case exitBreak:
		return errorAt(value.declSpan,
			"move error: owned value `%s` is not released before `break` leaves the loop",
			value.name)
	case exitContinue:
		return errorAt(value.declSpan,
			"move error: owned value `%s` is not released before `continue` ends the turn",
			value.name)
	}
	return errorAt(value.declSpan, "move error: owned value `%s` is never deinitialized",
		value.name)
}

// checkCleanupReceiverOverwrite rejects assigning over a binding that a `defer`
// or `errdefer` names. A registered cleanup releases the value that was live
// when it was written, not whatever the name later stands for, so an overwrite
// would leave the cleanup holding a value the name no longer means. Giving the
// new owner its own name keeps one name to one value to one cleanup.
func (c *Checker) checkCleanupReceiverOverwrite(target *binding, stmt *ast.AssignStmt) error {
	keyword := ""
	switch {
	case target.deferCleanup:
		keyword = "defer"
	case c.errDeferCoversID(target.id):
		keyword = "errdefer"
	default:
		return nil
	}
	return errorAt(expressionSpan(stmt.Value),
		"move error: `%s` cleanup receiver `%s` cannot be assigned over;"+
			" bind the new value to its own name", keyword, target.name)
}

// errDeferCoversID reports whether an active errdefer cleans up the binding
// with this id.
func (c *Checker) errDeferCoversID(id int) bool {
	if id == 0 {
		return false
	}
	for _, entry := range c.liveErrDefers {
		if entry.id == id {
			return true
		}
	}
	return false
}

// blockTerminates reports whether a block always exits through a return, in
// which case its fall-through path is unreachable and branch effects cannot
// leak past it.
func blockTerminates(block *ast.BlockStmt) bool {
	if block == nil || len(block.Statements) == 0 {
		return false
	}
	return stmtTerminates(block.Statements[len(block.Statements)-1])
}

// stmtTerminates reports whether a statement always returns. Exhaustiveness of
// a match is the type checker's promise, so all arms returning is enough here.
func stmtTerminates(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return blockTerminates(s)
	case *ast.IfStmt:
		return s.Alternative != nil && blockTerminates(s.Consequence) &&
			blockTerminates(s.Alternative)
	case *ast.MatchStmt:
		if len(s.Arms) == 0 {
			return false
		}
		for _, arm := range s.Arms {
			if !stmtTerminates(arm.Body) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// checkDeferredCleanups applies deferred cleanup effects in reverse order.
func (c *Checker) checkDeferredCleanups(defers []ast.Expression, env *scope) error {
	noLaterUses := map[string]int{}
	for idx := len(defers) - 1; idx >= 0; idx-- {
		stmt := &ast.ExprStmt{Expr: defers[idx], Semicolon: true}
		if err := c.checkExprStmt(stmt, env); err != nil {
			return err
		}
		// A later defer may release a source retained by the value just
		// consumed. Model runtime's reverse order one cleanup at a time.
		env.releaseLastUseBorrows(0, noLaterUses)
	}
	return nil
}

// checkReturnStmt rejects borrowed values before applying normal move rules.
func (c *Checker) checkReturnStmt(stmt *ast.ReturnStmt, env *scope) error {
	// A deinit owes its receiver's owner fields on every path that leaves it,
	// not only on the one that falls off the end.
	if err := c.checkDeinitCompleteness(c.currentFunction, env); err != nil {
		return err
	}
	if stmt.Value == nil {
		return c.checkOwnersConsumed(env, 0, leakExit{})
	}
	if done, err := c.checkReturnValueEscapes(stmt, env); done {
		return err
	}
	// An error return runs active errdefer cleanups before the function exits,
	// so their receivers must still be valid here. A success return transfers
	// the owner instead and must not be blocked by the cleanup it skips.
	errorPath := c.returnTakesErrorPath(stmt.Value, env)
	if errorPath {
		retired, err := c.validateErrDeferReceivers(env)
		if err != nil {
			return err
		}
		c.result.returnRetiredErrDefers[stmt] = retired
	}
	saved := c.borrowReturn
	if c.currentFunction != nil {
		if _, _, bare := typ.BorrowOptionalElem(returnTypeName(c.currentFunction)); bare {
			// A declared borrow-optional return is the second consumer of
			// `?&T`: the returned borrow flows on to the caller's capture.
			c.borrowReturn = true
		}
	}
	_, err := c.moveExpr(stmt.Value, env)
	c.borrowReturn = saved
	c.returnedViews = nil
	if err != nil {
		return err
	}
	exit := leakExit{}
	if errorPath {
		exit = leakExit{kind: exitErrorReturn, span: expressionSpan(stmt.Value)}
	}
	return c.checkOwnersConsumed(env, 0, exit)
}

// returnTakesErrorPath reports whether returning expr exits through the error
// path. What decides it is the type of the returned value, not the shape of the
// expression: a member of an error set fails, and so does an error union being
// propagated, whether either is written out or bound to a local first.
func (c *Checker) returnTakesErrorPath(expr ast.Expression, env *scope) bool {
	typeName, ok := c.returnedTypeName(expr, env)
	if !ok {
		return false
	}
	if c.errorSets[typeName] != nil {
		return true
	}
	_, _, isUnion := c.errorUnionParts(typeName)
	return isUnion
}

// returnedTypeName reads the type of a returned expression without moving it.
func (c *Checker) returnedTypeName(expr ast.Expression, env *scope) (string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		value, exists := env.lookup(e.Name)
		if !exists {
			return "", false
		}
		return value.typeName, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return "", false
		}
		name, err := c.readNamespaceExpr(e)
		if err != nil {
			return "", false
		}
		return name, true
	default:
		return "", false
	}
}

// checkReturnValueEscapes vets a returned value for frame escapes: borrowed
// bindings, arena handles, and tied-allocator factory calls. done means the
// return is fully checked here, including any consume checking it needs;
// every error comes back with done set.
func (c *Checker) checkReturnValueEscapes(stmt *ast.ReturnStmt, env *scope) (bool, error) {
	// A view still backed by a borrow — the binding itself, a view derived
	// from it, or a struct literal capturing either — leaves with its ties.
	// Ties to parameters flow on to the caller, which ties the result to what
	// it lent (ADR-0098), so such a view may go; one tied to local state is
	// refused where the value is consumed, as any other escape is.
	if roots := c.returnedViewRoots(stmt.Value, env); len(roots) > 0 && paramRootedAll(roots) {
		c.returnedViews = returnedViewExprs(stmt.Value, map[ast.Expression]bool{})
	}
	if ident, ok := stmt.Value.(*ast.IdentExpr); ok && !c.returnedViews[ident] {
		if done, err := c.checkReturnedBindingEscapes(ident, env); done {
			return true, err
		}
	}
	if arena := c.arenaAddReceiver(stmt.Value, env); arena != nil && arena.arenaID != 0 &&
		!c.arenaOutlivesFrame(arena.arenaID, env) {
		return true, errorf("arena error: handle from `%s` cannot outlive its arena", arena.name)
	}
	if handled, err := c.checkArenaBorrowReturnExit(stmt.Value, env); handled {
		return true, err
	}
	if call, ok := stmt.Value.(*ast.CallExpr); ok {
		if handled, err := c.checkTiedAllocatorReturn(call, env); handled {
			if err != nil {
				return true, err
			}
			return true, c.checkOwnersConsumed(env, 0, leakExit{})
		}
	}
	return false, nil
}

// checkReturnedBindingEscapes vets a returned binding for frame escapes: a
// borrow leaves only as the declared borrow return of a parameter, and an
// arena handle only when its arena outlives the frame.
func (c *Checker) checkReturnedBindingEscapes(ident *ast.IdentExpr, env *scope) (bool, error) {
	value, exists := env.lookup(ident.Name)
	if !exists {
		return false, nil
	}
	if value.borrowedParam {
		if c.borrowedReturnAllowed(ident.Name, value) {
			return true, nil
		}
		return true, errorAt(ident.Span, "borrow error: borrowed value `%s` cannot escape",
			ident.Name)
	}
	if value.handleArenaID != 0 && !c.arenaOutlivesFrame(value.handleArenaID, env) {
		return true, errorAt(ident.Span, "arena error: handle `%s` cannot outlive its arena",
			ident.Name)
	}
	return false, nil
}

// checkArenaBorrowReturnExit completes the return path once Arena.at is known
// to be the returned borrow, including the ordinary owner-exit check.
func (c *Checker) checkArenaBorrowReturnExit(
	expr ast.Expression,
	env *scope,
) (bool, error) {
	handled, err := c.checkArenaBorrowReturn(expr, env)
	if !handled || err != nil {
		return handled, err
	}
	return true, c.checkOwnersConsumed(env, 0, leakExit{})
}

// checkArenaBorrowReturn validates the structural source of a direct
// Arena.at borrow return. A source rooted in a borrow parameter outlives the
// frame by contract; a source rooted in local state would dangle.
func (c *Checker) checkArenaBorrowReturn(
	expr ast.Expression,
	env *scope,
) (bool, error) {
	if c.currentFunction == nil {
		return false, nil
	}
	_, returnMutable, returnElem, ok := explicitOwnershipBorrowType(
		returnTypeName(c.currentFunction),
	)
	if !ok || returnMutable {
		return false, nil
	}
	source, elem, ok, err := c.arenaAtBorrowSource(expr, env)
	if err != nil || !ok {
		return ok, err
	}
	if !sameOwnershipType(elem, returnElem) {
		return false, nil
	}
	if !paramRootedBinding(source.target) {
		return true, errorAt(expressionSpan(expr),
			"borrow error: Arena.at borrow from local `%s` cannot escape", source.target.name)
	}
	return true, nil
}

// checkTiedAllocatorReturn handles `return <factory>(...)` for tied-allocator
// factories. Sources rooted in the caller's own parameters travel with the
// signature — the caller re-derives the tie from its arguments — while a
// source rooted in local state would dangle and is rejected. Every error
// comes back with handled set.
func (c *Checker) checkTiedAllocatorReturn(call *ast.CallExpr, env *scope) (bool, error) {
	name, fn := c.calledFunction(call.Callee, env)
	if fn == nil || !tiedReturn(returnTypeName(fn)) {
		return false, nil
	}
	mutable := !taskSetReturn(returnTypeName(fn))
	sources, err := c.callBorrowReturnSources(name, fn, call, mutable, true, false, env)
	if err != nil {
		return true, err
	}
	if len(sources) == 0 {
		return false, nil
	}
	for _, source := range sources {
		if !paramRootedBinding(source.target) {
			return true, errorf(
				"borrow error: `%s` returns a value tied to local state and cannot escape",
				name)
		}
	}
	if err := c.checkTiedFactoryCall(name, call, env); err != nil {
		return true, err
	}
	return true, nil
}

// returnedViewRoots resolves the borrow-class bindings backing the views a
// returned expression carries out: the expression's own, or those of every
// field of a struct literal it builds.
func (c *Checker) returnedViewRoots(expr ast.Expression, env *scope) []*binding {
	if literal, ok := unwrapExpressionMarkers(expr).(*ast.StructLiteralExpr); ok {
		var roots []*binding
		for _, field := range literal.Fields {
			roots = mergeViewRoots(roots, c.returnedViewRoots(field.Value, env))
		}
		return roots
	}
	return c.borrowClassViewRoots(expr, env)
}

// paramRootedAll reports whether every binding's provenance ends in parameters.
func paramRootedAll(roots []*binding) bool {
	for _, root := range roots {
		if !paramRootedBinding(root) {
			return false
		}
	}
	return true
}

// returnedViewExprs collects the expressions whose view a return hands out:
// the returned expression and, through struct literals, their field values.
func returnedViewExprs(expr ast.Expression, into map[ast.Expression]bool) map[ast.Expression]bool {
	into[expr] = true
	if literal, ok := unwrapExpressionMarkers(expr).(*ast.StructLiteralExpr); ok {
		for _, field := range literal.Fields {
			returnedViewExprs(field.Value, into)
		}
	}
	return into
}

// paramRootedBinding reports whether a binding's provenance chain ends only in
// function parameters.
func paramRootedBinding(value *binding) bool {
	if len(value.borrowTargets) > 0 {
		for _, source := range value.borrowTargets {
			if !paramRootedBinding(source.target) {
				return false
			}
		}
		return true
	}
	return value.borrowedParam && !value.localBorrow
}

// borrowedReturnAllowed permits returning a borrowed source parameter. Every
// borrow parameter is a presumed provenance source (ADR-0098), so what remains
// to check is that the parameter's shape matches the declared borrow return.
func (c *Checker) borrowedReturnAllowed(name string, value *binding) bool {
	if c.currentFunction == nil {
		return false
	}
	_, mutable, inner, ok := explicitOwnershipBorrowType(returnTypeName(c.currentFunction))
	if !ok {
		return false
	}
	if mutable && !value.mutBorrow {
		return false
	}
	if !sameOwnershipType(value.typeName, inner) {
		return false
	}
	for idx, param := range c.currentFunction.sig.Params {
		if param.Name != name || !c.currentFunction.params[idx].borrow {
			continue
		}
		return !mutable || c.currentFunction.params[idx].mutBorrow
	}
	return false
}

// checkLetStmt moves the initializer into a new binding when needed.
func (c *Checker) checkLetStmt(stmt *ast.LetStmt, env *scope) error {
	if borrow, ok := borrowPrefix(stmt.Value); ok {
		return c.checkBorrowLetStmt(stmt, borrow, env)
	}
	target, field, elem, mutable, ok, err := c.boxBorrowInitializer(stmt.Value, env)
	if ok || err != nil {
		if err != nil {
			return err
		}
		return c.checkBoxBorrowLetStmt(stmt, target, field, elem, mutable, env)
	}
	if target, path, mutable, ok := c.stringViewInitializer(stmt.Value, env); ok {
		return c.checkStringViewLetStmt(stmt, target, path, mutable, env)
	}
	if handled, err := c.checkExplicitBorrowResultLetStmt(stmt, env); handled {
		return err
	}
	sources, elem, mutable, ok, err := c.returnedBorrowInitializer(stmt.Value, env)
	if ok || err != nil {
		if err != nil {
			return err
		}
		return c.checkReturnedBorrowLetStmt(stmt, sources, elem, mutable, env)
	}
	if handled, err := c.checkCaptureLetStmt(stmt, env); handled || err != nil {
		return err
	}
	typeName, err := c.moveExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	if err := c.rejectStoredOptional(typeName); err != nil {
		return err
	}
	value := c.newBinding(stmt.Name, typeName)
	value.mutable = stmt.Mutable
	value.declSpan = expressionSpan(stmt.Value)
	c.setArenaProvenance(value, stmt.Value, env)
	if err := c.attachAllocProvenance(value); err != nil {
		return err
	}
	env.define(value)
	return nil
}

// checkExplicitBorrowResultLetStmt recognizes Arena.at and declared borrow
// returns before the view-capture path, because &Owner binds a reference and
// must not inherit the owner's deinit obligation.
func (c *Checker) checkExplicitBorrowResultLetStmt(
	stmt *ast.LetStmt,
	env *scope,
) (bool, error) {
	source, elem, ok, err := c.arenaAtBorrowSource(stmt.Value, env)
	if ok || err != nil {
		if err != nil {
			return true, err
		}
		return true, c.checkExplicitReturnedBorrowLetStmt(
			stmt, []borrowSource{source}, elem, false, env,
		)
	}
	sources, elem, mutable, ok, err := c.explicitReturnedBorrowInitializer(stmt.Value, env)
	if !ok || err != nil {
		return ok || err != nil, err
	}
	return true, c.checkExplicitReturnedBorrowLetStmt(stmt, sources, elem, mutable, env)
}

// explicitReturnedBorrowInitializer separates a declared &T or &var T result
// from owners that merely capture views. Both carry structural sources, but
// only the latter retain an ownership obligation in the caller.
func (c *Checker) explicitReturnedBorrowInitializer(
	expr ast.Expression,
	env *scope,
) ([]borrowSource, string, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, "", false, false, nil
	}
	_, fn := c.calledFunction(call.Callee, env)
	if fn == nil {
		return nil, "", false, false, nil
	}
	if _, _, _, ok := explicitOwnershipBorrowType(returnTypeName(fn)); !ok {
		return nil, "", false, false, nil
	}
	return c.returnedBorrowInitializer(expr, env)
}

// arenaAtBorrowSource recognizes a direct Arena.at result and resolves the
// arena place that keeps the element alive. It validates the complete call
// before the caller records the returned shared borrow.
func (c *Checker) arenaAtBorrowSource(
	expr ast.Expression,
	env *scope,
) (borrowSource, string, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return borrowSource{}, "", false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Namespace || field.Name != "at" {
		return borrowSource{}, "", false, nil
	}
	receiverType, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return borrowSource{}, "", false, nil
	}
	base, elem, ok := splitGenericType(borrowedOwnershipValueType(receiverType))
	if !ok || base != "std::arena::Arena" {
		return borrowSource{}, "", false, nil
	}
	target, path, err := c.borrowTarget(field.Receiver, env)
	if err != nil {
		return borrowSource{}, "", true, err
	}
	result, err := c.readExpr(expr, env)
	if err != nil {
		return borrowSource{}, "", true, err
	}
	_, mutable, resultElem, resultBorrow := explicitOwnershipBorrowType(result)
	if !resultBorrow || mutable || !sameOwnershipType(resultElem, elem) {
		return borrowSource{}, "", true,
			errorf("arena error: Arena.at must return &%s, got %s", elem, result)
	}
	return borrowSource{target: target, field: path}, elem, true, nil
}

// rejectStoredOptional refuses to store a `?T` or `E!T` whose payload owns
// memory or carries a view. Inside the wrapper the payload is invisible to
// move and borrow tracking, so such a value lives only where it is consumed:
// a capture, an `orelse` / `catch` / `try`, or a return path. The two
// wrappers share the rule because they share the payload (SPEC §11.1).
func (c *Checker) rejectStoredOptional(typeName string) error {
	elem, wrapper, ok := c.wrappedPayloadElem(typeName)
	if !ok {
		return nil
	}
	if strings.HasPrefix(elem, "&") || c.viewCarryingType(elem) || c.valueTypeNeedsConsume(elem) {
		consumer := "capture or orelse"
		if wrapper == "error union" {
			consumer = "capture, catch, or try"
		}
		return errorf(
			"move error: %s `%s` must be consumed where it is produced (%s)",
			wrapper, typeName, consumer)
	}
	return nil
}

// wrappedPayloadElem returns the payload type a `?T` or `E!T` spelling
// wraps, and which wrapper it is. An error union around an optional
// (`E!?T`) reports the innermost payload: what its capture binds is the
// value, and the value is what carries the obligation.
func (c *Checker) wrappedPayloadElem(typeName string) (string, string, bool) {
	if elem, ok := typ.OptionalElem(typeName); ok {
		return elem, "optional", true
	}
	success, ok := c.errorUnionElement(typeName)
	if !ok {
		return "", "", false
	}
	if elem, ok := typ.OptionalElem(success); ok {
		return elem, "error union", true
	}
	return success, "error union", true
}

// checkCaptureLetStmt runs the let recognizers that tie a binding to
// borrow-class views: struct literal capture and view field reads.
func (c *Checker) checkCaptureLetStmt(stmt *ast.LetStmt, env *scope) (bool, error) {
	sources, elem, ok, err := c.structCaptureInitializer(stmt.Value, env)
	if err != nil {
		return true, err
	}
	if ok {
		return true, c.checkReturnedBorrowLetStmt(stmt, sources, elem, false, env)
	}
	sources, ok, err = c.viewInitializer(stmt.Value, env)
	if err != nil {
		return true, err
	}
	if ok {
		return true, c.checkReturnedBorrowLetStmt(stmt, sources, "[]u8", false, env)
	}
	return false, nil
}

// structCaptureInitializer recognizes a struct literal that captures
// borrow-class views in its fields. Only a let can carry the tie, so the
// recognizer lives on the let path; everywhere else the literal keeps being
// rejected by the move on its field values.
func (c *Checker) structCaptureInitializer(
	expr ast.Expression,
	env *scope,
) ([]borrowSource, string, bool, error) {
	literal, ok := expr.(*ast.StructLiteralExpr)
	if !ok || !c.viewCaptureStructType(literal.TypeName) {
		return nil, "", false, nil
	}
	captures := false
	for _, field := range literal.Fields {
		if c.borrowClassViewRoot(field.Value, env) != nil {
			captures = true
			break
		}
	}
	if !captures {
		return nil, "", false, nil
	}
	sources := []borrowSource{}
	for _, field := range literal.Fields {
		if view := c.borrowClassViewRoot(field.Value, env); view != nil {
			if _, err := c.readExpr(field.Value, env); err != nil {
				return nil, "", true, err
			}
			sources = append(sources, borrowSource{target: view})
			continue
		}
		if _, err := c.moveExpr(field.Value, env); err != nil {
			return nil, "", true, err
		}
	}
	return sources, literal.TypeName, true, nil
}

// viewInitializer recognizes an initializer that yields a view still backed
// by a borrow-class binding — a view field read, a slice of a local view, a
// view read back through a reference, or a conditional yielding one — and
// resolves the bindings that keep it alive. The new binding ties to them so
// the view cannot outlive its storage.
func (c *Checker) viewInitializer(
	expr ast.Expression,
	env *scope,
) ([]borrowSource, bool, error) {
	roots := c.borrowClassViewRoots(expr, env)
	if len(roots) == 0 {
		return nil, false, nil
	}
	if _, err := c.readExpr(expr, env); err != nil {
		return nil, true, err
	}
	sources := make([]borrowSource, 0, len(roots))
	for _, root := range roots {
		sources = append(sources, borrowSource{target: root})
	}
	return sources, true, nil
}

// attachAllocProvenance ties a fresh owner to the tied allocators its
// initializer consumed. The owner keeps its consume obligation; the shared
// borrow keeps each allocator — and transitively its buffer — alive until the
// owner's last use.
func (c *Checker) attachAllocProvenance(value *binding) error {
	if len(c.pendingAllocTaints) == 0 {
		return nil
	}
	taints := c.pendingAllocTaints
	c.pendingAllocTaints = nil
	sources := make([]borrowSource, 0, len(taints))
	for _, taint := range taints {
		sources = append(sources, borrowSource{target: taint.alloc})
	}
	return c.bindBorrowSources(value, sources, false)
}

// bindBorrowSources activates every source for a binding and records them as
// its borrow targets. Checking before each activation (not all checks first)
// rejects the same value passed for two mutable sources: the second check
// sees the first activation.
func (c *Checker) bindBorrowSources(
	value *binding,
	sources []borrowSource,
	mutable bool,
) error {
	for _, source := range sources {
		if err := checkBorrowConflictForField(source.target, source.field, mutable); err != nil {
			return err
		}
		c.activateBorrow(source.target, source.field, mutable)
		value.borrowTargets = append(value.borrowTargets, source)
	}
	return nil
}

// checkReturnedBorrowLetStmt binds a returned view carrier to its source owner.
// A view-only value lives in the borrow class. A value that also owes deinit
// stays an ordinary owner binding with borrow targets, like an owner allocated
// from a tied allocator: it must be consumed and cannot leave its sources.
func (c *Checker) checkReturnedBorrowLetStmt(
	stmt *ast.LetStmt,
	sources []borrowSource,
	elem string,
	mutable bool,
	env *scope,
) error {
	value := c.newBinding(stmt.Name, elem)
	value.mutable = stmt.Mutable
	value.declSpan = expressionSpan(stmt.Value)
	value.mutBorrow = mutable
	if !c.valueTypeNeedsConsume(elem) {
		value.borrowedParam = true
		value.localBorrow = true
	}
	if err := c.bindBorrowSources(value, sources, mutable); err != nil {
		return err
	}
	if err := c.attachFutureCapabilityProvenance(value, stmt.Value, env); err != nil {
		return err
	}
	if err := c.attachAllocProvenance(value); err != nil {
		return err
	}
	env.define(value)
	return nil
}

// checkExplicitReturnedBorrowLetStmt binds a declared borrow result. The
// element may itself be an owner type, but the binding owns only a reference
// to it and therefore never inherits the element's deinit obligation.
func (c *Checker) checkExplicitReturnedBorrowLetStmt(
	stmt *ast.LetStmt,
	sources []borrowSource,
	elem string,
	mutable bool,
	env *scope,
) error {
	value := c.newBinding(stmt.Name, elem)
	value.mutable = stmt.Mutable
	value.declSpan = expressionSpan(stmt.Value)
	value.borrowedParam = true
	value.localBorrow = true
	value.mutBorrow = mutable
	if err := c.bindBorrowSources(value, sources, mutable); err != nil {
		return err
	}
	env.define(value)
	return nil
}

// returnedBorrowInitializer recognizes calls returning a borrowed view. The
// sources are derived structurally (ADR-0098): every borrow parameter that can
// back the return — all of them for a shared return, the `&var` ones for a
// mutable return — is kept borrowed while the result lives.
func (c *Checker) returnedBorrowInitializer(
	expr ast.Expression,
	env *scope,
) ([]borrowSource, string, bool, bool, error) {
	// A factory that can refuse hands its result back through `try`. The tie
	// is a property of the value, not of how the failure was spelled, so the
	// `try` is read through rather than treated as a different expression.
	if try, ok := expr.(*ast.TryExpr); ok {
		return c.returnedBorrowInitializer(try.Value, env)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, "", false, false, nil
	}
	name, fn := c.calledFunction(call.Callee, env)
	if fn == nil {
		return nil, "", false, false, nil
	}
	retName := returnTypeName(fn)
	_, mutable, elem, ok := explicitOwnershipBorrowType(retName)
	allocatorReturn := false
	viewReturn := false
	tiedStruct := false
	if !ok {
		// An Allocator return with tie-capable sources is a tied allocator: it
		// holds the buffer's writable view exclusively, so it behaves as a
		// mutable borrow of its sources (page_allocator has none and falls
		// through as a plain copy value). A view or view-capturing struct return
		// ties the same way when a borrow-class view flows in, and falls through
		// as a plain value when none does.
		switch {
		case capabilityReturn(retName):
			// An allocator holds its buffer's writable view exclusively. An Io
			// is copied into every call that takes one, so it is shared: what
			// ties it is how long it may live, not who else may hold one.
			allocatorReturn = true
			mutable = retName != "Io"
			elem = retName
		case tiedStructReturn(retName):
			// A Future holds the state it was lent exclusively. A TaskSet owns
			// its worker states and only shares the capabilities it retains.
			tiedStruct = true
			mutable = !taskSetReturn(retName)
			allocatorReturn = taskSetReturn(retName)
			elem = viewCarrierPayload(retName)
		case retName == "[]u8" || c.viewCaptureStructType(retName):
			viewReturn = true
			elem = retName
		default:
			return nil, "", false, false, nil
		}
	}
	sources, err := c.callBorrowReturnSources(name, fn, call, mutable, allocatorReturn,
		viewReturn, env)
	if err != nil {
		return nil, "", false, true, err
	}
	if len(sources) == 0 {
		if allocatorReturn || viewReturn || tiedStruct {
			return nil, "", false, false, nil
		}
		return nil, "", false, true,
			errorf("borrow error: `%s` borrowed return has no source parameter", name)
	}
	if err := c.checkTiedFactoryCall(name, call, env); err != nil {
		return nil, "", false, true, err
	}
	return sources, elem, mutable, true, nil
}

// attachFutureCapabilityProvenance records the shared half of a Future's
// ties separately from its exclusive state borrow. The Future stores both Io
// and allocator, so either capability must outlive it without becoming
// exclusively borrowed merely because the worker state is one.
func (c *Checker) attachFutureCapabilityProvenance(
	value *binding,
	expr ast.Expression,
	env *scope,
) error {
	if attempt, ok := expr.(*ast.TryExpr); ok {
		return c.attachFutureCapabilityProvenance(value, attempt.Value, env)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	_, fn := c.calledFunction(call.Callee, env)
	if fn == nil || !tiedStructReturn(returnTypeName(fn)) || taskSetReturn(returnTypeName(fn)) {
		return nil
	}
	var sources []borrowSource
	for _, arg := range call.Args {
		if capability := c.tiedCapabilityArg(arg, env); capability != nil {
			sources = append(sources, borrowSource{target: capability})
		}
	}
	return c.bindBorrowSources(value, sources, false)
}

// checkTiedFactoryCall runs the ordinary call check for a factory whose result
// is tied, through whichever arm the callee's shape belongs to.
func (c *Checker) checkTiedFactoryCall(
	name string,
	call *ast.CallExpr,
	env *scope,
) error {
	if typeApply, ok := call.Callee.(*ast.TypeApplyExpr); ok {
		_, err := c.checkTypeApplyCallExpr(typeApply, call.Args, env, true)
		return err
	}
	if ident, ok := call.Callee.(*ast.IdentExpr); ok {
		if value, exists := env.lookup(ident.Name); exists {
			if node, pointer := funcPointerNode(value.typeName); pointer {
				_, err := c.checkFuncPointerCall(ident.Name, node, call.Args, env, true)
				return err
			}
		}
	}
	_, err := c.checkUserCall(name, call.Args, env, true)
	return err
}

// callBorrowReturnSources lists the caller-side bindings a borrow-shaped call
// result stays tied to: every qualifying borrow argument, plus — for Allocator
// returns — every already-tied allocator argument, so re-wrapping an allocator
// cannot launder its tie away, plus — for view returns — every borrow-class
// view argument the result could have captured.
func (c *Checker) callBorrowReturnSources(
	name string,
	fn *functionInfo,
	call *ast.CallExpr,
	mutable bool,
	allocatorReturn bool,
	viewReturn bool,
	env *scope,
) ([]borrowSource, error) {
	sources := []borrowSource{}
	for idx := range fn.params {
		if idx >= len(call.Args) {
			continue
		}
		if !fn.params[idx].borrow || (mutable && !fn.params[idx].mutBorrow) {
			if allocatorReturn {
				if alloc := c.tiedCapabilityArg(call.Args[idx], env); alloc != nil {
					sources = append(sources, borrowSource{target: alloc})
				}
			}
			if viewReturn && fn.params[idx].typeName == "[]u8" {
				if view := c.borrowClassViewRoot(call.Args[idx], env); view != nil {
					sources = append(sources, borrowSource{target: view})
				}
			}
			continue
		}
		target, field, err := c.callBorrowTarget(call.Args[idx], env)
		if err != nil {
			return nil, err
		}
		if target == nil {
			// callBorrowTarget tolerates non-place args for temporary call
			// borrows, but a provenance source must be a place the caller can
			// keep alive while the returned view is used.
			return nil, errorf(
				"borrow error: `%s` borrow source `%s` must be a local binding or direct field",
				name, fn.params[idx].name)
		}
		sources = append(sources, borrowSource{target: target, field: field})
	}
	return sources, nil
}

// tiedAllocatorArg resolves an argument to a tied allocator binding, or nil.
// Only an allocator, because this answers "did the callee get its memory from
// here" -- an Io is a permission to reach the world, and nothing a call made
// with one is held by it.
func (c *Checker) tiedAllocatorArg(arg ast.Expression, env *scope) *binding {
	value, ok := directAssignmentRoot(arg, env)
	if !ok || value.typeName != "Allocator" {
		return nil
	}
	if !value.borrowedParam || len(value.borrowTargets) == 0 {
		return nil
	}
	return value
}

// tiedCapabilityArg resolves an argument to a tied capability binding, or nil.
// This answers the other question: may this be handed over at all. Lending one
// is free; what is not free is outliving what it reaches.
func (c *Checker) tiedCapabilityArg(arg ast.Expression, env *scope) *binding {
	value, ok := directAssignmentRoot(arg, env)
	if !ok || !capabilityReturn(value.typeName) {
		return nil
	}
	if !value.borrowedParam || len(value.borrowTargets) == 0 {
		return nil
	}
	return value
}

// callTiesAllocator reports whether a call to fn yields a tied allocator: the
// signature takes a `&var` borrow the result stays tied to, or an argument is
// itself a tied allocator being re-wrapped. Callers pass nil args to ask about
// the signature alone.
func (c *Checker) callTiesAllocator(
	fn *functionInfo,
	args []ast.Expression,
	env *scope,
) bool {
	for idx := range fn.params {
		if fn.params[idx].borrow && fn.params[idx].mutBorrow {
			return true
		}
		if idx < len(args) && c.tiedCapabilityArg(args[idx], env) != nil {
			return true
		}
	}
	return false
}

// calledFunction resolves direct, namespace-qualified, and indirect source
// function calls through one ownership-facing signature.
func (c *Checker) calledFunction(
	callee ast.Expression,
	env *scope,
) (string, *functionInfo) {
	switch e := callee.(type) {
	case *ast.IdentExpr:
		// A binding shadows a declaration here exactly as it does in call
		// dispatch. Returned-borrow and tied-capability recognizers therefore
		// see the same indirect call the ordinary ownership effects accepted.
		if value, ok := env.lookup(e.Name); ok {
			if node, pointer := funcPointerNode(value.typeName); pointer {
				return e.Name, functionPointerInfo(node)
			}
		}
		return e.Name, c.functions[e.Name]
	case *ast.FieldExpr:
		name, ok := qualifiedName(e)
		if !ok {
			return "", nil
		}
		return name, c.functions[name]
	case *ast.TypeApplyExpr:
		// A generic call names the declaration its static arguments
		// instantiate, and the recognizers read the instantiated signature:
		// with `T` spelled out, a view or a tied allocator the call hands back
		// is seen for what it is, as it would be for a direct call.
		name, typeArg, err := c.typeApplyTarget(e)
		if err != nil {
			return "", nil
		}
		fn := c.functions[name]
		if fn == nil || len(fn.sig.TypeParamNames()) == 0 {
			return name, fn
		}
		subst, err := c.genericCallSubst(name, fn, typeArg)
		if err != nil {
			return name, fn
		}
		return name, instantiateFunctionInfo(fn, subst)
	default:
		return "", nil
	}
}

// functionPointerInfo presents a pointer signature through the same record as
// a declared function. It has no body or static parameters because a call site
// needs only its concrete parameter effects and result.
func functionPointerInfo(node *typ.Func) *functionInfo {
	fn := &functionInfo{returnType: typ.Text(node.Result)}
	for index, param := range node.Params {
		spelling := typ.Text(param)
		info := paramInfo{name: fmt.Sprintf("argument %d", index+1), typeName: spelling}
		if _, mutable, inner, ok := explicitOwnershipBorrowType(spelling); ok {
			info.typeName = inner
			info.borrow = true
			info.mutBorrow = mutable
		}
		fn.params = append(fn.params, info)
	}
	return fn
}

// checkStringViewLetStmt binds a local byte view and activates the String
// owner. The as_mut_bytes form takes an exclusive borrow and requires a
// writable String binding (ADR-0096).
func (c *Checker) checkStringViewLetStmt(
	stmt *ast.LetStmt,
	target *binding,
	path string,
	mutable bool,
	env *scope,
) error {
	if err := c.checkStringViewInitializerShape(stmt.Value); err != nil {
		return err
	}
	if mutable && !target.mutable && !(target.borrowedParam && target.mutBorrow) {
		kind := "String"
		if isBufferTypeName(target.typeName) {
			kind = "buffer"
		}
		return errorf("string error: `%s.as_mut_bytes` requires mutable %s binding", kind, kind)
	}
	if err := checkBorrowConflictForField(target, path, mutable); err != nil {
		return err
	}
	c.activateBorrow(target, path, mutable)
	value := c.newBinding(stmt.Name, "[]u8")
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTargets = []borrowSource{{target: target, field: path}}
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// checkStringViewInitializerShape validates string.as_bytes() / as_mut_bytes()
// local view syntax.
func (c *Checker) checkStringViewInitializerShape(expr ast.Expression) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return errorf("string error: String view initializer must call String.as_bytes")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "as_bytes" && field.Name != "as_mut_bytes") {
		return errorf("string error: String view initializer must call String.as_bytes")
	}
	if len(call.Args) != 0 {
		return errorf("string error: `String.%s` expects 0 args, got %d",
			field.Name, len(call.Args))
	}
	return nil
}

// stringViewInitializer recognizes string.as_bytes() / as_mut_bytes() local
// byte-view initializers, and returns the binding the view borrows together
// with the field path it names within it. A `String` reached through a field
// lends its bytes the way any other field path is borrowed (ADR-0111): the
// borrow is tracked on the root binding under that path, so the struct holding
// it is what the borrow conflicts with.
func (c *Checker) stringViewInitializer(
	expr ast.Expression,
	env *scope,
) (*binding, string, bool, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, "", false, false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "as_bytes" && field.Name != "as_mut_bytes") {
		return nil, "", false, false
	}
	root, path, ok := viewReceiverPath(field.Receiver)
	if !ok {
		return nil, "", false, false
	}
	target, exists := env.lookup(root)
	if !exists || target.moved {
		return nil, "", false, false
	}
	viewed := target.typeName
	if path != "" {
		viewed, ok = c.fieldPathType(target.typeName, path)
		if !ok {
			return nil, "", false, false
		}
	}
	if viewed != "std::string::String" && !isBufferTypeName(viewed) {
		return nil, "", false, false
	}
	return target, path, field.Name == "as_mut_bytes", true
}

// viewReceiverPath reads the local a view initializer borrows from, and the
// dotted field path within it. A bare name is the path-less case.
func viewReceiverPath(receiver ast.Expression) (string, string, bool) {
	if ident, ok := receiver.(*ast.IdentExpr); ok {
		return ident.Name, "", true
	}
	root, path, ok := ast.FieldPathRoot(receiver)
	if !ok {
		return "", "", false
	}
	return root.Name, path, true
}

// isBufferTypeName reports whether a type spelling is a fixed-length stack
// buffer (`[N]u8`).
func isBufferTypeName(typeName string) bool {
	return len(typeName) > 1 && typeName[0] == '[' &&
		typeName[1] >= '0' && typeName[1] <= '9'
}

// borrowCaptureTarget names one binding (or one field of it) a recognized
// capture condition borrows, and how.
type borrowCaptureTarget struct {
	name    string
	field   string
	mutable bool
}

// containerBorrowCondition describes a recognized borrow-optional capture
// condition: which bindings it borrows and what payload the capture binds.
type containerBorrowCondition struct {
	targets []borrowCaptureTarget
	elem    string
	mutable bool
}

// matchContainerBorrowCondition recognizes capture conditions whose call
// produces a borrow optional — a container capture accessor, or a function
// declaring a `?&T` / `?&var T` return. The call is read exactly once through
// the normal call path with the capture context set, so each callee's own
// checker validates receiver and arguments; this recognizer reads the payload
// type that comes back and the bindings the borrow ties to (ADR-0098:
// conservative union of borrow-capable arguments).
func (c *Checker) matchContainerBorrowCondition(
	cond ast.Expression,
	env *scope,
) (containerBorrowCondition, bool, error) {
	call, ok := cond.(*ast.CallExpr)
	if !ok {
		return containerBorrowCondition{}, false, nil
	}
	targets, fromAccessor, ok := c.borrowOptionalCallTargets(call, env)
	if !ok {
		return containerBorrowCondition{}, false, nil
	}
	saved := c.captureCondition
	c.captureCondition = true
	result, err := c.readExpr(cond, env)
	c.captureCondition = saved
	if err != nil {
		return containerBorrowCondition{}, false, err
	}
	elem, mutable, ok := typ.BorrowOptionalElem(result)
	if !ok {
		return containerBorrowCondition{}, false, errorf(
			"move error: capture accessor `%s` returned %s, not a borrow optional",
			call.Callee.String(), result)
	}
	if fromAccessor {
		// A container accessor borrows its container the way the payload
		// says: `at` shared, `at_mut` mutable.
		for i := range targets {
			targets[i].mutable = mutable
		}
	}
	return containerBorrowCondition{targets: targets, elem: elem, mutable: mutable}, true, nil
}

// borrowOptionalCallTargets pre-gates a capture condition without reading it:
// whether the call can produce a borrow optional, and which bindings the
// capture would borrow. fromAccessor separates container capture accessors,
// whose one target follows the payload's mutability, from declared returns,
// whose targets follow each borrow parameter's kind.
func (c *Checker) borrowOptionalCallTargets(
	call *ast.CallExpr,
	env *scope,
) ([]borrowCaptureTarget, bool, bool) {
	switch callee := call.Callee.(type) {
	case *ast.FieldExpr:
		place, fieldName, typeName, ok := c.captureReceiverPlace(callee.Receiver, env)
		if !ok {
			return nil, false, false
		}
		if base, generic := splitGenericBase(typeName); generic &&
			containerAccessTables[base].methods[callee.Name] == accessCapture {
			return []borrowCaptureTarget{{name: place, field: fieldName}}, true, true
		}
		method := c.implMethod(typeName, callee.Name)
		if method == nil {
			return nil, false, false
		}
		if _, _, bare := typ.BorrowOptionalElem(returnTypeName(method)); !bare {
			return nil, false, false
		}
		targets := []borrowCaptureTarget{}
		if len(method.params) > 0 && method.params[0].borrow {
			targets = append(targets, borrowCaptureTarget{
				name: place, field: fieldName, mutable: method.params[0].mutBorrow,
			})
		}
		return append(targets, c.borrowArgTargets(method.params[1:], call.Args, env)...),
			false, true
	case *ast.IdentExpr:
		fn, ok := c.functions[callee.Name]
		if !ok {
			return nil, false, false
		}
		if _, _, bare := typ.BorrowOptionalElem(returnTypeName(fn)); !bare {
			return nil, false, false
		}
		return c.borrowArgTargets(fn.params, call.Args, env), false, true
	default:
		return nil, false, false
	}
}

// splitGenericBase returns the base of a generic type name, or "" when the
// spelling is not generic.
func splitGenericBase(typeName string) (string, bool) {
	base, _, ok := splitGenericType(typeName)
	return base, ok
}

// borrowArgTargets resolves the bindings borrow parameters tie a returned
// borrow to. An argument that is not a nameable place carries no tie.
func (c *Checker) borrowArgTargets(
	params []paramInfo,
	args []ast.Expression,
	env *scope,
) []borrowCaptureTarget {
	targets := []borrowCaptureTarget{}
	for idx, param := range params {
		if idx >= len(args) || !param.borrow {
			continue
		}
		value, field, err := c.callBorrowTarget(args[idx], env)
		if err != nil || value == nil {
			continue
		}
		targets = append(targets, borrowCaptureTarget{
			name: value.name, field: field, mutable: param.mutBorrow,
		})
	}
	return targets
}

// captureReceiverPlace resolves the borrowable place a capture-condition
// receiver names: a live local binding, or a field path on a live owner —
// the same shape field method receivers support.
func (c *Checker) captureReceiverPlace(
	receiver ast.Expression,
	env *scope,
) (string, string, string, bool) {
	if expr, ok := receiver.(*ast.IdentExpr); ok {
		container, exists := env.lookup(expr.Name)
		if !exists || container.moved {
			return "", "", "", false
		}
		return expr.Name, "", container.typeName, true
	}
	owner, path, ok := ast.FieldPathRoot(receiver)
	if !ok {
		return "", "", "", false
	}
	value, exists := env.lookup(owner.Name)
	if !exists || value.moved {
		return "", "", "", false
	}
	fieldType, ok := c.fieldPathType(value.typeName, path)
	if !ok {
		return "", "", "", false
	}
	return owner.Name, path, fieldType, true
}

// tieContainerBorrowCapture builds the capture binding for a recognized
// borrow-optional condition and activates the payload borrow on the branch
// scope's clones of every tied binding — whole bindings and owner fields
// alike — so the branch body sees mutation and deinit wait on the capture.
func (c *Checker) tieContainerBorrowCapture(
	name string,
	match containerBorrowCondition,
	cond ast.Expression,
	branch *scope,
) (*binding, error) {
	value := c.newBinding(name, match.elem)
	value.declSpan = expressionSpan(cond)
	value.borrowedParam = true
	value.localBorrow = true
	value.mutBorrow = match.mutable
	for _, target := range match.targets {
		holder, exists := branch.lookup(target.name)
		if !exists {
			return nil, errorf("move error: unknown value `%s`", target.name)
		}
		if target.field != "" {
			if err := checkBorrowConflictForField(holder, target.field, target.mutable); err != nil {
				return nil, err
			}
		} else if err := checkBorrowConflict(holder, target.mutable); err != nil {
			return nil, err
		}
		c.activateBorrow(holder, target.field, target.mutable)
		value.borrowTargets = append(value.borrowTargets,
			borrowSource{target: holder, field: target.field})
	}
	return value, nil
}

// checkBoxBorrowLetStmt binds a Box payload borrow and activates the Box owner.
func (c *Checker) checkBoxBorrowLetStmt(
	stmt *ast.LetStmt,
	target *binding,
	field string,
	elem string,
	mutable bool,
	env *scope,
) error {
	if mutable && !boxBorrowTargetIsMutable(target, field) {
		return errorf("box error: `Box.borrow_mut` requires mutable Box receiver")
	}
	if err := c.checkBoxBorrowInitializerShape(stmt.Value); err != nil {
		return err
	}
	if err := checkBorrowConflictForField(target, field, mutable); err != nil {
		return err
	}
	c.activateBorrow(target, field, mutable)
	value := c.newBinding(stmt.Name, elem)
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTargets = []borrowSource{{target: target, field: field}}
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// boxBorrowTargetIsMutable reports whether the target can produce a mutable Box view.
func boxBorrowTargetIsMutable(target *binding, field string) bool {
	if field != "" {
		return target.mutable
	}
	return target.mutable
}

// checkBoxBorrowInitializerShape validates Box.borrow/borrow_mut local borrow syntax.
func (c *Checker) checkBoxBorrowInitializerShape(expr ast.Expression) error {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return errorf("box error: Box borrow initializer must call Box.borrow")
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "borrow" && field.Name != "borrow_mut") {
		return errorf("box error: Box borrow initializer must call Box.borrow")
	}
	if len(call.Args) != 0 {
		return errorf("box error: `Box.%s` expects 0 args, got %d", field.Name, len(call.Args))
	}
	return nil
}

// boxBorrowInitializer recognizes box.borrow/borrow_mut local borrow initializers.
func (c *Checker) boxBorrowInitializer(
	expr ast.Expression,
	env *scope,
) (*binding, string, string, bool, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, "", "", false, false, nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || (field.Name != "borrow" && field.Name != "borrow_mut") {
		return nil, "", "", false, false, nil
	}
	target, borrowedField, err := c.borrowTarget(field.Receiver, env)
	if err != nil {
		return nil, "", "", false, true, err
	}
	typeName, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return nil, "", "", false, true, err
	}
	base, elem, ok := splitGenericType(typeName)
	if !ok || base != "std::mem::Box" {
		return nil, "", "", false, true, errorf("box error: `Box.%s` expects Box receiver",
			field.Name)
	}
	return target, borrowedField, elem, field.Name == "borrow_mut", true, nil
}

// checkBorrowLetStmt binds a local borrow and activates its owner until last use.
func (c *Checker) checkBorrowLetStmt(
	stmt *ast.LetStmt,
	borrow *ast.PrefixExpr,
	env *scope,
) error {
	target, field, err := c.borrowTarget(borrow.Right, env)
	if err != nil {
		return err
	}
	mutable := borrow.Operator == "&var"
	if err := checkBorrowConflictForField(target, field, mutable); err != nil {
		return err
	}
	typeName, err := c.readExpr(borrow.Right, env)
	if err != nil {
		return err
	}
	c.activateBorrow(target, field, mutable)
	value := c.newBinding(stmt.Name, typeName)
	value.borrowedParam = true
	value.localBorrow = true
	value.borrowTargets = []borrowSource{{target: target, field: field}}
	value.mutBorrow = mutable
	env.define(value)
	return nil
}

// borrowTarget resolves a explicit borrow target.
func (c *Checker) borrowTarget(expr ast.Expression, env *scope) (*binding, string, error) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		value, ok := env.lookup(target.Name)
		if !ok {
			return nil, "", errorAt(target.Span,
				"borrow error: undefined variable `%s`", target.Name)
		}
		if err := checkDeinitializedBorrow(target.Name, value, target.Span); err != nil {
			return nil, "", err
		}
		if value.moved {
			return nil, "", errorAt(target.Span,
				"borrow error: moved value `%s` was borrowed", target.Name)
		}
		return value, "", nil
	case *ast.FieldExpr:
		ident, path, ok := ast.FieldPathRoot(target)
		if !ok {
			return nil, "", errorAt(target.Span,
				"borrow error: borrow target must be a local binding or field path")
		}
		value, ok := env.lookup(ident.Name)
		if !ok {
			return nil, "", errorAt(ident.Span,
				"borrow error: undefined variable `%s`", ident.Name)
		}
		if err := checkDeinitializedBorrow(ident.Name, value, ident.Span); err != nil {
			return nil, "", err
		}
		if value.moved {
			return nil, "", errorAt(ident.Span,
				"borrow error: moved value `%s` was borrowed", ident.Name)
		}
		if deinit, ok := overlappingFieldDeinit(value.fieldDeinit, path); ok {
			return nil, "", errorAt(target.Span,
				"move error: field `%s.%s` was deinitialized",
				ident.Name, deinit)
		}
		return value, path, nil
	default:
		return nil, "", errorf("borrow error: borrow target must be a local binding or field path")
	}
}

// activateBorrow records one active whole-value or field borrow on a target.
func (c *Checker) activateBorrow(target *binding, field string, mutable bool) {
	if field == "" {
		if mutable {
			target.activeMutBorrows++
		} else {
			target.activeBorrows++
		}
		return
	}
	if mutable {
		if target.fieldMutBorrows == nil {
			target.fieldMutBorrows = map[string]int{}
		}
		target.fieldMutBorrows[field]++
		return
	}
	if target.fieldBorrows == nil {
		target.fieldBorrows = map[string]int{}
	}
	target.fieldBorrows[field]++
}

// checkAssignStmt moves the assigned value into an existing binding.
func (c *Checker) checkAssignStmt(stmt *ast.AssignStmt, env *scope) error {
	if target, ok := directAssignmentRoot(stmt.Target, env); ok {
		typeName, err := c.moveContextualExpr(stmt.Value, target.typeName, env)
		if err != nil {
			return err
		}
		if target.hasAnyBorrow() {
			return errorf("borrow error: value `%s` cannot be assigned while borrowed",
				target.name)
		}
		// Overwriting a live owner silently drops it (ADR-0091). A `&var`
		// parameter always points at a live value the caller still owns, so
		// the same rule stops overwriting an owned type through it.
		if c.bindingNeedsConsume(target) ||
			(target.isMutBorrowParam() && c.valueTypeNeedsConsume(target.typeName)) {
			return errorAt(expressionSpan(stmt.Value),
				"move error: owned value `%s` is overwritten before cleanup", target.name)
		}
		if err := c.checkCleanupReceiverOverwrite(target, stmt); err != nil {
			return err
		}
		target.typeName = typeName
		target.moved = false
		target.deinitialized = false
		target.arenaID = 0
		target.handleArenaID = 0
		c.setArenaProvenance(target, stmt.Value, env)
		return nil
	}
	if _, err := c.moveExpr(stmt.Value, env); err != nil {
		return err
	}
	if err := c.checkAssignmentBorrowConflict(stmt.Target, env); err != nil {
		return err
	}
	if root, field, ok := directFieldRoot(stmt.Target, env); ok && field != "" {
		if err := c.checkOwnerFieldOverwrite(root, field, stmt); err != nil {
			return err
		}
		root.clearFieldDeinit(field)
	}
	if _, ok := assignmentRoot(stmt.Target, env); !ok {
		_, err := c.readExpr(stmt.Target, env)
		return err
	}
	return nil
}

// checkOwnerFieldOverwrite rejects assigning over a live owner field. The
// assignment releases nothing, so the value the field held leaks -- the wound
// the local rule already names one line up. A field the owner's own deinit has
// consumed is not live, so re-filling it stays allowed.
func (c *Checker) checkOwnerFieldOverwrite(
	root *binding,
	field string,
	stmt *ast.AssignStmt,
) error {
	if root == nil || root.fieldDeinit[field] {
		return nil
	}
	fieldType, ok := c.fieldPathType(root.typeName, field)
	if !ok || !c.fieldTypeNeedsConsume(fieldType) {
		return nil
	}
	return errorAt(expressionSpan(stmt.Value),
		"move error: owner field `%s.%s` is overwritten before cleanup", root.name, field)
}

// checkExprStmt reads standalone expressions, except normal calls handle argument moves.
func (c *Checker) checkExprStmt(stmt *ast.ExprStmt, env *scope) error {
	produced, err := c.readExpr(stmt.Expr, env)
	if err != nil {
		return err
	}
	return c.checkDiscardedOwner(stmt.Expr, produced)
}

// checkDiscardedOwner rejects an expression statement that produces an owner.
// Cleanup obligations are tracked per binding, so a value that is never bound
// is never tracked: `parent.pop();` moves an element out of the array and drops
// it where nothing can reach it. Binding the value is what makes the obligation
// visible, and `let _ = ...` is the spelling for discarding on purpose — it
// still has to name the cleanup.
func (c *Checker) checkDiscardedOwner(expr ast.Expression, produced string) error {
	if !c.resultTypeNeedsConsume(produced) {
		return nil
	}
	return errorAt(expressionSpan(expr),
		"move error: this expression produces owned `%s` and discards it;"+
			" bind the value and consume it", produced)
}

// checkIfStmt merges possible moves from either branch into the outer scope.
func (c *Checker) checkIfStmt(stmt *ast.IfStmt, env *scope) error {
	// The condition's product is held by the capture, or was a bool: the
	// branches start with nothing of it pending.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	var borrowCond containerBorrowCondition
	isBorrowCond := false
	var condType string
	if stmt.Capture != "" {
		match, ok, err := c.matchContainerBorrowCondition(stmt.Condition, env)
		if err != nil {
			return err
		}
		borrowCond, isBorrowCond = match, ok
	}
	if !isBorrowCond {
		read, err := c.readCaptureCondition(stmt.Condition, stmt.Capture, env)
		if err != nil {
			return err
		}
		condType = read
	}
	left := env.clone()
	leftScope := left.child()
	// Both clones come first, so what the capture records about a field lands
	// on the binding the branches were cloned from rather than on one of them.
	// Opening a `?Owner` field discharges its cleanup on both paths — the
	// value is released where it is there and there is nothing to release
	// where it is not — so it is not a branch that consumed it.
	right := env.clone()
	// The tie borrows the branch's clone of each container: the branch body is
	// checked against that clone, so only its bindings make its mutations wait.
	_, err := c.defineCapture(
		stmt.Capture, stmt.Condition, condType, borrowCond, isBorrowCond, false, env, leftScope)
	if err != nil {
		return err
	}
	c.settleOwnerTemps(pending, places, "void", nil)
	if err := c.checkBlock(stmt.Consequence, leftScope); err != nil {
		return err
	}
	if err := c.checkCapturePayloadConsumed(stmt.Capture, leftScope); err != nil {
		return err
	}
	if stmt.Alternative != nil {
		rightScope := right.child()
		if stmt.ErrCapture != "" {
			// `else |err|` binds the failure member: one scalar error code,
			// a plain copy with no obligations of its own.
			errName, _, _ := c.errorUnionParts(condType)
			errBinding := c.newBinding(stmt.ErrCapture, errName)
			errBinding.declSpan = expressionSpan(stmt.Condition)
			rightScope.define(errBinding)
		}
		if err := c.checkBlock(stmt.Alternative, rightScope); err != nil {
			return err
		}
	}
	// A branch that always returns cannot affect the code after the if.
	leftLive := !blockTerminates(stmt.Consequence)
	rightLive := stmt.Alternative == nil || !blockTerminates(stmt.Alternative)
	if err := c.checkBranchConsumeAgreement(env, left, right, leftLive, rightLive); err != nil {
		return err
	}
	if leftLive {
		env.mergeMovedFrom(left)
	}
	if rightLive {
		env.mergeMovedFrom(right)
	}
	return nil
}

// readCaptureCondition sanctions the one direct user call whose optional
// payload can carry an input view. The capture records that provenance after
// the call is read; nested calls remain ordinary calls and cannot borrow a
// local view past their statement.
func (c *Checker) readCaptureCondition(
	expr ast.Expression,
	capture string,
	env *scope,
) (string, error) {
	call, ok := unwrapExpressionMarkers(expr).(*ast.CallExpr)
	if capture == "" || !ok {
		return c.readExpr(expr, env)
	}
	_, fn := c.calledFunction(call.Callee, env)
	if fn == nil {
		return c.readExpr(expr, env)
	}
	resultType := returnTypeName(fn)
	if success, errorUnion := c.errorUnionElement(resultType); errorUnion {
		resultType = success
	}
	payload, optional := typ.OptionalElem(resultType)
	if !optional || !c.viewCarryingType(payload) {
		return c.readExpr(expr, env)
	}
	saved := c.viewCaptureCall
	c.viewCaptureCall = call
	result, err := c.readExpr(expr, env)
	c.viewCaptureCall = saved
	return result, err
}

// checkBranchConsumeAgreement rejects an owner only one surviving branch
// consumes. After the merge the value is unusable, so the branch that did not
// consume it can neither release it nor hand it on, and cleaning up after the
// if would double-free the branch that already did. Both paths must agree.
func (c *Checker) checkBranchConsumeAgreement(
	env, left, right *scope,
	leftLive, rightLive bool,
) error {
	// A branch that always returns had its own obligations checked at that
	// return, so it puts nothing on the path that continues past the if.
	if !leftLive || !rightLive {
		return nil
	}
	leftByID := map[int]*binding{}
	left.collectBindings(leftByID)
	rightByID := map[int]*binding{}
	right.collectBindings(rightByID)
	var split *binding
	env.walkBindings(func(value *binding) {
		if !c.bindingNeedsConsume(value) {
			return
		}
		inLeft, okLeft := leftByID[value.id]
		inRight, okRight := rightByID[value.id]
		if !okLeft || !okRight || bindingConsumed(inLeft) == bindingConsumed(inRight) {
			return
		}
		if split == nil || value.id > split.id {
			split = value
		}
	})
	if split == nil {
		return c.checkBranchFieldConsumeAgreement(env, left, right)
	}
	return errorAt(split.declSpan,
		"move error: owned value `%s` is consumed on one branch only;"+
			" consume it on both branches or on neither", split.name)
}

// checkBranchFieldConsumeAgreement rejects an owner field only one surviving
// branch cleaned. The reasoning is the one whole values already follow: past
// the merge the branch that left it alone can no longer release it, and
// releasing it after the if would release twice what the other branch already
// did. Both paths must agree.
func (c *Checker) checkBranchFieldConsumeAgreement(env, left, right *scope) error {
	leftByID := map[int]*binding{}
	left.collectBindings(leftByID)
	rightByID := map[int]*binding{}
	right.collectBindings(rightByID)
	var splitValue *binding
	splitField := ""
	env.walkBindings(func(value *binding) {
		fields := c.structs[value.typeName]
		inLeft, okLeft := leftByID[value.id]
		inRight, okRight := rightByID[value.id]
		if fields == nil || !okLeft || !okRight {
			return
		}
		for _, name := range c.structOrder[value.typeName] {
			if !c.fieldTypeNeedsConsume(fields[name]) {
				continue
			}
			if inLeft.fieldDeinit[name] == inRight.fieldDeinit[name] {
				continue
			}
			if splitValue == nil || value.id > splitValue.id {
				splitValue, splitField = value, name
			}
		}
	})
	if splitValue == nil {
		return nil
	}
	return errorAt(splitValue.declSpan,
		"move error: owner field `%s.%s` is consumed on one branch only;"+
			" consume it on both branches or on neither", splitValue.name, splitField)
}

// bindingConsumed reports whether a path has handed the value's cleanup
// obligation on, by move or by explicit cleanup.
func bindingConsumed(value *binding) bool {
	return value.moved || value.deinitialized
}

// checkWhileStmt checks a `while` loop: the condition runs once per turn like
// the body does, so it is read against the loop's clone, and what it binds
// is the turn's own.
func (c *Checker) checkWhileStmt(stmt *ast.WhileStmt, env *scope) error {
	// The condition's product is held by the capture, or was a bool: the
	// body starts with nothing of it pending.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	enter := func(clone, child *scope) error {
		var borrowCond containerBorrowCondition
		isBorrowCond := false
		condType := ""
		if stmt.Capture != "" {
			match, ok, err := c.matchContainerBorrowCondition(stmt.Condition, clone)
			if err != nil {
				return err
			}
			borrowCond, isBorrowCond = match, ok
		}
		if !isBorrowCond {
			read, err := c.readCaptureCondition(stmt.Condition, stmt.Capture, clone)
			if err != nil {
				return err
			}
			condType = read
		}
		// Borrow the loop's clone of each container, as in checkIfStmt.
		_, err := c.defineCapture(
			stmt.Capture, stmt.Condition, condType, borrowCond, isBorrowCond, true, env, child)
		if err != nil {
			return err
		}
		c.settleOwnerTemps(pending, places, "void", nil)
		return nil
	}
	leave := func(child *scope) error {
		return c.checkCapturePayloadConsumed(stmt.Capture, child)
	}
	return c.checkLoopRegion(stmt.Label, stmt.Body, env, enter, leave)
}

// checkLoopRegion checks one loop's body as a region the program enters an
// unknown number of times, whatever loop form wrote it. The turn runs against
// a clone of the scope: enter binds what the turn produces — a capture, an
// index — into the body's scope, leave checks what the turn must have
// consumed, and the body as a whole consumes nothing declared outside it,
// since zero turns would leave that unreleased and two would release it twice.
func (c *Checker) checkLoopRegion(
	label string,
	body *ast.BlockStmt,
	env *scope,
	enter func(clone, child *scope) error,
	leave func(child *scope) error,
) error {
	clone := env.clone()
	leaveLoop := c.enterLoop(label)
	defer leaveLoop()
	child := clone.child()
	if err := enter(clone, child); err != nil {
		return err
	}
	if err := c.checkBlock(body, child); err != nil {
		return err
	}
	if err := leave(child); err != nil {
		return err
	}
	if err := c.checkLoopConsumesNothingOutside(env, clone); err != nil {
		return err
	}
	env.mergeMovedFrom(clone)
	return nil
}

// checkLoopConsumesNothingOutside rejects a loop body that consumes a binding
// declared outside it. The body runs an unknown number of times: zero leaves
// the value unreleased, two release it twice. A value consumed in a loop is one
// the loop itself produced, which is what the `|name|` capture binds.
func (c *Checker) checkLoopConsumesNothingOutside(env *scope, body *scope) error {
	inBody := map[int]*binding{}
	body.collectBindings(inBody)
	var consumed *binding
	env.walkBindings(func(value *binding) {
		if !c.bindingNeedsConsume(value) {
			return
		}
		if inside, ok := inBody[value.id]; !ok || !bindingConsumed(inside) {
			return
		}
		if consumed == nil || value.id > consumed.id {
			consumed = value
		}
	})
	if consumed == nil {
		return c.checkLoopCleansNoOuterField(env, inBody)
	}
	return errorAt(consumed.declSpan,
		"move error: owned value `%s` is consumed inside a loop;"+
			" the body runs an unknown number of times", consumed.name)
}

// checkLoopCleansNoOuterField rejects a loop body that releases a field of a
// value declared outside it. It is the rule whole values already follow, read
// one level down: zero turns leave the field unreleased, two release it twice.
func (c *Checker) checkLoopCleansNoOuterField(env *scope, inBody map[int]*binding) error {
	var cleanedValue *binding
	cleanedField := ""
	env.walkBindings(func(value *binding) {
		fields := c.structs[value.typeName]
		inside, ok := inBody[value.id]
		if fields == nil || !ok {
			return
		}
		for _, name := range c.structOrder[value.typeName] {
			if !c.fieldTypeNeedsConsume(fields[name]) {
				continue
			}
			if value.fieldDeinit[name] || !inside.fieldDeinit[name] {
				continue
			}
			if cleanedValue == nil || value.id > cleanedValue.id {
				cleanedValue, cleanedField = value, name
			}
		}
	})
	if cleanedValue == nil {
		return nil
	}
	return errorAt(cleanedValue.declSpan,
		"move error: owner field `%s.%s` is released inside a loop;"+
			" the body runs an unknown number of times",
		cleanedValue.name, cleanedField)
}

// optionalPayloadName returns T for a `?T` condition type, or the success
// type for an `E!T` one, or the type itself when it is neither. An error
// union capture binds the success payload the same way an optional capture
// binds the value (SPEC §11.1).
func optionalPayloadName(typeName string) string {
	if elem, ok := typ.OptionalElem(typeName); ok {
		return elem
	}
	if parsed, err := typ.Parse(typeName); err == nil {
		if _, success, ok := typ.ErrorUnionParts(parsed); ok {
			return typ.Text(success)
		}
	}
	return typeName
}

// newCaptureBinding builds the `|name|` payload binding for an optional
// condition. The payload's class rides on its type: an owner payload owes a
// consume before the branch ends (checkBlock's sweep), a view payload is a
// free view like a parameter's, and a copy payload is a plain copy.
func (c *Checker) newCaptureBinding(
	name string,
	condType string,
	cond ast.Expression,
) *binding {
	value := c.newBinding(name, optionalPayloadName(condType))
	value.declSpan = expressionSpan(cond)
	return value
}

// optionalOwnerFieldCapture classifies a capture condition whose payload owns
// memory, and reports the field's root binding, the field name, and whether the
// payload may be consumed there. The last result is false for a condition that
// hands the payload over, which the caller leaves alone.
//
// A read of storage does not hand the payload over, so the capture borrows —
// the way a container accessor's `?&V` does (ADR-0104). The one place a field
// is owned is its type's own deinit, where the receiver arrived by value and
// direct field cleanup is already allowed: the same split `match` makes for an
// owner union payload.
//
// Reading storage is the default answer, not the recognized one. An owner
// payload becomes owned only where something positively hands it over, so a
// shape this does not know about stays closed (原理 8).
func (c *Checker) optionalOwnerFieldCapture(
	cond ast.Expression,
	condType string,
	env *scope,
) (*binding, string, bool, bool) {
	elem, _, ok := c.wrappedPayloadElem(condType)
	if !ok || !c.valueTypeNeedsConsume(elem) {
		return nil, "", false, false
	}
	if c.handsOverOptionalPayload(cond) {
		return nil, "", false, false
	}
	root, path, ok := directFieldRoot(cond, env)
	if !ok || root == nil || strings.Contains(path, ".") {
		// A nested path reads storage the same way a direct one does, and
		// cleanup through one is refused anyway (ADR-0067), so it borrows
		// with no field to attribute the obligation to.
		return nil, "", false, true
	}
	return root, path, c.allowsOwnerFieldCleanup(root), true
}

// handsOverOptionalPayload reports whether the condition produces the payload
// rather than reading it out of storage. A call produces one — `Array.pop`
// moves the element out, a function returning `?T` hands back what it built.
// The comptime field form is the exception: it is spelled as a call but reads
// one field of a borrowed struct (ADR-0113).
func (c *Checker) handsOverOptionalPayload(cond ast.Expression) bool {
	call, ok := unwrapExpressionMarkers(cond).(*ast.CallExpr)
	if !ok {
		return false
	}
	apply, isApply := call.Callee.(*ast.TypeApplyExpr)
	if !isApply {
		return true
	}
	name, _, err := c.typeApplyTarget(apply)
	return err != nil || stdmeta.Form(name) != stdmeta.Field
}

// unwrapExpressionMarkers strips the markers that wrap an expression without
// changing where its value came from: `try` forwards a call's success value,
// and `unsafe` only records who owns the obligation.
func unwrapExpressionMarkers(expr ast.Expression) ast.Expression {
	for {
		switch marked := expr.(type) {
		case *ast.TryExpr:
			expr = marked.Value
		case *ast.UnsafeExpr:
			expr = marked.Value
		default:
			return expr
		}
	}
}

// tieOptionalOwnerFieldCapture applies that split to one capture binding: a
// borrow everywhere but the owner's deinit, and there a consumable payload
// whose cleanup counts toward the field's obligation.
func (c *Checker) tieOptionalOwnerFieldCapture(
	capture *binding,
	cond ast.Expression,
	condType string,
	inLoop bool,
	env *scope,
) (bool, bool, error) {
	root, field, consumable, ok := c.optionalOwnerFieldCapture(cond, condType, env)
	if !ok {
		return false, false, nil
	}
	if !consumable {
		capture.borrowedParam = true
		return true, false, nil
	}
	if inLoop {
		// The condition reads the same storage every turn, so the payload the
		// first turn released would be released again by the second.
		return true, false, errorf(
			"move error: `while` cannot consume owner field `%s.%s`;"+
				" the condition reads the same storage every turn, so use `if`",
			root.name, field)
	}
	root.markFieldDeinit(field)
	return true, true, nil
}

// defineCapture builds the binding a `|name|` capture introduces, ties it to
// what it borrows, and defines it in the body's scope. It reports whether the
// payload is one the body has to consume — true only for an owner field opened
// inside that field's own type deinit.
func (c *Checker) defineCapture(
	name string,
	cond ast.Expression,
	condType string,
	borrowCond containerBorrowCondition,
	isBorrowCond bool,
	inLoop bool,
	env *scope,
	body *scope,
) (bool, error) {
	if name == "" {
		return false, nil
	}
	if isBorrowCond {
		capture, err := c.tieContainerBorrowCapture(name, borrowCond, cond, body)
		if err != nil {
			return false, err
		}
		body.define(capture)
		return false, nil
	}
	capture := c.newCaptureBinding(name, condType, cond)
	isFieldCapture, consumes, err := c.tieOptionalOwnerFieldCapture(
		capture, cond, condType, inLoop, env)
	if err != nil {
		return false, err
	}
	if !isFieldCapture {
		if err := c.tieViewCapture(capture, condType, cond, body); err != nil {
			return false, err
		}
	}
	body.define(capture)
	return consumes, nil
}

// checkCapturePayloadConsumed rejects a capture body that dropped an owner
// payload it was handed. A payload produced by the condition is the body's
// to consume (SPEC §7), and one opened out of an owner field inside that
// field's deinit discharged the field's cleanup when the capture bound, so
// leaving either here would release nothing (ADR-0091). A borrowed payload
// owes nothing and passes.
func (c *Checker) checkCapturePayloadConsumed(name string, scope *scope) error {
	if name == "" {
		return nil
	}
	value, ok := scope.values[name]
	if !ok || !c.bindingNeedsConsume(value) {
		return nil
	}
	return leakError(value, leakExit{})
}

// tieViewCapture ties a view-carrying capture to the containers its condition
// call read: the captured view may alias their storage, so each stays
// share-borrowed until the capture's last use, and mutation or deinit waits —
// the same exclusion `let view = string.as_bytes()` gets from its let borrow.
func (c *Checker) tieViewCapture(
	capture *binding,
	condType string,
	cond ast.Expression,
	env *scope,
) error {
	if _, ok := typ.OptionalElem(condType); !ok {
		return nil
	}
	if !c.viewCarryingType(optionalPayloadName(condType)) {
		return nil
	}
	sources, err := c.condViewSources(cond, env)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := checkBorrowConflictForField(source.target, source.field, false); err != nil {
			return err
		}
		c.activateBorrow(source.target, source.field, false)
		capture.borrowedParam = true
		capture.localBorrow = true
		capture.borrowTargets = append(capture.borrowTargets, source)
	}
	// The condition handed the payload over; a payload that owns something
	// is the capture's to release even while its views tie it here.
	capture.ownsTied = c.valueTypeNeedsConsume(capture.typeName)
	return nil
}

// condViewSources lists the storage and borrow-class view sources a
// capture-condition call reads. Declared functions derive sources from their
// signatures; the syntactic container walk covers builtin accessors.
func (c *Checker) condViewSources(cond ast.Expression, env *scope) ([]borrowSource, error) {
	cond = unwrapExpressionMarkers(cond)
	call, ok := cond.(*ast.CallExpr)
	if !ok {
		return nil, nil
	}
	var sources []borrowSource
	seen := map[borrowSource]bool{}
	addSource := func(source borrowSource) {
		if source.target == nil || seen[source] {
			return
		}
		seen[source] = true
		sources = append(sources, source)
	}
	name, fn := c.calledFunction(call.Callee, env)
	if fn != nil {
		derived, err := c.callBorrowReturnSources(name, fn, call, false, false, true, env)
		if err != nil {
			return nil, err
		}
		for _, source := range derived {
			addSource(source)
		}
	}
	add := func(expr ast.Expression) {
		if value := c.borrowClassViewRoot(expr, env); value != nil {
			addSource(borrowSource{target: value})
			return
		}
		ident, ok := expr.(*ast.IdentExpr)
		if !ok {
			return
		}
		value, exists := env.lookup(ident.Name)
		if !exists || value.moved || !isContainerTypeName(value.typeName) {
			return
		}
		addSource(borrowSource{target: value})
	}
	if field, ok := call.Callee.(*ast.FieldExpr); ok {
		add(field.Receiver)
	}
	for _, arg := range call.Args {
		add(arg)
	}
	return sources, nil
}

// isContainerTypeName reports whether a type owns storage a returned view
// could alias.
func isContainerTypeName(typeName string) bool {
	if typeName == "std::string::String" || isBufferTypeName(typeName) {
		return true
	}
	base, _, ok := splitGenericType(typeName)
	if !ok {
		return false
	}
	switch base {
	case "std::array::Array", "std::map::Map", "std::mem::Box", "std::arena::Arena":
		return true
	default:
		return false
	}
}

// refuseContainerViewOrelse rejects `orelse` over a view optional produced
// from an owned container: the result is a bare view no binding ties back to
// the container, so it could outlive a mutation. A capture ties the view for
// exactly its scope instead.
func (c *Checker) refuseContainerViewOrelse(
	condType string,
	cond ast.Expression,
	env *scope,
) error {
	if _, ok := typ.OptionalElem(condType); !ok {
		return nil
	}
	if !c.viewCarryingType(optionalPayloadName(condType)) {
		return nil
	}
	sources, err := c.condViewSources(cond, env)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	return errorf(
		"move error: view optional `%s` from a container must be consumed by a capture"+
			" (`if cond |name|` or `while cond |name|`)", condType)
}

// checkForStmt checks a `for` loop. The range is fixed, but its length is not
// known here, so the body is under the same rule as a while body.
func (c *Checker) checkForStmt(stmt *ast.ForStmt, env *scope) error {
	if _, err := c.readExpr(stmt.Start, env); err != nil {
		return err
	}
	if _, err := c.readExpr(stmt.End, env); err != nil {
		return err
	}
	enter := func(_, child *scope) error {
		child.define(c.newBinding(stmt.Name, "i64"))
		return nil
	}
	leave := func(*scope) error { return nil }
	return c.checkLoopRegion(stmt.Label, stmt.Body, env, enter, leave)
}

// checkLoopBranch checks a `break` or `continue`: it names a loop the
// statement sits in, and leaving that loop's body early skips the releases
// the body has not reached yet, so what the body declared since the loop
// began must already be consumed.
func (c *Checker) checkLoopBranch(label string, kind leakExitKind, env *scope) error {
	start, ok := c.loopStartFor(label)
	if !ok {
		return errorf("move error: loop branch `%s` used outside loop", label)
	}
	return c.checkOwnersConsumed(env, start.sinceID, leakExit{kind: kind})
}

// loopStartFor finds the loop a branch names: the innermost one for a bare
// branch, the one carrying the label otherwise.
func (c *Checker) loopStartFor(label string) (loopStart, bool) {
	for index := len(c.loopStarts) - 1; index >= 0; index-- {
		if label == "" || c.loopStarts[index].label == label {
			return c.loopStarts[index], true
		}
	}
	return loopStart{}, false
}

// enterLoop records a loop the body about to be checked sits in, and returns
// the call that leaves it. The watermark is taken before the loop defines
// anything, so a capture the condition binds counts as the body's own.
func (c *Checker) enterLoop(label string) func() {
	c.loopStarts = append(c.loopStarts, loopStart{label: label, sinceID: c.nextID})
	return func() { c.loopStarts = c.loopStarts[:len(c.loopStarts)-1] }
}

// checkMatchStmt merges possible moves from enum or union match arms into the outer scope.
func (c *Checker) checkMatchStmt(stmt *ast.MatchStmt, env *scope) error {
	// The value's product is held by the arm that binds it: the arms start
	// with nothing of it pending.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	valueType, err := c.readExpr(stmt.Value, env)
	if err != nil {
		return err
	}
	c.settleOwnerTemps(pending, places, "void", nil)
	valueType = borrowedOwnershipValueType(valueType)
	tags, unionPayloads, ok := c.matchTags(valueType)
	if !ok {
		return errorf("move error: match expects enum or union, got %s", valueType)
	}
	armVariants, err := c.matchArmVariants(stmt)
	if err != nil {
		return err
	}
	ownerDeinitDispatch := c.matchesOwnerUnionDeinit(stmt.Value, valueType)
	if ownerDeinitDispatch {
		c.consumeOwnerUnionReceiver(stmt.Value, env)
	}
	ownedMatch := c.matchScrutineeOwned(stmt.Value, env)
	if !ownerDeinitDispatch {
		if err := c.consumeMovedFromScrutinee(stmt, unionPayloads, env); err != nil {
			return err
		}
	}
	// Every arm starts from the state the match was reached in. Cloning inside
	// the loop would start each arm from what the arms before it merged back,
	// so an arm would inherit a cleanup only one other arm ran.
	live, err := c.checkMatchArms(stmt, env, matchArmContext{
		valueType:           valueType,
		tags:                tags,
		unionPayloads:       unionPayloads,
		armVariants:         armVariants,
		ownerDeinitDispatch: ownerDeinitDispatch,
		ownedMatch:          ownedMatch,
	})
	if err != nil {
		return err
	}
	if err := c.checkArmFieldConsumeAgreement(env, live); err != nil {
		return err
	}
	for _, armEnv := range live {
		env.mergeMovedFrom(armEnv)
	}
	return nil
}

// matchArmContext carries what every arm of one match is checked against.
type matchArmContext struct {
	valueType           string
	tags                map[string]bool
	unionPayloads       map[string]string
	armVariants         map[string]metaField
	ownerDeinitDispatch bool
	ownedMatch          bool
}

// checkMatchArms checks each arm against the state the match was reached in and
// returns the scopes of the arms that continue past it. Cloning inside the loop
// would start each arm from what the arms before it merged back, so an arm
// would inherit a cleanup only one other arm ran.
func (c *Checker) checkMatchArms(
	stmt *ast.MatchStmt,
	env *scope,
	ctx matchArmContext,
) ([]*scope, error) {
	base := env.clone()
	live := make([]*scope, 0, len(stmt.Arms))
	// One arm runs: what one arm produces -- a `return f()` is a statement
	// of its own -- is not live in the next.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	for _, arm := range stmt.Arms {
		c.settleOwnerTemps(pending, places, "void", nil)
		if arm.IsWildcard() {
			if arm.Binding != "" {
				return nil, errorf("move error: wildcard match arm cannot bind payload")
			}
		} else if !c.matchArmTagKnown(ctx.tags, arm) {
			return nil, errorf("move error: unknown match tag `%s::%s`", ctx.valueType, arm.Tag)
		}
		armEnv := base.clone()
		child := armEnv.child()
		c.defineMatchArmPayload(arm, ctx.unionPayloads, ctx.ownerDeinitDispatch,
			ctx.ownedMatch, child, expressionSpan(stmt.Value))
		restore := c.bindMetaField(stmt.MetaCapture, ctx.armVariants[arm.Tag])
		err := c.checkStmt(arm.Body, child)
		restore()
		if err != nil {
			return nil, err
		}
		if err := c.checkArmPayloadConsumed(arm, child); err != nil {
			return nil, err
		}
		if !stmtTerminates(arm.Body) {
			live = append(live, armEnv)
		}
	}
	return live, nil
}

// checkArmFieldConsumeAgreement rejects an owner field only some of the arms
// that continue past the match cleaned. It is the branch rule an if already
// follows, read across every surviving arm rather than two.
func (c *Checker) checkArmFieldConsumeAgreement(env *scope, live []*scope) error {
	if len(live) < 2 {
		return nil
	}
	armsByID := make([]map[int]*binding, 0, len(live))
	for _, armEnv := range live {
		byID := map[int]*binding{}
		armEnv.collectBindings(byID)
		armsByID = append(armsByID, byID)
	}
	var splitValue *binding
	splitField := ""
	env.walkBindings(func(value *binding) {
		fields := c.structs[value.typeName]
		if fields == nil {
			return
		}
		for _, name := range c.structOrder[value.typeName] {
			if !c.fieldTypeNeedsConsume(fields[name]) {
				continue
			}
			if fieldCleanupAgrees(armsByID, value.id, name) {
				continue
			}
			if splitValue == nil || value.id > splitValue.id {
				splitValue, splitField = value, name
			}
		}
	})
	if splitValue == nil {
		return nil
	}
	return errorAt(splitValue.declSpan,
		"move error: owner field `%s.%s` is consumed on some match arms only;"+
			" consume it on every arm that continues past the match or on none",
		splitValue.name, splitField)
}

// fieldCleanupAgrees reports whether every arm that carries this binding made
// the same decision about one of its fields.
func fieldCleanupAgrees(armsByID []map[int]*binding, id int, field string) bool {
	seen := false
	cleaned := false
	for _, byID := range armsByID {
		value, ok := byID[id]
		if !ok {
			continue
		}
		if !seen {
			seen, cleaned = true, value.fieldDeinit[field]
			continue
		}
		if value.fieldDeinit[field] != cleaned {
			return false
		}
	}
	return true
}

// checkArmPayloadConsumed rejects a match arm that drops an owned payload it
// moved out of the scrutinee (ADR-0091).
func (c *Checker) checkArmPayloadConsumed(arm ast.MatchArm, child *scope) error {
	if arm.Binding == "" {
		return nil
	}
	value, ok := child.values[arm.Binding]
	if !ok || !c.bindingNeedsConsume(value) {
		return nil
	}
	return leakError(value, leakExit{})
}

// matchScrutineeOwner returns the binding of a matched value that is an owned
// named local, the one thing a moving match must mark consumed.
func (c *Checker) matchScrutineeOwner(value ast.Expression, env *scope) *binding {
	ident, ok := value.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	owner, exists := env.lookup(ident.Name)
	if !exists {
		return nil
	}
	if owner.borrowedParam || owner.localBorrow || owner.handleArenaID != 0 {
		return nil
	}
	return owner
}

// matchScrutineeOwned reports whether the matched value owns its payloads: an
// owned named local, or a call temporary the match consumes for free. Borrows,
// projections, and calls that may return borrows (arena.at, methods, `borrows`
// returns) match in borrow mode and their aggregate payloads cannot move out.
func (c *Checker) matchScrutineeOwned(value ast.Expression, env *scope) bool {
	if c.matchScrutineeOwner(value, env) != nil {
		return true
	}
	call, ok := value.(*ast.CallExpr)
	if !ok {
		return false
	}
	if c.isArenaAtExpr(value, env) {
		return false
	}
	if field, ok := call.Callee.(*ast.FieldExpr); ok && !field.Namespace {
		return false
	}
	if _, fn := c.calledFunction(call.Callee, env); fn != nil {
		// A borrow-typed return is a view of caller state, not a fresh
		// temporary (ADR-0098: provenance is structural).
		return !strings.HasPrefix(returnTypeName(fn), "&")
	}
	// An unresolved namespace call is a variant constructor: a fresh temporary.
	return true
}

// consumeMovedFromScrutinee marks an owned matched value moved when any arm
// binds a payload that moves out. One arm moving is enough: which arm runs is
// not known statically, so the value is unavailable in the arm bodies and
// after the match either way.
func (c *Checker) consumeMovedFromScrutinee(
	stmt *ast.MatchStmt,
	unionPayloads map[string]string,
	env *scope,
) error {
	owner := c.matchScrutineeOwner(stmt.Value, env)
	if owner == nil || !c.matchMovesPayload(stmt.Arms, unionPayloads) {
		return nil
	}
	if owner.hasAnyBorrow() {
		return errorAt(expressionSpan(stmt.Value),
			"borrow error: value `%s` cannot be moved while borrowed", owner.name)
	}
	owner.moved = true
	return nil
}

// matchMovesPayload reports whether any arm binds a payload an owned match
// moves out rather than copies.
func (c *Checker) matchMovesPayload(arms []ast.MatchArm, unionPayloads map[string]string) bool {
	for _, arm := range arms {
		if arm.IsWildcard() || arm.Binding == "" {
			continue
		}
		if c.classifyMatchPayload(unionPayloads[arm.Tag]) == payloadMoves {
			return true
		}
	}
	return false
}

// consumeOwnerUnionReceiver marks an owner union's deinit receiver moved. The
// dispatch moves the active payload out for cleanup, so `self` is unavailable
// inside the arm bodies and after the match; a second read or re-match of the
// deinitialized union is then a use-after-move.
func (c *Checker) consumeOwnerUnionReceiver(value ast.Expression, env *scope) {
	ident, ok := value.(*ast.IdentExpr)
	if !ok {
		return
	}
	if self, found := env.lookup(ident.Name); found {
		self.moved = true
	}
}

// defineMatchArmPayload binds one union variant payload for a match arm.
// Scalar payloads bind as copies from any match; declared aggregates move out
// of an owned scrutinee (ADR-0090); view payloads and aggregate payloads of a
// borrowed scrutinee stay borrowed so they cannot escape. Inside an owner
// union's own `deinit` the active owner payload is bound as an owned local so
// it can be cleaned through its explicit `deinit`. Inactive variants are never
// bound.
func (c *Checker) defineMatchArmPayload(
	arm ast.MatchArm,
	unionPayloads map[string]string,
	ownerDeinitDispatch bool,
	ownedMatch bool,
	child *scope,
	span ast.Span,
) {
	payload := unionPayloads[arm.Tag]
	if arm.IsWildcard() || payload == "" || arm.Binding == "" {
		return
	}
	value := c.newBinding(arm.Binding, payload)
	value.declSpan = span
	class := c.classifyMatchPayload(payload)
	owned := class == payloadCopies ||
		(class == payloadMoves && ownedMatch) ||
		(ownerDeinitDispatch && !c.isCopyType(payload))
	if !owned {
		value.borrowedParam = true
	}
	child.define(value)
}

// matchPayloadClass says what a match on an owned value may do with one bound
// payload: copy it out, move it out, or keep it borrowed.
type matchPayloadClass int

const (
	payloadBorrows matchPayloadClass = iota
	payloadCopies
	payloadMoves
)

// classifyMatchPayload sorts a payload type for a match on an owned value.
// Only types positively known to be provenance-free copies or declared
// aggregates escape the arm; views and anything unclassified stay borrowed, so
// an unhandled type errs toward rejection, never toward escape (ADR-0090).
func (c *Checker) classifyMatchPayload(typeName string) matchPayloadClass {
	parsed, err := c.types.Parse(typeName)
	if err != nil {
		return payloadBorrows
	}
	name, ok := parsed.(*typ.Name)
	if !ok {
		// []T, &T, ?T, E!T: views and wrappers keep borrow handling.
		return payloadBorrows
	}
	if isRawPointerType(typeName) {
		return payloadBorrows
	}
	if c.isCopyType(typeName) {
		return payloadCopies
	}
	base := strings.Join(name.Path, "::")
	// A declared aggregate moves out, and so does any value with a deinit
	// contract: an owned scrutinee hands its std container payload over the
	// same way it hands a struct over, and the arm then owes the release.
	if c.structs[base] != nil || c.unions[base] != nil || c.valueTypeNeedsConsume(typeName) {
		return payloadMoves
	}
	return payloadBorrows
}

// matchArmTagKnown reports whether a match arm names a known tag. A
// qualified arm (`FsError::NotFound =>`) resolves through the set it names;
// whether that set fits the matched value is the types checker's question.
func (c *Checker) matchArmTagKnown(tags map[string]bool, arm ast.MatchArm) bool {
	if arm.TagSet != "" {
		members := c.errorSets[arm.TagSet]
		return members != nil && members[arm.Tag]
	}
	return tags[arm.Tag]
}

// matchTags returns known tags for enum and union match ownership checks.
func (c *Checker) matchTags(typeName string) (map[string]bool, map[string]string, bool) {
	if tags := c.enums[typeName]; tags != nil {
		return tags, nil, true
	}
	// An error carries nothing, so matching one moves nothing, which is what a
	// tag enum arm already means here.
	if members := c.errorSets[typeName]; members != nil {
		return members, nil, true
	}
	payloads := c.unions[typeName]
	if payloads == nil {
		return nil, nil, false
	}
	tags := map[string]bool{}
	for tag := range payloads {
		tags[tag] = true
	}
	return tags, payloads, true
}

// readExpr checks an expression without consuming owned values.
func (c *Checker) readExpr(expr ast.Expression, env *scope) (string, error) {
	if inner, ok := transparentExprValue(expr); ok {
		return c.readExpr(inner, env)
	}
	switch e := expr.(type) {
	case *ast.IntExpr, *ast.StringExpr, *ast.BoolExpr, *ast.TypeExpr, *ast.NullExpr:
		return c.readScalarExpr(e)
	case *ast.BufferLiteralExpr:
		return e.TypeText(), nil
	case *ast.ComptimeExpr:
		return c.readComptimeExpr(e, env)
	case *ast.IdentExpr:
		return c.readIdentExpr(e, env)
	case *ast.BinaryExpr:
		return c.readBinaryExpr(e, env)
	case *ast.CallExpr:
		return c.checkCallExpr(e, env)
	case *ast.CastExpr:
		return c.readCastExpr(e, env)
	case *ast.TryExpr:
		return c.readTryExpr(e, env)
	case *ast.IndexExpr:
		return c.readIndexExpr(e, env)
	case *ast.StructLiteralExpr:
		return c.readStructLiteralExpr(e, env)
	case *ast.FieldExpr:
		return c.readFieldExpr(e, env)
	case *ast.DerefExpr:
		return c.readDerefExpr(e, env)
	default:
		return c.readControlExpr(expr, env)
	}
}

// transparentExprValue returns the operand of a wrapper that reads as its inner
// expression. `unsafe` says who owns an obligation and carries no value of its
// own, and `&` / `!` / `-` read their operand, so ownership sees through both.
func transparentExprValue(expr ast.Expression) (ast.Expression, bool) {
	switch e := expr.(type) {
	case *ast.UnsafeExpr:
		return e.Value, true
	case *ast.PrefixExpr:
		return e.Right, true
	default:
		return nil, false
	}
}

// readScalarExpr reads literal-like scalar expressions without ownership effects.
func (c *Checker) readScalarExpr(expr ast.Expression) (string, error) {
	if _, ok := expr.(*ast.TypeExpr); ok {
		return "type", nil
	}
	return readLiteralType(expr)
}

// readControlExpr checks control flow expressions without consuming owned values.
func (c *Checker) readControlExpr(expr ast.Expression, env *scope) (string, error) {
	switch e := expr.(type) {
	case *ast.IfStmt:
		return c.readIfExpr(e, env)
	case *ast.MatchStmt:
		return c.readMatchExpr(e, env)
	case *ast.OrelseGuardExpr:
		return c.readOrelseGuardExpr(e, env)
	case *ast.CatchGuardExpr:
		return c.readCatchGuardExpr(e, env)
	case *ast.MoveExpr:
		return "", errorAt(e.Span,
			"move error: `move` marks a hand-off, and `%s` is only read here",
			e.Value.String())
	default:
		return "", errorf("move error: unsupported expression %T", expr)
	}
}

// readOrelseGuardExpr reads `cond orelse return/break/continue`. The null arm
// is a real exit, so it carries the matching statement's obligations, checked
// in a clone: the fall-through path did not take the exit, so nothing the
// exit consumes may look moved after the guard.
func (c *Checker) readOrelseGuardExpr(expr *ast.OrelseGuardExpr, env *scope) (string, error) {
	pending := len(c.pendingOwnerTemps)
	condType, err := c.readExpr(expr.Cond, env)
	if err != nil {
		return "", err
	}
	if err := c.refuseContainerViewOrelse(condType, expr.Cond, env); err != nil {
		return "", err
	}
	err = c.checkNoPendingOwnerTemps(pending, "orelse", expressionSpan(expr.Cond), expr.Cond)
	if err != nil {
		return "", err
	}
	if err := c.checkGuardExit(expr.Exit, env, pending); err != nil {
		return "", err
	}
	return optionalPayloadName(condType), nil
}

// checkGuardExit checks the exit an `orelse` / `catch` guard takes: a
// return, or a branch out of a loop. It is taken in the middle of a
// statement, so the places the statement has moved out of are still held
// on that path.
func (c *Checker) checkGuardExit(exit ast.Statement, env *scope, pending int) error {
	// On the exit path the condition produced nothing to keep -- the guard
	// is taken because it did not -- so what it added is not pending there.
	// It is once the guard falls through, so it comes back after.
	kept := append([]ast.Expression(nil), c.pendingOwnerTemps[pending:]...)
	c.pendingOwnerTemps = c.pendingOwnerTemps[:pending]
	defer func() { c.pendingOwnerTemps = append(c.pendingOwnerTemps, kept...) }()
	return c.withPendingPlacesLive(func() error {
		switch exit := exit.(type) {
		case *ast.ReturnStmt:
			return c.checkReturnStmt(exit, env.clone())
		case *ast.BreakStmt:
			return c.checkLoopBranch(exit.Label, exitBreak, env)
		case *ast.ContinueStmt:
			return c.checkLoopBranch(exit.Label, exitContinue, env)
		}
		return nil
	})
}

// readCatchGuardExpr reads `cond catch return/break/continue`. The failure
// arm is a real exit like an orelse guard's null arm: it carries the matching
// statement's obligations, checked in a clone so nothing it consumes looks
// moved on the fall-through path. The handled failure is not an error return
// path, so no errdefer obligations attach here.
func (c *Checker) readCatchGuardExpr(expr *ast.CatchGuardExpr, env *scope) (string, error) {
	pending := len(c.pendingOwnerTemps)
	condType, err := c.readExpr(expr.Cond, env)
	if err != nil {
		return "", err
	}
	err = c.checkNoPendingOwnerTemps(pending, "catch", expressionSpan(expr.Cond), expr.Cond)
	if err != nil {
		return "", err
	}
	if err := c.checkGuardExit(expr.Exit, env, pending); err != nil {
		return "", err
	}
	elem, _ := c.errorUnionElement(condType)
	if elem == "" {
		return condType, nil
	}
	return elem, nil
}

// readIndexExpr reads checked byte indexing and slicing without moving bytes.
func (c *Checker) readIndexExpr(expr *ast.IndexExpr, env *scope) (string, error) {
	target, err := c.readExpr(expr.Target, env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(target, "[]u8") {
		return "", errorf("move error: index/slice target expects []u8, got %s", target)
	}
	if !expr.Slice {
		if _, err := c.readExpr(expr.Index, env); err != nil {
			return "", err
		}
		return "u8", nil
	}
	if expr.Start != nil {
		if _, err := c.readExpr(expr.Start, env); err != nil {
			return "", err
		}
	}
	if expr.End != nil {
		if _, err := c.readExpr(expr.End, env); err != nil {
			return "", err
		}
	}
	return target, nil
}

// readLiteralType returns the ownership type of scalar literals.
func readLiteralType(expr ast.Expression) (string, error) {
	switch expr.(type) {
	case *ast.IntExpr:
		return "i64", nil
	case *ast.StringExpr:
		return "[]u8", nil
	case *ast.BoolExpr:
		return "bool", nil
	case *ast.NullExpr:
		return "null", nil
	default:
		return "", errorf("move error: unsupported literal %T", expr)
	}
}

// readContextualExpr reads expr and treats fit-checked integer literals as want.
func (c *Checker) readContextualExpr(
	expr ast.Expression,
	want string,
	env *scope,
) (string, error) {
	got, err := c.readExpr(expr, env)
	if err != nil {
		return "", err
	}
	return coerceContextualIntegerLiteral(expr, want, got)
}

// moveContextualExpr moves expr and treats fit-checked integer literals as want.
func (c *Checker) moveContextualExpr(
	expr ast.Expression,
	want string,
	env *scope,
) (string, error) {
	got, err := c.moveExpr(expr, env)
	if err != nil {
		return "", err
	}
	return coerceContextualIntegerLiteral(expr, want, got)
}

// coerceContextualIntegerLiteral narrows only source integer literals.
func coerceContextualIntegerLiteral(expr ast.Expression, want string, got string) (string, error) {
	if got == want || got != "i64" || !isIntegerOwnershipType(want) {
		return got, nil
	}
	value, ok := integerLiteralValue(expr)
	if !ok {
		return got, nil
	}
	if !integerLiteralFitsType(value, want) {
		return "", errorf("move error: integer literal `%s` does not fit %s",
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

// isIntegerOwnershipType reports whether a type is a fixed-width integer.
func isIntegerOwnershipType(typeName string) bool {
	switch typeName {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		return true
	default:
		return false
	}
}

// integerLiteralFitsType checks the fixed-width bounds for contextual literals.
func integerLiteralFitsType(value int64, typeName string) bool {
	switch typeName {
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

// readCastExpr reads the source value and returns the explicit target type.
func (c *Checker) readCastExpr(expr *ast.CastExpr, env *scope) (string, error) {
	if _, err := c.readExpr(expr.Value, env); err != nil {
		return "", err
	}
	return typ.Text(expr.TargetType), nil
}

// moveExpr checks an expression in a move context and enforces the `move`
// marker. A hand-off from a named place carries the marker: that is where the
// obligation leaves the place, and where an errdefer covering it retires
// (ADR-0114). A temporary has no place to leave, so it carries no marker.
func (c *Checker) moveExpr(expr ast.Expression, env *scope) (string, error) {
	marker, place := splitMoveMarker(expr)
	typeName, handedOff, err := c.movePlaceExpr(place, env)
	if err != nil {
		return "", err
	}
	if handedOff {
		// The value has left its place and reached nothing yet: until the
		// call or literal takes it, only this statement holds it. A whole
		// binding keeps its storage until then, so an exit can still release
		// it; a field taken out of a value has no cleanup of its own left.
		if value, ok := movedPlaceBinding(place, env); ok {
			c.pendingMovedPlaces = append(c.pendingMovedPlaces, value)
		} else {
			c.pendingOwnerTemps = append(c.pendingOwnerTemps, place)
		}
	}
	if marker != nil && !handedOff {
		if !c.isCopyPlace(place, env) {
			return "", errorAt(marker.Span,
				"move error: `move` marks a hand-off from a named place, and `%s` is not one",
				place.String())
		}
		// A generic body is one source line per instantiation. The owner one
		// hands off and needs the marker, so the copy one accepts it rather
		// than making the function unwritable.
		if !c.inGenericFunction() {
			return "", errorAt(marker.Span,
				"move error: `%s` is copy data and hands nothing off", place.String())
		}
	}
	// A compile-time expansion has no source line for an author to write on,
	// so the marker is required only where source exists. The generator writes
	// it on the fields it knows own something (ast.ConstructExpansion); the
	// rest of the expansion is checked like any other move.
	span := expressionSpan(place)
	if marker == nil && handedOff && !span.IsZero() {
		if !c.collectMissingMarkers {
			return "", errorAt(span,
				"move error: `%s` is handed off here; write `move %s`",
				place.String(), place.String())
		}
		c.missingMarkers = append(c.missingMarkers, MissingMarker{Span: span})
	}
	return typeName, nil
}

// inGenericFunction reports whether the body being checked defers a type to its
// instantiation, so whether a place hands off is not fixed by this source.
func (c *Checker) inGenericFunction() bool {
	return c.currentFunction != nil && len(c.currentFunction.sig.StaticParams) > 0
}

// isCopyPlace reports whether expr names a binding whose type copies, so a
// `move` marker on it can say that rather than that it found no place.
func (c *Checker) isCopyPlace(expr ast.Expression, env *scope) bool {
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return false
	}
	value, ok := env.lookup(ident.Name)
	return ok && c.isCopyType(value.typeName)
}

// splitMoveMarker separates a `move` marker from the place it covers.
func splitMoveMarker(expr ast.Expression) (*ast.MoveExpr, ast.Expression) {
	if marker, ok := expr.(*ast.MoveExpr); ok {
		return marker, marker.Value
	}
	return nil, expr
}

// checkMovePlaceBinding rejects a named value that cannot be handed off from
// its current frame before the ordinary owner and borrow checks run.
func checkMovePlaceBinding(ident *ast.IdentExpr, value *binding, env *scope, returned bool) error {
	if err := checkDeinitializedUse(ident.Name, value, env, ident.Span); err != nil {
		return err
	}
	if value.moved {
		return errorAt(ident.Span, "move error: moved value `%s` was used", ident.Name)
	}
	if value.borrowedParam && !returned {
		return errorAt(ident.Span,
			"borrow error: borrowed value `%s` cannot escape", ident.Name)
	}
	if tiedStructReturn(value.typeName) && len(value.borrowTargets) > 0 {
		if taskSetReturn(value.typeName) {
			return errorAt(ident.Span,
				"borrow error: TaskSet `%s` cannot outlive its Io or allocator", ident.Name)
		}
		return errorAt(ident.Span,
			"borrow error: borrowed value `%s` cannot escape", ident.Name)
	}
	return nil
}

// movePlaceExpr consumes a non-copy identifier when present and reports whether
// the value was handed off from a named place.
func (c *Checker) movePlaceExpr(expr ast.Expression, env *scope) (string, bool, error) {
	if field, ok := expr.(*ast.FieldExpr); ok && !field.Namespace {
		// A field of a value the frame holds is a place too: taking it out
		// hands the obligation on, so it is marked like any other hand-off.
		typeName, err := c.moveFieldExpr(field, env)
		if err != nil {
			return "", false, err
		}
		return typeName, !c.isCopyType(typeName), nil
	}
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		typeName, err := c.moveNonIdentExpr(expr, env)
		return typeName, false, err
	}
	value, ok := env.lookup(ident.Name)
	if !ok {
		return c.moveUnboundIdent(ident)
	}
	if err := checkMovePlaceBinding(ident, value, env, c.returnedViews[ident]); err != nil {
		return "", false, err
	}
	if value.allocTied() {
		return "", false, errorAt(ident.Span,
			"borrow error: value `%s` is allocated from a tied allocator and cannot escape its frame",
			ident.Name)
	}
	if value.hasAnyBorrow() && !c.isCopyType(value.typeName) {
		return "", false, errorAt(ident.Span,
			"borrow error: value `%s` cannot be moved while borrowed", ident.Name)
	}
	if field, ok := partiallyConsumedField(value); ok {
		return "", false, errorAt(ident.Span,
			"move error: field `%s.%s` is already consumed, so `%s` cannot be handed on whole",
			ident.Name, field, ident.Name)
	}
	if c.isCopyType(value.typeName) {
		return value.typeName, false, nil
	}
	value.moved = true
	return value.typeName, true, nil
}

// moveNonIdentExpr handles move contexts for compound expressions.
func (c *Checker) moveNonIdentExpr(expr ast.Expression, env *scope) (string, error) {
	if deref, ok := expr.(*ast.DerefExpr); ok {
		return c.moveDerefExpr(deref, env)
	}
	if c.isArenaAtExpr(expr, env) {
		typeName, err := c.readExpr(expr, env)
		if err != nil {
			return "", err
		}
		if c.isCopyType(typeName) {
			return typeName, nil
		}
		return "", errorAt(expressionSpan(expr),
			"arena error: Arena.at returns &T and cannot be moved")
	}
	if st, ok := expr.(*ast.StructLiteralExpr); ok {
		return c.moveStructLiteralExpr(st, env)
	}
	if field, ok := expr.(*ast.FieldExpr); ok {
		if field.Namespace {
			return c.readFieldExpr(field, env)
		}
		return c.moveFieldExpr(field, env)
	}
	if stmt, ok := expr.(*ast.IfStmt); ok {
		return c.moveIfExpr(stmt, env)
	}
	if stmt, ok := expr.(*ast.MatchStmt); ok {
		return c.moveMatchExpr(stmt, env)
	}
	typeName, err := c.readExpr(expr, env)
	if err != nil {
		return "", err
	}
	return typeName, c.refuseViewEscape(expr, typeName, env)
}

// refuseViewEscape rejects handing a view still backed by a local borrow to a
// consumer that may keep it, as moving the borrow binding itself is refused:
// the storage the view points into ends with the frame.
func (c *Checker) refuseViewEscape(expr ast.Expression, typeName string, env *scope) error {
	if !isViewType(typeName) || c.returnedViews[expr] {
		return nil
	}
	root := c.borrowClassViewRoot(expr, env)
	if root == nil {
		return nil
	}
	return errorAt(expressionSpan(expr),
		"borrow error: borrowed value `%s` cannot escape", root.name)
}

// readIfExpr checks ownership effects for an if expression in read context.
func (c *Checker) readIfExpr(stmt *ast.IfStmt, env *scope) (string, error) {
	return c.checkIfExprValue(stmt, env, false)
}

// moveIfExpr checks ownership effects for an if expression in move context.
func (c *Checker) moveIfExpr(stmt *ast.IfStmt, env *scope) (string, error) {
	return c.checkIfExprValue(stmt, env, true)
}

// checkIfExprValue merges possible branch moves from an if expression.
func (c *Checker) checkIfExprValue(stmt *ast.IfStmt, env *scope, moveTail bool) (string, error) {
	// One branch runs: what the first produces is not live in the second,
	// and what the expression as a whole hands on is one value.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	if _, err := c.readExpr(stmt.Condition, env); err != nil {
		return "", err
	}
	if stmt.Alternative == nil {
		return "", errorf("move error: if expression requires else branch")
	}
	c.settleOwnerTemps(pending, places, "void", nil)
	left := env.clone()
	leftType, err := c.checkBlockValue(stmt.Consequence, left.child(), moveTail)
	if err != nil {
		return "", err
	}
	c.settleOwnerTemps(pending, places, "void", nil)
	right := env.clone()
	rightType, err := c.checkBlockValue(stmt.Alternative, right.child(), moveTail)
	if err != nil {
		return "", err
	}
	if leftType != rightType {
		return "", errorf("move error: if expression branch types differ: %s vs %s",
			leftType, rightType)
	}
	c.settleOwnerTemps(pending, places, leftType, stmt)
	env.mergeMovedFrom(left)
	env.mergeMovedFrom(right)
	return leftType, nil
}

// readMatchExpr checks ownership effects for a match expression in read context.
func (c *Checker) readMatchExpr(stmt *ast.MatchStmt, env *scope) (string, error) {
	return c.checkMatchExprValue(stmt, env, false)
}

// moveMatchExpr checks ownership effects for a match expression in move context.
func (c *Checker) moveMatchExpr(stmt *ast.MatchStmt, env *scope) (string, error) {
	return c.checkMatchExprValue(stmt, env, true)
}

// checkMatchExprValue merges possible arm moves from a match expression.
func (c *Checker) checkMatchExprValue(
	stmt *ast.MatchStmt,
	env *scope,
	moveTail bool,
) (string, error) {
	// One arm runs: what the value produced is held by the arm that binds
	// it, what one arm produces is not live in the next, and what the
	// expression as a whole hands on is one value.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	valueType, err := c.readExpr(stmt.Value, env)
	if err != nil {
		return "", err
	}
	valueType = borrowedOwnershipValueType(valueType)
	tags, unionPayloads, ok := c.matchTags(valueType)
	if !ok {
		return "", errorf("move error: match expects enum or union, got %s", valueType)
	}
	ownedMatch := c.matchScrutineeOwned(stmt.Value, env)
	if err := c.consumeMovedFromScrutinee(stmt, unionPayloads, env); err != nil {
		return "", err
	}
	var result string
	for idx, arm := range stmt.Arms {
		c.settleOwnerTemps(pending, places, "void", nil)
		got, err := c.checkMatchExprArmValue(arm, tags, unionPayloads, ownedMatch,
			expressionSpan(stmt.Value), env, moveTail)
		if err != nil {
			return "", err
		}
		if idx == 0 {
			result = got
		} else if got != result {
			return "", errorf("move error: match arm types differ: %s vs %s", result, got)
		}
	}
	c.settleOwnerTemps(pending, places, result, stmt)
	return result, nil
}

// checkMatchExprArmValue checks one arm and merges its possible moves.
func (c *Checker) checkMatchExprArmValue(
	arm ast.MatchArm,
	tags map[string]bool,
	unionPayloads map[string]string,
	ownedMatch bool,
	span ast.Span,
	env *scope,
	moveTail bool,
) (string, error) {
	if arm.IsWildcard() {
		if arm.Binding != "" {
			return "", errorf("move error: wildcard match arm cannot bind payload")
		}
	} else if !c.matchArmTagKnown(tags, arm) {
		return "", errorf("move error: unknown match tag `%s`", arm.Tag)
	}
	armEnv := env.clone()
	child := armEnv.child()
	c.defineMatchArmPayload(arm, unionPayloads, false, ownedMatch, child, span)
	got, err := c.checkStmtValue(arm.Body, child, moveTail)
	if err != nil {
		return "", err
	}
	if err := c.checkArmPayloadConsumed(arm, child); err != nil {
		return "", err
	}
	env.mergeMovedFrom(armEnv)
	return got, nil
}

// checkBlockValue checks a branch block used in expression position.
func (c *Checker) checkBlockValue(block *ast.BlockStmt, env *scope, moveTail bool) (string, error) {
	if block == nil || len(block.Statements) == 0 {
		return "", errorf("move error: expression block must end with a value")
	}
	lastUses := blockLastUses(block)
	defers := []ast.Expression{}
	errDeferMark := len(c.liveErrDefers)
	defer c.restoreErrDefers(errDeferMark)
	bindingMark := c.nextID
	// Each statement starts from what the enclosing expression held pending,
	// as in checkBlock; the tail value is what the block itself hands on.
	pendingMark, placesMark := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	lastIdx := len(block.Statements) - 1
	for idx, stmt := range block.Statements[:lastIdx] {
		c.pendingOwnerTemps = c.pendingOwnerTemps[:pendingMark]
		c.pendingMovedPlaces = c.pendingMovedPlaces[:placesMark]
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if err := c.checkDeferStmt(deferStmt, env); err != nil {
				return "", err
			}
			defers = append(defers, deferStmt.Expr)
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if errDeferStmt, ok := stmt.(*ast.ErrDeferStmt); ok {
			if err := c.checkErrDeferStmt(errDeferStmt, env); err != nil {
				return "", err
			}
			env.releaseLastUseBorrows(idx, lastUses)
			continue
		}
		if err := c.checkStmt(stmt, env); err != nil {
			return "", err
		}
		env.releaseLastUseBorrows(idx, lastUses)
	}
	c.pendingOwnerTemps = c.pendingOwnerTemps[:pendingMark]
	c.pendingMovedPlaces = c.pendingMovedPlaces[:placesMark]
	valueType, err := c.checkStmtValue(block.Statements[lastIdx], env, moveTail)
	if err != nil {
		return "", err
	}
	env.releaseLastUseBorrows(lastIdx, lastUses)
	if err := c.checkDeferredCleanups(defers, env); err != nil {
		return "", err
	}
	if err := c.checkOwnersConsumed(env, bindingMark, leakExit{}); err != nil {
		return "", err
	}
	return valueType, nil
}

// checkStmtValue checks a value-producing tail statement.
func (c *Checker) checkStmtValue(stmt ast.Statement, env *scope, moveTail bool) (string, error) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if s.Semicolon {
			return "", errorf("move error: expression block must end with a value")
		}
		if moveTail {
			return c.moveExpr(s.Expr, env)
		}
		return c.readExpr(s.Expr, env)
	case *ast.IfStmt:
		return c.checkIfExprValue(s, env, moveTail)
	case *ast.MatchStmt:
		return c.checkMatchExprValue(s, env, moveTail)
	case *ast.BlockStmt:
		return c.checkBlockValue(s, env, moveTail)
	default:
		return "", errorf("move error: expression block must end with a value")
	}
}

// moveDerefExpr rejects moving non-copy values out through checked borrows.
func (c *Checker) moveDerefExpr(expr *ast.DerefExpr, env *scope) (string, error) {
	if typeName, ok, err := c.rawPointerDerefExprType(expr, env); ok || err != nil {
		return typeName, err
	}
	typeName, err := c.readDerefExpr(expr, env)
	if err != nil {
		return "", err
	}
	if c.isCopyType(typeName) {
		return typeName, c.refuseViewEscape(expr, typeName, env)
	}
	return "", errorf("borrow error: value `%s` cannot be moved out of borrow",
		expr.Receiver.String())
}

// readBinaryExpr reads both operands and returns the operator result type.
func (c *Checker) readBinaryExpr(expr *ast.BinaryExpr, env *scope) (string, error) {
	left, err := c.readExpr(expr.Left, env)
	if err != nil {
		return "", err
	}
	if expr.Operator == "orelse" {
		if err := c.refuseContainerViewOrelse(left, expr.Left, env); err != nil {
			return "", err
		}
		return c.readOrelseDefault(left, expr.Right, env)
	}
	if expr.Operator == "catch" {
		return c.readCatchDefault(left, expr.Right, env)
	}
	if _, err := c.readExpr(expr.Right, env); err != nil {
		return "", err
	}
	if expr.Operator == "and" || expr.Operator == "or" {
		return "bool", nil
	}
	if isBooleanBinaryOperator(expr.Operator) {
		return "bool", nil
	}
	return left, nil
}

// readOrelseDefault reads the default arm of `orelse`. The default stands in
// for the payload, so when the payload carries a deinit contract the default
// is a competing owner producer and must be moved, not aliased.
func (c *Checker) readOrelseDefault(left string, right ast.Expression, env *scope) (string, error) {
	elem := optionalPayloadName(left)
	if c.valueTypeNeedsConsume(elem) {
		if _, err := c.moveExpr(right, env); err != nil {
			return "", err
		}
		return elem, nil
	}
	if _, err := c.readExpr(right, env); err != nil {
		return "", err
	}
	return elem, nil
}

// readCatchDefault reads the default arm of `catch`. The default stands in
// for the success payload the same way an `orelse` default stands in for the
// optional payload, so an owner payload makes it a competing producer.
func (c *Checker) readCatchDefault(left string, right ast.Expression, env *scope) (string, error) {
	elem, _ := c.errorUnionElement(left)
	if elem == "" {
		elem = left
	}
	if c.valueTypeNeedsConsume(elem) {
		if _, err := c.moveExpr(right, env); err != nil {
			return "", err
		}
		return elem, nil
	}
	if _, err := c.readExpr(right, env); err != nil {
		return "", err
	}
	return elem, nil
}

// isBooleanBinaryOperator reports whether a binary operator returns bool.
func isBooleanBinaryOperator(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}

// checkCallExpr validates ownership effects of builtin and user calls. It is
// the funnel every call form passes through, so the tied-allocator taint of
// the result is recorded here once, and no call path can forget it.
func (c *Checker) checkCallExpr(expr *ast.CallExpr, env *scope) (string, error) {
	// The call consumes what its arguments handed it; what stays pending
	// after it is its own result, when that result owns something.
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	result, err := c.dispatchCallExpr(expr, env)
	if err != nil {
		return result, err
	}
	c.settleOwnerTemps(pending, places, result, expr)
	if err := c.checkBorrowOptionalResult(expr, result, env); err != nil {
		return "", err
	}
	c.pendTiedAllocatorArgs(expr.Args, result, env)
	return result, nil
}

// producesOwnerTemp reports whether a value of this type, held by nothing
// but the expression that produced it, is one an early exit would drop: an
// owner, bare or inside the `?T` / `E!T` wrapper its producer returns.
func (c *Checker) producesOwnerTemp(typeName string) bool {
	if elem, _, ok := c.wrappedPayloadElem(typeName); ok {
		typeName = elem
	}
	return c.valueTypeNeedsConsume(typeName)
}

// checkNoPendingOwnerTemps refuses an exit that can be taken while the
// enclosing statement still holds owners nothing has bound: the exit would
// drop them, and the value's own allocator is not written where the
// checker could release them. Binding the value first gives it a name an
// `errdefer` can cover.
func (c *Checker) checkNoPendingOwnerTemps(
	pending int,
	exit string,
	span ast.Span,
	own ast.Expression,
) error {
	for _, producer := range c.pendingOwnerTemps[:pending] {
		if producer == own {
			// The exit's own operand is what it unwraps and hands on, not
			// something it drops; it is here when the expression is read
			// more than once.
			continue
		}
		return errorAt(span,
			"move error: the owner `%s` produced earlier in this statement would leak on this `%s` exit;"+
				" bind it with `let` and register `errdefer` before it", producer.String(), exit)
	}
	return nil
}

// movedPlaceBinding returns the binding a hand-off took a whole value out
// of. A field path or a temporary is not one: nothing but the statement
// holds what left them.
func movedPlaceBinding(place ast.Expression, env *scope) (*binding, bool) {
	ident, ok := place.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}
	value, exists := env.lookup(ident.Name)
	if !exists || !value.moved {
		return nil, false
	}
	return value, true
}

// withPendingPlacesLive runs an exit check with the places this statement
// has moved out of counted as still held. Their storage is untouched until
// the call or literal that takes them runs, so an exit before that point
// leaves the value where it was: a cleanup covering it releases it, and an
// uncovered one is the leak the check reports. The hand-off stands again
// once the check is done, for the path on which the statement completes.
func (c *Checker) withPendingPlacesLive(check func() error) error {
	for _, value := range c.pendingMovedPlaces {
		value.moved = false
	}
	err := check()
	for _, value := range c.pendingMovedPlaces {
		value.moved = true
	}
	return err
}

// checkBorrowOptionalResult is the one gate for every call that produces a
// borrow optional: legal inside a capture condition, legal as the value of a
// declared borrow-optional return when every tied binding is a borrowed
// parameter (the borrow must survive the frame), refused everywhere else.
func (c *Checker) checkBorrowOptionalResult(
	expr *ast.CallExpr,
	result string,
	env *scope,
) error {
	if _, _, bare := typ.BorrowOptionalElem(result); !bare {
		return nil
	}
	if c.captureCondition {
		return nil
	}
	if !c.borrowReturn {
		return errorf("move error: a call returning %s must be consumed by a capture"+
			" (`if call |name|` or `while call |name|`)", result)
	}
	targets, _, ok := c.borrowOptionalCallTargets(expr, env)
	if !ok {
		return errorf("borrow error: returned %s has no recognizable borrow source", result)
	}
	for _, target := range targets {
		holder, exists := env.lookup(target.name)
		if !exists || !holder.borrowedParam {
			return errorf("borrow error: returned %s must borrow a borrowed parameter,"+
				" not local `%s`", result, target.name)
		}
	}
	return nil
}

// dispatchCallExpr routes a call expression to its checker by callee shape.
func (c *Checker) dispatchCallExpr(expr *ast.CallExpr, env *scope) (string, error) {
	sanctioned := c.viewCaptureCall == expr
	if field, ok := expr.Callee.(*ast.FieldExpr); ok {
		return c.checkFieldCallExpr(field, expr.Args, env, sanctioned)
	}
	if typeApply, ok := expr.Callee.(*ast.TypeApplyExpr); ok {
		return c.checkTypeApplyCallExpr(typeApply, expr.Args, env, sanctioned)
	}
	name, ok := expr.Callee.(*ast.IdentExpr)
	if !ok {
		return "", errorf("move error: callee must be a function name")
	}
	if result, ok, err := c.checkBuiltinCall(name.Name, expr, env); ok || err != nil {
		return result, err
	}
	// A binding shadows a declaration: a name bound to a function pointer is
	// called through the pointer. Its signature carries the same move and
	// borrow effects as a directly named function.
	if bound, ok := env.lookup(name.Name); ok {
		if node, isFunc := funcPointerNode(bound.typeName); isFunc {
			return c.checkFuncPointerCall(name.Name, node, expr.Args, env, sanctioned)
		}
	}
	return c.checkUserCall(name.Name, expr.Args, env, sanctioned)
}

// moveUnboundIdent resolves a name no binding holds. A top-level function's
// name is a function pointer value: it owns nothing and borrows nothing,
// because the code it names outlives the program.
func (c *Checker) moveUnboundIdent(ident *ast.IdentExpr) (string, bool, error) {
	if ident.Name == "void" {
		return "", false, errorAt(ident.Span, "move error: void is not a value")
	}
	if fn, isFunc := c.lookupFunctionByValueName(ident.Name); isFunc {
		return functionPointerText(fn.sig), false, nil
	}
	return "", false, errorAt(ident.Span,
		"move error: undefined variable `%s`", ident.Name)
}

// readIdentExpr resolves a name in a read position. A top-level function's
// name is a function pointer value: it owns nothing, and the code it names
// outlives the program.
func (c *Checker) readIdentExpr(expr *ast.IdentExpr, env *scope) (string, error) {
	if _, ok := c.typeArgValues[expr.Name]; ok {
		return "type", nil
	}
	if _, bound := env.lookup(expr.Name); !bound {
		if fn, isFunc := c.lookupFunctionByValueName(expr.Name); isFunc {
			return functionPointerText(fn.sig), nil
		}
	}
	return readIdent(expr, env)
}

// lookupFunctionByValueName resolves the declaration a name used as a value
// refers to. A callee is qualified by the resolver; a name in any other
// position is not, so a module-local function is looked up under the module
// the reader is inside as well as under the name as written.
func (c *Checker) lookupFunctionByValueName(name string) (*functionInfo, bool) {
	if fn, ok := c.functions[name]; ok {
		return fn, true
	}
	if c.currentFunction == nil {
		return nil, false
	}
	prefix := strings.LastIndex(c.currentFunction.name, "::")
	if prefix < 0 {
		return nil, false
	}
	fn, ok := c.functions[c.currentFunction.name[:prefix+2]+name]
	return fn, ok
}

// functionPointerText spells the function pointer type a declaration's name
// has as a value.
func functionPointerText(sig ast.FunctionSignature) string {
	node := &typ.Func{Unsafe: sig.RequiresUnsafe, Result: sig.ReturnType}
	for _, param := range sig.Params {
		node.Params = append(node.Params, borrowedParamType(param))
	}
	return node.String()
}

// borrowedParamType spells a parameter the way a caller writes it: the borrow
// markers a declaration wrote beside the type are part of what it takes.
func borrowedParamType(param ast.Param) typ.Type {
	if param.MutBorrow {
		return &typ.Borrow{Elem: param.TypeName, Mut: true}
	}
	if param.Borrow {
		return &typ.Borrow{Elem: param.TypeName}
	}
	return param.TypeName
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

// checkFuncPointerCall applies the move and borrow effects carried by a
// function pointer's signature. Indirection changes how code is reached, not
// what each parameter receives.
func (c *Checker) checkFuncPointerCall(
	name string,
	node *typ.Func,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, error) {
	fn := functionPointerInfo(node)
	for index, param := range fn.params {
		if index < len(args) && param.borrow && param.mutBorrow {
			c.result.functionPointerMutBorrows[args[index]] = true
		}
	}
	return c.checkCallableCall(name, fn, args, env, sanctioned)
}

// checkUserCall validates one declared-function call. sanctioned marks the
// callers that tie a factory result themselves and so may skip the
// tied-allocator let-binding requirement.
func (c *Checker) checkUserCall(
	name string,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, error) {
	if bound, ok := c.functionArgs[name]; ok {
		// A call written against a `Function` static parameter is a call to
		// what this instantiation bound it to.
		name = bound
	}
	fn, ok := c.functions[name]
	if !ok {
		return "", errorf("move error: undefined function `%s`", name)
	}
	if len(fn.sig.TypeParamNames()) > 0 {
		return "", errorf("move error: `%s` requires explicit static arguments", name)
	}
	return c.checkCallableCall(name, fn, args, env, sanctioned)
}

// checkCallableCall applies one resolved signature at a call site. Direct and
// indirect calls share this path so neither can lose move, borrow, handle, or
// capability effects while choosing how the callee is reached.
func (c *Checker) checkCallableCall(
	name string,
	fn *functionInfo,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, error) {
	if len(args) != len(fn.params) {
		return "", errorf("move error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	if err := c.checkCallHandlePairing(fn.params, nil, args, env); err != nil {
		return "", err
	}
	// A tied-allocator factory call is only legal where something ties its
	// result (the let recognizer, a param-rooted return — those pass
	// sanctioned); anywhere else the tie would silently drop.
	if !sanctioned && returnTypeName(fn) == "Allocator" && c.callTiesAllocator(fn, args, env) {
		return "", errorf("borrow error: `%s` returns a tied allocator; bind it with `let`", name)
	}
	borrowed, err := c.activateBorrowArgs(fn, args, env)
	if err != nil {
		return "", err
	}
	defer releaseTemporaryBorrows(borrowed)
	for idx, arg := range args {
		if err := c.checkUserCallArg(fn, idx, arg, env, sanctioned); err != nil {
			return "", err
		}
	}
	return returnTypeName(fn), nil
}

// checkUserCallArg applies one argument's ownership effect for a user call:
// a shared borrow reads, a lent view or allocator reads, anything else moves.
func (c *Checker) checkUserCallArg(
	fn *functionInfo,
	idx int,
	arg ast.Expression,
	env *scope,
	sanctioned bool,
) error {
	if fn.params[idx].borrow {
		if fn.params[idx].mutBorrow {
			return nil
		}
		_, err := c.readExpr(arg, env)
		return err
	}
	if c.viewArgLend(fn, idx, arg, env) || c.capabilityArgLend(fn, idx, arg, env) ||
		c.sanctionedViewLend(sanctioned, fn, idx, arg, env) {
		_, err := c.readExpr(arg, env)
		return err
	}
	_, err := c.moveExpr(arg, env)
	return err
}

// sanctionedViewLend reports whether arg is a borrow-class view lent to a
// `[]u8` parameter under a caller that ties the result itself — the let
// recognizer, which records the view as a borrow source of the binding. In
// every other context the view would escape untracked, so the lend stays
// refused and the move path rejects it.
func (c *Checker) sanctionedViewLend(
	sanctioned bool,
	fn *functionInfo,
	idx int,
	arg ast.Expression,
	env *scope,
) bool {
	if !sanctioned || fn.params[idx].typeName != "[]u8" {
		return false
	}
	return c.borrowClassViewRoot(arg, env) != nil
}

// capabilityArgLend reports whether arg is a tied capability binding lent to a
// capability parameter. The lend itself is free -- a capability is copied into
// every call that takes one -- and what the callee's result may carry out is
// covered by pendTiedAllocatorArgs at the call site.
func (c *Checker) capabilityArgLend(
	fn *functionInfo,
	idx int,
	arg ast.Expression,
	env *scope,
) bool {
	if !capabilityReturn(fn.params[idx].typeName) {
		return false
	}
	return c.tiedCapabilityArg(arg, env) != nil
}

// pendTiedAllocatorArgs records the tie obligation of a call that consumed a
// tied allocator argument and returns something that can carry its memory.
// The obligation is discharged by the `let` that binds the result; anything
// else leaves it pending and checkBlock rejects the statement.
func (c *Checker) pendTiedAllocatorArgs(
	args []ast.Expression,
	returnType string,
	env *scope,
) {
	if !allocationCarryingReturnType(returnType) {
		return
	}
	for _, arg := range args {
		if alloc := c.tiedAllocatorArg(arg, env); alloc != nil {
			c.pendingAllocTaints = append(c.pendingAllocTaints,
				allocTaint{alloc: alloc, span: expressionSpan(arg)})
		}
	}
}

// allocationCarryingReturnType reports a return type that can hold memory the
// callee allocated: owners and aggregates. Scalars carry nothing, a view
// carries only the view channel (a view of callee-allocated memory cannot
// leave the callee — its owner would leak there), and an Allocator result is
// the factory recognizer's job, not a taint.
func allocationCarryingReturnType(typeName string) bool {
	if viewFreeReturnType(typeName) || typeName == "Allocator" {
		return false
	}
	if idx := strings.Index(typeName, "!"); idx >= 0 {
		typeName = typeName[idx+1:]
	}
	return !strings.HasPrefix(typeName, "&") && !strings.HasPrefix(typeName, "[]")
}

// viewArgLend reports whether arg is a tracked view binding lent to a plain
// `[]u8` parameter for the call's duration (SPEC §9: a borrow argument ends
// with the call statement). Lending is only safe when the callee cannot retain
// the view past the statement: neither the return type nor any `&var`
// parameter it could write into may carry a view out.
func (c *Checker) viewArgLend(
	fn *functionInfo,
	idx int,
	arg ast.Expression,
	env *scope,
) bool {
	if fn.params[idx].typeName != "[]u8" {
		return false
	}
	if c.borrowClassViewRoot(arg, env) == nil && !borrowedViewParamArg(arg, env) {
		return false
	}
	return !c.viewCarryingType(returnTypeName(fn)) && !c.viewSmugglingParams(fn)
}

// borrowedViewParamArg reports whether arg names a `&var []u8` borrow
// parameter: its backing outlives the frame, but the binding itself cannot
// move, so passing it on is a lend rather than a copy.
func borrowedViewParamArg(arg ast.Expression, env *scope) bool {
	ident, ok := arg.(*ast.IdentExpr)
	if !ok {
		return false
	}
	value, exists := env.lookup(ident.Name)
	return exists && value.borrowedParam && value.typeName == "[]u8"
}

// viewSmugglingParams reports whether fn declares a `&var` parameter whose
// type can hold a view: a callee could store a lent view through it, so the
// storage outliving the call makes lending unsafe. A `&var []u8` slot cannot
// be re-pointed (ADR-0096) and is exempt.
func (c *Checker) viewSmugglingParams(fn *functionInfo) bool {
	for _, param := range fn.params {
		if !param.borrow || !param.mutBorrow || param.typeName == "[]u8" {
			continue
		}
		if c.viewCarryingType(param.typeName) {
			return true
		}
	}
	return false
}

// viewFreeReturnType reports a return type that cannot carry a view out:
// scalars and void, with or without an error union.
func viewFreeReturnType(typeName string) bool {
	if idx := strings.Index(typeName, "!"); idx >= 0 {
		typeName = typeName[idx+1:]
	}
	switch typeName {
	case "void", "bool", "u8", "i64", "i32", "u32", "u64", "f64":
		return true
	}
	return false
}

// viewCarryingType reports whether a value of typeName can hold a view: view
// spellings themselves, and declared structs or unions any of whose field
// types carry one. Generic containers follow the storage they actually retain:
// Map copies its byte key and Handle stores only an opaque ID.
func (c *Checker) viewCarryingType(typeName string) bool {
	return c.viewCarryingTypeSeen(typeName, map[string]bool{})
}

// capabilityCarryingType reports whether a retained value can keep an Io or
// Allocator alive. TaskSet already carries the two capabilities selected at
// construction; allowing another one inside an owned worker state could
// launder a frame-tied capability through the byte copy into the task.
func (c *Checker) capabilityCarryingType(typeName string) bool {
	return c.capabilityCarryingTypeSeen(typeName, map[string]bool{})
}

// capabilityCarryingTypeSeen walks recursive aggregate declarations once.
func (c *Checker) capabilityCarryingTypeSeen(typeName string, seen map[string]bool) bool {
	typeName = viewCarrierPayload(typeName)
	if capabilityReturn(typeName) {
		return true
	}
	if seen[typeName] {
		return false
	}
	seen[typeName] = true
	base := typeName
	if genericBase, arg, ok := splitGenericType(typeName); ok {
		base = genericBase
		if base != "std::arena::Handle" {
			args, err := typ.SplitArgs(arg)
			if err != nil {
				args = []string{arg}
			}
			for _, argType := range args {
				if c.capabilityCarryingTypeSeen(argType, seen) {
					return true
				}
			}
		}
	}
	for _, fieldType := range c.structs[base] {
		if c.capabilityCarryingTypeSeen(fieldType, seen) {
			return true
		}
	}
	for _, payload := range c.unions[base] {
		if payload != "" && c.capabilityCarryingTypeSeen(payload, seen) {
			return true
		}
	}
	return false
}

// viewCarrierPayload strips the wrappers a view rides through: the success of
// `!T` and the payload of `?T` carry their views the same way.
func viewCarrierPayload(typeName string) string {
	if idx := strings.Index(typeName, "!"); idx >= 0 {
		typeName = typeName[idx+1:]
	}
	if elem, ok := typ.OptionalElem(typeName); ok {
		return elem
	}
	return typeName
}

// viewCarryingTypeSeen is viewCarryingType with a cycle guard over named types.
func (c *Checker) viewCarryingTypeSeen(typeName string, seen map[string]bool) bool {
	typeName = viewCarrierPayload(typeName)
	if typeName == "[]u8" || strings.HasPrefix(typeName, "&") {
		return true
	}
	if seen[typeName] {
		return false
	}
	seen[typeName] = true
	base := typeName
	if genericBase, arg, ok := splitGenericType(typeName); ok {
		base = genericBase
		if c.genericArgsCarryView(base, arg, seen) {
			return true
		}
	}
	for _, fieldType := range c.structs[base] {
		if c.viewCarryingTypeSeen(fieldType, seen) {
			return true
		}
	}
	for _, payload := range c.unions[base] {
		if payload != "" && c.viewCarryingTypeSeen(payload, seen) {
			return true
		}
	}
	return false
}

// genericArgsCarryView follows only the generic values the runtime type
// retains. Map copies its byte key, and Handle retains no value of T.
func (c *Checker) genericArgsCarryView(base string, text string, seen map[string]bool) bool {
	args, err := typ.SplitArgs(text)
	if err != nil {
		args = []string{text}
	}
	if base == "std::arena::Handle" {
		return false
	}
	if base == "std::map::Map" && len(args) == 2 {
		return c.viewCarryingTypeSeen(args[1], seen)
	}
	for _, argType := range args {
		if c.viewCarryingTypeSeen(argType, seen) {
			return true
		}
	}
	return false
}

// viewCaptureStructType reports a struct type a let binding may capture views
// into. View-only values ride the borrow class; owner values keep both their
// explicit deinit obligation and their borrow targets.
func (c *Checker) viewCaptureStructType(typeName string) bool {
	if strings.Contains(typeName, "!") {
		return false
	}
	if c.structs[typeName] == nil {
		return false
	}
	return c.viewCarryingType(typeName)
}

// borrowClassViewRoot resolves expr to the first borrow-class binding backing
// the view it yields, or nil when there is none; see borrowClassViewRoots.
func (c *Checker) borrowClassViewRoot(expr ast.Expression, env *scope) *binding {
	if roots := c.borrowClassViewRoots(expr, env); len(roots) > 0 {
		return roots[0]
	}
	return nil
}

// borrowClassViewRoots resolves expr to the borrow-class bindings backing the
// view it yields — a local view binding, or a view-capturing struct whose
// `[]u8` field is read — through the forms that derive a view from a view
// without leaving its storage: a slice, a read back through `&[]u8`, an
// `orelse` / `catch` default, and the tails of an `if` or `match` expression.
// Empty for params, statics, and owned values, whose views are free: a
// parameter outlives the frame, so what it backs cannot dangle here.
func (c *Checker) borrowClassViewRoots(expr ast.Expression, env *scope) []*binding {
	switch e := unwrapExpressionMarkers(expr).(type) {
	case *ast.IndexExpr:
		if !e.Slice {
			return nil
		}
		return c.borrowClassViewRoots(e.Target, env)
	case *ast.DerefExpr:
		return c.placeViewRoots(e.Receiver, isViewOrReferenceType, env)
	case *ast.BinaryExpr:
		if e.Operator != "orelse" && e.Operator != "catch" {
			return nil
		}
		return c.borrowClassViewRoots(e.Right, env)
	case *ast.IfStmt:
		return mergeViewRoots(c.blockValueViewRoots(e.Consequence, env),
			c.blockValueViewRoots(e.Alternative, env))
	case *ast.MatchStmt:
		var roots []*binding
		for _, arm := range e.Arms {
			roots = mergeViewRoots(roots, c.stmtValueViewRoots(arm.Body, env))
		}
		return roots
	}
	return c.placeViewRoots(expr, isViewType, env)
}

// placeViewRoots resolves a named place — a binding or a field path into one
// — to its borrow-class root when the place has the type want accepts.
func (c *Checker) placeViewRoots(
	expr ast.Expression,
	want func(string) bool,
	env *scope,
) []*binding {
	root, field, ok := directFieldRoot(expr, env)
	if !ok || root == nil {
		return nil
	}
	if !root.localBorrow && len(root.borrowTargets) == 0 {
		return nil
	}
	typeName := root.typeName
	if field != "" {
		fieldType, ok := c.fieldPathType(root.typeName, field)
		if !ok {
			return nil
		}
		typeName = fieldType
	}
	if !want(typeName) {
		return nil
	}
	return []*binding{root}
}

// blockValueViewRoots resolves the view roots of the value a block yields.
func (c *Checker) blockValueViewRoots(block *ast.BlockStmt, env *scope) []*binding {
	if block == nil || len(block.Statements) == 0 {
		return nil
	}
	return c.stmtValueViewRoots(block.Statements[len(block.Statements)-1], env)
}

// stmtValueViewRoots resolves the view roots of the value a tail statement
// yields: an expression, or a nested `if`, `match`, or block.
func (c *Checker) stmtValueViewRoots(stmt ast.Statement, env *scope) []*binding {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if s.Semicolon {
			return nil
		}
		return c.borrowClassViewRoots(s.Expr, env)
	case *ast.IfStmt:
		return c.borrowClassViewRoots(s, env)
	case *ast.MatchStmt:
		return c.borrowClassViewRoots(s, env)
	case *ast.BlockStmt:
		return c.blockValueViewRoots(s, env)
	}
	return nil
}

// mergeViewRoots appends the roots of more not already in roots.
func mergeViewRoots(roots, more []*binding) []*binding {
	for _, root := range more {
		seen := false
		for _, known := range roots {
			if known == root {
				seen = true
				break
			}
		}
		if !seen {
			roots = append(roots, root)
		}
	}
	return roots
}

// isViewType reports whether typeName is the byte view.
func isViewType(typeName string) bool {
	return typeName == "[]u8"
}

// isViewOrReferenceType reports whether typeName is the byte view or a
// reference to one: a `let r = &view` binding carries the view's own type
// with the view as its borrow target, a `&[]u8` parameter the reference type,
// and a deref reads either back as the view itself.
func isViewOrReferenceType(typeName string) bool {
	if isViewType(typeName) {
		return true
	}
	_, _, inner, ok := explicitOwnershipBorrowType(typeName)
	return ok && inner == "[]u8"
}

// checkFieldCallExpr validates calls whose callee is a dotted expression.
func (c *Checker) checkFieldCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, error) {
	if typ, ok, err := c.checkUnionConstructor(field, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkQualifiedUserCall(
		field, args, env, sanctioned,
	); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkQualifiedBuiltin(field, args, env); ok || err != nil {
		return typ, err
	}
	return c.checkMethodCallExpr(field, args, env)
}

// checkQualifiedUserCall validates ownership for source-loaded qualified calls.
func (c *Checker) checkQualifiedUserCall(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, bool, error) {
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
	typ, err := c.checkUserCall(name, args, env, sanctioned)
	return typ, true, err
}

// checkQualifiedBuiltin validates ownership for std:: namespace prototype calls.
func (c *Checker) checkQualifiedBuiltin(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	name, ok := qualifiedName(field)
	if !ok {
		return "", false, nil
	}
	if err := c.rejectUnknownBuiltin(name); err != nil {
		return "", true, err
	}
	if typ, ok, err := c.checkQualifiedStdBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := checkIoBuiltin(name, args); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// rejectUnknownBuiltin refuses a primitive the Go implementation does not have.
// The type checker rejects these first in a full run; this keeps the rule true
// of this checker on its own, which is how its own tests read it.
func (c *Checker) rejectUnknownBuiltin(name string) error {
	if !strings.HasPrefix(name, "std::internal::builtin::") || stdprim.Primitive(name) {
		return nil
	}
	return errorf("move error: `%s` is not a primitive", name)
}

// checkQualifiedStdBuiltin validates declarative core and fs ownership effects.
func (c *Checker) checkQualifiedStdBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if typ, ok, err := c.checkSimpleCoreBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkFsBuiltin(name, args, env); ok || err != nil {
		return typ, ok, err
	}
	return "", false, nil
}

// checkSimpleCoreBuiltin validates declarative core primitive ownership effects.
func (c *Checker) checkSimpleCoreBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	signature, ok := stdprim.SimpleCoreSignatures[name]
	if !ok {
		return "", false, nil
	}
	if len(args) != len(signature.Args) {
		return "", true, errorf("move error: `%s` expects %s", name,
			coreOwnershipArgsText(signature.Args))
	}
	for idx, arg := range args {
		if err := c.checkCoreArg(name, idx, signature.Args[idx], arg, env); err != nil {
			return "", true, err
		}
	}
	return signature.Return, true, nil
}

// checkCoreArg reads one declarative primitive argument without moving it.
func (c *Checker) checkCoreArg(
	name string,
	index int,
	want stdprim.ArgKind,
	arg ast.Expression,
	env *scope,
) error {
	if want == stdprim.ArgIo {
		return c.checkIoArg(arg, env, name)
	}
	if want == stdprim.ArgStringOut {
		return c.checkStringOutArg(name, arg, env)
	}
	got, err := c.readContextualExpr(arg, string(want), env)
	if err != nil {
		return err
	}
	if got != string(want) {
		return errorf("move error: `%s` arg %d expects %s, got %s",
			name, index+1, want, got)
	}
	return nil
}

// coreOwnershipArgsText renders declarative primitive arguments for diagnostics.
func coreOwnershipArgsText(args []stdprim.ArgKind) string {
	if len(args) == 0 {
		return "0 args"
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == stdprim.ArgIo {
			parts = append(parts, "io")
			continue
		}
		parts = append(parts, string(arg))
	}
	return strings.Join(parts, " and ")
}

// checkFsBuiltin validates ownership for filesystem host primitives.
func (c *Checker) checkFsBuiltin(
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std::internal::builtin::fs_write_file":
		return c.checkFsWriteFile(args, env)
	case "std::internal::builtin::fs_exists":
		return c.checkFsPathOnly("std::fs::exists", args, env, "std::fs::Error!bool")
	case "std::internal::builtin::fs_metadata":
		return c.checkFsPathOnly("std::fs::metadata", args, env, "std::fs::Error!std::fs::Metadata")
	case "std::internal::builtin::fs_read_dir":
		return c.checkFsReadDir(args, env)
	case "std::internal::builtin::fs_create_dir",
		"std::internal::builtin::fs_remove_dir",
		"std::internal::builtin::fs_remove_file":
		return c.checkFsPathOnly(name, args, env, "std::fs::Error!void")
	case "std::internal::builtin::fs_rename":
		return c.checkFsRename(args, env)
	default:
		return "", false, nil
	}
}

// checkFsReadDir validates the explicit allocator and borrowed path without
// moving either capability into the returned directory entries.
func (c *Checker) checkFsReadDir(
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	const name = "std::fs::read_dir"
	if len(args) != 3 {
		return "", true, errorf("move error: `%s` expects io, allocator, and path", name)
	}
	if err := c.checkIoArg(args[0], env, name); err != nil {
		return "", true, err
	}
	if err := c.checkCoreArg(name, 1, stdprim.ArgAllocator, args[1], env); err != nil {
		return "", true, err
	}
	path, err := c.readExpr(args[2], env)
	if err != nil {
		return "", true, err
	}
	if !sameOwnershipType(path, "[]u8") {
		return "", true, errorf("move error: `%s` expects []u8 path, got %s", name, path)
	}
	return "std::fs::Error!std::array::Array<std::fs::DirEntry>", true, nil
}

// checkStringOutArg reads a &var std::string::String destination argument
// without moving the buffer behind it.
func (c *Checker) checkStringOutArg(label string, arg ast.Expression, env *scope) error {
	const stringType = "std::string::String"
	if prefix, ok := borrowPrefix(arg); ok {
		if prefix.Operator != "&var" {
			return errorf("move error: `%s` expects &var %s out", label, stringType)
		}
		got, err := c.readExpr(prefix.Right, env)
		if err != nil {
			return err
		}
		if !sameOwnershipType(got, stringType) {
			return errorf("move error: `%s` expects &var %s out, got &var %s",
				label, stringType, got)
		}
		return nil
	}
	got, err := c.readExpr(arg, env)
	if err != nil {
		return err
	}
	if ident, ok := arg.(*ast.IdentExpr); ok && sameOwnershipType(got, stringType) {
		if value, bound := env.lookup(ident.Name); bound && value.mutBorrow {
			return nil
		}
	}
	return errorf("move error: `%s` expects &var %s out, got %s",
		label, stringType, got)
}

// checkFsWriteFile validates ownership effects for std::fs::write_file.
func (c *Checker) checkFsWriteFile(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("move error: `std::fs::write_file` expects io, path, and bytes")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::write_file"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"path", "bytes"} {
		got, err := c.readExpr(args[idx+1], env)
		if err != nil {
			return "", true, err
		}
		if !sameOwnershipType(got, "[]u8") {
			return "", true, errorf(
				"move error: `std::fs::write_file` expects []u8 %s, got %s", label, got)
		}
	}
	return "std::fs::Error!void", true, nil
}

// checkFsRename validates ownership effects for std::fs::rename.
func (c *Checker) checkFsRename(args []ast.Expression, env *scope) (string, bool, error) {
	if len(args) != 3 {
		return "", true, errorf("move error: `std::fs::rename` expects io, from, and to")
	}
	if err := c.checkIoArg(args[0], env, "std::fs::rename"); err != nil {
		return "", true, err
	}
	for idx, label := range []string{"from", "to"} {
		got, err := c.readExpr(args[idx+1], env)
		if err != nil {
			return "", true, err
		}
		if !sameOwnershipType(got, "[]u8") {
			return "", true, errorf(
				"move error: `std::fs::rename` expects []u8 %s, got %s", label, got)
		}
	}
	return "std::fs::Error!void", true, nil
}

// checkFsPathOnly validates common std::fs Io and path arguments.
func (c *Checker) checkFsPathOnly(
	name string,
	args []ast.Expression,
	env *scope,
	result string,
) (string, bool, error) {
	if len(args) != 2 {
		return "", true, errorf("move error: `%s` expects io and path", name)
	}
	if err := c.checkIoArg(args[0], env, name); err != nil {
		return "", true, err
	}
	path, err := c.readExpr(args[1], env)
	if err != nil {
		return "", true, err
	}
	if !sameOwnershipType(path, "[]u8") {
		return "", true, errorf("move error: `%s` expects []u8 path, got %s", name, path)
	}
	return result, true, nil
}

// checkArrayConstructor validates std::array::new<T>(allocator) ownership.
func (c *Checker) checkArrayConstructor(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if elem != "T" {
		if err := c.rejectArrayElementType(elem); err != nil {
			return "", err
		}
	}
	if len(args) != 1 {
		return "", errorf("move error: `std::array::Array<%s>` expects allocator", elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("move error: `std::array::Array<%s>` expects Allocator, got %s",
			elem, got)
	}
	return fmt.Sprintf("std::array::Array<%s>", elem), nil
}

// checkMapConstructorAllowTypeParams validates std source Map wrapper construction.
func (c *Checker) checkMapConstructorAllowTypeParams(
	argsText string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	mapArgs, ok := splitGenericArgs(argsText)
	if !ok || len(mapArgs) != 2 {
		return "", errorf("map error: std::map::Map expects 2 static arguments")
	}
	if len(args) != 1 {
		return "", errorf("map error: `std::map::Map` expects allocator")
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("map error: `std::map::Map` expects Allocator, got %s", got)
	}
	return fmt.Sprintf("std::map::Map<%s, %s>", mapArgs[0], mapArgs[1]), nil
}

// rejectArrayElementType rejects element types with unresolved ownership hazards.
func (c *Checker) rejectArrayElementType(elem string) error {
	if err := c.rejectArrayStorageType(elem, map[string]bool{}); err != nil {
		return errorf("array error: Array element is not safe: %w", err)
	}
	return nil
}

// rejectArrayStorageType rejects values that are not Array-safe yet.
func (c *Checker) rejectArrayStorageType(typeName string, seen map[string]bool) error {
	if seen[typeName] {
		return nil
	}
	seen[typeName] = true
	if isRawPointerType(typeName) {
		return errorf("array error: Array element cannot be raw pointer")
	}
	// An owner inside `E!T` is released only by the capture that opens it,
	// and a container's element cleanup has no `if` to open one with; the
	// optional form is refused before this by the type checker.
	if elem, wrapper, ok := c.wrappedPayloadElem(typeName); ok &&
		wrapper == "error union" && c.valueTypeNeedsConsume(elem) {
		return errorf("array error: Array element cannot be an error union around an owner")
	}
	if base, arg, ok := splitGenericType(typeName); ok && base == "option" {
		if err := c.rejectArrayStorageType(arg, seen); err != nil {
			return err
		}
	}
	if err := c.rejectArrayStorageStruct(typeName, seen); err != nil {
		return err
	}
	return c.rejectArrayStorageUnion(typeName, seen)
}

// rejectArrayStorageStruct checks struct fields recursively for Array storage.
func (c *Checker) rejectArrayStorageStruct(typeName string, seen map[string]bool) error {
	fields := c.structs[typeName]
	for fieldName, fieldType := range fields {
		if err := c.rejectArrayStorageType(fieldType, seen); err != nil {
			return errorf("array error: struct `%s.%s` cannot be Array element: %w",
				typeName, fieldName, err)
		}
	}
	return nil
}

// rejectArrayStorageUnion checks union payloads recursively for Array storage.
func (c *Checker) rejectArrayStorageUnion(typeName string, seen map[string]bool) error {
	variants := c.unions[typeName]
	for variant, payload := range variants {
		if payload == "" {
			continue
		}
		if err := c.rejectArrayStorageType(payload, seen); err != nil {
			return errorf("array error: union `%s::%s` cannot be Array element: %w",
				typeName, variant, err)
		}
	}
	return nil
}

// checkIoArg reads and validates an explicit Io argument.
func (c *Checker) checkIoArg(arg ast.Expression, env *scope, name string) error {
	got, err := c.readExpr(arg, env)
	if err != nil {
		return err
	}
	if got != "Io" {
		return errorf("move error: `%s` expects Io, got %s", name, got)
	}
	return nil
}

// checkIoBuiltin validates std::io constructor ownership effects.
func checkIoBuiltin(name string, args []ast.Expression) (string, bool, error) {
	switch name {
	case "std::internal::builtin::io_blocking", "std::internal::builtin::io_failing":
		_, err := checkNoArgOwnershipCall(name, args)
		if err != nil {
			return "", true, err
		}
		return "Io", true, nil
	default:
		return "", false, nil
	}
}

// typeApplyTarget resolves the callee and static arguments of a `<...>` call,
// and refuses an unknown primitive before any of them is looked at.
func (c *Checker) typeApplyTarget(expr *ast.TypeApplyExpr) (string, string, error) {
	name, ok := qualifiedName(expr.Callee)
	if !ok {
		return "", "", errorf("move error: unsupported type application `%s`", expr.String())
	}
	if err := c.rejectUnknownBuiltin(name); err != nil {
		return "", "", err
	}
	return name, c.instantiateTypeArgText(expr.TypeArg), nil
}

// checkTypeApplyCallExpr validates typed std constructor ownership effects.
func (c *Checker) checkTypeApplyCallExpr(
	expr *ast.TypeApplyExpr,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, error) {
	name, typeArg, err := c.typeApplyTarget(expr)
	if err != nil {
		return "", err
	}
	if typ, ok, err := c.checkMetaApply(name, typeArg, args, env); ok || err != nil {
		return typ, err
	}
	if name == "ptr_from_int" || name == "int_from_ptr" {
		return c.checkPointerIntCastBuiltin(name, typeArg, args, env)
	}
	typ, ok, err := c.checkGenericUserTypeApply(name, typeArg, args, env, sanctioned)
	if ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinMapTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBuiltinContainerTypeApply(
		name, typeArg, args, env,
	); ok || err != nil {
		return typ, err
	}
	if name == "std::internal::builtin::mem_allocator_from" {
		return c.checkAllocatorFrom(args, env)
	}
	if name == "std::internal::builtin::task_new" {
		return c.checkTaskNew(args, env)
	}
	if name == "std::internal::builtin::task_set_spawn" {
		return c.checkTaskSetSpawn(args, env)
	}
	return "", errorf("move error: `%s` does not take static arguments", name)
}

// checkAllocatorFrom walks the arguments of `mem_allocator_from`. The state is
// borrowed for as long as the allocator lives, which the tied-allocator
// recognizer enforces from the wrapper's signature; the two functions own
// nothing, because the code they name outlives the program.
func (c *Checker) checkAllocatorFrom(
	args []ast.Expression,
	env *scope,
) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "Allocator", nil
}

// checkTaskNew walks the arguments of `task_new`. The state is borrowed for as
// long as the task runs, which the Future's own shape enforces: the cell the
// borrow points into is owned by the Future, and the only ways to release the
// Future are the two that finish the task first.
func (c *Checker) checkTaskNew(
	args []ast.Expression,
	env *scope,
) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "std::io::Error!i64", nil
}

// checkTaskSetSpawn moves the state into the task set. Every other argument
// is a capability, handle, function address, or size and is only read. The
// runtime copies the moved state only after all allocations have succeeded;
// lowering releases it on the primitive's failure path.
func (c *Checker) checkTaskSetSpawn(
	args []ast.Expression,
	env *scope,
) (string, error) {
	for index, arg := range args {
		var err error
		if index == 4 {
			_, err = c.moveExpr(arg, env)
		} else {
			_, err = c.readExpr(arg, env)
		}
		if err != nil {
			return "", err
		}
	}
	return "std::io::Error!void", nil
}

// checkArenaTypeApply validates std::arena::new<T>(allocator) ownership.
func (c *Checker) checkArenaTypeApply(
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	parts, ok := splitGenericArgs(typeArg)
	if !ok || len(parts) != 1 {
		return "", errorf("arena error: std::arena::new expects 1 type argument")
	}
	elem := parts[0]
	if len(args) != 1 {
		return "", errorf(
			"arena error: `std::arena::new<%s>` expects exactly one allocator argument",
			elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("arena error: `std::arena::new<%s>` expects Allocator, got %s",
			elem, got)
	}
	return fmt.Sprintf("std::arena::Arena<%s>", elem), nil
}

// checkBuiltinContainerTypeApply validates std-only generic container primitives.
func (c *Checker) checkBuiltinContainerTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if strings.HasPrefix(name, "std::internal::builtin::arena") {
		return c.checkBuiltinArenaTypeApply(name, typeArg, args, env)
	}
	if typ, ok, err := c.checkBuiltinBoxTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, ok, err
	}
	if typ, ok, err := c.checkBuiltinArrayTypeApply(name, typeArg, args, env); ok || err != nil {
		return typ, ok, err
	}
	return c.checkBuiltinTestingTypeApply(name, typeArg, args, env)
}

// checkBuiltinArenaTypeApply validates the Arena constructor and the storage
// primitives std::arena's methods forward to.
func (c *Checker) checkBuiltinArenaTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if name == "std::internal::builtin::arena" {
		typ, err := c.checkArenaTypeApply(typeArg, args, env)
		return typ, true, err
	}
	receiver := fmt.Sprintf("std::arena::Arena<%s>", typeArg)
	method := strings.TrimPrefix(name, "std::internal::builtin::arena_")
	return c.checkBuiltinReceiverMethod(name, receiver,
		func(rest []ast.Expression) (string, error) {
			return c.checkArenaPrimitiveMethod(typeArg, method, rest, env)
		}, args, env)
}

// checkArenaPrimitiveMethod validates Arena primitives that back source
// wrappers. Handle provenance is not asked here: only std::arena's own bodies
// reach a primitive, and the handle they pass on is the one the call site
// already had checked against the arena it names.
func (c *Checker) checkArenaPrimitiveMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "add":
		return c.readArenaPrimitiveAdd(elem, args, env)
	case "at":
		if err := c.readArenaPrimitiveHandle("Arena.at", elem, args, env); err != nil {
			return "", err
		}
		return "&" + elem, nil
	case "at_mut":
		if err := c.readArenaPrimitiveHandle("Arena.at_mut", elem, args, env); err != nil {
			return "", err
		}
		return "?&var " + elem, nil
	case "deinit":
		// The raw primitive frees the storage through the allocator the
		// release names: the header keeps none of its own (ADR-0131,
		// ADR-0132).
		err := c.readReleaseAllocator("Arena.deinit", nil, args, env)
		return "void", err
	}
	if len(args) != 0 {
		return "", errorf("arena error: `Arena.%s` expects 0 args, got %d", name, len(args))
	}
	switch name {
	case "len":
		return "i64", nil
	case "pop_or_panic":
		return elem, nil
	default:
		return "", errorf("arena error: Arena has no storage primitive `%s`", name)
	}
}

// readArenaPrimitiveAdd reads the allocator the storage is bought from and the
// element handed over to the arena.
func (c *Checker) readArenaPrimitiveAdd(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 2 {
		return "", errorf("arena error: `Arena.add` expects 2 args, got %d", len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("arena error: `Arena.add` expects Allocator, got %s", got)
	}
	if _, err := c.readExpr(args[1], env); err != nil {
		return "", err
	}
	return fmt.Sprintf("std::mem::Error!std::arena::Handle<%s>", elem), nil
}

// readArenaPrimitiveHandle reads the one handle argument an arena accessor
// takes. what names the accessor in errors.
func (c *Checker) readArenaPrimitiveHandle(
	what string,
	elem string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 1 {
		return errorf("arena error: `%s` expects 1 arg, got %d", what, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	want := fmt.Sprintf("std::arena::Handle<%s>", elem)
	if got != want {
		return errorf("arena error: `%s` expects %s, got %s", what, want, got)
	}
	return nil
}

// checkBuiltinTestingTypeApply validates typed std::testing primitives.
func (c *Checker) checkBuiltinTestingTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if name != "std::internal::builtin::test_fail_equal" {
		return "", false, nil
	}
	typ, err := c.checkBuiltinTestFailEqual(typeArg, args, env)
	return typ, true, err
}

// checkBuiltinTestFailEqual validates ownership for the typed testing failure primitive.
func (c *Checker) checkBuiltinTestFailEqual(
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 2 {
		return "", errorf("move error: `std::testing::expect_equal<%s>` expects 2 args", typeArg)
	}
	for idx, arg := range args {
		got, err := c.readContextualExpr(arg, typeArg, env)
		if err != nil {
			return "", err
		}
		if got != typeArg {
			return "", errorf(
				"move error: arg %d of `std::testing::expect_equal<%s>` expects %s, got %s",
				idx+1,
				typeArg,
				typeArg,
				got,
			)
		}
	}
	return "void", nil
}

// checkBuiltinArrayTypeApply validates std-only generic Array primitives.
func (c *Checker) checkBuiltinArrayTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std::internal::builtin::array":
		typ, err := c.checkArrayConstructor(typeArg, args, env)
		return typ, true, err
	case "std::internal::builtin::array_append",
		"std::internal::builtin::array_append_bytes",
		"std::internal::builtin::array_len",
		"std::internal::builtin::array_capacity",
		"std::internal::builtin::array_pop", "std::internal::builtin::array_pop_or_panic",
		"std::internal::builtin::array_get", "std::internal::builtin::array_get_or_panic",
		"std::internal::builtin::array_at", "std::internal::builtin::array_at_mut",
		"std::internal::builtin::array_set", "std::internal::builtin::array_swap",
		"std::internal::builtin::array_deinit":
		return c.checkBuiltinArrayMethod(name, typeArg, args, env)
	default:
		return "", false, nil
	}
}

// checkBuiltinBoxTypeApply validates std-only generic Box primitives.
func (c *Checker) checkBuiltinBoxTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	switch name {
	case "std::internal::builtin::box":
		typ, err := c.checkBoxConstructor(typeArg, args, env)
		return typ, true, err
	case "std::internal::builtin::box_borrow",
		"std::internal::builtin::box_borrow_mut",
		"std::internal::builtin::box_deinit",
		"std::internal::builtin::box_take":
		return c.checkBuiltinBoxMethod(name, typeArg, args, env)
	default:
		return "", false, nil
	}
}

// checkBoxConstructor validates std::mem::box<T>(allocator, value) ownership.
func (c *Checker) checkBoxConstructor(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 2 {
		return "", errorf("box error: `std::mem::Box<%s>` expects allocator and value", elem)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("box error: `std::mem::Box<%s>` expects Allocator, got %s", elem, got)
	}
	got, err = c.moveContextualExpr(args[1], elem, env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", errorf("box error: `std::mem::Box<%s>` expects %s value, got %s",
			elem, elem, got)
	}
	return fmt.Sprintf("std::mem::Error!std::mem::Box<%s>", elem), nil
}

// checkBuiltinBoxMethod validates std-only Box method primitives.
func (c *Checker) checkBuiltinBoxMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	method := strings.TrimPrefix(name, "std::internal::builtin::box_")
	if method == "borrow_mut" {
		method = "borrow_mut"
	}
	receiver := fmt.Sprintf("std::mem::Box<%s>", typeArg)
	return c.checkBuiltinReceiverMethod(name, receiver, func(rest []ast.Expression) (string, error) {
		// A release names the allocator the cell came from; a read of the
		// payload needs nothing beyond the receiver.
		if method == "deinit" || method == "take" {
			if err := c.readReleaseAllocator("Box."+method, nil, rest, env); err != nil {
				return "", err
			}
		} else if len(rest) != 0 {
			return "", errorf("box error: `Box.%s` expects 0 args, got %d",
				method, len(rest))
		}
		switch method {
		case "borrow":
			return "&" + typeArg, nil
		case "borrow_mut":
			return "&var " + typeArg, nil
		case "deinit":
			return "void", nil
		case "take":
			return typeArg, nil
		default:
			return "", errorf("box error: Box has no method `%s`", method)
		}
	}, args, env)
}

// readReleaseAllocator reads the single Allocator a release names. A value
// keeps no copy of the allocator that made it (ADR-0132), so the call spells
// it, the same way the construction did.
func (c *Checker) readReleaseAllocator(
	what string,
	receiver *binding,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 1 {
		return errorf("move error: `%s` expects 1 args, got %d", what, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	if got != "Allocator" {
		return errorf("move error: `%s` expects Allocator, got %s", what, got)
	}
	return c.checkReleaseTie(what, receiver, args[0], env)
}

// checkReleaseTie requires the allocator a release names to be the one the
// value was made from (ADR-0132). Only a tied allocator has an identity to
// compare: `page_allocator()` values are indistinguishable, so nothing there
// is checkable and nothing needs to be. An owner built from a tied allocator
// carries that allocator among its borrow targets, which is what says which
// one it is.
func (c *Checker) checkReleaseTie(
	what string,
	receiver *binding,
	arg ast.Expression,
	env *scope,
) error {
	if receiver == nil {
		return nil
	}
	tied := c.tiedAllocatorArg(arg, env)
	sources := receiver.tiedAllocatorSources()
	if len(sources) == 0 {
		if tied != nil {
			return errorf(
				"move error: `%s` names allocator `%s`, which is tied to state `%s` was not built from",
				what, tied.name, receiver.name)
		}
		return nil
	}
	if tied == nil {
		return errorf(
			"move error: `%s` must name the tied allocator `%s` was built from",
			what, receiver.name)
	}
	for _, source := range sources {
		// By id, not by pointer: a branch or a loop body is checked against a
		// clone of the scope, so the allocator an argument resolves to there
		// is a copy of the one the owner recorded. The id copies with the
		// binding and names the same declaration on both sides.
		if source.id == tied.id {
			return nil
		}
	}
	return errorf(
		"move error: `%s` names allocator `%s`, but `%s` was built from another",
		what, tied.name, receiver.name)
}

// checkBuiltinArrayMethod validates std-only Array method primitives.
func (c *Checker) checkBuiltinArrayMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	method := strings.TrimPrefix(name, "std::internal::builtin::array_")
	return c.checkBuiltinReceiverMethod(name, fmt.Sprintf("std::array::Array<%s>", typeArg),
		func(rest []ast.Expression) (string, error) {
			return c.checkArrayPrimitiveMethod(typeArg, method, rest, env)
		}, args, env)
}

// checkBuiltinReceiverMethod validates a trusted primitive receiver argument.
func (c *Checker) checkBuiltinReceiverMethod(
	name string,
	receiver string,
	checkRest func([]ast.Expression) (string, error),
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if len(args) == 0 {
		return "", true, errorf("move error: `%s` expects receiver", name)
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", true, err
	}
	if got != receiver {
		return "", true, errorf("move error: `%s` expects %s receiver, got %s",
			name, receiver, got)
	}
	typ, err := checkRest(args[1:])
	return typ, true, err
}

// checkArrayPrimitiveMethod validates Array primitives that back source wrappers.
func (c *Checker) checkArrayPrimitiveMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "at":
		if _, err := c.checkOneI64Arg("Array.at", args, env); err != nil {
			return "", err
		}
		return "?&" + elem, nil
	case "at_mut":
		if _, err := c.checkOneI64Arg("Array.at_mut", args, env); err != nil {
			return "", err
		}
		return "?&var " + elem, nil
	case "get", "get_or_panic":
		if len(args) != 1 {
			return "", errorf("array error: `Array.%s` expects 1 arg, got %d",
				name, len(args))
		}
		got, err := c.readExpr(args[0], env)
		if err != nil {
			return "", err
		}
		if got != "i64" {
			return "", errorf("array error: `Array.%s` expects i64 index, got %s", name, got)
		}
		if !isGenericParamName(elem) && !c.isCopyType(elem) {
			return "", errorf("array error: `Array.%s` requires copy element", name)
		}
		if name == "get" {
			return "?" + elem, nil
		}
		return elem, nil
	case "deinit":
		// The raw primitive frees the buffer through the allocator the release
		// names: the header keeps none of its own (ADR-0132).
		if err := c.readReleaseAllocator("Array.deinit", nil, args, env); err != nil {
			return "", err
		}
		return "void", nil
	default:
		array := &binding{typeName: fmt.Sprintf("std::array::Array<%s>", elem)}
		return c.checkArrayMethod(array, elem, name, args, env)
	}
}

// checkBuiltinMapTypeApply validates the std-only Map runtime primitive.
func (c *Checker) checkBuiltinMapTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if strings.HasPrefix(name, "std::internal::builtin::map_") {
		return c.checkBuiltinMapMethod(name, typeArg, args, env)
	}
	if name != "std::internal::builtin::map" {
		return "", false, nil
	}
	typ, err := c.checkMapConstructorAllowTypeParams(typeArg, args, env)
	return typ, true, err
}

// checkBuiltinMapMethod validates std-only Map method primitives.
func (c *Checker) checkBuiltinMapMethod(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	mapArgs, err := c.checkedMapArgsAllowTypeParams(typeArg)
	if err != nil {
		return "", true, err
	}
	receiver := fmt.Sprintf("std::map::Map<%s, %s>", mapArgs[0], mapArgs[1])
	method := strings.TrimPrefix(name, "std::internal::builtin::map_")
	return c.checkBuiltinReceiverMethod(name, receiver,
		func(rest []ast.Expression) (string, error) {
			mapValue := &binding{typeName: receiver}
			return c.checkMapPrimitiveMethod(mapValue, mapArgs[0], mapArgs[1], method, rest, env)
		}, args, env)
}

// checkMapPrimitiveMethod validates Map primitives that back source wrappers.
func (c *Checker) checkMapPrimitiveMethod(
	mapValue *binding,
	keyType string,
	valueType string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	// at/at_mut come first: only the std wrapper body reaches the primitive,
	// and routing a concrete instantiation to checkMapMethod would hit the
	// user-facing capture-only refusal.
	switch name {
	case "at":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env); err != nil {
			return "", err
		}
		return "?&" + valueType, nil
	case "at_mut":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env); err != nil {
			return "", err
		}
		return "?&var " + valueType, nil
	case "take_value_at":
		// Reserved to Map.deinit's cascade, so it is answered here and never
		// reaches checkMapMethod: a caller outside std spells no name for it.
		if err := c.checkMapIndexArg(name, args, env); err != nil {
			return "", err
		}
		return valueType, nil
	}
	if !isGenericParamName(keyType) && !isGenericParamName(valueType) {
		argsText := mapValue.typeName[len("std::map::Map<") : len(mapValue.typeName)-1]
		return c.checkMapMethod(mapValue, argsText, name, args, env)
	}
	return c.checkGenericMapPrimitiveMethod(mapValue, keyType, valueType, name, args, env)
}

// checkGenericMapPrimitiveMethod validates Map primitives applied to generic
// static arguments, which only a std wrapper body can spell.
func (c *Checker) checkGenericMapPrimitiveMethod(
	mapValue *binding,
	keyType string,
	valueType string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "insert":
		if len(args) != 3 {
			return "", errorf("map error: `Map.insert` expects 3 args, got %d", len(args))
		}
		if err := c.readGrowAllocator("map", "Map.insert", mapValue, args[0], env); err != nil {
			return "", err
		}
		if got, err := c.readExpr(args[1], env); err != nil {
			return "", err
		} else if got != keyType {
			return "", errorf("map error: `Map.insert` expects %s key, got %s", keyType, got)
		}
		got, err := c.moveContextualExpr(args[2], valueType, env)
		if err != nil {
			return "", err
		}
		if got != valueType {
			return "", errorf("map error: `Map.insert` expects %s value, got %s", valueType, got)
		}
		return "std::mem::Error!void", nil
	case "get":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env); err != nil {
			return "", err
		}
		return "?" + valueType, nil
	case "contains":
		if err := c.checkMapPrimitiveKeyArg(name, keyType, args, env); err != nil {
			return "", err
		}
		return "bool", nil
	default:
		return c.checkMapMethod(mapValue,
			mapValue.typeName[len("std::map::Map<"):len(mapValue.typeName)-1], name, args, env)
	}
}

// checkMapPrimitiveKeyArg validates a generic Map wrapper key argument.
func (c *Checker) checkMapPrimitiveKeyArg(
	name string,
	keyType string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 1 {
		return errorf("map error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	if got != keyType {
		return errorf("map error: `Map.%s` expects %s key, got %s", name, keyType, got)
	}
	return nil
}

// checkedMapArgsAllowTypeParams validates Map arguments inside std generic wrappers.
func (c *Checker) checkedMapArgsAllowTypeParams(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, errorf("map error: std::map::Map expects 2 static arguments")
	}
	if isGenericParamName(args[0]) {
		return args, nil
	}
	if isGenericParamName(args[1]) {
		return args, nil
	}
	return c.checkedMapArgs(arg)
}

// isGenericParamName reports whether a type spelling is a std wrapper parameter.
func isGenericParamName(name string) bool {
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// checkGenericUserTypeApply validates ownership for source std generic wrappers.
func (c *Checker) checkGenericUserTypeApply(
	name string,
	typeArg string,
	args []ast.Expression,
	env *scope,
	sanctioned bool,
) (string, bool, error) {
	fn := c.functions[name]
	if fn == nil || len(fn.sig.StaticParams) == 0 {
		return "", false, nil
	}
	if len(args) != len(fn.params) {
		return "", true, errorf("move error: `%s` expects %d args, got %d",
			name, len(fn.params), len(args))
	}
	subst, err := c.genericCallSubst(name, fn, typeArg)
	if err != nil {
		return "", true, err
	}
	// The call site sees the instantiated signature and nothing else: with
	// `T` spelled out, an owner argument moves, a view lends or is refused,
	// and two borrows of one value collide, exactly as for a direct call.
	result, err := c.checkCallableCall(name, instantiateFunctionInfo(fn, subst), args, env, sanctioned)
	if err != nil {
		return "", true, err
	}
	restore := c.bindMetaFields(c.genericCallFields(fn, typeArg))
	restoreFunctions := c.bindFunctionArgs(c.genericCallFunctions(fn, typeArg))
	err = c.checkGenericInstantiation(fn, subst)
	restoreFunctions()
	restore()
	if err != nil {
		return "", true, err
	}
	return result, true, nil
}

// instantiateFunctionInfo is the signature one generic call sees: the
// declaration's, with every type parameter replaced by the static argument
// the call spelled. Only the ownership-facing parts are substituted; the
// body and static parameter list stay the declaration's.
func instantiateFunctionInfo(fn *functionInfo, subst map[string]string) *functionInfo {
	inst := *fn
	inst.params = make([]paramInfo, len(fn.params))
	for idx, param := range fn.params {
		param.typeName = substituteOwnershipType(param.typeName, subst)
		inst.params[idx] = param
	}
	inst.returnType = substituteOwnershipType(returnTypeName(fn), subst)
	return &inst
}

// genericCallSubst resolves a generic call's static arguments into the
// type-parameter substitution the ownership check applies.
func (c *Checker) genericCallSubst(
	name string,
	fn *functionInfo,
	typeArg string,
) (map[string]string, error) {
	staticArgs, ok := splitGenericArgs(typeArg)
	if !ok || len(staticArgs) != len(fn.sig.StaticParams) {
		return nil, errorf("move error: `%s` expects %d static arguments",
			name, len(fn.sig.StaticParams))
	}
	// Only the entries that declare types take part in substitution; a
	// compile-time value carries no ownership.
	typeArgs := []string{}
	for idx, param := range fn.sig.StaticParams {
		if param.IsType() {
			typeArgs = append(typeArgs, staticArgs[idx])
		}
	}
	if err := c.checkGenericWrapperTypeArgs(name, typeArgs); err != nil {
		return nil, err
	}
	subst := map[string]string{}
	for idx, param := range fn.sig.TypeParamNames() {
		subst[param] = typeArgs[idx]
	}
	return subst, nil
}

// hasFunctionStaticParam reports whether a signature names a function among its
// compile-time parameters.
func hasFunctionStaticParam(sig ast.FunctionSignature) bool {
	for _, param := range sig.StaticParams {
		if param.IsType() {
			continue
		}
		if typ.Text(param.Type) == "Function" {
			return true
		}
	}
	return false
}

// genericCallFunctions reads the `Function` static arguments of one call, so a
// body that calls one is checked against what it was given.
func (c *Checker) genericCallFunctions(fn *functionInfo, typeArg string) map[string]string {
	staticArgs, ok := splitGenericArgs(typeArg)
	if !ok || len(staticArgs) != len(fn.sig.StaticParams) {
		return nil
	}
	bound := map[string]string{}
	for idx, param := range fn.sig.StaticParams {
		if param.IsType() || typ.Text(param.Type) != "Function" {
			continue
		}
		name := strings.TrimSpace(staticArgs[idx])
		if outer, ok := c.functionArgs[name]; ok {
			bound[param.Name] = outer
			continue
		}
		bound[param.Name] = name
	}
	if len(bound) == 0 {
		return nil
	}
	return bound
}

// bindFunctionArgs makes the `Function` arguments of one instantiation readable
// by the calls written against them, and returns the call that unbinds them.
func (c *Checker) bindFunctionArgs(bound map[string]string) func() {
	if len(bound) == 0 {
		return func() {}
	}
	previous := make(map[string]string, len(bound))
	had := make(map[string]bool, len(bound))
	for name, target := range bound {
		previous[name], had[name] = c.functionArgs[name]
		c.functionArgs[name] = target
	}
	return func() {
		for name := range bound {
			if had[name] {
				c.functionArgs[name] = previous[name]
				continue
			}
			delete(c.functionArgs, name)
		}
	}
}

// genericCallFields reads the `Field` static arguments of one call. A field
// token instantiates like a type argument, not like the other compile-time
// values: the body reads it through the `std::meta` forms written against it,
// so each bound field is its own instance.
func (c *Checker) genericCallFields(fn *functionInfo, typeArg string) map[string]metaField {
	staticArgs, ok := splitGenericArgs(typeArg)
	if !ok || len(staticArgs) != len(fn.sig.StaticParams) {
		return nil
	}
	fields := map[string]metaField{}
	owner := ""
	for idx, param := range fn.sig.StaticParams {
		if param.IsType() {
			owner = strings.TrimSpace(staticArgs[idx])
			continue
		}
		if typ.Text(param.Type) != "Field" {
			continue
		}
		name := strings.TrimSpace(staticArgs[idx])
		if bound, ok := c.metaFields[name]; ok {
			fields[param.Name] = bound
			continue
		}
		owned, err := c.publicFields(owner)
		if err != nil {
			continue
		}
		for _, field := range owned {
			if field.name == name {
				fields[param.Name] = field
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// bindMetaFields makes the `Field` arguments of one instantiation readable by
// the forms written against them, and returns the call that unbinds them.
func (c *Checker) bindMetaFields(fields map[string]metaField) func() {
	if len(fields) == 0 {
		return func() {}
	}
	previous := make(map[string]metaField, len(fields))
	had := make(map[string]bool, len(fields))
	for name, field := range fields {
		previous[name], had[name] = c.metaFields[name]
		c.metaFields[name] = field
	}
	return func() {
		for name := range fields {
			if had[name] {
				c.metaFields[name] = previous[name]
				continue
			}
			delete(c.metaFields, name)
		}
	}
}

// checkGenericInstantiation checks a generic function body for one static type set.
func (c *Checker) checkGenericInstantiation(fn *functionInfo, subst map[string]string) error {
	done, err := c.enterInstantiation(fn, subst)
	if err != nil || done {
		return err
	}
	defer func() { c.instantiationDepth-- }()
	env := newScope(nil)
	if err := c.defineParams(fn, env, subst); err != nil {
		return err
	}
	previousLoopStarts := c.loopStarts
	previousPending := c.pendingOwnerTemps
	previousPlaces := c.pendingMovedPlaces
	previousFunction := c.currentFunction
	previousStd := c.currentStd
	previousTypeArgValues := c.typeArgValues
	c.loopStarts = nil
	c.pendingOwnerTemps = nil
	c.pendingMovedPlaces = nil
	c.currentFunction = fn
	c.currentStd = fn.sig.Std
	c.typeArgValues = subst
	defer func() {
		c.loopStarts = previousLoopStarts
		c.pendingOwnerTemps = previousPending
		c.pendingMovedPlaces = previousPlaces
		c.currentFunction = previousFunction
		c.currentStd = previousStd
		c.typeArgValues = previousTypeArgValues
	}()
	return c.checkBlock(fn.body, env)
}

// maxInstantiationDepth bounds how deep generic instantiation may nest, for
// the reason the type checker's bound of the same name gives (#1627). The two
// checkers walk the same instantiations, so they carry the same bound.
const maxInstantiationDepth = 64

// enterInstantiation records one instantiation and reports whether it has
// already been checked.
func (c *Checker) enterInstantiation(fn *functionInfo, subst map[string]string) (bool, error) {
	args := make([]string, 0, len(subst))
	for _, param := range fn.sig.TypeParamNames() {
		args = append(args, subst[param])
	}
	key := fn.name + "<" + strings.Join(args, ", ") + ">"
	if c.checkedInstances[key] {
		return true, nil
	}
	if c.instantiationDepth >= maxInstantiationDepth {
		return true, errorf(
			"move error: generic instantiation nested deeper than %d at `%s`",
			maxInstantiationDepth, elideTypeText(key))
	}
	c.checkedInstances[key] = true
	c.instantiationDepth++
	return false, nil
}

// elideTypeText shortens a spelling for a diagnostic, so a type that grew past
// the bound does not bury the sentence that says what went wrong.
func elideTypeText(text string) string {
	const budget = 60
	if len(text) <= budget*2 {
		return text
	}
	return text[:budget] + " ... " + text[len(text)-budget:]
}

// checkGenericWrapperTypeArgs validates std wrapper-specific static ownership contracts.
func (c *Checker) checkGenericWrapperTypeArgs(name string, typeArgs []string) error {
	switch name {
	case "std::array::Array":
		return c.rejectArrayElementType(typeArgs[0])
	case "std::map::Map":
		if _, err := c.checkedMapArgs(strings.Join(typeArgs, ", ")); err != nil {
			return err
		}
	case "std::io::spawn":
		state := typeArgs[0]
		if c.viewCarryingType(state) || c.capabilityCarryingType(state) {
			return errorf(
				"borrow error: `std::io::spawn` state `%s` must own its data and contain no Io or Allocator",
				state)
		}
	}
	return nil
}

// checkBuiltinCall validates ownership effects for builtin calls.
func (c *Checker) checkBuiltinCall(
	name string,
	expr *ast.CallExpr,
	env *scope,
) (string, bool, error) {
	switch name {
	case "print":
		result, err := c.checkPrintCall(expr, env)
		return result, true, err
	case "ptr_read", "ptr_write", "volatile_read", "volatile_write":
		result, err := c.checkPointerBuiltin(expr, env)
		return result, true, err
	case "Io":
		return "", true, errorf("move error: use `std::io::blocking()`")
	default:
		return "", false, nil
	}
}

// readTryExpr reads a !T expression and returns T.
func (c *Checker) readTryExpr(expr *ast.TryExpr, env *scope) (string, error) {
	// What was pending before the operand is what the error exit drops; the
	// operand's own result is what the try unwraps and hands on.
	pending := len(c.pendingOwnerTemps)
	got, err := c.readExpr(expr.Value, env)
	if err != nil {
		return "", err
	}
	arg, ok := c.errorUnionElement(got)
	if !ok {
		return "", errorf("move error: try expects !T, got %s", got)
	}
	err = c.checkNoPendingOwnerTemps(pending, "try", expressionSpan(expr), expr.Value)
	if err != nil {
		return "", err
	}
	// A try can return early through the error path, which runs any active
	// errdefer cleanups. Their receivers must still be valid at this point,
	// except the ones a move has retired. The same early exit must not leak
	// a live owner: every owner must be consumed or covered by a defer /
	// errdefer cleanup before the try.
	err = c.withPendingPlacesLive(func() error {
		retired, err := c.validateErrDeferReceivers(env)
		if err != nil {
			return err
		}
		c.result.tryRetiredErrDefers[expr] = retired
		return c.checkOwnersConsumed(env, 0,
			leakExit{kind: exitTry, span: expressionSpan(expr)})
	})
	if err != nil {
		return "", err
	}
	return arg, nil
}

// checkPointerBuiltin reads raw pointer builtin arguments without moving values.
func (c *Checker) checkPointerBuiltin(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	if name, ok := expr.Callee.(*ast.IdentExpr); ok && name.Name == "ptr_read" {
		return "i64", nil
	}
	return "void", nil
}

// checkPointerIntCastBuiltin reads pointer/integer conversion arguments without moving values.
func (c *Checker) checkPointerIntCastBuiltin(
	_ string,
	typeArg string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return typeArg, nil
}

// readStructLiteralExpr checks a literal without consuming field values.
func (c *Checker) readStructLiteralExpr(expr *ast.StructLiteralExpr, env *scope) (string, error) {
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	for _, field := range expr.Fields {
		if _, err := c.readExpr(field.Value, env); err != nil {
			return "", err
		}
	}
	c.settleOwnerTemps(pending, places, expr.TypeName, expr)
	return expr.TypeName, nil
}

// moveStructLiteralExpr moves field values into a new struct value.
func (c *Checker) moveStructLiteralExpr(expr *ast.StructLiteralExpr, env *scope) (string, error) {
	pending, places := len(c.pendingOwnerTemps), len(c.pendingMovedPlaces)
	for _, field := range expr.Fields {
		if _, err := c.moveExpr(field.Value, env); err != nil {
			return "", err
		}
	}
	c.settleOwnerTemps(pending, places, expr.TypeName, expr)
	return expr.TypeName, nil
}

// settleOwnerTemps records that a call or literal took every owner its
// operands handed it -- the temporaries and the places moved since it
// began -- and that its own result is what stays pending when that result
// owns something.
func (c *Checker) settleOwnerTemps(
	pending int,
	places int,
	typeName string,
	producer ast.Expression,
) {
	c.pendingOwnerTemps = c.pendingOwnerTemps[:pending]
	c.pendingMovedPlaces = c.pendingMovedPlaces[:places]
	if producer != nil && c.producesOwnerTemp(typeName) {
		c.pendingOwnerTemps = append(c.pendingOwnerTemps, producer)
	}
}

// readFieldExpr reads a field without moving the receiver.
func (c *Checker) readFieldExpr(expr *ast.FieldExpr, env *scope) (string, error) {
	if expr.Namespace {
		return c.readNamespaceExpr(expr)
	}
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if _, exists := c.enums[ident.Name]; exists {
			return "", errorAt(expr.Span, "move error: enum tag `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
		if _, exists := c.errorSets[ident.Name]; exists {
			return "", errorAt(expr.Span, "move error: error `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
		if _, exists := c.unions[ident.Name]; exists {
			return "", errorAt(expr.Span, "move error: union variant `%s.%s` must use `::`",
				ident.Name, expr.Name)
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", err
	}
	receiverType = borrowedOwnershipValueType(receiverType)
	if root, field, ok := directFieldRoot(expr, env); ok {
		if field != "" && root.fieldDeinit[field] {
			return "", errorAt(expr.Span, "move error: field `%s.%s` was deinitialized",
				root.name, field)
		}
		if root.activeMutBorrows > 0 {
			return "", errorAt(expr.Span,
				"borrow error: value `%s` cannot be read while mutably borrowed",
				root.name)
		}
		if root.fieldMutBorrows[field] > 0 {
			return "", errorAt(expr.Span,
				"borrow error: field `%s.%s` cannot be read while mutably borrowed",
				root.name, field)
		}
	}
	if fields := c.structs[receiverType]; fields != nil {
		if typ, ok := fields[expr.Name]; ok {
			return typ, nil
		}
	}
	if typ, ok := readFsFieldType(receiverType, expr.Name); ok {
		return typ, nil
	}
	return receiverType, nil
}

// readFsFieldType returns ownership types for builtin filesystem structs.
func readFsFieldType(receiverType string, field string) (string, bool) {
	switch receiverType {
	case "std::fs::Metadata":
		return readFsMetadataFieldType(field)
	case "std::fs::DirEntry":
		return readFsDirEntryFieldType(field)
	default:
		return "", false
	}
}

// readFsMetadataFieldType returns ownership types for std::fs::Metadata fields.
func readFsMetadataFieldType(field string) (string, bool) {
	switch field {
	case "size":
		return "i64", true
	case "is_dir":
		return "bool", true
	default:
		return "", false
	}
}

// readFsDirEntryFieldType returns ownership types for std::fs::DirEntry fields.
func readFsDirEntryFieldType(field string) (string, bool) {
	switch field {
	case "name", "path":
		return "std::string::String", true
	case "is_dir":
		return "bool", true
	default:
		return "", false
	}
}

// readNamespaceExpr reads enum or payload-free union namespace lookup.
func (c *Checker) readNamespaceExpr(expr *ast.FieldExpr) (string, error) {
	dotted, ok := qualifiedName(expr.Receiver)
	if !ok {
		return "", errorAt(expr.Span, "move error: invalid namespace lookup `%s`", expr.String())
	}
	name := strings.ReplaceAll(dotted, ".", "::")
	if tags, exists := c.enums[name]; exists {
		if !tags[expr.Name] {
			return "", errorAt(expr.Span,
				"move error: unknown enum tag `%s::%s`", name, expr.Name)
		}
		return name, nil
	}
	if members, exists := c.errorSets[name]; exists {
		if !members[expr.Name] {
			return "", errorAt(expr.Span,
				"move error: unknown error `%s::%s`", name, expr.Name)
		}
		return name, nil
	}
	if variants, exists := c.unions[name]; exists {
		payload, ok := variants[expr.Name]
		if !ok {
			return "", errorAt(expr.Span, "move error: unknown union variant `%s::%s`",
				name, expr.Name)
		}
		if payload != "" {
			return "", errorAt(expr.Span,
				"move error: union variant `%s::%s` expects payload",
				name, expr.Name)
		}
		return name, nil
	}
	return "", errorAt(expr.Span, "move error: unknown namespace `%s`", name)
}

// moveFieldExpr rejects partial moves from borrowed or aggregate values.
func (c *Checker) moveFieldExpr(expr *ast.FieldExpr, env *scope) (string, error) {
	typeName, err := c.readFieldExpr(expr, env)
	if err != nil {
		return "", err
	}
	// A `[]u8` field of a borrow-class value is a view of the same backing;
	// copying it out in a move context would shed the tie.
	if typeName == "[]u8" && c.borrowClassViewRoot(expr, env) != nil {
		return "", errorAt(expr.Span,
			"borrow error: view field `%s` cannot escape its borrowed owner", expr.String())
	}
	if c.isCopyType(typeName) {
		return typeName, nil
	}
	if name, ok := c.borrowedFieldRoot(expr, env); ok {
		return "", errorAt(
			expr.Span,
			"borrow error: field `%s` cannot be moved out of borrowed value `%s`",
			expr.String(),
			name,
		)
	}
	if c.containsArenaAt(expr.Receiver, env) {
		return "", errorAt(
			expr.Span,
			"arena error: Arena.at returns &T, so its fields cannot be moved",
		)
	}
	if err := c.consumeOwnerField(expr, env); err != nil {
		return "", err
	}
	return typeName, nil
}

// consumeOwnerField takes one owner field out of a value the frame holds. The
// caller of a value parameter has already let go, and a local is the frame's
// own, so taking a field apart cannot free anything twice: the obligation moves
// from the aggregate to the field, and every field still has to be consumed
// before the block ends.
//
// A type that declares its own `deinit` is taken whole. Its obligation is the
// type's -- memory it took from an allocator, a descriptor -- and no sequence
// of field consumes discharges it, so taking it apart would leave a value with
// no way out (#1633).
func (c *Checker) consumeOwnerField(expr *ast.FieldExpr, env *scope) error {
	root, path, ok := directFieldRoot(expr, env)
	if !ok || root == nil || path == "" {
		return errorAt(expr.Span, "move error: field `%s` cannot be moved out of aggregate",
			expr.String())
	}
	if strings.Contains(path, ".") {
		// A nested field belongs to the value in between, which is still whole
		// here. Take that one out first and the field comes with it.
		return errorAt(expr.Span,
			"move error: field `%s` belongs to `%s`; move that one out first",
			expr.String(), path[:strings.LastIndexByte(path, '.')])
	}
	if ast.OwnerType(c.declaredDeinits, root.typeName) {
		return errorAt(expr.Span,
			"move error: `%s` declares its own deinit and is consumed whole, "+
				"so field `%s` cannot be moved out",
			root.typeName, expr.String())
	}
	if root.moved || root.deinitialized {
		return errorAt(expr.Span, "move error: moved value `%s` was used", root.name)
	}
	if root.hasAnyBorrow() {
		return errorAt(expr.Span,
			"borrow error: field `%s` cannot be moved while `%s` is borrowed",
			expr.String(), root.name)
	}
	if root.fieldDeinit[path] {
		return errorAt(expr.Span, "move error: field `%s` was already consumed", expr.String())
	}
	root.markFieldDeinit(path)
	return nil
}

// checkAssignmentBorrowConflict rejects writes that overlap active borrows.
func (c *Checker) checkAssignmentBorrowConflict(expr ast.Expression, env *scope) error {
	root, field, ok := directFieldRoot(expr, env)
	if !ok {
		return nil
	}
	if field == "" {
		if root.hasAnyBorrow() {
			return errorf("borrow error: value `%s` cannot be assigned while borrowed", root.name)
		}
		return nil
	}
	if root.borrowedParam && !root.mutBorrow {
		return errorf("borrow error: cannot assign field through shared borrow `%s`", root.name)
	}
	if root.activeBorrows > 0 || root.activeMutBorrows > 0 {
		return errorf("borrow error: field `%s.%s` cannot be assigned while value is borrowed",
			root.name, field)
	}
	if overlappingFieldCount(root.fieldBorrows, field) > 0 ||
		overlappingFieldCount(root.fieldMutBorrows, field) > 0 {
		return errorf("borrow error: field `%s.%s` cannot be assigned while borrowed",
			root.name, field)
	}
	return nil
}

// borrowedFieldRoot returns the borrowed identifier at the root of a field chain.
func (c *Checker) borrowedFieldRoot(expr ast.Expression, env *scope) (string, bool) {
	switch e := expr.(type) {
	case *ast.FieldExpr:
		return c.borrowedFieldRoot(e.Receiver, env)
	case *ast.DerefExpr:
		return c.borrowedFieldRoot(e.Receiver, env)
	case *ast.IdentExpr:
		value, ok := env.lookup(e.Name)
		return e.Name, ok && value.borrowedParam
	default:
		return "", false
	}
}

// readDerefExpr reads the value behind a local borrow or raw pointer.
func (c *Checker) readDerefExpr(expr *ast.DerefExpr, env *scope) (string, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		value, exists := env.lookup(ident.Name)
		if !exists {
			return "", errorAt(ident.Span, "move error: undefined variable `%s`", ident.Name)
		}
		if value.borrowedParam {
			return value.typeName, nil
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", err
	}
	if _, _, inner, ok := explicitOwnershipBorrowType(receiverType); ok {
		return inner, nil
	}
	if typeName, ok, err := c.rawPointerDerefExprType(expr, env); ok || err != nil {
		return typeName, err
	}
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		return "", errorAt(ident.Span,
			"borrow error: `%s` is not a borrow or raw pointer", ident.Name)
	}
	return "", errorAt(expr.OperatorSpan,
		"borrow error: dereference expects a local borrow or raw pointer")
}

// rawPointerDerefExprType returns the element type for unchecked pointer dereference.
func (c *Checker) rawPointerDerefExprType(
	expr *ast.DerefExpr,
	env *scope,
) (string, bool, error) {
	if ident, ok := expr.Receiver.(*ast.IdentExpr); ok {
		if value, exists := env.lookup(ident.Name); exists && value.borrowedParam {
			return "", false, nil
		}
	}
	receiverType, err := c.readExpr(expr.Receiver, env)
	if err != nil {
		return "", false, err
	}
	elem, ok := rawPointerElement(receiverType)
	if !ok {
		return "", false, nil
	}
	if strings.HasPrefix(receiverType, "?") {
		return "", true, errorf(
			"borrow error: nullable raw pointer `%s` cannot be dereferenced",
			receiverType,
		)
	}
	return strings.TrimPrefix(elem, "const "), true, nil
}

// assignmentRoot finds the binding invalidated by an assignment target.
func assignmentRoot(expr ast.Expression, env *scope) (*binding, bool) {
	switch target := expr.(type) {
	case *ast.IdentExpr:
		return env.lookup(target.Name)
	case *ast.FieldExpr:
		return assignmentRoot(target.Receiver, env)
	case *ast.DerefExpr:
		return assignmentRoot(target.Receiver, env)
	default:
		return nil, false
	}
}

// directAssignmentRoot returns a binding only for whole-binding assignment.
func directAssignmentRoot(expr ast.Expression, env *scope) (*binding, bool) {
	// `n.*` through a `&var` parameter names the same storage `n` does, so
	// both spellings take the direct-assignment path and its rules.
	if deref, ok := expr.(*ast.DerefExpr); ok {
		ident, ok := deref.Receiver.(*ast.IdentExpr)
		if !ok {
			return nil, false
		}
		value, found := env.lookup(ident.Name)
		if !found || !value.isMutBorrowParam() {
			return nil, false
		}
		return value, true
	}
	ident, ok := expr.(*ast.IdentExpr)
	if !ok {
		return nil, false
	}
	return env.lookup(ident.Name)
}

// checkUnionConstructor validates ownership effects of Union.Variant(payload).
func (c *Checker) checkUnionConstructor(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	if !field.Namespace {
		if ident, ok := field.Receiver.(*ast.IdentExpr); ok {
			if _, exists := c.enums[ident.Name]; exists {
				return "", true, errorf("move error: enum tag `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
			if _, exists := c.unions[ident.Name]; exists {
				return "", true, errorf("move error: union variant `%s.%s` must use `::`",
					ident.Name, field.Name)
			}
		}
		return "", false, nil
	}
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", false, nil
	}
	variants := c.unions[ident.Name]
	if variants == nil {
		return "", false, nil
	}
	payload, exists := variants[field.Name]
	if !exists {
		return "", true, errorf("move error: unknown union variant `%s::%s`",
			ident.Name, field.Name)
	}
	if payload == "" {
		return "", true, errorf("move error: union variant `%s::%s` expects 0 args",
			ident.Name, field.Name)
	}
	if len(args) != 1 {
		return "", true, errorf("move error: union variant `%s::%s` expects 1 arg, got %d",
			ident.Name, field.Name, len(args))
	}
	if _, err := c.moveExpr(args[0], env); err != nil {
		return "", true, err
	}
	return ident.Name, true, nil
}

// checkMethodCallExpr validates ownership effects of value receiver methods.
func (c *Checker) checkMethodCallExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := c.checkConsumingReceiverOwned(field, env); err != nil {
		return "", err
	}
	if typ, ok, err := c.checkArenaAtReceiverMethod(field, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkDirectFieldReceiverMethod(field, args, env); ok || err != nil {
		return typ, err
	}
	if typ, ok, err := c.checkBoxReceiverExpr(field, args, env); ok || err != nil {
		return typ, err
	}
	return c.checkLocalReceiverMethod(field, args, env)
}

// checkConsumingReceiverOwned rejects a method that takes its receiver over
// when the receiver is a borrow: the lender still holds the value, and the
// method's own release would run on it. Which methods consume is read from
// their signatures (functionConsumesReceiver), for user types and std alike.
func (c *Checker) checkConsumingReceiverOwned(field *ast.FieldExpr, env *scope) error {
	ident, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	value, exists := env.lookup(ident.Name)
	if !exists || !(value.borrowedParam || value.localBorrow) || value.ownsTied {
		return nil
	}
	receiverType := value.typeName
	if base, _, ok := splitGenericType(receiverType); ok {
		receiverType = base
	}
	method := c.implMethod(receiverType, field.Name)
	if method == nil || !functionConsumesReceiver(method) {
		return nil
	}
	label := strings.TrimSuffix(releaseLabel(value.typeName), "."+typ.CleanupMethod)
	return errorAt(field.Span,
		"move error: `%s.%s` requires owned %s receiver; `%s` is a borrow",
		label, field.Name, label, value.name)
}

// checkArenaAtReceiverMethod lets an immediate shared Arena.at result receive
// the same read-only methods as a bound &T. The arena source remains borrowed
// across argument checking and the call itself.
func (c *Checker) checkArenaAtReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	source, elem, ok, err := c.arenaAtBorrowSource(field.Receiver, env)
	if err != nil || !ok {
		return "", ok, err
	}
	if err := checkBorrowConflictForField(source.target, source.field, false); err != nil {
		return "", true, err
	}
	c.activateBorrow(source.target, source.field, false)
	defer releaseTemporaryBorrow(temporaryBorrow{
		value: source.target, field: source.field,
	})
	receiver := c.newBinding(field.Receiver.String(), elem)
	receiver.borrowedParam = true
	receiver.localBorrow = true
	result, err := c.checkDirectFieldReceiverByType(receiver, field.Name, args, env)
	return result, true, err
}

// checkLocalReceiverMethod validates methods whose receiver must be a local binding.
func (c *Checker) checkLocalReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, error) {
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if !ok {
		return "", errorAt(field.Span, "arena error: arena method receiver must be a local binding")
	}
	arena, exists := env.lookup(receiver.Name)
	if !exists {
		return "", errorAt(receiver.Span, "arena error: undefined arena `%s`", receiver.Name)
	}
	if arena.deinitialized {
		return "", errorAt(receiver.Span, "arena error: arena `%s` was deinitialized",
			receiver.Name)
	}
	if arena.moved {
		return "", errorAt(receiver.Span, "move error: moved value `%s` was used", receiver.Name)
	}
	base, elem, ok := splitGenericType(arena.typeName)
	if ok && base == "std::array::Array" {
		return c.checkArrayMethod(arena, elem, field.Name, args, env)
	}
	if ok && base == "std::map::Map" {
		if err := checkMapReceiverBorrow(arena, field.Name); err != nil {
			return "", err
		}
		return c.checkMapMethod(arena, elem, field.Name, args, env)
	}
	if !ok || base != "std::arena::Arena" {
		return c.checkNonArenaMethod(arena, field.Name, args, env)
	}
	return c.checkArenaMethod(arena, field.Name, args, env)
}

// checkArenaMethod dispatches methods on a local arena binding.
func (c *Checker) checkArenaMethod(
	arena *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := checkContainerMethodAccess("std::arena::Arena", arena, name); err != nil {
		return "", err
	}
	switch name {
	case "add":
		return c.checkArenaAdd(arena, args, env)
	case "at":
		return c.checkArenaAt(arena, args, env)
	case "at_mut":
		return c.checkArenaAtCondition(arena, args, env)
	case "deinit":
		return c.checkArenaDeinit(arena, args, env)
	default:
		// Unreachable while this switch and the shared access table agree;
		// the table refusal above is the user-facing one.
		return "", errorf("arena error: method `%s` is classified but unhandled", name)
	}
}

// checkArenaAtCondition checks at_mut inside a capture condition — a mutable
// binding and one handle whose provenance matches the arena — and refuses it
// everywhere else. Provenance is the guarantee here: a handle that passes it
// can only go absent if the checker itself is wrong, so the capture's else
// branch is a residue, not a normal path.
func (c *Checker) checkArenaAtCondition(
	arena *binding,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if !c.captureCondition && !c.borrowReturn {
		return "", errorf("arena error: `Arena.at_mut` must be consumed by a capture" +
			" (`if a.at_mut(handle) |name|` or `while a.at_mut(handle) |name|`)")
	}
	if !mutablePlace(arena) {
		return "", errorf("arena error: `Arena.at_mut` requires mutable arena binding")
	}
	// The context covers exactly this call: both flags off while the
	// argument is read — a nested at/at_mut in argument position refuses as
	// usual — and back for the result the call gate reads.
	savedCapture, savedReturn := c.captureCondition, c.borrowReturn
	c.captureCondition, c.borrowReturn = false, false
	elem, err := c.checkArenaHandleArg(arena, args, env, "Arena.at_mut")
	c.captureCondition, c.borrowReturn = savedCapture, savedReturn
	if err != nil {
		return "", err
	}
	return "?&var " + elem, nil
}

// checkNonArenaMethod validates methods on non-arena owned values.
func (c *Checker) checkNonArenaMethod(
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if value.typeName == "std::string::String" {
		if err := checkStringReceiverBorrow(value, name); err != nil {
			return "", err
		}
		return c.checkStringMethod(value, name, args, env)
	}
	if base, _, ok := splitGenericType(value.typeName); ok && base == "std::mem::Box" {
		return c.checkBoxMethod(value, name, args, env)
	}
	if err := checkMapReceiverBorrow(value, name); err != nil {
		return "", err
	}
	if typ, ok, err := c.checkImplMethodCall(value, name, args, env); ok || err != nil {
		return typ, err
	}
	return c.checkPlainMethodArgs(args, env)
}

// checkDirectFieldReceiverMethod validates owner.field.method(...) calls.
func (c *Checker) checkDirectFieldReceiverMethod(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	receiverExpr, ok := field.Receiver.(*ast.FieldExpr)
	if !ok {
		return "", false, nil
	}
	receiver, err := c.directFieldReceiver(receiverExpr, env)
	if err != nil {
		return "", true, err
	}
	if base, _, ok := splitGenericType(receiver.typeName); ok &&
		base == "std::mem::Box" && field.Name == "take" {
		return "", true, errorf("box error: `Box.take` requires local Box receiver")
	}
	if field.Name == typ.CleanupMethod {
		// Destructive cleanup stays on one direct field: a nested path would
		// bypass the intermediate type's own deinit (ADR-0067).
		if strings.Contains(receiver.field, ".") {
			return "", true, errorf(
				"move error: field cleanup `%s.%s` is only allowed on one direct field",
				receiver.path, field.Name,
			)
		}
		if !c.allowsDirectFieldCleanup(receiver) {
			return "", true, errorf(
				"move error: field cleanup `%s.%s` is only allowed inside owner deinit",
				receiver.path, field.Name,
			)
		}
	}
	value := c.bindingForDirectFieldReceiver(receiver)
	result, err := c.checkDirectFieldReceiverByType(value, field.Name, args, env)
	if err != nil {
		return "", true, err
	}
	if field.Name == typ.CleanupMethod {
		receiver.owner.markFieldDeinit(receiver.field)
	}
	return result, true, nil
}

// directFieldReceiver resolves the field path used as a method receiver: the
// root binding and the dotted path of the field the method runs on.
func (c *Checker) directFieldReceiver(
	field *ast.FieldExpr,
	env *scope,
) (*directFieldReceiver, error) {
	ownerIdent, fieldPath, ok := ast.FieldPathRoot(field)
	if !ok {
		return nil, errorAt(field.Span,
			"move error: field method receiver must be a field path on a local binding")
	}
	owner, exists := env.lookup(ownerIdent.Name)
	if !exists {
		return nil, errorAt(ownerIdent.Span,
			"move error: undefined variable `%s`", ownerIdent.Name)
	}
	if err := checkDeinitializedUse(ownerIdent.Name, owner, env, ownerIdent.Span); err != nil {
		return nil, err
	}
	if owner.moved {
		return nil, errorAt(ownerIdent.Span,
			"move error: moved value `%s` was used", ownerIdent.Name)
	}
	typeName, err := c.readFieldExpr(field, env)
	if err != nil {
		return nil, err
	}
	return &directFieldReceiver{
		owner: owner, field: fieldPath, typeName: typeName,
		path: ownerIdent.Name + "." + fieldPath,
	}, nil
}

// bindingForDirectFieldReceiver projects owner borrow state onto one owned
// field path. Whole-value borrows and borrows of any aliasing path count;
// borrows of disjoint sibling fields do not.
func (c *Checker) bindingForDirectFieldReceiver(receiver *directFieldReceiver) *binding {
	value := &binding{
		name: receiver.path, typeName: receiver.typeName,
		// A field of a mutable place is itself a mutable place.
		mutable:       mutablePlace(receiver.owner),
		borrowedParam: receiver.owner.borrowedParam,
		mutBorrow:     receiver.owner.mutBorrow,
		activeBorrows: receiver.owner.activeBorrows +
			overlappingFieldCount(receiver.owner.fieldBorrows, receiver.field),
		activeMutBorrows: receiver.owner.activeMutBorrows +
			overlappingFieldCount(receiver.owner.fieldMutBorrows, receiver.field),
		fieldOwner:     receiver.owner,
		fieldOwnerName: receiver.field,
	}
	base, _, ok := splitGenericType(receiver.typeName)
	if ok && base == "std::arena::Arena" {
		value.arenaID = c.directFieldArenaID(receiver)
	}
	return value
}

// checkDirectFieldReceiverByType dispatches a direct field receiver by its field type.
func (c *Checker) checkDirectFieldReceiverByType(
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if value.typeName == "std::string::String" {
		return c.checkStringMethod(value, name, args, env)
	}
	base, elem, ok := splitGenericType(value.typeName)
	if ok && base == "std::array::Array" {
		return c.checkArrayMethod(value, elem, name, args, env)
	}
	if ok && base == "std::map::Map" {
		return c.checkMapMethod(value, elem, name, args, env)
	}
	if ok && base == "std::mem::Box" {
		return c.checkBoxMethod(value, name, args, env)
	}
	if ok && base == "std::arena::Arena" {
		return c.checkFieldArenaMethod(value, name, args, env)
	}
	return c.checkNonArenaMethod(value, name, args, env)
}

// checkFieldArenaMethod validates direct field calls on owned arena fields.
func (c *Checker) checkFieldArenaMethod(
	arena *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := checkContainerMethodAccess("std::arena::Arena", arena, name); err != nil {
		return "", err
	}
	switch name {
	case "add":
		return c.checkArenaAdd(arena, args, env)
	case "at":
		return c.checkFieldArenaAt(arena, args, env)
	case "at_mut":
		// An owned arena field backs an at_mut capture like a local arena
		// does; handle provenance stays strict.
		return c.checkArenaAtCondition(arena, args, env)
	case "deinit":
		return c.checkArenaDeinit(arena, args, env)
	default:
		// Unreachable while this switch and the shared access table agree;
		// the table refusal above is the user-facing one.
		return "", errorf("arena error: method `%s` is classified but unhandled", name)
	}
}

// checkFieldArenaAt permits typed wrapper methods to unwrap their own arena handles.
func (c *Checker) checkFieldArenaAt(
	arena *binding,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("arena error: `Arena.at` expects 1 arg, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "std::arena::Arena" {
		return "", errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.checkKnownHandleProvenance(arena, args[0], env); err != nil {
		return "", err
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return "&" + arg, nil
}

// checkBoxReceiverExpr validates methods on local Box values and direct Box fields.
func (c *Checker) checkBoxReceiverExpr(
	field *ast.FieldExpr,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	typeName, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return "", false, nil
	}
	base, _, ok := splitGenericType(typeName)
	if !ok || base != "std::mem::Box" {
		return "", false, nil
	}
	target, borrowedField, err := c.borrowTarget(field.Receiver, env)
	if err != nil {
		return "", true, err
	}
	if (field.Name == "take" || field.Name == typ.CleanupMethod) && borrowedField != "" {
		return "", true, errorf("box error: `Box.%s` requires local Box receiver", field.Name)
	}
	typ, err := c.checkBoxMethodForTarget(target, borrowedField, field.Name, args, env)
	return typ, true, err
}

// checkBoxMethodForTarget validates Box methods with a tracked borrow root.
func (c *Checker) checkBoxMethodForTarget(
	target *binding,
	field string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "borrow", "borrow_mut":
		return "", errorf("box error: `Box.%s` must be bound with `let name = box.%s()`",
			name, name)
	case "take", "deinit":
		if target.hasAnyBorrow() {
			return "", errorf("box error: `Box.%s` cannot run while box is borrowed", name)
		}
		if err := c.readReleaseAllocator("Box."+name, target, args, env); err != nil {
			return "", err
		}
		if field == "" {
			target.moved = true
		}
		if name == "take" {
			_, elem, _ := splitGenericType(target.typeName)
			return elem, nil
		}
		return "void", nil
	default:
		return "", errorf("box error: Box has no method `%s`", name)
	}
}

// checkBoxMethod validates methods on a local Box binding.
func (c *Checker) checkBoxMethod(
	box *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	return c.checkBoxMethodForTarget(box, "", name, args, env)
}

// containerAccess classifies what one container method does to the storage a
// live borrow may alias: reads wait for mutable borrows, mutations and
// cleanup wait for any borrow, capture accessors are consumed by the capture
// recognizer, and view producers are guarded where their binding forms.
type containerAccess int

const (
	accessRead containerAccess = iota
	accessMutate
	accessCleanup
	accessCapture
	accessView
)

// containerAccessTable names one container's methods and how each touches
// storage. kind is the error prefix, label the method spelling.
type containerAccessTable struct {
	kind    string
	label   string
	methods map[string]containerAccess
}

// containerAccessTables is the one classification the container dispatchers
// share. A method missing here is refused at dispatch, so a new method has to
// name what it does to storage before any per-method checker sees it. Box is
// not here: its borrows are per-field and its conflicts are checked where the
// field borrow forms.
var containerAccessTables = map[string]containerAccessTable{
	"std::array::Array": {kind: "array", label: "Array", methods: map[string]containerAccess{
		"append": accessMutate, "append_bytes": accessMutate, "reserve": accessMutate,
		"set":  accessMutate,
		"swap": accessMutate,
		"pop":  accessMutate, "pop_or_panic": accessMutate,
		"truncate": accessMutate, "clear": accessMutate,
		"len": accessRead, "capacity": accessRead,
		"get": accessRead, "get_or_panic": accessRead, "clone": accessRead,
		// Unlike String's, Array's as_bytes/as_mut_bytes are std-internal
		// calls guarded here as reads; String's form view bindings and are
		// guarded where the binding forms.
		"as_bytes": accessRead, "as_mut_bytes": accessRead,
		"at": accessCapture, "at_mut": accessCapture,
		"deinit": accessCleanup,
	}},
	"std::map::Map": {kind: "map", label: "Map", methods: map[string]containerAccess{
		"insert": accessMutate,
		"get":    accessRead, "key_at": accessRead, "contains": accessRead,
		"len": accessRead,
		"at":  accessCapture, "at_mut": accessCapture,
		"deinit": accessCleanup,
	}},
	"std::string::String": {kind: "string", label: "String", methods: map[string]containerAccess{
		"append_bytes": accessMutate, "append_byte": accessMutate,
		"append_string": accessMutate,
		"reserve":       accessMutate, "truncate": accessMutate, "clear": accessMutate,
		"len": accessRead, "capacity": accessRead,
		"as_bytes": accessView, "as_mut_bytes": accessView,
		"deinit": accessCleanup,
	}},
	"std::arena::Arena": {kind: "arena", label: "Arena", methods: map[string]containerAccess{
		"add":    accessMutate,
		"at":     accessRead,
		"at_mut": accessCapture,
		"deinit": accessCleanup,
	}},
}

// checkContainerMethodAccess looks a container method up in the shared table
// and refuses it when a live borrow conflicts with what it does to storage.
// An unknown method is refused here: default deny, not fall-through.
func checkContainerMethodAccess(base string, value *binding, name string) error {
	table, ok := containerAccessTables[base]
	if !ok {
		return errorf("move error: `%s` has no container access table", base)
	}
	access, known := table.methods[name]
	if !known {
		return errorf("%s error: %s has no method `%s`", table.kind, table.label, name)
	}
	switch access {
	case accessRead:
		if value.activeMutBorrows > 0 {
			return errorf("%s error: `%s.%s` cannot read while mutably borrowed",
				table.kind, table.label, name)
		}
	case accessMutate, accessCleanup:
		if value.hasAnyBorrow() {
			return errorf("%s error: `%s.%s` cannot run while %s is borrowed",
				table.kind, table.label, name, table.kind)
		}
	}
	return nil
}

// checkStringReceiverBorrow rejects String methods whose receiver cannot be tracked safely.
func checkStringReceiverBorrow(value *binding, name string) error {
	if name == "deinit" && value.borrowedParam {
		return errorf("string error: `String.deinit` requires owned String receiver")
	}
	if isStringMutatingMethod(name) && value.borrowedParam && !value.mutBorrow {
		return errorf("string error: `String.%s` requires mutable String receiver", name)
	}
	return nil
}

// checkMapReceiverBorrow rejects Map methods whose receiver cannot be tracked safely.
func checkMapReceiverBorrow(value *binding, name string) error {
	base, _, ok := splitGenericType(value.typeName)
	if !ok || base != "std::map::Map" {
		return nil
	}
	if name == "deinit" && value.borrowedParam {
		return errorf("map error: `Map.deinit` requires owned Map receiver")
	}
	if name == "insert" && value.borrowedParam && !value.mutBorrow {
		return errorf("map error: `Map.insert` requires mutable Map receiver")
	}
	return nil
}

// checkStringMethod validates ownership effects for owned String methods.
func (c *Checker) checkStringMethod(
	str *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := checkContainerMethodAccess("std::string::String", str, name); err != nil {
		return "", err
	}
	switch name {
	case "append_bytes", "append_byte", "reserve", "truncate":
		return c.checkStringAppendOrReserve(str, name, args, env)
	case "append_string":
		if err := c.readStringGrowAllocator(str, name, args, env); err != nil {
			return "", err
		}
		return c.checkStringSourceArg(str, name, args[1:], env)
	case "len", "capacity":
		if err := checkStringNoArgs(name, args); err != nil {
			return "", err
		}
		return "i64", nil
	case "as_bytes", "as_mut_bytes":
		return "", errorf(
			"string error: `String.%s` must be bound with `let name = string.%s()`", name, name)
	case "clear":
		if err := checkStringNoArgs(name, args); err != nil {
			return "", err
		}
		return "void", nil
	case "deinit":
		if err := c.readReleaseAllocator("String.deinit", str, args, env); err != nil {
			return "", err
		}
		str.moved = true
		return "void", nil
	default:
		// Unreachable while this switch and the shared access table agree;
		// the table refusal above is the user-facing one.
		return "", errorf("string error: method `%s` is classified but unhandled", name)
	}
}

// checkStringAppendOrReserve validates the String mutators that take a value.
// A growth names the allocator its storage comes from first: a String keeps
// none of its own (ADR-0132). `truncate` neither grows nor frees.
func (c *Checker) checkStringAppendOrReserve(
	str *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	rest := args
	if stringGrowMethod(name) {
		if err := c.readStringGrowAllocator(str, name, args, env); err != nil {
			return "", err
		}
		rest = args[1:]
	}
	switch name {
	case "append_bytes":
		return c.checkStringBytesArg(name, rest, env)
	case "append_byte":
		return c.checkStringByteArg(name, rest, env)
	default:
		return c.checkStringReserveArg(name, rest, env)
	}
}

// stringGrowMethod reports the String methods that may ask for storage.
func stringGrowMethod(name string) bool {
	switch name {
	case "append_bytes", "append_byte", "append_string", "reserve":
		return true
	default:
		return false
	}
}

// readStringGrowAllocator reads the leading Allocator a growth names.
func (c *Checker) readStringGrowAllocator(
	str *binding,
	name string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 2 {
		return errorf("string error: `String.%s` expects 2 args, got %d", name, len(args))
	}
	return c.readGrowAllocator("string", "String."+name, str, args[0], env)
}

// checkStringNoArgs validates no-argument String methods.
func checkStringNoArgs(name string, args []ast.Expression) error {
	if len(args) != 0 {
		return errorf("string error: `String.%s` expects 0 args, got %d", name, len(args))
	}
	return nil
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

// checkStringBytesArg validates append_bytes without moving the source slice.
func (c *Checker) checkStringBytesArg(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(got, "[]u8") {
		return "", errorf("string error: `String.%s` expects []u8, got %s", name, got)
	}
	return "std::mem::Error!void", nil
}

// checkStringSourceArg validates append_string reading its source String
// without moving it. The receiver is the call's `&var` argument and the
// source its `&` argument, so the two are activated in that order and collide
// exactly as they would on a direct call.
func (c *Checker) checkStringSourceArg(
	str *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	if err := checkBorrowConflictForField(str, "", true); err != nil {
		return "", err
	}
	c.activateBorrow(str, "", true)
	defer releaseTemporaryBorrows([]temporaryBorrow{{value: str, field: "", mutable: true}})
	source, field, err := c.callBorrowTarget(args[0], env)
	if err != nil {
		return "", err
	}
	if source != nil {
		if err := checkBorrowConflictForField(source, field, false); err != nil {
			return "", err
		}
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(got, "std::string::String") {
		return "", errorf("string error: `String.%s` expects String, got %s", name, got)
	}
	return "std::mem::Error!void", nil
}

// checkStringReserveArg validates reserve without moving the count.
func (c *Checker) checkStringReserveArg(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", errorf("string error: `String.%s` expects i64, got %s", name, got)
	}
	// reserve can be refused a size or run out of memory; truncate only
	// refuses a bad length.
	if name == "truncate" {
		return "std::string::Error!void", nil
	}
	return "std::string::GrowError!void", nil
}

// checkStringByteArg validates append_byte without moving the source value.
func (c *Checker) checkStringByteArg(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("string error: `String.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readContextualExpr(args[0], "u8", env)
	if err != nil {
		return "", err
	}
	if got != "u8" {
		return "", errorf("string error: `String.%s` expects u8, got %s", name, got)
	}
	return "std::mem::Error!void", nil
}

// checkArrayMethod validates ownership effects for owned Array<T> methods.
func (c *Checker) checkArrayMethod(
	array *binding,
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if isStdArrayStorageMethod(name) && !c.currentStd {
		return "", errorf("array error: Array has no method `%s`", name)
	}
	if err := checkContainerMethodAccess("std::array::Array", array, name); err != nil {
		return "", err
	}
	if isStdArrayStorageMethod(name) {
		return c.checkStdArrayStorageMethod(elem, name, args, env)
	}
	if arrayGrowMethod(name) {
		return c.checkArrayGrowth(array, elem, name, args, env)
	}
	switch name {
	case "pop":
		return c.checkArrayPop(elem, name, args, true)
	case "pop_or_panic":
		return c.checkArrayPop(elem, name, args, false)
	case "len", "capacity":
		return c.checkArrayReadNoArgs(name, args)
	case "get", "get_or_panic", "clone":
		return c.checkArrayCopyMethod(elem, name, args, env)
	case "at", "at_mut":
		return c.checkArrayAtCondition(array, elem, name, args, env)
	case "set", "swap":
		return c.checkArrayIndexedMutation(array, elem, name, args, env)
	case "deinit":
		return c.checkArrayDeinit(array, name, args, env)
	default:
		// Unreachable while this switch and the shared access table agree;
		// the table refusal above is the user-facing one.
		return "", errorf("array error: method `%s` is classified but unhandled", name)
	}
}

// checkArrayDeinit consumes the array: releasing it is the last thing done
// with it, so the binding is moved rather than left readable.
func (c *Checker) checkArrayDeinit(
	array *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := c.readReleaseAllocator("Array."+name, array, args, env); err != nil {
		return "", err
	}
	array.moved = true
	return "void", nil
}

// checkArrayIndexedMutation validates set and owner-safe slot exchange.
func (c *Checker) checkArrayIndexedMutation(
	value *binding,
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if name == "set" {
		return c.checkArraySet(elem, args, env)
	}
	if value.borrowedParam && !value.mutBorrow {
		return "", errorf("array error: `Array.swap` requires mutable Array receiver")
	}
	return c.checkArraySwap(args, env)
}

// checkArraySwap validates two checked indexes without moving either element.
func (c *Checker) checkArraySwap(args []ast.Expression, env *scope) (string, error) {
	if len(args) != 2 {
		return "", errorf("array error: `Array.swap` expects 2 args, got %d", len(args))
	}
	for _, arg := range args {
		got, err := c.readExpr(arg, env)
		if err != nil {
			return "", err
		}
		if got != "i64" {
			return "", errorf("array error: `Array.swap` expects i64 index, got %s", got)
		}
	}
	return "std::array::Error!void", nil
}

// checkArrayCopyMethod dispatches operations whose result duplicates copy
// elements without consuming the source Array.
func (c *Checker) checkArrayCopyMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if name == "clone" {
		return c.checkArrayClone(elem, args, env)
	}
	return c.checkArrayGet(elem, name, args, env)
}

// checkArrayClone validates the explicit allocator and limits clone to copy
// elements. Owner elements need per-type deep-copy logic (ADR-0124).
func (c *Checker) checkArrayClone(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("array error: `Array.clone` expects 1 arg, got %d", len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "Allocator" {
		return "", errorf("array error: `Array.clone` expects Allocator, got %s", got)
	}
	if !c.isCopyType(elem) {
		return "", errorf("array error: `Array.clone` requires copy element")
	}
	return "std::mem::Error!std::array::Array<" + elem + ">", nil
}

// checkStdArrayStorageMethod validates Array helpers reserved to std source.
func (c *Checker) checkStdArrayStorageMethod(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	switch name {
	case "truncate":
		return c.checkArrayCountMutation(name, args, env)
	case "clear":
		if len(args) != 0 {
			return "", errorf("array error: `Array.clear` expects 0 args, got %d", len(args))
		}
		return "void", nil
	default:
		if elem != "u8" {
			return "", errorf("array error: `Array.%s` requires Array<u8>", name)
		}
		return c.checkArrayReadNoArgs(name, args)
	}
}

// checkArrayAppendBytes validates the run copied by Array.append_bytes. Only a
// u8 array has elements a byte run spells, and the source is read, not moved:
// it is a view.
func (c *Checker) checkArrayAppendBytes(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if elem != "u8" {
		return "", errorf("array error: `Array.append_bytes` requires Array<u8>")
	}
	if len(args) != 1 {
		return "", errorf("array error: `Array.append_bytes` expects 1 arg, got %d", len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "[]u8" {
		return "", errorf("array error: `Array.append_bytes` expects []u8, got %s", got)
	}
	return "std::mem::Error!void", nil
}

// isStdArrayStorageMethod reports methods reserved for std-owned storage wrappers.
func isStdArrayStorageMethod(name string) bool {
	return name == "truncate" || name == "clear" ||
		name == "as_bytes" || name == "as_mut_bytes"
}

// checkArrayCountMutation validates one-count Array mutations.
func (c *Checker) checkArrayCountMutation(
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("array error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "i64" {
		return "", errorf("array error: `Array.%s` expects i64, got %s", name, got)
	}
	// reserve fails only for memory; truncate refuses a bad length.
	if name == "truncate" {
		return "std::array::Error!void", nil
	}
	return "std::mem::Error!void", nil
}

// arrayGrowMethod reports the Array methods that may ask for storage.
func arrayGrowMethod(name string) bool {
	switch name {
	case "append", "append_bytes", "reserve":
		return true
	default:
		return false
	}
}

// checkArrayGrowth validates one storage-asking Array method. A growth names
// the allocator its storage comes from first: an Array header keeps none of
// its own (ADR-0132).
func (c *Checker) checkArrayGrowth(
	array *binding,
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := c.readArrayGrowAllocator(array, name, args, env); err != nil {
		return "", err
	}
	switch name {
	case "append":
		return c.checkArrayAppend(elem, args[1:], env)
	case "append_bytes":
		return c.checkArrayAppendBytes(elem, args[1:], env)
	default:
		return c.checkArrayCountMutation(name, args[1:], env)
	}
}

// readArrayGrowAllocator reads the leading Allocator a growth names.
func (c *Checker) readArrayGrowAllocator(
	array *binding,
	name string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 2 {
		return errorf("array error: `Array.%s` expects 2 args, got %d", name, len(args))
	}
	return c.readGrowAllocator("array", "Array."+name, array, args[0], env)
}

// checkArrayAppend validates append mutation and element move.
func (c *Checker) checkArrayAppend(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("array error: `Array.append` expects 1 arg, got %d", len(args))
	}
	got, err := c.moveContextualExpr(args[0], elem, env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", errorf("array error: `Array.append` expects %s, got %s", elem, got)
	}
	return "std::mem::Error!void", nil
}

// checkArrayPop validates moving one initialized element out of an Array.
func (c *Checker) checkArrayPop(
	elem string,
	name string,
	args []ast.Expression,
	returnsOptional bool,
) (string, error) {
	if len(args) != 0 {
		return "", errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
	}
	if !returnsOptional {
		return elem, nil
	}
	return "?" + elem, nil
}

// mutablePlace reports whether a binding is a place a mutable borrow may come
// from: a `var` local or a `&var` borrow.
func mutablePlace(value *binding) bool {
	return value.mutable || value.mutBorrow
}

// checkArrayAtCondition checks at/at_mut inside a capture condition — a
// mutable binding for at_mut and one i64 index — and refuses them everywhere
// else: the borrow optional they produce exists only there.
func (c *Checker) checkArrayAtCondition(
	array *binding,
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if !c.captureCondition && !c.borrowReturn {
		return "", errorf("array error: `Array.%s` must be consumed by a capture"+
			" (`if array.%s(...) |name|` or `while array.%s(...) |name|`)",
			name, name, name)
	}
	if name == "at_mut" && !mutablePlace(array) {
		return "", errorf("array error: `Array.at_mut` requires mutable array binding")
	}
	if len(args) != 1 {
		return "", errorf("array error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	// The context covers exactly this call: both flags off while the
	// argument is read — a nested at/at_mut in argument position refuses as
	// usual — and back for the result the call gate reads.
	got, err := c.readOutsideBorrowContext(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", errorf("array error: `Array.%s` expects i64 index, got %s", name, got)
	}
	if name == "at_mut" {
		return "?&var " + elem, nil
	}
	return "?&" + elem, nil
}

// readOutsideBorrowContext reads one expression with the borrow-optional
// contexts off, so producers nested inside it refuse as usual.
func (c *Checker) readOutsideBorrowContext(expr ast.Expression, env *scope) (string, error) {
	savedCapture, savedReturn := c.captureCondition, c.borrowReturn
	c.captureCondition, c.borrowReturn = false, false
	result, err := c.readExpr(expr, env)
	c.captureCondition, c.borrowReturn = savedCapture, savedReturn
	return result, err
}

// checkArrayReadNoArgs validates len/capacity reads.
func (c *Checker) checkArrayReadNoArgs(
	name string,
	args []ast.Expression,
) (string, error) {
	if len(args) != 0 {
		return "", errorf("array error: `Array.%s` expects 0 args, got %d", name, len(args))
	}
	return "i64", nil
}

// checkArraySet validates checked element replacement.
func (c *Checker) checkArraySet(
	elem string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 2 {
		return "", errorf("array error: `Array.set` expects 2 args, got %d", len(args))
	}
	if got, err := c.readExpr(args[0], env); err != nil {
		return "", err
	} else if got != "i64" {
		return "", errorf("array error: `Array.set` expects i64 index, got %s", got)
	}
	got, err := c.moveContextualExpr(args[1], elem, env)
	if err != nil {
		return "", err
	}
	if got != elem {
		return "", errorf("array error: `Array.set` expects %s value, got %s", elem, got)
	}
	return "std::array::Error!void", nil
}

// checkArrayGet validates copy-only Array<T> reads in the prototype.
func (c *Checker) checkArrayGet(
	elem string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("array error: `Array.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return "", err
	}
	if got != "i64" {
		return "", errorf("array error: `Array.%s` expects i64 index, got %s", name, got)
	}
	if !c.isCopyType(elem) {
		return "", errorf("array error: `Array.%s` requires copy element", name)
	}
	if name == "get" {
		return "?" + elem, nil
	}
	return elem, nil
}

// checkMapMethod validates ownership effects for owned Map<K, V> methods.
func (c *Checker) checkMapMethod(
	mapValue *binding,
	argsText string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	mapArgs, err := c.checkedMapArgs(argsText)
	if err != nil {
		return "", err
	}
	keyType, valueType := mapArgs[0], mapArgs[1]
	if err := checkContainerMethodAccess("std::map::Map", mapValue, name); err != nil {
		return "", err
	}
	switch name {
	case "insert":
		return c.checkMapInsert(mapValue, keyType, valueType, args, env)
	case "get":
		if err := c.checkMapKeyArg(keyType, name, args, env); err != nil {
			return "", err
		}
		if !c.isCopyType(valueType) {
			return "", errorf("map error: `Map.get` requires copy value")
		}
		return "?" + valueType, nil
	case "at", "at_mut":
		return c.checkMapAtCondition(mapValue, keyType, valueType, name, args, env)
	case "key_at":
		if err := c.checkMapIndexArg(name, args, env); err != nil {
			return "", err
		}
		return "?" + keyType, nil
	case "contains":
		if err := c.checkMapKeyArg(keyType, name, args, env); err != nil {
			return "", err
		}
		return "bool", nil
	case "len":
		return c.checkMapReadNoArgs(name, args)
	case "deinit":
		return c.checkMapDeinit(mapValue, args, env)
	default:
		// Unreachable while this switch and the shared access table agree;
		// the table refusal above is the user-facing one.
		return "", errorf("map error: method `%s` is classified but unhandled", name)
	}
}

// checkMapAtCondition checks at/at_mut inside a capture condition — a
// mutable binding for at_mut and one key — and refuses them everywhere
// else: the borrow optional they produce exists only there.
func (c *Checker) checkMapAtCondition(
	mapValue *binding,
	keyType string,
	valueType string,
	name string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if !c.captureCondition && !c.borrowReturn {
		return "", errorf("map error: `Map.%s` must be consumed by a capture"+
			" (`if m.%s(key) |name|` or `while m.%s(key) |name|`)",
			name, name, name)
	}
	if name == "at_mut" && !mutablePlace(mapValue) {
		return "", errorf("map error: `Map.at_mut` requires mutable map binding")
	}
	// The context covers exactly this call: both flags off while the
	// argument is read — a nested at/at_mut in argument position refuses as
	// usual — and back for the result the call gate reads.
	savedCapture, savedReturn := c.captureCondition, c.borrowReturn
	c.captureCondition, c.borrowReturn = false, false
	err := c.checkMapKeyArg(keyType, name, args, env)
	c.captureCondition, c.borrowReturn = savedCapture, savedReturn
	if err != nil {
		return "", err
	}
	if name == "at_mut" {
		return "?&var " + valueType, nil
	}
	return "?&" + valueType, nil
}

// checkMapInsert validates Map.insert(allocator, key, value). The insert is
// the call that buys storage, so it names the allocator it buys from, the same
// way an Array append does: the header keeps none of its own (ADR-0131).
func (c *Checker) checkMapInsert(
	mapValue *binding,
	keyType string,
	valueType string,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if len(args) != 3 {
		return "", errorf("map error: `Map.insert` expects 3 args, got %d", len(args))
	}
	if err := c.readGrowAllocator("map", "Map.insert", mapValue, args[0], env); err != nil {
		return "", err
	}
	if got, err := c.readContextualExpr(args[1], keyType, env); err != nil {
		return "", err
	} else if !sameOwnershipType(got, keyType) {
		return "", errorf("map error: `Map.insert` expects %s key, got %s", keyType, got)
	}
	got, err := c.moveContextualExpr(args[2], valueType, env)
	if err != nil {
		return "", err
	}
	if !sameOwnershipType(got, valueType) {
		return "", errorf("map error: `Map.insert` expects %s value, got %s", valueType, got)
	}
	return "std::mem::Error!void", nil
}

// readGrowAllocator reads the leading Allocator a storage-asking container
// method names, and requires it to be the one the container was built from.
// The storage a growth buys is released by the container's `deinit`, which
// names one allocator for all of it: a growth that named another would leave
// the release freeing bytes it never handed out (ADR-0132).
func (c *Checker) readGrowAllocator(
	kind string,
	what string,
	receiver *binding,
	arg ast.Expression,
	env *scope,
) error {
	got, err := c.readExpr(arg, env)
	if err != nil {
		return err
	}
	if got != "Allocator" {
		return errorf("%s error: `%s` expects Allocator, got %s", kind, what, got)
	}
	return c.checkReleaseTie(what, receiver, arg, env)
}

// checkMapIndexArg validates one i64 insertion-position argument.
func (c *Checker) checkMapIndexArg(
	name string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 1 {
		return errorf("map error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readExpr(args[0], env)
	if err != nil {
		return err
	}
	if !sameOwnershipType(got, "i64") {
		return errorf("map error: `Map.%s` expects i64 index, got %s", name, got)
	}
	return nil
}

// checkMapKeyArg validates one lookup key against the map's key type.
func (c *Checker) checkMapKeyArg(
	keyType string,
	name string,
	args []ast.Expression,
	env *scope,
) error {
	if len(args) != 1 {
		return errorf("map error: `Map.%s` expects 1 arg, got %d", name, len(args))
	}
	got, err := c.readContextualExpr(args[0], keyType, env)
	if err != nil {
		return err
	}
	if !sameOwnershipType(got, keyType) {
		return errorf("map error: `Map.%s` expects %s key, got %s", name, keyType, got)
	}
	return nil
}

// checkMapReadNoArgs validates no-argument Map reads.
func (c *Checker) checkMapReadNoArgs(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", errorf("map error: `Map.%s` expects 0 args, got %d", name, len(args))
	}
	return "i64", nil
}

// checkMapDeinit validates owned Map cleanup and marks it moved.
func (c *Checker) checkMapDeinit(
	mapValue *binding,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := c.readReleaseAllocator("Map.deinit", mapValue, args, env); err != nil {
		return "", err
	}
	mapValue.moved = true
	return "void", nil
}

// checkOneI64Arg reads one i64 argument.
func (c *Checker) checkOneI64Arg(name string, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 1 {
		return "", errorf("parallel error: `%s` expects 1 arg, got %d", name, len(args))
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return "i64", nil
}

// checkPlainMethodArgs reads non-arena method arguments after type checking.
func (c *Checker) checkPlainMethodArgs(args []ast.Expression, env *scope) (string, error) {
	for _, arg := range args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "!unknown", nil
}

// checkImplMethodCall applies a concrete impl method signature to a receiver call.
func (c *Checker) checkImplMethodCall(
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) (string, bool, error) {
	method := c.implMethod(value.typeName, name)
	if method == nil {
		return "", false, nil
	}
	if len(method.params) == 0 {
		return "", true, errorf("move error: method `%s` must have self parameter",
			method.name)
	}
	if method.params[0].mutBorrow && value.hasAnyBorrow() {
		return "", true, errorf(
			"borrow error: method `%s` mutates its receiver while it is borrowed", method.name)
	}
	if len(args) != len(method.params)-1 {
		return "", true, errorf("move error: `%s` expects %d args, got %d",
			method.name, len(method.params)-1, len(args))
	}
	if err := c.checkImplMethodCallArgs(method, value, name, args, env); err != nil {
		return "", true, err
	}
	cleanup := name == typ.CleanupMethod && returnTypeName(method) == "void"
	if method.params[0].mutBorrow {
		// The exclusive receiver borrow is active from here through the call,
		// so the deinit path below sees the receiver as borrowed.
		value.activeMutBorrows++
		defer func() { value.activeMutBorrows-- }()
	}
	if cleanup {
		if value.hasAnyBorrow() {
			return "", true, errorf(
				"borrow error: value `%s` cannot be moved while borrowed", value.name)
		}
		// Inside the type's own deinit the fields are what the body consumes,
		// and consuming them is how it finishes: the receiver's own cleanup
		// is the body being written, so calling it is recursion, and after a
		// field is gone it would release that field twice.
		if c.insideDeinitOf(value) {
			return "", true, errorf(
				"move error: `deinit` calls itself on `%s`; release the fields instead",
				value.name)
		}
		if field, ok := partiallyConsumedField(value); ok {
			return "", true, errorf(
				"move error: field `%s.%s` is already consumed, so `%s.deinit()` "+
					"would release it twice", value.name, field, value.name)
		}
		value.moved = true
	}
	return returnTypeName(method), true, nil
}

// checkImplMethodCallArgs applies the ordinary argument effects and the
// allocator identity carried by a user-defined cleanup. Containers have
// dedicated method paths; Future.deinit reaches this common impl path.
func (c *Checker) checkImplMethodCallArgs(
	method *functionInfo,
	value *binding,
	name string,
	args []ast.Expression,
	env *scope,
) error {
	if err := c.checkImplMethodArgs(method, value, args, env); err != nil {
		return err
	}
	if name != typ.CleanupMethod || returnTypeName(method) != "void" ||
		len(args) != 1 || method.params[1].typeName != "Allocator" {
		return nil
	}
	return c.checkReleaseTie(releaseLabel(value.typeName), value, args[0], env)
}

// receiverPlace returns the binding and field the receiver's call-duration
// borrow lands on: the owner and its field for a direct-field projection, the
// receiver binding itself otherwise. Routing through the owner is what makes
// an argument borrow of the same field collide with the receiver.
func receiverPlace(receiver *binding) (*binding, string) {
	if receiver.fieldOwner != nil {
		return receiver.fieldOwner, receiver.fieldOwnerName
	}
	return receiver, ""
}

// checkImplMethodArgs applies ownership effects for explicit method arguments.
//
// A `&var` receiver is two-phase: while the arguments are evaluated it is only
// reserved — a shared borrow, so arguments can read the receiver — and the
// exclusive borrow activates once every argument has settled.
func (c *Checker) checkImplMethodArgs(
	method *functionInfo,
	receiver *binding,
	args []ast.Expression,
	env *scope,
) error {
	call := &functionInfo{
		name: method.name, sig: method.sig, params: method.params[1:], body: method.body,
	}
	if err := c.checkCallHandlePairing(call.params, nil, args, env); err != nil {
		return err
	}
	receiverMut := method.params[0].mutBorrow
	target, targetField := receiverPlace(receiver)
	if receiverMut {
		c.activateBorrow(target, targetField, false)
	}
	borrowed, err := c.activateBorrowArgs(call, args, env)
	if err == nil {
		for idx, arg := range args {
			if err = c.checkImplMethodArg(method, idx+1, arg, env); err != nil {
				break
			}
		}
	}
	if receiverMut {
		releaseTemporaryBorrow(temporaryBorrow{value: target, field: targetField})
		if err == nil {
			// Activation: argument borrows still live at the call must not
			// overlap the receiver's exclusive borrow.
			err = checkBorrowConflictForField(target, targetField, true)
		}
	}
	releaseTemporaryBorrows(borrowed)
	return err
}

// checkImplMethodArg mirrors user-call argument ownership for one method
// parameter. A view lends to a method under the same condition as to a plain
// function, with the receiver counted among the parameters that could retain
// it: a `&var self` whose type carries a view could store the lend in a field.
func (c *Checker) checkImplMethodArg(
	method *functionInfo,
	paramIndex int,
	arg ast.Expression,
	env *scope,
) error {
	param := method.params[paramIndex]
	if param.borrow {
		if param.mutBorrow {
			return nil
		}
		_, err := c.readExpr(arg, env)
		return err
	}
	if c.viewArgLend(method, paramIndex, arg, env) ||
		c.capabilityArgLend(method, paramIndex, arg, env) {
		_, err := c.readExpr(arg, env)
		return err
	}
	_, err := c.moveExpr(arg, env)
	return err
}

// checkArenaAdd moves one value into an arena and returns a handle.
// checkArenaAdd validates Arena.add(allocator, value). The add is the call
// that buys storage, so it names the allocator it buys from, the same way an
// Array append does: the header keeps none of its own (ADR-0131).
func (c *Checker) checkArenaAdd(arena *binding, args []ast.Expression, env *scope) (string, error) {
	if len(args) != 2 {
		return "", errorf("arena error: `Arena.add` expects 2 args, got %d", len(args))
	}
	base, arg, ok := splitGenericType(arena.typeName)
	if !ok || base != "std::arena::Arena" {
		return "", errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.readGrowAllocator("arena", "Arena.add", arena, args[0], env); err != nil {
		return "", err
	}
	got, err := c.moveContextualExpr(args[1], arg, env)
	if err != nil {
		return "", err
	}
	if got != arg {
		return "", errorf("arena error: `Arena.add` expects %s, got %s", arg, got)
	}
	return fmt.Sprintf("std::mem::Error!std::arena::Handle<%s>", arg), nil
}

// checkArenaAt reads a handle and returns a shared borrow tied to the arena.
func (c *Checker) checkArenaAt(arena *binding, args []ast.Expression, env *scope) (string, error) {
	elem, err := c.checkArenaHandleArg(arena, args, env, "Arena.at")
	if err != nil {
		return "", err
	}
	return "&" + elem, nil
}

// checkArenaHandleArg validates the one handle argument an arena accessor
// takes — count, provenance, and the read itself — and returns the element
// type behind the handle. label names the accessor in the count error.
func (c *Checker) checkArenaHandleArg(
	arena *binding,
	args []ast.Expression,
	env *scope,
	label string,
) (string, error) {
	if len(args) != 1 {
		return "", errorf("arena error: `%s` expects 1 arg, got %d", label, len(args))
	}
	base, elem, ok := splitGenericType(arena.typeName)
	if !ok || base != "std::arena::Arena" {
		return "", errorf("arena error: `%s` is not an arena", arena.name)
	}
	if err := c.checkKnownHandleProvenance(arena, args[0], env); err != nil {
		return "", err
	}
	if _, err := c.readExpr(args[0], env); err != nil {
		return "", err
	}
	return elem, nil
}

// checkArenaDeinit validates explicit arena cleanup and invalidates the binding.
func (c *Checker) checkArenaDeinit(
	arena *binding,
	args []ast.Expression,
	env *scope,
) (string, error) {
	if err := c.readReleaseAllocator("Arena.deinit", arena, args, env); err != nil {
		return "", err
	}
	arena.deinitialized = true
	arena.moved = true
	return "void", nil
}

// checkKnownHandleProvenance rejects handle uses whose provenance is known to
// mismatch: both sides carry an arena identity and they differ. An unknown
// side — an arena that arrived as a borrow, a handle read out of a field —
// passes, because the reader cannot see where it was made and the signature
// is what carries the contract (ADR-0098). This is the one provenance rule;
// local arenas always carry an identity, so known confusions still stop.
func (c *Checker) checkKnownHandleProvenance(
	arena *binding,
	expr ast.Expression,
	env *scope,
) error {
	if arena.arenaID == 0 {
		return nil
	}
	if addArena := c.arenaAddReceiver(expr, env); addArena != nil && addArena.arenaID != 0 {
		if addArena.arenaID != arena.arenaID {
			return errorf("arena error: handle from `%s` does not belong to arena `%s`",
				addArena.name, arena.name)
		}
		return nil
	}
	provenance := c.knownHandleProvenance(expr, env)
	if provenance != 0 && provenance != arena.arenaID {
		if ident, ok := expr.(*ast.IdentExpr); ok {
			return errorf("arena error: handle `%s` does not belong to arena `%s`",
				ident.Name, arena.name)
		}
		return errorf("arena error: handle expression does not belong to arena `%s`",
			arena.name)
	}
	return nil
}

// knownHandleProvenance returns tracked arena identity for handle-like expressions.
func (c *Checker) knownHandleProvenance(expr ast.Expression, env *scope) int {
	ident, ok := expr.(*ast.IdentExpr)
	if ok {
		value, exists := env.lookup(ident.Name)
		if exists {
			return value.handleArenaID
		}
	}
	return 0
}

// arenaHandlePair names the parameter indices of one derived contract: the
// handle argument must come from the arena argument.
type arenaHandlePair struct {
	arena  int
	handle int
}

// arenaHandlePairs derives handle-pairing contracts from a signature, the
// same division of labor borrow provenance uses (ADR-0098: the callee trusts
// the signature, the caller re-derives the contract from it). A pair exists
// when a borrowed `Arena<T>` parameter and a by-value `Handle<T>` parameter
// appear for the same T exactly once each; any other shape — repeated T, a
// by-value arena, a borrowed handle — is ambiguous and derives nothing.
func arenaHandlePairs(params []paramInfo, subst map[string]string) []arenaHandlePair {
	const ambiguous = -1
	note := func(m map[string]int, elem string, idx int, eligible bool) {
		if _, seen := m[elem]; seen || !eligible {
			m[elem] = ambiguous
			return
		}
		m[elem] = idx
	}
	resolved := make([]string, len(params))
	arenas := map[string]int{}
	handles := map[string]int{}
	for idx, param := range params {
		typeName := param.typeName
		if subst != nil {
			typeName = substituteOwnershipType(typeName, subst)
		}
		resolved[idx] = typeName
		base, elem, ok := splitGenericType(typeName)
		if !ok {
			continue
		}
		switch base {
		case "std::arena::Arena":
			note(arenas, elem, idx, param.borrow)
		case "std::arena::Handle":
			note(handles, elem, idx, !param.borrow)
		}
	}
	pairs := []arenaHandlePair{}
	for idx := range params {
		_, elem, ok := splitGenericType(resolved[idx])
		if !ok {
			continue
		}
		handleIdx, ok := handles[elem]
		if !ok || handleIdx != idx {
			continue
		}
		arenaIdx, ok := arenas[elem]
		if !ok || arenaIdx == ambiguous {
			continue
		}
		pairs = append(pairs, arenaHandlePair{arena: arenaIdx, handle: idx})
	}
	return pairs
}

// checkCallHandlePairing enforces derived arena/handle contracts at a call
// site where both origins are visible. An unknown side — the caller itself
// received the arena or handle — passes, and the contract chains to the next
// signature (ADR-0098).
func (c *Checker) checkCallHandlePairing(
	params []paramInfo,
	subst map[string]string,
	args []ast.Expression,
	env *scope,
) error {
	for _, pair := range arenaHandlePairs(params, subst) {
		arena := c.arenaArgBinding(args[pair.arena], env)
		if arena == nil || arena.arenaID == 0 {
			continue
		}
		if err := c.checkKnownHandleProvenance(arena, args[pair.handle], env); err != nil {
			return err
		}
	}
	return nil
}

// arenaArgBinding resolves the arena identity an argument lends, if visible:
// a bare or &-prefixed local, or one direct owner field. Anything else has no
// visible identity and resolves to nil.
func (c *Checker) arenaArgBinding(expr ast.Expression, env *scope) *binding {
	if prefix, ok := borrowPrefix(expr); ok {
		expr = prefix.Right
	}
	switch e := expr.(type) {
	case *ast.IdentExpr:
		value, ok := env.lookup(e.Name)
		if !ok {
			return nil
		}
		return value
	case *ast.FieldExpr:
		if e.Namespace {
			return nil
		}
		direct, err := c.directFieldReceiver(e, env)
		if err != nil {
			return nil
		}
		base, _, ok := splitGenericType(direct.typeName)
		if !ok || base != "std::arena::Arena" {
			return nil
		}
		return c.bindingForDirectFieldReceiver(direct)
	}
	return nil
}

// activateBorrowArgs marks identifier arguments that are borrowed for this call.
func (c *Checker) activateBorrowArgs(
	fn *functionInfo,
	args []ast.Expression,
	env *scope,
) ([]temporaryBorrow, error) {
	borrowed := []temporaryBorrow{}
	for idx, arg := range args {
		if !fn.params[idx].borrow {
			continue
		}
		value, field, err := c.callBorrowTarget(arg, env)
		if err != nil {
			releaseTemporaryBorrows(borrowed)
			return nil, err
		}
		if value == nil && fn.params[idx].mutBorrow {
			releaseTemporaryBorrows(borrowed)
			return nil, errorf(
				"borrow error: mutable borrow argument must be a local binding or direct field",
			)
		}
		if value != nil {
			if fn.params[idx].mutBorrow && value.borrowedParam && !value.mutBorrow {
				releaseTemporaryBorrows(borrowed)
				return nil, errorf(
					"borrow error: shared borrow `%s` cannot be forwarded as mutable",
					value.name,
				)
			}
			mutable := fn.params[idx].mutBorrow
			if err := checkBorrowConflictForField(value, field, mutable); err != nil {
				releaseTemporaryBorrows(borrowed)
				return nil, err
			}
			c.activateBorrow(value, field, mutable)
			borrowed = append(borrowed, temporaryBorrow{
				value: value, field: field, mutable: mutable,
			})
		}
	}
	return borrowed, nil
}

// callBorrowTarget resolves call-scoped borrowable places and ignores non-place shared values.
func (c *Checker) callBorrowTarget(
	arg ast.Expression,
	env *scope,
) (*binding, string, error) {
	if prefix, ok := borrowPrefix(arg); ok {
		return c.borrowTarget(prefix.Right, env)
	}
	if field, ok := arg.(*ast.FieldExpr); ok && field.Namespace {
		return nil, "", nil
	}
	switch arg.(type) {
	case *ast.IdentExpr, *ast.FieldExpr:
		return c.borrowTarget(arg, env)
	default:
		return nil, "", nil
	}
}

// checkBorrowConflict rejects aliasing that would overlap a mutable borrow.
func checkBorrowConflict(value *binding, mutable bool) error {
	return checkBorrowConflictForField(value, "", mutable)
}

// checkBorrowConflictForField rejects overlapping whole-value or field borrows.
// Field paths conflict when they alias: one path names the other or a struct
// containing it, so `a.b` collides with `a.b.c` while `a.b` and `a.c` stay
// disjoint.
func checkBorrowConflictForField(value *binding, field string, mutable bool) error {
	// A value one of whose fields has been taken out no longer matches its own
	// type, and a borrow hands the whole of it to someone who will read the
	// field that is gone.
	if consumed, ok := partiallyConsumedField(value); ok && consumed != field {
		return errorf(
			"borrow error: field `%s.%s` is already consumed, so `%s` cannot be borrowed",
			value.name, consumed, value.name,
		)
	}
	if field != "" {
		return checkFieldBorrowConflict(value, field, mutable)
	}
	return checkValueBorrowConflict(value, mutable)
}

// checkFieldBorrowConflict rejects a field borrow that overlaps a live one.
func checkFieldBorrowConflict(value *binding, field string, mutable bool) error {
	if value.activeMutBorrows > 0 {
		return errorf(
			"borrow error: value `%s` cannot be borrowed while mutably borrowed",
			value.name,
		)
	}
	if mutable && value.activeBorrows > 0 {
		return errorf(
			"borrow error: field `%s.%s` cannot be mutably borrowed while value is borrowed",
			value.name,
			field,
		)
	}
	if mutable && overlappingFieldCount(value.fieldBorrows, field) > 0 {
		return errorf(
			"borrow error: field `%s.%s` cannot be mutably borrowed while borrowed",
			value.name,
			field,
		)
	}
	if overlappingFieldCount(value.fieldMutBorrows, field) > 0 {
		return errorf(
			"borrow error: field `%s.%s` cannot be borrowed while mutably borrowed",
			value.name,
			field,
		)
	}
	return nil
}

// checkValueBorrowConflict rejects a whole-value borrow that overlaps a live
// one, including a borrow of any of its fields.
func checkValueBorrowConflict(value *binding, mutable bool) error {
	if mutable && value.activeBorrows > 0 {
		return errorf(
			"borrow error: value `%s` cannot be mutably borrowed while borrowed",
			value.name,
		)
	}
	if value.activeMutBorrows > 0 || len(value.fieldMutBorrows) > 0 {
		return errorf(
			"borrow error: value `%s` cannot be borrowed while mutably borrowed",
			value.name,
		)
	}
	if mutable && len(value.fieldBorrows) > 0 {
		return errorf(
			"borrow error: value `%s` cannot be mutably borrowed while field is borrowed",
			value.name,
		)
	}
	return nil
}

// releaseTemporaryBorrows clears temporary borrow state for a completed call.
func releaseTemporaryBorrows(values []temporaryBorrow) {
	for _, borrow := range values {
		releaseTemporaryBorrow(borrow)
	}
}

// releaseTemporaryBorrow clears one call-scoped whole-value or field borrow.
func releaseTemporaryBorrow(borrow temporaryBorrow) {
	value := borrow.value
	if borrow.field == "" {
		if borrow.mutable && value.activeMutBorrows > 0 {
			value.activeMutBorrows--
		} else if !borrow.mutable && value.activeBorrows > 0 {
			value.activeBorrows--
		}
		return
	}
	if borrow.mutable && value.fieldMutBorrows[borrow.field] > 0 {
		value.fieldMutBorrows[borrow.field]--
		if value.fieldMutBorrows[borrow.field] == 0 {
			delete(value.fieldMutBorrows, borrow.field)
		}
		return
	}
	if !borrow.mutable && value.fieldBorrows[borrow.field] > 0 {
		value.fieldBorrows[borrow.field]--
		if value.fieldBorrows[borrow.field] == 0 {
			delete(value.fieldBorrows, borrow.field)
		}
	}
}

// releaseBorrow clears one local borrow from each of its owners.
func releaseBorrow(value *binding) {
	for _, source := range value.borrowTargets {
		releaseBorrowSource(value, source)
	}
}

// releaseBorrowSource clears one owner's share of a local borrow.
func releaseBorrowSource(value *binding, source borrowSource) {
	target := source.target
	if source.field == "" {
		if value.mutBorrow && target.activeMutBorrows > 0 {
			target.activeMutBorrows--
		} else if target.activeBorrows > 0 {
			target.activeBorrows--
		}
		return
	}
	if value.mutBorrow && target.fieldMutBorrows[source.field] > 0 {
		target.fieldMutBorrows[source.field]--
		if target.fieldMutBorrows[source.field] == 0 {
			delete(target.fieldMutBorrows, source.field)
		}
		return
	}
	if target.fieldBorrows[source.field] > 0 {
		target.fieldBorrows[source.field]--
		if target.fieldBorrows[source.field] == 0 {
			delete(target.fieldBorrows, source.field)
		}
	}
}

// checkPrintCall reads the printed value without taking ownership.
func (c *Checker) checkPrintCall(expr *ast.CallExpr, env *scope) (string, error) {
	for _, arg := range expr.Args {
		if _, err := c.readExpr(arg, env); err != nil {
			return "", err
		}
	}
	return "void", nil
}

// newBinding creates a local ownership binding with a stable ID.
func (c *Checker) newBinding(name string, typeName string) *binding {
	c.nextID++
	return &binding{id: c.nextID, name: name, typeName: typeName}
}

// setArenaProvenance records arena and handle origins for local bindings.
func (c *Checker) setArenaProvenance(value *binding, expr ast.Expression, env *scope) {
	if isArenaConstructorExpr(expr) {
		value.arenaID = value.id
		return
	}
	if arena := c.arenaAddReceiver(expr, env); arena != nil {
		value.handleArenaID = arena.arenaID
		return
	}
	// A copied handle still points into the arena the original came from,
	// so the copy carries the source's provenance.
	value.handleArenaID = c.knownHandleProvenance(expr, env)
}

// isArenaConstructorExpr reports the public std::arena::new<T>(allocator) constructor.
func isArenaConstructorExpr(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	typeApply, ok := call.Callee.(*ast.TypeApplyExpr)
	if !ok {
		return false
	}
	name, ok := qualifiedName(typeApply.Callee)
	return ok && name == "std::arena::new"
}

// arenaOutlivesFrame reports whether the arena with this provenance ID is
// storage the caller keeps: a field arena of a borrowed parameter. A handle
// into such an arena escapes this frame without outliving its arena (§10),
// which is what lets a method on `&var self` hand back the handle it added.
// An arena this frame owns — a local binding, or a field of one — still pins
// its handles to the frame.
func (c *Checker) arenaOutlivesFrame(arenaID int, env *scope) bool {
	owner, ok := env.fieldArenaOwner(arenaID)
	return ok && owner.borrowedParam
}

// directFieldArenaID returns a stable arena identity for one owner field.
func (c *Checker) directFieldArenaID(receiver *directFieldReceiver) int {
	if receiver.owner.fieldArenaIDs == nil {
		receiver.owner.fieldArenaIDs = map[string]int{}
	}
	if id := receiver.owner.fieldArenaIDs[receiver.field]; id != 0 {
		return id
	}
	c.nextID++
	receiver.owner.fieldArenaIDs[receiver.field] = c.nextID
	return c.nextID
}

// allowsDirectFieldCleanup reports whether field.deinit is inside owner deinit.
func (c *Checker) allowsDirectFieldCleanup(receiver *directFieldReceiver) bool {
	return c.allowsOwnerFieldCleanup(receiver.owner)
}

// allowsOwnerFieldCleanup reports whether a field of this binding may be
// consumed here. The value has to be one the frame holds -- a borrow is
// someone else's and taking it apart would free what the lender still has --
// and its type has to be one whose obligation is its fields' obligations.
// A type that declares its own deinit is taken whole everywhere but inside
// that deinit, which is where the declared obligation is being discharged.
func (c *Checker) allowsOwnerFieldCleanup(owner *binding) bool {
	if owner == nil || owner.borrowedParam || owner.localBorrow {
		return false
	}
	if !ast.OwnerType(c.declaredDeinits, owner.typeName) {
		return true
	}
	return c.insideDeinitOf(owner)
}

// insideDeinitOf reports whether the current function is this binding's own
// declared deinit.
func (c *Checker) insideDeinitOf(owner *binding) bool {
	fn := c.currentFunction
	if fn == nil || stdmethod.CallName(fn.sig.Name) != "deinit" || returnTypeName(fn) != "void" {
		return false
	}
	if len(fn.params) == 0 || len(fn.sig.Params) == 0 {
		return false
	}
	if owner == nil || fn.sig.Params[0].Name != owner.name {
		return false
	}
	if fn.params[0].borrow || fn.params[0].mutBorrow {
		return false
	}
	return sameOwnershipType(fn.params[0].typeName, owner.typeName)
}

// matchesOwnerUnionDeinit reports whether a `match` consumes the active variant
// of a union inside that union's own `deinit(self: T) -> void`. Only there is the
// active payload owned and cleanable; everywhere else it stays a borrow.
func (c *Checker) matchesOwnerUnionDeinit(value ast.Expression, valueType string) bool {
	fn := c.currentFunction
	if fn == nil || stdmethod.CallName(fn.sig.Name) != "deinit" || returnTypeName(fn) != "void" {
		return false
	}
	if len(fn.params) == 0 || len(fn.sig.Params) == 0 {
		return false
	}
	if fn.params[0].borrow || fn.params[0].mutBorrow {
		return false
	}
	if !sameOwnershipType(fn.params[0].typeName, valueType) {
		return false
	}
	ident, ok := value.(*ast.IdentExpr)
	if !ok || ident.Name != fn.sig.Params[0].Name {
		return false
	}
	return c.unions[valueType] != nil
}

// arenaAddReceiver returns the arena binding for arena.add(value) expressions.
// An add reports a refused allocation, so what a handle is bound to is a `try`
// over the call; the arena it came from is the same one either way, and losing
// it here would lose the provenance every later handle rule reads (§10).
func (c *Checker) arenaAddReceiver(expr ast.Expression, env *scope) *binding {
	if try, ok := expr.(*ast.TryExpr); ok {
		return c.arenaAddReceiver(try.Value, env)
	}
	if inner, ok := transparentExprValue(expr); ok {
		return c.arenaAddReceiver(inner, env)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "add" {
		return nil
	}
	receiver, ok := field.Receiver.(*ast.IdentExpr)
	if ok {
		arena, _ := env.lookup(receiver.Name)
		return arena
	}
	fieldReceiver, ok := field.Receiver.(*ast.FieldExpr)
	if !ok {
		return nil
	}
	direct, err := c.directFieldReceiver(fieldReceiver, env)
	if err != nil {
		return nil
	}
	base, _, ok := splitGenericType(direct.typeName)
	if !ok || base != "std::arena::Arena" {
		return nil
	}
	return c.bindingForDirectFieldReceiver(direct)
}

// isArenaAtExpr reports whether expr is an arena.at call.
func (c *Checker) isArenaAtExpr(expr ast.Expression, env *scope) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	field, ok := call.Callee.(*ast.FieldExpr)
	if !ok || field.Name != "at" {
		return false
	}
	receiver, err := c.readExpr(field.Receiver, env)
	if err != nil {
		return false
	}
	base, _, ok := splitGenericType(receiver)
	return ok && base == "std::arena::Arena"
}

// containsArenaAt reports whether an expression reads through arena.at.
func (c *Checker) containsArenaAt(expr ast.Expression, env *scope) bool {
	if inner, ok := transparentExprValue(expr); ok {
		return c.containsArenaAt(inner, env)
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if c.isArenaAtExpr(e, env) {
			return true
		}
		for _, arg := range e.Args {
			if c.containsArenaAt(arg, env) {
				return true
			}
		}
	case *ast.FieldExpr:
		return c.containsArenaAt(e.Receiver, env)
	case *ast.BinaryExpr:
		return c.containsArenaAt(e.Left, env) || c.containsArenaAt(e.Right, env)
	case *ast.CastExpr:
		return c.containsArenaAt(e.Value, env)
	case *ast.TryExpr:
		return c.containsArenaAt(e.Value, env)
	case *ast.ComptimeExpr:
		return c.containsArenaAt(e.Expr, env)
	}
	return false
}

// readIdent resolves a variable reference without moving it.
func readIdent(ident *ast.IdentExpr, env *scope) (string, error) {
	value, ok := env.lookup(ident.Name)
	if ok {
		if err := checkDeinitializedUse(ident.Name, value, env, ident.Span); err != nil {
			return "", err
		}
		if value.moved {
			return "", errorAt(ident.Span, "move error: moved value `%s` was used", ident.Name)
		}
		if value.activeMutBorrows > 0 || len(value.fieldMutBorrows) > 0 {
			return "", errorAt(ident.Span,
				"borrow error: value `%s` cannot be read while mutably borrowed",
				ident.Name)
		}
		return value.typeName, nil
	}
	if ident.Name == "void" {
		return "", errorAt(ident.Span, "move error: void is not a value")
	}
	return "", errorAt(ident.Span, "move error: undefined variable `%s`", ident.Name)
}

// checkDeinitializedUse rejects arenas and known handles after arena cleanup.
func checkDeinitializedUse(name string, value *binding, env *scope, span ast.Span) error {
	if value.deinitialized {
		return errorAt(span, "arena error: arena `%s` was deinitialized", name)
	}
	if value.handleArenaID == 0 {
		return nil
	}
	arena, ok := env.lookupArenaID(value.handleArenaID)
	if ok && arena.deinitialized {
		return errorAt(span, "arena error: handle `%s` cannot be used after arena `%s` deinit",
			name, arena.name)
	}
	return nil
}

// checkDeinitializedBorrow rejects borrowing an arena after cleanup.
func checkDeinitializedBorrow(name string, value *binding, span ast.Span) error {
	if value.deinitialized {
		return errorAt(span, "arena error: arena `%s` was deinitialized", name)
	}
	return nil
}

// errorUnionElement extracts T from !T.
func (c *Checker) errorUnionElement(typeName string) (string, bool) {
	_, success, ok := c.errorUnionParts(typeName)
	return success, ok
}

// errorUnionParts extracts error and success types from !T or Error!T.
func (c *Checker) errorUnionParts(typeName string) (string, string, bool) {
	parsed, err := c.types.Parse(typeName)
	if err != nil {
		return "", "", false
	}
	errorType, success, ok := typ.ErrorUnionParts(parsed)
	return typ.Text(errorType), typ.Text(success), ok
}

// returnTypeName returns void for functions without an explicit return type.
func returnTypeName(fn *functionInfo) string {
	if fn.returnType == "" {
		return "void"
	}
	return fn.returnType
}

// implMethod returns the concrete method signature for typeName when known.
func (c *Checker) implMethod(typeName string, method string) *functionInfo {
	methods := c.impls[typeName]
	if methods == nil {
		return nil
	}
	return methods[method]
}

// substituteOwnershipType instantiates simple generic wrapper type spellings.
func substituteOwnershipType(typeName string, subst map[string]string) string {
	if replacement, ok := subst[typeName]; ok {
		return replacement
	}
	out, err := typ.SubstituteText(typeName, subst)
	if err != nil {
		// A spelling this checker cannot parse is left as it stands; rejecting
		// it belongs to the type checker and its diagnostic.
		return typeName
	}
	return out
}

// instantiateTypeArgText replaces in-scope generic type parameters in a static list.
func (c *Checker) instantiateTypeArgText(typeArg string) string {
	args, err := typ.SplitArgs(typeArg)
	if err != nil {
		return c.resolveMetaTypeText(substituteOwnershipType(typeArg, c.typeArgValues))
	}
	for idx, arg := range args {
		args[idx] = c.resolveMetaTypeText(substituteOwnershipType(arg, c.typeArgValues))
	}
	return strings.Join(args, ", ")
}

// isCopyType reports whether values of typeName can be reused after move contexts.
func (c *Checker) isCopyType(typeName string) bool {
	if typeName == "[]u8" {
		return true
	}
	if isRawPointerType(typeName) {
		return true
	}
	// An error carries nothing, so reading one leaves nothing behind to move.
	if c.errorSets[typeName] != nil {
		return true
	}
	if c.enums[typeName] != nil {
		return true
	}
	switch typeName {
	case "bool", "void", "Io", "Allocator", "std::fs::Metadata",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64", "[]u8":
		return true
	}
	return c.isPlainDataType(typeName, nil)
}

// isPlainDataType reports whether typeName is plain copy data: a scalar, enum,
// error set, arena handle, or a declared struct / union whose fields and
// payloads are all plain copy data. Duplicating such a value creates no cleanup obligation, so
// copy propagates through it structurally. Views, capabilities, and owners are
// not plain data — aggregates holding them keep their own regimes — and a type
// that declares an explicit deinit stays move-only because the declared
// cleanup contract implies a consumption obligation.
func (c *Checker) isPlainDataType(typeName string, seen map[string]bool) bool {
	if inner, ok := strings.CutPrefix(typeName, "?"); ok {
		return c.isPlainDataType(inner, seen)
	}
	switch typeName {
	case "bool", "void",
		"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64":
		return true
	}
	if c.enums[typeName] != nil || c.errorSets[typeName] != nil {
		return true
	}
	// An arena handle is an opaque ID; the arena owns the value, so
	// duplicating the ID creates no cleanup obligation.
	if strings.HasPrefix(typeName, "std::arena::Handle<") {
		return true
	}
	// seen holds the current recursion path only: a revisit is a recursive
	// aggregate, which needs indirection and is never plain data.
	if seen[typeName] {
		return false
	}
	return c.isPlainDataAggregate(typeName, seen)
}

// isPlainDataAggregate walks a declared struct / union for isPlainDataType.
// A declared deinit keeps the type move-only regardless of its fields.
func (c *Checker) isPlainDataAggregate(typeName string, seen map[string]bool) bool {
	fields, isStruct := c.structs[typeName]
	variants, isUnion := c.unions[typeName]
	if (!isStruct && !isUnion) || c.implMethod(typeName, "deinit") != nil {
		return false
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	seen[typeName] = true
	defer delete(seen, typeName)
	if isStruct {
		for _, fieldType := range fields {
			if !c.isPlainDataType(fieldType, seen) {
				return false
			}
		}
		return true
	}
	for _, payload := range variants {
		if payload != "" && !c.isPlainDataType(payload, seen) {
			return false
		}
	}
	return true
}

// sameOwnershipType compares exact type spellings.
func sameOwnershipType(left string, right string) bool {
	return left == right
}

// fieldOwnershipType returns the full field type, including borrow prefixes.
func fieldOwnershipType(field ast.Field) string {
	if !field.Borrow {
		return typ.Text(field.TypeName)
	}
	if field.MutBorrow {
		return "&var " + typ.Text(field.TypeName)
	}
	return "&" + typ.Text(field.TypeName)
}

// explicitOwnershipBorrowType extracts &T and &var T spellings.
func explicitOwnershipBorrowType(typeName string) (string, bool, string, bool) {
	if !strings.HasPrefix(typeName, "&") {
		return "", false, "", false
	}
	rest := strings.TrimPrefix(typeName, "&")
	mutable := false
	if strings.HasPrefix(rest, "var ") {
		mutable = true
		rest = strings.TrimPrefix(rest, "var ")
	}
	if rest == "" {
		return "", false, "", false
	}
	return "", mutable, rest, true
}

// borrowedOwnershipValueType returns the value visible through an explicit
// borrow while leaving move-sensitive callers free to retain the &T spelling.
func borrowedOwnershipValueType(typeName string) string {
	if _, _, inner, ok := explicitOwnershipBorrowType(typeName); ok {
		return inner
	}
	return typeName
}

// isRawPointerType reports whether typeName is a raw pointer spelling.
func isRawPointerType(typeName string) bool {
	_, ok := rawPointerElement(typeName)
	return ok
}

// rawPointerElement extracts the element spelling from ptr<T> or ?ptr<T>.
func rawPointerElement(typeName string) (string, bool) {
	name := typeName
	if len(name) > 0 && name[0] == '?' {
		name = name[1:]
	}
	base, elem, ok := splitGenericType(name)
	if !ok || base != "ptr" {
		return "", false
	}
	return elem, true
}

// checkNoArgOwnershipCall validates a zero-argument builtin constructor.
func checkNoArgOwnershipCall(name string, args []ast.Expression) (string, error) {
	if len(args) != 0 {
		return "", errorf("move error: `%s` expects 0 args, got %d", name, len(args))
	}
	return name, nil
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

// checkedMapArgs validates and returns Map key/value static type arguments.
func (c *Checker) checkedMapArgs(arg string) ([]string, error) {
	args, ok := splitGenericArgs(arg)
	if !ok || len(args) != 2 {
		return nil, errorf("map error: std::map::Map expects 2 static arguments")
	}
	if !typ.IsMapKey(args[0]) {
		return nil, errorf("map error: std::map::Map key type must be one of %s",
			typ.MapKeyTypeNames())
	}
	return args, nil
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

// newScope creates a lexical ownership scope.
func newScope(parent *scope) *scope {
	return &scope{parent: parent, values: map[string]*binding{}}
}

// child creates a nested lexical ownership scope.
func (s *scope) child() *scope {
	return newScope(s)
}

// define binds a local name in the current scope.
func (s *scope) define(value *binding) {
	s.values[value.name] = value
}

// lookup resolves a local name by walking parent scopes.
func (s *scope) lookup(name string) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if value, ok := cur.values[name]; ok {
			return value, true
		}
	}
	return nil, false
}

// fieldArenaOwner resolves the binding whose field holds the arena with this
// provenance ID. Field arena bindings are transient projections, so the owner's
// fieldArenaIDs map is where the identity persists.
func (s *scope) fieldArenaOwner(arenaID int) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			for _, id := range value.fieldArenaIDs {
				if id == arenaID {
					return value, true
				}
			}
		}
	}
	return nil, false
}

// lookupArenaID resolves a tracked arena owner by provenance ID.
func (s *scope) lookupArenaID(arenaID int) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			if value.arenaID == arenaID {
				return value, true
			}
		}
	}
	return nil, false
}

// releaseLastUseBorrows ends local borrows whose binding is no longer used.
// A binding that is itself still borrowed keeps its own sources active: the
// chain buffer <- view <- allocator <- owner releases from the leaf inward,
// each release re-examining only the bindings it just unblocked.
func (s *scope) releaseLastUseBorrows(stmtIndex int, lastUses map[string]int) {
	for name, value := range s.values {
		s.releaseIfUnused(name, value, stmtIndex, lastUses)
	}
}

// releaseIfUnused releases one binding's borrow targets once it is past its
// last use and nothing borrows it, then retries the targets that release may
// have unblocked.
func (s *scope) releaseIfUnused(
	name string,
	value *binding,
	stmtIndex int,
	lastUses map[string]int,
) {
	if len(value.borrowTargets) == 0 {
		return
	}
	if last, ok := lastUses[name]; ok && last > stmtIndex {
		return
	}
	// A deferred Future or TaskSet cleanup is a use at scope exit: live workers
	// can still reach their retained state and capabilities. Keep those sources
	// alive until cleanup actually consumes the task owner.
	if value.deferCleanup && tiedStructReturn(value.typeName) &&
		!value.deinitialized && !value.moved {
		return
	}
	if value.hasAnyBorrow() {
		return
	}
	targets := value.borrowTargets
	releaseBorrow(value)
	value.borrowTargets = nil
	for _, source := range targets {
		if held, ok := s.values[source.target.name]; ok && held == source.target {
			s.releaseIfUnused(source.target.name, held, stmtIndex, lastUses)
		}
	}
}

// hasAnyBorrow reports whether a whole value or any direct field is borrowed.
func (b *binding) hasAnyBorrow() bool {
	return b.activeBorrows > 0 || b.activeMutBorrows > 0 ||
		len(b.fieldBorrows) > 0 || len(b.fieldMutBorrows) > 0
}

// markFieldDeinit records that an owned direct field has been cleaned up.
func (b *binding) markFieldDeinit(field string) {
	if b.fieldDeinit == nil {
		b.fieldDeinit = map[string]bool{}
	}
	b.fieldDeinit[field] = true
}

// clearFieldDeinit records assignment of a fresh value into a direct field.
func (b *binding) clearFieldDeinit(field string) {
	if b.fieldDeinit == nil {
		return
	}
	delete(b.fieldDeinit, field)
}

// directFieldRoot returns a local field-path assignment or read target: the
// root binding and the dotted field path relative to it.
func directFieldRoot(expr ast.Expression, env *scope) (*binding, string, bool) {
	if target, ok := expr.(*ast.IdentExpr); ok {
		value, exists := env.lookup(target.Name)
		return value, "", exists
	}
	ident, path, ok := ast.FieldPathRoot(expr)
	if !ok {
		return nil, "", false
	}
	value, exists := env.lookup(ident.Name)
	return value, path, exists
}

// fieldPathsOverlap reports whether two dotted field paths alias the same
// storage: equal paths, or one naming a struct that contains the other
// ("a.b" overlaps "a.b.c"; "a.b" does not overlap "a.bc").
func fieldPathsOverlap(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if !strings.HasPrefix(b, a) {
		return false
	}
	return len(a) == len(b) || b[len(a)] == '.'
}

// overlappingFieldCount sums the borrow counts of every tracked field path
// that aliases path.
func overlappingFieldCount(counts map[string]int, path string) int {
	total := 0
	for key, count := range counts {
		if fieldPathsOverlap(key, path) {
			total += count
		}
	}
	return total
}

// overlappingFieldDeinit returns a deinitialized field path that aliases path.
func overlappingFieldDeinit(deinit map[string]bool, path string) (string, bool) {
	for key, done := range deinit {
		if done && fieldPathsOverlap(key, path) {
			return key, true
		}
	}
	return "", false
}

// fieldPathType resolves a dotted field path against struct declarations,
// hop by hop, starting from the root's type.
func (c *Checker) fieldPathType(typeName string, path string) (string, bool) {
	current := typeName
	for _, segment := range strings.Split(path, ".") {
		fields := c.structs[current]
		if fields == nil {
			return "", false
		}
		next, ok := fields[segment]
		if !ok {
			return "", false
		}
		current = next
	}
	return current, true
}

// blockLastUses returns the last statement index where each identifier appears.
func blockLastUses(block *ast.BlockStmt) map[string]int {
	lastUses := map[string]int{}
	for idx, stmt := range block.Statements {
		for _, name := range loopIdentUses(stmt) {
			lastUses[name] = len(block.Statements) + 1
		}
		for _, name := range stmtIdentUses(stmt) {
			if lastUses[name] > len(block.Statements) {
				continue
			}
			lastUses[name] = idx
		}
	}
	return lastUses
}

// loopIdentUses returns identifiers used inside loop bodies.
func loopIdentUses(stmt ast.Statement) []string {
	switch s := stmt.(type) {
	case *ast.WhileStmt:
		return blockIdentUses(s.Body)
	case *ast.ForStmt:
		return blockIdentUses(s.Body)
	default:
		return nil
	}
}

// stmtIdentUses collects identifier reads from one statement.
func stmtIdentUses(stmt ast.Statement) []string {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return exprIdentUses(s.Value)
	case *ast.AssignStmt:
		uses := exprIdentUses(s.Value)
		return append(uses, exprIdentUses(s.Target)...)
	case *ast.ReturnStmt:
		return exprIdentUses(s.Value)
	case *ast.DeferStmt:
		return exprIdentUses(s.Expr)
	case *ast.ErrDeferStmt:
		return exprIdentUses(s.Expr)
	case *ast.ExprStmt:
		return exprIdentUses(s.Expr)
	case *ast.IfStmt:
		uses := exprIdentUses(s.Condition)
		uses = append(uses, blockIdentUses(s.Consequence)...)
		uses = append(uses, blockIdentUses(s.Alternative)...)
		return uses
	case *ast.WhileStmt:
		uses := exprIdentUses(s.Condition)
		return append(uses, blockIdentUses(s.Body)...)
	case *ast.ForStmt:
		uses := exprIdentUses(s.Start)
		uses = append(uses, exprIdentUses(s.End)...)
		return append(uses, blockIdentUses(s.Body)...)
	case *ast.MatchStmt:
		uses := exprIdentUses(s.Value)
		for _, arm := range s.Arms {
			uses = append(uses, stmtIdentUses(arm.Body)...)
		}
		return uses
	case *ast.ComptimeIfStmt:
		uses := exprIdentUses(s.Condition)
		uses = append(uses, blockIdentUses(s.Consequence)...)
		uses = append(uses, blockIdentUses(s.Alternative)...)
		return uses
	default:
		return nil
	}
}

// wrappedExprValue returns the operand of a one-operand expression wrapper.
func wrappedExprValue(expr ast.Expression) ast.Expression {
	switch e := expr.(type) {
	case *ast.CastExpr:
		return e.Value
	case *ast.TryExpr:
		return e.Value
	case *ast.MoveExpr:
		return e.Value
	default:
		return nil
	}
}

// blockIdentUses collects identifier reads inside a nested block.
func blockIdentUses(block *ast.BlockStmt) []string {
	if block == nil {
		return nil
	}
	uses := []string{}
	for _, stmt := range block.Statements {
		uses = append(uses, stmtIdentUses(stmt)...)
	}
	return uses
}

// exprIdentUses collects identifier reads from an expression.
func exprIdentUses(expr ast.Expression) []string {
	if inner, ok := transparentExprValue(expr); ok {
		return exprIdentUses(inner)
	}
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.IdentExpr:
		return []string{e.Name}
	case *ast.BinaryExpr:
		return append(exprIdentUses(e.Left), exprIdentUses(e.Right)...)
	case *ast.CallExpr:
		uses := exprIdentUses(e.Callee)
		for _, arg := range e.Args {
			uses = append(uses, exprIdentUses(arg)...)
		}
		return uses
	case *ast.CastExpr, *ast.TryExpr, *ast.MoveExpr:
		// One-operand wrappers read whatever they wrap. A `move` is among
		// them: missing it would end the value's live range at its previous
		// statement, and everything a last use releases -- the borrows it
		// holds, the allocator it is tied to -- would go one statement early.
		return exprIdentUses(wrappedExprValue(e))
	case *ast.StructLiteralExpr:
		uses := []string{}
		for _, field := range e.Fields {
			uses = append(uses, exprIdentUses(field.Value)...)
		}
		return uses
	case *ast.FieldExpr:
		return exprIdentUses(e.Receiver)
	case *ast.DerefExpr:
		return exprIdentUses(e.Receiver)
	case *ast.IndexExpr:
		uses := exprIdentUses(e.Target)
		uses = append(uses, exprIdentUses(e.Index)...)
		uses = append(uses, exprIdentUses(e.Start)...)
		return append(uses, exprIdentUses(e.End)...)
	case *ast.ComptimeExpr:
		return exprIdentUses(e.Expr)
	default:
		return valueStmtIdentUses(expr)
	}
}

// valueStmtIdentUses collects identifier reads from the expressions that
// carry statements: an `if` or `match` standing as a value reads whatever its
// branches read, and a guard's exit reads whatever it returns.
func valueStmtIdentUses(expr ast.Expression) []string {
	switch e := expr.(type) {
	case *ast.IfStmt:
		return stmtIdentUses(e)
	case *ast.MatchStmt:
		return stmtIdentUses(e)
	case *ast.OrelseGuardExpr:
		return append(exprIdentUses(e.Cond), stmtIdentUses(e.Exit)...)
	case *ast.CatchGuardExpr:
		return append(exprIdentUses(e.Cond), stmtIdentUses(e.Exit)...)
	default:
		return nil
	}
}

// borrowPrefix reports whether an expression is &T or &var T syntax.
func borrowPrefix(expr ast.Expression) (*ast.PrefixExpr, bool) {
	prefix, ok := expr.(*ast.PrefixExpr)
	if !ok || (prefix.Operator != "&" && prefix.Operator != "&var") {
		return nil, false
	}
	return prefix, true
}

// clone copies a scope chain so branch checks do not interfere with each other.
//
// A binding's scalar facts copy with the struct. Its map and slice facts do
// not, so each is copied here by name: sharing one would let a branch write
// into the state the other branches and the code after them read, which is a
// union merge nobody declared.
//
// Copying is only half of a fact's story. The other half is what happens where
// the branches meet, and that differs per fact:
//
//   - fieldDeinit is a cleanup obligation, so it has both a merge
//     (mergeMovedFrom) and an agreement check that refuses a field only some
//     surviving paths released.
//   - fieldBorrows and fieldMutBorrows are counters a capture or `let` raises
//     and drops inside one scope, so a branch always leaves them as it found
//     them and there is nothing to carry out.
//   - fieldArenaIDs hands a field arena a stable identity on first use. A
//     branch-local identity that is lost costs a fresh number later, and a
//     handle can only leave a branch through a binding that already had one,
//     which means the identity was fixed outside.
//   - borrowTargets is rebuilt where a borrow is tied, never read across a
//     branch boundary.
//
// TestBindingFactsAreCopiedByClone fails when a new map or slice appears on
// binding, so the next fact has to answer both halves before it compiles.
func (s *scope) clone() *scope {
	if s == nil {
		return nil
	}
	cloned := &scope{values: map[string]*binding{}}
	cloned.parent = s.parent.clone()
	for name, value := range s.values {
		copyValue := *value
		copyValue.fieldBorrows = copyIntMap(value.fieldBorrows)
		copyValue.fieldMutBorrows = copyIntMap(value.fieldMutBorrows)
		copyValue.fieldDeinit = copyBoolMap(value.fieldDeinit)
		copyValue.fieldArenaIDs = copyIntMap(value.fieldArenaIDs)
		copyValue.borrowTargets = append([]borrowSource(nil), value.borrowTargets...)
		cloned.values[name] = &copyValue
	}
	return cloned
}

// copyIntMap copies a per-field counter map so a clone counts on its own.
func copyIntMap(source map[string]int) map[string]int {
	if source == nil {
		return nil
	}
	out := make(map[string]int, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// copyBoolMap copies a per-field flag map so a clone marks on its own.
func copyBoolMap(source map[string]bool) map[string]bool {
	if source == nil {
		return nil
	}
	out := make(map[string]bool, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// mergeMovedFrom carries what a checked branch did to a binding out past the
// branch: that it moved it, that it released it, and which of its fields it
// cleaned. A field cleaned on one surviving branch only never reaches here —
// checkBranchConsumeAgreement refuses that first — so carrying the marks over
// says the same thing for a field that `moved` says for the whole value.
func (s *scope) mergeMovedFrom(other *scope) {
	byID := map[int]*binding{}
	s.collectBindings(byID)
	other.walkBindings(func(value *binding) {
		target, ok := byID[value.id]
		if !ok {
			return
		}
		if value.moved {
			target.moved = true
		}
		if value.deinitialized {
			target.deinitialized = true
		}
		for field, cleaned := range value.fieldDeinit {
			if cleaned {
				target.markFieldDeinit(field)
			}
		}
	})
}

// collectBindings stores bindings by stable ID.
func (s *scope) collectBindings(out map[int]*binding) {
	s.walkBindings(func(value *binding) {
		out[value.id] = value
	})
}

// lookupID finds the binding with this id, innermost scope first. A clone
// keeps the id of the binding it copied, so this reaches the current state of
// a value even where a later `let` of the same name hides it from lookup.
func (s *scope) lookupID(id int) (*binding, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			if value.id == id {
				return value, true
			}
		}
	}
	return nil, false
}

// walkBindings visits all bindings in this scope chain.
func (s *scope) walkBindings(visit func(*binding)) {
	for cur := s; cur != nil; cur = cur.parent {
		for _, value := range cur.values {
			visit(value)
		}
	}
}
