package ir

import "strings"

// KeepReachableFunctions removes module functions that cannot be reached from
// the supplied roots through direct calls. Calls to symbols outside the module
// do not add an edge.
func KeepReachableFunctions(module *Module, roots ...string) {
	functions := make(map[string]*Function, len(module.Functions))
	for _, fn := range module.Functions {
		functions[fn.Name] = fn
	}

	reachable := map[string]bool{}
	queue := make([]string, 0, len(roots))
	for _, root := range roots {
		if functions[root] != nil && !reachable[root] {
			reachable[root] = true
			queue = append(queue, root)
		}
	}
	if len(queue) == 0 {
		return
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, callee := range directCallees(functions[name]) {
			if functions[callee] == nil || reachable[callee] {
				continue
			}
			reachable[callee] = true
			queue = append(queue, callee)
		}
	}

	kept := make([]*Function, 0, len(reachable))
	for _, fn := range module.Functions {
		if reachable[fn.Name] {
			kept = append(kept, fn)
		}
	}
	module.Functions = kept
}

// directCallees returns direct call targets from a function body and its
// deferred cleanup paths.
func directCallees(fn *Function) []string {
	callees := []string{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if callee, ok := directCallee(instr.Op); ok {
				callees = append(callees, callee)
			}
			for _, cleanup := range instr.Cleanups {
				if callee, ok := directCallee(cleanup.Op); ok {
					callees = append(callees, callee)
				}
			}
		}
	}
	return callees
}

// directCallee decodes one direct call operation.
func directCallee(op string) (string, bool) {
	if !strings.HasPrefix(op, "call.") {
		return "", false
	}
	return strings.TrimPrefix(op, "call."), true
}
