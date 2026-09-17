package wasm

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ir"
)

// A loop that reads an Array's storage pointer and length, or a view's
// pointer and length, reads them through memory on every iteration, and an
// engine keeps them there: a store to an element could have changed them as
// far as it can tell. Where no instruction the loop contains can change them,
// they are read once into locals before the loop is entered and the loop
// reads the locals.
//
// A view is an immutable SSA value, so nothing changes its descriptor. An
// Array's header changes only through an operation that names it, and a loop
// whose every use of the Array is a read of the header, or a write of an
// element, leaves the header as it found it: another reference that could
// change the same header cannot be alive beside the one the loop reads.

// loopHoist is one value whose pointer and length a loop reads from locals.
type loopHoist struct {
	value ir.Value
	// view is set for a `[]T` descriptor, whose length is an i32 word; an
	// Array header's length is an i64.
	view   bool
	data   string
	length string
}

// lengthType is the wasm type of the length local.
func (h loopHoist) lengthType() string {
	if h.view {
		return "i32"
	}
	return "i64"
}

// planLoopHoists decides, for each loop header of fn, which values the loop
// reads through locals set before it. A loop is taken to be every block its
// header dominates, the blocks written inside its `loop`, so the exit paths
// written there are held to the same rule as the body.
func planLoopHoists(fn *ir.Function, s *structure) map[string][]loopHoist {
	plans := map[string][]loopHoist{}
	defined := map[string]string{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Result.Name != "" {
				defined[instr.Result.Name] = block.Name
			}
		}
	}
	count := 0
	for _, header := range s.order {
		if !s.loopHeaders[header] {
			continue
		}
		found, changed := loopHoistCandidates(s, header, defined)
		for _, value := range found {
			if changed[value.Name] {
				continue
			}
			count++
			plans[header] = append(plans[header], loopHoist{
				value:  value,
				view:   strings.HasPrefix(value.Type, "[]"),
				data:   fmt.Sprintf("$__kizu_hoist%d_data", count),
				length: fmt.Sprintf("$__kizu_hoist%d_len", count),
			})
		}
	}
	return plans
}

// loopHoistCandidates lists, in the order the loop first reads them, the
// Arrays and views defined outside the loop headed by header whose pointer and
// length it reads, and names the Arrays it may change.
func loopHoistCandidates(
	s *structure,
	header string,
	defined map[string]string,
) ([]ir.Value, map[string]bool) {
	inside := map[string]bool{}
	for _, name := range s.order {
		if s.dominates(header, name) {
			inside[name] = true
		}
	}
	found := []ir.Value{}
	seen := map[string]bool{}
	changed := map[string]bool{}
	visit := func(instr *ir.Instr, index int, value ir.Value) {
		switch {
		case isArrayWasmType(derefWasmType(value.Type)):
			if instr == nil || index != 0 || !readsArrayHeaderOnly(instr.Op) {
				changed[value.Name] = true
				return
			}
		case strings.HasPrefix(value.Type, "[]"):
			if instr == nil || index != 0 || !strings.HasPrefix(instr.Op, "slice.") {
				return
			}
		default:
			return
		}
		if !seen[value.Name] && !inside[defined[value.Name]] {
			seen[value.Name] = true
			found = append(found, value)
		}
	}
	for _, name := range s.order {
		if inside[name] {
			visitBlockReads(s.blocks[name], visit)
		}
	}
	return found, changed
}

// visitBlockReads calls visit with every value one block reads, the way
// forEachRead does for a whole function.
func visitBlockReads(block *ir.Block, visit func(instr *ir.Instr, index int, value ir.Value)) {
	for _, instr := range block.Instrs {
		for index, arg := range instr.Args {
			visit(instr, index, arg)
		}
		for _, incoming := range instr.Incoming {
			visit(instr, -1, incoming.Value)
		}
		for _, field := range instr.Fields {
			visit(instr, -1, field.Value)
		}
		for _, cleanup := range instr.Cleanups {
			for _, arg := range cleanup.Args {
				visit(instr, -1, arg)
			}
		}
	}
	visit(nil, -1, block.Terminator.Value)
	visit(nil, -1, block.Terminator.Cond)
}

// readsArrayHeaderOnly reports whether an Array operation leaves the header
// of the Array it is handed as it was: it reads the header, and at most
// writes an element or copies the whole value out.
func readsArrayHeaderOnly(op string) bool {
	switch op {
	case "array.len", "array.capacity", "array.get", "array.get_or_panic", "array.at",
		"array.at_mut", "array.set", "array.swap", "array.as_bytes", "ref.load":
		return true
	}
	return false
}

// loopHoistsInOrder lists every planned hoist of the current function in the
// order its loop is reached, which is the order their locals are declared in.
func (e *emitter) loopHoistsInOrder(s *structure) []loopHoist {
	all := []loopHoist{}
	for _, header := range s.order {
		all = append(all, e.loopHoists[header]...)
	}
	return all
}

// writeLoopHoists reads, before the loop headed by header is entered, what
// that loop reads through locals, and makes those locals what its reads use.
// A value an enclosing loop already reads through locals keeps them. It
// returns the values it made current, for endLoopHoists.
func (e *emitter) writeLoopHoists(header string) []string {
	started := []string{}
	for _, hoist := range e.loopHoists[header] {
		if _, active := e.hoisted[hoist.value.Name]; active {
			continue
		}
		base := e.value(hoist.value).expr
		if hoist.view {
			fmt.Fprintf(&e.out, "            (local.set %s (i32.load %s))\n", hoist.data, base)
			fmt.Fprintf(&e.out, "            (local.set %s (i32.load %s))\n",
				hoist.length, addressAt(base, 4))
		} else {
			fmt.Fprintf(&e.out, "            (local.set %s (i32.load %s))\n",
				hoist.data, arrayFieldAddress(base, arrayDataOffset))
			fmt.Fprintf(&e.out, "            (local.set %s (i64.load %s))\n",
				hoist.length, arrayFieldAddress(base, arrayLenOffset))
		}
		e.hoisted[hoist.value.Name] = hoist
		started = append(started, hoist.value.Name)
	}
	return started
}

// endLoopHoists stops the reads writeLoopHoists started from using locals
// once their loop is written.
func (e *emitter) endLoopHoists(started []string) {
	for _, name := range started {
		delete(e.hoisted, name)
	}
}

// viewPointer reads the element pointer of a view descriptor.
func (e *emitter) viewPointer(value ir.Value) string {
	if hoist, ok := e.hoisted[value.Name]; ok {
		return "(local.get " + hoist.data + ")"
	}
	return "(i32.load " + e.value(value).expr + ")"
}

// viewLength reads the i32 length word of a view descriptor.
func (e *emitter) viewLength(value ir.Value) string {
	if hoist, ok := e.hoisted[value.Name]; ok {
		return "(local.get " + hoist.length + ")"
	}
	return "(i32.load " + addressAt(e.value(value).expr, 4) + ")"
}
