package llvm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// threadPoolEachName is the primitive behind std::thread::each.
const threadPoolEachName = "std::internal::builtin::thread_pool_each"

// threadEachInvokeThunk is the one adapter every pool round calls its worker
// through.
const threadEachInvokeThunk = "kizu.thread.each.invoke"

// usesThreadPoolEach reports whether any function starts a pool round.
func (e *emitter) usesThreadPoolEach() bool {
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op == "call."+threadPoolEachName {
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
	if !e.usesThreadPoolEach() {
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

// writeThreadPoolEach lowers one pool round. The runtime does not know T, so
// the call carries the slice behind a pointer, what T measures, and the thunk
// that calls the worker; the runtime cuts chunks by that size.
func (e *emitter) writeThreadPoolEach(instr *ir.Instr) error {
	if len(instr.Args) != 4 {
		return fmt.Errorf("llvm error: thread_pool_each expects pool, data, chunk and worker")
	}
	elem, ok := strings.CutPrefix(instr.Args[1].Type, "[]")
	if !ok {
		return fmt.Errorf("llvm error: thread_pool_each data is %s, not a view", instr.Args[1].Type)
	}
	resultName := localName(fmt.Sprintf("%%thread.each.%d", e.nextThreadEach))
	e.nextThreadEach++
	dataSlot := resultName + ".data"
	fmt.Fprintf(&e.out, "  %s = alloca %%kizu.slice.u8\n", dataSlot)
	fmt.Fprintf(&e.out, "  store %%kizu.slice.u8 %s, ptr %s\n",
		e.value(instr.Args[1]).operand, dataSlot)
	fmt.Fprintf(&e.out,
		"  call void @%s(i64 %s, ptr %s, i64 %s, i64 %s, ptr %s, ptr @%s)\n",
		llvmFunctionName(threadPoolEachName),
		e.value(instr.Args[0]).operand,
		dataSlot,
		e.elementSizeOperand(elem),
		e.value(instr.Args[2]).operand,
		e.value(instr.Args[3]).operand,
		threadEachInvokeThunk,
	)
	return nil
}
