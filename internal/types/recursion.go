package types

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// checkInlineRecursion refuses a struct or union that holds itself by value,
// directly or through other structs and unions: its size would have no end.
// A value held through `std::mem::Box<T>`, an Array, a view, or a pointer is
// behind a fixed-size handle and ends the chain; a stack buffer, an optional,
// and an error union hold their element inline and continue it.
func (c *Checker) checkInlineRecursion(program *ast.Program) error {
	for _, decl := range program.Decls {
		name := ""
		switch d := decl.(type) {
		case *ast.StructDecl:
			if len(d.TypeParams) == 0 {
				name = d.Name
			}
		case *ast.UnionDecl:
			if len(d.TypeParams) == 0 {
				name = d.Name
			}
		}
		if name == "" {
			continue
		}
		if path := c.inlinePathTo(name, name, map[string]bool{}); path != nil {
			return errorAt(decl.DeclarationSpan(),
				"type error: `%s` holds itself by value (%s), so its size has no end\n"+
					"help: hold the inner `%s` through `std::mem::Box<%s>` or an arena handle",
				name, strings.Join(path, ", then "), name, name)
		}
	}
	return nil
}

// inlinePathTo answers the fields and payloads that lead from `from` to
// `target` by value, or nil when none does. `seen` stops a chain that loops
// without reaching target.
func (c *Checker) inlinePathTo(from, target string, seen map[string]bool) []string {
	if seen[from] {
		return nil
	}
	seen[from] = true
	for _, part := range c.inlineParts(from) {
		inner, ok := inlineTypeName(part.typ)
		if !ok {
			continue
		}
		step := part.name + ": " + typ.Text(part.typ)
		if inner == target {
			return []string{step}
		}
		if rest := c.inlinePathTo(inner, target, seen); rest != nil {
			return append([]string{step}, rest...)
		}
	}
	return nil
}

// inlinePart is one field of a struct or payload of a union variant.
type inlinePart struct {
	name string
	typ  typ.Type
}

// inlineParts lists what a struct or union stores, in declaration order.
func (c *Checker) inlineParts(name string) []inlinePart {
	var parts []inlinePart
	if decl, ok := c.structs[name]; ok && len(decl.TypeParams) == 0 {
		for _, field := range decl.Fields {
			label := "field `" + name + "." + field.Name + "`"
			parts = append(parts, inlinePart{name: label, typ: field.TypeName})
		}
	}
	if union, ok := c.unions[name]; ok && len(union.typeParams) == 0 {
		for _, variant := range union.order {
			payload, err := typ.Parse(string(union.variants[variant]))
			if err != nil || payload == nil {
				continue
			}
			label := "payload `" + name + "::" + variant + "`"
			parts = append(parts, inlinePart{name: label, typ: payload})
		}
	}
	return parts
}

// inlineTypeName answers the declared type a field type stores by value, if
// any: through stack buffers, optionals, and error unions, but not through a
// view, borrow, function, or generic application such as Box or Array.
func inlineTypeName(t typ.Type) (string, bool) {
	switch node := t.(type) {
	case *typ.Name:
		if len(node.Args) > 0 {
			return "", false
		}
		return strings.Join(node.Path, "::"), true
	case *typ.Buffer:
		return inlineTypeName(node.Elem)
	case *typ.Optional:
		return inlineTypeName(node.Elem)
	case *typ.ErrorUnion:
		return inlineTypeName(node.Ok)
	default:
		return "", false
	}
}
