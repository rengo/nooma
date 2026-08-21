//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestTimerRepo_Contract runs the same repocontract.RunTimerRepo suite PR 1
// ran against the in-memory fake at L2, now against a real temporary SQLite
// vault at L3.
func TestTimerRepo_Contract(t *testing.T) {
	repocontract.RunTimerRepo(t, func(t *testing.T) ports.TimerRepo {
		return NewTimerRepo(openTestVault(t))
	})
}

// TestTimerRepo_FireWritesSurfacedAtAndLeavesRenderedTextNull is the
// timer's half of the direct status assertion, and it is deliberately not
// symmetric with the trigger's.
//
// A timer's firing IS its delivery — there is no rendering step for it to
// wait on — so Fire writes surfaced_at, where a trigger's Fire leaves it
// NULL. rendered_text stays NULL because it is untouched, not because it
// is defaulted: m3d's fire-time rephrasing writes it when it has a caller,
// and a repository that wrote "" here would have made that column's
// meaning unrecoverable.
func TestTimerRepo_FireWritesSurfacedAtAndLeavesRenderedTextNull(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTimerRepo(v)

	if err := repo.Create(ctx, fixtureStoreTimer("tmr-fired")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	firedAt := triggerFixtureTime.Add(30 * time.Second)
	if err := repo.Fire(ctx, "tmr-fired", firedAt); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	var (
		status       string
		surfacedAt   sql.NullString
		renderedText sql.NullString
	)
	if err := v.db.QueryRowContext(ctx,
		`SELECT status, surfaced_at, rendered_text FROM timers WHERE id = ?`, "tmr-fired").
		Scan(&status, &surfacedAt, &renderedText); err != nil {
		t.Fatalf("select timer: %v", err)
	}

	if status != string(ports.TimerStatusFired) {
		t.Errorf("status: got %q, want %q", status, ports.TimerStatusFired)
	}
	if !surfacedAt.Valid {
		t.Error("surfaced_at: got NULL, want the instant Fire was given — a timer's firing is its delivery")
	} else if want := firedAt.UTC().Format(unitTimeLayout); surfacedAt.String != want {
		t.Errorf("surfaced_at: got %q, want %q", surfacedAt.String, want)
	}
	if renderedText.Valid {
		t.Errorf("rendered_text: got %q, want NULL — untouched, not defaulted", renderedText.String)
	}
}

// TestTimerRepo_CancelWritesOnlyTheStatus proves Cancel invents no
// timestamp — timers carries no cancelled_at column.
func TestTimerRepo_CancelWritesOnlyTheStatus(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTimerRepo(v)

	if err := repo.Create(ctx, fixtureStoreTimer("tmr-cancelled")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Cancel(ctx, "tmr-cancelled"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	var (
		status                        string
		firedAt, surfacedAt, rendered sql.NullString
	)
	if err := v.db.QueryRowContext(ctx,
		`SELECT status, fired_at, surfaced_at, rendered_text FROM timers WHERE id = ?`, "tmr-cancelled").
		Scan(&status, &firedAt, &surfacedAt, &rendered); err != nil {
		t.Fatalf("select timer: %v", err)
	}

	if status != string(ports.TimerStatusCancelled) {
		t.Errorf("status: got %q, want %q", status, ports.TimerStatusCancelled)
	}
	for _, col := range []struct {
		name string
		got  sql.NullString
	}{
		{"fired_at", firedAt},
		{"surfaced_at", surfacedAt},
		{"rendered_text", rendered},
	} {
		if col.got.Valid {
			t.Errorf("%s: got %q, want NULL — Cancel writes the status and nothing else", col.name, col.got.String)
		}
	}
}

// TestTimerRepo_CreateWritesThePendingStatusItself — see
// TestTriggerRepo_CreateWritesTheArmedStatusItself for why the column
// default is not allowed to be the thing under test.
func TestTimerRepo_CreateWritesThePendingStatusItself(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()

	if err := NewTimerRepo(v).Create(ctx, fixtureStoreTimer("tmr-pending")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var status string
	if err := v.db.QueryRowContext(ctx,
		`SELECT status FROM timers WHERE id = ?`, "tmr-pending").Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != string(ports.TimerStatusPending) {
		t.Fatalf("status: got %q, want %q", status, ports.TimerStatusPending)
	}
}

// TestTimerRepo_GenericNudgeStoresNullActionText pins nil ActionText to SQL
// NULL rather than the empty string: migration 0001:65 gives NULL a
// meaning ("generic nudge") that "" does not carry.
func TestTimerRepo_GenericNudgeStoresNullActionText(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()

	tmr := fixtureStoreTimer("tmr-generic")
	tmr.ActionText = nil
	if err := NewTimerRepo(v).Create(ctx, tmr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var actionText sql.NullString
	if err := v.db.QueryRowContext(ctx,
		`SELECT action_text FROM timers WHERE id = ?`, "tmr-generic").Scan(&actionText); err != nil {
		t.Fatalf("select action_text: %v", err)
	}
	if actionText.Valid {
		t.Fatalf("action_text: got %q, want NULL", actionText.String)
	}
}

// fixtureStoreTimer is one pending timer due at triggerFixtureTime.
func fixtureStoreTimer(id string) ports.Timer {
	actionText := "take the bread out of the oven"
	return ports.Timer{
		ID:         id,
		FireAt:     triggerFixtureTime,
		ActionText: &actionText,
		CreatedAt:  triggerFixtureTime.Add(-time.Hour),
	}
}
