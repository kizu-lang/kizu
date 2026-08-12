package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

type productionBoundaryCase struct {
	name      string
	args      []string
	exitCode  int
	stdoutKey string
	stderrKey string
}

// TestSelfhostProductionBoundaryGate runs #458 commands through the stage0-native artifact.
func TestSelfhostProductionBoundaryGate(t *testing.T) {
	if os.Getenv("KIZU_RUN_SELFHOST_PRODUCTION") != "1" {
		t.Skip("set KIZU_RUN_SELFHOST_PRODUCTION=1 to run selfhost production gate")
	}
	report, failures := runSelfhostProductionBoundary(t)
	if failures > 0 {
		t.Fatalf("selfhost production boundary failures=%d\n%s", failures, report)
	}
	t.Logf("selfhost production boundary report:\n%s", report)
}

// TestSelfhostProductionBoundaryRecipes keeps the production path explicit.
func TestSelfhostProductionBoundaryRecipes(t *testing.T) {
	bytes, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	content := string(bytes)
	production := justRecipe(content, "selfhost-production-gate")
	requireRecipeFragment(t, production, "KIZU_RUN_SELFHOST_PRODUCTION=1 go test")
	requireNoRecipeFragment(t, production, "go run ./cmd/kizu check selfhost")
	requireNoRecipeFragment(t, production, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")
	requireNoRecipeFragment(t, production, "KIZU_RUN_SELFHOST_ORACLE=1")

	fromScratch := justRecipe(content, "selfhost-production-from-scratch")
	requireRecipeFragment(t, fromScratch, "just selfhost-native")
	requireRecipeFragment(t, fromScratch, "just selfhost-fast-gate")

	fastGate := justRecipe(content, "selfhost-fast-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-production-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-corpus-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-parse-parity-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-check-parity-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-fmt-parity-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-run-parity-gate")
	requireRecipeFragment(t, fastGate, "just selfhost-test-parity-gate")
	requireNoRecipeFragment(t, fastGate, "just selfhost-bootstrap")
	requireNoRecipeFragment(t, fastGate, "KIZU_RUN_SELFHOST_BOOTSTRAP=1")
	requireNoRecipeFragment(t, fastGate, "KIZU_RUN_SELFHOST_ORACLE=1")

	switchGate := justRecipe(content, "selfhost-switch-gate")
	requireRecipeFragment(t, switchGate, "just selfhost-production-from-scratch")
	requireRecipeFragment(t, switchGate, "just selfhost-native")
	requireNoRecipeFragment(t, switchGate, "just selfhost-oracle")
	requireNoRecipeFragment(t, switchGate, "KIZU_RUN_SELFHOST_ORACLE=1")
	requireNoRecipeFragment(t, switchGate, "go run ./cmd/kizu check selfhost")
}

// runSelfhostProductionBoundary executes only the hosted artifact production path.
func runSelfhostProductionBoundary(t *testing.T) (string, int) {
	t.Helper()
	restore, err := chdirRepoRoot()
	if err != nil {
		t.Errorf("chdir repo root: %v", err)
		return "", 1
	}
	defer restore()
	runner := "target/selfhost/stage0-native/selfhost"
	if err := requireSupportedCorpusRunner(runner); err != nil {
		t.Errorf("require selfhost production runner: %v", err)
		return "", 1
	}
	start := time.Now()
	var report strings.Builder
	appendProductionBoundaryHeader(&report)
	failures := 0
	for _, item := range productionBoundaryCases() {
		result := runSelfhostCommand(t, runner, item.args...)
		expectedOut := productionBoundaryText(item.stdoutKey)
		expectedErr := productionBoundaryText(item.stderrKey)
		if result.code != item.exitCode || result.stdout != expectedOut ||
			result.stderr != expectedErr {
			t.Errorf("production command %s mismatch", item.name)
			failures++
		}
		appendProductionBoundaryResult(&report, item, result)
	}
	fmt.Fprintf(&report, "elapsed.ms %d\n", time.Since(start).Milliseconds())
	if failures == 0 {
		fmt.Fprintf(&report, "comparison.status pass\n")
	} else {
		fmt.Fprintf(&report, "comparison.status fail\n")
	}
	if err := os.WriteFile(
		"target/selfhost/reports/production-boundary.txt",
		[]byte(report.String()),
		0o644,
	); err != nil {
		t.Errorf("write production boundary report: %v", err)
		failures++
	}
	return report.String(), failures
}

// productionBoundaryCases returns the #458 command surface for the artifact.
func productionBoundaryCases() []productionBoundaryCase {
	return []productionBoundaryCase{
		{
			name:      "check_selfhost",
			args:      []string{"check", "selfhost"},
			exitCode:  0,
			stdoutKey: "check-ok",
			stderrKey: "empty",
		},
		{
			name:      "unsupported_command",
			args:      []string{"bad", "selfhost"},
			exitCode:  64,
			stdoutKey: "empty",
			stderrKey: "unsupported-command",
		},
	}
}

// productionBoundaryText resolves stable expected output keys.
func productionBoundaryText(key string) string {
	switch key {
	case "empty":
		return ""
	case "check-ok":
		return "check: ok\n"
	case "unsupported-command":
		return "usage: selfhost <check|parse|run|test|fmt> <target>\n"
	default:
		return "unsupported production output key: " + key + "\n"
	}
}

// appendProductionBoundaryHeader writes the report metadata.
func appendProductionBoundaryHeader(out *strings.Builder) {
	fmt.Fprintf(out, "kizu-selfhost-production-boundary-v0\n")
	fmt.Fprintf(out, "issue #461\n")
	fmt.Fprintf(out, "runner target/selfhost/stage0-native/selfhost\n")
	fmt.Fprintf(out, "runner.build stage0-native (go backend)\n")
	fmt.Fprintf(out, "production.path stage0-native-artifact\n")
	fmt.Fprintf(out, "go.production none\n")
	fmt.Fprintf(out, "go.allowed explicit-bootstrap-oracle-only\n")
}

// appendProductionBoundaryResult writes one command result line group.
func appendProductionBoundaryResult(
	out *strings.Builder,
	item productionBoundaryCase,
	result selfhostCommandResult,
) {
	fmt.Fprintf(out, "case.%s.command %s\n", item.name, strings.Join(item.args, " "))
	fmt.Fprintf(out, "case.%s.exit %d\n", item.name, result.code)
	fmt.Fprintf(out, "case.%s.stdout.sha256 %s\n", item.name, textFingerprint(result.stdout))
	fmt.Fprintf(out, "case.%s.stderr.sha256 %s\n", item.name, textFingerprint(result.stderr))
}

// justRecipe extracts one just recipe body from the checked-in justfile.
func justRecipe(content string, name string) string {
	lines := strings.Split(content, "\n")
	header := name + ":"
	var out strings.Builder
	inRecipe := false
	for _, line := range lines {
		if line == header {
			inRecipe = true
			continue
		}
		if inRecipe && isJustRecipeHeader(line) {
			break
		}
		if inRecipe {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// isJustRecipeHeader reports whether a line starts the next top-level recipe.
func isJustRecipeHeader(line string) bool {
	if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return false
	}
	if strings.HasPrefix(line, "#") {
		return false
	}
	return strings.HasSuffix(line, ":") || strings.Contains(line, ": ")
}

// requireRecipeFragment fails when a recipe fragment is missing.
func requireRecipeFragment(t *testing.T, recipe string, fragment string) {
	t.Helper()
	if !strings.Contains(recipe, fragment) {
		t.Fatalf("recipe missing %q:\n%s", fragment, recipe)
	}
}

// requireNoRecipeFragment fails when a recipe contains a forbidden fragment.
func requireNoRecipeFragment(t *testing.T, recipe string, fragment string) {
	t.Helper()
	if strings.Contains(recipe, fragment) {
		t.Fatalf("recipe contains forbidden %q:\n%s", fragment, recipe)
	}
}
