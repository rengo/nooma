//go:build integration

package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestSignalRepo_Contract runs the same repocontract.RunSignalRepo suite
// the in-memory fake answers at L2, now against a real temporary SQLite
// vault at L3 — design D6's "answered twice" standing rule.
func TestSignalRepo_Contract(t *testing.T) {
	repocontract.RunSignalRepo(t, func(t *testing.T) ports.SignalRepo {
		return NewSignalRepo(openTestVault(t))
	})
}

// TestSignalRepo_RecordSurvivesATargetThatNeverExisted is design D6's own
// L3 case and I13's behavioural half (task 12e.2): learning_signals.target_id
// carries no REFERENCES clause (migration 0002:13, confirmed directly off
// the DDL by TestI13_LearningSignalHasNoFKToTarget), so a signal naming a
// unit id that was never created must persist and read back against a real
// vault opened with foreign_keys=on — the one property the in-memory fake
// could never disprove, because it enforces no foreign key at all (C11's
// lesson: a promise only the store can keep lives at L3).
func TestSignalRepo_RecordSurvivesATargetThatNeverExisted(t *testing.T) {
	v := openTestVault(t)
	repo := NewSignalRepo(v)
	ctx := context.Background()

	targetKind := ports.TargetKindUnit
	targetID := "unit-never-created"
	s := ports.Signal{
		ID:         "signal-orphan-target",
		Type:       ports.SignalCorrection,
		Valence:    ports.ValenceNegative,
		TargetKind: &targetKind,
		TargetID:   &targetID,
		OccurredAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}

	if err := repo.Record(ctx, s); err != nil {
		t.Fatalf("Record for a target that never existed = %v, want nil error — "+
			"learning_signals.target_id carries no REFERENCES clause (migration 0002:13)", err)
	}

	got, err := repo.Since(ctx, s.OccurredAt.Add(-time.Minute), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Since returned %d signals, want 1: %v", len(got), got)
	}
	if got[0].TargetID == nil || *got[0].TargetID != targetID {
		t.Errorf("TargetID = %v, want %q", got[0].TargetID, targetID)
	}
}
