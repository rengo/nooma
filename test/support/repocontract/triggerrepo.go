// Package repocontract — see unitrepo.go for the package contract.
package repocontract

import (
	"context"
	"errors"
	"reflect"
	"slices"
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
		if !sameAnchor(got[0].RecurrenceAnchor, &anchor) {
			t.Errorf("RecurrenceAnchor: got %v, want %+v", got[0].RecurrenceAnchor, anchor)
		}
		if got[0].RecurrenceAnchor == &anchor {
			t.Error("RecurrenceAnchor: Due handed back the caller's own pointer")
		}
	})

	t.Run("a weekly anchor's Sunday survives the round trip", func(t *testing.T) {
		// Sunday specifically, because it is time.Weekday's ZERO value and
		// therefore the one a storage encoding drops by accident: a plain
		// int field with omitempty writes nothing for it, and the row reads
		// back as a weekly rule with no weekday, which falls back to yearly.
		// The user asked for every Sunday and would be reminded once a year,
		// with nothing anywhere recording the loss.
		//
		// Every other weekday would survive that bug untouched, so a test
		// picking any of them proves nothing about the one that matters.
		repo := newRepo(t)
		fireAt := contractNow

		rule := prospection.RuleWeekly
		sunday := time.Sunday
		anchor := prospection.Anchor{Month: time.September, Day: 4, Weekday: &sunday}
		trg := fixtureTrigger("trg-weekly-sunday", &fireAt)
		trg.RecurrenceRule = &rule
		trg.RecurrenceAnchor = &anchor
		createTrigger(t, repo, trg)

		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 {
			t.Fatalf("Due: got %d triggers, want 1", len(got))
		}
		back := got[0].RecurrenceAnchor
		if back == nil || back.Weekday == nil {
			t.Fatalf("RecurrenceAnchor: got %v — a stated Sunday came back unstated", back)
		}
		if *back.Weekday != time.Sunday {
			t.Errorf("Weekday: got %s, want Sunday", *back.Weekday)
		}
		if back.Weekday == &sunday {
			t.Error("Weekday: Due handed back the caller's own pointer")
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

// RunTriggerFollow runs the half of the ports.TriggerRepo contract a
// corrected date uses (I29): ArmedForUnit and Reschedule.
func RunTriggerFollow(t *testing.T, newRepo func(t *testing.T) TriggerHarness) {
	t.Helper()
	ctx := context.Background()

	// seed creates three triggers on one unit (two armed, one fired) and
	// one armed trigger on another unit.
	seed := func(t *testing.T, repo TriggerHarness) {
		t.Helper()
		late, early, fired := contractNow.Add(2*time.Hour), contractNow.Add(time.Hour), contractNow
		// trg-unclocked has no fire_at (a NULL the column allows): ArmedForUnit
		// returns DueTrigger, whose FireAt is not a pointer, so it is skipped.
		for _, trg := range []ports.Trigger{fixtureTrigger("trg-late", &late), fixtureTrigger("trg-early", &early), fixtureTrigger("trg-fired", &fired), fixtureTrigger("trg-other", &early), fixtureTrigger("trg-unclocked", nil)} {
			if trg.ID != "trg-other" {
				unitID := "unit-shared"
				trg.UnitID = &unitID
			}
			createTrigger(t, repo, trg)
		}
		if err := repo.Fire(ctx, "trg-fired", fired); err != nil {
			t.Fatalf("Fire: %v", err)
		}
	}
	armed := func(t *testing.T, repo TriggerHarness) []ports.DueTrigger {
		t.Helper()
		got, err := repo.ArmedForUnit(ctx, "unit-shared")
		if err != nil {
			t.Fatalf("ArmedForUnit: %v", err)
		}
		return got
	}

	t.Run("ArmedUnitIDs names each unit with an armed clocked trigger once, in id order", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo)
		ids := func() []string {
			t.Helper()
			got, err := repo.ArmedUnitIDs(ctx)
			if err != nil {
				t.Fatalf("ArmedUnitIDs: %v", err)
			}
			return got
		}
		// unit-shared holds two armed triggers, a fired one and an unclocked
		// one: it is named once.
		if got, want := ids(), []string{"unit-shared", "unit-trg-other"}; !reflect.DeepEqual(got, want) {
			t.Errorf("ArmedUnitIDs = %v, want %v", got, want)
		}
		unitID := "unit-a"
		at := contractNow.Add(3 * time.Hour)
		trg := fixtureTrigger("trg-a", &at)
		trg.UnitID = &unitID
		createTrigger(t, repo, trg)
		if got, want := ids(), []string{"unit-a", "unit-shared", "unit-trg-other"}; !reflect.DeepEqual(got, want) {
			t.Errorf("ArmedUnitIDs = %v, want %v", got, want)
		}
		if err := repo.Fire(ctx, "trg-a", at); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		if got, want := ids(), []string{"unit-shared", "unit-trg-other"}; !reflect.DeepEqual(got, want) {
			t.Errorf("ArmedUnitIDs after unit-a's only trigger fired = %v, want %v", got, want)
		}
	})

	t.Run("FiredForUnit returns the unit's fired one-shot triggers with what they carried", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo) // trg-fired fired at contractNow
		event := contractNow.Add(48 * time.Hour)
		at := contractNow.Add(90 * time.Minute)
		unitID := "unit-shared"
		trg := fixtureTrigger("trg-about", &at)
		trg.UnitID = &unitID
		trg.Payload.About, trg.Payload.UserLeadsMinutes = event, []int{180, 120}
		createTrigger(t, repo, trg)
		if got := armed(t, repo); len(got) != 3 || !got[1].Payload.About.Equal(event) || !reflect.DeepEqual(got[1].Payload.UserLeadsMinutes, []int{180, 120}) {
			t.Fatalf("ArmedForUnit = %+v, want trg-about carrying its event instant and user lead", got)
		}
		later := contractNow.Add(30 * time.Minute)
		if err := repo.Fire(ctx, "trg-about", later); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		got, err := repo.FiredForUnit(ctx, "unit-shared")
		if err != nil || len(got) != 2 {
			t.Fatalf("FiredForUnit = %+v, %v; want trg-fired and trg-about", got, err)
		}
		byFired := map[time.Time]ports.FiredTrigger{}
		for _, f := range got {
			byFired[f.FiredAt.UTC()] = f
		}
		if f, ok := byFired[later.UTC()]; !ok || !f.FireAt.Equal(at) || !f.Payload.About.Equal(event) || !reflect.DeepEqual(f.Payload.UserLeadsMinutes, []int{180, 120}) {
			t.Errorf("FiredForUnit lost trg-about's instants or payload: %+v", got)
		}
		if f, ok := byFired[contractNow.UTC()]; !ok || !f.Payload.About.IsZero() {
			t.Errorf("FiredForUnit lost trg-fired, or invented an event instant for it: %+v", got)
		}
		if other, _ := repo.FiredForUnit(ctx, "unit-trg-other"); len(other) != 0 {
			t.Errorf("FiredForUnit of a unit nothing fired for = %+v, want none", other)
		}
		yearlyID, yat := "unit-yearly", contractNow.Add(time.Hour)
		rule := prospection.RuleYearly
		yearly := fixtureTrigger("trg-yearly", &yat)
		yearly.UnitID, yearly.RecurrenceRule, yearly.RecurrenceAnchor = &yearlyID, &rule, &prospection.Anchor{Month: 10, Day: 1}
		createTrigger(t, repo, yearly)
		if err := repo.Fire(ctx, "trg-yearly", yat); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		if fired, _ := repo.FiredForUnit(ctx, "unit-yearly"); len(fired) != 0 {
			t.Errorf("FiredForUnit counted a recurring trigger: %+v", fired)
		}
	})

	t.Run("FiredForUnit_LapsedRowDoesNotCover", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo) // trg-fired fired at contractNow; trg-late, trg-early armed, all on unit-shared
		at := contractNow.Add(time.Hour)
		unitID := "unit-shared"
		lapsing := fixtureTrigger("trg-lapsing", &at)
		lapsing.UnitID = &unitID
		createTrigger(t, repo, lapsing)
		if err := repo.Fire(ctx, "trg-lapsing", at); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		if got, err := repo.FiredForUnit(ctx, "unit-shared"); err != nil || len(got) != 2 {
			t.Fatalf("FiredForUnit before the lapse = %+v, %v; want trg-fired and trg-lapsing", got, err)
		}
		if err := repo.Lapse(ctx, "trg-lapsing"); err != nil {
			t.Fatalf("Lapse: %v", err)
		}
		got, err := repo.FiredForUnit(ctx, "unit-shared")
		if err != nil || len(got) != 1 || !got[0].FiredAt.Equal(contractNow) {
			t.Errorf("FiredForUnit after the lapse = %+v, %v; want only trg-fired: a lapsed row was never sent and covers nothing", got, err)
		}
	})

	t.Run("OneShotForUnit returns the unit's one-shot triggers at every status, expired included, with the catch-up mark", func(t *testing.T) {
		repo := newRepo(t)
		unitID := "unit-oneshot"
		at := contractNow.Add(time.Hour)
		yearly := prospection.RuleYearly
		for _, id := range []string{"trg-armed", "trg-fired", "trg-expired", "trg-lapsed", "trg-recurring"} {
			trg := fixtureTrigger(id, &at)
			trg.UnitID = &unitID
			if id == "trg-recurring" {
				trg.RecurrenceRule, trg.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: 10, Day: 1}
			}
			if id == "trg-lapsed" {
				trg.Payload.CatchUp = true
			}
			createTrigger(t, repo, trg)
		}
		for _, id := range []string{"trg-fired", "trg-lapsed"} {
			if err := repo.Fire(ctx, id, at); err != nil {
				t.Fatalf("Fire %s: %v", id, err)
			}
		}
		if err := repo.Lapse(ctx, "trg-lapsed"); err != nil {
			t.Fatalf("Lapse: %v", err)
		}
		if err := repo.Expire(ctx, "trg-expired"); err != nil {
			t.Fatalf("Expire: %v", err)
		}

		got, err := repo.OneShotForUnit(ctx, unitID)
		if err != nil {
			t.Fatalf("OneShotForUnit: %v", err)
		}
		ids, marked := []string{}, false
		for _, g := range got {
			ids = append(ids, g.ID)
			if g.ID == "trg-lapsed" {
				marked = g.Payload.CatchUp
			}
		}
		slices.Sort(ids)
		if want := []string{"trg-armed", "trg-expired", "trg-fired", "trg-lapsed"}; !slices.Equal(ids, want) || !marked {
			t.Errorf("OneShotForUnit = %v (catch-up mark %v), want %v with the mark kept", ids, marked, want)
		}
		if other, _ := repo.OneShotForUnit(ctx, "unit-nothing"); len(other) != 0 {
			t.Errorf("OneShotForUnit of a unit with none = %+v", other)
		}
	})

	t.Run("Reschedule writes the event instant and user lead", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo)
		event := contractNow.Add(24 * time.Hour)
		if err := repo.Reschedule(ctx, "trg-early", ports.TriggerMove{FireAt: contractNow.Add(2 * time.Hour),
			Payload: ports.TriggerPayload{ActionText: "x", About: event, UserLeadsMinutes: []int{60}}}); err != nil {
			t.Fatalf("Reschedule: %v", err)
		}
		if got := armed(t, repo); !got[0].Payload.About.Equal(event) || !reflect.DeepEqual(got[0].Payload.UserLeadsMinutes, []int{60}) {
			t.Errorf("after Reschedule = %+v, want the event instant and user lead written", got[0])
		}
	})

	t.Run("ArmedForUnit returns the unit's armed triggers by fire_at", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo)
		got := armed(t, repo)
		assertDueTriggerIDs(t, got, "trg-early", "trg-late")
		if got[0].Payload.ActionText != "renew the passport" || got[0].InterruptLevel == nil {
			t.Errorf("ArmedForUnit row = %+v, want the stored payload and level", got[0])
		}
	})

	t.Run("Reschedule moves fire_at and payload and keeps the level", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo)
		to := contractNow.Add(5 * time.Hour)
		payload := ports.TriggerPayload{ActionText: "renew it today", Rationale: "armed for today", LeadDays: 3}
		if err := repo.Reschedule(ctx, "trg-early", ports.TriggerMove{FireAt: to, Payload: payload}); err != nil {
			t.Fatalf("Reschedule: %v", err)
		}
		got := armed(t, repo)
		assertDueTriggerIDs(t, got, "trg-late", "trg-early")
		moved := got[1]
		if !moved.FireAt.Equal(to) || !reflect.DeepEqual(moved.Payload, payload) {
			t.Errorf("moved = %s %+v, want %s %+v", moved.FireAt, moved.Payload, to, payload)
		}
		if moved.InterruptLevel == nil || *moved.InterruptLevel != 0.42 {
			t.Errorf("moved = %+v, want the level as armed", moved)
		}
		if moved.RecurrenceAnchor != nil {
			t.Errorf("RecurrenceAnchor = %+v, want nil — a nil move anchor writes none", moved.RecurrenceAnchor)
		}
	})

	t.Run("Reschedule replaces a recurring anchor and keeps it when nil", func(t *testing.T) {
		repo := newRepo(t)
		fireAt := contractNow
		rule := prospection.RuleYearly
		trg := fixtureTrigger("trg-rec", &fireAt)
		trg.RecurrenceRule = &rule
		trg.RecurrenceAnchor = &prospection.Anchor{Month: time.March, Day: 1}
		createTrigger(t, repo, trg)
		next := &prospection.Anchor{Month: time.April, Day: 2}
		if err := repo.Reschedule(ctx, "trg-rec", ports.TriggerMove{FireAt: fireAt, RecurrenceAnchor: next}); err != nil {
			t.Fatalf("Reschedule: %v", err)
		}
		if err := repo.Reschedule(ctx, "trg-rec", ports.TriggerMove{FireAt: fireAt}); err != nil {
			t.Fatalf("Reschedule: %v", err)
		}
		got := dueTriggers(t, repo, fireAt)
		if len(got) != 1 || !sameAnchor(got[0].RecurrenceAnchor, next) || got[0].RecurrenceRule == nil {
			t.Errorf("recurring = %+v, want anchor %+v and its rule kept", got, next)
		}
	})

	t.Run("Reschedule refuses a trigger that is not armed, or does not exist", func(t *testing.T) {
		repo := newRepo(t)
		seed(t, repo)
		if err := repo.Reschedule(ctx, "trg-fired", ports.TriggerMove{FireAt: contractNow}); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Errorf("Reschedule(fired) = %v, want ErrTriggerStatusConflict", err)
		}
		if err := repo.Reschedule(ctx, "nope", ports.TriggerMove{FireAt: contractNow}); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Reschedule(unknown) = %v, want ErrTriggerNotFound", err)
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

	t.Run("Lapse retires a fired, unsurfaced trigger: it leaves Undelivered and can no longer be surfaced", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-1")

		if err := repo.Lapse(ctx, "trg-1"); err != nil {
			t.Fatalf("Lapse: %v", err)
		}
		assertDueTriggerIDs(t, undelivered(t, repo))
		assertDueTriggerIDs(t, delivered(t, repo))
		if err := repo.Surface(ctx, "trg-1", contractNow); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("Surface after Lapse: got %v, want ErrTriggerStatusConflict: a lapsed reminder was never delivered", err)
		}
		if err := repo.Lapse(ctx, "trg-1"); !errors.Is(err, ports.ErrTriggerStatusConflict) {
			t.Fatalf("second Lapse: got %v, want ErrTriggerStatusConflict", err)
		}
	})

	t.Run("Lapse refuses an armed and a surfaced trigger, and an unknown id", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		at := contractNow
		createTrigger(t, repo, fixtureTrigger("trg-armed", &at))
		fire(t, repo, "trg-sent")
		if err := repo.Surface(ctx, "trg-sent", contractNow); err != nil {
			t.Fatalf("Surface: %v", err)
		}

		for _, id := range []string{"trg-armed", "trg-sent"} {
			if err := repo.Lapse(ctx, id); !errors.Is(err, ports.ErrTriggerStatusConflict) {
				t.Errorf("Lapse(%s): got %v, want ErrTriggerStatusConflict", id, err)
			}
		}
		if err := repo.Lapse(ctx, "trg-missing"); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Lapse of an unknown id: got %v, want ErrTriggerNotFound", err)
		}
		assertDueTriggerIDs(t, dueTriggers(t, repo, contractNow.Add(time.Hour)), "trg-armed")
		assertDueTriggerIDs(t, delivered(t, repo), "trg-sent")
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

	t.Run("Reask restamps an open check-in, which becomes the newest", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		for i, id := range []string{"trg-a", "trg-b"} {
			fire(t, repo, id)
			if err := repo.Surface(ctx, id, contractNow.Add(time.Duration(i)*time.Hour)); err != nil {
				t.Fatalf("Surface %s: %v", id, err)
			}
		}

		asked := contractNow.Add(2 * time.Hour)
		if err := repo.Reask(ctx, "trg-a", asked); err != nil {
			t.Fatalf("Reask: %v", err)
		}
		// An answer to the re-ask takes the head, so the head must be it.
		assertDueTriggerIDs(t, delivered(t, repo), "trg-a", "trg-b")
		got, err := repo.Delivered(ctx)
		if err != nil {
			t.Fatalf("Delivered: %v", err)
		}
		if !got[0].SurfacedAt.Equal(asked) {
			t.Errorf("SurfacedAt = %s, want the re-ask's %s", got[0].SurfacedAt, asked)
		}
	})

	t.Run("Reask refuses an unsurfaced and an answered trigger, and an unknown id", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-unsent")
		fire(t, repo, "trg-closed")
		if err := repo.Surface(ctx, "trg-closed", contractNow); err != nil {
			t.Fatalf("Surface: %v", err)
		}
		if err := repo.ExpireUnanswered(ctx, "trg-closed"); err != nil {
			t.Fatalf("ExpireUnanswered: %v", err)
		}
		assertDueTriggerIDs(t, delivered(t, repo))

		for _, id := range []string{"trg-unsent", "trg-closed"} {
			if err := repo.Reask(ctx, id, contractNow); !errors.Is(err, ports.ErrTriggerStatusConflict) {
				t.Errorf("Reask(%s): got %v, want ErrTriggerStatusConflict", id, err)
			}
		}
		if err := repo.Reask(ctx, "trg-missing", contractNow); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("Reask of an unknown id: got %v, want ErrTriggerNotFound", err)
		}
	})

	t.Run("ExpireUnanswered closes an open check-in that still covers its lead", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-1")
		if err := repo.Surface(ctx, "trg-1", contractNow); err != nil {
			t.Fatalf("Surface: %v", err)
		}
		if err := repo.ExpireUnanswered(ctx, "trg-1"); err != nil {
			t.Fatalf("ExpireUnanswered: %v", err)
		}
		assertDueTriggerIDs(t, delivered(t, repo))
		assertDueTriggerIDs(t, undelivered(t, repo))
		// It went out: a re-arm must not send its lead again.
		sent, err := repo.FiredForUnit(ctx, "unit-trg-1")
		if err != nil {
			t.Fatalf("FiredForUnit: %v", err)
		}
		if len(sent) != 1 || sent[0].ID != "trg-1" {
			t.Errorf("FiredForUnit = %+v, want the expired check-in", sent)
		}
		// An answer arriving later finds nothing open to reach.
		if err := repo.Resolve(ctx, "trg-1", ports.ResolutionEngaged, contractNow); err == nil {
			t.Error("Resolve after ExpireUnanswered succeeded; the check-in is closed")
		}
	})

	t.Run("ExpireUnanswered refuses an unsurfaced and an answered trigger, and an unknown id", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fire(t, repo, "trg-unsent")
		fire(t, repo, "trg-answered")
		if err := repo.Surface(ctx, "trg-answered", contractNow); err != nil {
			t.Fatalf("Surface: %v", err)
		}
		if err := repo.Resolve(ctx, "trg-answered", ports.ResolutionEngaged, contractNow); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		for _, id := range []string{"trg-unsent", "trg-answered"} {
			if err := repo.ExpireUnanswered(ctx, id); !errors.Is(err, ports.ErrTriggerStatusConflict) {
				t.Errorf("ExpireUnanswered(%s): got %v, want ErrTriggerStatusConflict", id, err)
			}
		}
		if err := repo.ExpireUnanswered(ctx, "trg-missing"); !errors.Is(err, ports.ErrTriggerNotFound) {
			t.Errorf("ExpireUnanswered of an unknown id: got %v, want ErrTriggerNotFound", err)
		}
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
	out := make([]ports.DueTrigger, 0, len(got))
	for _, c := range got {
		out = append(out, c.DueTrigger)
	}
	return out
}

// sameAnchor compares two anchors by value.
//
// prospection.Anchor carries a *time.Weekday, so Go's own == compares that
// field by ADDRESS: two anchors naming the same weekday through different
// pointers are unequal, and two naming different weekdays through the same
// pointer are equal. A round-trip assertion written with == therefore says
// nothing about the weekday it is meant to be checking, and would have kept
// passing while the value was mangled.
func sameAnchor(got, want *prospection.Anchor) bool {
	if got == nil || want == nil {
		return got == want
	}
	if got.Month != want.Month || got.Day != want.Day {
		return false
	}
	switch {
	case got.Weekday == nil && want.Weekday == nil:
		return true
	case got.Weekday == nil || want.Weekday == nil:
		return false
	default:
		return *got.Weekday == *want.Weekday
	}
}
