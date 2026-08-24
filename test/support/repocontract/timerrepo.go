// Package repocontract — see unitrepo.go for the package contract.
package repocontract

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// RunTimerRepo runs the ports.TimerRepo contract against a fresh
// repository instance, built by newRepo for every subtest. newRepo must
// return a repository with no timer already stored in it.
//
// It mirrors RunTriggerRepo case for case where the two ports agree, and
// diverges only where the tables do: timers.fire_at is NOT NULL
// (migration 0001:63), so there is no "a timer with no fire_at" case, and
// timers has no unit_id — I04's whole point is that a timer is never a
// unit. RunTriggerRepo's doc comment on what this suite cannot observe
// applies here unchanged: TimerRepo declares no any-status read either.
func RunTimerRepo(t *testing.T, newRepo func(t *testing.T) ports.TimerRepo) {
	t.Helper()

	t.Run("the port declares no removal method", func(t *testing.T) {
		assertNoRemovalMethod(t, reflect.TypeOf((*ports.TimerRepo)(nil)).Elem())
	})

	t.Run("Create then Due: present at and after fire_at, absent before", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Create(ctx, fixtureTimer("tmr-due", contractNow)); err != nil {
			t.Fatalf("Create: %v", err)
		}

		assertDueTimerIDs(t, dueTimers(t, repo, contractNow.Add(-time.Second)))
		assertDueTimerIDs(t, dueTimers(t, repo, contractNow), "tmr-due")
		assertDueTimerIDs(t, dueTimers(t, repo, contractNow.Add(time.Hour)), "tmr-due")
	})

	t.Run("Due returns the stored id, fire_at and action text", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		want := fixtureTimer("tmr-fields", contractNow)

		if err := repo.Create(ctx, want); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got := dueTimers(t, repo, contractNow)
		if len(got) != 1 {
			t.Fatalf("Due: got %d timers, want 1", len(got))
		}
		if got[0].ID != want.ID {
			t.Errorf("ID: got %q, want %q", got[0].ID, want.ID)
		}
		if !got[0].FireAt.Equal(want.FireAt) {
			t.Errorf("FireAt: got %s, want %s", got[0].FireAt, want.FireAt)
		}
		if got[0].ActionText == nil || *got[0].ActionText != *want.ActionText {
			t.Errorf("ActionText: got %v, want %q", got[0].ActionText, *want.ActionText)
		}
	})

	t.Run("Create on a duplicate id returns ErrTimerExists", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		tmr := fixtureTimer("tmr-dup", contractNow)

		if err := repo.Create(ctx, tmr); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		if err := repo.Create(ctx, tmr); !errors.Is(err, ports.ErrTimerExists) {
			t.Fatalf("second Create: got %v, want ErrTimerExists", err)
		}
	})

	t.Run("Fire moves pending to fired and the row leaves Due", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Create(ctx, fixtureTimer("tmr-fire", contractNow)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		assertDueTimerIDs(t, dueTimers(t, repo, contractNow), "tmr-fire")

		if err := repo.Fire(ctx, "tmr-fire", contractNow.Add(time.Minute), nil); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		assertDueTimerIDs(t, dueTimers(t, repo, contractNow.Add(time.Hour)))

		if err := repo.Fire(ctx, "tmr-fire", contractNow.Add(2*time.Minute), nil); !errors.Is(err, ports.ErrTimerStatusConflict) {
			t.Fatalf("second Fire: got %v, want ErrTimerStatusConflict", err)
		}
	})

	t.Run("Cancel moves pending to cancelled and the row leaves Due", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Create(ctx, fixtureTimer("tmr-cancel", contractNow)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		assertDueTimerIDs(t, dueTimers(t, repo, contractNow), "tmr-cancel")

		if err := repo.Cancel(ctx, "tmr-cancel"); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		assertDueTimerIDs(t, dueTimers(t, repo, contractNow.Add(time.Hour)))
	})

	t.Run("Fire on a cancelled timer is refused and does not resurrect it", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Create(ctx, fixtureTimer("tmr-conflict", contractNow)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.Cancel(ctx, "tmr-conflict"); err != nil {
			t.Fatalf("Cancel: %v", err)
		}

		if err := repo.Fire(ctx, "tmr-conflict", contractNow, nil); !errors.Is(err, ports.ErrTimerStatusConflict) {
			t.Fatalf("Fire on a cancelled timer: got %v, want ErrTimerStatusConflict", err)
		}

		assertDueTimerIDs(t, dueTimers(t, repo, contractNow.Add(time.Hour)))
		if err := repo.Cancel(ctx, "tmr-conflict"); !errors.Is(err, ports.ErrTimerStatusConflict) {
			t.Fatalf("second Cancel: got %v, want ErrTimerStatusConflict", err)
		}
	})

	t.Run("Fire and Cancel on an unknown id return ErrTimerNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if err := repo.Fire(ctx, "does-not-exist", contractNow, nil); !errors.Is(err, ports.ErrTimerNotFound) {
			t.Errorf("Fire: got %v, want ErrTimerNotFound", err)
		}
		if err := repo.Cancel(ctx, "does-not-exist"); !errors.Is(err, ports.ErrTimerNotFound) {
			t.Errorf("Cancel: got %v, want ErrTimerNotFound", err)
		}
	})

	t.Run("Due orders by fire_at then id", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		early := contractNow.Add(-time.Hour)

		for _, seed := range []struct {
			id     string
			fireAt time.Time
		}{
			{"tmr-b", contractNow},
			{"tmr-a", contractNow},
			{"tmr-c", early},
		} {
			if err := repo.Create(ctx, fixtureTimer(seed.id, seed.fireAt)); err != nil {
				t.Fatalf("Create %s: %v", seed.id, err)
			}
		}

		assertDueTimerIDs(t, dueTimers(t, repo, contractNow), "tmr-c", "tmr-a", "tmr-b")
	})

	t.Run("Due on an empty repository returns an empty slice, not an error", func(t *testing.T) {
		repo := newRepo(t)

		got := dueTimers(t, repo, contractNow)
		if len(got) != 0 {
			t.Fatalf("Due: got %d timers, want 0", len(got))
		}
	})

	t.Run("action text: NULL stays NULL — the generic nudge", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		// migration 0001:65: NULL action_text means a generic nudge, so
		// an empty string is a different value, not the same one.
		tmr := fixtureTimer("tmr-generic", contractNow)
		tmr.ActionText = nil
		if err := repo.Create(ctx, tmr); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got := dueTimers(t, repo, contractNow)
		if len(got) != 1 {
			t.Fatalf("Due: got %d timers, want 1", len(got))
		}
		if got[0].ActionText != nil {
			t.Fatalf("ActionText: got %q, want nil", *got[0].ActionText)
		}
	})

	t.Run("action text round-trips through a freshly allocated pointer", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		text := "take the bread out of the oven"
		tmr := fixtureTimer("tmr-fresh", contractNow)
		tmr.ActionText = &text
		if err := repo.Create(ctx, tmr); err != nil {
			t.Fatalf("Create: %v", err)
		}

		text = "mutated by the caller after Create"

		got := dueTimers(t, repo, contractNow)
		if len(got) != 1 {
			t.Fatalf("Due: got %d timers, want 1", len(got))
		}
		if got[0].ActionText == nil {
			t.Fatal("ActionText: got nil, want the stored text")
		}
		if *got[0].ActionText != "take the bread out of the oven" {
			t.Fatalf("ActionText: got %q — Create stored the caller's pointer, not its value", *got[0].ActionText)
		}
		if got[0].ActionText == &text {
			t.Fatal("ActionText: Due handed back the caller's own pointer")
		}
	})
}

// fixtureTimer is one pending timer. timers carries no unit_id and never
// becomes a unit (I04) — nothing in this fixture references one.
func fixtureTimer(id string, fireAt time.Time) ports.Timer {
	actionText := "stir the risotto"
	return ports.Timer{
		ID:         id,
		FireAt:     fireAt,
		ActionText: &actionText,
		CreatedAt:  contractNow.Add(-time.Hour),
	}
}

// dueTimers calls Due and fails the test on an error.
func dueTimers(t *testing.T, repo ports.TimerRepo, at time.Time) []ports.DueTimer {
	t.Helper()

	got, err := repo.Due(context.Background(), at)
	if err != nil {
		t.Fatalf("Due(%s): %v", at, err)
	}
	return got
}

// assertDueTimerIDs fails unless got names exactly wantIDs, in that order.
func assertDueTimerIDs(t *testing.T, got []ports.DueTimer, wantIDs ...string) {
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
