package ir

import (
	"strings"
	"testing"
)

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

// TestKeepReachableFunctionsRootsExplicitExports preserves callbacks the host
// can enter even when main never calls them.
func TestKeepReachableFunctionsRootsExplicitExports(t *testing.T) {
	exported := functionWithOps("callback", "call.helper")
	exported.ExportABI = "browser"
	exported.ExportName = "callback"
	module := &Module{Functions: []*Function{
		functionWithOps("main"),
		exported,
		functionWithOps("helper"),
		functionWithOps("dead"),
	}}

	KeepReachableFunctions(module, "main")

	want := []string{"main", "callback", "helper"}
	if len(module.Functions) != len(want) {
		t.Fatalf("reachable function count = %d, want %d", len(module.Functions), len(want))
	}
	for index, name := range want {
		if module.Functions[index].Name != name {
			t.Fatalf("function %d = %q, want %q", index, module.Functions[index].Name, name)
		}
	}
}

// TestKeepTargetReachableFunctionsRootsOnlyMatchingExports prevents one
// target's host adapter from entering another target's backend.
func TestKeepTargetReachableFunctionsRootsOnlyMatchingExports(t *testing.T) {
	browser := functionWithOps("browser_callback", "call.browser_helper")
	browser.ExportABI = "browser"
	other := functionWithOps("other_callback", "call.other_helper")
	other.ExportABI = "other"
	module := &Module{Functions: []*Function{
		functionWithOps("main"),
		browser,
		functionWithOps("browser_helper"),
		other,
		functionWithOps("other_helper"),
	}}

	KeepTargetReachableFunctions(module, "browser", "main")

	want := []string{"main", "browser_callback", "browser_helper"}
	if len(module.Functions) != len(want) {
		t.Fatalf("reachable function count = %d, want %d", len(module.Functions), len(want))
	}
	for index, name := range want {
		if module.Functions[index].Name != name {
			t.Fatalf("function %d = %q, want %q", index, module.Functions[index].Name, name)
		}
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

// TestLinkInputsFollowTheCallsThatRemain links what the remaining foreign
// calls declared, once each, and nothing for a declaration no kept call names.
func TestLinkInputsFollowTheCallsThatRemain(t *testing.T) {
	module := &Module{
		Externs: map[string]Extern{
			"cbrt":   {ABI: "c", Library: "m"},
			"sqrt":   {ABI: "c", Library: "m"},
			"fft":    {ABI: "c", Library: "fftw3"},
			"unused": {ABI: "c", Library: "never", Framework: "Never"},
			"vdsp":   {ABI: "c", Framework: "Accelerate"},
		},
		Functions: []*Function{{
			Name: "main",
			Blocks: []*Block{{Instrs: []*Instr{
				{Op: "call.cbrt", ExternABI: "c", ExternName: "cbrt"},
				{Op: "call.sqrt", ExternABI: "c", ExternName: "sqrt"},
				{Op: "call.fft", ExternABI: "c", ExternName: "fft"},
				{Op: "error.try", Cleanups: []Cleanup{{ExternABI: "c", ExternName: "vdsp"}}},
			}}},
		}},
	}
	libraries, frameworks := LinkInputs(module)
	if got := strings.Join(libraries, " "); got != "fftw3 m" {
		t.Fatalf("libraries = %q", got)
	}
	if got := strings.Join(frameworks, " "); got != "Accelerate" {
		t.Fatalf("frameworks = %q", got)
	}
}
