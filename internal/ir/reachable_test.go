package ir

import "testing"

// TestKeepReachableFunctionsFollowsCallsAddressesAndCleanups verifies the
// executable closure includes ordinary calls, function-pointer targets, and
// deferred error-path calls.
func TestKeepReachableFunctionsFollowsCallsAddressesAndCleanups(t *testing.T) {
	module := &Module{Functions: []*Function{
		functionWithOps("main", "call.live", "func.addr.callback"),
		functionWithOps("live", "array.append"),
		functionWithOps("cleanup", "call.external"),
		functionWithOps("callback", "call.external"),
		functionWithOps("dead", "call.cleanup"),
	}}
	module.Functions[1].Blocks[0].Instrs[0].Cleanups = []Cleanup{{Op: "call.cleanup"}}

	KeepReachableFunctions(module, "main")

	want := []string{"main", "live", "cleanup", "callback"}
	if len(module.Functions) != len(want) {
		t.Fatalf("reachable function count = %d, want %d", len(module.Functions), len(want))
	}
	for index, name := range want {
		if module.Functions[index].Name != name {
			t.Fatalf("function %d = %q, want %q", index, module.Functions[index].Name, name)
		}
	}
}

// TestKeepReachableFunctionsKeepsModuleWithoutRoot preserves library-shaped
// modules when none of the requested executable roots exist.
func TestKeepReachableFunctionsKeepsModuleWithoutRoot(t *testing.T) {
	module := &Module{Functions: []*Function{functionWithOps("library", "call.external")}}

	KeepReachableFunctions(module, "main")

	if len(module.Functions) != 1 || module.Functions[0].Name != "library" {
		t.Fatalf("module without requested root changed: %#v", module.Functions)
	}
}

// functionWithOps builds one test function with the requested operations.
func functionWithOps(name string, ops ...string) *Function {
	instrs := make([]*Instr, 0, len(ops))
	for _, op := range ops {
		instrs = append(instrs, &Instr{Op: op, Result: Value{Type: "void"}})
	}
	return &Function{Name: name, Blocks: []*Block{{Name: "entry", Instrs: instrs}}}
}
