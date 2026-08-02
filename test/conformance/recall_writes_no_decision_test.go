// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestRecallWritesNoDecisionRow is the half of I12 that is easiest to get
// backwards, and the one design D9 states as an absence rather than a rule:
// there is no `recall.answered` action, and there is no row for a read.
//
// I12 says every automatic decision **with an effect** is recorded. Recall has
// no effect — it returns what is already there. A row per read would grow the
// audit trail without adding a single decision to it, and §11's glass box
// would become a query log that nobody can find a decision in.
//
// Stated as its own test because the property is invisible in the code: a
// reviewer sees `RecallService` not calling `log.Record` and cannot tell
// whether that is deliberate or an omission. This says it is deliberate, and
// fails the day someone "fixes" it.
//
// Its honest limitation: `RecallService` is not handed a `ports.DecisionLog`
// at all, so this asserts a property the constructor already makes
// unrepresentable. That is the point — it pins the shape, so a future change
// that threads a log into recall has to delete this test on purpose rather
// than drift past it.
func TestRecallWritesNoDecisionRow(t *testing.T) {
	ctx := context.Background()

	units := memrepo.NewUnits()
	log := memrepo.NewDecisionLog()
	lex := memrepo.NewLexical()
	index := brain.NewIndex(recall.VectorIndex{Model: "model-a"})

	// One live unit, findable by both legs, so the recall below genuinely
	// returns something — a recall that found nothing would pass this test
	// vacuously.
	// A fixed instant, not time.Now(): this test asserts nothing about time,
	// and a real clock in a fixture is a source of nondeterminism with no
	// upside (nooma-testing's own rule).
	at := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	u := unit.Unit{
		ID: "unit-1", Type: unit.TypeKnowledge, Content: "descaling the coffee machine",
		Status: unit.StatusPool, Weight: 1, WeightDecayRate: 0.01,
		LastTouchedAt: at, CreatedAt: at, UpdatedAt: at,
	}
	if err := units.Create(ctx, u); err != nil {
		t.Fatalf("seeding the unit: %v", err)
	}
	lex.SeedLexical(t, u.ID, u.Content)
	index.Add(u.ID, []float32{1, 0, 0})

	got, err := brain.NewRecallService(index, lex, units).
		Candidates(ctx, u.Content, []float32{1, 0, 0}, "model-a", "")
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("recall returned nothing — this test would pass vacuously, since a read that " +
			"found no units is not evidence that reads write no rows")
	}

	// A zero time returns every row the log holds.
	rows, err := log.Since(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("recall wrote %d decision_log row(s): %+v — I12 records decisions with an "+
			"EFFECT, and a read has none. There is no recall.answered action (design D9)",
			len(rows), rows)
	}
}
