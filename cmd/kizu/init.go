package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const initMainSource = "fn main() {\n    print(\"hello, kizu\");\n}\n"

// commandAllowsNoTarget reports commands that may run with no path argument.
func commandAllowsNoTarget(command string) bool {
	return command == "init"
}

// initCommand scaffolds a minimal Kizu package.
func initCommand(args []string) error {
	if len(args) > 1 {
		usage()
		return fmt.Errorf("invalid init command")
	}
	target := "."
	if len(args) == 1 {
		target = args[0]
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	packageName, err := initPackageName(absTarget)
	if err != nil {
		return err
	}
	if err := ensureInitTarget(absTarget); err != nil {
		return err
	}
	if err := rejectExistingInitFiles(absTarget); err != nil {
		return err
	}
	srcDir := filepath.Join(absTarget, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return err
	}
	manifest := initManifest(packageName)
	if err := writeNewFile(filepath.Join(absTarget, "kizu.toml"), []byte(manifest)); err != nil {
		return err
	}
	if err := writeNewFile(filepath.Join(srcDir, "main.kizu"), []byte(initMainSource)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "initialized Kizu package `%s` in %s\n", packageName, absTarget)
	return nil
}

// initPackageName derives a Kizu namespace from the target directory name.
func initPackageName(target string) (string, error) {
	name := normalizeInitPackageName(filepath.Base(target))
	if name == "" {
		return "", fmt.Errorf("init error: package name is empty")
	}
	if name == "std" {
		return "", fmt.Errorf("init error: package name `std` is reserved")
	}
	if !isValidInitPackageName(name) {
		return "", fmt.Errorf("init error: invalid package name `%s`", name)
	}
	return name, nil
}

// normalizeInitPackageName maps common directory spellings to a Kizu identifier.
func normalizeInitPackageName(name string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
			lastUnderscore = false
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-':
			if builder.Len() > 0 && !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		default:
			return ""
		}
	}
	return strings.TrimRight(builder.String(), "_")
}

// isValidInitPackageName reports whether name is a Kizu identifier-like package root.
func isValidInitPackageName(name string) bool {
	if name == "" {
		return false
	}
	first := name[0]
	if !((first >= 'a' && first <= 'z') || first == '_') {
		return false
	}
	for idx := 1; idx < len(name); idx++ {
		ch := name[idx]
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

// ensureInitTarget creates the target directory or rejects non-directory paths.
func ensureInitTarget(target string) error {
	info, err := os.Stat(target)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("init error: target `%s` is not a directory", target)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(target, 0o755)
}

// rejectExistingInitFiles prevents init from overwriting user files.
func rejectExistingInitFiles(target string) error {
	for _, rel := range []string{"kizu.toml", filepath.Join("src", "main.kizu")} {
		path := filepath.Join(target, rel)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("init error: `%s` already exists", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// initManifest renders the minimal package manifest.
func initManifest(packageName string) string {
	return fmt.Sprintf(`[package]
name = "%s"
version = "0.1.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
`, packageName)
}

// writeNewFile writes content only if path does not already exist.
func writeNewFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	_, err = file.Write(content)
	return err
}
