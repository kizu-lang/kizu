package token

// Type identifies the kind of lexical token.
type Type string

const (
	Illegal Type = "ILLEGAL"
	EOF     Type = "EOF"

	Ident  Type = "IDENT"
	Int    Type = "INT"
	String Type = "STRING"

	Assign   Type = "="
	Plus     Type = "+"
	Minus    Type = "-"
	Bang     Type = "!"
	Question Type = "?"
	Asterisk Type = "*"
	Slash    Type = "/"
	Percent  Type = "%"

	Eq       Type = "=="
	FatArrow Type = "=>"
	NotEq    Type = "!="
	LT       Type = "<"
	LTE      Type = "<="
	GT       Type = ">"
	GTE      Type = ">="
	Arrow    Type = "->"
	Dot      Type = "."

	Comma     Type = ","
	Colon     Type = ":"
	Semicolon Type = ";"

	LParen   Type = "("
	RParen   Type = ")"
	LBrace   Type = "{"
	RBrace   Type = "}"
	LBracket Type = "["
	RBracket Type = "]"

	Function Type = "fn"
	Let      Type = "let"
	Var      Type = "var"
	Return   Type = "return"
	If       Type = "if"
	Else     Type = "else"
	While    Type = "while"
	Match    Type = "match"
	Struct   Type = "struct"
	Enum     Type = "enum"
	Union    Type = "union"
	Contract Type = "contract"
	Satisfy  Type = "satisfy"
	For      Type = "for"
	Impl     Type = "impl"
	True     Type = "true"
	False    Type = "false"
	Borrow   Type = "borrow"
	Unsafe   Type = "unsafe"
	Extern   Type = "extern"
	Comptime Type = "comptime"
	Try      Type = "try"
)

type Token struct {
	Type    Type
	Literal string
	Line    int
	Column  int
}

var keywords = map[string]Type{
	"fn":       Function,
	"let":      Let,
	"var":      Var,
	"return":   Return,
	"if":       If,
	"else":     Else,
	"while":    While,
	"match":    Match,
	"struct":   Struct,
	"enum":     Enum,
	"union":    Union,
	"contract": Contract,
	"satisfy":  Satisfy,
	"for":      For,
	"impl":     Impl,
	"true":     True,
	"false":    False,
	"borrow":   Borrow,
	"unsafe":   Unsafe,
	"extern":   Extern,
	"comptime": Comptime,
	"try":      Try,
}

// LookupIdent returns the keyword token for ident or Ident for user names.
func LookupIdent(ident string) Type {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return Ident
}
