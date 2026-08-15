package manifest

import (
	"fmt"
	"strings"
)

// Manifest is the minimal kizu.toml shape accepted by the compiler.
//
// There is no export list. What a package keeps to itself is decided by where
// the source sits: a module under an `internal` directory is reachable from the
// subtree that directory belongs to and nowhere else.
type Manifest struct {
	PackageName string
	Version     string
	Root        string
	Paths       []string
}

// ParseManifest parses the declarative subset of kizu.toml used by Kizu.
func ParseManifest(source string) (Manifest, error) {
	return parseManifest(source, false)
}

// ParseStdManifest parses the std package manifest with its reserved name.
func ParseStdManifest(source string) (Manifest, error) {
	return parseManifest(source, true)
}

// parseManifest parses the declarative subset shared by user and std manifests.
func parseManifest(source string, allowReservedName bool) (Manifest, error) {
	var manifest Manifest
	section := ""
	lines := strings.Split(source, "\n")
	for lineNo := 0; lineNo < len(lines); lineNo++ {
		line := strings.TrimSpace(stripComment(lines[lineNo]))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			next, err := parseSection(line, lineNo+1)
			if err != nil {
				return manifest, err
			}
			section = next
			continue
		}
		key, value, err := parseAssignment(line, lineNo+1)
		if err != nil {
			return manifest, err
		}
		if startsMultilineArray(value) {
			value, lineNo, err = collectMultilineArray(value, lines, lineNo)
			if err != nil {
				return manifest, err
			}
		}
		if err := assignManifestValue(&manifest, section, key, value, lineNo+1); err != nil {
			return manifest, err
		}
	}
	return validateManifest(manifest, allowReservedName)
}

// startsMultilineArray reports arrays that continue on later lines.
func startsMultilineArray(value string) bool {
	return strings.HasPrefix(value, "[") && !strings.HasSuffix(value, "]")
}

// collectMultilineArray joins the supported TOML string array subset.
func collectMultilineArray(value string, lines []string, lineNo int) (string, int, error) {
	var builder strings.Builder
	builder.WriteString(value)
	for next := lineNo + 1; next < len(lines); next++ {
		line := strings.TrimSpace(stripComment(lines[next]))
		if line == "" {
			continue
		}
		builder.WriteString(line)
		if strings.HasSuffix(line, "]") {
			return builder.String(), next, nil
		}
	}
	return "", lineNo, fmt.Errorf("manifest error:%d: unterminated string array", lineNo+1)
}

// stripComment removes a full-line or suffix TOML comment in the supported subset.
func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

// parseSection parses a TOML section header.
func parseSection(line string, lineNo int) (string, error) {
	if !strings.HasSuffix(line, "]") {
		return "", fmt.Errorf("manifest error:%d: invalid section header", lineNo)
	}
	section := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
	switch section {
	case "package", "modules":
		return section, nil
	default:
		return "", fmt.Errorf("manifest error:%d: unsupported section `%s`", lineNo, section)
	}
}

// parseAssignment parses one key/value assignment.
func parseAssignment(line string, lineNo int) (string, string, error) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("manifest error:%d: expected key = value", lineNo)
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" || value == "" {
		return "", "", fmt.Errorf("manifest error:%d: expected key = value", lineNo)
	}
	return key, value, nil
}

// assignManifestValue stores one parsed manifest value.
func assignManifestValue(
	manifest *Manifest,
	section string,
	key string,
	value string,
	lineNo int,
) error {
	switch section + "." + key {
	case "package.name":
		parsed, err := parseStringValue(value, lineNo)
		if err != nil {
			return err
		}
		manifest.PackageName = parsed
	case "package.version":
		parsed, err := parseStringValue(value, lineNo)
		if err != nil {
			return err
		}
		manifest.Version = parsed
	case "modules.root":
		parsed, err := parseStringValue(value, lineNo)
		if err != nil {
			return err
		}
		manifest.Root = parsed
	case "modules.paths":
		parsed, err := parseStringList(value, lineNo)
		if err != nil {
			return err
		}
		manifest.Paths = parsed
	default:
		return fmt.Errorf("manifest error:%d: unsupported key `%s.%s`", lineNo, section, key)
	}
	return nil
}

// parseStringValue parses a quoted string value.
func parseStringValue(value string, lineNo int) (string, error) {
	if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "\""), "\""), nil
	}
	return "", fmt.Errorf("manifest error:%d: expected quoted string", lineNo)
}

// parseStringList parses a simple TOML string array.
func parseStringList(value string, lineNo int) ([]string, error) {
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("manifest error:%d: expected string array", lineNo)
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if body == "" {
		return nil, nil
	}
	values := []string{}
	parts := strings.Split(body, ",")
	for idx, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" && idx == len(parts)-1 {
			continue
		}
		parsed, err := parseStringValue(trimmed, lineNo)
		if err != nil {
			return nil, err
		}
		values = append(values, parsed)
	}
	return values, nil
}

// validateManifest checks required fields and reserved package names.
func validateManifest(manifest Manifest, allowReservedName bool) (Manifest, error) {
	if manifest.PackageName == "" {
		return manifest, fmt.Errorf("manifest error: missing [package].name")
	}
	if manifest.PackageName == "std" && !allowReservedName {
		return manifest, fmt.Errorf("manifest error: package name `std` is reserved")
	}
	if len(manifest.Paths) == 0 {
		manifest.Paths = []string{"src"}
	}
	return manifest, nil
}
