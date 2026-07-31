// Package memrepo — see units.go for the package contract.
package memrepo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// TestUnits_TwoInstancesShareNoMutableState proves R3.3's isolation
// requirement (spec.md R3.3, design D6) — the one property
// repocontract.RunUnitRepo cannot cover, because it is specific to how
// NewUnits constructs a fake, not to what ports.UnitRepo promises.
//
// Three things must all be true, and this test proves all three against a
// single fixture carrying pointer fields (unit.Unit.DueAt, .Confidence,
// .StructuredData) so a shallow struct copy — sharing the pointee instead
// of copying it — cannot pass silently:
//
//  1. A write through one instance is not observable through another
//     independently constructed instance (no shared backing map).
//  2. Mutating the caller's original value after Create does not change
//     what the fake stored (deep copy on the way in).
//  3. Mutating a value returned by ByID does not change what a later ByID
//     call returns (deep copy on the way out) — design D6: "no caller can
//     reach the fake's interior".
func TestUnits_TwoInstancesShareNoMutableState(t *testing.T) {
	ctx := context.Background()
	a := NewUnits()
	b := NewUnits()

	dueAt := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	confidence := 0.5
	structured := json.RawMessage(`{"k":1}`)

	u := unit.Unit{
		ID:              "isolation-1",
		Type:            unit.TypeTask,
		Content:         "original content",
		Status:          unit.StatusPool,
		Weight:          1.0,
		WeightDecayRate: 0.1,
		LastTouchedAt:   dueAt,
		StructuredData:  structured,
		Source:          "isolation test",
		DueAt:           &dueAt,
		Confidence:      &confidence,
		CreatedAt:       dueAt,
		UpdatedAt:       dueAt,
	}

	if err := a.Create(ctx, u); err != nil {
		t.Fatalf("Create through instance a: %v", err)
	}

	t.Run("a write through one instance is not observable through another", func(t *testing.T) {
		if _, err := b.ByID(ctx, u.ID); !errors.Is(err, ports.ErrUnitNotFound) {
			t.Fatalf("ByID on instance b: got %v, want ErrUnitNotFound — a and b share state", err)
		}
	})

	t.Run("mutating the caller's original after Create does not change the stored copy", func(t *testing.T) {
		// Mutate the pointee and the underlying byte slice the caller
		// still holds a reference to, after Create already returned.
		dueAt = dueAt.Add(24 * time.Hour)
		confidence = 0.9
		structured[2] = 'X' // was '1' in `{"k":1}` — corrupt in place

		got, err := a.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.DueAt == nil || !got.DueAt.Equal(time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)) {
			t.Fatalf("DueAt after mutating caller's original: got %v, want unaffected 2026-08-01T09:00:00Z", got.DueAt)
		}
		if got.Confidence == nil || *got.Confidence != 0.5 {
			t.Fatalf("Confidence after mutating caller's original: got %v, want unaffected 0.5", got.Confidence)
		}
		if string(got.StructuredData) != `{"k":1}` {
			t.Fatalf("StructuredData after mutating caller's original: got %s, want unaffected {\"k\":1}", got.StructuredData)
		}
	})

	t.Run("mutating a value read back does not change a later read", func(t *testing.T) {
		got, err := a.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("first ByID: %v", err)
		}
		*got.DueAt = got.DueAt.Add(48 * time.Hour)
		*got.Confidence = 0.1
		got.StructuredData[2] = 'Y'

		again, err := a.ByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("second ByID: %v", err)
		}
		if !again.DueAt.Equal(time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)) {
			t.Fatalf("DueAt after mutating a read value: got %v, want unaffected 2026-08-01T09:00:00Z", again.DueAt)
		}
		if *again.Confidence != 0.5 {
			t.Fatalf("Confidence after mutating a read value: got %v, want unaffected 0.5", *again.Confidence)
		}
		if string(again.StructuredData) != `{"k":1}` {
			t.Fatalf("StructuredData after mutating a read value: got %s, want unaffected {\"k\":1}", again.StructuredData)
		}
	})
}
