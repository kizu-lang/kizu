package project

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/stdlib"
	"github.com/kizu-lang/kizu/internal/typ"
)

// resolveTypeNamespaceReceiver resolves the receiver of Type::Tag namespace lookups.
func (c *graphChecker) resolveTypeNamespaceReceiver(
	module *moduleFile,
	expr *ast.FieldExpr,
) (string, bool) {
	parts, ok := namespaceParts(expr.Receiver)
	if !ok {
		return "", false
	}
	return c.resolveTypeNamespaceParts(module, parts)
}

// resolveNamespacePath resolves a complete module-qualified namespace chain.
func (c *graphChecker) resolveNamespacePath(
	module *moduleFile,
	expr *ast.FieldExpr,
) (string, bool, error) {
	parts, ok := namespaceParts(expr)
	if !ok {
		return "", false, nil
	}
	return c.resolveNamespaceParts(module, parts)
}

// resolveNamespaceParts resolves local or imported function namespace parts.
func (c *graphChecker) resolveNamespaceParts(
	module *moduleFile,
	parts []string,
) (string, bool, error) {
	if len(parts) == 0 {
		return "", false, nil
	}
	if target, ok := module.namespaces[parts[0]]; ok {
		if len(parts) == 1 {
			return "", false, nil
		}
		module.use(parts[0])
		name := target + "::" + strings.Join(parts[1:], "::")
		if !ReachableFrom(name, module.path) {
			return "", false, internalModuleError(name, module)
		}
		if isStdPath(target) {
			return c.resolveStdFunction(module, name, parts)
		}
		return c.resolveImportedFunction(module, target, parts)
	}
	if err := rejectUnboundStd(module, parts); err != nil {
		return "", false, err
	}
	name := module.qualify(strings.Join(parts, "::"))
	if _, ok := c.functions[name]; ok {
		return name, true, nil
	}
	return "", false, nil
}

// rejectUnboundStd refuses a std path the file never brought into scope. It
// speaks only for paths that name a std module: a chain that names nothing there
// is somebody else's error, and saying `import it` would name a fix that does
// not exist.
func rejectUnboundStd(module *moduleFile, parts []string) error {
	if len(parts) < 2 || parts[0] != stdlib.Root {
		return nil
	}
	if _, bound := module.namespaces[parts[0]]; bound {
		return nil
	}
	path, size, err := stdModulePrefix(parts)
	if err != nil || size == 0 {
		return err
	}
	if !ReachableFrom(path, module.path) {
		return internalModuleError(path, module)
	}
	used := strings.Join(parts, "::")
	short := strings.Join(parts[size-1:], "::")
	return fmt.Errorf(
		"module error: `%s` is used without an import in `%s`"+
			"\nhelp: `import %s;` reaches it by this path,"+
			" or `import %s;` and write `%s`",
		used, module.name(), stdlib.Root, path, short,
	)
}

// stdModulePrefix returns the longest std module path that prefixes parts, and
// how many segments it spans. A path names the deepest module it can, so
// `std::path::internal::bits::is_slash` is an item of `std::path::internal::bits`
// rather than of `std::path`.
func stdModulePrefix(parts []string) (string, int, error) {
	for size := len(parts); size >= 2; size-- {
		path := strings.Join(parts[:size], "::")
		ok, err := stdModule(path)
		if err != nil {
			return "", 0, err
		}
		if ok {
			return path, size, nil
		}
	}
	return "", 0, nil
}

// internalModuleError refuses a module the reading file may not reach. What
// keeps it in is where it sits, so the message says where that is rather than
// naming a list it is missing from.
func internalModuleError(path string, module *moduleFile) error {
	internal, scope, _ := internalModule(path)
	return fmt.Errorf("module error: `%s` is internal to `%s` and is not reachable from `%s`",
		internal, scope, module.name())
}

// isStdPath reports whether a bound name stands for the standard library. A
// user package may not be named `std`, so the prefix decides it.
func isStdPath(path string) bool {
	return path == stdlib.Root || strings.HasPrefix(path, stdlib.Root+"::")
}

// resolveStdFunction validates visibility for a call into std. A name std does
// not declare is one the compiler provides itself -- `std::arena::Arena` has no
// Kizu source -- so it passes through to the checker that knows about it.
func (c *graphChecker) resolveStdFunction(
	module *moduleFile,
	name string,
	parts []string,
) (string, bool, error) {
	exported, ok := c.functions[name]
	if ok && exported.module != module.path && !exported.public {
		return "", false, fmt.Errorf("module error: function `%s` is private",
			strings.Join(parts, "::"))
	}
	return name, true, nil
}

// resolveImportedFunction validates visibility for a call through an import alias.
func (c *graphChecker) resolveImportedFunction(
	module *moduleFile,
	target string,
	parts []string,
) (string, bool, error) {
	name := target + "::" + strings.Join(parts[1:], "::")
	exported, ok := c.functions[name]
	if !ok {
		sourceName := strings.Join(parts, "::")
		return "", false, fmt.Errorf("module error: unknown function `%s`", sourceName)
	}
	if exported.module != module.path && !exported.public {
		sourceName := strings.Join(parts, "::")
		return "", false, fmt.Errorf("module error: function `%s` is private", sourceName)
	}
	return name, true, nil
}

// resolveTypeNamespaceParts resolves namespace parts only when they name a type.
func (c *graphChecker) resolveTypeNamespaceParts(
	module *moduleFile,
	parts []string,
) (string, bool) {
	if len(parts) == 0 {
		return "", false
	}
	if target, ok := module.namespaces[parts[0]]; ok && len(parts) > 1 {
		module.use(parts[0])
		name := target + "::" + strings.Join(parts[1:], "::")
		if !ReachableFrom(name, module.path) {
			// resolveNamespaceParts says why; this one has no way to report it.
			return "", false
		}
		if isStdPath(target) {
			return name, true
		}
		if _, exists := c.types[name]; exists {
			return name, true
		}
	}
	name := module.qualify(strings.Join(parts, "::"))
	if _, ok := c.types[name]; ok {
		return name, true
	}
	return "", false
}

// namespaceParts returns identifier segments from a namespace expression.
func namespaceParts(expr ast.Expression) ([]string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return []string{e.Name}, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return nil, false
		}
		parts, ok := namespaceParts(e.Receiver)
		if !ok {
			return nil, false
		}
		return append(parts, e.Name), true
	default:
		return nil, false
	}
}

// resolveType rewrites a source type name into its package-qualified form.
func (c *graphChecker) resolveType(module *moduleFile, name string) (string, error) {
	parsed, err := c.parsedTypes.Parse(name)
	if err != nil {
		return "", fmt.Errorf("module error: unknown type `%s`", name)
	}
	resolved, err := c.resolveTypeNode(module, parsed)
	if err != nil {
		return "", err
	}
	return typ.Text(resolved), nil
}

// resolveTypeNode rewrites every name a type mentions to its package-qualified
// form, keeping the structure around it.
func (c *graphChecker) resolveTypeNode(module *moduleFile, t typ.Type) (typ.Type, error) {
	resolver := typeResolver{checker: c, module: module}
	return typ.MapNames(t, func(path []string) ([]string, error) {
		resolved, err := resolver.resolveBase(strings.Join(path, "::"))
		if err != nil {
			return nil, err
		}
		return strings.Split(resolved, "::"), nil
	})
}

type typeResolver struct {
	checker *graphChecker
	module  *moduleFile
}

// resolveBase resolves one non-generic type base.
func (r typeResolver) resolveBase(name string) (string, error) {
	if strings.HasPrefix(name, stdlib.Root+"::") {
		r.module.use(stdlib.Root)
		if !ReachableFrom(name, r.module.path) {
			return "", internalModuleError(name, r.module)
		}
		return name, rejectUnboundStd(r.module, strings.Split(name, "::"))
	}
	if strings.Contains(name, "::") {
		return r.resolveQualifiedBase(name)
	}
	local := r.module.qualify(name)
	if _, ok := r.checker.types[local]; ok {
		return local, nil
	}
	if isPrimitiveType(name) {
		return name, nil
	}
	return name, nil
}

// resolveQualifiedBase resolves an imported module type by last-segment alias.
func (r typeResolver) resolveQualifiedBase(name string) (string, error) {
	parts := strings.Split(name, "::")
	targetModule, ok := r.module.namespaces[parts[0]]
	if ok {
		r.module.use(parts[0])
	}
	if !ok {
		return "", fmt.Errorf("module error: `%s` is not imported in `%s`", parts[0], r.module.path)
	}
	qualified := targetModule + "::" + strings.Join(parts[1:], "::")
	if !ReachableFrom(qualified, r.module.path) {
		return "", internalModuleError(qualified, r.module)
	}
	if isStdPath(targetModule) {
		return qualified, nil
	}
	exported, ok := r.checker.types[qualified]
	if !ok {
		return "", fmt.Errorf("module error: unknown type `%s`", name)
	}
	if exported.module != r.module.path && !exported.public {
		return "", fmt.Errorf("module error: type `%s` is private", name)
	}
	return qualified, nil
}

// isPrimitiveType reports source-level types that do not need module lookup.
func isPrimitiveType(name string) bool {
	switch name {
	case "bool", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"usize", "isize", "f32", "f64", "void", "Io", "Allocator", "Function", "Field",
		"type":
		return true
	default:
		return false
	}
}

// splitTypeArgs splits comma-separated static type arguments with nested angle support.
func splitTypeArgs(args string) ([]string, error) {
	parts, err := typ.SplitArgs(args)
	if err != nil {
		return nil, fmt.Errorf("module error: invalid static arguments `%s`", args)
	}
	return parts, nil
}

// sortedModuleGroups returns modules in deterministic path order.
func sortedModuleGroups(modules map[string]*moduleGroup) []*moduleGroup {
	paths := make([]string, 0, len(modules))
	for path := range modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]*moduleGroup, 0, len(paths))
	for _, path := range paths {
		out = append(out, modules[path])
	}
	return out
}

// sortedImportPaths returns imported module paths in deterministic order.
func sortedImportPaths(imports map[string]string) []string {
	paths := make([]string, 0, len(imports))
	for _, path := range imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
