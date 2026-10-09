package brain

import (
	"context"
	"encoding/json"
	"fmt"
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
	for _, e := range plan {
		if v, ok := e.EventAt(); ok {
			eventAt, moved = v, true
		}
		if v, ok := e.Content(); ok {
			body = v
		}
	}
	if !moved {
		return nil
	}

	armed, err := r.triggers.ArmedForUnit(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("correction: read reminders of unit %q: %w", target.ID, err)
	}
	live := make([]prospection.Live, len(armed))
	byID := make(map[string]ports.DueTrigger, len(armed))
	for i, t := range armed {
		live[i] = prospection.Live{ID: t.ID, FireAt: t.FireAt, Rule: t.RecurrenceRule}
		byID[t.ID] = t
	}
	f := prospection.Follow(eventAt, live, interruptLevel, now)

	switch {
	case f.Carry != "":
		if err := r.moveReminder(ctx, target, byID[f.Carry], f.Plan, eventAt, now); err != nil {
			return err
		}
	case f.Plan.What != prospection.ArmNothing:
		if err := r.armReminder(ctx, target.ID, body, f.Plan, now); err != nil {
			return err
		}
	}
	for _, id := range f.Expire {
		if err := r.cancelReminder(ctx, target.ID, byID[id], f.Plan, now); err != nil {
			return err
		}
	}
	return nil
}

// moveReminder reschedules t to plan, with the payload a fresh arming of
// plan writes and its text following the date as the unit's body did. A
// move that changes nothing visible writes nothing.
func (r correctionRunner) moveReminder(ctx context.Context, target unit.Unit, t ports.DueTrigger, plan prospection.Plan, eventAt, now time.Time) error {
	text := followTriggerText(t, target.EventAt, eventAt, now.Location())
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

	if err := r.recordReminder(ctx, ports.ActionCorrectionReminderMoved,
		fmt.Sprintf("moved the reminder of unit %q with its corrected date: %s", target.ID, armRationale(plan)),
		reminderChange{UnitID: target.ID, TriggerID: t.ID, Fields: fields, Previous: previous, Next: next,
			About: rfc3339(plan.About), Immediate: plan.Immediate}, now); err != nil {
		return err
	}
	if err := r.triggers.Reschedule(ctx, t.ID, move); err != nil {
		return fmt.Errorf("correction: move reminder %q: %w", t.ID, err)
	}
	return nil
}

// followTriggerText rewrites t's text from the instant it was about to
// eventAt. That instant is the unit's previous event_at; when the unit
// already holds eventAt — a retry after the reminder step failed — it is
// the trigger's own, fire_at plus its lead days, exact unless the firing
// had been pulled forward to its capture.
func followTriggerText(t ports.DueTrigger, previous *time.Time, eventAt time.Time, zone *time.Location) string {
	candidates := []time.Time{}
	if previous != nil {
		candidates = append(candidates, *previous)
	}
	if t.Payload.LeadDays > 0 {
		candidates = append(candidates, t.FireAt.AddDate(0, 0, t.Payload.LeadDays))
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
func (r correctionRunner) armReminder(ctx context.Context, unitID, body string, plan prospection.Plan, now time.Time) error {
	trigger := armedTrigger(r.ids.New(), &unitID, body, plan, now)
	if err := r.recordReminder(ctx, ports.ActionCorrectionReminderArmed,
		fmt.Sprintf("armed a reminder for unit %q's corrected date: %s", unitID, armRationale(plan)),
		reminderChange{UnitID: unitID, TriggerID: trigger.ID, Fields: []string{"fire_at"},
			Previous: map[string]any{"fire_at": nil}, Next: map[string]any{"fire_at": rfc3339(plan.FireAt)},
			About: rfc3339(plan.About), Immediate: plan.Immediate}, now); err != nil {
		return err
	}
	if err := r.triggers.Create(ctx, trigger); err != nil {
		return fmt.Errorf("correction: arm reminder for unit %q: %w", unitID, err)
	}
	return nil
}

// cancelReminder expires t: its date is past, or another trigger carries
// the unit's reminder. Expired is I15's status for a trigger that will not
// fire; nothing is deleted.
func (r correctionRunner) cancelReminder(ctx context.Context, unitID string, t ports.DueTrigger, plan prospection.Plan, now time.Time) error {
	why := "another reminder of the unit carries the corrected date"
	if plan.What == prospection.ArmNothing {
		why = "the corrected date has already passed"
	}
	if err := r.recordReminder(ctx, ports.ActionCorrectionReminderCancelled,
		fmt.Sprintf("cancelled reminder %q of unit %q: %s", t.ID, unitID, why),
		reminderChange{UnitID: unitID, TriggerID: t.ID, Fields: []string{"status"},
			Previous: map[string]any{"status": string(ports.TriggerStatusArmed)},
			Next:     map[string]any{"status": string(ports.TriggerStatusExpired)},
			FireAt:   rfc3339(t.FireAt)}, now); err != nil {
		return err
	}
	if err := r.triggers.Expire(ctx, t.ID); err != nil {
		return fmt.Errorf("correction: cancel reminder %q: %w", t.ID, err)
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

func (r correctionRunner) recordReminder(ctx context.Context, action ports.DecisionAction, rationale string, change reminderChange, now time.Time) error {
	contextJSON, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("correction: encode %s context: %w", action, err)
	}
	if err := r.log.Record(ctx, ports.Decision{ID: r.ids.New(), Action: action, Rationale: rationale, Context: contextJSON, OccurredAt: now}); err != nil {
		return fmt.Errorf("correction: record %s for trigger %q: %w", action, change.TriggerID, err)
	}
	return nil
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
