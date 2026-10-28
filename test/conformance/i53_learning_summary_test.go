package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I53 (doc 02 §9, ADR-0056): what the learner learned is shown as one
// sentence per value, built from the decision rows; an undo restores the
// previous value and pins it, and the learner leaves it alone; a release
// hands it back with the evidence reset; nothing is deleted.

func valueOf(t *testing.T, svc *brain.LearningService, kind brain.LearnedKind, key string) brain.LearnedValue {
	t.Helper()
	view, err := svc.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range view.Values {
		if v.Kind == kind && v.Key == key {
			return v
		}
	}
	t.Fatalf("the summary shows no %s %q: %+v", kind, key, view.Values)
	return brain.LearnedValue{}
}

func rowsOf(t *testing.T, log interface {
	Since(context.Context, time.Time, int) ([]ports.Decision, error)
}, action ports.DecisionAction) int {
	t.Helper()
	all, err := log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range all {
		if d.Action == action {
			n++
		}
	}
	return n
}

// freshRejects records n typed rejections of Nooma's same_topic links at at.
func (v *i48Vault) freshRejects(t *testing.T, n int, at time.Time) {
	t.Helper()
	typ, kind := "same_topic", ports.TargetKindRelation
	for i := range n {
		id := fmt.Sprintf("fresh-%s-%d", at.Format("0102"), i)
		if err := v.signals.Record(context.Background(), ports.Signal{ID: id, Type: ports.SignalRelationReject, Valence: ports.ValenceNegative,
			TargetKind: &kind, TargetID: &id, RelationType: &typ, Context: json.RawMessage(`{"created_by":"consolidation","confidence":0.4}`),
			OccurredAt: at.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestI53_AnUndoneRelationBarIsPinnedAndAReleaseResetsItsEvidence(t *testing.T) {
	v := newI48Vault(t)
	v.judgeSix(t)
	v.learn(t, i48Night)
	svc := func(now time.Time) *brain.LearningService {
		return brain.NewLearningService(fixedClock{now: now}, v.ids, v.rels, v.cfg, v.log)
	}

	got := valueOf(t, svc(i48Night), brain.LearnedRelationBar, "same_topic")
	if got.Pinned || !got.CanUndo || got.CanRelease || got.Value != "0.35" ||
		!strings.Contains(got.Sentence, "raised") || !strings.Contains(got.Sentence, "0.30") || !strings.Contains(got.Sentence, "6 of the last 8") {
		t.Fatalf("before the undo: %+v", got)
	}

	signalsBefore := len(signalsOf(t, v.signals))
	undone := i48Night.Add(time.Hour)
	if err := svc(undone).Undo(context.Background(), brain.LearnedRelationBar, "same_topic"); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if bar := v.bar(t, "same_topic"); bar != (relation.Thresholds{Persist: 0.30, Surface: 0.50}) {
		t.Fatalf("after the undo the bar is %+v, want the previous 0.30/0.50", bar)
	}
	got = valueOf(t, svc(undone), brain.LearnedRelationBar, "same_topic")
	if !got.Pinned || got.CanUndo || !got.CanRelease || !strings.Contains(got.Sentence, "undid") {
		t.Errorf("after the undo: %+v", got)
	}
	if n := rowsOf(t, v.log, ports.ActionLearningUndone); n != 1 {
		t.Errorf("%d learning.undone rows, want 1", n)
	}

	// The user decided: six fresh rejections a week later move nothing.
	v.freshRejects(t, 6, i48Night.AddDate(0, 0, 7))
	v.learn(t, i48Night.AddDate(0, 0, 8))
	if bar := v.bar(t, "same_topic"); bar.Persist != 0.30 {
		t.Errorf("the learner moved an undone, pinned bar to %+v", bar)
	}

	// Released, it is the learner's again, but what came before the release
	// is not counted: the same six rejections move nothing.
	released := i48Night.AddDate(0, 0, 9)
	if err := svc(released).Release(context.Background(), brain.LearnedRelationBar, "same_topic"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got = valueOf(t, svc(released), brain.LearnedRelationBar, "same_topic")
	if got.Pinned || got.CanRelease || !strings.Contains(got.Sentence, "learn") {
		t.Errorf("after the release: %+v", got)
	}
	for _, night := range []time.Time{released, released.AddDate(0, 0, 8)} {
		v.learn(t, night)
	}
	if bar := v.bar(t, "same_topic"); bar.Persist != 0.30 {
		t.Errorf("evidence from before the release moved the bar to %+v", bar)
	}
	if n := rowsOf(t, v.log, ports.ActionLearningReleased); n != 1 {
		t.Errorf("%d learning.released rows, want 1", n)
	}
	// Nothing is deleted: every signal is still there, and the row too.
	if n := len(signalsOf(t, v.signals)); n != signalsBefore+6 {
		t.Errorf("%d signals, want the %d before plus the 6 fresh", n, signalsBefore)
	}
	if rows, err := v.rels.LearnedThresholds(context.Background()); err != nil || len(rows) != 1 {
		t.Errorf("thresholds rows = %+v, %v; want the same_topic row kept", rows, err)
	}
}

func TestI53_AnUndoneGoalCadenceIsPinnedAndAReleaseResetsItsEvidence(t *testing.T) {
	v := newI52Vault()
	v.closed(t, "b-run", repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)
	// Its own ids, apart from the passes' counters, which restart at id-1.
	ids := &counterIDs{n: 1000}
	svc := func(now time.Time) *brain.LearningService {
		return brain.NewLearningService(fixedClock{now: now}, ids, nil, v.cfg, v.log)
	}
	got := valueOf(t, svc(i52Night), brain.LearnedGoalCadence, brain.GoalCadenceKey)
	if got.Value != "28 days" || got.Pinned || !got.CanUndo || !strings.Contains(got.Sentence, "5 of the last 5") {
		t.Fatalf("before the undo: %+v", got)
	}
	if err := svc(i52Night.Add(time.Hour)).Undo(context.Background(), brain.LearnedGoalCadence, brain.GoalCadenceKey); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	cfg, _ := v.cfg.Load(context.Background())
	if v.days(t) != 21 || !cfg.GoalStagnationPinned {
		t.Fatalf("after the undo: %d days, pinned %v; want 21, pinned", v.days(t), cfg.GoalStagnationPinned)
	}
	// Five ignored after the change, past its cooldown: without the pin they
	// would move it.
	v.closedBefore(t, "b-later", i52Night.AddDate(0, 0, 30), repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night.AddDate(0, 0, 30), nil, nil)
	if v.days(t) != 21 {
		t.Errorf("the learner moved an undone goal cadence to %d", v.days(t))
	}
	released := i52Night.AddDate(0, 0, 31)
	if err := svc(released).Release(context.Background(), brain.LearnedGoalCadence, brain.GoalCadenceKey); err != nil {
		t.Fatalf("Release: %v", err)
	}
	v.run(t, consolidation.PhaseLearn, released.AddDate(0, 0, 22), nil, nil)
	if v.days(t) != 21 {
		t.Errorf("evidence from before the release moved the cadence to %d", v.days(t))
	}
	if rowsOf(t, v.log, ports.ActionLearningUndone) != 1 || rowsOf(t, v.log, ports.ActionLearningReleased) != 1 {
		t.Error("want one learning.undone and one learning.released row")
	}
}

// TestI53_NothingToUndoOrReleaseWritesNothing: a value never learned has
// nothing to undo, an unpinned one nothing to release; both refuse, with no
// row (I12).
func TestI53_NothingToUndoOrReleaseWritesNothing(t *testing.T) {
	v := newI52Vault()
	svc := brain.NewLearningService(fixedClock{now: i52Night}, &counterIDs{}, nil, v.cfg, v.log)
	if err := svc.Undo(context.Background(), brain.LearnedGoalCadence, brain.GoalCadenceKey); !errors.Is(err, brain.ErrNothingToUndo) {
		t.Errorf("Undo of a value never learned = %v, want ErrNothingToUndo", err)
	}
	if err := svc.Release(context.Background(), brain.LearnedGoalCadence, brain.GoalCadenceKey); !errors.Is(err, ports.ErrNotPinned) {
		t.Errorf("Release of an unpinned value = %v, want ErrNotPinned", err)
	}
	if err := svc.Undo(context.Background(), "some_kind", "x"); !errors.Is(err, brain.ErrUnknownLearnedValue) {
		t.Errorf("Undo of an unknown kind = %v, want ErrUnknownLearnedValue", err)
	}
	if all, _ := v.log.Since(context.Background(), time.Time{}, -1); len(all) != 0 {
		t.Errorf("refusals wrote %d rows", len(all))
	}
}

// learnedGoal is an i52 vault whose learner moved the cadence 21 → 28, and a
// summary over it at now.
func learnedGoal(t *testing.T) (*i52Vault, func(now time.Time) *brain.LearningService) {
	t.Helper()
	v := newI52Vault()
	v.closed(t, "b-run", repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)
	ids := &counterIDs{n: 1000}
	return v, func(now time.Time) *brain.LearningService {
		return brain.NewLearningService(fixedClock{now: now}, ids, nil, v.cfg, v.log)
	}
}

// refusesUndo asserts the value offers no undo, and that an undo refuses with
// ErrNothingToUndo and writes no row.
func refusesUndo(t *testing.T, svc *brain.LearningService, log *memrepo.DecisionLog, kind brain.LearnedKind, key string) {
	t.Helper()
	if got := valueOf(t, svc, kind, key); got.CanUndo {
		t.Errorf("undo offered: %+v", got)
	}
	before, _ := log.Since(context.Background(), time.Time{}, -1)
	if err := svc.Undo(context.Background(), kind, key); !errors.Is(err, brain.ErrNothingToUndo) {
		t.Errorf("Undo = %v, want ErrNothingToUndo", err)
	}
	if after, _ := log.Since(context.Background(), time.Time{}, -1); len(after) != len(before) {
		t.Errorf("a refused undo wrote %d rows", len(after)-len(before))
	}
}

// TestI53_AnAdminSaveIsTheNewestWordOnTheGoalCadence: an Admin save after the
// learner's change is what the summary says, and the learner's older change
// is no longer undoable, even once the save is released.
func TestI53_AnAdminSaveIsTheNewestWordOnTheGoalCadence(t *testing.T) {
	saved := i52Night.Add(time.Hour)
	admin := func(v *i52Vault, raw string) {
		t.Helper()
		svc := brain.NewAdminService(fixedClock{now: saved}, &counterIDs{n: 2000}, v.cfg, memrepo.NewRelations(), v.log)
		if _, err := svc.Update(context.Background(), brain.ConfigGoalStagnationDays, raw); err != nil {
			t.Fatal(err)
		}
	}

	v, svc := learnedGoal(t)
	admin(v, "35")
	got := valueOf(t, svc(saved), brain.LearnedGoalCadence, brain.GoalCadenceKey)
	if got.Value != "35 days" || !got.Pinned || !strings.Contains(got.Sentence, "35") || strings.Contains(got.Sentence, "Nooma made") {
		t.Errorf("after an Admin save of 35: %+v", got)
	}
	refusesUndo(t, svc(saved), v.log, brain.LearnedGoalCadence, brain.GoalCadenceKey)

	v, svc = learnedGoal(t)
	admin(v, "28")
	released := saved.Add(time.Hour)
	if err := svc(released).Release(context.Background(), brain.LearnedGoalCadence, brain.GoalCadenceKey); err != nil {
		t.Fatalf("Release: %v", err)
	}
	refusesUndo(t, svc(released), v.log, brain.LearnedGoalCadence, brain.GoalCadenceKey)
	if v.days(t) != 28 {
		t.Errorf("the user's 28 became %d", v.days(t))
	}
}

// TestI53_UndoRefusesAValueMovedOrPinnedSinceTheLearnersChange: ADR-0056's
// guard. A value that is no longer the learner's change, or that is pinned
// (an act whose row never landed), has nothing to undo.
func TestI53_UndoRefusesAValueMovedOrPinnedSinceTheLearnersChange(t *testing.T) {
	ctx := context.Background()
	moved := i48Night.Add(time.Hour)
	for name, after := range map[string]func(*i48Vault) error{
		"bar moved": func(v *i48Vault) error {
			return v.rels.LearnThresholds(ctx, "same_topic", relation.Thresholds{Persist: 0.40, Surface: 0.50}, moved)
		},
		"bar pinned": func(v *i48Vault) error {
			return v.rels.PinThresholds(ctx, "same_topic", relation.Thresholds{Persist: 0.35, Surface: 0.50}, moved)
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := newI48Vault(t)
			v.judgeSix(t)
			v.learn(t, i48Night)
			if err := after(v); err != nil {
				t.Fatal(err)
			}
			want := v.bar(t, "same_topic")
			refusesUndo(t, brain.NewLearningService(fixedClock{now: moved}, v.ids, v.rels, v.cfg, v.log), v.log, brain.LearnedRelationBar, "same_topic")
			if bar := v.bar(t, "same_topic"); bar != want {
				t.Errorf("a refused undo moved the bar to %+v", bar)
			}
		})
	}
	for name, after := range map[string]func(*i52Vault) error{
		"cadence moved": func(v *i52Vault) error {
			if err := v.cfg.SetGoalStagnationDays(ctx, 35, moved); err != nil {
				return err
			}
			return v.cfg.ReleaseGoalStagnationDays(ctx, moved)
		},
		"cadence pinned": func(v *i52Vault) error { return v.cfg.SetGoalStagnationDays(ctx, 28, moved) },
	} {
		t.Run(name, func(t *testing.T) {
			v, svc := learnedGoal(t)
			if err := after(v); err != nil {
				t.Fatal(err)
			}
			want := v.days(t)
			refusesUndo(t, svc(moved), v.log, brain.LearnedGoalCadence, brain.GoalCadenceKey)
			if v.days(t) != want {
				t.Errorf("a refused undo moved the cadence to %d", v.days(t))
			}
		})
	}
}

// TestI53_AnUndoRestoresTheSurfaceBarTheLearnerLifted: a raise past the
// surface bar lifts it too, and an undo restores both.
func TestI53_AnUndoRestoresTheSurfaceBarTheLearnerLifted(t *testing.T) {
	v := newI48Vault(t)
	v.rels.SeedThreshold(t, "same_topic", 0.35, 0.35)
	v.judgeSix(t)
	v.learn(t, i48Night)
	if bar := v.bar(t, "same_topic"); bar != (relation.Thresholds{Persist: 0.40, Surface: 0.40}) {
		t.Fatalf("the learner set %+v, want 0.40/0.40", bar)
	}
	svc := brain.NewLearningService(fixedClock{now: i48Night.Add(time.Hour)}, v.ids, v.rels, v.cfg, v.log)
	if err := svc.Undo(context.Background(), brain.LearnedRelationBar, "same_topic"); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if bar := v.bar(t, "same_topic"); bar != (relation.Thresholds{Persist: 0.35, Surface: 0.35}) {
		t.Errorf("after the undo the bar is %+v, want 0.35/0.35", bar)
	}
}

// TestI53_InOneSecondTheLaterRowIsTheNewest: rows of one second are ordered
// by write order, so an undo, then a release, written the second the learner
// changed the bar, each read as the newest word.
func TestI53_InOneSecondTheLaterRowIsTheNewest(t *testing.T) {
	v := newI48Vault(t)
	v.judgeSix(t)
	v.learn(t, i48Night)
	svc := brain.NewLearningService(fixedClock{now: i48Night}, v.ids, v.rels, v.cfg, v.log)
	if err := svc.Undo(context.Background(), brain.LearnedRelationBar, "same_topic"); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := valueOf(t, svc, brain.LearnedRelationBar, "same_topic"); !strings.Contains(got.Sentence, "undid") {
		t.Errorf("an undo the learner's second reads: %+v", got)
	}
	if err := svc.Release(context.Background(), brain.LearnedRelationBar, "same_topic"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := valueOf(t, svc, brain.LearnedRelationBar, "same_topic"); !strings.Contains(got.Sentence, "learn the same_topic link bar again") || got.CanUndo {
		t.Errorf("a release the same second reads: %+v", got)
	}
}
