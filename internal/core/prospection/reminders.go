package prospection

import (
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
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
	// DefaultRemindMeLeadMinutes is how long before a named time an asked
	// reminder fires when the user has not chosen (ADR-0031 point 2).
	DefaultRemindMeLeadMinutes = 15
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
	// RemindMeLead is how long before a named time a one-shot asked
	// reminder fires (ADR-0031 point 2).
	RemindMeLead time.Duration
}

// DefaultReminderPrefs is ADR-0029's and ADR-0030's defaults.
func DefaultReminderPrefs() ReminderPrefs {
	return ReminderPrefs{
		TimedLeads:   []time.Duration{DefaultEventLeadFarHours * time.Hour, DefaultEventLeadNearHours * time.Hour},
		DateOnlyAt:   TimeOfDay{Hour: DefaultDateOnlyReminderHour},
		QuietEnd:     TimeOfDay{Hour: QuietHoursEndHour},
		EveningAt:    TimeOfDay{Hour: DefaultEveningReminderHour},
		RemindMeLead: DefaultRemindMeLeadMinutes * time.Minute,
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

// lead is one nominal reminder instant of a unit's instant, before any
// quiet-hours move. asked marks the one the user's own words asked for.
type lead struct {
	at    time.Time
	asked bool
}

// leadSet is the nominal instants a unit's reminders aim at (ADR-0031 D3). An
// event, or a recurring reminder read as one, keeps its ADR-0029 leads; an
// asked unit adds the asked lead: at minus RemindMeLead for a named time, that
// local day at DateOnlyAt for a day alone. An unasked kind other than those two
// has none.
func leadSet(kind classify.Kind, asked bool, at time.Time, dayOnly bool, prefs ReminderPrefs, zone *time.Location) []lead {
	var out []lead
	if kind == classify.KindEvent || kind == classify.KindRecurringReminder {
		for _, n := range reminderInstants(at, prefs, zone) {
			out = append(out, lead{at: n})
		}
	}
	if asked {
		n := at.Add(-prefs.RemindMeLead)
		if dayOnly {
			n = prefs.DateOnlyAt.on(at.In(zone), 0, zone)
		}
		out = append(out, lead{at: n, asked: true})
	}
	return out
}

// eventReminders is reminders for an unasked dated event: the ADR-0029 leads.
// sent is what already went out for the unit (FollowSent), nil for a capture
// or a correction.
func eventReminders(eventAt time.Time, prefs ReminderPrefs, now time.Time, interrupt Interrupt, sent []Sent) ([]Plan, bool) {
	leads := leadSet(classify.KindEvent, false, eventAt, false, prefs, now.Location())
	return reminders(eventAt, false, leads, prefs, now, interrupt, sent)
}

// reminders arms the one-shot reminders a dated unit owns: one per lead still
// ahead of now, in firing order, or one at once when every lead is behind and
// the unit's instant is not. An instant at or before now arms nothing — doc 02
// §5.1 refuses to arm on a date it cannot trust, and a nudge for something
// already over is the same refusal pointing the other way. A day-only unit's
// instant is its whole local day: it is over at the next local midnight.
//
// An asked lead is stronger than the night (ADR-0031 point 6): at a named time
// it is neither moved nor dropped by quiet hours, and a behind one becomes an
// at-once reminder whatever the hour, refused too_late only when less than
// AtOnceMarginMinutes remain (the instant, or the day's end).
func reminders(at time.Time, dayOnly bool, leads []lead, prefs ReminderPrefs, now time.Time, interrupt Interrupt, sent []Sent) ([]Plan, bool) {
	zone := now.Location()
	end := at
	if dayOnly {
		y, m, d := at.In(zone).Date()
		end = time.Date(y, m, d+1, 0, 0, 0, 0, zone)
	}
	if !end.After(now) {
		return []Plan{{What: ArmNothing, Why: RefusalAlreadyPast, Interrupt: interrupt}}, false
	}
	type item struct {
		lead
		once bool // sent at once rather than at a lead
	}
	var items []item
	askedBehind := false
	for _, l := range leads {
		switch {
		case !l.at.Before(now):
			items = append(items, item{lead: l})
		case l.asked:
			askedBehind = true
		}
	}
	switch {
	case askedBehind:
		items = append(items, item{lead: lead{at: now, asked: true}, once: true}) // ADR-0031 point 2
	case len(items) == 0:
		items = append(items, item{lead: lead{at: now}, once: true}) // ADR-0029 point 3's at-once reminder
	}
	var plans []Plan
	covered, tooLate := false, false
	for _, it := range items {
		if it.asked && it.once && end.Sub(now) < AtOnceMarginMinutes*time.Minute {
			tooLate = true
			continue
		}
		exempt := it.asked && (it.once || !dayOnly)
		due := it.at
		if !exempt {
			due = dueAt(it.at, end, zone, prefs)
		}
		if coveredBy(sent, at, it.at, due) {
			covered = true
			continue
		}
		fireAt := it.at
		if !exempt {
			var ok bool
			if fireAt, ok = deliverAt(it.at, end, now, prefs); !ok {
				continue
			}
		}
		servesLead := !it.once && (!it.asked || !dayOnly)
		// Two leads landing on one instant are one reminder serving both.
		if i := slices.IndexFunc(plans, func(p Plan) bool { return p.FireAt.Equal(fireAt) }); i >= 0 {
			plans[i].Asked = plans[i].Asked || it.asked
			plans[i].NightExempt = plans[i].NightExempt || exempt
			if servesLead {
				plans[i].Leads = append(plans[i].Leads, at.Sub(it.at))
			}
			continue
		}
		// Immediate is "sent at once rather than at a lead": the at-once
		// reminder, or one deliverAt pulled to now. A lead that is exactly
		// now is still that lead.
		immediate := fireAt.Equal(now) && (it.once || !it.at.Equal(now))
		plan := Plan{What: ArmTrigger, FireAt: fireAt, About: at, Immediate: immediate, Interrupt: interrupt,
			Asked: it.asked, DayOnly: dayOnly, NightExempt: exempt}
		if servesLead {
			plan.Leads = []time.Duration{at.Sub(it.at)}
		}
		if m := int(at.Sub(fireAt) / time.Minute); !plan.Immediate && m > 0 {
			plan.LeadMinutes = m
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		why := RefusalQuietHours
		switch {
		case covered:
			why = RefusalCovered
		case tooLate:
			why = RefusalTooLate
		}
		return []Plan{{What: ArmNothing, Why: why, Interrupt: interrupt}}, false
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
