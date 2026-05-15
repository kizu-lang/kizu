package buildcache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/project"
)

const (
	fileInputKind      = "file"
	packageInputKind   = "package"
	fallbackStdlibHash = "builtin-std-v0.3"
)

// newInput hashes source content and cache-shaping inputs.
func newInput(path string, target string) (cacheInput, error) {
	kind, baseDir, err := inputPathKind(path)
	if err != nil {
		return cacheInput{}, err
	}
	if kind == packageInputKind {
		return newPackageInput(baseDir, target)
	}
	return newFileInput(path, target)
}

// inputPathKind classifies a cache input path as a file or package.
func inputPathKind(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return packageInputKind, path, nil
	}
	if filepath.Base(path) == "kizu.toml" {
		return packageInputKind, filepath.Dir(path), nil
	}
	return fileInputKind, "", nil
}

// newFileInput builds the legacy single-file cache fingerprint.
func newFileInput(path string, target string) (cacheInput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheInput{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return cacheInput{}, err
	}
	sourceHash := hashBytes(data)
	stdlibHash, err := currentStdlibHash()
	if err != nil {
		return cacheInput{}, err
	}
	key := hashStrings(Version, target, fileInputKind, abs, sourceHash, stdlibHash)
	return cacheInput{
		key: key, target: target, sourcePath: abs, sourceHash: sourceHash,
		inputKind: fileInputKind, stdlibHash: stdlibHash,
	}, nil
}

// newPackageInput builds a module-aware package cache fingerprint.
func newPackageInput(baseDir string, target string) (cacheInput, error) {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return cacheInput{}, err
	}
	pkg, err := project.LoadPackage(absBase)
	if err != nil {
		return cacheInput{}, err
	}
	manifestPath := filepath.Join(absBase, "kizu.toml")
	manifestHash, err := hashFile(manifestPath)
	if err != nil {
		return cacheInput{}, err
	}
	sourceHashes, sourceHash, err := packageSourceHashes(pkg)
	if err != nil {
		return cacheInput{}, err
	}
	graphHash := moduleGraphHash(pkg)
	interfaceHash := publicInterfaceHash(pkg)
	stdlibHash, err := currentStdlibHash()
	if err != nil {
		return cacheInput{}, err
	}
	key := hashStrings(
		Version, target, packageInputKind, manifestPath, manifestHash,
		graphHash, sourceHash, interfaceHash, stdlibHash,
	)
	return cacheInput{
		key: key, target: target, sourcePath: manifestPath, sourceHash: sourceHash,
		inputKind: packageInputKind, manifestHash: manifestHash,
		moduleGraphHash: graphHash, publicInterfaceHash: interfaceHash,
		stdlibHash: stdlibHash, sourceHashes: sourceHashes,
	}, nil
}

// packageSourceHashes returns per-module hashes and their combined hash.
func packageSourceHashes(pkg *project.Package) (map[string]string, string, error) {
	hashes := map[string]string{}
	records := make([]string, 0, len(pkg.Modules))
	for _, module := range pkg.Modules {
		hash, err := hashFile(module.Module.File)
		if err != nil {
			return nil, "", err
		}
		hashes[module.Module.Path] = hash
		records = append(records, module.Module.Path+"="+hash)
	}
	sort.Strings(records)
	return hashes, hashStrings(records...), nil
}

// moduleGraphHash fingerprints module paths, files, and resolved imports.
func moduleGraphHash(pkg *project.Package) string {
	records := make([]string, 0, len(pkg.Modules))
	for _, module := range pkg.Modules {
		imports := make([]string, 0, len(module.Imports))
		for _, imported := range module.Imports {
			imports = append(imports, imported.Path)
		}
		sort.Strings(imports)
		records = append(records, fmt.Sprintf("%s|%s|%s",
			module.Module.Path, filepath.ToSlash(module.Module.File), strings.Join(imports, ",")))
	}
	sort.Strings(records)
	return hashStrings(records...)
}

// publicInterfaceHash fingerprints public declarations without function bodies.
func publicInterfaceHash(pkg *project.Package) string {
	records := []string{}
	for _, module := range pkg.Modules {
		for _, decl := range module.Program.Decls {
			if sig, ok := publicDeclSignature(decl); ok {
				records = append(records, module.Module.Path+"::"+sig)
			}
		}
	}
	sort.Strings(records)
	return hashStrings(records...)
}

// publicDeclSignature returns the public surface of one top-level declaration.
func publicDeclSignature(decl ast.Decl) (string, bool) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		return functionSignature(d), d.Public
	case *ast.StructDecl:
		return structSignature(d), d.Public
	case *ast.EnumDecl:
		return "enum " + d.Name + " {" + strings.Join(d.Tags, ",") + "}", d.Public
	case *ast.UnionDecl:
		return unionSignature(d), d.Public
	case *ast.ContractDecl:
		return contractSignature(d), d.Public
	default:
		return "", false
	}
}

// functionSignature formats a function declaration without its body.
func functionSignature(fn *ast.FunctionDecl) string {
	params := make([]string, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, param.String())
	}
	prefix := ""
	if fn.Unsafe {
		prefix += "unsafe "
	}
	if fn.ExternABI != "" {
		prefix += "extern " + fn.ExternABI + " "
	}
	return fmt.Sprintf("%sfn %s(%s) -> %s",
		prefix, fn.Name, strings.Join(params, ","), fn.ReturnType)
}

// structSignature formats public struct layout fields.
func structSignature(decl *ast.StructDecl) string {
	fields := make([]string, 0, len(decl.Fields))
	for _, field := range decl.Fields {
		fields = append(fields, field.String())
	}
	return "struct " + decl.Name + " {" + strings.Join(fields, ",") + "}"
}

// unionSignature formats public tagged union variants.
func unionSignature(decl *ast.UnionDecl) string {
	variants := make([]string, 0, len(decl.Variants))
	for _, variant := range decl.Variants {
		variants = append(variants, variant.String())
	}
	return "union " + decl.Name + " {" + strings.Join(variants, ",") + "}"
}

// contractSignature formats public contract method requirements.
func contractSignature(decl *ast.ContractDecl) string {
	methods := make([]string, 0, len(decl.Methods))
	for _, method := range decl.Methods {
		methods = append(methods, functionSignature(method))
	}
	return "contract " + decl.Name + " {" + strings.Join(methods, ",") + "}"
}

// explainChangedInput describes the first cache-shaping input that changed.
func explainChangedInput(previous Entry, input cacheInput) string {
	if previous.ManifestHash != input.manifestHash {
		return "cache miss: manifest changed"
	}
	if previous.ModuleGraphHash != input.moduleGraphHash {
		return "cache miss: module graph changed"
	}
	if previous.PublicInterfaceHash != input.publicInterfaceHash {
		return "cache miss: public interface changed"
	}
	if previous.SourceHash != input.sourceHash {
		if input.inputKind == packageInputKind {
			return "cache miss: source changed without public interface change"
		}
		return "cache miss: source changed"
	}
	if previous.StdlibHash != input.stdlibHash {
		return "cache miss: stdlib changed"
	}
	return "cache miss: build inputs changed"
}

// currentStdlibHash returns the hash of the checked-in std source skeleton.
func currentStdlibHash() (string, error) {
	root, ok, err := findStdRoot()
	if err != nil || !ok {
		return fallbackStdlibHash, err
	}
	files, err := stdlibFiles(root)
	if err != nil {
		return "", err
	}
	records := make([]string, 0, len(files))
	for _, file := range files {
		record, err := stdlibFileRecord(root, file)
		if err != nil {
			return "", err
		}
		records = append(records, record)
	}
	return hashStrings(records...), nil
}

// stdlibFileRecord returns a deterministic hash record for one std file.
func stdlibFileRecord(root string, file string) (string, error) {
	hash, err := hashFile(file)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel) + "=" + hash, nil
}

// findStdRoot walks upward from the current directory to locate std/kizu.toml.
func findStdRoot() (string, bool, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	for {
		candidate := filepath.Join(dir, "std")
		if _, err := os.Stat(filepath.Join(candidate, "kizu.toml")); err == nil {
			return candidate, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// stdlibFiles returns std manifest and Kizu source files in deterministic order.
func stdlibFiles(root string) ([]string, error) {
	files := []string{filepath.Join(root, "kizu.toml")}
	sourceRoot := filepath.Join(root, "src")
	err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".kizu" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return files, err
}

// hashFile returns the hex SHA-256 hash for one file.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

// hashBytes returns the hex SHA-256 hash for bytes.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// hashStrings returns the hex SHA-256 hash for ordered strings.
func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
