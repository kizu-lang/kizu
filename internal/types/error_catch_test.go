package types

import "testing"

// declaredSets is the error set fixture the catch and composition tests share:
// two declaring sets whose member names collide on NotFound, their union, and
// an alias.
const declaredSets = `error FsError { NotFound, Denied }
error JsonError { Truncated, NotFound }
error CacheError = FsError or JsonError;
error FsAlias = FsError;
fn read(ok: bool) -> FsError!i64 {
    if ok { return 1; }
    return FsError::NotFound;
}
`

// TestCheckCatchAndErrorCaptureForms accepts the SPEC §11.1 consumption
// forms: the catch default, the catch guard, and the statement capture with
// an exhaustive match over the bound member.
func TestCheckCatchAndErrorCaptureForms(t *testing.T) {
	source := declaredSets + `fn ping(ok: bool) -> FsError!void {
    if ok { return; }
    return FsError::Denied;
}
fn main() -> void {
    let port = read(false) catch 8080;
    print(port);
    let guarded = read(false) catch return;
    print(guarded);
    if read(true) |v| {
        print(v);
    } else |err| {
        match err {
            NotFound => print(-1),
            Denied => print(-2),
        }
    }
    if ping(false) {
        print(1);
    } else |err| {
        match err {
            _ => print(-3),
        }
    }
    return;
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckCombinedSetPropagation accepts `try` from a declaring set and from
// an alias inside a combined-set function: the propagation check is a member
// subset check, not a name comparison (ADR-0127).
func TestCheckCombinedSetPropagation(t *testing.T) {
	source := declaredSets + `fn load() -> CacheError!i64 {
    let v = try read(true);
    return v;
}
fn alias_read() -> FsAlias!i64 {
    let v = try read(true);
    return v;
}
fn relay() -> CacheError!i64 {
    let v = try alias_read();
    return v;
}
fn fail() -> CacheError!i64 {
    return JsonError::Truncated;
}
fn main() -> void { return; }`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckCombinedSetMatchQualifiesCollisions pins the collision rule: a
// member two declaring sets contribute is matched by its qualified spelling,
// and the qualified arms count toward exhaustiveness separately.
func TestCheckCombinedSetMatchQualifiesCollisions(t *testing.T) {
	source := declaredSets + `fn load() -> CacheError!i64 {
    let v = try read(true);
    return v;
}
fn main() -> void {
    if load() |v| {
        print(v);
    } else |err| {
        match err {
            FsError::NotFound => print(-1),
            Denied => print(-2),
            Truncated => print(-3),
            JsonError::NotFound => print(-4),
        }
    }
}`
	if err := checkSource(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// catchDiagnosticCases fixes the messages the SPEC §11.1 / §11.2 rules fail
// with, so a reworded rule shows up as a diff here.
var catchDiagnosticCases = []struct {
	name   string
	source string
	want   string
}{
	{
		name: "error union if requires else err",
		source: declaredSets + `fn main() -> void {
    if read(true) |v| {
        print(v);
    }
}`,
		want: "if on FsError!i64 requires `else |err|`",
	},
	{
		name: "error union if requires else err even with a bare else",
		source: declaredSets + `fn main() -> void {
    if read(true) |v| {
        print(v);
    } else {
        print(-1);
    }
}`,
		want: "if on FsError!i64 requires `else |err|`",
	},
	{
		name: "catch rejects a bare error union",
		source: declaredSets + `fn bare() -> !i64 { return 1; }
fn main() -> void {
    let v = bare() catch -1;
    print(v);
}`,
		want: "`catch` requires a declared error set; `!i64` propagates with `try`",
	},
	{
		name: "error capture rejects a bare error union",
		source: declaredSets + `fn bare() -> !i64 { return 1; }
fn main() -> void {
    if bare() |v| {
        print(v);
    } else |err| {
        print(-1);
    }
}`,
		want: "a `!T` without a declared error set propagates with `try`",
	},
	{
		name: "catch rejects a non union operand",
		source: declaredSets + `fn main() -> void {
    let v = 1 catch -1;
    print(v);
}`,
		want: "`catch` expects an error union left operand, got i64",
	},
	{
		name: "try rejects a non subset set",
		source: declaredSets + `error NetError { Timeout }
fn fetch() -> NetError!i64 { return 1; }
fn load() -> CacheError!i64 {
    let v = try fetch();
    return v;
}
fn main() -> void { return; }`,
		want: "try cannot propagate NetError from NetError!i64",
	},
	{
		name: "return rejects a non subset member",
		source: declaredSets + `error NetError { Timeout }
fn load() -> CacheError!i64 {
    return NetError::Timeout;
}
fn main() -> void { return; }`,
		want: "return expects CacheError!i64, got NetError",
	},
	{
		name: "bare arm collision must qualify",
		source: declaredSets + `fn load() -> CacheError!i64 {
    let v = try read(true);
    return v;
}
fn main() -> void {
    if load() |v| {
        print(v);
    } else |err| {
        match err {
            NotFound => print(-1),
            _ => print(-2),
        }
    }
}`,
		want: "`NotFound` reaches `CacheError` from more than one set;" +
			" write `FsError::NotFound` or `JsonError::NotFound`",
	},
	{
		name: "error match stays exhaustive",
		source: declaredSets + `fn main() -> void {
    if read(true) |v| {
        print(v);
    } else |err| {
        match err {
            NotFound => print(-1),
        }
    }
}`,
		want: "match on `FsError` is not exhaustive: missing Denied",
	},
	{
		name: "combined set names no members",
		source: declaredSets + `fn main() -> void {
    print(CacheError::NotFound);
}`,
		want: "`CacheError` is a combined set and declares no members",
	},
	{
		name: "combined set rejects an unknown part",
		source: `error CacheError = FsError or Ghost;
error FsError { NotFound }
fn main() -> void { return; }`,
		want: "combines `Ghost`, which is not a declared error set",
	},
	{
		name: "combined set rejects itself",
		source: `error Loop = Loop;
fn main() -> void { return; }`,
		want: "error set `Loop` is combined from itself",
	},
	{
		name: "void success takes no capture",
		source: declaredSets + `fn ping() -> FsError!void { return; }
fn main() -> void {
    if ping() |v| {
        print(v);
    } else |err| {
        match err { _ => print(-1), }
    }
}`,
		want: "FsError!void has no success payload to capture `|v|`",
	},
	{
		name: "non void success requires a capture",
		source: declaredSets + `fn main() -> void {
    if read(true) {
        print(1);
    } else |err| {
        match err { _ => print(-1), }
    }
}`,
		want: "if on FsError!i64 requires a success capture `|name|`",
	},
	{
		name: "else err requires an error union",
		source: declaredSets + `fn main() -> void {
    if true {
        print(1);
    } else |err| {
        print(-1);
    }
}`,
		want: "`else |err|` requires an error union condition, got bool",
	},
	{
		name: "qualified arm rejects an enum",
		source: `enum Color { Red, Blue }
fn main() -> void {
    let c = Color::Red;
    match c {
        Color::Red => print(1),
        Blue => print(2),
    }
}`,
		want: "qualified match arm `Color::Red` requires an error set value",
	},
}

// TestCheckCatchDiagnostics runs the fixed diagnostic table.
func TestCheckCatchDiagnostics(t *testing.T) {
	runErrorCases(t, catchDiagnosticCases)
}
