package llvm

import (
	"fmt"
	"strings"
)

// hoistAllocasToEntry moves every `alloca` in a function body into its entry
// block. LLVM folds an entry-block alloca into the function's frame, allocated
// once when the function is entered; an alloca anywhere else allocates on every
// execution and is only reclaimed when the function returns. A loop body
// holding one therefore grows the stack a slot per turn, and a program that
// loops long enough dies on the guard page with nothing in source to point at.
//
// The move is sound for the allocas this backend writes. Each has a fixed size
// and reads no register, so the entry block can hold it; and the slots they
// name are argument buffers, out-parameters, and locals, none of which outlive
// the turn that wrote them, so sharing one slot across turns changes nothing.
//
// An alloca with an element count is neither: the count is a register the entry
// block does not have yet, and a fresh slot per turn is the point of writing
// one. This refuses instead of moving it, because reclaiming the stack around a
// dynamic alloca is a decision about the loop it sits in — llvm.stacksave and
// llvm.stackrestore — not something this can invent.
func hoistAllocasToEntry(body string) (string, error) {
	lines := strings.Split(body, "\n")
	entry := -1
	for index, line := range lines {
		if isBlockLabelLine(line) {
			entry = index
			break
		}
	}
	if entry < 0 {
		return body, nil
	}
	allocas := make([]string, 0, 8)
	kept := make([]string, 0, len(lines))
	for index, line := range lines {
		if index > entry && isAllocaLine(line) {
			if err := rejectCountedAlloca(line); err != nil {
				return "", err
			}
			allocas = append(allocas, line)
			continue
		}
		kept = append(kept, line)
	}
	if len(allocas) == 0 {
		return body, nil
	}
	hoisted := make([]string, 0, len(kept)+len(allocas))
	hoisted = append(hoisted, kept[:entry+1]...)
	hoisted = append(hoisted, allocas...)
	hoisted = append(hoisted, kept[entry+1:]...)
	return strings.Join(hoisted, "\n"), nil
}

// rejectCountedAlloca refuses an alloca that takes an element count. Only the
// attributes that describe the one slot -- align, addrspace, inalloca -- may
// follow the type.
func rejectCountedAlloca(line string) error {
	_, rest, ok := leadingPercentName(strings.TrimLeft(line, " "))
	if !ok {
		return nil
	}
	operands := splitTopLevelCommas(strings.TrimPrefix(rest, " = alloca "))
	for _, operand := range operands[1:] {
		switch field := strings.Fields(operand); {
		case len(field) == 0,
			field[0] == "align",
			field[0] == "inalloca",
			strings.HasPrefix(field[0], "addrspace"):
			continue
		}
		return fmt.Errorf(
			"llvm error: emitter bug: `%s` allocates an element count, which the"+
				" entry block cannot hold; a per-turn stack slot needs llvm.stacksave"+
				" and llvm.stackrestore around the loop it sits in",
			strings.TrimSpace(line))
	}
	return nil
}

// splitTopLevelCommas splits an operand list on the commas that separate
// operands, leaving the ones inside an aggregate type spelling alone.
func splitTopLevelCommas(text string) []string {
	parts := make([]string, 0, 3)
	depth := 0
	start := 0
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '{', '[', '<', '(':
			depth++
		case '}', ']', '>', ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, text[start:index])
				start = index + 1
			}
		}
	}
	return append(parts, text[start:])
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
