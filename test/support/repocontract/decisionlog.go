package repocontract

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// RunDecisionLog runs the ports.DecisionLog contract against a fresh
// implementation built by newRepo for every subtest. newRepo must return a
// repository holding no decision already recorded.
//
// decision_log carries no foreign key (0001:95-102, confirmed directly off
// the DDL rather than assumed — design D6's "answered twice" rule caught
// C11's opposite mistake, an EmbeddingRepo contract that Put embeddings for
// units that were never inserted, which foreign_keys=on rejected at L3 and
// the fake let pass silently). There is therefore no EnsureUnit-shaped
// harness here: every case below is satisfiable by the real schema as
// written, checked against 0001:95-102 before being written, not after.
func RunDecisionLog(t *testing.T, newRepo func(t *testing.T) ports.DecisionLog) {
	t.Helper()

	t.Run("Record then Since returns the decision", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		d := fixtureDecision("decision-1", ports.ActionCaptureClassify,
			time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))

		if err := repo.Record(ctx, d); err != nil {
			t.Fatalf("Record: %v", err)
		}

		got, err := repo.Since(ctx, d.OccurredAt.Add(-time.Minute), 10)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Since returned %d decisions, want 1: %v", len(got), got)
		}
		if !reflect.DeepEqual(got[0], d) {
			t.Fatalf("Since round-trip: got %+v, want %+v", got[0], d)
		}
	})

	// decision_log.id is a PRIMARY KEY (0001:96). A caller-generated id
	// colliding is a bug worth surfacing, not silently overwriting an
	// existing audit row — an audit trail that can be overwritten in place
	// is not one. This case exists precisely because C11 showed a contract
	// answered by only one implementation is that implementation's opinion:
	// an in-memory fake keyed on a map could silently upsert on a duplicate
	// id and never notice it disagrees with the schema's PRIMARY KEY.
	t.Run("Record on a duplicate id returns ErrDecisionExists", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		d := fixtureDecision("decision-dup", ports.ActionCaptureClassify, at)

		if err := repo.Record(ctx, d); err != nil {
			t.Fatalf("first Record: %v", err)
		}
		second := fixtureDecision("decision-dup", ports.ActionRelationDiscarded, at.Add(time.Minute))
		if err := repo.Record(ctx, second); !errors.Is(err, ports.ErrDecisionExists) {
			t.Fatalf("second Record: got %v, want ErrDecisionExists", err)
		}
	})

	// "after t", not "at or after t": a caller reads forward from a cursor
	// by passing the occurred_at of the last decision it already saw. An
	// inclusive bound would return that same row again on every following
	// call.
	t.Run("Since excludes a decision occurring exactly at t", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		d := fixtureDecision("decision-boundary", ports.ActionCaptureClassify, at)
		if err := repo.Record(ctx, d); err != nil {
			t.Fatalf("Record: %v", err)
		}

		got, err := repo.Since(ctx, at, 10)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("Since(t) returned %v, want none — a decision exactly at t must not be "+
				"re-delivered by a caller reading forward from t as its cursor", got)
		}
	})

	// Ascending occurred_at order, bounded by limit — the read-forward-from-
	// a-cursor shape Since exists for (docs/02-cognitive-core.md §11's
	// "Pull: everything is recorded and explorable").
	t.Run("Since orders by occurred_at ascending and bounds by limit", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

		third := fixtureDecision("decision-third", ports.ActionCaptureUnitCreated, base.Add(3*time.Minute))
		first := fixtureDecision("decision-first", ports.ActionCaptureClassify, base.Add(1*time.Minute))
		second := fixtureDecision("decision-second", ports.ActionCaptureDiscarded, base.Add(2*time.Minute))
		for _, d := range []ports.Decision{third, first, second} {
			if err := repo.Record(ctx, d); err != nil {
				t.Fatalf("Record %s: %v", d.ID, err)
			}
		}

		got, err := repo.Since(ctx, base, 2)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		wantIDs := []string{first.ID, second.ID}
		gotIDs := make([]string, len(got))
		for i, d := range got {
			gotIDs[i] = d.ID
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("Since(base, 2) = %v, want the two earliest after base, ascending: %v",
				gotIDs, wantIDs)
		}
	})

	// The taxonomy's own completeness, design D9's closed-vocabulary
	// pattern, following unit.Status's precedent
	// (internal/core/unit/status_test.go). It needs no repository instance,
	// but lives here because it is part of what design D9 calls "the
	// DecisionLog contract" and this suite is where 10a's single RED
	// ("undefined: ports.DecisionLog") is meant to come from.
	t.Run("AllDecisionActions returns exactly the thirty-one design D9/D5/§7.5 members", func(t *testing.T) {
		want := map[ports.DecisionAction]bool{
			ports.ActionCaptureClassify:                 true,
			ports.ActionCaptureUnparseable:              true,
			ports.ActionCaptureUnclassifiable:           true,
			ports.ActionCaptureDiscarded:                true,
			ports.ActionCaptureUnitCreated:              true,
			ports.ActionCaptureEmbeddingFailed:          true,
			ports.ActionCaptureDedupFailed:              true,
			ports.ActionCapturePersonRefAmbiguous:       true,
			ports.ActionCaptureArmedTimer:               true,
			ports.ActionCaptureArmedTrigger:             true,
			ports.ActionCaptureArmedRecurring:           true,
			ports.ActionCaptureArmRefused:               true,
			ports.ActionCheckTriggerExpired:             true,
			ports.ActionCheckTimerFired:                 true,
			ports.ActionCheckTimerCancelled:             true,
			ports.ActionCaptureDedupJudged:              true,
			ports.ActionRelationPersisted:               true,
			ports.ActionRelationDiscarded:               true,
			ports.ActionRelationDuplicateRecorded:       true,
			ports.ActionCorrectionApplied:               true,
			ports.ActionCorrectionAmbiguous:             true,
			ports.ActionExpireIncompleteTransitioned:    true,
			ports.ActionArchiveArchived:                 true,
			ports.ActionArchiveConflictSkipped:          true,
			ports.ActionStrengthenApplied:               true,
			ports.ActionConnectRelationPersisted:        true,
			ports.ActionDeriveBeliefCreated:             true,
			ports.ActionDeriveBeliefReinforced:          true,
			ports.ActionReweightBoostApplied:            true,
			ports.ActionPatternEvalStagnationFound:      true,
			ports.ActionPatternEvalLoadHypothesisOpened: true,
		}

		got := ports.AllDecisionActions()
		if len(got) != len(want) {
			t.Fatalf("AllDecisionActions() returned %d members, want %d: %v", len(got), len(want), got)
		}
		seen := make(map[ports.DecisionAction]bool, len(got))
		for _, a := range got {
			if !want[a] {
				t.Errorf("AllDecisionActions() contains unexpected member %q", a)
			}
			if seen[a] {
				t.Errorf("AllDecisionActions() contains %q twice", a)
			}
			seen[a] = true
		}
	})
}

// fixtureDecision builds a minimal, valid ports.Decision. Context is always
// non-empty and explicit: whether an absent Context ends up '{}' is
// migration 0001's own DDL default, a store-only promise proved at L3
// (internal/store/sqlite/decisionlog_integration_test.go), not asserted
// here — the in-memory fake enforces no column default at all, so a case
// resting on it would pass over the fake and prove nothing about the store.
func fixtureDecision(id string, action ports.DecisionAction, at time.Time) ports.Decision {
	return ports.Decision{
		ID:         id,
		Action:     action,
		Rationale:  "fixture rationale for " + id,
		Context:    json.RawMessage(`{"fixture":true}`),
		OccurredAt: at,
	}
}
