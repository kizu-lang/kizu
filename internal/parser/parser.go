package parser

import (
	"fmt"

	"tiny-safe/internal/ast"
	"tiny-safe/internal/lexer"
	"tiny-safe/internal/token"
)

// Parser consumes tokens and builds a Kizu AST.
type Parser struct {
	l      *lexer.Lexer
	cur    token.Token
	peek   token.Token
	errors []string
}

// New creates a parser over l.
func New(l *lexer.Lexer) *Parser {
	p := &Parser{l: l}
	p.nextToken()
	p.nextToken()
	return p
}

// Errors returns parse errors collected so far.
func (p *Parser) Errors() []string {
	return p.errors
}

// ParseProgram parses a complete source file.
func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	for p.cur.Type != token.EOF {
		switch p.cur.Type {
		case token.Function:
			program.Decls = append(program.Decls, p.parseFunctionDecl())
			p.nextToken()
		default:
			p.errorf("expected declaration, got %s", p.cur.Type)
			p.nextToken()
		}
	}
	return program
}

// parseFunctionDecl parses a top-level function declaration.
func (p *Parser) parseFunctionDecl() ast.Decl {
	fn := &ast.FunctionDecl{}
	if !p.expectPeek(token.Ident) {
		return fn
	}
	fn.Name = p.cur.Literal
	if !p.expectPeek(token.LParen) {
		return fn
	}
	fn.Params = p.parseParams()
	if !p.expectCur(token.RParen) {
		return fn
	}
	if p.peek.Type == token.Arrow {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return fn
		}
		fn.ReturnType = p.cur.Literal
	}
	if !p.expectPeek(token.LBrace) {
		return fn
	}
	fn.Body = p.parseBlockStmt()
	return fn
}

// parseParams parses a function parameter list.
func (p *Parser) parseParams() []ast.Param {
	params := []ast.Param{}
	p.nextToken()
	if p.cur.Type == token.RParen {
		return params
	}
	for {
		param := ast.Param{}
		if p.cur.Type != token.Ident {
			p.errorf("expected parameter name, got %s", p.cur.Type)
			return params
		}
		param.Name = p.cur.Literal
		if !p.expectPeek(token.Colon) {
			return params
		}
		p.nextToken()
		if p.cur.Type == token.Borrow {
			param.Borrow = true
			p.nextToken()
		}
		if p.cur.Type != token.Ident {
			p.errorf("expected parameter type, got %s", p.cur.Type)
			return params
		}
		param.TypeName = p.cur.Literal
		params = append(params, param)

		if p.peek.Type != token.Comma {
			break
		}
		p.nextToken()
		p.nextToken()
	}
	if !p.expectPeek(token.RParen) {
		return params
	}
	return params
}

// parseBlockStmt parses a brace-delimited statement block.
func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		stmt := p.parseStatement()
		block.Statements = append(block.Statements, stmt)
		p.nextToken()
	}
	return block
}

// parseStatement parses a single statement.
func (p *Parser) parseStatement() ast.Statement {
	switch p.cur.Type {
	case token.Let:
		return p.parseLetStmt(false)
	case token.Var:
		return p.parseLetStmt(true)
	case token.Return:
		return p.parseReturnStmt()
	case token.If:
		return p.parseIfStmt()
	case token.While:
		return p.parseWhileStmt()
	case token.Ident:
		if p.peek.Type == token.Assign {
			return p.parseAssignStmt()
		}
	}
	expr := p.parseExpression(lowest)
	return &ast.ExprStmt{Expr: expr}
}

// parseLetStmt parses a let or var declaration.
func (p *Parser) parseLetStmt(mutable bool) ast.Statement {
	stmt := &ast.LetStmt{Mutable: mutable}
	if !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Name = p.cur.Literal
	if !p.expectPeek(token.Assign) {
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	return stmt
}

// parseAssignStmt parses assignment to an existing binding.
func (p *Parser) parseAssignStmt() ast.Statement {
	stmt := &ast.AssignStmt{Name: p.cur.Literal}
	p.nextToken()
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	return stmt
}

// parseReturnStmt parses an explicit return statement.
func (p *Parser) parseReturnStmt() ast.Statement {
	stmt := &ast.ReturnStmt{}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	return stmt
}

// parseIfStmt parses an if statement with an optional else block.
func (p *Parser) parseIfStmt() ast.Statement {
	stmt := &ast.IfStmt{}
	p.nextToken()
	stmt.Condition = p.parseExpression(lowest)
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Consequence = p.parseBlockStmt()
	if p.peek.Type == token.Else {
		p.nextToken()
		if !p.expectPeek(token.LBrace) {
			return stmt
		}
		stmt.Alternative = p.parseBlockStmt()
	}
	return stmt
}

// parseWhileStmt parses a while loop statement.
func (p *Parser) parseWhileStmt() ast.Statement {
	stmt := &ast.WhileStmt{}
	p.nextToken()
	stmt.Condition = p.parseExpression(lowest)
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStmt()
	return stmt
}

const (
	_ int = iota
	lowest
	equals
	lessGreater
	sum
	product
	prefix
	call
	field
)

var precedences = map[token.Type]int{
	token.Eq:       equals,
	token.NotEq:    equals,
	token.LT:       lessGreater,
	token.LTE:      lessGreater,
	token.GT:       lessGreater,
	token.GTE:      lessGreater,
	token.Plus:     sum,
	token.Minus:    sum,
	token.Asterisk: product,
	token.Slash:    product,
	token.Percent:  product,
	token.LParen:   call,
	token.Dot:      field,
}

// parseExpression parses an expression using Pratt parser precedence.
func (p *Parser) parseExpression(precedence int) ast.Expression {
	var left ast.Expression

	switch p.cur.Type {
	case token.Ident:
		left = &ast.IdentExpr{Name: p.cur.Literal}
	case token.Int:
		left = &ast.IntExpr{Value: p.cur.Literal}
	case token.String:
		left = &ast.StringExpr{Value: p.cur.Literal}
	case token.True:
		left = &ast.BoolExpr{Value: true}
	case token.False:
		left = &ast.BoolExpr{Value: false}
	case token.Bang, token.Minus:
		op := p.cur.Literal
		p.nextToken()
		left = &ast.PrefixExpr{Operator: op, Right: p.parseExpression(prefix)}
	case token.LParen:
		p.nextToken()
		left = p.parseExpression(lowest)
		p.expectPeek(token.RParen)
	default:
		p.errorf("expected expression, got %s", p.cur.Type)
		return &ast.IdentExpr{Name: "<error>"}
	}

	for p.peek.Type != token.Semicolon && precedence < p.peekPrecedence() {
		switch p.peek.Type {
		case token.Plus, token.Minus, token.Asterisk, token.Slash, token.Percent,
			token.Eq, token.NotEq, token.LT, token.LTE, token.GT, token.GTE:
			p.nextToken()
			left = p.parseBinaryExpr(left)
		case token.LParen:
			p.nextToken()
			left = p.parseCallExpr(left)
		case token.Dot:
			p.nextToken()
			left = p.parseFieldExpr(left)
		default:
			return left
		}
	}
	return left
}

// parseBinaryExpr parses an infix binary expression.
func (p *Parser) parseBinaryExpr(left ast.Expression) ast.Expression {
	expr := &ast.BinaryExpr{Left: left, Operator: p.cur.Literal}
	precedence := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

// parseCallExpr parses a function call expression.
func (p *Parser) parseCallExpr(callee ast.Expression) ast.Expression {
	expr := &ast.CallExpr{Callee: callee}
	if p.peek.Type == token.RParen {
		p.nextToken()
		return expr
	}
	p.nextToken()
	expr.Args = append(expr.Args, p.parseExpression(lowest))
	for p.peek.Type == token.Comma {
		p.nextToken()
		p.nextToken()
		expr.Args = append(expr.Args, p.parseExpression(lowest))
	}
	p.expectPeek(token.RParen)
	return expr
}

// parseFieldExpr parses field access on an expression.
func (p *Parser) parseFieldExpr(receiver ast.Expression) ast.Expression {
	expr := &ast.FieldExpr{Receiver: receiver}
	if !p.expectPeek(token.Ident) {
		return expr
	}
	expr.Name = p.cur.Literal
	return expr
}

// nextToken advances the current and lookahead tokens.
func (p *Parser) nextToken() {
	p.cur = p.peek
	p.peek = p.l.NextToken()
}

// expectCur reports whether the current token has type t.
func (p *Parser) expectCur(t token.Type) bool {
	if p.cur.Type == t {
		return true
	}
	p.errorf("expected %s, got %s", t, p.cur.Type)
	return false
}

// expectPeek advances if the lookahead token has type t.
func (p *Parser) expectPeek(t token.Type) bool {
	if p.peek.Type == t {
		p.nextToken()
		return true
	}
	p.errorf("expected next token %s, got %s", t, p.peek.Type)
	return false
}

// curPrecedence returns the precedence of the current token.
func (p *Parser) curPrecedence() int {
	if prec, ok := precedences[p.cur.Type]; ok {
		return prec
	}
	return lowest
}

// peekPrecedence returns the precedence of the lookahead token.
func (p *Parser) peekPrecedence() int {
	if prec, ok := precedences[p.peek.Type]; ok {
		return prec
	}
	return lowest
}

// errorf records a parse error at the current token.
func (p *Parser) errorf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	p.errors = append(p.errors, fmt.Sprintf("error: %s at %d:%d", message, p.cur.Line, p.cur.Column))
}
