package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	kizufmt "github.com/kizu-lang/kizu/internal/fmt"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/types"
)

// fmtCommand prints or rewrites a Kizu source file in canonical form.
//
// usage: kizu fmt [--write] <file>
//
//	--write: rewrite the file in-place.
func fmtCommand(args []string) error {
	write := false
	if len(args) == 2 && isFmtWriteFlag(args[0]) {
		write = true
		args = args[1:]
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		usage()
		return fmt.Errorf("fmt takes an optional --write and one target")
	}
	source, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	// The formatter is token based and will happily lay out source that does not
	// parse, so validate first: rewriting a file whose syntax is broken would
	// bake the breakage into a "formatted" result.
	if _, errs, err := parsePath(args[0]); err != nil {
		return err
	} else if len(errs) > 0 {
		printParserDiagnostics(errs)
		return fmt.Errorf("parse failed")
	}
	marked, err := insertMoveMarkers(args[0], string(source))
	if err != nil {
		return err
	}
	formatted := kizufmt.Format(marked)
	if !write {
		_, _ = fmt.Print(formatted)
		return nil
	}
	return os.WriteFile(args[0], []byte(formatted), 0o644)
}

// insertMoveMarkers writes `move` at every place the ownership checker reports
// one missing, so a file that predates the marker comes up to date by being
// formatted instead of by hand.
//
// The sites are only trustworthy when the rest of the program checks, so a
// program the type or ownership checker rejects for any other reason is
// formatted as it stands and keeps its diagnostic: `check` still reports it,
// and formatting it again after the fix inserts the markers.
func insertMoveMarkers(path, source string) (string, error) {
	program, err := loadFileProgram(path)
	if err != nil {
		return source, nil
	}
	if err := types.New().Check(program); err != nil {
		return source, nil
	}
	markers, err := ownership.New().MissingMoveMarkers(program)
	if err != nil {
		return source, nil
	}
	offsets := make([]int, 0, len(markers))
	for _, marker := range markers {
		// Loading a file resolves the std it imports, whose markers belong to
		// their own files and are already written.
		if marker.Span.Source.Path() != path {
			continue
		}
		offset, ok := lineColumnOffset(source, marker.Span.Start.Line, marker.Span.Start.Column)
		if !ok {
			return "", fmt.Errorf("fmt: %s:%d:%d is outside the file",
				path, marker.Span.Start.Line, marker.Span.Start.Column)
		}
		offsets = append(offsets, offset)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(offsets)))
	for _, offset := range offsets {
		source = source[:offset] + "move " + source[offset:]
	}
	return source, nil
}

// lineColumnOffset converts a 1-based line and column into a byte offset.
func lineColumnOffset(source string, line, column int) (int, bool) {
	if line < 1 || column < 1 {
		return 0, false
	}
	offset := 0
	for ; line > 1; line-- {
		next := strings.IndexByte(source[offset:], '\n')
		if next < 0 {
			return 0, false
		}
		offset += next + 1
	}
	offset += column - 1
	if offset > len(source) {
		return 0, false
	}
	return offset, true
}

// isFmtWriteFlag reports whether an argument selects in-place formatting.
func isFmtWriteFlag(arg string) bool {
	return arg == "--write" || arg == "-w"
}
