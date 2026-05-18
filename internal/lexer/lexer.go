package lexer

import "github.com/kizu-lang/kizu/internal/token"

// Lexer scans Kizu source text into tokens.
type Lexer struct {
	input        []rune
	position     int
	readPosition int
	ch           rune
	line         int
	column       int
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
	'&': {next: '&', compound: token.AndAnd, single: token.Amp},
	'<': {next: '=', compound: token.LTE, single: token.LT},
	'>': {next: '=', compound: token.GTE, single: token.GT},
	'.': {next: '.', compound: token.Range, single: token.Dot},
	':': {next: ':', compound: token.DoubleColon, single: token.Colon},
	'|': {next: '|', compound: token.OrOr, single: token.Pipe},
}

// New creates a lexer for input.
func New(input string) *Lexer {
	l := &Lexer{input: []rune(input), line: 1}
	l.readChar()
	return l
}

// NextToken returns the next token from the input stream.
func (l *Lexer) NextToken() token.Token {
	l.skipWhitespace()

	tok := token.Token{Line: l.line, Column: l.column}

	switch l.ch {
	case '/':
		if l.peekChar() == '/' {
			l.skipLineComment()
			return l.NextToken()
		}
		tok = l.oneCharToken(token.Slash)
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
			tok.Type = token.Int
			tok.Literal = l.readNumber()
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
	return token.Token{Type: t, Literal: string(l.ch), Line: l.line, Column: l.column}
}

// twoCharToken returns a token spanning the current rune and the next rune.
func (l *Lexer) twoCharToken(t token.Type) token.Token {
	ch := l.ch
	line := l.line
	column := l.column
	l.readChar()
	return token.Token{Type: t, Literal: string([]rune{ch, l.ch}), Line: line, Column: column}
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

// skipWhitespace advances past insignificant whitespace.
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

// skipLineComment advances past a line comment.
func (l *Lexer) skipLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
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

// isLetter reports whether ch can appear in an identifier.
func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

// isDigit reports whether ch is an ASCII digit.
func isDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}
