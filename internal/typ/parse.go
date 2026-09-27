package typ

import (
	"fmt"
	"strconv"
	"strings"
)

// parser reads one type spelling left to right.
type parser struct {
	input string
	pos   int
}

// parseType reads a type, including the `E!T` spelling whose error type sits to
// the left of the `!`.
func (p *parser) parseType() (Type, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	if !p.accept("!") {
		return left, nil
	}
	ok, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	return &ErrorUnion{Err: left, Ok: ok}, nil
}

// parsePrefix reads the spellings that wrap a type from the left.
func (p *parser) parsePrefix() (Type, error) {
	switch {
	case p.accept("!"):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &ErrorUnion{Ok: elem}, nil
	case p.peekByte('['):
		return p.parseBracketed()
	case p.accept("&var "):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Borrow{Elem: elem, Mut: true}, nil
	case p.accept("&"):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Borrow{Elem: elem}, nil
	case p.accept("?"):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Optional{Elem: elem}, nil
	case p.accept("unsafe fn("):
		return p.parseFunc(true, "")
	case p.accept("extern \"c\" fn("):
		return p.parseFunc(false, "c")
	case p.accept("fn("):
		return p.parseFunc(false, "")
	case p.accept("const "):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Const{Elem: elem}, nil
	default:
		return p.parseName()
	}
}

// parseFunc reads a function pointer spelling once its `fn(` is consumed. An
// empty parameter list is the `fn() -> T` a nullary function points at.
func (p *parser) parseFunc(unsafeFn bool, abi string) (Type, error) {
	out := &Func{Unsafe: unsafeFn, ABI: abi}
	if !p.accept(")") {
		for {
			param, err := p.parseType()
			if err != nil {
				return nil, err
			}
			out.Params = append(out.Params, param)
			if p.accept(",") {
				p.accept(" ")
				continue
			}
			break
		}
		if !p.accept(")") {
			return nil, fmt.Errorf("type error: unclosed `(` in `%s`", p.input)
		}
	}
	if !p.accept(" -> ") {
		return nil, fmt.Errorf("type error: expected `->` in `%s`", p.input)
	}
	result, err := p.parseType()
	if err != nil {
		return nil, err
	}
	out.Result = result
	return out, nil
}

// peekByte reports whether the next unread byte is ch.
func (p *parser) peekByte(ch byte) bool {
	return p.pos < len(p.input) && p.input[p.pos] == ch
}

// parseBracketed reads the `[]T` and `[N]T` spellings.
func (p *parser) parseBracketed() (Type, error) {
	if p.accept("[]") {
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Slice{Elem: elem}, nil
	}
	return p.parseBuffer()
}

// parseBuffer reads a `[N]T` fixed-length buffer spelling. N is a number, a
// static name, or a static expression in its canonical parenthesized spelling.
func (p *parser) parseBuffer() (Type, error) {
	p.pos++
	start := p.pos
	if err := p.skipLength(); err != nil {
		return nil, err
	}
	length := p.input[start:p.pos]
	if !p.accept("]") {
		return nil, fmt.Errorf("type error: expected `]` in `%s`", p.input)
	}
	elem, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	if length[0] < '0' || length[0] > '9' {
		return &Buffer{Len: length, Elem: elem}, nil
	}
	size, err := strconv.ParseInt(length, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("type error: buffer size in `%s`: %v", p.input, err)
	}
	return &Buffer{Size: size, Elem: elem}, nil
}

// skipLength moves past a length: a run of name bytes, or a parenthesized
// expression, whose parentheses balance.
func (p *parser) skipLength() error {
	depth := 0
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		switch {
		case ch == '(':
			depth++
		case ch == ')' && depth > 0:
			depth--
		case depth == 0 && !isLengthByte(ch):
			if start == p.pos {
				return fmt.Errorf("type error: expected buffer size in `%s`", p.input)
			}
			return nil
		}
		p.pos++
	}
	return fmt.Errorf("type error: expected `]` in `%s`", p.input)
}

// isLengthByte reports whether ch may appear in a length outside parentheses.
func isLengthByte(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
}

// parseName reads a `::` separated name and the static arguments it applies.
func (p *parser) parseName() (Type, error) {
	path := []string{}
	for {
		part := p.parseIdent()
		if part == "" {
			return nil, fmt.Errorf("type error: expected a name in `%s`", p.input)
		}
		path = append(path, part)
		if !p.accept("::") {
			break
		}
	}
	name := &Name{Path: path}
	if !p.accept("<") {
		return name, nil
	}
	for {
		arg, err := p.parseType()
		if err != nil {
			return nil, err
		}
		name.Args = append(name.Args, arg)
		if p.accept(",") {
			p.accept(" ")
			continue
		}
		break
	}
	if !p.accept(">") {
		return nil, fmt.Errorf("type error: unclosed `<` in `%s`", p.input)
	}
	return name, nil
}

// parseIdent reads one name segment.
func (p *parser) parseIdent() string {
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		isWord := ch == '_' ||
			(ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9')
		if !isWord {
			break
		}
		p.pos++
	}
	return p.input[start:p.pos]
}

// accept consumes text when it is next, and reports whether it was.
func (p *parser) accept(text string) bool {
	if !strings.HasPrefix(p.input[p.pos:], text) {
		return false
	}
	p.pos += len(text)
	return true
}
