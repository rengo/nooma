package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// openCheckins closes every open check-in prospection.CheckinStep expires and
// returns those this digest asks again, at most MaxReasksPerDigest, the longest
// waiting first (doc 02 §7). A dry run closes nothing.
//
// The re-ask count is the row's own (Reask writes it with the restamp); when
// the user last put a check-in off is read from the audit trail the digest
// already loaded, heldCounts' way. A check-in with no surfaced_at, or whose
// unit cannot be read this morning for any reason but its absence, is left
// for a morning that can decide it.
func (r checkRunner) openCheckins(ctx context.Context, history []ports.Decision, now time.Time, commit bool) ([]ports.DueTrigger, error) {
	open, err := r.triggers.Delivered(ctx)
	if err != nil {
		return nil, fmt.Errorf("check: open check-ins: %w", err)
	}
	snoozed := snoozedAt(history)

	var ask []ports.OpenCheckIn
	for _, c := range open {
		if c.SurfacedAt.IsZero() {
			continue
		}
		u, readable := r.checkinUnit(ctx, c.DueTrigger)
		if !readable {
			continue
		}
		step := r.night.CheckinStep(prospection.OpenCheckin{
			SurfacedAt: c.SurfacedAt, SnoozedAt: snoozed[c.ID], UnitType: u.Type, UnitStatus: u.Status, Reasks: c.Payload.Reasks,
		}, now)
		switch step {
		case prospection.CheckinWait:
		case prospection.CheckinReask:
			ask = append(ask, c)
		case prospection.CheckinExpire:
			if !commit {
				continue
			}
			if err := r.expireCheckin(ctx, c, now); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("check: no path for check-in step %q", step)
		}
	}
	// The longest waiting first, and no more than one digest asks.
	slices.SortFunc(ask, func(a, b ports.OpenCheckIn) int {
		if c := a.SurfacedAt.Compare(b.SurfacedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	out := make([]ports.DueTrigger, 0, min(len(ask), prospection.MaxReasksPerDigest))
	for _, c := range ask[:min(len(ask), prospection.MaxReasksPerDigest)] {
		out = append(out, c.DueTrigger)
	}
	return out, nil
}

// expireCheckin closes c unanswered and records it; a row another pass
// already moved is recorded as a conflict and skipped.
func (r checkRunner) expireCheckin(ctx context.Context, c ports.OpenCheckIn, now time.Time) error {
	if err := r.triggers.ExpireUnanswered(ctx, c.ID); err != nil {
		if skipped, serr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "trigger", c.ID, c.FireAt); skipped || serr != nil {
			return serr
		}
		return fmt.Errorf("check: close check-in %q: %w", c.ID, err)
	}
	return r.record(ctx, now, ports.ActionCheckCheckinExpired,
		fmt.Sprintf("check-in %q went unanswered and closed after %d re-ask(s); an answer no longer reaches it", c.ID, c.Payload.Reasks),
		checkDetail{ID: c.ID})
}

// checkinUnit is t's unit now, the zero unit when it has none or it is gone,
// and readable false when the read failed for another reason.
func (r checkRunner) checkinUnit(ctx context.Context, t ports.DueTrigger) (unit.Unit, bool) {
	if t.UnitID == nil || r.units == nil {
		return unit.Unit{}, true
	}
	u, err := r.units.ByID(ctx, *t.UnitID)
	switch {
	case errors.Is(err, ports.ErrUnitNotFound):
		return unit.Unit{}, true
	case err != nil:
		slog.Warn("a check-in's unit could not be read, so it is left for tomorrow's digest", "trigger", t.ID, "error", err)
		return unit.Unit{}, false
	}
	return u, true
}

// markReasked records that the digest just sent asked t again: Reask restamps
// surfaced_at at at, so an answer reaches it, and counts the re-ask in the
// same write; only then the audit row, whose failure can therefore grant no
// extra re-ask.
func (r checkRunner) markReasked(ctx context.Context, t ports.DueTrigger, at, now time.Time) error {
	if err := r.triggers.Reask(ctx, t.ID, at); err != nil {
		if skipped, serr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "trigger", t.ID, t.FireAt); skipped || serr != nil {
			return serr
		}
		return fmt.Errorf("check: digest was sent but check-in %q was not marked asked again: %w", t.ID, err)
	}
	return r.record(ctx, now, ports.ActionCheckCheckinReasked,
		fmt.Sprintf("the daily digest asked check-in %q again", t.ID), checkDetail{ID: t.ID})
}

// snoozedAt is when the user last put each check-in off, read from the
// capture.checkin.snoozed rows. A row whose context cannot be read is
// skipped, as heldCounts skips one: it only shortens one check-in's life.
func snoozedAt(history []ports.Decision) map[string]time.Time {
	at := map[string]time.Time{}
	for i := range history {
		if history[i].Action != ports.ActionCaptureCheckInSnoozed {
			continue
		}
		var row struct {
			TriggerID string `json:"trigger_id"`
		}
		if json.Unmarshal(history[i].Context, &row) == nil && row.TriggerID != "" && history[i].OccurredAt.After(at[row.TriggerID]) {
			at[row.TriggerID] = history[i].OccurredAt
		}
	}
	return at
}

// reaskSaid is each check-in asked again as the digest names it: its unit's
// title or content now, as a reminder says it, without an instant, which may
// be past (ADR-0032: a reminder never speaks of the past), or the armed text
// when the unit cannot be read.
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
