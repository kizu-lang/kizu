package ast

// MetaVariant is one variant of an enum or union: the source name it is
// written under, and whether it carries a payload. Every enum tag and a
// tag-only union variant has no payload (SPEC §6.7, §6.8).
type MetaVariant struct {
	Name       string
	HasPayload bool
	// Origin is the error set that declares a member of an error set, and ""
	// for an enum tag or a union variant. A member keeps the set that declared
	// it (SPEC §11.2), so the arm names that set the way a written arm would.
	Origin string
}

// ComptimeMatchExpansion is the code `comptime match value |v, p| { ... }`
// stands for: an ordinary match with one arm per variant, in declaration
// order, binding the payload wherever there is one.
//
//	match value {
//	    Point => { body },
//	    Circle(p) => { body },
//	}
//
// The body is the same block in every arm; each phase walks it once per arm
// with `v` bound to that arm's variant. No dispatch rule is added by this:
// what the checkers and lowering read is a match, so exhaustiveness, payload
// binding, and the ownership of a borrowed payload are the rules match already
// has. Every phase expands through here, so none of them can disagree about
// which arms exist.
func ComptimeMatchExpansion(
	stmt *ComptimeMatchStmt,
	owner string,
	variants []MetaVariant,
) *MatchStmt {
	arms := make([]MatchArm, 0, len(variants))
	for _, variant := range variants {
		binding := ""
		if variant.HasPayload {
			binding = stmt.Binding
		}
		arms = append(arms, MatchArm{
			Tag: variant.Name, TagSet: variant.Origin, Binding: binding, Body: stmt.Body,
		})
	}
	return &MatchStmt{
		Value:       stmt.Value,
		Arms:        arms,
		MetaCapture: stmt.Name,
		MetaOwner:   owner,
	}
}

// VariantExpansion is the value `std::meta::variant<T, v>(payload)` stands
// for: the `T::v(payload)` a program writes by hand, or `T::v` for a variant
// that carries no payload.
func VariantExpansion(owner string, variant string, args []Expression) Expression {
	path := &FieldExpr{
		Receiver:  &IdentExpr{Name: owner},
		Name:      variant,
		Namespace: true,
	}
	if len(args) == 0 {
		return path
	}
	return &CallExpr{Callee: path, Args: args}
}
