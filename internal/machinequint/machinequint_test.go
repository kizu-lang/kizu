package machinequint

import (
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/machinelean"
)

// TestRenderWritesTheMachine checks the module holds every move of the table.
func TestRenderWritesTheMachine(t *testing.T) {
	table := machinelean.Table{
		States:   []string{"Open", "Shut"},
		Events:   []string{"Close"},
		Outcomes: []string{"Applied", "Refused"},
		Start:    "Open",
		Ends:     []string{"Shut"},
		Moves: []machinelean.Move{
			{From: "Open", Event: "Close", Outcome: "Applied", To: "Shut"},
			{From: "Shut", Event: "Close", Outcome: "Refused", To: "Shut"},
		},
	}
	got, err := Render("door", table)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"module door {",
		"type State = Open | Shut",
		"(Open, Close) -> (Shut, Applied),",
		"(Shut, Close) -> (Shut, Refused),",
		"state' = Open,",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("no %q in:\n%s", want, got)
		}
	}
}

// TestRenderRefusesAPartialTable checks a pair left out is an error, since step
// would have no answer for it.
func TestRenderRefusesAPartialTable(t *testing.T) {
	table := machinelean.Table{
		States:   []string{"A"},
		Events:   []string{"B"},
		Outcomes: []string{"C"},
		Start:    "A",
	}
	if _, err := Render("partial", table); err == nil {
		t.Fatal("a table with no moves rendered")
	}
}
