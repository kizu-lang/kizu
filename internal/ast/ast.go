package ast

import (
	"bytes"
	"fmt"
	"strings"
)

type Node interface {
	String() string
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

type Program struct {
	Decls []Decl
}

type Decl interface {
	Node
	declNode()
}

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

type FunctionDecl struct {
	Name       string
	Params     []Param
	ReturnType string
	Body       *BlockStmt
}

func (*FunctionDecl) declNode() {}

func (d *FunctionDecl) String() string {
	params := make([]string, 0, len(d.Params))
	for _, p := range d.Params {
		params = append(params, p.String())
	}
	ret := ""
	if d.ReturnType != "" {
		ret = " -> " + d.ReturnType
	}
	return fmt.Sprintf("fn %s(%s)%s %s", d.Name, strings.Join(params, ", "), ret, d.Body.String())
}

type Param struct {
	Name     string
	TypeName string
	Borrow   bool
}

func (p Param) String() string {
	prefix := ""
	if p.Borrow {
		prefix = "borrow "
	}
	return fmt.Sprintf("%s: %s%s", p.Name, prefix, p.TypeName)
}

type BlockStmt struct {
	Statements []Statement
}

func (*BlockStmt) statementNode() {}

func (s *BlockStmt) String() string {
	parts := make([]string, 0, len(s.Statements))
	for _, stmt := range s.Statements {
		parts = append(parts, stmt.String())
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

type LetStmt struct {
	Mutable bool
	Name    string
	Value   Expression
}

func (*LetStmt) statementNode() {}

func (s *LetStmt) String() string {
	kw := "let"
	if s.Mutable {
		kw = "var"
	}
	return fmt.Sprintf("%s %s = %s", kw, s.Name, s.Value.String())
}

type AssignStmt struct {
	Name  string
	Value Expression
}

func (*AssignStmt) statementNode() {}

func (s *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", s.Name, s.Value.String())
}

type ReturnStmt struct {
	Value Expression
}

func (*ReturnStmt) statementNode() {}

func (s *ReturnStmt) String() string {
	return "return " + s.Value.String()
}

type ExprStmt struct {
	Expr Expression
}

func (*ExprStmt) statementNode() {}

func (s *ExprStmt) String() string {
	return s.Expr.String()
}

type IdentExpr struct {
	Name string
}

func (*IdentExpr) expressionNode() {}

func (e *IdentExpr) String() string {
	return e.Name
}

type IntExpr struct {
	Value string
}

func (*IntExpr) expressionNode() {}

func (e *IntExpr) String() string {
	return e.Value
}

type StringExpr struct {
	Value string
}

func (*StringExpr) expressionNode() {}

func (e *StringExpr) String() string {
	return fmt.Sprintf("%q", e.Value)
}

type BoolExpr struct {
	Value bool
}

func (*BoolExpr) expressionNode() {}

func (e *BoolExpr) String() string {
	if e.Value {
		return "true"
	}
	return "false"
}

type PrefixExpr struct {
	Operator string
	Right    Expression
}

func (*PrefixExpr) expressionNode() {}

func (e *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", e.Operator, e.Right.String())
}

type BinaryExpr struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (*BinaryExpr) expressionNode() {}

func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left.String(), e.Operator, e.Right.String())
}

type CallExpr struct {
	Callee Expression
	Args   []Expression
}

func (*CallExpr) expressionNode() {}

func (e *CallExpr) String() string {
	args := make([]string, 0, len(e.Args))
	for _, arg := range e.Args {
		args = append(args, arg.String())
	}
	return fmt.Sprintf("%s(%s)", e.Callee.String(), strings.Join(args, ", "))
}

type FieldExpr struct {
	Receiver Expression
	Name     string
}

func (*FieldExpr) expressionNode() {}

func (e *FieldExpr) String() string {
	return e.Receiver.String() + "." + e.Name
}
