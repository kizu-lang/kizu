package lsp

import (
	"sort"
	"strings"
)

// codeActionOrganizeImports is the LSP kind for the import-sorting action.
const codeActionOrganizeImports = "source.organizeImports"

// codeActions offers refactors available for a document. Currently it provides
// "Organize Imports", which alphabetically sorts each run of consecutive import
// statements. The action is omitted when nothing would change.
func (s *Server) codeActions(uri string) []codeAction {
	source, ok := s.documents[uri]
	if !ok {
		return []codeAction{}
	}
	edits := organizeImportEdits(source)
	if len(edits) == 0 {
		return []codeAction{}
	}
	return []codeAction{{
		Title: "Organize Imports",
		Kind:  codeActionOrganizeImports,
		Edit:  &workspaceEdit{Changes: map[string][]textEdit{uri: edits}},
	}}
}

// organizeImportEdits returns the edits that sort each maximal run of adjacent
// import lines. Because comment and blank lines are not import lines, they break
// runs and are never reordered, keeping any doc comments attached.
func organizeImportEdits(source string) []textEdit {
	lines := strings.Split(source, "\n")
	edits := []textEdit{}
	for start := 0; start < len(lines); {
		if !isImportLine(lines[start]) {
			start++
			continue
		}
		end := start
		for end < len(lines) && isImportLine(lines[end]) {
			end++
		}
		if edit, ok := sortImportRun(lines, start, end); ok {
			edits = append(edits, edit)
		}
		start = end
	}
	return edits
}

// sortImportRun builds an edit that replaces lines [start,end) with their sorted
// order, reporting false when the run holds fewer than two lines or is already
// sorted.
func sortImportRun(lines []string, start int, end int) (textEdit, bool) {
	if end-start < 2 {
		return textEdit{}, false
	}
	original := append([]string{}, lines[start:end]...)
	sorted := append([]string{}, original...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.TrimSpace(sorted[i]) < strings.TrimSpace(sorted[j])
	})
	if equalLines(original, sorted) {
		return textEdit{}, false
	}
	return textEdit{
		Range: Range{
			Start: Position{Line: start, Character: 0},
			End:   Position{Line: end, Character: 0},
		},
		NewText: strings.Join(sorted, "\n") + "\n",
	}, true
}

// isImportLine reports whether a source line is a bare import statement.
func isImportLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "import ")
}

// equalLines reports whether two line slices are identical in order.
func equalLines(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
