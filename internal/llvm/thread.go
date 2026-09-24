package llvm

import (
	"fmt"
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

// threadEachInvokeThunk is the one adapter every pool round calls its worker
// through.
const threadEachInvokeThunk = "kizu.thread.each.invoke"

// isThreadPoolRound reports whether name starts a pool round.
func isThreadPoolRound(name string) bool {
	_, ok := threadRoundData[name]
	return ok
}

// usesThreadPoolRound reports whether any function starts a pool round.
func (e *emitter) usesThreadPoolRound() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if isThreadPoolRound(strings.TrimPrefix(instr.Op, "call.")) {
					return true
				}
			}
		}
	}
	return false
}

// writeThreadEachInvokeThunk emits the typed side of the pool's callback ABI.
// A worker is a Kizu `fn(&var []T, i64)`, which takes the view as its {ptr,
// len} value; a C caller passing a struct by value would be leaning on the
// target's aggregate rules. The runtime hands the chunk behind a pointer
// instead, and this thunk makes the one call with Kizu's own ABI. Every view
// is the same pair whatever T is, so one thunk serves every element type.
func (e *emitter) writeThreadEachInvokeThunk() {
	if !e.usesThreadPoolRound() {
		return
	}
	fmt.Fprintf(&e.out,
		"define internal void @%s(ptr %%kizu.worker, ptr %%kizu.part, i64 %%kizu.index) #0 {\n",
		threadEachInvokeThunk)
	e.out.WriteString("entry:\n")
	e.out.WriteString("  %kizu.part.value = load %kizu.slice.u8, ptr %kizu.part\n")
	e.out.WriteString("  call void %kizu.worker(%kizu.slice.u8 %kizu.part.value, i64 %kizu.index)\n")
	e.out.WriteString("  ret void\n}\n\n")
}

// threadPoolRoundDecl declares a pool round the way writeThreadPoolRound
// calls it: a failure comes back through a leading slot, the slice behind a
// pointer followed by its element size, and the thunk last.
func (e *emitter) threadPoolRoundDecl(name string, instr *ir.Instr) string {
	params := []string{}
	if instr.Result.Type != "void" {
		params = append(params, "ptr")
	}
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
// that calls the worker; the runtime cuts chunks and lanes by that size.
func (e *emitter) writeThreadPoolRound(name string, instr *ir.Instr) error {
	dataIndex := threadRoundData[name]
	if len(instr.Args) <= dataIndex {
		return fmt.Errorf("llvm error: %s expects a slice operand", name)
	}
	elem, ok := strings.CutPrefix(instr.Args[dataIndex].Type, "[]")
	if !ok {
		return fmt.Errorf("llvm error: %s data is %s, not a view", name, instr.Args[dataIndex].Type)
	}
	slot := localName(fmt.Sprintf("%%thread.round.%d", e.nextThreadEach))
	e.nextThreadEach++
	args := []string{}
	if instr.Result.Type != "void" {
		fmt.Fprintf(&e.out, "  %s.result = alloca %s\n", slot, e.llvmType(instr.Result.Type))
		args = append(args, "ptr "+slot+".result")
	}
	for index, arg := range instr.Args {
		if index != dataIndex {
			args = append(args, e.llvmType(arg.Type)+" "+e.value(arg).operand)
			continue
		}
		fmt.Fprintf(&e.out, "  %s.data = alloca %%kizu.slice.u8\n", slot)
		fmt.Fprintf(&e.out, "  store %%kizu.slice.u8 %s, ptr %s.data\n", e.value(arg).operand, slot)
		args = append(args, "ptr "+slot+".data", "i64 "+e.elementSizeOperand(elem))
	}
	args = append(args, "ptr @"+threadEachInvokeThunk)
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n", llvmFunctionName(name), strings.Join(args, ", "))
	if instr.Result.Type == "void" {
		return nil
	}
	resultName := localName(instr.Result.Name)
	resultType := e.llvmType(instr.Result.Type)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s.result\n", resultName, resultType, slot)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}
