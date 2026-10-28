package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I52 (doc 02 §9, ADR-0055): the nightly learn slot moves goal_stagnation_days
// from how goal check-ins closed, never writes a value the user pinned from
// Admin, and the decline factor follows whatever interval is in force.

var i52Night = time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)

type i52Vault struct {
	cfg     *memrepo.Config
	signals *memrepo.Signals
	log     *memrepo.DecisionLog
}

func newI52Vault() *i52Vault {
	return &i52Vault{cfg: memrepo.NewConfig(), signals: memrepo.NewSignals(), log: memrepo.NewDecisionLog()}
}

// closed records one closed goal check-in per type, a day apart, the newest a
// day before the night; belief "" records a nudge about no goal.
func (v *i52Vault) closed(t *testing.T, belief string, types ...ports.SignalType) {
	t.Helper()
	v.closedBefore(t, belief, i52Night, types...)
}

// closedBefore is closed with the newest a day before anchor.
func (v *i52Vault) closedBefore(t *testing.T, belief string, anchor time.Time, types ...ports.SignalType) {
	t.Helper()
	kind := ports.TargetKindTrigger
	for i, typ := range types {
		target := fmt.Sprintf("trg-%s-%d", belief, i)
		ctx, _ := json.Marshal(map[string]string{"belief_id": belief})
		if belief == "" {
			ctx = []byte(`{"unit_id":"u"}`)
		}
		if err := v.signals.Record(context.Background(), ports.Signal{ID: fmt.Sprintf("s-%s-%d", belief, i), Type: typ,
			Valence: ports.ValenceNegative, TargetKind: &kind, TargetID: &target, Context: ctx,
			OccurredAt: anchor.Add(-time.Duration(len(types)-i) * 24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
}

func (v *i52Vault) run(t *testing.T, phase consolidation.Phase, now time.Time, selfModel *memrepo.SelfModel, triggers *memrepo.Triggers) {
	t.Helper()
	if selfModel == nil {
		selfModel = memrepo.NewSelfModel()
	}
	if triggers == nil {
		triggers = memrepo.NewTriggers()
	}
	svc := brain.NewConsolidateService(fixedClock{now: now}, v.cfg, memrepo.NewUnits(), memrepo.NewRelations(), &counterIDs{}, v.log, nil, nil,
		selfModel, memrepo.NewState(), memrepo.NewPendingQuestions(), v.signals, triggers)
	if _, err := svc.Consolidate(context.Background(), brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("%s at %s: %v", phase, now, err)
	}
}

func (v *i52Vault) days(t *testing.T) int {
	t.Helper()
	cfg, err := v.cfg.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return consolidation.ResolveGoalStagnationDays(cfg.GoalStagnationDays)
}

func (v *i52Vault) cadenceRows(t *testing.T) []ports.Decision {
	t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out []ports.Decision
	for _, d := range all {
		if d.Action == ports.ActionLearnGoalCadence {
			out = append(out, d)
		}
	}
	return out
}

func repeatSignal(typ ports.SignalType, n int) []ports.SignalType {
	out := make([]ports.SignalType, n)
	for i := range out {
		out[i] = typ
	}
	return out
}

func TestI52_FiveIgnoredGoalCheckInsLengthenTheInterval(t *testing.T) {
	v := newI52Vault()
	v.closed(t, "b-run", repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)
	if d := v.days(t); d != 28 {
		t.Fatalf("goal_stagnation_days = %d, want 21 -> 28", d)
	}
	rows := v.cadenceRows(t)
	if len(rows) != 1 || !rows[0].OccurredAt.Equal(i52Night) ||
		!strings.Contains(rows[0].Rationale, "21 → 28") || !strings.Contains(rows[0].Rationale, "5 of the last 5") {
		t.Fatalf("rows = %+v, want one at the pass's instant saying 21 → 28 on 5 of the last 5 ignored", rows)
	}
	var d struct {
		Previous, Next, Ignored int
		SignalIDs               []string `json:"signal_ids"`
	}
	if err := json.Unmarshal(rows[0].Context, &d); err != nil || d.Previous != 21 || d.Next != 28 || d.Ignored != 5 || len(d.SignalIDs) != 5 {
		t.Errorf("context %s: %+v, %v", rows[0].Context, d, err)
	}
	cfg, _ := v.cfg.Load(context.Background())
	if cfg.GoalCalibratedAt == nil || !cfg.GoalCalibratedAt.Equal(i52Night) {
		t.Errorf("calibrated at %v, want the pass's instant", cfg.GoalCalibratedAt)
	}
	// The same night, and a night past the new interval, change nothing more.
	for _, again := range []time.Time{i52Night, i52Night.Add(29 * 24 * time.Hour)} {
		v.run(t, consolidation.PhaseLearn, again, nil, nil)
	}
	if d := v.days(t); d != 28 || len(v.cadenceRows(t)) != 1 {
		t.Errorf("after re-running: %d days, %d rows; want 28 and one", d, len(v.cadenceRows(t)))
	}
}

func TestI52_ThreeEngagedShortenIt(t *testing.T) {
	v := newI52Vault()
	v.closed(t, "b-run", ports.SignalNudgeEngaged, ports.SignalNudgeIgnored, ports.SignalNudgeEngaged, ports.SignalNudgeDeclined, ports.SignalNudgeEngaged)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)
	if d := v.days(t); d != 14 {
		t.Errorf("goal_stagnation_days = %d, want 21 -> 14", d)
	}
}

// TestI52_AnAdminSavedIntervalIsNeverWritten: client answer 6 of 2026-10-27.
func TestI52_AnAdminSavedIntervalIsNeverWritten(t *testing.T) {
	v := newI52Vault()
	if err := v.cfg.SetGoalStagnationDays(context.Background(), 21, i52Night.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	v.closed(t, "b-run", repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)
	if d, n := v.days(t), len(v.cadenceRows(t)); d != 21 || n != 0 {
		t.Errorf("a pinned interval: %d days, %d rows; want 21 and none", d, n)
	}
}

// TestI52_OnlyGoalCheckInsAreEvidence: a nudge about no goal, or a snooze
// (nudge_ack, which closes nothing), does not count.
func TestI52_OnlyGoalCheckInsAreEvidence(t *testing.T) {
	v := newI52Vault()
	v.closed(t, "", repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.closed(t, "b-run", repeatSignal(ports.SignalNudgeAck, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)
	if d, n := v.days(t), len(v.cadenceRows(t)); d != 21 || n != 0 {
		t.Errorf("%d days, %d rows; want 21 and none", d, n)
	}
}

// TestI52_TheDeclineFactorFollowsTheLearnedInterval: once learning set 28, a
// goal whose check-in was declined waits 56 days, twice the interval in force.
func TestI52_TheDeclineFactorFollowsTheLearnedInterval(t *testing.T) {
	v := newI52Vault()
	v.closed(t, "b-run", repeatSignal(ports.SignalNudgeIgnored, 5)...)
	v.run(t, consolidation.PhaseLearn, i52Night, nil, nil)

	ctx := context.Background()
	selfModel, triggers := memrepo.NewSelfModel(), memrepo.NewTriggers()
	long := i52Night.AddDate(0, 0, -200)
	if err := selfModel.UpsertByTopicKey(ctx, ports.Belief{ID: "b-german", Facet: "goal", TopicKey: "goal/german", Content: "aprender alemán",
		Confidence: 0.8, Origin: "user_stated", Status: "active", LastReinforcedAt: long, CreatedAt: long, UpdatedAt: long}); err != nil {
		t.Fatal(err)
	}
	armed := i52Night.AddDate(0, 0, -50)
	if err := triggers.Create(ctx, ports.Trigger{ID: "g-1", Kind: ports.TriggerKindPatternBased, FireAt: &armed, CreatedAt: armed,
		Payload: ports.TriggerPayload{ActionText: "x", BeliefID: "b-german"}}); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Fire(ctx, "g-1", armed); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Surface(ctx, "g-1", armed); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Resolve(ctx, "g-1", ports.ResolutionDeclined, armed); err != nil {
		t.Fatal(err)
	}
	v.run(t, consolidation.PhasePatternEval, i52Night, selfModel, triggers) // day 50 of 56
	v.run(t, consolidation.PhasePatternEval, i52Night.AddDate(0, 0, 6), selfModel, triggers)
	if n := len(triggers.All()); n != 2 {
		t.Errorf("%d check-ins, want the declined one and one armed at day 56 only", n)
	}
	for _, trg := range triggers.All() {
		if trg.ID != "g-1" && !trg.CreatedAt.Equal(i52Night.AddDate(0, 0, 6)) {
			t.Errorf("armed at %s, want day 56", trg.CreatedAt)
		}
	}
}
