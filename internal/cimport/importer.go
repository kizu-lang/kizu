package cimport

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var functionPattern = regexp.MustCompile(`^(.+?)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)$`)

var primitiveTypes = map[string]string{
	"void":               "void",
	"char":               "i8",
	"signed char":        "i8",
	"unsigned char":      "u8",
	"short":              "i16",
	"signed short":       "i16",
	"unsigned short":     "u16",
	"int":                "i32",
	"signed int":         "i32",
	"unsigned int":       "u32",
	"long long":          "i64",
	"signed long long":   "i64",
	"unsigned long long": "u64",
	"int8_t":             "i8",
	"uint8_t":            "u8",
	"int16_t":            "i16",
	"uint16_t":           "u16",
	"int32_t":            "i32",
	"uint32_t":           "u32",
	"int64_t":            "i64",
	"uint64_t":           "u64",
	"size_t":             "usize",
	"intptr_t":           "isize",
	"float":              "f32",
	"double":             "f64",
}

// Import converts supported C function prototypes into Kizu extern declarations.
func Import(header string) (string, error) {
	clean := stripComments(header)
	decls := []string{}
	for _, stmt := range splitStatements(clean) {
		if stmt == "" {
			continue
		}
		decl, err := importStatement(stmt)
		if err != nil {
			return "", err
		}
		decls = append(decls, decl)
	}
	sort.Strings(decls)
	return strings.Join(decls, "\n"), nil
}

// importStatement imports one semicolon-terminated C declaration.
func importStatement(stmt string) (string, error) {
	if err := rejectUnsupported(stmt); err != nil {
		return "", err
	}
	match := functionPattern.FindStringSubmatch(normalizeSpaces(stmt))
	if match == nil {
		return "", fmt.Errorf("c import error: unsupported declaration `%s`", stmt)
	}
	ret, err := cTypeToKizu(match[1])
	if err != nil {
		return "", err
	}
	params, err := importParams(match[3])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("extern \"c\" fn %s(%s) -> %s", match[2], strings.Join(params, ", "), ret), nil
}

// importParams imports a function parameter list.
func importParams(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "void" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	params := make([]string, 0, len(parts))
	for idx, part := range parts {
		param, err := importParam(part, idx+1)
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}
	return params, nil
}

// importParam imports one C parameter declaration.
func importParam(raw string, idx int) (string, error) {
	typePart, name, err := splitParam(raw, idx)
	if err != nil {
		return "", err
	}
	typ, err := cTypeToKizu(typePart)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s: %s", name, typ), nil
}

// splitParam separates a parameter type from its name.
func splitParam(raw string, idx int) (string, string, error) {
	raw = normalizePointerStars(normalizeSpaces(raw))
	if strings.Contains(raw, "[") || strings.Contains(raw, "]") {
		return "", "", fmt.Errorf("c import error: arrays are unsupported in `%s`", raw)
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", "", fmt.Errorf("c import error: empty parameter")
	}
	last := fields[len(fields)-1]
	if strings.Contains(last, "*") {
		return raw, fmt.Sprintf("p%d", idx), nil
	}
	return strings.Join(fields[:len(fields)-1], " "), last, nil
}

// cTypeToKizu maps a limited C type spelling to a Kizu type.
func cTypeToKizu(raw string) (string, error) {
	raw = normalizePointerStars(normalizeSpaces(raw))
	pointerDepth := strings.Count(raw, "*")
	raw = strings.ReplaceAll(raw, "*", "")
	elem, isConst := normalizeCBase(raw)
	base, ok := primitiveTypes[elem]
	if !ok {
		return "", fmt.Errorf("c import error: unsupported C type `%s`", raw)
	}
	return pointerType(base, pointerDepth, isConst), nil
}

// normalizeCBase removes qualifiers and returns whether the element is const.
func normalizeCBase(raw string) (string, bool) {
	fields := strings.Fields(raw)
	kept := make([]string, 0, len(fields))
	isConst := false
	for _, field := range fields {
		switch field {
		case "const":
			isConst = true
		case "volatile", "restrict":
			continue
		default:
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, " "), isConst
}

// pointerType wraps base in ptr<T> for each C pointer level.
func pointerType(base string, depth int, isConst bool) string {
	out := base
	for level := 0; level < depth; level++ {
		if level == 0 && isConst {
			out = fmt.Sprintf("ptr<const %s>", out)
		} else {
			out = fmt.Sprintf("ptr<%s>", out)
		}
	}
	return out
}

// rejectUnsupported reports C features outside the Phase 14 subset.
func rejectUnsupported(stmt string) error {
	trimmed := strings.TrimSpace(stmt)
	switch {
	case strings.HasPrefix(trimmed, "#"):
		return fmt.Errorf("c import error: preprocessor directives are unsupported")
	case strings.HasPrefix(trimmed, "typedef"):
		return fmt.Errorf("c import error: typedef is unsupported")
	case strings.HasPrefix(trimmed, "struct "):
		return fmt.Errorf("c import error: struct declarations are unsupported")
	case strings.HasPrefix(trimmed, "enum "):
		return fmt.Errorf("c import error: enum declarations are unsupported")
	case strings.Contains(trimmed, "..."):
		return fmt.Errorf("c import error: variadic functions are unsupported")
	case strings.Contains(trimmed, "(*"):
		return fmt.Errorf("c import error: function pointers are unsupported")
	default:
		return nil
	}
}

// splitStatements splits simple semicolon-delimited C declarations.
func splitStatements(header string) []string {
	parts := strings.Split(header, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if stmt := strings.TrimSpace(part); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

// stripComments removes line and block comments without running a preprocessor.
func stripComments(input string) string {
	noBlock := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(input, " ")
	lines := strings.Split(noBlock, "\n")
	for idx, line := range lines {
		if at := strings.Index(line, "//"); at >= 0 {
			lines[idx] = line[:at]
		}
	}
	return strings.Join(lines, "\n")
}

// normalizeSpaces collapses runs of whitespace.
func normalizeSpaces(input string) string {
	return strings.Join(strings.Fields(input), " ")
}

// normalizePointerStars makes C pointer stars easy to split.
func normalizePointerStars(input string) string {
	input = strings.ReplaceAll(input, "*", " * ")
	return normalizeSpaces(input)
}
