package stdprim

import "sort"

// ArgKind identifies a primitive argument shape shared by static checkers.
type ArgKind string

const (
	// ArgBytes is a read-only byte slice argument.
	ArgBytes ArgKind = "[]u8"
	// ArgI64 is a signed 64-bit integer argument.
	ArgI64 ArgKind = "i64"
	// ArgIo is an explicit I/O capability argument.
	ArgIo ArgKind = "Io"
	// ArgStringOut is a &var std::string::String destination the callee
	// appends into; the caller's buffer, mutated in place, never moved.
	ArgStringOut ArgKind = "&var std::string::String"
)

// CoreSignature describes simple std::internal::builtin calls with no ownership transfer.
type CoreSignature struct {
	Args   []ArgKind
	Return string
}

// SimpleCoreSignatures lists primitives whose type and ownership shape is declarative.
var SimpleCoreSignatures = map[string]CoreSignature{
	"std::internal::builtin::mem_page_allocator": {Return: "Allocator"},
	"std::internal::builtin::mem_fixed_buffer":   {Args: []ArgKind{ArgBytes}, Return: "Allocator"},
	"std::internal::builtin::mem_len":            {Args: []ArgKind{ArgBytes}, Return: "i64"},
	"std::internal::builtin::io_write_stdout": {
		Args:   []ArgKind{ArgIo, ArgBytes},
		Return: "std::io::Error!void",
	},
	"std::internal::builtin::io_write_stderr": {
		Args:   []ArgKind{ArgIo, ArgBytes},
		Return: "std::io::Error!void",
	},
	"std::internal::builtin::fs_read_file_into": {
		Args:   []ArgKind{ArgIo, ArgBytes, ArgStringOut, ArgI64},
		Return: "std::fs::Error!void",
	},
	"std::internal::builtin::io_read_stdin_into": {
		Args:   []ArgKind{ArgIo, ArgStringOut, ArgI64},
		Return: "std::io::Error!void",
	},
	"std::internal::builtin::fs_real_path_into": {
		Args:   []ArgKind{ArgIo, ArgBytes, ArgStringOut},
		Return: "std::fs::Error!void",
	},
	"std::internal::builtin::process_executable_path_into": {
		Args:   []ArgKind{ArgStringOut},
		Return: "std::process::Error!void",
	},
	"std::internal::builtin::process_arg_count": {Return: "i64"},
	"std::internal::builtin::process_arg": {
		Args:   []ArgKind{ArgI64},
		Return: "std::process::Error![]u8",
	},
	"std::internal::builtin::process_env": {
		Args:   []ArgKind{ArgBytes},
		Return: "?[]u8",
	},
	"std::internal::builtin::process_monotonic_millis": {Return: "i64"},
	"std::internal::builtin::process_unix_millis":      {Return: "i64"},
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
	"std::internal::builtin::panic":     {Args: []ArgKind{ArgBytes}, Return: "void"},
}

// TypedCoreBuiltins lists typed primitives that require explicit type application.
var TypedCoreBuiltins = map[string]string{
	"std::internal::builtin::test_fail_equal": "std::testing::expect_equal",
}

// primitives is every `std::internal::builtin::` name the Go implementation
// provides. It is the list of what exists, not a table of what to use instead:
// a program outside std cannot name the module at all, so nothing here has to
// suggest a replacement.
//
// A name missing from here reads as a misspelling, which is the one thing std
// source needs told -- a primitive it names that does not exist would lower to
// nothing.
var primitives = map[string]bool{
	"std::internal::builtin::arena":                        true,
	"std::internal::builtin::arena_deinit":                 true,
	"std::internal::builtin::arena_len":                    true,
	"std::internal::builtin::arena_pop_or_panic":           true,
	"std::internal::builtin::array":                        true,
	"std::internal::builtin::array_append":                 true,
	"std::internal::builtin::array_append_bytes":           true,
	"std::internal::builtin::array_at":                     true,
	"std::internal::builtin::array_as_bytes":               true,
	"std::internal::builtin::array_as_mut_bytes":           true,
	"std::internal::builtin::array_at_mut":                 true,
	"std::internal::builtin::array_capacity":               true,
	"std::internal::builtin::array_clear":                  true,
	"std::internal::builtin::array_deinit":                 true,
	"std::internal::builtin::array_get":                    true,
	"std::internal::builtin::array_get_or_panic":           true,
	"std::internal::builtin::array_len":                    true,
	"std::internal::builtin::array_pop":                    true,
	"std::internal::builtin::array_pop_or_panic":           true,
	"std::internal::builtin::array_reserve":                true,
	"std::internal::builtin::array_set":                    true,
	"std::internal::builtin::array_swap":                   true,
	"std::internal::builtin::array_truncate":               true,
	"std::internal::builtin::box":                          true,
	"std::internal::builtin::box_borrow":                   true,
	"std::internal::builtin::box_borrow_mut":               true,
	"std::internal::builtin::box_deinit":                   true,
	"std::internal::builtin::box_take":                     true,
	"std::internal::builtin::fs_create_dir":                true,
	"std::internal::builtin::fs_exists":                    true,
	"std::internal::builtin::fs_metadata":                  true,
	"std::internal::builtin::fs_read_dir":                  true,
	"std::internal::builtin::fs_read_file_into":            true,
	"std::internal::builtin::fs_real_path_into":            true,
	"std::internal::builtin::fs_remove_dir":                true,
	"std::internal::builtin::fs_remove_file":               true,
	"std::internal::builtin::fs_rename":                    true,
	"std::internal::builtin::fs_write_file":                true,
	"std::internal::builtin::io_blocking":                  true,
	"std::internal::builtin::io_evented":                   true,
	"std::internal::builtin::io_failing":                   true,
	"std::internal::builtin::io_read_stdin_into":           true,
	"std::internal::builtin::io_write_stderr":              true,
	"std::internal::builtin::io_write_stdout":              true,
	"std::internal::builtin::map":                          true,
	"std::internal::builtin::map_at":                       true,
	"std::internal::builtin::map_at_mut":                   true,
	"std::internal::builtin::map_contains":                 true,
	"std::internal::builtin::map_deinit":                   true,
	"std::internal::builtin::map_get":                      true,
	"std::internal::builtin::map_insert":                   true,
	"std::internal::builtin::map_key_at":                   true,
	"std::internal::builtin::map_len":                      true,
	"std::internal::builtin::map_take_value_at":            true,
	"std::internal::builtin::mem_allocator_from":           true,
	"std::internal::builtin::mem_fixed_buffer":             true,
	"std::internal::builtin::mem_len":                      true,
	"std::internal::builtin::mem_page_allocator":           true,
	"std::internal::builtin::panic":                        true,
	"std::internal::builtin::process_arg":                  true,
	"std::internal::builtin::process_arg_count":            true,
	"std::internal::builtin::process_executable_path_into": true,
	"std::internal::builtin::process_env":                  true,
	"std::internal::builtin::process_monotonic_millis":     true,
	"std::internal::builtin::process_spawn_wait8":          true,
	"std::internal::builtin::process_unix_millis":          true,
	"std::internal::builtin::test_fail":                    true,
	"std::internal::builtin::test_fail_equal":              true,
}

// Primitive reports whether name is a `std::internal::builtin::` primitive.
func Primitive(name string) bool {
	return primitives[name]
}

// BuiltinNames returns every primitive, so a test can enumerate them.
func BuiltinNames() []string {
	names := make([]string, 0, len(primitives))
	for name := range primitives {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
