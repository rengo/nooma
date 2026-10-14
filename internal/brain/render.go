package brain

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/ports"
)

// pushQueue is the reminders a pass will push, sent once the pass has found them
// all: a retry is found after the lead that just fired, and one unit's
// reminders due together are one message.
type pushQueue struct{ rows []ports.DueTrigger }

func (q *pushQueue) add(t ports.DueTrigger) {
	if !slices.ContainsFunc(q.rows, func(r ports.DueTrigger) bool { return r.ID == t.ID }) {
		q.rows = append(q.rows, t)
	}
}

// groups splits the queue by unit, first found first; no unit, a group of one.
func (q *pushQueue) groups() [][]ports.DueTrigger {
	var out [][]ports.DueTrigger
	byUnit := map[string]int{}
	for _, t := range q.rows {
		if t.UnitID == nil {
			out = append(out, []ports.DueTrigger{t})
			continue
		}
		i, ok := byUnit[*t.UnitID]
		if !ok {
			i = len(out)
			byUnit[*t.UnitID] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], t)
	}
	for _, g := range out {
		slices.SortFunc(g, func(a, b ports.DueTrigger) int {
			if c := a.FireAt.Compare(b.FireAt); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return out
}

// flush pushes the queue, one message per unit, and counts the reminders delivered.
func (r checkRunner) flush(ctx context.Context, q *pushQueue, now time.Time) (int, error) {
	delivered := 0
	for _, g := range q.groups() {
		n, err := r.deliver(ctx, g, now)
		delivered += n
		if err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

// sentences are the fallback language's until a unit carries its own (ADR-0032).
func sentences() phrase.Set { return phrase.For(classify.Fallback()) }

// unitWords is what a reminder says: its unit's text now and when its event is,
// in the clock's zone, with no hour for a whole day (ADR-0032 point 4). ok is
// false when the unit cannot be read: the caller says what it was armed with.
func (r checkRunner) unitWords(ctx context.Context, t ports.DueTrigger, now time.Time) (text, when string, ok bool) {
	if t.UnitID == nil || r.units == nil {
		return "", "", false
	}
	u, err := r.units.ByID(ctx, *t.UnitID)
	if err != nil {
		if !errors.Is(err, ports.ErrUnitNotFound) {
			slog.Warn("a reminder's unit could not be read, so it says what it was armed with", "trigger", t.ID, "error", err)
		}
		return "", "", false
	}
	if strings.TrimSpace(u.Content) == "" {
		return "", "", false
	}
	reminder := newReminder(t, true)
	reminder.Current = currentEvent(u)
	set := sentences()
	if at, dayOnly, known := reminder.Occasion(now.Location()); known {
		if at = at.In(now.Location()); dayOnly {
			when = set.Date(at)
		} else {
			when = set.Time(at)
		}
	}
	return u.Content, when, true
}

// pushText is the one message for a unit's reminders due together. It admits
// being late only when every reminder in it is.
func (r checkRunner) pushText(ctx context.Context, group []ports.DueTrigger, now time.Time) string {
	latest := group[len(group)-1].FireAt // groups come sorted by fire time
	if text, when, ok := r.unitWords(ctx, group[0], now); ok {
		return withDelayCaveat(sentences().Remind(text, when), latest, now)
	}
	var lines []string
	for _, t := range group {
		if line := frozenText(t); !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	return withDelayCaveat(strings.Join(lines, "\n"), latest, now)
}
