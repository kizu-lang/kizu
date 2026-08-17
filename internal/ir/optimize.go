package ir

import "strconv"

// Optimize applies bounded local cleanup passes to a module. A pass rewrites
// values in place, so what it produces is verified the same way lowering is.
func Optimize(module *Module) error {
	ConstantFold(module)
	CopyPropagate(module)
	DeadCodeEliminate(module)
	return Verify(module)
}

// ConstantFold folds binary instructions with integer constants.
func ConstantFold(module *Module) {
	for _, fn := range module.Functions {
		for _, block := range fn.Blocks {
			consts := constantsIn(block)
			for _, instr := range block.Instrs {
				foldInstr(instr, consts)
			}
		}
	}
}

// CopyPropagate replaces id instruction results with their source values.
func CopyPropagate(module *Module) {
	for _, fn := range module.Functions {
		copies := map[string]Value{}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				replaceArgs(instr, copies)
				if instr.Op == "id" && len(instr.Args) == 1 {
					copies[instr.Result.Name] = instr.Args[0]
				}
			}
			replaceTerminatorArgs(&block.Terminator, copies)
		}
	}
}

// DeadCodeEliminate removes unused pure instructions.
func DeadCodeEliminate(module *Module) {
	for _, fn := range module.Functions {
		used := usedValues(fn)
		for _, block := range fn.Blocks {
			block.Instrs = keepLiveInstrs(block.Instrs, used)
		}
	}
}

// constantsIn returns integer constants already emitted in a block.
func constantsIn(block *Block) map[string]int64 {
	consts := map[string]int64{}
	for _, instr := range block.Instrs {
		if instr.Op != "const" || instr.Result.Type != "i64" {
			continue
		}
		value, err := strconv.ParseInt(instr.Immediate, 10, 64)
		if err == nil {
			consts[instr.Result.Name] = value
		}
	}
	return consts
}

// foldInstr folds one instruction when both operands are known constants.
func foldInstr(instr *Instr, consts map[string]int64) {
	if len(instr.Args) != 2 || instr.Result.Type != "i64" {
		return
	}
	left, okLeft := consts[instr.Args[0].Name]
	right, okRight := consts[instr.Args[1].Name]
	if !okLeft || !okRight {
		return
	}
	value, ok := foldBinary(instr.Op, left, right)
	if !ok {
		return
	}
	instr.Op = "const"
	instr.Args = nil
	instr.Immediate = strconv.FormatInt(value, 10)
	consts[instr.Result.Name] = value
}

// foldBinary computes supported integer binary operations.
func foldBinary(op string, left int64, right int64) (int64, bool) {
	switch op {
	case "binary.+":
		return left + right, true
	case "binary.-":
		return left - right, true
	case "binary.*":
		return left * right, true
	default:
		return 0, false
	}
}

// replaceArgs applies copy propagation to instruction operands.
func replaceArgs(instr *Instr, copies map[string]Value) {
	for idx, arg := range instr.Args {
		if replacement, ok := copies[arg.Name]; ok {
			instr.Args[idx] = replacement
		}
	}
	for idx, incoming := range instr.Incoming {
		if replacement, ok := copies[incoming.Value.Name]; ok {
			instr.Incoming[idx].Value = replacement
		}
	}
	for idx, field := range instr.Fields {
		if replacement, ok := copies[field.Value.Name]; ok {
			instr.Fields[idx].Value = replacement
		}
	}
	for cleanupIdx, cleanup := range instr.Cleanups {
		for argIdx, arg := range cleanup.Args {
			if replacement, ok := copies[arg.Name]; ok {
				instr.Cleanups[cleanupIdx].Args[argIdx] = replacement
			}
		}
	}
}

// replaceTerminatorArgs applies copy propagation to terminators.
func replaceTerminatorArgs(term *Terminator, copies map[string]Value) {
	if replacement, ok := copies[term.Value.Name]; ok {
		term.Value = replacement
	}
	if replacement, ok := copies[term.Cond.Name]; ok {
		term.Cond = replacement
	}
}

// usedValues returns the set of SSA names read by a function.
func usedValues(fn *Function) map[string]bool {
	used := map[string]bool{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			for _, arg := range instr.Args {
				used[arg.Name] = true
			}
			for _, incoming := range instr.Incoming {
				used[incoming.Value.Name] = true
			}
			for _, field := range instr.Fields {
				used[field.Value.Name] = true
			}
			for _, cleanup := range instr.Cleanups {
				for _, arg := range cleanup.Args {
					used[arg.Name] = true
				}
			}
		}
		used[block.Terminator.Value.Name] = true
		used[block.Terminator.Cond.Name] = true
	}
	return used
}

// keepLiveInstrs keeps instructions with effects or used results.
func keepLiveInstrs(instrs []*Instr, used map[string]bool) []*Instr {
	out := make([]*Instr, 0, len(instrs))
	for _, instr := range instrs {
		if hasEffect(instr) || used[instr.Result.Name] {
			out = append(out, instr)
		}
	}
	return out
}

// hasEffect reports whether an instruction must remain even if its result is unused.
func hasEffect(instr *Instr) bool {
	switch instr.Op {
	case "call.print", "arena.add", "arena.at":
		return true
	default:
		return instr.Result.Type == "void"
	}
}
