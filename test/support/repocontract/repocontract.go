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
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
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
