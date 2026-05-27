package stdprim

import "strings"

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
