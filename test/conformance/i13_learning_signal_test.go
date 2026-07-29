package conformance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// migrationsDirRelPath is internal/store/sqlite/migrations, relative to the
// repository root. I13 reads migration files directly from disk (design D1
// §6.4 / the D10 guard's own wording), not through internal/store/sqlite's
// exported surface — that package exports no way to read a migration's SQL
// text (design D7, the scope boundary this change enforces).
const migrationsDirRelPath = "internal/store/sqlite/migrations"

// migrationSQLText concatenates every *.sql file under
// internal/store/sqlite/migrations, sorted by file name (so multi-migration
// content is deterministic regardless of directory read order), and fails
// the test if none are found — the D10 guard this test's own RED depends
// on: without it, a directory move or an empty migrations folder would make
// this test pass vacuously instead of failing for the right reason.
func migrationSQLText(t *testing.T) string {
	t.Helper()

	repoRoot := repoRootFromCaller(t)
	dir := filepath.Join(repoRoot, migrationsDirRelPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		t.Fatalf("found zero migration files under %s (D10's non-empty-corpus guard)", dir)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sb.Write(content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// extractTableBody returns the text between the outermost matching
// parentheses of "CREATE TABLE <tableName> (...)" in sqlText, depth-counted
// so a parenthesized default expression or CHECK constraint (none of which
// learning_signals declares today, but the parser should not assume that)
// cannot close the scan early. ok is false when the table is not declared
// anywhere in sqlText at all — this is the property TestI13 asserts BEFORE
// the FK property (D10's guard, and the reason this test's red today is
// "learning_signals is not found", not a false pass).
func extractTableBody(sqlText, tableName string) (body string, ok bool) {
	marker := "CREATE TABLE " + tableName
	idx := strings.Index(sqlText, marker)
	if idx == -1 {
		return "", false
	}
	rest := sqlText[idx+len(marker):]

	open := strings.IndexByte(rest, '(')
	if open == -1 {
		return "", false
	}

	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return rest[open+1 : i], true
			}
		}
	}
	return "", false
}

// targetIDLine returns the line inside a learning_signals table body that
// declares its target_id column, or "" if no such line exists.
func targetIDLine(tableBody string) string {
	for _, line := range strings.Split(tableBody, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "target_id") {
			return line
		}
	}
	return ""
}

// TestI13_LearningSignalHasNoFKToTarget asserts I13: learning_signals.target_id
// carries no REFERENCES clause, because a learning signal must outlive the
// deletion of the unit, trigger, belief or relation it evidences (spec
// R6.3, docs/03-data-model.md's "Learning" section). Untagged, unlike I01/
// I03/I21: this invariant is real today, not pending on a future domain
// layer (spec R6.3's own "not pending" framing).
func TestI13_LearningSignalHasNoFKToTarget(t *testing.T) {
	sqlText := migrationSQLText(t)

	body, ok := extractTableBody(sqlText, "learning_signals")
	if !ok {
		t.Fatal("learning_signals is not found in the embedded migrations — I13 has nothing to check yet")
	}

	line := targetIDLine(body)
	if line == "" {
		t.Fatal("learning_signals.target_id column is not found in its table definition")
	}

	if strings.Contains(strings.ToUpper(line), "REFERENCES") {
		t.Errorf(
			"learning_signals.target_id carries a REFERENCES clause: %q — "+
				"I13 requires no FK here, so a signal outlives the deletion of its target",
			strings.TrimSpace(line),
		)
	}
}
