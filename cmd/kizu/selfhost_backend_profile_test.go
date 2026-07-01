package main

import (
	"strings"
	"testing"
)

// TestSelfhostBackendProfileFlushesIncrementallyOnlyWhenRequested keeps detailed
// backend profile I/O from dominating phase timing runs unless explicitly requested.
func TestSelfhostBackendProfileFlushesIncrementallyOnlyWhenRequested(t *testing.T) {
	profile := readSelfhostFile(t, "../../selfhost/src/backend/profile.kizu")
	start := selfhostKizuFunctionBody(t, profile, "pub fn start(")
	if strings.Contains(start, "KIZU_SELFHOST_STAGE_PROFILE") {
		t.Fatal("backend profile must not be enabled implicitly by stage profiling")
	}
	for _, fragment := range []string{
		`std::process::env_or_empty("KIZU_SELFHOST_BACKEND_PROFILE")`,
		`std::process::env_or_empty("KIZU_SELFHOST_BACKEND_PROFILE_FLUSH_EACH")`,
		`let enabled = std::mem::equal_bytes(backend_enabled, "1");`,
		`let flush_each = std::mem::equal_bytes(flush_each_value, "1");`,
		`flush_each: flush_each,`,
	} {
		if !strings.Contains(start, fragment) {
			t.Fatalf("backend profile start missing %q", fragment)
		}
	}

	begin := selfhostKizuFunctionBody(t, profile, "pub fn begin(")
	end := selfhostKizuFunctionBody(t, profile, "pub fn end(")
	beginComponent := selfhostKizuFunctionBody(t, profile, "pub fn begin_component_member(")
	endComponent := selfhostKizuFunctionBody(t, profile, "pub fn end_component_member(")
	for name, body := range map[string]string{
		"begin":                  begin,
		"end":                    end,
		"begin_component_member": beginComponent,
		"end_component_member":   endComponent,
	} {
		if !strings.Contains(body, "try flush_incremental(profile, out, io);") {
			t.Fatalf("%s does not use incremental profile flushing", name)
		}
		if strings.Contains(body, "try flush(profile, out, io);") {
			t.Fatalf("%s still flushes every profile event unconditionally", name)
		}
	}

	flushIncremental := selfhostKizuFunctionBody(t, profile, "fn flush_incremental(")
	for _, fragment := range []string{
		"if !profile.flush_each {",
		"try flush(profile, out, io);",
	} {
		if !strings.Contains(flushIncremental, fragment) {
			t.Fatalf("flush_incremental missing %q", fragment)
		}
	}

	finish := selfhostKizuFunctionBody(t, profile, "pub fn finish(")
	if !strings.Contains(finish, "try flush(profile, out, io);") {
		t.Fatal("backend profile finish must write the final profile")
	}
}
