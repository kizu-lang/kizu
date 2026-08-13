package ir

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/typ"
)

// borrowStorageSource writes through a `&var` parameter, so the caller has to
// lend the local rather than a copy of it.
const borrowStorageSource = `
struct User {
    name: []u8,
}

fn rename(user: &var User) -> void {
    user.name = "bob";
}

fn main() -> void {
    var user = User { name: "alice" };
    rename(user);
    print(user.name);
}
`

// TestLentLocalGetsStorage keeps the two readings of one decision together. The
// callee receiving the caller's storage and the caller having storage to lend
// are the same fact, and when they came apart the write landed in a copy and
// the program printed the value it started with.
func TestLentLocalGetsStorage(t *testing.T) {
	module := lowerSource(t, borrowStorageSource)
	dump := Dump(module)
	if !strings.Contains(dump, "fn rename(%user: &var User)") {
		t.Fatalf("callee does not take the caller's storage:\n%s", dump)
	}
	for _, want := range []string{
		"= local.slot",         // the local lives in memory
		"call.rename %3: &var", // its address goes to the callee
		"= ref.load",           // and the caller re-reads after the call
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("caller does not `%s`:\n%s", want, dump)
		}
	}
}

// TestLowerParamAgreesWithItself checks the one place that decides both the type
// a callee sees and how the call hands the parameter over. A type that says
// borrow while the passing says value, or the other way round, is the drift this
// pairing exists to prevent: the slot analysis reads the passing, so a parameter
// lent without saying so leaves the caller with nothing to lend.
func TestLowerParamAgreesWithItself(t *testing.T) {
	l := newLowerer(&ast.Program{})
	l.module.Unions["Shape"] = Union{Name: "Shape"}
	for _, tc := range []struct {
		param   ast.Param
		want    string
		passing Passing
	}{
		{param: param("User", false, false), want: "User", passing: PassValue},
		{param: param("User", true, false), want: "User", passing: PassValue},
		{param: param("User", false, true), want: "&var User", passing: PassCallerStorage},
		{param: param("Shape", false, false), want: "Shape", passing: PassValue},
		{param: param("Shape", true, false), want: "&Shape", passing: PassCopyAddress},
		{param: param("Shape", false, true), want: "&var Shape", passing: PassCallerStorage},
	} {
		got := l.lowerParam(tc.param)
		if got.Type != tc.want || got.Passing != tc.passing {
			t.Fatalf("lowerParam(%s) = %+v, want %q passing %d",
				tc.param.TypeName, got, tc.want, tc.passing)
		}
		if isMutableReferenceType(got.Type) != (got.Passing == PassCallerStorage) {
			t.Fatalf("lowerParam(%s) = %+v: type and passing disagree",
				tc.param.TypeName, got)
		}
		if isReferenceType(got.Type) != (got.Passing != PassValue) {
			t.Fatalf("lowerParam(%s) = %+v: type and passing disagree",
				tc.param.TypeName, got)
		}
	}
}

// param builds one parameter declaration for the table above.
func param(typeName string, borrow bool, mutBorrow bool) ast.Param {
	parsed, err := typ.Parse(typeName)
	if err != nil {
		panic(err)
	}
	return ast.Param{Name: "value", TypeName: parsed, Borrow: borrow, MutBorrow: mutBorrow}
}
