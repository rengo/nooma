// Package repocontract — see unitrepo.go for the package contract.
package repocontract

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// contractNow is the single instant every case in triggerrepo.go and
// timerrepo.go is expressed against. Every boundary below is an offset
// from it, never a second literal date — focus/priority.go:137-140's own
// discipline, applied to fixtures.
var contractNow = time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)

// removalMethodPrefixes restates test/conformance's own
// deniedMethodPrefixes (i03_units_never_deleted_test.go:101) — I03's
// strengthened set {Delete, Remove, Purge, Drop, Destroy}. It is restated
// rather than imported because test/conformance is a package of tests, not
// a library, so nothing but this comment pins the two lists together. That
// is the honest limit of restating it, named here rather than left to be
// discovered.
var removalMethodPrefixes = []string{"Delete", "Remove", "Purge", "Drop", "Destroy"}

// assertNoRemovalMethod fails when iface declares a method whose name
// begins with any removalMethodPrefixes member.
//
// The scan runs over reflect's own method set — NumMethod and
// Method(i).Name — and never over a hand-typed list of the methods the
// interface is expected to have. That distinction is the whole check: a
// list would only ever catch a method someone remembered to add to it,
// while a sixth DeleteByUnitID appearing on the port tomorrow is caught by
// this scan with no test edit at all.
func assertNoRemovalMethod(t *testing.T, iface reflect.Type) {
	t.Helper()

	if iface.Kind() != reflect.Interface {
		t.Fatalf("%s has kind %s, want interface", iface, iface.Kind())
	}
	if iface.NumMethod() == 0 {
		t.Fatalf("%s declares zero methods — nothing to check yet", iface)
	}

	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, prefix := range removalMethodPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Errorf("%s declares %s: no repository port gives deletion a verb (I03)", iface, name)
			}
		}
	}
}

// TriggerHarness is what a TriggerRepo implementation must offer the
// contract so the suite can put triggers in front of it.
//
// EnsureUnit exists because triggers.unit_id REFERENCES units(id)
// (migration 0001:43) and the vault opens with foreign_keys=on: over the
// real store, creating a trigger for a unit that does not exist is a
// constraint violation, while the in-memory fake has no such notion.
// Without this hook the suite would pass at L2 and be impossible to run at
// L3 — which is not a contract, it is a fake's opinion.
// repocontract.EmbeddingHarness exists for the identical reason, and this
// is its shape.
//
// ports.TimerRepo needs no such harness, and the asymmetry is the schema's
// own: timers carries no unit_id at all, because a timer is never a unit
// (I04).
type TriggerHarness interface {
	ports.TriggerRepo

	// EnsureUnit makes id a valid trigger target. The store inserts the
	// row; the fake does nothing.
	EnsureUnit(t *testing.T, id string)
}

// RunTriggerRepo runs the ports.TriggerRepo contract against a fresh
// implementation, built by newRepo for every subtest. newRepo must return
// one with no trigger already stored in it.
//
// One thing this suite cannot observe, stated rather than papered over:
// ports.TriggerRepo declares no any-status read (there is no ByID —
// UnitRepo's deliberate escape hatch has no counterpart here yet), so
// "the row is unchanged" after a refused transition is verified through
// Due plus a second refused transition, never by reading the status back.
// The status column itself is asserted directly at L3, where raw SQL is
// available (internal/store/sqlite/triggerrepo_integration_test.go).
func RunTriggerRepo(t *testing.T, newRepo func(t *testing.T) TriggerHarness) {
	t.Helper()

	t.Run("the port declares no removal method", func(t *testing.T) {
		assertNoRemovalMethod(t, reflect.TypeOf((*ports.TriggerRepo)(nil)).Elem())
	})

	t.Run("Create then Due: present at and after fire_at, absent before", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow

		createTrigger(t, repo, fixtureTrigger("trg-due", &fireAt))

		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt.Add(-time.Second)))
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt), "trg-due")
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt.Add(time.Hour)), "trg-due")
	})

	t.Run("Due returns the stored id, unit id and fire_at", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow
		want := fixtureTrigger("trg-fields", &fireAt)

		createTrigger(t, repo, want)

		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 {
			t.Fatalf("Due: got %d triggers, want 1", len(got))
		}
		if got[0].ID != want.ID {
			t.Errorf("ID: got %q, want %q", got[0].ID, want.ID)
		}
		if got[0].UnitID == nil || *got[0].UnitID != *want.UnitID {
			t.Errorf("UnitID: got %v, want %q", got[0].UnitID, *want.UnitID)
		}
		if !got[0].FireAt.Equal(fireAt) {
			t.Errorf("FireAt: got %s, want %s", got[0].FireAt, fireAt)
		}
	})

	t.Run("Create on a duplicate id returns ErrTriggerExists", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fireAt := contractNow
		trg := fixtureTrigger("trg-dup", &fireAt)

		createTrigger(t, repo, trg)
		if err := repo.Create(ctx, trg); !errors.Is(err, ports.ErrTriggerExists) {
			t.Fatalf("second Create: got %v, want ErrTriggerExists", err)
		}
	})

	t.Run("a trigger with no fire_at never appears in Due", func(t *testing.T) {
		repo := newRepo(t)

		// A pattern_based trigger legitimately carries no fire_at
		// (migration 0001:44, :50), so fire_at IS NOT NULL is part of
		// Due's predicate rather than a defensive scan guard.
		patterned := fixtureTrigger("trg-pattern", nil)
		patterned.Kind = ports.TriggerKindPatternBased
		patterned.UnitID = nil
		createTrigger(t, repo, patterned)

		assertDueTriggerIDs(t, dueTriggers(t, repo, contractNow.Add(100*365*24*time.Hour)))
	})

	t.Run("Fire moves armed to fired and the row leaves Due", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fireAt := contractNow

		createTrigger(t, repo, fixtureTrigger("trg-fire", &fireAt))
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt), "trg-fire")

		if err := repo.Fire(ctx, "trg-fire", fireAt.Add(time.Minute)); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt.Add(time.Hour)))

		if err := repo.Fire(ctx, "trg-fire", fireAt.Add(2*time.Minute)); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("second Fire: got %v, want ErrTriggerStatusConflict", err)
		}
	})

	t.Run("Expire moves armed to expired and the row leaves Due", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fireAt := contractNow

		createTrigger(t, repo, fixtureTrigger("trg-expire", &fireAt))
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt), "trg-expire")

		if err := repo.Expire(ctx, "trg-expire"); err != nil {
			t.Fatalf("Expire: %v", err)
		}
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt.Add(time.Hour)))
	})

	t.Run("Fire on an expired trigger is refused and does not resurrect it", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fireAt := contractNow

		createTrigger(t, repo, fixtureTrigger("trg-conflict", &fireAt))
		if err := repo.Expire(ctx, "trg-conflict"); err != nil {
			t.Fatalf("Expire: %v", err)
		}

		if err := repo.Fire(ctx, "trg-conflict", fireAt); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("Fire on an expired trigger: got %v, want ErrTriggerStatusConflict", err)
		}

		// Two observations of "the row is unchanged", both of them
		// indirect — see RunTriggerRepo's own doc comment: it stayed out
		// of Due, and a second Expire is still refused, so the refused
		// Fire did not move it back to armed.
		assertDueTriggerIDs(t, dueTriggers(t, repo, fireAt.Add(time.Hour)))
		if err := repo.Expire(ctx, "trg-conflict"); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("second Expire: got %v, want ErrTriggerStatusConflict", err)
		}
	})

	t.Run("Fire and Expire on an unknown id return ErrTriggerNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Fire(ctx, "does-not-exist", contractNow); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Fire: got %v, want ErrTriggerNotFound", err)
		}
		if err := repo.Expire(ctx, "does-not-exist"); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Expire: got %v, want ErrTriggerNotFound", err)
		}
	})

	t.Run("Due orders by fire_at then id", func(t *testing.T) {
		repo := newRepo(t)
		early := contractNow.Add(-time.Hour)
		late := contractNow

		// Created out of order on purpose: insertion order must not be
		// what Due happens to return.
		for _, seed := range []struct {
			id     string
			fireAt time.Time
		}{
			{"trg-b", late},
			{"trg-a", late},
			{"trg-c", early},
		} {
			at := seed.fireAt
			createTrigger(t, repo, fixtureTrigger(seed.id, &at))
		}

		assertDueTriggerIDs(t, dueTriggers(t, repo, late), "trg-c", "trg-a", "trg-b")
	})

	t.Run("Due on an empty repository returns an empty slice, not an error", func(t *testing.T) {
		repo := newRepo(t)

		got := dueTriggers(t, repo, contractNow)
		if len(got) != 0 {
			t.Fatalf("Due: got %d triggers, want 0", len(got))
		}
	})

	t.Run("interrupt_level: nil stays nil", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow

		trg := fixtureTrigger("trg-no-interrupt", &fireAt)
		trg.InterruptLevel = nil
		createTrigger(t, repo, trg)

		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 {
			t.Fatalf("Due: got %d triggers, want 1", len(got))
		}
		if got[0].InterruptLevel != nil {
			t.Fatalf("InterruptLevel: got %v, want nil", *got[0].InterruptLevel)
		}
	})

	t.Run("interrupt_level: a value round-trips through a freshly allocated pointer", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow

		level := 0.37
		trg := fixtureTrigger("trg-interrupt", &fireAt)
		trg.InterruptLevel = &level
		createTrigger(t, repo, trg)

		// The caller mutates its own variable after Create. A repository
		// that stored the pointer instead of the float would now hold
		// 0.99.
		level = 0.99

		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 {
			t.Fatalf("Due: got %d triggers, want 1", len(got))
		}
		if got[0].InterruptLevel == nil {
			t.Fatal("InterruptLevel: got nil, want 0.37")
		}
		if *got[0].InterruptLevel != 0.37 {
			t.Fatalf("InterruptLevel: got %v, want 0.37 — Create stored the caller's pointer, not its value", *got[0].InterruptLevel)
		}
		if got[0].InterruptLevel == &level {
			t.Fatal("InterruptLevel: Due handed back the caller's own pointer")
		}

		// And the pointer Due handed out is the caller's to mutate: doing
		// so must not reach back into the repository.
		*got[0].InterruptLevel = 1.0
		again := dueTriggers(t, repo, fireAt)
		if len(again) != 1 || again[0].InterruptLevel == nil || *again[0].InterruptLevel != 0.37 {
			t.Fatalf("InterruptLevel after the caller mutated Due's own result: got %v, want 0.37", again)
		}
	})

	t.Run("recurrence rule and anchor round-trip", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow

		rule := prospection.RuleYearly
		anchor := prospection.Anchor{Month: time.September, Day: 4}
		trg := fixtureTrigger("trg-recurring", &fireAt)
		trg.RecurrenceRule = &rule
		trg.RecurrenceAnchor = &anchor
		createTrigger(t, repo, trg)

		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 {
			t.Fatalf("Due: got %d triggers, want 1", len(got))
		}
		if got[0].RecurrenceRule == nil || *got[0].RecurrenceRule != prospection.RuleYearly {
			t.Errorf("RecurrenceRule: got %v, want %q", got[0].RecurrenceRule, prospection.RuleYearly)
		}
		if got[0].RecurrenceAnchor == nil || *got[0].RecurrenceAnchor != anchor {
			t.Errorf("RecurrenceAnchor: got %v, want %+v", got[0].RecurrenceAnchor, anchor)
		}
		if got[0].RecurrenceAnchor == &anchor {
			t.Error("RecurrenceAnchor: Due handed back the caller's own pointer")
		}
	})

	t.Run("a trigger with no recurrence reads back with none", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow

		createTrigger(t, repo, fixtureTrigger("trg-one-shot", &fireAt))

		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 {
			t.Fatalf("Due: got %d triggers, want 1", len(got))
		}
		if got[0].RecurrenceRule != nil {
			t.Errorf("RecurrenceRule: got %q, want nil", *got[0].RecurrenceRule)
		}
		if got[0].RecurrenceAnchor != nil {
			t.Errorf("RecurrenceAnchor: got %+v, want nil", *got[0].RecurrenceAnchor)
		}
	})
}

// fixtureTrigger is one armed, time_based trigger — the only kind m3b's
// Arm produces. fireAt is a pointer because triggers.fire_at is nullable
// and a pattern_based trigger legitimately has none.
func fixtureTrigger(id string, fireAt *time.Time) ports.Trigger {
	unitID := "unit-" + id
	level := 0.42
	return ports.Trigger{
		ID:             id,
		UnitID:         &unitID,
		Kind:           ports.TriggerKindTimeBased,
		InterruptLevel: &level,
		Payload: ports.TriggerPayload{
			ActionText: "renew the passport",
			Rationale:  "it expires in three months",
			LeadDays:   7,
		},
		FireAt:    fireAt,
		CreatedAt: contractNow.Add(-24 * time.Hour),
	}
}

// createTrigger seeds trg's unit and stores it. Every case that expects a
// successful Create goes through here: seeding is a precondition of the
// case, not part of what it proves.
func createTrigger(t *testing.T, repo TriggerHarness, trg ports.Trigger) {
	t.Helper()

	if trg.UnitID != nil {
		repo.EnsureUnit(t, *trg.UnitID)
	}
	if err := repo.Create(context.Background(), trg); err != nil {
		t.Fatalf("Create %s: %v", trg.ID, err)
	}
}

// dueTriggers calls Due and fails the test on an error, so every case above
// reads as the assertion it is making rather than as error handling.
func dueTriggers(t *testing.T, repo ports.TriggerRepo, at time.Time) []ports.DueTrigger {
	t.Helper()

	got, err := repo.Due(context.Background(), at)
	if err != nil {
		t.Fatalf("Due(%s): %v", at, err)
	}
	return got
}

// assertDueTriggerIDs fails unless got names exactly wantIDs, in that order.
func assertDueTriggerIDs(t *testing.T, got []ports.DueTrigger, wantIDs ...string) {
	t.Helper()

	ids := make([]string, 0, len(got))
	for _, d := range got {
		ids = append(ids, d.ID)
	}
	if len(ids) != len(wantIDs) {
		t.Fatalf("Due: got %v, want %v", ids, wantIDs)
	}
	for i, want := range wantIDs {
		if ids[i] != want {
			t.Fatalf("Due: got %v, want %v", ids, wantIDs)
		}
	}
}

// RunTriggerDelivery runs the delivery half of the ports.TriggerRepo
// contract — Surface, Undelivered, Delivered and Resolve.
//
// Separate from RunTriggerRepo because it needs a trigger that has already
// fired, and threading that setup through every case in the arming suite
// would make each one carry a precondition it does not use.
func RunTriggerDelivery(t *testing.T, newRepo func(t *testing.T) TriggerHarness) {
	t.Helper()

	// fire creates an armed trigger and fires it, returning its id.
	fire := func(t *testing.T, repo TriggerHarness, id string) string {
		t.Helper()
		at := contractNow
		createTrigger(t, repo, fixtureTrigger(id, &at))
		if err := repo.Fire(context.Background(), id, contractNow); err != nil {
			t.Fatalf("Fire %s: %v", id, err)
		}
		return id
	}

	t.Run("a fired trigger is undelivered until it is surfaced", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-1")

		assertDueTriggerIDs(t, undelivered(t, repo), "trg-1")
		assertDueTriggerIDs(t, delivered(t, repo))

		if err := repo.Surface(ctx, "trg-1", contractNow.Add(time.Minute)); err != nil {
			t.Fatalf("Surface: %v", err)
		}

		assertDueTriggerIDs(t, undelivered(t, repo))
		assertDueTriggerIDs(t, delivered(t, repo), "trg-1")
	})

	t.Run("Surface on an unfired trigger is refused", func(t *testing.T) {
		repo := newRepo(t)
		at := contractNow
		createTrigger(t, repo, fixtureTrigger("trg-armed", &at))

		// The trigger is armed, not fired. Surfacing it would record a
		// delivery of something that never came due.
		if err := repo.Surface(context.Background(), "trg-armed", contractNow); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("Surface on an armed trigger: got %v, want ErrTriggerStatusConflict", err)
		}
	})

	t.Run("Surface twice is refused, so two passes cannot both deliver one trigger", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-1")

		if err := repo.Surface(ctx, "trg-1", contractNow); err != nil {
			t.Fatalf("first Surface: %v", err)
		}
		if err := repo.Surface(ctx, "trg-1", contractNow); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("second Surface: got %v, want ErrTriggerStatusConflict", err)
		}
	})

	t.Run("Resolve records the answer and closes the check-in", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-1")
		if err := repo.Surface(ctx, "trg-1", contractNow); err != nil {
			t.Fatalf("Surface: %v", err)
		}

		if err := repo.Resolve(ctx, "trg-1", ports.ResolutionEngaged, contractNow.Add(time.Hour)); err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		// It is no longer an open check-in.
		assertDueTriggerIDs(t, delivered(t, repo))
	})

	t.Run("Resolve on an unsurfaced trigger is refused", func(t *testing.T) {
		repo := newRepo(t)
		fire(t, repo, "trg-1")

		// A check-in cannot resolve something the user never saw.
		if err := repo.Resolve(context.Background(), "trg-1", ports.ResolutionEngaged, contractNow); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("Resolve on an unsurfaced trigger: got %v, want ErrTriggerStatusConflict", err)
		}
	})

	t.Run("Resolve twice is refused", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-1")
		if err := repo.Surface(ctx, "trg-1", contractNow); err != nil {
			t.Fatalf("Surface: %v", err)
		}
		if err := repo.Resolve(ctx, "trg-1", ports.ResolutionEngaged, contractNow); err != nil {
			t.Fatalf("first Resolve: %v", err)
		}
		if err := repo.Resolve(ctx, "trg-1", ports.ResolutionDeclined, contractNow); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("second Resolve: got %v, want ErrTriggerStatusConflict — an answer already given is not overwritten by a later one", err)
		}
	})

	t.Run("Surface and Resolve on an unknown id return ErrTriggerNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Surface(ctx, "nope", contractNow); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Surface: got %v, want ErrTriggerNotFound", err)
		}
		if err := repo.Resolve(ctx, "nope", ports.ResolutionEngaged, contractNow); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Resolve: got %v, want ErrTriggerNotFound", err)
		}
	})

	t.Run("Delivered is most recent first", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		for i, id := range []string{"trg-a", "trg-b", "trg-c"} {
			fire(t, repo, id)
			if err := repo.Surface(ctx, id, contractNow.Add(time.Duration(i)*time.Hour)); err != nil {
				t.Fatalf("Surface %s: %v", id, err)
			}
		}

		// A caller resolving one answer takes the head, so the head must
		// be the one the user most likely just answered.
		assertDueTriggerIDs(t, delivered(t, repo), "trg-c", "trg-b", "trg-a")
	})

	t.Run("both reads are empty on a repository with nothing fired", func(t *testing.T) {
		repo := newRepo(t)
		assertDueTriggerIDs(t, undelivered(t, repo))
		assertDueTriggerIDs(t, delivered(t, repo))
	})
}

func undelivered(t *testing.T, repo ports.TriggerRepo) []ports.DueTrigger {
	t.Helper()
	got, err := repo.Undelivered(context.Background())
	if err != nil {
		t.Fatalf("Undelivered: %v", err)
	}
	return got
}

func delivered(t *testing.T, repo ports.TriggerRepo) []ports.DueTrigger {
	t.Helper()
	got, err := repo.Delivered(context.Background())
	if err != nil {
		t.Fatalf("Delivered: %v", err)
	}
	return got
}
