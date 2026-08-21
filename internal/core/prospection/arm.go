package prospection

import "time"

// EventLeadDays is doc 02 §7's notification horizon: how far before a dated
// event its trigger fires. Untyped and not a time.Duration, for the reason
// QuietHoursStartHour's own comment gives.
const EventLeadDays = 7

// LeadTime returns the instant EventLeadDays before eventAt, in eventAt's
// own location.
//
// Deliberately unclamped (spec R5.2, Finding F5): it answers "when is the
// horizon for this event", which is a fact about the event, and returns an
// instant in the past when the event is closer than the horizon. Arm applies
// the now-clamp at its own call site, because "do not arm in the past" is a
// fact about arming rather than about the horizon. Two layers, so each is
// testable on its own terms.
func LeadTime(eventAt time.Time) time.Time {
	y, m, d := eventAt.Date()
	return time.Date(y, m, d-EventLeadDays, eventAt.Hour(), eventAt.Minute(),
		eventAt.Second(), eventAt.Nanosecond(), eventAt.Location())
}
