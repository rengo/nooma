// Package repocontract holds the shared conformance suite for
// ports.UnitRepo (design.md D6). RunUnitRepo pins the contract every
// implementation of the port must satisfy — the in-memory fake
// (test/support/memrepo, PR 3) at L2, and internal/store/sqlite's real
// implementation (PR 4) at L3 — so both answer the exact same suite
// instead of drifting apart the moment one implementation lags behind the
// other (design D6's "answered twice" standing rule: a PR that widens
// ports.UnitRepo adds the contract case and the fake's implementation in
// the same PR).
//
// This package is untagged so both the untagged L2 suite
// (test/conformance) and the integration-tagged L3 suite
// (test/integration) can import it — the same shape test/support/schema
// already established for schema_doc_test.go and schema_golden_test.go.
package repocontract

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
)

// RunUnitRepo runs the ports.UnitRepo contract against a fresh repository
// instance, built by newRepo for every subtest. newRepo must return a
// repository with no unit already stored in it.
func RunUnitRepo(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("Create and ByID round-trip", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		u := fixtureUnit("unit-1", unit.StatusPool)

		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if !reflect.DeepEqual(got, u) {
			t.Fatalf("ByID round-trip: got %+v, want %+v", got, u)
		}
	})

	t.Run("Create on a duplicate id returns ErrUnitExists", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		u := fixtureUnit("unit-dup", unit.StatusPool)

		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		if err := repo.Create(ctx, u); !errors.Is(err, ports.ErrUnitExists) {
			t.Fatalf("second Create: got %v, want ErrUnitExists", err)
		}
	})

	t.Run("ByID on a missing id returns ErrUnitNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if _, err := repo.ByID(ctx, "does-not-exist"); !errors.Is(err, ports.ErrUnitNotFound) {
			t.Fatalf("ByID: got %v, want ErrUnitNotFound", err)
		}
	})

	t.Run("LiveByIDs excludes archived, superseded and incomplete", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		pool := fixtureUnit("live-pool", unit.StatusPool)
		archived := fixtureUnit("live-archived", unit.StatusArchived)
		superseded := fixtureUnit("live-superseded", unit.StatusSuperseded)
		incomplete := fixtureUnit("live-incomplete", unit.StatusIncomplete)
		for _, u := range []unit.Unit{pool, archived, superseded, incomplete} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		got, err := repo.LiveByIDs(ctx, []string{incomplete.ID, pool.ID, superseded.ID, archived.ID})
		if err != nil {
			t.Fatalf("LiveByIDs: %v", err)
		}
		gotIDs := idsOf(got)
		wantIDs := []string{pool.ID}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("LiveByIDs: got %v, want exactly %v (only the pool unit)", gotIDs, wantIDs)
		}
	})

	t.Run("LiveByIDs preserves the caller's ids order", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		a := fixtureUnit("order-a", unit.StatusPool)
		b := fixtureUnit("order-b", unit.StatusPool)
		c := fixtureUnit("order-c", unit.StatusPool)
		for _, u := range []unit.Unit{a, b, c} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		got, err := repo.LiveByIDs(ctx, []string{c.ID, a.ID, b.ID})
		if err != nil {
			t.Fatalf("LiveByIDs: %v", err)
		}
		gotIDs := idsOf(got)
		wantIDs := []string{c.ID, a.ID, b.ID}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("LiveByIDs order: got %v, want %v (the caller's ids order)", gotIDs, wantIDs)
		}
	})

	t.Run("UpdateContent leaves every other column unchanged", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		u := fixtureUnit("update-1", unit.StatusPool)
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		at := u.UpdatedAt.Add(time.Hour)
		const newContent = "revised content"
		if err := repo.UpdateContent(ctx, u.ID, newContent, at); err != nil {
			t.Fatalf("UpdateContent: %v", err)
		}

		got, err := repo.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		want := u
		want.Content = newContent
		want.UpdatedAt = at
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UpdateContent changed more than Content/UpdatedAt: got %+v, want %+v", got, want)
		}
	})

	t.Run("UpdateEventAt writes event_at and updated_at, leaving due_at and content untouched", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		u := fixtureUnit("update-event-1", unit.StatusPool)
		existingDue := u.UpdatedAt.Add(72 * time.Hour)
		u.DueAt = &existingDue
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// Two distinguishable instants — the new event value and the audit
		// timestamp — so a call site that swaps its own two arguments, or
		// an implementation that writes this method's value into due_at
		// instead of event_at, is caught: design D4's "residual risk a name
		// cannot close" (C11's lesson — the contract, not review, decides
		// this).
		newEventAt := u.UpdatedAt.Add(240 * time.Hour)
		at := u.UpdatedAt.Add(time.Hour)
		if err := repo.UpdateEventAt(ctx, u.ID, newEventAt, at); err != nil {
			t.Fatalf("UpdateEventAt: %v", err)
		}

		got, err := repo.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		want := u
		want.EventAt = &newEventAt
		want.UpdatedAt = at
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UpdateEventAt changed more than EventAt/UpdatedAt: got %+v, want %+v", got, want)
		}
	})

	t.Run("UpdateDueAt writes due_at and updated_at, leaving event_at and content untouched — the mirror case", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		u := fixtureUnit("update-due-1", unit.StatusPool)
		existingEvent := u.UpdatedAt.Add(72 * time.Hour)
		u.EventAt = &existingEvent
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		newDueAt := u.UpdatedAt.Add(240 * time.Hour)
		at := u.UpdatedAt.Add(time.Hour)
		if err := repo.UpdateDueAt(ctx, u.ID, newDueAt, at); err != nil {
			t.Fatalf("UpdateDueAt: %v", err)
		}

		got, err := repo.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		want := u
		want.DueAt = &newDueAt
		want.UpdatedAt = at
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UpdateDueAt changed more than DueAt/UpdatedAt: got %+v, want %+v", got, want)
		}
	})

	t.Run("UpdateEventAt on a missing id returns ErrUnitNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		someInstant := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
		if err := repo.UpdateEventAt(ctx, "does-not-exist", someInstant, someInstant); !errors.Is(err, ports.ErrUnitNotFound) {
			t.Fatalf("UpdateEventAt: got %v, want ErrUnitNotFound", err)
		}
	})

	t.Run("UpdateDueAt on a missing id returns ErrUnitNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		someInstant := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
		if err := repo.UpdateDueAt(ctx, "does-not-exist", someInstant, someInstant); !errors.Is(err, ports.ErrUnitNotFound) {
			t.Fatalf("UpdateDueAt: got %v, want ErrUnitNotFound", err)
		}
	})

	t.Run("SetStatus with a mismatched from returns ErrStatusConflict", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		u := fixtureUnit("status-1", unit.StatusPool)
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		wrongFrom := unit.StatusArchived
		if err := repo.SetStatus(ctx, u.ID, wrongFrom, unit.StatusPool, u.UpdatedAt); !errors.Is(err, ports.ErrStatusConflict) {
			t.Fatalf("SetStatus with wrong from (%s, actual status is %s): got %v, want ErrStatusConflict", wrongFrom, u.Status, err)
		}

		at := u.UpdatedAt.Add(time.Minute)
		if err := repo.SetStatus(ctx, u.ID, unit.StatusPool, unit.StatusArchived, at); err != nil {
			t.Fatalf("SetStatus with the correct from: %v", err)
		}
		got, err := repo.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Status != unit.StatusArchived {
			t.Fatalf("Status after SetStatus: got %s, want %s", got.Status, unit.StatusArchived)
		}
	})
}

// RunApplyBoosts runs the ports.UnitRepo.ApplyBoosts contract against a
// fresh repository instance, built by newRepo for every subtest. newRepo
// must return a repository with no unit already stored in it.
//
// Spec R1.1, R1.4; design §3.1(a)-(e), §5.2.
func RunApplyBoosts(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("writes each unit's own (Weight, LastTouchedAt) pair, never a cross-unit zip", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		a := fixtureUnit("boost-a", unit.StatusPool)
		b := fixtureUnit("boost-b", unit.StatusPool)
		c := fixtureUnit("boost-c", unit.StatusPool)
		for _, u := range []unit.Unit{a, b, c} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		// Every unit gets a distinguishable (Weight, LastTouchedAt) pair —
		// a cross-unit zip (unit A's weight landing with unit B's
		// timestamp, or vice versa) would be caught by this fixture and
		// missed by one where every unit shares the same values.
		at := a.UpdatedAt.Add(time.Hour)
		boosts := []weight.Boost{
			{UnitID: a.ID, Weight: 1.1, LastTouchedAt: at.Add(1 * time.Minute)},
			{UnitID: b.ID, Weight: 1.2, LastTouchedAt: at.Add(2 * time.Minute)},
			{UnitID: c.ID, Weight: 1.3, LastTouchedAt: at.Add(3 * time.Minute)},
		}
		if err := repo.ApplyBoosts(ctx, boosts, at); err != nil {
			t.Fatalf("ApplyBoosts: %v", err)
		}

		for _, b := range boosts {
			got, err := repo.ByID(ctx, b.UnitID)
			if err != nil {
				t.Fatalf("ByID %s: %v", b.UnitID, err)
			}
			if got.Weight != b.Weight {
				t.Errorf("unit %s Weight = %v, want %v (its own Boost, not another unit's)",
					b.UnitID, got.Weight, b.Weight)
			}
			if !got.LastTouchedAt.Equal(b.LastTouchedAt) {
				t.Errorf("unit %s LastTouchedAt = %v, want %v (its own Boost, not another unit's)",
					b.UnitID, got.LastTouchedAt, b.LastTouchedAt)
			}
			if !got.UpdatedAt.Equal(at) {
				t.Errorf("unit %s UpdatedAt = %v, want the call's own at %v", b.UnitID, got.UpdatedAt, at)
			}
		}
	})

	t.Run("a boost naming a non-existent unit returns ErrUnitNotFound and touches nothing", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		a := fixtureUnit("boost-existing-a", unit.StatusPool)
		c := fixtureUnit("boost-existing-c", unit.StatusPool)
		for _, u := range []unit.Unit{a, c} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		at := a.UpdatedAt.Add(time.Hour)
		boosts := []weight.Boost{
			{UnitID: a.ID, Weight: 1.5, LastTouchedAt: at},
			{UnitID: "boost-missing", Weight: 1.6, LastTouchedAt: at},
			{UnitID: c.ID, Weight: 1.7, LastTouchedAt: at},
		}
		if err := repo.ApplyBoosts(ctx, boosts, at); !errors.Is(err, ports.ErrUnitNotFound) {
			t.Fatalf("ApplyBoosts: got %v, want ErrUnitNotFound", err)
		}

		// Every *other* row in the same call — including ones that come
		// before the missing id in the slice — must be untouched.
		for _, u := range []unit.Unit{a, c} {
			got, err := repo.ByID(ctx, u.ID)
			if err != nil {
				t.Fatalf("ByID %s: %v", u.ID, err)
			}
			if got.Weight != u.Weight || !got.LastTouchedAt.Equal(u.LastTouchedAt) {
				t.Errorf("unit %s was touched despite ApplyBoosts failing on a different id in the "+
					"same call: got Weight=%v LastTouchedAt=%v, want unchanged %v/%v",
					u.ID, got.Weight, got.LastTouchedAt, u.Weight, u.LastTouchedAt)
			}
		}
	})

	t.Run("a non-finite Weight anywhere in the batch returns ErrNonFiniteWeight and writes nothing", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		a := fixtureUnit("boost-finite-a", unit.StatusPool)
		b := fixtureUnit("boost-finite-b", unit.StatusPool)
		for _, u := range []unit.Unit{a, b} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		at := a.UpdatedAt.Add(time.Hour)
		for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			boosts := []weight.Boost{
				{UnitID: a.ID, Weight: 1.9, LastTouchedAt: at}, // finite, same call
				{UnitID: b.ID, Weight: bad, LastTouchedAt: at},
			}
			if err := repo.ApplyBoosts(ctx, boosts, at); !errors.Is(err, ports.ErrNonFiniteWeight) {
				t.Fatalf("ApplyBoosts with Weight=%v: got %v, want ErrNonFiniteWeight", bad, err)
			}

			gotA, err := repo.ByID(ctx, a.ID)
			if err != nil {
				t.Fatalf("ByID %s: %v", a.ID, err)
			}
			if gotA.Weight != a.Weight || !gotA.LastTouchedAt.Equal(a.LastTouchedAt) {
				t.Errorf("finite boost for %s in the same batch as Weight=%v was written despite "+
					"the refusal: got %v/%v, want unchanged %v/%v",
					a.ID, bad, gotA.Weight, gotA.LastTouchedAt, a.Weight, a.LastTouchedAt)
			}
		}
	})

	t.Run("at lands in updated_at, Boost.LastTouchedAt lands in last_touched_at — distinct instants", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		u := fixtureUnit("boost-swap", unit.StatusPool)
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// UpdateEventAt's own fixturing pattern (repocontract's existing
		// case above): distinct instants, so a swapped-argument
		// implementation fails instead of passing by coincidence.
		at := u.UpdatedAt.Add(time.Hour)
		touchedAt := u.UpdatedAt.Add(48 * time.Hour)
		boosts := []weight.Boost{{UnitID: u.ID, Weight: 1.42, LastTouchedAt: touchedAt}}
		if err := repo.ApplyBoosts(ctx, boosts, at); err != nil {
			t.Fatalf("ApplyBoosts: %v", err)
		}

		got, err := repo.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if !got.UpdatedAt.Equal(at) {
			t.Errorf("UpdatedAt = %v, want the call's own at %v", got.UpdatedAt, at)
		}
		if !got.LastTouchedAt.Equal(touchedAt) {
			t.Errorf("LastTouchedAt = %v, want the Boost's own LastTouchedAt %v", got.LastTouchedAt, touchedAt)
		}
		if got.UpdatedAt.Equal(got.LastTouchedAt) {
			t.Fatal("UpdatedAt and LastTouchedAt landed equal — the fixture uses distinct instants " +
				"precisely so a swapped-argument implementation cannot pass silently")
		}
	})
}

// RunCountLiveByType runs the ports.UnitRepo.CountLiveByType contract
// against a fresh repository instance, built by newRepo for every subtest.
// newRepo must return a repository with no unit already stored in it.
//
// Spec R1.2; design §4.1.
func RunCountLiveByType(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("counts live units of the requested type only", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		// Live and non-live units across two unit.Type values, plus a
		// third type with no live unit at all, so the zero case is
		// exercised too.
		fixtures := []struct {
			id     string
			typ    unit.Type
			status unit.Status
		}{
			{"count-task-live-1", unit.TypeTask, unit.StatusPool},
			{"count-task-live-2", unit.TypeTask, unit.StatusPool},
			{"count-task-archived", unit.TypeTask, unit.StatusArchived},
			{"count-task-superseded", unit.TypeTask, unit.StatusSuperseded},
			{"count-task-incomplete", unit.TypeTask, unit.StatusIncomplete},
			{"count-event-live", unit.TypeEvent, unit.StatusPool},
			{"count-knowledge-none-live", unit.TypeKnowledge, unit.StatusArchived},
		}
		for _, f := range fixtures {
			u := fixtureUnit(f.id, f.status)
			u.Type = f.typ
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		if got, err := repo.CountLiveByType(ctx, unit.TypeTask); err != nil {
			t.Fatalf("CountLiveByType(task): %v", err)
		} else if got != 2 {
			t.Errorf("CountLiveByType(task) = %d, want 2 (only the two pool-status task units)", got)
		}

		if got, err := repo.CountLiveByType(ctx, unit.TypeEvent); err != nil {
			t.Fatalf("CountLiveByType(event): %v", err)
		} else if got != 1 {
			t.Errorf("CountLiveByType(event) = %d, want 1", got)
		}

		if got, err := repo.CountLiveByType(ctx, unit.TypeKnowledge); err != nil {
			t.Fatalf("CountLiveByType(knowledge): %v", err)
		} else if got != 0 {
			t.Errorf("CountLiveByType(knowledge) = %d, want 0 (its one unit is archived, not live)", got)
		}
	})
}

// RunIncompleteOlderThan runs the ports.UnitRepo.IncompleteOlderThan
// contract against a fresh repository instance, built by newRepo for every
// subtest. newRepo must return a repository with no unit already stored in
// it.
//
// Spec R5.1; design §4.1. IncompleteOlderThan is the one deliberate
// non-live read in M2 (I02's exception) — its filter is status = incomplete
// AND created_at < cutoff, never a general status-parameterized list.
func RunIncompleteOlderThan(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("an incomplete unit older than cutoff is returned, a younger one is not, and every other status is excluded regardless of age", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

		older := fixtureUnit("incomplete-older", unit.StatusIncomplete)
		older.CreatedAt = cutoff.Add(-time.Hour)
		younger := fixtureUnit("incomplete-younger", unit.StatusIncomplete)
		younger.CreatedAt = cutoff.Add(time.Hour)
		// I02's exception is named, not general: a pool/archived/superseded
		// unit created well before cutoff must never surface here, no
		// matter its age.
		oldPool := fixtureUnit("pool-old", unit.StatusPool)
		oldPool.CreatedAt = cutoff.Add(-time.Hour)
		oldArchived := fixtureUnit("archived-old", unit.StatusArchived)
		oldArchived.CreatedAt = cutoff.Add(-time.Hour)
		oldSuperseded := fixtureUnit("superseded-old", unit.StatusSuperseded)
		oldSuperseded.CreatedAt = cutoff.Add(-time.Hour)
		for _, u := range []unit.Unit{older, younger, oldPool, oldArchived, oldSuperseded} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		got, err := repo.IncompleteOlderThan(ctx, cutoff)
		if err != nil {
			t.Fatalf("IncompleteOlderThan: %v", err)
		}
		if len(got) != 1 || got[0].UnitID != older.ID {
			t.Fatalf("IncompleteOlderThan(%v) = %v, want exactly [%s] (the incomplete unit older "+
				"than cutoff; not the younger incomplete unit, and not any pool/archived/superseded "+
				"unit regardless of age)", cutoff, got, older.ID)
		}
		if got[0].CreatedAt.IsZero() {
			t.Errorf("IncompleteOlderThan()[0].CreatedAt is zero, want %v", older.CreatedAt)
		}
	})

	// The boundary is the whole point of this case. The subtest above uses
	// CreatedAt one hour either side of cutoff, which cannot tell `<` from
	// `<=` — both operators pass it. This suite is what PR 5's real SQL
	// predicate gets validated against, so without an exactly-at-cutoff
	// fixture a `WHERE created_at <= ?` would ship green.
	//
	// Strict `<` is the correct operator, and it is not an arbitrary pick:
	// the port's doc comment says "strictly before cutoff", and callers
	// compute cutoff as now-IncompleteExpiryHours, so a unit created exactly
	// at cutoff has aged exactly the expiry window and not past it.
	t.Run("an incomplete unit created exactly at cutoff is excluded, because the bound is strict", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

		atCutoff := fixtureUnit("incomplete-at-cutoff", unit.StatusIncomplete)
		atCutoff.CreatedAt = cutoff
		if err := repo.Create(ctx, atCutoff); err != nil {
			t.Fatalf("Create %s: %v", atCutoff.ID, err)
		}

		got, err := repo.IncompleteOlderThan(ctx, cutoff)
		if err != nil {
			t.Fatalf("IncompleteOlderThan: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("IncompleteOlderThan(%v) = %v, want empty: a unit created exactly at cutoff "+
				"is not strictly older than it. An implementation returning it is using `<=` where "+
				"the port promises `<`.", cutoff, got)
		}
	})
}

// RunLiveDecayStates runs the ports.UnitRepo.LiveDecayStates contract
// against a fresh repository instance, built by newRepo for every subtest.
// newRepo must return a repository with no unit already stored in it.
//
// Design §4.1. LiveDecayStates returns pool-status units only, carrying the
// five decay fields (consolidation.Cold's shape) — no unit.Unit-shaped
// value anywhere in the return.
func RunLiveDecayStates(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("returns pool-status units only, carrying the decay fields", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		live := fixtureUnit("decay-live", unit.StatusPool)
		archived := fixtureUnit("decay-archived", unit.StatusArchived)
		superseded := fixtureUnit("decay-superseded", unit.StatusSuperseded)
		incomplete := fixtureUnit("decay-incomplete", unit.StatusIncomplete)
		for _, u := range []unit.Unit{live, archived, superseded, incomplete} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		got, err := repo.LiveDecayStates(ctx)
		if err != nil {
			t.Fatalf("LiveDecayStates: %v", err)
		}
		// The return type is asserted by the compiler (got is
		// []consolidation.Cold, never []unit.Unit) — this check is on the
		// content, not the shape.
		if len(got) != 1 {
			t.Fatalf("LiveDecayStates() = %v, want exactly one entry (the pool-status unit only)", got)
		}
		want := consolidation.Cold{
			UnitID:        live.ID,
			Status:        unit.StatusPool,
			Weight:        live.Weight,
			DecayRate:     live.WeightDecayRate,
			LastTouchedAt: live.LastTouchedAt,
		}
		if got[0] != want {
			t.Fatalf("LiveDecayStates()[0] = %+v, want %+v", got[0], want)
		}
	})
}

// fixtureUnit builds a minimal, valid unit.Unit for contract cases that do
// not care about any field beyond ID and Status.
func fixtureUnit(id string, status unit.Status) unit.Unit {
	at := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	return unit.Unit{
		ID:              id,
		Type:            unit.TypeTask,
		Content:         "fixture content for " + id,
		Status:          status,
		Weight:          1.0,
		WeightDecayRate: 0.1,
		LastTouchedAt:   at,
		Source:          "repocontract fixture",
		CreatedAt:       at,
		UpdatedAt:       at,
	}
}

func idsOf(units []unit.Unit) []string {
	ids := make([]string, len(units))
	for i, u := range units {
		ids[i] = u.ID
	}
	return ids
}

// RunLiveFocusCandidates runs the ports.UnitRepo.LiveFocusCandidates
// contract against a fresh repository instance.
//
// One thing this suite cannot distinguish, named rather than assumed: the
// fixture seeds a superseded and an incomplete unit among the ids, so a
// negative implementation (status != 'superseded' AND status !=
// 'incomplete') passes every case here exactly as a positive one
// (status = 'pool') does. I02 requires the positive filter, and no fixture
// built from today's vocabulary can tell the two apart — the difference
// only appears when M4 adds a fifth status, at which point the negative
// implementation silently starts returning it. The guard is therefore the
// SQL itself and this comment, not a case below.
func RunLiveFocusCandidates(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("returns only the live ids, with every focus.Candidate field", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		pool := fixtureUnit("focus-pool", unit.StatusPool)
		// Values distinct from fixtureUnit's defaults, so a field read
		// from the wrong column cannot pass by coincidence.
		pool.Type = unit.TypeEvent
		pool.Weight = 0.73
		pool.WeightDecayRate = 0.017
		pool.LastTouchedAt = focusFixtureTime.Add(-3 * time.Hour)
		pool.CreatedAt = focusFixtureTime.Add(-72 * time.Hour)
		dueAt := focusFixtureTime.Add(48 * time.Hour)
		pool.DueAt = &dueAt

		for _, u := range []unit.Unit{
			pool,
			fixtureUnit("focus-archived", unit.StatusArchived),
			fixtureUnit("focus-superseded", unit.StatusSuperseded),
			fixtureUnit("focus-incomplete", unit.StatusIncomplete),
		} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		got, err := repo.LiveFocusCandidates(ctx, []string{
			"focus-archived", "focus-pool", "focus-superseded", "focus-incomplete", "focus-absent",
		})
		if err != nil {
			t.Fatalf("LiveFocusCandidates: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("LiveFocusCandidates: got %+v, want exactly the pool unit", got)
		}

		want := focus.Candidate{
			ID:            pool.ID,
			Type:          pool.Type,
			Weight:        pool.Weight,
			DecayRate:     pool.WeightDecayRate,
			LastTouchedAt: pool.LastTouchedAt,
			CreatedAt:     pool.CreatedAt,
			DueAt:         &dueAt,
		}
		if got[0].DueAt == nil || !got[0].DueAt.Equal(*want.DueAt) {
			t.Errorf("DueAt: got %v, want %s", got[0].DueAt, *want.DueAt)
		}
		if got[0].DueAt == pool.DueAt {
			t.Error("DueAt: the repository handed back the caller's own pointer")
		}
		got[0].DueAt, want.DueAt = nil, nil
		if !reflect.DeepEqual(got[0], want) {
			t.Errorf("LiveFocusCandidates()[0] = %+v, want %+v", got[0], want)
		}
	})

	t.Run("a unit with no due date reads back with none", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Create(ctx, fixtureUnit("focus-undated", unit.StatusPool)); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.LiveFocusCandidates(ctx, []string{"focus-undated"})
		if err != nil {
			t.Fatalf("LiveFocusCandidates: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("LiveFocusCandidates: got %d candidates, want 1", len(got))
		}
		if got[0].DueAt != nil {
			t.Fatalf("DueAt: got %s, want nil", *got[0].DueAt)
		}
	})

	t.Run("orders by id, whatever order the ids arrive in", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		for _, id := range []string{"focus-c", "focus-a", "focus-b"} {
			if err := repo.Create(ctx, fixtureUnit(id, unit.StatusPool)); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}

		// Deliberately not the ids' own order, and deliberately not
		// LiveByIDs' posture either: that one answers in the caller's
		// order, this one answers in id order, because it is a set to be
		// ranked in core and not a lookup to be zipped back up.
		got, err := repo.LiveFocusCandidates(ctx, []string{"focus-b", "focus-c", "focus-a"})
		if err != nil {
			t.Fatalf("LiveFocusCandidates: %v", err)
		}

		ids := make([]string, 0, len(got))
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		if !reflect.DeepEqual(ids, []string{"focus-a", "focus-b", "focus-c"}) {
			t.Fatalf("LiveFocusCandidates ids = %v, want [focus-a focus-b focus-c]", ids)
		}
	})

	t.Run("an empty id set returns an empty slice, never an error", func(t *testing.T) {
		repo := newRepo(t)

		got, err := repo.LiveFocusCandidates(context.Background(), nil)
		if err != nil {
			t.Fatalf("LiveFocusCandidates: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("LiveFocusCandidates: got %d candidates, want 0", len(got))
		}
	})
}

// focusFixtureTime is RunLiveFocusCandidates' own anchor instant. Every
// value in that suite is an offset from it, so no case carries a second
// literal date.
var focusFixtureTime = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

// RunLiveFocusCandidatesByType runs the ports.UnitRepo.LiveFocusCandidatesByType
// contract against a fresh repository instance, built by newRepo for every
// subtest.
//
// Like RunLiveFocusCandidates, this suite cannot distinguish a positive
// status = 'pool' filter from a negative exclusion list built out of
// today's four statuses — I02 requires the positive filter, and the
// difference only appears once a fifth status exists. Named here, not
// re-derived: the same limitation RunLiveFocusCandidates' own doc comment
// states.
func RunLiveFocusCandidatesByType(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("returns pool units of the wanted types only, excluding archived/superseded/incomplete and unwanted types", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		wantedTask := fixtureUnit("byType-pool-task", unit.StatusPool)
		wantedTask.Type = unit.TypeTask
		wantedEvent := fixtureUnit("byType-pool-event", unit.StatusPool)
		wantedEvent.Type = unit.TypeEvent
		unwantedType := fixtureUnit("byType-pool-knowledge", unit.StatusPool)
		unwantedType.Type = unit.TypeKnowledge
		archived := fixtureUnit("byType-archived", unit.StatusArchived)
		archived.Type = unit.TypeTask
		superseded := fixtureUnit("byType-superseded", unit.StatusSuperseded)
		superseded.Type = unit.TypeTask
		incomplete := fixtureUnit("byType-incomplete", unit.StatusIncomplete)
		incomplete.Type = unit.TypeTask

		for _, u := range []unit.Unit{wantedTask, wantedEvent, unwantedType, archived, superseded, incomplete} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		got, err := repo.LiveFocusCandidatesByType(ctx, []unit.Type{unit.TypeTask, unit.TypeEvent})
		if err != nil {
			t.Fatalf("LiveFocusCandidatesByType: %v", err)
		}
		gotIDs := focusCandidateIDs(got)
		wantIDs := []string{wantedEvent.ID, wantedTask.ID} // id order: "byType-pool-event" < "byType-pool-task"
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("LiveFocusCandidatesByType ids = %v, want %v (id order, wanted types only, live only)", gotIDs, wantIDs)
		}
	})

	t.Run("an empty types set returns an empty slice, never an error", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		if err := repo.Create(ctx, fixtureUnit("byType-any", unit.StatusPool)); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.LiveFocusCandidatesByType(ctx, nil)
		if err != nil {
			t.Fatalf("LiveFocusCandidatesByType: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("LiveFocusCandidatesByType(nil) = %v, want empty", got)
		}
	})

	t.Run("a requested type with zero live matches returns a non-nil empty slice", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		// Unlike "an unwanted type" above (which never appears in the types
		// argument at all), TypeKnowledge is named in the request here and
		// still has no live unit anywhere in the repo — this exercises the
		// SQL branch itself (WHERE status = ? AND type IN (...) returning
		// zero rows), which the nil/empty-types case above never reaches:
		// that one short-circuits before any query runs.
		if err := repo.Create(ctx, fixtureUnit("byType-zero-archived", unit.StatusArchived)); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.LiveFocusCandidatesByType(ctx, []unit.Type{unit.TypeKnowledge})
		if err != nil {
			t.Fatalf("LiveFocusCandidatesByType: %v", err)
		}
		if got == nil {
			t.Fatal("LiveFocusCandidatesByType() = nil, want a non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("LiveFocusCandidatesByType() = %v, want empty", got)
		}
	})
}

// focusCandidateIDs extracts the id of every focus.Candidate, in order.
func focusCandidateIDs(cs []focus.Candidate) []string {
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

// browseFixtureTime is RunLiveBrowsePage's own anchor instant — fixtureUnit's
// own default CreatedAt, named here so cases that override CreatedAt with an
// offset read as offsets from one place.
var browseFixtureTime = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

// RunLiveBrowsePage runs the ports.UnitRepo.LiveBrowsePage contract against
// a fresh repository instance, built by newRepo for every subtest.
//
// Spec R1; design §3.2. LiveBrowsePage is I02's third live read surface
// (LiveByIDs, LiveFocusCandidates/ByType and now this one), bounded by
// ports.BrowsePageSize and keyset-paginated by (CreatedAt, ID) DESC.
func RunLiveBrowsePage(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo) {
	t.Helper()

	t.Run("excludes archived, superseded and incomplete units (I02)", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		pool := fixtureUnit("browse-pool", unit.StatusPool)
		archived := fixtureUnit("browse-archived", unit.StatusArchived)
		superseded := fixtureUnit("browse-superseded", unit.StatusSuperseded)
		incomplete := fixtureUnit("browse-incomplete", unit.StatusIncomplete)
		for _, u := range []unit.Unit{pool, archived, superseded, incomplete} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		page, err := repo.LiveBrowsePage(ctx, nil, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage: %v", err)
		}
		gotIDs := idsOf(page.Units)
		wantIDs := []string{pool.ID}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("LiveBrowsePage: got %v, want exactly %v (only the pool unit)", gotIDs, wantIDs)
		}
		if page.Next != nil {
			t.Errorf("Next = %+v, want nil — every live unit fit on one page", page.Next)
		}
	})

	t.Run("an empty types means all types; a non-empty set narrows", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		task := fixtureUnit("browse-type-task", unit.StatusPool)
		task.Type = unit.TypeTask
		knowledge := fixtureUnit("browse-type-knowledge", unit.StatusPool)
		knowledge.Type = unit.TypeKnowledge
		for _, u := range []unit.Unit{task, knowledge} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		all, err := repo.LiveBrowsePage(ctx, nil, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage(nil types): %v", err)
		}
		wantAll := []string{knowledge.ID, task.ID} // CreatedAt tied; id DESC breaks the tie
		gotAll := idsOf(all.Units)
		sortStrings(gotAll)
		sortStrings(wantAll)
		if !reflect.DeepEqual(gotAll, wantAll) {
			t.Fatalf("LiveBrowsePage(nil types) ids = %v, want %v (both units, unnarrowed)", gotAll, wantAll)
		}

		onlyTask, err := repo.LiveBrowsePage(ctx, []unit.Type{unit.TypeTask}, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage(types=[task]): %v", err)
		}
		if got := idsOf(onlyTask.Units); !reflect.DeepEqual(got, []string{task.ID}) {
			t.Fatalf("LiveBrowsePage(types=[task]) = %v, want exactly [%s]", got, task.ID)
		}

		onlyKnowledge, err := repo.LiveBrowsePage(ctx, []unit.Type{unit.TypeKnowledge}, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage(types=[knowledge]): %v", err)
		}
		if got := idsOf(onlyKnowledge.Units); !reflect.DeepEqual(got, []string{knowledge.ID}) {
			t.Fatalf("LiveBrowsePage(types=[knowledge]) = %v, want exactly [%s]", got, knowledge.ID)
		}
	})

	t.Run("orders created_at DESC, id DESC, ties broken by id", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		older := fixtureUnit("browse-order-older", unit.StatusPool)
		older.CreatedAt = browseFixtureTime.Add(-time.Hour)
		newer := fixtureUnit("browse-order-newer", unit.StatusPool)
		newer.CreatedAt = browseFixtureTime
		// Two units sharing the newest CreatedAt: only their id decides
		// which comes first.
		tieB := fixtureUnit("browse-order-tie-b", unit.StatusPool)
		tieB.CreatedAt = browseFixtureTime
		tieA := fixtureUnit("browse-order-tie-a", unit.StatusPool)
		tieA.CreatedAt = browseFixtureTime

		for _, u := range []unit.Unit{older, newer, tieB, tieA} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		page, err := repo.LiveBrowsePage(ctx, nil, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage: %v", err)
		}
		// newer, tieB and tieA all share browseFixtureTime; id DESC among
		// them puts "browse-order-tie-b" ahead of "browse-order-newer"
		// ahead of "browse-order-tie-a" (lexical: "tie-b" > "order-newer"
		// is false — computed below instead of asserted by hand, so this
		// case cannot drift from Go's own string comparison).
		tied := []string{newer.ID, tieB.ID, tieA.ID}
		sortStringsDesc(tied)
		want := append(append([]string{}, tied...), older.ID)

		got := idsOf(page.Units)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("LiveBrowsePage order = %v, want %v (created_at DESC, id DESC tie-break)", got, want)
		}
	})

	t.Run("a page never exceeds BrowsePageSize, and a second page never repeats a row from the first — union equals the live set", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		const total = ports.BrowsePageSize + 5
		var ids []string
		for i := range total {
			id := fmt.Sprintf("browse-bound-%03d", i)
			ids = append(ids, id)
			u := fixtureUnit(id, unit.StatusPool)
			u.CreatedAt = browseFixtureTime.Add(time.Duration(i) * time.Second)
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}

		first, err := repo.LiveBrowsePage(ctx, nil, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage page 1: %v", err)
		}
		if len(first.Units) != ports.BrowsePageSize {
			t.Fatalf("page 1 len = %d, want exactly BrowsePageSize (%d)", len(first.Units), ports.BrowsePageSize)
		}
		if first.Next == nil {
			t.Fatal("page 1 Next = nil, want a cursor — more live units remain")
		}

		second, err := repo.LiveBrowsePage(ctx, nil, first.Next)
		if err != nil {
			t.Fatalf("LiveBrowsePage page 2: %v", err)
		}
		if len(second.Units) != total-ports.BrowsePageSize {
			t.Fatalf("page 2 len = %d, want %d (the remaining live units)", len(second.Units), total-ports.BrowsePageSize)
		}
		if second.Next != nil {
			t.Errorf("page 2 Next = %+v, want nil — this is the last page", second.Next)
		}

		gotIDs := append(idsOf(first.Units), idsOf(second.Units)...)
		seen := make(map[string]bool, len(gotIDs))
		for _, id := range gotIDs {
			if seen[id] {
				t.Fatalf("id %s appears on both pages", id)
			}
			seen[id] = true
		}
		sortStrings(gotIDs)
		wantIDs := append([]string{}, ids...)
		sortStrings(wantIDs)
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("union of both pages = %v, want exactly the seeded live set %v", gotIDs, wantIDs)
		}
	})

	t.Run("exactly BrowsePageSize live units fit on one page with no Next cursor", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		for i := range ports.BrowsePageSize {
			id := fmt.Sprintf("browse-exact-%03d", i)
			u := fixtureUnit(id, unit.StatusPool)
			u.CreatedAt = browseFixtureTime.Add(time.Duration(i) * time.Second)
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}

		page, err := repo.LiveBrowsePage(ctx, nil, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage: %v", err)
		}
		if len(page.Units) != ports.BrowsePageSize {
			t.Fatalf("page len = %d, want exactly BrowsePageSize (%d)", len(page.Units), ports.BrowsePageSize)
		}
		if page.Next != nil {
			t.Errorf("Next = %+v, want nil — exactly BrowsePageSize live units fit on one page", page.Next)
		}
	})

	t.Run("a cursor resumes correctly when the last unit of one page and the first of the next share an identical CreatedAt", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		const total = ports.BrowsePageSize + 2
		tieTime := browseFixtureTime

		var allIDs []string

		// BrowsePageSize-1 units strictly newer than the tie instant —
		// together with the two tied units below, they fill exactly page
		// 1's BrowsePageSize rows, leaving the tie's second unit and the
		// one older unit for page 2. This is the case a naive
		// `created_at < ?` cursor predicate (dropping the id tie-break)
		// gets wrong: it would exclude the tied unit that belongs on page
		// 2 as well, since its created_at is not strictly less than the
		// cursor's.
		for i := range ports.BrowsePageSize - 1 {
			id := fmt.Sprintf("browse-tie-newer-%03d", i)
			allIDs = append(allIDs, id)
			u := fixtureUnit(id, unit.StatusPool)
			u.CreatedAt = tieTime.Add(time.Duration(i+1) * time.Second)
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
		}

		// The two units sharing CreatedAt, on opposite sides of the page
		// boundary: id DESC breaks the tie, and "browse-tie-z" >
		// "browse-tie-a" lexically, so tieHigh lands on page 1 (the
		// boundary's last row) and tieLow on page 2 (the boundary's first
		// row).
		tieHigh := fixtureUnit("browse-tie-z", unit.StatusPool)
		tieHigh.CreatedAt = tieTime
		tieLow := fixtureUnit("browse-tie-a", unit.StatusPool)
		tieLow.CreatedAt = tieTime
		allIDs = append(allIDs, tieHigh.ID, tieLow.ID)
		for _, u := range []unit.Unit{tieHigh, tieLow} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		older := fixtureUnit("browse-tie-older", unit.StatusPool)
		older.CreatedAt = tieTime.Add(-time.Second)
		allIDs = append(allIDs, older.ID)
		if err := repo.Create(ctx, older); err != nil {
			t.Fatalf("Create %s: %v", older.ID, err)
		}

		if len(allIDs) != total {
			t.Fatalf("fixture seeded %d ids, want %d (BrowsePageSize + 2)", len(allIDs), total)
		}

		first, err := repo.LiveBrowsePage(ctx, nil, nil)
		if err != nil {
			t.Fatalf("LiveBrowsePage page 1: %v", err)
		}
		if len(first.Units) != ports.BrowsePageSize {
			t.Fatalf("page 1 len = %d, want exactly BrowsePageSize (%d)", len(first.Units), ports.BrowsePageSize)
		}
		firstIDs := idsOf(first.Units)
		if firstIDs[len(firstIDs)-1] != tieHigh.ID {
			t.Fatalf("page 1's last row = %s, want %s (the tied unit that ranks first between the two under id DESC)",
				firstIDs[len(firstIDs)-1], tieHigh.ID)
		}
		if first.Next == nil {
			t.Fatal("page 1 Next = nil, want a cursor — more live units remain")
		}

		second, err := repo.LiveBrowsePage(ctx, nil, first.Next)
		if err != nil {
			t.Fatalf("LiveBrowsePage page 2: %v", err)
		}
		secondIDs := idsOf(second.Units)
		if len(secondIDs) == 0 || secondIDs[0] != tieLow.ID {
			t.Fatalf("page 2's first row = %v, want starting with %s (the tied unit's other half, sharing "+
				"the boundary's CreatedAt but not skipped by the cursor)", secondIDs, tieLow.ID)
		}
		if second.Next != nil {
			t.Errorf("page 2 Next = %+v, want nil — this is the last page", second.Next)
		}

		gotIDs := append(idsOf(first.Units), idsOf(second.Units)...)
		seen := make(map[string]bool, len(gotIDs))
		for _, id := range gotIDs {
			if seen[id] {
				t.Fatalf("id %s appears on both pages", id)
			}
			seen[id] = true
		}
		sortStrings(gotIDs)
		wantIDs := append([]string{}, allIDs...)
		sortStrings(wantIDs)
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("union of both pages = %v, want exactly the seeded live set %v (the tied unit must "+
				"appear exactly once, not be skipped)", gotIDs, wantIDs)
		}
	})

	t.Run("a cursor at a unit archived since the previous page still resumes", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		older := fixtureUnit("browse-resume-older", unit.StatusPool)
		older.CreatedAt = browseFixtureTime
		boundary := fixtureUnit("browse-resume-boundary", unit.StatusPool)
		boundary.CreatedAt = browseFixtureTime.Add(time.Second)
		newest := fixtureUnit("browse-resume-newest", unit.StatusPool)
		newest.CreatedAt = browseFixtureTime.Add(2 * time.Second)
		for _, u := range []unit.Unit{older, boundary, newest} {
			if err := repo.Create(ctx, u); err != nil {
				t.Fatalf("Create %s: %v", u.ID, err)
			}
		}

		// Simulate "boundary was the last row of a previously read page":
		// take its cursor, then change its status — a live-read cursor must
		// resume by position, not by re-checking the row it names still
		// exists or is still live.
		cursor := &ports.BrowseCursor{CreatedAt: boundary.CreatedAt, ID: boundary.ID}
		if err := repo.SetStatus(ctx, boundary.ID, unit.StatusPool, unit.StatusArchived, browseFixtureTime.Add(time.Hour)); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}

		page, err := repo.LiveBrowsePage(ctx, nil, cursor)
		if err != nil {
			t.Fatalf("LiveBrowsePage(after=boundary's now-stale cursor): %v", err)
		}
		if got := idsOf(page.Units); !reflect.DeepEqual(got, []string{older.ID}) {
			t.Fatalf("LiveBrowsePage resumed = %v, want exactly [%s] (older, the only live unit strictly past the cursor)", got, older.ID)
		}
	})
}

// sortStrings sorts ss ascending, in place.
func sortStrings(ss []string) { sort.Strings(ss) }

// sortStringsDesc sorts ss descending, in place.
func sortStringsDesc(ss []string) {
	sort.Sort(sort.Reverse(sort.StringSlice(ss)))
}
