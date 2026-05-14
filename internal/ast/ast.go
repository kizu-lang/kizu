package ast

import (
	"bytes"
	"fmt"
	"strings"
)

// Node is implemented by every AST node.
type Node interface {
	String() string
}

// Statement is implemented by nodes that can appear as statements.
type Statement interface {
	Node
	statementNode()
}

// Expression is implemented by nodes that produce a value.
type Expression interface {
	Node
	expressionNode()
}

// Program is the root AST node for a Kizu source file.
type Program struct {
	Decls []Decl
}

// Decl is implemented by top-level declarations.
type Decl interface {
	Node
	declNode()
}

// String returns a compact debug representation of the program.
func (p *Program) String() string {
	var out bytes.Buffer
	for i, decl := range p.Decls {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(decl.String())
	}
	return out.String()
}

// FunctionDecl represents a function declaration.
type FunctionDecl struct {
	Name       string
	Params     []Param
	ReturnType string
	Body       *BlockStmt
	Unsafe     bool
	ExternABI  string
}

// declNode marks FunctionDecl as a declaration node.
func (*FunctionDecl) declNode() {}

// String returns a compact debug representation of the function.
func (d *FunctionDecl) String() string {
	params := make([]string, 0, len(d.Params))
	for _, p := range d.Params {
		params = append(params, p.String())
	}
	ret := ""
	if d.ReturnType != "" {
		ret = " -> " + d.ReturnType
	}
	prefix := ""
	if d.Unsafe {
		prefix = "unsafe "
	}
	if d.ExternABI != "" {
		return fmt.Sprintf("%sextern %q fn %s(%s)%s",
			prefix, d.ExternABI, d.Name, strings.Join(params, ", "), ret)
	}
	return fmt.Sprintf("%sfn %s(%s)%s %s",
		prefix, d.Name, strings.Join(params, ", "), ret, d.Body.String())
}

// StructDecl represents a top-level struct declaration.
type StructDecl struct {
	Name   string
	Fields []Field
}

// declNode marks StructDecl as a declaration node.
func (*StructDecl) declNode() {}

// String returns a compact debug representation of the struct declaration.
func (d *StructDecl) String() string {
	fields := make([]string, 0, len(d.Fields))
	for _, field := range d.Fields {
		fields = append(fields, field.String())
	}
	return fmt.Sprintf("struct %s { %s }", d.Name, strings.Join(fields, "; "))
}

// EnumDecl represents a Zig/C-style tag enum declaration.
type EnumDecl struct {
	Name string
	Tags []string
}

// declNode marks EnumDecl as a declaration node.
func (*EnumDecl) declNode() {}

// String returns a compact debug representation of the enum declaration.
func (d *EnumDecl) String() string {
	return fmt.Sprintf("enum %s { %s }", d.Name, strings.Join(d.Tags, "; "))
}

// Field represents a named struct field.
type Field struct {
	Name     string
	TypeName string
	Borrow   bool
}

// String returns a compact debug representation of the field.
func (f Field) String() string {
	prefix := ""
	if f.Borrow {
		prefix = "borrow "
	}
	return fmt.Sprintf("%s: %s%s", f.Name, prefix, f.TypeName)
}

// Param represents a function parameter.
type Param struct {
	Name     string
	TypeName string
	Borrow   bool
	Comptime bool
}

// String returns a compact debug representation of the parameter.
func (p Param) String() string {
	prefix := ""
	if p.Borrow {
		prefix = "borrow "
	}
	if !p.Comptime {
		return fmt.Sprintf("%s: %s%s", p.Name, prefix, p.TypeName)
	}
	return fmt.Sprintf("comptime %s: %s%s", p.Name, prefix, p.TypeName)
}

// BlockStmt represents a sequence of statements.
type BlockStmt struct {
	Statements []Statement
}

// statementNode marks BlockStmt as a statement node.
func (*BlockStmt) statementNode() {}

// String returns a compact debug representation of the block.
func (s *BlockStmt) String() string {
	parts := make([]string, 0, len(s.Statements))
	for _, stmt := range s.Statements {
		parts = append(parts, stmt.String())
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

// LetStmt represents a let or var declaration.
type LetStmt struct {
	Mutable bool
	Name    string
	Value   Expression
}

// statementNode marks LetStmt as a statement node.
func (*LetStmt) statementNode() {}

// String returns a compact debug representation of the declaration.
func (s *LetStmt) String() string {
	kw := "let"
	if s.Mutable {
		kw = "var"
	}
	return fmt.Sprintf("%s %s = %s", kw, s.Name, s.Value.String())
}

// AssignStmt represents assignment to an existing binding.
type AssignStmt struct {
	Name  string
	Value Expression
}

// statementNode marks AssignStmt as a statement node.
func (*AssignStmt) statementNode() {}

// String returns a compact debug representation of the assignment.
func (s *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", s.Name, s.Value.String())
}

// ReturnStmt represents an explicit return statement.
type ReturnStmt struct {
	Value Expression
}

// statementNode marks ReturnStmt as a statement node.
func (*ReturnStmt) statementNode() {}

// String returns a compact debug representation of the return statement.
func (s *ReturnStmt) String() string {
	return "return " + s.Value.String()
}

// IfStmt represents a conditional branch.
type IfStmt struct {
	Condition   Expression
	Consequence *BlockStmt
	Alternative *BlockStmt
}

// statementNode marks IfStmt as a statement node.
func (*IfStmt) statementNode() {}

// String returns a compact debug representation of the if statement.
func (s *IfStmt) String() string {
	out := fmt.Sprintf("if %s %s", s.Condition.String(), s.Consequence.String())
	if s.Alternative != nil {
		out += " else " + s.Alternative.String()
	}
	return out
}

// WhileStmt represents a loop guarded by a condition expression.
type WhileStmt struct {
	Condition Expression
	Body      *BlockStmt
}

// statementNode marks WhileStmt as a statement node.
func (*WhileStmt) statementNode() {}

// String returns a compact debug representation of the while statement.
func (s *WhileStmt) String() string {
	return fmt.Sprintf("while %s %s", s.Condition.String(), s.Body.String())
}

// UnsafeStmt represents an explicit unsafe block.
type UnsafeStmt struct {
	Body *BlockStmt
}

// statementNode marks UnsafeStmt as a statement node.
func (*UnsafeStmt) statementNode() {}

// String returns a compact debug representation of the unsafe block.
func (s *UnsafeStmt) String() string {
	return "unsafe " + s.Body.String()
}

// ComptimeIfStmt represents a branch selected during compilation.
type ComptimeIfStmt struct {
	Condition   Expression
	Consequence *BlockStmt
	Alternative *BlockStmt
}

// statementNode marks ComptimeIfStmt as a statement node.
func (*ComptimeIfStmt) statementNode() {}

// String returns a compact debug representation of the comptime branch.
func (s *ComptimeIfStmt) String() string {
	out := fmt.Sprintf("comptime if %s %s", s.Condition.String(), s.Consequence.String())
	if s.Alternative != nil {
		out += " else " + s.Alternative.String()
	}
	return out
}

// ExprStmt wraps an expression used as a statement.
type ExprStmt struct {
	Expr Expression
}

// statementNode marks ExprStmt as a statement node.
func (*ExprStmt) statementNode() {}

// String returns a compact debug representation of the expression statement.
func (s *ExprStmt) String() string {
	return s.Expr.String()
}

// IdentExpr represents a name reference.
type IdentExpr struct {
	Name string
}

// expressionNode marks IdentExpr as an expression node.
func (*IdentExpr) expressionNode() {}

// String returns the identifier name.
func (e *IdentExpr) String() string {
	return e.Name
}

// IntExpr represents an integer literal.
type IntExpr struct {
	Value string
}

// expressionNode marks IntExpr as an expression node.
func (*IntExpr) expressionNode() {}

// String returns the literal spelling.
func (e *IntExpr) String() string {
	return e.Value
}

// StringExpr represents a string literal.
type StringExpr struct {
	Value string
}

// expressionNode marks StringExpr as an expression node.
func (*StringExpr) expressionNode() {}

// String returns the quoted literal spelling.
func (e *StringExpr) String() string {
	return fmt.Sprintf("%q", e.Value)
}

// BoolExpr represents a boolean literal.
type BoolExpr struct {
	Value bool
}

// expressionNode marks BoolExpr as an expression node.
func (*BoolExpr) expressionNode() {}

// String returns the boolean literal spelling.
func (e *BoolExpr) String() string {
	if e.Value {
		return "true"
	}
	return "false"
}

// ComptimeExpr represents an expression evaluated during compilation.
type ComptimeExpr struct {
	Expr Expression
}

// expressionNode marks ComptimeExpr as an expression node.
func (*ComptimeExpr) expressionNode() {}

// String returns a compact debug representation of the comptime expression.
func (e *ComptimeExpr) String() string {
	return "comptime " + e.Expr.String()
}

// PrefixExpr represents a unary operator expression.
type PrefixExpr struct {
	Operator string
	Right    Expression
}

// expressionNode marks PrefixExpr as an expression node.
func (*PrefixExpr) expressionNode() {}

// String returns a compact debug representation of the prefix expression.
func (e *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", e.Operator, e.Right.String())
}

// BinaryExpr represents an infix binary operator expression.
type BinaryExpr struct {
	Left     Expression
	Operator string
	Right    Expression
}

// expressionNode marks BinaryExpr as an expression node.
func (*BinaryExpr) expressionNode() {}

// String returns a compact debug representation of the binary expression.
func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left.String(), e.Operator, e.Right.String())
}

// CallExpr represents a function call.
type CallExpr struct {
	Callee Expression
	Args   []Expression
}

// expressionNode marks CallExpr as an expression node.
func (*CallExpr) expressionNode() {}

// String returns a compact debug representation of the call.
func (e *CallExpr) String() string {
	args := make([]string, 0, len(e.Args))
	for _, arg := range e.Args {
		args = append(args, arg.String())
	}
	return fmt.Sprintf("%s(%s)", e.Callee.String(), strings.Join(args, ", "))
}

// CastExpr represents an explicit cast<T>(value) conversion.
type CastExpr struct {
	TargetType string
	Value      Expression
}

// expressionNode marks CastExpr as an expression node.
func (*CastExpr) expressionNode() {}

// String returns a compact debug representation of the cast.
func (e *CastExpr) String() string {
	return fmt.Sprintf("cast<%s>(%s)", e.TargetType, e.Value.String())
}

// TryExpr unwraps a result<T> or returns the error from the current function.
type TryExpr struct {
	Value Expression
}

// expressionNode marks TryExpr as an expression node.
func (*TryExpr) expressionNode() {}

// String returns a compact debug representation of the try expression.
func (e *TryExpr) String() string {
	return "try " + e.Value.String()
}

// ArenaNewExpr represents arena<T>() construction.
type ArenaNewExpr struct {
	TypeName string
}

// expressionNode marks ArenaNewExpr as an expression node.
func (*ArenaNewExpr) expressionNode() {}

// String returns a compact debug representation of arena construction.
func (e *ArenaNewExpr) String() string {
	return fmt.Sprintf("arena<%s>()", e.TypeName)
}

// StructLiteralExpr represents construction of a struct value.
type StructLiteralExpr struct {
	TypeName string
	Fields   []FieldValue
}

// expressionNode marks StructLiteralExpr as an expression node.
func (*StructLiteralExpr) expressionNode() {}

// String returns a compact debug representation of the struct literal.
func (e *StructLiteralExpr) String() string {
	fields := make([]string, 0, len(e.Fields))
	for _, field := range e.Fields {
		fields = append(fields, field.String())
	}
	return fmt.Sprintf("%s { %s }", e.TypeName, strings.Join(fields, ", "))
}

// FieldValue represents one field initializer in a struct literal.
type FieldValue struct {
	Name  string
	Value Expression
}

// String returns a compact debug representation of the field initializer.
func (v FieldValue) String() string {
	return fmt.Sprintf("%s: %s", v.Name, v.Value.String())
}

// FieldExpr represents field access on a receiver expression.
type FieldExpr struct {
	Receiver Expression
	Name     string
}

// expressionNode marks FieldExpr as an expression node.
func (*FieldExpr) expressionNode() {}

// String returns a compact debug representation of the field access.
func (e *FieldExpr) String() string {
	return e.Receiver.String() + "." + e.Name
}
