package llvm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// threadRoundShape is where a pool round's `[]T` operand is and how many
// operands it has without states. The runtime is untyped, so the call carries
// what T measures right after that operand, the states' Array (or null) with
// what S measures before the worker, and the thunk that calls the worker last.
type threadRoundShape struct {
	data, plain int
}

var threadRoundShapes = map[string]threadRoundShape{
	"std::internal::builtin::thread_pool_each":      {data: 1, plain: 4},
	"std::internal::builtin::thread_pool_each_lane": {data: 2, plain: 6},
}

// isThreadPoolRound reports whether name starts a pool round.
func isThreadPoolRound(name string) bool {
	_, ok := threadRoundShapes[name]
	return ok
}

// threadRoundHasStates reports whether a round hands each thread a state:
// its `[]S` operand sits between the shape's operands and the worker.
func threadRoundHasStates(name string, instr *ir.Instr) bool {
	return len(instr.Args) > threadRoundShapes[name].plain
}

// threadRoundSet names the error set a pool round's worker fails with: the
// round returns `E!void` with the same E.
func (e *emitter) threadRoundSet(instr *ir.Instr) string {
	set, _, _ := e.errorUnionParts(instr.Result.Type)
	return set
}

// threadInvoke is one adapter the module's rounds call workers through: a
// worker error set, and whether the worker takes a state first.
type threadInvoke struct {
	set    string
	states bool
}

// threadInvokeThunkName is the adapter a round whose worker fails with `set`
// calls that worker through.
func threadInvokeThunkName(invoke threadInvoke) string {
	if invoke.states {
		return "kizu.thread.each.state.invoke." + llvmNamePart(invoke.set)
	}
	return "kizu.thread.each.invoke." + llvmNamePart(invoke.set)
}

// threadRoundInvokes lists the adapters the module's pool rounds use.
func (e *emitter) threadRoundInvokes() []threadInvoke {
	seen := map[threadInvoke]bool{}
	for _, fn := range e.module.Functions {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				name := strings.TrimPrefix(instr.Op, "call.")
				if isThreadPoolRound(name) {
					seen[threadInvoke{e.threadRoundSet(instr), threadRoundHasStates(name, instr)}] = true
				}
			}
		}
	}
	invokes := make([]threadInvoke, 0, len(seen))
	for invoke := range seen {
		invokes = append(invokes, invoke)
	}
	sort.Slice(invokes, func(i, j int) bool {
		if invokes[i].states != invokes[j].states {
			return !invokes[i].states
		}
		return invokes[i].set < invokes[j].set
	})
	return invokes
}

// writeThreadEachInvokeThunks emits the typed side of the pool's callback
// ABI, one adapter per worker error set and state or none. A worker is a
// Kizu `fn(&var []T, i64) -> E!void`, or `fn(&var S, &var []T, i64) ->
// E!void`: it takes the view as its {ptr, len} value and returns E's union,
// and a C caller doing either by value would be leaning on the target's
// aggregate rules. The runtime hands the chunk behind a pointer, the state as
// the pointer `&var S` already is, and is told the failure code -- zero for
// success -- and nothing else. Every view is the same pair whatever T is, so
// T and S need no adapter of their own.
func (e *emitter) writeThreadEachInvokeThunks() {
	for _, invoke := range e.threadRoundInvokes() {
		unionType := e.llvmType(invoke.set + "!void")
		fmt.Fprintf(&e.out,
			"define internal i64 @%s(ptr %%kizu.worker, ptr %%kizu.state,"+
				" ptr %%kizu.part, i64 %%kizu.index) #0 {\n",
			threadInvokeThunkName(invoke))
		e.out.WriteString("entry:\n")
		e.out.WriteString("  %kizu.part.value = load %kizu.slice.u8, ptr %kizu.part\n")
		state := ""
		if invoke.states {
			state = "ptr %kizu.state, "
		}
		fmt.Fprintf(&e.out,
			"  %%kizu.result = call %s %%kizu.worker("+
				"%s%%kizu.slice.u8 %%kizu.part.value, i64 %%kizu.index)\n",
			unionType, state)
		fmt.Fprintf(&e.out,
			"  %%kizu.code = extractvalue %s %%kizu.result, %d\n", unionType, errorCodeField)
		e.out.WriteString("  ret i64 %kizu.code\n}\n\n")
	}
}

// threadPoolRoundDecl declares a pool round the way writeThreadPoolRound
// calls it: the result through a leading slot, the slice behind a pointer
// followed by its element size, the states' Array and what S measures, and
// the thunk last.
func (e *emitter) threadPoolRoundDecl(name string, instr *ir.Instr) string {
	shape := threadRoundShapes[name]
	params := []string{"ptr"}
	for index, arg := range instr.Args[:shape.plain-1] {
		if index == shape.data {
			params = append(params, "ptr", "i64")
			continue
		}
		params = append(params, e.llvmType(arg.Type))
	}
	params = append(params, "ptr", "i64", "ptr", "ptr")
	return fmt.Sprintf("declare void @%s(%s)", llvmFunctionName(name), strings.Join(params, ", "))
}

// writeThreadPoolRound lowers one pool round. The runtime does not know T or
// S, so the call carries the slice behind a pointer and what T measures, the
// address of the states' Array and what S measures (a null pointer when the
// round has none), and the thunk that calls the worker; the runtime cuts chunks and lanes by those
// sizes and writes the round's `E!void` into the leading slot.
func (e *emitter) writeThreadPoolRound(name string, instr *ir.Instr) error {
	shape := threadRoundShapes[name]
	if len(instr.Args) < shape.plain {
		return fmt.Errorf("llvm error: %s expects %d operands", name, shape.plain)
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
	for index, arg := range instr.Args[:shape.plain-1] {
		if index != shape.data {
			args = append(args, e.llvmType(arg.Type)+" "+e.value(arg).operand)
			continue
		}
		view, err := e.writeThreadRoundView(name, slot+".data", arg)
		if err != nil {
			return err
		}
		args = append(args, view)
	}
	states := threadRoundHasStates(name, instr)
	if states {
		arg := instr.Args[shape.plain-1]
		state, ok := strings.CutPrefix(arg.Type, "&var std::array::Array<")
		if !ok {
			return fmt.Errorf("llvm error: %s states are %s, not an Array", name, arg.Type)
		}
		state = strings.TrimSuffix(state, ">")
		args = append(args, "ptr "+e.value(arg).operand+", i64 "+e.elementSizeOperand(state))
	} else {
		args = append(args, "ptr null, i64 0")
	}
	worker := instr.Args[len(instr.Args)-1]
	args = append(args,
		e.llvmType(worker.Type)+" "+e.value(worker).operand,
		"ptr @"+threadInvokeThunkName(threadInvoke{set, states}))
	fmt.Fprintf(&e.out, "  call void @%s(%s)\n", llvmFunctionName(name), strings.Join(args, ", "))
	resultName := localName(instr.Result.Name)
	fmt.Fprintf(&e.out, "  %s = load %s, ptr %s.result\n", resultName, resultType, slot)
	e.values[instr.Result.Name] = valueInfo{typ: instr.Result.Type, operand: resultName}
	return nil
}

// writeThreadRoundView stores a round's `[]T` operand in `slot` and answers
// the two arguments the runtime takes for it: the slot, and what T measures.
func (e *emitter) writeThreadRoundView(name string, slot string, arg ir.Value) (string, error) {
	elem, ok := strings.CutPrefix(arg.Type, "[]")
	if !ok {
		return "", fmt.Errorf("llvm error: %s operand is %s, not a view", name, arg.Type)
	}
	fmt.Fprintf(&e.out, "  %s = alloca %%kizu.slice.u8\n", slot)
	fmt.Fprintf(&e.out, "  store %%kizu.slice.u8 %s, ptr %s\n", e.value(arg).operand, slot)
	return "ptr " + slot + ", i64 " + e.elementSizeOperand(elem), nil
}
