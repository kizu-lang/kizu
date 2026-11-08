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
//
// One pass over the by-value graph finds which types sit on a cycle; only
// the first such declaration walks again to spell the path it reports.
func (c *Checker) checkInlineRecursion(program *ast.Program) error {
	cycles := inlineCycles{
		checker: c,
		index:   map[string]int{},
		low:     map[string]int{},
		onStack: map[string]bool{},
		cyclic:  map[string]bool{},
	}
	for _, decl := range program.Decls {
		name := inlineDeclName(decl)
		if name == "" {
			continue
		}
		if _, visited := cycles.index[name]; !visited {
			cycles.visit(name)
		}
	}
	for _, decl := range program.Decls {
		name := inlineDeclName(decl)
		if name == "" || !cycles.cyclic[name] {
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

// inlineDeclName answers the name of a non-generic struct or union decl, or
// "" for any other decl.
func inlineDeclName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.StructDecl:
		if len(d.TypeParams) == 0 {
			return d.Name
		}
	case *ast.UnionDecl:
		if len(d.TypeParams) == 0 {
			return d.Name
		}
	}
	return ""
}

// inlineCycles is Tarjan's strongly connected components over the by-value
// graph: a type is cyclic when its component has more than one member or it
// holds itself directly.
type inlineCycles struct {
	checker *Checker
	next    int
	index   map[string]int
	low     map[string]int
	onStack map[string]bool
	stack   []string
	cyclic  map[string]bool
}

// visit numbers name and everything it reaches by value, closing each
// component as its root finishes.
func (g *inlineCycles) visit(name string) {
	g.index[name] = g.next
	g.low[name] = g.next
	g.next++
	g.stack = append(g.stack, name)
	g.onStack[name] = true
	selfLoop := false
	for _, part := range g.checker.inlineParts(name) {
		inner, ok := inlineTypeName(part.typ)
		if !ok {
			continue
		}
		if inner == name {
			selfLoop = true
		}
		if _, visited := g.index[inner]; !visited {
			g.visit(inner)
			g.low[name] = min(g.low[name], g.low[inner])
		} else if g.onStack[inner] {
			g.low[name] = min(g.low[name], g.index[inner])
		}
	}
	if g.low[name] != g.index[name] {
		return
	}
	start := len(g.stack) - 1
	for g.stack[start] != name {
		start--
	}
	component := g.stack[start:]
	for _, member := range component {
		g.onStack[member] = false
		if len(component) > 1 || selfLoop {
			g.cyclic[member] = true
		}
	}
	g.stack = g.stack[:start]
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
		if inner == target {
			return []string{part.step()}
		}
		if rest := c.inlinePathTo(inner, target, seen); rest != nil {
			return append([]string{part.step()}, rest...)
		}
	}
	return nil
}

// inlinePart is one field of a struct or payload of a union variant.
type inlinePart struct {
	owner   string
	member  string
	payload bool
	typ     typ.Type
}

// step spells the part as one link of the reported path.
func (p inlinePart) step() string {
	if p.payload {
		return "payload `" + p.owner + "::" + p.member + "`: " + typ.Text(p.typ)
	}
	return "field `" + p.owner + "." + p.member + "`: " + typ.Text(p.typ)
}

// inlineParts lists what a struct or union stores, in declaration order.
func (c *Checker) inlineParts(name string) []inlinePart {
	var parts []inlinePart
	if decl, ok := c.structs[name]; ok && len(decl.TypeParams) == 0 {
		for _, field := range decl.Fields {
			parts = append(parts, inlinePart{owner: name, member: field.Name, typ: field.TypeName})
		}
	}
	if union, ok := c.unions[name]; ok && len(union.typeParams) == 0 {
		for _, variant := range union.order {
			payload, err := typ.Parse(string(union.variants[variant]))
			if err != nil || payload == nil {
				continue
			}
			parts = append(parts, inlinePart{owner: name, member: variant, payload: true, typ: payload})
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
