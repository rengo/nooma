package prospection

import "time"

// CatchUp is the one reminder an event is owed when every lead of it has
// passed and nothing about it reached the user (ADR-0032 point 3): the event is
// ahead, no reminder of it is live, and nothing sent is about this instant.
// owed is false otherwise, and then the caller plans nothing.
//
// It goes at once through ADR-0030's rule: now, or at quiet end when that is
// before the event. When no instant before the event is outside quiet hours the
// plan is an ArmNothing refusal (RefusalQuietHours), still owed, so the caller
// can say why nothing was sent. Never one per lapsed lead, and never more than
// one outstanding: a live reminder, a delivered one or one still being
// delivered all make it not owed.
func CatchUp(eventAt time.Time, live []Live, sent []Sent, prefs ReminderPrefs, interrupt Interrupt, now time.Time) (Plan, bool) {
	if !eventAt.After(now) || len(live) > 0 || !everyLeadBehind(eventAt, prefs, now) {
		return Plan{}, false
	}
	for _, s := range sent {
		if s.About.Equal(eventAt) {
			return Plan{}, false
		}
	}
	at, ok := deliverAt(now, eventAt, now, prefs)
	if !ok {
		return Plan{What: ArmNothing, Why: RefusalQuietHours, Interrupt: interrupt}, true
	}
	plan := Plan{What: ArmTrigger, FireAt: at, About: eventAt, Immediate: at.Equal(now), Interrupt: interrupt}
	if !plan.Immediate {
		plan.LeadMinutes = int(eventAt.Sub(at) / time.Minute)
	}
	return plan, true
}
