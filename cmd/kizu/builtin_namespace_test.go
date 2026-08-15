package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/stdprim"
)

// builtinCallPattern finds the `std::internal::builtin::` calls std source makes.
var builtinCallPattern = regexp.MustCompile(`std::internal::builtin::([a-z_0-9]+)`)

// goBuiltinPattern finds the primitives the Go sources name.
var goBuiltinPattern = regexp.MustCompile(`"std::internal::builtin::([a-z_0-9]+)"`)

// TestBuiltinNamespaceIsClosedToUserCode keeps the primitive namespace shut.
// One case is the whole fact: what closes it is where `std::internal::builtin`
// sits, not a list with an entry per primitive, so a primitive added tomorrow is
// closed the moment it is named rather than when someone remembers it.
//
// `std::internal::builtin::io_blocking()` once returned an Io capability to any
// program that asked for one, because the guard was written per family and that
// family never got one.
func TestBuiltinNamespaceIsClosedToUserCode(t *testing.T) {
	source := "fn main() -> void {\n    print(std::internal::builtin::mem_len(\"abc\"));\n}\n"
	path := filepath.Join(t.TempDir(), "reserved.kizu")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	raw, err := kizuCommand("check", path).CombinedOutput()
	out := string(raw)
	if err == nil {
		t.Fatalf("user code accepted, output %q", out)
	}
	if !strings.Contains(out, "`std::internal::builtin` is internal to `std`") {
		t.Fatalf("rejected for the wrong reason: %s", firstLineOf(out))
	}
}

// TestBuiltinRegistryCoversStd keeps the registry ahead of std. A primitive std
// calls but the registry does not name is one nothing reports as a misspelling.
func TestBuiltinRegistryCoversStd(t *testing.T) {
	known := map[string]bool{}
	for _, name := range stdprim.BuiltinNames() {
		known[name] = true
	}
	missing := map[string]bool{}
	for _, path := range kizuSourcePaths(t, "../../lib/kizu/std") {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, match := range builtinCallPattern.FindAllStringSubmatch(string(source), -1) {
			name := "std::internal::builtin::" + match[1]
			if !known[name] {
				missing[name] = true
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Fatalf("std calls primitives the registry does not name: %s",
		strings.Join(names, ", "))
}

// TestBuiltinRegistryNamesRealPrimitives checks the registry the other
// way. A name nothing implements reserves a spelling that does not exist, which
// is how `std.builtin.process_spawn_wait` got in: it is the prefix of
// `process_spawn_wait8`, and a pattern that stopped before the digit read it as
// a separate primitive.
func TestBuiltinRegistryNamesRealPrimitives(t *testing.T) {
	implemented := map[string]bool{}
	for _, path := range kizuSourcePaths(t, "../../lib/kizu/std") {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, match := range builtinCallPattern.FindAllStringSubmatch(string(source), -1) {
			implemented["std::internal::builtin::"+match[1]] = true
		}
	}
	for _, path := range []string{
		"../../internal/types/checker.go",
		"../../internal/ownership/checker.go",
		"../../internal/ir/lower.go",
		"../../internal/ir/helpers.go",
		"../../internal/stdprim/registry.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, match := range goBuiltinPattern.FindAllStringSubmatch(string(source), -1) {
			implemented["std::internal::builtin::"+match[1]] = true
		}
	}
	unknown := []string{}
	for _, name := range stdprim.BuiltinNames() {
		if !implemented[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		t.Fatalf("the registry names primitives nothing implements: %s",
			strings.Join(unknown, ", "))
	}
}

// firstLineOf trims command output to its first line.
func firstLineOf(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}
