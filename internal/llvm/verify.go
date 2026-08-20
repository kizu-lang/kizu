package llvm

import (
	"fmt"
	"strings"
)

// verifyEmittedText checks that every register and label a function reads is
// defined in that function, or is a module-level named type. The emitter
// resolves values by name while blocks are written out of control-flow order,
// so a resolution bug produces text clang rejects with a bare SSA name and no
// source position. This scan reports the same defect as an error that names
// the function, before the text leaves the package.
//
// It also checks that every `alloca` sits in its function's entry block. An
// alloca anywhere else allocates on every execution and is reclaimed only when
// the function returns, so one inside a loop grows the stack a slot per turn
// until the program dies on the guard page — a failure with nothing in source
// to point at. hoistAllocasToEntry is what holds this; the scan is what says so
// if it ever stops.
//
// The scan checks existence and placement only. It pins no instruction shape:
// any text where read names are defined somewhere in the function, and every
// alloca is in the entry block, passes.
func verifyEmittedText(text string) error {
	types := map[string]bool{}
	function := ""
	blocks := 0
	var defs map[string]bool
	var uses []string
	for _, line := range strings.Split(text, "\n") {
		switch {
		case function == "" && strings.HasPrefix(line, "define "):
			function = defineName(line)
			blocks = 0
			defs = map[string]bool{}
			uses = nil
			for _, name := range percentTokens(line) {
				defs[name] = true
			}
		case function == "":
			if name, ok := typeDefName(line); ok {
				types[name] = true
			}
		case line == "}":
			for _, name := range uses {
				if !defs[name] && !types[name] {
					return fmt.Errorf(
						"llvm error: emitter bug: function `%s` reads `%%%s` but nothing defines it",
						function, name)
				}
			}
			function = ""
		default:
			if label, ok := labelDefName(line); ok {
				defs[label] = true
				blocks++
				continue
			}
			if blocks > 1 && isAllocaLine(line) {
				return fmt.Errorf(
					"llvm error: emitter bug: function `%s` allocates outside its entry"+
						" block, which grows the stack on every execution: %s",
					function, strings.TrimSpace(line))
			}
			names := percentTokens(line)
			if def, ok := instructionDef(line); ok {
				defs[def] = true
				names = names[1:]
			}
			uses = append(uses, names...)
		}
	}
	return nil
}

// defineName extracts the symbol a `define` line declares.
func defineName(line string) string {
	start := strings.IndexByte(line, '@')
	if start < 0 {
		return line
	}
	end := strings.IndexByte(line[start:], '(')
	if end < 0 {
		return line[start+1:]
	}
	return line[start+1 : start+end]
}

// typeDefName extracts the name of a module-level `%name = type ...` line.
func typeDefName(line string) (string, bool) {
	name, rest, ok := leadingPercentName(line)
	if !ok || !strings.HasPrefix(rest, " = type ") {
		return "", false
	}
	return name, true
}

// labelDefName extracts the label a block-opening `name:` line defines.
func labelDefName(line string) (string, bool) {
	if len(line) < 2 || !strings.HasSuffix(line, ":") {
		return "", false
	}
	name := line[:len(line)-1]
	for i := 0; i < len(name); i++ {
		if !isLLVMNameByte(name[i]) {
			return "", false
		}
	}
	return name, true
}

// instructionDef extracts the register a `  %name = ...` body line defines.
func instructionDef(line string) (string, bool) {
	name, rest, ok := leadingPercentName(strings.TrimLeft(line, " "))
	if !ok || !strings.HasPrefix(rest, " = ") {
		return "", false
	}
	return name, true
}

// leadingPercentName splits a `%name` head off a line.
func leadingPercentName(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "%") {
		return "", "", false
	}
	end := 1
	for end < len(line) && isLLVMNameByte(line[end]) {
		end++
	}
	if end == 1 {
		return "", "", false
	}
	return line[1:end], line[end:], true
}

// percentTokens returns each `%name` token in a line in order, skipping
// quoted spans.
func percentTokens(line string) []string {
	names := []string{}
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"':
			inQuote = !inQuote
		case inQuote || line[i] != '%':
		default:
			end := i + 1
			for end < len(line) && isLLVMNameByte(line[end]) {
				end++
			}
			if end > i+1 {
				names = append(names, line[i+1:end])
			}
			i = end - 1
		}
	}
	return names
}

// isLLVMNameByte reports bytes valid in an unquoted LLVM identifier.
func isLLVMNameByte(ch byte) bool {
	return ch == '-' || ch == '$' || ch == '.' || ch == '_' ||
		('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') ||
		('0' <= ch && ch <= '9')
}
