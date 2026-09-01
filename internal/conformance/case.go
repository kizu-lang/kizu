package conformance

import (
	"fmt"
	"path/filepath"
	"strings"
)

// A case is one program and what the tree promises about it, and the program
// itself is where that promise is written. There is no registry, so a program
// cannot fall out of one, and nothing has to be kept in step with the file it
// describes.

// Case is one program and what running it has to produce.
type Case struct {
	// Path names the file or package to run, relative to the repository root.
	// It is also the case name: one program, one promise, one place to look.
	Path string
	// Command is the CLI verb the case is run with.
	Command string
	// MustFail is set by the `-fails` directives, where failing is the promise.
	MustFail bool
	// Args are the arguments the directive line passes after the path.
	Args []string
	// Env contains explicit NAME=value host bindings. A case owns only the
	// variables it names; runners must not smuggle process-global test state in.
	Env []string
	// Dirs contains repository-relative host directories the program needs.
	// Wasm runners expose only these declared preopens to the guest.
	Dirs []string
	// Stdout is what the program must print. It is nil when the directive
	// produces no output, which is not the same as printing nothing.
	Stdout    *string
	ErrorText string
	Pending   string
	Features  []string
}

// directives maps each directive word to the CLI verb it runs and whether the
// promise is that the verb fails.
var directives = map[string]struct {
	command  string
	mustFail bool
}{
	"run":         {"run", false},
	"check":       {"check", false},
	"test":        {"test", false},
	"run-fails":   {"run", true},
	"check-fails": {"check", true},
	"test-fails":  {"test", true},
	"parse-fails": {"parse", true},
}

// block returns the run of comment lines a program ends with, with the comment
// marker stripped. That trailing run is the case: a program says what it is
// after it has said what it does, so reading it starts with the code.
func block(source string) ([]string, error) {
	lines := strings.Split(strings.TrimRight(source, "\n"), "\n")
	start := len(lines)
	for start > 0 && strings.HasPrefix(lines[start-1], "//") {
		start--
	}
	if start == len(lines) {
		return nil, fmt.Errorf(
			"must end with a case block: a directive line, `features:`, and what it produces",
		)
	}
	out := make([]string, 0, len(lines)-start)
	for _, line := range lines[start:] {
		out = append(out, strings.TrimPrefix(strings.TrimPrefix(line, "//"), " "))
	}
	return out, nil
}

// parse reads one case block. The directive comes first because it decides what
// the rest of the block is allowed to say.
func parse(path string, lines []string) (Case, error) {
	head := strings.Fields(lines[0])
	if len(head) == 0 {
		return Case{}, fmt.Errorf("case block must start with a directive")
	}
	directive, known := directives[head[0]]
	if !known {
		return Case{}, fmt.Errorf("unknown directive `%s`", head[0])
	}
	entry := Case{
		Path: path, Command: directive.command, MustFail: directive.mustFail, Args: head[1:],
	}
	for index := 1; index < len(lines); index++ {
		key, value, found := strings.Cut(lines[index], ":")
		if !found {
			return Case{}, fmt.Errorf("expected `key: value`, got `%s`", lines[index])
		}
		value = strings.TrimSpace(value)
		switch key {
		case "features":
			entry.Features = strings.Fields(value)
		case "env":
			entry.Env = append(entry.Env, value)
		case "dir":
			entry.Dirs = append(entry.Dirs, value)
		case "pending":
			entry.Pending = value
		case "error":
			entry.ErrorText = value
		case "output":
			// The rest of the block is the output, so nothing can follow it.
			// A line that looks like a key is a line the program prints.
			output := strings.Join(lines[index+1:], "\n")
			if output != "" {
				output += "\n"
			}
			entry.Stdout = &output
			index = len(lines)
		default:
			return Case{}, fmt.Errorf("unknown key `%s`", key)
		}
	}
	return entry, validate(entry)
}

// validate rejects a block that does not say enough to be checked.
func validate(entry Case) error {
	if err := validateEnv(entry.Env); err != nil {
		return err
	}
	if err := validateDirs(entry.Dirs); err != nil {
		return err
	}
	return validatePromise(entry)
}

// validateEnv checks repeatable environment bindings independently from the
// command's output promise.
func validateEnv(bindings []string) error {
	seenEnv := map[string]bool{}
	for _, binding := range bindings {
		name, _, ok := strings.Cut(binding, "=")
		if !ok || name == "" || strings.ContainsRune(binding, '\x00') {
			return fmt.Errorf("`env:` must be `NAME=value`, got `%s`", binding)
		}
		if seenEnv[name] {
			return fmt.Errorf("`env:` repeats `%s`", name)
		}
		seenEnv[name] = true
	}
	return nil
}

// validateDirs keeps filesystem capabilities reproducible and inside the
// repository instead of accepting Wasmtime-specific guest mappings.
func validateDirs(dirs []string) error {
	seenDirs := map[string]bool{}
	for _, dir := range dirs {
		clean := filepath.Clean(dir)
		if dir == "" || strings.ContainsRune(dir, '\x00') || filepath.IsAbs(dir) ||
			clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
			strings.Contains(dir, "::") {
			return fmt.Errorf("`dir:` must name a repository-relative directory")
		}
		if seenDirs[dir] {
			return fmt.Errorf("`dir:` repeats `%s`", dir)
		}
		seenDirs[dir] = true
	}
	return nil
}

// validatePromise checks directive-specific features, failure text, and
// observable output after host capability metadata has been validated.
func validatePromise(entry Case) error {
	if len(entry.Features) == 0 {
		return fmt.Errorf("`features:` must not be empty")
	}
	if entry.MustFail {
		if entry.ErrorText == "" {
			return fmt.Errorf("a `-fails` case must declare `error:`")
		}
		if entry.Stdout != nil {
			return fmt.Errorf("a `-fails` case has no `output:` to declare")
		}
		return nil
	}
	if entry.ErrorText != "" {
		return fmt.Errorf("`error:` belongs to a `-fails` case")
	}
	if entry.Command == "check" {
		if entry.Stdout != nil {
			return fmt.Errorf("a `check` case runs nothing and has no `output:`")
		}
		return nil
	}
	if entry.Stdout == nil {
		return fmt.Errorf("a `%s` case must declare `output:`", entry.Command)
	}
	return nil
}
