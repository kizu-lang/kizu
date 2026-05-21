package ir

import (
	"errors"
	"testing"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/parser"
	"github.com/kizu-lang/kizu/internal/types"
)

// TestDumpSnapshots checks stable typed SSA IR dumps.
func TestDumpSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "hello", source: `fn main() { print("hello, kizu"); }`, want: helloSnapshot},
		{name: "functions", source: functionsSource, want: functionsSnapshot},
		{name: "variables", source: variablesSource, want: variablesSnapshot},
		{name: "if", source: ifSource, want: ifSnapshot},
		{name: "while", source: whileSource, want: whileSnapshot},
		{name: "arena", source: arenaSource, want: arenaSnapshot},
		{name: "comptime", source: comptimeSource, want: comptimeSnapshot},
		{name: "cast", source: castSource, want: castSnapshot},
		{name: "error_union", source: errorUnionSource, want: errorUnionSnapshot},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			module := lowerSource(t, tt.source)
			got := Dump(module)
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestOptimizePasses checks constant folding, copy propagation, and DCE.
func TestOptimizePasses(t *testing.T) {
	module := &Module{Functions: []*Function{{Name: "main", Return: "void"}}}
	fn := module.Functions[0]
	block := &Block{Name: "entry"}
	fn.Blocks = append(fn.Blocks, block)
	c1 := Value{Name: "%1", Type: "i64"}
	c2 := Value{Name: "%2", Type: "i64"}
	sum := Value{Name: "%3", Type: "i64"}
	copyValue := Value{Name: "%4", Type: "i64"}
	dead := Value{Name: "%5", Type: "i64"}
	block.Instrs = []*Instr{
		{Result: c1, Op: "const", Immediate: "1"},
		{Result: c2, Op: "const", Immediate: "2"},
		{Result: sum, Op: "binary.+", Args: []Value{c1, c2}},
		{Result: copyValue, Op: "id", Args: []Value{sum}},
		{Result: dead, Op: "const", Immediate: "99"},
	}
	block.Terminator = Terminator{Op: "return", Value: copyValue}
	Optimize(module)
	got := Dump(module)
	want := `fn main() -> void {
entry:
  %3: i64 = const 3
  return %3: i64
}`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// lowerSource parses, checks, and lowers a source snippet.
func lowerSource(t *testing.T, source string) *Module {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	if err := types.New().Check(program); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	if err := ownership.New().Check(program); err != nil {
		t.Fatalf("ownership check failed: %v", err)
	}
	module, err := Lower(program)
	if err != nil {
		t.Fatalf("lower failed: %v", err)
	}
	if module == nil {
		t.Fatal(errors.New("nil module"))
	}
	return module
}

const functionsSource = `fn add(a: i64, b: i64) -> i64 {
    return a + b;
}
fn main() {
    print(add(1, 2));
}`

const variablesSource = `fn main() {
    let name = "alice";
    var age = 30;
    age = age + 1;
    print(name);
    print(age);
}`

const ifSource = `fn main() {
    let age = 20;
    if age >= 20 { print("adult"); } else { print("minor"); }
}`

const whileSource = `fn main() {
    var i = 0;
    while i < 3 {
        print(i);
        i = i + 1;
    }
}`

const arenaSource = `struct User {
    name: []const u8,
}
fn main(allocator: Allocator) {
    let users = std::arena::Arena<User>(allocator);
    let alice = users.add(User { name: "alice" });
    print(users.get(alice).name);
    users.deinit();
}`

const comptimeSource = `fn main() {
    let size = comptime 4 * 1024;
    comptime if 1 + 1 == 2 {
        print(size);
    } else {
        print(0);
    }
}`

const castSource = `fn main() {
    let x = cast<i32>(1);
    print(x);
}`

const errorUnionSource = `fn parse() -> !i64 {
    return 1;
}
fn main() -> !i64 {
    let value = try parse();
    return value;
}`

const helloSnapshot = `fn main() -> void {
entry:
  %1: []const u8 = const "hello, kizu"
  call.print %1: []const u8
  return void: void
}`

const functionsSnapshot = `fn add(%a: i64, %b: i64) -> i64 {
entry:
  %1: i64 = binary.+ %a: i64, %b: i64
  return %1: i64
}
fn main() -> void {
entry:
  %1: i64 = const 1
  %2: i64 = const 2
  %3: i64 = call.add %1: i64, %2: i64
  call.print %3: i64
  return void: void
}`

const variablesSnapshot = `fn main() -> void {
entry:
  %1: []const u8 = const "alice"
  %2: i64 = const 30
  %3: i64 = const 1
  %4: i64 = binary.+ %2: i64, %3: i64
  call.print %1: []const u8
  call.print %4: i64
  return void: void
}`

const ifSnapshot = `fn main() -> void {
entry:
  %1: i64 = const 20
  %2: i64 = const 20
  %3: bool = binary.>= %1: i64, %2: i64
  branch %3: bool, if.then.1, if.else.2
if.then.1:
  %4: []const u8 = const "adult"
  call.print %4: []const u8
  jump if.end.3
if.else.2:
  %6: []const u8 = const "minor"
  call.print %6: []const u8
  jump if.end.3
if.end.3:
  return void: void
}`

const whileSnapshot = `fn main() -> void {
entry:
  %1: i64 = const 0
  jump while.header.1
while.header.1:
  %2: i64 = phi [entry, %1: i64], [while.body.2, %7: i64]
  %3: i64 = const 3
  %4: bool = binary.< %2: i64, %3: i64
  branch %4: bool, while.body.2, while.end.3
while.body.2:
  call.print %2: i64
  %6: i64 = const 1
  %7: i64 = binary.+ %2: i64, %6: i64
  jump while.header.1
while.end.3:
  return void: void
}`

const arenaSnapshot = `fn main(%allocator: Allocator) -> void {
entry:
  %1: std::arena::Arena<User> = arena.new %allocator: Allocator, User
  %2: []const u8 = const "alice"
  %3: User = struct.new {name: %2: []const u8}
  %4: std::arena::Handle<User> = arena.add %1: std::arena::Arena<User>, %3: User
  %5: User = arena.get %1: std::arena::Arena<User>, %4: std::arena::Handle<User>
  %6: []const u8 = field.name %5: User
  call.print %6: []const u8
  arena.deinit %1: std::arena::Arena<User>
  return void: void
}`

const comptimeSnapshot = `fn main() -> void {
entry:
  %1: i64 = const 4
  %2: i64 = const 1024
  %3: i64 = binary.* %1: i64, %2: i64
  call.print %3: i64
  return void: void
}`

const castSnapshot = `fn main() -> void {
entry:
  %1: i64 = const 1
  %2: i32 = cast %1: i64, i32
  call.print %2: i32
  return void: void
}`

const errorUnionSnapshot = `fn parse() -> !i64 {
entry:
  %1: i64 = const 1
  return %1: i64
}
fn main() -> !i64 {
entry:
  %1: !i64 = call.parse
  %2: i64 = error.try %1: !i64
  return %2: i64
}`
