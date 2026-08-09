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
	"math"
	"reflect"
	"testing"
	"time"

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
