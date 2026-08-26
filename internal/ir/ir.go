package ir

import (
	"fmt"

	"github.com/kizu-lang/kizu/internal/ast"
)

// Module is one lowered Kizu program, from either a package or a loose source file.
type Module struct {
	Structs map[string]Struct
	Enums   map[string]Enum
	// ErrorSets are named like enums but numbered globally: an error value is
	// one integer that means the same member in every error union it crosses.
	ErrorSets map[string]Enum
	Unions    map[string]Union
	Functions []*Function
}

// Struct is the IR view of a declared Kizu struct.
type Struct struct {
	Name   string
	Fields []Field
}

// Enum is the IR view of a declared tag enum.
type Enum struct {
	Name string
	Tags map[string]int
	// Origins lists the `{ }`-form error sets a combined error set
	// (`error C = A or B;`) takes its members from, fully resolved. It is nil
	// for enums and for sets that declare their own members. A combined set
	// carries no Tags of its own: its member codes stay filed under the sets
	// that declared them, which keeps every code's spelling unique and lets a
	// match arm resolve through the declaring set (ADR-0127).
	Origins []string
}

// Union is the IR view of a tagged union declaration.
type Union struct {
	Name     string
	Variants map[string]UnionVariant
}

// UnionVariant stores one union tag and optional payload type.
type UnionVariant struct {
	Name    string
	Index   int
	Payload string
}

// Field is a typed struct field.
type Field struct {
	Name string
	Type string
}

// Function is a typed SSA function. Its parameters keep the Passing lowering
// decided for them, so a reader asks how an argument travels instead of
// inferring it back from the type's spelling.
type Function struct {
	Name   string
	Params []Param
	Return string
	Blocks []*Block
}

// Block is a basic block with instructions and one terminator. Its predecessors
// are not stored: they follow from the terminators naming it, and a stored copy
// is one more thing that can disagree with the CFG.
type Block struct {
	Name       string
	Instrs     []*Instr
	Terminator Terminator
}

// Value names an SSA value and its type.
type Value struct {
	Name string
	Type string
}

// String returns a typed value reference.
func (v Value) String() string {
	if v.Type == "" {
		return v.Name
	}
	return fmt.Sprintf("%s: %s", v.Name, v.Type)
}

// Instr is one SSA instruction.
type Instr struct {
	Result Value
	Op     string
	Args   []Value
	// Immediate is what the instruction carries that no value of its own
	// spells: the text of a literal, the variant a union instruction selects,
	// and the kind a cond_fail reports. A type argument is not among them --
	// a container spells its element in its own type, and a second copy here
	// could be missing (a Cleanup carries none) or disagree, either of which
	// measures a release against the wrong size.
	Immediate string
	Fields    []FieldArg
	Incoming  []Incoming
	Cleanups  []Cleanup
	// Span is set where a runtime failure must name a source position. It is
	// zero for instructions that cannot fail, and for failures whose source
	// node does not carry a span yet.
	Span ast.Span
}

// Cleanup is a deferred void instruction that must run before a scope exits.
// OnError marks errdefer cleanups, which run only on error-return paths.
// Receiver names the local the cleanup consumes, so an error exit can drop the
// cleanups a move has retired (ADR-0114).
type Cleanup struct {
	Op       string
	Args     []Value
	OnError  bool
	Receiver string
}

// FieldArg connects a struct field name to a value.
type FieldArg struct {
	Name  string
	Value Value
}

// Incoming is one predecessor value for a phi instruction.
type Incoming struct {
	Block string
	Value Value
}

// Terminator ends a basic block.
type Terminator struct {
	Op     string
	Value  Value
	Cond   Value
	Target string
	Else   string
}

// Successors names the blocks control can reach from this terminator. Deriving
// the CFG edges here is what keeps a new kind of terminator from having to be
// remembered everywhere an edge is followed.
func (t Terminator) Successors() []string {
	targets := make([]string, 0, 2)
	for _, target := range []string{t.Target, t.Else} {
		if target != "" {
			targets = append(targets, target)
		}
	}
	return targets
}

// Signature stores a function's callable type.
type Signature struct {
	Params []Param
	Return string
	// Unsafe carries the obligation the declaration named, so the type its
	// name has as a value spells `unsafe fn(...)` the way the checker did.
	Unsafe bool
}

// Param is one parameter of a callable: its SSA name, the type the callee sees,
// and how the call hands it over. The last two come from one decision, so a
// caller reading how a parameter is passed reads the same answer the type was
// built from.
type Param struct {
	Name    string
	Type    string
	Passing Passing
}

// Value is the parameter as the body reads it: the SSA value bound to its name
// on entry. Passing is how the call gets it there and has no reading inside.
func (p Param) Value() Value {
	return Value{Name: p.Name, Type: p.Type}
}

// TakesAddressOf reports whether a parameter reading through an address is
// being handed the value that address is taken of, rather than an address the
// caller already held. The IR does not name an address, so both arrive spelled
// like whatever the caller had, and this is the one question that separates
// them. It stops being a question when taking an address is an instruction with
// a result of its own.
func (p Param) TakesAddressOf(argType string) bool {
	return p.Passing == PassCopyAddress && p.Type != argType && derefType(p.Type) == argType
}

// Passing is how a call hands one parameter to the callee.
type Passing int

const (
	// PassValue copies the parameter. The callee cannot write back through it.
	PassValue Passing = iota
	// PassCallerStorage hands over the caller's local itself, so a write in the
	// callee lands in it. A local passed this way has to have storage to lend,
	// which is what makes it more than an SSA value.
	PassCallerStorage
	// PassCopyAddress hands over the address of a copy made for the call. The
	// callee reads through it and the caller keeps its own value, so nothing
	// about the caller's local changes.
	PassCopyAddress
)
