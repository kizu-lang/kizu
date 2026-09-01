package ir

import "strings"

// KeepReachableFunctions removes module functions that cannot be reached from
// the supplied roots through direct calls or a function address taken on a
// reachable path. Calls to symbols outside the module do not add an edge.
func KeepReachableFunctions(module *Module, roots ...string) {
	keepReachableFunctions(module, "", true, roots...)
}

// KeepTargetReachableFunctions removes functions outside one target's entry
// closure. Only explicit exports for exportABI become additional host roots;
// an empty ABI means that the target exposes no source-declared exports.
func KeepTargetReachableFunctions(module *Module, exportABI string, roots ...string) {
	keepReachableFunctions(module, exportABI, false, roots...)
}

// keepReachableFunctions closes direct reachability over requested roots and
// either every explicit export or the exports accepted by one target.
func keepReachableFunctions(
	module *Module,
	exportABI string,
	allExports bool,
	roots ...string,
) {
	functions := make(map[string]*Function, len(module.Functions))
	for _, fn := range module.Functions {
		functions[fn.Name] = fn
	}

	reachable, queue := seedReachableFunctions(
		module, functions, exportABI, allExports, roots,
	)
	if len(queue) == 0 {
		return
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, callee := range directCallees(functions[name]) {
			enqueueReachableFunction(functions, reachable, &queue, callee)
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

// seedReachableFunctions adds requested entries and matching explicit exports.
func seedReachableFunctions(
	module *Module,
	functions map[string]*Function,
	exportABI string,
	allExports bool,
	roots []string,
) (map[string]bool, []string) {
	reachable := map[string]bool{}
	queue := make([]string, 0, len(roots))
	for _, root := range roots {
		enqueueReachableFunction(functions, reachable, &queue, root)
	}
	for _, fn := range module.Functions {
		exported := fn.ExportABI != "" && (allExports || fn.ExportABI == exportABI)
		if exported {
			enqueueReachableFunction(functions, reachable, &queue, fn.Name)
		}
	}
	return reachable, queue
}

// enqueueReachableFunction queues one defined function at most once.
func enqueueReachableFunction(
	functions map[string]*Function,
	reachable map[string]bool,
	queue *[]string,
	name string,
) {
	if functions[name] == nil || reachable[name] {
		return
	}
	reachable[name] = true
	*queue = append(*queue, name)
}

// directCallees returns direct call targets from a function body and its
// deferred cleanup paths.
func directCallees(fn *Function) []string {
	callees := []string{}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if callee, ok := directCallee(instr.Op); ok && instr.ExternABI == "" {
				callees = append(callees, callee)
			}
			for _, cleanup := range instr.Cleanups {
				if callee, ok := directCallee(cleanup.Op); ok && cleanup.ExternABI == "" {
					callees = append(callees, callee)
				}
			}
		}
	}
	return callees
}

// directCallee decodes one operation that names a function: a direct call, and
// taking a function's address. A name whose only use is as a pointer value is
// still reached, because the call that reaches it is indirect.
func directCallee(op string) (string, bool) {
	if name, ok := strings.CutPrefix(op, "func.addr."); ok {
		return name, true
	}
	if op == "call.indirect" {
		return "", false
	}
	if !strings.HasPrefix(op, "call.") {
		return "", false
	}
	return strings.TrimPrefix(op, "call."), true
}
