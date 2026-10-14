package prospection

import (
	"slices"
	"time"
)

// The default event reminders (doc 02 §7 "Lead time", ADR-0029): a timed
// event is reminded DefaultEventLeadFarHours and DefaultEventLeadNearHours
// before it, a date-only event the day before at DefaultDateOnlyReminderHour
// local. Defaults only: the user's own preferences override them.
const (
	DefaultEventLeadFarHours    = 24
	DefaultEventLeadNearHours   = 2
	DefaultDateOnlyReminderHour = 9
	// DefaultEveningReminderHour is the local hour, the evening before, an
	// event reminder is moved to when its lead falls in quiet hours and
	// the event is at or before quiet end (ADR-0030).
	DefaultEveningReminderHour = 22
)

// TimeOfDay is a local wall-clock time.
type TimeOfDay struct{ Hour, Minute int }

// ReminderPrefs are the user's event reminder preferences: how long before
// a timed event each reminder fires, when, the day before, a date-only
// event is reminded, when quiet hours end, and when, the evening before, a
// reminder that cannot wait for quiet end is sent.
type ReminderPrefs struct {
	TimedLeads []time.Duration
	DateOnlyAt TimeOfDay
	QuietEnd   TimeOfDay
	EveningAt  TimeOfDay
}

// DefaultReminderPrefs is ADR-0029's and ADR-0030's defaults.
func DefaultReminderPrefs() ReminderPrefs {
	return ReminderPrefs{
		TimedLeads: []time.Duration{DefaultEventLeadFarHours * time.Hour, DefaultEventLeadNearHours * time.Hour},
		DateOnlyAt: TimeOfDay{Hour: DefaultDateOnlyReminderHour},
		QuietEnd:   TimeOfDay{Hour: QuietHoursEndHour},
		EveningAt:  TimeOfDay{Hour: DefaultEveningReminderHour},
	}
}

// on is this time of day on t's calendar day, offset by days, in zone.
func (d TimeOfDay) on(t time.Time, days int, zone *time.Location) time.Time {
	y, m, day := t.Date()
	return time.Date(y, m, day+days, d.Hour, d.Minute, 0, 0, zone)
}

// deliverAt is when a reminder nominally due at nominal is armed, or false
// when no allowed instant is left before the event (ADR-0030 points 2-4).
// Quiet hours are [QuietHoursStartHour, QuietEnd) on zone's wall clock,
// zone being now's. In them, the reminder waits for quiet end when that is
// still before the event, and goes out the evening before at EveningAt
// otherwise — earlier, never later. An instant left behind now is sent at
// once when now is outside quiet hours, and dropped when it is not. Every
// result is before eventAt: the quiet-end branch checks it, and the other
// two are at or before nominal, itself before the event.
func deliverAt(nominal, eventAt, now time.Time, prefs ReminderPrefs) (time.Time, bool) {
	night := QuietHours{End: prefs.QuietEnd}
	at := dueAt(nominal, eventAt, now.Location(), prefs)
	if !at.Before(now) {
		return at, true
	}
	if night.Contains(now) {
		return time.Time{}, false
	}
	return now, true
}

// Sent is a one-shot reminder of a unit that already went out: the event
// instant it was about, every user lead it served (none for one armed at
// once, or when its row predates recording them) and when it went out. A
// zero About is a send nobody can place, which covers nothing.
type Sent struct {
	About time.Time
	Leads []time.Duration
	At    time.Time
}

// coveredBy reports whether a reminder already sent about eventAt covers
// the lead whose nominal instant is n and whose quiet-moved instant is
// due: one served that very lead, or one went out at or after due. A send
// about any other instant covers nothing.
func coveredBy(sent []Sent, eventAt, n, due time.Time) bool {
	for _, s := range sent {
		if s.About.Equal(eventAt) && (slices.Contains(s.Leads, eventAt.Sub(n)) || !due.After(s.At)) {
			return true
		}
	}
	return false
}

// dueAt is nominal moved out of quiet hours: to quiet end when that is
// before the event, else to the evening before at EveningAt.
func dueAt(nominal, eventAt time.Time, zone *time.Location, prefs ReminderPrefs) time.Time {
	night := QuietHours{End: prefs.QuietEnd}
	if local := nominal.In(zone); night.Contains(local) {
		if at := night.DeliverableFrom(local); at.Before(eventAt) {
			return at
		}
		return prefs.EveningAt.on(local, -1, zone)
	}
	return nominal
}

// eventReminders arms the one-shot reminders a dated event owns: one per
// lead still ahead of now, in firing order, or one at once when every lead
// is behind and the event is not. An event at or before now arms nothing —
// doc 02 §5.1 refuses to arm on a date it cannot trust, and a nudge for
// something already over is the same refusal pointing the other way.
// sent is what already went out for the unit (FollowSent), nil for a
// capture or a correction.
func eventReminders(eventAt time.Time, prefs ReminderPrefs, now time.Time, interrupt Interrupt, sent []Sent) ([]Plan, bool) {
	if !eventAt.After(now) {
		return []Plan{{What: ArmNothing, Why: RefusalAlreadyPast, Interrupt: interrupt}}, false
	}
	nominal := leadsAhead(eventAt, prefs, now)
	atOnce := len(nominal) == 0
	if atOnce {
		nominal = []time.Time{now} // ADR-0029 point 3's at-once reminder
	}
	var plans []Plan
	covered := false
	for _, n := range nominal {
		if coveredBy(sent, eventAt, n, dueAt(n, eventAt, now.Location(), prefs)) {
			covered = true
			continue
		}
		fireAt, ok := deliverAt(n, eventAt, now, prefs)
		if !ok {
			continue
		}
		// Two leads landing on one instant are one reminder serving both.
		if i := slices.IndexFunc(plans, func(p Plan) bool { return p.FireAt.Equal(fireAt) }); i >= 0 {
			if !atOnce {
				plans[i].Leads = append(plans[i].Leads, eventAt.Sub(n))
			}
			continue
		}
		// Immediate is "sent at once rather than at a lead": the at-once
		// reminder, or one deliverAt pulled to now. A lead that is exactly
		// now is still that lead.
		immediate := fireAt.Equal(now) && (atOnce || !n.Equal(now))
		plan := Plan{What: ArmTrigger, FireAt: fireAt, About: eventAt, Immediate: immediate, Interrupt: interrupt}
		if !atOnce {
			plan.Leads = []time.Duration{eventAt.Sub(n)}
		}
		if !plan.Immediate {
			plan.LeadMinutes = int(eventAt.Sub(fireAt) / time.Minute)
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 && covered {
		return []Plan{{What: ArmNothing, Why: RefusalCovered, Interrupt: interrupt}}, false
	}
	if len(plans) == 0 {
		return []Plan{{What: ArmNothing, Why: RefusalQuietHours, Interrupt: interrupt}}, false
	}
	slices.SortFunc(plans, func(a, b Plan) int { return a.FireAt.Compare(b.FireAt) })
	return plans, true
}

// reminderInstants is where an event's reminders would fire, behind now or
// not. A date-only event — classify stores a bare date as midnight in the
// user's zone, which zone is — is reminded the day before at DateOnlyAt,
// built on that zone's wall clock. A timed one is reminded each lead
// before it, as absolute durations: 24 hours before is 24 hours before
// across a DST change too.
func reminderInstants(eventAt time.Time, prefs ReminderPrefs, zone *time.Location) []time.Time {
	local := eventAt.In(zone)
	if isDateOnly(local) {
		y, m, d := local.Date()
		return []time.Time{time.Date(y, m, d-1, prefs.DateOnlyAt.Hour, prefs.DateOnlyAt.Minute, 0, 0, zone)}
	}
	out := make([]time.Time, len(prefs.TimedLeads))
	for i, lead := range prefs.TimedLeads {
		out[i] = eventAt.Add(-lead)
	}
	return out
}

// isDateOnly reports whether local (in the user's zone) is midnight sharp, how
// classify stores a bare date; a timed 00:00 event reads as one. Shared by
// reminderInstants and the delivery gate.
func isDateOnly(local time.Time) bool {
	return local.Hour() == 0 && local.Minute() == 0 && local.Second() == 0 && local.Nanosecond() == 0
}

// leadsAhead is the nominal instants of eventAt's reminders not behind now
// — clampToNow's own boundary: an instant equal to now is not behind.
func leadsAhead(eventAt time.Time, prefs ReminderPrefs, now time.Time) []time.Time {
	var ahead []time.Time
	for _, fireAt := range reminderInstants(eventAt, prefs, now.Location()) {
		if !fireAt.Before(now) {
			ahead = append(ahead, fireAt)
		}
	}
	return ahead
}

// everyLeadBehind reports ADR-0029 point 3's case: no lead of eventAt is
// left, so a capture arms its one reminder at once.
func everyLeadBehind(eventAt time.Time, prefs ReminderPrefs, now time.Time) bool {
	return len(leadsAhead(eventAt, prefs, now)) == 0
}
