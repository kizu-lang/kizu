// Package bootstrap_test validates the Go/Kizu 1:1 completion audit.
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type phaseRow struct {
	Phase    string
	Evidence string
	Strength string
	Missing  string
}

// TestBootstrapAuditCompletionGate checks the audit cannot silently claim completion.
func TestBootstrapAuditCompletionGate(t *testing.T) {
	audit := readAudit(t)
	rows := parsePhaseRows(t, audit)
	if len(rows) == 0 {
		t.Fatal("bootstrap audit has no phase rows")
	}
	requireKnownStrengths(t, rows)
	requireOpenWorkForIncompleteRows(t, audit, rows)
}

// TestBootstrapAuditStrictGate enforces the final 1:1 completion bar on demand.
func TestBootstrapAuditStrictGate(t *testing.T) {
	if os.Getenv("KIZU_REQUIRE_1TO1") != "1" {
		t.Skip("set KIZU_REQUIRE_1TO1=1 to enforce final Go/Kizu 1:1 completion")
	}
	rows := parsePhaseRows(t, readAudit(t))
	for _, row := range rows {
		if coverageStrength(row.Strength) != "strong" {
			t.Fatalf("phase %s is not complete: strength=%s missing=%s",
				row.Phase, row.Strength, row.Missing)
		}
		if row.Missing != "" && row.Missing != "none" {
			t.Fatalf("phase %s still has missing work: %s", row.Phase, row.Missing)
		}
	}
}

// readAudit loads the checked-in bootstrap 1:1 audit.
func readAudit(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "docs", "bootstrap-1to1-audit.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// parsePhaseRows extracts the current evidence table from the audit document.
func parsePhaseRows(t *testing.T, audit string) []phaseRow {
	t.Helper()
	rows := []phaseRow{}
	for _, line := range strings.Split(audit, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.Contains(line, "---") {
			continue
		}
		cells := splitMarkdownRow(line)
		if len(cells) != 4 || cells[0] == "Phase" {
			continue
		}
		rows = append(rows, phaseRow{
			Phase: cells[0], Evidence: cells[1], Strength: cells[2], Missing: cells[3],
		})
	}
	return rows
}

// splitMarkdownRow returns trimmed cells from a simple pipe-delimited row.
func splitMarkdownRow(line string) []string {
	trimmed := strings.Trim(line, "| ")
	raw := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(raw))
	for _, cell := range raw {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

// requireKnownStrengths rejects unreviewed coverage labels.
func requireKnownStrengths(t *testing.T, rows []phaseRow) {
	t.Helper()
	for _, row := range rows {
		switch coverageStrength(row.Strength) {
		case "strong", "medium", "weak":
		default:
			t.Fatalf("phase %s uses unknown coverage strength %q", row.Phase, row.Strength)
		}
	}
}

// requireOpenWorkForIncompleteRows keeps incomplete audits tied to tracked work.
func requireOpenWorkForIncompleteRows(t *testing.T, audit string, rows []phaseRow) {
	t.Helper()
	incomplete := 0
	for _, row := range rows {
		if coverageStrength(row.Strength) == "strong" && (row.Missing == "" || row.Missing == "none") {
			continue
		}
		incomplete++
	}
	if incomplete == 0 {
		return
	}
	for _, issue := range []string{"#111", "#112", "#113", "#114", "#115", "#116", "#117", "#118"} {
		if !strings.Contains(audit, issue) {
			t.Fatalf("incomplete audit is missing required tracking issue %s", issue)
		}
	}
}

// coverageStrength normalizes a short strength label from the audit table.
func coverageStrength(value string) string {
	for _, strength := range []string{"strong", "medium", "weak"} {
		if strings.HasPrefix(value, strength) {
			return strength
		}
	}
	return value
}

// repoRoot returns the repository root from this test package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
