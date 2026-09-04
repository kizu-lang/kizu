package lexer

import (
	"strings"

	"github.com/kizu-lang/kizu/internal/source"
	"github.com/kizu-lang/kizu/internal/token"
)

// Lexer scans Kizu source text into tokens.
type Lexer struct {
	input        []rune
	position     int
	readPosition int
	ch           rune
	line         int
	column       int
	pending      comments
	source       source.ID
}

// comments are the comment lines the lexer keeps for the next real token: the
// `///` lines that describe what a declaration promises, and the `// SAFETY:`
// lines that say why a statement may break the compiler's proof. Every other
// comment is read and dropped.
type comments struct {
	doc    []string
	safety []string
}

var singleCharTokens = map[rune]token.Type{
	'+': token.Plus,
	// skipIgnored has already consumed every `//`, so a `/` left for the
	// scanner is always division.
	'/': token.Slash,
	'&': token.Amp,
	'*': token.Asterisk,
	'%': token.Percent,
	'^': token.Caret,
	'~': token.Tilde,
	'@': token.At,
	'?': token.Question,
	'|': token.Pipe,
	',': token.Comma,
	':': token.Colon,
	';': token.Semicolon,
	'(': token.LParen,
	')': token.RParen,
	'{': token.LBrace,
	'}': token.RBrace,
	'[': token.LBracket,
	']': token.RBracket,
}

type compoundToken struct {
	next     rune
	compound token.Type
	single   token.Type
}

var compoundTokens = map[rune]compoundToken{
	'=': {next: '=', compound: token.Eq, single: token.Assign},
	'-': {next: '>', compound: token.Arrow, single: token.Minus},
	'!': {next: '=', compound: token.NotEq, single: token.Bang},
	'<': {next: '=', compound: token.LTE, single: token.LT},
	'>': {next: '=', compound: token.GTE, single: token.GT},
	'.': {next: '.', compound: token.Range, single: token.Dot},
	':': {next: ':', compound: token.DoubleColon, single: token.Colon},
}

// New creates a lexer for input.
func New(input string) *Lexer {
	return NewFile("", input)
}

// NewFile creates a lexer that stamps file onto every token it produces, so a
// diagnostic built from one can name the source it came from.
func NewFile(file string, input string) *Lexer {
	sources := source.NewMap()
	return NewSource(sources.Add(file, input))
}

// NewSource creates a lexer over one record already owned by a source map.
func NewSource(input source.ID) *Lexer {
	l := &Lexer{input: []rune(input.Text()), line: 1, source: input}
	l.readChar()
	return l
}

// NextToken returns the next token from the input stream.
//
// skipIgnored has already read every comment before the token, so the comments
// it kept are stamped on here rather than threaded through each scan helper.
func (l *Lexer) NextToken() token.Token {
	l.skipIgnored()
	kept := l.pending
	l.pending = comments{}
	tok := l.scanToken()
	tok.DocComments = kept.doc
	tok.Safety = kept.safety
	return tok
}

// scanToken reads the token at the current position.
func (l *Lexer) scanToken() token.Token {
	tok := token.Token{Source: l.source, Line: l.line, Column: l.column}

	switch l.ch {
	case '=':
		if l.peekChar() == '>' {
			tok = l.twoCharToken(token.FatArrow)
			break
		}
		tok = l.readCompoundToken(compoundTokens[l.ch])
	case '"':
		tok.Type = token.String
		tok.Literal = l.readString()
		return tok
	case '\\':
		if l.peekChar() == '\\' {
			tok.Type = token.String
			tok.Literal = l.readMultilineString()
			return tok
		}
		tok = l.oneCharToken(token.Illegal)
	case 0:
		tok.Type = token.EOF
		tok.Literal = ""
	default:
		if spec, ok := compoundTokens[l.ch]; ok {
			tok = l.readCompoundToken(spec)
			break
		}
		if tokType, ok := singleCharTokens[l.ch]; ok {
			tok = l.oneCharToken(tokType)
			break
		}
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			return tok
		}
		if isDigit(l.ch) {
			tok.Literal, tok.Type = l.readNumber()
			return tok
		}
		tok = l.oneCharToken(token.Illegal)
	}

	l.readChar()
	return tok
}

// readCompoundToken reads an operator that may have a two-character spelling.
func (l *Lexer) readCompoundToken(spec compoundToken) token.Token {
	if l.peekChar() == spec.next {
		return l.twoCharToken(spec.compound)
	}
	return l.oneCharToken(spec.single)
}

// oneCharToken returns a token for the current rune.
func (l *Lexer) oneCharToken(t token.Type) token.Token {
	return token.Token{
		Type:    t,
		Literal: string(l.ch),
		Source:  l.source,
		Line:    l.line,
		Column:  l.column,
	}
}

// twoCharToken returns a token spanning the current rune and the next rune.
func (l *Lexer) twoCharToken(t token.Type) token.Token {
	ch := l.ch
	line := l.line
	column := l.column
	l.readChar()
	return token.Token{
		Type:    t,
		Literal: string([]rune{ch, l.ch}),
		Source:  l.source,
		Line:    line,
		Column:  column,
	}
}

// readChar advances the lexer by one rune.
func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
	if l.ch == '\n' {
		l.line++
		l.column = 0
	} else {
		l.column++
	}
}

// peekChar returns the next rune without advancing.
func (l *Lexer) peekChar() rune {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

// peekRune returns a rune at an offset from the current lexer position.
func (l *Lexer) peekRune(offset int) rune {
	position := l.position + offset
	if position < 0 || position >= len(l.input) {
		return 0
	}
	return l.input[position]
}

// skipIgnored advances past insignificant whitespace and comments, keeping the
// comment lines that belong to the token that follows. A blank line separates a
// comment from what comes after it, so it drops what has been kept so far.
func (l *Lexer) skipIgnored() {
	for {
		if l.skipWhitespace() {
			l.pending = comments{}
		}
		if l.ch != '/' || l.peekChar() != '/' {
			return
		}
		// Neither kept kind clears the other: a declaration may be described
		// and its statement justified without one comment cancelling the
		// other. Only a blank line or an ordinary comment clears both.
		if l.isDocCommentStart() {
			l.pending.doc = append(l.pending.doc, l.readDocComment())
			continue
		}
		if text, ok := l.readSafetyComment(); ok {
			l.pending.safety = append(l.pending.safety, text)
			continue
		}
		l.pending = comments{}
		l.skipLineComment()
	}
}

// skipWhitespace advances past insignificant whitespace and reports blank lines.
func (l *Lexer) skipWhitespace() bool {
	newlines := 0
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		if l.ch == '\n' {
			newlines++
		}
		l.readChar()
	}
	return newlines > 1
}

// skipLineComment advances past a line comment.
func (l *Lexer) skipLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

// isDocCommentStart reports whether the current line starts an exact `///` doc comment.
func (l *Lexer) isDocCommentStart() bool {
	return l.peekRune(2) == '/' && l.peekRune(3) != '/'
}

// safetyPrefix is the fixed marker a justification comment starts with. It is
// ASCII and case sensitive so that what the compiler requires is one spelling,
// not a family of near misses. What follows it is free text.
const safetyPrefix = "SAFETY:"

// readSafetyComment reads a `// SAFETY:` line and returns the text after the
// prefix. It reports false without consuming anything when the line is an
// ordinary comment.
func (l *Lexer) readSafetyComment() (string, bool) {
	offset := 2
	for l.peekRune(offset) == ' ' || l.peekRune(offset) == '\t' {
		offset++
	}
	for i, want := range safetyPrefix {
		if l.peekRune(offset+i) != want {
			return "", false
		}
	}
	for i := 0; i < offset+len(safetyPrefix); i++ {
		l.readChar()
	}
	start := l.position
	l.skipLineComment()
	return strings.TrimSpace(string(l.input[start:l.position])), true
}

// readDocComment advances past one doc comment and returns its normalized text.
func (l *Lexer) readDocComment() string {
	l.readChar()
	l.readChar()
	l.readChar()
	if l.ch == ' ' {
		l.readChar()
	}
	start := l.position
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
	end := l.position
	if end > start && l.input[end-1] == '\r' {
		end--
	}
	return string(l.input[start:end])
}

// readIdentifier reads an identifier or keyword literal.
func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return string(l.input[position:l.position])
}

// readNumber reads a number literal and reports its kind. An integer is
// decimal, or `0x` / `0b` followed by its digits; a float is decimal digits
// with a fraction after `.`, an exponent after `e`, or both. `_` is allowed
// between digits. The scanner only finds where the literal ends; the parser
// decides whether the spelling is a number. A `.` not followed by a digit
// is left alone, so `0..n` stays a range.
func (l *Lexer) readNumber() (string, token.Type) {
	position := l.position
	if l.ch == '0' && (l.peekChar() == 'x' || l.peekChar() == 'b') {
		l.readChar()
		l.readChar()
		for isHexDigit(l.ch) || l.ch == '_' {
			l.readChar()
		}
		return string(l.input[position:l.position]), token.Int
	}
	kind := token.Int
	l.readDigits()
	if l.readFraction() {
		kind = token.Float
	}
	if l.readExponent() {
		kind = token.Float
	}
	return string(l.input[position:l.position]), kind
}

// readDigits consumes decimal digits and the `_` between them.
func (l *Lexer) readDigits() {
	for isDigit(l.ch) || l.ch == '_' {
		l.readChar()
	}
}

// readFraction consumes `.digits` when a digit follows the dot, reporting
// whether it did.
func (l *Lexer) readFraction() bool {
	if l.ch != '.' || !isDigit(l.peekChar()) {
		return false
	}
	l.readChar()
	l.readDigits()
	return true
}

// readExponent consumes `e[+-]digits` when one follows, reporting whether it
// did. An `e` with no digit after it is left for the next token.
func (l *Lexer) readExponent() bool {
	if l.ch != 'e' && l.ch != 'E' {
		return false
	}
	next := l.peekChar()
	if !isDigit(next) && next != '+' && next != '-' {
		return false
	}
	l.readChar()
	if l.ch == '+' || l.ch == '-' {
		l.readChar()
	}
	for isDigit(l.ch) {
		l.readChar()
	}
	return true
}

// isHexDigit reports whether ch can appear in a `0x` literal. Decimal and
// binary literals read the same set so that `0b12` becomes one bad token
// rather than a number followed by an identifier.
func isHexDigit(ch rune) bool {
	return isDigit(ch) || ('a' <= ch && ch <= 'f') || ('A' <= ch && ch <= 'F')
}

// readString reads a string literal without the surrounding quotes.
func (l *Lexer) readString() string {
	l.readChar()
	position := l.position
	for l.ch != '"' && l.ch != 0 {
		l.readChar()
	}
	out := string(l.input[position:l.position])
	if l.ch == '"' {
		l.readChar()
	}
	return out
}

// readMultilineString reads one or more `\\`-prefixed lines and joins them with newlines.
//
// Each segment consists of `\\` followed by the rest of the line. Consecutive segments
// separated only by whitespace and a single newline are concatenated with `\n`.
func (l *Lexer) readMultilineString() string {
	var out []rune
	first := true
	for {
		l.readChar() // consume first '\\'
		l.readChar() // consume second '\\'
		start := l.position
		for l.ch != '\n' && l.ch != 0 {
			l.readChar()
		}
		if !first {
			out = append(out, '\n')
		}
		first = false
		out = append(out, l.input[start:l.position]...)
		if !l.peekMultilineContinuation() {
			break
		}
	}
	return string(out)
}

// peekMultilineContinuation reports whether the next non-blank line continues a `\\` string.
func (l *Lexer) peekMultilineContinuation() bool {
	if l.ch != '\n' {
		return false
	}
	pos := l.readPosition
	for pos < len(l.input) {
		ch := l.input[pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			pos++
			continue
		}
		if ch == '\\' && pos+1 < len(l.input) && l.input[pos+1] == '\\' {
			for l.readPosition <= pos {
				l.readChar()
			}
			return true
		}
		return false
	}
	return false
}

// isLetter reports whether ch can appear in an identifier.
func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

// isDigit reports whether ch is an ASCII digit.
func isDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}
