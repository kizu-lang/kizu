package llvm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// threadRoundData names the primitives that start a pool round and where
// each one's `[]T` operand is. The runtime is untyped, so both carry what T
// measures right after that operand and the thunk that calls the worker last.
var threadRoundData = map[string]int{
	"std::internal::builtin::thread_pool_each":      1,
	"std::internal::builtin::thread_pool_each_lane": 2,
}

// isThreadPoolRound reports whether name starts a pool round.
func isThreadPoolRound(name string) bool {
	_, ok := threadRoundData[name]
	return ok
}

// threadRoundSet names the error set a pool round's worker fails with: the
// round returns `E!void` with the same E.
func (e *emitter) threadRoundSet(instr *ir.Instr) string {
	set, _, _ := e.errorUnionParts(instr.Result.Type)
	return set
}

// threadInvokeThunkName is the adapter a round whose worker fails with `set`
// calls that worker through.
func threadInvokeThunkName(set string) string {
	return "kizu.thread.each.invoke." + llvmNamePart(set)
}

// threadRoundSets lists the worker error sets the module's pool rounds use.
func (e *emitter) threadRoundSets() []string {
	seen := map[string]bool{}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if isThreadPoolRound(strings.TrimPrefix(instr.Op, "call.")) {
					seen[e.threadRoundSet(instr)] = true
				}
			}
		}
	}
	sets := make([]string, 0, len(seen))
	for set := range seen {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	return sets
}

// writeThreadEachInvokeThunks emits the typed side of the pool's callback
// ABI, one adapter per worker error set. A worker is a Kizu `fn(&var []T,
// i64) -> E!void`: it takes the view as its {ptr, len} value and returns E's
// union, and a C caller doing either by value would be leaning on the
// target's aggregate rules. The runtime hands the chunk behind a pointer and
// is told the failure code -- zero for success -- and nothing else. Every
// view is the same pair whatever T is, so T needs no adapter of its own.
func (e *emitter) writeThreadEachInvokeThunks() {
	for _, set := range e.threadRoundSets() {
		unionType := e.llvmType(set + "!void")
		fmt.Fprintf(&e.out,
			"define internal i64 @%s(ptr %%kizu.worker, ptr %%kizu.part, i64 %%kizu.index) #0 {\n",
			threadInvokeThunkName(set))
		e.out.WriteString("entry:\n")
		e.out.WriteString("  %kizu.part.value = load %kizu.slice.u8, ptr %kizu.part\n")
		fmt.Fprintf(&e.out,
			"  %%kizu.result = call %s %%kizu.worker(%%kizu.slice.u8 %%kizu.part.value, i64 %%kizu.index)\n",
			unionType)
		fmt.Fprintf(&e.out,
			"  %%kizu.code = extractvalue %s %%kizu.result, %d\n", unionType, errorCodeField)
		e.out.WriteString("  ret i64 %kizu.code\n}\n\n")
	}
}

// threadPoolRoundDecl declares a pool round the way writeThreadPoolRound
// calls it: the result through a leading slot, the slice behind a pointer
// followed by its element size, and the thunk last.
func (e *emitter) threadPoolRoundDecl(name string, instr *ir.Instr) string {
	params := []string{"ptr"}
	for index, arg := range instr.Args {
		if index == threadRoundData[name] {
			params = append(params, "ptr", "i64")
			continue
		}
		params = append(params, e.llvmType(arg.Type))
	}
	params = append(params, "ptr")
	return fmt.Sprintf("declare void @%s(%s)", llvmFunctionName(name), strings.Join(params, ", "))
}

// writeThreadPoolRound lowers one pool round. The runtime does not know T, so
// the call carries the slice behind a pointer, what T measures, and the thunk
// that calls the worker; the runtime cuts chunks and lanes by that size and
// writes the round's `E!void` into the leading slot.
func (e *emitter) writeThreadPoolRound(name string, instr *ir.Instr) error {
	dataIndex := threadRoundData[name]
	if len(instr.Args) <= dataIndex {
		return fmt.Errorf("llvm error: %s expects a slice operand", name)
	}
	elem, ok := strings.CutPrefix(instr.Args[dataIndex].Type, "[]")
	if !ok {
		return fmt.Errorf("llvm error: %s data is %s, not a view", name, instr.Args[dataIndex].Type)
	}
	set := e.threadRoundSet(instr)
	if set == "" {
		return fmt.Errorf("llvm error: %s returns %s, not E!void", name, instr.Result.Type)
	}
	slot := localName(fmt.Sprintf("%%thread.round.%d", e.nextThreadEach))
	e.nextThreadEach++
	resultType := e.llvmType(instr.Result.Type)
	fmt.Fprintf(&e.out, "  %s.result = alloca %s\n", slot, resultType)
	args := []string{"ptr " + slot + ".result"}
	for index, arg := range instr.Args {
		if index != dataIndex {
			args = append(args, e.llvmType(arg.Type)+" "+e.value(arg).operand)
			continue
		}
		fmt.Fprintf(&e.out, "  %s.data = alloca %%kizu.slice.u8\n", slot)
		fmt.Fprintf(&e.out, "  store %%kizu.slice.u8 %s, ptr %s.data\n", e.value(arg).operand, slot)
		args = append(args, "ptr "+slot+".data", "i64 "+e.elementSizeOperand(elem))
	}
	args = append(args, "ptr @"+threadInvokeThunkName(set))
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n", llvmFunctionName(name), strings.Join(args, ", "))
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s.result\n", resultName, resultType, slot)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}
