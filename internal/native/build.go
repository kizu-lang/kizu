package native

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/buildcache"
)

// Options describes one native link request.
type Options struct {
	LLVMIR string
	// ErrorSets maps each declared error set to the number its members lower to.
	// The runtime returns those numbers, and generating the constants from the
	// declarations is what keeps the two from drifting: the order a set is
	// written in is the only thing that decides them.
	ErrorSets map[string]map[string]int
	Output    string
	Triple    string
	CPU       string
	ABI       string
	LibC      string
	Runtime   string
	Emit      string
	Linker    string
	Opt       bool
}

// Build writes transient inputs and links them into a native executable.
func Build(options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "kizu-native-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	irPath := filepath.Join(tmp, "main.ll")
	if err := os.WriteFile(irPath, []byte(options.LLVMIR), 0o644); err != nil {
		return err
	}
	runtimePath, err := runtimeObject(options)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.Output), 0o755); err != nil {
		return err
	}
	command, err := runClang(irPath, runtimePath, options)
	if err != nil {
		return err
	}
	return writeMetadata(options, command)
}

// validateOptions rejects native build modes that do not have a concrete backend yet.
func validateOptions(options Options) error {
	if options.Output == "" {
		return fmt.Errorf("native error: output path is required")
	}
	if options.LibC != "on" {
		return fmt.Errorf("native error: --libc %s is not implemented yet", options.LibC)
	}
	if options.Runtime != "hosted" {
		return fmt.Errorf("native error: --runtime %s is not implemented yet", options.Runtime)
	}
	if options.Emit != "exe" {
		return fmt.Errorf("native error: --emit %s is not implemented yet", options.Emit)
	}
	if options.CPU != "" {
		return fmt.Errorf("native error: --cpu is not implemented yet")
	}
	if options.ABI != "" {
		return fmt.Errorf("native error: --abi is not implemented yet")
	}
	if options.Linker != "clang" {
		return fmt.Errorf("native error: --linker %s is not implemented yet", options.Linker)
	}
	return nil
}

// runtimeObject returns the compiled runtime to link, compiling it only when
// nothing has it yet. The runtime is part of the compiler, not part of the
// program: its source is a constant of this binary and the numbers it names
// failures with are read from std, so compiling it once per program is the same
// work reaching the same answer every time.
func runtimeObject(options Options) (string, error) {
	if err := requireRuntimeErrorSets(options.ErrorSets); err != nil {
		return "", err
	}
	source := errorSetConstants(options.ErrorSets) + runtimeSource
	cache, err := buildcache.New()
	if err != nil {
		return "", err
	}
	return cache.GetOrBuildArtifact(
		"native-runtime.c",
		runtimeCacheTarget(options),
		[]byte(source),
		func(output string) error { return compileRuntime(source, output, options) },
	)
}

// runtimeCacheTarget spells what changes the object but is not in its source:
// the toolchain that builds it, the machine it is built for, and what it is
// asked to produce.
func runtimeCacheTarget(options Options) string {
	key := []string{"native-runtime", options.Linker, runtime.GOOS + "-" + runtime.GOARCH}
	return strings.Join(append(key, clangFlags(options)...), "/")
}

// compileRuntime compiles the runtime source into one object file.
func compileRuntime(source string, output string, options Options) error {
	dir, err := os.MkdirTemp("", "kizu-runtime-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	sourcePath := filepath.Join(dir, "runtime.c")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		return err
	}
	args := append(clangFlags(options), "-c", sourcePath, "-o", output)
	out, err := exec.Command(options.Linker, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("native error: %s failed: %w\n%s", options.Linker, err, out)
	}
	return nil
}

// runtimeErrorSets names the sets the runtime reports its failures with. It
// refers to all of them whatever a program uses, so a build that does not carry
// them would fail in the C compiler with an undeclared name.
var runtimeErrorSets = []string{"std::fs::Error", "std::io::Error", "std::process::Error"}

// requireRuntimeErrorSets rejects a build that cannot name what the runtime
// returns.
func requireRuntimeErrorSets(sets map[string]map[string]int) error {
	for _, name := range runtimeErrorSets {
		if len(sets[name]) == 0 {
			return fmt.Errorf("native error: error set `%s` is missing", name)
		}
	}
	return nil
}

// errorSetConstants writes the number each error set member lowers to, so the
// runtime names a failure the same way the program that reads it does.
func errorSetConstants(sets map[string]map[string]int) string {
	if len(sets) == 0 {
		return ""
	}
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString("/* Generated from the error set declarations. */\n")
	for _, name := range names {
		members := sets[name]
		spellings := make([]string, 0, len(members))
		for member := range members {
			spellings = append(spellings, member)
		}
		sort.Strings(spellings)
		for _, member := range spellings {
			fmt.Fprintf(&out, "#define %s %d\n", errorConstantName(name, member), members[member])
		}
	}
	out.WriteString("\n")
	return out.String()
}

// errorConstantName spells one member as a C identifier.
func errorConstantName(set string, member string) string {
	replacer := strings.NewReplacer("::", "_", ".", "_")
	return "KIZU_ERR_" + strings.ToUpper(replacer.Replace(set)+"_"+camelToSnake(member))
}

// camelToSnake separates the words in a member name for the C spelling.
func camelToSnake(name string) string {
	var out strings.Builder
	for index, r := range name {
		if index > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// runClang invokes the configured C/LLVM toolchain with explicit inputs.
func runClang(irPath string, runtimePath string, options Options) ([]string, error) {
	args := append(clangFlags(options), irPath, runtimePath, "-o", options.Output)
	cmd := exec.Command(options.Linker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("native error: %s failed: %w\n%s", options.Linker, err, out)
	}
	return append([]string{options.Linker}, args...), nil
}

// clangFlags spells what the toolchain is asked to produce. The runtime object
// is keyed by these, so the key cannot name one build and the compile another.
func clangFlags(options Options) []string {
	flags := []string{}
	if options.Triple != "" {
		flags = append(flags, "-target", options.Triple)
	}
	return append(flags, clangOptimizationFlag(options.Opt))
}

// clangOptimizationFlag selects the native toolchain optimization level.
func clangOptimizationFlag(opt bool) string {
	if opt {
		return "-O2"
	}
	return "-O0"
}

// Metadata records explicit native build inputs next to the output artifact.
type Metadata struct {
	Target  string   `json:"target"`
	Triple  string   `json:"triple"`
	CPU     string   `json:"cpu"`
	ABI     string   `json:"abi"`
	LibC    string   `json:"libc"`
	Runtime string   `json:"runtime"`
	Emit    string   `json:"emit"`
	Linker  string   `json:"linker"`
	OptMode string   `json:"optimization_mode"`
	Output  string   `json:"output"`
	Command []string `json:"command"`
}

// writeMetadata writes the explicit build configuration used for this artifact.
func writeMetadata(options Options, command []string) error {
	metadata := Metadata{
		Target: "native", Triple: options.Triple, CPU: options.CPU, ABI: options.ABI,
		LibC: options.LibC, Runtime: options.Runtime, Emit: options.Emit,
		Linker: options.Linker, OptMode: optimizationModeName(options.Opt),
		Output: options.Output, Command: command,
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(options.Output+".kizu-build.json", append(data, '\n'), 0o644)
}

// optimizationModeName records the native optimization mode in artifact metadata.
func optimizationModeName(opt bool) string {
	if opt {
		return "opt"
	}
	return "debug"
}

const runtimeSource = `
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dirent.h>
#include <errno.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

typedef struct {
    unsigned char *data;
    int64_t len;
    int64_t cap;
    int64_t elem_size;
} KizuArray;

typedef struct {
    unsigned char *ptr;
    int64_t len;
} KizuSliceU8;

typedef struct {
    int64_t size;
    _Bool is_dir;
} KizuFsMetadata;

typedef struct {
    KizuSliceU8 name;
    KizuSliceU8 path;
    _Bool is_dir;
} KizuFsDirEntry;

typedef struct {
    _Bool ok;
    int64_t error;
} KizuErrorVoid;

typedef struct {
    _Bool ok;
    _Bool value;
    int64_t error;
} KizuErrorBool;

typedef struct {
    _Bool ok;
    int64_t value;
    int64_t error;
} KizuErrorI64;

typedef struct {
    _Bool ok;
    KizuSliceU8 value;
    int64_t error;
} KizuErrorSliceU8;

typedef struct {
    _Bool ok;
    KizuFsMetadata value;
    int64_t error;
} KizuErrorFsMetadata;

typedef struct {
    _Bool ok;
    void *value;
    int64_t error;
} KizuErrorPtr;

/* A map is an array of entries in the order they were first inserted, plus an
   open-addressed table of indices into it. The entries array is what iteration
   and insertion order come from; the index is what makes lookup O(1). Splitting
   them this way is why insertion order costs nothing here. */
typedef struct {
    unsigned char *key;
    int64_t key_len;
    unsigned char *value;
    uint64_t hash;
} KizuMapEntry;

typedef struct {
    KizuMapEntry *entries;
    int64_t len;
    int64_t cap;
    int64_t value_size;
    /* index[slot] is an entry number, or -1 for a free slot. index_cap is a
       power of two and is kept above the entry count, so a probe always meets
       a free slot and terminates. */
    int64_t *index;
    int64_t index_cap;
} KizuMap;

typedef struct {
    unsigned char *data;
    int64_t len;
    int64_t cap;
    int64_t elem_size;
} KizuArena;

void *kizu_array_new(int64_t elem_size);
_Bool kizu_array_append(void *handle, const void *elem);

static int kizu_runtime_argc = 0;
static char **kizu_runtime_argv = NULL;

void kizu_runtime_init_args(int32_t argc, char **argv) {
    kizu_runtime_argc = argc;
    kizu_runtime_argv = argv;
}

static KizuSliceU8 kizu_slice_from_cstr(const char *value) {
    KizuSliceU8 out;
    out.ptr = (unsigned char *)value;
    out.len = value ? (int64_t)strlen(value) : 0;
    return out;
}

/* An error is a member of a declared set, and the numbers come from the
 * declarations rather than from a table written here, so the two cannot drift.
 * What the OS reports is mapped to the member that names the same thing. */
static int64_t kizu_errno_failure(int code) {
    switch (code) {
    case ENOENT:
        return KIZU_ERR_STD_FS_ERROR_NOT_FOUND;
    case EACCES:
    case EPERM:
        return KIZU_ERR_STD_FS_ERROR_PERMISSION_DENIED;
    case EISDIR:
        return KIZU_ERR_STD_FS_ERROR_IS_DIRECTORY;
    case ENOTDIR:
        return KIZU_ERR_STD_FS_ERROR_NOT_DIRECTORY;
    case EEXIST:
        return KIZU_ERR_STD_FS_ERROR_ALREADY_EXISTS;
    case ENOTEMPTY:
        return KIZU_ERR_STD_FS_ERROR_DIRECTORY_NOT_EMPTY;
    case ENOSPC:
        return KIZU_ERR_STD_FS_ERROR_NO_SPACE_LEFT;
    case EMFILE:
    case ENFILE:
        return KIZU_ERR_STD_FS_ERROR_TOO_MANY_OPEN_FILES;
    default:
        return KIZU_ERR_STD_FS_ERROR_OPERATION_FAILED;
    }
}

static KizuErrorVoid kizu_ok_void(void) {
    KizuErrorVoid out;
    out.ok = 1;
    out.error = 0;
    return out;
}

static KizuErrorVoid kizu_err_void(int64_t failure) {
    KizuErrorVoid out;
    out.ok = 0;
    out.error = failure;
    return out;
}

static KizuErrorBool kizu_ok_bool(_Bool value) {
    KizuErrorBool out;
    out.ok = 1;
    out.value = value;
    out.error = 0;
    return out;
}

static KizuErrorBool kizu_err_bool(int64_t failure) {
    KizuErrorBool out;
    out.ok = 0;
    out.value = 0;
    out.error = failure;
    return out;
}

static KizuErrorI64 kizu_ok_i64(int64_t value) {
    KizuErrorI64 out;
    out.ok = 1;
    out.value = value;
    out.error = 0;
    return out;
}

static KizuErrorI64 kizu_err_i64(int64_t failure) {
    KizuErrorI64 out;
    out.ok = 0;
    out.value = 1;
    out.error = failure;
    return out;
}

static KizuErrorSliceU8 kizu_ok_slice(KizuSliceU8 value) {
    KizuErrorSliceU8 out;
    out.ok = 1;
    out.value = value;
    out.error = 0;
    return out;
}

static KizuErrorSliceU8 kizu_err_slice(int64_t failure) {
    KizuErrorSliceU8 out;
    out.ok = 0;
    out.value = kizu_slice_from_cstr("");
    out.error = failure;
    return out;
}

static KizuErrorFsMetadata kizu_ok_metadata(KizuFsMetadata value) {
    KizuErrorFsMetadata out;
    out.ok = 1;
    out.value = value;
    out.error = 0;
    return out;
}

static KizuErrorFsMetadata kizu_err_metadata(int64_t failure) {
    KizuErrorFsMetadata out;
    out.ok = 0;
    out.value.size = 0;
    out.value.is_dir = 0;
    out.error = failure;
    return out;
}

static KizuErrorPtr kizu_ok_ptr(void *value) {
    KizuErrorPtr out;
    out.ok = 1;
    out.value = value;
    out.error = 0;
    return out;
}

static KizuErrorPtr kizu_err_ptr(int64_t failure) {
    KizuErrorPtr out;
    out.ok = 0;
    out.value = NULL;
    out.error = failure;
    return out;
}

static char *kizu_slice_to_cstr(KizuSliceU8 value) {
    if (value.len < 0) {
        return NULL;
    }
    char *out = (char *)malloc((size_t)value.len + 1);
    if (!out) {
        return NULL;
    }
    if (value.len > 0 && value.ptr) {
        memcpy(out, value.ptr, (size_t)value.len);
    }
    out[value.len] = '\0';
    return out;
}

static KizuSliceU8 kizu_owned_slice_from_cstr(const char *value) {
    KizuSliceU8 out;
    out.len = value ? (int64_t)strlen(value) : 0;
    out.ptr = NULL;
    if (out.len == 0) {
        return out;
    }
    out.ptr = (unsigned char *)malloc((size_t)out.len);
    if (!out.ptr) {
        out.len = 0;
        return out;
    }
    memcpy(out.ptr, value, (size_t)out.len);
    return out;
}

static KizuSliceU8 kizu_join_path_slice(const char *dir, const char *name) {
    size_t dir_len = strlen(dir);
    size_t name_len = strlen(name);
    size_t needs_slash = dir_len > 0 && dir[dir_len - 1] != '/';
    char *out = (char *)malloc(dir_len + needs_slash + name_len + 1);
    if (!out) {
        return kizu_slice_from_cstr("");
    }
    memcpy(out, dir, dir_len);
    if (needs_slash) {
        out[dir_len] = '/';
    }
    memcpy(out + dir_len + needs_slash, name, name_len);
    out[dir_len + needs_slash + name_len] = '\0';
    KizuSliceU8 slice;
    slice.ptr = (unsigned char *)out;
    slice.len = (int64_t)(dir_len + needs_slash + name_len);
    return slice;
}

void kizu_print_string(const unsigned char *s, int64_t len) {
    fwrite(s, 1, (size_t)len, stdout);
    fputc('\n', stdout);
}

void kizu_main_error_message(const unsigned char *s, int64_t len) {
    fwrite("runtime error: ", 1, 15, stderr);
    fwrite(s, 1, (size_t)len, stderr);
    fputc('\n', stderr);
}

/* Checked runtime failures. The wording lives here so that a failure reads the
 * same however the program reached it, per ADR-0072:
 *
 *   runtime error: <summary> at <line>:<column>
 *   note: <context>
 *
 * The position is omitted when the reporting instruction has no source span.
 */
static void kizu_panic_at(int64_t line, int64_t column) {
    if (line > 0) {
        fprintf(stderr, " at %lld:%lld", (long long)line, (long long)column);
    }
    fputc('\n', stderr);
}

static void kizu_panic_summary(const char *summary, int64_t line, int64_t column) {
    fputs("runtime error: ", stderr);
    fputs(summary, stderr);
    kizu_panic_at(line, column);
}

void kizu_panic_bounds(int64_t index, int64_t length, int64_t line, int64_t column) {
    kizu_panic_summary("index out of bounds", line, column);
    fprintf(stderr, "note: index is %lld, length is %lld\n",
            (long long)index, (long long)length);
    abort();
}

void kizu_panic_range(int64_t start, int64_t end, int64_t length,
                      int64_t line, int64_t column) {
    kizu_panic_summary("range out of bounds", line, column);
    fprintf(stderr, "note: range is %lld..%lld, length is %lld\n",
            (long long)start, (long long)end, (long long)length);
    abort();
}

void kizu_panic_array_empty(int64_t line, int64_t column) {
    kizu_panic_summary("array pop from empty", line, column);
    abort();
}

void kizu_panic_arena_handle(int64_t line, int64_t column) {
    kizu_panic_summary("invalid arena handle", line, column);
    abort();
}

void kizu_panic_arena_add(int64_t line, int64_t column) {
    kizu_panic_summary("arena add failed", line, column);
    abort();
}

void kizu_panic_test_fail(const unsigned char *s, int64_t len,
                          int64_t line, int64_t column) {
    fputs("runtime error: ", stderr);
    fwrite(s, 1, (size_t)len, stderr);
    kizu_panic_at(line, column);
    abort();
}

void kizu_panic_expect_equal_int(int64_t expected, int64_t actual,
                                 int64_t line, int64_t column) {
    fprintf(stderr, "runtime error: expected %lld, got %lld",
            (long long)expected, (long long)actual);
    kizu_panic_at(line, column);
    abort();
}

void kizu_panic_expect_equal_bool(_Bool expected, _Bool actual,
                                  int64_t line, int64_t column) {
    fprintf(stderr, "runtime error: expected %s, got %s",
            expected ? "true" : "false", actual ? "true" : "false");
    kizu_panic_at(line, column);
    abort();
}

void kizu_panic_expect_equal_bytes(const unsigned char *expected, int64_t expected_len,
                                   const unsigned char *actual, int64_t actual_len,
                                   int64_t line, int64_t column) {
    fputs("runtime error: expected \"", stderr);
    fwrite(expected, 1, (size_t)expected_len, stderr);
    fputs("\", got \"", stderr);
    fwrite(actual, 1, (size_t)actual_len, stderr);
    fputc('"', stderr);
    kizu_panic_at(line, column);
    abort();
}

void kizu_print_int(int64_t v) {
    printf("%lld\n", (long long)v);
}

/* Print an enum by its tag. The table holds one Enum::Tag spelling per tag, in
 * tag order, so the backend indexes rather than branches. */
void kizu_print_enum(const KizuSliceU8 *names, int64_t count, int64_t tag) {
    if (tag < 0 || tag >= count) {
        kizu_panic_bounds(tag, count, 0, 0);
    }
    fwrite(names[tag].ptr, 1, (size_t)names[tag].len, stdout);
    fputc('\n', stdout);
}

void kizu_print_bool(_Bool v) {
    fputs(v ? "true\n" : "false\n", stdout);
}

void *std_builtin_mem_page_allocator(void) {
    return NULL;
}

// An Io capability is a token rather than a handle. std::io::failing() hands
// out the one every operation refuses, so a program can be written against a
// runtime that fails and the failure can be tested.
#define KIZU_IO_WORKING ((void *)1)
#define KIZU_IO_FAILING ((void *)2)

void *std_builtin_io_blocking(void) {
    return KIZU_IO_WORKING;
}

void *std_builtin_io_failing(void) {
    return KIZU_IO_FAILING;
}

static int kizu_io_is_failing(void *io) {
    return io == KIZU_IO_FAILING;
}

static KizuErrorVoid kizu_std_builtin_io_write_stdout_result(void *io, KizuSliceU8 bytes) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_IO_ERROR_IO_FAILING);
    }
    if (bytes.len > 0 && bytes.ptr) {
        fwrite(bytes.ptr, 1, (size_t)bytes.len, stdout);
    }
    return kizu_ok_void();
}

void std_builtin_io_write_stdout(KizuErrorVoid *out, void *io, const KizuSliceU8 *bytes) {
    *out = kizu_std_builtin_io_write_stdout_result(io, *bytes);
}

static KizuErrorVoid kizu_std_builtin_io_write_stderr_result(void *io, KizuSliceU8 bytes) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_IO_ERROR_IO_FAILING);
    }
    if (bytes.len > 0 && bytes.ptr) {
        fwrite(bytes.ptr, 1, (size_t)bytes.len, stderr);
    }
    return kizu_ok_void();
}

void std_builtin_io_write_stderr(KizuErrorVoid *out, void *io, const KizuSliceU8 *bytes) {
    *out = kizu_std_builtin_io_write_stderr_result(io, *bytes);
}

static KizuErrorSliceU8 kizu_std_builtin_io_read_stdin_result(void *io) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_slice(KIZU_ERR_STD_IO_ERROR_IO_FAILING);
    }
    int64_t len = 0;
    int64_t cap = 4096;
    unsigned char *data = (unsigned char *)malloc((size_t)cap);
    if (!data) {
        return kizu_err_slice(KIZU_ERR_STD_IO_ERROR_OUT_OF_MEMORY);
    }
    for (;;) {
        if (len == cap) {
            cap *= 2;
            unsigned char *next = (unsigned char *)realloc(data, (size_t)cap);
            if (!next) {
                free(data);
                return kizu_err_slice(KIZU_ERR_STD_IO_ERROR_OUT_OF_MEMORY);
            }
            data = next;
        }
        size_t read_count = fread(data + len, 1, (size_t)(cap - len), stdin);
        len += (int64_t)read_count;
        if (read_count == 0) {
            if (ferror(stdin)) {
                free(data);
                return kizu_err_slice(KIZU_ERR_STD_IO_ERROR_READ_FAILED);
            }
            break;
        }
    }
    KizuSliceU8 out;
    out.ptr = data;
    out.len = len;
    return kizu_ok_slice(out);
}

void std_builtin_io_read_stdin(KizuErrorSliceU8 *out, void *io) {
    *out = kizu_std_builtin_io_read_stdin_result(io);
}

int64_t std_builtin_process_arg_count(void) {
    if (kizu_runtime_argc <= 0) {
        return 0;
    }
    return (int64_t)kizu_runtime_argc - 1;
}

static KizuErrorSliceU8 kizu_std_builtin_process_arg_result(int64_t index) {
    if (index < 0 || index >= std_builtin_process_arg_count()) {
        return kizu_err_slice(KIZU_ERR_STD_PROCESS_ERROR_ARG_INDEX_OUT_OF_BOUNDS);
    }
    return kizu_ok_slice(kizu_slice_from_cstr(kizu_runtime_argv[index + 1]));
}

void std_builtin_process_arg(KizuErrorSliceU8 *out, int64_t index) {
    *out = kizu_std_builtin_process_arg_result(index);
}

static KizuErrorSliceU8 kizu_std_builtin_process_env_result(KizuSliceU8 name) {
    char *key = kizu_slice_to_cstr(name);
    if (!key) {
        return kizu_err_slice(KIZU_ERR_STD_PROCESS_ERROR_INVALID_ENV_NAME);
    }
    char *value = getenv(key);
    free(key);
    if (!value) {
        return kizu_err_slice(KIZU_ERR_STD_PROCESS_ERROR_ENV_NOT_FOUND);
    }
    return kizu_ok_slice(kizu_slice_from_cstr(value));
}

void std_builtin_process_env(KizuErrorSliceU8 *out, const KizuSliceU8 *name) {
    *out = kizu_std_builtin_process_env_result(*name);
}

KizuSliceU8 std_builtin_process_env_or_empty(KizuSliceU8 name) {
    char *key = kizu_slice_to_cstr(name);
    if (!key) {
        return kizu_slice_from_cstr("");
    }
    char *value = getenv(key);
    free(key);
    if (!value) {
        return kizu_slice_from_cstr("");
    }
    return kizu_slice_from_cstr(value);
}

int64_t std_builtin_process_monotonic_millis(void) {
    struct timespec ts;
    if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) {
        return 0;
    }
    return ((int64_t)ts.tv_sec * 1000) + ((int64_t)ts.tv_nsec / 1000000);
}

static int kizu_run_child_process(char *const argv[]) {
    pid_t pid = fork();
    if (pid < 0) {
        return 127;
    }
    if (pid == 0) {
        execvp(argv[0], argv);
        _exit(127);
    }
    int status = 0;
    while (waitpid(pid, &status, 0) < 0) {
        if (errno != EINTR) {
            return 127;
        }
    }
    if (WIFEXITED(status)) {
        return WEXITSTATUS(status);
    }
    if (WIFSIGNALED(status)) {
        return 128 + WTERMSIG(status);
    }
    return 127;
}

void std_builtin_process_spawn_wait8(
    KizuErrorI64 *out,
    int64_t argc,
    const KizuSliceU8 *arg0,
    const KizuSliceU8 *arg1,
    const KizuSliceU8 *arg2,
    const KizuSliceU8 *arg3,
    const KizuSliceU8 *arg4,
    const KizuSliceU8 *arg5,
    const KizuSliceU8 *arg6,
    const KizuSliceU8 *arg7
) {
    KizuSliceU8 raw_args[] = {*arg0, *arg1, *arg2, *arg3, *arg4, *arg5, *arg6, *arg7};
    char *owned_args[8] = {0};
    char *argv[9] = {0};
    if (argc < 1 || argc > 8) {
        *out = kizu_err_i64(KIZU_ERR_STD_PROCESS_ERROR_INVALID_ARGUMENT_COUNT);
        return;
    }
    if (raw_args[0].len == 0) {
        *out = kizu_err_i64(KIZU_ERR_STD_PROCESS_ERROR_MISSING_EXECUTABLE);
        return;
    }
    for (int index = 0; index < argc; index++) {
        owned_args[index] = kizu_slice_to_cstr(raw_args[index]);
        if (!owned_args[index]) {
            for (int free_index = 0; free_index < 8; free_index++) {
                free(owned_args[free_index]);
            }
            *out = kizu_err_i64(KIZU_ERR_STD_PROCESS_ERROR_OUT_OF_MEMORY);
            return;
        }
        argv[index] = owned_args[index];
    }
    int code = kizu_run_child_process(argv);
    for (int free_index = 0; free_index < 8; free_index++) {
        free(owned_args[free_index]);
    }
    *out = kizu_ok_i64((int64_t)code);
}

static KizuErrorSliceU8 kizu_std_builtin_fs_read_file_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_slice(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_slice(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    FILE *file = fopen(cpath, "rb");
    int code = errno;
    free(cpath);
    if (!file) {
        return kizu_err_slice(kizu_errno_failure(code));
    }
    if (fseek(file, 0, SEEK_END) != 0) {
        fclose(file);
        return kizu_err_slice(KIZU_ERR_STD_FS_ERROR_READ_FAILED);
    }
    long size = ftell(file);
    if (size < 0 || fseek(file, 0, SEEK_SET) != 0) {
        fclose(file);
        return kizu_err_slice(KIZU_ERR_STD_FS_ERROR_READ_FAILED);
    }
    unsigned char *data = NULL;
    if (size > 0) {
        data = (unsigned char *)malloc((size_t)size);
        if (!data) {
            fclose(file);
            return kizu_err_slice(KIZU_ERR_STD_FS_ERROR_OUT_OF_MEMORY);
        }
        if (fread(data, 1, (size_t)size, file) != (size_t)size) {
            free(data);
            fclose(file);
            return kizu_err_slice(KIZU_ERR_STD_FS_ERROR_READ_FAILED);
        }
    }
    fclose(file);
    KizuSliceU8 out;
    out.ptr = data;
    out.len = (int64_t)size;
    return kizu_ok_slice(out);
}

void std_builtin_fs_read_file(KizuErrorSliceU8 *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_read_file_result(io, *path);
}

static KizuErrorVoid kizu_std_builtin_fs_write_file_result(
    void *io,
    KizuSliceU8 path,
    KizuSliceU8 bytes
) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    FILE *file = fopen(cpath, "wb");
    int code = errno;
    free(cpath);
    if (!file) {
        return kizu_err_void(kizu_errno_failure(code));
    }
    if (bytes.len > 0 && fwrite(bytes.ptr, 1, (size_t)bytes.len, file) != (size_t)bytes.len) {
        fclose(file);
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_WRITE_FAILED);
    }
    fclose(file);
    return kizu_ok_void();
}

void std_builtin_fs_write_file(
    KizuErrorVoid *out,
    void *io,
    const KizuSliceU8 *path,
    const KizuSliceU8 *bytes
) {
    *out = kizu_std_builtin_fs_write_file_result(io, *path, *bytes);
}

static KizuErrorVoid kizu_std_builtin_fs_rename_result(
    void *io,
    KizuSliceU8 from,
    KizuSliceU8 to
) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cfrom = kizu_slice_to_cstr(from);
    char *cto = kizu_slice_to_cstr(to);
    if (!cfrom || !cto) {
        free(cfrom);
        free(cto);
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    int result = rename(cfrom, cto);
    int code = errno;
    free(cfrom);
    free(cto);
    if (result != 0) {
        return kizu_err_void(kizu_errno_failure(code));
    }
    return kizu_ok_void();
}

void std_builtin_fs_rename(
    KizuErrorVoid *out,
    void *io,
    const KizuSliceU8 *from,
    const KizuSliceU8 *to
) {
    *out = kizu_std_builtin_fs_rename_result(io, *from, *to);
}

static KizuErrorBool kizu_std_builtin_fs_exists_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_bool(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_bool(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    _Bool found = access(cpath, F_OK) == 0;
    free(cpath);
    return kizu_ok_bool(found);
}

void std_builtin_fs_exists(KizuErrorBool *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_exists_result(io, *path);
}

static KizuErrorFsMetadata kizu_std_builtin_fs_metadata_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_metadata(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_metadata(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    struct stat st;
    if (stat(cpath, &st) != 0) {
        int code = errno;
        free(cpath);
        return kizu_err_metadata(kizu_errno_failure(code));
    }
    free(cpath);
    KizuFsMetadata out;
    out.size = (int64_t)st.st_size;
    out.is_dir = S_ISDIR(st.st_mode);
    return kizu_ok_metadata(out);
}

void std_builtin_fs_metadata(KizuErrorFsMetadata *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_metadata_result(io, *path);
}

static KizuErrorPtr kizu_std_builtin_fs_read_dir_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_ptr(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_ptr(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    DIR *dir = opendir(cpath);
    if (!dir) {
        int code = errno;
        free(cpath);
        return kizu_err_ptr(kizu_errno_failure(code));
    }
    void *array = kizu_array_new((int64_t)sizeof(KizuFsDirEntry));
    if (!array) {
        closedir(dir);
        free(cpath);
        return kizu_err_ptr(KIZU_ERR_STD_FS_ERROR_OUT_OF_MEMORY);
    }
    struct dirent *entry;
    while ((entry = readdir(dir)) != NULL) {
        if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0) {
            continue;
        }
        KizuFsDirEntry item;
        item.name = kizu_owned_slice_from_cstr(entry->d_name);
        item.path = kizu_join_path_slice(cpath, entry->d_name);
        item.is_dir = 0;
        char *entry_path = kizu_slice_to_cstr(item.path);
        if (entry_path) {
            struct stat st;
            item.is_dir = stat(entry_path, &st) == 0 && S_ISDIR(st.st_mode);
            free(entry_path);
        }
        if (!kizu_array_append(array, &item)) {
            closedir(dir);
            free(cpath);
            return kizu_err_ptr(KIZU_ERR_STD_FS_ERROR_OUT_OF_MEMORY);
        }
    }
    closedir(dir);
    free(cpath);
    return kizu_ok_ptr(array);
}

void std_builtin_fs_read_dir(KizuErrorPtr *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_read_dir_result(io, *path);
}

static KizuErrorVoid kizu_std_builtin_fs_create_dir_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    int result = mkdir(cpath, 0755);
    int code = errno;
    free(cpath);
    if (result != 0 && code != EEXIST) {
        return kizu_err_void(kizu_errno_failure(code));
    }
    return kizu_ok_void();
}

void std_builtin_fs_create_dir(KizuErrorVoid *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_create_dir_result(io, *path);
}

static KizuErrorVoid kizu_std_builtin_fs_remove_dir_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    int result = rmdir(cpath);
    int code = errno;
    free(cpath);
    if (result != 0) {
        return kizu_err_void(kizu_errno_failure(code));
    }
    return kizu_ok_void();
}

void std_builtin_fs_remove_dir(KizuErrorVoid *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_remove_dir_result(io, *path);
}

static KizuErrorVoid kizu_std_builtin_fs_remove_file_result(void *io, KizuSliceU8 path) {
    if (kizu_io_is_failing(io)) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_IO_FAILING);
    }
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void(KIZU_ERR_STD_FS_ERROR_INVALID_PATH);
    }
    int result = unlink(cpath);
    int code = errno;
    free(cpath);
    if (result != 0) {
        return kizu_err_void(kizu_errno_failure(code));
    }
    return kizu_ok_void();
}

void std_builtin_fs_remove_file(KizuErrorVoid *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_remove_file_result(io, *path);
}

_Bool kizu_bytes_equal(
    const unsigned char *left,
    int64_t left_len,
    const unsigned char *right,
    int64_t right_len
) {
    if (left_len != right_len || left_len < 0) {
        return 0;
    }
    if (left_len == 0) {
        return 1;
    }
    if (!left || !right) {
        return 0;
    }
    return memcmp(left, right, (size_t)left_len) == 0;
}

void *kizu_array_new(int64_t elem_size) {
    if (elem_size <= 0) {
        return NULL;
    }
    KizuArray *array = (KizuArray *)calloc(1, sizeof(KizuArray));
    if (!array) {
        return NULL;
    }
    array->elem_size = elem_size;
    return array;
}

void *kizu_arena_new(int64_t elem_size) {
    if (elem_size <= 0) {
        return NULL;
    }
    KizuArena *arena = (KizuArena *)calloc(1, sizeof(KizuArena));
    if (!arena) {
        return NULL;
    }
    arena->elem_size = elem_size;
    return arena;
}

static _Bool kizu_arena_reserve(KizuArena *arena, int64_t needed) {
    if (!arena || needed < 0) {
        return 0;
    }
    if (needed <= arena->cap) {
        return 1;
    }
    int64_t next = arena->cap == 0 ? 16 : arena->cap * 2;
    while (next < needed) {
        next *= 2;
    }
    if (arena->elem_size > 0 && next > INT64_MAX / arena->elem_size) {
        return 0;
    }
    unsigned char *data = (unsigned char *)realloc(arena->data, (size_t)(next * arena->elem_size));
    if (!data) {
        return 0;
    }
    arena->data = data;
    arena->cap = next;
    return 1;
}

int64_t kizu_arena_add(void *handle, const void *elem) {
    KizuArena *arena = (KizuArena *)handle;
    if (!arena || !elem || !kizu_arena_reserve(arena, arena->len + 1)) {
        return -1;
    }
    int64_t index = arena->len;
    memcpy(arena->data + index * arena->elem_size, elem, (size_t)arena->elem_size);
    arena->len += 1;
    return index;
}

void *kizu_arena_get(void *handle, int64_t index) {
    KizuArena *arena = (KizuArena *)handle;
    if (!arena || index < 0 || index >= arena->len) {
        return NULL;
    }
    return arena->data + index * arena->elem_size;
}

void kizu_arena_deinit(void *handle) {
    KizuArena *arena = (KizuArena *)handle;
    if (!arena) {
        return;
    }
    free(arena->data);
    free(arena);
}

/* One cache line's worth of elements, at least one, so a small Array does not
 * start with a sub-cache-line allocation. */
static int64_t kizu_array_init_capacity(int64_t elem_size) {
    const int64_t cache_line = 64;
    if (elem_size <= 0 || elem_size >= cache_line) {
        return 1;
    }
    return cache_line / elem_size;
}

/* Grow by half again plus the initial capacity, following Zig's
 * ArrayList.growCapacity. A factor below the golden ratio lets a later
 * allocation reuse blocks freed by earlier ones; doubling never can. */
static int64_t kizu_array_grow_capacity(int64_t minimum, int64_t elem_size) {
    int64_t half = minimum / 2;
    int64_t init = kizu_array_init_capacity(elem_size);
    if (minimum > INT64_MAX - half - init) {
        return INT64_MAX;
    }
    return minimum + half + init;
}

static _Bool kizu_array_reserve_storage(KizuArray *array, int64_t needed) {
    if (!array || needed < 0) {
        return 0;
    }
    if (needed <= array->cap) {
        return 1;
    }
    int64_t next = kizu_array_grow_capacity(needed, array->elem_size);
    if (array->elem_size > 0 && next > INT64_MAX / array->elem_size) {
        return 0;
    }
    unsigned char *data = (unsigned char *)realloc(array->data, (size_t)(next * array->elem_size));
    if (!data) {
        return 0;
    }
    array->data = data;
    array->cap = next;
    return 1;
}

_Bool kizu_array_append(void *handle, const void *elem) {
    KizuArray *array = (KizuArray *)handle;
    if (!array || !elem || !kizu_array_reserve_storage(array, array->len + 1)) {
        return 0;
    }
    memcpy(array->data + array->len * array->elem_size, elem, (size_t)array->elem_size);
    array->len += 1;
    return 1;
}

int64_t kizu_array_len(void *handle) {
    KizuArray *array = (KizuArray *)handle;
    return array ? array->len : 0;
}

int64_t kizu_array_capacity(void *handle) {
    KizuArray *array = (KizuArray *)handle;
    return array ? array->cap : 0;
}

_Bool kizu_array_reserve(void *handle, int64_t additional) {
    KizuArray *array = (KizuArray *)handle;
    if (!array || additional < 0 || additional > INT64_MAX - array->len) {
        return 0;
    }
    return kizu_array_reserve_storage(array, array->len + additional);
}

void *kizu_array_get(void *handle, int64_t index) {
    KizuArray *array = (KizuArray *)handle;
    if (!array || index < 0 || index >= array->len) {
        return NULL;
    }
    return array->data + index * array->elem_size;
}

void *kizu_array_pop(void *handle) {
    KizuArray *array = (KizuArray *)handle;
    if (!array || array->len <= 0) {
        return NULL;
    }
    array->len -= 1;
    return array->data + array->len * array->elem_size;
}

_Bool kizu_array_set(void *handle, int64_t index, const void *elem) {
    KizuArray *array = (KizuArray *)handle;
    if (!array || !elem || index < 0 || index >= array->len) {
        return 0;
    }
    memcpy(array->data + index * array->elem_size, elem, (size_t)array->elem_size);
    return 1;
}

_Bool kizu_array_truncate(void *handle, int64_t len) {
    KizuArray *array = (KizuArray *)handle;
    if (!array || len < 0 || len > array->len) {
        return 0;
    }
    array->len = len;
    return 1;
}

void kizu_array_clear(void *handle) {
    KizuArray *array = (KizuArray *)handle;
    if (array) {
        array->len = 0;
    }
}

KizuSliceU8 kizu_array_as_bytes(void *handle) {
    KizuArray *array = (KizuArray *)handle;
    KizuSliceU8 result;
    result.ptr = array ? array->data : NULL;
    result.len = array ? array->len : 0;
    return result;
}

void *kizu_map_new(int64_t value_size) {
    if (value_size <= 0) {
        return NULL;
    }
    KizuMap *map = (KizuMap *)calloc(1, sizeof(KizuMap));
    if (!map) {
        return NULL;
    }
    map->value_size = value_size;
    return map;
}

static uint64_t kizu_map_hash(const unsigned char *key, int64_t key_len) {
    uint64_t hash = 1469598103934665603ULL;
    for (int64_t i = 0; i < key_len; i += 1) {
        hash ^= (uint64_t)key[i];
        hash *= 1099511628211ULL;
    }
    return hash;
}

/* kizu_map_slot returns the slot holding key, or the free slot it belongs in.
   The caller tells the two apart by reading index[slot]. */
static int64_t kizu_map_slot(
    KizuMap *map, const unsigned char *key, int64_t key_len, uint64_t hash) {
    uint64_t mask = (uint64_t)map->index_cap - 1;
    uint64_t slot = hash & mask;
    while (map->index[slot] >= 0) {
        KizuMapEntry *entry = &map->entries[map->index[slot]];
        if (entry->hash == hash && entry->key_len == key_len &&
            memcmp(entry->key, key, (size_t)key_len) == 0) {
            return (int64_t)slot;
        }
        slot = (slot + 1) & mask;
    }
    return (int64_t)slot;
}

static int64_t kizu_map_find(KizuMap *map, const unsigned char *key, int64_t key_len) {
    if (!map || !key || key_len < 0 || map->index_cap == 0) {
        return -1;
    }
    return map->index[kizu_map_slot(map, key, key_len, kizu_map_hash(key, key_len))];
}

/* kizu_map_reindex grows the index to hold needed entries below a 3/4 load and
   rebuilds it. Entries carry their hash, so nothing rehashes the key bytes. */
static _Bool kizu_map_reindex(KizuMap *map, int64_t needed) {
    int64_t next = map->index_cap == 0 ? 16 : map->index_cap;
    while (needed * 4 > next * 3) {
        if (next > INT64_MAX / (int64_t)sizeof(int64_t) / 2) {
            return 0;
        }
        next *= 2;
    }
    if (next == map->index_cap) {
        return 1;
    }
    int64_t *index = (int64_t *)malloc((size_t)next * sizeof(int64_t));
    if (!index) {
        return 0;
    }
    for (int64_t slot = 0; slot < next; slot += 1) {
        index[slot] = -1;
    }
    free(map->index);
    map->index = index;
    map->index_cap = next;
    uint64_t mask = (uint64_t)next - 1;
    for (int64_t entry = 0; entry < map->len; entry += 1) {
        uint64_t slot = map->entries[entry].hash & mask;
        while (map->index[slot] >= 0) {
            slot = (slot + 1) & mask;
        }
        map->index[slot] = entry;
    }
    return 1;
}

static _Bool kizu_map_reserve(KizuMap *map, int64_t needed) {
    if (!map || needed < 0) {
        return 0;
    }
    if (needed <= map->cap) {
        return 1;
    }
    int64_t next = map->cap == 0 ? 8 : map->cap * 2;
    while (next < needed) {
        next *= 2;
    }
    if (next > INT64_MAX / (int64_t)sizeof(KizuMapEntry)) {
        return 0;
    }
    KizuMapEntry *entries = (KizuMapEntry *)realloc(
        map->entries, (size_t)(next * (int64_t)sizeof(KizuMapEntry)));
    if (!entries) {
        return 0;
    }
    memset(entries + map->cap, 0, (size_t)((next - map->cap) * (int64_t)sizeof(KizuMapEntry)));
    map->entries = entries;
    map->cap = next;
    return 1;
}

_Bool kizu_map_insert(void *handle, const unsigned char *key, int64_t key_len, const void *value) {
    KizuMap *map = (KizuMap *)handle;
    if (!map || !key || key_len < 0 || !value) {
        return 0;
    }
    /* Before the slot is taken, because growing the index moves every slot. */
    if (!kizu_map_reindex(map, map->len + 1)) {
        return 0;
    }
    uint64_t hash = kizu_map_hash(key, key_len);
    int64_t slot = kizu_map_slot(map, key, key_len, hash);
    if (map->index[slot] >= 0) {
        memcpy(map->entries[map->index[slot]].value, value, (size_t)map->value_size);
        return 1;
    }
    if (!kizu_map_reserve(map, map->len + 1)) {
        return 0;
    }
    unsigned char *key_copy = (unsigned char *)malloc((size_t)key_len);
    unsigned char *value_copy = (unsigned char *)malloc((size_t)map->value_size);
    if ((!key_copy && key_len > 0) || !value_copy) {
        free(key_copy);
        free(value_copy);
        return 0;
    }
    if (key_len > 0) {
        memcpy(key_copy, key, (size_t)key_len);
    }
    memcpy(value_copy, value, (size_t)map->value_size);
    map->entries[map->len].key = key_copy;
    map->entries[map->len].key_len = key_len;
    map->entries[map->len].value = value_copy;
    map->entries[map->len].hash = hash;
    map->index[slot] = map->len;
    map->len += 1;
    return 1;
}

void *kizu_map_get(void *handle, const unsigned char *key, int64_t key_len) {
    KizuMap *map = (KizuMap *)handle;
    int64_t found = kizu_map_find(map, key, key_len);
    if (found < 0) {
        return NULL;
    }
    return map->entries[found].value;
}

_Bool kizu_map_contains(void *handle, const unsigned char *key, int64_t key_len) {
    return kizu_map_find((KizuMap *)handle, key, key_len) >= 0;
}

int64_t kizu_map_len(void *handle) {
    KizuMap *map = (KizuMap *)handle;
    return map ? map->len : 0;
}

void kizu_map_deinit(void *handle) {
    KizuMap *map = (KizuMap *)handle;
    if (!map) {
        return;
    }
    for (int64_t i = 0; i < map->len; i += 1) {
        free(map->entries[i].key);
        free(map->entries[i].value);
    }
    free(map->entries);
    free(map->index);
    free(map);
}

void kizu_array_deinit(void *handle) {
    KizuArray *array = (KizuArray *)handle;
    if (!array) {
        return;
    }
    free(array->data);
    free(array);
}
`
