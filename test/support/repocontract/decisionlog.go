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
		second := fixtureDecision("decision-second", ports.ActionCaptureOutOfScope, base.Add(2*time.Minute))
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

	// FX-A (m4e-activity design §6): five rows, three sharing one instant,
	// written in an order that differs from both id order and time order, so
	// neither an id tie-break nor an ascending read can pass by accident. The
	// tied group straddles the page boundary at page size 2.
	//
	//	write order: z-a(T2) m-old(T1) a-b(T2) k-new(T3) c-c(T2)
	//	newest first: k-new, c-c, a-b, z-a, m-old
	t.Run("Before orders newest first, tied rows in reverse write order", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fx := writeFXA(ctx, t, repo)

		got, err := repo.Before(ctx, nil, "", 10)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if ids := rowIDs(got); !reflect.DeepEqual(ids, fx.newestFirst) {
			t.Fatalf("Before(nil, \"\", 10) = %v, want %v (occurred_at DESC, then reverse write order)",
				ids, fx.newestFirst)
		}
		if !reflect.DeepEqual(got[0].Decision, fx.byID["k-new"]) {
			t.Fatalf("Before round-trip: got %+v, want %+v", got[0].Decision, fx.byID["k-new"])
		}
	})

	t.Run("Before walks every row exactly once across pages, tie group included", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fx := writeFXA(ctx, t, repo)

		var walked []string
		var cursor *ports.DecisionCursor
		for pages := 0; pages < 10; pages++ {
			page, err := repo.Before(ctx, cursor, "", 2)
			if err != nil {
				t.Fatalf("Before page %d: %v", pages, err)
			}
			if len(page) == 0 {
				break
			}
			walked = append(walked, rowIDs(page)...)
			last := page[len(page)-1]
			cursor = &ports.DecisionCursor{OccurredAt: last.OccurredAt, Seq: last.Seq}
		}
		if !reflect.DeepEqual(walked, fx.newestFirst) {
			t.Fatalf("paged walk = %v, want every row once, newest first: %v", walked, fx.newestFirst)
		}
	})

	t.Run("Before with an action prefix returns only that family", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		rows := []ports.Decision{
			fixtureDecision("d-check", ports.ActionCheckTimerFired, at),
			fixtureDecision("d-checkin", ports.ActionCaptureCheckInResolved, at.Add(time.Second)),
			fixtureDecision("d-check2", ports.ActionCheckDigestSent, at.Add(2*time.Second)),
			// "a_b." must match literally: "_" is a LIKE wildcard, not here.
			fixtureDecision("d-under", "a_b.x", at.Add(3*time.Second)),
			fixtureDecision("d-lookalike", "aXb.x", at.Add(4*time.Second)),
		}
		for _, d := range rows {
			if err := repo.Record(ctx, d); err != nil {
				t.Fatalf("Record %s: %v", d.ID, err)
			}
		}

		cases := []struct {
			prefix string
			want   []string
		}{
			{"check.", []string{"d-check2", "d-check"}},
			{"capture.checkin.", []string{"d-checkin"}},
			{"a_b.", []string{"d-under"}},
			{"nomatch.", nil},
			{"", []string{"d-lookalike", "d-under", "d-check2", "d-checkin", "d-check"}},
		}
		for _, c := range cases {
			got, err := repo.Before(ctx, nil, c.prefix, 10)
			if err != nil {
				t.Fatalf("Before prefix %q: %v", c.prefix, err)
			}
			if ids := rowIDs(got); !reflect.DeepEqual(ids, c.want) {
				t.Errorf("Before(prefix %q) = %v, want %v", c.prefix, ids, c.want)
			}
		}
	})

	t.Run("Before bounds the page by limit and returns nothing for limit below one", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fx := writeFXA(ctx, t, repo)

		for _, limit := range []int{0, -1} {
			got, err := repo.Before(ctx, nil, "", limit)
			if err != nil {
				t.Fatalf("Before limit %d: %v", limit, err)
			}
			if len(got) != 0 {
				t.Errorf("Before(limit %d) = %v, want an empty page", limit, rowIDs(got))
			}
		}
		got, err := repo.Before(ctx, nil, "", 2)
		if err != nil {
			t.Fatalf("Before limit 2: %v", err)
		}
		if ids := rowIDs(got); !reflect.DeepEqual(ids, fx.newestFirst[:2]) {
			t.Errorf("Before(limit 2) = %v, want the two newest: %v", ids, fx.newestFirst[:2])
		}
	})

	t.Run("Before treats a nil cursor as the newest and a zero cursor as the oldest", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		writeFXA(ctx, t, repo)

		got, err := repo.Before(ctx, nil, "", 1)
		if err != nil {
			t.Fatalf("Before nil: %v", err)
		}
		if ids := rowIDs(got); !reflect.DeepEqual(ids, []string{"k-new"}) {
			t.Errorf("Before(nil, limit 1) = %v, want the newest row", ids)
		}
		got, err = repo.Before(ctx, &ports.DecisionCursor{}, "", 10)
		if err != nil {
			t.Fatalf("Before zero cursor: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Before(zero cursor) = %v, want none: nothing is older than the zero time", rowIDs(got))
		}
	})

	t.Run("Before reports an insertion sequence that increases strictly with write order", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		fx := writeFXA(ctx, t, repo)

		got, err := repo.Before(ctx, nil, "", 10)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		seqs := make(map[string]int64, len(got))
		for _, r := range got {
			seqs[r.ID] = r.Seq
		}
		for i := 1; i < len(fx.writeOrder); i++ {
			prev, cur := fx.writeOrder[i-1], fx.writeOrder[i]
			if seqs[cur] <= seqs[prev] {
				t.Errorf("Seq(%s)=%d is not greater than Seq(%s)=%d, written earlier",
					cur, seqs[cur], prev, seqs[prev])
			}
		}
	})

	// The taxonomy's own completeness, design D9's closed-vocabulary
	// pattern, following unit.Status's precedent
	// (internal/core/unit/status_test.go). It needs no repository instance,
	// but lives here because it is part of what design D9 calls "the
	// DecisionLog contract" and this suite is where 10a's single RED
	// ("undefined: ports.DecisionLog") is meant to come from.
	t.Run("AllDecisionActions returns exactly the fifty-two design D9/D5/§7.5/ADR-0021/m3e/m4c/m4e members", func(t *testing.T) {
		want := map[ports.DecisionAction]bool{
			ports.ActionCaptureClassify:                 true,
			ports.ActionCaptureUnparseable:              true,
			ports.ActionCaptureUnclassifiable:           true,
			ports.ActionCaptureConversed:                true,
			ports.ActionCaptureOutOfScope:               true,
			ports.ActionCaptureUnitCreated:              true,
			ports.ActionCaptureEmbeddingFailed:          true,
			ports.ActionCaptureDedupFailed:              true,
			ports.ActionCaptureChatFailed:               true,
			ports.ActionCapturePersonRefAmbiguous:       true,
			ports.ActionCaptureArmedTimer:               true,
			ports.ActionCaptureArmedTrigger:             true,
			ports.ActionCaptureArmedRecurring:           true,
			ports.ActionCaptureArmRefused:               true,
			ports.ActionCheckTriggerExpired:             true,
			ports.ActionCheckTimerFired:                 true,
			ports.ActionCheckTimerCancelled:             true,
			ports.ActionCheckConflictSkipped:            true,
			ports.ActionCheckTriggerFired:               true,
			ports.ActionCheckTriggerDelivered:           true,
			ports.ActionCheckDeliveryFailed:             true,
			ports.ActionCheckDigestSent:                 true,
			ports.ActionCheckDigestHeld:                 true,
			ports.ActionCheckFocusUnavailable:           true,
			ports.ActionCheckTimerRephraseFailed:        true,
			ports.ActionCaptureCheckInResolved:          true,
			ports.ActionCaptureCheckInUnmatched:         true,
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
			ports.ActionDeriveBeliefSkipped:             true,
			ports.ActionDeriveRetiredEmbedFailed:        true,
			ports.ActionReweightBoostApplied:            true,
			ports.ActionPatternEvalStagnationFound:      true,
			ports.ActionPatternEvalLoadHypothesisOpened: true,
			ports.ActionConnectQuestionCreated:          true,
			ports.ActionCheckDigestQuestionAsked:        true,
			ports.ActionCheckQuestionExpired:            true,
			ports.ActionCaptureRelationCheckInResolved:  true,
			ports.ActionCaptureRelationCheckInUnmatched: true,
			ports.ActionBeliefEdited:                    true,
			ports.ActionBeliefRetired:                   true,
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

// fxA is the FX-A fixture: the ids in write order and in the order Before
// must return them.
type fxA struct {
	writeOrder  []string
	newestFirst []string
	byID        map[string]ports.Decision
}

// writeFXA records the FX-A rows into repo. Every instant is a whole second:
// SQLite stores seconds and the in-memory fake keeps nanoseconds, so a
// sub-second fixture would let the two implementations diverge on a boundary.
func writeFXA(ctx context.Context, t *testing.T, repo ports.DecisionLog) fxA {
	t.Helper()
	t1 := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	t2, t3 := t1.Add(time.Minute), t1.Add(2*time.Minute)
	fx := fxA{
		writeOrder:  []string{"z-a", "m-old", "a-b", "k-new", "c-c"},
		newestFirst: []string{"k-new", "c-c", "a-b", "z-a", "m-old"},
		byID:        map[string]ports.Decision{},
	}
	at := map[string]time.Time{"z-a": t2, "m-old": t1, "a-b": t2, "k-new": t3, "c-c": t2}
	for _, id := range fx.writeOrder {
		if at[id].Nanosecond() != 0 {
			t.Fatalf("fixture %s has a sub-second instant %v", id, at[id])
		}
		d := fixtureDecision(id, ports.ActionCaptureClassify, at[id])
		if err := repo.Record(ctx, d); err != nil {
			t.Fatalf("Record %s: %v", id, err)
		}
		fx.byID[id] = d
	}
	return fx
}

func rowIDs(rows []ports.DecisionRow) []string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}
