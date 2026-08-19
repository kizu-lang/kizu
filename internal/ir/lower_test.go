package ir

import (
	"errors"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/typ"
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
	// The function returns what its block returns: Optimize verifies the module
	// it produced, and a fixture declaring otherwise is rejected before the
	// passes can be read.
	module := &Module{Functions: []*Function{{Name: "main", Return: "i64"}}}
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
	if err := Optimize(module); err != nil {
		t.Fatalf("optimize failed: %v", err)
	}
	got := Dump(module)
	want := `fn main() -> i64 {
entry:
  %3: i64 = const 3
  return %3: i64
}`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestOptimizeKeepsUnusedVolatileLoad pins the volatile access as an effect:
// a device register changes state when read, so DCE must keep a volatile.load
// nothing reads the result of.
func TestOptimizeKeepsUnusedVolatileLoad(t *testing.T) {
	module := &Module{Functions: []*Function{{Name: "main", Return: "void"}}}
	fn := module.Functions[0]
	block := &Block{Name: "entry"}
	fn.Blocks = append(fn.Blocks, block)
	pointer := Value{Name: "%1", Type: "ptr<u8>"}
	unused := Value{Name: "%2", Type: "u8"}
	block.Instrs = []*Instr{
		{Result: pointer, Op: "const", Immediate: "0"},
		{Result: unused, Op: "volatile.load", Args: []Value{pointer}},
	}
	block.Terminator = Terminator{Op: "return"}
	if err := Optimize(module); err != nil {
		t.Fatalf("optimize failed: %v", err)
	}
	kept := false
	for _, instr := range module.Functions[0].Blocks[0].Instrs {
		if instr.Op == "volatile.load" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("volatile.load was eliminated:\n%s", Dump(module))
	}
}

// TestOptimizeKeepsStructFieldAndCleanupOperandsLive pins struct field values and
// cleanup arguments as real uses: copy propagation has to rewrite them through the
// `id` copy, and DCE must keep a producer whose only readers sit in those two
// positions. Missing either one drops the allocation a struct still owns.
func TestOptimizeKeepsStructFieldAndCleanupOperandsLive(t *testing.T) {
	module := &Module{Functions: []*Function{{Name: "main", Return: "Holder"}}}
	fn := module.Functions[0]
	block := &Block{Name: "entry"}
	fn.Blocks = append(fn.Blocks, block)
	owned := Value{Name: "%1", Type: "std::array::Array<u8>"}
	copyValue := Value{Name: "%2", Type: "std::array::Array<u8>"}
	holder := Value{Name: "%3", Type: "Holder"}
	block.Instrs = []*Instr{
		{Result: owned, Op: "array.new"},
		{Result: copyValue, Op: "id", Args: []Value{owned}},
		{
			Result: holder,
			Op:     "struct.new",
			Fields: []FieldArg{{Name: "bytes", Value: copyValue}},
			Cleanups: []Cleanup{{
				Op:   "array.deinit",
				Args: []Value{copyValue},
			}},
		},
	}
	block.Terminator = Terminator{Op: "return", Value: holder}

	if err := Optimize(module); err != nil {
		t.Fatalf("optimize failed: %v", err)
	}

	if len(block.Instrs) != 2 {
		t.Fatalf("optimized instruction count = %d, want producer + struct", len(block.Instrs))
	}
	if got := block.Instrs[1].Fields[0].Value.Name; got != owned.Name {
		t.Fatalf("struct field copy propagation = %q, want %q", got, owned.Name)
	}
	if got := block.Instrs[1].Cleanups[0].Args[0].Name; got != owned.Name {
		t.Fatalf("cleanup copy propagation = %q, want %q", got, owned.Name)
	}
}

// TestLowerArrayPopOrPanicPreservesMoveAndTrapOperation fixes the IR boundary.
func TestLowerArrayPopOrPanicPreservesMoveAndTrapOperation(t *testing.T) {
	module := lowerSource(t, `fn take(values: std::array::Array<i64>) -> i64 {
    let value = values.pop_or_panic();
    values.deinit();
    return value;
}
fn main() {}`)
	got := Dump(module)
	if !strings.Contains(got, "array.pop_or_panic") {
		t.Fatalf("lowered IR missing array.pop_or_panic:\n%s", got)
	}
	if strings.Contains(got, "array.pop:") {
		t.Fatalf("trap variant collapsed to recoverable pop:\n%s", got)
	}
}

// TestLowerBorrowCaptureArgumentReadsValue hands borrow-capture bindings to
// `&T` parameters, which travel as the element value (borrowIRType): the call
// has to read the capture's reference, the way a slot-backed borrow is read.
// Lower verifies its module, so ill-typed call arguments fail here.
func TestLowerBorrowCaptureArgumentReadsValue(t *testing.T) {
	lowerSource(t, `struct Item { value: i64 }
fn read_it(x: &Item) -> i64 {
    return x.value;
}
fn main() {
    let allocator = std::mem::page_allocator();
    var xs = std::array::new<Item>(allocator);
    if xs.at(0) |r| {
        print(read_it(r));
    }
    if xs.at_mut(0) |m| {
        print(read_it(m));
    }
    xs.deinit();
}`)
	lowerSource(t, `union Shape { Dot, Bar(i64) }
fn read_shape(s: &Shape) -> i64 {
    return match s {
        Dot => 0,
        Bar(n) => n,
    };
}
fn main() {
    let allocator = std::mem::page_allocator();
    var xs = std::array::new<Shape>(allocator);
    if xs.at_mut(0) |m| {
        print(read_shape(m));
    }
    xs.deinit();
}`)
}

// TestLowerNamespaceQualifiedFunctionCall keeps std-style namespace calls from
// being lowered as field or method access on a local `std` value.
func TestLowerNamespaceQualifiedFunctionCall(t *testing.T) {
	program := &ast.Program{Decls: []ast.Decl{
		&ast.FunctionDecl{
			FunctionSignature: ast.FunctionSignature{
				Name: "std::mem::len",
				Params: []ast.Param{
					{Name: "bytes", TypeName: &typ.Slice{Elem: &typ.Name{Path: []string{"u8"}}}},
				},
				ReturnType: &typ.Name{Path: []string{"i64"}},
			},
			Body: &ast.BlockStmt{Statements: []ast.Statement{
				&ast.ReturnStmt{Value: &ast.IntExpr{Value: "1"}},
			}},
		},
		&ast.FunctionDecl{
			FunctionSignature: ast.FunctionSignature{
				Name:       "main",
				ReturnType: &typ.Name{Path: []string{"i64"}},
			},
			Body: &ast.BlockStmt{Statements: []ast.Statement{
				&ast.ReturnStmt{Value: &ast.CallExpr{
					Callee: &ast.FieldExpr{
						Receiver: &ast.FieldExpr{
							Receiver:  &ast.IdentExpr{Name: "std"},
							Name:      "mem",
							Namespace: true,
						},
						Name:      "len",
						Namespace: true,
					},
					Args: []ast.Expression{&ast.StringExpr{Value: "abc"}},
				}},
			}},
		},
	}}
	module, err := Lower(program)
	if err != nil {
		t.Fatalf("lower failed: %v", err)
	}
	got := Dump(module)
	want := "  %2: i64 = call.std::mem::len %1: []u8\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
	}
}

// TestLowerErrDeferRunsOnlyOnErrorReturn checks errdefer cleanup attaches to
// the try error path and an explicit error return, but is skipped on success.
func TestLowerErrDeferRunsOnlyOnErrorReturn(t *testing.T) {
	errorReturn := lowerSource(t, `error BuildError { Boom }
struct User { name: []u8 }
fn make() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    return BuildError::Boom;
}
fn main() {}`)
	dump := Dump(errorReturn)
	if !strings.Contains(dump, "arena.deinit") {
		t.Fatalf("error return must emit errdefer cleanup:\n%s", dump)
	}

	successReturn := lowerSource(t, `error BuildError { Boom }
struct User { name: []u8 }
fn make() -> !std::arena::Arena<User> {
    let allocator = std::mem::page_allocator();
    let users = std::arena::new<User>(allocator);
    errdefer users.deinit();
    return users;
}
fn main() {}`)
	if got := Dump(successReturn); strings.Contains(got, "arena.deinit") {
		t.Fatalf("success return must skip errdefer cleanup:\n%s", got)
	}
}

// TestLowerErrDeferRetiresAtMove checks the error paths after a move stop
// carrying the moved receiver's cleanup. From the move on the parent owns the
// child, and emitting both cleanups would free the same buffer twice
// (ADR-0114).
func TestLowerErrDeferRetiresAtMove(t *testing.T) {
	module := lowerSource(t, `
fn build(allocator: Allocator) -> !std::array::Array<std::string::String> {
    let parent = std::array::new<std::string::String>(allocator);
    errdefer parent.deinit_all();
    let child = std::string::new(allocator);
    errdefer child.deinit();
    try child.append_byte(cast<u8>(97));
    try parent.append(child);
    try parent.reserve(1);
    return parent;
}
fn main() {}`)
	got := tryCleanupReceivers(module, "build")
	want := [][]string{{"child", "parent"}, {"parent"}, {"parent"}}
	if len(got) != len(want) {
		t.Fatalf("got %d try exits, want %d:\n%s", len(got), len(want), Dump(module))
	}
	for index, receivers := range want {
		if strings.Join(got[index], ",") != strings.Join(receivers, ",") {
			t.Fatalf("try %d cleans up %v, want %v:\n%s",
				index, got[index], receivers, Dump(module))
		}
	}
}

// tryCleanupReceivers lists, in emission order, the receivers each try's error
// path cleans up in the named function.
func tryCleanupReceivers(module *Module, name string) [][]string {
	var receivers [][]string
	for _, fn := range module.Functions {
		if fn.Name != name {
			continue
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if instr.Op != "error.try" {
					continue
				}
				names := []string{}
				for _, cleanup := range instr.Cleanups {
					names = append(names, cleanup.Receiver)
				}
				receivers = append(receivers, names)
			}
		}
	}
	return receivers
}

// TestLowerElementBorrowMethodReceiver checks a read method called through a
// borrow optional's capture reads the receiver out of the element's storage.
// `array.at(i)` hands over a real address, and a method declaring `self: T`
// takes the value, so the load has to be there.
func TestLowerElementBorrowMethodReceiver(t *testing.T) {
	module := lowerSource(t, `
struct Point { pub x: i64 }
fn (self: Point) sum() -> i64 { return self.x; }
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var points = std::array::new<Point>(allocator);
    defer points.deinit();
    try points.append(Point { x: 7 });
    if points.at(0) |p| {
        print(p.sum());
    }
    return;
}`)
	got := Dump(module)
	for _, want := range []string{
		"  %9: &Point = opt.value %8: ?&Point\n",
		"  %11: Point = ref.load %9: &Point\n",
		"  %12: i64 = call.Point.sum %11: Point\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
		}
	}
}

// TestLowerByteSliceAccess emits explicit checked slice operations.
func TestLowerByteSliceAccess(t *testing.T) {
	module := lowerSource(t, `fn main() {
    let bytes = "hello";
    let byte = bytes[1];
    let part = bytes[1..4];
    print(byte);
    print(part);
}`)
	got := Dump(module)
	for _, want := range []string{
		"  cond_fail %4: bool, bounds(%2: i64, %3: i64)\n",
		"  cond_fail %5: bool, bounds(%2: i64, %3: i64)\n",
		"  %6: u8 = slice.index %1: []u8, %2: i64\n",
		"  cond_fail %12: bool, range(%7: i64, %8: i64, %9: i64)\n",
		"  %13: []u8 = slice.slice %1: []u8, %7: i64, %8: i64\n",
		"  call.print %6: u8\n",
		"  call.print %13: []u8\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
		}
	}
}

// TestLowerWhileContinueFeedsLoopPhis keeps assignments before nested continue
// edges visible to the next loop iteration.
func TestLowerWhileContinueFeedsLoopPhis(t *testing.T) {
	module := lowerSource(t, `fn main() {
    var index = 0;
    var in_string = false;
    while index < 3 {
        if in_string {
            in_string = false;
            index = index + 1;
            continue;
        }
        in_string = true;
        index = index + 1;
    }
    print(in_string);
}`)
	header := findTestBlock(t, module.Functions[0], "while.header.1")
	for _, want := range []Incoming{
		{Block: "if.end.6", Value: Value{Name: "%10", Type: "bool"}},
		{Block: "if.then.4", Value: Value{Name: "%7", Type: "bool"}},
	} {
		if !blockHasPhiIncoming(header, "bool", want) {
			t.Fatalf("missing bool phi incoming %#v in:\n%s", want, Dump(module))
		}
	}
}

// TestLowerWhileMatchAssignmentsFeedLoopPhis keeps loop-carried variables
// updated when the assignment happens inside a match arm.
func TestLowerWhileMatchAssignmentsFeedLoopPhis(t *testing.T) {
	module := lowerSource(t, `enum Step {
    Advance,
    Stop,
}

fn main() {
    var current = 0;
    let step = Step::Advance;
    while current < 2 {
        match step {
            Advance => current = current + 1;,
            Stop => current = 2;,
        }
    }
    print(current);
}`)
	header := findTestBlock(t, module.Functions[0], "while.header.1")
	if !blockHasPhiIncomingFrom(header, "i64", "match.end.4") {
		t.Fatalf("missing loop phi incoming from match end in:\n%s", Dump(module))
	}
}

// TestLowerExhaustiveMatchExpressionMissIsUnreachable keeps the implicit
// impossible miss edge out of the match value phi predecessors.
func TestLowerExhaustiveMatchExpressionMissIsUnreachable(t *testing.T) {
	module := lowerSource(t, `enum Choice {
    A,
    B,
}

fn choose(choice: Choice) -> i64 {
    return match choice {
        A => 1,
        B => 2,
    };
}`)
	fn := module.Functions[0]
	var end *Block
	for _, block := range fn.Blocks {
		if strings.HasPrefix(block.Name, "match.end") {
			end = block
			break
		}
	}
	if end == nil {
		t.Fatalf("missing match end block:\n%s", Dump(module))
	}
	foundUnreachable := false
	for _, block := range fn.Blocks {
		if strings.HasPrefix(block.Name, "match.unreachable") &&
			block.Terminator.Op == "unreachable" {
			foundUnreachable = true
		}
		if strings.HasPrefix(block.Name, "match.check") &&
			block.Terminator.Else == end.Name {
			t.Fatalf("match miss edge still targets value phi block:\n%s", Dump(module))
		}
	}
	if !foundUnreachable {
		t.Fatalf("missing unreachable match miss block:\n%s", Dump(module))
	}
}

// TestLowerWhileBreakAssignmentsFeedExitPhis keeps values assigned before an
// explicit break visible after the loop.
func TestLowerWhileBreakAssignmentsFeedExitPhis(t *testing.T) {
	module := lowerSource(t, `fn main() {
    var index = 0;
    var found = false;
    while index < 3 {
        if index == 1 {
            found = true;
            break;
        }
        index = index + 1;
    }
    print(found);
}`)
	exit := findTestBlock(t, module.Functions[0], "while.end.3")
	if !blockHasPhiIncomingFrom(exit, "bool", "if.then.4") {
		t.Fatalf("missing break phi incoming in:\n%s", Dump(module))
	}
}

// TestLowerMatchPayloadBindingStaysArmLocal keeps a payload binding out of the
// environment after its match, so a later match in the same function does not
// phi over a value only some of the first match's arms defined.
func TestLowerMatchPayloadBindingStaysArmLocal(t *testing.T) {
	module := lowerSource(t, `union Slot {
    Empty,
    A(i64),
    B(i64),
}

fn main() {
    var w = -1;
    let p = Slot::A(1);
    match p {
        A(x) => { w = x; },
        B(x) => { w = x; },
        Empty => { w = 0; },
    }
    let q = Slot::B(2);
    match q {
        A(x) => { w = w + x; },
        B(x) => { w = w + x; },
        Empty => { w = 0; },
    }
    print(w);
}`)
	if err := Verify(module); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

// TestLowerSkipsGenericDeclarations keeps the non-monomorphized backend from
// lowering unused generic wrapper bodies.
func TestLowerSkipsGenericDeclarations(t *testing.T) {
	program := &ast.Program{Decls: []ast.Decl{
		&ast.FunctionDecl{
			FunctionSignature: ast.FunctionSignature{
				Name:         "unused",
				StaticParams: []ast.StaticParam{{Name: "T"}},
				Params: []ast.Param{
					{Name: "value", TypeName: &typ.Name{Path: []string{"T"}}},
				},
				ReturnType: &typ.Name{Path: []string{"T"}},
			},
			Body: &ast.BlockStmt{Statements: []ast.Statement{
				&ast.ReturnStmt{Value: &ast.IdentExpr{Name: "value"}},
			}},
		},
		&ast.FunctionDecl{
			FunctionSignature: ast.FunctionSignature{Name: "main"},
			Body: &ast.BlockStmt{Statements: []ast.Statement{
				&ast.ReturnStmt{},
			}},
		},
	}}
	module, err := Lower(program)
	if err != nil {
		t.Fatalf("lower failed: %v", err)
	}
	if len(module.Functions) != 1 || module.Functions[0].Name != "main" {
		t.Fatalf("got functions %#v, want only main", module.Functions)
	}
}

// findTestBlock returns a named block from a lowered test function.
func findTestBlock(t *testing.T, fn *Function, name string) *Block {
	t.Helper()
	for _, block := range fn.Blocks {
		if block.Name == name {
			return block
		}
	}
	t.Fatalf("missing block %s in:\n%s", name, Dump(&Module{Functions: []*Function{fn}}))
	return nil
}

// blockHasPhiIncoming reports whether block has a phi incoming edge.
func blockHasPhiIncoming(block *Block, typ string, incoming Incoming) bool {
	for _, instr := range block.Instrs {
		if instr.Op != "phi" || instr.Result.Type != typ {
			continue
		}
		for _, found := range instr.Incoming {
			if found.Block == incoming.Block && sameValue(found.Value, incoming.Value) {
				return true
			}
		}
	}
	return false
}

// blockHasPhiIncomingFrom reports whether a phi of typ has an incoming edge
// from the given predecessor block.
func blockHasPhiIncomingFrom(block *Block, typ string, incomingBlock string) bool {
	for _, instr := range block.Instrs {
		if instr.Op != "phi" || instr.Result.Type != typ {
			continue
		}
		for _, found := range instr.Incoming {
			if found.Block == incomingBlock {
				return true
			}
		}
	}
	return false
}

// lowerSource parses, checks, and lowers a source snippet.
func lowerSource(t *testing.T, source string) *Module {
	t.Helper()
	// std container method signatures come from std's declarations, so a
	// program is only checkable with std loaded, as every real path does.
	program, err := project.LoadSource("", withStdImport(source))
	if err != nil {
		t.Fatalf("load failed: %v", err)
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

const arenaSource = `import std::arena;

struct User {
    name: []u8,
}
fn main(allocator: Allocator) {
    let users = arena::new<User>(allocator);
    let alice = users.add(User { name: "alice" });
    print(users.at(alice).name);
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
fn main() -> !void {
    let value = try parse();
    print(value);
    return;
}`

const helloSnapshot = `fn main() -> void {
entry:
  %1: []u8 = const "hello, kizu"
  call.print %1: []u8
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
  %1: []u8 = const "alice"
  %2: i64 = const 30
  %3: i64 = const 1
  %4: i64 = binary.+ %2: i64, %3: i64
  call.print %1: []u8
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
  %4: []u8 = const "adult"
  call.print %4: []u8
  jump if.end.3
if.else.2:
  %6: []u8 = const "minor"
  call.print %6: []u8
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
  %2: []u8 = const "alice"
  %3: User = struct.new {name: %2: []u8}
  %4: std::arena::Handle<User> = arena.add %1: std::arena::Arena<User>, %3: User
  %5: User = arena.at %1: std::arena::Arena<User>, %4: std::arena::Handle<User>
  %6: []u8 = field.name %5: User
  call.print %6: []u8
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
  %2: !i64 = error.ok %1: i64
  return %2: !i64
}
fn main() -> !void {
entry:
  %1: !i64 = call.parse
  %2: i64 = error.try %1: !i64
  call.print %2: i64
  %4: !void = error.ok
  return %4: !void
}`

// blockScopedLetSource declares the same name in two sibling loop bodies, with
// the second loop inside an if. Before block-scoped bindings, the name stayed in
// the environment after each loop, so the if merge built a phi over one value
// from each loop body -- and a loop that runs zero times never defines its
// value, so neither dominated the merge. clang accepted the module only because
// Apple's toolchain does not run the IR verifier; LLVM 21 rejected it.
const blockScopedLetSource = `fn run(n: i64, flag: bool) -> i64 {
    var total = 0;
    var i = 0;
    while i < n {
        let entry = i * 2;
        total = total + entry;
        i = i + 1;
    }
    if flag {
        var j = 0;
        while j < n {
            let entry = j * 3;
            total = total + entry;
            j = j + 1;
        }
    }
    return total;
}

fn main() {
    print(run(3, true));
}`

// TestLowerBlockScopedLetDominatesItsMerge keeps the source that exposed the
// bug. Phi dominance is a Verify rule now and Lower ends in Verify, so lowering
// this at all is the assertion; TestVerifyRejects covers the rule itself.
func TestLowerBlockScopedLetDominatesItsMerge(t *testing.T) {
	lowerSource(t, blockScopedLetSource)
}

// TestLowerFalliblePrimitiveReleasesOwnerArgument pins the cleanup a fallible
// runtime primitive cannot run itself. The primitive stores its by-value owner
// only after the allocation succeeds, so its failure path has to release what
// the caller already moved. The wrapper is where `T` is bound, which is the
// whole reason the release can be spelled at all.
func TestLowerFalliblePrimitiveReleasesOwnerArgument(t *testing.T) {
	module := lowerSource(t, `
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var parent = std::array::new<std::string::String>(allocator);
    defer parent.deinit_all();
    var name = std::string::new(allocator);
    errdefer name.deinit();
    try name.append_byte(cast<u8>(97));
    try parent.append(name);
    let boxed = try std::mem::box<std::string::String>(allocator, std::string::new(allocator));
    defer boxed.deinit_all();
    return;
}`)
	got := Dump(module)
	for _, want := range []string{
		"  error.try %1: !void, cleanup call.std::string::String.deinit" +
			" %value: std::string::String\n",
		"  %2: std::mem::Box<std::string::String> = error.try" +
			" %1: !std::mem::Box<std::string::String>," +
			" cleanup call.std::string::String.deinit %value: std::string::String\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
		}
	}
}

// TestLowerFalliblePrimitiveLeavesCopyArgumentAlone keeps the release
// type-directed: an element with no deinit contract needs no failure path.
func TestLowerFalliblePrimitiveLeavesCopyArgumentAlone(t *testing.T) {
	module := lowerSource(t, `
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var values = std::array::new<i64>(allocator);
    defer values.deinit();
    try values.append(1);
    return;
}`)
	got := Dump(module)
	want := "  %1: !void = array.append %self: std::array::Array<i64>, %value: i64, i64\n" +
		"  return %1: !void\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
	}
}

// TestLowerFieldStaticParamIsOneInstancePerField pins the symbol a `Field`
// argument produces. Each bound field is its own instance, so the two calls
// lower to two functions and each reads its own field name.
func TestLowerFieldStaticParamIsOneInstancePerField(t *testing.T) {
	module := lowerSource(t, `
struct User { pub name: []u8, pub age: i64 }
fn label<T, f: Field>() -> []u8 {
    return std::meta::field_name<T, f>();
}
fn main() -> void {
    print(label<User, name>());
    print(label<User, age>());
}`)
	got := Dump(module)
	for _, want := range []string{
		"fn label.User.name() -> []u8 {",
		"fn label.User.age() -> []u8 {",
		`%1: []u8 = const "name"`,
		`%1: []u8 = const "age"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
		}
	}
}

// TestLowerMetaConstructBuildsInOneLiteral pins the shape the form expands to:
// one call per field, an errdefer covering the fields already built, and the
// struct literal that consumes them. Nothing holds a half-built value, and the
// cleanup does not outlive the literal.
func TestLowerMetaConstructBuildsInOneLiteral(t *testing.T) {
	module := lowerSource(t, `
struct Names { pub first: std::string::String, pub second: std::string::String }
fn (self: Names) deinit() -> void {
    self.first.deinit();
    self.second.deinit();
    return;
}
fn field_from_name<T, f: Field>(allocator: Allocator) -> !std::meta::field_type<T, f> {
    var out = std::string::new(allocator);
    errdefer out.deinit();
    try out.append_bytes(std::meta::field_name<T, f>());
    return out;
}
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    var names = try std::meta::construct<Names, field_from_name>(allocator);
    defer names.deinit();
    return;
}`)
	got := Dump(module)
	for _, want := range []string{
		"  %2: !std::string::String = call.field_from_name.Names.first %1: Allocator\n",
		"  %4: !std::string::String = call.field_from_name.Names.second %1: Allocator\n",
		"  %5: std::string::String = error.try %4: !std::string::String," +
			" cleanup call.std::string::String.deinit %3: std::string::String\n",
		"  %6: Names = struct.new {first: %3: std::string::String," +
			" second: %5: std::string::String}\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got:\n%s\nwant substring:\n%s", got, want)
		}
	}
}

// withStdImport gives a snippet the root import a file needs to spell full std
// paths. The snippets are about what the checker does with std types, not about
// import lines, and a file that writes `std::mem::page_allocator` has to bring
// the root into scope the same way any other file does.
func withStdImport(source string) string {
	if strings.Contains(source, "import std;") || !writesFullStdPath(source) {
		return source
	}
	return "import std;\n" + source
}

// writesFullStdPath reports whether a snippet spells a std path in code rather
// than only naming one in an import declaration.
func writesFullStdPath(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "import ") &&
			strings.Contains(line, "std::") {
			return true
		}
	}
	return false
}
