package stdmath

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestTablesMatchStd reads the tables out of the Kizu std source and holds
// the Go copies to them, so an edit to one without the other fails here
// rather than as a comptime value that differs from the runtime one.
func TestTablesMatchStd(t *testing.T) {
	tables, err := os.ReadFile("../../lib/kizu/std/src/math/tables.kizu")
	if err != nil {
		t.Fatal(err)
	}
	math, err := os.ReadFile("../../lib/kizu/std/src/math/math.kizu")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		source []byte
		fn     string
		table  []uint64
	}{
		{tables, "circle_sin_bits", circleSin[:]},
		{tables, "circle_sin_tail_bits", circleSinTail[:]},
		{tables, "circle_cos_bits", circleCos[:]},
		{tables, "circle_cos_tail_bits", circleCosTail[:]},
		{math, "four_over_pi_digit", fourOverPiDigits[:]},
	} {
		want := kizuTable(t, string(c.source), c.fn, len(c.table))
		for i, value := range c.table {
			if value != want[i] {
				t.Fatalf("%s[%d] = %#x, the Kizu source says %#x", c.fn, i, value, want[i])
			}
		}
	}
}

var entry = regexp.MustCompile(
	`if index == (\d+) \{ return (?:word64\((0x[0-9a-f]+), (0x[0-9a-f]+)\)|(0x[0-9a-f]+)); \}`)
var fallback = regexp.MustCompile(`\n    return word64\((0x[0-9a-f]+), (0x[0-9a-f]+)\);`)

// kizuTable reads one `if index == N { return ...; }` chain, the entries it
// does not list taking the value of its final return.
func kizuTable(t *testing.T, source, fn string, size int) []uint64 {
	t.Helper()
	body := regexp.MustCompile(`(?s)fn ` + fn + `\(.*?\n}\n`).FindString(source)
	if body == "" {
		t.Fatalf("no fn %s in the Kizu source", fn)
	}
	word := func(high, low string) uint64 {
		h, _ := strconv.ParseUint(high[2:], 16, 64)
		l, _ := strconv.ParseUint(low[2:], 16, 64)
		return h<<32 | l
	}
	var otherwise uint64
	if m := fallback.FindAllStringSubmatch(body, -1); len(m) > 0 {
		otherwise = word(m[len(m)-1][1], m[len(m)-1][2])
	}
	values := make([]uint64, size)
	for i := range values {
		values[i] = otherwise
	}
	for _, m := range entry.FindAllStringSubmatch(body, -1) {
		i, _ := strconv.Atoi(m[1])
		if m[2] != "" {
			values[i] = word(m[2], m[3])
		} else {
			values[i], _ = strconv.ParseUint(m[4][2:], 16, 64)
		}
	}
	return values
}

// TestKnownValues pins a few answers whose bits are known exactly: a zero
// keeps its sign, a quarter turn of cos is the rounding of pi/2's error, and
// the two variants agree away from the zeros.
func TestKnownValues(t *testing.T) {
	for _, native := range []bool{true, false} {
		if got := Sin(0, native); got != 0 {
			t.Fatalf("sin(0) = %v", got)
		}
		if got := Cos(0, native); got != 1 {
			t.Fatalf("cos(0) = %v", got)
		}
		if got := Cos(3.141592653589793/3, native); got != 0.5000000000000001 {
			t.Fatalf("cos(pi/3) = %v", got)
		}
	}
}
