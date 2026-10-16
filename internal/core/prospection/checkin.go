package prospection

import (
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// MaxCheckinReasks is how many times the morning digest asks a check-in the
// user put off again before it closes by itself (doc 02 §7, the client's
// ruling of 2026-10-16). It is also a check-in's lifetime when nobody
// answers it: one left unanswered closes at the digest one put off the
// evening it reached the user would close at, MaxCheckinReasks + 1 digests
// after it.
const MaxCheckinReasks = 2

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
	// digest that last asked it again.
	SurfacedAt time.Time
	// UnitType is its unit's kind, empty for a nudge with no unit.
	UnitType unit.Type
	// Snoozed is whether the user put it off ("not yet").
	Snoozed bool
	// Reasks is how many digests have already asked it again.
	Reasks int
}

// CheckinStep decides c at now, a pass in which a digest is due. Digests
// are counted as the mornings due in q's night strictly after c.SurfacedAt,
// in now's zone, so a digest that carried nothing still counts as a morning
// passed.
//
// A check-in asked again is asked again at every later digest, put off
// again or left unanswered alike, until MaxCheckinReasks; the digest after
// the last closes it. One put off and never asked yet is asked at the next
// digest when it has a unit. Any other closes at the
// MaxCheckinReasks + 1st digest after it reached the user.
func (q QuietHours) CheckinStep(c OpenCheckin, now time.Time) CheckinStep {
	digests := q.digestsAfter(c.SurfacedAt, now, MaxCheckinReasks+1)
	switch {
	case digests == 0:
		return CheckinWait
	case c.Reasks >= MaxCheckinReasks:
		return CheckinExpire
	case c.Reasks > 0 || (c.Snoozed && pendingWork(c.UnitType)):
		return CheckinReask
	case digests > MaxCheckinReasks:
		return CheckinExpire
	}
	return CheckinWait
}

// pendingWork reports whether a unit of kind t is still something to do, so a
// check-in about it can be asked again: a task or a mental load, an open loop.
// An event, and every kind that is memory rather than work, is not: asked the
// morning after, an event is usually past, and a reminder never speaks of the
// past (ADR-0032). One case per kind, so a kind added later is decided here.
func pendingWork(t unit.Type) bool {
	switch t {
	case unit.TypeTask, unit.TypeMentalLoad:
		return true
	case unit.TypeEvent, unit.TypeKnowledge, unit.TypeProcedural, unit.TypeEmotional,
		unit.TypeList, unit.TypeStructuredRef, unit.TypeInsight:
		return false
	}
	return false
}

// digestsAfter counts the digest instants in (since, now], read on now's
// wall clock, stopping at limit: an old check-in costs no more than a new one.
func (q QuietHours) digestsAfter(since, now time.Time, limit int) int {
	n := 0
	for day := now; n < limit; day = day.AddDate(0, 0, -1) {
		at := q.digestAt(day)
		if !at.After(since) {
			break
		}
		if !at.After(now) {
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
