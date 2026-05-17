package project

import (
	"fmt"
	"strings"
)

// Manifest is the minimal kizu.toml shape accepted by the compiler.
type Manifest struct {
	PackageName string
	Version     string
	Root        string
	Paths       []string
	Entries     []Module
}

// ParseManifest parses the declarative subset of kizu.toml used by Kizu.
func ParseManifest(source string) (Manifest, error) {
	return parseManifest(source, false)
}

// ParseStdManifest parses the compiler-owned std manifest.
func ParseStdManifest(source string) (Manifest, error) {
	return parseManifest(source, true)
}

// parseManifest parses kizu.toml with an optional std package exception.
func parseManifest(source string, allowStd bool) (Manifest, error) {
	var manifest Manifest
	section := ""
	for lineNo, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(stripComment(raw))
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
		if err := assignManifestValue(&manifest, section, key, value, lineNo+1); err != nil {
			return manifest, err
		}
	}
	return validateManifest(manifest, allowStd)
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
	case "modules.entries":
		parsed, err := parseModuleEntries(value, lineNo)
		if err != nil {
			return err
		}
		manifest.Entries = parsed
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
	for _, part := range strings.Split(body, ",") {
		parsed, err := parseStringValue(strings.TrimSpace(part), lineNo)
		if err != nil {
			return nil, err
		}
		values = append(values, parsed)
	}
	return values, nil
}

// parseModuleEntries parses module path and file entries as `module|file`.
func parseModuleEntries(value string, lineNo int) ([]Module, error) {
	values, err := parseStringList(value, lineNo)
	if err != nil {
		return nil, err
	}
	entries := make([]Module, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, "|")
		if len(parts) < 2 || len(parts) > 3 || strings.TrimSpace(parts[0]) == "" ||
			strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("manifest error:%d: expected module entry `path|file`", lineNo)
		}
		if len(parts) == 3 && parts[2] != "test" {
			return nil, fmt.Errorf("manifest error:%d: expected module entry marker `test`", lineNo)
		}
		entries = append(entries, Module{Path: parts[0], File: parts[1]})
	}
	return entries, nil
}

// validateManifest checks required fields and reserved package names.
func validateManifest(manifest Manifest, allowStd bool) (Manifest, error) {
	if manifest.PackageName == "" {
		return manifest, fmt.Errorf("manifest error: missing [package].name")
	}
	if manifest.PackageName == "std" && !allowStd {
		return manifest, fmt.Errorf("manifest error: package name `std` is reserved")
	}
	if manifest.Root == "" {
		return manifest, fmt.Errorf("manifest error: missing [modules].root")
	}
	if len(manifest.Paths) == 0 {
		manifest.Paths = []string{"src"}
	}
	return manifest, nil
}
