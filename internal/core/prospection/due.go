package prospection

import (
	"cmp"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// DueTaskDigestDays is how many mornings the digest names an open task by its
// due date: its due day and the mornings after it, while it stays open (the
// client's ruling of 2026-10-19, "el resumen alcanza").
//
// Chosen inside a band. More than 1, or a task missed on its day is never
// named again. Fewer than a week, because each morning past its day is a line
// the user must answer to stop: a done in reply closes the task (ADR-0040),
// and the bound is what stops the line of one nobody answers.
const DueTaskDigestDays = 3

// DueTaskWindow is the span of due instants whose tasks the digest at now can
// name: from the local midnight that opens the oldest day still named, to the
// local midnight that closes today, in now's zone. The brain reads this span
// from the store; TasksDue decides.
func DueTaskWindow(now time.Time) (from, to time.Time) {
	y, m, d := now.Date()
	loc := now.Location()
	return time.Date(y, m, d-(DueTaskDigestDays-1), 0, 0, 0, 0, loc), time.Date(y, m, d+1, 0, 0, 0, 0, loc)
}

// DueDayAhead reports whether a task due at due falls on a local day after
// now's, in now's zone: the day a capture's reply may promise that morning's
// digest will name it (doc 02 §7). A task due today or earlier gets no such
// promise, since its morning may already be past.
func DueDayAhead(due, now time.Time) bool {
	_, to := DueTaskWindow(now)
	return !due.Before(to)
}

// TasksDue is what the digest at now names as due (doc 02 §7), as unit ids:
// every live task whose due date, a local calendar day in now's zone, is today
// or one of the DueTaskDigestDays-1 days before, oldest due first, then by id.
// Ids and not units, because this package never returns a unit (I04).
//
// A task the user asked to be reminded of (unit.RemindMe) is left out: its own
// reminder speaks for it (ADR-0031), and naming it in the digest too would
// tell the user the same thing twice the same morning. Every other task with
// a due date arms nothing (ADR-0029, ADR-0030), so this is the one place it
// reaches the user.
func TasksDue(units []unit.Unit, now time.Time) []string {
	from, to := DueTaskWindow(now)
	var due []unit.Unit
	for _, u := range units {
		if u.Type != unit.TypeTask || !u.Status.IsLive() || u.RemindMe || u.DueAt == nil {
			continue
		}
		if u.DueAt.Before(from) || !u.DueAt.Before(to) {
			continue
		}
		due = append(due, u)
	}
	slices.SortFunc(due, func(a, b unit.Unit) int {
		if c := a.DueAt.Compare(*b.DueAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	ids := make([]string, len(due))
	for i, u := range due {
		ids[i] = u.ID
	}
	return ids
}

// DoneCloses reports whether a done the user said of a listed item closes it
// (doc 02 §5 step 4, ADR-0040): a task still in the pool whose reminders
// recur on no rule. A series is not ended by a done of one occurrence, and no
// other kind is a thing to get done.
func DoneCloses(t unit.Type, s unit.Status, recurring bool) bool {
	return t == unit.TypeTask && s == unit.StatusPool && !recurring
}
