package brain

import (
	"context"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
)

// title is the night's title phase (doc 02 §6 item 8, ADR-0065): it names the
// live dated units that still have no title, the ones that will ring first
// ahead, at most consolidation.TitleBackfillPerPass of them. The decision of
// which units and in what order is consolidation.SelectToName's; the naming is
// the step capture and correction use, so the same rule text, the same
// ResolveTitle and the same "never over a title" write apply.
//
// A unit that could not be named is logged as consolidate.title.unnamed and the
// pass goes on: a provider error, an unusable answer and a failed title write are
// one unit's, not the night's. A failed read of the candidates or their reminders,
// a failed log row or a failed rewrite of a reminder's text is not, and aborts the
// phase as it does every other. A reminder rewritten after the title is a
// consolidate.title.reminder_retexted row, never a correction's. With no provider
// the phase writes nothing, as connect does.
func (r consolidateRunner) title(ctx context.Context, now time.Time) error {
	if r.namer == nil {
		return nil
	}
	us, err := r.units.UntitledDated(ctx)
	if err != nil {
		return fmt.Errorf("consolidate: title: read untitled dated units: %w", err)
	}
	cands := make([]consolidation.Untitled, len(us))
	byID := make(map[string]int, len(us))
	for i, u := range us {
		cands[i] = consolidation.Untitled{ID: u.ID, EventAt: u.EventAt, DueAt: u.DueAt}
		byID[u.ID] = i
		if r.triggers == nil {
			continue
		}
		armed, err := r.triggers.ArmedForUnit(ctx, u.ID)
		if err != nil {
			return fmt.Errorf("consolidate: title: read reminders of unit %q: %w", u.ID, err)
		}
		if len(armed) > 0 { // ordered by fire_at: the first is the soonest
			next := armed[0].FireAt
			cands[i].NextFire = &next
		}
	}

	namer := correctionRunner{units: r.units, log: r.log, ids: r.ids, triggers: r.triggers, llm: r.namer}
	for _, id := range consolidation.SelectToName(cands, now, consolidation.TitleBackfillPerPass) {
		u := us[byID[id]]
		title, why, err := namer.attemptName(ctx, u, ports.ActionTitleNamed, now)
		if err != nil {
			return fmt.Errorf("consolidate: title: %w", err)
		}
		if title != "" || why == "" {
			continue
		}
		if err := r.record(ctx, now, ports.ActionTitleUnnamed,
			fmt.Sprintf("asked for a title for the dated unit %q and got none: %s", id, why),
			struct {
				UnitID string `json:"unit_id"`
				Reason string `json:"reason"`
			}{UnitID: id, Reason: why}); err != nil {
			return err
		}
	}
	return nil
}
