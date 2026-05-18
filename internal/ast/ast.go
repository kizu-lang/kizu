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

// ImportDecl represents one explicit top-level module import.
type ImportDecl struct {
	Path []string
}

// declNode marks ImportDecl as a declaration node.
func (*ImportDecl) declNode() {}

// String returns a compact debug representation of the import declaration.
func (d *ImportDecl) String() string {
	return "import " + strings.Join(d.Path, "::")
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
	Public     bool
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
	if d.Public {
		prefix += "pub "
	}
	if d.Unsafe {
		prefix += "unsafe "
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
	Public bool
}

// declNode marks StructDecl as a declaration node.
func (*StructDecl) declNode() {}

// String returns a compact debug representation of the struct declaration.
func (d *StructDecl) String() string {
	fields := make([]string, 0, len(d.Fields))
	for _, field := range d.Fields {
		fields = append(fields, field.String())
	}
	prefix := ""
	if d.Public {
		prefix = "pub "
	}
	return fmt.Sprintf("%sstruct %s { %s }", prefix, d.Name, strings.Join(fields, "; "))
}

// EnumDecl represents a Zig/C-style tag enum declaration.
type EnumDecl struct {
	Name   string
	Tags   []string
	Public bool
}

// declNode marks EnumDecl as a declaration node.
func (*EnumDecl) declNode() {}

// String returns a compact debug representation of the enum declaration.
func (d *EnumDecl) String() string {
	prefix := ""
	if d.Public {
		prefix = "pub "
	}
	return fmt.Sprintf("%senum %s { %s }", prefix, d.Name, strings.Join(d.Tags, "; "))
}

// UnionDecl represents a tagged union declaration.
type UnionDecl struct {
	Name     string
	Variants []UnionVariant
	Public   bool
}

// declNode marks UnionDecl as a declaration node.
func (*UnionDecl) declNode() {}

// String returns a compact debug representation of the union declaration.
func (d *UnionDecl) String() string {
	variants := make([]string, 0, len(d.Variants))
	for _, variant := range d.Variants {
		variants = append(variants, variant.String())
	}
	prefix := ""
	if d.Public {
		prefix = "pub "
	}
	return fmt.Sprintf("%sunion %s { %s }", prefix, d.Name, strings.Join(variants, "; "))
}

// UnionVariant represents one tagged union variant.
type UnionVariant struct {
	Name    string
	Payload string
}

// String returns a compact debug representation of the union variant.
func (v UnionVariant) String() string {
	if v.Payload == "" {
		return v.Name
	}
	return fmt.Sprintf("%s(%s)", v.Name, v.Payload)
}

// ContractDecl represents required method signatures.
type ContractDecl struct {
	Name    string
	Methods []*FunctionDecl
	Public  bool
}

// declNode marks ContractDecl as a declaration node.
func (*ContractDecl) declNode() {}

// String returns a compact debug representation of the contract declaration.
func (d *ContractDecl) String() string {
	methods := make([]string, 0, len(d.Methods))
	for _, method := range d.Methods {
		methods = append(methods, method.String())
	}
	prefix := ""
	if d.Public {
		prefix = "pub "
	}
	return fmt.Sprintf("%scontract %s { %s }", prefix, d.Name, strings.Join(methods, "; "))
}

// ImplDecl represents methods implemented for one concrete type.
type ImplDecl struct {
	TypeName string
	Methods  []*FunctionDecl
}

// declNode marks ImplDecl as a declaration node.
func (*ImplDecl) declNode() {}

// String returns a compact debug representation of the impl declaration.
func (d *ImplDecl) String() string {
	methods := make([]string, 0, len(d.Methods))
	for _, method := range d.Methods {
		methods = append(methods, method.String())
	}
	return fmt.Sprintf("impl %s { %s }", d.TypeName, strings.Join(methods, "; "))
}

// SatisfyDecl represents explicit contract satisfaction.
type SatisfyDecl struct {
	ContractName string
	TypeName     string
}

// declNode marks SatisfyDecl as a declaration node.
func (*SatisfyDecl) declNode() {}

// String returns a compact debug representation of the satisfy declaration.
func (d *SatisfyDecl) String() string {
	return fmt.Sprintf("satisfy %s for %s", d.ContractName, d.TypeName)
}

// Field represents a named struct field.
type Field struct {
	Name      string
	TypeName  string
	Borrow    bool
	MutBorrow bool
	Public    bool
}

// String returns a compact debug representation of the field.
func (f Field) String() string {
	prefix := ""
	if f.MutBorrow {
		prefix = "&mut "
	} else if f.Borrow {
		prefix = "&"
	}
	visibility := ""
	if f.Public {
		visibility = "pub "
	}
	return fmt.Sprintf("%s%s: %s%s", visibility, f.Name, prefix, f.TypeName)
}

// Param represents a function parameter.
type Param struct {
	Name      string
	TypeName  string
	Borrow    bool
	MutBorrow bool
	Comptime  bool
}

// String returns a compact debug representation of the parameter.
func (p Param) String() string {
	prefix := ""
	if p.MutBorrow {
		prefix = "&mut "
	} else if p.Borrow {
		prefix = "&"
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
	return "{ " + strings.Join(parts, " ") + " }"
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
	return fmt.Sprintf("%s %s = %s;", kw, s.Name, s.Value.String())
}

// AssignStmt represents assignment to an existing binding.
type AssignStmt struct {
	Target Expression
	Value  Expression
}

// statementNode marks AssignStmt as a statement node.
func (*AssignStmt) statementNode() {}

// String returns a compact debug representation of the assignment.
func (s *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s;", s.Target.String(), s.Value.String())
}

// ReturnStmt represents an explicit return statement.
type ReturnStmt struct {
	Value Expression
}

// statementNode marks ReturnStmt as a statement node.
func (*ReturnStmt) statementNode() {}

// String returns a compact debug representation of the return statement.
func (s *ReturnStmt) String() string {
	if s.Value == nil {
		return "return;"
	}
	return "return " + s.Value.String() + ";"
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
	Label     string
	Condition Expression
	Body      *BlockStmt
}

// statementNode marks WhileStmt as a statement node.
func (*WhileStmt) statementNode() {}

// String returns a compact debug representation of the while statement.
func (s *WhileStmt) String() string {
	out := fmt.Sprintf("while %s %s", s.Condition.String(), s.Body.String())
	if s.Label != "" {
		return s.Label + ": " + out
	}
	return out
}

// ForStmt represents a bounded integer range loop.
type ForStmt struct {
	Label string
	Name  string
	Start Expression
	End   Expression
	Body  *BlockStmt
}

// statementNode marks ForStmt as a statement node.
func (*ForStmt) statementNode() {}

// String returns a compact debug representation of the for statement.
func (s *ForStmt) String() string {
	out := fmt.Sprintf("for %s..%s |%s| %s",
		s.Start.String(), s.End.String(), s.Name, s.Body.String())
	if s.Label != "" {
		return s.Label + ": " + out
	}
	return out
}

// BreakStmt exits the nearest loop or a named enclosing loop.
type BreakStmt struct {
	Label string
}

// statementNode marks BreakStmt as a statement node.
func (*BreakStmt) statementNode() {}

// String returns a compact debug representation of break.
func (s *BreakStmt) String() string {
	if s.Label != "" {
		return "break :" + s.Label + ";"
	}
	return "break;"
}

// ContinueStmt skips to the next iteration of the nearest or named loop.
type ContinueStmt struct {
	Label string
}

// statementNode marks ContinueStmt as a statement node.
func (*ContinueStmt) statementNode() {}

// String returns a compact debug representation of continue.
func (s *ContinueStmt) String() string {
	if s.Label != "" {
		return "continue :" + s.Label + ";"
	}
	return "continue;"
}

// MatchStmt represents a simple enum tag match statement.
type MatchStmt struct {
	Value Expression
	Arms  []MatchArm
}

// statementNode marks MatchStmt as a statement node.
func (*MatchStmt) statementNode() {}

// String returns a compact debug representation of the match statement.
func (s *MatchStmt) String() string {
	arms := make([]string, 0, len(s.Arms))
	for _, arm := range s.Arms {
		arms = append(arms, arm.String())
	}
	return fmt.Sprintf("match %s { %s }", s.Value.String(), strings.Join(arms, " "))
}

// MatchArm represents one enum tag branch in a match statement.
type MatchArm struct {
	Tag     string
	Binding string
	Body    Statement
}

// String returns a compact debug representation of the match arm.
func (a MatchArm) String() string {
	if a.Binding != "" {
		return fmt.Sprintf("%s(%s) => %s", a.Tag, a.Binding, a.Body.String())
	}
	return fmt.Sprintf("%s => %s", a.Tag, a.Body.String())
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
	return s.Expr.String() + ";"
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

// IfExpr represents a conditional expression with a value in each branch.
type IfExpr struct {
	Condition   Expression
	Consequence *BlockStmt
	Alternative *BlockStmt
}

// expressionNode marks IfExpr as an expression node.
func (*IfExpr) expressionNode() {}

// String returns a compact debug representation of the if expression.
func (e *IfExpr) String() string {
	return fmt.Sprintf("if %s %s else %s",
		e.Condition.String(), e.Consequence.String(), e.Alternative.String())
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

// TypeApplyExpr represents a namespace item with explicit type arguments.
type TypeApplyExpr struct {
	Callee  Expression
	TypeArg string
}

// expressionNode marks TypeApplyExpr as an expression node.
func (*TypeApplyExpr) expressionNode() {}

// String returns a compact debug representation of the type application.
func (e *TypeApplyExpr) String() string {
	return fmt.Sprintf("%s<%s>", e.Callee.String(), e.TypeArg)
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

// TryExpr unwraps a !T value or returns the error from the current function.
type TryExpr struct {
	Value Expression
}

// expressionNode marks TryExpr as an expression node.
func (*TryExpr) expressionNode() {}

// String returns a compact debug representation of the try expression.
func (e *TryExpr) String() string {
	return "try " + e.Value.String()
}

// IndexExpr represents checked byte indexing or one-dimensional slicing.
type IndexExpr struct {
	Target Expression
	Index  Expression
	Start  Expression
	End    Expression
	Slice  bool
}

// expressionNode marks IndexExpr as an expression node.
func (*IndexExpr) expressionNode() {}

// String returns a compact debug representation of checked access.
func (e *IndexExpr) String() string {
	if !e.Slice {
		return fmt.Sprintf("%s[%s]", e.Target.String(), e.Index.String())
	}
	start := ""
	if e.Start != nil {
		start = e.Start.String()
	}
	end := ""
	if e.End != nil {
		end = e.End.String()
	}
	return fmt.Sprintf("%s[%s..%s]", e.Target.String(), start, end)
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
	Receiver  Expression
	Name      string
	Namespace bool
}

// expressionNode marks FieldExpr as an expression node.
func (*FieldExpr) expressionNode() {}

// String returns a compact debug representation of the field access.
func (e *FieldExpr) String() string {
	if e.Namespace {
		return e.Receiver.String() + "::" + e.Name
	}
	return e.Receiver.String() + "." + e.Name
}

// DerefExpr represents explicit postfix dereference with Zig-style .*
type DerefExpr struct {
	Receiver Expression
}

// expressionNode marks DerefExpr as an expression node.
func (*DerefExpr) expressionNode() {}

// String returns a compact debug representation of explicit dereference.
func (e *DerefExpr) String() string {
	return e.Receiver.String() + ".*"
}
