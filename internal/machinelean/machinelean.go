// Package machinelean writes the tables std::machine reads out of a transition
// function as Lean definitions, into the marked blocks of a Lean specification.
//
// A block starts with a line naming its table and its Lean names:
//
//	-- BEGIN GENERATED client state=State event=Event table=table post=post applied=Applied
//
// and ends with `-- END GENERATED`. Everything between is replaced. The outcome
// type stays hand written, so two tables of one file can share it.
package machinelean

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	beginMarker = "-- BEGIN GENERATED "
	endMarker   = "-- END GENERATED"
)

// Table is one document of std::machine's to_json.
type Table struct {
	States   []string `json:"states"`
	Events   []string `json:"events"`
	Outcomes []string `json:"outcomes"`
	Start    string   `json:"start"`
	Ends     []string `json:"ends"`
	Moves    []Move   `json:"moves"`
}

// Move is one entry of a Table.
type Move struct {
	From    string `json:"from"`
	Event   string `json:"event"`
	Outcome string `json:"outcome"`
	To      string `json:"to"`
}

// ReadTables reads a stream of to_json documents.
func ReadTables(stream io.Reader) ([]Table, error) {
	decoder := json.NewDecoder(stream)
	decoder.DisallowUnknownFields()
	tables := []Table{}
	for {
		var table Table
		err := decoder.Decode(&table)
		if errors.Is(err, io.EOF) {
			return tables, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read table: %w", err)
		}
		tables = append(tables, table)
	}
}

// Update returns spec with each named table written into its block. Every name
// must have a block and every block a name, so a table cannot be left behind.
func Update(spec string, names []string, tables []Table) (string, error) {
	if len(names) != len(tables) {
		return "", fmt.Errorf("%d names for %d tables", len(names), len(tables))
	}
	byName := map[string]Table{}
	for index, name := range names {
		byName[name] = tables[index]
	}
	lines := strings.SplitAfter(spec, "\n")
	out := strings.Builder{}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		out.WriteString(line)
		if !strings.HasPrefix(line, beginMarker) {
			continue
		}
		name, options, err := parseBegin(line)
		if err != nil {
			return "", err
		}
		table, ok := byName[name]
		if !ok {
			return "", fmt.Errorf("no table for block %q", name)
		}
		delete(byName, name)
		text, err := Render(table, options)
		if err != nil {
			return "", fmt.Errorf("block %q: %w", name, err)
		}
		out.WriteString(text)
		for index++; index < len(lines) && strings.TrimSpace(lines[index]) != endMarker; index++ {
			continue
		}
		if index == len(lines) {
			return "", fmt.Errorf("block %q has no %q", name, endMarker)
		}
		out.WriteString(lines[index])
	}
	for name := range byName {
		return "", fmt.Errorf("no block for table %q", name)
	}
	return out.String(), nil
}

// Options are the Lean names of one block.
type Options struct {
	State   string
	Event   string
	Table   string
	Post    string
	Applied string
}

// parseBegin reads the table name and the Lean names from a block line.
func parseBegin(line string) (string, Options, error) {
	fields := strings.Fields(strings.TrimPrefix(line, beginMarker))
	if len(fields) != 6 {
		return "", Options{}, fmt.Errorf("block line %q needs a name and 5 options",
			strings.TrimSpace(line))
	}
	options := Options{}
	targets := map[string]*string{
		"state":   &options.State,
		"event":   &options.Event,
		"table":   &options.Table,
		"post":    &options.Post,
		"applied": &options.Applied,
	}
	for _, field := range fields[1:] {
		key, value, found := strings.Cut(field, "=")
		target, known := targets[key]
		if !found || !known {
			return "", Options{}, fmt.Errorf("block line has unknown option %q", field)
		}
		*target = value
	}
	for key, target := range targets {
		if *target == "" {
			return "", Options{}, fmt.Errorf("block line %q has no %s", strings.TrimSpace(line), key)
		}
	}
	return fields[0], options, nil
}

// Render writes the state and event types and the two functions of a table. A
// table of every pair is required: a pair left out would have no answer in post.
func Render(table Table, options Options) (string, error) {
	if err := validate(table, options); err != nil {
		return "", err
	}
	out := strings.Builder{}
	writeInductive(&out, options.State, table.States)
	writeInductive(&out, options.Event, table.Events)

	applied := []Move{}
	for _, move := range table.Moves {
		if move.Outcome == options.Applied {
			applied = append(applied, move)
		}
	}
	fmt.Fprintf(&out, "def %s : %s → %s → Option %s\n",
		options.Table, options.State, options.Event, options.State)
	for _, move := range applied {
		fmt.Fprintf(&out, "  | .%s, .%s => some .%s\n",
			constructor(move.From), constructor(move.Event), constructor(move.To))
	}
	if len(applied) < len(table.Moves) {
		out.WriteString("  | _, _ => none\n")
	}
	out.WriteString("\n")

	fmt.Fprintf(&out, "def %s : %s → %s → %s × Outcome\n",
		options.Post, options.State, options.Event, options.State)
	for _, move := range table.Moves {
		fmt.Fprintf(&out, "  | .%s, .%s => (.%s, .%s)\n",
			constructor(move.From), constructor(move.Event), constructor(move.To), constructor(move.Outcome))
	}
	return out.String(), nil
}

// writeInductive writes one enum as a Lean inductive type.
func writeInductive(out *strings.Builder, name string, variants []string) {
	fmt.Fprintf(out, "inductive %s\n", name)
	for _, variant := range variants {
		fmt.Fprintf(out, "  | %s\n", constructor(variant))
	}
	out.WriteString("deriving Repr, DecidableEq\n\n")
}

// validate refuses a table that does not answer every pair exactly once.
func validate(table Table, options Options) error {
	if !contains(table.Outcomes, options.Applied) {
		return fmt.Errorf("applied outcome %q is not one of %v", options.Applied, table.Outcomes)
	}
	seen := map[[2]string]bool{}
	for _, move := range table.Moves {
		if !contains(table.States, move.From) || !contains(table.States, move.To) {
			return fmt.Errorf("move %v names a state the table does not list", move)
		}
		if !contains(table.Events, move.Event) {
			return fmt.Errorf("move %v names an event the table does not list", move)
		}
		if !contains(table.Outcomes, move.Outcome) {
			return fmt.Errorf("move %v names an outcome the table does not list", move)
		}
		pair := [2]string{move.From, move.Event}
		if seen[pair] {
			return fmt.Errorf("the table has two moves for %s and %s", move.From, move.Event)
		}
		seen[pair] = true
	}
	for _, state := range table.States {
		for _, event := range table.Events {
			if !seen[[2]string{state, event}] {
				return fmt.Errorf("the table has no move for %s and %s", state, event)
			}
		}
	}
	return nil
}

// contains reports whether wanted is one of values.
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// constructor spells a variant name as Lean spells a constructor.
func constructor(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}
