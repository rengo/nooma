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
	"github.com/rengo/nooma/internal/core/unit"
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
	groups := q.groups()
	q.rows = nil // sent or failed once: an abort after this never sends them again
	delivered := 0
	for _, g := range groups {
		n, err := r.deliver(ctx, g, now)
		delivered += n
		if err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

// sentences are the fallback language's: what a message says when no unit has
// told which language its person writes in (ADR-0032 point 5).
func sentences() phrase.Set { return phrase.For(classify.Fallback()) }

// setFor is the sentences a message speaks: own, when it names a language Nooma
// holds, else the newest unit's, else the fallback's (ADR-0032 point 5). A read
// that fails is logged and answered with the fallback: a message in English
// beats no message.
func (r checkRunner) setFor(ctx context.Context, own string) phrase.Set {
	if l, ok := heldLanguage(own); ok {
		return phrase.For(l)
	}
	if r.units == nil {
		return sentences()
	}
	latest, err := r.units.LatestLanguage(ctx)
	if err != nil {
		slog.Warn("the newest unit's language could not be read, so a message speaks the fallback", "error", err)
		return sentences()
	}
	if l, ok := heldLanguage(latest); ok {
		return phrase.For(l)
	}
	return sentences()
}

// heldLanguage is s as a language Nooma has sentences for.
func heldLanguage(s string) (classify.Language, bool) {
	for _, l := range classify.AllLanguages() {
		if string(l) == s {
			return l, true
		}
	}
	return "", false
}

// readUnit is the unit a reminder is about, or ok false when it has none or it
// cannot be read or says nothing a reminder could frame: the caller then says
// what the reminder was armed with.
func (r checkRunner) readUnit(ctx context.Context, t ports.DueTrigger) (u unit.Unit, ok bool) {
	if t.UnitID == nil || r.units == nil {
		return unit.Unit{}, false
	}
	u, err := r.units.ByID(ctx, *t.UnitID)
	if err != nil {
		if !errors.Is(err, ports.ErrUnitNotFound) {
			slog.Warn("a reminder's unit could not be read, so it says what it was armed with", "trigger", t.ID, "error", err)
		}
		return unit.Unit{}, false
	}
	return u, strings.TrimSpace(u.Content) != ""
}

// unitWords is what a reminder says of u: its text now and when its event is, in
// the clock's zone, with no hour for a whole day (ADR-0032 point 4), in set's
// words for the days.
func unitWords(u unit.Unit, t ports.DueTrigger, now time.Time, set phrase.Set) (text, when string) {
	reminder := newReminder(t, true)
	reminder.Current = currentEvent(u)
	if at, dayOnly, known := reminder.Occasion(now.Location()); known {
		if at = at.In(now.Location()); dayOnly {
			when = set.Date(at)
		} else {
			when = set.Time(at)
		}
	}
	return clip(u.Content), when
}

// pushText is the one message for a unit's reminders due together, in the unit's
// language. It admits being late only when every reminder in it is.
func (r checkRunner) pushText(ctx context.Context, group []ports.DueTrigger, now time.Time) string {
	latest := group[len(group)-1].FireAt // groups come sorted by fire time
	u, ok := r.readUnit(ctx, group[0])
	set := r.setFor(ctx, u.Language)
	if ok {
		text, when := unitWords(u, group[0], now, set)
		return withDelayCaveat(set.Remind(text, when), latest, now, set)
	}
	var lines []string
	for _, t := range group {
		if line := frozenText(t, set); !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	return withDelayCaveat(strings.Join(lines, "\n"), latest, now, set)
}

// reminderTextRunes bounds how much of a unit's own text one reminder says.
// units.content is unbounded (a capture may be 64 KiB) and Telegram refuses a
// message over 4096 UTF-16 units, which would fail every pass; like
// questionSnippetRunes, a rendering bound nothing branches on.
const reminderTextRunes = 500

// clip cuts s to reminderTextRunes runes with an ellipsis, never inside a rune.
func clip(s string) string {
	if runes := []rune(s); len(runes) > reminderTextRunes {
		return string(runes[:reminderTextRunes]) + "\u2026"
	}
	return s
}
