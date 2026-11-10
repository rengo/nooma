package brain

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// remindReferent makes u, the item a chat follow-up named, an asked reminder
// (ADR-0043): "recordámelo" right after a task. It is armed as a capture that
// asked arms it (ADR-0031), on the unit it is, read as it stands: nothing is
// stored and nothing about the unit is rewritten but the asked mark. Only the
// asked reminder is armed, so an event's own leads are never armed twice, and
// an asked reminder already armed at the same instant is not armed again. An
// item with no date is still marked asked, and the answer says no reminder
// was set and why, as an asked capture's does. An armed answer names every
// ring the item has (ADR-0044), as a capture's names all it armed.
//
// A recurring reminder already reminds of itself: its stored event_at is the
// anchor it repeats from, no one-shot instant, and arming it as one refused it
// as past (the live report of 2026-11-10). It is answered by its live trigger,
// which is left as it is.
func (r captureRunner) remindReferent(ctx context.Context, u unit.Unit, now time.Time) (CaptureResult, error) {
	if res, ok, err := r.remindRecurring(ctx, u, now); err != nil || ok {
		return res, err
	}
	res, err := r.remindReferentOnly(ctx, u, now)
	if err != nil || res.Armed == nil {
		return res, err
	}
	armed, err := r.triggers.ArmedForUnit(ctx, u.ID)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture: read reminders of unit %q: %w", u.ID, err)
	}
	var fires []time.Time
	for _, t := range armed { // ordered by fire time
		if t.RecurrenceRule == nil {
			fires = append(fires, t.FireAt)
		}
	}
	if len(fires) > 0 {
		res.Armed.FireAt, res.Armed.Later = fires[0], fires[1:]
	}
	return res, nil
}

// remindRecurring answers a remind-me of u when u has a live recurring
// trigger: u is marked asked, nothing is armed or refused, and the answer names
// the trigger's rule, at its occurrence (About), and when it actually rings
// next (FireAt, prospection.NextRing). false when no live trigger of u recurs.
func (r captureRunner) remindRecurring(ctx context.Context, u unit.Unit, now time.Time) (CaptureResult, bool, error) {
	armed, err := r.triggers.ArmedForUnit(ctx, u.ID)
	if err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: read reminders of unit %q: %w", u.ID, err)
	}
	i := slices.IndexFunc(armed, func(t ports.DueTrigger) bool { return t.RecurrenceRule != nil && t.RecurrenceAnchor != nil })
	if i < 0 {
		return CaptureResult{}, false, nil
	}
	if !u.RemindMe {
		if err := r.units.SetRemindMe(ctx, u.ID, now); err != nil {
			return CaptureResult{}, false, fmt.Errorf("capture: mark unit %q asked: %w", u.ID, err)
		}
	}
	t := armed[i]
	// When the row actually rings, as the due scan will judge it in the user's
	// night: never a past instant, nor one the night holds it past.
	night := prospection.QuietHours{End: reminderPrefs(ctx, r.config).QuietEnd}
	occurrence, ring := night.NextRing(newReminder(t, false), now)
	return CaptureResult{Outcome: OutcomeArmed, UnitID: u.ID, Armed: &Armed{What: prospection.ArmRecurring, ID: t.ID,
		FireAt: ring.In(now.Location()), About: occurrence.In(now.Location()), Rule: *t.RecurrenceRule, Anchor: *t.RecurrenceAnchor, Already: true}}, true, nil
}

// remindReferentOnly is remindReferent with an answer naming the asked
// reminder alone.
func (r captureRunner) remindReferentOnly(ctx context.Context, u unit.Unit, now time.Time) (CaptureResult, error) {
	c := asCapture(u, now)
	plans, armable := prospection.Arm(askedOnly(c), true, reminderPrefs(ctx, r.config), now)
	if !u.RemindMe {
		if err := r.units.SetRemindMe(ctx, u.ID, now); err != nil {
			return CaptureResult{}, fmt.Errorf("capture: mark unit %q asked: %w", u.ID, err)
		}
	}
	armed, err := r.triggers.ArmedForUnit(ctx, u.ID)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture: read reminders of unit %q: %w", u.ID, err)
	}
	var asked []prospection.Plan
	var already *prospection.Plan
	for _, p := range plans {
		if !armable || !p.Asked {
			continue
		}
		if armedAt(armed, p.FireAt) {
			already = &p
			continue
		}
		asked = append(asked, p)
	}
	switch {
	case len(asked) > 0:
		res, err := r.armAll(ctx, c, asked, now, &u.ID)
		if err != nil {
			return CaptureResult{}, err
		}
		res.UnitID = u.ID
		return res, nil
	case already != nil:
		p := *already
		return CaptureResult{Outcome: OutcomeArmed, UnitID: u.ID, Armed: &Armed{What: p.What, FireAt: p.FireAt, About: p.About,
			Immediate: p.Immediate, DayOnly: p.DayOnly, Rule: p.Rule, Anchor: p.Anchor}}, nil
	}
	why := plans[0].Why
	if err := r.recordOrphanDecision(ctx, ports.ActionCaptureArmRefused, refusalMessage(why), now, struct {
		Why    string `json:"why"`
		UnitID string `json:"unit_id"`
		Asked  bool   `json:"asked"`
	}{Why: string(why), UnitID: u.ID, Asked: true}); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{Outcome: OutcomeStored, UnitID: u.ID, ArmRefused: &ArmRefused{Why: why, Message: refusalMessage(why)}}, nil
}

// moveThenRemind is a remind-me that also moves the item it names ("es a
// las 11, recordámelo", ADR-0043 point 4): the move is applied first, through the
// correction path as any follow-up's is, and the asked reminder is then armed
// on the moved unit. The answer names both. A correction that asks instead of
// applying is answered as it is, and arms nothing: the item is not where the
// user said, so a reminder of it would be of the wrong time.
func (r captureRunner) moveThenRemind(ctx context.Context, in CaptureInput, c classify.Classification, u unit.Unit, now time.Time) (CaptureResult, error) {
	res, err := r.correct(ctx, in, c, &u, now)
	if err != nil || res.Outcome != OutcomeCorrected {
		return res, err
	}
	moved, err := r.units.ByID(ctx, u.ID)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture: read moved unit %q: %w", u.ID, err)
	}
	reminded, err := r.remindReferent(ctx, moved, now)
	if err != nil {
		return CaptureResult{}, err
	}
	res.Armed, res.ArmRefused = reminded.Armed, reminded.ArmRefused
	return res, nil
}

// asCapture is u read as the capture that told it, for prospection.Arm: its
// kind, its instants in the user's zone (now's), and an instant at local
// midnight read as a day alone, as the prompt shows it. The store hands
// instants back in UTC; a classification carries them in the user's frame,
// and the reply names the plan's About as it stands, so an instant left in
// UTC was answered at its UTC hour.
func asCapture(u unit.Unit, now time.Time) classify.Classification {
	kind := classify.Kind(u.Type)
	c := classify.Classification{Kind: &kind, EventAt: inZone(u.EventAt, now), DueAt: inZone(u.DueAt, now), NormalizedContent: &u.Content,
		EventAtDayOnly: atMidnight(u.EventAt, now), DueAtDayOnly: atMidnight(u.DueAt, now)}
	if u.Title != "" {
		c.Title = &u.Title
	}
	return c
}

// askedOnly is c read for its asked reminder alone. Arm gives an event, or a
// recurring reminder read as its occurrence, its own leads beside the asked
// one, and those are already armed; a task gets the asked lead and nothing
// else. So c is handed over as a task whose due instant is the one Arm would
// have asked about. Arming it alone keeps the refusal Arm reports the asked
// reminder's own ("too late") rather than none, when only an own lead would
// still fit, and never doubles an own lead the asked one lands on.
func askedOnly(c classify.Classification) classify.Classification {
	if k := *c.Kind; k == classify.KindEvent || k == classify.KindRecurringReminder {
		c.DueAt, c.EventAt = c.EventAt, c.DueAt
		c.DueAtDayOnly, c.EventAtDayOnly = c.EventAtDayOnly, c.DueAtDayOnly
	}
	task := classify.KindTask
	c.Kind = &task
	return c
}

// inZone is t in now's location, or nil.
func inZone(t *time.Time, now time.Time) *time.Time {
	if t == nil {
		return nil
	}
	local := t.In(now.Location())
	return &local
}

func atMidnight(t *time.Time, now time.Time) bool {
	if t == nil {
		return false
	}
	h, m, s := t.In(now.Location()).Clock()
	return h == 0 && m == 0 && s == 0 && t.Nanosecond() == 0
}

// armedAt reports whether an asked reminder is already armed at fireAt.
func armedAt(armed []ports.DueTrigger, fireAt time.Time) bool {
	for _, t := range armed {
		if t.Payload.Asked && t.FireAt.Equal(fireAt) {
			return true
		}
	}
	return false
}
