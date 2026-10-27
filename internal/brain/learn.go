package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/learning"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// learn is the nightly learn slot (doc 02 §6 item 8, §9; ADR-0053): for each
// relation type with typed verdicts, learning.StepRelation decides from the
// type's verdicts and its stored state, and a change is written stamped with
// now, the pass's one instant, and recorded in one decision_log row. The rule
// reads only verdicts after that stamp, so the evidence of one night is never
// counted on another.
//
// A runner wired without a signal store (a test) learns nothing.
func (r consolidateRunner) learn(ctx context.Context, now time.Time) error {
	if r.signals == nil {
		return nil
	}
	signals, err := r.signals.RelationVerdicts(ctx)
	if err != nil {
		return fmt.Errorf("consolidate: learn: read relation verdicts: %w", err)
	}
	byType := map[string][]learning.Verdict{}
	for _, s := range signals {
		byType[*s.RelationType] = append(byType[*s.RelationType], verdictOf(s))
	}
	stored, err := r.rels.LearnedThresholds(ctx)
	if err != nil {
		return fmt.Errorf("consolidate: learn: read relation thresholds: %w", err)
	}
	rows := map[string]ports.LearnedThreshold{}
	for _, row := range stored {
		rows[row.RelationType] = row
	}

	types := make([]string, 0, len(byType))
	for relType := range byType {
		types = append(types, relType)
	}
	slices.Sort(types)
	for _, relType := range types {
		state := learning.TypeState{Current: relation.Resolve(nil)}
		if row, ok := rows[relType]; ok {
			state = learning.TypeState{Current: row.Thresholds, Pinned: row.Origin == ports.ThresholdOriginUser, AdjustedAt: row.AdjustedAt}
		}
		c, ok := learning.StepRelation(state, byType[relType], now)
		if !ok {
			continue
		}
		if err := r.rels.LearnThresholds(ctx, relType, c.Next, now); err != nil {
			if errors.Is(err, ports.ErrThresholdsPinned) {
				// Pinned between the read and the write: the user's word
				// wins, nothing changed, and nothing is recorded (I12).
				continue
			}
			return fmt.Errorf("consolidate: learn: write thresholds for %q: %w", relType, err)
		}
		if err := r.record(ctx, now, ports.ActionLearnRelationThreshold, learnRationale(relType, c), learnDetailOf(relType, c)); err != nil {
			return err
		}
	}
	return nil
}

// verdictOf reads one typed relation signal as the rule's evidence. The maker
// comes from the signal's context; one that cannot be read is empty, which
// the rule's allowlist does not count.
func verdictOf(s ports.Signal) learning.Verdict {
	var ctx struct {
		CreatedBy string `json:"created_by"`
	}
	_ = json.Unmarshal(s.Context, &ctx)
	return learning.Verdict{SignalID: s.ID, Rejected: s.Type == ports.SignalRelationReject,
		CreatedBy: relation.CreatedBy(ctx.CreatedBy), At: s.OccurredAt}
}

// learnRationale says what moved and why, in the words doc 02 §9 promises:
// "I raised the same_topic threshold 0.30→0.35 — you rejected 6 of 8".
func learnRationale(relType string, c learning.Change) string {
	verb, n := "rejected", c.Rejected
	if c.Direction == learning.Lowered {
		verb, n = "confirmed", c.Confirmed
	}
	out := fmt.Sprintf("learn: %s the %s bar %.2f → %.2f: you %s %d of the last %d %s links Nooma made",
		c.Direction, relType, c.Previous.Persist, c.Next.Persist, verb, n, len(c.SignalIDs), relType)
	if c.Next.Surface != c.Previous.Surface {
		out += fmt.Sprintf("; the surface bar followed it, %.2f → %.2f", c.Previous.Surface, c.Next.Surface)
	}
	return out
}

// learnThresholds is a thresholds pair as a learn row writes it.
type learnThresholds struct {
	Persist float64 `json:"persist"`
	Surface float64 `json:"surface"`
}

// learnDetail is the consolidate.learn.relation_threshold row's context: the
// change and the evidence it was decided on, signal ids newest first.
type learnDetail struct {
	RelationType string          `json:"relation_type"`
	Direction    string          `json:"direction"`
	Previous     learnThresholds `json:"previous"`
	Next         learnThresholds `json:"next"`
	Rejected     int             `json:"rejected"`
	Confirmed    int             `json:"confirmed"`
	SignalIDs    []string        `json:"signal_ids"`
}

func learnDetailOf(relType string, c learning.Change) learnDetail {
	return learnDetail{
		RelationType: relType, Direction: string(c.Direction),
		Previous: learnThresholds{Persist: c.Previous.Persist, Surface: c.Previous.Surface},
		Next:     learnThresholds{Persist: c.Next.Persist, Surface: c.Next.Surface},
		Rejected: c.Rejected, Confirmed: c.Confirmed, SignalIDs: c.SignalIDs,
	}
}
