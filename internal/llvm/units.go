package llvm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
	typpkg "github.com/kizu-lang/kizu/internal/typ"
)

// Unit is one part of a program the native toolchain compiles on its own.
type Unit struct {
	// Module is the module path whose functions the unit defines. The unit of
	// the empty path is the program unit: it holds what the compiler writes
	// for the whole program (the package entry point, the test runner, the
	// table of error names), and a loose file's functions with them.
	Module string
	Text   string
}

// EmitUnits formats a module as the units a native build compiles. Unsplit,
// it is one unit with the text emit writes for the target. Split, it is one unit per
// module and the program unit last, each a module LLVM compiles alone: a
// unit's text depends only on its own functions, so an edit to one module
// changes the text of that unit alone, and what the toolchain made of the
// others can be reused.
//
// A unit is decided by data alone -- each function's Module, the order of
// ir.Module.Modules, the order of the module's functions -- never by a map's
// iteration order, so the compiler and the compiler it builds split a
// program the same way and link the same executable.
func EmitUnits(module *ir.Module, darwin bool, arch Arch, split bool) ([]Unit, error) {
	if !split {
		text, err := emit(module, darwin, arch)
		if err != nil {
			return nil, err
		}
		return []Unit{{Text: text}}, nil
	}
	whole := newEmitter(module, darwin, arch)
	if err := whole.validateForeignBoundary(); err != nil {
		return nil, err
	}
	if err := whole.validateModuleTypes(); err != nil {
		return nil, err
	}
	program := &splitProgram{
		functions:  module.Functions,
		byName:     map[string]*ir.Function{},
		errorNames: whole.needsErrorNameTable(),
	}
	for _, fn := range module.Functions {
		program.byName[fn.Name] = fn
	}
	plans := partitionUnits(module, program.byName)
	units := make([]Unit, 0, len(plans))
	for _, plan := range plans {
		view := *module
		view.Functions = plan.functions
		e := newEmitter(&view, darwin, arch)
		e.split = &splitUnit{program: program, programUnit: plan.module == ""}
		text, err := e.finish()
		if err != nil {
			return nil, err
		}
		units = append(units, Unit{Module: plan.module, Text: text})
	}
	return units, nil
}

// splitProgram is what every unit of a split program shares: the whole
// program's functions, which a unit calls by symbol whichever unit defines
// them, and whether the program carries a table of error names.
type splitProgram struct {
	functions  []*ir.Function
	byName     map[string]*ir.Function
	errorNames bool
}

// splitUnit is how an emitter writing one unit of a split program differs
// from one writing the whole program.
type splitUnit struct {
	program *splitProgram
	// programUnit is set on the unit that defines the table of error names.
	programUnit bool
}

// unitPlan is one unit before it is written: its module and the functions it
// defines, in the order the module holds them.
type unitPlan struct {
	module    string
	functions []*ir.Function
}

// partitionUnits splits a module's functions into units: the modules in the
// order ir.Module.Modules lists them, then any module it does not list in the
// order its first function appears, then the program unit. A module none of
// whose functions survived reachability has no unit; the program unit is
// always written, since it holds the error names every unit may read.
//
// A generic instance is defined by every unit whose functions reach it,
// directly or through other instances: its code depends on the static
// arguments its caller chose, so it belongs to the unit that asked for it
// and not to the one that declared the generic, and choosing one owner from
// the whole program would move it whenever another module began to use it.
func partitionUnits(module *ir.Module, byName map[string]*ir.Function) []unitPlan {
	order := unitOrder(module)
	plans := make([]unitPlan, 0, len(order))
	for _, path := range order {
		functions := unitFunctions(module, byName, path)
		if len(functions) == 0 && path != "" {
			continue
		}
		plans = append(plans, unitPlan{module: path, functions: functions})
	}
	return plans
}

// unitOrder lists the module paths of the units in the order they are
// written: those ir.Module.Modules lists, then any other a function names,
// then the program unit's empty path.
func unitOrder(module *ir.Module) []string {
	order := []string{}
	listed := map[string]bool{"": true}
	for _, path := range module.Modules {
		if !listed[path] {
			listed[path] = true
			order = append(order, path)
		}
	}
	for _, fn := range module.Functions {
		if !fn.Instance && !listed[fn.Module] {
			listed[fn.Module] = true
			order = append(order, fn.Module)
		}
	}
	return append(order, "")
}

// unitFunctions returns, in the order the module holds them, the functions
// the unit of path defines: those declared in its module, and the generic
// instances they reach.
func unitFunctions(module *ir.Module, byName map[string]*ir.Function, path string) []*ir.Function {
	members := map[*ir.Function]bool{}
	queue := []*ir.Function{}
	for _, fn := range module.Functions {
		if !fn.Instance && fn.Module == path {
			members[fn] = true
			queue = append(queue, fn)
		}
	}
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		for _, name := range ir.DirectCallees(fn) {
			callee := byName[name]
			if callee != nil && callee.Instance && !members[callee] {
				members[callee] = true
				queue = append(queue, callee)
			}
		}
	}
	functions := make([]*ir.Function, 0, len(members))
	for _, fn := range module.Functions {
		if members[fn] {
			functions = append(functions, fn)
		}
	}
	return functions
}

// unitSymbolPrefix starts the symbol of every Kizu function a split unit
// shares with the others. A unit calls another's functions, so those cannot
// keep the internal linkage that keeps a program's names from answering the
// C runtime's calls (functionLinkage); the `-` keeps them apart instead,
// because no C identifier and nothing llvmNamePart writes holds one.
const unitSymbolPrefix = "kz-"

// functionSymbol is the symbol the function name is defined and called by.
func (e *emitter) functionSymbol(name string) string {
	if e.split == nil {
		return llvmFunctionName(name)
	}
	fn := e.split.program.byName[name]
	if fn == nil || ownsSymbol(fn) {
		return llvmFunctionName(name)
	}
	return unitSymbolPrefix + llvmFunctionName(name)
}

// ownsSymbol reports whether a function is defined under the symbol its name
// spells: the entry point, and a C export whose symbol is its own name.
func ownsSymbol(fn *ir.Function) bool {
	return fn.Name == "main" || fn.ExportABI == "c" && llvmFunctionName(fn.Name) == fn.ExportName
}

// splitLinkage is the linkage a function of a split unit is defined with.
// Every function another unit may call is visible to the other units and
// hidden from everything outside the program. Which functions another unit
// calls is not asked: the answer would come from the other units, and an
// edit to one of them would change this unit's text. A generic instance is
// one copy among those every unit that reaches it carries, and the linker
// keeps one.
func splitLinkage(fn *ir.Function) string {
	switch {
	case ownsSymbol(fn):
		return ""
	case fn.Instance:
		return "linkonce_odr hidden "
	default:
		return "hidden "
	}
}

// writeComdat writes the comdat a copy the linker may find in several units
// is grouped by, and returns the clause its definition names it with. ELF and
// COFF keep one copy per comdat; Mach-O has no comdats, and its linker keeps
// one of the weak definitions linkonce_odr writes there.
func (e *emitter) writeComdat(symbol string) string {
	if e.darwin {
		return ""
	}
	fmt.Fprintf(&e.out, "$%s = comdat any\n", symbol)
	return " comdat"
}

// thunkLinkage is the linkage of an adapter the runtime calls a worker
// through. Unsplit it is internal; split, every unit that spawns a worker of
// one state type carries the same adapter, as it would a generic instance.
func (e *emitter) thunkLinkage(symbol string) (string, string) {
	if e.split == nil {
		return "internal ", ""
	}
	return "linkonce_odr hidden ", e.writeComdat(symbol)
}

// writeUnitFunctionDecls declares the Kizu functions the unit calls or takes
// the address of that another unit defines, in name order.
func (e *emitter) writeUnitFunctionDecls() {
	if e.split == nil {
		return
	}
	names := e.unitExternalFunctions()
	for _, name := range names {
		e.out.WriteString(e.functionDecl(e.split.program.byName[name]))
		e.out.WriteByte('\n')
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
}

// unitExternalFunctions names, sorted, the Kizu functions the unit's own
// functions name but another unit defines.
func (e *emitter) unitExternalFunctions() []string {
	defined := map[string]bool{}
	for _, fn := range e.module.Functions {
		defined[fn.Name] = true
	}
	seen := map[string]bool{}
	for _, fn := range e.module.Functions {
		for _, name := range ir.DirectCallees(fn) {
			if !defined[name] && e.split.program.byName[name] != nil {
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// functionDecl declares a function another unit defines, with the parameters
// definitionParams defines it with: a function that returns through a slot
// takes that slot first, a C export takes each value as C passes it, and a
// struct otherwise arrives as the address of the caller's copy.
func (e *emitter) functionDecl(fn *ir.Function) string {
	returnType := e.llvmType(fn.Return)
	params := make([]string, 0, len(fn.Params)+1)
	if e.returnsThroughSlot(fn.Return) {
		returnType = "void"
		params = append(params, "ptr")
	}
	returnAttrs := ""
	for _, param := range fn.Params {
		switch {
		case fn.ExportABI == "c":
			params = append(params, e.llvmType(param.Type)+cIntegerExtension(param.Type))
		case e.usesIndirectStructParamABI(param.Type):
			params = append(params, "ptr")
		default:
			params = append(params, e.llvmType(param.Type))
		}
	}
	if fn.ExportABI == "c" {
		returnAttrs = strings.TrimPrefix(cIntegerExtension(fn.Return), " ")
		if returnAttrs != "" {
			returnAttrs += " "
		}
	}
	visibility := "hidden "
	if ownsSymbol(fn) {
		visibility = ""
	}
	return fmt.Sprintf("declare %s%s%s @%s(%s)",
		visibility, returnAttrs, returnType,
		e.functionSymbol(fn.Name), strings.Join(params, ", "))
}

// writesErrorNameTable reports whether this emitter defines the table of
// error names: the whole program does when anything in it can carry an
// error, and of a split program the program unit does.
func (e *emitter) writesErrorNameTable() bool {
	if e.split == nil {
		return e.needsErrorNameTable()
	}
	return e.split.programUnit && e.split.program.errorNames
}

// typeScope names the structs and unions a split unit writes the definitions
// of: every one its functions spell, those the functions it declares spell,
// and what the fields and payloads of those hold, transitively. A unit that
// wrote every type of the program would change whenever any module added
// one. Unsplit, every type is written.
func (e *emitter) collectTypeScope() {
	if e.split == nil {
		return
	}
	e.typeScope = map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if name == "" {
			return
		}
		e.addScopedType(name, visit)
		parsed, err := e.types.Parse(name)
		if err != nil {
			return
		}
		typpkg.Walk(parsed, func(node typpkg.Type) {
			e.addScopedType(node.String(), visit)
		})
	}
	for _, fn := range e.module.Functions {
		visitFunctionTypes(fn, visit)
	}
	for _, name := range e.unitExternalFunctions() {
		fn := e.split.program.byName[name]
		visit(fn.Return)
		for _, param := range fn.Params {
			visit(param.Type)
		}
	}
}

// addScopedType records a struct or union the unit spells, and visits what
// it holds the first time.
func (e *emitter) addScopedType(name string, visit func(string)) {
	if e.typeScope[name] {
		return
	}
	if st, ok := e.module.Structs[name]; ok {
		e.typeScope[name] = true
		for _, field := range st.Fields {
			visit(field.Type)
		}
		return
	}
	if union, ok := e.module.Unions[name]; ok {
		e.typeScope[name] = true
		for _, variant := range sortedVariants(union) {
			visit(variant.Payload)
		}
	}
}

// sortedVariants returns a union's variants in tag order.
func sortedVariants(union ir.Union) []ir.UnionVariant {
	variants := make([]ir.UnionVariant, 0, len(union.Variants))
	for _, variant := range union.Variants {
		variants = append(variants, variant)
	}
	sort.Slice(variants, func(i int, j int) bool { return variants[i].Index < variants[j].Index })
	return variants
}

// visitFunctionTypes hands every type a function spells to visit: its
// signature, the values its instructions take and make, and the type
// `mem_size_of<T>` names in its immediate.
func visitFunctionTypes(fn *ir.Function, visit func(string)) {
	visit(fn.Return)
	for _, param := range fn.Params {
		visit(param.Type)
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			visit(instr.Result.Type)
			if instr.Op == "call."+memSizeOf {
				visit(instr.Immediate)
			}
			for _, arg := range instr.Args {
				visit(arg.Type)
			}
			for _, field := range instr.Fields {
				visit(field.Value.Type)
			}
			for _, incoming := range instr.Incoming {
				visit(incoming.Value.Type)
			}
			for _, param := range instr.CallParams {
				visit(param.Type)
			}
			for _, cleanup := range instr.Cleanups {
				for _, arg := range cleanup.Args {
					visit(arg.Type)
				}
			}
		}
		visit(block.Terminator.Value.Type)
		visit(block.Terminator.Cond.Type)
	}
}

// inTypeScope reports whether the unit writes the definition of a struct or
// union.
func (e *emitter) inTypeScope(name string) bool {
	return e.typeScope == nil || e.typeScope[name]
}
