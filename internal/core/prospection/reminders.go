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
)

// TimeOfDay is a local wall-clock time.
type TimeOfDay struct{ Hour, Minute int }

// ReminderPrefs are the user's event reminder preferences: how long before
// a timed event each reminder fires, and when, the day before, a date-only
// event is reminded.
type ReminderPrefs struct {
	TimedLeads []time.Duration
	DateOnlyAt TimeOfDay
}

// DefaultReminderPrefs is ADR-0029's defaults.
func DefaultReminderPrefs() ReminderPrefs {
	return ReminderPrefs{
		TimedLeads: []time.Duration{DefaultEventLeadFarHours * time.Hour, DefaultEventLeadNearHours * time.Hour},
		DateOnlyAt: TimeOfDay{Hour: DefaultDateOnlyReminderHour},
	}
}

// eventReminders arms the one-shot reminders a dated event owns: one per
// lead still ahead of now, in firing order, or one at once when every lead
// is behind and the event is not. An event at or before now arms nothing —
// doc 02 §5.1 refuses to arm on a date it cannot trust, and a nudge for
// something already over is the same refusal pointing the other way.
func eventReminders(eventAt time.Time, prefs ReminderPrefs, now time.Time, interrupt Interrupt) ([]Plan, bool) {
	if !eventAt.After(now) {
		return []Plan{{What: ArmNothing, Why: RefusalAlreadyPast, Interrupt: interrupt}}, false
	}
	var plans []Plan
	for _, fireAt := range reminderInstants(eventAt, prefs, now.Location()) {
		// clampToNow's own boundary: an instant equal to now is not behind.
		if fireAt.Before(now) {
			continue
		}
		plans = append(plans, Plan{What: ArmTrigger, FireAt: fireAt, About: eventAt,
			LeadMinutes: int(eventAt.Sub(fireAt) / time.Minute), Interrupt: interrupt})
	}
	if len(plans) == 0 {
		return []Plan{{What: ArmTrigger, FireAt: now, About: eventAt, Immediate: true, Interrupt: interrupt}}, true
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
	if local.Hour() == 0 && local.Minute() == 0 && local.Second() == 0 && local.Nanosecond() == 0 {
		y, m, d := local.Date()
		return []time.Time{time.Date(y, m, d-1, prefs.DateOnlyAt.Hour, prefs.DateOnlyAt.Minute, 0, 0, zone)}
	}
	out := make([]time.Time, len(prefs.TimedLeads))
	for i, lead := range prefs.TimedLeads {
		out[i] = eventAt.Add(-lead)
	}
	return out
}
