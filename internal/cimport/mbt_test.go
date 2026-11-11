package cimport_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kizu-lang/kizu/internal/cimport"
	"github.com/kizu-lang/kizu/internal/ownership"
	"github.com/kizu-lang/kizu/internal/project"
	"github.com/kizu-lang/kizu/internal/types"
)

// The programs here come from the Quint model of the C header import in
// spec/cimport/cimport.qnt (README.md there says what it covers). Each case is
// a C header, an optional Kizu caller of its first function, and two
// predictions: under the faithful mapping (`spec`) and, where the importer as
// it is differs, under that (`implemented`, with the findings that explain
// it). The test imports the header, checks what the importer wrote together
// with the caller, and compares with `implemented` when there is one and
// `spec` otherwise. A divergence is an importer or checker bug or a model bug,
// and which one is a question for SPEC.md: record it in
// findings/cimport-quint-findings.md rather than adjusting either side to agree.
//
// KIZU_MBT_BIN=<kizu binary> runs `<binary> import-c-header` and
// `<binary> check` instead of the in-process importer and checker, so the same
// cases can be put to the compiler built from compiler/. KIZU_MBT_FIXTURE=<file>
// replays another fixture, such as a wider sample than the checked-in one.

// mbtCaller is the Kizu function that calls the header's first function:
//
//	fn use(a1: <args[0]>, ..) -> <ret> {
//	    // SAFETY: ..                     only when marked
//	    return unsafe f(a1, ..);          `unsafe` only when marked; no `return` for void
//	}
type mbtCaller struct {
	Marked bool     `json:"marked"`
	Args   []string `json:"args"`
	Ret    string   `json:"ret"`
}

// mbtPrediction is one outcome: the classes the pipeline may report (none:
// it must accept), and what the importer writes when it imports the header.
type mbtPrediction struct {
	Expect []string `json:"expect"`
	Decls  *string  `json:"decls,omitempty"`
}

// mbtCase is one program of the fixture scripts/cimport-mbt/main.kizu writes.
type mbtCase struct {
	Bucket      string         `json:"bucket"`
	Header      string         `json:"header"`
	Caller      *mbtCaller     `json:"caller,omitempty"`
	Spec        mbtPrediction  `json:"spec"`
	Implemented *mbtPrediction `json:"implemented,omitempty"`
	Findings    []string       `json:"findings,omitempty"`
}

// mbtResult is what the importer and the checker said about one program.
type mbtResult struct {
	decls     string
	importErr error
	checkErr  error
}

// decls is the text the importer writes for the given declarations.
func decls(lines ...string) *string {
	text := strings.Join(lines, "\n")
	return &text
}

// accept predicts the declarations and no diagnostic.
func accept(lines ...string) mbtPrediction {
	return mbtPrediction{Expect: []string{}, Decls: decls(lines...)}
}

// reject predicts that the importer rejects the header with one of classes.
func reject(classes ...string) mbtPrediction { return mbtPrediction{Expect: classes} }

// checked predicts the declaration and a checker diagnostic of one of classes.
func checked(decl string, classes ...string) mbtPrediction {
	return mbtPrediction{Expect: classes, Decls: decls(decl)}
}

// call is a marked caller with the given result and argument types.
func call(ret string, args ...string) *mbtCaller {
	return &mbtCaller{Marked: true, Args: append([]string{}, args...), Ret: ret}
}

// shape is a known shape with no caller.
func shape(name, header string, want mbtPrediction) mbtCase {
	return mbtCase{Bucket: name, Header: header, Spec: want}
}

// called is a known shape with a caller.
func called(name, header string, caller *mbtCaller, want mbtPrediction) mbtCase {
	return mbtCase{Bucket: name, Header: header, Caller: caller, Spec: want}
}

// diverges records what the importer as it is does instead, and why.
func diverges(c mbtCase, impl mbtPrediction, findings ...string) mbtCase {
	c.Implemented = &impl
	c.Findings = findings
	return c
}

// mbtKnownShapes mirrors spec/cimport/cimport_test.qnt: shapes whose outcome
// SPEC §12.2 and the importer's own tests fix, and the known divergences of
// the findings, so a harness that renders or classifies wrongly fails here
// before it disagrees with the model elsewhere.
var mbtKnownShapes = []mbtCase{
	shape("puts", "int f(const char *s);\n", accept(`extern "c" fn f(s: ptr<const i8>) -> i32`)),
	called("writeByte", "void f(unsigned char *p, unsigned char value);\n",
		call("void", "ptr<u8>", "u8"),
		accept(`extern "c" fn f(p: ptr<u8>, value: u8) -> void`)),
	shape("innermostConst", "int f(const char **p);\n",
		accept(`extern "c" fn f(p: ptr<ptr<const i8>>) -> i32`)),
	called("lenCall", "size_t f(const uint8_t *data, size_t n);\n",
		call("usize", "ptr<const u8>", "usize"),
		accept(`extern "c" fn f(data: ptr<const u8>, n: usize) -> usize`)),
	shape("unnamedPointer", "int f(const int32_t *);\n",
		accept(`extern "c" fn f(p1: ptr<const i32>) -> i32`)),
	shape("unnamedTrailingQualifier", "int f(int *const);\n",
		accept(`extern "c" fn f(const: ptr<i32>) -> i32`)),
	shape("unnamedScalar", "int f(int);\n", reject("Import:type")),
	diverges(called("constPointer", "void f(char *const p);\n", call("void", "ptr<const i8>"),
		checked(`extern "c" fn f(p: ptr<i8>) -> void`, "Use:arg_type")),
		accept(`extern "c" fn f(p: ptr<const i8>) -> void`), "F1"),
	diverges(shape("middleConst", "void f(char *const *p);\n",
		accept(`extern "c" fn f(p: ptr<const ptr<i8>>) -> void`)),
		accept(`extern "c" fn f(p: ptr<ptr<const i8>>) -> void`), "F1"),
	diverges(called("pointerResult", "char* f(const char* name);\n",
		call("ptr<i8>", "ptr<const i8>"),
		checked(`extern "c" fn f(name: ptr<const i8>) -> ?ptr<i8>`, "Use:ret_type")),
		accept(`extern "c" fn f(name: ptr<const i8>) -> ptr<i8>`), "F2"),
	diverges(called("pointerResultChecked", "char* f(const char* name);\n",
		call("?ptr<i8>", "ptr<const i8>"),
		accept(`extern "c" fn f(name: ptr<const i8>) -> ?ptr<i8>`)),
		accept(`extern "c" fn f(name: ptr<const i8>) -> ptr<i8>`), "F2"),
	shape("gluedResult", "int *f(void);\n", reject("Import:declaration")),
	diverges(shape("volatile", "void f(volatile int *x);\n", reject("Import:volatile")),
		accept(`extern "c" fn f(x: ptr<i32>) -> void`), "F3"),
	shape("topVolatile", "int f(int *volatile x);\n", accept(`extern "c" fn f(x: ptr<i32>) -> i32`)),
	shape("define", "#define X 1\n", reject("Import:preprocessor")),
	shape("typedef", "typedef int my_int;\n", reject("Import:typedef")),
	shape("struct", "struct point { int x; };\n", reject("Import:struct")),
	shape("enum", "enum color { RED };\n", reject("Import:enum")),
	shape("var", "int counter;\n", reject("Import:declaration")),
	shape("variadic", "int f(int x, ...);\n", reject("Import:variadic")),
	shape("fnptr", "int f(int (*cb)(int));\n", reject("Import:fnptr")),
	shape("array", "int f(int buf[8]);\n", reject("Import:array")),
	shape("emptyParam", "int f(int x, );\n", reject("Import:empty_param")),
	shape("long", "long f(void);\n", reject("Import:type")),
	shape("bool", "int f(_Bool b);\n", reject("Import:type")),
	shape("defineSwallowsNext", "#define X 1\nint f(void);\n", reject("Import:preprocessor")),
	shape("firstRejection", "int f(int);\ntypedef int my_int;\n", reject("Import:type")),
	shape("keywordName", "int fn(void);\n", checked(`extern "c" fn fn() -> i32`, "Check:parse")),
	shape("keywordParam", "int f(int match);\n",
		checked(`extern "c" fn f(match: i32) -> i32`, "Check:parse")),
	shape("voidParam", "int f(void x);\n",
		checked(`extern "c" fn f(x: void) -> i32`, "Check:void_param")),
	shape("duplicate", "int f(void);\nlong long f(void);\n", mbtPrediction{
		Expect: []string{"Check:duplicate"},
		Decls:  decls(`extern "c" fn f() -> i32`, `extern "c" fn f() -> i64`)}),
	called("noParams", "int f();\n", call("i32"), accept(`extern "c" fn f() -> i32`)),
	called("loneVoid", "int f(void);\n", call("i32"), accept(`extern "c" fn f() -> i32`)),
	called("unmarked", "int f(int x);\n", &mbtCaller{Args: []string{"i32"}, Ret: "i32"},
		checked(`extern "c" fn f(x: i32) -> i32`, "Use:missing_marker")),
	called("wide", "int f(int x);\n", call("i32", "i64"),
		checked(`extern "c" fn f(x: i32) -> i32`, "Use:arg_type")),
	called("nullableArg", "int f(int *p);\n", call("i32", "?ptr<i32>"),
		checked(`extern "c" fn f(p: ptr<i32>) -> i32`, "Use:arg_type")),
	called("readonlyArg", "int f(int *p);\n", call("i32", "ptr<const i32>"),
		checked(`extern "c" fn f(p: ptr<i32>) -> i32`, "Use:arg_type")),
	called("sliceArg", "int f(uint8_t *p);\n", call("i32", "[]u8"),
		checked(`extern "c" fn f(p: ptr<u8>) -> i32`, "Use:arg_type")),
	// Not a shape the model draws (its caller takes the result at the C type):
	// here so the classifier is seen to name a result type mismatch.
	called("resultType", "int f(void);\n", call("u8"),
		checked(`extern "c" fn f() -> i32`, "Use:ret_type")),
	called("readonlyToConst", "int f(const char *s);\n", call("i32", "ptr<const i8>"),
		accept(`extern "c" fn f(s: ptr<const i8>) -> i32`)),
}

// TestCImportModelKnownShapes checks the harness on shapes with a known
// outcome, and then puts each to the pipeline again with the opposite verdict,
// which must diverge: a renderer whose programs all fail the same way, or a
// classifier that agrees with anything, would let a flipped shape through.
func TestCImportModelKnownShapes(t *testing.T) {
	run := mbtPipeline(t)
	for _, c := range mbtKnownShapes {
		if problem := mbtCompare(c, run); problem != "" {
			t.Errorf("%s", problem)
		}
		flipped := c
		flipped.Implemented = nil
		want := mbtWanted(c)
		if len(want.Expect) == 0 {
			flipped.Spec = mbtPrediction{Expect: []string{"Import:type"}}
		} else {
			flipped.Spec = mbtPrediction{Expect: []string{}, Decls: want.Decls}
		}
		if mbtCompare(flipped, run) == "" {
			t.Errorf("%s: the harness did not notice the flipped verdict", c.Bucket)
		}
	}
}

// TestCImportModelTraces replays the sampled programs.
func TestCImportModelTraces(t *testing.T) {
	path := os.Getenv("KIZU_MBT_FIXTURE")
	if path == "" {
		path = filepath.Join("..", "..", "compiler", "tests", "mbt", "cimport.json")
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
	run := mbtPipeline(t)
	failed := 0
	known := map[string]int{}
	for _, c := range fixture.Cases {
		if problem := mbtCompare(c, run); problem != "" {
			failed++
			t.Errorf("%s", problem)
		}
		for _, f := range c.Findings {
			known[f]++
		}
	}
	t.Logf("%d programs, %d diverged from the model; known divergences by finding: %v",
		len(fixture.Cases), failed, known)
}

// mbtWanted is the prediction a case is held to: the importer as it is when
// the model says it differs, the faithful mapping otherwise.
func mbtWanted(c mbtCase) mbtPrediction {
	if c.Implemented != nil {
		return *c.Implemented
	}
	return c.Spec
}

// mbtCompare runs one case and describes how the pipeline diverged from the
// model, or returns "" when it did not. A case with a known divergence that
// now agrees with the faithful mapping is a divergence too: the finding is
// fixed, and the fixture and findings/cimport-quint-findings.md need updating.
// (Where both predictions allow what the pipeline did, it is not told apart.)
func mbtCompare(c mbtCase, run func(string, *mbtCaller) mbtResult) string {
	got := run(c.Header, c.Caller)
	problem := mbtMatch(mbtWanted(c), got)
	if problem == "" {
		return ""
	}
	if c.Implemented != nil && mbtMatch(c.Spec, got) == "" {
		return mbtDescribe(c, got, fmt.Sprintf("the pipeline now does what the faithful mapping says; "+
			"%v is fixed here", c.Findings))
	}
	return mbtDescribe(c, got, problem)
}

// mbtMatch compares one result with one prediction.
func mbtMatch(want mbtPrediction, got mbtResult) string {
	allowed := func(kind string) bool {
		for _, w := range want.Expect {
			if w == kind {
				return true
			}
		}
		return false
	}
	if got.importErr != nil {
		kind := mbtClassify(got.importErr)
		switch {
		case kind == "?":
			return "the importer reported an error outside the model: " + got.importErr.Error()
		case want.Decls != nil:
			return "the importer rejected the header: " + got.importErr.Error()
		case !allowed(kind):
			return "the importer reported " + kind + ": " + got.importErr.Error()
		}
		return ""
	}
	if want.Decls == nil {
		return "the importer accepted the header"
	}
	if got.decls != *want.Decls {
		return "the importer wrote something else"
	}
	if got.checkErr == nil {
		if len(want.Expect) == 0 {
			return ""
		}
		return "the checker accepted"
	}
	kind := mbtClassify(got.checkErr)
	switch {
	case kind == "?":
		return "the checker reported a diagnostic outside the model: " + got.checkErr.Error()
	case len(want.Expect) == 0:
		return "the checker rejected: " + got.checkErr.Error()
	case !allowed(kind):
		return "the checker reported " + kind + ": " + got.checkErr.Error()
	}
	return ""
}

// mbtDescribe writes what diverged, with the program and both predictions.
func mbtDescribe(c mbtCase, got mbtResult, what string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n--- header\n%s", c.Bucket, what, c.Header)
	fmt.Fprintf(&b, "--- spec %v\n", c.Spec.Expect)
	if c.Spec.Decls != nil {
		fmt.Fprintf(&b, "%s\n", *c.Spec.Decls)
	}
	if c.Implemented != nil {
		fmt.Fprintf(&b, "--- implemented %v %v\n", c.Implemented.Expect, c.Findings)
		if c.Implemented.Decls != nil {
			fmt.Fprintf(&b, "%s\n", *c.Implemented.Decls)
		}
	}
	fmt.Fprintf(&b, "--- importer wrote\n%s\n", got.decls)
	if c.Caller != nil {
		fmt.Fprintf(&b, "--- caller\n%s", mbtRenderCaller(c.Caller))
	}
	return b.String()
}

// mbtClasses maps a fragment of an importer or checker message to the class
// the model spells it with. The first fragment found wins.
var mbtClasses = []struct{ fragment, class string }{
	{"preprocessor directives are unsupported", "Import:preprocessor"},
	{"typedef is unsupported", "Import:typedef"},
	{"struct declarations are unsupported", "Import:struct"},
	{"enum declarations are unsupported", "Import:enum"},
	{"variadic functions are unsupported", "Import:variadic"},
	{"function pointers are unsupported", "Import:fnptr"},
	{"arrays are unsupported", "Import:array"},
	{"unsupported C type", "Import:type"},
	{"empty parameter", "Import:empty_param"},
	{"unsupported declaration", "Import:declaration"},
	{"cannot have type void", "Check:void_param"},
	{"duplicate function", "Check:duplicate"},
	{"requires `unsafe`", "Use:missing_marker"},
	{"return expects ", "Use:ret_type"},
}

// mbtClassify names the class of an importer or checker error, as the model
// spells it, or "?" for a message the model does not know.
func mbtClassify(err error) string {
	message := err.Error()
	for _, c := range mbtClasses {
		if strings.Contains(message, c.fragment) {
			return c.class
		}
	}
	switch {
	case strings.Contains(message, "c import error") && strings.Contains(message, "volatile"):
		return "Import:volatile"
	case strings.Contains(message, "arg ") && strings.Contains(message, " expects "):
		return "Use:arg_type"
	case strings.Contains(message, "expected ") && strings.Contains(message, ", got `") &&
		!strings.Contains(message, "type error"):
		return "Check:parse"
	}
	return "?"
}

// mbtRenderCaller writes the caller of the header's first function.
func mbtRenderCaller(c *mbtCaller) string {
	params := make([]string, 0, len(c.Args))
	args := make([]string, 0, len(c.Args))
	for i, ty := range c.Args {
		params = append(params, fmt.Sprintf("a%d: %s", i+1, ty))
		args = append(args, fmt.Sprintf("a%d", i+1))
	}
	callText := "f(" + strings.Join(args, ", ") + ")"
	var b strings.Builder
	fmt.Fprintf(&b, "fn use(%s) -> %s {\n", strings.Join(params, ", "), c.Ret)
	if c.Marked {
		b.WriteString("    // SAFETY: the arguments are what the C declaration asks for\n")
		callText = "unsafe " + callText
	}
	if c.Ret == "void" {
		fmt.Fprintf(&b, "    %s;\n", callText)
	} else {
		fmt.Fprintf(&b, "    return %s;\n", callText)
	}
	b.WriteString("}\n")
	return b.String()
}

// mbtSource is the module the checker sees: what the importer wrote, and the caller.
func mbtSource(decls string, caller *mbtCaller) string {
	source := decls + "\n"
	if caller != nil {
		source += mbtRenderCaller(caller)
	}
	return source
}

// mbtPipeline returns the pipeline the cases are put to: the in-process
// importer and the type and ownership checks `kizu check` runs, or the binary
// KIZU_MBT_BIN names.
func mbtPipeline(t *testing.T) func(string, *mbtCaller) mbtResult {
	bin := os.Getenv("KIZU_MBT_BIN")
	if bin == "" {
		return func(header string, caller *mbtCaller) mbtResult {
			out, err := cimport.Import(header)
			if err != nil {
				return mbtResult{importErr: err}
			}
			source := mbtSource(out, caller)
			graph := project.Graph{Modules: []project.Module{{Path: "", Files: []string{"a.kizu"}}}}
			program, err := project.LoadProgramWithSources(graph, map[string]string{"a.kizu": source})
			if err == nil {
				err = types.New().Check(program)
			}
			if err == nil {
				err = ownership.New().Check(program)
			}
			return mbtResult{decls: out, checkErr: err}
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
	headerPath := filepath.Join(dir, "header.h")
	return func(header string, caller *mbtCaller) mbtResult {
		if err := os.WriteFile(headerPath, []byte(header), 0o644); err != nil {
			return mbtResult{importErr: err}
		}
		var stdout, stderr strings.Builder
		cmd := exec.Command(bin, "import-c-header", headerPath)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return mbtResult{importErr: errors.New(stderr.String() + stdout.String())}
		}
		out := strings.TrimSuffix(stdout.String(), "\n")
		source := []byte(mbtSource(out, caller))
		if err := os.WriteFile(filepath.Join(dir, "src", "a.kizu"), source, 0o644); err != nil {
			return mbtResult{importErr: err}
		}
		output, err := exec.Command(bin, "check", dir).CombinedOutput()
		if err != nil {
			return mbtResult{decls: out, checkErr: errors.New(string(output))}
		}
		return mbtResult{decls: out}
	}
}

// TestCImportModelCoversEveryClass keeps the known shapes in step with the
// classes the classifier knows: a class no shape exercises is one a broken
// classifier could map anything to unnoticed.
func TestCImportModelCoversEveryClass(t *testing.T) {
	classes := []string{"Import:preprocessor", "Import:typedef", "Import:struct", "Import:enum",
		"Import:variadic", "Import:fnptr", "Import:array", "Import:type", "Import:empty_param",
		"Import:declaration", "Check:void_param", "Check:duplicate", "Check:parse",
		"Use:missing_marker", "Use:arg_type", "Use:ret_type"}
	seen := map[string]bool{}
	for _, c := range mbtKnownShapes {
		for _, kind := range mbtWanted(c).Expect {
			seen[kind] = true
		}
	}
	var missing []string
	for _, kind := range classes {
		if !seen[kind] {
			missing = append(missing, kind)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no known shape the pipeline reports with %v", missing)
	}
}
