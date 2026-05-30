package native

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Options describes one native link request.
type Options struct {
	LLVMIR  string
	Output  string
	Triple  string
	CPU     string
	ABI     string
	LibC    string
	Runtime string
	Emit    string
	Linker  string
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
	irPath, runtimePath, err := writeInputs(tmp, options.LLVMIR)
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

// writeInputs writes LLVM IR and the minimal Kizu runtime shim.
func writeInputs(dir string, llvmIR string) (string, string, error) {
	irPath := filepath.Join(dir, "main.ll")
	runtimePath := filepath.Join(dir, "runtime.c")
	if err := os.WriteFile(irPath, []byte(llvmIR), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(runtimePath, []byte(runtimeSource), 0o644); err != nil {
		return "", "", err
	}
	return irPath, runtimePath, nil
}

// runClang invokes the configured C/LLVM toolchain with explicit inputs.
func runClang(irPath string, runtimePath string, options Options) ([]string, error) {
	args := []string{}
	if options.Triple != "" {
		args = append(args, "-target", options.Triple)
	}
	args = append(args, "-O0", irPath, runtimePath, "-o", options.Output)
	cmd := exec.Command(options.Linker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("native error: %s failed: %w\n%s", options.Linker, err, out)
	}
	return append([]string{options.Linker}, args...), nil
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
	Output  string   `json:"output"`
	Command []string `json:"command"`
}

// writeMetadata writes the explicit build configuration used for this artifact.
func writeMetadata(options Options, command []string) error {
	metadata := Metadata{
		Target: "native", Triple: options.Triple, CPU: options.CPU, ABI: options.ABI,
		LibC: options.LibC, Runtime: options.Runtime, Emit: options.Emit,
		Linker: options.Linker, Output: options.Output, Command: command,
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(options.Output+".kizu-build.json", append(data, '\n'), 0o644)
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
    KizuSliceU8 error;
} KizuErrorVoid;

typedef struct {
    _Bool ok;
    _Bool value;
    KizuSliceU8 error;
} KizuErrorBool;

typedef struct {
    _Bool ok;
    int64_t value;
    KizuSliceU8 error;
} KizuErrorI64;

typedef struct {
    _Bool ok;
    KizuSliceU8 value;
    KizuSliceU8 error;
} KizuErrorSliceU8;

typedef struct {
    _Bool ok;
    KizuFsMetadata value;
    KizuSliceU8 error;
} KizuErrorFsMetadata;

typedef struct {
    _Bool ok;
    void *value;
    KizuSliceU8 error;
} KizuErrorPtr;

typedef struct {
    unsigned char *key;
    int64_t key_len;
    unsigned char *value;
} KizuMapEntry;

typedef struct {
    KizuMapEntry *entries;
    int64_t len;
    int64_t cap;
    int64_t value_size;
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

static KizuSliceU8 kizu_error_message(const char *value) {
    return kizu_slice_from_cstr(value);
}

static KizuErrorVoid kizu_ok_void(void) {
    KizuErrorVoid out;
    out.ok = 1;
    out.error = kizu_slice_from_cstr("");
    return out;
}

static KizuErrorVoid kizu_err_void(const char *message) {
    KizuErrorVoid out;
    out.ok = 0;
    out.error = kizu_error_message(message);
    return out;
}

static KizuErrorBool kizu_ok_bool(_Bool value) {
    KizuErrorBool out;
    out.ok = 1;
    out.value = value;
    out.error = kizu_slice_from_cstr("");
    return out;
}

static KizuErrorBool kizu_err_bool(const char *message) {
    KizuErrorBool out;
    out.ok = 0;
    out.value = 0;
    out.error = kizu_error_message(message);
    return out;
}

static KizuErrorI64 kizu_ok_i64(int64_t value) {
    KizuErrorI64 out;
    out.ok = 1;
    out.value = value;
    out.error = kizu_slice_from_cstr("");
    return out;
}

static KizuErrorI64 kizu_err_i64(const char *message) {
    KizuErrorI64 out;
    out.ok = 0;
    out.value = 1;
    out.error = kizu_error_message(message);
    return out;
}

static KizuErrorSliceU8 kizu_ok_slice(KizuSliceU8 value) {
    KizuErrorSliceU8 out;
    out.ok = 1;
    out.value = value;
    out.error = kizu_slice_from_cstr("");
    return out;
}

static KizuErrorSliceU8 kizu_err_slice(const char *message) {
    KizuErrorSliceU8 out;
    out.ok = 0;
    out.value = kizu_slice_from_cstr("");
    out.error = kizu_error_message(message);
    return out;
}

static KizuErrorFsMetadata kizu_ok_metadata(KizuFsMetadata value) {
    KizuErrorFsMetadata out;
    out.ok = 1;
    out.value = value;
    out.error = kizu_slice_from_cstr("");
    return out;
}

static KizuErrorFsMetadata kizu_err_metadata(const char *message) {
    KizuErrorFsMetadata out;
    out.ok = 0;
    out.value.size = 0;
    out.value.is_dir = 0;
    out.error = kizu_error_message(message);
    return out;
}

static KizuErrorPtr kizu_ok_ptr(void *value) {
    KizuErrorPtr out;
    out.ok = 1;
    out.value = value;
    out.error = kizu_slice_from_cstr("");
    return out;
}

static KizuErrorPtr kizu_err_ptr(const char *message) {
    KizuErrorPtr out;
    out.ok = 0;
    out.value = NULL;
    out.error = kizu_error_message(message);
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

void kizu_print_int(int64_t v) {
    printf("%lld\n", (long long)v);
}

void kizu_print_bool(_Bool v) {
    fputs(v ? "true\n" : "false\n", stdout);
}

void *std_builtin_mem_page_allocator(void) {
    return NULL;
}

void *std_builtin_io_blocking(void) {
    return (void *)1;
}

void *std_builtin_io_threaded(void) {
    return (void *)1;
}

void *std_builtin_io_failing(void) {
    return (void *)2;
}

static KizuErrorVoid kizu_std_builtin_io_write_stdout_result(void *io, KizuSliceU8 bytes) {
    (void)io;
    if (bytes.len > 0 && bytes.ptr) {
        fwrite(bytes.ptr, 1, (size_t)bytes.len, stdout);
    }
    return kizu_ok_void();
}

void std_builtin_io_write_stdout(KizuErrorVoid *out, void *io, const KizuSliceU8 *bytes) {
    *out = kizu_std_builtin_io_write_stdout_result(io, *bytes);
}

static KizuErrorVoid kizu_std_builtin_io_write_stderr_result(void *io, KizuSliceU8 bytes) {
    (void)io;
    if (bytes.len > 0 && bytes.ptr) {
        fwrite(bytes.ptr, 1, (size_t)bytes.len, stderr);
    }
    return kizu_ok_void();
}

void std_builtin_io_write_stderr(KizuErrorVoid *out, void *io, const KizuSliceU8 *bytes) {
    *out = kizu_std_builtin_io_write_stderr_result(io, *bytes);
}

static KizuErrorSliceU8 kizu_std_builtin_io_read_stdin_result(void *io) {
    (void)io;
    int64_t len = 0;
    int64_t cap = 4096;
    unsigned char *data = (unsigned char *)malloc((size_t)cap);
    if (!data) {
        return kizu_err_slice("out of memory");
    }
    for (;;) {
        if (len == cap) {
            cap *= 2;
            unsigned char *next = (unsigned char *)realloc(data, (size_t)cap);
            if (!next) {
                free(data);
                return kizu_err_slice("out of memory");
            }
            data = next;
        }
        size_t read_count = fread(data + len, 1, (size_t)(cap - len), stdin);
        len += (int64_t)read_count;
        if (read_count == 0) {
            if (ferror(stdin)) {
                free(data);
                return kizu_err_slice("read stdin failed");
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
        return kizu_err_slice("process arg index out of range");
    }
    return kizu_ok_slice(kizu_slice_from_cstr(kizu_runtime_argv[index + 1]));
}

void std_builtin_process_arg(KizuErrorSliceU8 *out, int64_t index) {
    *out = kizu_std_builtin_process_arg_result(index);
}

static KizuErrorSliceU8 kizu_std_builtin_process_env_result(KizuSliceU8 name) {
    char *key = kizu_slice_to_cstr(name);
    if (!key) {
        return kizu_err_slice("invalid env name");
    }
    char *value = getenv(key);
    free(key);
    if (!value) {
        return kizu_err_slice("environment variable not found");
    }
    return kizu_ok_slice(kizu_slice_from_cstr(value));
}

void std_builtin_process_env(KizuErrorSliceU8 *out, const KizuSliceU8 *name) {
    *out = kizu_std_builtin_process_env_result(*name);
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
        *out = kizu_err_i64("invalid process argument count");
        return;
    }
    if (raw_args[0].len == 0) {
        *out = kizu_err_i64("missing process executable");
        return;
    }
    for (int index = 0; index < argc; index++) {
        owned_args[index] = kizu_slice_to_cstr(raw_args[index]);
        if (!owned_args[index]) {
            for (int free_index = 0; free_index < 8; free_index++) {
                free(owned_args[free_index]);
            }
            *out = kizu_err_i64("allocation failed");
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
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_slice("invalid path");
    }
    FILE *file = fopen(cpath, "rb");
    free(cpath);
    if (!file) {
        return kizu_err_slice("read file failed");
    }
    if (fseek(file, 0, SEEK_END) != 0) {
        fclose(file);
        return kizu_err_slice("read file failed");
    }
    long size = ftell(file);
    if (size < 0 || fseek(file, 0, SEEK_SET) != 0) {
        fclose(file);
        return kizu_err_slice("read file failed");
    }
    unsigned char *data = NULL;
    if (size > 0) {
        data = (unsigned char *)malloc((size_t)size);
        if (!data) {
            fclose(file);
            return kizu_err_slice("out of memory");
        }
        if (fread(data, 1, (size_t)size, file) != (size_t)size) {
            free(data);
            fclose(file);
            return kizu_err_slice("read file failed");
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
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void("invalid path");
    }
    FILE *file = fopen(cpath, "wb");
    free(cpath);
    if (!file) {
        return kizu_err_void("write file failed");
    }
    if (bytes.len > 0 && fwrite(bytes.ptr, 1, (size_t)bytes.len, file) != (size_t)bytes.len) {
        fclose(file);
        return kizu_err_void("write file failed");
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

static KizuErrorBool kizu_std_builtin_fs_exists_result(void *io, KizuSliceU8 path) {
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_bool("invalid path");
    }
    _Bool found = access(cpath, F_OK) == 0;
    free(cpath);
    return kizu_ok_bool(found);
}

void std_builtin_fs_exists(KizuErrorBool *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_exists_result(io, *path);
}

static KizuErrorFsMetadata kizu_std_builtin_fs_metadata_result(void *io, KizuSliceU8 path) {
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_metadata("invalid path");
    }
    struct stat st;
    if (stat(cpath, &st) != 0) {
        free(cpath);
        return kizu_err_metadata("metadata failed");
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
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_ptr("invalid path");
    }
    DIR *dir = opendir(cpath);
    if (!dir) {
        free(cpath);
        return kizu_err_ptr("read dir failed");
    }
    void *array = kizu_array_new((int64_t)sizeof(KizuFsDirEntry));
    if (!array) {
        closedir(dir);
        free(cpath);
        return kizu_err_ptr("out of memory");
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
            return kizu_err_ptr("out of memory");
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
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void("invalid path");
    }
    int result = mkdir(cpath, 0755);
    free(cpath);
    if (result != 0 && errno != EEXIST) {
        return kizu_err_void("create dir failed");
    }
    return kizu_ok_void();
}

void std_builtin_fs_create_dir(KizuErrorVoid *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_create_dir_result(io, *path);
}

static KizuErrorVoid kizu_std_builtin_fs_remove_dir_result(void *io, KizuSliceU8 path) {
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void("invalid path");
    }
    int result = rmdir(cpath);
    free(cpath);
    if (result != 0) {
        return kizu_err_void("remove dir failed");
    }
    return kizu_ok_void();
}

void std_builtin_fs_remove_dir(KizuErrorVoid *out, void *io, const KizuSliceU8 *path) {
    *out = kizu_std_builtin_fs_remove_dir_result(io, *path);
}

static KizuErrorVoid kizu_std_builtin_fs_remove_file_result(void *io, KizuSliceU8 path) {
    (void)io;
    char *cpath = kizu_slice_to_cstr(path);
    if (!cpath) {
        return kizu_err_void("invalid path");
    }
    int result = unlink(cpath);
    free(cpath);
    if (result != 0) {
        return kizu_err_void("remove file failed");
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

static _Bool kizu_array_reserve_storage(KizuArray *array, int64_t needed) {
    if (!array || needed < 0) {
        return 0;
    }
    if (needed <= array->cap) {
        return 1;
    }
    int64_t next = array->cap == 0 ? 4 : array->cap * 2;
    while (next < needed) {
        next *= 2;
    }
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

static int64_t kizu_map_find(KizuMap *map, const unsigned char *key, int64_t key_len) {
    if (!map || !key || key_len < 0) {
        return -1;
    }
    for (int64_t i = 0; i < map->len; i += 1) {
        if (map->entries[i].key_len == key_len &&
            memcmp(map->entries[i].key, key, (size_t)key_len) == 0) {
            return i;
        }
    }
    return -1;
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
    int64_t found = kizu_map_find(map, key, key_len);
    if (found >= 0) {
        memcpy(map->entries[found].value, value, (size_t)map->value_size);
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
