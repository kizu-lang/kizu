package llvm

import (
	"fmt"
	"strings"
)

// A phi whose type is an optional or an error union carries several values at
// once: an optional's presence and payload, an error union's code and
// payload. LLVM keeps such a phi whole -- it splits aggregates held in memory,
// never ones held in registers -- and everything downstream stops folding
// with it: an `if` chain that answers with `?i64` becomes an indirect jump
// through a table of blocks rather than one load from a table of values, and a
// tag that is a constant on every arm is rebuilt and tested instead of
// deciding the branch. Writing one phi per field says the same thing in the
// form the optimizer reads, and the value is put back together for whoever
// wants it whole; where only the fields are read, the assembled form goes.
//
// The fields are read at the end of the block each value arrives from, which
// is where they are known and where the phi already said the value is.

// rememberAggregate records the field types one declared aggregate holds.
func (e *emitter) rememberAggregate(name string, fields ...string) {
	if e.aggregates == nil {
		e.aggregates = map[string][]string{}
	}
	e.aggregates[name] = fields
}

// occupiesNothing reports whether a payload type has no bytes. A phi of a type
// with no bytes is one the code generator at -O0 does not lower, and a field
// that carries nothing is no reason to split the value it is in, so an
// aggregate holding one is left whole.
func (e *emitter) occupiesNothing(typ string) bool {
	size, _, ok := e.typeLayout(typ)
	return ok && size == 0
}

// aggregateFields answers the field types of a declared aggregate.
func (e *emitter) aggregateFields(name string) ([]string, bool) {
	fields, ok := e.aggregates[name]
	return fields, ok
}

// isBlockLabel reports whether a body line opens a block.
func isBlockLabel(line string) bool {
	return strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ")
}

// isBlockTerminator reports whether a body line is the instruction a block
// ends with.
func isBlockTerminator(line string) bool {
	for _, op := range []string{"  br ", "  ret ", "  switch ", "  indirectbr "} {
		if strings.HasPrefix(line, op) {
			return true
		}
	}
	return line == "  unreachable"
}

// phiArm is one `[ value, %block ]` arm of a phi.
type phiArm struct {
	value string
	block string
}

// parsePhiArms reads the arms of a phi from the text after its type.
func parsePhiArms(text string) []phiArm {
	arms := []phiArm{}
	for {
		open := strings.Index(text, "[ ")
		if open < 0 {
			return arms
		}
		text = text[open+2:]
		comma := strings.Index(text, ", %")
		closing := strings.Index(text, " ]")
		if comma < 0 || closing < comma {
			return arms
		}
		arms = append(arms, phiArm{value: text[:comma], block: text[comma+3 : closing]})
		text = text[closing+2:]
	}
}

// phiParts splits a phi line into its result, its type and the text of its
// arms, or answers false for a line that is not a phi.
func phiParts(line string) (string, string, string, bool) {
	if !strings.HasPrefix(line, "  %") {
		return "", "", "", false
	}
	marker := strings.Index(line, " = phi ")
	if marker < 0 {
		return "", "", "", false
	}
	rest := line[marker+len(" = phi "):]
	space := strings.Index(rest, " ")
	if space < 0 {
		return "", "", "", false
	}
	return line[2:marker], rest[:space], rest[space+1:], true
}

// dropUnreachableTails removes what a block says after it has trapped, and the
// phi arms that named it. A trap ends the block -- the instructions the source
// wrote after a panic cannot run, and a jump that cannot be taken is not an
// edge -- so a phi that still names the block claims a predecessor the block
// no longer is. An inlined body is where that shows: the copy's returns become
// jumps into the caller's continuation, and one of them may be a block that
// only traps.
func dropUnreachableTails(body string) string {
	if !hasUnreachableTail(body) {
		return body
	}
	lines := strings.Split(body, "\n")
	trapped := map[string]bool{}
	kept := make([]string, 0, len(lines))
	block := ""
	dead := false
	for _, line := range lines {
		if isBlockLabel(line) {
			block = strings.TrimSuffix(line, ":")
			dead = false
		} else if dead && line != "" {
			// The empty line is the body's last newline, not an instruction.
			continue
		}
		kept = append(kept, line)
		if line == "  unreachable" {
			trapped[block] = true
			dead = true
		}
	}
	for index, line := range kept {
		result, typeName, armText, ok := phiParts(line)
		if !ok {
			continue
		}
		arms := parsePhiArms(armText)
		live := make([]string, 0, len(arms))
		for _, arm := range arms {
			if !trapped[arm.block] {
				live = append(live, fmt.Sprintf("[ %s, %%%s ]", arm.value, arm.block))
			}
		}
		// A phi with no arm left is in a block nothing reaches; it keeps what
		// it said.
		if len(live) == 0 || len(live) == len(arms) {
			continue
		}
		kept[index] = fmt.Sprintf("  %s = phi %s %s", result, typeName, strings.Join(live, ", "))
	}
	return strings.Join(kept, "\n")
}

// hasUnreachableTail reports whether some block goes on after it has trapped.
// Nearly every function traps somewhere -- a bounds check is enough -- and
// nearly every trap is the last thing its block says, so this is what decides
// whether the body is taken apart at all.
func hasUnreachableTail(body string) bool {
	const trap = "\n  unreachable\n"
	for rest := body; ; {
		at := strings.Index(rest, trap)
		if at < 0 {
			return false
		}
		rest = rest[at+len(trap):]
		next := rest
		if end := strings.IndexByte(rest, '\n'); end >= 0 {
			next = rest[:end]
		}
		if next != "" && !isBlockLabel(next) {
			return true
		}
	}
}

// splitAggregatePhis rewrites every phi of an optional or error union in one
// function body into one phi per field and an assembly of the result.
func (e *emitter) splitAggregatePhis(body string) string {
	if !strings.Contains(body, " = phi %kizu.opt.") && !strings.Contains(body, " = phi %kizu.error.") {
		return body
	}
	lines := strings.Split(body, "\n")
	// reads holds what each block gains before its terminator, and rewritten
	// what each phi line becomes. Both are collected before anything moves,
	// so a phi that a later block feeds is found the same way as one an
	// earlier block does.
	reads := map[string][]string{}
	rewritten := map[int]splitPhi{}
	split := 0
	for index, line := range lines {
		result, typeName, armText, ok := phiParts(line)
		if !ok {
			continue
		}
		fields, ok := e.aggregateFields(typeName)
		if !ok {
			continue
		}
		arms := parsePhiArms(armText)
		if len(arms) == 0 {
			continue
		}
		rewritten[index] = splitOnePhi(reads, result, typeName, fields, arms, split)
		split++
	}
	if split == 0 {
		return body
	}
	out := make([]string, 0, len(lines)+len(reads))
	block := ""
	// The phis have to lead their block, so what assembles a split one waits
	// until the block's last phi has been written.
	pending := []string{}
	for index, line := range lines {
		if isBlockLabel(line) {
			block = strings.TrimSuffix(line, ":")
		}
		if replacement, ok := rewritten[index]; ok {
			out = append(out, replacement.phis...)
			pending = append(pending, replacement.builds...)
			continue
		}
		if _, _, _, isPhi := phiParts(line); !isPhi && len(pending) > 0 {
			out = append(out, pending...)
			pending = pending[:0]
		}
		if isBlockTerminator(line) {
			out = append(out, reads[block]...)
			delete(reads, block)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// splitPhi is what one phi becomes: a phi per field, and the instructions that
// put the fields back together after the block's phis.
type splitPhi struct {
	phis   []string
	builds []string
}

// splitOnePhi returns the lines one phi becomes, and records the field reads
// its arms need in the blocks they arrive from.
func splitOnePhi(
	reads map[string][]string,
	result string,
	typeName string,
	fields []string,
	arms []phiArm,
	split int,
) splitPhi {
	phis := make([]string, 0, len(fields))
	builds := make([]string, 0, len(fields))
	assembled := "zeroinitializer"
	for field, fieldType := range fields {
		incoming := make([]string, 0, len(arms))
		for arm, from := range arms {
			name := fmt.Sprintf("%%%s.split%d.arm%d.f%d", result[1:], split, arm, field)
			reads[from.block] = append(reads[from.block],
				fmt.Sprintf("  %s = extractvalue %s %s, %d", name, typeName, from.value, field))
			incoming = append(incoming, fmt.Sprintf("[ %s, %%%s ]", name, from.block))
		}
		name := fmt.Sprintf("%%%s.split%d.f%d", result[1:], split, field)
		phis = append(phis,
			fmt.Sprintf("  %s = phi %s %s", name, fieldType, strings.Join(incoming, ", ")))
		next := fmt.Sprintf("%%%s.split%d.built%d", result[1:], split, field)
		if field+1 == len(fields) {
			next = result
		}
		builds = append(builds, fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d",
			next, typeName, assembled, fieldType, name, field))
		assembled = next
	}
	return splitPhi{phis: phis, builds: builds}
}

// inlinableBodyLines is the longest body whose returns are gathered. What the
// gathering buys is paid back when LLVM copies the function into a caller,
// and a body past this is past what LLVM copies at -O3; every return it
// gathers there costs a read of each field and an arm of each phi, for a
// merge the optimizer never gets to see from the caller's side.
const inlinableBodyLines = 100

// returnJoinLabel names the block every return of an aggregate goes through
// once the returns are gathered into one.
const returnJoinLabel = "kizu.return.join"

// unifyAggregateReturns gathers the returns of a function that answers an
// optional or error union from more than one place into one return, built
// from one phi per field. Inlining such a function joins its returns in the
// caller, and LLVM joins them the way they are written: returned whole, they
// meet in a phi of the aggregate, which is the shape splitAggregatePhis exists
// to take apart and which nothing downstream can fold. Returned through one
// block that assembles the fields, they meet as the fields, and the caller's
// reads of them fold into the arms they came from.
func (e *emitter) unifyAggregateReturns(body string, returnType string) string {
	fields, ok := e.aggregateFields(returnType)
	if !ok || strings.Count(body, "\n") > inlinableBodyLines {
		return body
	}
	prefix := "  ret " + returnType + " "
	if strings.Count(body, "\n"+prefix) < 2 {
		return body
	}
	lines := strings.Split(body, "\n")
	arms := []phiArm{}
	out := make([]string, 0, len(lines)+len(fields)*4)
	block := ""
	for _, line := range lines {
		if isBlockLabel(line) {
			block = strings.TrimSuffix(line, ":")
		}
		if !strings.HasPrefix(line, prefix) {
			out = append(out, line)
			continue
		}
		arm := len(arms)
		value := strings.TrimPrefix(line, prefix)
		for field := range fields {
			out = append(out, fmt.Sprintf("  %%%s.arm%d.f%d = extractvalue %s %s, %d",
				returnJoinLabel, arm, field, returnType, value, field))
		}
		out = append(out, "  br label %"+returnJoinLabel)
		arms = append(arms, phiArm{value: value, block: block})
	}
	// The body ends in the newline after its last terminator; the join goes
	// after it, and ends the same way.
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	out = append(out, returnJoinLabel+":")
	for field, fieldType := range fields {
		incoming := make([]string, 0, len(arms))
		for index, arm := range arms {
			incoming = append(incoming,
				fmt.Sprintf("[ %%%s.arm%d.f%d, %%%s ]", returnJoinLabel, index, field, arm.block))
		}
		out = append(out, fmt.Sprintf("  %%%s.f%d = phi %s %s",
			returnJoinLabel, field, fieldType, strings.Join(incoming, ", ")))
	}
	assembled := "zeroinitializer"
	for field, fieldType := range fields {
		next := fmt.Sprintf("%%%s.built%d", returnJoinLabel, field)
		out = append(out, fmt.Sprintf("  %s = insertvalue %s %s, %s %%%s.f%d, %d",
			next, returnType, assembled, fieldType, returnJoinLabel, field, field))
		assembled = next
	}
	out = append(out, "  ret "+returnType+" "+assembled, "")
	return strings.Join(out, "\n")
}
