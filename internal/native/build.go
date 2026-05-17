package native

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const darwinArm64Target = "aarch64-apple-darwin"

// Artifact describes the files written by a native build.
type Artifact struct {
	Executable string
	IR         string
	Object     string
	RuntimeIR  string
	RuntimeObj string
}

// Build writes LLVM IR, lowers it to objects with llc, and links with lld.
func Build(sourcePath string, target string, llvmText string) (Artifact, error) {
	if target != darwinArm64Target {
		return Artifact{}, fmt.Errorf("native error: unsupported target `%s`", target)
	}
	artifact := artifactFor(sourcePath, target)
	if err := os.MkdirAll(filepath.Dir(artifact.Executable), 0o755); err != nil {
		return Artifact{}, err
	}
	nativeIR, err := withNativeEntry(llvmText)
	if err != nil {
		return Artifact{}, err
	}
	if err := writeBuildInputs(artifact, nativeIR); err != nil {
		return Artifact{}, err
	}
	if err := runLLC(target, artifact.IR, artifact.Object); err != nil {
		return Artifact{}, err
	}
	if err := runLLC(target, artifact.RuntimeIR, artifact.RuntimeObj); err != nil {
		return Artifact{}, err
	}
	if err := runLLD(target, artifact.Executable, artifact.Object, artifact.RuntimeObj); err != nil {
		return Artifact{}, err
	}
	if err := os.Chmod(artifact.Executable, 0o755); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

// SupportedTarget reports whether the native backend owns the target.
func SupportedTarget(target string) bool {
	return target == darwinArm64Target
}

// ArtifactPath returns the executable path that Build will write.
func ArtifactPath(sourcePath string, target string) string {
	return artifactFor(sourcePath, target).Executable
}

// artifactFor creates deterministic target paths for a source file or package.
func artifactFor(sourcePath string, target string) Artifact {
	base := artifactBaseName(sourcePath)
	dir := filepath.Join("target", "native", target, "debug")
	exe := filepath.Join(dir, base)
	if base == "kizu-selfhost" {
		exe = filepath.Join("target", base)
		dir = filepath.Join("target", "native", target, "debug", base+".build")
	}
	return Artifact{
		Executable: exe,
		IR:         filepath.Join(dir, base+".ll"),
		Object:     filepath.Join(dir, base+".o"),
		RuntimeIR:  filepath.Join(dir, "kizu_runtime.ll"),
		RuntimeObj: filepath.Join(dir, "kizu_runtime.o"),
	}
}

// artifactBaseName maps compiler package input to its public artifact name.
func artifactBaseName(sourcePath string) string {
	clean := filepath.Clean(sourcePath)
	base := filepath.Base(clean)
	if base == "selfhost" {
		return "kizu-selfhost"
	}
	if base == "kizu.toml" && filepath.Base(filepath.Dir(clean)) == "selfhost" {
		return "kizu-selfhost"
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// writeBuildInputs stores generated LLVM IR files before toolchain execution.
func writeBuildInputs(artifact Artifact, llvmText string) error {
	if err := os.MkdirAll(filepath.Dir(artifact.IR), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(artifact.IR, []byte(llvmText+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(artifact.RuntimeIR, []byte(RuntimeLLVM()), 0o644)
}

// runLLC lowers one LLVM IR file to one native object file.
func runLLC(target string, input string, output string) error {
	tool := getenvDefault("KIZU_LLC", "llc")
	args := []string{"-mtriple=" + target, "-filetype=obj", "-o", output, input}
	return runTool(tool, args...)
}

// runLLD links program and runtime objects into a native executable.
func runLLD(target string, output string, objects ...string) error {
	if target != darwinArm64Target {
		return fmt.Errorf("native error: unsupported lld target `%s`", target)
	}
	tool := getenvDefault("KIZU_LLD", "ld64.lld")
	args := []string{"-arch", "arm64", "-platform_version", "macos", "13.0.0", "13.0.0"}
	if sysroot := darwinSysroot(); sysroot != "" {
		args = append(args, "-syslibroot", sysroot)
	}
	args = append(args, darwinLibraryPaths()...)
	args = append(args, strings.Fields(os.Getenv("KIZU_LLD_FLAGS"))...)
	args = append(args, "-lSystem", "-o", output)
	args = append(args, objects...)
	return runTool(tool, args...)
}

// withNativeEntry wraps the Kizu main function in a C-compatible i32 main.
func withNativeEntry(llvmText string) (string, error) {
	type mainShape struct {
		returnType string
		prefix     string
		call       string
		checkError bool
	}
	shapes := []mainShape{
		{returnType: "void", prefix: "define void @main(", call: "call void @kizu_user_main()"},
		{returnType: "i64", prefix: "define i64 @main(", call: "call i64 @kizu_user_main()"},
		{
			returnType: "ptr",
			prefix:     "define ptr @main(",
			call:       "call ptr @kizu_user_main()",
			checkError: true,
		},
	}
	for _, shape := range shapes {
		if strings.Contains(llvmText, "\n"+shape.prefix) ||
			strings.HasPrefix(llvmText, shape.prefix) {
			renamed := replaceMainDefinitionLine(llvmText, shape.prefix,
				"define "+shape.returnType+" @kizu_user_main(")
			return renamed + nativeMainWrapper(shape.call, shape.checkError), nil
		}
	}
	return "", fmt.Errorf("native error: missing main function")
}

// replaceMainDefinitionLine renames only an LLVM function definition line.
func replaceMainDefinitionLine(llvmText string, oldPrefix string, newPrefix string) string {
	lines := strings.Split(llvmText, "\n")
	for idx, line := range lines {
		if strings.HasPrefix(line, oldPrefix) {
			lines[idx] = strings.Replace(line, oldPrefix, newPrefix, 1)
			break
		}
	}
	return strings.Join(lines, "\n")
}

// nativeMainWrapper returns the C ABI entry point for a Kizu main.
func nativeMainWrapper(call string, checkError bool) string {
	if checkError {
		return nativeErrorMainWrapper(call)
	}
	return `
@kizu_argc = external global i64
@kizu_argv = external global ptr

define i32 @main(i32 %argc, ptr %argv) {
entry:
  %wide_argc = sext i32 %argc to i64
  store i64 %wide_argc, ptr @kizu_argc
  store ptr %argv, ptr @kizu_argv
  ` + call + `
  ret i32 0
}
`
}

// nativeErrorMainWrapper maps a non-null !void sentinel to process exit 1.
func nativeErrorMainWrapper(call string) string {
	return `
@kizu_argc = external global i64
@kizu_argv = external global ptr

define i32 @main(i32 %argc, ptr %argv) {
entry:
  %wide_argc = sext i32 %argc to i64
  store i64 %wide_argc, ptr @kizu_argc
  store ptr %argv, ptr @kizu_argv
  %kizu_status = ` + call + `
  %kizu_failed = icmp ne ptr %kizu_status, null
  br i1 %kizu_failed, label %error, label %ok
ok:
  ret i32 0
error:
  ret i32 1
}
`
}

// darwinSysroot returns the explicit SDK root used by direct ld64.lld calls.
func darwinSysroot() string {
	if value := os.Getenv("KIZU_SYSROOT"); value != "" {
		return value
	}
	if value := os.Getenv("SDKROOT"); value != "" {
		return value
	}
	if value := sysrootFromFlags(os.Getenv("NIX_CFLAGS_COMPILE")); value != "" {
		return value
	}
	return xcrunSDKPath()
}

// sysrootFromFlags extracts an explicit -isysroot value from compiler flags.
func sysrootFromFlags(flags string) string {
	parts := strings.Fields(flags)
	for index, part := range parts {
		if part == "-isysroot" && index+1 < len(parts) {
			return parts[index+1]
		}
		if strings.HasPrefix(part, "-isysroot") && len(part) > len("-isysroot") {
			return strings.TrimPrefix(part, "-isysroot")
		}
	}
	return ""
}

// darwinLibraryPaths returns -L flags from Nix or caller-provided linker flags.
func darwinLibraryPaths() []string {
	paths := libraryPathsFromFlags(os.Getenv("NIX_LDFLAGS"))
	if path := nixLibSystemPath(os.Getenv("NIX_CFLAGS_COMPILE")); path != "" {
		paths = append(paths, "-L"+path)
	}
	if path := nixStoreLibSystemPath(); path != "" {
		paths = append(paths, "-L"+path)
	}
	return paths
}

// libraryPathsFromFlags extracts linker search directories.
func libraryPathsFromFlags(flags string) []string {
	parts := strings.Fields(flags)
	paths := []string{}
	for index, part := range parts {
		if part == "-L" && index+1 < len(parts) {
			paths = append(paths, "-L"+parts[index+1])
		}
		if strings.HasPrefix(part, "-L") && len(part) > len("-L") {
			paths = append(paths, part)
		}
	}
	return paths
}

// nixLibSystemPath derives the libSystem library path from Nix compile flags.
func nixLibSystemPath(flags string) string {
	parts := strings.Fields(flags)
	for index, part := range parts {
		if part == "-idirafter" && index+1 < len(parts) {
			if path := libSystemLibDir(parts[index+1]); path != "" {
				return path
			}
		}
		if strings.HasPrefix(part, "-idirafter") {
			if path := libSystemLibDir(strings.TrimPrefix(part, "-idirafter")); path != "" {
				return path
			}
		}
	}
	return ""
}

// libSystemLibDir maps a Nix libSystem include directory to its library dir.
func libSystemLibDir(path string) string {
	if !strings.Contains(path, "libSystem") || filepath.Base(path) != "include" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "lib")
}

// nixStoreLibSystemPath discovers Nix's explicit libSystem package if present.
func nixStoreLibSystemPath() string {
	matches, err := filepath.Glob("/nix/store/*-libSystem-*/lib/libSystem.B.tbd")
	if err != nil || len(matches) == 0 {
		return ""
	}
	return filepath.Dir(matches[0])
}

// xcrunSDKPath asks the host toolchain for the active macOS SDK path.
func xcrunSDKPath() string {
	out, err := exec.Command("xcrun", "--show-sdk-path").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runTool executes one external tool and reports stderr on failure.
func runTool(tool string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(tool, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("native error: %s failed: %s", tool, msg)
	}
	return nil
}

// getenvDefault returns an environment override or a default tool name.
func getenvDefault(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
