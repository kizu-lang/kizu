package machinelean

import (
	"strings"
	"testing"
)

// sampleTable returns a sound two-state table the tests change.
func sampleTable() Table {
	return Table{
		States:   []string{"Open", "Shut"},
		Events:   []string{"Close"},
		Outcomes: []string{"Applied", "Refused"},
		Start:    "Open",
		Ends:     []string{"Shut"},
		Moves: []Move{
			{From: "Open", Event: "Close", Outcome: "Applied", To: "Shut"},
			{From: "Shut", Event: "Close", Outcome: "Refused", To: "Shut"},
		},
	}
}

const sampleBlock = "before\n" +
	"-- BEGIN GENERATED door state=Door event=Knock table=table post=post applied=Applied\n" +
	"old\n" +
	"-- END GENERATED\n" +
	"after\n"

// TestUpdateReplacesTheBlock checks the text outside the block stays and the
// text inside comes from the table.
func TestUpdateReplacesTheBlock(t *testing.T) {
	got, err := Update(sampleBlock, []string{"door"}, []Table{sampleTable()})
	if err != nil {
		t.Fatal(err)
	}
	want := `before
-- BEGIN GENERATED door state=Door event=Knock table=table post=post applied=Applied
inductive Door
  | open
  | shut
deriving Repr, DecidableEq

inductive Knock
  | close
deriving Repr, DecidableEq

def table : Door → Knock → Option Door
  | .open, .close => some .shut
  | _, _ => none

def post : Door → Knock → Door × Outcome
  | .open, .close => (.shut, .applied)
  | .shut, .close => (.shut, .refused)
-- END GENERATED
after
`
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// TestUpdateRefuses checks each way a table and a file can disagree is an error
// rather than a block left as it was.
func TestUpdateRefuses(t *testing.T) {
	missing := sampleTable()
	missing.Moves = missing.Moves[:1]
	unknown := sampleTable()
	unknown.Moves[1].To = "Ajar"
	twice := sampleTable()
	twice.Moves[1].From = "Open"
	one := []Table{sampleTable()}
	cases := []struct {
		name   string
		spec   string
		names  []string
		tables []Table
		want   string
	}{
		{"pair left out", sampleBlock, []string{"door"}, []Table{missing}, "no move for Shut and Close"},
		{"unknown state", sampleBlock, []string{"door"}, []Table{unknown}, "names a state"},
		{"pair twice", sampleBlock, []string{"door"}, []Table{twice}, "two moves"},
		{"block without table", sampleBlock, []string{"gate"}, one, "no table for block"},
		{"table without block", "before\n", []string{"door"}, one, "no block for table"},
		{
			"open block",
			strings.TrimSuffix(sampleBlock, "-- END GENERATED\nafter\n"),
			[]string{"door"},
			one,
			"has no",
		},
		{"count", sampleBlock, []string{"door", "gate"}, one, "2 names for 1 tables"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := Update(test.spec, test.names, test.tables)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

// TestReadTablesReadsAStream checks consecutive documents and refuses a field
// this package does not know, which would be a table it cannot honour.
func TestReadTablesReadsAStream(t *testing.T) {
	tables, err := ReadTables(strings.NewReader(`{"states":["A"]} {"states":["B"]}`))
	if err != nil || len(tables) != 2 || tables[1].States[0] != "B" {
		t.Fatalf("got %v, %v", tables, err)
	}
	if _, err := ReadTables(strings.NewReader(`{"rows":[]}`)); err == nil {
		t.Fatal("unknown field was read")
	}
}
