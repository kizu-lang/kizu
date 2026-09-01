package wasm

import (
	"fmt"
	"strings"
)

// Module is the single parsed WebAssembly module both text and binary
// renderers consume. The current IR lowerer fills it through its canonical WAT
// spelling; keeping that spelling makes the migration to structured sections
// observable-output neutral.
type Module struct {
	text  string
	nodes []moduleNode
	root  int
}

type moduleNodeKind uint8

const (
	moduleAtom moduleNodeKind = iota
	moduleString
	moduleList
)

// moduleNode is one atom, quoted byte string, or list in the generated WAT.
// Text nodes retain source spans so parsing does not copy every symbol.
type moduleNode struct {
	kind     moduleNodeKind
	start    int
	end      int
	children []int
}

// WAT renders the inspection form of this module.
func (m *Module) WAT() string {
	return m.text
}

// nodeText returns the source bytes named by one atom or quoted string.
func (m *Module) nodeText(index int) string {
	node := m.nodes[index]
	return m.text[node.start:node.end]
}

// nodeKind returns the parsed kind of one stable node index.
func (m *Module) nodeKind(index int) moduleNodeKind {
	return m.nodes[index].kind
}

// listHead returns the first atom in one list, or an empty string when the
// node is not a headed list.
func (m *Module) listHead(index int) string {
	if m.nodeKind(index) != moduleList {
		return ""
	}
	children := m.nodes[index].children
	if len(children) == 0 || m.nodeKind(children[0]) != moduleAtom {
		return ""
	}
	return m.nodeText(children[0])
}

// stringBytes decodes one generated WAT byte string.
func (m *Module) stringBytes(index int) ([]byte, error) {
	if m.nodeKind(index) != moduleString {
		return nil, fmt.Errorf("wasm error: invalid generated WAT: expected string")
	}
	text := m.nodeText(index)
	out := make([]byte, 0, len(text))
	for position := 0; position < len(text); position++ {
		if text[position] != '\\' {
			out = append(out, text[position])
			continue
		}
		if position+1 >= len(text) {
			return nil, fmt.Errorf("wasm error: invalid generated WAT: unclosed string escape")
		}
		if position+2 < len(text) {
			high, highOK := hexNibble(text[position+1])
			low, lowOK := hexNibble(text[position+2])
			if highOK && lowOK {
				out = append(out, high<<4|low)
				position += 2
				continue
			}
		}
		position++
		switch text[position] {
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case '\\', '"':
			out = append(out, text[position])
		default:
			return nil, fmt.Errorf("wasm error: invalid generated WAT: invalid string escape")
		}
	}
	return out, nil
}

// hexNibble decodes one hexadecimal WAT string digit.
func hexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

// parseModule validates and retains the canonical WAT emitted by the IR
// lowerer. It is deliberately private: user input is Kizu source, not WAT.
func parseModule(text string) (*Module, error) {
	parser := moduleParser{text: text}
	root, err := parser.parseNode()
	if err != nil {
		return nil, fmt.Errorf("wasm error: invalid generated WAT: %w", err)
	}
	parser.skipSpace()
	if parser.position != len(text) {
		return nil, fmt.Errorf("wasm error: invalid generated WAT: trailing text at byte %d",
			parser.position)
	}
	module := &Module{text: text, nodes: parser.nodes, root: root}
	rootNode := module.nodes[root]
	if rootNode.kind != moduleList || len(rootNode.children) == 0 {
		return nil, fmt.Errorf("wasm error: invalid generated WAT: expected module")
	}
	head := module.nodes[rootNode.children[0]]
	if head.kind != moduleAtom || module.nodeText(rootNode.children[0]) != "module" {
		return nil, fmt.Errorf("wasm error: invalid generated WAT: expected module")
	}
	return module, nil
}

type moduleParser struct {
	text     string
	position int
	nodes    []moduleNode
}

// parseNode reads one list, quoted byte string, or atom at the current
// position.
func (p *moduleParser) parseNode() (int, error) {
	p.skipSpace()
	if p.position >= len(p.text) {
		return 0, fmt.Errorf("expected node at byte %d", p.position)
	}
	switch p.text[p.position] {
	case '(':
		return p.parseList()
	case ')':
		return 0, fmt.Errorf("unexpected `)` at byte %d", p.position)
	case '"':
		return p.parseString()
	default:
		return p.parseAtom()
	}
}

// parseList reads a balanced parenthesized node and all of its children.
func (p *moduleParser) parseList() (int, error) {
	p.position++
	children := []int{}
	for {
		p.skipSpace()
		if p.position >= len(p.text) {
			return 0, fmt.Errorf("unclosed list")
		}
		if p.text[p.position] == ')' {
			p.position++
			return p.addNode(moduleNode{kind: moduleList, children: children}), nil
		}
		child, err := p.parseNode()
		if err != nil {
			return 0, err
		}
		children = append(children, child)
	}
}

// parseString reads one quoted WAT byte string while retaining its source
// span without the quotes.
func (p *moduleParser) parseString() (int, error) {
	p.position++
	start := p.position
	for p.position < len(p.text) {
		switch p.text[p.position] {
		case '"':
			end := p.position
			p.position++
			return p.addNode(moduleNode{kind: moduleString, start: start, end: end}), nil
		case '\\':
			p.position += 2
			if p.position > len(p.text) {
				return 0, fmt.Errorf("unclosed string")
			}
		default:
			p.position++
		}
	}
	return 0, fmt.Errorf("unclosed string")
}

// parseAtom reads one unquoted token up to whitespace or a parenthesis.
func (p *moduleParser) parseAtom() (int, error) {
	start := p.position
	for p.position < len(p.text) {
		byteValue := p.text[p.position]
		if moduleSpace(byteValue) || byteValue == '(' || byteValue == ')' {
			break
		}
		p.position++
	}
	if p.position == start {
		return 0, fmt.Errorf("expected atom at byte %d", start)
	}
	return p.addNode(moduleNode{kind: moduleAtom, start: start, end: p.position}), nil
}

// skipSpace advances past WAT whitespace between nodes.
func (p *moduleParser) skipSpace() {
	for p.position < len(p.text) && moduleSpace(p.text[p.position]) {
		p.position++
	}
}

// addNode retains one parsed node and returns its stable index.
func (p *moduleParser) addNode(node moduleNode) int {
	index := len(p.nodes)
	p.nodes = append(p.nodes, node)
	return index
}

// moduleSpace reports whether a byte separates generated WAT nodes.
func moduleSpace(value byte) bool {
	return strings.ContainsRune(" \t\r\n", rune(value))
}
