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
		shift:       detectShiftPairs(tokens, generic),
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
		if tokens[i].Type != token.LT || shiftHalf(tokens, i) {
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
				if j > 0 && tokens[j-1].Type == token.Ident && !shiftHalf(tokens, j) {
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

// shiftHalf reports whether the `<` or `>` at i sits against another of its
// kind with nothing between: the two tokens the lexer hands a shift over as
// (SPEC §6.9.2). Whether such a `>` pair is a shift or two generic closers is
// detectShiftPairs's question; here it only keeps a `<<` out of a generic scan.
func shiftHalf(tokens []token.Token, i int) bool {
	adjacent := func(a int, b int) bool {
		return tokens[a].Type == tokens[b].Type && tokens[a].Line == tokens[b].Line &&
			tokens[b].Column == tokens[a].Column+1
	}
	if i+1 < len(tokens) && adjacent(i, i+1) {
		return true
	}
	return i > 0 && adjacent(i-1, i)
}

// detectShiftPairs marks the two `<` of a `<<` and the two `>` of a `>>`, the
// halves the formatter writes with no space between. A `>` that closes a
// generic argument list is not one, however close the next `>` is.
func detectShiftPairs(tokens []token.Token, generic []bool) []bool {
	flags := make([]bool, len(tokens))
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].Type != token.LT && tokens[i].Type != token.GT {
			continue
		}
		if !shiftHalf(tokens, i) || tokens[i+1].Type != tokens[i].Type ||
			generic[i] || generic[i+1] {
			continue
		}
		if tokens[i+1].Line == tokens[i].Line && tokens[i+1].Column == tokens[i].Column+1 {
			flags[i], flags[i+1] = true, true
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
			// Two backslashes outside a quoted string open a multiline
			// literal segment. Everything after them is payload, including
			// `//`; Kizu strings have no escape syntax inside quotes.
			if !inString && i+1 < len(line) && line[i+1] == '\\' {
				return -1
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
	out            strings.Builder
	depth          int
	delimiterLines []int
	continuation   int
	atLineStart    bool
	prev           token.Token
	hasPrev        bool
	prevIndex      int
	index          int
	tokens         []token.Token
	generic        []bool
	// shift marks the halves of a `<<` or `>>`, which take no space between.
	shift        []bool
	sourceLine   int
	lastTopDecl  token.Type
	comments     []lineComment
	commentIdx   int
	blockStack   []blockState
	afterComment bool
	// inCapture is true between the pipes of a `|name|` payload capture; the
	// name hugs both pipes. A `|` that opens no capture is the bitwise or.
	inCapture bool
}

type blockKind int

type blockState struct {
	kind          blockKind
	delimiterBase int
}

const (
	normalBlock blockKind = iota
	// inlineLiteralBlock keeps a same-line aggregate literal compact. It does
	// not change structural indentation because all of its tokens stay on the
	// line that opened it.
	inlineLiteralBlock
	// commaTerminatedBlock is a block whose last entry keeps its comma. Enum
	// variants and match arms need it because the grammar requires it, and
	// struct fields keep it because SPEC §6.4 writes them that way.
	commaTerminatedBlock
)

// emit appends a token using current layout state.
func (b *builder) emit(t token.Token, next token.Token) {
	if t.Type == token.RBrace {
		b.emitRBrace(t, next)
		return
	}
	// Kizu single-line strings have no escape syntax. A literal containing a
	// quote therefore has to stay in the multiline form even when its value
	// contains no newline, or formatting would emit invalid source.
	if t.Type == token.String && strings.ContainsAny(t.Literal, "\"\n") {
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
	if b.hasPrev {
		b.continuation = b.continuationIndent(token.Token{})
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
	if !b.hasPrev || b.sourceLine == 0 || t.Line <= b.sourceLine {
		return
	}
	if b.atLineStart {
		if b.afterComment {
			b.continuation = b.continuationIndent(t)
		}
		return
	}
	b.out.WriteByte('\n')
	b.atLineStart = true
	b.continuation = b.continuationIndent(t)
}

// continuationIndent reports the indentation beyond the surrounding brace
// depth for one source-preserved line break. Group delimiters nest naturally;
// a broken expression outside delimiters gets one visible continuation level.
func (b *builder) continuationIndent(t token.Token) int {
	end := len(b.delimiterLines)
	if b.closesContinuationGroup(t) {
		closedLine := -1
		if end > b.currentDelimiterBase() {
			closedLine = b.delimiterLines[end-1]
			end--
		}
		groups := b.distinctDelimiterLines(end)
		if end > b.currentDelimiterBase() && b.delimiterLines[end-1] == closedLine {
			groups--
		}
		return groups
	}
	groups := b.distinctDelimiterLines(end)
	if groups > 0 {
		return groups
	}
	switch b.prev.Type {
	case token.LBrace, token.RBrace, token.Semicolon, token.Comma:
		return 0
	}
	return 1
}

// distinctDelimiterLines counts visible nesting rather than raw delimiters.
// `outer(inner(` opened on one line is one continuation level, while an inner
// call that starts on its own line adds another level.
func (b *builder) distinctDelimiterLines(end int) int {
	base := b.currentDelimiterBase()
	if end <= base {
		return 0
	}
	count := 0
	lastLine := -1
	for _, line := range b.delimiterLines[base:end] {
		if line != lastLine {
			count++
			lastLine = line
		}
	}
	return count
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
	if t.Type == token.Pipe {
		if b.inCapture {
			b.inCapture = false
		} else if b.captureOpensAt(b.index) {
			b.inCapture = true
		}
	}
	if b.opensContinuationGroup(t) {
		b.delimiterLines = append(b.delimiterLines, t.Line)
	} else if b.closesContinuationGroup(t) && len(b.delimiterLines) > 0 {
		b.delimiterLines = b.delimiterLines[:len(b.delimiterLines)-1]
	}
}

// captureOpensAt reports whether the `|` at i opens a payload capture: the
// form `|name| {` or `|a, b| {` (SPEC §6.9), which is the only place a `|`
// is not the bitwise or.
func (b *builder) captureOpensAt(i int) bool {
	j := i + 1
	for {
		if j >= len(b.tokens) || b.tokens[j].Type != token.Ident {
			return false
		}
		j++
		if j < len(b.tokens) && b.tokens[j].Type == token.Comma {
			j++
			continue
		}
		break
	}
	return j+1 < len(b.tokens) && b.tokens[j].Type == token.Pipe &&
		b.tokens[j+1].Type == token.LBrace
}

// opensContinuationGroup reports whether t adds one grouping delimiter to the
// continuation stack.
func (b *builder) opensContinuationGroup(t token.Token) bool {
	if t.Type == token.LParen || t.Type == token.LBracket {
		return true
	}
	return t.Type == token.LT && b.index < len(b.generic) && b.generic[b.index]
}

// closesContinuationGroup reports whether t retires one grouping delimiter.
func (b *builder) closesContinuationGroup(t token.Token) bool {
	if t.Type == token.RParen || t.Type == token.RBracket {
		return true
	}
	return t.Type == token.GT && b.index < len(b.generic) && b.generic[b.index]
}

// maybeTrailingNewline handles structural tokens that always force a line break.
func (b *builder) maybeTrailingNewline(t token.Token, next token.Token) {
	switch t.Type {
	case token.LBrace:
		kind := b.currentOpenBlockKind()
		b.blockStack = append(b.blockStack, blockState{
			kind:          kind,
			delimiterBase: len(b.delimiterLines),
		})
		if kind == inlineLiteralBlock {
			return
		}
		b.depth++
		if next.Type == token.RBrace {
			return
		}
		b.out.WriteByte('\n')
		b.atLineStart = true
	case token.Semicolon:
		b.out.WriteByte('\n')
		b.atLineStart = true
		b.continuation = 0
	}
}

// emitRBrace closes a block, deciding whether to emit a trailing newline.
func (b *builder) emitRBrace(t token.Token, next token.Token) {
	block := b.popBlock()
	if block.kind == inlineLiteralBlock {
		b.emitInlineRBrace(t, next)
		return
	}
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

// emitInlineRBrace closes a compact aggregate literal without treating it as
// a statement block.
func (b *builder) emitInlineRBrace(t token.Token, next token.Token) {
	if b.hasPrev && b.prev.Type != token.LBrace {
		b.out.WriteByte(' ')
	}
	b.writeToken(t)
	b.prev = t
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
		b.continuation = 0
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
	if opensInlineLiteralAtCurrentIndex(b.tokens, b.index) {
		return inlineLiteralBlock
	}
	return normalBlock
}

// currentBlockKind reports the innermost block kind without popping it.
func (b *builder) currentBlockKind() blockKind {
	if len(b.blockStack) == 0 {
		return normalBlock
	}
	return b.blockStack[len(b.blockStack)-1].kind
}

// popBlock removes and returns the current closing block state.
func (b *builder) popBlock() blockState {
	if len(b.blockStack) == 0 {
		return blockState{}
	}
	idx := len(b.blockStack) - 1
	block := b.blockStack[idx]
	b.blockStack = b.blockStack[:idx]
	return block
}

// currentDelimiterBase returns the grouping depth already present when the
// current brace block opened.
func (b *builder) currentDelimiterBase() int {
	if len(b.blockStack) == 0 {
		return 0
	}
	return b.blockStack[len(b.blockStack)-1].delimiterBase
}

// opensCommaTerminatedBlockAtCurrentIndex reports whether tokens[index] opens
// a struct or tagged declaration, or a runtime match arm list.
func opensCommaTerminatedBlockAtCurrentIndex(tokens []token.Token, index int) bool {
	if index < 0 || index >= len(tokens) || tokens[index].Type != token.LBrace {
		return false
	}
	if opensErrorSetBlock(tokens, index) {
		return true
	}
	// Walk back to the keyword that introduced this `{`. Another brace or a
	// semicolon means the keyword belongs to an enclosing construct, not this
	// block, which is what keeps a match arm's own `{ ... }` body normal.
	for cursor := index - 1; cursor >= 0; cursor-- {
		switch tokens[cursor].Type {
		case token.Struct, token.Enum, token.Union:
			return true
		case token.Match:
			return cursor == 0 || tokens[cursor-1].Type != token.Comptime
		case token.LBrace, token.RBrace, token.Semicolon:
			return false
		}
	}
	return false
}

// opensErrorSetBlock reports whether the current brace follows `error Name`.
// Looking for any earlier identifier spelled `error` also catches an ordinary
// function or method named error and incorrectly adds an enum-style comma to
// its final statement.
func opensErrorSetBlock(tokens []token.Token, index int) bool {
	return index >= 2 &&
		tokens[index-1].Type == token.Ident &&
		tokens[index-2].Type == token.Ident &&
		tokens[index-2].Literal == "error"
}

// opensInlineLiteralAtCurrentIndex recognizes a same-line aggregate literal.
// A top-level colon distinguishes fields from an ordinary statement block;
// declaration and control-flow keywords keep their braces structural.
func opensInlineLiteralAtCurrentIndex(tokens []token.Token, index int) bool {
	if index <= 0 || index >= len(tokens) || tokens[index].Type != token.LBrace {
		return false
	}
	if !canOpenAggregateLiteral(tokens[index-1]) || braceStartsBody(tokens, index) {
		return false
	}
	line := tokens[index].Line
	depth := 0
	hasField := false
	for cursor := index + 1; cursor < len(tokens); cursor++ {
		t := tokens[cursor]
		switch t.Type {
		case token.LBrace:
			depth++
		case token.RBrace:
			if depth == 0 {
				return hasField && t.Line == line
			}
			depth--
		case token.Colon:
			if depth == 0 {
				hasField = true
			}
		case token.Semicolon, token.EOF:
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

// canOpenAggregateLiteral reports whether t can name an aggregate constructor.
func canOpenAggregateLiteral(t token.Token) bool {
	return t.Type == token.Ident || t.Type == token.GT
}

// braceStartsBody distinguishes declaration and control-flow bodies from
// aggregate literals whose opening token also follows an identifier.
func braceStartsBody(tokens []token.Token, index int) bool {
	if opensErrorSetBlock(tokens, index) {
		return true
	}
	for cursor := index - 1; cursor >= 0; cursor-- {
		switch tokens[cursor].Type {
		case token.Function, token.Struct, token.Enum, token.Union, token.Contract,
			token.Impl, token.If, token.Else, token.While, token.For, token.Match:
			return true
		case token.Ident:
			if tokens[cursor].Literal == "test" {
				return true
			}
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
	for i := 0; i < b.depth+b.continuation; i++ {
		b.out.WriteString(indentUnit)
	}
	b.continuation = 0
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
	if b.tightGenericBracket(curr) || b.shiftHalves(curr) {
		return false
	}
	if curr.Type == token.LParen {
		return b.parenTakesSpace(prev)
	}
	if b.attachedTokenHugsLeft(curr, prev) {
		return false
	}
	if noSpaceBefore(curr) {
		return false
	}
	if noSpaceAfter(prev) && !b.binaryAmp() {
		return false
	}
	if prev.Type == token.Minus && b.signMinus() {
		return false
	}
	// A `|name|` capture hugs its pipes: no space after the opening one and
	// none before the closing one.
	if b.inCapture && (curr.Type == token.Pipe || prev.Type == token.Pipe) {
		return false
	}
	if prev.Type == token.RBracket && canFollowSliceMarker(curr) {
		return false
	}
	return true
}

// shiftHalves reports whether prev and curr are the two halves of one shift.
func (b *builder) shiftHalves(curr token.Token) bool {
	return b.index < len(b.shift) && b.shift[b.index] &&
		b.prevIndex < len(b.shift) && b.shift[b.prevIndex] && b.prev.Type == curr.Type
}

// binaryAmp reports whether the `&` just written is the bitwise and rather
// than a borrow: it follows an operand, the way a subtraction's `-` does.
func (b *builder) binaryAmp() bool {
	return b.prev.Type == token.Amp && b.prevIndex > 0 && endsOperand(b.tokens[b.prevIndex-1])
}

// parenTakesSpace distinguishes a receiver or grouping parenthesis from a
// call parenthesis. A generic `>` ends a callee; a comparison `>` does not.
func (b *builder) parenTakesSpace(prev token.Token) bool {
	if prev.Type == token.Function {
		return b.functionOpensDeclaration()
	}
	switch prev.Type {
	case token.Ident, token.RParen, token.RBracket:
		return false
	case token.GT:
		return b.prevIndex >= len(b.generic) || !b.generic[b.prevIndex]
	}
	return !noSpaceAfter(prev)
}

// functionOpensDeclaration reports whether the `fn` just emitted begins a
// declaration, whose receiver parenthesis takes a space, rather than a
// function pointer type, whose parameter list hugs the keyword (SPEC §7).
func (b *builder) functionOpensDeclaration() bool {
	if b.prevIndex == 0 {
		return true
	}
	switch b.tokens[b.prevIndex-1].Type {
	case token.Public, token.LBrace, token.RBrace, token.Semicolon:
		return true
	}
	return false
}

// attachedTokenHugsLeft reports whether curr is postfix indexing or the `!`
// between the two sides of a named error union.
func (b *builder) attachedTokenHugsLeft(curr token.Token, prev token.Token) bool {
	switch curr.Type {
	case token.LBracket, token.Bang:
		return b.attachedLeftOperand(prev)
	}
	return false
}

// attachedLeftOperand reports whether postfix syntax can hug the preceding
// value. A generic `>` qualifies; a comparison `>` does not.
func (b *builder) attachedLeftOperand(prev token.Token) bool {
	switch prev.Type {
	case token.Ident, token.String, token.RParen, token.RBracket, token.RBrace:
		return true
	case token.GT:
		return b.prevIndex < len(b.generic) && b.generic[b.prevIndex]
	}
	return false
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
		token.Dot, token.DoubleColon, token.Colon,
		token.Range:
		return true
	}
	return false
}

// noSpaceAfter reports whether t never takes a following space.
func noSpaceAfter(t token.Token) bool {
	switch t.Type {
	case token.LParen, token.LBracket, token.Dot, token.DoubleColon,
		token.Bang, token.Question, token.Amp, token.Tilde, token.Range:
		return true
	}
	return false
}

// signMinus reports whether the just-emitted `-` signs a value rather than
// subtracting: nothing before it, or a token that cannot end an operand. A
// postfix deref `.*` ends one, unlike the `*` of a product.
func (b *builder) signMinus() bool {
	if b.prevIndex == 0 {
		return true
	}
	before := b.tokens[b.prevIndex-1]
	if before.Type == token.Asterisk {
		return b.prevIndex < 2 || b.tokens[b.prevIndex-2].Type != token.Dot
	}
	return !endsOperand(before)
}

// endsOperand reports whether t can end an operand, which makes a following
// `-` a subtraction rather than a sign.
func endsOperand(t token.Token) bool {
	switch t.Type {
	case token.Ident, token.Int, token.Float, token.String, token.True, token.False,
		token.Null, token.RParen, token.RBracket:
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
	case token.Import, token.Public, token.Extern, token.Export,
		token.Function, token.Struct, token.Enum, token.Union, token.Contract, token.Impl:
		return true
	case token.Ident:
		return t.Literal == "test" || t.Literal == "error"
	}
	return false
}

// endsWithBlankLine reports whether the buffer already ends with `\n\n`.
func endsWithBlankLine(out *strings.Builder) bool {
	s := out.String()
	n := len(s)
	return n >= 2 && s[n-1] == '\n' && s[n-2] == '\n'
}
