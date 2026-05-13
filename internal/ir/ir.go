package ir

import "fmt"

// Module is a lowered Kizu source file.
type Module struct {
	Structs   map[string]Struct
	Functions []*Function
}

// Struct is the IR view of a declared Kizu struct.
type Struct struct {
	Name   string
	Fields []Field
}

// Field is a typed struct field.
type Field struct {
	Name string
	Type string
}

// Function is a typed SSA function.
type Function struct {
	Name   string
	Params []Value
	Return string
	Blocks []*Block
}

// Block is a basic block with instructions and one terminator.
type Block struct {
	Name         string
	Instrs       []*Instr
	Terminator   Terminator
	Predecessors []string
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
	Result    Value
	Op        string
	Args      []Value
	Immediate string
	Fields    []FieldArg
	Incoming  []Incoming
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

// Signature stores a function's callable type.
type Signature struct {
	Params []string
	Return string
}
