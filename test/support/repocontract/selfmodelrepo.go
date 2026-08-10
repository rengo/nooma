package repocontract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// RunActiveBeliefs runs the ports.SelfModelRepo.ActiveBeliefs contract
// against a fresh repository instance, built by newRepo for every subtest.
// newRepo must return a repository with no belief already stored in it.
//
// Spec R2.3; design §4.3. ActiveBeliefs is the one read both derive and
// pattern_eval share — every facet, no status parameter on the call.
func RunActiveBeliefs(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	t.Run("returns every active belief across every facet, and excludes non-active beliefs", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		facets := []selfmodel.Facet{
			selfmodel.FacetIdentity, selfmodel.FacetValue, selfmodel.FacetGoal,
			selfmodel.FacetSocial, selfmodel.FacetPreference,
		}
		wantIDs := make(map[string]bool, len(facets))
		for _, f := range facets {
			b := fixtureBelief(string(f)+"-active", f, string(f)+"-active-key")
			b.Status = "active"
			if err := repo.UpsertByTopicKey(ctx, b); err != nil {
				t.Fatalf("UpsertByTopicKey %s: %v", b.ID, err)
			}
			wantIDs[b.ID] = true
		}

		// A non-active belief in the same facet set must not appear —
		// ActiveBeliefs carries no status parameter but still must filter.
		inactive := fixtureBelief("goal-merged", selfmodel.FacetGoal, "goal-merged-key")
		inactive.Status = "merged"
		if err := repo.UpsertByTopicKey(ctx, inactive); err != nil {
			t.Fatalf("UpsertByTopicKey %s: %v", inactive.ID, err)
		}

		got, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		if len(got) != len(facets) {
			t.Fatalf("ActiveBeliefs() returned %d beliefs, want %d (one per facet, the merged one excluded): %v",
				len(got), len(facets), got)
		}
		gotIDs := make(map[string]bool, len(got))
		gotFacets := make(map[selfmodel.Facet]bool, len(got))
		for _, b := range got {
			gotIDs[b.ID] = true
			gotFacets[b.Facet] = true
			if b.Status != "active" {
				t.Errorf("ActiveBeliefs() returned belief %s with status %q, want only status = active", b.ID, b.Status)
			}
		}
		for _, f := range facets {
			if !gotFacets[f] {
				t.Errorf("ActiveBeliefs() omitted facet %s — every facet must be included, no facet filter at this port", f)
			}
		}
		if gotIDs[inactive.ID] {
			t.Errorf("ActiveBeliefs() included %s, whose status is %q, not active", inactive.ID, inactive.Status)
		}
	})

	t.Run("no belief stored returns none", func(t *testing.T) {
		repo := newRepo(t)

		got, err := repo.ActiveBeliefs(context.Background())
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ActiveBeliefs() = %v, want none", got)
		}
	})
}

// RunUpsertByTopicKey runs the ports.SelfModelRepo.UpsertByTopicKey
// contract against a fresh repository instance, built by newRepo for every
// subtest. newRepo must return a repository with no belief already stored
// in it.
//
// Spec R2.1; design §4.3. Writing the same TopicKey twice produces one
// row, updated in place — RelationRepo.Upsert's own pattern, applied to
// self_beliefs.topic_key (UNIQUE, migration 0001:75).
func RunUpsertByTopicKey(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	t.Run("writing the same topic_key twice updates in place, keyed on topic_key not id", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		first := fixtureBelief("belief-1", selfmodel.FacetGoal, "goal/shared-key")
		if err := repo.UpsertByTopicKey(ctx, first); err != nil {
			t.Fatalf("first UpsertByTopicKey: %v", err)
		}

		// A different caller-supplied ID over the same TopicKey — a plausible
		// wrong implementation keyed on ID would create a second row here
		// instead of updating the first (the exact ID-vs-topic_key confusion
		// the two-write-method split exists to make unreachable, design
		// §4.3). The correct implementation keeps the FIRST row's identity
		// and updates its content.
		second := first
		second.ID = "belief-2"
		second.Content = "revised content"
		second.Confidence = 0.9
		if err := repo.UpsertByTopicKey(ctx, second); err != nil {
			t.Fatalf("second UpsertByTopicKey over the same topic_key: %v", err)
		}

		got, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("ActiveBeliefs() returned %d beliefs after two UpsertByTopicKey calls over one "+
				"topic_key, want 1 (updated in place, not duplicated): %v", len(got), got)
		}
		if got[0].ID != first.ID {
			t.Errorf("belief id = %q, want %q — the second write's id must not replace the first "+
				"row's identity (topic_key is the conflict target, not id)", got[0].ID, first.ID)
		}
		if got[0].Content != second.Content {
			t.Errorf("belief content = %q, want %q — the second write's content must be reflected",
				got[0].Content, second.Content)
		}
	})

	t.Run("distinct topic_keys leave distinct rows", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		a := fixtureBelief("belief-a", selfmodel.FacetGoal, "goal/a")
		b := fixtureBelief("belief-b", selfmodel.FacetGoal, "goal/b")
		if err := repo.UpsertByTopicKey(ctx, a); err != nil {
			t.Fatalf("UpsertByTopicKey a: %v", err)
		}
		if err := repo.UpsertByTopicKey(ctx, b); err != nil {
			t.Fatalf("UpsertByTopicKey b: %v", err)
		}

		got, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("ActiveBeliefs() returned %d beliefs, want 2 (distinct topic_keys): %v", len(got), got)
		}
	})
}

// RunReinforceByID runs the ports.SelfModelRepo.ReinforceByID contract
// against a fresh repository instance, built by newRepo for every subtest.
// newRepo must return a repository with no belief already stored in it.
//
// Spec R2.2; design §4.3. Reinforcing an existing id changes only
// Confidence and LastReinforcedAt; reinforcing an absent id returns
// ErrBeliefNotFound and creates no row.
func RunReinforceByID(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	t.Run("reinforcing an existing id changes only confidence and last_reinforced_at", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		b := fixtureBelief("belief-reinforce", selfmodel.FacetValue, "value/reinforce")
		b.Confidence = 0.5
		b.LastReinforcedAt = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		if err := repo.UpsertByTopicKey(ctx, b); err != nil {
			t.Fatalf("UpsertByTopicKey: %v", err)
		}

		reinforcedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
		if err := repo.ReinforceByID(ctx, b.ID, 0.8, reinforcedAt); err != nil {
			t.Fatalf("ReinforceByID: %v", err)
		}

		got, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("ActiveBeliefs() returned %d beliefs, want 1 (reinforce touches an existing row, "+
				"it does not add one): %v", len(got), got)
		}
		reinforced := got[0]
		if reinforced.Confidence != 0.8 {
			t.Errorf("Confidence = %v, want 0.8", reinforced.Confidence)
		}
		if !reinforced.LastReinforcedAt.Equal(reinforcedAt) {
			t.Errorf("LastReinforcedAt = %v, want %v", reinforced.LastReinforcedAt, reinforcedAt)
		}
		if reinforced.TopicKey != b.TopicKey {
			t.Errorf("TopicKey = %q, want unchanged %q", reinforced.TopicKey, b.TopicKey)
		}
		if reinforced.Content != b.Content {
			t.Errorf("Content = %q, want unchanged %q", reinforced.Content, b.Content)
		}
		if reinforced.Facet != b.Facet {
			t.Errorf("Facet = %q, want unchanged %q", reinforced.Facet, b.Facet)
		}
		if reinforced.Origin != b.Origin {
			t.Errorf("Origin = %q, want unchanged %q", reinforced.Origin, b.Origin)
		}
	})

	t.Run("reinforcing an absent id returns ErrBeliefNotFound and creates no row", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		err := repo.ReinforceByID(ctx, "no-such-belief", 0.9, time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC))
		if !errors.Is(err, ports.ErrBeliefNotFound) {
			t.Fatalf("ReinforceByID(absent) error = %v, want ports.ErrBeliefNotFound", err)
		}

		got, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ActiveBeliefs() = %v after a failed ReinforceByID, want none — a not-found "+
				"reinforcement must never silently create a row", got)
		}
	})
}

// fixtureBelief builds a minimal, valid, active ports.Belief.
func fixtureBelief(id string, facet selfmodel.Facet, topicKey string) ports.Belief {
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return ports.Belief{
		ID:               id,
		Facet:            facet,
		TopicKey:         topicKey,
		Content:          "fixture content for " + id,
		Confidence:       0.5,
		Origin:           "derived",
		SourceUnitID:     nil,
		Status:           "active",
		LastReinforcedAt: at,
		CreatedAt:        at,
		UpdatedAt:        at,
	}
}
