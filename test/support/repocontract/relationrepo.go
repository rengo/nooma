package repocontract

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// RelationHarness is what a ports.RelationRepo implementation must offer the
// contract so the suite can put relations in front of it.
//
// EnsureUnit exists for the same reason repocontract.EmbeddingHarness's does
// (C11): relations.from_unit_id and relations.to_unit_id both REFERENCE
// units(id) (migration 0001:32-33) and the vault opens with foreign_keys=on,
// so an Upsert naming a unit that does not exist is a constraint violation
// over the real store and a no-op over an in-memory fake. Checked directly
// off the DDL before this file's first case was written, not assumed —
// design D6's "answered twice" rule caught exactly this shape of mistake
// once already (PR 9a's EmbeddingRepo contract) and it is not worth catching
// twice.
//
// SeedThreshold exists so ThresholdsFor's "a row is present" path is proven
// by both implementations, not only the absent-row path the task text names
// explicitly. relation_thresholds has no write method on ports.RelationRepo
// at all (by design — nothing in this PR needs one), so without a seed hook
// ThresholdsFor's SELECT would only ever be exercised against an empty
// table, and its column mapping would be untested.
type RelationHarness interface {
	ports.RelationRepo

	// EnsureUnit makes id a valid relation endpoint. The store inserts the
	// row; the fake does nothing.
	EnsureUnit(t *testing.T, id string)

	// SeedThreshold inserts a relation_thresholds row directly, bypassing
	// ports.RelationRepo entirely — there is no Upsert-shaped method for
	// this table in this port, on purpose (design D8 names only three
	// methods, each with a real caller; a fourth write method whose only
	// caller would be this suite is exactly the shape m1a-substrate D7
	// rejected for TranscriptionProvider).
	SeedThreshold(t *testing.T, relType string, persist, surface float64)
}

// RunRelationRepo runs the ports.RelationRepo contract against a fresh
// implementation built by newRepo for every subtest. newRepo must return one
// holding no relation and no relation_thresholds row.
//
// design D6's "answered twice" rule applies here as it does to every other
// repository port: the in-memory fake answers this suite at L2
// (test/conformance/relationrepo_memrepo_test.go, PR 11b) and
// internal/store/sqlite's implementation answers the identical suite at L3
// (internal/store/sqlite/relationrepo_integration_test.go, PR 11b), so the
// two cannot drift while one lags behind the other.
func RunRelationRepo(t *testing.T, newRepo func(t *testing.T) RelationHarness) {
	t.Helper()

	// I07's central claim, spec R5.3: a second Upsert over the same
	// (from, to, type) triple leaves exactly one row, never a
	// uniqueness-constraint error and never two rows, with the second run's
	// strength/confidence reflected. ID, CreatedBy and CreatedAt are the
	// first run's, unchanged — only the store can prove the SQL actually
	// does this via ON CONFLICT (asserted again at L3 against the real
	// UNIQUE constraint), but "which fields change on conflict" is ordinary
	// upsert semantics both implementations must agree on, not a store-only
	// promise, so it belongs here.
	t.Run("Upsert twice over the same triple leaves exactly one row (I07)", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-from")
		repo.EnsureUnit(t, "unit-to")
		ctx := context.Background()

		first := fixtureRelation("rel-1", "unit-from", "unit-to", "reference", 0.4, 0.5)
		if err := repo.Upsert(ctx, first); err != nil {
			t.Fatalf("first Upsert: %v", err)
		}

		second := first
		second.ID = "rel-2" // a caller-generated id that must NOT replace the first row's
		second.Strength = 0.9
		second.Confidence = 0.95
		second.CreatedBy = "consolidation" // must not overwrite the first row's CreatedBy either
		if err := repo.Upsert(ctx, second); err != nil {
			t.Fatalf("second Upsert over the same triple: %v", err)
		}

		got, err := repo.ByUnit(ctx, "unit-from")
		if err != nil {
			t.Fatalf("ByUnit: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("ByUnit returned %d relations after two Upserts over one triple, want 1: %v",
				len(got), got)
		}
		if got[0].ID != first.ID {
			t.Errorf("relation id = %q, want %q — the second Upsert's id must not replace the "+
				"first row's identity", got[0].ID, first.ID)
		}
		if got[0].CreatedBy != first.CreatedBy {
			t.Errorf("CreatedBy = %q, want %q — only strength/confidence change on conflict",
				got[0].CreatedBy, first.CreatedBy)
		}
		if !got[0].CreatedAt.Equal(first.CreatedAt) {
			t.Errorf("CreatedAt = %v, want %v — only strength/confidence change on conflict",
				got[0].CreatedAt, first.CreatedAt)
		}
		if got[0].Strength != second.Strength || got[0].Confidence != second.Confidence {
			t.Errorf("strength/confidence = %v/%v, want the second Upsert's %v/%v reflected",
				got[0].Strength, got[0].Confidence, second.Strength, second.Confidence)
		}
	})

	t.Run("Upsert over distinct types for the same pair leaves two rows", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-from")
		repo.EnsureUnit(t, "unit-to")
		ctx := context.Background()

		reference := fixtureRelation("rel-ref", "unit-from", "unit-to", "reference", 0.4, 0.5)
		duplicate := fixtureRelation("rel-dup", "unit-from", "unit-to", "duplicate", 0.6, 0.7)
		if err := repo.Upsert(ctx, reference); err != nil {
			t.Fatalf("Upsert reference: %v", err)
		}
		if err := repo.Upsert(ctx, duplicate); err != nil {
			t.Fatalf("Upsert duplicate: %v", err)
		}

		got, err := repo.ByUnit(ctx, "unit-from")
		if err != nil {
			t.Fatalf("ByUnit: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("ByUnit returned %d relations, want 2 — type is part of the UNIQUE triple, "+
				"so two types between the same pair are two distinct rows: %v", len(got), got)
		}
	})

	// design D8: "ByUnit's consumer is Phase C's read-only units route" — a
	// unit page shows every relation touching it, not only the ones it
	// originates.
	t.Run("ByUnit returns relations where the unit is either endpoint", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-a")
		repo.EnsureUnit(t, "unit-b")
		repo.EnsureUnit(t, "unit-c")
		ctx := context.Background()

		fromA := fixtureRelation("rel-from-a", "unit-a", "unit-b", "reference", 0.5, 0.5)
		toA := fixtureRelation("rel-to-a", "unit-c", "unit-a", "duplicate", 0.5, 0.5)
		unrelated := fixtureRelation("rel-unrelated", "unit-b", "unit-c", "reference", 0.5, 0.5)
		for _, r := range []ports.Relation{fromA, toA, unrelated} {
			if err := repo.Upsert(ctx, r); err != nil {
				t.Fatalf("Upsert %s: %v", r.ID, err)
			}
		}

		got, err := repo.ByUnit(ctx, "unit-a")
		if err != nil {
			t.Fatalf("ByUnit: %v", err)
		}
		gotIDs := make(map[string]bool, len(got))
		for _, r := range got {
			gotIDs[r.ID] = true
		}
		if len(got) != 2 || !gotIDs[fromA.ID] || !gotIDs[toA.ID] {
			t.Fatalf("ByUnit(unit-a) = %v, want exactly [%s %s]", got, fromA.ID, toA.ID)
		}
		if gotIDs[unrelated.ID] {
			t.Error("ByUnit(unit-a) includes a relation that touches neither endpoint of unit-a")
		}
	})

	t.Run("ByUnit on a unit with no relations returns none", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-lonely")

		got, err := repo.ByUnit(context.Background(), "unit-lonely")
		if err != nil {
			t.Fatalf("ByUnit: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ByUnit(unit-lonely) = %v, want none", got)
		}
	})

	// Design D8, R5.6, Q1's closed answer: relation_thresholds seeds no row
	// for any type, so a nil row is the ordinary case, not an error — this is
	// where relation.Resolve's nil-row fallback actually originates.
	t.Run("ThresholdsFor returns (nil, nil) for an absent row", func(t *testing.T) {
		repo := newRepo(t)

		got, err := repo.ThresholdsFor(context.Background(), "reference")
		if err != nil {
			t.Fatalf("ThresholdsFor: %v", err)
		}
		if got != nil {
			t.Errorf("ThresholdsFor(absent) = %v, want nil", got)
		}
	})

	// The present-row path: not named explicitly by the task text, but added
	// because otherwise ThresholdsFor's SELECT and column mapping is only
	// ever exercised against an empty table, and the whole reason this
	// method exists is to feed relation.Resolve a real row when one exists.
	t.Run("ThresholdsFor returns the stored thresholds when a row is present", func(t *testing.T) {
		repo := newRepo(t)
		repo.SeedThreshold(t, "reference", 0.42, 0.77)

		got, err := repo.ThresholdsFor(context.Background(), "reference")
		if err != nil {
			t.Fatalf("ThresholdsFor: %v", err)
		}
		if got == nil {
			t.Fatal("ThresholdsFor(present) = nil, want the seeded row")
		}
		want := relation.Thresholds{Persist: 0.42, Surface: 0.77}
		if *got != want {
			t.Errorf("ThresholdsFor(present) = %+v, want %+v", *got, want)
		}
	})

	t.Run("ThresholdsFor for one type does not see another type's row", func(t *testing.T) {
		repo := newRepo(t)
		repo.SeedThreshold(t, "reference", 0.42, 0.77)

		got, err := repo.ThresholdsFor(context.Background(), "duplicate")
		if err != nil {
			t.Fatalf("ThresholdsFor: %v", err)
		}
		if got != nil {
			t.Errorf("ThresholdsFor(duplicate) = %v, want nil — the seeded row is for %q", got, "reference")
		}
	})
}

// fixtureRelation builds a minimal, valid ports.Relation. At is a fixed
// instant — Upsert reads no clock (design D5's rule, applied to this port
// too), so every fixture in this suite carries the same literal.
func fixtureRelation(id, from, to, relType string, strength, confidence float64) ports.Relation {
	return ports.Relation{
		ID:         id,
		FromUnitID: from,
		ToUnitID:   to,
		Type:       relType,
		Strength:   strength,
		Confidence: confidence,
		CreatedBy:  "system",
		CreatedAt:  time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}
}
