package stdmethod

import "github.com/kizu-lang/kizu/internal/typ"

// MethodName is what a method is filed under: the type it is a method on and the
// name a call spells. This and SplitMethodName are the one place that pairing is
// written down.
func MethodName(receiver string, name string) string {
	return receiver + "." + name
}

// SplitMethodName separates a method's name into the type it is a method on and
// the name a call spells.
func SplitMethodName(name string) (string, string, bool) {
	return typ.SplitMethodName(name)
}

// CallName returns the name a call spells for a declared function. A method is
// filed under its receiver's type, and a call names only the part after it.
func CallName(name string) string {
	if _, called, ok := SplitMethodName(name); ok {
		return called
	}
	return name
}
