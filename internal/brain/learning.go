package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// LearnedKind is what the learner tunes (doc 02 §9): a relation type's bar,
// keyed by type, or the goal cadence, keyed by GoalCadenceKey.
type LearnedKind string

const (
	LearnedRelationBar LearnedKind = "relation_bar"
	LearnedGoalCadence LearnedKind = "goal_cadence"
	GoalCadenceKey                 = "goal_stagnation_days"
	// learningHistory bounds the rows of each prefix read: years of changes.
	learningHistory = 500
)

var (
	// ErrNothingToUndo: the value in force is not the learner's last change,
	// or it is pinned already.
	ErrNothingToUndo       = errors.New("learning: nothing to undo")
	ErrUnknownLearnedValue = errors.New("learning: no such learned value")
)

// LearnedValue is one learned value as the summary shows it: the value in
// force, its pin, one sentence built from the decision rows, when that
// sentence's change happened (nil for none), and what can be done to it.
type LearnedValue struct {
	Kind                LearnedKind
	Key, Value          string
	Pinned              bool
	Sentence            string
	At                  *time.Time
	CanUndo, CanRelease bool
}

// LearningView is every relation type's bar, then the goal cadence.
type LearningView struct{ Values []LearnedValue }

// LearningService is the summary of what was learned and its two acts (doc 02
// §9, ADR-0056): undo restores and pins; release unpins with the evidence
// before it set aside. Nothing is deleted.
type LearningService struct {
	clock ports.Clock
	ids   ports.IDGen
	rels  ports.RelationRepo
	cfg   ports.ConfigRepo
	log   ports.DecisionLog
}

// NewLearningService wires the summary; a nil rels shows no relation bar.
func NewLearningService(clock ports.Clock, ids ports.IDGen, rels ports.RelationRepo, cfg ports.ConfigRepo, log ports.DecisionLog) *LearningService {
	return &LearningService{clock: clock, ids: ids, rels: rels, cfg: cfg, log: log}
}

// learnEvent is one decision row about one learned value.
type learnEvent struct {
	action ports.DecisionAction
	at     time.Time
	ctx    json.RawMessage
}

// summaryDetail is a learning.undone or learning.released row's context.
type summaryDetail struct {
	Kind     LearnedKind `json:"kind"`
	Key      string      `json:"key"`
	Previous any         `json:"previous,omitempty"`
	Next     any         `json:"next,omitempty"`
}

// history is every learn, summary and Admin save row about a learned value,
// newest first, by value.
func (s *LearningService) history(ctx context.Context) (map[LearnedKind]map[string][]learnEvent, error) {
	out := map[LearnedKind]map[string][]learnEvent{LearnedRelationBar: {}, LearnedGoalCadence: {}}
	var rows []ports.DecisionRow
	for _, prefix := range []string{"consolidate.learn.", "learning.", string(ports.ActionConfigUpdated)} {
		got, err := s.log.Before(ctx, nil, prefix, learningHistory)
		if err != nil {
			return nil, fmt.Errorf("learning: read history: %w", err)
		}
		rows = append(rows, got...)
	}
	slices.SortStableFunc(rows, func(a, b ports.DecisionRow) int { return b.OccurredAt.Compare(a.OccurredAt) })
	for _, r := range rows {
		kind, key := LearnedKind(""), ""
		switch r.Action {
		case ports.ActionLearnRelationThreshold:
			var d learnDetail
			_ = json.Unmarshal(r.Context, &d)
			kind, key = LearnedRelationBar, d.RelationType
		case ports.ActionLearnGoalCadence:
			kind, key = LearnedGoalCadence, GoalCadenceKey
		case ports.ActionConfigUpdated:
			var d struct{ Fields []string }
			if _ = json.Unmarshal(r.Context, &d); slices.Contains(d.Fields, GoalCadenceKey) {
				kind, key = LearnedGoalCadence, GoalCadenceKey
			}
		case ports.ActionLearningUndone, ports.ActionLearningReleased:
			var d summaryDetail
			_ = json.Unmarshal(r.Context, &d)
			kind, key = d.Kind, d.Key
		}
		if byKey, ok := out[kind]; ok && key != "" {
			byKey[key] = append(byKey[key], learnEvent{action: r.Action, at: r.OccurredAt, ctx: r.Context})
		}
	}
	return out, nil
}

// lastLearned is the learner's change when it is the newest event: after the
// user's undo, release or Admin save there is nothing of the learner's to undo.
func lastLearned(events []learnEvent) (learnEvent, bool) {
	if len(events) > 0 && (events[0].action == ports.ActionLearnRelationThreshold || events[0].action == ports.ActionLearnGoalCadence) {
		return events[0], true
	}
	return learnEvent{}, false
}

// View is the summary. It writes nothing.
func (s *LearningService) View(ctx context.Context) (LearningView, error) {
	hist, err := s.history(ctx)
	if err != nil {
		return LearningView{}, err
	}
	var view LearningView
	if s.rels != nil {
		rows, err := s.rels.LearnedThresholds(ctx)
		if err != nil {
			return LearningView{}, fmt.Errorf("learning: read relation bars: %w", err)
		}
		for _, row := range rows {
			view.Values = append(view.Values, relationValue(row, hist[LearnedRelationBar][row.RelationType]))
		}
	}
	cfg, err := s.cfg.Load(ctx)
	if err != nil {
		return LearningView{}, fmt.Errorf("learning: read config: %w", err)
	}
	view.Values = append(view.Values, goalValue(cfg, hist[LearnedGoalCadence][GoalCadenceKey]))
	return view, nil
}

func relationValue(row ports.LearnedThreshold, events []learnEvent) LearnedValue {
	v := LearnedValue{Kind: LearnedRelationBar, Key: row.RelationType, Value: fmt.Sprintf("%.2f", row.Persist),
		Pinned: row.Origin == ports.ThresholdOriginUser}
	v.CanRelease = v.Pinned
	if learned, ok := lastLearned(events); ok && !v.Pinned {
		var d learnDetail
		_ = json.Unmarshal(learned.ctx, &d)
		v.CanUndo = d.Next.Persist == row.Persist && d.Next.Surface == row.Surface
	}
	if len(events) == 0 {
		v.Sentence = fmt.Sprintf("The %s link bar is %s.", row.RelationType, v.Value)
		if v.Pinned {
			v.Sentence = fmt.Sprintf("The %s link bar is %s, set by you.", row.RelationType, v.Value)
		}
		return v
	}
	e := events[0]
	v.At = &e.at
	switch e.action {
	case ports.ActionLearningUndone:
		v.Sentence = fmt.Sprintf("You undid Nooma's last change to the %s link bar, so it stays at %s until you let Nooma learn it again.", row.RelationType, v.Value)
	case ports.ActionLearningReleased:
		v.Sentence = fmt.Sprintf("You let Nooma learn the %s link bar again; it counts only what you do from then on.", row.RelationType)
	default:
		var d learnDetail
		_ = json.Unmarshal(e.ctx, &d)
		verb, n := "rejected", d.Rejected
		if d.Direction == "lowered" {
			verb, n = "confirmed", d.Confirmed
		}
		v.Sentence = fmt.Sprintf("Nooma %s the %s link bar from %.2f to %.2f because you %s %d of the last %d %s links it made.",
			d.Direction, row.RelationType, d.Previous.Persist, d.Next.Persist, verb, n, len(d.SignalIDs), row.RelationType)
	}
	return v
}

func goalValue(cfg ports.VaultConfig, events []learnEvent) LearnedValue {
	days := consolidation.ResolveGoalStagnationDays(cfg.GoalStagnationDays)
	v := LearnedValue{Kind: LearnedGoalCadence, Key: GoalCadenceKey, Value: fmt.Sprintf("%d days", days),
		Pinned: cfg.GoalStagnationPinned, CanRelease: cfg.GoalStagnationPinned}
	if learned, ok := lastLearned(events); ok && !v.Pinned {
		var d goalCadenceDetail
		_ = json.Unmarshal(learned.ctx, &d)
		v.CanUndo = d.Next == days
	}
	if len(events) == 0 {
		v.Sentence = fmt.Sprintf("Goal check-ins wait %d days.", days)
		if v.Pinned {
			v.Sentence = fmt.Sprintf("Goal check-ins wait %d days, as you set them in Admin.", days)
		}
		return v
	}
	e := events[0]
	v.At = &e.at
	switch e.action {
	case ports.ActionLearningUndone:
		v.Sentence = fmt.Sprintf("You undid Nooma's last change to how long goal check-ins wait, so it stays at %d days until you let Nooma learn it again.", days)
	case ports.ActionLearningReleased:
		v.Sentence = "You let Nooma learn how long goal check-ins wait again; it counts only what you do from then on."
	case ports.ActionConfigUpdated:
		v.Sentence = fmt.Sprintf("You set goal check-ins to wait %d days in Admin.", days)
	default:
		var d goalCadenceDetail
		_ = json.Unmarshal(e.ctx, &d)
		verb, n := "ignored", d.Ignored
		if d.Direction == "lowered" {
			verb, n = "answered", d.Engaged
		}
		v.Sentence = fmt.Sprintf("Nooma made goal check-ins wait %d days instead of %d because you %s %d of the last %d.",
			d.Next, d.Previous, verb, n, len(d.SignalIDs))
	}
	return v
}

// Undo restores the value before the learner's last change and pins it
// (ADR-0056); ErrNothingToUndo writes nothing.
func (s *LearningService) Undo(ctx context.Context, kind LearnedKind, key string) error {
	if err := knownValue(kind, key, s.rels != nil); err != nil {
		return err
	}
	hist, err := s.history(ctx)
	if err != nil {
		return err
	}
	view, err := s.View(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(view.Values, func(v LearnedValue) bool { return v.Kind == kind && v.Key == key })
	if i < 0 || !view.Values[i].CanUndo {
		return ErrNothingToUndo
	}
	learned, _ := lastLearned(hist[kind][key])
	now := s.now()
	var detail summaryDetail
	if kind == LearnedGoalCadence {
		var d goalCadenceDetail
		_ = json.Unmarshal(learned.ctx, &d)
		if err := s.cfg.PinGoalStagnationDays(ctx, d.Previous, now); err != nil {
			return fmt.Errorf("learning: undo %s: %w", key, err)
		}
		detail = summaryDetail{Kind: kind, Key: key, Previous: d.Next, Next: d.Previous}
	} else {
		var d learnDetail
		_ = json.Unmarshal(learned.ctx, &d)
		back := relation.Thresholds{Persist: d.Previous.Persist, Surface: d.Previous.Surface}
		if err := s.rels.PinThresholds(ctx, key, back, now); err != nil {
			return fmt.Errorf("learning: undo %s: %w", key, err)
		}
		detail = summaryDetail{Kind: kind, Key: key, Previous: d.Next, Next: d.Previous}
	}
	return s.record(ctx, ports.ActionLearningUndone, fmt.Sprintf("the user undid the learner's last change to %s and pinned the value before it", key), detail, now)
}

// Release hands a pinned value back to the learner, the evidence before it
// set aside (ADR-0056); ports.ErrNotPinned writes nothing.
func (s *LearningService) Release(ctx context.Context, kind LearnedKind, key string) error {
	if err := knownValue(kind, key, s.rels != nil); err != nil {
		return err
	}
	now := s.now()
	var err error
	if kind == LearnedGoalCadence {
		err = s.cfg.ReleaseGoalStagnationDays(ctx, now)
	} else {
		err = s.rels.ReleaseThresholds(ctx, key, now)
	}
	if err != nil {
		return fmt.Errorf("learning: release %s: %w", key, err)
	}
	return s.record(ctx, ports.ActionLearningReleased, fmt.Sprintf("the user let Nooma learn %s again, counting only what comes after", key),
		summaryDetail{Kind: kind, Key: key}, now)
}

// now is the one instant an act is written at: the file's one clock read.
func (s *LearningService) now() time.Time { return s.clock.Now() }

// knownValue refuses a kind the summary does not show.
func knownValue(kind LearnedKind, key string, haveRelations bool) error {
	switch {
	case kind == LearnedGoalCadence && key == GoalCadenceKey, kind == LearnedRelationBar && key != "" && haveRelations:
		return nil
	}
	return ErrUnknownLearnedValue
}

// record writes the act's row after the act landed (I12).
func (s *LearningService) record(ctx context.Context, action ports.DecisionAction, rationale string, detail summaryDetail, now time.Time) error {
	contextJSON, err := json.Marshal(detail)
	if err == nil {
		err = s.log.Record(ctx, ports.Decision{ID: s.ids.New(), Action: action, Rationale: rationale, Context: contextJSON, OccurredAt: now})
	}
	if err != nil {
		return &WriteLandedError{Record: true, Err: fmt.Errorf("learning: record %s: %w", action, err)}
	}
	return nil
}
