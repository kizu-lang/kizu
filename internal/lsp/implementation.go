package lsp

import "github.com/kizu-lang/kizu/internal/token"

// implBlock captures one `impl Contract for Type` block: which contract it
// satisfies, the implementing type, and the method names it defines.
type implBlock struct {
	contract  string
	typeName  string
	typeRange Range
	uri       string
	methods   map[string]Range
}

// implementation resolves the contract (or contract method) under the cursor to
// its concrete implementations. Pointing at a contract name jumps to each
// implementing type; pointing at a contract method jumps to each implementation
// of that method. It returns an empty slice when nothing implements the symbol.
func (s *Server) implementation(uri string, position Position) []location {
	source, ok := s.documents[uri]
	if !ok {
		return []location{}
	}
	tokens := lexCompletionTokens(source)
	tokIdx := tokenIndexAtPosition(tokens, position)
	if tokIdx < 0 || tokens[tokIdx].Type != token.Ident {
		return []location{}
	}
	name := tokens[tokIdx].Literal
	index, sources := s.navigationIndex(uri, source)

	contractName, methodName := contractTarget(tokens, tokIdx, name, index)
	if contractName == "" {
		return []location{}
	}
	return collectImplementations(sources, contractName, methodName)
}

// contractTarget decides which contract (and optionally which of its methods)
// the cursor refers to: a method name inside a contract body, or a contract
// name used anywhere else.
func contractTarget(
	tokens []token.Token,
	tokIdx int,
	name string,
	index navigationIndex,
) (contractName string, methodName string) {
	if enclosing := enclosingContractName(tokens, tokenRange(tokens[tokIdx]).Start); enclosing != "" {
		if tokIdx > 0 && tokens[tokIdx-1].Type == token.Function {
			return enclosing, name
		}
	}
	if decl, ok := index.types[name]; ok && decl.kind == symbolKindInterface {
		return name, ""
	}
	return "", ""
}

// collectImplementations gathers locations across all package sources for the
// given contract, narrowing to a single method when methodName is set.
func collectImplementations(
	sources []navigationSource,
	contractName string,
	methodName string,
) []location {
	locations := []location{}
	for _, src := range sources {
		for _, block := range implBlocksIn(src.uri, lexCompletionTokens(src.source)) {
			if block.contract != contractName {
				continue
			}
			if methodName == "" {
				locations = append(locations, location{URI: block.uri, Range: block.typeRange})
				continue
			}
			if rng, ok := block.methods[methodName]; ok {
				locations = append(locations, location{URI: block.uri, Range: rng})
			}
		}
	}
	return locations
}

// implBlocksIn scans one source for every contract-implementing impl block.
func implBlocksIn(uri string, tokens []token.Token) []implBlock {
	blocks := []implBlock{}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.Impl {
			continue
		}
		block, end, ok := readImplBlock(uri, tokens, i)
		if ok {
			blocks = append(blocks, block)
		}
		i = end
	}
	return blocks
}

// readImplBlock parses a single impl block starting at the impl keyword. It
// reports false for inherent impls (those without a `for` contract clause).
func readImplBlock(uri string, tokens []token.Token, start int) (implBlock, int, bool) {
	forIdx, braceIdx := -1, -1
	for j := start + 1; j < len(tokens); j++ {
		if tokens[j].Type == token.For && forIdx < 0 {
			forIdx = j
		}
		if tokens[j].Type == token.LBrace || tokens[j].Type == token.EOF {
			braceIdx = j
			break
		}
	}
	if braceIdx < 0 || tokens[braceIdx].Type != token.LBrace {
		return implBlock{}, start, false
	}
	end := skipBalanced(tokens, braceIdx, token.LBrace, token.RBrace)
	if forIdx < 0 {
		return implBlock{}, end, false
	}
	contractIdx := lastIdentBetween(tokens, start+1, forIdx)
	typeIdx := lastIdentBetween(tokens, forIdx+1, braceIdx)
	if contractIdx < 0 || typeIdx < 0 {
		return implBlock{}, end, false
	}
	return implBlock{
		contract:  tokens[contractIdx].Literal,
		typeName:  tokens[typeIdx].Literal,
		typeRange: tokenRange(tokens[typeIdx]),
		uri:       uri,
		methods:   implMethodRanges(tokens, braceIdx, end),
	}, end, true
}

// implMethodRanges records each method name and its range within an impl body.
func implMethodRanges(tokens []token.Token, braceIdx int, end int) map[string]Range {
	methods := map[string]Range{}
	for i := braceIdx + 1; i < end; i++ {
		if tokens[i].Type != token.Function || i+1 >= len(tokens) || tokens[i+1].Type != token.Ident {
			continue
		}
		methods[tokens[i+1].Literal] = tokenRange(tokens[i+1])
		i = skipDeclarationBody(tokens, i+1)
	}
	return methods
}

// enclosingContractName returns the contract whose body contains position.
func enclosingContractName(tokens []token.Token, position Position) string {
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Type != token.Contract {
			continue
		}
		nameIdx := nextIdentifierIndex(tokens, i+1)
		if nameIdx < 0 {
			continue
		}
		brace := findNextToken(tokens, nameIdx+1, token.LBrace)
		if brace < 0 {
			continue
		}
		end := skipBalanced(tokens, brace, token.LBrace, token.RBrace)
		bodyStart := tokenRange(tokens[brace]).End
		bodyEnd := tokenRange(tokens[end]).Start
		if positionLessEqual(bodyStart, position) && positionBefore(position, bodyEnd) {
			return tokens[nameIdx].Literal
		}
		i = end
	}
	return ""
}

// lastIdentBetween returns the index of the final identifier token in [from,to),
// so module-qualified names such as pkg::Type resolve to their last segment.
func lastIdentBetween(tokens []token.Token, from int, to int) int {
	result := -1
	for i := from; i < to && i < len(tokens); i++ {
		if tokens[i].Type == token.Ident {
			result = i
		}
	}
	return result
}
