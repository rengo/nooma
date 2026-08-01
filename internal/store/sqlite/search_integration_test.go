//go:build integration

package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/test/support/repocontract"
)

var searchFixtureTime = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// searchHarness adapts the real FTS5 leg to repocontract.LexicalSeeder.
//
// SeedLexical inserts a unit rather than writing units_fts directly: the FTS
// table is external-content (content='units') and kept current by triggers,
// so writing it by hand would test a path production never takes.
type searchHarness struct {
	*Search
	v *Vault
}

func (h searchHarness) SeedLexical(t *testing.T, id, content string) {
	t.Helper()
	h.seedUnit(t, id, content, unit.StatusPool)
}

func (h searchHarness) seedUnit(t *testing.T, id, content string, status unit.Status) {
	t.Helper()

	stamp := searchFixtureTime.Format(unitTimeLayout)
	if _, err := h.v.db.ExecContext(context.Background(), `
INSERT INTO units (id, type, content, status, weight, weight_decay_rate,
                   last_touched_at, source, created_at, updated_at)
VALUES (?, 'knowledge', ?, ?, 1.0, 0.01, ?, 'chat', ?, ?)`,
		id, content, string(status), stamp, stamp, stamp); err != nil {
		t.Fatalf("seeding unit %s: %v", id, err)
	}
}

// TestSearch_Contract runs the same repocontract.RunLexicalSearch suite the
// in-memory fake answers at L2, now against real FTS5 at L3 — design D6's
// "answered twice" rule, and after C11 the run that decides whether the
// contract was a contract or one implementation's opinion.
func TestSearch_Contract(t *testing.T) {
	repocontract.RunLexicalSearch(t, func(t *testing.T) repocontract.LexicalSeeder {
		v := openTestVault(t)
		return searchHarness{Search: NewSearch(v), v: v}
	})
}

// TestSearch_ReturnsOnlyPoolUnits is I02's storage half (R3.3), and the half
// the shared contract cannot carry: the fake has no notion of status.
//
// All four statuses share matching vocabulary, so any unit that comes back
// other than the pool one came back because of its status, not because of
// its text. The filter is positive — status = 'pool' — because a negation
// list would silently admit a status added later, which is the same argument
// unit.Status.IsLive already makes.
func TestSearch_ReturnsOnlyPoolUnits(t *testing.T) {
	v := openTestVault(t)
	h := searchHarness{Search: NewSearch(v), v: v}

	const shared = "descaling solution for the coffee machine"
	for id, status := range map[string]unit.Status{
		"unit-pool":       unit.StatusPool,
		"unit-archived":   unit.StatusArchived,
		"unit-superseded": unit.StatusSuperseded,
		"unit-incomplete": unit.StatusIncomplete,
	} {
		h.seedUnit(t, id, shared, status)
	}

	got, err := h.SearchLexical(context.Background(), recall.Tokenize(shared), 10)
	if err != nil {
		t.Fatalf("SearchLexical: %v", err)
	}
	if len(got) != 1 || got[0] != "unit-pool" {
		t.Errorf("SearchLexical = %v, want [unit-pool] — every seeded unit matches the same "+
			"words, so anything else here came back on its status (I02)", got)
	}
}

// TestSearch_BM25SignConventionIsAscending executes what no design session
// could: ADR-0010:19-22 records that bm25() returns NEGATIVE values, so
// ORDER BY bm25(units_fts) ascending is best-first. The design flagged it as
// design Risk #2 — assumed, never run against a real SQLite.
//
// This asserts the sign directly rather than inferring it from a ranking, so
// a failure says which half is wrong: the ADR's claim, or the ORDER BY built
// on it.
func TestSearch_BM25SignConventionIsAscending(t *testing.T) {
	v := openTestVault(t)
	h := searchHarness{Search: NewSearch(v), v: v}

	h.SeedLexical(t, "unit-strong", "descaling descaling descaling")
	h.SeedLexical(t, "unit-weak",
		"descaling appears once in a much longer document about entirely other things")

	rows, err := v.db.QueryContext(context.Background(), `
SELECT u.id, bm25(units_fts)
FROM units_fts
JOIN units u ON u.rowid = units_fts.rowid
WHERE units_fts MATCH ?
ORDER BY bm25(units_fts)`, `"descaling"`)
	if err != nil {
		t.Fatalf("querying bm25 directly: %v", err)
	}
	defer func() { _ = rows.Close() }()

	type scored struct {
		id    string
		score float64
	}
	var got []scored
	for rows.Next() {
		var s scored
		if err := rows.Scan(&s.id, &s.score); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both units to match, got %v", got)
	}

	for _, s := range got {
		if s.score >= 0 {
			t.Errorf("bm25(%s) = %v, but ADR-0010:19-22 states bm25() returns negative "+
				"values — if this is genuinely non-negative, ORDER BY bm25 ascending is "+
				"worst-first and design Risk #2 resolved the wrong way", s.id, s.score)
		}
	}
	if got[0].score > got[1].score {
		t.Fatalf("ascending order did not put the lower score first: %v", got)
	}
	if got[0].id != "unit-strong" {
		t.Errorf("ascending bm25 ranked %q first, want unit-strong — ascending must be "+
			"best-first, or the ORDER BY in search.go is backwards", got[0].id)
	}
}

// TestSearch_ReproducesCorpusLexicalRanking closes design §4.2's loop: the
// recall corpus states a lexical_ranking per query, and until real FTS5
// reproduces it that number is one a case author invented rather than a
// recording of what the engine does.
//
// This is task 9c.2's implicit dependency on PR 8c's corpus, which
// design.md's own dependency graph does not state.
func TestSearch_ReproducesCorpusLexicalRanking(t *testing.T) {
	repoRoot := repoRootFromPackageDir(t)
	casesDir := filepath.Join(repoRoot, "testdata", "recall", "cases")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", casesDir, err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		var example goldenset.RecallExample
		path := filepath.Join(casesDir, entry.Name())
		if err := goldenset.Load(path, &example); err != nil {
			t.Fatalf("loading %s: %v", path, err)
		}

		for qi, q := range example.Queries {
			if len(q.LexicalRanking) == 0 {
				continue
			}
			checked++

			t.Run(entry.Name(), func(t *testing.T) {
				v := openTestVault(t)
				h := searchHarness{Search: NewSearch(v), v: v}
				for _, u := range example.Units {
					h.seedUnit(t, u.ID, u.Content, unit.Status(u.Status))
				}

				got, err := h.SearchLexical(context.Background(), recall.Tokenize(q.Query), 0)
				if err != nil {
					t.Fatalf("SearchLexical: %v", err)
				}

				want := q.LexicalRanking
				if !equalIDs(got, want) {
					gotJSON, _ := json.Marshal(got)
					wantJSON, _ := json.Marshal(want)
					t.Errorf("query %d (%q): real FTS5 ranked\n  %s\nbut the case states\n  %s\n\n"+
						"The corpus is the thing that is wrong here unless search.go's query "+
						"changed: bm25 is bm25, and a stated ranking no engine produces is a "+
						"number someone invented (design §4.2).",
						qi, q.Query, gotJSON, wantJSON)
				}
			})
		}
	}

	if checked == 0 {
		t.Fatal("no corpus query states a lexical_ranking — this test would have passed " +
			"vacuously, and design §4.2's loop would still be open")
	}
}

// repoRootFromPackageDir walks up from this package's directory to the module
// root. It verifies go.mod is there rather than trusting the hop count: a
// package move would otherwise silently point the corpus read at the wrong
// directory, and os.ReadDir on a missing path fails with a message that says
// nothing about why.
func repoRootFromPackageDir(t *testing.T) string {
	t.Helper()

	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s — this test assumes it runs from "+
			"internal/store/sqlite; if the package moved, fix the hop count: %v", root, err)
	}
	return root
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
