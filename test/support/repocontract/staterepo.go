package repocontract

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// RunOpenHypothesis runs the ports.StateRepo.OpenHypothesis contract
// against a fresh repository instance, built by newRepo for every subtest.
// newRepo must return a repository holding no current_state row.
//
// Design §4.4. OpenHypothesis is append-only: two calls append two rows,
// neither call updates the other.
func RunOpenHypothesis(t *testing.T, newRepo func(t *testing.T) ports.StateRepo) {
	t.Helper()

	t.Run("two calls append two rows, neither updates the other", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		first := ports.StateHypothesis{
			ID:         "hyp-1",
			Mood:       ports.MoodLoaded,
			RecordedAt: time.Date(2026, 8, 8, 22, 0, 0, 0, time.UTC),
		}
		second := ports.StateHypothesis{
			ID:         "hyp-2",
			Mood:       ports.MoodLoaded,
			RecordedAt: time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC),
		}
		if err := repo.OpenHypothesis(ctx, first); err != nil {
			t.Fatalf("first OpenHypothesis: %v", err)
		}
		if err := repo.OpenHypothesis(ctx, second); err != nil {
			t.Fatalf("second OpenHypothesis: %v", err)
		}

		got, err := repo.LastHypothesisAt(ctx)
		if err != nil {
			t.Fatalf("LastHypothesisAt: %v", err)
		}
		if got == nil || !got.Equal(second.RecordedAt) {
			t.Fatalf("LastHypothesisAt() = %v, want %v — the second call's row must still be "+
				"visible, meaning the first append was never overwritten by the second", got, second.RecordedAt)
		}
	})
}

// RunLastHypothesisAt runs the ports.StateRepo.LastHypothesisAt contract
// against a fresh repository instance, built by newRepo for every subtest.
// newRepo must return a repository holding no current_state row.
//
// Design §4.4. LastHypothesisAt returns the recorded_at of the most
// recent row written with source = consolidation, or nil when there is
// none — it feeds consolidation.EvaluateLoad's lastHypothesisAt parameter
// directly (m2b §9 Q6).
func RunLastHypothesisAt(t *testing.T, newRepo func(t *testing.T) ports.StateRepo) {
	t.Helper()

	t.Run("no row written returns nil", func(t *testing.T) {
		repo := newRepo(t)

		got, err := repo.LastHypothesisAt(context.Background())
		if err != nil {
			t.Fatalf("LastHypothesisAt: %v", err)
		}
		if got != nil {
			t.Errorf("LastHypothesisAt() = %v, want nil", got)
		}
	})

	t.Run("returns the most recent recorded_at among rows this repo wrote", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		older := ports.StateHypothesis{
			ID:         "hyp-older",
			Mood:       ports.MoodLoaded,
			RecordedAt: time.Date(2026, 8, 7, 22, 0, 0, 0, time.UTC),
		}
		newer := ports.StateHypothesis{
			ID:         "hyp-newer",
			Mood:       ports.MoodLoaded,
			RecordedAt: time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC),
		}
		// Written out of chronological order on purpose: a plausible wrong
		// implementation that returns "the last row appended" rather than
		// "the row with the greatest recorded_at" would pass a fixture
		// where insertion order and time order coincide. They do not here.
		if err := repo.OpenHypothesis(ctx, newer); err != nil {
			t.Fatalf("OpenHypothesis newer: %v", err)
		}
		if err := repo.OpenHypothesis(ctx, older); err != nil {
			t.Fatalf("OpenHypothesis older: %v", err)
		}

		got, err := repo.LastHypothesisAt(ctx)
		if err != nil {
			t.Fatalf("LastHypothesisAt: %v", err)
		}
		if got == nil || !got.Equal(newer.RecordedAt) {
			t.Errorf("LastHypothesisAt() = %v, want %v (the greatest recorded_at, not the last "+
				"row appended)", got, newer.RecordedAt)
		}
	})
}
