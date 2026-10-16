package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// openCheckins closes every open check-in prospection.CheckinStep expires and
// returns those this digest asks again (doc 02 §7). A dry run closes nothing.
//
// What the decision needs that the row does not hold is read from the audit
// trail the digest already loaded, heldCounts' own way: whether a snooze put
// it off (capture.checkin.snoozed) and how many digests asked it again
// (check.checkin.reasked). The history window covers both: a check-in is
// asked again at the next digest after a snooze, and re-asks are consecutive.
func (r checkRunner) openCheckins(ctx context.Context, history []ports.Decision, now time.Time, commit bool) ([]ports.DueTrigger, error) {
	open, err := r.triggers.Delivered(ctx)
	if err != nil {
		return nil, fmt.Errorf("check: open check-ins: %w", err)
	}
	snoozed, reasks := checkinHistory(history)

	var ask []ports.DueTrigger
	for _, c := range open {
		step := r.night.CheckinStep(prospection.OpenCheckin{
			SurfacedAt: c.SurfacedAt, UnitType: r.checkinUnitType(ctx, c.DueTrigger), Snoozed: snoozed[c.ID], Reasks: reasks[c.ID],
		}, now)
		switch step {
		case prospection.CheckinWait:
		case prospection.CheckinReask:
			ask = append(ask, c.DueTrigger)
		case prospection.CheckinExpire:
			if !commit {
				continue
			}
			if err := r.triggers.ExpireUnanswered(ctx, c.ID); err != nil {
				if skipped, serr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "trigger", c.ID, c.FireAt); skipped || serr != nil {
					if serr != nil {
						return nil, serr
					}
					continue
				}
				return nil, fmt.Errorf("check: close check-in %q: %w", c.ID, err)
			}
			if err := r.record(ctx, now, ports.ActionCheckCheckinExpired,
				fmt.Sprintf("check-in %q went unanswered and closed after %d re-ask(s); an answer no longer reaches it", c.ID, reasks[c.ID]),
				checkDetail{ID: c.ID}); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("check: no path for check-in step %q", step)
		}
	}
	return ask, nil
}

// checkinUnitType is the kind of t's unit, or empty when it has none or it
// cannot be read: a check-in whose words cannot be read is not asked again.
func (r checkRunner) checkinUnitType(ctx context.Context, t ports.DueTrigger) unit.Type {
	if u, ok := readUnit(ctx, r.units, t); ok {
		return u.Type
	}
	return ""
}

// markReasked records that the digest just sent asked id again: surfaced_at
// restamped, so an answer reaches it, and the row that counts the re-ask.
func (r checkRunner) markReasked(ctx context.Context, t ports.DueTrigger, now time.Time) error {
	if err := r.triggers.Reask(ctx, t.ID, now); err != nil {
		if skipped, serr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "trigger", t.ID, t.FireAt); skipped || serr != nil {
			return serr
		}
		return fmt.Errorf("check: digest was sent but check-in %q was not marked asked again: %w", t.ID, err)
	}
	return r.record(ctx, now, ports.ActionCheckCheckinReasked,
		fmt.Sprintf("the daily digest asked check-in %q again", t.ID), checkDetail{ID: t.ID})
}

// checkinHistory reads which check-ins a snooze put off and how many times
// each was asked again. A row whose context cannot be read is skipped, as
// heldCounts skips one: it only delays or shortens one check-in's life.
func checkinHistory(history []ports.Decision) (snoozed map[string]bool, reasks map[string]int) {
	snoozed, reasks = map[string]bool{}, map[string]int{}
	for i := range history {
		switch history[i].Action {
		case ports.ActionCaptureCheckInSnoozed:
			var row struct {
				TriggerID string `json:"trigger_id"`
			}
			if json.Unmarshal(history[i].Context, &row) == nil && row.TriggerID != "" {
				snoozed[row.TriggerID] = true
			}
		case ports.ActionCheckCheckinReasked:
			var detail checkDetail
			if unmarshalCheckDetail(history[i].Context, &detail) == nil && detail.ID != "" {
				reasks[detail.ID]++
			}
		}
	}
	return snoozed, reasks
}

// reaskSaid is each check-in asked again as the digest says it: its unit's
// words now, without an instant, which may be past (ADR-0032: a reminder never
// speaks of the past), or the armed text when the unit cannot be read.
func reaskSaid(ctx context.Context, units ports.UnitRepo, reasks []ports.DueTrigger, now time.Time, set phrase.Set) []ports.DueTrigger {
	out := make([]ports.DueTrigger, len(reasks))
	for i, t := range reasks {
		out[i] = t
		if u, ok := readUnit(ctx, units, t); ok {
			out[i].Payload.ActionText, _ = unitWords(u, t, now, set)
		} else {
			out[i].Payload.ActionText = clip(t.Payload.ActionText)
		}
	}
	return out
}
