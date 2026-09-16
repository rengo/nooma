//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakechannel"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// TestPendingQuestions_WriteOnlyVocabularyResolutions is the constraint
// migration 0004 deliberately does not carry.
//
// pending_questions.resolution is plain TEXT with no CHECK — no table in
// this schema carries one — and SQLite's dynamic typing will store anything
// at all in it. A mistyped literal anywhere in the write path persists
// happily and is found much later, by which point rows exist that no
// vocabulary can parse. Every L2 test in this change stays green through
// exactly that mutation: the fakes hold a ports.QuestionResolution, a typed
// value that cannot be a wrong string, while SQLite holds whatever the
// UPDATE wrote.
//
// The three resolutions are reached through the three methods that are the
// ONLY channels to that column — the port takes no resolution parameter, by
// design (ports/pendingquestionrepo.go's own note). Expire is driven by a
// real check pass, end to end; Confirm and Reject are driven directly,
// because the check-in path that will call them is PR6's and this test
// would otherwise have nothing to say about two thirds of the vocabulary
// until then.
func TestPendingQuestions_WriteOnlyVocabularyResolutions(t *testing.T) {
	ctx := context.Background()

	// A Wednesday at the digest hour; the question was asked five days
	// earlier and MaxDigestDeferrals digests have gone out since.
	now := time.Date(2026, 8, 5, prospection.DigestHour, 5, 0, 0, time.UTC)
	askedAt := now.AddDate(0, 0, -5)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q): %v", dbPath, err)
	}

	units := sqlite.NewUnitRepo(v)
	rels := sqlite.NewRelationRepo(v)
	questions := sqlite.NewPendingQuestionRepo(v)
	decisions := sqlite.NewDecisionLog(v)

	// One relation per question, because Create's own existence guard and
	// both reads' inner joins require a live one.
	//
	// Only q-expired was asked long enough ago to be stale. The other two
	// were asked an hour before this pass, with no digest in between, so
	// the sweep leaves them open for the two direct calls below — a
	// fixture that aged all three would have every question expire and
	// nothing left to confirm or reject.
	for i, q := range []struct {
		id      string
		askedAt time.Time
	}{
		{"q-expired", askedAt},
		{"q-confirmed", now.Add(-time.Hour)},
		{"q-rejected", now.Add(-time.Hour)},
	} {
		from, to := q.id+"-from", q.id+"-to"
		for _, u := range []string{from, to} {
			if err := units.Create(ctx, unit.Unit{
				ID: u, Type: unit.TypeKnowledge, Status: unit.StatusPool,
				Content: "content of " + u, Source: "chat",
				Weight: 1.0, WeightDecayRate: 0.01,
				LastTouchedAt: askedAt, CreatedAt: askedAt, UpdatedAt: askedAt,
			}); err != nil {
				t.Fatalf("seeding unit %s: %v", u, err)
			}
		}
		if err := rels.Upsert(ctx, ports.Relation{
			ID: "rel-" + q.id, FromUnitID: from, ToUnitID: to, Type: "same_topic",
			Strength: 0.5, Confidence: 0.4, CreatedBy: "consolidation", CreatedAt: askedAt,
		}); err != nil {
			t.Fatalf("seeding relation for %s: %v", q.id, err)
		}
		if err := questions.Create(ctx, ports.PendingQuestion{
			ID: q.id, Kind: ports.QuestionKindRelation, RelationID: "rel-" + q.id,
			CreatedAt: q.askedAt.Add(-time.Duration(i+1) * time.Hour),
		}); err != nil {
			t.Fatalf("seeding question %s: %v", q.id, err)
		}
		if err := questions.MarkAsked(ctx, q.id, q.askedAt); err != nil {
			t.Fatalf("MarkAsked(%s): %v", q.id, err)
		}
	}

	// MaxDigestDeferrals digests since the ask, and none of them today —
	// a digest sent today would make this pass's own digest not due.
	for i := 0; i < prospection.MaxDigestDeferrals; i++ {
		if err := decisions.Record(ctx, ports.Decision{
			ID: "dec-digest-" + string(rune('a'+i)), Action: ports.ActionCheckDigestSent,
			Rationale: "seeded digest history", OccurredAt: askedAt.AddDate(0, 0, i+1),
		}); err != nil {
			t.Fatalf("seeding digest history: %v", err)
		}
	}

	// The real pass. It needs a channel, a unit repo and a state repo to
	// reach the digest at all (check.go's own guard), and the expiry sweep
	// runs before anything is sent.
	if _, err := brain.NewCheckService(fixedClock{now: now},
		sqlite.NewTriggerRepo(v), sqlite.NewTimerRepo(v), &counterIDs{}, decisions,
		fakechannel.New(), units, sqlite.NewStateRepo(v), nil, "12449194", questions).
		Check(ctx, brain.CheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}

	// The two the check-in path will drive in PR6.
	if err := questions.Confirm(ctx, "q-confirmed", now); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := questions.Reject(ctx, "q-rejected", now); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	vocabulary := map[string]bool{}
	for _, r := range ports.AllQuestionResolutions() {
		vocabulary[string(r)] = true
	}

	// Closing the vault first keeps the two handles from arguing over the
	// WAL — due_scan_status_vocabulary_test.go's own reason.
	if err := v.Close(); err != nil {
		t.Fatalf("closing the vault before the raw read: %v", err)
	}
	raw, err := sql.Open("sqlite3", pragmaDSN(dbPath))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	assertDistinctResolutions(t, raw, vocabulary, len(vocabulary))
}

// assertDistinctResolutions fails unless every resolution the table holds
// is a vocabulary member, and unless it holds wantDistinct of them — an
// assertion that all three literals were actually reached, rather than one
// that a single write happened to spell correctly.
//
// A NULL resolution is not a failure here, unlike triggers.status: the
// column is nullable on purpose, because an unresolved question has no
// resolution and the row exists long before it has one. Only rows whose
// resolved_at is set are read.
func assertDistinctResolutions(t *testing.T, raw *sql.DB, vocabulary map[string]bool, wantDistinct int) {
	t.Helper()

	rows, err := raw.QueryContext(context.Background(),
		`SELECT DISTINCT resolution FROM pending_questions WHERE resolved_at IS NOT NULL`)
	if err != nil {
		t.Fatalf("select distinct pending_questions.resolution: %v", err)
	}
	defer func() { _ = rows.Close() }()

	seen := 0
	for rows.Next() {
		var resolution sql.NullString
		if err := rows.Scan(&resolution); err != nil {
			t.Fatalf("scan pending_questions.resolution: %v", err)
		}
		seen++
		if !resolution.Valid {
			t.Errorf("a resolved pending question holds a NULL resolution — resolved_at and resolution are set in one statement, so one without the other is a write path that split them")
			continue
		}
		if !vocabulary[resolution.String] {
			t.Errorf("pending_questions holds resolution %q, which is not a member of ports.AllQuestionResolutions() — the column has no CHECK constraint, so this test is the constraint", resolution.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("select distinct pending_questions.resolution: %v", err)
	}
	if seen != wantDistinct {
		t.Fatalf("pending_questions holds %d distinct resolution(s), want %d — every member of the vocabulary must be reached, or this test checks only the ones that were", seen, wantDistinct)
	}
}
