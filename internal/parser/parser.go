package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kizu-lang/kizu/internal/ast"
	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/token"
	"github.com/kizu-lang/kizu/internal/typ"
)

// Parser consumes tokens and builds a Kizu AST.
type Parser struct {
	l      *lexer.Lexer
	cur    token.Token
	peek   token.Token
	errors []Diagnostic
	// safety is the `// SAFETY:` text of the statement being parsed. It is the
	// justification every `unsafe` marker in that statement is stamped with.
	safety string
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
	errs := make([]string, 0, len(p.errors))
	for _, diag := range p.errors {
		errs = append(errs, diag.Error())
	}
	return errs
}

// Diagnostics returns parse diagnostics collected so far.
func (p *Parser) Diagnostics() []Diagnostic {
	return p.errors
}

// commentText normalizes the comment lines the lexer attached to a token. Both
// kinds go through here, so "the comment is empty" means one thing whether it
// was written as `///` or as `// SAFETY:`.
func commentText(lines []string) string {
	return strings.TrimSpace(strings.Join(lines, "\n"))
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
		case token.Ident:
			program.Decls = append(program.Decls, p.parseIdentLedDecl()...)
			p.nextToken()
		case token.Function:
			program.Decls = append(program.Decls, p.parseFunctionDecl())
			p.nextToken()
		case token.Unsafe:
			program.Decls = append(program.Decls, p.parseUnsafeDecl(commentText(p.cur.DocComments)))
			p.nextToken()
		case token.Extern:
			program.Decls = append(program.Decls, p.parseExternDecl())
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
		default:
			p.errorExpectedDeclaration()
			p.nextToken()
		}
	}
	return program
}

// parseImportDecl parses an explicit top-level module import.
func (p *Parser) parseImportDecl() ast.Decl {
	decl := &ast.ImportDecl{}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Path = append(decl.Path, p.cur.Literal)
	for p.peek.Type == token.DoubleColon {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return decl
		}
		decl.Path = append(decl.Path, p.cur.Literal)
	}
	p.expectStatementTerminator("import declaration")
	return decl
}

// parsePublicDecl parses public top-level declarations.
func (p *Parser) parsePublicDecl() ast.Decl {
	docs := commentText(p.cur.DocComments)
	p.nextToken()
	decl := p.parseTopLevelDeclWithDoc(docs)
	setPublicDecl(decl)
	return decl
}

// parseTopLevelDeclWithDoc parses one declaration and applies already-read docs.
func (p *Parser) parseTopLevelDeclWithDoc(docs string) ast.Decl {
	switch p.cur.Type {
	case token.Function:
		return p.parseFunctionDeclWithDoc(docs)
	case token.Unsafe:
		return p.parseUnsafeDecl(docs)
	case token.Extern:
		return p.parseExternDeclWithDoc(docs)
	case token.Struct:
		return p.parseStructDeclWithDoc(docs)
	case token.Enum:
		return p.parseEnumDeclWithDoc(docs)
	case token.Union:
		return p.parseUnionDeclWithDoc(docs)
	case token.Contract:
		return p.parseContractDecl()
	case token.Ident:
		if p.startsErrorSetDecl() {
			return p.parseErrorSetDeclWithDoc(docs)
		}
		p.errorf("expected public declaration, got %s", tokenDescription(p.cur))
		return functionStub(true, docs)
	default:
		p.errorf("expected public declaration, got %s", tokenDescription(p.cur))
		return functionStub(true, docs)
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
	case *ast.ErrorSetDecl:
		d.Public = true
	}
}

// functionStub is the declaration the parser hands back when it has reported an
// error and has to keep going. It stands in for the function that failed to
// parse, so the caller is given a declaration rather than nil.
func functionStub(public bool, docs string) *ast.FunctionDecl {
	return &ast.FunctionDecl{
		FunctionSignature: ast.FunctionSignature{Public: public},
		Doc:               docs,
	}
}

// parseUnsafeDecl parses the declarations `unsafe` can introduce. In both the
// marker says a memory safety obligation the compiler cannot check is owned by
// someone else: by the caller for a function, by the writer of a field for a
// struct.
func (p *Parser) parseUnsafeDecl(docs string) ast.Decl {
	switch p.peek.Type {
	case token.Struct:
		p.nextToken()
		decl := p.parseStructDeclWithDoc(docs)
		decl.RequiresUnsafe = true
		return decl
	case token.Function:
		fn := functionStub(false, docs)
		fn.RequiresUnsafe = true
		p.nextToken()
		return p.parseFunctionAfterFn(fn, true)
	default:
		p.errorf("expected fn or struct after unsafe, got %s", tokenDescription(p.peek))
		return functionStub(false, docs)
	}
}

// parseExternDecl parses extern "abi" fn declarations.
func (p *Parser) parseExternDecl() ast.Decl {
	return p.parseExternDeclWithDoc(commentText(p.cur.DocComments))
}

// parseExternDeclWithDoc parses extern "abi" fn declarations with attached docs.
func (p *Parser) parseExternDeclWithDoc(docs string) ast.Decl {
	fn := &ast.FunctionDecl{Doc: docs}
	if !p.expectPeek(token.String) {
		return fn
	}
	fn.ExternABI = p.cur.Literal
	if !p.expectPeek(token.Function) {
		return fn
	}
	return p.parseFunctionAfterFn(fn, false)
}

// parseFunctionDecl parses a top-level function declaration.
func (p *Parser) parseFunctionDecl() ast.Decl {
	return p.parseFunctionDeclWithDoc(commentText(p.cur.DocComments))
}

// parseFunctionDeclWithDoc parses a top-level function declaration with attached docs.
func (p *Parser) parseFunctionDeclWithDoc(docs string) ast.Decl {
	return p.parseFunctionAfterFn(&ast.FunctionDecl{Doc: docs}, true)
}

// parseTestDecl parses a top-level test block.
func (p *Parser) parseTestDecl() ast.Decl {
	decl := &ast.TestDecl{}
	if !p.expectPeek(token.String) {
		return decl
	}
	decl.Name = p.cur.Literal
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Body = p.parseBlockStmt()
	return decl
}

// parseFunctionAfterFn parses a function declaration after the fn token. It is
// not named for ast.FunctionSignature: it parses the body too, which is the one
// thing that type does not hold.
func (p *Parser) parseFunctionAfterFn(fn *ast.FunctionDecl, requireBody bool) ast.Decl {
	if p.peek.Type == token.LParen && !p.parseReceiver(fn) {
		return fn
	}
	if !p.expectPeek(token.Ident) {
		return fn
	}
	fn.Name = p.cur.Literal
	if p.peek.Type == token.LT {
		p.nextToken()
		fn.StaticParams = p.parseStaticParamList()
		if len(fn.StaticParams) == 0 || !p.expectTypeClose() {
			return fn
		}
	}
	if !p.expectPeek(token.LParen) {
		return fn
	}
	fn.Params = append(fn.Params, p.parseParams()...)
	if !p.expectCur(token.RParen) {
		return fn
	}
	if !p.parseReturnClause(fn) {
		return fn
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
	decl := &ast.ContractDecl{}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
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
			p.errorf("expected contract method, got %s", tokenDescription(p.cur))
			return methods
		}
		method := p.parseFunctionAfterFn(&ast.FunctionDecl{}, false)
		if fn, ok := method.(*ast.FunctionDecl); ok {
			if fn.Receiver {
				p.errorf("a contract method takes no receiver;" +
					" it says what a method looks like, not what it is on")
				return methods
			}
			methods = append(methods, fn)
		}
		if p.peek.Type == token.Semicolon {
			p.nextToken()
		}
		p.nextToken()
	}
	return methods
}

// parseImplDecl parses the one-line assertion that a type satisfies a contract.
// A type satisfies one by having the methods, so this carries no body: it asks
// for the check to run here, where a reader can see the intent written down.
func (p *Parser) parseImplDecl() ast.Decl {
	decl := &ast.ImplDecl{}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	firstName := p.cur.Literal
	if p.peek.Type != token.For {
		p.errorf("expected `impl <contract> for %s;`;"+
			" a method on a type is `fn (self: %s) name(...)`", firstName, firstName)
		return decl
	}
	decl.ContractName = firstName
	p.nextToken()
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.TypeName = p.cur.Literal
	if p.peek.Type == token.LBrace {
		p.errorf("`impl %s for %s` takes no body;"+
			" write the methods as `fn (self: %s) name(...)` and end this with `;`",
			decl.ContractName, decl.TypeName, decl.TypeName)
		return decl
	}
	p.expectStatementTerminator("impl declaration")
	return decl
}

// parseReturnClause reads the `-> T` return type. A `borrows` clause is no
// longer part of the language (ADR-0098): return provenance is derived from
// the signature, so a written clause falls through to a plain parse error.
func (p *Parser) parseReturnClause(fn *ast.FunctionDecl) bool {
	if p.peek.Type != token.Arrow {
		return true
	}
	p.nextToken()
	p.nextToken()
	fn.ReturnType = p.parseTypeName()
	return fn.ReturnType != nil
}

// parseReceiver reads the `(self: T)` slot a method declares before its name,
// and records the receiver as the function's first parameter. A method reads as
// a function that takes its receiver first, which is what it is; the slot is
// what says the name belongs to the receiver's type rather than to the module.
func (p *Parser) parseReceiver(fn *ast.FunctionDecl) bool {
	p.nextToken()
	params := p.parseParams()
	if !p.expectCur(token.RParen) {
		return false
	}
	if len(params) != 1 {
		p.errorf("expected one receiver in `fn (...)`, got %d", len(params))
		return false
	}
	fn.Receiver = true
	fn.Params = params
	return true
}

// parseStructDecl parses a top-level struct declaration.
func (p *Parser) parseStructDecl() ast.Decl {
	return p.parseStructDeclWithDoc(commentText(p.cur.DocComments))
}

// parseStructDeclWithDoc parses a top-level struct declaration with attached
// docs. It returns the concrete declaration so a caller that has more to say
// about it — `unsafe struct` — can say it without a type assertion.
func (p *Parser) parseStructDeclWithDoc(docs string) *ast.StructDecl {
	decl := &ast.StructDecl{Doc: docs}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	if p.peek.Type == token.LT {
		p.nextToken()
		decl.TypeParams = p.parseGenericParamList()
		if len(decl.TypeParams) == 0 || !p.expectTypeClose() {
			return decl
		}
	}
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Fields = p.parseStructFields()
	return decl
}

// parseStructFields parses comma-separated struct fields.
func (p *Parser) parseStructFields() []ast.Field {
	fields := []ast.Field{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		field, ok := p.parseStructField()
		if !ok {
			return fields
		}
		fields = append(fields, field)
		if !p.consumeListDelimiter("struct field") {
			return fields
		}
		p.nextToken()
	}
	return fields
}

// parseStructField parses one struct field declaration.
func (p *Parser) parseStructField() (ast.Field, bool) {
	field := ast.Field{Doc: commentText(p.cur.DocComments)}
	if p.cur.Type == token.Public {
		field.Public = true
		p.nextToken()
	}
	if p.cur.Type != token.Ident {
		p.errorf("expected field name, got %s", tokenDescription(p.cur))
		return field, false
	}
	field.Name = p.cur.Literal
	if !p.expectPeek(token.Colon) {
		return field, false
	}
	p.nextToken()
	if p.cur.Type == token.Amp {
		field.Borrow = true
		p.nextToken()
		if p.cur.Type == token.Var {
			field.MutBorrow = true
			p.nextToken()
		}
	}
	field.TypeName = p.parseTypeName()
	if field.TypeName == nil {
		return field, false
	}
	return field, true
}

// parseEnumDecl parses a top-level Zig/C-style tag enum declaration.
func (p *Parser) parseEnumDecl() ast.Decl {
	return p.parseEnumDeclWithDoc(commentText(p.cur.DocComments))
}

// parseEnumDeclWithDoc parses a top-level enum declaration with attached docs.
func (p *Parser) parseEnumDeclWithDoc(docs string) ast.Decl {
	decl := &ast.EnumDecl{Doc: docs}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Tags, decl.TagDocs = p.parseNameList("enum tag")
	return decl
}

// parseIdentLedDecl parses the declarations that start with a plain name rather
// than a keyword, and reports none when the name starts neither.
func (p *Parser) parseIdentLedDecl() []ast.Decl {
	switch {
	case p.cur.Literal == "test":
		return []ast.Decl{p.parseTestDecl()}
	case p.startsErrorSetDecl():
		return []ast.Decl{p.parseErrorSetDecl()}
	default:
		p.errorExpectedDeclaration()
		return nil
	}
}

// startsErrorSetDecl reports whether the current `error` begins a declaration.
// `error` is not a keyword, so a declaration is told apart by what follows:
// a name and a brace.
func (p *Parser) startsErrorSetDecl() bool {
	return p.cur.Literal == "error" && p.peek.Type == token.Ident
}

// parseErrorSetDecl parses `error Name { A, B }`.
func (p *Parser) parseErrorSetDecl() ast.Decl {
	return p.parseErrorSetDeclWithDoc(commentText(p.cur.DocComments))
}

// parseErrorSetDeclWithDoc parses an error set declaration with attached docs.
func (p *Parser) parseErrorSetDeclWithDoc(docs string) ast.Decl {
	decl := &ast.ErrorSetDecl{Doc: docs}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	if !p.expectPeek(token.LBrace) {
		return decl
	}
	decl.Members, decl.MemberDocs = p.parseNameList("error name")
	return decl
}

// parseNameList parses the comma-separated names inside a brace. An enum tag and
// an error set member are written the same way, so they are read the same way,
// and label says which one a malformed list is reported as.
func (p *Parser) parseNameList(label string) ([]string, map[string]string) {
	names := []string{}
	docs := map[string]string{}
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		if p.cur.Type != token.Ident {
			p.errorf("expected %s, got %s", label, tokenDescription(p.cur))
			return names, tagDocsOrNil(docs)
		}
		names = append(names, p.cur.Literal)
		if text := commentText(p.cur.DocComments); text != "" {
			docs[p.cur.Literal] = text
		}
		if !p.consumeListDelimiter(label) {
			return names, tagDocsOrNil(docs)
		}
		p.nextToken()
	}
	return names, tagDocsOrNil(docs)
}

// tagDocsOrNil keeps empty enum doc metadata compact.
func tagDocsOrNil(tagDocs map[string]string) map[string]string {
	if len(tagDocs) == 0 {
		return nil
	}
	return tagDocs
}

// parseUnionDecl parses a top-level tagged union declaration.
func (p *Parser) parseUnionDecl() ast.Decl {
	return p.parseUnionDeclWithDoc(commentText(p.cur.DocComments))
}

// parseUnionDeclWithDoc parses a top-level union declaration with attached docs.
func (p *Parser) parseUnionDeclWithDoc(docs string) ast.Decl {
	decl := &ast.UnionDecl{Doc: docs}
	if !p.expectPeek(token.Ident) {
		return decl
	}
	decl.Name = p.cur.Literal
	if p.peek.Type == token.LT {
		p.nextToken()
		decl.TypeParams = p.parseGenericParamList()
		if len(decl.TypeParams) == 0 || !p.expectTypeClose() {
			return decl
		}
	}
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
		if !p.consumeListDelimiter("union variant") {
			return variants
		}
		p.nextToken()
	}
	return variants
}

// parseUnionVariant parses one tagged union variant declaration.
func (p *Parser) parseUnionVariant() (ast.UnionVariant, bool) {
	variant := ast.UnionVariant{Doc: commentText(p.cur.DocComments)}
	if p.cur.Type != token.Ident {
		p.errorf("expected union variant, got %s", tokenDescription(p.cur))
		return variant, false
	}
	variant.Name = p.cur.Literal
	if p.peek.Type != token.LParen {
		return variant, true
	}
	p.nextToken()
	p.nextToken()
	variant.Payload = p.parseTypeName()
	if variant.Payload == nil || !p.expectPeek(token.RParen) {
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
			// A compile-time value belongs in the `<...>` list, not among
			// values that move and borrow.
			p.errorf("compile-time parameter belongs in `<...>`, not `(...)`")
			return params
		}
		if p.cur.Type != token.Ident {
			p.errorf("expected parameter name, got %s", tokenDescription(p.cur))
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
			if p.cur.Type == token.Var {
				param.MutBorrow = true
				p.nextToken()
			}
		}
		param.TypeName = p.parseTypeName()
		if param.TypeName == nil {
			return params
		}
		params = append(params, param)

		if p.peek.Type != token.Comma {
			break
		}
		p.nextToken()
		if p.peek.Type == token.RParen {
			break
		}
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
	// A `// SAFETY:` comment justifies the statement it sits above, so it is in
	// scope until that statement ends. Restoring the enclosing one afterwards is
	// what makes a nested statement need its own comment rather than inherit.
	enclosing := p.safety
	p.safety = commentText(p.cur.Safety)
	stmt := p.parseStatementForm()
	p.safety = enclosing
	return stmt
}

// parseStatementForm parses the statement itself, once its `// SAFETY:` comment
// is in scope.
func (p *Parser) parseStatementForm() ast.Statement {
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
	return p.finishExprStmt(expr)
}

// finishExprStmt requires a terminating `;` after an expression statement,
// except when the expression is a branch value (`}`) or a match arm value (`,`).
func (p *Parser) finishExprStmt(expr ast.Expression) ast.Statement {
	switch p.peek.Type {
	case token.Semicolon:
		p.nextToken()
		return &ast.ExprStmt{Expr: expr, Semicolon: true}
	case token.RBrace, token.Comma:
		return &ast.ExprStmt{Expr: expr, Semicolon: false}
	default:
		p.errorf("expected `;` after expression statement")
		return &ast.ExprStmt{Expr: expr, Semicolon: false}
	}
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
	case token.Defer:
		return p.parseDeferStmt(), true
	case token.ErrDefer:
		return p.parseErrDeferStmt(), true
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
		return &ast.ExprStmt{Expr: &ast.IdentExpr{Name: "<error>", Span: tokenSpan(p.cur)}}
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

// parseMatchArmReturn parses `return` or `return <expr>` as a match arm body.
// The arm's own comma is the terminator, so no semicolon is consumed here.
func (p *Parser) parseMatchArmReturn() ast.Statement {
	stmt := &ast.ReturnStmt{}
	if p.peek.Type == token.Comma || p.peek.Type == token.RBrace {
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(lowest)
	return stmt
}

// parseDeferStmt parses one block-exit cleanup expression statement.
func (p *Parser) parseDeferStmt() ast.Statement {
	stmt := &ast.DeferStmt{}
	if p.peek.Type == token.Let || p.peek.Type == token.Var ||
		p.peek.Type == token.Return || p.peek.Type == token.Defer ||
		p.peek.Type == token.ErrDefer || p.peek.Type == token.LBrace {
		p.errorf("defer expects an expression statement")
		return stmt
	}
	p.nextToken()
	stmt.Expr = p.parseExpression(lowest)
	p.expectStatementTerminator("defer statement")
	return stmt
}

// parseErrDeferStmt parses one error-path cleanup expression statement.
func (p *Parser) parseErrDeferStmt() ast.Statement {
	stmt := &ast.ErrDeferStmt{}
	if p.peek.Type == token.Let || p.peek.Type == token.Var ||
		p.peek.Type == token.Return || p.peek.Type == token.Defer ||
		p.peek.Type == token.ErrDefer || p.peek.Type == token.LBrace {
		p.errorf("errdefer expects an expression statement")
		return stmt
	}
	p.nextToken()
	stmt.Expr = p.parseExpression(lowest)
	p.expectStatementTerminator("errdefer statement")
	return stmt
}

// parseIfStmt parses an if statement with an optional else block.
func (p *Parser) parseIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{}
	p.nextToken()
	stmt.Condition = p.parseExpression(lowest)
	capture, ok := p.parsePayloadCapture()
	if !ok {
		return stmt
	}
	stmt.Capture = capture
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

// parseKeywordLiteralExpression parses the keyword-spelled literals.
func (p *Parser) parseKeywordLiteralExpression() ast.Expression {
	switch p.cur.Type {
	case token.True:
		return &ast.BoolExpr{Value: true}
	case token.False:
		return &ast.BoolExpr{Value: false}
	default:
		return &ast.NullExpr{Span: tokenSpan(p.cur)}
	}
}

// parsePayloadCapture parses a `|name|` payload capture when one follows the
// current expression, reporting false on a malformed one.
func (p *Parser) parsePayloadCapture() (string, bool) {
	if p.peek.Type != token.Pipe {
		return "", true
	}
	if !p.expectPeek(token.Pipe) || !p.expectPeek(token.Ident) {
		return "", false
	}
	capture := p.cur.Literal
	if !p.expectPeek(token.Pipe) {
		return "", false
	}
	return capture, true
}

// parseWhileStmt parses a while loop statement, with an optional payload
// capture (`while expr |name| { ... }`) matching the for-loop spelling.
func (p *Parser) parseWhileStmt(label string) ast.Statement {
	stmt := &ast.WhileStmt{Label: label}
	p.nextToken()
	stmt.Condition = p.parseExpression(lowest)
	capture, ok := p.parsePayloadCapture()
	if !ok {
		return stmt
	}
	stmt.Capture = capture
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

// expectStatementTerminator requires an explicit Zig/C-style semicolon after a
// simple statement. Missing semicolons are parse errors (ADR-0036/ADR-0074).
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

// consumeListDelimiter consumes a comma or accepts the end of a brace-delimited list.
func (p *Parser) consumeListDelimiter(context string) bool {
	switch p.peek.Type {
	case token.Comma:
		p.nextToken()
		return true
	case token.RBrace, token.EOF:
		return true
	default:
		p.errorf("expected `,` after %s", context)
		return false
	}
}

// consumeRequiredListComma consumes a comma-only list delimiter.
func (p *Parser) consumeRequiredListComma(context string) bool {
	if p.peek.Type == token.Comma {
		p.nextToken()
		return true
	}
	p.errorf("expected `,` after %s", context)
	return false
}

// parseMatchStmt parses a simple enum tag match statement.
func (p *Parser) parseMatchStmt() *ast.MatchStmt {
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
		if !p.consumeRequiredListComma("match arm") {
			return arms
		}
		p.nextToken()
	}
	return arms
}

// parseMatchArm parses one enum tag arm.
func (p *Parser) parseMatchArm() (ast.MatchArm, bool) {
	arm := ast.MatchArm{}
	if p.cur.Type != token.Ident {
		p.errorf("expected match tag, got %s", tokenDescription(p.cur))
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
	// A statement-position arm body is an expression or a `return` statement
	// (SPEC §6.12); the arm's comma terminates it in place of `;`.
	if p.cur.Type == token.Return {
		arm.Body = p.parseMatchArmReturn()
		return arm, true
	}
	arm.Body = p.parseStatement()
	return arm, true
}

const (
	_ int = iota
	lowest
	orElse
	logicalOr
	logicalAnd
	equals
	lessGreater
	sum
	product
	prefix
	call
	field
)

var precedences = map[token.Type]int{
	token.Orelse:      orElse,
	token.Or:          logicalOr,
	token.And:         logicalAnd,
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
	token.LBracket:    call,
	token.Dot:         field,
	token.DoubleColon: field,
}

// parseExpression parses an expression using Pratt parser precedence.
func (p *Parser) parseExpression(precedence int) ast.Expression {
	left := p.parsePrefixExpression()
	// A `<` that opens a static argument list binds like a call rather than
	// like a comparison, so `try f<T>(x)` reads as `try (f<T>(x))`.
	for p.peek.Type != token.Semicolon &&
		(precedence < p.peekPrecedence() ||
			(p.peek.Type == token.LT && p.shouldParseTypeApply(left))) {
		switch p.peek.Type {
		case token.Plus, token.Minus, token.Asterisk, token.Slash, token.Percent,
			token.And, token.Or, token.Eq, token.NotEq, token.LTE, token.GT, token.GTE,
			token.Orelse:
			p.nextToken()
			left = p.parseBinaryExpr(left)
		case token.LParen:
			p.nextToken()
			left = p.parseCallExpr(left)
		case token.LBracket:
			p.nextToken()
			left = p.parseIndexExpr(left)
		case token.LT:
			if !p.shouldParseTypeApply(left) {
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
	if p.peek.Type == token.LBrace {
		if typeName, ok := structLiteralTypeName(left); ok {
			return p.parseStructLiteralExpr(typeName)
		}
	}
	return left
}

// parseIndexExpr parses checked index and one-dimensional slice expressions.
func (p *Parser) parseIndexExpr(target ast.Expression) ast.Expression {
	expr := &ast.IndexExpr{Target: target, Span: tokenSpan(p.cur)}
	p.nextToken()
	if p.cur.Type == token.Range {
		expr.Slice = true
		return p.parseOpenStartSlice(expr)
	}
	expr.Start = p.parseExpression(lowest)
	if p.peek.Type == token.Range {
		p.nextToken()
		expr.Slice = true
		return p.parseClosedStartSlice(expr)
	}
	expr.Index = expr.Start
	expr.Start = nil
	p.expectPeek(token.RBracket)
	return expr
}

// parseOpenStartSlice parses `[ ..end ]` after the range token.
func (p *Parser) parseOpenStartSlice(expr *ast.IndexExpr) ast.Expression {
	if p.peek.Type == token.RBracket {
		p.errorf("slice expression requires at least one bound")
		return expr
	}
	p.nextToken()
	expr.End = p.parseExpression(lowest)
	p.expectPeek(token.RBracket)
	return expr
}

// parseClosedStartSlice parses `[ start..end ]` or `[ start.. ]`.
func (p *Parser) parseClosedStartSlice(expr *ast.IndexExpr) ast.Expression {
	if p.peek.Type == token.RBracket {
		p.nextToken()
		return expr
	}
	p.nextToken()
	expr.End = p.parseExpression(lowest)
	p.expectPeek(token.RBracket)
	return expr
}

// parsePrefixExpression parses literals, identifiers, and unary expressions.
func (p *Parser) parsePrefixExpression() ast.Expression {
	switch p.cur.Type {
	case token.Ident:
		return p.parseIdentPrefixExpression()
	case token.Int:
		expr := &ast.IntExpr{Value: p.cur.Literal}
		if v, err := strconv.ParseInt(expr.Value, 10, 64); err == nil {
			expr.Parsed = v
			expr.ParseOK = true
		}
		return expr
	case token.String:
		return &ast.StringExpr{Value: p.cur.Literal}
	case token.True, token.False, token.Null:
		return p.parseKeywordLiteralExpression()
	case token.If:
		return p.parseIfStmt()
	case token.Match:
		return p.parseMatchStmt()
	case token.Comptime, token.Try, token.Unsafe:
		return p.parseMarkerExpression()
	case token.Amp:
		p.nextToken()
		op := "&"
		if p.cur.Type == token.Var {
			op = "&var"
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
	case token.LBracket:
		return p.parseBufferLiteralExpr()
	default:
		p.errorf("expected expression, got %s", tokenDescription(p.cur))
		return &ast.IdentExpr{Name: "<error>", Span: tokenSpan(p.cur)}
	}
}

// parseBufferLiteralExpr parses `[N]u8{}`, the zero-filled fixed-length stack
// buffer literal (ADR-0097).
func (p *Parser) parseBufferLiteralExpr() ast.Expression {
	span := tokenSpan(p.cur)
	parsed := p.parseTypeName()
	if parsed == nil {
		return &ast.IdentExpr{Name: "<error>", Span: span}
	}
	buffer, ok := parsed.(*typ.Buffer)
	if !ok {
		p.errorf("expected buffer literal `[N]u8{}`, got type `%s`", typ.Text(parsed))
		return &ast.IdentExpr{Name: "<error>", Span: span}
	}
	if typ.Text(buffer.Elem) != "u8" {
		p.errorf("buffer element must be u8, got %s", typ.Text(buffer.Elem))
		return &ast.IdentExpr{Name: "<error>", Span: span}
	}
	if !p.expectPeek(token.LBrace) || !p.expectPeek(token.RBrace) {
		return &ast.IdentExpr{Name: "<error>", Span: span}
	}
	return &ast.BufferLiteralExpr{Size: buffer.Size, Span: span}
}

// parseMarkerExpression parses the keywords that sit in front of an expression
// and say something about it without changing its value: `comptime`, `try` and
// `unsafe`.
func (p *Parser) parseMarkerExpression() ast.Expression {
	marker := p.cur
	p.nextToken()
	switch marker.Type {
	case token.Comptime:
		return &ast.ComptimeExpr{Expr: p.parseExpression(lowest)}
	case token.Try:
		return &ast.TryExpr{Value: p.parseExpression(prefix)}
	default:
		return &ast.UnsafeExpr{
			Value:  p.parseExpression(prefix),
			Safety: p.safety,
			Span:   tokenSpan(marker),
		}
	}
}

// parseIdentPrefixExpression parses identifiers and identifier-led special forms.
func (p *Parser) parseIdentPrefixExpression() ast.Expression {
	if p.cur.Literal == "cast" && p.peek.Type == token.LT {
		return p.parseCastExpr()
	}
	if p.cur.Literal == "type" && p.peek.Type == token.LT {
		return p.parseTypeExpr()
	}
	if p.peek.Type == token.LBrace && startsUpper(p.cur.Literal) {
		return p.parseStructLiteralExpr(p.cur.Literal)
	}
	return &ast.IdentExpr{Name: p.cur.Literal, Span: tokenSpan(p.cur)}
}

// parseTypeExpr parses type<T> compile-time type literals.
func (p *Parser) parseTypeExpr() ast.Expression {
	expr := &ast.TypeExpr{}
	if !p.expectPeek(token.LT) {
		return expr
	}
	p.nextToken()
	expr.TypeName = p.parseStaticTypeArg(false)
	if expr.TypeName == "" || !p.expectTypeClose() {
		return expr
	}
	return expr
}

// parseCastExpr parses cast<T>(value).
func (p *Parser) parseCastExpr() ast.Expression {
	expr := &ast.CastExpr{KeywordSpan: tokenSpan(p.cur)}
	if !p.expectPeek(token.LT) {
		return expr
	}
	p.nextToken()
	expr.TargetType = p.parseTypeName()
	if expr.TargetType == nil || !p.expectPeek(token.GT) {
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

// parseTypeName parses a plain, borrow, pointer, or generic type name.
func (p *Parser) parseTypeName() typ.Type {
	switch p.cur.Type {
	case token.Bang:
		return p.parseErrorUnionTypeName()
	case token.Amp:
		return p.parseBorrowTypeName()
	case token.Dyn:
		return p.parseDynTypeName()
	case token.LBracket:
		return p.parseSliceTypeName()
	case token.Question:
		return p.parseNullableTypeName()
	case token.Ident:
		return p.parseNamedTypeName()
	default:
		p.errorf("expected type, got %s", tokenDescription(p.cur))
		return nil
	}
}

// parseNullableTypeName parses ?T type spellings.
func (p *Parser) parseNullableTypeName() typ.Type {
	p.nextToken()
	inner := p.parseTypeName()
	if inner == nil {
		return nil
	}
	return &typ.Optional{Elem: inner}
}

// parseNamedTypeName parses named, typed-error-union, and generic type spellings.
func (p *Parser) parseNamedTypeName() typ.Type {
	name := &typ.Name{Path: p.parseTypeBaseName()}
	if p.peek.Type == token.Bang {
		p.nextToken()
		p.nextToken()
		success := p.parseTypeName()
		if success == nil {
			return nil
		}
		return &typ.ErrorUnion{Err: name, Ok: success}
	}
	if p.peek.Type != token.LT {
		return name
	}
	p.nextToken()
	p.nextToken()
	args := p.parseTypeArgNodes(len(name.Path) == 1 && name.Path[0] == "ptr")
	if len(args) == 0 || !p.expectTypeClose() {
		return nil
	}
	name.Args = args
	return name
}

// parseTypeArgNodes parses the `<...>` list of a type, whose entries are types.
func (p *Parser) parseTypeArgNodes(allowConst bool) []typ.Type {
	args := []typ.Type{}
	for {
		arg := p.parseTypeArg(allowConst)
		if arg == nil {
			return nil
		}
		args = append(args, arg)
		if p.peek.Type != token.Comma {
			break
		}
		p.nextToken()
		if p.peek.Type == token.GT {
			break
		}
		p.nextToken()
	}
	return args
}

// parseDynTypeName parses dyn Contract type spellings.
func (p *Parser) parseDynTypeName() typ.Type {
	p.nextToken()
	if p.cur.Type != token.Ident {
		p.errorf("expected contract after dyn, got %s", tokenDescription(p.cur))
		return nil
	}
	name := p.parseTypeBaseName()
	if p.peek.Type == token.LT {
		p.errorf("dyn expects a contract name")
		return nil
	}
	return &typ.Dyn{Contract: &typ.Name{Path: name}}
}

// parseErrorUnionTypeName parses !T type spellings.
func (p *Parser) parseErrorUnionTypeName() typ.Type {
	p.nextToken()
	inner := p.parseTypeName()
	if inner == nil {
		return nil
	}
	return &typ.ErrorUnion{Ok: inner}
}

// parseBorrowTypeName parses &T and &var T type spellings.
func (p *Parser) parseBorrowTypeName() typ.Type {
	p.nextToken()
	mut := p.cur.Type == token.Var
	if mut {
		p.nextToken()
	}
	inner := p.parseTypeName()
	if inner == nil {
		return nil
	}
	return &typ.Borrow{Elem: inner, Mut: mut}
}

// parseSliceTypeName parses []T and [N]T type spellings.
func (p *Parser) parseSliceTypeName() typ.Type {
	if p.peek.Type == token.Int {
		return p.parseBufferTypeName()
	}
	if !p.expectPeek(token.RBracket) {
		return nil
	}
	p.nextToken()
	arg := p.parseTypeArg(false)
	if arg == nil {
		return nil
	}
	return &typ.Slice{Elem: arg}
}

// parseBufferTypeName parses `[N]T` fixed-length buffer type spellings.
func (p *Parser) parseBufferTypeName() typ.Type {
	p.nextToken()
	size, err := strconv.ParseInt(p.cur.Literal, 10, 64)
	if err != nil || size <= 0 {
		p.errorf("buffer size must be a positive integer, got %s", p.cur.Literal)
		return nil
	}
	if !p.expectPeek(token.RBracket) {
		return nil
	}
	p.nextToken()
	arg := p.parseTypeArg(false)
	if arg == nil {
		return nil
	}
	return &typ.Buffer{Size: size, Elem: arg}
}

// parseTypeBaseName parses an identifier or namespace-qualified type base.
func (p *Parser) parseTypeBaseName() []string {
	parts := []string{p.cur.Literal}
	for p.peek.Type == token.DoubleColon {
		p.nextToken()
		if !p.expectPeek(token.Ident) {
			return parts
		}
		parts = append(parts, p.cur.Literal)
	}
	return parts
}

// parseTypeArgList parses one or more comma-separated static type arguments.
func (p *Parser) parseTypeArgList(allowConst bool) string {
	args := []string{}
	for {
		arg := p.parseStaticTypeArg(allowConst)
		if arg == "" {
			return ""
		}
		args = append(args, arg)
		if p.peek.Type != token.Comma {
			break
		}
		p.nextToken()
		if p.peek.Type == token.GT {
			break
		}
		p.nextToken()
	}
	return strings.Join(args, ", ")
}

// parseGenericParamList parses type parameters.
func (p *Parser) parseGenericParamList() []string {
	names := []string{}
	for _, param := range p.parseStaticParamList() {
		if !param.IsType() {
			p.errorf("expected type parameter, got %s", param.String())
			return nil
		}
		names = append(names, param.Name)
	}
	return names
}

// parseStaticParamList parses a `<...>` declaration list. A bare name declares
// a type parameter; `name: Type` declares a compile-time value.
func (p *Parser) parseStaticParamList() []ast.StaticParam {
	params := []ast.StaticParam{}
	seen := map[string]bool{}
	p.nextToken()
	for {
		if p.cur.Type != token.Ident {
			p.errorf("expected static parameter, got %s", tokenDescription(p.cur))
			return nil
		}
		if seen[p.cur.Literal] {
			p.errorf("duplicate static parameter %s", p.cur.Literal)
			return nil
		}
		param := ast.StaticParam{Name: p.cur.Literal}
		seen[param.Name] = true
		if p.peek.Type == token.Colon {
			p.nextToken()
			p.nextToken()
			param.Type = p.parseTypeName()
		}
		params = append(params, param)
		if p.peek.Type != token.Comma {
			break
		}
		p.nextToken()
		if p.peek.Type == token.GT {
			break
		}
		p.nextToken()
	}
	return params
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

// parseStaticTypeArg parses one entry of a `<...>` argument list. An entry is a
// type, or a compile-time value for a parameter that declared one.
func (p *Parser) parseStaticTypeArg(allowConst bool) string {
	switch p.cur.Type {
	case token.Ident, token.Bang, token.Amp, token.Dyn, token.LBracket, token.Question:
		return typ.Text(p.parseTypeArg(allowConst))
	case token.Int:
		return p.cur.Literal
	case token.True, token.False:
		return p.cur.Literal
	default:
		p.errorf("expected static argument, got %s", tokenDescription(p.cur))
		return ""
	}
}

// parseTypeArg parses a type embedded inside a generic-like type spelling.
func (p *Parser) parseTypeArg(allowConst bool) typ.Type {
	if p.cur.Type == token.Ident && p.cur.Literal == "const" {
		if !allowConst {
			p.errorf("expected static type argument, got const")
			return nil
		}
		p.nextToken()
		inner := p.parseTypeName()
		if inner == nil {
			return nil
		}
		return &typ.Const{Elem: inner}
	}
	return p.parseTypeName()
}

// parseBinaryExpr parses an infix binary expression.
func (p *Parser) parseBinaryExpr(left ast.Expression) ast.Expression {
	expr := &ast.BinaryExpr{
		Left:         left,
		Operator:     p.cur.Literal,
		OperatorSpan: tokenSpan(p.cur),
		Op:           ast.ClassifyBinaryOp(p.cur.Literal),
	}
	precedence := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

// tokenSpan converts a token position into a half-open AST span.
func tokenSpan(tok token.Token) ast.Span {
	width := len([]rune(tok.Literal))
	if width == 0 {
		width = 1
	}
	return ast.Span{
		File:  tok.File,
		Start: ast.Position{Line: tok.Line, Column: tok.Column},
		End:   ast.Position{Line: tok.Line, Column: tok.Column + width},
	}
}

// expressionSpan returns the best source span currently stored on an expression.
func expressionSpan(expr ast.Expression) ast.Span {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return e.Span
	case *ast.BinaryExpr:
		return e.OperatorSpan
	case *ast.CallExpr:
		return expressionSpan(e.Callee)
	case *ast.TypeApplyExpr:
		return expressionSpan(e.Callee)
	case *ast.CastExpr:
		return e.KeywordSpan
	case *ast.FieldExpr:
		return e.Span
	case *ast.DerefExpr:
		return e.OperatorSpan
	default:
		return ast.Span{}
	}
}

// joinSpans returns the range from first.Start to last.End when both are known.
func joinSpans(first ast.Span, last ast.Span) ast.Span {
	if first.IsZero() {
		return last
	}
	if last.IsZero() {
		return first
	}
	return ast.Span{File: first.File, Start: first.Start, End: last.End}
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
		if p.peek.Type == token.RParen {
			break
		}
		p.nextToken()
		expr.Args = append(expr.Args, p.parseExpression(lowest))
	}
	p.expectPeek(token.RParen)
	return expr
}

// parseTypeApplyExpr parses Namespace::Item<T> static type application.
func (p *Parser) parseTypeApplyExpr(callee ast.Expression) ast.Expression {
	expr := &ast.TypeApplyExpr{Callee: callee}
	p.nextToken()
	expr.TypeArg = p.parseTypeArgList(false)
	if expr.TypeArg == "" || !p.expectTypeClose() {
		return expr
	}
	return expr
}

// parseStructLiteralExpr parses Type { field: value }.
func (p *Parser) parseStructLiteralExpr(typeName string) ast.Expression {
	expr := &ast.StructLiteralExpr{TypeName: typeName, Span: tokenSpan(p.cur)}
	p.nextToken()
	p.nextToken()
	for p.cur.Type != token.RBrace && p.cur.Type != token.EOF {
		field, ok := p.parseFieldValue()
		if !ok {
			return expr
		}
		expr.Fields = append(expr.Fields, field)
		if !p.consumeListDelimiter("struct literal field") {
			return expr
		}
		p.nextToken()
	}
	return expr
}

// structLiteralTypeName returns a namespaced type usable before a struct literal.
func structLiteralTypeName(expr ast.Expression) (string, bool) {
	parts, ok := namespaceExprParts(expr)
	if !ok || len(parts) == 0 || !startsUpper(parts[len(parts)-1]) {
		return "", false
	}
	if len(parts) > 1 && startsUpper(parts[len(parts)-2]) {
		return "", false
	}
	return strings.Join(parts, "::"), true
}

// namespaceExprParts extracts identifier parts from a namespace expression.
func namespaceExprParts(expr ast.Expression) ([]string, bool) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		return []string{e.Name}, true
	case *ast.FieldExpr:
		if !e.Namespace {
			return nil, false
		}
		parts, ok := namespaceExprParts(e.Receiver)
		if !ok {
			return nil, false
		}
		return append(parts, e.Name), true
	default:
		return nil, false
	}
}

// parseFieldValue parses one field initializer.
func (p *Parser) parseFieldValue() (ast.FieldValue, bool) {
	field := ast.FieldValue{}
	if p.cur.Type != token.Ident {
		p.errorf("expected field name, got %s", tokenDescription(p.cur))
		return field, false
	}
	field.Name = p.cur.Literal
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
		return &ast.DerefExpr{Receiver: receiver, OperatorSpan: tokenSpan(p.cur)}
	}
	expr := &ast.FieldExpr{Receiver: receiver, Namespace: namespace}
	if !p.expectPeek(token.Ident) {
		return expr
	}
	expr.Name = p.cur.Literal
	expr.Span = joinSpans(expressionSpan(receiver), tokenSpan(p.cur))
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
	p.errorf("expected %s, got %s", tokenTypeDescription(t), tokenDescription(p.cur))
	return false
}

// expectPeek advances if the lookahead token has type t.
func (p *Parser) expectPeek(t token.Type) bool {
	if p.peek.Type == t {
		p.nextToken()
		return true
	}
	p.errorf("expected next token %s, got %s", tokenTypeDescription(t), tokenDescription(p.peek))
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
	p.errors = append(p.errors, diagnosticAtToken(p.cur, message))
}

// errorExpectedDeclaration reports the accepted top-level declaration starts.
func (p *Parser) errorExpectedDeclaration() {
	p.errorf("expected declaration (%s), got %s",
		"fn, test, import, struct, enum, union, contract, impl, extern, pub, or unsafe",
		tokenDescription(p.cur),
	)
}

// tokenDescription renders a short user-facing token name.
func tokenDescription(tok token.Token) string {
	switch tok.Type {
	case token.Ident:
		return fmt.Sprintf("identifier %q", tok.Literal)
	case token.Int:
		return fmt.Sprintf("integer %q", tok.Literal)
	case token.String:
		return fmt.Sprintf("string %q", tok.Literal)
	case token.Illegal:
		if tok.Literal != "" {
			return fmt.Sprintf("illegal token %q", tok.Literal)
		}
		return "illegal token"
	case token.EOF:
		return "end of file"
	default:
		return fmt.Sprintf("`%s`", tok.Literal)
	}
}

// tokenTypeDescription renders an expected token kind without lexer internals.
func tokenTypeDescription(typ token.Type) string {
	switch typ {
	case token.Ident:
		return "identifier"
	case token.Int:
		return "integer"
	case token.String:
		return "string"
	case token.EOF:
		return "end of file"
	default:
		return fmt.Sprintf("`%s`", typ)
	}
}

// startsUpper reports whether name follows the v0 struct literal convention.
func startsUpper(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// shouldParseTypeApply reports whether `<...>` is a static call argument list.
func (p *Parser) shouldParseTypeApply(expr ast.Expression) bool {
	if !canTypeApplyTarget(expr) {
		return false
	}
	return p.typeApplyLooksLikeCall()
}

// typeApplyLooksLikeCall scans ahead for `<...>(` without committing tokens.
func (p *Parser) typeApplyLooksLikeCall() bool {
	cur := p.cur
	peek := p.peek
	lexerState := *p.l
	errorCount := len(p.errors)
	defer func() {
		p.cur = cur
		p.peek = peek
		*p.l = lexerState
		p.errors = p.errors[:errorCount]
	}()

	p.nextToken()
	depth := 1
	for depth > 0 {
		p.nextToken()
		switch p.cur.Type {
		case token.EOF, token.Semicolon, token.LBrace, token.RBrace:
			return false
		case token.LT:
			depth++
		case token.GT:
			depth--
		}
	}
	return p.peek.Type == token.LParen
}

// canTypeApplyTarget reports whether expr may receive static call arguments.
func canTypeApplyTarget(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.FieldExpr:
		return e.Namespace
	case *ast.IdentExpr:
		return e.Name != ""
	default:
		return false
	}
}
