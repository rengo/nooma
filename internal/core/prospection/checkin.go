package prospection

import (
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// MaxCheckinReasks is how many times the morning digest asks a check-in the
// user put off again before it closes by itself (doc 02 §7, the client's
// ruling of 2026-10-16). It is also a check-in's lifetime when nobody
// answers it: one left unanswered closes at the morning one put off the
// evening it reached the user would close at, the MaxCheckinReasks + 1st
// morning after it last reached the user.
const MaxCheckinReasks = 2

// MaxReasksPerDigest is how many check-ins one morning digest asks again at
// most, the longest waiting first; the rest wait, uncounted, for a later
// morning. It bounds the block so a digest always fits one message, and keeps
// it a short list after the day's items.
const MaxReasksPerDigest = 3

// CheckinStep is what the digest does with one open check-in: a delivered
// trigger nobody has answered.
type CheckinStep string

const (
	// CheckinWait leaves it open and unsaid.
	CheckinWait CheckinStep = "wait"
	// CheckinReask asks it again in this digest.
	CheckinReask CheckinStep = "reask"
	// CheckinExpire closes it unanswered: an answer can no longer reach it.
	CheckinExpire CheckinStep = "expire"
)

// OpenCheckin is an open check-in as the digest sees it.
type OpenCheckin struct {
	// SurfacedAt is the last time it reached the user: its delivery, or the
	// digest that last asked it again (Reask restamps it). Its mornings are
	// counted from here.
	SurfacedAt time.Time
	// UnitType is its unit's kind, empty for a nudge with no unit.
	UnitType unit.Type
	// UnitStatus is its unit's status now.
	UnitStatus unit.Status
	// SnoozedAt is when the user last put it off ("not yet"); zero if never.
	SnoozedAt time.Time
	// Reasks is how many digests have already asked it again.
	Reasks int
}

// CheckinStep decides c at now, a pass in which a digest is due. A morning is
// a later local calendar day, in now's zone, whose digest is due by now: the
// client's "next morning's digest", so a check-in pushed or put off on a day is
// first asked the day after, even when that day's digest was still to come. A
// morning counts whether or not a digest went out on it.
//
// A check-in is asked again only while its unit is still something to do
// (stillToDo), re-checked at every re-ask: one put off is asked the morning
// after the snooze, and once asked it is asked every later morning, put off
// again or left unanswered alike, until MaxCheckinReasks; the morning after
// the last closes it. Any other closes at the MaxCheckinReasks + 1st morning
// after it last reached the user.
func (q QuietHours) CheckinStep(c OpenCheckin, now time.Time) CheckinStep {
	mornings := q.morningsAfter(c.SurfacedAt, now, MaxCheckinReasks+1)
	switch {
	case mornings == 0:
		return CheckinWait
	case c.Reasks >= MaxCheckinReasks:
		return CheckinExpire
	case stillToDo(c.UnitType, c.UnitStatus) &&
		(c.Reasks > 0 || (!c.SnoozedAt.IsZero() && q.morningsAfter(c.SnoozedAt, now, 1) > 0)):
		return CheckinReask
	case mornings > MaxCheckinReasks:
		return CheckinExpire
	}
	return CheckinWait
}

// stillToDo reports whether a unit of kind t and status s is still something
// to do, so a check-in about it can be asked again: a task or a mental load,
// an open loop, still live. An event, and every kind that is memory rather
// than work, is not: asked the morning after, an event is usually past, and a
// reminder never speaks of the past (ADR-0032). Archived and superseded units
// are not live. One case per kind, so a kind added later is decided here.
func stillToDo(t unit.Type, s unit.Status) bool {
	if s != unit.StatusPool && s != unit.StatusIncomplete {
		return false
	}
	switch t {
	case unit.TypeTask, unit.TypeMentalLoad:
		return true
	case unit.TypeEvent, unit.TypeKnowledge, unit.TypeProcedural, unit.TypeEmotional,
		unit.TypeList, unit.TypeStructuredRef, unit.TypeInsight:
		return false
	}
	return false
}

// morningsAfter counts the local days after since's, up to now's, whose digest
// is due by now, in now's zone, stopping at limit: an old check-in costs no
// more than a new one.
func (q QuietHours) morningsAfter(since, now time.Time, limit int) int {
	loc := now.Location()
	sy, sm, sd := since.In(loc).Date()
	first := time.Date(sy, sm, sd+1, 0, 0, 0, 0, loc)
	n := 0
	for back := 0; n < limit; back++ {
		y, m, d := now.Date()
		day := time.Date(y, m, d-back, 12, 0, 0, 0, loc)
		if day.Before(first) {
			break
		}
		if !q.digestAt(day).After(now) {
			n++
		}
	}
	return n
}

// digestAt is when the digest of t's local date is due: the later of
// DigestHour and q's end (ADR-0030 point 5).
func (q QuietHours) digestAt(t time.Time) time.Time {
	hour := TimeOfDay{Hour: DigestHour}
	if hour.before(q.End) {
		hour = q.End
	}
	return hour.on(t, 0, t.Location())
}
