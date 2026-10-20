package brain

import (
	"context"
	"fmt"
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
// was set and why, as an asked capture's does.
func (r captureRunner) remindReferent(ctx context.Context, u unit.Unit, now time.Time) (CaptureResult, error) {
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
// kind, its instants, and an instant at local midnight read as a day alone,
// as the prompt shows it.
func asCapture(u unit.Unit, now time.Time) classify.Classification {
	kind := classify.Kind(u.Type)
	c := classify.Classification{Kind: &kind, EventAt: u.EventAt, DueAt: u.DueAt, NormalizedContent: &u.Content,
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
