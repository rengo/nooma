package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// followReminder makes an event unit's armed triggers follow a corrected
// event_at (doc 02 §5 step 4, I29): prospection.Follow decides, by the
// functions a fresh capture arms with, and each move, creation or expiry
// writes its change-shaped decision row before its trigger write —
// ADR-0016's order, applied to the reminder. A due_at edit, or a unit of
// any other type, touches no trigger: nothing a unit-keeping capture arms
// is about either.
func (r correctionRunner) followReminder(ctx context.Context, target unit.Unit, plan []correction.Edit, interruptLevel *float64, now time.Time) error {
	if target.Type != unit.TypeEvent {
		return nil
	}
	var eventAt time.Time
	moved := false
	body := target.Content
	bodyChanged := false
	for _, e := range plan {
		if v, ok := e.EventAt(); ok {
			eventAt, moved = v, true
		}
		if v, ok := e.Content(); ok {
			body, bodyChanged = v, v != target.Content
		}
	}
	if !moved {
		return nil
	}

	armed, err := r.triggers.ArmedForUnit(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("correction: read reminders of unit %q: %w", target.ID, err)
	}
	live, byID := liveSet(armed)
	f := prospection.Follow(eventAt, live, reminderPrefs(ctx, r.config), interruptLevel, now)
	w := reminderSet{triggers: r.triggers, log: r.log, ids: r.ids, cause: correctionCause()}
	return w.apply(ctx, target, f, byID, eventAt, body, bodyChanged, now)
}

// liveSet is a unit's armed triggers as Follow reads them, and by id.
func liveSet(armed []ports.DueTrigger) ([]prospection.Live, map[string]ports.DueTrigger) {
	live := make([]prospection.Live, len(armed))
	byID := make(map[string]ports.DueTrigger, len(armed))
	for i, t := range armed {
		live[i] = prospection.Live{ID: t.ID, FireAt: t.FireAt, Rule: t.RecurrenceRule}
		byID[t.ID] = t
	}
	return live, byID
}

// reminderCause is why a unit's armed set is followed: the three rows it
// writes and the words its rationales use.
type reminderCause struct {
	op                      string
	moved, armed, cancelled ports.DecisionAction
	movedWith, armedFor     string
	carried, quiet, passed  string
	covered                 string
}

// correctionCause is a corrected event date's (I29).
func correctionCause() reminderCause {
	return reminderCause{
		op:    "correction",
		moved: ports.ActionCorrectionReminderMoved, armed: ports.ActionCorrectionReminderArmed, cancelled: ports.ActionCorrectionReminderCancelled,
		movedWith: "with its corrected date", armedFor: "for unit %q's corrected date",
		carried: "another reminder of the unit carries the corrected date",
		quiet:   "every moment left before the corrected date is in quiet hours, when nothing is sent",
		passed:  "the corrected date has already passed",
		covered: "a reminder of the event already sent covers its leads",
	}
}

// reminderSet writes what Follow decided for one unit, each move, creation
// or expiry recorded before its trigger write (ADR-0016's order).
type reminderSet struct {
	triggers ports.TriggerRepo
	log      ports.DecisionLog
	ids      ports.IDGen
	cause    reminderCause
	// tally, when set, counts each change; dry then writes none of them.
	tally *reminderTally
	dry   bool
}

func (r reminderSet) apply(ctx context.Context, target unit.Unit, f prospection.Following, byID map[string]ports.DueTrigger, eventAt time.Time, body string, bodyChanged bool, now time.Time) error {
	for i, plan := range f.Plans {
		var err error
		switch {
		case f.Carry[i] != "":
			err = r.moveReminder(ctx, target, byID[f.Carry[i]], plan, eventAt, body, bodyChanged, now)
		case plan.What != prospection.ArmNothing:
			err = r.armReminder(ctx, target.ID, body, plan, now)
		}
		if err != nil {
			return err
		}
	}
	for _, id := range f.Expire {
		if err := r.cancelReminder(ctx, target.ID, byID[id], f.Plans[0], now); err != nil {
			return err
		}
	}
	return nil
}

// moveReminder reschedules t to plan, with the payload a fresh arming of
// plan writes and its text following the date as the unit's body did. A
// move that changes nothing the reminder says or is read by writes nothing:
// not its instant, its text, its lead, what it is about, the leads it
// serves or its anchor. lead_days is not
// compared: only a legacy reminder carries one, nothing but the text
// follower reads it, and a new arming of the same date never does.
func (r reminderSet) moveReminder(ctx context.Context, target unit.Unit, t ports.DueTrigger, plan prospection.Plan, eventAt time.Time, body string, bodyChanged bool, now time.Time) error {
	text := followTriggerText(t, target.EventAt, eventAt, now.Location())
	if bodyChanged && t.Payload.ActionText == target.Content {
		text = body // the reminder said what the unit said, and the unit now says this
	}
	move := ports.TriggerMove{FireAt: plan.FireAt, Payload: armedPayload(text, plan)}

	fields := []string{}
	previous, next := map[string]any{}, map[string]any{}
	// Compared at the store's own precision: fire_at is kept to the second,
	// and a pull to now carries the clock's sub-second part.
	if !t.FireAt.Truncate(time.Second).Equal(plan.FireAt.Truncate(time.Second)) {
		fields = append(fields, "fire_at")
		previous["fire_at"], next["fire_at"] = rfc3339(t.FireAt), rfc3339(plan.FireAt)
	}
	if text != t.Payload.ActionText {
		fields = append(fields, "action_text")
		previous["action_text"], next["action_text"] = t.Payload.ActionText, text
	}
	// The leads are what name the event: the due scan reads fire_at plus
	// lead_minutes as its instant (ADR-0029 D4), so a lead that changed under
	// an unchanged fire_at is a move, not a no-op.
	if want := move.Payload; want.LeadMinutes != t.Payload.LeadMinutes {
		fields = append(fields, "lead_minutes")
		previous["lead_minutes"], next["lead_minutes"] = t.Payload.LeadMinutes, want.LeadMinutes
	}
	// What it is about and which leads it serves are what a later re-arm
	// reads back as sent (doc 02 §7): a lead merged into it, or a row from
	// before they were recorded, is a move even at an unchanged fire_at.
	if want := move.Payload; !want.About.Equal(t.Payload.About) {
		fields = append(fields, "event_at")
		previous["event_at"], next["event_at"] = instantText(t.Payload.About), instantText(want.About)
	}
	if want := move.Payload; !slices.Equal(want.UserLeadsMinutes, t.Payload.UserLeadsMinutes) {
		fields = append(fields, "user_leads_minutes")
		previous["user_leads_minutes"], next["user_leads_minutes"] = t.Payload.UserLeadsMinutes, want.UserLeadsMinutes
	}
	if plan.What == prospection.ArmRecurring {
		a := plan.Anchor
		move.RecurrenceAnchor = &a
		if was := anchorText(t.RecurrenceAnchor); was != anchorText(&a) {
			fields = append(fields, "recurrence_anchor")
			previous["recurrence_anchor"], next["recurrence_anchor"] = was, anchorText(&a)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	if r.tally != nil {
		r.tally.moved++
	}
	if r.dry {
		return nil
	}

	if err := r.recordReminder(ctx, r.cause.moved,
		fmt.Sprintf("moved the reminder of unit %q %s: %s", target.ID, r.cause.movedWith, armRationale(plan)),
		reminderChange{UnitID: target.ID, TriggerID: t.ID, Fields: fields, Previous: previous, Next: next,
			About: rfc3339(plan.About), Immediate: plan.Immediate}, now); err != nil {
		return err
	}
	if err := r.triggers.Reschedule(ctx, t.ID, move); err != nil {
		return fmt.Errorf("%s: move reminder %q: %w", r.cause.op, t.ID, err)
	}
	return nil
}

// followTriggerText rewrites t's text from the instant it was about to
// eventAt. That instant is the unit's previous event_at; when the unit
// already holds eventAt — a retry after the reminder step failed — it is
// the trigger's own, fire_at plus its lead (days for a recurring trigger,
// minutes for an event reminder), exact unless the firing had been pulled
// forward to its capture.
func followTriggerText(t ports.DueTrigger, previous *time.Time, eventAt time.Time, zone *time.Location) string {
	candidates := []time.Time{}
	if previous != nil {
		candidates = append(candidates, *previous)
	}
	if t.Payload.LeadDays > 0 {
		candidates = append(candidates, t.FireAt.AddDate(0, 0, t.Payload.LeadDays))
	}
	if t.Payload.LeadMinutes > 0 {
		candidates = append(candidates, t.FireAt.Add(time.Duration(t.Payload.LeadMinutes)*time.Minute))
	}
	for _, was := range candidates {
		if text, changed := correction.FollowDate(t.Payload.ActionText, was, eventAt, zone); changed {
			return text
		}
	}
	return t.Payload.ActionText
}

// anchorText renders a recurrence anchor as MM-DD, or "" for none.
func anchorText(a *prospection.Anchor) string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("%02d-%02d", int(a.Month), a.Day)
}

// armReminder creates the trigger a fresh capture of the corrected date
// arms, when the unit had none armed.
func (r reminderSet) armReminder(ctx context.Context, unitID, body string, plan prospection.Plan, now time.Time) error {
	if r.tally != nil {
		r.tally.armed++
	}
	if r.dry {
		return nil
	}
	trigger := armedTrigger(r.ids.New(), &unitID, body, plan, now)
	if err := r.recordReminder(ctx, r.cause.armed,
		fmt.Sprintf("armed a reminder "+r.cause.armedFor+": %s", unitID, armRationale(plan)),
		reminderChange{UnitID: unitID, TriggerID: trigger.ID, Fields: []string{"fire_at"},
			Previous: map[string]any{"fire_at": nil}, Next: map[string]any{"fire_at": rfc3339(plan.FireAt)},
			About: rfc3339(plan.About), Immediate: plan.Immediate}, now); err != nil {
		return err
	}
	if err := r.triggers.Create(ctx, trigger); err != nil {
		return fmt.Errorf("%s: arm reminder for unit %q: %w", r.cause.op, unitID, err)
	}
	return nil
}

// cancelReminder expires t: its date is past, no instant before it is
// outside quiet hours, or another trigger carries the unit's reminder. Expired is I15's status for a trigger that will not
// fire; nothing is deleted.
func (r reminderSet) cancelReminder(ctx context.Context, unitID string, t ports.DueTrigger, plan prospection.Plan, now time.Time) error {
	if r.tally != nil {
		r.tally.cancelled++
	}
	if r.dry {
		return nil
	}
	why := r.cause.carried
	switch {
	case plan.What == prospection.ArmNothing && plan.Why == prospection.RefusalQuietHours:
		why = r.cause.quiet
	case plan.What == prospection.ArmNothing && plan.Why == prospection.RefusalCovered:
		why = r.cause.covered
	case plan.What == prospection.ArmNothing:
		why = r.cause.passed
	}
	if err := r.recordReminder(ctx, r.cause.cancelled,
		fmt.Sprintf("cancelled reminder %q of unit %q: %s", t.ID, unitID, why),
		reminderChange{UnitID: unitID, TriggerID: t.ID, Fields: []string{"status"},
			Previous: map[string]any{"status": string(ports.TriggerStatusArmed)},
			Next:     map[string]any{"status": string(ports.TriggerStatusExpired)},
			FireAt:   rfc3339(t.FireAt)}, now); err != nil {
		return err
	}
	if err := r.triggers.Expire(ctx, t.ID); err != nil {
		return fmt.Errorf("%s: cancel reminder %q: %w", r.cause.op, t.ID, err)
	}
	return nil
}

// reminderChange is the context of the three reminder rows: change-shaped,
// so /ui/activity renders previous -> next with no template of its own.
type reminderChange struct {
	UnitID    string         `json:"unit_id"`
	TriggerID string         `json:"trigger_id"`
	Fields    []string       `json:"fields"`
	Previous  map[string]any `json:"previous"`
	Next      map[string]any `json:"next"`
	About     string         `json:"about,omitempty"`
	Immediate bool           `json:"immediate,omitempty"`
	FireAt    string         `json:"fire_at,omitempty"`
}

func (r reminderSet) recordReminder(ctx context.Context, action ports.DecisionAction, rationale string, change reminderChange, now time.Time) error {
	contextJSON, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("%s: encode %s context: %w", r.cause.op, action, err)
	}
	if err := r.log.Record(ctx, ports.Decision{ID: r.ids.New(), Action: action, Rationale: rationale, Context: contextJSON, OccurredAt: now}); err != nil {
		return fmt.Errorf("%s: record %s for trigger %q: %w", r.cause.op, action, change.TriggerID, err)
	}
	return nil
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// instantText is rfc3339, or nil for an instant never recorded.
func instantText(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return rfc3339(t)
}
