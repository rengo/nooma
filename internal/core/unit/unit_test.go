package unit

import (
	"encoding/json"
	"testing"
	"time"
)

// TestUnit_FieldsCompileWithDesignedShape is a shape check, not a
// behavior test — Unit carries no logic of its own in Phase A (task
// 2.10). It exists so design D9's presence guard
// (test/conformance/core_exported_decls_have_tests_test.go) has a
// reference to name, and so a future field-type change (e.g. Confidence
// drifting from *float64 to float64) fails to compile here first, before
// it reaches PR 3's fake or PR 4's SQLite implementation.
func TestUnit_FieldsCompileWithDesignedShape(t *testing.T) {
	now := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	confidence := 0.9

	u := Unit{
		ID:              "u1",
		Type:            TypeTask,
		Content:         "buy milk",
		Status:          StatusPool,
		Weight:          1.0,
		WeightDecayRate: 0.01,
		LastTouchedAt:   now,
		StructuredData:  json.RawMessage(`{"k":"v"}`),
		Source:          "chat",
		EventAt:         &now,
		DueAt:           nil,
		Confidence:      &confidence,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if u.DueAt != nil {
		t.Error("DueAt should be nil when never set — a pointer, not the zero time")
	}
	if u.EventAt == nil || !u.EventAt.Equal(now) {
		t.Error("EventAt should carry the assigned instant")
	}
	if u.StructuredData == nil {
		t.Error("StructuredData should be the assigned raw JSON, not nil")
	}
}
