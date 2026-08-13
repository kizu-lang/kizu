package typ

import (
	"fmt"
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
	case p.accept("[]"):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Slice{Elem: elem}, nil
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
	case p.accept("dyn "):
		elem, err := p.parsePrefix()
		if err != nil {
			return nil, err
		}
		return &Dyn{Contract: elem}, nil
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
