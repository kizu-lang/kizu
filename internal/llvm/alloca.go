package llvm

import "strings"

// hoistAllocasToEntry moves every `alloca` in a function body into its entry
// block. LLVM folds an entry-block alloca into the function's frame, allocated
// once when the function is entered; an alloca anywhere else allocates on every
// execution and is only reclaimed when the function returns. A loop body
// holding one therefore grows the stack a slot per turn, and a program that
// loops long enough dies on the guard page with nothing in source to point at.
//
// The move is always sound here. Every alloca this backend writes has a fixed
// size and reads no register, so the entry block can hold it; and the slots it
// names are argument buffers, out-parameters, and locals, none of which outlive
// the turn that wrote them, so sharing one slot across turns changes nothing.
func hoistAllocasToEntry(body string) string {
	lines := strings.Split(body, "\n")
	entry := -1
	for index, line := range lines {
		if isBlockLabelLine(line) {
			entry = index
			break
		}
	}
	if entry < 0 {
		return body
	}
	allocas := make([]string, 0, 8)
	kept := make([]string, 0, len(lines))
	for index, line := range lines {
		if index > entry && isAllocaLine(line) {
			allocas = append(allocas, line)
			continue
		}
		kept = append(kept, line)
	}
	if len(allocas) == 0 {
		return body
	}
	hoisted := make([]string, 0, len(kept)+len(allocas))
	hoisted = append(hoisted, kept[:entry+1]...)
	hoisted = append(hoisted, allocas...)
	hoisted = append(hoisted, kept[entry+1:]...)
	return strings.Join(hoisted, "\n")
}

// isAllocaLine reports whether a body line is a register-defining alloca.
func isAllocaLine(line string) bool {
	_, rest, ok := leadingPercentName(strings.TrimLeft(line, " "))
	return ok && strings.HasPrefix(rest, " = alloca ")
}

// isBlockLabelLine reports whether a body line opens a basic block.
func isBlockLabelLine(line string) bool {
	_, ok := labelDefName(line)
	return ok
}
