package ir

import (
	"fmt"
	"strings"
)

// inlineLimit is how many instructions a function may hold for Inline to copy
// its body into a caller. A backend that compiles each function alone, as
// wasm's engines do, otherwise pays a call, a frame and a result written to
// memory for each std accessor that is one instruction long.
const inlineLimit = 8

// Inline replaces direct calls to small functions with a copy of the callee's
// body. A callee is copied as it stands when its caller is reached, in module
// order, and the copy is not searched again, so a chain of calls is flattened
// no deeper than the order the functions are declared in allows, and a
// recursive callee is never copied into itself.
func Inline(module *Module) {
	functions := make(map[string]*Function, len(module.Functions))
	for _, fn := range module.Functions {
		functions[fn.Name] = fn
	}
	for _, fn := range module.Functions {
		inlineInto(fn, functions)
	}
}

// inlineInto copies each inlinable callee of fn into it. A single-block callee
// is spliced where its call was; a callee with more blocks splits the calling
// block, and its returns jump to the half after the call.
func inlineInto(fn *Function, functions map[string]*Function) {
	site := 0
	results := map[string]Value{}
	for blockIndex := 0; blockIndex < len(fn.Blocks); blockIndex++ {
		block := fn.Blocks[blockIndex]
		for instrIndex := 0; instrIndex < len(block.Instrs); instrIndex++ {
			call := block.Instrs[instrIndex]
			callee := inlineCallee(fn, call, functions)
			if callee == nil {
				continue
			}
			site++
			copier := newBodyCopier(fmt.Sprintf("in%d.", site), callee, call)
			if len(callee.Blocks) == 1 {
				body := copier.instrs(callee.Blocks[0].Instrs)
				spliced := make([]*Instr, 0, len(block.Instrs)-1+len(body))
				spliced = append(spliced, block.Instrs[:instrIndex]...)
				spliced = append(spliced, body...)
				spliced = append(spliced, block.Instrs[instrIndex+1:]...)
				block.Instrs = spliced
				if call.Result.Type != "void" {
					results[call.Result.Name] = copier.value(callee.Blocks[0].Terminator.Value)
				}
				instrIndex += len(body) - 1
				continue
			}
			inserted := splitAtInlinedCall(fn, blockIndex, instrIndex, callee, copier, results)
			blockIndex += inserted - 1
			break
		}
	}
	if len(results) > 0 {
		replaceFunctionValues(fn, results)
	}
}

// splitAtInlinedCall replaces the call at instrIndex of fn.Blocks[blockIndex]
// with a jump into a copy of callee's blocks, whose returns jump to a block
// holding what followed the call. It returns how many blocks now follow the
// calling block, the last of which is that continuation.
func splitAtInlinedCall(
	fn *Function,
	blockIndex int,
	instrIndex int,
	callee *Function,
	copier *bodyCopier,
	results map[string]Value,
) int {
	block := fn.Blocks[blockIndex]
	call := block.Instrs[instrIndex]
	cont := &Block{
		Name:       copier.prefix + "cont",
		Instrs:     append([]*Instr{}, block.Instrs[instrIndex+1:]...),
		Terminator: block.Terminator,
	}
	retargetPhis(fn, block.Name, cont.Name, block.Terminator.Successors())
	block.Instrs = block.Instrs[:instrIndex]
	block.Terminator = Terminator{Op: "jump", Target: copier.prefix + callee.Blocks[0].Name}

	body := make([]*Block, 0, len(callee.Blocks)+1)
	returns := []Incoming{}
	for _, source := range callee.Blocks {
		copied := &Block{
			Name:       copier.prefix + source.Name,
			Instrs:     copier.instrs(source.Instrs),
			Terminator: copier.terminator(source.Terminator),
		}
		if source.Terminator.Op == "return" {
			returns = append(returns, Incoming{Block: copied.Name, Value: copied.Terminator.Value})
			copied.Terminator = Terminator{Op: "jump", Target: cont.Name}
		}
		body = append(body, copied)
	}
	if call.Result.Type != "void" {
		if len(returns) == 1 {
			results[call.Result.Name] = returns[0].Value
		} else {
			phi := &Instr{Result: call.Result, Op: "phi", Incoming: returns}
			cont.Instrs = append([]*Instr{phi}, cont.Instrs...)
		}
	}
	body = append(body, cont)

	blocks := make([]*Block, 0, len(fn.Blocks)+len(body))
	blocks = append(blocks, fn.Blocks[:blockIndex+1]...)
	blocks = append(blocks, body...)
	blocks = append(blocks, fn.Blocks[blockIndex+1:]...)
	fn.Blocks = blocks
	return len(body)
}

// retargetPhis renames the predecessor `from` to `to` in the phis of the
// blocks named by successors, the edges that moved with a split block's
// terminator.
func retargetPhis(fn *Function, from string, to string, successors []string) {
	for _, block := range fn.Blocks {
		named := false
		for _, successor := range successors {
			if successor == block.Name {
				named = true
			}
		}
		if !named {
			continue
		}
		for _, instr := range block.Instrs {
			for index := range instr.Incoming {
				if instr.Incoming[index].Block == from {
					instr.Incoming[index].Block = to
				}
			}
		}
	}
}

// inlineCallee returns the function a call instruction of fn may be replaced
// with, or nil. The callee must be small, reach no error.try (whose failure
// would return from the caller), not call itself, not be looped back to at its
// entry, and return values of the type it declares. Each argument must be the
// type its parameter reads, or a `&var` borrow of a parameter's `&`.
func inlineCallee(fn *Function, call *Instr, functions map[string]*Function) *Function {
	if call.ExternABI != "" || call.Op == "call.indirect" {
		return nil
	}
	name, ok := strings.CutPrefix(call.Op, "call.")
	if !ok || name == fn.Name {
		return nil
	}
	callee := functions[name]
	if callee == nil || len(callee.Blocks) == 0 || callee.Return != call.Result.Type ||
		len(callee.Params) != len(call.Args) {
		return nil
	}
	for index, param := range callee.Params {
		if !inlineArgumentFits(param.Type, call.Args[index].Type) {
			return nil
		}
	}
	if !inlinableBody(callee) {
		return nil
	}
	return callee
}

// inlineArgumentFits reports whether an argument of type got can stand for a
// parameter of type want once the call is gone.
func inlineArgumentFits(want string, got string) bool {
	if want == got {
		return true
	}
	elem, ok := strings.CutPrefix(want, "&")
	return ok && !strings.HasPrefix(elem, "var ") && got == "&var "+elem
}

// inlinableBody reports whether callee's body can be copied into a caller.
// The body has to return: a call's result is read afterwards as what the
// copy returned, and a single block is spliced in whole, terminator and all,
// only when that terminator is a return.
func inlinableBody(callee *Function) bool {
	count := 0
	returns := 0
	self := "call." + callee.Name
	entry := callee.Blocks[0].Name
	if len(callee.Blocks) == 1 && callee.Blocks[0].Terminator.Op != "return" {
		return false
	}
	for _, block := range callee.Blocks {
		for _, instr := range block.Instrs {
			count++
			if count > inlineLimit || instr.Op == "error.try" || instr.Op == self {
				return false
			}
		}
		term := block.Terminator
		for _, target := range term.Successors() {
			if target == entry {
				return false
			}
		}
		if term.Op == "return" {
			returns++
			if callee.Return != "void" && term.Value.Type != callee.Return {
				return false
			}
		}
	}
	return returns > 0
}

// bodyCopier renames one callee body for one call site: parameters become the
// call's arguments, and every value and block the body defines gains the
// site's prefix.
type bodyCopier struct {
	prefix  string
	params  map[string]Value
	defined map[string]bool
}

// newBodyCopier maps callee's parameters to call's arguments and records the
// names callee's instructions define.
func newBodyCopier(prefix string, callee *Function, call *Instr) *bodyCopier {
	copier := &bodyCopier{prefix: prefix, params: map[string]Value{}, defined: map[string]bool{}}
	for index, param := range callee.Params {
		copier.params[param.Name] = call.Args[index]
	}
	for _, block := range callee.Blocks {
		for _, instr := range block.Instrs {
			copier.defined[instr.Result.Name] = true
		}
	}
	return copier
}

// value renames one value the body reads.
func (c *bodyCopier) value(value Value) Value {
	if arg, ok := c.params[value.Name]; ok {
		return arg
	}
	if value.Name != "" && c.defined[value.Name] {
		return Value{Name: "%" + c.prefix + strings.TrimPrefix(value.Name, "%"), Type: value.Type}
	}
	return value
}

// instrs copies a list of instructions under the site's names.
func (c *bodyCopier) instrs(source []*Instr) []*Instr {
	out := make([]*Instr, 0, len(source))
	for _, instr := range source {
		copied := *instr
		copied.Result = c.value(instr.Result)
		copied.Args = make([]Value, len(instr.Args))
		for index, arg := range instr.Args {
			copied.Args[index] = c.value(arg)
		}
		copied.CallParams = append([]Param(nil), instr.CallParams...)
		copied.Fields = make([]FieldArg, len(instr.Fields))
		for index, field := range instr.Fields {
			copied.Fields[index] = FieldArg{Name: field.Name, Value: c.value(field.Value)}
		}
		copied.Incoming = make([]Incoming, len(instr.Incoming))
		for index, incoming := range instr.Incoming {
			copied.Incoming[index] = Incoming{
				Block: c.prefix + incoming.Block,
				Value: c.value(incoming.Value),
			}
		}
		copied.Cleanups = make([]Cleanup, len(instr.Cleanups))
		for index, cleanup := range instr.Cleanups {
			args := make([]Value, len(cleanup.Args))
			for argIndex, arg := range cleanup.Args {
				args[argIndex] = c.value(arg)
			}
			cleanup.Args = args
			copied.Cleanups[index] = cleanup
		}
		out = append(out, &copied)
	}
	return out
}

// terminator copies a terminator under the site's names.
func (c *bodyCopier) terminator(term Terminator) Terminator {
	copied := Terminator{Op: term.Op, Value: c.value(term.Value), Cond: c.value(term.Cond)}
	if term.Target != "" {
		copied.Target = c.prefix + term.Target
	}
	if term.Else != "" {
		copied.Else = c.prefix + term.Else
	}
	return copied
}

// replaceFunctionValues rewrites every read of a replaced name in fn, following
// a replacement that is itself replaced.
func replaceFunctionValues(fn *Function, replacements map[string]Value) {
	resolve := func(value Value) Value {
		for {
			next, ok := replacements[value.Name]
			if !ok {
				return value
			}
			value = next
		}
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			for index, arg := range instr.Args {
				instr.Args[index] = resolve(arg)
			}
			for index, incoming := range instr.Incoming {
				instr.Incoming[index].Value = resolve(incoming.Value)
			}
			for index, field := range instr.Fields {
				instr.Fields[index].Value = resolve(field.Value)
			}
			for cleanupIndex, cleanup := range instr.Cleanups {
				for argIndex, arg := range cleanup.Args {
					instr.Cleanups[cleanupIndex].Args[argIndex] = resolve(arg)
				}
			}
		}
		block.Terminator.Value = resolve(block.Terminator.Value)
		block.Terminator.Cond = resolve(block.Terminator.Cond)
	}
}
