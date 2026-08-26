package ast

import (
	"github.com/kizu-lang/kizu/internal/typ"
)

// ConstructField is one field a `std::meta::construct` expansion fills: the
// source name it is written under and the type its value has.
type ConstructField struct {
	Name string
	Type string
}

// ConstructExpansion is the code `std::meta::construct<T, worker>(args...)`
// stands for: one call to the worker per public field, an `errdefer` for each
// field that owns something, and the struct literal that consumes them all
// (ADR-0115).
//
//	let f1 = try worker<T, f1>(args...);
//	errdefer f1.deinit(args[0]);
//	let f2 = try worker<T, f2>(args...);
//	T { f1: move f1, f2: f2 }
//
// There is no place a half-built `T` could sit: the values are separate
// bindings until the literal takes all of them at once. Every phase expands
// through here, so the code the type checker reads, the code the ownership
// checker reads, and the code lowering emits cannot disagree.
//
// The returned statements run before the returned expression, whose type is
// `T`. Wrapping that in the form's `!T` result is the caller's job, because
// only the caller knows whether it is checking a type or emitting one.
func ConstructExpansion(
	owner string,
	worker string,
	fields []ConstructField,
	args []Expression,
	owners map[string]bool,
) ([]Statement, Expression) {
	statements := make([]Statement, 0, len(fields)*2)
	values := make([]FieldValue, 0, len(fields))
	// The release an owner field's errdefer writes names the allocator the
	// worker built it from, which is the construct's own first argument
	// (ADR-0132). The checker requires one there before an owner field can
	// reach here.
	release := []Expression{}
	if len(args) > 0 {
		release = []Expression{args[0]}
	}
	for _, field := range fields {
		binding := constructBinding(field.Name)
		statements = append(statements, &LetStmt{
			Name: binding,
			Value: &TryExpr{Value: &CallExpr{
				Callee: &TypeApplyExpr{
					Callee:  &IdentExpr{Name: worker},
					TypeArg: owner + ", " + field.Name,
				},
				Args: args,
			}},
		})
		// A value with no cleanup contract has nothing to release, and the
		// fields already built are what an `errdefer` protects: a later worker
		// that fails leaves them owned by nobody else. The release names the
		// allocator the worker built the field from, which is the construct's
		// own first argument (ADR-0132); the checker requires one there as
		// soon as any field owns something.
		value := Expression(&IdentExpr{Name: binding})
		if OwnerType(owners, field.Type) {
			statements = append(statements, &ErrDeferStmt{Expr: &CallExpr{
				Callee: &FieldExpr{
					Receiver: &IdentExpr{Name: binding},
					Name:     typ.CleanupMethod,
				},
				Args: release,
			}})
			// The literal takes the value out of its binding, which is the
			// same hand-off a program spells `move`. Writing the marker here
			// is what lets the ownership checker read the expansion as a move
			// rather than trust it.
			value = &MoveExpr{Value: value}
		}
		values = append(values, FieldValue{
			Name:  field.Name,
			Value: value,
		})
	}
	return statements, &StructLiteralExpr{TypeName: owner, Fields: values}
}

// constructBinding names the local one expanded field is bound to. The `$`
// keeps it out of reach of source, so an expansion cannot shadow or be
// shadowed by a name the caller wrote.
func constructBinding(field string) string {
	return "construct$" + field
}
