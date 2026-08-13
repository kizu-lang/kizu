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

// CoreSignature describes simple std::builtin calls with no ownership transfer.
type CoreSignature struct {
	Args   []ArgKind
	Return string
}

// SimpleCoreSignatures lists primitives whose type and ownership shape is declarative.
var SimpleCoreSignatures = map[string]CoreSignature{
	"std.builtin.mem_page_allocator": {Return: "Allocator"},
	"std.builtin.mem_len":            {Args: []ArgKind{ArgBytes}, Return: "i64"},
	"std.builtin.io_write_stdout":    {Args: []ArgKind{ArgIo, ArgBytes}, Return: "!void"},
	"std.builtin.io_write_stderr":    {Args: []ArgKind{ArgIo, ArgBytes}, Return: "!void"},
	"std.builtin.io_read_stdin":      {Args: []ArgKind{ArgIo}, Return: "![]u8"},
	"std.builtin.process_arg_count":  {Return: "i64"},
	"std.builtin.process_arg":        {Args: []ArgKind{ArgI64}, Return: "![]u8"},
	"std.builtin.process_env":        {Args: []ArgKind{ArgBytes}, Return: "![]u8"},
	"std.builtin.process_env_or_empty": {
		Args:   []ArgKind{ArgBytes},
		Return: "[]u8",
	},
	"std.builtin.process_monotonic_millis": {Return: "i64"},
	"std.builtin.process_spawn_wait8": {
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
		Return: "!i64",
	},
	"std.builtin.test_fail": {Args: []ArgKind{ArgBytes}, Return: "void"},
}

// TypedCoreBuiltins lists typed primitives that require explicit type application.
var TypedCoreBuiltins = map[string]string{
	"std.builtin.test_fail_equal": "std::testing::expect_equal",
}

// reserved lists every `std::builtin::` primitive and the public std API that
// wraps it.
//
// The namespace is what reserves a primitive, not each family remembering to
// check. A primitive missing from here is reachable from user code, which is
// how `std::builtin::io_blocking` handed out an Io capability to any program
// that asked; the guard now reads this table, and BuiltinNames drives the test
// that keeps it complete.
var reserved = map[string]string{
	"std.builtin.array":                    "std::array",
	"std.builtin.array_append":             "std::array",
	"std.builtin.array_at":                 "std::array",
	"std.builtin.array_at_mut":             "std::array",
	"std.builtin.array_capacity":           "std::array",
	"std.builtin.array_deinit":             "std::array",
	"std.builtin.array_get":                "std::array",
	"std.builtin.array_get_or_panic":       "std::array",
	"std.builtin.array_len":                "std::array",
	"std.builtin.array_pop":                "std::array",
	"std.builtin.array_pop_or_panic":       "std::array",
	"std.builtin.array_reserve":            "std::array",
	"std.builtin.array_set":                "std::array",
	"std.builtin.atomic":                   "std::atomic",
	"std.builtin.atomic_load":              "std::atomic",
	"std.builtin.atomic_store":             "std::atomic",
	"std.builtin.box":                      "std::mem::Box",
	"std.builtin.box_borrow":               "std::mem::Box",
	"std.builtin.box_borrow_mut":           "std::mem::Box",
	"std.builtin.box_deinit":               "std::mem::Box",
	"std.builtin.channel":                  "std::channel",
	"std.builtin.channel_recv":             "std::channel",
	"std.builtin.channel_send":             "std::channel",
	"std.builtin.fs_create_dir":            "std::fs",
	"std.builtin.fs_exists":                "std::fs",
	"std.builtin.fs_metadata":              "std::fs",
	"std.builtin.fs_read_dir":              "std::fs",
	"std.builtin.fs_read_file":             "std::fs",
	"std.builtin.fs_remove_dir":            "std::fs",
	"std.builtin.fs_remove_file":           "std::fs",
	"std.builtin.fs_rename":                "std::fs",
	"std.builtin.fs_write_file":            "std::fs",
	"std.builtin.io_blocking":              "std::io",
	"std.builtin.io_evented":               "std::io",
	"std.builtin.io_failing":               "std::io",
	"std.builtin.io_read_stdin":            "std::io",
	"std.builtin.io_threaded":              "std::io",
	"std.builtin.io_write_stderr":          "std::io",
	"std.builtin.io_write_stdout":          "std::io",
	"std.builtin.map":                      "std::map",
	"std.builtin.map_contains":             "std::map",
	"std.builtin.map_deinit":               "std::map",
	"std.builtin.map_get":                  "std::map",
	"std.builtin.map_insert":               "std::map",
	"std.builtin.map_len":                  "std::map",
	"std.builtin.mem_len":                  "std::mem",
	"std.builtin.mem_page_allocator":       "std::mem",
	"std.builtin.mutex":                    "std::sync",
	"std.builtin.mutex_get":                "std::sync",
	"std.builtin.process_arg":              "std::process",
	"std.builtin.process_arg_count":        "std::process",
	"std.builtin.process_env":              "std::process",
	"std.builtin.process_env_or_empty":     "std::process",
	"std.builtin.process_monotonic_millis": "std::process",
	"std.builtin.process_spawn_wait":       "std::process",
	"std.builtin.process_spawn_wait8":      "std::process",
	"std.builtin.task_group":               "std::task",
	"std.builtin.task_local_buffer":        "std::task",
	"std.builtin.task_parallel_for":        "std::task",
	"std.builtin.task_parallel_map":        "std::task",
	"std.builtin.task_partition_mut":       "std::task",
	"std.builtin.task_queue":               "std::task",
	"std.builtin.test_fail":                "std::testing",
	"std.builtin.test_fail_equal":          "std::testing",
	"std.builtin.thread_scoped":            "std::thread",
}

// ReservedBuiltin reports whether name is a `std::builtin::` primitive, and the
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
	"std.builtin.mem_byte_at":       "std::mem::byte_at",
	"std.builtin.mem_slice":         "std::mem::slice",
	"std.builtin.process_exit_code": "std::process::exit_code",
	"std.builtin.mem_equal_bytes":   "std::mem::equal_bytes",
	"std.builtin.mem_starts_with":   "std::mem::starts_with",
	"std.builtin.mem_trim_ascii":    "std::mem::trim_ascii",
	"std.builtin.path_basename":     "std::path::basename",
	"std.builtin.path_dirname":      "std::path::dirname",
	"std.builtin.path_extension":    "std::path::extension",
	"std.builtin.path_clean":        "std::path::clean",
	"std.builtin.path_join":         "std::path::join",
}

// RemovedBuiltinReplacement reports removed primitives and their public replacement.
func RemovedBuiltinReplacement(name string) (string, bool) {
	if replacement, ok := removedExact[name]; ok {
		return replacement, true
	}
	if strings.HasPrefix(name, "std.builtin.string_") {
		return "std::string", true
	}
	if strings.HasPrefix(name, "std.builtin.testing_") {
		return "std::testing", true
	}
	return "", false
}
