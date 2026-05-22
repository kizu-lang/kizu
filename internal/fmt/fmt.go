// Package fmt produces a canonical, stable formatting of Kizu source text.
//
// The formatter is token based: it lexes the input and emits tokens with
// consistent spacing, indentation, and block layout. It expects syntactically
// valid source; callers that mutate files should validate before writing.
package fmt

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/token"
)

// indentUnit is the canonical indent for one nesting level.
const indentUnit = "    "

// Format returns a canonical formatting of source.
func Format(source string) string {
	tokens := tokenize(source)
	generic := detectGenericBrackets(tokens)
	b := &builder{generic: generic}
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.Type == token.EOF {
			break
		}
		if isTrailingCommaBeforeClose(tokens, i) {
			continue
		}
		next := token.Token{Type: token.EOF}
		if i+1 < len(tokens) {
			next = tokens[i+1]
		}
		b.index = i
		b.emit(t, next)
	}
	return b.finish()
}

// detectGenericBrackets marks `<` and `>` tokens that form a generic argument
// or parameter list (e.g., `Array<T>`). A bracket is treated as generic when
// it directly follows an identifier and a matching `>` is found before any
// statement-terminating token.
func detectGenericBrackets(tokens []token.Token) []bool {
	flags := make([]bool, len(tokens))
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.LT {
			continue
		}
		if i == 0 || tokens[i-1].Type != token.Ident {
			continue
		}
		depth := 1
		matchIdx := -1
	scan:
		for j := i + 1; j < len(tokens); j++ {
			switch tokens[j].Type {
			case token.LT:
				if j > 0 && tokens[j-1].Type == token.Ident {
					depth++
				}
			case token.GT:
				depth--
				if depth == 0 {
					matchIdx = j
					break scan
				}
			case token.Semicolon, token.LBrace, token.RBrace, token.Assign,
				token.Eq, token.NotEq, token.LTE, token.GTE,
				token.And, token.Or, token.EOF:
				break scan
			}
		}
		if matchIdx >= 0 {
			flags[i] = true
			flags[matchIdx] = true
		}
	}
	return flags
}

// tokenize collects every token from the lexer including EOF.
func tokenize(source string) []token.Token {
	l := lexer.New(source)
	var tokens []token.Token
	for {
		t := l.NextToken()
		tokens = append(tokens, t)
		if t.Type == token.EOF {
			return tokens
		}
	}
}

// isTrailingCommaBeforeClose drops a trailing `,` that immediately precedes `}` or `)` or `]`.
func isTrailingCommaBeforeClose(tokens []token.Token, i int) bool {
	if tokens[i].Type != token.Comma {
		return false
	}
	if i+1 >= len(tokens) {
		return false
	}
	switch tokens[i+1].Type {
	case token.RBrace, token.RParen, token.RBracket:
		return true
	}
	return false
}

// builder accumulates formatted output and tracks layout state.
type builder struct {
	out         strings.Builder
	depth       int
	atLineStart bool
	prev        token.Token
	hasPrev     bool
	prevIndex   int
	index       int
	generic     []bool
	sourceLine  int
}

// emit appends a token using current layout state.
func (b *builder) emit(t token.Token, next token.Token) {
	if t.Type == token.RBrace {
		b.emitRBrace(t, next)
		return
	}
	if t.Type == token.String && strings.ContainsRune(t.Literal, '\n') {
		b.emitMultilineString(t)
		return
	}
	b.maybeBlankLineForTopLevel(t)
	b.maybePreserveSourceLineBreak(t)
	b.writeSeparator(t)
	b.writeToken(t)
	b.recordEmitted(t)
	b.maybeTrailingNewline(t, next)
}

// maybeBlankLineForTopLevel emits a blank line before each top-level declaration after the first.
func (b *builder) maybeBlankLineForTopLevel(t token.Token) {
	if !(b.hasPrev && b.depth == 0 && b.atLineStart && isTopLevelDeclStart(t) && b.out.Len() > 0) {
		return
	}
	if !endsWithBlankLine(&b.out) {
		b.out.WriteByte('\n')
	}
}

// maybePreserveSourceLineBreak inserts a newline when the source had one between tokens.
func (b *builder) maybePreserveSourceLineBreak(t token.Token) {
	if b.atLineStart || !b.hasPrev || b.sourceLine == 0 || t.Line <= b.sourceLine {
		return
	}
	b.out.WriteByte('\n')
	b.atLineStart = true
}

// writeSeparator emits the indent (line start) or a single space between tokens.
func (b *builder) writeSeparator(t token.Token) {
	if b.atLineStart {
		b.writeIndent()
		return
	}
	if b.hasPrev && b.shouldInsertSpace(t) {
		b.out.WriteByte(' ')
	}
}

// recordEmitted updates layout state after writing t.
func (b *builder) recordEmitted(t token.Token) {
	b.prev = t
	b.prevIndex = b.index
	b.hasPrev = true
	b.atLineStart = false
	if t.Line > 0 {
		b.sourceLine = t.Line
	}
}

// maybeTrailingNewline handles structural tokens that always force a line break.
func (b *builder) maybeTrailingNewline(t token.Token, next token.Token) {
	switch t.Type {
	case token.LBrace:
		b.depth++
		if next.Type == token.RBrace {
			return
		}
		b.out.WriteByte('\n')
		b.atLineStart = true
	case token.Semicolon:
		b.out.WriteByte('\n')
		b.atLineStart = true
	}
}

// emitRBrace closes a block, deciding whether to emit a trailing newline.
func (b *builder) emitRBrace(t token.Token, next token.Token) {
	if b.depth > 0 {
		b.depth--
	}
	rb := token.Token{Type: token.RBrace, Literal: "}", Line: t.Line, Column: t.Column}

	if b.hasPrev && b.prev.Type == token.LBrace {
		// Empty block `{}` collapses onto one line.
		b.writeToken(rb)
	} else {
		if !b.atLineStart {
			b.out.WriteByte('\n')
		}
		b.writeIndent()
		b.writeToken(rb)
	}
	b.prev = rb
	b.prevIndex = b.index
	b.hasPrev = true
	b.atLineStart = false
	if t.Line > 0 {
		b.sourceLine = t.Line
	}

	if rbraceWantsNewline(next) {
		b.out.WriteByte('\n')
		b.atLineStart = true
	}
}

// rbraceWantsNewline reports whether a `}` should be followed by a newline.
func rbraceWantsNewline(next token.Token) bool {
	switch next.Type {
	case token.EOF,
		token.Comma, token.Semicolon, token.Dot, token.DoubleColon,
		token.RParen, token.RBracket, token.RBrace,
		token.Question, token.Bang,
		token.Else:
		return false
	}
	return true
}

// finish produces the final source string, guaranteeing a single trailing newline.
func (b *builder) finish() string {
	s := b.out.String()
	if s == "" {
		return ""
	}
	s = strings.TrimRight(s, "\n")
	return s + "\n"
}

// writeIndent emits the indent string for the current block depth.
func (b *builder) writeIndent() {
	for i := 0; i < b.depth; i++ {
		b.out.WriteString(indentUnit)
	}
}

// writeToken emits the canonical spelling of t.
func (b *builder) writeToken(t token.Token) {
	switch t.Type {
	case token.String:
		b.writeStringLiteral(t.Literal)
	default:
		b.out.WriteString(tokenSpelling(t))
	}
}

// writeStringLiteral emits a single-line string literal in `"..."` form.
func (b *builder) writeStringLiteral(value string) {
	b.out.WriteByte('"')
	b.out.WriteString(value)
	b.out.WriteByte('"')
}

// emitMultilineString writes a `\\`-prefixed multi-line string. The literal is
// laid out on its own lines indented one level deeper than the current context,
// and the next token starts on a fresh line at the parent indent.
func (b *builder) emitMultilineString(t token.Token) {
	if !b.atLineStart {
		b.out.WriteByte('\n')
	}
	lines := strings.Split(t.Literal, "\n")
	indent := strings.Repeat(indentUnit, b.depth+1)
	for i, line := range lines {
		b.out.WriteString(indent)
		b.out.WriteString(`\\`)
		b.out.WriteString(line)
		if i < len(lines)-1 {
			b.out.WriteByte('\n')
		}
	}
	b.out.WriteByte('\n')
	b.atLineStart = true
	b.prev = t
	b.prevIndex = b.index
	b.hasPrev = true
	if t.Line > 0 {
		b.sourceLine = t.Line + len(lines) - 1
	}
}

// tokenSpelling returns the canonical printable form of t.
func tokenSpelling(t token.Token) string {
	switch t.Type {
	case token.Ident, token.Int:
		return t.Literal
	case token.String:
		return `"` + t.Literal + `"`
	}
	if t.Literal != "" {
		return t.Literal
	}
	return string(t.Type)
}

// shouldInsertSpace decides whether a space goes between prev and curr.
func (b *builder) shouldInsertSpace(curr token.Token) bool {
	prev := b.prev
	// Generic brackets: tight spacing on both sides of `<` and before `>`.
	if curr.Type == token.LT && b.index < len(b.generic) && b.generic[b.index] {
		return false
	}
	if prev.Type == token.LT && b.prevIndex < len(b.generic) && b.generic[b.prevIndex] {
		return false
	}
	if curr.Type == token.GT && b.index < len(b.generic) && b.generic[b.index] {
		return false
	}
	if noSpaceBefore(curr) {
		return false
	}
	if noSpaceAfter(prev) {
		return false
	}
	if prev.Type == token.RBracket && canFollowSliceMarker(curr) {
		return false
	}
	return true
}

// noSpaceBefore reports whether t never takes a preceding space.
func noSpaceBefore(t token.Token) bool {
	switch t.Type {
	case token.RParen, token.RBracket, token.Semicolon, token.Comma,
		token.Dot, token.DoubleColon, token.LParen, token.Colon,
		token.Range:
		return true
	}
	return false
}

// noSpaceAfter reports whether t never takes a following space.
func noSpaceAfter(t token.Token) bool {
	switch t.Type {
	case token.LParen, token.LBracket, token.Dot, token.DoubleColon,
		token.Bang, token.Question, token.Amp, token.Range:
		return true
	}
	return false
}

// canFollowSliceMarker mirrors the selfhost formatter rule for `[]T`-style slices.
func canFollowSliceMarker(t token.Token) bool {
	switch t.Type {
	case token.Ident, token.Amp, token.Bang, token.Question, token.LBracket:
		return true
	}
	if len(t.Literal) > 0 {
		c := t.Literal[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' {
			return true
		}
	}
	return false
}

// isTopLevelDeclStart reports whether t begins a top-level declaration.
func isTopLevelDeclStart(t token.Token) bool {
	switch t.Type {
	case token.Import, token.Public, token.Unsafe, token.Extern,
		token.Function, token.Struct, token.Enum, token.Union, token.Contract, token.Impl:
		return true
	}
	return false
}

// endsWithBlankLine reports whether the buffer already ends with `\n\n`.
func endsWithBlankLine(out *strings.Builder) bool {
	s := out.String()
	n := len(s)
	return n >= 2 && s[n-1] == '\n' && s[n-2] == '\n'
}
