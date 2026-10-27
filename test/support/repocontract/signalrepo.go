package repocontract

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// RunSignalRepo runs the ports.SignalRepo contract against a fresh
// implementation built by newRepo for every subtest. newRepo must return a
// repository holding no signal already recorded.
//
// learning_signals.target_id carries deliberately no foreign key (migration
// 0002:13, I13) — proving that a signal whose TargetID names a unit that
// was never created still persists is not this suite's job. That is I13's
// behavioural half, and design D6 places it at L3 only
// (internal/store/sqlite/signalrepo_integration_test.go): a promise only
// the store can keep is not provable through a fake that enforces no
// foreign key at all (C11's lesson). This suite proves what both
// implementations owe equally: the vocabulary is closed, and a signal
// written comes back unchanged.
func RunSignalRepo(t *testing.T, newRepo func(t *testing.T) ports.SignalRepo) {
	t.Helper()

	t.Run("AllSignalTypes returns exactly the eleven design D6 members", func(t *testing.T) {
		want := map[ports.SignalType]bool{
			ports.SignalCorrection:      true,
			ports.SignalNudgeAck:        true,
			ports.SignalNudgeIgnored:    true,
			ports.SignalNudgeEngaged:    true,
			ports.SignalNudgeDeclined:   true,
			ports.SignalBeliefDelete:    true,
			ports.SignalBeliefEdit:      true,
			ports.SignalRelationReject:  true,
			ports.SignalRelationConfirm: true,
			ports.SignalStateConfirmed:  true,
			ports.SignalStateDenied:     true,
		}
		got := ports.AllSignalTypes()
		if len(got) != len(want) {
			t.Fatalf("AllSignalTypes() returned %d members, want %d: %v", len(got), len(want), got)
		}
		seen := make(map[ports.SignalType]bool, len(got))
		for _, s := range got {
			if !want[s] {
				t.Errorf("AllSignalTypes() contains unexpected member %q", s)
			}
			if seen[s] {
				t.Errorf("AllSignalTypes() contains %q twice", s)
			}
			seen[s] = true
		}
	})

	t.Run("AllValences returns exactly the three members", func(t *testing.T) {
		want := map[ports.Valence]bool{
			ports.ValencePositive: true,
			ports.ValenceNegative: true,
			ports.ValenceNeutral:  true,
		}
		got := ports.AllValences()
		if len(got) != len(want) {
			t.Fatalf("AllValences() returned %d members, want %d: %v", len(got), len(want), got)
		}
		seen := make(map[ports.Valence]bool, len(got))
		for _, v := range got {
			if !want[v] {
				t.Errorf("AllValences() contains unexpected member %q", v)
			}
			if seen[v] {
				t.Errorf("AllValences() contains %q twice", v)
			}
			seen[v] = true
		}
	})

	// M5 link 2c: the learn pass reads every typed relation verdict.
	t.Run("RelationVerdicts returns the typed relation verdicts, oldest first", func(t *testing.T) {
		ctx := context.Background()
		repo := newRepo(t)
		base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
		typ := "same_topic"
		for i, s := range []ports.Signal{
			{ID: "b-confirm", Type: ports.SignalRelationConfirm, Valence: ports.ValencePositive, RelationType: &typ},
			{ID: "a-reject", Type: ports.SignalRelationReject, Valence: ports.ValenceNegative, RelationType: &typ},
			{ID: "untyped", Type: ports.SignalRelationReject, Valence: ports.ValenceNegative},
			{ID: "nudge", Type: ports.SignalNudgeIgnored, Valence: ports.ValenceNegative},
		} {
			s.Context, s.OccurredAt = json.RawMessage(`{"created_by":"consolidation"}`), base.Add(time.Duration(i)*time.Minute)
			if err := repo.Record(ctx, s); err != nil {
				t.Fatalf("Record %s: %v", s.ID, err)
			}
		}
		got, err := repo.RelationVerdicts(ctx)
		if err != nil {
			t.Fatalf("RelationVerdicts: %v", err)
		}
		if len(got) != 2 || got[0].ID != "b-confirm" || got[1].ID != "a-reject" {
			t.Fatalf("RelationVerdicts = %+v, want b-confirm then a-reject", got)
		}
		if got[1].RelationType == nil || *got[1].RelationType != typ || string(got[1].Context) != `{"created_by":"consolidation"}` {
			t.Errorf("a verdict came back as %+v", got[1])
		}
	})

	// M5 link 5: goal cadence reads how goal check-ins closed.
	t.Run("GoalVerdicts returns the closed goal check-ins, oldest first", func(t *testing.T) {
		ctx := context.Background()
		repo := newRepo(t)
		base := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
		for i, s := range []struct {
			id, ctx string
			typ     ports.SignalType
		}{
			{"b-ignored", `{"belief_id":"b-run"}`, ports.SignalNudgeIgnored},
			{"a-engaged", `{"belief_id":"b-run"}`, ports.SignalNudgeEngaged},
			{"declined", `{"belief_id":"b-german"}`, ports.SignalNudgeDeclined},
			{"acked", `{"belief_id":"b-run"}`, ports.SignalNudgeAck},
			{"task", `{"unit_id":"u"}`, ports.SignalNudgeEngaged},
			{"empty", `{"belief_id":""}`, ports.SignalNudgeIgnored},
		} {
			if err := repo.Record(ctx, ports.Signal{ID: s.id, Type: s.typ, Valence: ports.ValenceNegative,
				Context: json.RawMessage(s.ctx), OccurredAt: base.Add(time.Duration(i) * time.Minute)}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := repo.GoalVerdicts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		if !reflect.DeepEqual(ids, []string{"b-ignored", "a-engaged", "declined"}) {
			t.Errorf("GoalVerdicts = %v, want the three closed goal check-ins in order", ids)
		}
	})

	t.Run("Record then Since returns the signal", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		s := fixtureSignal("signal-1", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))

		if err := repo.Record(ctx, s); err != nil {
			t.Fatalf("Record: %v", err)
		}

		got, err := repo.Since(ctx, s.OccurredAt.Add(-time.Minute), 10)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Since returned %d signals, want 1: %v", len(got), got)
		}
		if !reflect.DeepEqual(got[0], s) {
			t.Fatalf("Since round-trip: got %+v, want %+v", got[0], s)
		}
	})

	// Since is part of the port, not a test affordance (design D6): the
	// invariant that a signal outlives its target is only observable by
	// reading signals back, and reading them back in a deterministic order
	// is what makes a cursor-style caller possible at all.
	t.Run("Since orders by occurred_at ascending and bounds by limit", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

		third := fixtureSignal("signal-third", base.Add(3*time.Minute))
		first := fixtureSignal("signal-first", base.Add(1*time.Minute))
		second := fixtureSignal("signal-second", base.Add(2*time.Minute))
		for _, s := range []ports.Signal{third, first, second} {
			if err := repo.Record(ctx, s); err != nil {
				t.Fatalf("Record %s: %v", s.ID, err)
			}
		}

		got, err := repo.Since(ctx, base, 2)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		wantIDs := []string{first.ID, second.ID}
		gotIDs := make([]string, len(got))
		for i, s := range got {
			gotIDs[i] = s.ID
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("Since(base, 2) = %v, want the two earliest after base, ascending: %v",
				gotIDs, wantIDs)
		}
	})
}

// fixtureSignal builds a minimal, valid ports.Signal shaped like design
// D6's correction signal: TargetKind = unit, a non-nil TargetID,
// Valence = negative, DecisionAction left nil (design D6: the bucket a
// correction is evidence against cannot be identified here, and is not
// guessed).
func fixtureSignal(id string, at time.Time) ports.Signal {
	targetKind := ports.TargetKindUnit
	targetID := "unit-" + id
	return ports.Signal{
		ID:         id,
		Type:       ports.SignalCorrection,
		Valence:    ports.ValenceNegative,
		TargetKind: &targetKind,
		TargetID:   &targetID,
		Context:    json.RawMessage(`{"fixture":true}`),
		OccurredAt: at,
	}
}
