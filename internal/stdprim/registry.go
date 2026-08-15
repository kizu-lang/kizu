package stdprim

import (
	"sort"
	"strings"
)

// ArgKind identifies a primitive argument shape shared by static checkers.
type ArgKind string

const (
	// ArgBytes is a read-only byte slice argument.
	ArgBytes ArgKind = "[]u8"
	// ArgI64 is a signed 64-bit integer argument.
	ArgI64 ArgKind = "i64"
	// ArgIo is an explicit I/O capability argument.
	ArgIo ArgKind = "Io"
)

// CoreSignature describes simple std::internal::builtin calls with no ownership transfer.
type CoreSignature struct {
	Args   []ArgKind
	Return string
}

// SimpleCoreSignatures lists primitives whose type and ownership shape is declarative.
var SimpleCoreSignatures = map[string]CoreSignature{
	"std::internal::builtin::mem_page_allocator": {Return: "Allocator"},
	"std::internal::builtin::mem_len":            {Args: []ArgKind{ArgBytes}, Return: "i64"},
	"std::internal::builtin::io_write_stdout": {
		Args:   []ArgKind{ArgIo, ArgBytes},
		Return: "std::io::Error!void",
	},
	"std::internal::builtin::io_write_stderr": {
		Args:   []ArgKind{ArgIo, ArgBytes},
		Return: "std::io::Error!void",
	},
	"std::internal::builtin::io_read_stdin": {
		Args:   []ArgKind{ArgIo},
		Return: "std::io::Error![]u8",
	},
	"std::internal::builtin::process_arg_count": {Return: "i64"},
	"std::internal::builtin::process_arg": {
		Args:   []ArgKind{ArgI64},
		Return: "std::process::Error![]u8",
	},
	"std::internal::builtin::process_env": {
		Args:   []ArgKind{ArgBytes},
		Return: "std::process::Error![]u8",
	},
	"std::internal::builtin::process_env_or_empty": {
		Args:   []ArgKind{ArgBytes},
		Return: "[]u8",
	},
	"std::internal::builtin::process_monotonic_millis": {Return: "i64"},
	"std::internal::builtin::process_spawn_wait8": {
		Args: []ArgKind{
			ArgI64,
			ArgBytes,
			ArgBytes,
			ArgBytes,
			ArgBytes,
			ArgBytes,
			ArgBytes,
			ArgBytes,
			ArgBytes,
		},
		Return: "std::process::Error!i64",
	},
	"std::internal::builtin::test_fail": {Args: []ArgKind{ArgBytes}, Return: "void"},
}

// TypedCoreBuiltins lists typed primitives that require explicit type application.
var TypedCoreBuiltins = map[string]string{
	"std::internal::builtin::test_fail_equal": "std::testing::expect_equal",
}

// reserved lists every `std::internal::builtin::` primitive and the public std API that
// wraps it.
//
// The namespace is what reserves a primitive, not each family remembering to
// check. A primitive missing from here is reachable from user code, which is
// how `std::internal::builtin::io_blocking` handed out an Io capability to any program
// that asked; the guard now reads this table, and BuiltinNames drives the test
// that keeps it complete.
var reserved = map[string]string{
	"std::internal::builtin::array":                    "std::array",
	"std::internal::builtin::array_append":             "std::array",
	"std::internal::builtin::array_at":                 "std::array",
	"std::internal::builtin::array_as_bytes":           "std::array",
	"std::internal::builtin::array_at_mut":             "std::array",
	"std::internal::builtin::array_capacity":           "std::array",
	"std::internal::builtin::array_clear":              "std::array",
	"std::internal::builtin::array_deinit":             "std::array",
	"std::internal::builtin::array_get":                "std::array",
	"std::internal::builtin::array_get_or_panic":       "std::array",
	"std::internal::builtin::array_len":                "std::array",
	"std::internal::builtin::array_pop":                "std::array",
	"std::internal::builtin::array_pop_or_panic":       "std::array",
	"std::internal::builtin::array_reserve":            "std::array",
	"std::internal::builtin::array_set":                "std::array",
	"std::internal::builtin::array_truncate":           "std::array",
	"std::internal::builtin::box":                      "std::mem::Box",
	"std::internal::builtin::box_borrow":               "std::mem::Box",
	"std::internal::builtin::box_borrow_mut":           "std::mem::Box",
	"std::internal::builtin::box_deinit":               "std::mem::Box",
	"std::internal::builtin::fs_create_dir":            "std::fs",
	"std::internal::builtin::fs_exists":                "std::fs",
	"std::internal::builtin::fs_metadata":              "std::fs",
	"std::internal::builtin::fs_read_dir":              "std::fs",
	"std::internal::builtin::fs_read_file":             "std::fs",
	"std::internal::builtin::fs_remove_dir":            "std::fs",
	"std::internal::builtin::fs_remove_file":           "std::fs",
	"std::internal::builtin::fs_rename":                "std::fs",
	"std::internal::builtin::fs_write_file":            "std::fs",
	"std::internal::builtin::io_blocking":              "std::io",
	"std::internal::builtin::io_evented":               "std::io",
	"std::internal::builtin::io_failing":               "std::testing",
	"std::internal::builtin::io_read_stdin":            "std::io",
	"std::internal::builtin::io_write_stderr":          "std::io",
	"std::internal::builtin::io_write_stdout":          "std::io",
	"std::internal::builtin::map":                      "std::map",
	"std::internal::builtin::map_contains":             "std::map",
	"std::internal::builtin::map_deinit":               "std::map",
	"std::internal::builtin::map_get":                  "std::map",
	"std::internal::builtin::map_insert":               "std::map",
	"std::internal::builtin::map_len":                  "std::map",
	"std::internal::builtin::mem_len":                  "std::mem",
	"std::internal::builtin::mem_page_allocator":       "std::mem",
	"std::internal::builtin::process_arg":              "std::process",
	"std::internal::builtin::process_arg_count":        "std::process",
	"std::internal::builtin::process_env":              "std::process",
	"std::internal::builtin::process_env_or_empty":     "std::process",
	"std::internal::builtin::process_monotonic_millis": "std::process",
	"std::internal::builtin::process_spawn_wait8":      "std::process",
	"std::internal::builtin::test_fail":                "std::testing",
	"std::internal::builtin::test_fail_equal":          "std::testing",
}

// ReservedBuiltin reports whether name is a `std::internal::builtin::` primitive, and the
// public std API a caller should reach for instead.
func ReservedBuiltin(name string) (string, bool) {
	replacement, ok := reserved[name]
	return replacement, ok
}

// BuiltinNames returns every reserved primitive, so a test can enumerate them.
func BuiltinNames() []string {
	names := make([]string, 0, len(reserved))
	for name := range reserved {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var removedExact = map[string]string{
	"std::internal::builtin::mem_byte_at":       "std::mem::byte_at",
	"std::internal::builtin::mem_slice":         "std::mem::slice",
	"std::internal::builtin::process_exit_code": "std::process::exit_code",
	"std::internal::builtin::mem_equal_bytes":   "std::mem::equal_bytes",
	"std::internal::builtin::mem_starts_with":   "std::mem::starts_with",
	"std::internal::builtin::mem_trim_ascii":    "std::mem::trim_ascii",
	"std::internal::builtin::path_basename":     "std::path::basename",
	"std::internal::builtin::path_dirname":      "std::path::dirname",
	"std::internal::builtin::path_extension":    "std::path::extension",
	"std::internal::builtin::path_clean":        "std::path::clean",
	"std::internal::builtin::path_join":         "std::path::join",
}

// RemovedBuiltinReplacement reports removed primitives and their public replacement.
func RemovedBuiltinReplacement(name string) (string, bool) {
	if replacement, ok := removedExact[name]; ok {
		return replacement, true
	}
	if strings.HasPrefix(name, "std::internal::builtin::string_") {
		return "std::string", true
	}
	if strings.HasPrefix(name, "std::internal::builtin::testing_") {
		return "std::testing", true
	}
	return "", false
}
