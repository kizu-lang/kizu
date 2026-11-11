package unsafecap_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
	"github.com/kizu-lang/kizu/internal/unsafecap"
)

// The programs here come from the Quint model of the `unsafe` marker check in
// spec/unsafecap/unsafecap.qnt (README.md there says what it covers). Each case
// is two functions, g and f, and the diagnostic classes the model allows; the
// test renders it to a two-file Kizu module, checks it, and compares. A
// divergence is either a checker bug or a model bug, and which one is a
// question for SPEC.md: record it in findings/unsafecap-quint-findings.md
// rather than adjusting either side to agree.
//
// KIZU_MBT_BIN=<kizu binary> runs `<binary> check` on each program instead of
// the in-process checker, so the same cases can be put to the compiler built
// from compiler/. KIZU_MBT_FIXTURE=<file> replays another fixture, such as a
// wider sample than the checked-in one.
//
// A statement is written as
//
//	stmt  := ["$ "] form           "$ " is the `// SAFETY:` line before it
//	form  := "print" E
//	       | ("ptr_write" | "vol_write" | "deref_set" | "field_set") M E
//	       | ("view" | "deref_borrow" | "field_borrow") M
//	       | "defer" M ("ev" | "uv" | "sv")
//	       | "bind" ("safe" | "unsafe" | "extern") ("sf" | "uf" | "ext") M
//	M     := "-" | "!"+             the statement's own markers
//	E     := L | "!"* "(" L "+" L ")" | "!"* "{" ["$"] L ";" ["$"] L "}"
//	L     := "!"* atom              each "!" is one `unsafe`
//
// with the atoms of mbtAtoms. The fields are separated by single spaces.

// mbtFunc is one generated function.
type mbtFunc struct {
	Kind string   `json:"kind"` // "fn" or "unsafe"
	File string   `json:"file"` // "home" (declares Buf) or "away"
	Body []string `json:"body"`
}

// mbtCase is one program of the fixture scripts/unsafecap-mbt/main.kizu writes.
type mbtCase struct {
	Bucket string   `json:"bucket"`
	G      mbtFunc  `json:"g"`
	F      mbtFunc  `json:"f"`
	Expect []string `json:"expect"`
}

// mbtHome is a `fn` in the file that declares Buf.
func mbtHome(body ...string) mbtFunc { return mbtFunc{Kind: "fn", File: "home", Body: body} }

// mbtUnsafe is an `unsafe fn` in the file that declares Buf.
func mbtUnsafe(body ...string) mbtFunc { return mbtFunc{Kind: "unsafe", File: "home", Body: body} }

// mbtAway is a `fn` in the other file of the module.
func mbtAway(body ...string) mbtFunc { return mbtFunc{Kind: "fn", File: "away", Body: body} }

// mbtShape writes a known shape whose g is empty.
func mbtShape(name string, f mbtFunc, expect ...string) mbtCase {
	return mbtCase{Bucket: name, G: mbtHome(), F: f, Expect: expect}
}

// mbtKnownShapes mirrors spec/unsafecap/unsafecap_test.qnt: shapes whose
// verdict SPEC §12 and the examples fix, so a harness that renders or
// classifies wrongly fails here before it disagrees with the model elsewhere.
// The last ones are the forms a checker that ignored markers, leaked them into
// a callee, or let a function be bound at a laxer pointer type would accept.
var mbtKnownShapes = []mbtCase{
	mbtShape("marked", mbtHome("$ ptr_write ! lit")),
	mbtShape("unmarkedRead", mbtHome("print read"), "Missing:ptr_read"),
	mbtShape("unmarkedDeref", mbtHome("print deref"), "Missing:ptr_deref"),
	mbtShape("unmarkedVolatile", mbtHome("print vread"), "Missing:volatile"),
	mbtShape("unmarkedOffset", mbtHome("print offset"), "Missing:ptr_offset"),
	mbtShape("unmarkedCast", mbtHome("print cast"), "Missing:ptr_cast"),
	mbtShape("unmarkedIntCast", mbtHome("print intcast"), "Missing:ptr_int_cast"),
	mbtShape("unmarkedExtern", mbtHome("print ext"), "Missing:extern_call"),
	mbtShape("unmarkedUnsafeFn", mbtHome("print uf"), "Missing:unsafe_call"),
	mbtShape("unmarkedUnsafeFnPtr", mbtHome("print fu"), "Missing:unsafe_call"),
	mbtShape("unmarkedExternFnPtr", mbtHome("print fe"), "Missing:extern_call"),
	mbtShape("unmarkedBuild", mbtHome("print build"), "Missing:struct_invariant"),
	mbtShape("unmarkedPtrWrite", mbtHome("ptr_write - lit"), "Missing:ptr_write"),
	mbtShape("unmarkedVolWrite", mbtHome("vol_write - lit"), "Missing:volatile"),
	mbtShape("unmarkedDerefSet", mbtHome("deref_set - lit"), "Missing:ptr_deref"),
	mbtShape("unmarkedFieldSet", mbtHome("field_set - lit"), "Missing:struct_invariant"),
	mbtShape("unmarkedView", mbtHome("view -"), "Missing:ptr_view"),
	mbtShape("unmarkedRawBorrow", mbtHome("deref_borrow -"), "Missing:ptr_deref"),
	mbtShape("safeOperands", mbtHome("print (sf+fs)", "print field", "print lit")),
	mbtShape("markerOnSafe", mbtHome("$ print !sf"), "Unused"),
	mbtShape("oneMarkerCoversBoth", mbtHome("$ ptr_write ! read")),
	mbtShape("redundantNested", mbtHome("$ print !!read"), "Unused"),
	mbtShape("outerOverInner", mbtHome("$ print !(!read+!ext)"), "Unused"),
	mbtShape("outerAndInner", mbtHome("$ print !(!read+ext)")),
	mbtShape("assignmentValueIsSeparate", mbtHome("$ deref_set ! read"), "Missing:ptr_read"),
	mbtShape("assignmentValueMarked", mbtHome("$ deref_set ! !read")),
	mbtShape("noSafety", mbtHome("print !read"), "NoSafety"),
	mbtShape("safetyDoesNotReachInner", mbtHome("$ print {!read;lit}"), "NoSafety"),
	mbtShape("innerSafety", mbtHome("print {$!read;lit}")),
	mbtShape("outerCoversInnerStatements", mbtHome("$ print !{read;ext}")),
	mbtShape("unsafeFnBodyNotCovered", mbtUnsafe("print read"), "Missing:ptr_read"),
	mbtShape("deferExtern", mbtHome("$ defer ! ev")),
	mbtShape("deferUnmarked", mbtHome("defer - uv"), "Missing:unsafe_call"),
	mbtShape("deferSafeMarked", mbtHome("$ defer ! sv"), "Unused"),
	mbtShape("bindSafe", mbtHome("bind safe sf -")),
	mbtShape("bindUnsafe", mbtHome("$ bind unsafe uf !")),
	mbtShape("bindExtern", mbtHome("$ bind extern ext !")),
	mbtShape("bindUnsafeUnmarked", mbtHome("bind unsafe uf -"), "Missing:unsafe_call"),
	mbtShape("launderUnsafe", mbtHome("bind safe uf -"), "FnTypeMismatch"),
	mbtShape("launderExtern", mbtHome("bind safe ext -"), "FnTypeMismatch"),
	mbtShape("structInHome", mbtHome("$ field_set ! lit", "$ print !build")),
	mbtShape("fieldSetAway", mbtAway("$ field_set ! lit"), "Boundary"),
	mbtShape("buildAway", mbtAway("$ print !build"), "Boundary"),
	mbtShape("fieldReadAway", mbtAway("print field")),
	mbtShape("fieldBorrowUnmarked", mbtHome("field_borrow -"), "Missing:struct_invariant"),
	mbtShape("fieldBorrowMarked", mbtHome("$ field_borrow !")),
	mbtShape("fieldBorrowAway", mbtAway("$ field_borrow !"), "Boundary"),
	mbtShape("fieldBorrowAwayUnmarked", mbtAway("field_borrow -"),
		"Boundary", "Missing:struct_invariant"),
	{Bucket: "callUnsafeG", G: mbtUnsafe("$ print !read"), F: mbtHome("print g"),
		Expect: []string{"Missing:unsafe_call"}},
	{Bucket: "callUnsafeGMarked", G: mbtUnsafe("$ print !read"), F: mbtHome("$ print !g")},
	{Bucket: "markerDoesNotLeakIntoSafeG", G: mbtHome("print read"), F: mbtHome("$ print !g"),
		Expect: []string{"Missing:ptr_read", "Unused"}},
	{Bucket: "markerDoesNotLeakIntoUnsafeG", G: mbtUnsafe("print read"), F: mbtHome("$ print !g"),
		Expect: []string{"Missing:ptr_read"}},
	{Bucket: "markerDoesNotLeakIntoAwayG", G: mbtAway("$ field_set ! lit"), F: mbtHome("$ print !(g+read)"),
		Expect: []string{"Boundary"}},
}

// mbtKnownDivergences are shapes on which the checker and SPEC §12 are known
// to disagree (findings/unsafecap-quint-findings.md). `model` is what the model
// allows, `checker` what the checker reports ("" for accept). The test fails if
// the checker stops doing what is written here, so a fix shows up as a prompt
// to move the shape into mbtKnownShapes.
var mbtKnownDivergences = []struct {
	c       mbtCase
	checker string
}{
	// Two markers on a form the checker reads with one are a form error, not an
	// unused marker. Both reject; the diagnostic differs.
	{mbtShape("doubleMarkedView", mbtHome("$ view !!"), "Unused"), "?"},
	{mbtShape("doubleMarkedRawBorrow", mbtHome("$ deref_borrow !!"), "Unused"), "Missing:ptr_deref"},
	{mbtShape("doubleMarkedDefer", mbtHome("$ defer !! ev"), "Unused"), "?"},
}

// mbtRawBorrowFormMessage is the checker's answer to `let r = &var p.*;`
// with no marker. SPEC §12 counts that initializer as a `ptr_deref` and says
// a missing marker's diagnostic names the kind; the checker names the form
// instead, and mbtClassify maps it to Missing:ptr_deref
// (findings/unsafecap-quint-findings.md, F2).
const mbtRawBorrowFormMessage = "a raw pointer's value is borrowed by an `unsafe` binding"

// TestUnsafeModelKnownRawBorrowDiagnostic pins F2, which mbtClassify hides
// from the other tests: once the checker reports a missing marker here, the
// mapping in mbtClassify is no longer needed.
func TestUnsafeModelKnownRawBorrowDiagnostic(t *testing.T) {
	check := mbtChecker(t)
	source, err := mbtRender(mbtShape("unmarkedRawBorrow", mbtHome("deref_borrow -")))
	if err != nil {
		t.Fatal(err)
	}
	got := check(source)
	if got == nil {
		t.Fatal("the checker accepted an unmarked raw borrow")
	}
	if !strings.Contains(got.Error(), mbtRawBorrowFormMessage) || strings.Contains(got.Error(), "requires `unsafe`") {
		t.Errorf("the checker now reports %q; drop the F2 case of mbtClassify and this test, "+
			"and update findings/unsafecap-quint-findings.md", got.Error())
	}
}

// TestUnsafeModelKnownShapes checks the harness on shapes with a known
// verdict, and then puts each to the checker again with the opposite verdict,
// which must diverge: a renderer whose programs all fail the same way, or a
// classifier that agrees with anything, would let a flipped shape through.
func TestUnsafeModelKnownShapes(t *testing.T) {
	check := mbtMemo(mbtChecker(t))
	for _, c := range mbtKnownShapes {
		if problem := mbtCompare(c, check); problem != "" {
			t.Errorf("%s", problem)
		}
		flipped := c
		if len(c.Expect) == 0 {
			flipped.Expect = []string{"Missing:ptr_read"}
		} else {
			flipped.Expect = nil
		}
		if mbtCompare(flipped, check) == "" {
			t.Errorf("%s: the harness did not notice the flipped verdict %v", c.Bucket, flipped.Expect)
		}
	}
}

// mbtMemo remembers the checker's answer for each program.
func mbtMemo(check func(mbtSources) error) func(mbtSources) error {
	seen := map[mbtSources]error{}
	return func(s mbtSources) error {
		if err, ok := seen[s]; ok {
			return err
		}
		err := check(s)
		seen[s] = err
		return err
	}
}

// TestUnsafeModelKnownDivergences pins the recorded disagreements.
func TestUnsafeModelKnownDivergences(t *testing.T) {
	check := mbtChecker(t)
	for _, d := range mbtKnownDivergences {
		if mbtCompare(d.c, check) == "" {
			t.Errorf("%s: the checker now agrees with the model; move it to mbtKnownShapes "+
				"and update findings/unsafecap-quint-findings.md", d.c.Bucket)
			continue
		}
		source, err := mbtRender(d.c)
		if err != nil {
			t.Fatal(err)
		}
		if got := mbtClassify(check(source)); got != d.checker {
			t.Errorf("%s: checker reported %q, recorded %q", d.c.Bucket, got, d.checker)
		}
	}
}

// TestUnsafeModelTraces replays the sampled programs.
func TestUnsafeModelTraces(t *testing.T) {
	path := os.Getenv("KIZU_MBT_FIXTURE")
	if path == "" {
		path = filepath.Join("..", "..", "compiler", "tests", "mbt", "unsafecap.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []mbtCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the fixture has no cases")
	}
	check := mbtChecker(t)
	failed := 0
	for _, c := range fixture.Cases {
		if problem := mbtCompare(c, check); problem != "" {
			failed++
			t.Errorf("%s", problem)
		}
	}
	t.Logf("%d programs, %d diverged", len(fixture.Cases), failed)
}

// mbtSources is one rendered program: the file that declares Buf, and the
// other file of the module, empty when no function is there.
type mbtSources struct {
	home string
	away string
}

// mbtCompare checks one case and describes how the checker diverged from the
// model, or returns "" when it did not.
func mbtCompare(c mbtCase, check func(mbtSources) error) string {
	source, err := mbtRender(c)
	if err != nil {
		return fmt.Sprintf("%s: %v", c.Bucket, err)
	}
	got := check(source)
	describe := func(what string) string {
		return fmt.Sprintf("%s: %s\nmodel allows %v\ng %s %s %q\nf %s %s %q\n--- home.kizu\n%s--- away.kizu\n%s",
			c.Bucket, what, c.Expect, c.G.Kind, c.G.File, c.G.Body, c.F.Kind, c.F.File, c.F.Body,
			source.home, source.away)
	}
	if got == nil {
		if len(c.Expect) == 0 {
			return ""
		}
		return describe("checker accepted")
	}
	kind := mbtClassify(got)
	if kind == "?" {
		return describe("checker reported a diagnostic outside the model: " + got.Error())
	}
	if len(c.Expect) == 0 {
		return describe("checker rejected: " + got.Error())
	}
	for _, want := range c.Expect {
		if want == kind {
			return ""
		}
	}
	return describe("checker reported " + kind + ": " + got.Error())
}

// mbtClassify names the diagnostic class of a checker error, as the model's
// Kind has them: "" for none, "?" for a message the model does not know. A
// missing marker is told apart by the help line, which internal/unsafecap
// writes for each kind of operation.
func mbtClassify(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "requires `unsafe`"):
		for _, info := range unsafecap.All() {
			if strings.Contains(message, unsafecap.Hint(info)) {
				return "Missing:" + info.Name
			}
		}
		return "?"
	case strings.Contains(message, mbtRawBorrowFormMessage):
		// `let r = &var p.*;` with no marker: the checker reads the form
		// `unsafe &var p.*` as one, so it names the form instead of the
		// missing marker (findings/unsafecap-quint-findings.md, F2).
		return "Missing:ptr_deref"
	case strings.Contains(message, "`unsafe` covers no operation that needs it"):
		return "Unused"
	case strings.Contains(message, "needs a `// SAFETY:` comment"):
		return "NoSafety"
	case strings.Contains(message, "is confined to its declaration file"):
		return "Boundary"
	case strings.Contains(message, "` is declared ") && strings.Contains(message, "fn() -> i64, got "):
		return "FnTypeMismatch"
	}
	return "?"
}

// mbtPrelude declares what the generated functions call and touch. It is the
// frame spec/unsafecap/unsafecap.qnt describes.
const mbtPrelude = `extern "c" fn ext() -> i64
extern "c" fn ev() -> void
/// The caller keeps nothing; this exists to be called under a marker.
unsafe fn uf() -> i64 {
    return 1;
}
/// The caller keeps nothing; this exists to be called under a marker.
unsafe fn uv() -> void {
    return;
}
fn sf() -> i64 {
    return 1;
}
fn sv() -> void {
    return;
}
fn pid(q: ptr<i64>) -> i64 {
    return 0;
}
/// data points to one live i64.
unsafe struct Buf {
    data: ptr<i64>,
    n: i64,
}
fn blen(x: Buf) -> i64 {
    return x.n;
}
fn setn(x: &var i64) -> void {
    x = 7;
}
fn main() {}
`

// mbtParams is the parameter list of g and f; mbtArgs passes them on.
const (
	mbtParams = "p: ptr<i64>, b: &var Buf, fu: unsafe fn() -> i64, " +
		"fe: extern \"c\" fn() -> i64, fs: fn() -> i64"
	mbtArgs = "p, b, fu, fe, fs"
)

// mbtAtoms is the Kizu each operand stands for.
var mbtAtoms = map[string]string{
	"lit":     "1",
	"sf":      "sf()",
	"fs":      "fs()",
	"field":   "b.n",
	"read":    "ptr_read(p)",
	"deref":   "p.*",
	"vread":   "volatile_read(p)",
	"offset":  "pid(ptr_offset(p, 0))",
	"cast":    "pid(cast<ptr<i64>>(p))",
	"intcast": "cast<i64>(int_from_ptr<usize>(p))",
	"ext":     "ext()",
	"uf":      "uf()",
	"fu":      "fu()",
	"fe":      "fe()",
	"build":   "blen(Buf { data: p, n: 1 })",
	"g":       "g(" + mbtArgs + ")",
}

var mbtFnTypes = map[string]string{
	"safe": "fn() -> i64", "unsafe": "unsafe fn() -> i64", "extern": "extern \"c\" fn() -> i64",
}

// mbtRender writes a case as the two files of one module.
func mbtRender(c mbtCase) (mbtSources, error) {
	var home, away strings.Builder
	home.WriteString(mbtPrelude)
	for _, fn := range []struct {
		name string
		f    mbtFunc
	}{{"g", c.G}, {"f", c.F}} {
		out := &home
		if fn.f.File == "away" {
			out = &away
		}
		if err := mbtRenderFunc(out, fn.name, fn.f); err != nil {
			return mbtSources{}, fmt.Errorf("%s: %w", fn.name, err)
		}
	}
	return mbtSources{home: home.String(), away: away.String()}, nil
}

// mbtRenderFunc writes one generated function with its statements.
func mbtRenderFunc(out *strings.Builder, name string, f mbtFunc) error {
	header := "fn "
	switch f.Kind {
	case "fn":
	case "unsafe":
		out.WriteString("/// The caller keeps nothing; the body states its own obligations.\n")
		header = "unsafe fn "
	default:
		return fmt.Errorf("unknown function kind %q", f.Kind)
	}
	out.WriteString(header + name + "(" + mbtParams + ") -> i64 {\n")
	for i, stmt := range f.Body {
		r := &mbtStmtRenderer{out: out, indent: "    ", id: fmt.Sprintf("%s%d", name, i)}
		if err := r.stmt(stmt); err != nil {
			return fmt.Errorf("statement %q: %w", stmt, err)
		}
	}
	out.WriteString("    return 0;\n}\n")
	return nil
}

// mbtStmtRenderer writes one statement; id names its bindings.
type mbtStmtRenderer struct {
	out    *strings.Builder
	indent string
	id     string
}

const mbtSafety = "// SAFETY: the model says this marker is the one that covers it"

// line writes one line at the current indent.
func (r *mbtStmtRenderer) line(text string) {
	r.out.WriteString(r.indent + text + "\n")
}

// markers writes the prefix of n markers.
func markers(n int) string { return strings.Repeat("unsafe ", n) }

// markWord reads a statement's own markers: "-" or "!"+.
func markWord(word string) (int, error) {
	if word == "-" {
		return 0, nil
	}
	if word == "" || strings.Trim(word, "!") != "" {
		return 0, fmt.Errorf("bad markers %q", word)
	}
	return len(word), nil
}

// stmt writes one statement of the fixture syntax.
func (r *mbtStmtRenderer) stmt(text string) error {
	safety := strings.HasPrefix(text, "$ ")
	fields := strings.Fields(strings.TrimPrefix(text, "$ "))
	if len(fields) == 0 {
		return fmt.Errorf("empty statement")
	}
	comment := func() {
		if safety {
			r.line(mbtSafety)
		}
	}
	arity := map[string]int{
		"print": 2, "ptr_write": 3, "vol_write": 3, "deref_set": 3, "field_set": 3,
		"view": 2, "deref_borrow": 2, "field_borrow": 2, "defer": 3, "bind": 4,
	}
	want, ok := arity[fields[0]]
	if !ok || len(fields) != want {
		return fmt.Errorf("bad statement")
	}
	if fields[0] == "print" {
		e, err := r.expr(fields[1])
		if err != nil {
			return err
		}
		comment()
		r.line("print(" + e + ");")
		return nil
	}
	// The statement's own markers come right after the form, except for a
	// binding, where they come last.
	m, err := markWord(fields[1])
	if fields[0] == "bind" {
		m, err = markWord(fields[3])
	}
	if err != nil {
		return err
	}
	return r.markedStmt(fields, markers(m), comment)
}

// mbtWriteTarget spells a write of e by its form, without the markers.
func mbtWriteTarget(form, e string) string {
	switch form {
	case "ptr_write":
		return "ptr_write(p, " + e + ");"
	case "vol_write":
		return "volatile_write(p, " + e + ");"
	case "deref_set":
		return "p.* = " + e + ";"
	default:
		return "b.n = " + e + ";"
	}
}

// markedStmt writes a statement that carries markers: `prefix` is them, put
// where the statement's operation is, and `comment` writes the SAFETY line
// when the statement asked for one.
func (r *mbtStmtRenderer) markedStmt(fields []string, prefix string, comment func()) error {
	switch fields[0] {
	case "ptr_write", "vol_write", "deref_set", "field_set":
		e, err := r.expr(fields[2])
		if err != nil {
			return err
		}
		comment()
		r.line(prefix + mbtWriteTarget(fields[0], e))
	case "view":
		comment()
		r.line("let v" + r.id + " = " + prefix + "view_from_ptr(p, 1);")
		r.line("print(v" + r.id + "[0]);")
	case "deref_borrow":
		comment()
		r.line("let r" + r.id + " = " + prefix + "&var p.*;")
		r.line("print(r" + r.id + ".*);")
	case "field_borrow":
		comment()
		r.line(prefix + "setn(&var b.n);")
	case "defer":
		if fields[2] != "ev" && fields[2] != "uv" && fields[2] != "sv" {
			return fmt.Errorf("bad deferred call %q", fields[2])
		}
		comment()
		r.line("defer " + prefix + fields[2] + "();")
	case "bind":
		ty, ok := mbtFnTypes[fields[1]]
		if !ok || (fields[2] != "sf" && fields[2] != "uf" && fields[2] != "ext") {
			return fmt.Errorf("bad binding")
		}
		r.line("let h" + r.id + ": " + ty + " = " + fields[2] + ";")
		comment()
		r.line("print(" + prefix + "h" + r.id + "());")
	}
	return nil
}

// leadingMarks splits the "!" prefix off a token.
func leadingMarks(text string) (int, string) {
	rest := strings.TrimLeft(text, "!")
	return len(text) - len(rest), rest
}

// leaf writes one operand with its markers.
func (r *mbtStmtRenderer) leaf(text string) (string, error) {
	m, atom := leadingMarks(text)
	kizu, ok := mbtAtoms[atom]
	if !ok {
		return "", fmt.Errorf("unknown operand %q", atom)
	}
	return markers(m) + kizu, nil
}

// expr writes an expression. A Block's inner statements go on lines of their
// own, one level in, each with its own `// SAFETY:` when the token says so.
func (r *mbtStmtRenderer) expr(text string) (string, error) {
	m, rest := leadingMarks(text)
	switch {
	case strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")"):
		left, right, ok := strings.Cut(rest[1:len(rest)-1], "+")
		if !ok {
			return "", fmt.Errorf("bad sum %q", text)
		}
		l, err := r.leaf(left)
		if err != nil {
			return "", err
		}
		rr, err := r.leaf(right)
		if err != nil {
			return "", err
		}
		return markers(m) + "((" + l + ") + (" + rr + "))", nil
	case strings.HasPrefix(rest, "{") && strings.HasSuffix(rest, "}"):
		left, right, ok := strings.Cut(rest[1:len(rest)-1], ";")
		if !ok {
			return "", fmt.Errorf("bad block %q", text)
		}
		inner := r.indent + "    "
		var b strings.Builder
		b.WriteString(markers(m) + "if true {\n")
		for i, part := range []string{left, right} {
			part, safe := strings.CutPrefix(part, "$")
			l, err := r.leaf(part)
			if err != nil {
				return "", err
			}
			if safe {
				b.WriteString(inner + mbtSafety + "\n")
			}
			if i == 0 {
				b.WriteString(inner + "let t" + r.id + " = " + l + ";\n")
			} else {
				b.WriteString(inner + "t" + r.id + " + (" + l + ")\n")
			}
		}
		b.WriteString(r.indent + "} else {\n" + inner + "0\n" + r.indent + "}")
		return b.String(), nil
	}
	// A lone operand's markers are the operand's.
	return r.leaf(text)
}

// mbtChecker returns the checker the cases are put to: the in-process type and
// ownership checks `kizu check` runs, or the binary KIZU_MBT_BIN names.
func mbtChecker(t *testing.T) func(mbtSources) error {
	bin := os.Getenv("KIZU_MBT_BIN")
	if bin == "" {
		return func(s mbtSources) error {
			files := []string{"home.kizu"}
			sources := map[string]string{"home.kizu": s.home}
			if s.away != "" {
				files = append(files, "away.kizu")
				sources["away.kizu"] = s.away
			}
			graph := project.Graph{Modules: []project.Module{{Path: "", Files: files}}}
			program, err := project.LoadProgramWithSources(graph, sources)
			if err != nil {
				return err
			}
			if err := types.New().Check(program); err != nil {
				return err
			}
			return ownership.New().Check(program)
		}
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "[package]\nname = \"mbt\"\nversion = \"0.1.0\"\n\n[modules]\npaths = [\"src\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "kizu.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return func(s mbtSources) error {
		away := filepath.Join(dir, "src", "away.kizu")
		_ = os.Remove(away)
		if err := os.WriteFile(filepath.Join(dir, "src", "home.kizu"), []byte(s.home), 0o644); err != nil {
			return err
		}
		if s.away != "" {
			if err := os.WriteFile(away, []byte(s.away), 0o644); err != nil {
				return err
			}
		}
		output, err := exec.Command(bin, "check", dir).CombinedOutput()
		if err == nil {
			return nil
		}
		return fmt.Errorf("%s", output)
	}
}

// TestUnsafeModelCoversEveryKind keeps the model's operation kinds and
// internal/unsafecap's in step: a kind added to one is a kind the other must
// learn.
func TestUnsafeModelCoversEveryKind(t *testing.T) {
	var names []string
	for _, info := range unsafecap.All() {
		names = append(names, info.Name)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, c := range append(mbtKnownShapes, mbtCase{}) {
		for _, want := range c.Expect {
			if name, ok := strings.CutPrefix(want, "Missing:"); ok {
				seen[name] = true
			}
		}
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("no known shape reports a missing marker for %q", name)
		}
	}
}
