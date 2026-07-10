#include <dirent.h>
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

typedef struct {
    const uint8_t *ptr;
    int64_t len;
} kizu_slice_u8;

typedef struct {
    void *ptr;
} kizu_owned;

typedef struct {
    void *allocator;
    void *data;
    int64_t len;
    int64_t cap;
    int64_t element_size;
} kizu_rt_array;

typedef struct {
    int64_t size;
    int is_dir;
} kizu_fs_metadata;

typedef struct {
    kizu_slice_u8 name;
    kizu_slice_u8 path;
    uint8_t is_dir;
} kizu_fs_dir_entry;

typedef struct {
    uint8_t ok;
    uint8_t value;
    kizu_slice_u8 message;
} kizu_error_bool;

typedef struct {
    uint8_t ok;
    int64_t value;
    kizu_slice_u8 message;
} kizu_error_i64;

typedef struct {
    uint8_t ok;
    kizu_owned value;
    kizu_slice_u8 message;
} kizu_error_owned;

typedef struct {
    uint8_t ok;
    kizu_fs_metadata value;
    kizu_slice_u8 message;
} kizu_error_metadata;

typedef struct {
    uint8_t ok;
    kizu_slice_u8 value;
    kizu_slice_u8 message;
} kizu_error_slice_u8;

typedef struct {
    uint8_t ok;
    kizu_slice_u8 message;
} kizu_error_void;

static int kizu_argc;
static char **kizu_argv;
static uint8_t kizu_page_allocator_token;
static uint8_t kizu_io_token;
static uint8_t kizu_failing_io_token;

void kizu_host_init(int argc, char **argv) {
    if (argc > 0 && argv != NULL) {
        kizu_argc = argc - 1;
        kizu_argv = argv + 1;
        return;
    }
    kizu_argc = 0;
    kizu_argv = NULL;
}

static kizu_slice_u8 borrowed_slice(const char *text) {
    kizu_slice_u8 out;
    out.ptr = (const uint8_t *)text;
    out.len = (int64_t)strlen(text);
    return out;
}

static kizu_slice_u8 copied_slice(const uint8_t *bytes, size_t len) {
    uint8_t *copy = malloc(len == 0 ? 1 : len);
    if (copy == NULL) {
        return borrowed_slice("");
    }
    if (len > 0) {
        memcpy(copy, bytes, len);
    }
    kizu_slice_u8 out;
    out.ptr = copy;
    out.len = (int64_t)len;
    return out;
}

static char *slice_c_string(kizu_slice_u8 slice) {
    size_t len = slice.len < 0 ? 0 : (size_t)slice.len;
    char *out = malloc(len + 1);
    if (out == NULL) {
        return NULL;
    }
    if (len > 0) {
        memcpy(out, slice.ptr, len);
    }
    out[len] = '\0';
    return out;
}

static kizu_slice_u8 copied_c_slice(const char *text) {
    return copied_slice((const uint8_t *)text, strlen(text));
}

static kizu_slice_u8 joined_path_slice(const char *dir, const char *name) {
    size_t dir_len = strlen(dir);
    size_t name_len = strlen(name);
    int needs_sep = dir_len > 0 && dir[dir_len - 1] != '/';
    size_t total = dir_len + (needs_sep ? 1 : 0) + name_len;
    uint8_t *copy = malloc(total == 0 ? 1 : total);
    if (copy == NULL) {
        return borrowed_slice("");
    }
    size_t pos = 0;
    if (dir_len > 0) {
        memcpy(copy + pos, dir, dir_len);
        pos += dir_len;
    }
    if (needs_sep) {
        copy[pos] = (uint8_t)'/';
        pos += 1;
    }
    if (name_len > 0) {
        memcpy(copy + pos, name, name_len);
    }
    kizu_slice_u8 out;
    out.ptr = copy;
    out.len = (int64_t)total;
    return out;
}

static int dir_entry_name_compare(const void *left, const void *right) {
    const kizu_fs_dir_entry *a = (const kizu_fs_dir_entry *)left;
    const kizu_fs_dir_entry *b = (const kizu_fs_dir_entry *)right;
    int64_t min = a->name.len < b->name.len ? a->name.len : b->name.len;
    if (min > 0) {
        int cmp = memcmp(a->name.ptr, b->name.ptr, (size_t)min);
        if (cmp != 0) {
            return cmp;
        }
    }
    if (a->name.len < b->name.len) {
        return -1;
    }
    if (a->name.len > b->name.len) {
        return 1;
    }
    return 0;
}

static kizu_error_void ok_void(void) {
    kizu_error_void out;
    out.ok = 1;
    out.message = borrowed_slice("");
    return out;
}

static kizu_error_void error_void(const char *message) {
    kizu_error_void out;
    out.ok = 0;
    out.message = borrowed_slice(message);
    return out;
}

static kizu_error_slice_u8 error_slice(const char *message) {
    kizu_error_slice_u8 out;
    out.ok = 0;
    out.value = borrowed_slice("");
    out.message = borrowed_slice(message);
    return out;
}

static kizu_error_i64 ok_i64(int64_t value) {
    kizu_error_i64 out;
    out.ok = 1;
    out.value = value;
    out.message = borrowed_slice("");
    return out;
}

static kizu_error_i64 error_i64(const char *message) {
    kizu_error_i64 out;
    out.ok = 0;
    out.value = 1;
    out.message = borrowed_slice(message);
    return out;
}

static int run_child_process(char *const argv[]) {
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

void *kizu_host_page_allocator(void) {
    return &kizu_page_allocator_token;
}

void *kizu_host_io_blocking(void) {
    return &kizu_io_token;
}

void *kizu_host_io_failing(void) {
    return &kizu_failing_io_token;
}

void *kizu_host_alloc(void *allocator, int64_t bytes) {
    (void)allocator;
    size_t size = bytes <= 0 ? 1 : (size_t)bytes;
    return calloc(1, size);
}

void kizu_host_free(void *allocator, void *value) {
    (void)allocator;
    free(value);
}

void kizu_host_fs_exists(kizu_error_bool *out, void *io, kizu_slice_u8 path) {
    (void)io;
    char *name = slice_c_string(path);
    out->ok = 1;
    out->value = 0;
    out->message = borrowed_slice("");
    if (name == NULL) {
        out->ok = 0;
        out->message = borrowed_slice("allocation failed");
        return;
    }
    struct stat info;
    out->value = stat(name, &info) == 0;
    free(name);
    return;
}

void kizu_host_fs_metadata(kizu_error_metadata *out, void *io, kizu_slice_u8 path) {
    (void)io;
    char *name = slice_c_string(path);
    out->ok = 0;
    out->value.size = 0;
    out->value.is_dir = 0;
    out->message = borrowed_slice("stat failed");
    if (name == NULL) {
        out->message = borrowed_slice("allocation failed");
        return;
    }
    struct stat info;
    if (stat(name, &info) == 0) {
        out->ok = 1;
        out->value.size = (int64_t)info.st_size;
        out->value.is_dir = S_ISDIR(info.st_mode);
        out->message = borrowed_slice("");
    }
    free(name);
    return;
}

void kizu_host_fs_read_dir(kizu_error_owned *out, void *io, kizu_slice_u8 path) {
    (void)io;
    char *name = slice_c_string(path);
    out->ok = 0;
    out->value.ptr = NULL;
    out->message = borrowed_slice("read_dir failed");
    if (name == NULL) {
        out->message = borrowed_slice("allocation failed");
        return;
    }
    DIR *dir = opendir(name);
    if (dir == NULL) {
        free(name);
        return;
    }
    kizu_rt_array *array = calloc(1, sizeof(kizu_rt_array));
    if (array == NULL) {
        closedir(dir);
        free(name);
        out->message = borrowed_slice("allocation failed");
        return;
    }
    array->allocator = NULL;
    array->element_size = (int64_t)sizeof(kizu_fs_dir_entry);
    struct dirent *entry;
    while ((entry = readdir(dir)) != NULL) {
        if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0) {
            continue;
        }
        if (array->len == array->cap) {
            int64_t next_cap = array->cap == 0 ? 16 : array->cap * 2;
            size_t next_bytes = (size_t)next_cap * sizeof(kizu_fs_dir_entry);
            void *next_data = realloc(array->data, next_bytes);
            if (next_data == NULL) {
                closedir(dir);
                free(array->data);
                free(array);
                free(name);
                out->message = borrowed_slice("allocation failed");
                return;
            }
            array->data = next_data;
            array->cap = next_cap;
        }
        kizu_fs_dir_entry *items = (kizu_fs_dir_entry *)array->data;
        kizu_fs_dir_entry item;
        item.name = copied_c_slice(entry->d_name);
        item.path = joined_path_slice(name, entry->d_name);
        item.is_dir = 0;
        char *entry_path = slice_c_string(item.path);
        if (entry_path != NULL) {
            struct stat info;
            item.is_dir = stat(entry_path, &info) == 0 && S_ISDIR(info.st_mode);
            free(entry_path);
        }
        items[array->len] = item;
        array->len = array->len + 1;
    }
    closedir(dir);
    free(name);
    if (array->len > 1) {
        qsort(array->data, (size_t)array->len, sizeof(kizu_fs_dir_entry), dir_entry_name_compare);
    }
    out->ok = 1;
    out->value.ptr = array;
    out->message = borrowed_slice("");
    return;
}

void kizu_host_fs_read_file(kizu_error_slice_u8 *out, void *io, kizu_slice_u8 path) {
    if (io == &kizu_failing_io_token) {
        *out = error_slice("io runtime is failing");
        return;
    }
    char *name = slice_c_string(path);
    if (name == NULL) {
        *out = error_slice("allocation failed");
        return;
    }
    FILE *file = fopen(name, "rb");
    free(name);
    if (file == NULL) {
        *out = error_slice(errno == ENOENT ? "no such file or directory" : strerror(errno));
        return;
    }
    if (fseek(file, 0, SEEK_END) != 0) {
        fclose(file);
        *out = error_slice("seek failed");
        return;
    }
    long size = ftell(file);
    if (size < 0 || fseek(file, 0, SEEK_SET) != 0) {
        fclose(file);
        *out = error_slice("tell failed");
        return;
    }
    uint8_t *buffer = malloc((size_t)size == 0 ? 1 : (size_t)size);
    if (buffer == NULL) {
        fclose(file);
        *out = error_slice("allocation failed");
        return;
    }
    size_t read = fread(buffer, 1, (size_t)size, file);
    fclose(file);
    if (read != (size_t)size) {
        free(buffer);
        *out = error_slice("read failed");
        return;
    }
    out->ok = 1;
    out->value.ptr = buffer;
    out->value.len = (int64_t)read;
    out->message = borrowed_slice("");
    return;
}

void kizu_host_fs_write_file(
    kizu_error_void *out,
    void *io,
    kizu_slice_u8 path,
    kizu_slice_u8 bytes
) {
    (void)io;
    char *name = slice_c_string(path);
    if (name == NULL) {
        *out = error_void("allocation failed");
        return;
    }
    FILE *file = fopen(name, "wb");
    free(name);
    if (file == NULL) {
        *out = error_void("open failed");
        return;
    }
    size_t len = bytes.len < 0 ? 0 : (size_t)bytes.len;
    size_t written = fwrite(bytes.ptr, 1, len, file);
    fclose(file);
    if (written != len) {
        *out = error_void("write failed");
        return;
    }
    *out = ok_void();
    return;
}

void kizu_host_fs_create_dir(kizu_error_void *out, void *io, kizu_slice_u8 path) {
    (void)io;
    char *name = slice_c_string(path);
    if (name == NULL) {
        *out = error_void("allocation failed");
        return;
    }
    int status = mkdir(name, 0755);
    int saved_errno = errno;
    free(name);
    if (status == 0 || saved_errno == EEXIST) {
        *out = ok_void();
        return;
    }
    *out = error_void("mkdir failed");
    return;
}

void kizu_host_fs_remove_dir(kizu_error_void *out, void *io, kizu_slice_u8 path) {
    (void)io;
    char *name = slice_c_string(path);
    if (name == NULL) {
        *out = error_void("allocation failed");
        return;
    }
    int status = rmdir(name);
    free(name);
    *out = status == 0 ? ok_void() : error_void("remove_dir failed");
    return;
}

void kizu_host_fs_rename(
    kizu_error_void *out,
    void *io,
    kizu_slice_u8 from,
    kizu_slice_u8 to
) {
    (void)io;
    char *from_name = slice_c_string(from);
    char *to_name = slice_c_string(to);
    if (from_name == NULL || to_name == NULL) {
        free(from_name);
        free(to_name);
        *out = error_void("allocation failed");
        return;
    }
    int status = rename(from_name, to_name);
    free(from_name);
    free(to_name);
    *out = status == 0 ? ok_void() : error_void("rename failed");
    return;
}

void kizu_host_io_write_stdout(kizu_error_void *out, void *io, kizu_slice_u8 bytes) {
    if (io == &kizu_failing_io_token) {
        *out = error_void("io runtime is failing");
        return;
    }
    size_t len = bytes.len < 0 ? 0 : (size_t)bytes.len;
    *out = fwrite(bytes.ptr, 1, len, stdout) == len ? ok_void() : error_void("stdout failed");
    return;
}

void kizu_host_io_write_stderr(kizu_error_void *out, void *io, kizu_slice_u8 bytes) {
    if (io == &kizu_failing_io_token) {
        *out = error_void("io runtime is failing");
        return;
    }
    size_t len = bytes.len < 0 ? 0 : (size_t)bytes.len;
    *out = fwrite(bytes.ptr, 1, len, stderr) == len ? ok_void() : error_void("stderr failed");
    return;
}

int64_t kizu_host_process_arg_count(void) {
    return (int64_t)kizu_argc;
}

void kizu_host_process_arg(kizu_error_slice_u8 *out, int64_t index) {
    if (index < 0 || index >= kizu_argc || kizu_argv == NULL) {
        *out = error_slice("process arg index out of bounds");
        return;
    }
    out->ok = 1;
    out->value = borrowed_slice(kizu_argv[index]);
    out->message = borrowed_slice("");
    return;
}

void kizu_host_process_env(kizu_error_slice_u8 *out, kizu_slice_u8 name) {
    char *key = slice_c_string(name);
    if (key == NULL) {
        *out = error_slice("allocation failed");
        return;
    }
    char *value = getenv(key);
    free(key);
    if (value == NULL) {
        *out = error_slice("environment variable not found");
        return;
    }
    out->ok = 1;
    out->value = borrowed_slice(value);
    out->message = borrowed_slice("");
    return;
}

kizu_slice_u8 kizu_host_process_env_or_empty(kizu_slice_u8 name) {
    char *key = slice_c_string(name);
    if (key == NULL) {
        return borrowed_slice("");
    }
    char *value = getenv(key);
    free(key);
    if (value == NULL) {
        return borrowed_slice("");
    }
    return borrowed_slice(value);
}

int64_t kizu_host_process_monotonic_millis(void) {
    struct timespec ts;
    if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) {
        return 0;
    }
    return ((int64_t)ts.tv_sec * 1000) + ((int64_t)ts.tv_nsec / 1000000);
}

int64_t kizu_host_process_id(void) {
    return (int64_t)getpid();
}

int64_t kizu_host_process_exit_code(int64_t code) {
    return code;
}

void kizu_host_process_exit(int64_t code) {
    exit((int)code);
}

void kizu_host_process_spawn_wait8(
    kizu_error_i64 *out,
    int64_t argc,
    kizu_slice_u8 arg0,
    kizu_slice_u8 arg1,
    kizu_slice_u8 arg2,
    kizu_slice_u8 arg3,
    kizu_slice_u8 arg4,
    kizu_slice_u8 arg5,
    kizu_slice_u8 arg6,
    kizu_slice_u8 arg7
) {
    kizu_slice_u8 raw_args[] = {arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7};
    char *owned_args[8] = {0};
    char *argv[9] = {0};
    if (argc < 1 || argc > 8) {
        *out = error_i64("invalid process argument count");
        return;
    }
    if (raw_args[0].len == 0) {
        *out = error_i64("missing process executable");
        return;
    }
    for (int index = 0; index < argc; index++) {
        owned_args[index] = slice_c_string(raw_args[index]);
        if (owned_args[index] == NULL) {
            for (int free_index = 0; free_index < 8; free_index++) {
                free(owned_args[free_index]);
            }
            *out = error_i64("allocation failed");
            return;
        }
        argv[index] = owned_args[index];
    }
    int code = run_child_process(argv);
    for (int free_index = 0; free_index < 8; free_index++) {
        free(owned_args[free_index]);
    }
    *out = ok_i64((int64_t)code);
    return;
}

void kizu_host_process_spawn_wait_forwarded(
    kizu_error_i64 *out,
    kizu_slice_u8 executable,
    int64_t arg_start,
    int64_t arg_count
) {
    if (executable.len == 0) {
        *out = error_i64("missing process executable");
        return;
    }
    if (arg_start < 0 || arg_count < 0 || arg_start > kizu_argc - arg_count) {
        *out = error_i64("process arg range out of bounds");
        return;
    }
    char *owned_executable = slice_c_string(executable);
    char **argv = calloc((size_t)arg_count + 2, sizeof(char *));
    if (owned_executable == NULL || argv == NULL) {
        free(owned_executable);
        free(argv);
        *out = error_i64("allocation failed");
        return;
    }
    argv[0] = owned_executable;
    for (int64_t index = 0; index < arg_count; index++) {
        argv[index + 1] = kizu_argv[arg_start + index];
    }
    int code = run_child_process(argv);
    free(owned_executable);
    free(argv);
    *out = ok_i64((int64_t)code);
}

void kizu_host_trap(kizu_slice_u8 message) {
    size_t len = message.len < 0 ? 0 : (size_t)message.len;
    fwrite(message.ptr, 1, len, stderr);
    exit(1);
}
