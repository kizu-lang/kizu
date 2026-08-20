package llvm

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/ir"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Emit formats a typed SSA IR module as LLVM IR.
func Emit(module *ir.Module) (string, error) {
	e := &emitter{
		module:  module,
		strings: map[string]string{},
		values:  map[string]valueInfo{},
	}
	if err := e.emit(); err != nil {
		return "", err
	}
	text := strings.TrimRight(e.out.String(), "\n")
	if err := verifyEmittedText(text); err != nil {
		return "", err
	}
	return text, nil
}

type emitter struct {
	module          *ir.Module
	out             bytes.Buffer
	strings         map[string]string
	values          map[string]valueInfo
	functionNames   map[string]bool
	functionParams  map[string][]ir.Param
	currentReturn   string
	mainReturnsInt  bool
	nextLabel       int
	currentBlock    string
	blockExitLabel  map[string]string
	entryParamLoads []string
	wroteParamLoads bool
}

type valueInfo struct {
	typ     string
	operand string
}

// emit writes declarations and function definitions.
func (e *emitter) emit() error {
	e.collectFunctionNames()
	e.collectStrings()
	if err := e.validateModuleTypes(); err != nil {
		return err
	}
	e.writeHeader()
	for _, fn := range e.module.Functions {
		if err := e.writeFunction(fn); err != nil {
			return err
		}
	}
	return nil
}

// collectFunctionNames records module-local functions before call emission.
func (e *emitter) collectFunctionNames() {
	e.functionNames = map[string]bool{}
	e.functionParams = map[string][]ir.Param{}
	for _, fn := range e.module.Functions {
		e.functionNames[fn.Name] = true
		e.functionParams[fn.Name] = fn.Params
	}
}

// collectStrings assigns stable global names to string constants.
func (e *emitter) collectStrings() {
	next := 0
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "const" && instr.Result.Type == "[]u8" {
					if _, ok := e.strings[instr.Immediate]; !ok {
						e.strings[instr.Immediate] = fmt.Sprintf("@.str.%d", next)
						next++
					}
				}
			}
		}
	}
}

// writeHeader writes globals and runtime declarations.
func (e *emitter) writeHeader() {
	e.out.WriteString("; Kizu LLVM IR\n")
	if e.usesSliceABI() {
		e.out.WriteString("%kizu.slice.u8 = type { ptr, i64 }\n")
	}
	// Optionals go first: an `E!?T` union references its `?T` member.
	e.writeOptionalTypes()
	e.writeErrorUnionTypes()
	e.writeUnionTypes()
	e.writeStructTypes()
	e.writeEnumNameTables()
	e.writeErrorNameTable()
	for _, lit := range e.sortedStringLiterals() {
		name := e.strings[lit]
		unquoted, _ := strconv.Unquote(lit)
		fmt.Fprintf(&e.out, "%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n",
			name, len(unquoted)+1, escapeString(unquoted))
	}
	if len(e.strings) > 0 {
		e.out.WriteByte('\n')
	}
	e.out.WriteString("declare void @kizu_print_string(ptr, i64)\n")
	e.out.WriteString("declare void @kizu_print_int(i64)\n")
	e.out.WriteString("declare void @kizu_print_bool(i1)\n")
	if e.printsNamedTag() {
		e.out.WriteString("declare void @kizu_print_enum(ptr, i64, i64)\n")
	}
	e.out.WriteString("declare void @kizu_main_error_message(ptr, i64)\n\n")
	e.out.WriteString("declare void @kizu_runtime_init_args(i32, ptr)\n\n")
	e.writeArrayRuntimeDecls()
	e.writeMapRuntimeDecls()
	e.writeArenaRuntimeDecls()
	e.writeBoxRuntimeDecls()
	e.writeTestRuntimeDecls()
	e.writeExternalCallDecls()
	e.writePanicDecls()
}

// panicEntry is one runtime failure report: the entry that prints it and the
// values it takes. Every checked failure the backend can raise is listed here,
// and the wording for all of them lives in the runtime, so a failure reads the
// same however the program reached it.
type panicEntry struct {
	entry  string
	params []string
}

// panicEntries are the failures a backend can report, keyed by the name an
// instruction uses to select one. `cond_fail` takes its key from the IR; the
// other keys are chosen by the instruction that reports the failure.
var panicEntries = map[string]panicEntry{
	"bounds":       {entry: "kizu_panic_bounds", params: []string{"i64", "i64"}},
	"range":        {entry: "kizu_panic_range", params: []string{"i64", "i64", "i64"}},
	"array_empty":  {entry: "kizu_panic_array_empty"},
	"arena_empty":  {entry: "kizu_panic_arena_empty"},
	"arena_handle": {entry: "kizu_panic_arena_handle"},
	"arena_add":    {entry: "kizu_panic_arena_add"},
	"test_fail":    {entry: "kizu_panic_test_fail", params: []string{"ptr", "i64"}},
	"panic_fail":   {entry: "kizu_panic_fail", params: []string{"ptr", "i64"}},
	"expect_int":   {entry: "kizu_panic_expect_equal_int", params: []string{"i64", "i64"}},
	"expect_bool":  {entry: "kizu_panic_expect_equal_bool", params: []string{"i1", "i1"}},
	"expect_bytes": {
		entry:  "kizu_panic_expect_equal_bytes",
		params: []string{"ptr", "i64", "ptr", "i64"},
	},
}

// panicParams returns one entry's parameter types. Every entry takes the source
// line and column last, so any failure can say where it happened; the runtime
// omits the position when the line is zero.
func panicParams(key string) []string {
	return append(append([]string{}, panicEntries[key].params...), "i64", "i64")
}

// panicPosition renders the source position arguments for a failure call.
func panicPosition(span ast.Span) []string {
	return []string{
		fmt.Sprintf("i64 %d", span.Start.Line),
		fmt.Sprintf("i64 %d", span.Start.Column),
	}
}

// failureErrors are the error set members a std container returns as a failure
// *value* (a recoverable !T), as opposed to panicEntries, which abort. The
// member is named here; its number comes from the set declaration in std.
var failureErrors = map[string]struct{ set, member string }{
	"array_append":   {"std::array::Error", "OutOfMemory"},
	"array_bounds":   {"std::array::Error", "OutOfBounds"},
	"array_reserve":  {"std::array::Error", "OutOfMemory"},
	"array_truncate": {"std::array::Error", "OutOfBounds"},
	"map_insert":     {"std::map::Error", "OutOfMemory"},
	"box_new":        {"std::mem::Error", "OutOfMemory"},
}

// failureErrorCode returns the global code one container failure lowers to.
func (e *emitter) failureErrorCode(key string) (int, error) {
	target, ok := failureErrors[key]
	if !ok {
		return 0, fmt.Errorf("llvm error: unknown failure `%s`", key)
	}
	set, ok := e.module.ErrorSets[target.set]
	if !ok {
		return 0, fmt.Errorf("llvm error: failure `%s` needs error set `%s`", key, target.set)
	}
	code, ok := set.Tags[target.member]
	if !ok {
		return 0, fmt.Errorf("llvm error: error set `%s` has no member `%s`",
			target.set, target.member)
	}
	return code, nil
}

// errorNameTable is the global holding every error code's spelling.
const errorNameTable = "@.kizu.error.names"

// needsErrorNameTable reports whether any code in the module reads a spelling
// out of the table: a failure leaving `main`, or a printed error value.
func (e *emitter) needsErrorNameTable() bool {
	return len(e.module.ErrorSets) > 0 || len(e.sortedErrorUnionNames()) > 0
}

// errorNameTableRows returns how many rows the error name table holds. The
// table is indexed by the error code itself, so it spans 0..max, and row 0 is
// the reserved "no error" entry.
func (e *emitter) errorNameTableRows() int {
	max := 0
	for _, set := range e.module.ErrorSets {
		for _, code := range set.Tags {
			if code > max {
				max = code
			}
		}
	}
	return max + 1
}

// writeErrorNameTable defines the spelling of every error code this module can
// carry, indexed by the code itself. Codes are global, so one table serves all
// sets; row 0 is the reserved "no error" entry and stays empty.
func (e *emitter) writeErrorNameTable() {
	if !e.needsErrorNameTable() {
		return
	}
	spellings := map[int]string{}
	for _, set := range e.module.ErrorSets {
		for member, code := range set.Tags {
			spellings[code] = set.Name + "::" + member
		}
	}
	rows := make([]string, 0, e.errorNameTableRows())
	for code := 0; code < e.errorNameTableRows(); code++ {
		spelling, ok := spellings[code]
		if !ok {
			rows = append(rows, "{ ptr, i64 } { ptr null, i64 0 }")
			continue
		}
		global := fmt.Sprintf("@.kizu.error.name.%d", code)
		e.writeStaticStringGlobal(global, spelling)
		rows = append(rows,
			fmt.Sprintf("{ ptr, i64 } { ptr %s, i64 %d }", global, len(spelling)))
	}
	fmt.Fprintf(&e.out, "%s = private unnamed_addr constant [%d x { ptr, i64 }] [%s]\n\n",
		errorNameTable, len(rows), strings.Join(rows, ", "))
}

// writePanicDecls declares the failure entries this module reports through.
func (e *emitter) writePanicDecls() {
	keys := e.usedPanicEntries()
	for _, key := range keys {
		fmt.Fprintf(&e.out, "declare void @%s(%s)\n",
			panicEntries[key].entry, strings.Join(panicParams(key), ", "))
	}
	if len(keys) > 0 {
		e.out.WriteString("\n")
	}
}

// usedPanicEntries returns the failure keys the module reports, sorted.
func (e *emitter) usedPanicEntries() []string {
	seen := map[string]bool{}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				for _, key := range instrPanicEntries(instr) {
					seen[key] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// instrPanicEntries returns the failure keys one instruction can report.
func instrPanicEntries(instr *ir.Instr) []string {
	switch instr.Op {
	case "cond_fail":
		return []string{instr.Immediate}
	case "array.get_or_panic", "map.take_value_at":
		return []string{"bounds"}
	case "array.pop_or_panic":
		return []string{"array_empty"}
	case "arena.pop_or_panic":
		return []string{"arena_empty"}
	case "arena.at":
		return []string{"arena_handle"}
	case "arena.add":
		return []string{"arena_add"}
	case "test.fail":
		return []string{"test_fail"}
	case "panic.fail":
		return []string{"panic_fail"}
	case "test.expect_equal":
		return []string{expectEntryKey(instr)}
	default:
		return nil
	}
}

// expectEntryKey selects the reporting entry for one equality assertion.
func expectEntryKey(instr *ir.Instr) string {
	if len(instr.Args) == 0 {
		return "expect_int"
	}
	switch instr.Args[0].Type {
	case "bool":
		return "expect_bool"
	case "[]u8":
		return "expect_bytes"
	default:
		return "expect_int"
	}
}

// writeErrorUnionTypes writes named recoverable-result ABI definitions.
func (e *emitter) writeErrorUnionTypes() {
	names := e.sortedErrorUnionNames()
	for _, name := range names {
		_, success, _ := errorUnionParts(name)
		failureType := "i64"
		if success == "void" {
			fmt.Fprintf(&e.out, "%s = type { i8, %s }\n",
				llvmErrorUnionTypeName(name), failureType)
			continue
		}
		fmt.Fprintf(&e.out, "%s = type { i8, %s, %s }\n",
			llvmErrorUnionTypeName(name), e.llvmType(success), failureType)
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
}

// writeOptionalTypes writes named optional-value ABI definitions: a presence
// tag and the payload, the layout `!T` success already uses.
func (e *emitter) writeOptionalTypes() {
	names := e.sortedOptionalNames()
	for _, name := range names {
		elem, _ := optionalElemLLVM(name)
		fmt.Fprintf(&e.out, "%s = type { i8, %s }\n",
			llvmOptionalTypeName(name), e.llvmType(elem))
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
}

// sortedOptionalNames returns every optional type this module spells.
func (e *emitter) sortedOptionalNames() []string {
	return e.collectModuleTypeNames(collectOptionalName)
}

// collectOptionalName records name when it is an optional value type, or an
// error union whose success is one (`E!?T` defines `?T`'s aggregate too).
func collectOptionalName(seen map[string]bool, name string) {
	if _, ok := optionalElemLLVM(name); ok {
		seen[name] = true
		return
	}
	// Only an error union can hold an optional, and every error-union
	// spelling contains `!`; the byte check keeps this walk parse-free for
	// the scalar names that dominate a module.
	if !strings.Contains(name, "!") {
		return
	}
	if success, ok := errorUnionSuccessType(name); ok {
		collectOptionalName(seen, success)
	}
}

// optionalElemLLVM returns T for an optional value type `?T`.
func optionalElemLLVM(name string) (string, bool) {
	return typ.OptionalElem(name)
}

// writeBorrowOptionalResult wraps a nullable runtime pointer into a borrow
// optional `{i8, ptr}` result, branch-free: the pointer becomes the payload
// and its non-nullness the presence tag. Array.at, Map.at, and Arena.at_mut
// all end here.
func (e *emitter) writeBorrowOptionalResult(instr *ir.Instr, ptrName string) error {
	if _, ok := optionalElemLLVM(instr.Result.Type); !ok {
		return fmt.Errorf(
			"llvm error: %s expects a borrow optional result, got %s",
			instr.Op, instr.Result.Type)
	}
	resultName := localName(instr.Result.Name)
	optType := e.llvmType(instr.Result.Type)
	liveName := resultName + ".live"
	tagName := resultName + ".tag"
	someName := resultName + ".some"
	fmt.Fprintf(&e.out, "  %s = icmp ne ptr %s, null\n", liveName, ptrName)
	fmt.Fprintf(&e.out, "  %s = zext i1 %s to i8\n", tagName, liveName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 %s, 0\n",
		someName, optType, tagName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, ptr %s, 1\n",
		resultName, optType, someName, ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// llvmOptionalTypeName names the LLVM aggregate of one optional type.
func llvmOptionalTypeName(name string) string {
	elem, _ := optionalElemLLVM(name)
	return "%kizu.opt." + llvmNamePart(elem)
}

// writeExternalCallDecls declares runtime calls that are not defined in the module.
func (e *emitter) writeExternalCallDecls() {
	decls := e.externalCallDecls()
	for _, decl := range decls {
		e.out.WriteString(decl)
		e.out.WriteByte('\n')
	}
	if len(decls) > 0 {
		e.out.WriteByte('\n')
	}
}

// externalCallDecls returns stable declarations for external call instructions.
func (e *emitter) externalCallDecls() []string {
	defined := map[string]bool{}
	for _, fn := range e.module.Functions {
		defined[fn.Name] = true
	}
	seen := map[string]string{}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if !strings.HasPrefix(instr.Op, "call.") {
					continue
				}
				name := strings.TrimPrefix(instr.Op, "call.")
				if name == "print" || defined[name] {
					continue
				}
				seen[name] = e.externalCallDecl(name, instr)
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	decls := make([]string, 0, len(names))
	for _, name := range names {
		decls = append(decls, seen[name])
	}
	return decls
}

// externalCallDecl formats one external call declaration from typed IR operands.
func (e *emitter) externalCallDecl(name string, instr *ir.Instr) string {
	if e.usesHostedRuntimeABI(name, instr) {
		params := []string{"ptr"}
		params = append(params, e.hostedRuntimeParamTypes(instr.Args)...)
		return fmt.Sprintf(
			"declare void @%s(%s)",
			llvmFunctionName(name),
			strings.Join(params, ", "),
		)
	}
	params := make([]string, 0, len(instr.Args))
	for _, arg := range instr.Args {
		params = append(params, e.llvmType(arg.Type))
	}
	return fmt.Sprintf(
		"declare %s @%s(%s)",
		e.llvmType(instr.Result.Type),
		llvmFunctionName(name),
		strings.Join(params, ", "),
	)
}

// writeStructTypes writes named LLVM aggregate definitions for declared structs.
func (e *emitter) writeStructTypes() {
	names := e.sortedStructNames()
	for _, name := range names {
		st := e.module.Structs[name]
		fields := make([]string, 0, len(st.Fields))
		for _, field := range st.Fields {
			fields = append(fields, e.llvmType(field.Type))
		}
		fmt.Fprintf(&e.out, "%s = type { %s }\n",
			llvmStructTypeName(name), strings.Join(fields, ", "))
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
}

// writeUnionTypes writes the #991 `tag + inline payload storage` layout for
// each declared tagged union: `{ i64, [N x i8] }`, where N is the byte capacity
// of the largest variant payload. Payload shapes are validated in
// validateModuleTypes, so the capacity is known here.
func (e *emitter) writeUnionTypes() {
	names := e.sortedUnionNames()
	for _, name := range names {
		capacity, _ := e.unionPayloadCapacity(name)
		fmt.Fprintf(&e.out, "%s = type { i64, [%d x i8] }\n", llvmUnionTypeName(name), capacity)
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
}

// sortedStringLiterals returns string constants in global-name order.
func (e *emitter) sortedStringLiterals() []string {
	literals := make([]string, 0, len(e.strings))
	for lit := range e.strings {
		literals = append(literals, lit)
	}
	sort.Slice(literals, func(i int, j int) bool {
		return e.strings[literals[i]] < e.strings[literals[j]]
	})
	return literals
}

// sortedStructNames returns declared structs in stable order.
func (e *emitter) sortedStructNames() []string {
	names := make([]string, 0, len(e.module.Structs))
	for name := range e.module.Structs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sortedUnionNames returns declared unions in stable order.
func (e *emitter) sortedUnionNames() []string {
	names := make([]string, 0, len(e.module.Unions))
	for name := range e.module.Unions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// usesSliceABI reports whether this module needs the byte-slice value ABI.
func (e *emitter) usesSliceABI() bool {
	return e.moduleHasSliceType()
}

// moduleHasSliceType reports whether any non-error-union value uses []u8.
func (e *emitter) moduleHasSliceType() bool {
	for _, st := range e.module.Structs {
		for _, field := range st.Fields {
			if isSliceType(field.Type) {
				return true
			}
		}
	}
	for _, fn := range e.module.Functions {
		if e.functionHasSliceType(fn) {
			return true
		}
	}
	return false
}

// functionHasSliceType reports whether a function body/signature mentions []u8.
func (e *emitter) functionHasSliceType(fn *ir.Function) bool {
	if isSliceType(fn.Return) {
		return true
	}
	for _, param := range fn.Params {
		if isSliceType(param.Type) {
			return true
		}
	}
	for _, block := range fn.Blocks {
		if blockHasSliceType(block) {
			return true
		}
	}
	return false
}

// blockHasSliceType reports whether a block uses a []u8 value.
func blockHasSliceType(block *ir.Block) bool {
	for _, instr := range block.Instrs {
		if instrHasSliceType(instr) {
			return true
		}
	}
	return isSliceType(block.Terminator.Value.Type) || isSliceType(block.Terminator.Cond.Type)
}

// instrHasSliceType reports whether one instruction uses a []u8 value.
func instrHasSliceType(instr *ir.Instr) bool {
	if isSliceType(instr.Result.Type) {
		return true
	}
	for _, arg := range instr.Args {
		if isSliceType(arg.Type) {
			return true
		}
	}
	for _, field := range instr.Fields {
		if isSliceType(field.Value.Type) {
			return true
		}
	}
	for _, incoming := range instr.Incoming {
		if isSliceType(incoming.Value.Type) {
			return true
		}
	}
	return false
}

// isSliceType reports whether a lowered IR type mentions the byte-slice value
// type, itself or inside a wrapper: `?[]u8` and `![]u8` aggregates embed
// %kizu.slice.u8, so a module whose only slice sits inside one still needs
// the declaration.
func isSliceType(typ string) bool {
	return strings.Contains(typ, "[]u8")
}

// sortedErrorUnionNames returns all error-union types referenced by this module.
func (e *emitter) sortedErrorUnionNames() []string {
	return e.collectModuleTypeNames(e.collectErrorUnionName)
}

// collectModuleTypeNames hands every type spelling the module holds to
// collect, then returns the recorded names sorted. One walk serves every
// named-type collector, so none of them can drop a position the others scan.
func (e *emitter) collectModuleTypeNames(collect func(map[string]bool, string)) []string {
	seen := map[string]bool{}
	for _, st := range e.module.Structs {
		for _, field := range st.Fields {
			collect(seen, field.Type)
		}
	}
	for _, fn := range e.module.Functions {
		collect(seen, fn.Return)
		for _, param := range fn.Params {
			collect(seen, param.Type)
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				collect(seen, instr.Result.Type)
				for _, arg := range instr.Args {
					collect(seen, arg.Type)
				}
				for _, field := range instr.Fields {
					collect(seen, field.Value.Type)
				}
				for _, incoming := range instr.Incoming {
					collect(seen, incoming.Value.Type)
				}
			}
			collect(seen, block.Terminator.Value.Type)
			collect(seen, block.Terminator.Cond.Type)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validateModuleTypes rejects unsupported ABI shapes before header emission.
func (e *emitter) validateModuleTypes() error {
	for _, name := range e.sortedErrorUnionNames() {
		if err := validateErrorUnionType(name); err != nil {
			return err
		}
	}
	for _, name := range e.sortedUnionNames() {
		if _, err := e.unionPayloadCapacity(name); err != nil {
			return err
		}
	}
	return nil
}

// unionPayloadCapacity returns the inline payload byte capacity for a declared
// union, or a #991 diagnostic when a variant payload is an unsupported shape.
func (e *emitter) unionPayloadCapacity(name string) (int, error) {
	union, ok := e.module.Unions[name]
	if !ok {
		return 0, fmt.Errorf("llvm error: unknown union `%s`", name)
	}
	capacity, align, ok := e.unionPayloadStorage(name, union, nil)
	if !ok {
		return 0, fmt.Errorf(
			"llvm error: union `%s` has an unsupported tagged-union payload shape; "+
				"inline payload size/alignment must be compile-time known per the #991 ABI "+
				"(broader shapes tracked by #495)",
			name,
		)
	}
	if align > maxInlinePayloadAlign {
		return 0, fmt.Errorf(
			"llvm error: union `%s` requires payload alignment %d, but the #991 inline "+
				"payload storage only guarantees alignment %d",
			name, align, maxInlinePayloadAlign,
		)
	}
	return capacity, nil
}

// collectErrorUnionName records concrete !T / Error!T type names.
func (e *emitter) collectErrorUnionName(seen map[string]bool, name string) {
	parsed, err := typ.Parse(name)
	if err != nil {
		return
	}
	// An error union nested in a static argument needs its named type too:
	// `std::array::Array<!i64>` stores `!i64`, so the element has to be sized.
	typ.Walk(parsed, func(node typ.Type) {
		if _, ok := node.(*typ.ErrorUnion); ok {
			seen[node.String()] = true
		}
	})
}

// writeFunction writes one LLVM function.
func (e *emitter) writeFunction(fn *ir.Function) error {
	if err := e.validateFunctionTypes(fn); err != nil {
		return err
	}
	e.values = map[string]valueInfo{}
	e.currentReturn = fn.Return
	e.nextLabel = 0
	e.currentBlock = ""
	e.blockExitLabel = map[string]string{}
	e.entryParamLoads = nil
	e.wroteParamLoads = false
	e.precomputeBlockExitLabels(fn)
	returnType := e.llvmType(fn.Return)
	_, returnsErrorUnion := errorUnionSuccessType(fn.Return)
	e.mainReturnsInt = fn.Name == "main" && (fn.Return == "void" || returnsErrorUnion)
	params := make([]string, 0, len(fn.Params))
	if e.mainReturnsInt {
		returnType = "i32"
		params = []string{"i32 %kizu.argc", "ptr %kizu.argv"}
	} else {
		for _, param := range fn.Params {
			params = append(params, e.functionParamABI(param))
			e.values[param.Name] = valueInfo{typ: param.Type, operand: localName(param.Name)}
		}
	}
	e.registerForwardedValues(fn)
	fmt.Fprintf(&e.out,
		"define %s @%s(%s) {\n",
		returnType,
		llvmFunctionName(fn.Name),
		strings.Join(params, ", "),
	)
	bodyStart := e.out.Len()
	for _, block := range fn.Blocks {
		if err := e.writeBlock(block); err != nil {
			return fmt.Errorf("llvm error: function `%s` block `%s`: %w",
				fn.Name, block.Name, err)
		}
	}
	body := e.out.String()[bodyStart:]
	hoisted, err := hoistAllocasToEntry(body)
	if err != nil {
		return fmt.Errorf("llvm error: function `%s`: %w", fn.Name, err)
	}
	e.out.Truncate(bodyStart)
	e.out.WriteString(hoisted)
	e.out.WriteString("}\n\n")
	e.mainReturnsInt = false
	e.currentReturn = ""
	e.currentBlock = ""
	e.entryParamLoads = nil
	e.wroteParamLoads = false
	return nil
}

// functionParamABI returns the LLVM ABI parameter spelling for one Kizu value.
func (e *emitter) functionParamABI(param ir.Param) string {
	paramType := e.llvmType(param.Type)
	paramName := localName(param.Name)
	if !e.usesIndirectStructParamABI(param.Type) {
		return paramType + " " + paramName
	}
	addrName := paramName + ".addr"
	e.entryParamLoads = append(
		e.entryParamLoads,
		fmt.Sprintf("  %s = load %s, ptr %s\n", paramName, paramType, addrName),
	)
	return fmt.Sprintf("ptr byval(%s) %s", paramType, addrName)
}

// registerForwardedValues resolves every result that never becomes an LLVM
// instruction — scalar constants, no-op casts, borrow-shaped box reads —
// before any block is written. A match merge block is written before the arms
// that feed it, and a phi that reads a forwarded value would otherwise name a
// register nothing ever defines.
func (e *emitter) registerForwardedValues(fn *ir.Function) {
	defs := map[string]*ir.Instr{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Result.Name != "" {
				defs[instr.Result.Name] = instr
			}
		}
	}
	var operandOf func(value ir.Value) string
	operandOf = func(value ir.Value) string {
		instr, ok := defs[value.Name]
		if !ok || !e.forwardsOperand(instr) {
			return localName(value.Name)
		}
		if instr.Op == "const" {
			operand, _ := e.scalarConstOperand(instr)
			return operand
		}
		return operandOf(instr.Args[0])
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if !e.forwardsOperand(instr) {
				continue
			}
			e.values[instr.Result.Name] = valueInfo{
				typ:     instr.Result.Type,
				operand: operandOf(instr.Result),
			}
		}
	}
}

// forwardsOperand reports whether an instruction emits no LLVM instruction and
// instead hands an operand through under a new name. Its cases must stay the
// exact set of writes that register a value without printing a definition.
func (e *emitter) forwardsOperand(instr *ir.Instr) bool {
	switch instr.Op {
	case "const":
		_, ok := e.scalarConstOperand(instr)
		return ok
	case "cast":
		return len(instr.Args) == 1 && castForwardsOperand(instr)
	case "box.borrow", "box.borrow_mut":
		return len(instr.Args) == 1 && strings.HasPrefix(instr.Result.Type, "&")
	}
	return false
}

// validateFunctionTypes rejects ABI shapes this backend cannot lower faithfully.
func (e *emitter) validateFunctionTypes(fn *ir.Function) error {
	if err := validateErrorUnionType(fn.Return); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if err := validateErrorUnionType(param.Type); err != nil {
			return err
		}
	}
	return nil
}

// validateErrorUnionType checks the supported error-union ABI subset.
func validateErrorUnionType(typ string) error {
	success, ok := errorUnionSuccessType(typ)
	if !ok {
		return nil
	}
	if !isLowerableErrorUnionSuccess(success) {
		return fmt.Errorf(
			"llvm error: error union success type `%s` is not supported by the LLVM backend yet",
			success,
		)
	}
	return nil
}

// writeBlock writes one LLVM basic block.
func (e *emitter) writeBlock(block *ir.Block) error {
	fmt.Fprintf(&e.out, "%s:\n", block.Name)
	e.currentBlock = block.Name
	if !e.wroteParamLoads {
		for _, load := range e.entryParamLoads {
			e.out.WriteString(load)
		}
		e.wroteParamLoads = true
	}
	if e.mainReturnsInt && block.Name == "entry" {
		e.out.WriteString("  call void @kizu_runtime_init_args(i32 %kizu.argc, ptr %kizu.argv)\n")
	}
	for _, instr := range block.Instrs {
		if err := e.writeInstr(instr); err != nil {
			return fmt.Errorf("llvm error: block `%s`: %w", block.Name, err)
		}
	}
	if err := e.writeTerminator(block.Terminator); err != nil {
		return fmt.Errorf("llvm error: block `%s`: %w", block.Name, err)
	}
	return nil
}

// writeInstr writes one LLVM instruction.
func (e *emitter) writeInstr(instr *ir.Instr) error {
	switch {
	case instr.Op == "const":
		return e.writeConst(instr)
	case strings.HasPrefix(instr.Op, "binary."):
		return e.writeBinary(instr)
	case strings.HasPrefix(instr.Op, "unary."):
		return e.writeUnary(instr)
	case strings.HasPrefix(instr.Op, "call."):
		return e.writeCall(instr)
	case instr.Op == "cast":
		return e.writeCast(instr)
	case instr.Op == "phi":
		return e.writePhi(instr)
	case instr.Op == "struct.new":
		return e.writeStructNew(instr)
	case strings.HasPrefix(instr.Op, "field."):
		return e.writeFieldInstr(instr)
	case instr.Op == "ref.store", instr.Op == "volatile.store":
		return e.writeRefStore(instr)
	case instr.Op == "ref.load", instr.Op == "volatile.load":
		return e.writeRefLoad(instr)
	case instr.Op == "local.slot":
		return e.writeLocalSlot(instr)
	case instr.Op == "buffer.new":
		return e.writeBufferNew(instr)
	case instr.Op == "buffer.as_bytes":
		return e.writeBufferAsBytes(instr)
	case instr.Op == "cond_fail":
		return e.writeCondFail(instr)
	default:
		return e.writeRuntimeInstr(instr)
	}
}

// writeRuntimeInstr dispatches instructions backed by hosted runtime helpers.
func (e *emitter) writeRuntimeInstr(instr *ir.Instr) error {
	switch {
	case strings.HasPrefix(instr.Op, "array."):
		return e.writeArrayInstr(instr)
	case strings.HasPrefix(instr.Op, "map."):
		return e.writeMapInstr(instr)
	case strings.HasPrefix(instr.Op, "arena."):
		return e.writeArenaInstr(instr)
	case strings.HasPrefix(instr.Op, "box."):
		return e.writeBoxInstr(instr)
	case strings.HasPrefix(instr.Op, "union."):
		return e.writeUnionInstr(instr)
	case strings.HasPrefix(instr.Op, "test."):
		return e.writeTestInstr(instr)
	case instr.Op == "panic.fail":
		return e.writePanicFail(instr)
	case strings.HasPrefix(instr.Op, "slice."):
		return e.writeSliceInstr(instr)
	case strings.HasPrefix(instr.Op, "error."):
		return e.writeErrorInstr(instr)
	case strings.HasPrefix(instr.Op, "opt."):
		return e.writeOptInstr(instr)
	default:
		return fmt.Errorf("llvm error: unsupported instruction `%s`", instr.Op)
	}
}

// writeOptInstr dispatches optional-value operations.
func (e *emitter) writeOptInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "opt.some":
		return e.writeOptSome(instr)
	case "opt.null":
		return e.writeOptNull(instr)
	case "opt.has":
		return e.writeOptHas(instr)
	case "opt.value":
		return e.writeOptValue(instr)
	default:
		return fmt.Errorf("llvm error: unsupported optional instruction `%s`", instr.Op)
	}
}

// writeOptSome wraps a payload into a present optional.
func (e *emitter) writeOptSome(instr *ir.Instr) error {
	elem, ok := optionalElemLLVM(instr.Result.Type)
	if !ok || len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: opt.some expects one payload and a `?T` result")
	}
	resultName := localName(instr.Result.Name)
	optType := e.llvmType(instr.Result.Type)
	someName := resultName + ".some"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n",
		someName, optType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		resultName, optType, someName, e.llvmType(elem), e.value(instr.Args[0]).operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeOptNull builds the empty optional.
func (e *emitter) writeOptNull(instr *ir.Instr) error {
	if _, ok := optionalElemLLVM(instr.Result.Type); !ok || len(instr.Args) != 0 {
		return fmt.Errorf("llvm error: opt.null expects no args and a `?T` result")
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 0, 0\n",
		resultName, e.llvmType(instr.Result.Type))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeOptHas tests whether an optional holds a value.
func (e *emitter) writeOptHas(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: opt.has expects 1 arg")
	}
	source := instr.Args[0]
	if _, ok := optionalElemLLVM(source.Type); !ok {
		return fmt.Errorf("llvm error: opt.has expects `?T`, got %s", source.Type)
	}
	resultName := localName(instr.Result.Name)
	tagName := resultName + ".tag"
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n",
		tagName, e.llvmType(source.Type), e.value(source).operand)
	fmt.Fprintf(&e.out, "  %s = icmp ne i8 %s, 0\n", resultName, tagName)
	e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: resultName}
	return nil
}

// writeOptValue extracts the payload of an optional. The caller has already
// branched on presence; an absent payload is only ever produced, never read.
func (e *emitter) writeOptValue(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: opt.value expects 1 arg")
	}
	source := instr.Args[0]
	elem, ok := optionalElemLLVM(source.Type)
	if !ok {
		return fmt.Errorf("llvm error: opt.value expects `?T`, got %s", source.Type)
	}
	if instr.Result.Type != elem {
		return fmt.Errorf("llvm error: opt.value returns %s, got %s", elem, instr.Result.Type)
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n",
		resultName, e.llvmType(source.Type), e.value(source).operand)
	e.values[instr.Result.Name] = valueInfo{typ: elem, operand: resultName}
	return nil
}

// writeFieldInstr dispatches struct field reads and writes. The borrowed-write
// prefix is checked before the borrowed-read one because it extends it.
func (e *emitter) writeFieldInstr(instr *ir.Instr) error {
	switch {
	case strings.HasPrefix(instr.Op, "field.ref.set."):
		return e.writeFieldRefSet(instr)
	case strings.HasPrefix(instr.Op, "field.ref."):
		return e.writeFieldRef(instr)
	case strings.HasPrefix(instr.Op, "field.addr."):
		return e.writeFieldAddr(instr)
	case strings.HasPrefix(instr.Op, "field.set."):
		return e.writeFieldSet(instr)
	default:
		return e.writeField(instr)
	}
}

// writeErrorInstr dispatches recoverable-result operations.
func (e *emitter) writeErrorInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "error.ok":
		return e.writeErrorOK(instr)
	case "error.error":
		return e.writeErrorError(instr)
	case "error.try":
		return e.writeErrorTry(instr)
	default:
		return fmt.Errorf("llvm error: unsupported error instruction `%s`", instr.Op)
	}
}

// writeSliceInstr dispatches checked byte-slice operations.
func (e *emitter) writeSliceInstr(instr *ir.Instr) error {
	switch instr.Op {
	case "slice.len":
		return e.writeSliceLen(instr)
	case "slice.index":
		return e.writeSliceIndex(instr)
	case "slice.store":
		return e.writeSliceStore(instr)
	case "slice.slice":
		return e.writeSliceSlice(instr)
	default:
		return fmt.Errorf("llvm error: unsupported slice instruction `%s`", instr.Op)
	}
}

// writeSliceStore writes one byte through a writable view. The bounds test
// reaches the backend as preceding cond_fail instructions, so no check is
// generated here.
func (e *emitter) writeSliceStore(instr *ir.Instr) error {
	if len(instr.Args) != 3 ||
		instr.Args[0].Type != "[]u8" ||
		instr.Args[1].Type != "i64" ||
		instr.Args[2].Type != "u8" {
		return fmt.Errorf("llvm error: slice.store expects []u8, i64, u8")
	}
	slice := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	byteValue := e.value(instr.Args[2])
	base := "%" + e.nextSyntheticValue("slice.store")
	ptrName := base + ".ptr"
	elemPtrName := base + ".elem.ptr"
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 0\n", ptrName, slice.operand)
	fmt.Fprintf(&e.out, "  %s = getelementptr i8, ptr %s, i64 %s\n",
		elemPtrName, ptrName, index.operand)
	fmt.Fprintf(&e.out, "  store i8 %s, ptr %s\n", byteValue.operand, elemPtrName)
	return nil
}

// scalarConstOperand returns the LLVM immediate for a constant whose value is
// a bare scalar: integers at whatever width the type says, bool, enums, and
// error set members.
func (e *emitter) scalarConstOperand(instr *ir.Instr) (string, bool) {
	typ := instr.Result.Type
	if _, ok := e.module.Enums[typ]; ok {
		return instr.Immediate, true
	}
	if _, ok := e.module.ErrorSets[typ]; ok {
		return instr.Immediate, true
	}
	if _, ok := integerBitWidth(typ); ok {
		return instr.Immediate, true
	}
	if typ == "bool" {
		return llvmBool(instr.Immediate), true
	}
	return "", false
}

// writeConst writes scalar and string constants.
func (e *emitter) writeConst(instr *ir.Instr) error {
	if operand, ok := e.scalarConstOperand(instr); ok {
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: operand}
		return nil
	}
	switch instr.Result.Type {
	case "[]u8":
		unquoted, _ := strconv.Unquote(instr.Immediate)
		global := e.strings[instr.Immediate]
		name := localName(instr.Result.Name)
		ptrName := name + ".ptr"
		baseName := name + ".base"
		fmt.Fprintf(&e.out, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n",
			ptrName, len(unquoted)+1, global)
		fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 poison, ptr %s, 0\n",
			baseName, ptrName)
		fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 %s, i64 %d, 1\n",
			name, baseName, len(unquoted))
		e.values[instr.Result.Name] = valueInfo{typ: "[]u8", operand: name}
	default:
		return fmt.Errorf("llvm error: unsupported const type `%s`", instr.Result.Type)
	}
	return nil
}

// writeBinary writes arithmetic and comparison instructions.
func (e *emitter) writeBinary(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: binary expects 2 args")
	}
	left := e.value(instr.Args[0])
	right := e.value(instr.Args[1])
	op := strings.TrimPrefix(instr.Op, "binary.")
	if instr.Result.Type == "bool" {
		if e.llvmType(instr.Args[0].Type) != e.llvmType(instr.Args[1].Type) {
			return fmt.Errorf(
				"llvm error: binary %s operand type mismatch: %s and %s",
				op,
				instr.Args[0].Type,
				instr.Args[1].Type,
			)
		}
		pred := llvmTypedPredicate(op, instr.Args[0].Type)
		name := localName(instr.Result.Name)
		fmt.Fprintf(&e.out, "  %s = icmp %s %s %s, %s\n",
			name, pred, e.llvmType(instr.Args[0].Type), left.operand, right.operand)
		e.values[instr.Result.Name] = valueInfo{typ: "bool", operand: name}
		return nil
	}
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s %s %s, %s\n",
		name, llvmBinaryOp(op), e.llvmType(instr.Result.Type), left.operand, right.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeUnary writes boolean and integer unary operations.
func (e *emitter) writeUnary(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: unary expects 1 arg")
	}
	value := e.value(instr.Args[0])
	name := localName(instr.Result.Name)
	op := strings.TrimPrefix(instr.Op, "unary.")
	switch op {
	case "!":
		if instr.Result.Type != "bool" {
			return fmt.Errorf("llvm error: unary ! expects bool")
		}
		fmt.Fprintf(&e.out, "  %s = xor i1 %s, true\n", name, value.operand)
	case "-":
		fmt.Fprintf(&e.out, "  %s = sub %s 0, %s\n",
			name, e.llvmType(instr.Result.Type), value.operand)
	default:
		return fmt.Errorf("llvm error: unsupported unary `%s`", op)
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeCall writes runtime print and user function calls.
func (e *emitter) writeCall(instr *ir.Instr) error {
	name := strings.TrimPrefix(instr.Op, "call.")
	if name == "print" {
		return e.writePrint(instr.Args)
	}
	if e.usesHostedRuntimeABI(name, instr) {
		return e.writeHostedRuntimeCall(name, instr)
	}
	if e.functionNames[name] {
		return e.writeInternalCall(name, instr)
	}
	args := make([]string, 0, len(instr.Args))
	for _, arg := range instr.Args {
		value := e.value(arg)
		args = append(args, e.llvmType(arg.Type)+" "+value.operand)
	}
	call := fmt.Sprintf(
		"call %s @%s(%s)",
		e.llvmType(instr.Result.Type),
		llvmFunctionName(name),
		strings.Join(args, ", "),
	)
	if instr.Result.Type == "void" {
		fmt.Fprintf(&e.out, "  %s\n", call)
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s\n", resultName, call)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeInternalCall adapts module-local struct values to Kizu's explicit
// byval pointer ABI, avoiding target-dependent aggregate argument lowering.
func (e *emitter) writeInternalCall(name string, instr *ir.Instr) error {
	args := make([]string, 0, len(instr.Args))
	params := e.functionParams[name]
	for index, arg := range instr.Args {
		// A callee this module does not declare takes its arguments by value,
		// which is what the zero Param says.
		param := ir.Param{}
		if index < len(params) {
			param = params[index]
		}
		callArg, err := e.internalCallArg(arg, param, index)
		if err != nil {
			return err
		}
		args = append(args, callArg)
	}
	call := fmt.Sprintf(
		"call %s @%s(%s)",
		e.llvmType(instr.Result.Type),
		llvmFunctionName(name),
		strings.Join(args, ", "),
	)
	if instr.Result.Type == "void" {
		fmt.Fprintf(&e.out, "  %s\n", call)
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s\n", resultName, call)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// internalCallArg returns one module-local call argument in functionParamABI form.
// A parameter handed the value its address is taken of gets that address made
// here, which is the one thing the callee's declared Passing decides.
func (e *emitter) internalCallArg(arg ir.Value, param ir.Param, index int) (string, error) {
	value := e.value(arg)
	argType := e.llvmType(arg.Type)
	if param.TakesAddressOf(arg.Type) {
		slotName := "%" + e.nextSyntheticValue(fmt.Sprintf("arg.%d", index))
		fmt.Fprintf(&e.out, "  %s = alloca %s, align %d\n",
			slotName, argType, maxInlinePayloadAlign)
		fmt.Fprintf(&e.out, "  store %s %s, ptr %s, align %d\n",
			argType, value.operand, slotName, maxInlinePayloadAlign)
		return "ptr " + slotName, nil
	}
	if !e.usesIndirectStructParamABI(arg.Type) {
		return argType + " " + value.operand, nil
	}
	slotName := "%" + e.nextSyntheticValue(fmt.Sprintf("arg.%d", index))
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slotName, argType)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n", argType, value.operand, slotName)
	return fmt.Sprintf("ptr byval(%s) %s", argType, slotName), nil
}

// usesHostedRuntimeABI reports whether a std hosted runtime call uses the
// explicit out-pointer ABI instead of the platform C aggregate return ABI.
// Error unions and optionals are both C-side aggregates, so both go out
// through the pointer.
func (e *emitter) usesHostedRuntimeABI(name string, instr *ir.Instr) bool {
	if !strings.HasPrefix(llvmFunctionName(name), "std__internal__builtin__") {
		return false
	}
	if _, ok := errorUnionSuccessType(instr.Result.Type); ok {
		return true
	}
	_, ok := optionalElemLLVM(instr.Result.Type)
	return ok
}

// hostedRuntimeParamTypes returns the lowered parameter ABI for hosted runtime
// calls. Aggregates -- slices and module structs -- reach the C runtime behind
// a pointer, never by value.
func (e *emitter) hostedRuntimeParamTypes(args []ir.Value) []string {
	params := make([]string, 0, len(args))
	for _, arg := range args {
		if e.hostedRuntimeIndirectABI(arg.Type) {
			params = append(params, "ptr")
			continue
		}
		params = append(params, e.llvmType(arg.Type))
	}
	return params
}

// writeHostedRuntimeCall adapts Kizu aggregate values to the narrow hosted C ABI.
func (e *emitter) writeHostedRuntimeCall(name string, instr *ir.Instr) error {
	resultName := localName(instr.Result.Name)
	resultType := e.llvmType(instr.Result.Type)
	resultSlot := resultName + ".slot"
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", resultSlot, resultType)
	args := []string{"ptr " + resultSlot}
	for index, arg := range instr.Args {
		value := e.value(arg)
		if e.hostedRuntimeIndirectABI(arg.Type) {
			argType := e.llvmType(arg.Type)
			argSlot := fmt.Sprintf("%s.arg.%d", resultName, index)
			fmt.Fprintf(&e.out, "  %s = alloca %s\n", argSlot, argType)
			fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n", argType, value.operand, argSlot)
			args = append(args, "ptr "+argSlot)
			continue
		}
		args = append(args, e.llvmType(arg.Type)+" "+value.operand)
	}
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n",
		llvmFunctionName(name),
		strings.Join(args, ", "),
	)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", resultName, resultType, resultSlot)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeCast emits scalar no-op casts and explicit error-union ABI adaptation.
func (e *emitter) writeCast(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: cast expects 1 arg")
	}
	source := instr.Args[0]
	if _, sourceIsError := errorUnionSuccessType(source.Type); sourceIsError {
		return fmt.Errorf(
			"llvm error: cannot cast error union %s to %s",
			source.Type,
			instr.Result.Type,
		)
	}
	if _, targetIsError := errorUnionSuccessType(instr.Result.Type); targetIsError {
		return fmt.Errorf(
			"llvm error: cannot cast %s to error union %s",
			source.Type,
			instr.Result.Type,
		)
	}
	if castForwardsOperand(instr) {
		value := e.value(source)
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: value.operand}
		return nil
	}
	if _, ok := integerBitWidth(source.Type); ok {
		if _, targetOK := integerBitWidth(instr.Result.Type); targetOK {
			return e.writeIntegerCast(instr)
		}
		return e.writePointerIntegerCast(instr, "inttoptr")
	}
	return e.writePointerIntegerCast(instr, "ptrtoint")
}

// castForwardsOperand reports whether a cast only changes the Kizu type — a
// same-width integer cast, or a cast whose LLVM representation is already the
// target's — so writeCast forwards the operand instead of emitting anything.
func castForwardsOperand(instr *ir.Instr) bool {
	source := instr.Args[0].Type
	target := instr.Result.Type
	sourceWidth, sourceInt := integerBitWidth(source)
	targetWidth, targetInt := integerBitWidth(target)
	if sourceInt && targetInt {
		return sourceWidth == targetWidth
	}
	if sourceInt {
		return !isRawPointerType(target)
	}
	if isRawPointerType(source) {
		return !targetInt
	}
	return true
}

// writePointerIntegerCast emits the address conversion between a raw pointer
// and an integer. Width changes ride the conversion itself: inttoptr and
// ptrtoint extend and truncate to the target on their own.
func (e *emitter) writePointerIntegerCast(instr *ir.Instr, op string) error {
	source := instr.Args[0]
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = %s %s %s to %s\n",
		name,
		op,
		e.llvmType(source.Type),
		e.value(source).operand,
		e.llvmType(instr.Result.Type),
	)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeIntegerCast emits explicit truncate/extend casts between scalar integer
// widths. Same-width casts never reach here: castForwardsOperand claims them.
func (e *emitter) writeIntegerCast(instr *ir.Instr) error {
	source := instr.Args[0]
	sourceWidth, _ := integerBitWidth(source.Type)
	targetWidth, _ := integerBitWidth(instr.Result.Type)
	value := e.value(source)
	name := localName(instr.Result.Name)
	sourceType := e.llvmType(source.Type)
	targetType := e.llvmType(instr.Result.Type)
	if sourceWidth > targetWidth {
		fmt.Fprintf(&e.out, "  %s = trunc %s %s to %s\n",
			name, sourceType, value.operand, targetType)
	} else {
		op := "sext"
		if isUnsignedIntegerType(source.Type) {
			op = "zext"
		}
		fmt.Fprintf(&e.out, "  %s = %s %s %s to %s\n",
			name, op, sourceType, value.operand, targetType)
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// failureCode extracts an error-union failure. A failure is one global code,
// so it reads the same i64 out of every union and needs no conversion.
func (e *emitter) failureCode(resultName string, source ir.Value) string {
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %d\n",
		resultName,
		e.llvmType(source.Type),
		e.value(source).operand,
		errorUnionFailureIndex(source.Type),
	)
	return resultName
}

// writeStructNew lowers a checked struct literal to an LLVM aggregate value.
func (e *emitter) writeStructNew(instr *ir.Instr) error {
	st, ok := e.module.Structs[instr.Result.Type]
	if !ok {
		return fmt.Errorf("llvm error: unknown struct type `%s`", instr.Result.Type)
	}
	values := map[string]ir.Value{}
	for _, field := range instr.Fields {
		if _, ok := structFieldIndex(st, field.Name); !ok {
			return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, field.Name)
		}
		if _, exists := values[field.Name]; exists {
			return fmt.Errorf("llvm error: duplicate struct field `%s.%s`", st.Name, field.Name)
		}
		values[field.Name] = field.Value
	}
	structType := e.llvmType(instr.Result.Type)
	aggregate := "zeroinitializer"
	resultName := localName(instr.Result.Name)
	for index, field := range st.Fields {
		value, ok := values[field.Name]
		if !ok {
			return fmt.Errorf("llvm error: missing struct field `%s.%s`", st.Name, field.Name)
		}
		if value.Type != field.Type {
			return fmt.Errorf(
				"llvm error: struct field `%s.%s` expects %s, got %s",
				st.Name,
				field.Name,
				field.Type,
				value.Type,
			)
		}
		fieldValue := e.value(value)
		name := fmt.Sprintf("%s.field%d", resultName, index)
		if index == len(st.Fields)-1 {
			name = resultName
		}
		fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, %d\n",
			name, structType, aggregate, e.llvmType(field.Type), fieldValue.operand, index)
		aggregate = name
	}
	if len(st.Fields) == 0 {
		resultName = "zeroinitializer"
	}
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeField lowers a checked struct field read to an LLVM aggregate extraction.
func (e *emitter) writeField(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: field read expects 1 arg")
	}
	receiver := instr.Args[0]
	st, ok := e.module.Structs[receiver.Type]
	if !ok {
		return fmt.Errorf("llvm error: unknown struct type `%s`", receiver.Type)
	}
	fieldName := strings.TrimPrefix(instr.Op, "field.")
	index, ok := structFieldIndex(st, fieldName)
	if !ok {
		return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, fieldName)
	}
	if instr.Result.Type != st.Fields[index].Type {
		return fmt.Errorf(
			"llvm error: field `%s.%s` returns %s, got %s",
			st.Name,
			fieldName,
			st.Fields[index].Type,
			instr.Result.Type,
		)
	}
	value := e.value(receiver)
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, %d\n",
		name, e.llvmType(receiver.Type), value.operand, index)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeFieldRef lowers a field read through a borrowed struct pointer.
func (e *emitter) writeFieldRef(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: borrowed field read expects 1 arg")
	}
	receiver := instr.Args[0]
	structType := derefLLVMType(receiver.Type)
	st, ok := e.module.Structs[structType]
	if !ok {
		return fmt.Errorf("llvm error: unknown borrowed struct type `%s`", receiver.Type)
	}
	fieldName := strings.TrimPrefix(instr.Op, "field.ref.")
	index, ok := structFieldIndex(st, fieldName)
	if !ok {
		return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, fieldName)
	}
	if instr.Result.Type != st.Fields[index].Type {
		return fmt.Errorf(
			"llvm error: borrowed field `%s.%s` returns %s, got %s",
			st.Name,
			fieldName,
			st.Fields[index].Type,
			instr.Result.Type,
		)
	}
	value := e.value(receiver)
	ptrName := localName(instr.Result.Name) + ".ptr"
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 %d\n",
		ptrName, e.llvmType(structType), value.operand, index)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s\n", name, e.llvmType(instr.Result.Type), ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeFieldAddr lowers a field borrow out of a borrowed struct pointer: the
// projection is the GEP alone, and the result is the field's own storage.
func (e *emitter) writeFieldAddr(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: field address expects 1 arg")
	}
	receiver := instr.Args[0]
	structType := derefLLVMType(receiver.Type)
	st, ok := e.module.Structs[structType]
	if !ok {
		return fmt.Errorf("llvm error: unknown borrowed struct type `%s`", receiver.Type)
	}
	fieldName := strings.TrimPrefix(instr.Op, "field.addr.")
	index, ok := structFieldIndex(st, fieldName)
	if !ok {
		return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, fieldName)
	}
	if derefLLVMType(instr.Result.Type) != st.Fields[index].Type {
		return fmt.Errorf(
			"llvm error: field address `%s.%s` returns &var %s, got %s",
			st.Name,
			fieldName,
			st.Fields[index].Type,
			instr.Result.Type,
		)
	}
	value := e.value(receiver)
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 %d\n",
		name, e.llvmType(structType), value.operand, index)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeFieldSet lowers a struct field write on a value receiver to an LLVM
// aggregate insertion. The receiver is SSA, so the write produces a new struct
// value that the lowerer has already bound in place of the old one.
func (e *emitter) writeFieldSet(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: field write expects 2 args")
	}
	receiver := instr.Args[0]
	st, ok := e.module.Structs[receiver.Type]
	if !ok {
		return fmt.Errorf("llvm error: unknown struct type `%s`", receiver.Type)
	}
	fieldName := strings.TrimPrefix(instr.Op, "field.set.")
	index, ok := structFieldIndex(st, fieldName)
	if !ok {
		return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, fieldName)
	}
	if instr.Args[1].Type != st.Fields[index].Type {
		return fmt.Errorf(
			"llvm error: field `%s.%s` accepts %s, got %s",
			st.Name,
			fieldName,
			st.Fields[index].Type,
			instr.Args[1].Type,
		)
	}
	if instr.Result.Type != receiver.Type {
		return fmt.Errorf(
			"llvm error: field write on `%s` returns %s, got %s",
			st.Name,
			receiver.Type,
			instr.Result.Type,
		)
	}
	value := e.value(instr.Args[1])
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, %d\n",
		name,
		e.llvmType(receiver.Type),
		e.value(receiver).operand,
		e.llvmType(instr.Args[1].Type),
		value.operand,
		index,
	)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeFieldRefSet lowers a struct field write through a borrowed struct
// pointer to an addressed store, so the write lands in the borrowed storage
// rather than in a copy of it.
func (e *emitter) writeFieldRefSet(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: borrowed field write expects 2 args")
	}
	receiver := instr.Args[0]
	structType := derefLLVMType(receiver.Type)
	st, ok := e.module.Structs[structType]
	if !ok {
		return fmt.Errorf("llvm error: unknown borrowed struct type `%s`", receiver.Type)
	}
	fieldName := strings.TrimPrefix(instr.Op, "field.ref.set.")
	index, ok := structFieldIndex(st, fieldName)
	if !ok {
		return fmt.Errorf("llvm error: unknown struct field `%s.%s`", st.Name, fieldName)
	}
	if instr.Args[1].Type != st.Fields[index].Type {
		return fmt.Errorf(
			"llvm error: borrowed field `%s.%s` accepts %s, got %s",
			st.Name,
			fieldName,
			st.Fields[index].Type,
			instr.Args[1].Type,
		)
	}
	if instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: borrowed field write returns void, got %s",
			instr.Result.Type)
	}
	ptrName := localName(instr.Result.Name) + ".ptr"
	fmt.Fprintf(&e.out, "  %s = getelementptr %s, ptr %s, i32 0, i32 %d\n",
		ptrName, e.llvmType(structType), e.value(receiver).operand, index)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		e.llvmType(instr.Args[1].Type), e.value(instr.Args[1]).operand, ptrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeRefStore lowers `receiver.* = value` to a store through the borrow.
func (e *emitter) writeRefStore(instr *ir.Instr) error {
	if len(instr.Args) != 2 {
		return fmt.Errorf("llvm error: dereference write expects 2 args")
	}
	receiver := instr.Args[0]
	if want := derefLLVMType(receiver.Type); want != instr.Args[1].Type {
		return fmt.Errorf("llvm error: dereference write on `%s` accepts %s, got %s",
			receiver.Type, want, instr.Args[1].Type)
	}
	if instr.Result.Type != "void" {
		return fmt.Errorf("llvm error: dereference write returns void, got %s",
			instr.Result.Type)
	}
	fmt.Fprintf(&e.out, "  store %s%s %s, ptr %s\n",
		volatileKeyword(instr.Op),
		e.llvmType(instr.Args[1].Type),
		e.value(instr.Args[1]).operand,
		e.value(receiver).operand,
	)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// volatileKeyword marks the memory accesses no pass may drop, merge, or
// reorder against other volatile accesses: device registers change state on
// access, so each one the source wrote has to happen.
func volatileKeyword(op string) string {
	if strings.HasPrefix(op, "volatile.") {
		return "volatile "
	}
	return ""
}

// writeLocalSlot gives a local storage and puts its first value there. A local
// only reaches here when the function mutably borrows it, so the address it
// yields is what the callee writes through.
func (e *emitter) writeLocalSlot(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: local slot expects 1 arg")
	}
	initial := instr.Args[0]
	if want := derefLLVMType(instr.Result.Type); want != initial.Type {
		return fmt.Errorf("llvm error: local slot of %s holds %s, got %s",
			instr.Result.Type, want, initial.Type)
	}
	slotName := localName(instr.Result.Name)
	valueType := e.llvmType(initial.Type)
	fmt.Fprintf(&e.out, "  %s = alloca %s\n", slotName, valueType)
	fmt.Fprintf(&e.out, "  store %s %s, ptr %s\n",
		valueType, e.value(initial).operand, slotName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: slotName}
	return nil
}

// writeRefLoad reads the value currently behind a borrow.
func (e *emitter) writeRefLoad(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: borrow read expects 1 arg")
	}
	receiver := instr.Args[0]
	if want := derefLLVMType(receiver.Type); want != instr.Result.Type {
		return fmt.Errorf("llvm error: borrow read of `%s` gives %s, got %s",
			receiver.Type, want, instr.Result.Type)
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s%s, ptr %s\n",
		resultName, volatileKeyword(instr.Op), e.llvmType(instr.Result.Type),
		e.value(receiver).operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeSliceLen extracts the byte length from a []u8 value.
func (e *emitter) writeSliceLen(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" || instr.Result.Type != "i64" {
		return fmt.Errorf("llvm error: slice.len expects []u8 -> i64")
	}
	slice := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 1\n",
		resultName, slice.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeSliceIndex loads one byte. The bounds test reaches the backend as a
// preceding cond_fail instruction, so no check is generated here.
func (e *emitter) writeSliceIndex(instr *ir.Instr) error {
	if len(instr.Args) != 2 ||
		instr.Args[0].Type != "[]u8" ||
		instr.Args[1].Type != "i64" ||
		instr.Result.Type != "u8" {
		return fmt.Errorf("llvm error: slice.index expects []u8, i64 -> u8")
	}
	slice := e.value(instr.Args[0])
	index := e.value(instr.Args[1])
	resultName := localName(instr.Result.Name)
	ptrName := resultName + ".ptr"
	elemPtrName := resultName + ".elem.ptr"
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 0\n", ptrName, slice.operand)
	fmt.Fprintf(&e.out, "  %s = getelementptr i8, ptr %s, i64 %s\n",
		elemPtrName, ptrName, index.operand)
	fmt.Fprintf(&e.out, "  %s = load i8, ptr %s\n", resultName, elemPtrName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeSliceSlice builds a sub-slice. The bounds test reaches the backend as
// preceding cond_fail instructions, so no check is generated here.
func (e *emitter) writeSliceSlice(instr *ir.Instr) error {
	if len(instr.Args) != 3 ||
		instr.Args[0].Type != "[]u8" ||
		instr.Args[1].Type != "i64" ||
		instr.Args[2].Type != "i64" ||
		instr.Result.Type != "[]u8" {
		return fmt.Errorf("llvm error: slice.slice expects []u8, i64, i64 -> []u8")
	}
	slice := e.value(instr.Args[0])
	start := e.value(instr.Args[1])
	end := e.value(instr.Args[2])
	resultName := localName(instr.Result.Name)
	ptrName := resultName + ".source.ptr"
	slicePtrName := resultName + ".ptr"
	baseName := resultName + ".base"
	sliceLenName := resultName + ".len"
	fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 0\n", ptrName, slice.operand)
	fmt.Fprintf(&e.out, "  %s = getelementptr i8, ptr %s, i64 %s\n",
		slicePtrName, ptrName, start.operand)
	fmt.Fprintf(&e.out, "  %s = sub i64 %s, %s\n", sliceLenName, end.operand, start.operand)
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 poison, ptr %s, 0\n",
		baseName, slicePtrName)
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 %s, i64 %s, 1\n",
		resultName, baseName, sliceLenName)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeBufferNew allocates a zero-filled fixed-length stack buffer. The value
// registered for the result is the alloca pointer: a buffer is its storage,
// and the views buffer.as_bytes hands out point into it (ADR-0097).
func (e *emitter) writeBufferNew(instr *ir.Instr) error {
	size, ok := bufferSize(instr.Result.Type)
	if !ok {
		return fmt.Errorf("llvm error: buffer.new expects `[N]u8` result, got %s",
			instr.Result.Type)
	}
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = alloca [%d x i8]\n", name, size)
	fmt.Fprintf(&e.out, "  store [%d x i8] zeroinitializer, ptr %s\n", size, name)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// writeBufferAsBytes builds the {ptr, len} view of a stack buffer.
func (e *emitter) writeBufferAsBytes(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Result.Type != "[]u8" {
		return fmt.Errorf("llvm error: buffer.as_bytes expects buffer -> []u8")
	}
	size, ok := bufferSize(instr.Args[0].Type)
	if !ok {
		return fmt.Errorf("llvm error: buffer.as_bytes expects `[N]u8`, got %s",
			instr.Args[0].Type)
	}
	buffer := e.value(instr.Args[0])
	resultName := localName(instr.Result.Name)
	baseName := resultName + ".base"
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 poison, ptr %s, 0\n",
		baseName, buffer.operand)
	fmt.Fprintf(&e.out, "  %s = insertvalue %%kizu.slice.u8 %s, i64 %d, 1\n",
		resultName, baseName, size)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// bufferSize parses N from a `[N]u8` spelling.
func bufferSize(typeName string) (int64, bool) {
	parsed, err := typ.Parse(typeName)
	if err != nil {
		return 0, false
	}
	buffer, ok := parsed.(*typ.Buffer)
	if !ok {
		return 0, false
	}
	return buffer.Size, true
}

// writeCondFail reports the named failure when the tested condition holds.
func (e *emitter) writeCondFail(instr *ir.Instr) error {
	spec, ok := panicEntries[instr.Immediate]
	if !ok {
		return fmt.Errorf("llvm error: unknown cond_fail kind `%s`", instr.Immediate)
	}
	if len(instr.Args) != len(spec.params)+1 || instr.Args[0].Type != "bool" {
		return fmt.Errorf("llvm error: cond_fail `%s` expects bool and %d values",
			instr.Immediate, len(spec.params))
	}
	cond := e.value(instr.Args[0])
	args := make([]string, 0, len(spec.params)+2)
	for i, arg := range instr.Args[1:] {
		args = append(args, spec.params[i]+" "+e.value(arg).operand)
	}
	args = append(args, panicPosition(instr.Span)...)
	failLabel := helperLabel(instr.Args[0].Name, "fail")
	okLabel := helperLabel(instr.Args[0].Name, "pass")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n",
		cond.operand, failLabel, okLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n", spec.entry, strings.Join(args, ", "))
	e.out.WriteString("  unreachable\n")
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	return nil
}

// writePanicFail reports an unconditional runtime failure with a message and stops.
func (e *emitter) writePanicFail(instr *ir.Instr) error {
	if len(instr.Args) != 1 || instr.Args[0].Type != "[]u8" {
		return fmt.Errorf("llvm error: panic.fail expects one []u8 message")
	}
	ptr, length := e.writeSliceParts(localName(instr.Result.Name)+".msg",
		e.value(instr.Args[0]).operand)
	fmt.Fprintf(&e.out, "  call void @kizu_panic_fail(ptr %s, i64 %s, %s)\n",
		ptr, length, strings.Join(panicPosition(instr.Span), ", "))
	e.out.WriteString("  unreachable\n")
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: "void"}
	return nil
}

// writeErrorOK builds a successful error-union value.
func (e *emitter) writeErrorOK(instr *ir.Instr) error {
	success, ok := errorUnionSuccessType(instr.Result.Type)
	if !ok {
		return fmt.Errorf("llvm error: error.ok result must be !T, got %s", instr.Result.Type)
	}
	if err := validateErrorUnionType(instr.Result.Type); err != nil {
		return err
	}
	resultName := localName(instr.Result.Name)
	unionType := e.llvmType(instr.Result.Type)
	if success == "void" {
		if len(instr.Args) != 0 {
			return fmt.Errorf("llvm error: error.ok !void expects 0 args")
		}
		fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n",
			resultName, unionType)
		e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
		return nil
	}
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: error.ok %s expects 1 arg", instr.Result.Type)
	}
	value := instr.Args[0]
	if value.Type != success {
		return fmt.Errorf("llvm error: error.ok expects %s, got %s", success, value.Type)
	}
	okName := resultName + ".ok"
	argInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n",
		okName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		resultName, unionType, okName, e.llvmType(success), argInfo.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeErrorError builds a failed error-union value. The failure payload is a
// member of an error set, and its representation is already the global code.
func (e *emitter) writeErrorError(instr *ir.Instr) error {
	if _, _, ok := errorUnionParts(instr.Result.Type); !ok {
		return fmt.Errorf("llvm error: error.error result must be !T, got %s", instr.Result.Type)
	}
	if err := validateErrorUnionType(instr.Result.Type); err != nil {
		return err
	}
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: error.error expects one error value")
	}
	arg := instr.Args[0]
	if _, ok := e.module.ErrorSets[arg.Type]; !ok {
		return fmt.Errorf("llvm error: error.error expects an error set member, got %s", arg.Type)
	}
	// A declared `E!T` accepts only E. Codes are global, so a member of another
	// set would lower to a union that claims E while holding another set's code,
	// and nothing below here records which set a union carries.
	if errorName, _, _ := errorUnionParts(instr.Result.Type); errorName != "" &&
		arg.Type != errorName {
		return fmt.Errorf("llvm error: error.error cannot put %s into %s",
			arg.Type, instr.Result.Type)
	}
	resultName := localName(instr.Result.Name)
	e.writeErrorFailureCode(resultName, instr.Result.Type, e.value(arg).operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeErrorFailureCode builds a failed error union around one error code.
func (e *emitter) writeErrorFailureCode(
	resultName string,
	resultType string,
	codeOperand string,
) {
	unionType := e.llvmType(resultType)
	baseName := resultName + ".base"
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 0, 0\n",
		baseName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, i64 %s, %d\n",
		resultName, unionType, baseName, codeOperand,
		errorUnionFailureIndex(resultType))
}

// writeErrorTry unwraps success or returns failure from the current function.
func (e *emitter) writeErrorTry(instr *ir.Instr) error {
	if len(instr.Args) != 1 {
		return fmt.Errorf("llvm error: error.try expects 1 arg")
	}
	source := instr.Args[0]
	sourceError, success, ok := errorUnionParts(source.Type)
	if !ok {
		return fmt.Errorf("llvm error: error.try expects !T, got %s", source.Type)
	}
	if err := validateErrorUnionType(source.Type); err != nil {
		return err
	}
	if instr.Result.Type != success {
		return fmt.Errorf("llvm error: error.try returns %s, got %s", success, instr.Result.Type)
	}
	targetError, _, ok := errorUnionParts(e.currentReturn)
	if !ok {
		return fmt.Errorf("llvm error: error.try requires function to return !T")
	}
	if targetError != "" && sourceError != targetError {
		return fmt.Errorf(
			"llvm error: error.try cannot propagate %s into %s",
			source.Type,
			e.currentReturn,
		)
	}
	sourceValue := e.value(source)
	sourceType := e.llvmType(source.Type)
	okValue := localName(instr.Result.Name) + ".ok"
	okBool := okValue + ".bool"
	okLabel := helperLabel(instr.Result.Name, "try.ok")
	errLabel := helperLabel(instr.Result.Name, "try.err")
	e.markCurrentBlockExit(okLabel)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", okValue, sourceType, sourceValue.operand)
	fmt.Fprintf(&e.out, "  %s = icmp ne i8 %s, 0\n", okBool, okValue)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okBool, okLabel, errLabel)
	fmt.Fprintf(&e.out, "%s:\n", errLabel)
	for _, cleanup := range instr.Cleanups {
		if err := e.writeCleanup(cleanup); err != nil {
			return err
		}
	}
	if err := e.writeErrorFailureReturn(source); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	if success == "void" {
		return nil
	}
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n", resultName, sourceType, sourceValue.operand)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeCleanup emits one deferred void cleanup inside an already-open block.
// Cleanup args arrive as values, never `&var` slots: the lowerer loads
// slot-backed receivers before attaching cleanups to an instruction.
func (e *emitter) writeCleanup(cleanup ir.Cleanup) error {
	instr := &ir.Instr{
		Result: ir.Value{Name: "%" + e.nextSyntheticValue("cleanup"), Type: "void"},
		Op:     cleanup.Op,
		Args:   cleanup.Args,
	}
	return e.writeInstr(instr)
}

// writePrint writes calls to the Kizu runtime print ABI.
func (e *emitter) writePrint(args []ir.Value) error {
	if len(args) != 1 {
		return fmt.Errorf("llvm error: print expects 1 arg")
	}
	value := e.value(args[0])
	switch args[0].Type {
	case "[]u8":
		ptrName := "%" + e.nextSyntheticValue("print.slice.ptr")
		lenName := "%" + e.nextSyntheticValue("print.slice.len")
		fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 0\n",
			ptrName, value.operand)
		fmt.Fprintf(&e.out, "  %s = extractvalue %%kizu.slice.u8 %s, 1\n",
			lenName, value.operand)
		fmt.Fprintf(&e.out, "  call void @kizu_print_string(ptr %s, i64 %s)\n",
			ptrName, lenName)
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "usize", "isize":
		operand := e.runtimeIntegerOperand(args[0].Type, value.operand)
		fmt.Fprintf(&e.out, "  call void @kizu_print_int(i64 %s)\n", operand)
	case "bool":
		fmt.Fprintf(&e.out, "  call void @kizu_print_bool(i1 %s)\n", value.operand)
	default:
		if _, ok := e.module.Enums[args[0].Type]; ok {
			e.writePrintEnum(args[0].Type, value.operand)
			return nil
		}
		if _, ok := e.module.ErrorSets[args[0].Type]; ok {
			// An error is a global code, so its spelling comes from the one
			// table every error shares rather than from a per-type table.
			e.writePrintNameTable(errorNameTable, e.errorNameTableRows(), value.operand)
			return nil
		}
		return fmt.Errorf("llvm error: print does not support `%s`", args[0].Type)
	}
	return nil
}

// writePrintEnum prints an enum by indexing its name table, so a new tag costs
// a table row rather than a branch in the backend.
func (e *emitter) writePrintEnum(typ string, operand string) {
	e.writePrintNameTable(enumNameTable(typ), len(e.module.Enums[typ].Tags), operand)
}

// writePrintNameTable prints one tag by indexing a table of spellings.
func (e *emitter) writePrintNameTable(table string, rows int, operand string) {
	fmt.Fprintf(&e.out, "  call void @kizu_print_enum(ptr %s, i64 %d, i64 %s)\n",
		table, rows, operand)
}

// enumNameTable returns the global holding one enum's tag spellings.
func enumNameTable(typ string) string {
	return "@.kizu.enum." + mangleGlobalName(typ)
}

// mangleGlobalName makes a type name usable inside an LLVM global identifier.
func mangleGlobalName(typ string) string {
	return strings.NewReplacer(":", "_", "<", "_", ">", "_", ",", "_", " ", "").Replace(typ)
}

// printsNamedTag reports whether the module prints an enum or an error set,
// both of which name their value through a table of spellings.
func (e *emitter) printsNamedTag() bool {
	if len(e.printedEnums()) > 0 {
		return true
	}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op != "call.print" || len(instr.Args) != 1 {
					continue
				}
				if _, ok := e.module.ErrorSets[instr.Args[0].Type]; ok {
					return true
				}
			}
		}
	}
	return false
}

// printedEnums returns the enums this module prints, in sorted order.
func (e *emitter) printedEnums() []string {
	seen := map[string]bool{}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op != "call.print" || len(instr.Args) != 1 {
					continue
				}
				if _, ok := e.module.Enums[instr.Args[0].Type]; ok {
					seen[instr.Args[0].Type] = true
				}
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

// writeEnumNameTables defines the `Enum::Tag` spellings the module prints.
func (e *emitter) writeEnumNameTables() {
	names := e.printedEnums()
	for _, typ := range names {
		tags := e.module.Enums[typ].Tags
		spellings := make([]string, len(tags))
		for tag, index := range tags {
			spellings[index] = typ + "::" + tag
		}
		rows := make([]string, 0, len(spellings))
		for index, spelling := range spellings {
			global := fmt.Sprintf("%s.%d", enumNameTable(typ), index)
			e.writeStaticStringGlobal(global, spelling)
			rows = append(rows,
				fmt.Sprintf("{ ptr, i64 } { ptr %s, i64 %d }", global, len(spelling)))
		}
		// A literal { ptr, i64 } rather than %kizu.slice.u8: the named type is
		// only defined when the module otherwise uses slices, and a module can
		// print an enum without ever touching one.
		fmt.Fprintf(&e.out, "%s = private unnamed_addr constant [%d x { ptr, i64 }] [%s]\n",
			enumNameTable(typ), len(rows), strings.Join(rows, ", "))
	}
	if len(names) > 0 {
		e.out.WriteByte('\n')
	}
}

// runtimeIntegerOperand widens a narrow integer to the i64 the runtime helpers
// take. Every helper that reports a value -- print, an expectation that failed
// -- reads one i64, so the widening is the same one wherever a value crosses
// into the runtime.
func (e *emitter) runtimeIntegerOperand(typ string, operand string) string {
	sourceType := e.llvmType(typ)
	if sourceType == "i64" {
		return operand
	}
	name := "%" + e.nextSyntheticValue("runtime.int")
	op := "sext"
	if isUnsignedIntegerType(typ) {
		op = "zext"
	}
	fmt.Fprintf(&e.out, "  %s = %s %s %s to i64\n", name, op, sourceType, operand)
	return name
}

// writePhi writes an LLVM phi instruction.
func (e *emitter) writePhi(instr *ir.Instr) error {
	parts := make([]string, 0, len(instr.Incoming))
	for _, incoming := range instr.Incoming {
		value := e.value(incoming.Value)
		parts = append(parts, fmt.Sprintf(
			"[ %s, %%%s ]",
			value.operand,
			e.incomingBlockLabel(incoming.Block),
		))
	}
	name := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = phi %s %s\n",
		name, e.llvmType(instr.Result.Type), strings.Join(parts, ", "))
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: name}
	return nil
}

// incomingBlockLabel returns the concrete LLVM label that reaches a successor.
func (e *emitter) incomingBlockLabel(block string) string {
	if label, ok := e.blockExitLabel[block]; ok {
		return label
	}
	return block
}

// precomputeBlockExitLabels records helper labels before phi nodes are emitted.
func (e *emitter) precomputeBlockExitLabels(fn *ir.Function) {
	for _, block := range fn.Blocks {
		if label, ok := e.computeBlockExitLabel(block); ok {
			e.blockExitLabel[block.Name] = label
		}
	}
}

// computeBlockExitLabel returns the final helper label that continues one IR block.
func (e *emitter) computeBlockExitLabel(block *ir.Block) (string, bool) {
	label := ""
	for _, instr := range block.Instrs {
		if next, ok := continuationLabel(instr); ok {
			label = next
		}
	}
	if label == "" {
		return "", false
	}
	return label, true
}

// continuationLabel reports helper labels introduced by instruction expansion.
func continuationLabel(instr *ir.Instr) (string, bool) {
	switch instr.Op {
	case "error.try":
		return helperLabel(instr.Result.Name, "try.ok"), true
	case "cond_fail":
		return helperLabel(instr.Args[0].Name, "pass"), true
	case "array.pop", "array.get", "map.get":
		return helperLabel(instr.Result.Name, "array.join"), true
	case "array.get_or_panic", "map.take_value_at", "arena.at", "arena.pop_or_panic":
		return helperLabel(localName(instr.Result.Name)+".ptr", "ok"), true
	case "arena.add":
		return helperLabel(localName(instr.Result.Name)+".bad", "ok"), true
	case "test.expect_equal":
		return helperLabel(localName(instr.Result.Name)+".ok", "ok"), true
	default:
		return "", false
	}
}

// markCurrentBlockExit records the concrete label where the current IR block continues.
func (e *emitter) markCurrentBlockExit(label string) {
	if e.currentBlock != "" {
		e.blockExitLabel[e.currentBlock] = label
	}
}

// helperLabel derives a deterministic local label from an SSA value or operand.
func helperLabel(base string, suffix string) string {
	label := base
	if strings.HasPrefix(label, "%kizu.") {
		label = strings.TrimPrefix(label, "%")
	} else if strings.HasPrefix(label, "%") {
		label = strings.TrimPrefix(localName(base), "%")
	} else {
		label = strings.TrimPrefix(label, "%")
	}
	return label + "." + suffix
}

// writeTerminator writes one LLVM terminator.
func (e *emitter) writeTerminator(term ir.Terminator) error {
	switch term.Op {
	case "return":
		if _, ok := errorUnionSuccessType(e.currentReturn); ok {
			return e.writeErrorUnionReturn(term.Value)
		}
		if term.Value.Type == "void" {
			if e.mainReturnsInt {
				e.out.WriteString("  ret i32 0\n")
				return nil
			}
			e.out.WriteString("  ret void\n")
			return nil
		}
		value := e.value(term.Value)
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(term.Value.Type), value.operand)
	case "jump":
		fmt.Fprintf(&e.out, "  br label %%%s\n", term.Target)
	case "branch":
		cond := e.value(term.Cond)
		fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n",
			cond.operand, term.Target, term.Else)
	case "unreachable":
		e.out.WriteString("  unreachable\n")
	default:
		return fmt.Errorf("llvm error: unsupported terminator `%s`", term.Op)
	}
	return nil
}

// writeErrorUnionReturn emits a return from a function declared as !T. Lowering
// wraps every success in error.ok before it gets here, so what arrives is the
// union itself, a failure to name, or another union to absorb.
func (e *emitter) writeErrorUnionReturn(value ir.Value) error {
	if e.mainReturnsInt {
		if value.Type == e.currentReturn {
			return e.writeMainErrorUnionReturn(value)
		}
		if _, isSet := e.module.ErrorSets[value.Type]; isSet {
			e.writeMainErrorName(e.value(value).operand)
			e.out.WriteString("  ret i32 1\n")
			return nil
		}
		return fmt.Errorf(
			"llvm error: cannot return %s from %s",
			value.Type,
			e.currentReturn,
		)
	}
	if value.Type == e.currentReturn {
		valueInfo := e.value(value)
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(value.Type), valueInfo.operand)
		return nil
	}
	if e.absorbsErrorUnionReturn(value) {
		return e.writeAbsorbedErrorUnionReturn(value)
	}
	if _, isSet := e.module.ErrorSets[value.Type]; isSet {
		valueInfo := e.value(value)
		name := "%" + e.nextSyntheticValue("return.err")
		e.writeErrorFailureCode(name, e.currentReturn, valueInfo.operand)
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(e.currentReturn), name)
		return nil
	}
	return fmt.Errorf("llvm error: cannot return %s from %s", value.Type, e.currentReturn)
}

// absorbsErrorUnionReturn reports whether one error union is returned straight
// from a function that declares no error set, which is the absorption `try`
// does written as a return.
func (e *emitter) absorbsErrorUnionReturn(value ir.Value) bool {
	return typ.AbsorbsErrorSet(e.currentReturn, value.Type)
}

// writeAbsorbedErrorUnionReturn returns one error union as another, naming the
// failure on the way because the target describes its own with text.
func (e *emitter) writeAbsorbedErrorUnionReturn(value ir.Value) error {
	_, success, _ := errorUnionParts(value.Type)
	source := e.value(value)
	sourceType := e.llvmType(value.Type)
	okName := "%" + e.nextSyntheticValue("absorb.ok")
	okBool := okName + ".bool"
	okLabel := e.nextSyntheticValue("absorb.ok.block")
	errLabel := e.nextSyntheticValue("absorb.err.block")
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", okName, sourceType, source.operand)
	fmt.Fprintf(&e.out, "  %s = icmp ne i8 %s, 0\n", okBool, okName)
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okBool, okLabel, errLabel)
	fmt.Fprintf(&e.out, "%s:\n", errLabel)
	if err := e.writeErrorFailureReturn(value); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	if success == "void" {
		return e.writeSuccessReturn(ir.Value{Name: value.Name, Type: "void"})
	}
	successName := "%" + e.nextSyntheticValue("absorb.value")
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n", successName, sourceType, source.operand)
	e.values[successName] = valueInfo{typ: success, operand: successName}
	return e.writeSuccessReturn(ir.Value{Name: successName, Type: success})
}

// writeSuccessReturn returns a success value as the error union around it. An
// absorbed return arrives holding the success the source union carried, so the
// wrap is written here rather than by the lowerer that could not name the target
// union yet.
func (e *emitter) writeSuccessReturn(value ir.Value) error {
	name := "%" + e.nextSyntheticValue("return.ok")
	unionType := e.llvmType(e.currentReturn)
	if value.Type == "void" {
		fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n",
			name, unionType)
		fmt.Fprintf(&e.out, "  ret %s %s\n", unionType, name)
		return nil
	}
	okName := "%" + e.nextSyntheticValue("return.ok.flag")
	valueInfo := e.value(value)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s zeroinitializer, i8 1, 0\n",
		okName, unionType)
	fmt.Fprintf(&e.out, "  %s = insertvalue %s %s, %s %s, 1\n",
		name, unionType, okName, e.llvmType(value.Type), valueInfo.operand)
	fmt.Fprintf(&e.out, "  ret %s %s\n", unionType, name)
	return nil
}

// writeMainErrorUnionReturn maps main's !T result to process exit status.
func (e *emitter) writeMainErrorUnionReturn(value ir.Value) error {
	valueInfo := e.value(value)
	unionType := e.llvmType(value.Type)
	okName := "%" + e.nextSyntheticValue("main.ok")
	okBoolName := okName + ".bool"
	codeName := "%" + e.nextSyntheticValue("main.code")
	_, success, _ := errorUnionParts(value.Type)
	fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 0\n", okName, unionType, valueInfo.operand)
	fmt.Fprintf(&e.out, "  %s = icmp ne i8 %s, 0\n", okBoolName, okName)
	okLabel := e.nextSyntheticValue("main.exit.ok")
	failLabel := e.nextSyntheticValue("main.exit.fail")
	fmt.Fprintf(&e.out, "  br i1 %s, label %%%s, label %%%s\n", okBoolName, okLabel, failLabel)
	fmt.Fprintf(&e.out, "%s:\n", failLabel)
	if err := e.writeMainErrorMessage(value); err != nil {
		return err
	}
	e.out.WriteString("  ret i32 1\n")
	fmt.Fprintf(&e.out, "%s:\n", okLabel)
	if success == "i64" {
		successName := "%" + e.nextSyntheticValue("main.success")
		fmt.Fprintf(&e.out, "  %s = extractvalue %s %s, 1\n", successName, unionType, valueInfo.operand)
		fmt.Fprintf(&e.out, "  %s = trunc i64 %s to i32\n", codeName, successName)
		fmt.Fprintf(&e.out, "  ret i32 %s\n", codeName)
		return nil
	}
	e.out.WriteString("  ret i32 0\n")
	return nil
}

// writeMainErrorMessage names the failure a `main -> !T` process exits with,
// so it reports why it exits 1 instead of failing silently. The failure is one
// global code, and the spelling comes from the module's table of error names.
func (e *emitter) writeMainErrorMessage(source ir.Value) error {
	if _, _, ok := errorUnionParts(source.Type); !ok {
		return nil
	}
	e.writeMainErrorName(e.failureCode("%"+e.nextSyntheticValue("main.err.code"), source))
	return nil
}

// writeMainErrorName reports one error code by its spelling. An error carries
// nothing, so the text comes from the table of names rather than from the
// value, which is the number saying which member it is.
func (e *emitter) writeMainErrorName(codeOperand string) {
	name := "%" + e.nextSyntheticValue("main.err.name")
	rowName := name + ".row"
	ptrName := name + ".ptr"
	lenAddr := name + ".len.addr"
	lenName := name + ".len"
	fmt.Fprintf(&e.out, "  %s = getelementptr [%d x { ptr, i64 }], ptr %s, i64 0, i64 %s\n",
		rowName, e.errorNameTableRows(), errorNameTable, codeOperand)
	fmt.Fprintf(&e.out, "  %s = load ptr, ptr %s\n", ptrName, rowName)
	fmt.Fprintf(&e.out, "  %s = getelementptr { ptr, i64 }, ptr %s, i64 0, i32 1\n",
		lenAddr, rowName)
	fmt.Fprintf(&e.out, "  %s = load i64, ptr %s\n", lenName, lenAddr)
	fmt.Fprintf(&e.out, "  call void @kizu_main_error_message(ptr %s, i64 %s)\n",
		ptrName, lenName)
}

// writeErrorFailureReturn propagates a failed try from the current function.
func (e *emitter) writeErrorFailureReturn(source ir.Value) error {
	if e.mainReturnsInt {
		if err := e.writeMainErrorMessage(source); err != nil {
			return err
		}
		e.out.WriteString("  ret i32 1\n")
		return nil
	}
	sourceInfo := e.value(source)
	if source.Type == e.currentReturn {
		fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(source.Type), sourceInfo.operand)
		return nil
	}
	name := "%" + e.nextSyntheticValue("try.err")
	code := e.failureCode(name+".code", source)
	e.writeErrorFailureCode(name, e.currentReturn, code)
	fmt.Fprintf(&e.out, "  ret %s %s\n", e.llvmType(e.currentReturn), name)
	return nil
}

// sliceValue materializes a %kizu.slice.u8 from the current ptr+length string view.
func (e *emitter) sliceValue(value ir.Value) (string, error) {
	info := e.value(value)
	if value.Type != "[]u8" {
		return "", fmt.Errorf("llvm error: expected []u8, got %s", value.Type)
	}
	return info.operand, nil
}

// structFieldIndex resolves a field offset in a declared struct.
func structFieldIndex(st ir.Struct, name string) (int, bool) {
	for index, field := range st.Fields {
		if field.Name == name {
			return index, true
		}
	}
	return 0, false
}

// errorUnionFailureIndex returns the field index of the failure payload.
func errorUnionFailureIndex(typ string) int {
	success, ok := errorUnionSuccessType(typ)
	if ok && success == "void" {
		return 1
	}
	return 2
}

// nextSyntheticValue returns a unique helper value name without a leading percent.
func (e *emitter) nextSyntheticValue(prefix string) string {
	e.nextLabel++
	return fmt.Sprintf("kizu.%s.%d", prefix, e.nextLabel)
}

// value resolves an SSA value to a LLVM operand.
func (e *emitter) value(value ir.Value) valueInfo {
	if found, ok := e.values[value.Name]; ok {
		return found
	}
	return valueInfo{typ: value.Type, operand: localName(value.Name)}
}
