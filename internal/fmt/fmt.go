// Package fmt produces a canonical, stable formatting of Kizu source text.
//
// The formatter is token based: it lexes the input and emits tokens with
// consistent spacing, indentation, and block layout. It expects syntactically
// valid source; callers that mutate files should validate before writing.
package fmt

import (
	"sort"
	"strings"

	"github.com/kizu-lang/kizu/internal/lexer"
	"github.com/kizu-lang/kizu/internal/token"
)

// indentUnit is the canonical indent for one nesting level.
const indentUnit = "    "

// Format returns a canonical formatting of source.
func Format(source string) string {
	tokens := tokenize(source)
	tokens = normalizeLeadingImports(tokens)
	generic := detectGenericBrackets(tokens)
	b := &builder{
		atLineStart: true,
		generic:     generic,
		tokens:      tokens,
		comments:    lineComments(source),
	}
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.Type == token.RBrace {
			b.emitTrailingCommaBeforeClose()
		}
		b.emitCommentsBefore(t)
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
	b.emitRemainingComments()
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

type importRange struct {
	start int
	end   int
}

type lineComment struct {
	line        int
	text        string
	blankBefore bool
	blankAfter  bool
}

// lineComments records every line comment in source, whether it stands alone or
// trails code. The canonical form puts each one on its own line, so a trailing
// comment is recorded against the line it was written on and emitted before the
// next line's content. Dropping them instead would lose what the author wrote.
func lineComments(source string) []lineComment {
	lines := strings.Split(source, "\n")
	comments := make([]lineComment, 0)
	for idx, line := range lines {
		start := commentStart(line)
		if start < 0 {
			continue
		}
		standalone := strings.TrimSpace(line[:start]) == ""
		comments = append(comments, lineComment{
			line:        idx + 1,
			text:        strings.TrimRight(strings.TrimSpace(line[start:]), "\r"),
			blankBefore: standalone && hasBlankLineBefore(lines, idx),
			blankAfter:  standalone && hasBlankLineAfter(lines, idx),
		})
	}
	return comments
}

// commentStart returns the index of the `//` that opens a line comment, or -1
// when the line has none. A `//` inside a string literal is not a comment.
func commentStart(line string) int {
	inString := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if inString {
				i++
			}
		case '"':
			inString = !inString
		case '/':
			if !inString && i+1 < len(line) && line[i+1] == '/' {
				return i
			}
		}
	}
	return -1
}

// hasBlankLineBefore reports whether a standalone comment had a blank line before it.
func hasBlankLineBefore(lines []string, idx int) bool {
	if idx == 0 {
		return false
	}
	return strings.TrimSpace(lines[idx-1]) == ""
}

// hasBlankLineAfter reports whether a standalone comment had a blank line after it.
func hasBlankLineAfter(lines []string, idx int) bool {
	if idx+1 >= len(lines) {
		return false
	}
	return strings.TrimSpace(lines[idx+1]) == ""
}

// normalizeLeadingImports sorts the initial contiguous import block. Later
// imports are left untouched because moving them across declarations would
// change the source structure rather than formatting it.
func normalizeLeadingImports(tokens []token.Token) []token.Token {
	ranges := leadingImportRanges(tokens)
	if len(ranges) < 2 {
		return tokens
	}
	sorted := append([]importRange(nil), ranges...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return importPathKey(tokens, sorted[i]) < importPathKey(tokens, sorted[j])
	})
	out := make([]token.Token, 0, len(tokens))
	for _, r := range sorted {
		out = append(out, tokens[r.start:r.end]...)
	}
	out = append(out, tokens[ranges[len(ranges)-1].end:]...)
	return out
}

// leadingImportRanges returns token ranges for the initial contiguous import block.
func leadingImportRanges(tokens []token.Token) []importRange {
	var ranges []importRange
	for i := 0; i < len(tokens) && tokens[i].Type == token.Import; {
		start := i
		for i < len(tokens) && tokens[i].Type != token.Semicolon && tokens[i].Type != token.EOF {
			i++
		}
		if i < len(tokens) && tokens[i].Type == token.Semicolon {
			i++
		}
		ranges = append(ranges, importRange{start: start, end: i})
	}
	return ranges
}

// importPathKey returns the canonical import path used for sort order.
func importPathKey(tokens []token.Token, r importRange) string {
	var b strings.Builder
	for i := r.start + 1; i < r.end; i++ {
		switch tokens[i].Type {
		case token.Ident:
			b.WriteString(tokens[i].Literal)
		case token.DoubleColon:
			b.WriteString("::")
		}
	}
	return b.String()
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
	out          strings.Builder
	depth        int
	atLineStart  bool
	prev         token.Token
	hasPrev      bool
	prevIndex    int
	index        int
	tokens       []token.Token
	generic      []bool
	sourceLine   int
	lastTopDecl  token.Type
	comments     []lineComment
	commentIdx   int
	blockStack   []blockKind
	afterComment bool
}

type blockKind int

const (
	normalBlock blockKind = iota
	// commaTerminatedBlock is a block whose entries the grammar requires to end
	// with a comma, so dropping the trailing one produces source that no longer
	// parses. Enum variants and match arms are both written this way.
	commaTerminatedBlock
)

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

// emitCommentsBefore writes preserved standalone comments before token t.
func (b *builder) emitCommentsBefore(t token.Token) {
	if t.Line <= 0 {
		return
	}
	for b.commentIdx < len(b.comments) && b.comments[b.commentIdx].line < t.Line {
		b.writeCommentLine(b.comments[b.commentIdx])
		b.commentIdx++
	}
}

// emitRemainingComments writes comments after the final token.
func (b *builder) emitRemainingComments() {
	for b.commentIdx < len(b.comments) {
		b.writeCommentLine(b.comments[b.commentIdx])
		b.commentIdx++
	}
}

// writeCommentLine emits one standalone comment at the current indentation depth.
func (b *builder) writeCommentLine(comment lineComment) {
	if comment.blankBefore && b.out.Len() > 0 && !endsWithBlankLine(&b.out) {
		if !b.atLineStart {
			b.out.WriteByte('\n')
			b.atLineStart = true
		}
		b.out.WriteByte('\n')
	}
	if !b.atLineStart {
		b.out.WriteByte('\n')
		b.atLineStart = true
	}
	b.writeIndent()
	b.out.WriteString(comment.text)
	b.out.WriteByte('\n')
	b.atLineStart = true
	b.afterComment = true
	if comment.blankAfter && !endsWithBlankLine(&b.out) {
		b.out.WriteByte('\n')
	}
	b.sourceLine = comment.line
}

// maybeBlankLineForTopLevel emits a blank line before each top-level declaration after the first.
func (b *builder) maybeBlankLineForTopLevel(t token.Token) {
	if !b.isTopLevelDeclAtLineStart(t) {
		return
	}
	if b.afterComment || !b.hasPrev || b.out.Len() == 0 {
		b.lastTopDecl = t.Type
		return
	}
	if !b.isConsecutiveImport(t) && !endsWithBlankLine(&b.out) {
		b.out.WriteByte('\n')
	}
	b.lastTopDecl = t.Type
}

// isTopLevelDeclAtLineStart reports whether t starts a declaration at top level.
func (b *builder) isTopLevelDeclAtLineStart(t token.Token) bool {
	return b.depth == 0 && b.atLineStart && isTopLevelDeclStart(t)
}

// isConsecutiveImport reports whether t continues a top-level import block.
func (b *builder) isConsecutiveImport(t token.Token) bool {
	return t.Type == token.Import && b.lastTopDecl == token.Import
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
	b.afterComment = false
}

// maybeTrailingNewline handles structural tokens that always force a line break.
func (b *builder) maybeTrailingNewline(t token.Token, next token.Token) {
	switch t.Type {
	case token.LBrace:
		b.blockStack = append(b.blockStack, b.currentOpenBlockKind())
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
	b.popBlockKind()
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
	b.afterComment = false

	if rbraceWantsNewline(next) {
		b.out.WriteByte('\n')
		b.atLineStart = true
	}
}

// emitTrailingCommaBeforeClose restores the comma that isTrailingCommaBeforeClose
// dropped, for the blocks whose grammar requires it on the last entry.
func (b *builder) emitTrailingCommaBeforeClose() {
	if b.currentBlockKind() != commaTerminatedBlock ||
		!b.hasPrev ||
		b.prev.Type == token.Comma ||
		b.prev.Type == token.LBrace {
		return
	}
	comma := token.Token{Type: token.Comma, Literal: ","}
	b.writeToken(comma)
	b.prev = comma
	b.hasPrev = true
	b.atLineStart = false
	b.afterComment = false
}

// currentOpenBlockKind reports the kind of block opened by the current `{`.
func (b *builder) currentOpenBlockKind() blockKind {
	if opensCommaTerminatedBlockAtCurrentIndex(b.tokens, b.index) {
		return commaTerminatedBlock
	}
	return normalBlock
}

// currentBlockKind reports the innermost block kind without popping it.
func (b *builder) currentBlockKind() blockKind {
	if len(b.blockStack) == 0 {
		return normalBlock
	}
	return b.blockStack[len(b.blockStack)-1]
}

// popBlockKind removes and returns the current closing block kind.
func (b *builder) popBlockKind() blockKind {
	if len(b.blockStack) == 0 {
		return normalBlock
	}
	idx := len(b.blockStack) - 1
	kind := b.blockStack[idx]
	b.blockStack = b.blockStack[:idx]
	return kind
}

// opensEnumBlockAtCurrentIndex reports whether tokens[index] opens an enum body.
func opensCommaTerminatedBlockAtCurrentIndex(tokens []token.Token, index int) bool {
	if index < 0 || index >= len(tokens) || tokens[index].Type != token.LBrace {
		return false
	}
	// Walk back to the keyword that introduced this `{`. Another brace or a
	// semicolon means the keyword belongs to an enclosing construct, not this
	// block, which is what keeps a match arm's own `{ ... }` body normal.
	for cursor := index - 1; cursor >= 0; cursor-- {
		switch tokens[cursor].Type {
		case token.Enum, token.Match:
			return true
		case token.LBrace, token.RBrace, token.Semicolon:
			return false
		}
	}
	return false
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
	if b.tightGenericBracket(curr) {
		return false
	}
	// A receiver slot follows `fn` with a space: `fn (self: T) name()`. It is
	// the one `(` that opens a declaration rather than a call or a group.
	if curr.Type == token.LParen && prev.Type == token.Function {
		return true
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

// tightGenericBracket reports the `<` and `>` of a static argument list, which
// take no space on either side of `<` and none before `>`.
func (b *builder) tightGenericBracket(curr token.Token) bool {
	if curr.Type == token.LT && b.index < len(b.generic) && b.generic[b.index] {
		return true
	}
	if b.prev.Type == token.LT && b.prevIndex < len(b.generic) && b.generic[b.prevIndex] {
		return true
	}
	return curr.Type == token.GT && b.index < len(b.generic) && b.generic[b.index]
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
		token.Bang, token.Question, token.Amp, token.At, token.Range:
		return true
	}
	return false
}

// canFollowSliceMarker reports the tokens that may follow `[]` in a `[]T` slice type.
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
	case token.Import, token.Public, token.Extern,
		token.Function, token.Struct, token.Enum, token.Union, token.Contract, token.Impl:
		return true
	case token.Ident:
		return t.Literal == "test"
	}
	return false
}

// endsWithBlankLine reports whether the buffer already ends with `\n\n`.
func endsWithBlankLine(out *strings.Builder) bool {
	s := out.String()
	n := len(s)
	return n >= 2 && s[n-1] == '\n' && s[n-2] == '\n'
}
