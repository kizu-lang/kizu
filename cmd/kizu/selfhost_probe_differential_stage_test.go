package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The staged phase exists because `run <file>` cannot reach the lowering that
// matters. selfhost::backend::compiled_mir_lower only ever lowers the selfhost
// package, so a probe reaches it by being a module of that package. The gate copies
// the package into a scratch tree, adds the probes as modules, replaces the root
// that the external-ABI closure starts from with one that calls every declared
// probe expression (which is what makes them reachable and therefore lowered), links
// the emitted module with a driver that calls each probe symbol, and runs it.
const (
	probeStageWorkDir  = "target/selfhost/probes/stage"
	probeStageDriver   = "probe_driver.c"
	probeStageExe      = "probe-exe"
	probeStageModule   = "target/selfhost/selfhost.ll"
	probeStageHostLL   = "target/selfhost/selfhost.host.ll"
	probeStageRuntimeC = "selfhost/runtime/selfhost.hosted.c"
)

// runProbeStagePhase stages the probes the baseline expects the lowering to accept,
// runs the emitted code, and records each probe's staged result.
func runProbeStagePhase(
	t *testing.T,
	runner string,
	cases []probeCase,
	observations map[string]*probeObservation,
) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Errorf("selfhost probe gate requires clang: %v", err)
		return 1
	}
	var participants []probeCase
	var refused []probeCase
	for _, item := range cases {
		if item.stageStatus == "refused" {
			refused = append(refused, item)
			continue
		}
		participants = append(participants, item)
	}
	failures := countProbeStageRefusals(t, clang, runner, refused, observations)
	if len(participants) == 0 {
		return failures
	}
	values, err := stageProbeGroup(t, clang, runner, probeStageWorkDir, participants)
	if err == nil {
		return failures + recordProbeStageValues(t, participants, observations, values)
	}
	t.Errorf("probe stage group failed: %v", err)
	return failures + 1 +
		countProbeStageAttribution(t, clang, runner, participants, observations)
}

// countProbeStageRefusals re-measures the probes the baseline records as refused.
// A refusal that has silently become an answer is drift too: the baseline would
// otherwise keep asserting a limitation the compiler no longer has, and one that
// silently became a wrong answer would be recorded as loud.
func countProbeStageRefusals(
	t *testing.T,
	clang string,
	runner string,
	refused []probeCase,
	observations map[string]*probeObservation,
) int {
	t.Helper()
	failures := 0
	for _, item := range refused {
		dir := filepath.Join(probeStageWorkDir+"-refused", item.name)
		values, err := stageProbeGroup(t, clang, runner, dir, []probeCase{item})
		if err != nil {
			observations[item.name].stageStatus = "refused"
			observations[item.name].stageNote = oneLine(err.Error())
			continue
		}
		failures += recordProbeStageValues(t, []probeCase{item}, observations, values)
	}
	return failures
}

// countProbeStageAttribution re-stages each probe alone so a group failure names the
// probes it belongs to. Only a red run pays for this.
func countProbeStageAttribution(
	t *testing.T,
	clang string,
	runner string,
	participants []probeCase,
	observations map[string]*probeObservation,
) int {
	t.Helper()
	failures := 0
	for _, item := range participants {
		dir := filepath.Join(probeStageWorkDir+"-one", item.name)
		values, err := stageProbeGroup(t, clang, runner, dir, []probeCase{item})
		if err != nil {
			observations[item.name].stageStatus = "refused"
			observations[item.name].stageNote = oneLine(err.Error())
			t.Errorf("probe %s staged path failed: %v", item.name, err)
			failures++
			continue
		}
		failures += recordProbeStageValues(t, []probeCase{item}, observations, values)
	}
	return failures
}

// recordProbeStageValues splits the driver's value stream back out per probe and
// compares each probe's values against the Go reference for the same calls.
func recordProbeStageValues(
	t *testing.T,
	participants []probeCase,
	observations map[string]*probeObservation,
	values []string,
) int {
	t.Helper()
	expected := 0
	for _, item := range participants {
		expected += len(item.calls)
	}
	if len(values) != expected {
		t.Errorf("staged probes printed %d values, want %d", len(values), expected)
		return 1
	}
	failures := 0
	cursor := 0
	for _, item := range participants {
		observed := observations[item.name]
		observed.stageStdout = joinProbeLines(values[cursor : cursor+len(item.calls)])
		cursor += len(item.calls)
		observed.stageStatus = "ok"
		if observed.stageStdout != observed.reference.stdout {
			observed.stageStatus = "mismatch"
			observed.stageNote = fmt.Sprintf(
				"staged[%s] go[%s]",
				oneLine(observed.stageStdout),
				oneLine(observed.reference.stdout),
			)
			failures++
		}
	}
	return failures
}

// joinProbeLines renders printed values the way `print` renders them.
func joinProbeLines(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, "\n") + "\n"
}

// stageProbeGroup builds one scratch package, stages it, links it, and runs it.
func stageProbeGroup(
	t *testing.T,
	clang string,
	runner string,
	dir string,
	participants []probeCase,
) ([]string, error) {
	t.Helper()
	if err := buildProbeStageWorkspace(dir, participants); err != nil {
		return nil, err
	}
	if len(participants) == 1 {
		// Single-probe workspaces exist only to name an offender, and there is one
		// per probe. Keeping them would leave a copy of the package per probe.
		defer os.RemoveAll(dir)
	}
	stage := runProbeStageCommand(runner, dir)
	if stage.code != 0 {
		return nil, fmt.Errorf("stage: %s", oneLine(stage.stdout+stage.stderr))
	}
	if err := linkProbeStageDriver(clang, dir); err != nil {
		return nil, err
	}
	exe, err := filepath.Abs(filepath.Join(dir, probeStageExe))
	if err != nil {
		return nil, err
	}
	run := exec.Command(exe)
	run.Dir = dir
	out, err := run.Output()
	if err != nil {
		return nil, fmt.Errorf("run staged probes: %w", err)
	}
	return splitProbeValues(string(out)), nil
}

// splitProbeValues drops the trailing newline the driver always writes.
func splitProbeValues(text string) []string {
	var values []string
	for _, line := range strings.Split(text, "\n") {
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

// runProbeStageCommand runs `stage selfhost` inside the scratch package.
func runProbeStageCommand(runner string, dir string) bootstrapCommandResult {
	absRunner, err := filepath.Abs(runner)
	if err != nil {
		absRunner = runner
	}
	stage := exec.Command(absRunner, "stage", "selfhost")
	stage.Dir = dir
	var stdout, stderr strings.Builder
	stage.Stdout = &stdout
	stage.Stderr = &stderr
	runErr := stage.Run()
	return bootstrapCommandResult{
		name:    "stage selfhost",
		command: absRunner + " stage selfhost",
		stdout:  stdout.String(),
		stderr:  stderr.String(),
		code:    exitCode(runErr),
	}
}

// linkProbeStageDriver links the staged module with the generated probe driver.
func linkProbeStageDriver(clang string, dir string) error {
	args := append([]string{"-Wno-override-module", "-fno-integrated-as"}, hostedLinkStackArgs()...)
	args = append(
		args,
		probeStageModule,
		probeStageHostLL,
		probeStageRuntimeC,
		probeStageDriver,
		"-o",
		probeStageExe,
	)
	link := exec.Command(clang, args...)
	link.Dir = dir
	if out, err := link.CombinedOutput(); err != nil {
		return fmt.Errorf("link staged probes: %w\n%s", err, firstToolError(string(out)))
	}
	return nil
}

// firstToolError keeps the first compiler diagnostic, which names the defect class.
func firstToolError(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "error:") {
			return line
		}
	}
	return oneLine(out)
}

// buildProbeStageWorkspace writes the scratch selfhost package for one group.
func buildProbeStageWorkspace(dir string, participants []probeCase) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := copyProbeStageTree("selfhost", filepath.Join(dir, "selfhost")); err != nil {
		return err
	}
	stdPath, err := filepath.Abs("std")
	if err != nil {
		return err
	}
	if err := os.Symlink(stdPath, filepath.Join(dir, "std")); err != nil {
		return err
	}
	moduleDir := filepath.Join(dir, "selfhost/src/probes")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		return err
	}
	for _, item := range participants {
		if err := copyProbeFile(item.path, filepath.Join(moduleDir, item.name+".kizu")); err != nil {
			return err
		}
	}
	if err := os.WriteFile(
		filepath.Join(dir, "selfhost/src/cli/check.kizu"),
		[]byte(renderProbeStageRoot(participants)),
		0o644,
	); err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(dir, probeStageDriver),
		[]byte(renderProbeStageDriver(participants)),
		0o644,
	)
}

// copyProbeStageTree copies the selfhost package sources, skipping its tests.
func copyProbeStageTree(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relative == "tests" {
			return fs.SkipDir
		}
		target := filepath.Join(dst, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyProbeFile(path, target)
	})
}

// copyProbeFile copies one source file into the scratch package.
func copyProbeFile(src string, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, content, 0o644)
}

// probeStageRootPrologue is the scratch package root's fixed half. It stands in
// for selfhost::cli::check, which the external-ABI manifest names as an emission
// root, so package_cli is what makes the probes reachable and therefore lowered.
// The frontend still checks the whole package, so the other entry points this
// module owns are kept as accepting stubs.
const probeStageRootPrologue = `import selfhost::source;
import selfhost::types::constructor_facts;
%s

pub fn checked_ast_node(
    allocator: Allocator,
    io: Io,
    files: &std::array::Array<source::SourceFile>,
    file: &source::SourceFile,
    ast: std::kizu::ast::Ast,
    root: std::kizu::ast::NodeId,
    constructor_identities: &var constructor_facts::ConstructorFacts
) -> !bool {
    return true;
}

pub fn fast_diagnostics_parsed_file(
    allocator: Allocator,
    io: Io,
    path: []u8,
    text: []u8,
    parsed: std::kizu::ast::ParseResult
) -> !bool {
    return true;
}

pub fn package_fast_diagnostics(
    allocator: Allocator,
    io: Io,
    files: &std::array::Array<source::SourceFile>
) -> !bool {
    return true;
}

pub fn file_cli(allocator: Allocator, io: Io, path: []u8) -> !i64 {
    return 0;
}

pub fn package_cli(allocator: Allocator, io: Io, root: []u8) -> !i64 {
    var acc = 0;
%s
    return acc;
}
`

// renderProbeStageRoot writes the reachability root for one probe group.
func renderProbeStageRoot(participants []probeCase) string {
	var imports []string
	var body []string
	for _, item := range participants {
		imports = append(imports, "import selfhost::probes::"+item.name+";")
		for _, call := range item.calls {
			body = append(body, fmt.Sprintf("    acc = acc + %s::%s;", item.name, call))
		}
		// A reach expression is lowered, not measured: the root is `!i64`, so `try`
		// is available here and the value is dropped. The probe's own i64 entry is
		// what the driver reads.
		for _, call := range item.reach {
			body = append(body, fmt.Sprintf("    try %s::%s;", item.name, call))
		}
	}
	return fmt.Sprintf(
		probeStageRootPrologue,
		strings.Join(imports, "\n"),
		strings.Join(body, "\n"),
	)
}

// renderProbeStageDriver writes a C driver that calls each lowered probe symbol and
// prints its value, so the comparison reads the emitted code rather than the module
// text. The CLI entry the staged module exports is hand-written LLVM whose `check`
// path never reaches package_cli, so the driver calls the probes directly.
func renderProbeStageDriver(participants []probeCase) string {
	var declarations []string
	var statements []string
	declared := map[string]bool{}
	for _, item := range participants {
		for _, call := range item.calls {
			name, arguments, found := strings.Cut(call, "(")
			if !found {
				continue
			}
			symbol := "kizu_selfhost__probes_" + item.name + "_" + name
			if !declared[symbol] {
				declared[symbol] = true
				declarations = append(
					declarations,
					fmt.Sprintf("int64_t %s(%s);", symbol, probeSymbolParams(arguments)),
				)
			}
			statements = append(
				statements,
				fmt.Sprintf(
					"    printf(\"%%lld\\n\", (long long)%s(%s));",
					symbol,
					strings.TrimSuffix(strings.TrimSpace(arguments), ")"),
				),
			)
		}
	}
	return fmt.Sprintf(
		"#include <stdint.h>\n#include <stdio.h>\n"+
			"void kizu_host_init(int argc, char **argv);\n%s\n"+
			"int main(int argc, char **argv) {\n"+
			"    kizu_host_init(argc, argv);\n%s\n    return 0;\n}\n",
		strings.Join(declarations, "\n"),
		strings.Join(statements, "\n"),
	)
}

// probeSymbolParams renders the C parameter list for a probe's declared arity.
func probeSymbolParams(arguments string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(arguments), ")")
	if strings.TrimSpace(trimmed) == "" {
		return "void"
	}
	count := strings.Count(trimmed, ",") + 1
	params := make([]string, count)
	for index := range params {
		params[index] = "int64_t"
	}
	return strings.Join(params, ", ")
}
