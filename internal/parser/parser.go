package parser

import (
	"fmt"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/token"
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
		case token.Import:
			program.Decls = append(program.Decls, p.parseImportDecl())
			p.nextToken()
		case token.Public:
			program.Decls = append(program.Decls, p.parsePublicDecl())
			p.nextToken()
		case token.Function:
			program.Decls = append(program.Decls, p.parseFunctionDecl())
			p.nextToken()
		case token.Unsafe:
			program.Decls = append(program.Decls, p.parseUnsafeDecl())
			p.nextToken()
		case token.Extern:
			program.Decls = append(program.Decls, p.parseExternDecl(false))
			p.nextToken()
		case token.Struct:
			program.Decls = append(program.Decls, p.parseStructDecl())
			p.nextToken()
		case token.Enum:
			program.Decls = append(program.Decls, p.parseEnumDecl())
			p.nextToken()
		case token.Union:
			program.Decls = append(program.Decls, p.parseUnionDecl())
			p.nextToken()
		case token.Contract:
			program.Decls = append(program.Decls, p.parseContractDecl())
			p.nextToken()
		case token.Impl:
			program.Decls = append(program.Decls, p.parseImplDecl())
			p.nextToken()
		case token.Satisfy:
			program.Decls = append(program.Decls, p.parseSatisfyDecl())
			p.nextToken()
		default:
			p.errorf("expected declaration, got %s", p.cur.Type)
			p.nextToken()
		}
	}
	return program
}

// parseImportDecl parses an explicit top-level module import.
func (p *Parser) parseImportDecl() ast.Decl {
	decl := &ast.ImportDecl{Span: p.curSpan()}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Path = append(decl.Path, p.cur.Literal)
	decl.Span.End = p.cur.End
	for p.peek.Type == token.DoubleColon {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return decl
		}
		decl.Path = append(decl.Path, p.cur.Literal)
		decl.Span.End = p.cur.End
	}
	p.expectStatementTerminator("import declaration")
	return decl
}

// parsePublicDecl parses public top-level declarations.
func (p *Parser) parsePublicDecl() ast.Decl {
	p.nextToken()
	decl := p.parseTopLevelDecl()
	setPublicDecl(decl)
	return decl
}

// parseTopLevelDecl parses one declaration whose starting token is current.
func (p *Parser) parseTopLevelDecl() ast.Decl {
	switch p.cur.Type {
	case token.Function:
		return p.parseFunctionDecl()
	case token.Unsafe:
		return p.parseUnsafeDecl()
	case token.Extern:
		return p.parseExternDecl(false)
	case token.Struct:
		return p.parseStructDecl()
	case token.Enum:
		return p.parseEnumDecl()
	case token.Union:
		return p.parseUnionDecl()
	case token.Contract:
		return p.parseContractDecl()
	default:
		p.errorf("expected public declaration, got %s", p.cur.Type)
		return &ast.FunctionDecl{Public: true}
	}
}

// setPublicDecl marks declarations that support public visibility.
func setPublicDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FunctionDecl:
		d.Public = true
	case *ast.StructDecl:
		d.Public = true
	case *ast.EnumDecl:
		d.Public = true
	case *ast.UnionDecl:
		d.Public = true
	case *ast.ContractDecl:
		d.Public = true
	}
}

// parseUnsafeDecl parses unsafe top-level declarations.
func (p *Parser) parseUnsafeDecl() ast.Decl {
	switch p.peek.Type {
	case token.Function:
		p.nextToken()
		decl := p.parseFunctionDecl()
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			fn.Unsafe = true
		}
		return decl
	case token.Extern:
		p.nextToken()
		return p.parseExternDecl(true)
	default:
		p.errorf("expected fn or extern after unsafe, got %s", p.peek.Type)
		return &ast.FunctionDecl{Unsafe: true}
	}
}

// parseExternDecl parses extern "abi" fn declarations.
func (p *Parser) parseExternDecl(unsafe bool) ast.Decl {
	fn := &ast.FunctionDecl{Unsafe: unsafe}
	if !p.expectPeek(token.String) {
		return fn
	}
	fn.ExternABI = p.cur.Literal
	if !p.expectPeek(token.Function) {
		return fn
	}
	return p.parseFunctionSignature(fn, false)
}

// parseFunctionDecl parses a top-level function declaration.
func (p *Parser) parseFunctionDecl() ast.Decl {
	return p.parseFunctionSignature(&ast.FunctionDecl{}, true)
}

// parseFunctionSignature parses a function declaration after the fn token.
func (p *Parser) parseFunctionSignature(fn *ast.FunctionDecl, requireBody bool) ast.Decl {
	fn.Span = p.curSpan()
	if !p.expectPeek(token.Ident) {
		return fn
	}
	fn.Name = p.cur.Literal
	fn.Span.End = p.cur.End
	if !p.expectPeek(token.LParen) {
		return fn
	}
	fn.Params = p.parseParams()
	if !p.expectCur(token.RParen) {
		return fn
	}
	if p.peek.Type == token.Arrow {
		p.nextToken()
		p.nextToken()
		fn.ReturnType = p.parseTypeName()
		if fn.ReturnType == "" {
			return fn
		}
	}
	if !requireBody {
		return fn
	}
	if !p.expectPeek(token.LBrace) {
		return fn
	}
	fn.Body = p.parseBlockStmt()
	return fn
}

// parseContractDecl parses a contract with method requirements.
func (p *Parser) parseContractDecl() ast.Decl {
	decl := &ast.ContractDecl{Span: p.curSpan()}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	decl.Span.End = p.cur.End
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Methods = p.parseContractMethods()
	return decl
}

// parseContractMethods parses fn signatures inside a contract.
func (p *Parser) parseContractMethods() []*ast.FunctionDecl {
	methods := []*ast.FunctionDecl{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		if p.cur.Type != token.Function {
			p.errorf("expected contract method, got %s", p.cur.Type)
			return methods
		}
		method := p.parseFunctionSignature(&ast.FunctionDecl{}, false)
		if fn, ok := method.(*ast.FunctionDecl); ok {
			methods = append(methods, fn)
		}
		if p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return methods
}

// parseImplDecl parses an impl block with method bodies.
func (p *Parser) parseImplDecl() ast.Decl {
	decl := &ast.ImplDecl{}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.TypeName = p.cur.Literal
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Methods = p.parseImplMethods()
	return decl
}

// parseImplMethods parses method declarations inside an impl block.
func (p *Parser) parseImplMethods() []*ast.FunctionDecl {
	methods := []*ast.FunctionDecl{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		if p.cur.Type != token.Function {
			p.errorf("expected impl method, got %s", p.cur.Type)
			return methods
		}
		method := p.parseFunctionSignature(&ast.FunctionDecl{}, true)
		if fn, ok := method.(*ast.FunctionDecl); ok {
			methods = append(methods, fn)
		}
		p.nextToken()
	}
	return methods
}

// parseSatisfyDecl parses an explicit contract satisfaction declaration.
func (p *Parser) parseSatisfyDecl() ast.Decl {
	decl := &ast.SatisfyDecl{}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.ContractName = p.cur.Literal
	if !p.expectPeek(token.For) {
		return decl
	}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.TypeName = p.cur.Literal
	return decl
}

// parseStructDecl parses a top-level struct declaration.
func (p *Parser) parseStructDecl() ast.Decl {
	decl := &ast.StructDecl{Span: p.curSpan()}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	decl.Span.End = p.cur.End
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Fields = p.parseStructFields()
	return decl
}

// parseStructFields parses newline- or comma-separated struct fields.
func (p *Parser) parseStructFields() []ast.Field {
	fields := []ast.Field{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		field, ok := p.parseStructField()
		if !ok {
			return fields
		}
		fields = append(fields, field)
		if p.peek.Type == token.Comma || p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return fields
}

// parseStructField parses one struct field declaration.
func (p *Parser) parseStructField() (ast.Field, bool) {
	field := ast.Field{Span: p.curSpan()}
	if p.cur.Type == token.Public {
		field.Public = true
		p.nextToken()
		field.Span = p.curSpan()
	}
	if p.cur.Type != token.Ident {
		p.errorf("expected field name, got %s", p.cur.Type)
		return field, false
	}
	field.Name = p.cur.Literal
	field.Span.End = p.cur.End
	if !p.expectPeek(token.Colon) {
		return field, false
	}
	p.nextToken()
	if p.cur.Type == token.Amp {
		field.Borrow = true
		p.nextToken()
		if p.cur.Type == token.Mut {
			field.MutBorrow = true
			p.nextToken()
		}
	}
	field.TypeName = p.parseTypeName()
	if field.TypeName == "" {
		return field, false
	}
	return field, true
}

// parseEnumDecl parses a top-level Zig/C-style tag enum declaration.
func (p *Parser) parseEnumDecl() ast.Decl {
	decl := &ast.EnumDecl{Span: p.curSpan()}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	decl.Span.End = p.cur.End
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Tags = p.parseEnumTags()
	return decl
}

// parseEnumTags parses newline-, comma-, or semicolon-separated enum tags.
func (p *Parser) parseEnumTags() []string {
	tags := []string{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		if p.cur.Type != token.Ident {
			p.errorf("expected enum tag, got %s", p.cur.Type)
			return tags
		}
		tags = append(tags, p.cur.Literal)
		if p.peek.Type == token.Comma || p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return tags
}

// parseUnionDecl parses a top-level tagged union declaration.
func (p *Parser) parseUnionDecl() ast.Decl {
	decl := &ast.UnionDecl{Span: p.curSpan()}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	decl.Span.End = p.cur.End
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Variants = p.parseUnionVariants()
	return decl
}

// parseUnionVariants parses tagged union variants until the closing brace.
func (p *Parser) parseUnionVariants() []ast.UnionVariant {
	variants := []ast.UnionVariant{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		variant, ok := p.parseUnionVariant()
		if !ok {
			return variants
		}
		variants = append(variants, variant)
		if p.peek.Type == token.Comma || p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return variants
}

// parseUnionVariant parses one tagged union variant declaration.
func (p *Parser) parseUnionVariant() (ast.UnionVariant, bool) {
	variant := ast.UnionVariant{}
	if p.cur.Type != token.Ident {
		p.errorf("expected union variant, got %s", p.cur.Type)
		return variant, false
	}
	variant.Name = p.cur.Literal
	if p.peek.Type != token.LParen {
		return variant, true
	}
	p.nextToken()
	p.nextToken()
	variant.Payload = p.parseTypeName()
	if variant.Payload == "" || !p.expectPeek(token.RParen) {
		return variant, false
	}
	return variant, true
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
		if p.cur.Type == token.Comptime {
			param.Comptime = true
			p.nextToken()
		}
		if p.cur.Type != token.Ident {
			p.errorf("expected parameter name, got %s", p.cur.Type)
			return params
		}
		param.Name = p.cur.Literal
		if !p.expectPeek(token.Colon) {
			return params
		}
		p.nextToken()
		if p.cur.Type == token.Amp {
			param.Borrow = true
			p.nextToken()
			if p.cur.Type == token.Mut {
				param.MutBorrow = true
				p.nextToken()
			}
		}
		param.TypeName = p.parseTypeName()
		if param.TypeName == "" {
			return params
		}
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
	if stmt, ok := p.parseKeywordStatement(); ok {
		return stmt
	}
	if p.cur.Type == token.Ident && p.peek.Type == token.Colon {
		return p.parseLabeledStmt()
	}
	expr := p.parseExpression(lowest)
	if p.peek.Type == token.Assign {
		return p.parseAssignStmt(expr)
	}
	p.expectStatementTerminator("expression statement")
	return &ast.ExprStmt{Expr: expr}
}

// parseKeywordStatement parses statements that start with reserved keywords.
func (p *Parser) parseKeywordStatement() (ast.Statement, bool) {
	switch p.cur.Type {
	case token.Let:
		return p.parseLetStmt(false), true
	case token.Var:
		return p.parseLetStmt(true), true
	case token.Return:
		return p.parseReturnStmt(), true
	case token.If:
		return p.parseIfStmt(), true
	case token.While:
		return p.parseWhileStmt(""), true
	case token.For:
		return p.parseForStmt(""), true
	case token.Break:
		return p.parseBreakStmt(), true
	case token.Continue:
		return p.parseContinueStmt(), true
	case token.Match:
		return p.parseMatchStmt(), true
	case token.Unsafe:
		return p.parseUnsafeStmt(), true
	case token.Comptime:
		if p.peek.Type == token.If {
			return p.parseComptimeIfStmt(), true
		}
	}
	return nil, false
}

// parseLabeledStmt parses a loop label followed by a loop statement.
func (p *Parser) parseLabeledStmt() ast.Statement {
	label := p.cur.Literal
	p.nextToken()
	p.nextToken()
	switch p.cur.Type {
	case token.While:
		return p.parseWhileStmt(label)
	case token.For:
		return p.parseForStmt(label)
	default:
		p.errorf("label `%s` must be attached to while or for", label)
		return &ast.ExprStmt{Expr: &ast.IdentExpr{Name: "<error>"}}
	}
}

// parseComptimeIfStmt parses a comptime-selected if statement.
func (p *Parser) parseComptimeIfStmt() ast.Statement {
	stmt := &ast.ComptimeIfStmt{}
	if !p.expectPeek(token.If) {
		return stmt
	}
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

// parseUnsafeStmt parses an unsafe statement block.
func (p *Parser) parseUnsafeStmt() ast.Statement {
	stmt := &ast.UnsafeStmt{}
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStmt()
	return stmt
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
	p.expectStatementTerminator("let statement")
	return stmt
}

// parseAssignStmt parses assignment to a binding, field, or dereference target.
func (p *Parser) parseAssignStmt(target ast.Expression) ast.Statement {
	stmt := &ast.AssignStmt{Target: target}
	p.nextToken()
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	p.expectStatementTerminator("assignment")
	return stmt
}

// parseReturnStmt parses an explicit return statement.
func (p *Parser) parseReturnStmt() ast.Statement {
	stmt := &ast.ReturnStmt{}
	if p.peek.Type == token.Semicolon {
		p.nextToken()
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	p.expectStatementTerminator("return statement")
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
func (p *Parser) parseWhileStmt(label string) ast.Statement {
	stmt := &ast.WhileStmt{Label: label}
	p.nextToken()
	stmt.Condition = p.parseExpression(lowest)
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStmt()
	return stmt
}

// parseForStmt parses a bounded integer range loop.
func (p *Parser) parseForStmt(label string) ast.Statement {
	stmt := &ast.ForStmt{Label: label}
	p.nextToken()
	stmt.Start = p.parseExpression(lowest)
	if !p.expectPeek(token.Range) {
		return stmt
	}
	p.nextToken()
	stmt.End = p.parseExpression(lowest)
	if !p.expectPeek(token.Pipe) || !p.expectPeek(token.Ident) {
		return stmt
	}
	stmt.Name = p.cur.Literal
	if !p.expectPeek(token.Pipe) || !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Body = p.parseBlockStmt()
	return stmt
}

// parseBreakStmt parses break with an optional target label.
func (p *Parser) parseBreakStmt() ast.Statement {
	stmt := &ast.BreakStmt{Label: p.parseOptionalBranchLabel()}
	p.expectStatementTerminator("break statement")
	return stmt
}

// parseContinueStmt parses continue with an optional target label.
func (p *Parser) parseContinueStmt() ast.Statement {
	stmt := &ast.ContinueStmt{Label: p.parseOptionalBranchLabel()}
	p.expectStatementTerminator("continue statement")
	return stmt
}

// parseOptionalBranchLabel parses Zig-style :label branch targets.
func (p *Parser) parseOptionalBranchLabel() string {
	if p.peek.Type != token.Colon {
		return ""
	}
	p.nextToken()
	if !p.expectPeek(token.Ident) {
		return ""
	}
	return p.cur.Literal
}

// expectStatementTerminator requires Zig/C-style semicolons after simple statements.
func (p *Parser) expectStatementTerminator(context string) bool {
	if p.cur.Type == token.Semicolon {
		return true
	}
	if p.peek.Type == token.Semicolon {
		p.nextToken()
		return true
	}
	p.errorf("expected `;` after %s", context)
	return false
}

// parseMatchStmt parses a simple enum tag match statement.
func (p *Parser) parseMatchStmt() ast.Statement {
	stmt := &ast.MatchStmt{}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	if !p.expectPeek(token.LBrace) {
		return stmt
	}
	stmt.Arms = p.parseMatchArms()
	return stmt
}

// parseMatchArms parses enum tag arms until the closing brace.
func (p *Parser) parseMatchArms() []ast.MatchArm {
	arms := []ast.MatchArm{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		arm, ok := p.parseMatchArm()
		if !ok {
			return arms
		}
		arms = append(arms, arm)
		if p.peek.Type == token.Comma || p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return arms
}

// parseMatchArm parses one enum tag arm.
func (p *Parser) parseMatchArm() (ast.MatchArm, bool) {
	arm := ast.MatchArm{}
	if p.cur.Type != token.Ident {
		p.errorf("expected match tag, got %s", p.cur.Type)
		return arm, false
	}
	arm.Tag = p.cur.Literal
	if p.peek.Type == token.LParen {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return arm, false
		}
		arm.Binding = p.cur.Literal
		if !p.expectPeek(token.RParen) {
			return arm, false
		}
	}
	if !p.expectPeek(token.FatArrow) {
		return arm, false
	}
	p.nextToken()
	arm.Body = p.parseStatement()
	return arm, true
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
	token.Eq:          equals,
	token.NotEq:       equals,
	token.LT:          lessGreater,
	token.LTE:         lessGreater,
	token.GT:          lessGreater,
	token.GTE:         lessGreater,
	token.Plus:        sum,
	token.Minus:       sum,
	token.Asterisk:    product,
	token.Slash:       product,
	token.Percent:     product,
	token.LParen:      call,
	token.Dot:         field,
	token.DoubleColon: field,
}

// parseExpression parses an expression using Pratt parser precedence.
func (p *Parser) parseExpression(precedence int) ast.Expression {
	left := p.parsePrefixExpression()
	for p.peek.Type != token.Semicolon {
		if precedence == lowest && p.peek.Type == token.LBrace && canStructLiteral(left) {
			left = p.parseStructLiteralExpr(left.String())
			continue
		}
		if precedence >= p.peekPrecedence() {
			return left
		}
		switch p.peek.Type {
		case token.Plus, token.Minus, token.Asterisk, token.Slash, token.Percent,
			token.Eq, token.NotEq, token.LTE, token.GT, token.GTE:
			p.nextToken()
			left = p.parseBinaryExpr(left)
		case token.LParen:
			p.nextToken()
			left = p.parseCallExpr(left)
		case token.LT:
			if !canTypeApply(left) {
				p.nextToken()
				left = p.parseBinaryExpr(left)
				continue
			}
			p.nextToken()
			left = p.parseTypeApplyExpr(left)
		case token.Dot:
			p.nextToken()
			left = p.parseFieldExpr(left, false)
		case token.DoubleColon:
			p.nextToken()
			left = p.parseFieldExpr(left, true)
		default:
			return left
		}
	}
	return left
}

// canStructLiteral reports whether expr may be followed by a struct literal body.
func canStructLiteral(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return startsUpper(e.Name)
	case *ast.FieldExpr:
		return e.Namespace
	default:
		return false
	}
}

// parsePrefixExpression parses literals, identifiers, and unary expressions.
func (p *Parser) parsePrefixExpression() ast.Expression {
	switch p.cur.Type {
	case token.Ident:
		return p.parseIdentPrefixExpression()
	case token.Int:
		return &ast.IntExpr{Value: p.cur.Literal}
	case token.String:
		return &ast.StringExpr{Value: p.cur.Literal}
	case token.True:
		return &ast.BoolExpr{Value: true}
	case token.False:
		return &ast.BoolExpr{Value: false}
	case token.If:
		return p.parseIfExpr()
	case token.Comptime:
		p.nextToken()
		return &ast.ComptimeExpr{Expr: p.parseExpression(lowest)}
	case token.Try:
		p.nextToken()
		return &ast.TryExpr{Value: p.parseExpression(prefix)}
	case token.Amp:
		p.nextToken()
		op := "&"
		if p.cur.Type == token.Mut {
			op = "&mut"
			p.nextToken()
		}
		return &ast.PrefixExpr{Operator: op, Right: p.parseExpression(prefix)}
	case token.Bang, token.Minus:
		op := p.cur.Literal
		p.nextToken()
		return &ast.PrefixExpr{Operator: op, Right: p.parseExpression(prefix)}
	case token.LParen:
		p.nextToken()
		left := p.parseExpression(lowest)
		p.expectPeek(token.RParen)
		return left
	default:
		p.errorf("expected expression, got %s", p.cur.Type)
		return &ast.IdentExpr{Name: "<error>"}
	}
}

// parseIfExpr parses a value-producing if expression.
func (p *Parser) parseIfExpr() ast.Expression {
	expr := &ast.IfExpr{}
	p.nextToken()
	expr.Condition = p.parseExpression(lowest)
	if !p.expectPeek(token.LBrace) {
		return expr
	}
	expr.Consequence = p.parseBlockStmt()
	if !p.expectPeek(token.Else) {
		return expr
	}
	if !p.expectPeek(token.LBrace) {
		return expr
	}
	expr.Alternative = p.parseBlockStmt()
	return expr
}

// parseIdentPrefixExpression parses identifiers and identifier-led special forms.
func (p *Parser) parseIdentPrefixExpression() ast.Expression {
	if p.cur.Literal == "cast" && p.peek.Type == token.LT {
		return p.parseCastExpr()
	}
	if p.cur.Literal == "arena" && p.peek.Type == token.LT {
		return p.parseArenaNewExpr()
	}
	if p.peek.Type == token.LBrace && startsUpper(p.cur.Literal) {
		return p.parseStructLiteralExpr(p.cur.Literal)
	}
	return &ast.IdentExpr{Name: p.cur.Literal}
}

// parseCastExpr parses cast<T>(value).
func (p *Parser) parseCastExpr() ast.Expression {
	expr := &ast.CastExpr{}
	if !p.expectPeek(token.LT) {
		return expr
	}
	p.nextToken()
	expr.TargetType = p.parseTypeName()
	if expr.TargetType == "" || !p.expectPeek(token.GT) {
		return expr
	}
	if !p.expectPeek(token.LParen) {
		return expr
	}
	p.nextToken()
	expr.Value = p.parseExpression(lowest)
	p.expectPeek(token.RParen)
	return expr
}

// parseTypeName parses a plain, pointer, or generic type name.
func (p *Parser) parseTypeName() string {
	if p.cur.Type == token.Bang {
		p.nextToken()
		inner := p.parseTypeName()
		if inner == "" {
			return ""
		}
		return "!" + inner
	}
	if p.cur.Type == token.LBracket {
		if !p.expectPeek(token.RBracket) {
			return ""
		}
		p.nextToken()
		arg := p.parseTypeArg()
		if arg == "" {
			return ""
		}
		return "[]" + arg
	}
	nullable := false
	if p.cur.Type == token.Question {
		nullable = true
		p.nextToken()
	}
	if p.cur.Type != token.Ident {
		p.errorf("expected type, got %s", p.cur.Type)
		return ""
	}
	name := p.parseTypeBaseName()
	if p.peek.Type == token.Bang {
		p.nextToken()
		p.nextToken()
		success := p.parseTypeName()
		if success == "" {
			return ""
		}
		return name + "!" + success
	}
	if p.peek.Type != token.LT {
		if nullable {
			return "?" + name
		}
		return name
	}
	p.nextToken()
	p.nextToken()
	args := p.parseTypeArgList()
	if args == "" || !p.expectTypeClose() {
		return ""
	}
	out := fmt.Sprintf("%s<%s>", name, args)
	if nullable {
		return "?" + out
	}
	return out
}

// parseTypeBaseName parses an identifier or namespace-qualified type base.
func (p *Parser) parseTypeBaseName() string {
	parts := []string{p.cur.Literal}
	for p.peek.Type == token.DoubleColon {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return strings.Join(parts, "::")
		}
		parts = append(parts, p.cur.Literal)
	}
	return strings.Join(parts, "::")
}

// parseTypeArgList parses one or more comma-separated generic type arguments.
func (p *Parser) parseTypeArgList() string {
	args := []string{}
	for {
		arg := p.parseTypeArg()
		if arg == "" {
			return ""
		}
		args = append(args, arg)
		if p.peek.Type != token.Comma {
			break
		}
		p.nextToken()
		p.nextToken()
	}
	return strings.Join(args, ", ")
}

// expectTypeClose consumes or accepts the closing generic angle bracket.
func (p *Parser) expectTypeClose() bool {
	if p.cur.Type == token.GT {
		if p.peek.Type == token.GT {
			p.nextToken()
		}
		return true
	}
	return p.expectPeek(token.GT)
}

// parseTypeArg parses a generic type argument.
func (p *Parser) parseTypeArg() string {
	if p.cur.Type == token.Ident && p.cur.Literal == "const" {
		p.nextToken()
		inner := p.parseTypeName()
		if inner == "" {
			return ""
		}
		return "const " + inner
	}
	return p.parseTypeName()
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

// parseTypeApplyExpr parses Namespace::Item<T> before a constructor call.
func (p *Parser) parseTypeApplyExpr(callee ast.Expression) ast.Expression {
	expr := &ast.TypeApplyExpr{Callee: callee}
	p.nextToken()
	expr.TypeArg = p.parseTypeArgList()
	if expr.TypeArg == "" || !p.expectTypeClose() {
		return expr
	}
	return expr
}

// parseArenaNewExpr parses arena<T>().
func (p *Parser) parseArenaNewExpr() ast.Expression {
	expr := &ast.ArenaNewExpr{}
	if !p.expectPeek(token.LT) {
		return expr
	}
	if !p.expectPeek(token.Ident) {
		return expr
	}
	expr.TypeName = p.cur.Literal
	if !p.expectPeek(token.GT) {
		return expr
	}
	if !p.expectPeek(token.LParen) || !p.expectPeek(token.RParen) {
		return expr
	}
	return expr
}

// parseStructLiteralExpr parses Type { field: value }.
func (p *Parser) parseStructLiteralExpr(typeName string) ast.Expression {
	expr := &ast.StructLiteralExpr{
		TypeName: typeName,
		Span:     p.curSpan(),
	}
	p.nextToken()
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		field, ok := p.parseFieldValue()
		if !ok {
			return expr
		}
		expr.Fields = append(expr.Fields, field)
		if p.peek.Type == token.Comma || p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return expr
}

// parseFieldValue parses one field initializer.
func (p *Parser) parseFieldValue() (ast.FieldValue, bool) {
	field := ast.FieldValue{Span: p.curSpan()}
	if p.cur.Type != token.Ident {
		p.errorf("expected field name, got %s", p.cur.Type)
		return field, false
	}
	field.Name = p.cur.Literal
	field.Span.End = p.cur.End
	if !p.expectPeek(token.Colon) {
		return field, false
	}
	p.nextToken()
	field.Value = p.parseExpression(lowest)
	return field, true
}

// parseFieldExpr parses field access on an expression.
func (p *Parser) parseFieldExpr(receiver ast.Expression, namespace bool) ast.Expression {
	if !namespace && p.peek.Type == token.Asterisk {
		p.nextToken()
		return &ast.DerefExpr{Receiver: receiver}
	}
	expr := &ast.FieldExpr{Receiver: receiver, Namespace: namespace}
	if !p.expectPeek(token.Ident) {
		return expr
	}
	expr.Name = p.cur.Literal
	expr.Span = p.curSpan()
	return expr
}

// curSpan returns the current token's source span.
func (p *Parser) curSpan() ast.Span {
	return ast.Span{
		Start: p.cur.Start, End: p.cur.End,
		Line: p.cur.Line, Column: p.cur.Column,
	}
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

// startsUpper reports whether name follows the v0 struct literal convention.
func startsUpper(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// canTypeApply reports whether expr may be followed by a constructor type argument.
func canTypeApply(expr ast.Expression) bool {
	field, ok := expr.(*ast.FieldExpr)
	return ok && field.Namespace
}
