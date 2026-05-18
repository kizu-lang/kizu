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
	args = append(args, irPath, runtimePath, "-o", options.Output)
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
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static int kizu_argc = 0;
static char **kizu_argv = 0;

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

void kizu_print_cstring(const unsigned char *s) {
    if (s == 0) {
        fputc('\n', stdout);
        return;
    }
    fputs((const char *)s, stdout);
    fputc('\n', stdout);
}

typedef struct {
    unsigned char *data;
    int64_t len;
    int64_t cap;
} KizuString;

static void kizu_string_ensure(KizuString *s, int64_t additional) {
    if (s == 0 || additional < 0) {
        return;
    }
    int64_t need = s->len + additional + 1;
    if (need <= s->cap) {
        return;
    }
    int64_t cap = s->cap;
    if (cap < 32) {
        cap = 32;
    }
    while (cap < need) {
        cap = cap * 2;
    }
    unsigned char *data = (unsigned char *)realloc(s->data, (size_t)cap);
    if (data == 0) {
        return;
    }
    s->data = data;
    s->cap = cap;
    s->data[s->len] = 0;
}

KizuString *kizu_string_new(void) {
    KizuString *s = (KizuString *)calloc(1, sizeof(KizuString));
    kizu_string_ensure(s, 0);
    return s;
}

void kizu_string_append_bytes(KizuString *s, const unsigned char *bytes) {
    if (s == 0 || bytes == 0) {
        return;
    }
    int64_t length = (int64_t)strlen((const char *)bytes);
    kizu_string_ensure(s, length);
    if (s->data == 0 || s->len + length + 1 > s->cap) {
        return;
    }
    memcpy(s->data + s->len, bytes, (size_t)length);
    s->len += length;
    s->data[s->len] = 0;
}

void kizu_string_append_byte(KizuString *s, unsigned char byte) {
    if (s == 0) {
        return;
    }
    kizu_string_ensure(s, 1);
    if (s->data == 0 || s->len + 2 > s->cap) {
        return;
    }
    s->data[s->len] = byte;
    s->len += 1;
    s->data[s->len] = 0;
}

void kizu_string_reserve(KizuString *s, int64_t additional) {
    kizu_string_ensure(s, additional);
}

void kizu_string_truncate(KizuString *s, int64_t length) {
    if (s == 0 || length < 0 || length > s->len) {
        return;
    }
    s->len = length;
    if (s->data != 0) {
        s->data[s->len] = 0;
    }
}

void kizu_string_clear(KizuString *s) {
    kizu_string_truncate(s, 0);
}

void kizu_string_deinit(KizuString *s) {
    if (s == 0) {
        return;
    }
    free(s->data);
    free(s);
}

const unsigned char *kizu_string_as_bytes(KizuString *s) {
    if (s == 0 || s->data == 0) {
        return (const unsigned char *)"";
    }
    s->data[s->len] = 0;
    return s->data;
}

int64_t kizu_string_len(KizuString *s) {
    if (s == 0) {
        return 0;
    }
    return s->len;
}

int64_t kizu_string_capacity(KizuString *s) {
    if (s == 0) {
        return 0;
    }
    return s->cap;
}

void kizu_process_init(int argc, char **argv) {
    kizu_argc = argc;
    kizu_argv = argv;
}

int64_t kizu_process_arg_count(void) {
    (void)kizu_argv;
    return (int64_t)kizu_argc;
}

const unsigned char *kizu_process_arg(int64_t index) {
    if (index < 0 || index >= kizu_argc || kizu_argv == 0) {
        return (const unsigned char *)"";
    }
    return (const unsigned char *)kizu_argv[index];
}

const unsigned char *kizu_process_env(const unsigned char *name) {
    char *value = getenv((const char *)name);
    if (value == 0) {
        return (const unsigned char *)"";
    }
    return (const unsigned char *)value;
}

const unsigned char *kizu_fs_read_file(const unsigned char *path) {
    FILE *file = fopen((const char *)path, "rb");
    if (file == 0) {
        return (const unsigned char *)"";
    }
    if (fseek(file, 0, SEEK_END) != 0) {
        fclose(file);
        return (const unsigned char *)"";
    }
    long size = ftell(file);
    if (size < 0) {
        fclose(file);
        return (const unsigned char *)"";
    }
    rewind(file);
    unsigned char *buffer = (unsigned char *)malloc((size_t)size + 1);
    if (buffer == 0) {
        fclose(file);
        return (const unsigned char *)"";
    }
    size_t read = fread(buffer, 1, (size_t)size, file);
    fclose(file);
    buffer[read] = 0;
    return buffer;
}

void kizu_fs_write_file(const unsigned char *path, const unsigned char *bytes) {
    FILE *file = fopen((const char *)path, "wb");
    if (file == 0) {
        return;
    }
    if (bytes != 0) {
        fputs((const char *)bytes, file);
    }
    fclose(file);
}

_Bool kizu_fs_exists(const unsigned char *path) {
    FILE *file = fopen((const char *)path, "rb");
    if (file == 0) {
        return 0;
    }
    fclose(file);
    return 1;
}

int64_t kizu_mem_len(const unsigned char *bytes) {
    if (bytes == 0) {
        return 0;
    }
    return (int64_t)strlen((const char *)bytes);
}
`
