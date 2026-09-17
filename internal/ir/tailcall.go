package ir

import (
	"fmt"
	"strings"
)

// EliminateTailRecursion turns a function's calls to itself in tail position
// into jumps back to its start. A call whose result the function returns as it
// is becomes a jump with the call's arguments as the new parameters. A call
// whose result is combined with another value by one integer operation that is
// associative and commutative -- `+`, `*`, `|` or `^`, which wrap -- and
// then returned becomes a jump too, with the other value folded into an
// accumulator that every other return combines with what it returns: the
// operation gives the same bits in either order, so `fib(n - 1) + fib(n - 2)`
// can run the second call as the next iteration of a loop.
//
// The calls the function makes run in the order they did, and a recursion that
// used one frame per level runs in one; nothing else about what the function
// does changes. A function is left alone unless every parameter is a scalar a
// phi can carry, since a parameter at an address would be read from the frame
// the loop keeps writing.
func EliminateTailRecursion(module *Module) {
	for _, fn := range module.Functions {
		eliminateTailRecursionIn(fn)
	}
}

// tailSite is one return a tail call ends in: the block, the call, and for an
// accumulated call the operation and the value it combines the result with.
type tailSite struct {
	block *Block
	call  *Instr
	op    string
	other Value
}

// accumulatorOps are the integer operations a tail call's result may be
// combined with, and the value each starts from.
var accumulatorOps = map[string]string{
	"binary.+": "0",
	"binary.*": "1",
	"binary.|": "0",
	"binary.^": "0",
}

// eliminateTailRecursionIn rewrites fn when it has a tail call to itself.
func eliminateTailRecursionIn(fn *Function) {
	if len(fn.Blocks) == 0 || fn.Name == "main" || fn.ExportABI != "" {
		return
	}
	for _, param := range fn.Params {
		if param.Passing != PassValue || !isLoopScalar(param.Type) {
			return
		}
	}
	sites := []tailSite{}
	op := ""
	for _, block := range fn.Blocks {
		site, ok := tailSiteOf(fn, block)
		if !ok {
			continue
		}
		if site.op != "" {
			if op != "" && op != site.op {
				return
			}
			op = site.op
		}
		sites = append(sites, site)
	}
	if len(sites) == 0 {
		return
	}
	if op != "" && !isWrappingInteger(fn.Return) {
		return
	}
	rewriteTailRecursion(fn, sites, op)
}

// isLoopScalar reports whether a value of typ is one register a loop phi can
// carry on every backend.
func isLoopScalar(typ string) bool {
	return typ == "bool" || typ == "f32" || typ == "f64" || isWrappingInteger(typ)
}

// isWrappingInteger reports whether typ is an integer type, whose `+`, `*`
// and bitwise operations wrap.
func isWrappingInteger(typ string) bool {
	switch typ {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "isize", "usize":
		return true
	}
	return false
}

// tailSiteOf reports the tail call block ends in, if any: a return of a call
// to fn, or of one accumulator operation on such a call and a value that does
// not depend on it. Only pure instructions may stand between the call and the
// return.
func tailSiteOf(fn *Function, block *Block) (tailSite, bool) {
	term := block.Terminator
	if term.Op != "return" || term.Value.Type != fn.Return || fn.Return == "void" {
		return tailSite{}, false
	}
	self := "call." + fn.Name
	for index := len(block.Instrs) - 1; index >= 0; index-- {
		call := block.Instrs[index]
		if call.Op != self {
			continue
		}
		if call.ExternABI != "" || len(call.Args) != len(fn.Params) || len(call.Cleanups) > 0 {
			return tailSite{}, false
		}
		after := block.Instrs[index+1:]
		if len(after) == 0 {
			return tailSite{block: block, call: call}, term.Value.Name == call.Result.Name
		}
		return accumulatedSite(block, call, after)
	}
	return tailSite{}, false
}

// accumulatedSite reports the site a call makes when what follows it in its
// block is pure instructions that do not read its result, then one
// accumulator operation on the result and another value that the block
// returns.
func accumulatedSite(block *Block, call *Instr, after []*Instr) (tailSite, bool) {
	combine := after[len(after)-1]
	if combine.Result.Name != block.Terminator.Value.Name || len(combine.Args) != 2 {
		return tailSite{}, false
	}
	if _, ok := accumulatorOps[combine.Op]; !ok {
		return tailSite{}, false
	}
	other := combine.Args[0]
	if other.Name == call.Result.Name {
		other = combine.Args[1]
	} else if combine.Args[1].Name != call.Result.Name {
		return tailSite{}, false
	}
	if other.Name == call.Result.Name {
		return tailSite{}, false
	}
	for _, instr := range after[:len(after)-1] {
		if !isPureOp(instr.Op) || readsValue(instr, call.Result.Name) {
			return tailSite{}, false
		}
	}
	return tailSite{block: block, call: call, op: combine.Op, other: other}, true
}

// readsValue reports whether instr reads the value named name.
func readsValue(instr *Instr, name string) bool {
	found := false
	renameInstrReads(instr, func(value Value) Value {
		found = found || value.Name == name
		return value
	})
	return found
}

// rewriteTailRecursion puts a new entry block before fn's body, turns the old
// entry into a loop header whose phis stand for the parameters and the
// accumulator, and turns each site into a jump to it. A parameter's phi is
// named after it, and a parameter's name is an identifier, which holds no dot,
// so the accumulator's values are named under `%tre.acc.` where no parameter's
// phi can be.
func rewriteTailRecursion(fn *Function, sites []tailSite, op string) {
	start := fn.Blocks[0]
	entry := &Block{Name: "tre.entry", Terminator: Terminator{Op: "jump", Target: start.Name}}
	params := map[string]Value{}
	phis := []*Instr{}
	for _, param := range fn.Params {
		phi := &Instr{
			Result:   Value{Name: "%tre." + strings.TrimPrefix(param.Name, "%"), Type: param.Type},
			Op:       "phi",
			Incoming: []Incoming{{Block: entry.Name, Value: param.Value()}},
		}
		params[param.Name] = phi.Result
		phis = append(phis, phi)
	}
	replaceFunctionValues(fn, params)
	var acc *Instr
	if op != "" {
		identity := &Instr{
			Result:    Value{Name: "%tre.acc.identity", Type: fn.Return},
			Op:        "const",
			Immediate: accumulatorOps[op],
		}
		entry.Instrs = append(entry.Instrs, identity)
		acc = &Instr{
			Result:   Value{Name: "%tre.acc.phi", Type: fn.Return},
			Op:       "phi",
			Incoming: []Incoming{{Block: entry.Name, Value: identity.Result}},
		}
		phis = append(phis, acc)
	}
	isSite := map[*Block]bool{}
	for index, site := range sites {
		isSite[site.block] = true
		if param, ok := params[site.other.Name]; ok {
			site.other = param
		}
		rewriteTailSite(site, index, start.Name, phis, acc)
	}
	if acc != nil {
		for index, block := range fn.Blocks {
			if isSite[block] || block.Terminator.Op != "return" {
				continue
			}
			combined := &Instr{
				Result: Value{Name: fmt.Sprintf("%%tre.acc.ret%d", index), Type: fn.Return},
				Op:     op,
				Args:   []Value{acc.Result, block.Terminator.Value},
			}
			block.Instrs = append(block.Instrs, combined)
			block.Terminator.Value = combined.Result
		}
	}
	start.Instrs = append(phis, start.Instrs...)
	fn.Blocks = append([]*Block{entry}, fn.Blocks...)
}

// rewriteTailSite replaces one site's call, and its accumulator operation, by
// a jump to the loop header feeding the header's phis.
func rewriteTailSite(site tailSite, index int, header string, phis []*Instr, acc *Instr) {
	block := site.block
	kept := []*Instr{}
	for _, instr := range block.Instrs {
		if instr == site.call || instr.Result.Name == block.Terminator.Value.Name {
			continue
		}
		kept = append(kept, instr)
	}
	block.Instrs = kept
	for position, arg := range site.call.Args {
		phis[position].Incoming = append(phis[position].Incoming, Incoming{Block: block.Name, Value: arg})
	}
	if acc != nil {
		next := acc.Result
		if site.op != "" {
			folded := &Instr{
				Result: Value{Name: fmt.Sprintf("%%tre.acc.site%d", index), Type: acc.Result.Type},
				Op:     site.op,
				Args:   []Value{acc.Result, site.other},
			}
			block.Instrs = append(block.Instrs, folded)
			next = folded.Result
		}
		acc.Incoming = append(acc.Incoming, Incoming{Block: block.Name, Value: next})
	}
	block.Terminator = Terminator{Op: "jump", Target: header}
}
