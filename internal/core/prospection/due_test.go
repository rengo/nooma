package prospection

import (
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// dueZone is the user's zone in these tests: a day boundary that is not
// UTC's, so a test that compared UTC dates would fail.
var dueZone = time.FixedZone("UTC-3", -3*60*60)

func dueTask(id string, due time.Time) unit.Unit {
	return unit.Unit{ID: id, Type: unit.TypeTask, Status: unit.StatusPool, DueAt: &due}
}

// TestTasksDue_NamesATaskFromItsDueDayForDueTaskDigestDays: a task is named
// on the morning of its due day, in the user's zone, and on the mornings
// after it while it stays open, DueTaskDigestDays mornings in all; never
// before its day, and never after the bound.
func TestTasksDue_NamesATaskFromItsDueDayForDueTaskDigestDays(t *testing.T) {
	// A bare date, as capture stores "mañana": the local midnight, written in
	// UTC by the store.
	due := time.Date(2026, 10, 20, 0, 0, 0, 0, dueZone).UTC()
	units := []unit.Unit{dueTask("passport", due)}
	morning := func(day int) time.Time { return time.Date(2026, 10, day, 7, 0, 0, 0, dueZone) }

	if got := TasksDue(units, morning(19)); len(got) != 0 {
		t.Errorf("the day before = %v, want nothing", got)
	}
	// 23:59 on the day before is still the day before, though it is the 20th
	// in UTC.
	if got := TasksDue(units, time.Date(2026, 10, 19, 23, 59, 0, 0, dueZone)); len(got) != 0 {
		t.Errorf("23:59 the day before = %v, want nothing", got)
	}
	for day := 20; day < 20+DueTaskDigestDays; day++ {
		if got := TasksDue(units, morning(day)); !slices.Equal(got, []string{"passport"}) {
			t.Errorf("Oct %d = %v, want the task named", day, got)
		}
	}
	if got := TasksDue(units, morning(20+DueTaskDigestDays)); len(got) != 0 {
		t.Errorf("past the bound = %v, want nothing", got)
	}
}

// TestTasksDue_OnlyAnOpenTaskNobodyAskedToBeRemindedOf: an event, an archived
// or superseded task, a task with no due date, and a task the user asked to be
// reminded of (its own reminder speaks for it, ADR-0031) are not named.
func TestTasksDue_OnlyAnOpenTaskNobodyAskedToBeRemindedOf(t *testing.T) {
	now := time.Date(2026, 10, 20, 7, 0, 0, 0, dueZone)
	due := time.Date(2026, 10, 20, 0, 0, 0, 0, dueZone)
	open := dueTask("open", due)
	event := dueTask("event", due)
	event.Type = unit.TypeEvent
	archived := dueTask("archived", due)
	archived.Status = unit.StatusArchived
	superseded := dueTask("superseded", due)
	superseded.Status = unit.StatusSuperseded
	incomplete := dueTask("incomplete", due)
	incomplete.Status = unit.StatusIncomplete
	undated := dueTask("undated", due)
	undated.DueAt = nil
	asked := dueTask("asked", due)
	asked.RemindMe = true

	got := TasksDue([]unit.Unit{event, archived, superseded, incomplete, undated, asked, open}, now)
	if !slices.Equal(got, []string{"open"}) {
		t.Errorf("TasksDue = %v, want only the open task", got)
	}
}

// TestTasksDue_OldestDueFirst: the order is the due instant, then the id, so
// a digest names the longest-overdue task first and the same input always
// reads the same way.
func TestTasksDue_OldestDueFirst(t *testing.T) {
	now := time.Date(2026, 10, 21, 7, 0, 0, 0, dueZone)
	units := []unit.Unit{
		dueTask("b-today", time.Date(2026, 10, 21, 0, 0, 0, 0, dueZone)),
		dueTask("a-today", time.Date(2026, 10, 21, 0, 0, 0, 0, dueZone)),
		dueTask("today-at-six", time.Date(2026, 10, 21, 18, 0, 0, 0, dueZone)),
		dueTask("yesterday", time.Date(2026, 10, 20, 0, 0, 0, 0, dueZone)),
	}
	want := []string{"yesterday", "a-today", "b-today", "today-at-six"}
	if got := TasksDue(units, now); !slices.Equal(got, want) {
		t.Errorf("TasksDue = %v, want %v", got, want)
	}
}

// TestDueTaskWindow_IsWhatTasksDueReads: the read the brain makes from the
// store covers exactly the instants TasksDue can name, local midnight to local
// midnight, so a store read never drops a task TasksDue would name.
func TestDueTaskWindow_IsWhatTasksDueReads(t *testing.T) {
	now := time.Date(2026, 10, 22, 7, 0, 0, 0, dueZone)
	from, to := DueTaskWindow(now)
	wantFrom := time.Date(2026, 10, 22-(DueTaskDigestDays-1), 0, 0, 0, 0, dueZone)
	wantTo := time.Date(2026, 10, 23, 0, 0, 0, 0, dueZone)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Fatalf("DueTaskWindow = [%v, %v), want [%v, %v)", from, to, wantFrom, wantTo)
	}
	for _, at := range []time.Time{from, to.Add(-time.Second)} {
		if got := TasksDue([]unit.Unit{dueTask("in", at)}, now); len(got) != 1 {
			t.Errorf("a task due at %v, inside the window, was not named", at)
		}
	}
	for _, at := range []time.Time{from.Add(-time.Second), to} {
		if got := TasksDue([]unit.Unit{dueTask("out", at)}, now); len(got) != 0 {
			t.Errorf("a task due at %v, outside the window, was named", at)
		}
	}
}

// TestDueTaskDigestDaysIsABoundThatCarries: more than one morning, or a task
// missed on its day is never named again; fewer than a week, because every
// morning past its day is a line the user must answer to stop (ADR-0040).
func TestDueTaskDigestDaysIsABoundThatCarries(t *testing.T) {
	if DueTaskDigestDays <= 1 || DueTaskDigestDays >= 7 {
		t.Errorf("DueTaskDigestDays = %d, want 2 to 6", DueTaskDigestDays)
	}
}

// TestDueDayAhead: a capture's reply promises the digest of the task's due day
// only when that day is after the capture's own, in the capture's zone; a task
// due today or earlier gets no promise of a morning already past.
func TestDueDayAhead(t *testing.T) {
	now := time.Date(2026, 10, 19, 16, 34, 0, 0, dueZone)
	for _, c := range []struct {
		due  time.Time
		want bool
	}{
		{time.Date(2026, 10, 20, 0, 0, 0, 0, dueZone), true},
		// The 20th in UTC, still the 19th in the user's zone.
		{time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC), false},
		{time.Date(2026, 10, 19, 23, 59, 0, 0, dueZone), false},
		{time.Date(2026, 10, 18, 0, 0, 0, 0, dueZone), false},
		{time.Date(2026, 11, 2, 18, 0, 0, 0, dueZone), true},
	} {
		if got := DueDayAhead(c.due, now); got != c.want {
			t.Errorf("DueDayAhead(%v) = %v, want %v", c.due, got, c.want)
		}
	}
}

// TestDoneCloses: a done said of a listed item closes it only when it is a
// task still in the pool with no recurring reminder (ADR-0040). Every other
// kind, status, and a series, closes nothing: a done of one occurrence must
// not end every later one.
func TestDoneCloses(t *testing.T) {
	for _, typ := range unit.AllTypes() {
		for _, st := range unit.AllStatuses() {
			for _, recurring := range []bool{false, true} {
				want := typ == unit.TypeTask && st == unit.StatusPool && !recurring
				if got := DoneCloses(typ, st, recurring); got != want {
					t.Errorf("DoneCloses(%s, %s, recurring %v) = %v, want %v", typ, st, recurring, got, want)
				}
			}
		}
	}
}
