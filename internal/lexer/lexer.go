package lexer

import "github.com/kizu-lang/kizu/internal/token"

// Lexer scans Kizu source text into tokens.
type Lexer struct {
	input              []rune
	position           int
	readPosition       int
	ch                 rune
	line               int
	column             int
	pendingDocComments []string
}

var singleCharTokens = map[rune]token.Type{
	'+': token.Plus,
	'&': token.Amp,
	'*': token.Asterisk,
	'%': token.Percent,
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
	l := &Lexer{input: []rune(input), line: 1}
	l.readChar()
	return l
}

// NextToken returns the next token from the input stream.
func (l *Lexer) NextToken() token.Token {
	l.skipIgnored()

	tok := token.Token{Line: l.line, Column: l.column, DocComments: l.takeDocComments()}

	switch l.ch {
	case '/':
		if l.peekChar() == '/' {
			l.clearDocComments()
			l.skipLineComment()
			return l.NextToken()
		}
		tok = l.oneCharToken(token.Slash, tok.DocComments)
	case '=':
		if l.peekChar() == '>' {
			tok = l.twoCharToken(token.FatArrow, tok.DocComments)
			break
		}
		tok = l.readCompoundToken(compoundTokens[l.ch], tok.DocComments)
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
		tok = l.oneCharToken(token.Illegal, tok.DocComments)
	case 0:
		tok.Type = token.EOF
		tok.Literal = ""
	default:
		if spec, ok := compoundTokens[l.ch]; ok {
			tok = l.readCompoundToken(spec, tok.DocComments)
			break
		}
		if tokType, ok := singleCharTokens[l.ch]; ok {
			tok = l.oneCharToken(tokType, tok.DocComments)
			break
		}
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			return tok
		}
		if isDigit(l.ch) {
			tok.Type = token.Int
			tok.Literal = l.readNumber()
			return tok
		}
		tok = l.oneCharToken(token.Illegal, tok.DocComments)
	}

	l.readChar()
	return tok
}

// readCompoundToken reads an operator that may have a two-character spelling.
func (l *Lexer) readCompoundToken(spec compoundToken, docs []string) token.Token {
	if l.peekChar() == spec.next {
		return l.twoCharToken(spec.compound, docs)
	}
	return l.oneCharToken(spec.single, docs)
}

// oneCharToken returns a token for the current rune.
func (l *Lexer) oneCharToken(t token.Type, docs []string) token.Token {
	return token.Token{
		Type:        t,
		Literal:     string(l.ch),
		Line:        l.line,
		Column:      l.column,
		DocComments: docs,
	}
}

// twoCharToken returns a token spanning the current rune and the next rune.
func (l *Lexer) twoCharToken(t token.Type, docs []string) token.Token {
	ch := l.ch
	line := l.line
	column := l.column
	l.readChar()
	return token.Token{
		Type:        t,
		Literal:     string([]rune{ch, l.ch}),
		Line:        line,
		Column:      column,
		DocComments: docs,
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

// skipIgnored advances past insignificant whitespace and comments.
func (l *Lexer) skipIgnored() {
	for {
		if l.skipWhitespace() {
			l.clearDocComments()
		}
		if l.ch != '/' || l.peekChar() != '/' {
			return
		}
		if l.isDocCommentStart() {
			l.pendingDocComments = append(l.pendingDocComments, l.readDocComment())
			continue
		}
		l.clearDocComments()
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

// clearDocComments discards doc comments no longer adjacent to a declaration.
func (l *Lexer) clearDocComments() {
	l.pendingDocComments = nil
}

// takeDocComments returns and clears comments for the next real token.
func (l *Lexer) takeDocComments() []string {
	if len(l.pendingDocComments) == 0 {
		return nil
	}
	out := append([]string(nil), l.pendingDocComments...)
	l.pendingDocComments = nil
	return out
}

// readIdentifier reads an identifier or keyword literal.
func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return string(l.input[position:l.position])
}

// readNumber reads an integer literal.
func (l *Lexer) readNumber() string {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	return string(l.input[position:l.position])
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
	for {
		l.readChar() // consume first '\\'
		l.readChar() // consume second '\\'
		start := l.position
		for l.ch != '\n' && l.ch != 0 {
			l.readChar()
		}
		if len(out) > 0 {
			out = append(out, '\n')
		}
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
