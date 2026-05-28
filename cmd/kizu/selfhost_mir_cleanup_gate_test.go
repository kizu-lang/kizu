package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestSelfhostMirOwnerAggregatesHaveDeinit is the #1001 focused gate: every MIR
// owner aggregate in the selfhost backend must expose a source-visible
// deinit(self: T) -> void. A new owner-containing MIR record without cleanup
// fails this test instead of silently leaking, since the ownership checker does
// not yet flag leaked owner aggregates.
func TestSelfhostMirOwnerAggregatesHaveDeinit(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/backend/compiled_mir.kizu")
	structs := mirStructBodies(source)
	owners := mirOwnerAggregates(structs)
	if len(owners) == 0 {
		t.Fatal("expected to find MIR owner aggregates in compiled_mir.kizu")
	}
	for _, name := range owners {
		deinit := "fn deinit(self: " + name + ") -> void"
		if !strings.Contains(source, deinit) {
			t.Errorf("MIR owner aggregate %s has no source-visible `%s`", name, deinit)
		}
	}
}

// TestSelfhostMirFunctionConsumersDeinit is the #1001 focused gate for consume
// sites: every read-only render of a lowered MirFunction must be paired with an
// explicit cleanup of the local owner, so the lowered function is not leaked.
func TestSelfhostMirFunctionConsumersDeinit(t *testing.T) {
	source := readSelfhostFile(t, "../../selfhost/src/backend/compiled_llvm.kizu")
	lines := strings.Split(source, "\n")
	renders := 0
	for idx, line := range lines {
		if !strings.Contains(line, "append_function(out, &mir);") {
			continue
		}
		renders++
		if idx == 0 || strings.TrimSpace(lines[idx-1]) != "defer mir.deinit();" {
			t.Errorf("append_function render at line %d is not preceded by `defer mir.deinit();`", idx+1)
		}
	}
	if renders == 0 {
		t.Fatal("expected MirFunction render sites in compiled_llvm.kizu")
	}
}

// mirStructBodies maps each `pub struct MirX` name to its brace body text.
func mirStructBodies(source string) map[string]string {
	bodies := map[string]string{}
	re := regexp.MustCompile(`(?s)pub struct (Mir\w+) \{(.*?)\n\}`)
	for _, match := range re.FindAllStringSubmatch(source, -1) {
		bodies[match[1]] = match[2]
	}
	return bodies
}

// mirOwnerAggregates returns the MIR struct names that own resources, either an
// `std::array::Array<...>` field directly or a field of another owner aggregate.
func mirOwnerAggregates(structs map[string]string) []string {
	owners := map[string]bool{}
	for name, body := range structs {
		if strings.Contains(body, "std::array::Array<") {
			owners[name] = true
		}
	}
	// Propagate ownership through fields typed as a known owner aggregate until
	// the set stops growing (handles MirBlock -> MirInstruction, MirFunction ->
	// MirBlock).
	for changed := true; changed; {
		changed = false
		for name, body := range structs {
			if owners[name] {
				continue
			}
			for owner := range owners {
				if fieldOfType(body, owner) {
					owners[name] = true
					changed = true
					break
				}
			}
		}
	}
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	return names
}

// fieldOfType reports whether a struct body declares a field of exactly type.
func fieldOfType(body string, typeName string) bool {
	for _, line := range strings.Split(body, "\n") {
		field := strings.TrimSpace(line)
		if strings.HasSuffix(field, ": "+typeName+",") || strings.HasSuffix(field, ": "+typeName) {
			return true
		}
	}
	return false
}
