package ir

import (
	"errors"
	"testing"

	"tiny-safe/internal/lexer"
	"tiny-safe/internal/ownership"
	"tiny-safe/internal/parser"
	"tiny-safe/internal/types"
)

// TestDumpSnapshots checks stable typed SSA IR dumps.
func TestDumpSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "hello", source: `fn main() { print("hello, kizu") }`, want: helloSnapshot},
		{name: "functions", source: functionsSource, want: functionsSnapshot},
		{name: "variables", source: variablesSource, want: variablesSnapshot},
		{name: "if", source: ifSource, want: ifSnapshot},
		{name: "while", source: whileSource, want: whileSnapshot},
		{name: "arena", source: arenaSource, want: arenaSnapshot},
		{name: "comptime", source: comptimeSource, want: comptimeSnapshot},
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
	c1 := Value{Name: "%1", Type: "int"}
	c2 := Value{Name: "%2", Type: "int"}
	sum := Value{Name: "%3", Type: "int"}
	copyValue := Value{Name: "%4", Type: "int"}
	dead := Value{Name: "%5", Type: "int"}
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
  %3: int = const 3
  return %3: int
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

const functionsSource = `fn add(a: int, b: int) -> int {
    return a + b
}
fn main() {
    print(add(1, 2))
}`

const variablesSource = `fn main() {
    let name = "alice"
    var age = 30
    age = age + 1
    print(name)
    print(age)
}`

const ifSource = `fn main() {
    let age = 20
    if age >= 20 { print("adult") } else { print("minor") }
}`

const whileSource = `fn main() {
    var i = 0
    while i < 3 {
        print(i)
        i = i + 1
    }
}`

const arenaSource = `struct User {
    name: string
}
fn main() {
    let users = arena<User>()
    let alice = users.add(User { name: "alice" })
    print(users.get(alice).name)
}`

const comptimeSource = `fn main() {
    let size = comptime 4 * 1024
    comptime if 1 + 1 == 2 {
        print(size)
    } else {
        print(0)
    }
}`

const helloSnapshot = `fn main() -> void {
entry:
  %1: string = const "hello, kizu"
  call.print %1: string
  return void: void
}`

const functionsSnapshot = `fn add(%a: int, %b: int) -> int {
entry:
  %1: int = binary.+ %a: int, %b: int
  return %1: int
}
fn main() -> void {
entry:
  %1: int = const 1
  %2: int = const 2
  %3: int = call.add %1: int, %2: int
  call.print %3: int
  return void: void
}`

const variablesSnapshot = `fn main() -> void {
entry:
  %1: string = const "alice"
  %2: int = const 30
  %3: int = const 1
  %4: int = binary.+ %2: int, %3: int
  call.print %1: string
  call.print %4: int
  return void: void
}`

const ifSnapshot = `fn main() -> void {
entry:
  %1: int = const 20
  %2: int = const 20
  %3: bool = binary.>= %1: int, %2: int
  branch %3: bool, if.then.1, if.else.2
if.then.1:
  %4: string = const "adult"
  call.print %4: string
  jump if.end.3
if.else.2:
  %6: string = const "minor"
  call.print %6: string
  jump if.end.3
if.end.3:
  return void: void
}`

const whileSnapshot = `fn main() -> void {
entry:
  %1: int = const 0
  jump while.header.1
while.header.1:
  %2: int = phi [entry, %1: int], [while.body.2, %7: int]
  %3: int = const 3
  %4: bool = binary.< %2: int, %3: int
  branch %4: bool, while.body.2, while.end.3
while.body.2:
  call.print %2: int
  %6: int = const 1
  %7: int = binary.+ %2: int, %6: int
  jump while.header.1
while.end.3:
  return void: void
}`

const arenaSnapshot = `fn main() -> void {
entry:
  %1: arena<User> = arena.new User
  %2: string = const "alice"
  %3: User = struct.new {name: %2: string}
  %4: handle<User> = arena.add %1: arena<User>, %3: User
  %5: User = arena.get %1: arena<User>, %4: handle<User>
  %6: string = field.name %5: User
  call.print %6: string
  return void: void
}`

const comptimeSnapshot = `fn main() -> void {
entry:
  %1: int = const 4
  %2: int = const 1024
  %3: int = binary.* %1: int, %2: int
  call.print %3: int
  return void: void
}`
